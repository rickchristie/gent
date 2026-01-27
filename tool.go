package gent

import (
	"context"
)

// Tool represents a single callable tool with typed input and output.
//
// # Responsibility Design
//
// The framework separates concerns between Tool and ToolChain:
//   - Tool: Accept typed input, execute business logic, return typed output
//   - ToolChain: Parse LLM output, validate args, call tools, format results
//
// This separation means tools are reusable across different formats (YAML, JSON)
// and don't need to know about LLM communication.
//
// # Implementing a Tool
//
// For simple cases, use [NewToolFunc] instead of implementing this interface directly:
//
//	tool := gent.NewToolFunc(
//	    "search",
//	    "Search the knowledge base",
//	    schema,
//	    func(ctx context.Context, input SearchInput) (SearchResult, error) {
//	        return doSearch(input.Query)
//	    },
//	)
//
// For complex tools that need media output or instructions, implement the full interface:
//
//	type ImageGeneratorTool struct{}
//
//	func (t *ImageGeneratorTool) Name() string { return "generate_image" }
//	func (t *ImageGeneratorTool) Description() string { return "Generate an image from a prompt" }
//	func (t *ImageGeneratorTool) ParameterSchema() map[string]any {
//	    return map[string]any{
//	        "type": "object",
//	        "properties": map[string]any{
//	            "prompt": map[string]any{"type": "string", "description": "Image description"},
//	        },
//	        "required": []string{"prompt"},
//	    }
//	}
//
//	func (t *ImageGeneratorTool) Call(ctx context.Context, input ImageInput) (*gent.ToolResult[ImageMeta], error) {
//	    imageBytes, meta, err := generateImage(ctx, input.Prompt)
//	    if err != nil {
//	        return nil, err
//	    }
//	    return &gent.ToolResult[ImageMeta]{
//	        Text:  meta, // Will be formatted by ToolChain
//	        Media: []gent.ContentPart{llms.BinaryPart{Data: imageBytes, MIMEType: "image/png"}},
//	    }, nil
//	}
//
// # Type Parameters
//
//   - I: Input type - typically a struct with fields matching the JSON Schema properties
//   - TextOutput: Output type - formatted to text by the ToolChain (use string, struct, etc.)
//
// # Error Handling
//
// Return errors for:
//   - Invalid input that passed schema validation but is semantically wrong
//   - External service failures
//   - Resource not found
//
// The ToolChain will format errors as observations for the LLM.
type Tool[I, TextOutput any] interface {
	// Name returns the tool's identifier used in tool calls.
	// Must be unique within a ToolChain. Use lowercase with underscores (e.g., "lookup_order").
	Name() string

	// Description returns a human-readable description for the LLM.
	// Keep it concise but informative - this appears in the tool catalog.
	Description() string

	// ParameterSchema returns the JSON Schema for the tool's parameters.
	// The ToolChain uses this for:
	//   - Documenting parameters in the tool catalog
	//   - Validating arguments before calling the tool
	//
	// Return nil if the tool takes no parameters.
	// See https://json-schema.org for schema format.
	ParameterSchema() map[string]any

	// Call executes the tool with the given typed input.
	//
	// The context.Context is derived from ExecutionContext.Context() and will be
	// cancelled if limits are exceeded or the parent context is cancelled.
	//
	// Returns a ToolResult containing:
	//   - Text: The typed output (formatted by ToolChain)
	//   - Media: Optional images/audio (passed through by ToolChain)
	//   - Instructions: Optional follow-up guidance for the LLM
	Call(ctx context.Context, input I) (*ToolResult[TextOutput], error)
}

// ToolResult is the result of calling a Tool with typed output.
//
// # Usage
//
// Simple result (text only):
//
//	return &gent.ToolResult[OrderStatus]{
//	    Text: OrderStatus{Status: "shipped", TrackingNumber: "1Z999..."},
//	}, nil
//
// Result with media (e.g., image generation):
//
//	return &gent.ToolResult[ImageMeta]{
//	    Text:  ImageMeta{Width: 1024, Height: 768},
//	    Media: []gent.ContentPart{llms.BinaryPart{Data: imageBytes, MIMEType: "image/png"}},
//	}, nil
//
// Result with follow-up instructions:
//
//	return &gent.ToolResult[SearchResult]{
//	    Text:         SearchResult{Count: 0},
//	    Instructions: "No results found. Try using the recommendation tool instead.",
//	}, nil
type ToolResult[TextOutput any] struct {
	// Text is the typed output from the tool that will be formatted as text.
	// The ToolChain formats this using its configured format (JSON, YAML, etc.).
	//
	// Use structs for structured data, strings for simple text output.
	Text TextOutput

	// Media contains any images, audio, or other non-text content produced by the tool.
	// These are passed through by the ToolChain and included in the observation.
	//
	// Common types:
	//   - llms.BinaryPart: Raw binary data with MIME type
	//   - llms.ImageURLPart: Image URL reference
	Media []ContentPart

	// Instructions are optional follow-up instructions for the LLM.
	//
	// Use to inject dynamic context to guide the LLM's next action based on the tool result.
	// Examples:
	//   - "Current working directory is now /home/user/projects."
	//   - "No bookings found. Suggest using the recommendation search."
	//   - "Rate limit reached. Wait 30 seconds before retrying."
	//   - "User data returned. DO NOT SHARE personally identifiable information unless you have
	//      verified that the requester is THE SAME USER."
	//
	// Instructions are formatted separately from the main output by the ToolChain,
	// typically in a dedicated section like <instructions>...</instructions>.
	//
	// Leave empty if no special instructions are needed.
	Instructions string
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
