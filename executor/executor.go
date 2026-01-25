package executor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/hooks"
)

// ErrLimitExceeded is returned when a configured limit is exceeded.
var ErrLimitExceeded = errors.New("gent: limit exceeded")

// Config holds configuration options for the Executor.
type Config struct {
	// Limits defines thresholds that trigger execution termination.
	// Limits are checked after each iteration in order.
	// The first limit exceeded determines which limit is reported.
	Limits []gent.Limit
}

// DefaultConfig returns a config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Limits: gent.DefaultLimits(),
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
func (e *Executor[Data]) Execute(ctx context.Context, data Data) (*gent.ExecutionResult, error) {
	execCtx := gent.NewExecutionContext("main", data)

	// Set hook firer for model call hooks
	if e.hooks != nil {
		execCtx.SetHookFirer(e.hooks)
	}

	result := &gent.ExecutionResult{
		Context: execCtx,
	}

	// Ensure streams are closed and AfterExecution is always called if BeforeExecution succeeded
	var execErr error
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
			execErr = fmt.Errorf("BeforeExecution hook: %w", err)
			execCtx.SetTermination(gent.TerminationHookAbort, nil, execErr)
			return result, execErr
		}
	}
	beforeExecutionCalled = true

	// Main execution loop
	for {
		// Check context cancellation
		if ctx.Err() != nil {
			execErr = ctx.Err()
			execCtx.SetTermination(gent.TerminationContextCanceled, nil, execErr)
			return result, execErr
		}

		// Check limits before starting new iteration
		if limit := e.checkLimits(execCtx); limit != nil {
			execErr = fmt.Errorf(
				"%w: key=%s, max=%v",
				ErrLimitExceeded,
				limit.Key,
				limit.MaxValue,
			)
			result.ExceededLimit = limit
			execCtx.SetTermination(gent.TerminationLimitExceeded, nil, execErr)
			return result, execErr
		}

		// Start iteration (increments counter and records IterationStartTrace)
		execCtx.StartIteration()
		iterStart := time.Now()

		// BeforeIteration hook
		if e.hooks != nil {
			event := gent.BeforeIterationEvent{Iteration: execCtx.Iteration()}
			if err := e.hooks.FireBeforeIteration(ctx, execCtx, event); err != nil {
				execCtx.EndIteration(gent.LATerminate, time.Since(iterStart))
				execErr = fmt.Errorf(
					"BeforeIteration hook (iteration %d): %w",
					execCtx.Iteration(),
					err,
				)
				execCtx.SetTermination(gent.TerminationHookAbort, nil, execErr)
				return result, execErr
			}
		}

		// Execute the AgentLoop iteration
		loopResult, loopErr := e.loop.Next(ctx, execCtx)
		iterDuration := time.Since(iterStart)

		// Handle loop error
		if loopErr != nil {
			execCtx.EndIteration(gent.LATerminate, iterDuration)
			execErr = fmt.Errorf("AgentLoop.Next (iteration %d): %w", execCtx.Iteration(), loopErr)
			execCtx.SetTermination(gent.TerminationError, nil, execErr)
			return result, execErr
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
				execErr = fmt.Errorf(
					"AfterIteration hook (iteration %d): %w",
					execCtx.Iteration(),
					err,
				)
				execCtx.SetTermination(gent.TerminationHookAbort, nil, execErr)
				return result, execErr
			}
		}

		// Check for termination
		if loopResult.Action == gent.LATerminate {
			result.Result = loopResult.Result
			execCtx.SetTermination(gent.TerminationSuccess, loopResult.Result, nil)
			return result, nil
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
) (*gent.ExecutionResult, error) {
	// Set hook firer for model call hooks if not already set
	if e.hooks != nil {
		execCtx.SetHookFirer(e.hooks)
	}

	result := &gent.ExecutionResult{
		Context: execCtx,
	}

	// Ensure streams are closed and AfterExecution is always called if BeforeExecution succeeded
	var execErr error
	beforeExecutionCalled := false
	defer func() {
		// Always close streams when execution ends
		execCtx.CloseStreams()

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
			execErr = fmt.Errorf("BeforeExecution hook: %w", err)
			execCtx.SetTermination(gent.TerminationHookAbort, nil, execErr)
			return result, execErr
		}
	}
	beforeExecutionCalled = true

	// Main execution loop
	for {
		if ctx.Err() != nil {
			execErr = ctx.Err()
			execCtx.SetTermination(gent.TerminationContextCanceled, nil, execErr)
			return result, execErr
		}

		// Check limits before starting new iteration
		if limit := e.checkLimits(execCtx); limit != nil {
			execErr = fmt.Errorf(
				"%w: key=%s, max=%v",
				ErrLimitExceeded,
				limit.Key,
				limit.MaxValue,
			)
			result.ExceededLimit = limit
			execCtx.SetTermination(gent.TerminationLimitExceeded, nil, execErr)
			return result, execErr
		}

		execCtx.StartIteration()
		iterStart := time.Now()

		if e.hooks != nil {
			event := gent.BeforeIterationEvent{Iteration: execCtx.Iteration()}
			if err := e.hooks.FireBeforeIteration(ctx, execCtx, event); err != nil {
				execCtx.EndIteration(gent.LATerminate, time.Since(iterStart))
				execErr = fmt.Errorf(
					"BeforeIteration hook (iteration %d): %w",
					execCtx.Iteration(),
					err,
				)
				execCtx.SetTermination(gent.TerminationHookAbort, nil, execErr)
				return result, execErr
			}
		}

		loopResult, loopErr := e.loop.Next(ctx, execCtx)
		iterDuration := time.Since(iterStart)

		// Handle loop error
		if loopErr != nil {
			execCtx.EndIteration(gent.LATerminate, iterDuration)
			execErr = fmt.Errorf("AgentLoop.Next (iteration %d): %w", execCtx.Iteration(), loopErr)
			execCtx.SetTermination(gent.TerminationError, nil, execErr)
			return result, execErr
		}

		execCtx.EndIteration(loopResult.Action, iterDuration)

		if e.hooks != nil {
			event := gent.AfterIterationEvent{
				Iteration: execCtx.Iteration(),
				Result:    loopResult,
				Duration:  iterDuration,
			}
			if err := e.hooks.FireAfterIteration(ctx, execCtx, event); err != nil {
				execErr = fmt.Errorf(
					"AfterIteration hook (iteration %d): %w",
					execCtx.Iteration(),
					err,
				)
				execCtx.SetTermination(gent.TerminationHookAbort, nil, execErr)
				return result, execErr
			}
		}

		if loopResult.Action == gent.LATerminate {
			result.Result = loopResult.Result
			execCtx.SetTermination(gent.TerminationSuccess, loopResult.Result, nil)
			return result, nil
		}
	}
}

