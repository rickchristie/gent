package gent

import "github.com/tmc/langchaingo/llms"

// ToolCall represents a parsed tool invocation from LLM output.
type ToolCall struct {
	Name string
	Args map[string]any
}

// RawToolCallResult represents the raw result of executing a single tool call.
// This is the non-generic version used in RawToolChainResult for programmatic access.
type RawToolCallResult struct {
	Name   string // Name of the tool that was called
	Output any    // Raw typed output (type-erased)
}

// RawToolChainResult contains the raw results of tool execution for programmatic access.
// This preserves the original outputs before formatting.
type RawToolChainResult struct {
	Calls   []*ToolCall          // Parsed tool calls
	Results []*RawToolCallResult // Raw execution results (if executed)
	Errors  []error              // Execution errors (if any)
}

// ToolChainResult is the result of parsing and executing tool calls.
// It provides both formatted output (Text, Media) ready for the LLM,
// and raw results (Raw) for programmatic access.
type ToolChainResult struct {
	// Text is the fully formatted observation text with section separators and wrapper.
	// This is ready to use directly - no additional formatting needed.
	// For XML format, this includes <observation>...</observation> tags.
	Text string

	// Media contains any images, audio, or other non-text content from tool results.
	// These are collected from all tool results and passed through (or transformed).
	Media []ContentPart

	// Raw contains the original tool call results for programmatic access.
	// Use this when you need to inspect individual tool outputs or errors.
	Raw *RawToolChainResult
}

// AsContentParts returns the result as a slice of ContentParts for building LLM messages.
// The text is returned first, followed by any media.
func (r *ToolChainResult) AsContentParts() []ContentPart {
	parts := []ContentPart{llms.TextContent{Text: r.Text}}
	return append(parts, r.Media...)
}

// ToolChain manages a collection of tools and implements TextSection.
//
// Responsibilities:
//   - Guidance: Brief instruction for the action section (inherited from TextSection)
//   - AvailableToolsPrompt: Full tool catalog with format instructions and schemas
//   - Parse: Extract tool calls from LLM output
//   - Execute: Call tools with parsed arguments, format results using TextFormat
//
// The ToolChain handles output formatting so tools can focus on business logic.
// Different ToolChain implementations (JSON, YAML) use the same tools but format
// outputs differently based on their serialization format.
//
// Tools are stored as []any to support generic Tool[I, TextOutput] with different
// type parameters. The ToolChain uses reflection to call tools at runtime.
//
// When an ExecutionContext is provided, tool executions are automatically traced.
// Use execCtx.Context() for any operations that require context.Context.
type ToolChain interface {
	TextSection

	// RegisterTool adds a tool to the chain. The tool must implement Tool[I, TextOutput].
	// Returns self for chaining.
	RegisterTool(tool any) ToolChain

	// AvailableToolsPrompt returns the tool catalog with parameter schemas for each
	// registered tool. This should be placed in the system prompt to inform the LLM
	// about available tools.
	//
	// Note: Format instructions for how to call tools are provided by Guidance(),
	// which is inherited from TextSection.
	AvailableToolsPrompt() string

	// Execute parses tool calls from content and executes them.
	// The textFormat parameter is used to format the results - it must not be nil.
	//
	// Returns a ToolChainResult with:
	//   - Text: Fully formatted observation text with section separators and wrapper
	//   - Media: Any images/audio from tool results
	//   - Raw: Original results for programmatic access
	//
	// The execCtx parameter enables automatic tracing and provides context for
	// tool execution. Use execCtx.Context() for operations requiring context.Context.
	//
	// Panics if textFormat is nil.
	Execute(execCtx *ExecutionContext, content string, textFormat TextFormat) (*ToolChainResult, error)
}
