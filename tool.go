package gent

import (
	"context"
)

// Tool represents a single callable tool with typed input and output.
// The generic parameters allow for compile-time type safety when implementing tools.
//
// Responsibility design:
//   - Tool: Accept typed input, execute logic, return raw typed output
//   - ToolChain: Prompt LLM about format, parse tool calls, call tools, format output for LLM
//
// Tools should focus on business logic only. Output formatting (JSON, YAML, etc.)
// is handled by the ToolChain, allowing the same tool to work with different formats.
type Tool[I, O any] interface {
	// Name returns the tool's identifier used in tool calls.
	Name() string

	// Description returns a human-readable description for the LLM.
	Description() string

	// ParameterSchema returns the JSON Schema for the tool's parameters.
	// Returns nil if the tool takes no parameters.
	ParameterSchema() map[string]any

	// Call executes the tool with the given typed input.
	// Returns a ToolResult containing the typed output.
	Call(ctx context.Context, input I) (*ToolResult[O], error)
}

// ToolResult is the result of calling a Tool with typed output.
type ToolResult[O any] struct {
	// Output is the raw typed output from the tool.
	Output O

	// Result is the formatted output for LLM consumption.
	// This field is set by the ToolChain after calling the tool, not by the tool itself.
	// Tools should leave this nil; the ToolChain handles formatting based on its format
	// (e.g., JSON toolchain formats as JSON, YAML toolchain formats as YAML).
	Result []ContentPart
}

// ToolFunc is a convenience type for creating tools from functions with typed I/O.
type ToolFunc[I, O any] struct {
	name        string
	description string
	schema      map[string]any
	fn          func(ctx context.Context, input I) (O, error)
}

// NewToolFunc creates a new ToolFunc with typed input and output.
// Output formatting is handled by the ToolChain during execution.
func NewToolFunc[I, O any](
	name, description string,
	schema map[string]any,
	fn func(ctx context.Context, input I) (O, error),
) *ToolFunc[I, O] {
	return &ToolFunc[I, O]{
		name:        name,
		description: description,
		schema:      schema,
		fn:          fn,
	}
}

// Name returns the tool's identifier.
func (t *ToolFunc[I, O]) Name() string {
	return t.name
}

// Description returns a human-readable description for the LLM.
func (t *ToolFunc[I, O]) Description() string {
	return t.description
}

// ParameterSchema returns the JSON Schema for the tool's parameters.
func (t *ToolFunc[I, O]) ParameterSchema() map[string]any {
	return t.schema
}

// Call executes the tool function with the given typed input.
// The Result field is left nil as formatting is handled by the ToolChain.
func (t *ToolFunc[I, O]) Call(ctx context.Context, input I) (*ToolResult[O], error) {
	output, err := t.fn(ctx, input)
	if err != nil {
		return nil, err
	}
	return &ToolResult[O]{
		Output: output,
		Result: nil,
	}, nil
}
