package gent

import "context"

// Tool represents a single callable tool available to the agent.
type Tool interface {
	// Name returns the tool's identifier used in tool calls.
	Name() string

	// Description returns a human-readable description for the LLM.
	Description() string

	// ParameterSchema returns the JSON Schema for the tool's parameters.
	// Returns nil if the tool takes no parameters.
	ParameterSchema() map[string]any

	// Execute runs the tool with the given arguments.
	// args is the parsed arguments (map[string]any from JSON/YAML).
	// Returns the tool output as a string, or an error.
	Execute(ctx context.Context, args map[string]any) (string, error)
}

// ToolFunc is a convenience type for creating tools from functions.
type ToolFunc struct {
	name        string
	description string
	schema      map[string]any
	fn          func(ctx context.Context, args map[string]any) (string, error)
}

// NewToolFunc creates a new ToolFunc.
func NewToolFunc(
	name, description string,
	schema map[string]any,
	fn func(ctx context.Context, args map[string]any) (string, error),
) *ToolFunc {
	return &ToolFunc{
		name:        name,
		description: description,
		schema:      schema,
		fn:          fn,
	}
}

// Name returns the tool's identifier.
func (t *ToolFunc) Name() string {
	return t.name
}

// Description returns a human-readable description for the LLM.
func (t *ToolFunc) Description() string {
	return t.description
}

// ParameterSchema returns the JSON Schema for the tool's parameters.
func (t *ToolFunc) ParameterSchema() map[string]any {
	return t.schema
}

// Execute runs the tool function with the given arguments.
func (t *ToolFunc) Execute(ctx context.Context, args map[string]any) (string, error) {
	return t.fn(ctx, args)
}
