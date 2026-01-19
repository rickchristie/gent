package gent

import (
	"context"
	"time"

	"github.com/tmc/langchaingo/llms"
)

// Model is gent's model interface. It wraps LangChainGo's llms.Model but provides
// a cleaner interface with normalized token usage information and automatic tracing.
//
// When an ExecutionContext is provided, the model will automatically trace the call.
// If execCtx is nil, tracing is skipped.
type Model interface {
	// GenerateContent generates content from a sequence of messages.
	// Unlike llms.Model, this returns a GenerationInfo struct with normalized
	// token counts that work across all providers.
	//
	// The execCtx parameter enables automatic tracing. Pass nil to skip tracing.
	GenerateContent(
		ctx context.Context,
		execCtx *ExecutionContext,
		messages []llms.MessageContent,
		options ...llms.CallOption,
	) (
		*ContentResponse,
		error,
	)
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
	// The stream is non-blocking and uses an unbounded buffer internally,
	// so producers never block even if consumers are slow or absent.
	//
	// Usage:
	//
	//	stream, err := model.GenerateContentStream(ctx, execCtx, messages)
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
		ctx context.Context,
		execCtx *ExecutionContext,
		messages []llms.MessageContent,
		options ...llms.CallOption,
	) (Stream, error)
}

// Stream represents a streaming response from the model.
// It provides access to content chunks as they arrive and the final response.
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

// StreamChunk represents a single chunk of streamed content.
type StreamChunk struct {
	// Content is the text content delta for this chunk.
	Content string

	// ReasoningContent is the reasoning/thinking content delta for this chunk.
	ReasoningContent string

	// Err is set if an error occurred during streaming.
	// When Err is non-nil, the stream should be considered terminated.
	Err error
}
