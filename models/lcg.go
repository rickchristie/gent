package models

import (
	"context"
	"sync"
	"time"

	"github.com/rickchristie/gent"
	"github.com/tmc/langchaingo/llms"
)

// LCGWrapper wraps an llms.Model and implements gent's Model interface.
// It normalizes token usage across providers. Invoke it through ExecutionContext's managed
// GenerateContentStream method so requests, chunks, responses, and usage are recorded together.
//
// Example usage:
//
//	model := models.NewLCGWrapper(llm).WithModelName(modelName)
//	request := gent.NewModelCallRequest(messages)
//	stream, err := execCtx.GenerateContentStream(
//	    model, gent.ModelCall{StreamId: "answer"}, request,
//	)
//
// Handle err, consume stream.Chunks(), then call stream.Response() to await producer completion.
type LCGWrapper struct {
	model           llms.Model
	modelName       string // Optional model name for events
	provider        string // Optional provider name for trace/debug events
	recoveryVersion string
}

// WithRecoveryVersion identifies provider configuration hidden inside the injected client.
// Change this value when endpoint, provider defaults, or middleware behavior changes.
func (m *LCGWrapper) WithRecoveryVersion(version string) *LCGWrapper {
	m.recoveryVersion = version
	return m
}

func (m *LCGWrapper) RecoveryConfig() any {
	return map[string]any{"model": m.modelName, "provider": m.provider,
		"clientVersion": m.recoveryVersion}
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

// GenerateContentStream adapts LangChainGo's callbacks to a provider stream. Framework observation
// belongs to ExecutionContext.GenerateContentStream, which captures the final resolved options.
func (m *LCGWrapper) GenerateContentStream(
	base context.Context, request gent.ModelCallRequest,
) (gent.Stream, error) {
	ctx, cancel := context.WithCancel(base)
	stream, err := gent.NewBoundedStreamWithDuration(ctx, 64, cancel)
	if err != nil {
		cancel()
		return nil, err
	}
	deduper := newCallbackContentDeduper()
	options := request.Options
	if options.Model == "" {
		options.Model = m.modelName
	}
	options.StreamingFunc = func(_ context.Context, content []byte) error {
		if len(content) > 0 && deduper.shouldEmitStreaming(string(content)) {
			if !stream.SendContent(string(content)) {
				return ctx.Err()
			}
		}
		return ctx.Err()
	}
	options.StreamingReasoningFunc = func(_ context.Context, reasoning, content []byte) error {
		if len(reasoning) > 0 {
			if !stream.SendReasoning(string(reasoning)) {
				return ctx.Err()
			}
		}
		if len(content) > 0 && deduper.shouldEmitReasoning(string(content)) {
			if !stream.SendContent(string(content)) {
				return ctx.Err()
			}
		}
		return ctx.Err()
	}
	go func() {
		defer cancel()
		providerResponse, err := m.model.GenerateContent(
			ctx,
			m.providerMessages(request.Messages),
			llms.WithOptions(options),
		)
		duration := stream.Duration()
		var response *gent.ContentResponse
		if providerResponse != nil {
			response = convertLCGResponse(m.providerResponse(providerResponse), duration)
		} else {
			response = &gent.ContentResponse{Choices: []*gent.ContentChoice{{
				Content:          stream.AccumulatedContent(),
				ReasoningContent: stream.AccumulatedReasoning(),
			}}, Info: &gent.GenerationInfo{Duration: duration}}
		}
		stream.Complete(response, err)
	}()
	return stream, nil
}

// Compile-time check that LCGWrapper implements gent.Model.
var _ gent.Model = (*LCGWrapper)(nil)
