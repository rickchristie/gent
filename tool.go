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
//   - ToolChain: Decode native arguments, validate inputs, call tools, format results
//
// This separation keeps business tools independent of the model provider and its wire format.
//
// # Implementing a Tool
//
// Use [DefineTool] for function tools. It infers parameters from Go types and accepts field
// descriptions, constraints, search metadata, and a recovery handler before Build.
//
// For complex tools that need media output or instructions, implement the full interface:
//
//	type ImageGeneratorTool struct{}
//
//	func (t *ImageGeneratorTool) Name() string { return "generate_image" }
//	func (t *ImageGeneratorTool) Description() string { return "Generate an image from a prompt" }
//	func (t *ImageGeneratorTool) Policy() string { return "" }
//	func (t *ImageGeneratorTool) Domain() string { return "Images" }
//	func (t *ImageGeneratorTool) Categories() []string { return nil }
//	func (t *ImageGeneratorTool) Keywords() []string { return nil }
//	func (t *ImageGeneratorTool) SyntheticQueries() []string { return nil }
//	func (t *ImageGeneratorTool) Strict() bool { return false }
//	func (t *ImageGeneratorTool) OutputSchema() map[string]any { return nil }
//	func (t *ImageGeneratorTool) Recover(context.Context, *gent.ToolRecoveryRequest) (
//	    *gent.ToolRecoveryResponse, error,
//	) {
//	    return &gent.ToolRecoveryResponse{Result: gent.ToolRecoveryUnresolved}, nil
//	}
//	func (t *ImageGeneratorTool) DeduplicateSummary(ImageInput, ImageMeta) string { return "" }
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
//	        Media: []gent.ContentPart{gent.BinaryContent{Data: imageBytes, MIMEType: "image/png"}},
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
	// Search metadata is part of every tool. Empty values are valid.
	IndexableTool

	// Name returns the tool's identifier used in tool calls.
	// Must be unique within a ToolChain. Use lowercase with underscores (e.g., "lookup_order").
	Name() string

	// Description returns a human-readable description for the LLM.
	// This appears in the tool catalog. Keep it concise 1-2 sentences, state what the tool does
	// and its purpose.
	Description() string

	// Policy returns optional usage policy instructions for the LLM.
	// This provides dynamic steering on HOW the LLM should use the tool, separate from
	// the tool's description (which describes WHAT the tool does).
	//
	// Examples:
	//   - Privacy guardrails: "Only search using the exact email/phone from the current user"
	//   - Rate limiting hints: "Prefer batching multiple IDs in one call"
	//   - Safety rules: "Never use this tool without explicit user confirmation"
	//
	// When non-empty, the ToolChain includes this in the tool catalog prompt.
	// Return empty string if no policy is needed.
	Policy() string

	// Recover checks an interrupted operation when no saved result exists. Return Completed
	// with its output, RetryAllowed only when another attempt is safe, or Unresolved.
	// Recovery may run again after a process failure, so the check must be safe to repeat.
	Recover(context.Context, *ToolRecoveryRequest) (*ToolRecoveryResponse, error)

	// Strict requests provider-side schema enforcement. Local validation always applies.
	Strict() bool

	// OutputSchema describes the text output. Nil omits output documentation.
	// The tool builder infers it from the output type unless explicitly set.
	OutputSchema() map[string]any

	// ParameterSchema returns the JSON Schema for the tool's parameters.
	// The ToolChain uses this for:
	//   - Documenting parameters in the tool catalog
	//   - Validating arguments before calling the tool
	//
	// Nil omits schema constraints. The chain still requires a JSON object.
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

	// DeduplicateSummary returns an abbreviated observation text
	// for this tool call when the same call (same name + args)
	// appears in an earlier iteration's scratchpad.
	//
	// Return a non-empty string to enable deduplication. The
	// string replaces the full tool output in earlier iterations
	// to reduce token usage. The most recent occurrence always
	// keeps its full text.
	//
	// Return "" to disable deduplication for this tool. Most
	// tools should return "" — only stateless tools where the
	// same input always produces the same output benefit from
	// deduplication.
	//
	// Example for a search tool:
	//
	//	func (t *SearchTool) DeduplicateSummary(
	//	    input SearchInput, output SearchResult,
	//	) string {
	//	    return fmt.Sprintf(
	//	        "Searched for %q. Found %d results. "+
	//	            "Full results available below.",
	//	        input.Query, len(output.Items),
	//	    )
	//	}
	DeduplicateSummary(input I, output TextOutput) string
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
//	    Media: []gent.ContentPart{gent.BinaryContent{Data: imageBytes, MIMEType: "image/png"}},
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
	// The ToolChain preserves strings and encodes structured output as JSON.
	//
	// Use structs for structured data, strings for simple text output.
	Text TextOutput

	// Media contains any images, audio, or other non-text content produced by the tool.
	// These are passed through by the ToolChain and included in the observation.
	//
	// Common types:
	//   - BinaryContent: Raw binary data with MIME type
	//   - ImageURLContent: Image URL reference
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
