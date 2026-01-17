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

// -----------------------------------------------------------------------------
// Executor Hook Registry
// -----------------------------------------------------------------------------

// HookRegistry manages a collection of hooks and dispatches events to them.
// Hooks can implement any combination of hook interfaces - they will only
// receive events for the interfaces they implement.
//
// Example usage:
//
//	type MyHook struct{}
//	func (h *MyHook) OnBeforeExecution(ctx context.Context, e BeforeExecutionEvent[MyData]) error { ... }
//	func (h *MyHook) OnAfterExecution(ctx context.Context, e AfterExecutionEvent) error { ... }
//
//	registry := NewHookRegistry[MyData]()
//	registry.Register(&MyHook{})
type HookRegistry[Data LoopData] struct {
	hooks []any
}

// NewHookRegistry creates a new empty HookRegistry.
func NewHookRegistry[Data LoopData]() *HookRegistry[Data] {
	return &HookRegistry[Data]{
		hooks: make([]any, 0),
	}
}

// Register adds a hook to the registry. The hook can implement any combination
// of hook interfaces (BeforeExecutionHook, AfterExecutionHook, etc.).
//
// Hooks are called in the order they are registered.
func (r *HookRegistry[Data]) Register(hook any) *HookRegistry[Data] {
	r.hooks = append(r.hooks, hook)
	return r
}

// FireBeforeExecution dispatches a BeforeExecutionEvent to all registered BeforeExecutionHook implementations.
// Returns the first error encountered, or nil if all hooks succeed.
func (r *HookRegistry[Data]) FireBeforeExecution(ctx context.Context, event BeforeExecutionEvent[Data]) error {
	for _, h := range r.hooks {
		if hook, ok := h.(BeforeExecutionHook[Data]); ok {
			if err := hook.OnBeforeExecution(ctx, event); err != nil {
				return err
			}
		}
	}
	return nil
}

// FireAfterExecution dispatches an AfterExecutionEvent to all registered AfterExecutionHook implementations.
// All hooks are called even if some return errors. Returns the first error encountered.
func (r *HookRegistry[Data]) FireAfterExecution(ctx context.Context, event AfterExecutionEvent) error {
	var firstErr error
	for _, h := range r.hooks {
		if hook, ok := h.(AfterExecutionHook); ok {
			if err := hook.OnAfterExecution(ctx, event); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// FireBeforeIteration dispatches a BeforeIterationEvent to all registered BeforeIterationHook implementations.
// Returns the first error encountered, or nil if all hooks succeed.
func (r *HookRegistry[Data]) FireBeforeIteration(ctx context.Context, event BeforeIterationEvent[Data]) error {
	for _, h := range r.hooks {
		if hook, ok := h.(BeforeIterationHook[Data]); ok {
			if err := hook.OnBeforeIteration(ctx, event); err != nil {
				return err
			}
		}
	}
	return nil
}

// FireAfterIteration dispatches an AfterIterationEvent to all registered AfterIterationHook implementations.
// Returns the first error encountered, or nil if all hooks succeed.
func (r *HookRegistry[Data]) FireAfterIteration(ctx context.Context, event AfterIterationEvent[Data]) error {
	for _, h := range r.hooks {
		if hook, ok := h.(AfterIterationHook[Data]); ok {
			if err := hook.OnAfterIteration(ctx, event); err != nil {
				return err
			}
		}
	}
	return nil
}

// FireError dispatches an ErrorEvent to all registered ErrorHook implementations.
// This is informational only; errors from hooks are not propagated.
func (r *HookRegistry[Data]) FireError(ctx context.Context, event ErrorEvent) {
	for _, h := range r.hooks {
		if hook, ok := h.(ErrorHook); ok {
			hook.OnError(ctx, event)
		}
	}
}

// Len returns the number of registered hooks.
func (r *HookRegistry[Data]) Len() int {
	return len(r.hooks)
}

// Clear removes all registered hooks.
func (r *HookRegistry[Data]) Clear() {
	r.hooks = make([]any, 0)
}

// -----------------------------------------------------------------------------
// Model Hook Registry
// -----------------------------------------------------------------------------

// ModelHookRegistry manages hooks for Model calls.
type ModelHookRegistry struct {
	hooks []any
}

// NewModelHookRegistry creates a new empty ModelHookRegistry.
func NewModelHookRegistry() *ModelHookRegistry {
	return &ModelHookRegistry{
		hooks: make([]any, 0),
	}
}

// Register adds a hook to the registry. The hook can implement any combination
// of model hook interfaces.
func (r *ModelHookRegistry) Register(hook any) *ModelHookRegistry {
	r.hooks = append(r.hooks, hook)
	return r
}

// FireBeforeGenerateContent dispatches a BeforeGenerateContentEvent to all
// registered BeforeGenerateContentHook implementations.
func (r *ModelHookRegistry) FireBeforeGenerateContent(ctx context.Context, event BeforeGenerateContentEvent) error {
	for _, h := range r.hooks {
		if hook, ok := h.(BeforeGenerateContentHook); ok {
			if err := hook.OnBeforeGenerateContent(ctx, event); err != nil {
				return err
			}
		}
	}
	return nil
}

// FireAfterGenerateContent dispatches an AfterGenerateContentEvent to all
// registered AfterGenerateContentHook implementations.
func (r *ModelHookRegistry) FireAfterGenerateContent(ctx context.Context, event AfterGenerateContentEvent) {
	for _, h := range r.hooks {
		if hook, ok := h.(AfterGenerateContentHook); ok {
			hook.OnAfterGenerateContent(ctx, event)
		}
	}
}

// Len returns the number of registered hooks.
func (r *ModelHookRegistry) Len() int {
	return len(r.hooks)
}

// Clear removes all registered hooks.
func (r *ModelHookRegistry) Clear() {
	r.hooks = make([]any, 0)
}
