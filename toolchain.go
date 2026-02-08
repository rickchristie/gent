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

// ToolChain manages a collection of tools and implements [TextSection].
//
// # Responsibilities
//
//   - Guidance: Brief instruction for the action section (inherited from TextSection)
//   - AvailableToolsPrompt: Full tool catalog with format instructions and schemas
//   - Execute: Parse tool calls, validate args, call tools, format results
//
// # Implementing a ToolChain
//
// To create a custom ToolChain (e.g., for a new serialization format):
//
//  1. Implement [TextSection]: Name(), Guidance(), ParseSection()
//  2. Implement RegisterTool to store tools (use reflection for generic tools)
//  3. Implement AvailableToolsPrompt to generate the tool catalog
//  4. Implement Execute with proper tracing and error handling
//
// # Event Publishing Requirements
//
// Execute MUST publish tool call events for stats tracking and subscriber notification:
//
//	// For each tool call:
//	beforeEvent := execCtx.PublishBeforeToolCall(toolName, input)
//	input = beforeEvent.Args // subscribers can modify args
//
//	startTime := time.Now()
//	output, err := tool.Call(execCtx.Context(), input)
//
//	execCtx.PublishAfterToolCall(toolName, input, output, time.Since(startTime), err)
//
// This enables automatic stat updates: [SCToolCalls], [SCToolCallsFor],
// [SCToolCallsErrorTotal], [SGToolCallsErrorConsecutive], etc.
//
// # Parse Error Handling
//
// Execute MUST publish parse errors:
//
//	if parseErr != nil {
//	    execCtx.PublishParseError("toolchain", content, parseErr)
//	}
//
// On successful parse, reset the consecutive error gauge:
//
//	execCtx.Stats().ResetGauge(SGToolchainParseErrorConsecutive)
//
// # Available Implementations
//
//   - toolchain.NewYAML(): YAML-based tool calls (recommended for readability)
//   - toolchain.NewJSON(): JSON-based tool calls
//
// Tools are stored as []any to support generic Tool[I, TextOutput] with different
// type parameters. The ToolChain uses reflection to call tools at runtime.
type ToolChain interface {
	TextSection

	// RegisterTool adds a tool to the chain.
	//
	// The tool must implement Tool[I, TextOutput] for some types I and TextOutput.
	// The ToolChain uses reflection to discover and call the tool's methods.
	//
	// Panics if:
	//   - tool is nil
	//   - tool doesn't implement the Tool interface
	//   - a tool with the same name is already registered
	//
	// Returns self for method chaining.
	RegisterTool(tool any) ToolChain

	// AvailableToolsPrompt returns the tool catalog with parameter schemas.
	//
	// This should be placed in the system prompt to inform the LLM about available
	// tools. The format depends on the implementation (YAML schema, JSON schema, etc.).
	//
	// Note: Format instructions for HOW to call tools (e.g., "use YAML format")
	// are provided by Guidance(), which is inherited from TextSection.
	AvailableToolsPrompt() string

	// Execute parses tool calls from content and executes them.
	//
	// The textFormat parameter is used to format the results - it must not be nil.
	//
	// Returns a ToolChainResult with:
	//   - Text: Formatted sections using textFormat.FormatSections, NOT wrapped
	//   - Media: Any images/audio from tool results
	//   - Raw: Original results for programmatic access
	//
	// The Text field contains formatted sections but is not wrapped in an observation
	// section. Callers who want the result wrapped should use:
	//
	//	textFormat.FormatSections([]FormattedSection{{Name: "observation", Content: result.Text}})
	//
	// The execCtx parameter enables automatic tracing and provides context for
	// tool execution. Use execCtx.Context() for operations requiring context.Context.
	//
	// Panics if textFormat is nil.
	Execute(execCtx *ExecutionContext, content string, textFormat TextFormat) (*ToolChainResult, error)
}
