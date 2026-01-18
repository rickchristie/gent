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
//	) error {
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
// Returns the first error encountered, or nil if all hooks succeed.
func (r *Registry) FireBeforeExecution(
	ctx context.Context,
	execCtx *gent.ExecutionContext,
	event gent.BeforeExecutionEvent,
) error {
	for _, h := range r.hooks {
		if hook, ok := h.(gent.BeforeExecutionHook); ok {
			if err := hook.OnBeforeExecution(ctx, execCtx, event); err != nil {
				return err
			}
		}
	}
	return nil
}

// FireAfterExecution dispatches an AfterExecutionEvent to all registered
// AfterExecutionHook implementations.
// All hooks are called even if some return errors. Returns the first error encountered.
func (r *Registry) FireAfterExecution(
	ctx context.Context,
	execCtx *gent.ExecutionContext,
	event gent.AfterExecutionEvent,
) error {
	var firstErr error
	for _, h := range r.hooks {
		if hook, ok := h.(gent.AfterExecutionHook); ok {
			if err := hook.OnAfterExecution(ctx, execCtx, event); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// FireBeforeIteration dispatches a BeforeIterationEvent to all registered
// BeforeIterationHook implementations.
// Returns the first error encountered, or nil if all hooks succeed.
func (r *Registry) FireBeforeIteration(
	ctx context.Context,
	execCtx *gent.ExecutionContext,
	event gent.BeforeIterationEvent,
) error {
	for _, h := range r.hooks {
		if hook, ok := h.(gent.BeforeIterationHook); ok {
			if err := hook.OnBeforeIteration(ctx, execCtx, event); err != nil {
				return err
			}
		}
	}
	return nil
}

// FireAfterIteration dispatches an AfterIterationEvent to all registered
// AfterIterationHook implementations.
// Returns the first error encountered, or nil if all hooks succeed.
func (r *Registry) FireAfterIteration(
	ctx context.Context,
	execCtx *gent.ExecutionContext,
	event gent.AfterIterationEvent,
) error {
	for _, h := range r.hooks {
		if hook, ok := h.(gent.AfterIterationHook); ok {
			if err := hook.OnAfterIteration(ctx, execCtx, event); err != nil {
				return err
			}
		}
	}
	return nil
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

// Len returns the number of registered hooks.
func (r *Registry) Len() int {
	return len(r.hooks)
}

// Clear removes all registered hooks.
func (r *Registry) Clear() {
	r.hooks = make([]any, 0)
}
