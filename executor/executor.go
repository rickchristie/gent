package executor

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/events"
)

// Config holds configuration options for the Runner.
type Config struct {
	// Events is the event registry for subscribers.
	// If nil, a new registry is created automatically.
	Events *events.Registry
	// RecoveryVersion identifies custom loop, hook, or compaction behavior. Stateful extensions
	// also implement gent.CheckpointState; a version cannot restore hidden mutable state.
	RecoveryVersion string
}

// DefaultConfig returns a config with sensible defaults.
func DefaultConfig() Config {
	return Config{}
}

// Runner runs a Loop and manages the lifecycle,
// event publishing, and trace collection via ExecutionContext.
//
// The Runner is responsible for:
//   - Running the Loop repeatedly until it returns [gent.LATerminate]
//   - Publishing lifecycle events at appropriate points
//   - Handling context cancellation and limit exceeded signals
//
// Limits are configured on the ExecutionContext, not the Runner. This allows
// limits to be shared across nested agent loops and enforced in real-time as
// stats are updated.
type Runner[T any] struct {
	loop   gent.Loop[T]
	config Config
	events *events.Registry
}

// NewRunner creates a runner for any Loop. The context supplies its task and custom data.
func NewRunner[T any](loop gent.Loop[T], config Config) *Runner[T] {
	registry := config.Events
	if registry == nil {
		registry = events.NewRegistry()
	}
	return &Runner[T]{
		loop:   loop,
		config: config,
		events: registry,
	}
}

// WithEvents replaces the executor's event registry with the provided one.
// Use this when you need to share a registry across multiple executors.
// Returns the executor for chaining.
//
// Example:
//
//	// Share events across multiple executors
//	sharedRegistry := events.NewRegistry()
//	sharedRegistry.Subscribe(&MetricsSubscriber{})
//
//	exec1 := executor.NewRunner(loop1, config).WithEvents(sharedRegistry)
//	exec2 := executor.NewRunner(loop2, config).WithEvents(sharedRegistry)
func (e *Runner[T]) WithEvents(registry *events.Registry) *Runner[T] {
	e.events = registry
	return e
}

// Subscribe adds a subscriber to the executor's existing event registry.
// The subscriber can implement any combination of subscriber interfaces
// (BeforeExecutionSubscriber, AfterToolCallSubscriber, etc.).
// Returns the executor for chaining.
//
// This is the simpler option when you don't need to share subscribers across executors.
// For sharing, use WithEvents instead.
//
// Example:
//
//	exec := executor.NewRunner(loop, config).
//	    Subscribe(&LoggerSubscriber{}).
//	    Subscribe(&MetricsSubscriber{})
func (e *Runner[T]) Subscribe(subscriber any) *Runner[T] {
	e.events.Subscribe(subscriber)
	return e
}

// Execute runs the Loop until termination.
//
// The execution flow:
//  1. Publish BeforeExecutionEvent
//  2. Repeatedly call Loop.Next until:
//     - It returns LATerminate
//     - A limit is exceeded (context cancelled)
//     - Context is canceled
//     - An error occurs
//  3. Publish AfterExecutionEvent
//
// The result is stored in execCtx.Result() after execution completes. Handle the returned
// error even when result is nil: a rejected save may prevent recording a terminal outcome.
//
// Example:
//
//	execCtx := gent.NewExecutionContext(ctx, "main", data)
//	execCtx.SetLimits(customLimits) // optional
//	value, result, err := exec.Execute(execCtx)
//	if err != nil {
//	    // handle error
//	}
func (e *Runner[T]) Execute(execCtx *gent.ExecutionContext) (T, *gent.ExecutionResult, error) {
	return e.executeTyped(execCtx, false)
}

// Continue runs an already restored context. Prepare its loop and bindings through
// RestoreExecutionContext before calling; Continue validates configuration without reinitializing
// restored state. Terminal runs return the saved outcome. Resume handles this setup automatically.
func (e *Runner[T]) Continue(execCtx *gent.ExecutionContext) (T, *gent.ExecutionResult, error) {
	return e.executeTyped(execCtx, true)
}

func Run[T any](ctx *gent.ExecutionContext, loop gent.Loop[T], config Config) (
	T, *gent.ExecutionResult, error,
) {
	return NewRunner(loop, config).Execute(ctx)
}

func (e *Runner[T]) executeTyped(ctx *gent.ExecutionContext, continuing bool) (
	T, *gent.ExecutionResult, error,
) {
	var zero T
	result, err := e.execute(ctx, continuing)
	if err != nil || result == nil || result.TerminationReason != gent.TerminationSuccess {
		return zero, result, err
	}
	value, err := gent.OutputValue[T](ctx)
	if err != nil {
		return zero, nil, err
	}
	return value, result, nil
}

