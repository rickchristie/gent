package gent

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"
)

// FlushProgress accepts all queued evidence without moving the resumable control boundary.
// It is also the barrier used before external calls, so a failed save stops further side effects.
func (ctx *ExecutionContext) FlushProgress() error {
	return ctx.flushHistory("")
}

func (ctx *ExecutionContext) CommitCheckpoint(phase ExecutionPhase) error {
	return ctx.flushHistory(phase)
}

func (ctx *ExecutionContext) flushHistory(phase ExecutionPhase) error {
	if phase != "" && ctx.Parent() == nil {
		if err := ctx.captureExtensions(); err != nil {
			return ctx.stopRecording(err)
		}
	}
	run := ctx.run
	run.flushMu.Lock()
	locked := true
	defer func() {
		if locked {
			run.flushMu.Unlock()
		}
	}()
	if err := ctx.RecordingError(); err != nil {
		return err
	}
	contexts := run.root.allContexts()
	runtimeState := RuntimeState{}
	for _, child := range contexts {
		runtimeState.Contexts = append(runtimeState.Contexts, child.runtimeSnapshot())
	}
	run.mu.Lock()
	runtimeState.Contexts = append(runtimeState.Contexts, cloneWire(run.archivedRuntime)...)
	runtimeState.Allocation = run.allocation
	var checkpoint *Checkpoint
	var err error
	// Child completion accepts its evidence and runtime without replacing the root boundary.
	if phase != "" && ctx.Parent() == nil {
		checkpoint, err = ctx.prepareCheckpoint(phase, contexts, runtimeState)
	}
	if err == nil && phase == PhaseTerminal && ctx.phase != PhaseTerminal {
		err = ctx.queueFinalContent()
	}
	events := append([]*RecordedEvent(nil), run.pending...)
	pendingCount := len(events)
	run.mu.Unlock()
	if err != nil {
		return ctx.stopRecording(err)
	}
	if pendingCount == 0 && checkpoint == nil {
		return nil
	}
	// A canceled worker must still be able to record already observed evidence. Storage adapters
	// fence stale workers; the independent bounded context is not permission to ignore a lease.
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx.Context()), 30*time.Second)
	defer cancel()
	batch, err := run.accept(saveCtx, &SaveBatch{Events: events, Checkpoint: checkpoint,
		Runtime: runtimeState})
	if err != nil {
		return ctx.stopRecording(err)
	}
	run.mu.Lock()
	for i, event := range events {
		event.RunId, event.EventNumber = batch.Events[i].RunId, batch.Events[i].EventNumber
	}
	clear(run.pending[:pendingCount])
	run.pending = run.pending[pendingCount:]
	if len(run.pending) == 0 {
		run.pending = nil
	}
	if phase != "" {
		ctx.phase = phase
		if phase == PhaseIterationCompleted || phase == PhaseTerminal {
			ctx.completedIteration = ctx.Iteration()
		}
	}
	run.mu.Unlock()
	// Stream queues do not call application code. Deliver under the acceptance lock, before
	// queuing observers, so a delayed observer cannot let Release close a later chunk's stream.
	if err := ctx.emitCommittedChunks(batch.Events); err != nil {
		return ctx.stopRecording(err)
	}
	run.mu.Lock()
	run.notifications = append(run.notifications, committedNotification{ctx, batch.Events})
	run.mu.Unlock()
	run.flushMu.Unlock()
	locked = false
	return ctx.notifyCommitted()
}

type committedNotification struct {
	ctx    *ExecutionContext
	events []*RecordedEvent
}

