package gent

import (
	"context"
	"encoding/json"

	"github.com/tmc/langchaingo/llms"
)

// Tool represents a single callable tool with typed input and output.
// The generic parameters allow for compile-time type safety when implementing tools.
type Tool[I, O any] interface {
	// Name returns the tool's identifier used in tool calls.
	Name() string

	// Description returns a human-readable description for the LLM.
	Description() string

	// ParameterSchema returns the JSON Schema for the tool's parameters.
	// Returns nil if the tool takes no parameters.
	ParameterSchema() map[string]any

	// Call executes the tool with the given typed input.
	// Returns a ToolResult containing the typed output and formatted ContentPart slice.
	Call(ctx context.Context, input I) (*ToolResult[O], error)
}

// ToolResult is the result of calling a Tool with typed output.
type ToolResult[O any] struct {
	Output O             // The typed output from the tool
	Result []ContentPart // Formatted result for LLM consumption
}

// ToolFunc is a convenience type for creating tools from functions with typed I/O.
type ToolFunc[I, O any] struct {
	name        string
	description string
	schema      map[string]any
	fn          func(ctx context.Context, input I) (O, error)
	formatter   func(O) []ContentPart
}

// NewToolFunc creates a new ToolFunc with typed input and output.
// The formatter converts the output to ContentPart slice for LLM consumption.
// If formatter is nil, a default JSON formatter is used.
func NewToolFunc[I, O any](
	name, description string,
	schema map[string]any,
	fn func(ctx context.Context, input I) (O, error),
	formatter func(O) []ContentPart,
) *ToolFunc[I, O] {
	if formatter == nil {
		formatter = defaultFormatter[O]
	}
	return &ToolFunc[I, O]{
		name:        name,
		description: description,
		schema:      schema,
		fn:          fn,
		formatter:   formatter,
	}
}

// defaultFormatter converts output to JSON and wraps in TextContent.
func defaultFormatter[O any](output O) []ContentPart {
	data, err := json.Marshal(output)
	if err != nil {
		return []ContentPart{llms.TextContent{Text: "error: failed to marshal output"}}
	}
	return []ContentPart{llms.TextContent{Text: string(data)}}
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
func (t *ToolFunc[I, O]) Call(ctx context.Context, input I) (*ToolResult[O], error) {
	output, err := t.fn(ctx, input)
	if err != nil {
		return nil, err
	}
	return &ToolResult[O]{
		Output: output,
		Result: t.formatter(output),
	}, nil
}
