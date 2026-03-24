package gent

import (
	"strings"

	"github.com/rickchristie/gent/schema"
	"github.com/tmc/langchaingo/llms"
)

// ToolCall represents a parsed tool invocation from LLM output.
type ToolCall struct {
	Name string
	Args map[string]any
}

// ToolCallResult represents the complete result of a single tool call,
// including both raw typed data and formatted output for the LLM.
//
// The ToolChain populates all fields during Execute(). Agent loops use
// the formatted fields (Text, Media) for building LLM messages, and
// the raw fields (Input, Output) for programmatic access such as
// deduplication.
type ToolCallResult struct {
	// Name of the tool that was called.
	Name string

	// Args are the raw parsed arguments from the LLM output.
	// Args are processed to [ToolCallResult.Input].
	Args map[string]any

	// Hash is a deterministic key derived from Name + canonical Args.
	// Used by ScratchpadToMessages to identify duplicate calls across
	// iterations. Computed by the ToolChain during Execute().
	Hash string

	// Input is the typed tool input (type-erased) after schema
	// validation and arg transformation. This is passed to
	// Tool.DeduplicateSummary for generating abbreviated text.
	Input any

	// Output is the typed tool output (type-erased) from Tool.Call.
	// Nil when Error is non-nil.
	Output any

	// Error is the execution error, nil on success. Covers schema
	// validation errors, arg transformation errors, and Tool.Call
	// errors.
	Error error

	// Text is the formatted output text for this single tool call,
	// ready to be placed in an observation section. For successful
	// calls this is the formatted tool output; for errors this is
	// the formatted error message.
	Text string

	// Media contains images, audio, or other non-text content
	// produced by this tool call. Nil for most tools.
	Media []ContentPart
}

// ToolChainResult is the result of parsing and executing tool calls.
// Each element in Results corresponds to one parsed tool call,
// preserving execution order.
//
// Use [ToolChainResult.ToIteration] to convert into an [Iteration]
// suitable for the agent loop scratchpad.
type ToolChainResult struct {
	Results []*ToolCallResult
}

// ToIteration builds an [Iteration] containing a single Human-role
// observation message. The observation text is constructed by merging
// each result's Text, wrapped using the provided [TextFormat].
//
// The resulting [Iteration] stores the ToolChainResult as metadata
// on the observation [MessageContent] under [MMKToolChainResult].
// This metadata is used by ScratchpadToMessages to deduplicate
// repeated stateless tool calls.
func (r *ToolChainResult) ToIteration(
	format TextFormat,
) *Iteration {
	var sections []string
	var allMedia []ContentPart
	for _, result := range r.Results {
		if result.Text != "" {
			sections = append(sections, result.Text)
		}
		allMedia = append(allMedia, result.Media...)
	}

	observation := ""
	if len(sections) > 0 {
		observation = format.FormatSections([]FormattedSection{
			{
				Name:    "observation",
				Content: strings.Join(sections, "\n"),
			},
		})
	}

	var parts []ContentPart
	if observation != "" {
		parts = append(
			parts, llms.TextContent{Text: observation},
		)
	}
	parts = append(parts, allMedia...)

	msg := &MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: parts,
	}
	msg.SetMetadata(MMKToolChainResult, r)

	return &Iteration{
		Messages: []*MessageContent{msg},
	}
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
	// The tool must implement [Tool] as Tool[I, TextOutput] for some types I and TextOutput.
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

	// GetToolSchema returns the compiled schema for the named tool,
	// or nil if the tool has no schema or doesn't exist.
	// Used by wrappers (e.g., JsToolChainWrapper) for schema
	// validation and error reporting.
	GetToolSchema(name string) *schema.Schema

	// Execute parses tool calls from content and executes them.
	//
	// The textFormat parameter is used to format each result's Text field.
	// It must not be nil.
	//
	// Each [ToolCallResult] in the returned [ToolChainResult] contains
	// the per-call formatted Text, Media, and raw Input/Output. The
	// caller is responsible for merging and wrapping results (typically
	// via [ToolChainResult.ToIteration]).
	//
	// The execCtx parameter enables automatic tracing and provides
	// context for tool execution. Use execCtx.Context() for operations
	// requiring context.Context.
	//
	// Panics if textFormat is nil.
	Execute(
		execCtx *ExecutionContext,
		content string,
		textFormat TextFormat,
	) (*ToolChainResult, error)

	// DeduplicateSummary returns an abbreviated observation text for
	// a tool call result whose output duplicates an earlier call.
	//
	// The method looks up the tool by result.Name and calls the tool's
	// DeduplicateSummary method with the typed Input and Output. If the
	// tool is not found or returns an empty string, the result should
	// not be deduplicated.
	//
	// This is called by ScratchpadToMessages when building the LLM
	// message list to replace earlier duplicate observations with
	// concise summaries.
	DeduplicateSummary(result *ToolCallResult) string
}
