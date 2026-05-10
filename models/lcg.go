package models

import (
	"context"
	"sync"
	"time"

	"github.com/rickchristie/gent"
	"github.com/tmc/langchaingo/llms"
)

// LCGWrapper wraps an llms.Model and implements gent's Model interface.
// It normalizes token usage across providers and automatically publishes model call events
// when an ExecutionContext is provided.
//
// Example usage:
//
//	llm, _ := openai.New(openai.WithToken(apiKey))
//	model := models.NewLCGWrapper(llm).WithModelName("gpt-4")
//
//	// Generate content with event publishing and streaming support
//	response, err := model.GenerateContent(execCtx, "req-1", "llm", messages)
type LCGWrapper struct {
	model     llms.Model
	modelName string // Optional model name for events
	provider  string // Optional provider name for trace/debug events
}

type callbackContentDeduper struct {
	mu        sync.Mutex
	streaming map[string]int
	reasoning map[string]int
}

func newCallbackContentDeduper() *callbackContentDeduper {
	return &callbackContentDeduper{
		streaming: make(map[string]int),
		reasoning: make(map[string]int),
	}
}

func (d *callbackContentDeduper) shouldEmitStreaming(content string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	if decrementContentCount(d.reasoning, content) {
		return false
	}
	d.streaming[content]++
	return true
}

func (d *callbackContentDeduper) shouldEmitReasoning(content string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	if decrementContentCount(d.streaming, content) {
		return false
	}
	d.reasoning[content]++
	return true
}

func decrementContentCount(counts map[string]int, content string) bool {
	count := counts[content]
	if count == 0 {
		return false
	}
	if count == 1 {
		delete(counts, content)
		return true
	}
	counts[content] = count - 1
	return true
}

// NewLCGWrapper creates a new LCGWrapper wrapping the given llms.Model.
func NewLCGWrapper(model llms.Model) *LCGWrapper {
	return &LCGWrapper{
		model: model,
	}
}

// WithModelName sets the model name used in events.
// Returns the model for chaining.
func (m *LCGWrapper) WithModelName(name string) *LCGWrapper {
	m.modelName = name
	return m
}

// WithProvider sets the provider name used in events.
// Returns the model for chaining.
func (m *LCGWrapper) WithProvider(provider string) *LCGWrapper {
	m.provider = provider
	return m
}

// ModelName returns the model name. Implements [gent.ModelNamer].
func (m *LCGWrapper) ModelName() string {
	return m.modelName
}

// Provider returns the provider name used in events.
func (m *LCGWrapper) Provider() string {
	return m.provider
}

// Unwrap returns the underlying llms.Model.
func (m *LCGWrapper) Unwrap() llms.Model {
	return m.model
}

// convertLCGResponse converts an llms.ContentResponse to gent.ContentResponse with normalized
// tokens.
func convertLCGResponse(
	lcgResponse *llms.ContentResponse,
	duration time.Duration,
) *gent.ContentResponse {
	response := &gent.ContentResponse{
		Choices: make([]*gent.ContentChoice, len(lcgResponse.Choices)),
		Info:    &gent.GenerationInfo{Duration: duration},
	}

	// Convert choices
	for i, choice := range lcgResponse.Choices {
		response.Choices[i] = &gent.ContentChoice{
			Content:          choice.Content,
			StopReason:       choice.StopReason,
			FuncCall:         choice.FuncCall,
			ToolCalls:        choice.ToolCalls,
			ReasoningContent: choice.ReasoningContent,
		}
	}

	// Extract and normalize token info from the first choice's GenerationInfo
	if len(lcgResponse.Choices) > 0 && lcgResponse.Choices[0].GenerationInfo != nil {
		rawInfo := lcgResponse.Choices[0].GenerationInfo
		response.Info.RawGenerationInfo = rawInfo
		response.Info.InputTokens = extractInputTokens(rawInfo)
		response.Info.OutputTokens = extractOutputTokens(rawInfo)
		response.Info.TotalTokens = extractTotalTokens(
			rawInfo,
			response.Info.InputTokens,
			response.Info.OutputTokens,
		)
		response.Info.CachedInputTokens = extractCachedInputTokens(rawInfo)
		response.Info.ReasoningTokens = extractReasoningTokens(rawInfo)
	}

	return response
}

// extractInputTokens extracts input/prompt token count from GenerationInfo.
// Handles different key names used by different providers.
func extractInputTokens(info map[string]any) int {
	// OpenAI / Ollama / Maritaca / Google (compat)
	if v := getIntFromMap(info, "PromptTokens"); v > 0 {
		return v
	}
	// Anthropic
	if v := getIntFromMap(info, "InputTokens"); v > 0 {
		return v
	}
	// Google / Bedrock
	if v := getIntFromMap(info, "input_tokens"); v > 0 {
		return v
	}
	return 0
}

