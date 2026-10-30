package gent

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"
)

type ContextOption func(*contextOptions)

type contextOptions struct {
	runId         string
	recorder      Recorder
	observers     []RecordedEventObserver
	codecs        *CodecRegistry
	compatibility CompatibilityManifest
}

func WithRunId(runId string) ContextOption {
	return func(options *contextOptions) { options.runId = runId }
}

func WithRecorder(recorder Recorder) ContextOption {
	return func(options *contextOptions) { options.recorder = recorder }
}

func WithObserver(observer RecordedEventObserver) ContextOption {
	return func(options *contextOptions) { options.observers = append(options.observers, observer) }
}

func WithCodecs(codecs *CodecRegistry) ContextOption {
	return func(options *contextOptions) { options.codecs = codecs }
}

func WithCompatibility(manifest CompatibilityManifest) ContextOption {
	return func(options *contextOptions) { options.compatibility = manifest }
}

type executionRun struct {
	mu              sync.RWMutex
	flushMu         sync.Mutex
	notifyMu        sync.Mutex
	notifications   []committedNotification
	view            RunView
	recorder        Recorder
	observers       []RecordedEventObserver
	failed          *PersistenceError
	codecs          *CodecRegistry
	id              string
	attempt         string
	allocation      uint64
	working         HistorySnapshot
	pending         []*RecordedEvent
	err             error
	compatibility   CompatibilityManifest
	root            *ExecutionContext
	components      map[string]CheckpointState
	extensions      map[string]SavedCheckpointState
	archived        []ContextCheckpoint
	archivedRuntime []ContextRuntime
}

func randomIdentity() string {
	// crypto/rand.Read cannot fail on supported Go platforms; Go terminates the process on
	// unrecoverable system entropy failure. IDs must not repeat across worker processes.
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(fmt.Errorf("gent: allocate run identity: %w", err))
	}
	return fmt.Sprintf("%x", value[:])
}

func (ctx *ExecutionContext) initializeHistory(options []ContextOption) {
	cfg := contextOptions{codecs: NewCodecRegistry()}
	for _, option := range options {
		option(&cfg)
	}
	if cfg.codecs == nil {
		cfg.codecs = NewCodecRegistry()
	}
	runId := cfg.runId
	if runId == "" {
		runId = "run:" + randomIdentity()
	}
	var initErr error
	ctx.run = &executionRun{recorder: cfg.recorder, observers: cfg.observers,
		view:   RunView{SchemaVersion: RunSchemaVersion, RunId: runId, Batches: map[string]string{}},
		codecs: cfg.codecs, id: runId,
		attempt: randomIdentity(), compatibility: cloneWire(
			cfg.compatibility,
		), root: ctx, err: initErr}
	ctx.contextId = runId + ":root"
	ctx.state = map[string]EncodedValue{}
	ctx.phase = PhaseInitial
	var task *Task
	if ctx.data != nil {
		task = ctx.data.GetTask()
	}
	ctx.savedTask, initErr = cfg.codecs.Encode(task)
	if initErr != nil {
		ctx.run.err = fmt.Errorf("gent: original task: %w", initErr)
	}
}

func (ctx *ExecutionContext) RunId() string { return ctx.run.id }

func (ctx *ExecutionContext) AttemptId() string {
	ctx.run.mu.RLock()
	defer ctx.run.mu.RUnlock()
	return ctx.run.attempt
}

func (ctx *ExecutionContext) Codecs() *CodecRegistry { return ctx.run.codecs }

func (ctx *ExecutionContext) RecordingError() error {
	ctx.run.mu.RLock()
	defer ctx.run.mu.RUnlock()
	return ctx.run.err
}

func (ctx *ExecutionContext) stopRecording(err error) error {
	if err == nil {
		return nil
	}
	ctx.run.mu.Lock()
	if ctx.run.err == nil {
		ctx.run.err = err
	}
	ctx.run.mu.Unlock()
	ctx.run.root.cancel(err)
	return err
}

func (ctx *ExecutionContext) Task() (*Task, error) {
	return decodeAs[*Task](ctx.run.codecs, ctx.savedTask)
}

func (ctx *ExecutionContext) History() HistorySnapshot {
	// Accepted history is immutable. Copy only the requested history, not the full stream,
	// checkpoint, and batch index that Export supplies to a persistence caller.
	ctx.run.mu.RLock()
	history := ctx.run.view.History
	ctx.run.mu.RUnlock()
	owned, err := cloneOwned(ctx.run.codecs, history)
	if err != nil {
		ctx.stopRecording(err)
		return HistorySnapshot{}
	}
	return owned
}

