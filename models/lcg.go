package models

import (
	"context"
	"time"

	"github.com/rickchristie/gent"
	"github.com/tmc/langchaingo/llms"
)

// LCGWrapper wraps an llms.Model and implements gent's Model interface.
// It normalizes token usage across providers and automatically traces model calls
// when an ExecutionContext is provided.
//
// Example usage:
//
//	llm, _ := openai.New(openai.WithToken(apiKey))
//	model := models.NewLCGWrapper(llm).WithModelName("gpt-4")
//
//	// With ExecutionContext (automatic tracing)
//	response, err := model.GenerateContent(ctx, execCtx, messages)
//
//	// Without ExecutionContext (no tracing)
//	response, err := model.GenerateContent(ctx, nil, messages)
type LCGWrapper struct {
	model     llms.Model
	modelName string // Optional model name for tracing
}

// NewLCGWrapper creates a new LCGWrapper wrapping the given llms.Model.
func NewLCGWrapper(model llms.Model) *LCGWrapper {
	return &LCGWrapper{
		model: model,
	}
}

// WithModelName sets the model name used in trace events.
// Returns the model for chaining.
func (m *LCGWrapper) WithModelName(name string) *LCGWrapper {
	m.modelName = name
	return m
}

// Unwrap returns the underlying llms.Model.
func (m *LCGWrapper) Unwrap() llms.Model {
	return m.model
}

// GenerateContent implements gent.Model.GenerateContent.
// When execCtx is provided, the call is automatically traced with token counts and duration.
// Token usage is automatically normalized across providers.
func (m *LCGWrapper) GenerateContent(
	ctx context.Context,
	execCtx *gent.ExecutionContext,
	messages []llms.MessageContent,
	options ...llms.CallOption,
) (*gent.ContentResponse, error) {
	// Fire BeforeModelCall hook
	if execCtx != nil {
		execCtx.FireBeforeModelCall(ctx, gent.BeforeModelCallEvent{
			Model:   m.modelName,
			Request: messages,
		})
	}

	// Call the underlying model
	startTime := time.Now()
	lcgResponse, err := m.model.GenerateContent(ctx, messages, options...)
	duration := time.Since(startTime)

	// Convert response
	var response *gent.ContentResponse
	if lcgResponse != nil {
		response = convertLCGResponse(lcgResponse, duration)
	}

	// Fire AfterModelCall hook and trace
	if execCtx != nil {
		// Fire AfterModelCall hook first (for logging)
		execCtx.FireAfterModelCall(ctx, gent.AfterModelCallEvent{
			Model:    m.modelName,
			Request:  messages,
			Response: response,
			Duration: duration,
			Error:    err,
		})

		// Then trace for aggregation
		trace := gent.ModelCallTrace{
			Model:    m.modelName,
			Request:  messages,
			Response: response,
			Duration: duration,
			Error:    err,
		}
		if response != nil && response.Info != nil {
			trace.InputTokens = response.Info.InputTokens
			trace.OutputTokens = response.Info.OutputTokens
			// Cost calculation can be added here based on model pricing
		}
		execCtx.Trace(trace)
	}

	return response, err
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

// GenerateContentStream implements gent.StreamingModel.GenerateContentStream.
// It provides streaming token-by-token generation with support for reasoning/thinking content.
//
// The returned stream uses an unbounded internal buffer, so this method never blocks
// the producer even if the consumer is slow or not reading.
func (m *LCGWrapper) GenerateContentStream(
	ctx context.Context,
	execCtx *gent.ExecutionContext,
	messages []llms.MessageContent,
	options ...llms.CallOption,
) (gent.Stream, error) {
	// Fire BeforeModelCall hook
	if execCtx != nil {
		execCtx.FireBeforeModelCall(ctx, gent.BeforeModelCallEvent{
			Model:   m.modelName,
			Request: messages,
		})
	}

	// Create stream with duration tracking
	stream := gent.NewStreamWithDuration()

	// Set up streaming callback using WithStreamingReasoningFunc.
	// This callback receives both reasoning and content chunks, avoiding duplication
	// that would occur if we also used WithStreamingFunc.
	streamingCallback := llms.WithStreamingReasoningFunc(
		func(_ context.Context, reasoningChunk, contentChunk []byte) error {
			if len(reasoningChunk) > 0 {
				stream.SendReasoning(string(reasoningChunk))
			}
			if len(contentChunk) > 0 {
				stream.SendContent(string(contentChunk))
			}
			return nil
		},
	)

	// Build options with streaming enabled.
	// StreamThinking is added before user options so users can override it.
	// The streaming callback is added last to ensure it takes effect.
	opts := make([]llms.CallOption, 0, len(options)+2)
	opts = append(opts, llms.WithStreamThinking(true))
	opts = append(opts, options...)
	opts = append(opts, streamingCallback)

	// Start the model call in a goroutine
	go func() {
		lcgResponse, err := m.model.GenerateContent(ctx, messages, opts...)
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

		// Fire AfterModelCall hook and trace
		if execCtx != nil {
			execCtx.FireAfterModelCall(ctx, gent.AfterModelCallEvent{
				Model:    m.modelName,
				Request:  messages,
				Response: response,
				Duration: duration,
				Error:    err,
			})

			trace := gent.ModelCallTrace{
				Model:    m.modelName,
				Request:  messages,
				Response: response,
				Duration: duration,
				Error:    err,
			}
			if response != nil && response.Info != nil {
				trace.InputTokens = response.Info.InputTokens
				trace.OutputTokens = response.Info.OutputTokens
			}
			execCtx.Trace(trace)
		}

		// Complete the stream
		stream.Complete(response, err)
	}()

	return stream, nil
}

// Compile-time check that LCGWrapper implements gent.Model.
var _ gent.Model = (*LCGWrapper)(nil)

// Compile-time check that LCGWrapper implements gent.StreamingModel.
var _ gent.StreamingModel = (*LCGWrapper)(nil)
