package gent

import (
	"context"
	"time"
)

// Model is a provider adapter. The framework owns request capture, IDs, statistics and streaming.
// Implementations honor cancellation and complete Response only after their producer has exited.
type Model interface {
	GenerateContentStream(context.Context, ModelCallRequest) (Stream, error)
}

// ModelCall identifies a logical call independently from its physical worker attempt.
// Id is optional. An empty Id uses call order after the last checkpoint for recovery.
// Supply a stable, unique Id when calls can run concurrently or in a different order on recovery.
// StreamId and StreamTopicId group display output; they do not identify a logical call.
type ModelCall struct {
	Id            string
	StreamId      string
	StreamTopicId string
	Source        string
	Model         string
	Provider      string
}

// NewModelCallRequest resolves option functions exactly once, before mutable request hooks.
func NewModelCallRequest(
	messages []*ModelMessage,
	options ...CallOption,
) ModelCallRequest {
	var resolved CallOptions
	WithStreamThinking(true)(&resolved)
	for _, option := range options {
		if option != nil {
			option(&resolved)
		}
	}
	return ModelCallRequest{Messages: messages, Options: resolved, OptionCaptureComplete: true}
}

// ContentResponse is a provider response. It does not establish execution success.
type ContentResponse struct {
	// Choices contains the generated content choices.
	Choices []*ContentChoice

	// Info contains generation metadata including normalized token counts.
	Info *GenerationInfo
}

// ContentChoice is a single content choice from the model.
type ContentChoice struct {
	Continuation *ProviderContinuation

	// CandidateContent is provider content to consider after tools and completion checks.
	// It is not an accepted execution answer. Stream it as intermediary metadata.
	CandidateContent []ContentPart
	CandidateSource  *ContentSource

	// MetaContent preserves ordered provider-visible metadata. Unknown types remain readable.
	MetaContent []*MetaContent
	Completion  ModelCompletion
	ToolCalls   []ModelToolCall
}

// GenerationInfo contains metadata about the generation including normalized token counts.
type GenerationInfo struct {
	// InputTokens is the number of input/prompt tokens used.
	// This is normalized across providers:
	//   - OpenAI: PromptTokens
	//   - Anthropic: InputTokens
	//   - Google: input_tokens / PromptTokens
	//   - Ollama: PromptTokens
	//   - Bedrock: input_tokens
	InputTokens int

	// OutputTokens is the number of output/completion tokens generated.
	// This is normalized across providers:
	//   - OpenAI: CompletionTokens
	//   - Anthropic: OutputTokens
	//   - Google: output_tokens / CompletionTokens
	//   - Ollama: CompletionTokens
	//   - Bedrock: output_tokens
	OutputTokens int

	// TotalTokens is the total token count (InputTokens + OutputTokens).
	// Some providers return this directly; otherwise it's computed.
	TotalTokens int

	// CachedInputTokens is the number of input tokens served from cache.
	// This is normalized across providers:
	//   - OpenAI: PromptCachedTokens
	//   - Anthropic: CacheReadInputTokens
	//   - Google: CachedTokens / CacheReadInputTokens
	CachedInputTokens int

	// ReasoningTokens is the number of tokens used for reasoning/thinking.
	// This is normalized across providers:
	//   - OpenAI: ReasoningTokens / CompletionReasoningTokens
	//   - Anthropic: (extracted from ThinkingTokens if available)
	ReasoningTokens int

	// RawGenerationInfo contains the original provider-specific GenerationInfo map.
	// Use this to access provider-specific fields not covered by the normalized fields.
	RawGenerationInfo map[string]any

	// Duration is how long the generation took.
	Duration time.Duration
}

// ModelCallRequest captures the debuggable and mutable parts of a model request.
// Model-call events use this typed shape instead of raw message slices so subscribers, trace
// capture, and tests all share one request contract.
// Subscribers may modify Messages in BeforeModelCallEvent for ephemeral context injection.
// Options contains meaningful resolved call options when the model wrapper can capture them.
// OptionCaptureComplete must be false when provider-specific options cannot be fully introspected.
type ModelCallRequest struct {
	Messages          []*ModelMessage
	Options           CallOptions
	SelectionRevision HistoryRevision
	ActiveEntryIds    []EntryId

	OptionCaptureComplete bool
	OptionCaptureNotes    []string
}

// Stream represents a streaming response from the model.
// It provides access to content chunks as they arrive and the final response.
// Text uses MetaContent. Tools and opaque continuation items are available in Response.
type Stream interface {
	// Chunks returns a channel that receives content chunks as they stream in.
	// The channel is closed when streaming completes (either successfully or with error).
	// Provider chunks contain metadata or an error; execution completion publishes Content.
	Chunks() <-chan StreamChunk

	// Response blocks until streaming completes and returns the final ContentResponse.
	// Provider responses contain candidates and completion data, not an accepted execution answer.
	// If an error occurred during streaming, it is returned here.
	Response() (*ContentResponse, error)

	// Close cancels the stream and releases resources.
	// It's safe to call multiple times.
	Close()
}

// StreamChunk represents a single chunk of streamed content with metadata.
type StreamChunk struct {
	// Content is the complete accepted execution answer. Only terminal publication sets it.
	Content []ContentPart

	// MetaContent contains provisional text deltas, reasoning, and provider commentary.
	MetaContent []*MetaContent

	// MessageId identifies the final answer. SourceMessageIds identify its provisional messages.
	MessageId        string
	SourceMessageIds []string

	// RunId identifies the logical execution. EventNumber is the fixed live event position.
	// A live chunk can precede its save. Deduplicate within one connection; after recovery,
	// discard the old preview and start from saved state, which can have a lower cursor.
	RunId       string
	EventNumber uint64

	// Err is set if an error occurred during streaming.
	// When Err is non-nil, the stream should be considered terminated.
	Err error

	// Timestamp is when this chunk was emitted.
	Timestamp time.Time

	// Iteration is the iteration number when this chunk was emitted.
	Iteration int

	// Depth is the nesting depth when this chunk was emitted.
	Depth int

	// Source is the hierarchical execution path that produced this chunk.
	// Format: "contextName/iteration/childName/childIteration/..."
	// Examples:
	//   - "main/1" - Root context, iteration 1
	//   - "main/2/research/1" - Root iter 2, child "research" iter 1
	Source string

	// ContextId is the opaque identity of the context that emitted this chunk.
	ContextId string

	// ParentContextId is the opaque identity of the parent context.
	// Empty for root contexts.
	ParentContextId string

	// ModelCallId identifies the model call that emitted this chunk.
	ModelCallId string

	// StreamId is a caller-defined stream label. Use MessageId and ContextId for display identity.
	StreamId string

	// StreamTopicId groups related streams (caller-provided).
	// Multiple streams may share the same topic; subscribers handle interleaving.
	StreamTopicId string
}