func (ctx *ExecutionContext) ContextDraft() ContextDraft {
	ctx.run.mu.RLock()
	defer ctx.run.mu.RUnlock()
	return ContextDraft{Revision: ctx.draftRevision, Active: append([]EntryId(nil), ctx.active...)}
}

func (ctx *ExecutionContext) Scratchpad() ([]*HistoryEntry, error) {
	input, err := ctx.CompactionInput()
	return input.Entries, err
}

// CompactionInput captures selection and revision under one lock. A strategy cannot accidentally
// pair an old list of IDs with records from a concurrent context change.
func (ctx *ExecutionContext) CompactionInput() (CompactionInput, error) {
	ctx.run.mu.RLock()
	entries := make([]*HistoryEntry, len(ctx.active))
	for i, id := range ctx.active {
		var ok bool
		entries[i], ok = ctx.run.working.Entry(id)
		if !ok {
			ctx.run.mu.RUnlock()
			return CompactionInput{}, fmt.Errorf("gent: missing active history entry %s", id)
		}
	}
	revision := ctx.draftRevision
	required, err := ctx.requiredHistoryLocked()
	ctx.run.mu.RUnlock()
	if err != nil {
		return CompactionInput{}, err
	}
	owned, err := cloneOwned(ctx.run.codecs, entries)
	return CompactionInput{Revision: revision, Entries: owned, Required: required}, err
}

// NewHistoryEntry freezes content now. Later caller mutations cannot change the proposed entry.
func (ctx *ExecutionContext) NewHistoryEntry(input *HistoryEntry) (*HistoryEntry, error) {
	if err := ctx.RecordingError(); err != nil {
		return nil, err
	}
	if input == nil {
		return nil, errors.New("gent: nil history entry")
	}
	entry, err := cloneOwned(ctx.run.codecs, input)
	if err != nil {
		return nil, err
	}
	if entry.Kind == "" {
		entry.Kind = HistoryMessages
	}
	iteration := ctx.Iteration()
	ctx.run.mu.Lock()
	defer ctx.run.mu.Unlock()
	ctx.run.allocation++
	entry.Id = EntryId(fmt.Sprintf("%s:entry:%s:%d", ctx.run.id, ctx.run.attempt, ctx.run.allocation))
	entry.RunId, entry.ContextId, entry.AttemptId = ctx.run.id, ctx.contextId, ctx.run.attempt
	entry.Iteration, entry.Order, entry.CreatedAt = iteration, 0, time.Now()
	return entry, nil
}

func (ctx *ExecutionContext) UpdateHistory(update HistoryUpdate) error {
	// Freeze before accepting entries. Caller mutations must never rewrite historical evidence.
	owned, err := cloneOwned(ctx.run.codecs, update)
	if err != nil {
		return err
	}
	update = owned
	for _, entry := range update.AppendedEntries {
		if entry == nil {
			return errors.New("gent: nil history entry")
		}
	}
	for _, state := range update.CustomState {
		if state.ContextId != ctx.ContextId() || state.Name == "" {
			return errors.New("gent: invalid custom state identity")
		}
		if _, err := ctx.Codecs().Decode(state.Value); err != nil {
			return err
		}
	}
	iteration := ctx.Iteration()
	ctx.run.mu.Lock()
	if ctx.run.err != nil {
		err := ctx.run.err
		ctx.run.mu.Unlock()
		return err
	}
	if update.ExpectedDraftRevision != ctx.draftRevision {
		ctx.run.mu.Unlock()
		return ErrHistoryConflict
	}
	required, err := ctx.requiredHistoryLocked()
	if err != nil {
		ctx.run.mu.Unlock()
		return err
	}
	update.Active = keepRequiredRecords(update.Active, required)
	if update.Compaction != nil {
		update.Compaction.Active = append([]EntryId(nil), update.Active...)
	}
	for _, entry := range update.AppendedEntries {
		if entry.Compaction != nil {
			entry.Compaction.Active = append([]EntryId(nil), update.Active...)
		}
	}
	change := HistoryChange{PreviousRevision: ctx.run.working.Revision,
		Revision: ctx.run.working.Revision + 1, AppendedEntries: update.AppendedEntries,
		Selections:  []ContextSelection{{ctx.contextId, append([]EntryId(nil), update.Active...)}},
		CustomState: cloneWire(update.CustomState), Reason: update.Reason}
	for i := range change.AppendedEntries {
		entry := change.AppendedEntries[i]
		if entry.ContextId != ctx.contextId || entry.RunId != ctx.run.id {
			ctx.run.mu.Unlock()
			return ErrWrongRun
		}
		entry.Order = uint64(len(ctx.run.working.Entries) + i + 1)
	}
	if err := applyHistory(&ctx.run.working, change, ctx.run.id); err != nil {
		ctx.run.mu.Unlock()
		return err
	}
	ctx.active = append([]EntryId(nil), update.Active...)
	for _, state := range change.CustomState {
		ctx.state[state.Name] = cloneWire(state.Value)
	}
	ctx.draftRevision++
	ctx.run.pending = append(ctx.run.pending, &RecordedEvent{Type: "state_change",
		ContextId: ctx.contextId, Iteration: iteration, AttemptId: ctx.run.attempt,
		EventName: EventNameStateChange, Change: &change})
	ctx.run.mu.Unlock()
	ctx.Stats().SetGauge(SGScratchpadLength, float64(len(update.Active)))
	return ctx.RecordingError()
}