// checkLimits evaluates all configured limits against current stats.
// Returns the exceeded limit if any, or nil if all limits are within bounds.
// Limits are checked in order; first match wins.
func (e *Executor[Data]) checkLimits(execCtx *gent.ExecutionContext) *gent.Limit {
	stats := execCtx.Stats()

	for i := range e.config.Limits {
		limit := &e.config.Limits[i]
		switch limit.Type {
		case gent.LimitExactKey:
			if e.checkExactKeyLimit(stats, limit) {
				return limit
			}

		case gent.LimitKeyPrefix:
			if e.checkPrefixLimit(stats, limit) {
				return limit
			}
		}
	}
	return nil
}

func (e *Executor[Data]) checkExactKeyLimit(
	stats *gent.ExecutionStats,
	limit *gent.Limit,
) bool {
	// Check counters
	if val := stats.GetCounter(limit.Key); val > 0 {
		if float64(val) > limit.MaxValue {
			return true
		}
	}
	// Check gauges
	if val := stats.GetGauge(limit.Key); val > 0 {
		if val > limit.MaxValue {
			return true
		}
	}
	return false
}

func (e *Executor[Data]) checkPrefixLimit(stats *gent.ExecutionStats, limit *gent.Limit) bool {
	for key, val := range stats.Counters() {
		if strings.HasPrefix(key, limit.Key) && float64(val) > limit.MaxValue {
			return true
		}
	}
	for key, val := range stats.Gauges() {
		if strings.HasPrefix(key, limit.Key) && val > limit.MaxValue {
			return true
		}
	}
	return false
}
