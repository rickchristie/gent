package hooks

import (
	"context"

	"github.com/rickchristie/gent"
)

// Registry manages a collection of hooks and dispatches events to them.
// Hooks can implement any combination of hook interfaces - they will only
// receive events for the interfaces they implement.
//
// ExecutionContext is passed separately from events, making it clear that
// the context is always available. Hooks can access LoopData via execCtx.Data()
// and can spawn child contexts if needed.
//
// Example usage:
//
//	type MyHook struct{}
//	func (h *MyHook) OnBeforeExecution(
//	    ctx context.Context, execCtx *gent.ExecutionContext, e gent.BeforeExecutionEvent,
//	) {
//	    data := execCtx.Data().(MyLoopData)
//	    ...
//	}
//
//	registry := hooks.NewRegistry()
//	registry.Register(&MyHook{})
type Registry struct {
	hooks []any
}

// NewRegistry creates a new empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		hooks: make([]any, 0),
	}
}

// Register adds a hook to the registry. The hook can implement any combination
// of hook interfaces (BeforeExecutionHook, AfterExecutionHook, etc.).
//
// Hooks are called in the order they are registered.
func (r *Registry) Register(hook any) *Registry {
	r.hooks = append(r.hooks, hook)
	return r
}

// FireBeforeExecution dispatches a BeforeExecutionEvent to all registered
// BeforeExecutionHook implementations.
func (r *Registry) FireBeforeExecution(
	ctx context.Context,
	execCtx *gent.ExecutionContext,
	event gent.BeforeExecutionEvent,
) {
	for _, h := range r.hooks {
		if hook, ok := h.(gent.BeforeExecutionHook); ok {
			hook.OnBeforeExecution(ctx, execCtx, event)
		}
	}
}

// FireAfterExecution dispatches an AfterExecutionEvent to all registered
// AfterExecutionHook implementations.
func (r *Registry) FireAfterExecution(
	ctx context.Context,
	execCtx *gent.ExecutionContext,
	event gent.AfterExecutionEvent,
) {
	for _, h := range r.hooks {
		if hook, ok := h.(gent.AfterExecutionHook); ok {
			hook.OnAfterExecution(ctx, execCtx, event)
		}
	}
}

// FireBeforeIteration dispatches a BeforeIterationEvent to all registered
// BeforeIterationHook implementations.
func (r *Registry) FireBeforeIteration(
	ctx context.Context,
	execCtx *gent.ExecutionContext,
	event gent.BeforeIterationEvent,
) {
	for _, h := range r.hooks {
		if hook, ok := h.(gent.BeforeIterationHook); ok {
			hook.OnBeforeIteration(ctx, execCtx, event)
		}
	}
}

// FireAfterIteration dispatches an AfterIterationEvent to all registered
// AfterIterationHook implementations.
func (r *Registry) FireAfterIteration(
	ctx context.Context,
	execCtx *gent.ExecutionContext,
	event gent.AfterIterationEvent,
) {
	for _, h := range r.hooks {
		if hook, ok := h.(gent.AfterIterationHook); ok {
			hook.OnAfterIteration(ctx, execCtx, event)
		}
	}
}

// FireError dispatches an ErrorEvent to all registered ErrorHook implementations.
// This is informational only; errors from hooks are not propagated.
func (r *Registry) FireError(ctx context.Context, execCtx *gent.ExecutionContext, event gent.ErrorEvent) {
	for _, h := range r.hooks {
		if hook, ok := h.(gent.ErrorHook); ok {
			hook.OnError(ctx, execCtx, event)
		}
	}
}

// FireBeforeModelCall dispatches a BeforeModelCallEvent to all registered
// BeforeModelCallHook implementations.
// This is informational only; errors from hooks are not propagated.
func (r *Registry) FireBeforeModelCall(
	ctx context.Context,
	execCtx *gent.ExecutionContext,
	event gent.BeforeModelCallEvent,
) {
	for _, h := range r.hooks {
		if hook, ok := h.(gent.BeforeModelCallHook); ok {
			hook.OnBeforeModelCall(ctx, execCtx, event)
		}
	}
}

// FireAfterModelCall dispatches an AfterModelCallEvent to all registered
// AfterModelCallHook implementations.
// This is informational only; errors from hooks are not propagated.
func (r *Registry) FireAfterModelCall(
	ctx context.Context,
	execCtx *gent.ExecutionContext,
	event gent.AfterModelCallEvent,
) {
	for _, h := range r.hooks {
		if hook, ok := h.(gent.AfterModelCallHook); ok {
			hook.OnAfterModelCall(ctx, execCtx, event)
		}
	}
}

// FireBeforeToolCall dispatches a BeforeToolCallEvent to all registered
// BeforeToolCallHook implementations.
// Hooks can modify event.Args to change the tool input.
func (r *Registry) FireBeforeToolCall(
	ctx context.Context,
	execCtx *gent.ExecutionContext,
	event *gent.BeforeToolCallEvent,
) {
	for _, h := range r.hooks {
		if hook, ok := h.(gent.BeforeToolCallHook); ok {
			hook.OnBeforeToolCall(ctx, execCtx, event)
		}
	}
}

// FireAfterToolCall dispatches an AfterToolCallEvent to all registered
// AfterToolCallHook implementations.
// This is informational only; errors from hooks are not propagated.
func (r *Registry) FireAfterToolCall(
	ctx context.Context,
	execCtx *gent.ExecutionContext,
	event gent.AfterToolCallEvent,
) {
	for _, h := range r.hooks {
		if hook, ok := h.(gent.AfterToolCallHook); ok {
			hook.OnAfterToolCall(ctx, execCtx, event)
		}
	}
}

// Len returns the number of registered hooks.
func (r *Registry) Len() int {
	return len(r.hooks)
}

// Clear removes all registered hooks.
func (r *Registry) Clear() {
	r.hooks = make([]any, 0)
}
