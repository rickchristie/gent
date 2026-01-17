package hooks

import (
	"context"

	"github.com/rickchristie/gent"
)

// Registry manages a collection of hooks and dispatches events to them.
// Hooks can implement any combination of hook interfaces - they will only
// receive events for the interfaces they implement.
//
// Example usage:
//
//	type MyHook struct{}
//	func (h *MyHook) OnBeforeExecution(
//	    ctx context.Context, e gent.BeforeExecutionEvent[MyData],
//	) error {
//	    ...
//	}
//	func (h *MyHook) OnAfterExecution(ctx context.Context, e gent.AfterExecutionEvent) error { ... }
//
//	registry := hooks.NewRegistry[MyData]()
//	registry.Register(&MyHook{})
type Registry[Data gent.LoopData] struct {
	hooks []any
}

// NewRegistry creates a new empty Registry.
func NewRegistry[Data gent.LoopData]() *Registry[Data] {
	return &Registry[Data]{
		hooks: make([]any, 0),
	}
}

// Register adds a hook to the registry. The hook can implement any combination
// of hook interfaces (BeforeExecutionHook, AfterExecutionHook, etc.).
//
// Hooks are called in the order they are registered.
func (r *Registry[Data]) Register(hook any) *Registry[Data] {
	r.hooks = append(r.hooks, hook)
	return r
}

// FireBeforeExecution dispatches a BeforeExecutionEvent to all registered
// BeforeExecutionHook implementations.
// Returns the first error encountered, or nil if all hooks succeed.
func (r *Registry[Data]) FireBeforeExecution(
	ctx context.Context,
	event gent.BeforeExecutionEvent[Data],
) error {
	for _, h := range r.hooks {
		if hook, ok := h.(gent.BeforeExecutionHook[Data]); ok {
			if err := hook.OnBeforeExecution(ctx, event); err != nil {
				return err
			}
		}
	}
	return nil
}

// FireAfterExecution dispatches an AfterExecutionEvent to all registered
// AfterExecutionHook implementations.
// All hooks are called even if some return errors. Returns the first error encountered.
func (r *Registry[Data]) FireAfterExecution(
	ctx context.Context,
	event gent.AfterExecutionEvent,
) error {
	var firstErr error
	for _, h := range r.hooks {
		if hook, ok := h.(gent.AfterExecutionHook); ok {
			if err := hook.OnAfterExecution(ctx, event); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// FireBeforeIteration dispatches a BeforeIterationEvent to all registered
// BeforeIterationHook implementations.
// Returns the first error encountered, or nil if all hooks succeed.
func (r *Registry[Data]) FireBeforeIteration(
	ctx context.Context,
	event gent.BeforeIterationEvent[Data],
) error {
	for _, h := range r.hooks {
		if hook, ok := h.(gent.BeforeIterationHook[Data]); ok {
			if err := hook.OnBeforeIteration(ctx, event); err != nil {
				return err
			}
		}
	}
	return nil
}

// FireAfterIteration dispatches an AfterIterationEvent to all registered
// AfterIterationHook implementations.
// Returns the first error encountered, or nil if all hooks succeed.
func (r *Registry[Data]) FireAfterIteration(
	ctx context.Context,
	event gent.AfterIterationEvent[Data],
) error {
	for _, h := range r.hooks {
		if hook, ok := h.(gent.AfterIterationHook[Data]); ok {
			if err := hook.OnAfterIteration(ctx, event); err != nil {
				return err
			}
		}
	}
	return nil
}

// FireError dispatches an ErrorEvent to all registered ErrorHook implementations.
// This is informational only; errors from hooks are not propagated.
func (r *Registry[Data]) FireError(ctx context.Context, event gent.ErrorEvent) {
	for _, h := range r.hooks {
		if hook, ok := h.(gent.ErrorHook); ok {
			hook.OnError(ctx, event)
		}
	}
}

// Len returns the number of registered hooks.
func (r *Registry[Data]) Len() int {
	return len(r.hooks)
}

// Clear removes all registered hooks.
func (r *Registry[Data]) Clear() {
	r.hooks = make([]any, 0)
}

// ----------------------------------------------------------------------------
// Model Hook Registry
// ----------------------------------------------------------------------------

// ModelRegistry manages hooks for Model calls.
type ModelRegistry struct {
	hooks []any
}

// NewModelRegistry creates a new empty ModelRegistry.
func NewModelRegistry() *ModelRegistry {
	return &ModelRegistry{
		hooks: make([]any, 0),
	}
}

// Register adds a hook to the registry. The hook can implement any combination
// of model hook interfaces.
func (r *ModelRegistry) Register(hook any) *ModelRegistry {
	r.hooks = append(r.hooks, hook)
	return r
}

// FireBeforeGenerateContent dispatches a BeforeGenerateContentEvent to all
// registered BeforeGenerateContentHook implementations.
func (r *ModelRegistry) FireBeforeGenerateContent(
	ctx context.Context,
	event gent.BeforeGenerateContentEvent,
) error {
	for _, h := range r.hooks {
		if hook, ok := h.(gent.BeforeGenerateContentHook); ok {
			if err := hook.OnBeforeGenerateContent(ctx, event); err != nil {
				return err
			}
		}
	}
	return nil
}

// FireAfterGenerateContent dispatches an AfterGenerateContentEvent to all
// registered AfterGenerateContentHook implementations.
func (r *ModelRegistry) FireAfterGenerateContent(
	ctx context.Context,
	event gent.AfterGenerateContentEvent,
) {
	for _, h := range r.hooks {
		if hook, ok := h.(gent.AfterGenerateContentHook); ok {
			hook.OnAfterGenerateContent(ctx, event)
		}
	}
}

// Len returns the number of registered hooks.
func (r *ModelRegistry) Len() int {
	return len(r.hooks)
}

// Clear removes all registered hooks.
func (r *ModelRegistry) Clear() {
	r.hooks = make([]any, 0)
}
