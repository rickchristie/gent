package executor

import (
	"fmt"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/hooks"
)

// Config holds configuration options for the Executor.
type Config struct {
	// Reserved for future configuration options.
	// Limits are now configured on ExecutionContext via SetLimits().
}

// DefaultConfig returns a config with sensible defaults.
func DefaultConfig() Config {
	return Config{}
}

// Executor orchestrates the execution of an AgentLoop, managing the lifecycle,
// hooks, and trace collection via ExecutionContext.
//
// The Executor is responsible for:
//   - Running the AgentLoop repeatedly until it returns [gent.LATerminate]
//   - Invoking lifecycle hooks at appropriate points
//   - Handling context cancellation and limit exceeded signals
//
// Limits are configured on the ExecutionContext, not the Executor. This allows
// limits to be shared across nested agent loops and enforced in real-time as
// stats are updated.
type Executor[Data gent.LoopData] struct {
	loop   gent.AgentLoop[Data]
	config Config
	hooks  *hooks.Registry
}

// New creates a new Executor with the given AgentLoop and configuration.
func New[Data gent.LoopData](loop gent.AgentLoop[Data], config Config) *Executor[Data] {
	return &Executor[Data]{
		loop:   loop,
		config: config,
		hooks:  hooks.NewRegistry(),
	}
}

// WithHooks replaces the executor's hook registry with the provided one.
// Use this when you need to share a registry across multiple executors.
// Returns the executor for chaining.
//
// Example:
//
//	// Share hooks across multiple executors
//	sharedRegistry := hooks.NewRegistry()
//	sharedRegistry.Register(&MetricsHook{})
//
//	exec1 := executor.New(loop1, config).WithHooks(sharedRegistry)
//	exec2 := executor.New(loop2, config).WithHooks(sharedRegistry)
func (e *Executor[Data]) WithHooks(h *hooks.Registry) *Executor[Data] {
	e.hooks = h
	return e
}

// RegisterHook adds a hook to the executor's existing hook registry.
// The hook can implement any combination of hook interfaces
// (BeforeExecutionHook, AfterToolCallHook, etc.).
// Returns the executor for chaining.
//
// This is the simpler option when you don't need to share hooks across executors.
// For sharing hooks, use WithHooks instead.
//
// Example:
//
//	exec := executor.New(loop, config).
//	    RegisterHook(&LoggerHook{}).
//	    RegisterHook(&MetricsHook{})
func (e *Executor[Data]) RegisterHook(hook any) *Executor[Data] {
	e.hooks.Register(hook)
	return e
}

// Execute runs the AgentLoop until termination.
//
// The execution flow:
//  1. Call BeforeExecution hook (if set)
//  2. Repeatedly call AgentLoop.Next until:
//     - It returns LATerminate
//     - A limit is exceeded (context cancelled)
//     - Context is canceled
//     - An error occurs
//  3. Call AfterExecution hook (if set)
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
	// Set hook firer for model call hooks
	if e.hooks != nil {
		execCtx.SetHookFirer(e.hooks)
	}

	// Ensure streams are closed and AfterExecution is always called if BeforeExecution succeeded
	beforeExecutionCalled := false
	defer func() {
		// Always close streams when execution ends
		execCtx.CloseStreams()

		if beforeExecutionCalled && e.hooks != nil {
			// AfterExecution errors are logged but don't change the result
			event := gent.AfterExecutionEvent{
				TerminationReason: execCtx.TerminationReason(),
				Error:             execCtx.Error(),
			}
			ctx := execCtx.Context()
			if hookErr := e.hooks.FireAfterExecution(ctx, execCtx, event); hookErr != nil {
				// The AfterExecution error doesn't override existing errors
				// but should be available for logging
				e.hooks.FireError(ctx, execCtx, gent.ErrorEvent{
					Iteration: execCtx.Iteration(),
					Err:       fmt.Errorf("AfterExecution hook: %w", hookErr),
				})
			}
		}
	}()

	// BeforeExecution hook
	ctx := execCtx.Context()
	if e.hooks != nil {
		event := gent.BeforeExecutionEvent{}
		if err := e.hooks.FireBeforeExecution(ctx, execCtx, event); err != nil {
			execErr := fmt.Errorf("BeforeExecution hook: %w", err)
			execCtx.SetTermination(gent.TerminationHookAbort, nil, execErr)
			return
		}
	}
	beforeExecutionCalled = true

	// Main execution loop
	for {
		// Check context cancellation (handles both user cancel and limit exceeded)
		ctx = execCtx.Context()
		if ctx.Err() != nil {
			if execCtx.ExceededLimit() != nil {
				execCtx.SetTermination(
					gent.TerminationLimitExceeded,
					nil,
					fmt.Errorf("limit exceeded: %s > %v",
						execCtx.ExceededLimit().Key,
						execCtx.ExceededLimit().MaxValue),
				)
			} else {
				execCtx.SetTermination(gent.TerminationContextCanceled, nil, ctx.Err())
			}
			return
		}

		// Start iteration (increments counter and records IterationStartTrace)
		execCtx.StartIteration()
		iterStart := time.Now()

		// BeforeIteration hook
		if e.hooks != nil {
			event := gent.BeforeIterationEvent{Iteration: execCtx.Iteration()}
			if err := e.hooks.FireBeforeIteration(ctx, execCtx, event); err != nil {
				execCtx.EndIteration(gent.LATerminate, time.Since(iterStart))
				execErr := fmt.Errorf(
					"BeforeIteration hook (iteration %d): %w",
					execCtx.Iteration(),
					err,
				)
				execCtx.SetTermination(gent.TerminationHookAbort, nil, execErr)
				return
			}
		}

		// Execute the AgentLoop iteration
		loopResult, loopErr := e.loop.Next(execCtx)
		iterDuration := time.Since(iterStart)

		// Handle loop error - check if it was due to limit exceeded
		if loopErr != nil {
			execCtx.EndIteration(gent.LATerminate, iterDuration)
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

		// End iteration (records IterationEndTrace)
		execCtx.EndIteration(loopResult.Action, iterDuration)

		// AfterIteration hook
		if e.hooks != nil {
			event := gent.AfterIterationEvent{
				Iteration: execCtx.Iteration(),
				Result:    loopResult,
				Duration:  iterDuration,
			}
			if err := e.hooks.FireAfterIteration(ctx, execCtx, event); err != nil {
				execErr := fmt.Errorf(
					"AfterIteration hook (iteration %d): %w",
					execCtx.Iteration(),
					err,
				)
				execCtx.SetTermination(gent.TerminationHookAbort, nil, execErr)
				return
			}
		}

		// Check for termination
		if loopResult.Action == gent.LATerminate {
			execCtx.SetTermination(gent.TerminationSuccess, loopResult.Result, nil)
			return
		}

		// Continue - the AgentLoop is responsible for updating data with NextPrompt
		// The exact mechanism depends on the LoopData implementation
	}
}
