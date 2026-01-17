package gent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrMaxIterationsExceeded is returned when the executor exceeds the configured maximum iterations.
var ErrMaxIterationsExceeded = errors.New("gent: maximum iterations exceeded")

// ExecutorConfig holds configuration options for the Executor.
type ExecutorConfig struct {
	// MaxIterations is the maximum number of loop iterations before termination with error.
	// Set to 0 for unlimited iterations.
	MaxIterations int
}

// DefaultExecutorConfig returns a config with sensible defaults.
func DefaultExecutorConfig() ExecutorConfig {
	return ExecutorConfig{
		MaxIterations: 100,
	}
}

// Executor orchestrates the execution of an AgentLoop, managing the lifecycle,
// hooks, and trace collection.
//
// The Executor is responsible for:
//   - Running the AgentLoop repeatedly until it returns [LATerminate]
//   - Invoking lifecycle hooks at appropriate points
//   - Collecting execution trace data for debugging and observability
//   - Enforcing configuration limits (e.g., max iterations)
type Executor[Data LoopData] struct {
	loop   AgentLoop[Data]
	config ExecutorConfig
	hooks  *HookRegistry[Data]

	mu    sync.RWMutex
	trace *ExecutionTrace
}

// NewExecutor creates a new Executor with the given AgentLoop and configuration.
func NewExecutor[Data LoopData](loop AgentLoop[Data], config ExecutorConfig) *Executor[Data] {
	return &Executor[Data]{
		loop:   loop,
		config: config,
		hooks:  NewHookRegistry[Data](),
	}
}

// WithHooks sets the executor hook registry. Returns the executor for chaining.
func (e *Executor[Data]) WithHooks(hooks *HookRegistry[Data]) *Executor[Data] {
	e.hooks = hooks
	return e
}

// RegisterHook adds a hook to the executor's hook registry.
// The hook can implement any combination of hook interfaces.
// Returns the executor for chaining.
func (e *Executor[Data]) RegisterHook(hook any) *Executor[Data] {
	e.hooks.Register(hook)
	return e
}

// ExecutionResult contains the final result of an execution run.
type ExecutionResult struct {
	// Result is the final output from the AgentLoop (set when terminated successfully).
	Result string

	// Trace contains detailed execution trace data.
	Trace *ExecutionTrace

	// Error is any error that occurred (nil if successful).
	Error error
}

