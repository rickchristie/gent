// Package hooks provides a registry for managing execution lifecycle hooks.
//
// Hooks allow you to observe and intercept events during agent execution.
// Each hook interface corresponds to a specific event type - implement only
// the interfaces you need.
//
// # Hook Interfaces
//
// Executor lifecycle hooks:
//   - [gent.BeforeExecutionHook] - Called once before first iteration
//   - [gent.AfterExecutionHook] - Called once after execution ends
//   - [gent.BeforeIterationHook] - Called before each iteration
//   - [gent.AfterIterationHook] - Called after each iteration
//   - [gent.ErrorHook] - Called when errors occur
//
// Model call hooks:
//   - [gent.BeforeModelCallHook] - Called before each LLM API call
//   - [gent.AfterModelCallHook] - Called after each LLM API call
//
// Tool call hooks:
//   - [gent.BeforeToolCallHook] - Called before each tool execution (can modify args)
//   - [gent.AfterToolCallHook] - Called after each tool execution
//
// # Creating a Hook
//
// Create a hook by implementing any combination of interfaces:
//
//	type MetricsHook struct{}
//
//	func (h *MetricsHook) OnAfterToolCall(
//	    ctx context.Context,
//	    execCtx *gent.ExecutionContext,
//	    event gent.AfterToolCallEvent,
//	) {
//	    metrics.RecordToolCall(event.ToolName, event.Duration)
//	}
//
//	// Compile-time check
//	var _ gent.AfterToolCallHook = (*MetricsHook)(nil)
//
// # Registering Hooks
//
// There are two ways to register hooks with an executor:
//
// Option 1: Register directly on the executor (simple cases):
//
//	exec := executor.New(loop, config).
//	    RegisterHook(&LoggerHook{}).
//	    RegisterHook(&MetricsHook{})
//
// Option 2: Use a shared registry (when sharing across executors):
//
//	registry := hooks.NewRegistry()
//	registry.Register(&SharedHook{})
//
//	exec1 := executor.New(loop1, config).WithHooks(registry)
//	exec2 := executor.New(loop2, config).WithHooks(registry)
//
// The key difference:
//   - RegisterHook adds to the executor's existing registry
//   - WithHooks replaces the entire registry (useful for sharing)
//
// # Example
//
// See integrationtest/loggers/logger.go for a complete example that implements
// all hook interfaces.
package hooks