func (e *Runner[T]) execute(
	execCtx *gent.ExecutionContext, continuing bool,
) (*gent.ExecutionResult, error) {
	defer execCtx.Release()
	if execCtx.Phase() == gent.PhaseTerminal {
		if err := gent.PrepareOutput(execCtx, e.loop.Output()); err != nil {
			return nil, err
		}
		if err := e.configureRecovery(execCtx); err != nil {
			return nil, err
		}
		return execCtx.Result(), execCtx.Error()
	}
	if err := execCtx.RecordingError(); err != nil {
		return nil, err
	}
	if e.events != nil {
		execCtx.SetEventPublisher(e.events)
	}
	if continuing {
		if err := gent.PrepareOutput(execCtx, e.loop.Output()); err != nil {
			return nil, err
		}
		if err := e.configureRecovery(execCtx); err != nil {
			return nil, err
		}
	} else if err := e.configureExecution(execCtx); err != nil {
		return nil, err
	}
	if !continuing {
		execCtx.PublishBeforeExecution()
		if err := execCtx.CommitCheckpoint(gent.PhaseInitial); err != nil {
			return nil, err
		}
	} else if err := execCtx.FlushProgress(); err != nil {
		return nil, err
	}

	for {
		if err := execCtx.RecordingError(); err != nil {
			return nil, err
		}
		if terminateIfCanceled(execCtx) {
			return e.finish(execCtx)
		}
		phase := execCtx.Phase()
		resumeIteration := phase == gent.PhaseIterationReady || phase == gent.PhaseDecisionReady ||
			phase == gent.PhaseToolReady || phase == gent.PhaseToolCompleted
		if !resumeIteration {
			if err := execCtx.ExpireRecords(execCtx.Iteration() + 1); err != nil {
				return nil, err
			}
			if execCtx.Iteration() > 0 && phase != gent.PhaseCompactionCompleted {
				if err := e.compactIfNeeded(execCtx); err != nil {
					if recordErr := execCtx.RecordingError(); recordErr != nil {
						return nil, recordErr
					}
					execCtx.SetTermination(gent.TerminationCompactionFailed, nil,
						fmt.Errorf("compaction (iteration %d): %w", execCtx.Iteration(), err))
					return e.finish(execCtx)
				}
			}
			if terminateIfCanceled(execCtx) {
				return e.finish(execCtx)
			}
			execCtx.IncrementIteration()
			execCtx.PublishBeforeIteration()
			if err := execCtx.CommitCheckpoint(gent.PhaseIterationReady); err != nil {
				return nil, err
			}
		}
		if terminateIfCanceled(execCtx) {
			return e.finish(execCtx)
		}
		started := time.Now()
		result, loopErr := e.loop.Next(execCtx)
		if err := execCtx.RecordingError(); err != nil {
			return nil, err
		}
		if execCtx.IsWorkerInterrupted() {
			execCtx.SetTermination(gent.TerminationContextCanceled, nil, execCtx.Context().Err())
			return e.finish(execCtx)
		}
		if errors.Is(loopErr, gent.ErrUncertainTool) ||
			errors.Is(loopErr, gent.ErrUnsupportedRecovery) ||
			errors.Is(loopErr, gent.ErrIncompatibleCheckpoint) {
			if err := execCtx.RecordInterruption(loopErr); err != nil {
				return nil, err
			}
			return nil, loopErr
		}
		if result == nil && loopErr == nil {
			loopErr = errors.New("Loop.Next returned a nil result")
		}
		execCtx.PublishAfterIteration(result, time.Since(started), loopErr)
		execCtx.EnforceIterationLimit()
		if terminateIfCanceled(execCtx) {
			return e.finish(execCtx)
		}
		if loopErr != nil {
			execCtx.SetTermination(gent.TerminationError, nil,
				fmt.Errorf("Loop.Next (iteration %d): %w", execCtx.Iteration(), loopErr))
			return e.finish(execCtx)
		}
		if result.Action == gent.LATerminate {
			switch result.TerminationReason {
			case gent.TerminationSuccess:
				if result.Refusal != nil {
					return nil, errors.New("gent: successful loop result has refusal")
				}
				if result.Value == nil {
					return nil, errors.New("gent: successful loop result has no typed value")
				}
				decoded, err := execCtx.Codecs().Decode(*result.Value)
				if err != nil {
					return nil, err
				}
				nilInterface := decoded == nil && reflect.TypeFor[T]().Kind() == reflect.Interface
				if _, ok := decoded.(T); !ok && !nilInterface {
					return nil, fmt.Errorf("gent: loop output type %T does not match %T", decoded, *new(T))
				}
				execCtx.SetAcceptedValue(result.Value)
				execCtx.SetFinalContentSource(result.ContentSource)
				execCtx.SetTermination(gent.TerminationSuccess, result.Result, nil)
			case gent.TerminationRefused:
				if result.Refusal == nil || len(result.Result) > 0 {
					return nil, errors.New("gent: invalid refused loop result")
				}
				execCtx.SetRefusal(result.Refusal)
			default:
				return nil, fmt.Errorf("gent: invalid terminal loop reason %q", result.TerminationReason)
			}
			return e.finish(execCtx)
		}
		if err := execCtx.CommitCheckpoint(gent.PhaseIterationCompleted); err != nil {
			return nil, err
		}
	}
}

