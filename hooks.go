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
