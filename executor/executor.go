package executor

import (
	"fmt"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/events"
)

// Config holds configuration options for the Executor.
type Config struct {
	// Events is the event registry for subscribers.
	// If nil, a new registry is created automatically.
	Events *events.Registry
}

// DefaultConfig returns a config with sensible defaults.
func DefaultConfig() Config {
	return Config{}
}

// Executor orchestrates the execution of an AgentLoop, managing the lifecycle,
// event publishing, and trace collection via ExecutionContext.
//
// The Executor is responsible for:
//   - Running the AgentLoop repeatedly until it returns [gent.LATerminate]
//   - Publishing lifecycle events at appropriate points
//   - Handling context cancellation and limit exceeded signals
//
// Limits are configured on the ExecutionContext, not the Executor. This allows
// limits to be shared across nested agent loops and enforced in real-time as
// stats are updated.
type Executor[Data gent.LoopData] struct {
	loop   gent.AgentLoop[Data]
	config Config
	events *events.Registry
}

// New creates a new Executor with the given AgentLoop and configuration.
func New[Data gent.LoopData](loop gent.AgentLoop[Data], config Config) *Executor[Data] {
	registry := config.Events
	if registry == nil {
		registry = events.NewRegistry()
	}
	return &Executor[Data]{
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
//	exec1 := executor.New(loop1, config).WithEvents(sharedRegistry)
//	exec2 := executor.New(loop2, config).WithEvents(sharedRegistry)
func (e *Executor[Data]) WithEvents(registry *events.Registry) *Executor[Data] {
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
//	exec := executor.New(loop, config).
//	    Subscribe(&LoggerSubscriber{}).
//	    Subscribe(&MetricsSubscriber{})
func (e *Executor[Data]) Subscribe(subscriber any) *Executor[Data] {
	e.events.Subscribe(subscriber)
	return e
}

// Execute runs the AgentLoop until termination.
//
// The execution flow:
//  1. Publish BeforeExecutionEvent
//  2. Repeatedly call AgentLoop.Next until:
//     - It returns LATerminate
//     - A limit is exceeded (context cancelled)
//     - Context is canceled
//     - An error occurs
//  3. Publish AfterExecutionEvent
//
// The result is stored in execCtx.Result() after execution completes.
// Check execCtx.Result().Error for any errors that occurred.
//
// Example:
//
//	execCtx := gent.NewExecutionContext(ctx, "main", data)
//	execCtx.SetLimits(customLimits) // optional
//	executor.Execute(execCtx)
//	result := execCtx.Result()
//	if result.Error != nil {
//	    // handle error
//	}
func (e *Executor[Data]) Execute(execCtx *gent.ExecutionContext) {
	// Set event publisher for event dispatching
	if e.events != nil {
		execCtx.SetEventPublisher(e.events)
	}

	// Ensure streams are closed and AfterExecution is always published if BeforeExecution was
	beforeExecutionPublished := false
	defer func() {
		// Always close streams when execution ends
		execCtx.CloseStreams()

		if beforeExecutionPublished {
			execCtx.PublishAfterExecution(execCtx.TerminationReason(), execCtx.Error())
		}
	}()

	// BeforeExecution event
	execCtx.PublishBeforeExecution()
	beforeExecutionPublished = true

	// Main execution loop
	for {
		// Check context cancellation (handles both user cancel and limit exceeded)
		goCtx := execCtx.Context()
		if goCtx.Err() != nil {
			if execCtx.ExceededLimit() != nil {
				execCtx.SetTermination(
					gent.TerminationLimitExceeded,
					nil,
					fmt.Errorf("limit exceeded: %s > %v",
						execCtx.ExceededLimit().Key,
						execCtx.ExceededLimit().MaxValue),
				)
			} else {
				execCtx.SetTermination(
					gent.TerminationContextCanceled,
					nil,
					goCtx.Err(),
				)
			}
			return
		}

		// Start iteration: increment counter and publish BeforeIterationEvent
		// (BeforeIterationEvent updates SCIterations stat)
		execCtx.IncrementIteration()
		iterStart := time.Now()
		execCtx.PublishBeforeIteration()

		// Execute the AgentLoop iteration
		loopResult, loopErr := e.loop.Next(execCtx)
		iterDuration := time.Since(iterStart)

		// Handle loop error - check if it was due to limit exceeded
		if loopErr != nil {
			execCtx.PublishAfterIteration(
				&gent.AgentLoopResult{Action: gent.LATerminate},
				iterDuration,
			)
			if execCtx.ExceededLimit() != nil {
				execCtx.SetTermination(
					gent.TerminationLimitExceeded,
					nil,
					fmt.Errorf("limit exceeded: %s > %v",
						execCtx.ExceededLimit().Key,
						execCtx.ExceededLimit().MaxValue),
				)
			} else {
				execErr := fmt.Errorf(
					"AgentLoop.Next (iteration %d): %w",
					execCtx.Iteration(),
					loopErr,
				)
				execCtx.SetTermination(gent.TerminationError, nil, execErr)
			}
			return
		}

		// Publish AfterIterationEvent
		execCtx.PublishAfterIteration(loopResult, iterDuration)

		// Check for termination
		if loopResult.Action == gent.LATerminate {
			execCtx.SetTermination(gent.TerminationSuccess, loopResult.Result, nil)
			return
		}

		// Continue - the AgentLoop is responsible for updating data with NextPrompt
		// The exact mechanism depends on the LoopData implementation
	}
}
