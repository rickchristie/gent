package gent

import "context"

// ToolCall represents a parsed tool invocation from LLM output.
type ToolCall struct {
	Name string
	Args map[string]any
}

// ToolChainResult is the result of parsing and optionally executing tool calls.
type ToolChainResult struct {
	Calls   []ToolCall // Parsed tool calls
	Results []string   // Execution results (if executed)
	Errors  []error    // Execution errors (if any)
}

// ToolChain manages a collection of tools and implements TextOutputSection.
// It handles describing tools to the LLM and parsing tool calls from output.
type ToolChain interface {
	TextOutputSection

	// Tools returns all registered tools.
	Tools() []Tool

	// RegisterTool adds a tool to the chain. Returns self for chaining.
	RegisterTool(tool Tool) ToolChain

	// Execute parses tool calls from content and executes them.
	// Returns results for each tool call.
	Execute(ctx context.Context, content string) (*ToolChainResult, error)
}
