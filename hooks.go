package gent

import (
	"context"
)

// -----------------------------------------------------------------------------
// Executor Hook Interfaces
// -----------------------------------------------------------------------------

// BeforeExecutionHook is implemented by hooks that want to be notified before execution starts.
//
// If the hook panics, execution will stop. Hooks should implement proper error recovery
// if they need to handle errors gracefully.
type BeforeExecutionHook interface {
	// OnBeforeExecution is called once before the first iteration.
	OnBeforeExecution(ctx context.Context, execCtx *ExecutionContext, event BeforeExecutionEvent)
}

// AfterExecutionHook is implemented by hooks that want to be notified after execution terminates.
//
// If the hook panics, the panic will propagate. Hooks should implement proper error recovery
// if they need to handle errors gracefully.
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
