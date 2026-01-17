package models

import (
	"context"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/hooks"
	"github.com/tmc/langchaingo/llms"
)

// LCGWrapper wraps an llms.Model and implements gent's Model interface.
// It normalizes token usage across providers and fires hooks before/after calls.
//
// Example usage:
//
//	llm, _ := openai.New(openai.WithToken(apiKey))
//	model := models.NewLCGWrapper(llm).
//	    RegisterHook(&MyLoggingHook{}).
//	    RegisterHook(&MyMetricsHook{})
//
//	response, err := model.GenerateContent(ctx, messages)
//	fmt.Printf("Used %d input tokens, %d output tokens\n",
//	    response.Info.InputTokens, response.Info.OutputTokens)
type LCGWrapper struct {
	model llms.Model
	hooks *hooks.ModelRegistry
}

// NewLCGWrapper creates a new LCGWrapper wrapping the given llms.Model.
func NewLCGWrapper(model llms.Model) *LCGWrapper {
	return &LCGWrapper{
		model: model,
		hooks: hooks.NewModelRegistry(),
	}
}

// WithHooks sets the model hook registry. Returns the model for chaining.
func (m *LCGWrapper) WithHooks(h *hooks.ModelRegistry) *LCGWrapper {
	m.hooks = h
	return m
}

// RegisterHook adds a hook to the model's hook registry.
// Returns the model for chaining.
func (m *LCGWrapper) RegisterHook(hook any) *LCGWrapper {
	m.hooks.Register(hook)
	return m
}

// Unwrap returns the underlying llms.Model.
func (m *LCGWrapper) Unwrap() llms.Model {
	return m.model
}

// GenerateContent implements gent.Model.GenerateContent.
// It fires BeforeGenerateContent hooks before the call and AfterGenerateContent hooks after.
// Token usage is automatically normalized across providers.
func (m *LCGWrapper) GenerateContent(
	ctx context.Context,
	messages []llms.MessageContent,
	options ...llms.CallOption,
) (*gent.ContentResponse, error) {
	// Resolve options to get CallOptions struct
	opts := resolveCallOptions(options)

	// Fire before hooks
	beforeEvent := gent.BeforeGenerateContentEvent{
		Messages: messages,
		Options:  opts,
	}
	if err := m.hooks.FireBeforeGenerateContent(ctx, beforeEvent); err != nil {
		return nil, err
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

	// Fire after hooks
	afterEvent := gent.AfterGenerateContentEvent{
		Messages: messages,
		Options:  opts,
		Response: response,
		Error:    err,
	}
	m.hooks.FireAfterGenerateContent(ctx, afterEvent)

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

// resolveCallOptions applies all CallOption functions to get the resolved CallOptions.
func resolveCallOptions(options []llms.CallOption) *llms.CallOptions {
	opts := &llms.CallOptions{}
	for _, opt := range options {
		opt(opts)
	}
	return opts
}

// Compile-time check that LCGWrapper implements gent.Model.
var _ gent.Model = (*LCGWrapper)(nil)
