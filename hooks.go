package gent

import (
	"context"
)

// -----------------------------------------------------------------------------
// Executor Hook Interfaces
// -----------------------------------------------------------------------------

// BeforeExecutionHook is implemented by hooks that want to be notified before execution starts.
type BeforeExecutionHook[Data LoopData] interface {
	// OnBeforeExecution is called once before the first iteration.
	// Return an error to abort execution before it begins.
	OnBeforeExecution(ctx context.Context, event BeforeExecutionEvent[Data]) error
}

// AfterExecutionHook is implemented by hooks that want to be notified after execution terminates.
type AfterExecutionHook interface {
	// OnAfterExecution is called once after the loop terminates (successfully or with error).
	// This is always called if OnBeforeExecution succeeded, even on error.
	// Errors returned here are logged but do not change the execution result.
	OnAfterExecution(ctx context.Context, event AfterExecutionEvent) error
}

// BeforeIterationHook is implemented by hooks that want to be notified before each iteration.
type BeforeIterationHook[Data LoopData] interface {
	// OnBeforeIteration is called before each AgentLoop.Iterate call.
	// Return an error to abort execution.
	OnBeforeIteration(ctx context.Context, event BeforeIterationEvent[Data]) error
}

// AfterIterationHook is implemented by hooks that want to be notified after each iteration.
type AfterIterationHook[Data LoopData] interface {
	// OnAfterIteration is called after each AgentLoop.Iterate call.
	// Return an error to abort execution (the current result is still recorded).
	OnAfterIteration(ctx context.Context, event AfterIterationEvent[Data]) error
}

// ErrorHook is implemented by hooks that want to be notified of errors.
type ErrorHook interface {
	// OnError is called when an error occurs during execution.
	// This is informational; the error will still be returned from Execute.
	OnError(ctx context.Context, event ErrorEvent)
}

// -----------------------------------------------------------------------------
// Model Hook Interfaces
// -----------------------------------------------------------------------------

// BeforeGenerateContentHook is implemented by hooks that want to be notified
// before GenerateContent is called.
type BeforeGenerateContentHook interface {
	// OnBeforeGenerateContent is called before Model.GenerateContent.
	// Return an error to abort the call (the error will be returned to the caller).
	OnBeforeGenerateContent(ctx context.Context, event BeforeGenerateContentEvent) error
}

// AfterGenerateContentHook is implemented by hooks that want to be notified
// after GenerateContent returns.
type AfterGenerateContentHook interface {
	// OnAfterGenerateContent is called after Model.GenerateContent returns.
	// This is always called, even if the model returned an error.
	// Errors returned here are informational and do not affect the result.
	OnAfterGenerateContent(ctx context.Context, event AfterGenerateContentEvent)
}
