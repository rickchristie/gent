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
	Result []ContentPart // Formatted result for LLM consumption (set by ToolChain)
}

// ToolChainResult is the result of parsing and optionally executing tool calls.
type ToolChainResult struct {
	Calls   []*ToolCall       // Parsed tool calls
	Results []*ToolCallResult // Execution results (if executed)
	Errors  []error           // Execution errors (if any)
}

// ToolChain manages a collection of tools and implements TextOutputSection.
//
// Responsibilities:
//   - Prompt: Brief instruction for the action section (inherited from TextOutputSection)
//   - AvailableToolsPrompt: Full tool catalog with format instructions and schemas
//   - Parse: Extract tool calls from LLM output
//   - Execute: Call tools with parsed arguments
//   - Format: Convert tool outputs to the appropriate format (JSON, YAML, etc.) for the LLM
//
// The ToolChain handles output formatting so tools can focus on business logic.
// Different ToolChain implementations (JSON, YAML) use the same tools but format
// outputs differently based on their format.
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

	// AvailableToolsPrompt returns the tool catalog with parameter schemas for each
	// registered tool. This should be placed in the system prompt to inform the LLM
	// about available tools.
	//
	// Note: Format instructions for how to call tools are provided by Guidance(),
	// which is inherited from TextOutputSection.
	AvailableToolsPrompt() string

	// Execute parses tool calls from content and executes them.
	// Returns results for each tool call.
	//
	// The execCtx parameter enables automatic tracing and provides context for
	// tool execution. Use execCtx.Context() for operations requiring context.Context.
	Execute(execCtx *ExecutionContext, content string) (*ToolChainResult, error)
}
