package gent

import "github.com/rickchristie/gent/schema"

// ToolCallResult represents the complete result of a single tool call,
// including both raw typed data and formatted output for the LLM.
//
// The ToolChain populates all fields during ExecuteNext(). Agent loops use
// the formatted fields (Text, Media) for building LLM messages, and
// the raw fields (Input, Output) for programmatic access such as
// deduplication.
type ToolCallResult struct {
	// Outcome is set for recovery results: unknown or not_started. Empty means a normal result.
	Outcome    string
	ToolCallId string
	AttemptId  string
	// DiscoveredTools lists tools whose full definitions this result made available.
	// SearchGet sets this only after get_tool_schema succeeds. Native requests use these names
	// from active history so compaction and expiry also bound the active tool catalog.
	DiscoveredTools      []string
	Instructions         string
	DeduplicationSummary string
	SummaryCaptured      bool
	// Name of the tool that was called.
	Name string

	// Args are the raw parsed arguments from the LLM output.
	// Args are processed to [ToolCallResult.Input].
	Args map[string]any

	// Hash is a deterministic key derived from Name + canonical Args.
	// Used by ScratchpadToMessages to identify duplicate calls across
	// iterations. Computed by the ToolChain during ExecuteNext().
	Hash string

	// Input is the typed tool input (type-erased) after schema
	// validation and arg transformation. This is passed to
	// Tool.DeduplicateSummary for generating abbreviated text.
	Input any

	// Output is the typed tool output (type-erased) from Tool.Call.
	// An interrupted program keeps its partial results even when Error is non-nil.
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
// preserving input order, including when calls execute concurrently.
type ToolChainResult struct {
	Results []*ToolCallResult
}

// ToolChain selects and executes native tool calls. Implementations can embed toolchain.Registry
// and override NativeTools to control visibility while retaining validation, hooks, and recovery.
// RegisterCodecs and RecoveryConfig describe saved state and binding compatibility.
type ToolChain interface {
	RegisterTool(any, ...ToolOption) ToolChain
	NativeTools(*ExecutionContext) ([]ToolDefinition, error)
	PrepareCalls(*ExecutionContext, []ModelToolCall) (*ToolPlan, error)
	ExecuteNext(*ExecutionContext, *ToolPlan, TextFormat) (*ToolCallResult, error)
	GetToolSchema(string) *schema.Schema
	DeduplicateSummary(*ToolCallResult) string
	RecoveryMessage(*RecoveryInfo) string
	RegisterCodecs(*CodecRegistry) error
	RecoveryConfig() any
}

// ParallelToolChain declares whether ExecuteNext can run concurrently with independent plans
// in the same context. Each plan must have a distinct checkpoint key. The JavaScript wrapper
// always calls ExecuteNext on the supplied chain, including overrides on an embedding type.
type ParallelToolChain interface {
	ToolChain
	SupportsParallelCalls() bool
}
