package executor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/hooks"
)

// ErrMaxIterationsExceeded is returned when the executor exceeds the configured maximum iterations.
var ErrMaxIterationsExceeded = errors.New("gent: maximum iterations exceeded")

// Config holds configuration options for the Executor.
type Config struct {
	// MaxIterations is the maximum number of loop iterations before termination with error.
	// Set to 0 for unlimited iterations.
	MaxIterations int
}

// DefaultConfig returns a config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		MaxIterations: 100,
	}
}

// Executor orchestrates the execution of an AgentLoop, managing the lifecycle,
// hooks, and trace collection via ExecutionContext.
//
// The Executor is responsible for:
//   - Creating and managing the ExecutionContext
//   - Running the AgentLoop repeatedly until it returns [gent.LATerminate]
//   - Invoking lifecycle hooks at appropriate points
//   - Enforcing configuration limits (e.g., max iterations)
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

// WithHooks sets the executor hook registry. Returns the executor for chaining.
func (e *Executor[Data]) WithHooks(h *hooks.Registry) *Executor[Data] {
	e.hooks = h
	return e
}

// RegisterHook adds a hook to the executor's hook registry.
// The hook can implement any combination of hook interfaces.
// Returns the executor for chaining.
func (e *Executor[Data]) RegisterHook(hook any) *Executor[Data] {
	e.hooks.Register(hook)
	return e
}

// Execute runs the AgentLoop until termination.
//
// The execution flow:
//  1. Create ExecutionContext with the provided data
//  2. Call BeforeExecution hook (if set)
//  3. Repeatedly call AgentLoop.Next until:
//     - It returns LATerminate
//     - MaxIterations is exceeded (if configured)
//     - Context is canceled
//     - An error occurs
//  4. Call AfterExecution hook (if set)
//
// The ExecutionContext flows through all components (Model, ToolChain, Hooks),
// enabling automatic tracing without manual wiring.
func (e *Executor[Data]) Execute(ctx context.Context, data Data) *gent.ExecutionResult {
	execCtx := gent.NewExecutionContext("main", data)

	// Set hook firer for model call hooks
	if e.hooks != nil {
		execCtx.SetHookFirer(e.hooks)
	}

	result := &gent.ExecutionResult{
		Context: execCtx,
	}

	// Ensure AfterExecution is always called if BeforeExecution succeeded
	beforeExecutionCalled := false
	defer func() {
		if beforeExecutionCalled && e.hooks != nil {
			// AfterExecution errors are logged but don't change the result
			event := gent.AfterExecutionEvent{
				TerminationReason: execCtx.TerminationReason(),
				Error:             execCtx.Error(),
			}
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
	if e.hooks != nil {
		event := gent.BeforeExecutionEvent{}
		if err := e.hooks.FireBeforeExecution(ctx, execCtx, event); err != nil {
			result.Error = fmt.Errorf("BeforeExecution hook: %w", err)
			execCtx.SetTermination(gent.TerminationHookAbort, nil, result.Error)
			return result
		}
	}
	beforeExecutionCalled = true

	// Main execution loop
	for {
		// Check context cancellation
		if ctx.Err() != nil {
			result.Error = ctx.Err()
			execCtx.SetTermination(gent.TerminationContextCanceled, nil, result.Error)
			return result
		}

		// Check max iterations
		if e.config.MaxIterations > 0 && execCtx.Iteration() >= e.config.MaxIterations {
			result.Error = fmt.Errorf(
				"%w: exceeded %d iterations",
				ErrMaxIterationsExceeded,
				e.config.MaxIterations,
			)
			execCtx.SetTermination(gent.TerminationMaxIterations, nil, result.Error)
			return result
		}

		// Start iteration (increments counter and records IterationStartTrace)
		execCtx.StartIteration()
		iterStart := time.Now()

		// BeforeIteration hook
		if e.hooks != nil {
			event := gent.BeforeIterationEvent{Iteration: execCtx.Iteration()}
			if err := e.hooks.FireBeforeIteration(ctx, execCtx, event); err != nil {
				execCtx.EndIteration(gent.LATerminate, time.Since(iterStart))
				result.Error = fmt.Errorf(
					"BeforeIteration hook (iteration %d): %w",
					execCtx.Iteration(),
					err,
				)
				execCtx.SetTermination(gent.TerminationHookAbort, nil, result.Error)
				return result
			}
		}

		// Execute the AgentLoop iteration
		loopResult := e.loop.Next(ctx, execCtx)
		iterDuration := time.Since(iterStart)

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
				result.Error = fmt.Errorf(
					"AfterIteration hook (iteration %d): %w",
					execCtx.Iteration(),
					err,
				)
				execCtx.SetTermination(gent.TerminationHookAbort, nil, result.Error)
				return result
			}
		}

		// Check for termination
		if loopResult.Action == gent.LATerminate {
			result.Result = loopResult.Result
			execCtx.SetTermination(gent.TerminationSuccess, loopResult.Result, nil)
			return result
		}

		// Continue - the AgentLoop is responsible for updating data with NextPrompt
		// The exact mechanism depends on the LoopData implementation
	}
}