// extractOutputTokens extracts output/completion token count from GenerationInfo.
func extractOutputTokens(info map[string]any) int {
	// OpenAI / Ollama / Maritaca / Google (compat)
	if v := getIntFromMap(info, "CompletionTokens"); v > 0 {
		return v
	}
	// Anthropic
	if v := getIntFromMap(info, "OutputTokens"); v > 0 {
		return v
	}
	// Google / Bedrock
	if v := getIntFromMap(info, "output_tokens"); v > 0 {
		return v
	}
	return 0
}

// extractTotalTokens extracts total token count or computes it.
func extractTotalTokens(info map[string]any, input, output int) int {
	// OpenAI / Ollama / Maritaca / Google (compat)
	if v := getIntFromMap(info, "TotalTokens"); v > 0 {
		return v
	}
	// Google / Bedrock
	if v := getIntFromMap(info, "total_tokens"); v > 0 {
		return v
	}
	// Compute if not available
	return input + output
}

// extractCachedInputTokens extracts cached input token count from GenerationInfo.
func extractCachedInputTokens(info map[string]any) int {
	// OpenAI
	if v := getIntFromMap(info, "PromptCachedTokens"); v > 0 {
		return v
	}
	// Anthropic
	if v := getIntFromMap(info, "CacheReadInputTokens"); v > 0 {
		return v
	}
	// Google / Ollama
	if v := getIntFromMap(info, "CachedTokens"); v > 0 {
		return v
	}
	return 0
}

// extractReasoningTokens extracts reasoning/thinking token count from GenerationInfo.
func extractReasoningTokens(info map[string]any) int {
	// OpenAI
	if v := getIntFromMap(info, "ReasoningTokens"); v > 0 {
		return v
	}
	if v := getIntFromMap(info, "CompletionReasoningTokens"); v > 0 {
		return v
	}
	// OpenAI standardized field
	if v := getIntFromMap(info, "ThinkingTokens"); v > 0 {
		return v
	}
	return 0
}

// getIntFromMap extracts an int value from a map, handling various numeric types.
func getIntFromMap(m map[string]any, key string) int {
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	case float64:
		return int(n)
	case float32:
		return int(n)
	default:
		return 0
	}
}

func resolveCallOptions(options []llms.CallOption) llms.CallOptions {
	var resolved llms.CallOptions
	llms.WithStreamThinking(true)(&resolved)
	for _, option := range options {
		if option != nil {
			option(&resolved)
		}
	}
	return resolved
}