// Drain in acceptance order, outside the commit lock. A subscriber may publish another event;
// that nested flush queues its notification for this drain instead of recursively taking a lock.
func (ctx *ExecutionContext) notifyCommitted() error {
	run := ctx.run
	if !run.notifyMu.TryLock() {
		return nil
	}
	for {
		run.mu.Lock()
		if len(run.notifications) == 0 {
			run.notifyMu.Unlock()
			run.mu.Unlock()
			return nil
		}
		notification := run.notifications[0]
		run.notifications[0] = committedNotification{}
		run.notifications = run.notifications[1:]
		if len(run.notifications) == 0 {
			run.notifications = nil
		}
		run.mu.Unlock()
		if err := notification.ctx.dispatchCommitted(notification.events); err != nil {
			run.notifyMu.Unlock()
			return ctx.stopRecording(err)
		}
	}
}

func (ctx *ExecutionContext) dispatchCommitted(events []*RecordedEvent) error {
	for _, observer := range ctx.run.observers {
		if observer == nil {
			continue
		}
		owned, err := cloneOwned(ctx.run.codecs, events)
		if err != nil {
			return err
		}
		if err := observer.ObserveCommitted(owned, ctx.run.codecs); err != nil {
			return err
		}
	}
	for _, recorded := range events {
		if recorded.Change == nil {
			continue
		}
		owner := ctx
		for _, candidate := range ctx.run.root.allContexts() {
			if candidate.ContextId() == recorded.ContextId {
				owner = candidate
				break
			}
		}
		change, err := cloneOwned(ctx.run.codecs, *recorded.Change)
		if err != nil {
			return err
		}
		stored, err := cloneOwned(ctx.run.codecs, change)
		if err != nil {
			return err
		}
		event := &StateChangeEvent{BaseEvent: BaseEvent{EventName: recorded.EventName,
			EventNumber: recorded.EventNumber, AttemptId: recorded.AttemptId,
			Timestamp: recorded.Ts, ContextId: recorded.ContextId,
			ParentContextId: recorded.ParentContextId, Iteration: recorded.Iteration},
			Change: change}
		owner.mu.Lock()
		owner.events = append(owner.events, &StateChangeEvent{BaseEvent: event.BaseEvent,
			Change: stored})
		publisher := owner.eventPublisher
		owner.mu.Unlock()
		if publisher != nil {
			publisher.Dispatch(owner, event)
		}
	}
	return nil
}

// emitCommittedChunks delivers only durable text. Observer notification is independent of the
// unbounded subscription queues; a slow observer must not hold the last answer until after release.
func (ctx *ExecutionContext) emitCommittedChunks(events []*RecordedEvent) error {
	for _, recorded := range events {
		if recorded.Type == "model_stream_chunk" || recorded.Type == "execution_content" {
			value, err := ctx.DecodeEvent(recorded)
			if err != nil {
				return err
			}
			chunk := value.(StreamChunk)
			chunk.RunId, chunk.EventNumber = recorded.RunId, recorded.EventNumber
			for _, owner := range ctx.run.root.allContexts() {
				if owner.ContextId() == recorded.ContextId {
					owner.emitAcceptedChunk(chunk)
					break
				}
			}
		}
	}
	return nil
}