// ExecuteWithContext runs the AgentLoop with an existing ExecutionContext.
// This is useful for nested agent loops where you want to use a child context.
func (e *Executor[Data]) ExecuteWithContext(
	ctx context.Context,
	execCtx *gent.ExecutionContext,
) *gent.ExecutionResult {
	// Set hook firer for model call hooks if not already set
	if e.hooks != nil {
		execCtx.SetHookFirer(e.hooks)
	}

	result := &gent.ExecutionResult{
		Context: execCtx,
	}

	// Ensure AfterExecution is always called if BeforeExecution succeeded
	beforeExecutionCalled := false
	defer func() {
		if beforeExecutionCalled && e.hooks != nil {
			event := gent.AfterExecutionEvent{
				TerminationReason: execCtx.TerminationReason(),
				Error:             execCtx.Error(),
			}
			if hookErr := e.hooks.FireAfterExecution(ctx, execCtx, event); hookErr != nil {
				e.hooks.FireError(ctx, execCtx, gent.ErrorEvent{
					Iteration: execCtx.Iteration(),
					Err:       fmt.Errorf("AfterExecution hook: %w", hookErr),
				})
			}
		}
	}()

	// BeforeExecution hook
	if e.hooks != nil {
		event := gent.BeforeExecutionEvent{}
		if err := e.hooks.FireBeforeExecution(ctx, execCtx, event); err != nil {
			result.Error = fmt.Errorf("BeforeExecution hook: %w", err)
			execCtx.SetTermination(gent.TerminationHookAbort, nil, result.Error)
			return result
		}
	}
	beforeExecutionCalled = true

	// Main execution loop
	for {
		if ctx.Err() != nil {
			result.Error = ctx.Err()
			execCtx.SetTermination(gent.TerminationContextCanceled, nil, result.Error)
			return result
		}

		if e.config.MaxIterations > 0 && execCtx.Iteration() >= e.config.MaxIterations {
			result.Error = fmt.Errorf(
				"%w: exceeded %d iterations",
				ErrMaxIterationsExceeded,
				e.config.MaxIterations,
			)
			execCtx.SetTermination(gent.TerminationMaxIterations, nil, result.Error)
			return result
		}

		execCtx.StartIteration()
		iterStart := time.Now()

		if e.hooks != nil {
			event := gent.BeforeIterationEvent{Iteration: execCtx.Iteration()}
			if err := e.hooks.FireBeforeIteration(ctx, execCtx, event); err != nil {
				execCtx.EndIteration(gent.LATerminate, time.Since(iterStart))
				result.Error = fmt.Errorf(
					"BeforeIteration hook (iteration %d): %w",
					execCtx.Iteration(),
					err,
				)
				execCtx.SetTermination(gent.TerminationHookAbort, nil, result.Error)
				return result
			}
		}

		loopResult := e.loop.Next(ctx, execCtx)
		iterDuration := time.Since(iterStart)
		execCtx.EndIteration(loopResult.Action, iterDuration)

		if e.hooks != nil {
			event := gent.AfterIterationEvent{
				Iteration: execCtx.Iteration(),
				Result:    loopResult,
				Duration:  iterDuration,
			}
			if err := e.hooks.FireAfterIteration(ctx, execCtx, event); err != nil {
				result.Error = fmt.Errorf(
					"AfterIteration hook (iteration %d): %w",
					execCtx.Iteration(),
					err,
				)
				execCtx.SetTermination(gent.TerminationHookAbort, nil, result.Error)
				return result
			}
		}

		if loopResult.Action == gent.LATerminate {
			result.Result = loopResult.Result
			execCtx.SetTermination(gent.TerminationSuccess, loopResult.Result, nil)
			return result
		}
	}
}