// GenerateContentStream implements gent.StreamingModel.GenerateContentStream.
// It provides streaming token-by-token generation with support for reasoning/thinking content.
//
// The returned stream uses an unbounded internal buffer, so this method never blocks
// the producer even if the consumer is slow or not reading.
//
// When execCtx is provided, chunks are also emitted to streaming subscribers via EmitChunk.
func (m *LCGWrapper) GenerateContentStream(
	execCtx *gent.ExecutionContext,
	streamId string,
	streamTopicId string,
	messages []llms.MessageContent,
	options ...llms.CallOption,
) (gent.Stream, error) {
	request := gent.ModelCallRequest{
		Messages:              messages,
		Options:               resolveCallOptions(options),
		OptionCaptureComplete: false,
		OptionCaptureNotes: []string{
			"LangChainGo may hide provider-specific client options outside llms.CallOptions",
		},
	}
	callIteration := 0
	sourcePath := ""
	modelCallId := ""
	contextId := ""
	parentContextId := ""
	callDepth := 0
	baseCtx := context.Background()

	if execCtx != nil {
		// Publish BeforeModelCall event — subscribers may modify event.Request
		// for ephemeral dynamic context injection.
		beforeEvent := execCtx.PublishBeforeModelCall(
			m.modelName, request,
			gent.WithModelStream(streamId, streamTopicId),
			gent.WithModelProvider(m.provider),
		)

		// Subscribers may inject ephemeral context into Request.Messages. Use the same mutated
		// request for the provider call and after-event so traces show what was actually sent.
		request = beforeEvent.Request
		callIteration = beforeEvent.Iteration
		sourcePath = beforeEvent.Source
		modelCallId = beforeEvent.ModelCallId
		contextId = beforeEvent.ContextId
		parentContextId = beforeEvent.ParentContextId
		callDepth = beforeEvent.Depth
		baseCtx = execCtx.Context()
	}

	ctx, cancel := context.WithCancel(baseCtx)

	// Create stream with duration tracking
	stream := gent.NewStreamWithDurationWithClose(cancel)

	// Set up streaming callbacks.
	//
	// We use BOTH WithStreamingFunc and WithStreamingReasoningFunc because
	// providers handle them differently:
	//   - OpenAI: routes both reasoning and content through StreamingReasoningFunc
	//   - Anthropic: routes content through StreamingFunc and reasoning through
	//     StreamingReasoningFunc separately. Also has a bug (langchaingo v0.1.14)
	//     where only StreamingFunc is checked for streaming response routing —
	//     without it, the client tries to JSON-parse SSE data.
	//
	// LangChainGo can deliver the same content chunk through both callbacks. We emit
	// whichever callback arrives first and pair off the later duplicate from the other
	// callback. Counts preserve legitimate repeated same-text chunks from one callback.
	contentDeduper := newCallbackContentDeduper()

	streamingContentCallback := llms.WithStreamingFunc(
		func(_ context.Context, contentChunk []byte) error {
			if len(contentChunk) > 0 {
				content := string(contentChunk)
				if !contentDeduper.shouldEmitStreaming(content) {
					return nil
				}
				chunk := gent.StreamChunk{
					Content:         content,
					Source:          sourcePath,
					Iteration:       callIteration,
					Depth:           callDepth,
					ContextId:       contextId,
					ParentContextId: parentContextId,
					ModelCallId:     modelCallId,
					StreamId:        streamId,
					StreamTopicId:   streamTopicId,
				}
				if stream.SendContent(chunk.Content) {
					if execCtx != nil {
						execCtx.EmitChunk(chunk)
					}
				}
			}
			return nil
		},
	)

	streamingReasoningCallback := llms.WithStreamingReasoningFunc(
		func(_ context.Context, reasoningChunk, contentChunk []byte) error {
			if len(reasoningChunk) > 0 {
				chunk := gent.StreamChunk{
					ReasoningContent: string(reasoningChunk),
					Source:           sourcePath,
					Iteration:        callIteration,
					Depth:            callDepth,
					ContextId:        contextId,
					ParentContextId:  parentContextId,
					ModelCallId:      modelCallId,
					StreamId:         streamId,
					StreamTopicId:    streamTopicId,
				}
				if stream.SendReasoning(chunk.ReasoningContent) {
					if execCtx != nil {
						execCtx.EmitChunk(chunk)
					}
				}
			}
			if len(contentChunk) > 0 {
				content := string(contentChunk)
				if !contentDeduper.shouldEmitReasoning(content) {
					return nil
				}
				chunk := gent.StreamChunk{
					Content:         content,
					Source:          sourcePath,
					Iteration:       callIteration,
					Depth:           callDepth,
					ContextId:       contextId,
					ParentContextId: parentContextId,
					ModelCallId:     modelCallId,
					StreamId:        streamId,
					StreamTopicId:   streamTopicId,
				}
				if stream.SendContent(chunk.Content) {
					if execCtx != nil {
						execCtx.EmitChunk(chunk)
					}
				}
			}
			return nil
		},
	)

	// Build options with streaming enabled.
	// StreamThinking is added before user options so users can override it.
	// The streaming callbacks are added last to ensure they take effect.
	opts := make([]llms.CallOption, 0, len(options)+3)
	opts = append(opts, llms.WithStreamThinking(true))
	opts = append(opts, options...)
	opts = append(opts, streamingContentCallback, streamingReasoningCallback)

	// Start the model call in a goroutine
	go func() {
		defer cancel()

		lcgResponse, err := m.model.GenerateContent(ctx, request.Messages, opts...)
		duration := stream.Duration()

		// Convert response
		var response *gent.ContentResponse
		if lcgResponse != nil && err == nil {
			response = convertLCGResponse(lcgResponse, duration)
		} else if err == nil {
			// Build response from accumulated content
			response = &gent.ContentResponse{
				Choices: []*gent.ContentChoice{
					{
						Content:          stream.AccumulatedContent(),
						ReasoningContent: stream.AccumulatedReasoning(),
					},
				},
				Info: &gent.GenerationInfo{Duration: duration},
			}
		}

		// Publish AfterModelCall event (also updates stats for tokens)
		if execCtx != nil {
			execCtx.PublishAfterModelCallForIteration(
				callIteration, m.modelName, request, response, duration, err,
				gent.WithModelCallId(modelCallId),
				gent.WithModelStream(streamId, streamTopicId),
				gent.WithModelCallSource(sourcePath),
				gent.WithModelProvider(m.provider),
			)
		}

		// Complete the stream
		completed := stream.TryComplete(response, err)

		// Emit error chunk if error occurred before the consumer closed the stream.
		if err != nil && completed && execCtx != nil {
			execCtx.EmitChunk(gent.StreamChunk{
				Source:          sourcePath,
				Iteration:       callIteration,
				Depth:           callDepth,
				ContextId:       contextId,
				ParentContextId: parentContextId,
				ModelCallId:     modelCallId,
				StreamId:        streamId,
				StreamTopicId:   streamTopicId,
				Err:             err,
			})
		}
	}()

	return stream, nil
}

// Compile-time check that LCGWrapper implements gent.Model.
var _ gent.Model = (*LCGWrapper)(nil)
