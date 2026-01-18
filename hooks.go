package gent

import (
	"context"
)

// -----------------------------------------------------------------------------
// Executor Hook Interfaces
// -----------------------------------------------------------------------------

// BeforeExecutionHook is implemented by hooks that want to be notified before execution starts.
type BeforeExecutionHook interface {
	// OnBeforeExecution is called once before the first iteration.
	// Return an error to abort execution before it begins.
	OnBeforeExecution(ctx context.Context, execCtx *ExecutionContext, event BeforeExecutionEvent) error
}

// AfterExecutionHook is implemented by hooks that want to be notified after execution terminates.
type AfterExecutionHook interface {
	// OnAfterExecution is called once after the loop terminates (successfully or with error).
	// This is always called if OnBeforeExecution succeeded, even on error.
	// Errors returned here are logged but do not change the execution result.
	OnAfterExecution(ctx context.Context, execCtx *ExecutionContext, event AfterExecutionEvent) error
}

// BeforeIterationHook is implemented by hooks that want to be notified before each iteration.
type BeforeIterationHook interface {
	// OnBeforeIteration is called before each AgentLoop.Next call.
	// Return an error to abort execution.
	OnBeforeIteration(ctx context.Context, execCtx *ExecutionContext, event BeforeIterationEvent) error
}

// AfterIterationHook is implemented by hooks that want to be notified after each iteration.
type AfterIterationHook interface {
	// OnAfterIteration is called after each AgentLoop.Next call.
	// Return an error to abort execution (the current result is still recorded).
	OnAfterIteration(ctx context.Context, execCtx *ExecutionContext, event AfterIterationEvent) error
}

// ErrorHook is implemented by hooks that want to be notified of errors.
type ErrorHook interface {
	// OnError is called when an error occurs during execution.
	// This is informational; the error will still be returned from Execute.
	OnError(ctx context.Context, execCtx *ExecutionContext, event ErrorEvent)
}

// -----------------------------------------------------------------------------
// Model Call Hook Interfaces
// -----------------------------------------------------------------------------

// BeforeModelCallHook is implemented by hooks that want to be notified before model calls.
type BeforeModelCallHook interface {
	// OnBeforeModelCall is called before each model API call.
	// This is informational; errors are not propagated.
	OnBeforeModelCall(ctx context.Context, execCtx *ExecutionContext, event BeforeModelCallEvent)
}

// AfterModelCallHook is implemented by hooks that want to be notified after model calls.
type AfterModelCallHook interface {
	// OnAfterModelCall is called after each model API call completes.
	// This is informational; errors are not propagated.
	OnAfterModelCall(ctx context.Context, execCtx *ExecutionContext, event AfterModelCallEvent)
}

// -----------------------------------------------------------------------------
// Tool Call Hook Interfaces
// -----------------------------------------------------------------------------

// BeforeToolCallHook is implemented by hooks that want to be notified before tool calls.
type BeforeToolCallHook interface {
	// OnBeforeToolCall is called before each tool execution.
	// The hook can modify event.Args to change the input.
	// Return an error to abort the tool call (the error becomes the tool's error result).
	OnBeforeToolCall(
		ctx context.Context,
		execCtx *ExecutionContext,
		event *BeforeToolCallEvent,
	) error
}

// AfterToolCallHook is implemented by hooks that want to be notified after tool calls.
type AfterToolCallHook interface {
	// OnAfterToolCall is called after each tool execution completes.
	// This is informational; errors are not propagated.
	OnAfterToolCall(ctx context.Context, execCtx *ExecutionContext, event AfterToolCallEvent)
}
