package hooks

import (
	"github.com/rickchristie/gent"
)

// Registry manages a collection of hooks and dispatches events to them.
//
// # Overview
//
// Registry is the central coordination point for hooks. It:
//   - Stores registered hooks in order
//   - Dispatches events to hooks that implement the relevant interface
//   - Passes ExecutionContext to hooks for access to stats, data,
//     and tracing
//
// Hooks can implement any combination of hook interfaces - they only
// receive events for the interfaces they implement.
//
// # Creating and Using
//
//	// Create a registry and register hooks
//	registry := hooks.NewRegistry()
//	registry.Register(&LoggingHook{})
//	registry.Register(&MetricsHook{})
//
//	// Use with executor
//	exec := executor.New(loop, config).WithHooks(registry)
//
// # Hooks with Multiple Interfaces
//
// A single hook can implement multiple interfaces:
//
//	type FullHook struct {
//	    logger *log.Logger
//	}
//
//	func (h *FullHook) OnBeforeExecution(
//	    execCtx *gent.ExecutionContext,
//	    e *gent.BeforeExecutionEvent,
//	) {
//	    h.logger.Print("Execution started")
//	}
//
//	func (h *FullHook) OnAfterToolCall(
//	    execCtx *gent.ExecutionContext,
//	    e *gent.AfterToolCallEvent,
//	) {
//	    h.logger.Printf("Tool %s: %v", e.ToolName, e.Duration)
//	}
//
//	// Register once - receives both event types
//	registry.Register(&FullHook{logger: log.Default()})
//
// # Accessing ExecutionContext
//
// Hooks receive the ExecutionContext which provides:
//   - execCtx.Data() - Access to the agent's LoopData
//   - execCtx.Stats() - Read/write stats counters and gauges
//   - execCtx.GetTraces() - Access recorded traces
//   - execCtx.Context() - The underlying context.Context
//
// Example:
//
//	func (h *MyHook) OnAfterIteration(
//	    execCtx *gent.ExecutionContext,
//	    e *gent.AfterIterationEvent,
//	) {
//	    iterations := execCtx.Stats().GetIterations()
//	    h.logger.Printf("Completed iteration %d", iterations)
//	}
//
// # Thread Safety
//
// Registry is NOT thread-safe. Register all hooks before starting
// execution. Fire methods should only be called by the executor.
type Registry struct {
	hooks []any
}

// NewRegistry creates a new empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		hooks: make([]any, 0),
	}
}

// Register adds a hook to the registry. The hook can implement any
// combination of hook interfaces (BeforeExecutionHook,
// AfterExecutionHook, etc.).
//
// Hooks are called in the order they are registered.
func (r *Registry) Register(hook any) *Registry {
	r.hooks = append(r.hooks, hook)
	return r
}

// FireBeforeExecution dispatches a BeforeExecutionEvent to all
// registered BeforeExecutionHook implementations.
func (r *Registry) FireBeforeExecution(
	execCtx *gent.ExecutionContext,
	event *gent.BeforeExecutionEvent,
) {
	for _, h := range r.hooks {
		if hook, ok := h.(gent.BeforeExecutionHook); ok {
			hook.OnBeforeExecution(execCtx, event)
		}
	}
}

// FireAfterExecution dispatches an AfterExecutionEvent to all
// registered AfterExecutionHook implementations.
func (r *Registry) FireAfterExecution(
	execCtx *gent.ExecutionContext,
	event *gent.AfterExecutionEvent,
) {
	for _, h := range r.hooks {
		if hook, ok := h.(gent.AfterExecutionHook); ok {
			hook.OnAfterExecution(execCtx, event)
		}
	}
}

// FireBeforeIteration dispatches a BeforeIterationEvent to all
// registered BeforeIterationHook implementations.
func (r *Registry) FireBeforeIteration(
	execCtx *gent.ExecutionContext,
	event *gent.BeforeIterationEvent,
) {
	for _, h := range r.hooks {
		if hook, ok := h.(gent.BeforeIterationHook); ok {
			hook.OnBeforeIteration(execCtx, event)
		}
	}
}

// FireAfterIteration dispatches an AfterIterationEvent to all
// registered AfterIterationHook implementations.
func (r *Registry) FireAfterIteration(
	execCtx *gent.ExecutionContext,
	event *gent.AfterIterationEvent,
) {
	for _, h := range r.hooks {
		if hook, ok := h.(gent.AfterIterationHook); ok {
			hook.OnAfterIteration(execCtx, event)
		}
	}
}

// FireError dispatches an ErrorEvent to all registered ErrorHook
// implementations.
func (r *Registry) FireError(
	execCtx *gent.ExecutionContext,
	event *gent.ErrorEvent,
) {
	for _, h := range r.hooks {
		if hook, ok := h.(gent.ErrorHook); ok {
			hook.OnError(execCtx, event)
		}
	}
}

// FireBeforeModelCall dispatches a BeforeModelCallEvent to all
// registered BeforeModelCallHook implementations.
// Hooks can modify event.Request for ephemeral context injection.
func (r *Registry) FireBeforeModelCall(
	execCtx *gent.ExecutionContext,
	event *gent.BeforeModelCallEvent,
) {
	for _, h := range r.hooks {
		if hook, ok := h.(gent.BeforeModelCallHook); ok {
			hook.OnBeforeModelCall(execCtx, event)
		}
	}
}

// FireAfterModelCall dispatches an AfterModelCallEvent to all
// registered AfterModelCallHook implementations.
func (r *Registry) FireAfterModelCall(
	execCtx *gent.ExecutionContext,
	event *gent.AfterModelCallEvent,
) {
	for _, h := range r.hooks {
		if hook, ok := h.(gent.AfterModelCallHook); ok {
			hook.OnAfterModelCall(execCtx, event)
		}
	}
}

// FireBeforeToolCall dispatches a BeforeToolCallEvent to all
// registered BeforeToolCallHook implementations.
// Hooks can modify event.Args to change the tool input.
func (r *Registry) FireBeforeToolCall(
	execCtx *gent.ExecutionContext,
	event *gent.BeforeToolCallEvent,
) {
	for _, h := range r.hooks {
		if hook, ok := h.(gent.BeforeToolCallHook); ok {
			hook.OnBeforeToolCall(execCtx, event)
		}
	}
}

// FireAfterToolCall dispatches an AfterToolCallEvent to all registered
// AfterToolCallHook implementations.
func (r *Registry) FireAfterToolCall(
	execCtx *gent.ExecutionContext,
	event *gent.AfterToolCallEvent,
) {
	for _, h := range r.hooks {
		if hook, ok := h.(gent.AfterToolCallHook); ok {
			hook.OnAfterToolCall(execCtx, event)
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
