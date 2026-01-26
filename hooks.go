package gent

import (
	"context"
)

// -----------------------------------------------------------------------------
// Executor Hook Interfaces
// -----------------------------------------------------------------------------
//
// Hooks allow observing and intercepting execution at various points. To use hooks:
//
//  1. Implement the desired hook interface(s)
//  2. Register with hooks.Registry
//  3. Pass the registry to executor.Config
//
// Example:
//
//	type LoggingHook struct {
//	    logger *log.Logger
//	}
//
//	func (h *LoggingHook) OnBeforeIteration(ctx context.Context, execCtx *ExecutionContext, e BeforeIterationEvent) {
//	    h.logger.Printf("Starting iteration %d", e.Iteration)
//	}
//
//	func (h *LoggingHook) OnAfterModelCall(ctx context.Context, execCtx *ExecutionContext, e AfterModelCallEvent) {
//	    h.logger.Printf("Model %s: %d tokens in %v", e.Model, e.Response.Info.InputTokens, e.Duration)
//	}
//
//	// Register and use
//	registry := hooks.NewRegistry()
//	registry.Register(&LoggingHook{logger: log.Default()})
//	exec := executor.New(agent, executor.Config{Hooks: registry})
//
// # Hook Execution Order
//
// Hooks are called in registration order. For paired hooks (Before/After), the After
// hook is always called if the Before hook was called, even on error.
//
// # Error Handling
//
// Hooks should NOT return errors. If a hook panics:
//   - Before hooks: Execution stops, panic propagates
//   - After hooks: Panic propagates after cleanup
//
// Implement proper error recovery if you need to handle errors gracefully.
//
// # Available Hooks
//
//   - Execution lifecycle: [BeforeExecutionHook], [AfterExecutionHook]
//   - Iteration lifecycle: [BeforeIterationHook], [AfterIterationHook]
//   - Model calls: [BeforeModelCallHook], [AfterModelCallHook]
//   - Tool calls: [BeforeToolCallHook], [AfterToolCallHook]
//   - Error handling: [ErrorHook]
// -----------------------------------------------------------------------------

// BeforeExecutionHook is implemented by hooks that want to be notified before execution starts.
//
// This hook is called once at the very beginning of Execute(), before any iterations run.
// Use it for:
//   - Initializing per-execution resources (timers, spans)
//   - Logging execution start with task information
//   - Setting up monitoring or tracing contexts
//
// The event contains the original Task that was passed to Execute().
//
// Example:
//
//	func (h *MyHook) OnBeforeExecution(
//	    ctx context.Context,
//	    execCtx *gent.ExecutionContext,
//	    event gent.BeforeExecutionEvent,
//	) {
//	    h.startTime = time.Now()
//	    h.logger.Printf("Starting execution: %s", event.Task.Text[:50])
//	}
type BeforeExecutionHook interface {
	// OnBeforeExecution is called once before the first iteration.
	OnBeforeExecution(ctx context.Context, execCtx *ExecutionContext, event BeforeExecutionEvent)
}

// AfterExecutionHook is implemented by hooks that want to be notified after execution terminates.
//
// This hook is always called if BeforeExecution was called, even when execution ends with an
// error. Use it for:
//   - Cleaning up per-execution resources
//   - Recording final metrics (duration, token usage)
//   - Closing monitoring spans
//
// The event contains the final result (if successful) or error (if failed).
//
// Example:
//
//	func (h *MyHook) OnAfterExecution(
//	    ctx context.Context,
//	    execCtx *gent.ExecutionContext,
//	    event gent.AfterExecutionEvent,
//	) {
//	    duration := time.Since(h.startTime)
//	    stats := execCtx.Stats()
//	    h.metrics.RecordExecution(duration, stats.GetCounter(gent.KeyInputTokens))
//	}
type AfterExecutionHook interface {
	// OnAfterExecution is called once after the loop terminates (successfully or with error).
	// This is always called if OnBeforeExecution was called, even on error.
	OnAfterExecution(ctx context.Context, execCtx *ExecutionContext, event AfterExecutionEvent)
}

// BeforeIterationHook is implemented by hooks that want to be notified before each iteration.
//
// If the hook panics, execution will stop. Hooks should implement proper error recovery
// if they need to handle errors gracefully.
type BeforeIterationHook interface {
	// OnBeforeIteration is called before each AgentLoop.Next call.
	OnBeforeIteration(ctx context.Context, execCtx *ExecutionContext, event BeforeIterationEvent)
}

// AfterIterationHook is implemented by hooks that want to be notified after each iteration.
//
// If the hook panics, execution will stop. Hooks should implement proper error recovery
// if they need to handle errors gracefully.
type AfterIterationHook interface {
	// OnAfterIteration is called after each AgentLoop.Next call.
	OnAfterIteration(ctx context.Context, execCtx *ExecutionContext, event AfterIterationEvent)
}

// ErrorHook is implemented by hooks that want to be notified of errors.
//
// If the hook panics, the panic will propagate. Hooks should implement proper error recovery
// if they need to handle errors gracefully.
type ErrorHook interface {
	// OnError is called when an error occurs during execution.
	// The error will still be returned from Execute.
	OnError(ctx context.Context, execCtx *ExecutionContext, event ErrorEvent)
}

// -----------------------------------------------------------------------------
// Model Call Hook Interfaces
// -----------------------------------------------------------------------------

// BeforeModelCallHook is implemented by hooks that want to be notified before model calls.
//
// If the hook panics, the panic will propagate. Hooks should implement proper error recovery
// if they need to handle errors gracefully.
type BeforeModelCallHook interface {
	// OnBeforeModelCall is called before each model API call.
	OnBeforeModelCall(ctx context.Context, execCtx *ExecutionContext, event BeforeModelCallEvent)
}

// AfterModelCallHook is implemented by hooks that want to be notified after model calls.
//
// If the hook panics, the panic will propagate. Hooks should implement proper error recovery
// if they need to handle errors gracefully.
type AfterModelCallHook interface {
	// OnAfterModelCall is called after each model API call completes.
	OnAfterModelCall(ctx context.Context, execCtx *ExecutionContext, event AfterModelCallEvent)
}

// -----------------------------------------------------------------------------
// Tool Call Hook Interfaces
// -----------------------------------------------------------------------------

// BeforeToolCallHook is implemented by hooks that want to be notified before tool calls.
//
// If the hook panics, execution will stop. Hooks should implement proper error recovery
// if they need to handle errors gracefully.
type BeforeToolCallHook interface {
	// OnBeforeToolCall is called before each tool execution.
	// The hook can modify event.Args to change the input.
	OnBeforeToolCall(ctx context.Context, execCtx *ExecutionContext, event *BeforeToolCallEvent)
}

// AfterToolCallHook is implemented by hooks that want to be notified after tool calls.
//
// If the hook panics, the panic will propagate. Hooks should implement proper error recovery
// if they need to handle errors gracefully.
type AfterToolCallHook interface {
	// OnAfterToolCall is called after each tool execution completes.
	OnAfterToolCall(ctx context.Context, execCtx *ExecutionContext, event AfterToolCallEvent)
}