// prepareCheckpoint runs with run.mu held; no application I/O is performed here.
func (ctx *ExecutionContext) prepareCheckpoint(
	phase ExecutionPhase, contexts []*ExecutionContext, runtimeState RuntimeState,
) (*Checkpoint, error) {
	checkpoint := &Checkpoint{SchemaVersion: RunSchemaVersion, RunId: ctx.run.id,
		Revision: ctx.run.working.Revision, Phase: phase,
		Compatibility: cloneWire(ctx.run.compatibility)}
	for i, child := range contexts {
		child.mu.RLock()
		state := ContextCheckpoint{
			ContextId:          child.contextId,
			ParentContextId:    child.parentContextId,
			Name:               child.name,
			Task:               cloneWire(child.savedTask),
			Active:             append([]EntryId(nil), child.active...),
			Iteration:          child.iteration,
			CompletedIteration: child.completedIteration,
			AttemptId:          ctx.run.attempt,
			Runtime:            runtimeState.Contexts[i],
			Limits:             cloneLimits(child.limits),
			StartedAt:          child.startTime,
			State:              cloneWire(child.state),
			Completed:          !child.endTime.IsZero(),
		}
		child.mu.RUnlock()
		if deadline, ok := child.Context().Deadline(); ok {
			state.Deadline = &deadline
		}
		if child == ctx && (phase == PhaseIterationCompleted || phase == PhaseTerminal) {
			state.CompletedIteration = state.Iteration
		}
		checkpoint.Contexts = append(checkpoint.Contexts, state)
	}
	checkpoint.Contexts[0].Extensions = cloneWire(ctx.run.extensions)
	checkpoint.Contexts = append(checkpoint.Contexts, cloneWire(ctx.run.archived)...)
	if phase == PhaseTerminal {
		ctx.mu.RLock()
		result, err := ctx.run.codecs.Encode(ctx.finalResult)
		if err != nil {
			ctx.mu.RUnlock()
			return nil, err
		}
		checkpoint.Outcome = &SavedOutcome{Reason: ctx.terminationReason, Result: result,
			Error: SaveError(ctx.err), CompletedAt: ctx.endTime}
		ctx.mu.RUnlock()
	}
	return checkpoint, nil
}

func (ctx *ExecutionContext) allContexts() []*ExecutionContext {
	result := []*ExecutionContext{ctx}
	for _, child := range ctx.Children() {
		result = append(result, child.allContexts()...)
	}
	return result
}

func (ctx *ExecutionContext) runtimeSnapshot() ContextRuntime {
	ctx.mu.RLock()
	result := ContextRuntime{ContextId: ctx.contextId, Iteration: ctx.iteration,
		NextModelCall: ctx.nextModelCallSeq, NextToolCall: ctx.nextToolCallSeq}
	if ctx.exceededLimit != nil {
		limit := *ctx.exceededLimit
		result.ExceededLimit = &limit
	}
	ctx.mu.RUnlock()
	result.Counters = ctx.stats.Counters()
	result.Gauges = ctx.stats.Gauges()
	return result
}

func (ctx *ExecutionContext) IsWorkerInterrupted() bool {
	ctx.mu.RLock()
	interrupted := ctx.goCtx.Err() != nil && !errors.Is(
		ctx.goCtx.Err(),
		context.DeadlineExceeded,
	) &&
		!ctx.runCanceled && ctx.exceededLimit == nil &&
		!errors.Is(context.Cause(ctx.goCtx), errExecutionReleased)
	ctx.mu.RUnlock()
	return interrupted && ctx.RecordingError() == nil
}