// AppendRecord is the ordinary append-and-select operation. Final answers can set active=false
// to retain evidence without changing the context used by a subsequent request.
func (ctx *ExecutionContext) AppendRecord(record *HistoryEntry, active bool) error {
	entry, err := ctx.NewHistoryEntry(record)
	if err != nil {
		return err
	}
	draft := ctx.ContextDraft()
	if active {
		draft.Active = append(draft.Active, entry.Id)
	}
	return ctx.UpdateHistory(HistoryUpdate{ExpectedDraftRevision: draft.Revision,
		AppendedEntries: []*HistoryEntry{entry}, Active: draft.Active, Reason: ContextAppend})
}

func (ctx *ExecutionContext) ExpireRecords(nextIteration int) error {
	// Expiry needs only immutable entry metadata. Copying all selected media and tool payloads
	// here adds work on every iteration. Capture IDs and revision together, then let UpdateHistory
	// reject a stale selection if a concurrent append wins after the read lock is released.
	ctx.run.mu.RLock()
	revision := ctx.draftRevision
	active := make([]EntryId, 0, len(ctx.active))
	for _, id := range ctx.active {
		record, ok := ctx.run.working.Entry(id)
		if !ok {
			ctx.run.mu.RUnlock()
			return fmt.Errorf("gent: missing active history entry %s", id)
		}
		if record.ExpireAfterIteration > 0 && nextIteration >= record.ExpireAfterIteration {
			continue
		}
		active = append(active, record.Id)
	}
	unchanged := len(active) == len(ctx.active)
	ctx.run.mu.RUnlock()
	if unchanged {
		return nil
	}
	return ctx.UpdateHistory(HistoryUpdate{ExpectedDraftRevision: revision,
		Active: active, Reason: ContextExpire})
}

func (ctx *ExecutionContext) SetCheckpointState(name string, value any) error {
	encoded, err := ctx.run.codecs.Encode(value)
	if err != nil {
		return err
	}
	ctx.run.mu.Lock()
	defer ctx.run.mu.Unlock()
	ctx.state[name] = encoded
	return nil
}

func (ctx *ExecutionContext) CheckpointState(name string) (any, bool, error) {
	ctx.run.mu.RLock()
	value, ok := ctx.state[name]
	value = cloneWire(value)
	ctx.run.mu.RUnlock()
	if !ok {
		return nil, false, nil
	}
	decoded, err := ctx.run.codecs.Decode(value)
	return decoded, true, err
}

func (ctx *ExecutionContext) Phase() ExecutionPhase {
	ctx.run.mu.RLock()
	defer ctx.run.mu.RUnlock()
	return ctx.phase
}

func (ctx *ExecutionContext) SetCompatibility(manifest CompatibilityManifest) error {
	ctx.run.mu.Lock()
	defer ctx.run.mu.Unlock()
	if ctx.restored && !reflect.DeepEqual(ctx.run.compatibility, manifest) {
		return ErrIncompatibleCheckpoint
	}
	ctx.run.compatibility = cloneWire(manifest)
	return nil
}

func (ctx *ExecutionContext) ClearCheckpointState(names ...string) {
	ctx.run.mu.Lock()
	defer ctx.run.mu.Unlock()
	for _, name := range names {
		delete(ctx.state, name)
	}
}

func (ctx *ExecutionContext) Compatibility() CompatibilityManifest {
	ctx.run.mu.RLock()
	defer ctx.run.mu.RUnlock()
	return cloneWire(ctx.run.compatibility)
}

// CancelRun expresses an application decision to stop the generation. Canceling the worker's
// context alone remains an interruption that a replacement worker may continue.
func (ctx *ExecutionContext) CancelRun(cause error) {
	if cause == nil {
		cause = context.Canceled
	}
	ctx.mu.Lock()
	ctx.runCanceled = true
	ctx.mu.Unlock()
	ctx.cancel(cause)
}