// Execute runs the AgentLoop until termination.
//
// The execution flow:
//  1. Call BeforeExecution hook (if set)
//  2. Repeatedly call AgentLoop.Iterate until:
//     - It returns LATerminate
//     - MaxIterations is exceeded (if configured)
//     - Context is canceled
//     - An error occurs
//  3. Call AfterExecution hook (if set)
//
// Execute is safe to call concurrently (each call is independent), but the same
// Executor instance should not be used for concurrent executions if you want
// accurate trace data.
func (e *Executor[Data]) Execute(ctx context.Context, data Data) *ExecutionResult {
	e.mu.Lock()
	e.trace = &ExecutionTrace{
		StartTime:  time.Now(),
		Iterations: make([]IterationTrace, 0),
	}
	e.mu.Unlock()

	result := &ExecutionResult{
		Trace: e.trace,
	}

	// Ensure AfterExecution is always called if BeforeExecution succeeded
	beforeExecutionCalled := false
	defer func() {
		e.mu.Lock()
		e.trace.EndTime = time.Now()
		e.trace.TotalDuration = e.trace.EndTime.Sub(e.trace.StartTime)
		result.Trace = e.trace
		iterations := make([]IterationTrace, len(e.trace.Iterations))
		copy(iterations, e.trace.Iterations)
		e.mu.Unlock()

		if beforeExecutionCalled && e.hooks != nil {
			// AfterExecution errors are logged but don't change the result
			event := AfterExecutionEvent{Result: result, Iterations: iterations}
			if hookErr := e.hooks.FireAfterExecution(ctx, event); hookErr != nil {
				// The AfterExecution error doesn't override existing errors
				// but should be available for logging
				e.hooks.FireError(ctx, ErrorEvent{
					Iteration: e.trace.FinalIteration,
					Err:       fmt.Errorf("AfterExecution hook: %w", hookErr),
				})
			}
		}
	}()

	// BeforeExecution hook
	if e.hooks != nil {
		event := BeforeExecutionEvent[Data]{Data: data}
		if err := e.hooks.FireBeforeExecution(ctx, event); err != nil {
			result.Error = fmt.Errorf("BeforeExecution hook: %w", err)
			e.trace.TerminationReason = TerminationHookAbort
			return result
		}
	}
	beforeExecutionCalled = true

	// Main execution loop
	iteration := 0
	for {
		iteration++

		// Check context cancellation
		if ctx.Err() != nil {
			result.Error = ctx.Err()
			e.trace.TerminationReason = TerminationContextCanceled
			e.trace.FinalIteration = iteration - 1
			return result
		}

		// Check max iterations
		if e.config.MaxIterations > 0 && iteration > e.config.MaxIterations {
			result.Error = fmt.Errorf("%w: exceeded %d iterations", ErrMaxIterationsExceeded, e.config.MaxIterations)
			e.trace.TerminationReason = TerminationMaxIterations
			e.trace.FinalIteration = iteration - 1
			return result
		}

		// Create iteration trace
		iterTrace := IterationTrace{
			Iteration: iteration,
			StartTime: time.Now(),
			Metadata:  make(map[string]any),
		}

		// BeforeIteration hook
		if e.hooks != nil {
			event := BeforeIterationEvent[Data]{Iteration: iteration, Data: data}
			if err := e.hooks.FireBeforeIteration(ctx, event); err != nil {
				iterTrace.EndTime = time.Now()
				iterTrace.Duration = iterTrace.EndTime.Sub(iterTrace.StartTime)
				iterTrace.Error = err
				e.appendIterationTrace(iterTrace)

				result.Error = fmt.Errorf("BeforeIteration hook (iteration %d): %w", iteration, err)
				e.trace.TerminationReason = TerminationHookAbort
				e.trace.FinalIteration = iteration
				return result
			}
		}

		// Iterate the AgentLoop
		loopResult := e.loop.Iterate(ctx, data)
		iterTrace.Result = loopResult
		iterTrace.EndTime = time.Now()
		iterTrace.Duration = iterTrace.EndTime.Sub(iterTrace.StartTime)

		// AfterIteration hook
		if e.hooks != nil {
			event := AfterIterationEvent[Data]{Iteration: iteration, Result: loopResult, Data: data}
			if err := e.hooks.FireAfterIteration(ctx, event); err != nil {
				iterTrace.Error = err
				e.appendIterationTrace(iterTrace)

				result.Error = fmt.Errorf("AfterIteration hook (iteration %d): %w", iteration, err)
				e.trace.TerminationReason = TerminationHookAbort
				e.trace.FinalIteration = iteration
				return result
			}
		}

		e.appendIterationTrace(iterTrace)

		// Check for termination
		if loopResult.Action == LATerminate {
			result.Result = loopResult.Result
			e.trace.TerminationReason = TerminationSuccess
			e.trace.FinalIteration = iteration
			return result
		}

		// Continue - the AgentLoop is responsible for updating data with NextPrompt
		// The exact mechanism depends on the LoopData implementation
	}
}

// appendIterationTrace safely appends an iteration trace.
func (e *Executor[Data]) appendIterationTrace(trace IterationTrace) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.trace.Iterations = append(e.trace.Iterations, trace)
}

// GetTrace returns a copy of the current execution trace.
// This can be called during execution to get partial trace data.
func (e *Executor[Data]) GetTrace() *ExecutionTrace {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.trace == nil {
		return nil
	}

	// Return a shallow copy
	traceCopy := *e.trace
	traceCopy.Iterations = make([]IterationTrace, len(e.trace.Iterations))
	copy(traceCopy.Iterations, e.trace.Iterations)
	return &traceCopy
}