// RestoreExecutionContext validates the portable bundle before rebinding runtime objects.
// A terminal run needs codecs for its saved types but no executable bindings. Active children
// have no resumable Go stack.
func RestoreExecutionContext(
	worker context.Context, bundle RunView, bindings RestoreBindings,
) (restored *ExecutionContext, err error) {
	if bundle.Checkpoint == nil {
		return nil, errors.New("gent: run has no resumable checkpoint")
	}
	checkpoint := bundle.Checkpoint
	if len(checkpoint.Contexts) == 0 {
		return nil, errors.New("gent: checkpoint has no root context")
	}
	if bundle.SchemaVersion != RunSchemaVersion || checkpoint.SchemaVersion != RunSchemaVersion ||
		bundle.RunId == "" || checkpoint.RunId != bundle.RunId {
		return nil, ErrIncompatibleCheckpoint
	}
	registry := bindings.Codecs
	if registry == nil {
		registry = NewCodecRegistry()
	}
	state := checkpoint.Contexts[0]
	data := bindings.Data
	if data == nil {
		var task *Task
		if checkpoint.Phase != PhaseTerminal {
			var err error
			task, err = decodeAs[*Task](registry, state.Task)
			if err != nil {
				return nil, err
			}
		}
		data = NewBasicLoopData(task)
	}
	// Use one worker context throughout configuration and continuation. Replacing its cancel
	// function later would detach configuration work and leak the original cancellation scope.
	var deadlineCancel context.CancelFunc
	if state.Deadline != nil {
		worker, deadlineCancel = context.WithDeadline(worker, *state.Deadline)
	}
	ctx := NewExecutionContext(worker, state.Name, data, WithCodecs(registry),
		WithRunId(bundle.RunId), WithRecorder(bindings.Recorder))
	ctx.deadlineCancel = deadlineCancel
	defer func() {
		if restored == nil {
			ctx.Release()
		}
	}()
	// RestoreBindings is caller-owned; binding components must not mutate its map on retries.
	components := make(map[string]CheckpointState, len(bindings.Components))
	for name, component := range bindings.Components {
		components[name] = component
	}
	bindings.Components = components
	ctx.run.observers = append([]RecordedEventObserver(nil), bindings.Observers...)
	if bindings.CompactionStrategy != nil || bindings.CompactionTrigger != nil {
		if bindings.CompactionStrategy == nil || bindings.CompactionTrigger == nil {
			return nil, errors.New("gent: restore requires both compaction strategy and trigger")
		}
		ctx.SetCompaction(bindings.CompactionTrigger, bindings.CompactionStrategy)
	}
	ctx.savedTask = cloneWire(state.Task)
	if checkpoint.Phase != PhaseTerminal && bindings.Configure != nil {
		if err := bindings.Configure(ctx); err != nil {
			return nil, err
		}
		bindings.Compatibility = ctx.Compatibility()
		if bindings.Components == nil {
			bindings.Components = map[string]CheckpointState{}
		}
		for name, component := range ctx.run.components {
			bindings.Components[name] = component
		}
	}
	if err := ValidateBundle(bundle, registry); err != nil {
		return nil, err
	}
	owned, err := cloneOwned(registry, bundle)
	if err != nil {
		return nil, err
	}
	bundle = owned
	checkpoint = bundle.Checkpoint
	state = checkpoint.Contexts[0]
	if checkpoint.Contexts[0].ParentContextId != "" {
		return nil, errors.New("gent: first checkpoint context is not the root")
	}
	if checkpoint.Phase != PhaseTerminal {
		if !reflect.DeepEqual(checkpoint.Compatibility, bindings.Compatibility) {
			return nil, ErrIncompatibleCheckpoint
		}
		for _, state := range checkpoint.Contexts[1:] {
			if !state.Completed {
				return nil, fmt.Errorf(
					"%w: active child %s",
					ErrUnsupportedRecovery,
					state.ContextId,
				)
			}
		}
		known := map[string]bool{}
		for _, state := range checkpoint.Contexts {
			known[state.ContextId] = true
		}
		for _, state := range bundle.Runtime.Contexts {
			if !known[state.ContextId] {
				return nil, fmt.Errorf("%w: child newer than checkpoint", ErrUnsupportedRecovery)
			}
		}
		if checkpoint.Compatibility.Components["gent:recovery"] == "unsupported" {
			return nil, fmt.Errorf(
				"%w: explicit recovery bindings required",
				ErrUnsupportedRecovery,
			)
		}
	}
	if checkpoint.Phase != PhaseTerminal {
		for name, value := range state.State {
			decoded, err := registry.Decode(value)
			if err != nil {
				return nil, fmt.Errorf("gent: checkpoint state %q: %w", name, err)
			}
			_ = decoded
		}
		if err := restoreExtensions(state, bindings); err != nil {
			return nil, err
		}
	}
	ctx.data = data
	ctx.contextId = state.ContextId
	ctx.savedTask = cloneWire(state.Task)
	ctx.startTime = state.StartedAt
	ctx.limits = cloneLimits(state.Limits)
	ctx.iteration = state.Iteration
	ctx.completedIteration = state.CompletedIteration
	ctx.active = append([]EntryId(nil), state.Active...)
	ctx.state = cloneWire(state.State)
	ctx.phase = checkpoint.Phase
	ctx.restored = true
	ctx.run = &executionRun{view: bundle, recorder: bindings.Recorder,
		observers: append([]RecordedEventObserver(nil), bindings.Observers...),
		codecs:    registry, id: bundle.RunId,
		attempt: randomIdentity(), allocation: bundle.Runtime.Allocation, root: ctx,
		working: bundle.History, compatibility: cloneWire(checkpoint.Compatibility),
		toolRecovery: bindings.ToolRecovery, extensions: cloneWire(state.Extensions),
		archived: cloneWire(checkpoint.Contexts[1:])}
	for _, runtime := range bundle.Runtime.Contexts {
		if runtime.ContextId != ctx.contextId {
			ctx.run.archivedRuntime = append(ctx.run.archivedRuntime, cloneWire(runtime))
		}
	}
	for name, component := range bindings.Components {
		if err := ctx.BindCheckpointState(name, component); err != nil {
			return nil, err
		}
	}
	if component, ok := data.(CheckpointState); ok {
		if err := ctx.BindCheckpointState("data", component); err != nil {
			return nil, err
		}
	}
	// Absolute accepted accounting includes completions newer than the control checkpoint.
	// Importing it does not call event publishers, increment counters, or reset retry gauges.
	runtimeState := state.Runtime
	for _, saved := range bundle.Runtime.Contexts {
		if saved.ContextId == ctx.contextId {
			runtimeState = saved
		}
	}
	ctx.stats.counters = cloneWire(runtimeState.Counters)
	ctx.stats.gauges = cloneWire(runtimeState.Gauges)
	if ctx.stats.counters == nil {
		ctx.stats.counters = map[string]int64{}
	}
	if ctx.stats.gauges == nil {
		ctx.stats.gauges = map[string]float64{}
	}
	ctx.stats.gauges[string(SGScratchpadLength)] = float64(len(ctx.active))
	ctx.nextModelCallSeq = runtimeState.NextModelCall
	ctx.nextToolCallSeq = runtimeState.NextToolCall
	ctx.modelReplay = modelReplay{next: state.Runtime.NextModelCall,
		end: runtimeState.NextModelCall, eventNumber: bundle.LastAppliedEventNumber,
		iteration: state.Iteration}
	ctx.exceededLimit = cloneWire(runtimeState.ExceededLimit)
	if checkpoint.Outcome != nil {
		result, err := decodeAs[[]ContentPart](registry, checkpoint.Outcome.Result)
		if err != nil {
			return nil, err
		}
		var savedErr error
		if checkpoint.Outcome.Error != nil {
			savedErr = cloneWire(checkpoint.Outcome.Error)
		}
		ctx.SetTermination(checkpoint.Outcome.Reason, result, savedErr)
		ctx.endTime = checkpoint.Outcome.CompletedAt
		ctx.Release()
		return ctx, nil
	}
	if ctx.exceededLimit != nil {
		ctx.cancel(fmt.Errorf("gent: restored exceeded limit %s", ctx.exceededLimit.Key))
	}
	// Progress after the checkpoint may have accepted a new selection. Record the explicit
	// recovery selection instead of pretending that those later entries disappeared.
	latest, err := bundle.History.ContextAt(ctx.contextId, bundle.History.Revision)
	if err != nil {
		return nil, err
	}
	if !equalIds(latest, ctx.active) {
		if err := ctx.UpdateHistory(
			HistoryUpdate{ExpectedDraftRevision: ctx.ContextDraft().Revision,
				Active: ctx.active, Reason: ContextReplace},
		); err != nil {
			return nil, err
		}
	}
	ctx.run.pending = append(ctx.run.pending, &RecordedEvent{Type: "attempt_started",
		ContextId: ctx.contextId, Iteration: ctx.iteration, AttemptId: ctx.run.attempt})
	return ctx, nil
}