func (e *Runner[T]) finish(execCtx *gent.ExecutionContext) (*gent.ExecutionResult, error) {
	if err := execCtx.RecordingError(); err != nil {
		return nil, err
	}
	if execCtx.IsWorkerInterrupted() {
		if err := execCtx.RecordInterruption(execCtx.Context().Err()); err != nil {
			return nil, err
		}
		return execCtx.Result(), execCtx.Context().Err()
	}
	execCtx.PublishAfterExecution(execCtx.TerminationReason(), execCtx.Error())
	if err := execCtx.CommitCheckpoint(gent.PhaseTerminal); err != nil {
		return nil, err
	}
	return execCtx.Result(), execCtx.Error()
}

func terminateIfCanceled(execCtx *gent.ExecutionContext) bool {
	goCtx := execCtx.Context()
	if goCtx.Err() == nil {
		return false
	}
	if execCtx.ExceededLimit() != nil {
		limit := execCtx.ExceededLimit()
		op := ">"
		if limit.Key == gent.SCIterations || limit.Key == gent.SCIterations.Self() {
			op = ">="
		}
		execCtx.SetTermination(
			gent.TerminationLimitExceeded,
			nil,
			fmt.Errorf("limit exceeded: %s %s %v", limit.Key, op, limit.MaxValue),
		)
		return true
	}
	cause := goCtx.Err()
	if detail := context.Cause(goCtx); detail != nil && detail != cause {
		// Preserve both cancellation classification and the application's reason for stopping.
		cause = errors.Join(cause, detail)
	}
	execCtx.SetTermination(gent.TerminationContextCanceled, nil, cause)
	return true
}

// compactIfNeeded checks the compaction trigger and runs the
// strategy if triggered.
func (e *Runner[T]) compactIfNeeded(
	execCtx *gent.ExecutionContext,
) error {
	trigger := execCtx.CompactionTrigger()
	strategy := execCtx.CompactionStrategy()
	if trigger == nil || strategy == nil {
		return nil
	}

	shouldCompact, err := trigger.ShouldCompact(execCtx)
	if err != nil {
		return err
	}
	if !shouldCompact {
		return nil
	}

	input, err := execCtx.CompactionInput()
	if err != nil {
		return err
	}
	inputIds := input.EntryIds()
	before, err := execCtx.Export()
	if err != nil {
		return err
	}
	started := time.Now()
	proposal, compactErr := strategy.Compact(execCtx, input)
	after, err := execCtx.Export()
	if err != nil {
		return err
	}
	var modelCallIds []string
	seenCalls := map[string]bool{}
	for _, event := range after.Activity {
		if event.EventNumber <= before.LastAppliedEventNumber ||
			event.ContextId != execCtx.ContextId() {
			continue
		}
		if event.ModelCallId != "" && !seenCalls[event.ModelCallId] {
			modelCallIds = append(modelCallIds, event.ModelCallId)
			seenCalls[event.ModelCallId] = true
		}
	}
	if compactErr != nil {
		proposal = gent.HistoryUpdate{ExpectedDraftRevision: input.Revision, Active: inputIds}
	}
	if proposal.ExpectedDraftRevision != input.Revision {
		return gent.ErrHistoryConflict
	}
	decision := proposal.Compaction
	if decision == nil {
		decision = &gent.CompactionRecord{}
	}
	decision.Strategy = fmt.Sprintf("%T", strategy)
	decision.InputIds, decision.Active = inputIds, proposal.Active
	decision.ModelCallIds, decision.Error = modelCallIds, gent.SaveError(compactErr)
	marker, err := execCtx.NewHistoryEntry(&gent.HistoryEntry{
		Kind: gent.HistoryCompaction, Compaction: decision})
	if err != nil {
		return err
	}
	proposal.AppendedEntries = append(proposal.AppendedEntries, marker)
	proposal.Reason = gent.ContextCompact
	if err := execCtx.UpdateHistory(proposal); err != nil {
		return err
	}
	if compactErr != nil {
		return compactErr
	}
	execCtx.PublishCompaction(len(inputIds), len(execCtx.ContextDraft().Active), time.Since(started))
	if err := trigger.NotifyCompacted(execCtx); err != nil {
		return err
	}
	return execCtx.CommitCheckpoint(gent.PhaseCompactionCompleted)
}
