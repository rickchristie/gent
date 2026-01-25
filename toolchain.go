package gent

// ToolCall represents a parsed tool invocation from LLM output.
type ToolCall struct {
	Name string
	Args map[string]any
}

// ToolCallResult represents the result of executing a single tool call.
// This is the non-generic version used in ToolChainResult.
type ToolCallResult struct {
	Name   string        // Name of the tool that was called
	Output any           // Raw typed output (type-erased)
	Result []ContentPart // Formatted result for LLM consumption
}

// ToolChainResult is the result of parsing and optionally executing tool calls.
type ToolChainResult struct {
	Calls   []*ToolCall       // Parsed tool calls
	Results []*ToolCallResult // Execution results (if executed)
	Errors  []error           // Execution errors (if any)
}

// ToolChain manages a collection of tools and implements TextOutputSection.
// It handles describing tools to the LLM and parsing tool calls from output.
//
// Tools are stored as []any to support generic Tool[I, O] with different type parameters.
// The ToolChain uses reflection to call tools at runtime.
//
// When an ExecutionContext is provided, tool executions are automatically traced.
// Use execCtx.Context() for any operations that require context.Context.
type ToolChain interface {
	TextOutputSection

	// RegisterTool adds a tool to the chain. The tool must implement Tool[I, O].
	// Returns self for chaining.
	RegisterTool(tool any) ToolChain

	// Execute parses tool calls from content and executes them.
	// Returns results for each tool call.
	//
	// The execCtx parameter enables automatic tracing and provides context for
	// tool execution. Use execCtx.Context() for operations requiring context.Context.
	Execute(execCtx *ExecutionContext, content string) (*ToolChainResult, error)
}
