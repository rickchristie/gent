package gent

import (
	"context"
	"time"

	"github.com/tmc/langchaingo/llms"
)

// -----------------------------------------------------------------------------
// Gent Model Interface
// -----------------------------------------------------------------------------

// Model is gent's model interface. It wraps LangChainGo's llms.Model but provides
// a cleaner interface with normalized token usage information.
type Model interface {
	// GenerateContent generates content from a sequence of messages.
	// Unlike llms.Model, this returns a GenerationInfo struct with normalized
	// token counts that work across all providers.
	GenerateContent(
		ctx context.Context,
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

// -----------------------------------------------------------------------------
// LangChainGo Model Wrapper
// -----------------------------------------------------------------------------

// LCGModelWrapper wraps an llms.Model and implements gent's Model interface.
// It normalizes token usage across providers and fires hooks before/after calls.
//
// Example usage:
//
//	llm, _ := openai.New(openai.WithToken(apiKey))
//	model := gent.NewLCGModelWrapper(llm).
//	    RegisterHook(&MyLoggingHook{}).
//	    RegisterHook(&MyMetricsHook{})
//
//	response, err := model.GenerateContent(ctx, messages)
//	fmt.Printf("Used %d input tokens, %d output tokens\n",
//	    response.Info.InputTokens, response.Info.OutputTokens)
type LCGModelWrapper struct {
	model llms.Model
	hooks *ModelHookRegistry
}

// NewLCGModelWrapper creates a new LCGModelWrapper wrapping the given llms.Model.
func NewLCGModelWrapper(model llms.Model) *LCGModelWrapper {
	return &LCGModelWrapper{
		model: model,
		hooks: NewModelHookRegistry(),
	}
}

// WithHooks sets the model hook registry. Returns the model for chaining.
func (m *LCGModelWrapper) WithHooks(hooks *ModelHookRegistry) *LCGModelWrapper {
	m.hooks = hooks
	return m
}

// RegisterHook adds a hook to the model's hook registry.
// Returns the model for chaining.
func (m *LCGModelWrapper) RegisterHook(hook any) *LCGModelWrapper {
	m.hooks.Register(hook)
	return m
}

// Unwrap returns the underlying llms.Model.
func (m *LCGModelWrapper) Unwrap() llms.Model {
	return m.model
}

// GenerateContent implements Model.GenerateContent.
// It fires BeforeGenerateContent hooks before the call and AfterGenerateContent hooks after.
// Token usage is automatically normalized across providers.
func (m *LCGModelWrapper) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*ContentResponse, error) {
	// Resolve options to get CallOptions struct
	opts := resolveCallOptions(options)

	// Fire before hooks
	beforeEvent := BeforeGenerateContentEvent{
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
	var response *ContentResponse
	if lcgResponse != nil {
		response = convertLCGResponse(lcgResponse, duration)
	}

	// Fire after hooks
	afterEvent := AfterGenerateContentEvent{
		Messages: messages,
		Options:  opts,
		Response: response,
		Error:    err,
	}
	m.hooks.FireAfterGenerateContent(ctx, afterEvent)

	return response, err
}

// convertLCGResponse converts an llms.ContentResponse to our ContentResponse with normalized tokens.
func convertLCGResponse(lcgResponse *llms.ContentResponse, duration time.Duration) *ContentResponse {
	response := &ContentResponse{
		Choices: make([]*ContentChoice, len(lcgResponse.Choices)),
		Info:    &GenerationInfo{Duration: duration},
	}

	// Convert choices
	for i, choice := range lcgResponse.Choices {
		response.Choices[i] = &ContentChoice{
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
		response.Info.TotalTokens = extractTotalTokens(rawInfo, response.Info.InputTokens, response.Info.OutputTokens)
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

// Compile-time check that LCGModelWrapper implements Model.
var _ Model = (*LCGModelWrapper)(nil)
