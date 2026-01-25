package gent

import (
	"time"

	"github.com/tmc/langchaingo/llms"
)

// Model is gent's model interface. It wraps LangChainGo's llms.Model but provides
// a cleaner interface with normalized token usage information and automatic tracing.
//
// When an ExecutionContext is provided, the model will automatically trace the call
// and emit chunks for streaming subscribers.
//
// If execCtx is nil, tracing and chunk emission are skipped.
type Model interface {
	// GenerateContent generates content from a sequence of messages.
	// Unlike llms.Model, this returns a GenerationInfo struct with normalized
	// token counts that work across all providers.
	//
	// Parameters:
	//   - execCtx: ExecutionContext for tracing, cancellation, and stream fan-in
	//   - streamId: Unique identifier for this call (caller-provided)
	//   - streamTopicId: Topic for grouping related calls (caller-provided)
	//   - messages: Input messages
	//   - options: LLM call options
	//
	// Cancellation:
	// The implementation should use execCtx.Context() for HTTP client calls.
	// This context is cancelled when limits are exceeded or the execution is stopped.
	//
	// Stream Emission Requirement:
	// Implementations MUST call execCtx.EmitChunk() with the complete response
	// content as a single chunk. This ensures subscribers receive content
	// regardless of whether the underlying model supports streaming.
	//
	// The emitted chunk should have:
	//   - Content: The full response text
	//   - StreamId: The provided streamId
	//   - StreamTopicId: The provided streamTopicId
	//   - Source: Will be auto-populated by EmitChunk if empty
	//
	// The streamId should be unique across concurrent calls. If empty, chunks
	// are still emitted but cannot be filtered by stream ID.
	GenerateContent(
		execCtx *ExecutionContext,
		streamId string,
		streamTopicId string,
		messages []llms.MessageContent,
		options ...llms.CallOption,
	) (*ContentResponse, error)
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

// StreamingModel extends Model with streaming capabilities.
// Models that support token-by-token streaming should implement this interface.
type StreamingModel interface {
	Model

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
	// This context is cancelled when limits are exceeded or the execution is stopped.
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
	// The streamId should be unique across concurrent streams. If empty, chunks
	// are still emitted but cannot be filtered by stream ID.
	//
	// Usage:
	//
	//	stream, err := model.GenerateContentStream(execCtx, "req-123", "llm", msgs)
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

	// Source is the hierarchical execution path that produced this chunk.
	// Format: "contextName/iteration/childName/childIteration/..."
	// Examples:
	//   - "main/1" - Root context, iteration 1
	//   - "main/2/research/1" - Root iter 2, child "research" iter 1
	Source string

	// StreamId uniquely identifies this stream (caller-provided).
	// This should be unique per LLM call to avoid interleaving confusion.
	StreamId string

	// StreamTopicId groups related streams (caller-provided).
	// Multiple streams may share the same topic; subscribers handle interleaving.
	StreamTopicId string
}
