package gent

import (
	"context"
)

// Tool represents a single callable tool with typed input and output.
// The generic parameters allow for compile-time type safety when implementing tools.
//
// Responsibility design:
//   - Tool: Accept typed input, execute logic, return raw typed output and any media
//   - ToolChain: Prompt LLM about format, parse tool calls, call tools, format output for LLM
//
// Tools should focus on business logic only. Text output formatting (JSON, YAML, etc.)
// is handled by the ToolChain, allowing the same tool to work with different formats.
// Media (images, audio) is passed through by the ToolChain.
type Tool[I, TextOutput any] interface {
	// Name returns the tool's identifier used in tool calls.
	Name() string

	// Description returns a human-readable description for the LLM.
	Description() string

	// ParameterSchema returns the JSON Schema for the tool's parameters.
	// Returns nil if the tool takes no parameters.
	ParameterSchema() map[string]any

	// Call executes the tool with the given typed input.
	// Returns a ToolResult containing the typed text output and any media.
	Call(ctx context.Context, input I) (*ToolResult[TextOutput], error)
}

// ToolResult is the result of calling a Tool with typed output.
type ToolResult[TextOutput any] struct {
	// Text is the raw typed output from the tool that will be formatted as text.
	// The ToolChain formats this using its configured format (JSON, YAML, etc.).
	Text TextOutput

	// Media contains any images, audio, or other non-text content produced by the tool.
	// These are passed through by the ToolChain (possibly transformed, e.g., resized).
	// The ToolChain is not responsible for formatting media - it's passed as-is.
	Media []ContentPart
}

// ToolFunc is a convenience type for creating tools from functions with typed I/O.
type ToolFunc[I, TextOutput any] struct {
	name        string
	description string
	schema      map[string]any
	fn          func(ctx context.Context, input I) (TextOutput, error)
}

// NewToolFunc creates a new ToolFunc with typed input and output.
// Output formatting is handled by the ToolChain during execution.
func NewToolFunc[I, TextOutput any](
	name, description string,
	schema map[string]any,
	fn func(ctx context.Context, input I) (TextOutput, error),
) *ToolFunc[I, TextOutput] {
	return &ToolFunc[I, TextOutput]{
		name:        name,
		description: description,
		schema:      schema,
		fn:          fn,
	}
}

// Name returns the tool's identifier.
func (t *ToolFunc[I, TextOutput]) Name() string {
	return t.name
}

// Description returns a human-readable description for the LLM.
func (t *ToolFunc[I, TextOutput]) Description() string {
	return t.description
}

// ParameterSchema returns the JSON Schema for the tool's parameters.
func (t *ToolFunc[I, TextOutput]) ParameterSchema() map[string]any {
	return t.schema
}

// Call executes the tool function with the given typed input.
// Media is left nil for functions; use a full Tool implementation for media-producing tools.
func (t *ToolFunc[I, TextOutput]) Call(
	ctx context.Context,
	input I,
) (*ToolResult[TextOutput], error) {
	output, err := t.fn(ctx, input)
	if err != nil {
		return nil, err
	}
	return &ToolResult[TextOutput]{
		Text:  output,
		Media: nil,
	}, nil
}
