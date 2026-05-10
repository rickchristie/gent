package gent

import (
	"time"

	"github.com/tmc/langchaingo/llms"
)

// Model is gent's model interface. All model calls use streaming — chunks are
// emitted in real-time as they arrive from the LLM, enabling real-time
// observation, repetition detection, and early cancellation.
//
// When an ExecutionContext is provided, the model will automatically trace the
// call and emit chunks for streaming subscribers.
//
// If execCtx is nil, tracing and chunk emission are skipped.
type Model interface {
	// GenerateContentStream generates content with streaming support.
	// It returns a Stream that provides chunks as they arrive from the model.
	//
	// Parameters:
	//   - execCtx: ExecutionContext for tracing, cancellation, and stream fan-in
	//   - streamId: Unique identifier for this stream (caller-provided)
	//   - streamTopicId: Topic for grouping related streams (caller-provided)
	//   - messages: Input messages
	//   - options: LLM call options
	//
	// Cancellation:
	// The implementation should use execCtx.Context() for HTTP client calls.
	// This context is cancelled when limits are exceeded or the execution is
	// stopped.
	//
	// Stream Emission Requirement:
	// Implementations MUST call execCtx.EmitChunk() for each chunk as it
	// arrives from the LLM. This enables real-time observation of responses
	// across the execution tree.
	//
	// Each emitted chunk should have:
	//   - Content/ReasoningContent: The chunk's content delta
	//   - StreamId: The provided streamId
	//   - StreamTopicId: The provided streamTopicId
	//   - Source: Will be auto-populated by EmitChunk if empty
	//   - Err: Set if an error occurred (final chunk only)
	//
	// The streamId should be unique across concurrent streams. If empty,
	// chunks are still emitted but cannot be filtered by stream ID.
	//
	// Usage:
	//
	//	stream, err := model.GenerateContentStream(execCtx, "req-1", "llm", msgs)
	//	if err != nil {
	//	    return err
	//	}
	//	for chunk := range stream.Chunks() {
	//	    if chunk.Err != nil {
	//	        return chunk.Err
	//	    }
	//	    fmt.Print(chunk.Content)
	//	}
	//	response, err := stream.Response()
	GenerateContentStream(
		execCtx *ExecutionContext,
		streamId string,
		streamTopicId string,
		messages []llms.MessageContent,
		options ...llms.CallOption,
	) (Stream, error)
}

// ContentResponse is the response from a GenerateContent call.
type ContentResponse struct {
	// Choices contains the generated content choices.
	Choices []*ContentChoice

	// Info contains generation metadata including normalized token counts.
	Info *GenerationInfo
}

// ContentChoice is a single content choice from the model.
type ContentChoice struct {
	// Content is the textual content of the response.
	Content string

	// StopReason is the reason the model stopped generating.
	StopReason string

	// FuncCall is non-nil when the model asks to invoke a function/tool.
	FuncCall *llms.FunctionCall

	// ToolCalls is a list of tool calls the model asks to invoke.
	ToolCalls []llms.ToolCall

	// ReasoningContent contains reasoning/thinking content if supported.
	ReasoningContent string
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
	Messages []llms.MessageContent
	Options  llms.CallOptions

	OptionCaptureComplete bool
	OptionCaptureNotes    []string
}

// StreamingModel is an alias for [Model] for backward compatibility.
// Deprecated: Use [Model] directly.
type StreamingModel = Model

// Stream represents a streaming response from the model.
// It provides access to content chunks as they arrive and the final response.
// Currently [Stream] interface only supports text content streaming. In the future, we may add
// support for othe modalities by adding more fields to [StreamChunk].
type Stream interface {
	// Chunks returns a channel that receives content chunks as they stream in.
	// The channel is closed when streaming completes (either successfully or with error).
	// Each chunk may contain content, reasoning content, or an error.
	Chunks() <-chan StreamChunk

	// Response blocks until streaming completes and returns the final ContentResponse.
	// This aggregates all streamed content into a single response.
	// If an error occurred during streaming, it is returned here.
	Response() (*ContentResponse, error)

	// Close cancels the stream and releases resources.
	// It's safe to call multiple times.
	Close()
}

// StreamChunk represents a single chunk of streamed content with metadata.
type StreamChunk struct {
	// Content is the text content delta for this chunk.
	Content string

	// ReasoningContent is the reasoning/thinking content delta for this chunk.
	ReasoningContent string

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

	// StreamId uniquely identifies this stream (caller-provided).
	// This should be unique per LLM call to avoid interleaving confusion.
	StreamId string

	// StreamTopicId groups related streams (caller-provided).
	// Multiple streams may share the same topic; subscribers handle interleaving.
	StreamTopicId string
}
