package gent

// =============================================================================
// OpenAI Models
// https://platform.openai.com/docs/models/
// =============================================================================

const (
	// GPT-5.5 Series (Latest)
	ModelOpenAIGPT55 = "gpt-5.5"

	// GPT-5.4 Series
	ModelOpenAIGPT54     = "gpt-5.4"
	ModelOpenAIGPT54Mini = "gpt-5.4-mini"
	ModelOpenAIGPT54Nano = "gpt-5.4-nano"

	// GPT-5 Series
	ModelOpenAIGPT52      = "gpt-5.2"
	ModelOpenAIGPT52Codex = "gpt-5.2-codex"
	ModelOpenAIGPT5       = "gpt-5"
	ModelOpenAIGPT5Mini   = "gpt-5-mini"
	ModelOpenAIGPT5Nano   = "gpt-5-nano"

	// GPT-4.1 Series
	ModelOpenAIGPT41     = "gpt-4.1"
	ModelOpenAIGPT41Mini = "gpt-4.1-mini"
	ModelOpenAIGPT41Nano = "gpt-4.1-nano"

	// GPT-4o Series
	ModelOpenAIGPT4o      = "gpt-4o"
	ModelOpenAIGPT4oMini  = "gpt-4o-mini"
	ModelOpenAIGPT4oAudio = "gpt-4o-audio-preview"

	// O-Series (Reasoning Models)
	ModelOpenAIO3     = "o3"
	ModelOpenAIO3Pro  = "o3-pro"
	ModelOpenAIO4Mini = "o4-mini"
	ModelOpenAIO1     = "o1"
	ModelOpenAIO1Mini = "o1-mini"
)

// =============================================================================
// Anthropic Claude Models
// https://docs.anthropic.com/en/docs/about-claude/models/overview
// =============================================================================

const (
	// Claude 4.6 Series (Latest)
	ModelAnthropicClaude46Opus   = "claude-opus-4-6"
	ModelAnthropicClaude46Sonnet = "claude-sonnet-4-6"

	// Claude 4.5 Series
	ModelAnthropicClaude45Opus   = "claude-opus-4-5-20251101"
	ModelAnthropicClaude45Sonnet = "claude-sonnet-4-5-20250929"
	ModelAnthropicClaude45Haiku  = "claude-haiku-4-5-20251001"

	// Claude 4.x Series (Legacy)
	ModelAnthropicClaude41Opus  = "claude-opus-4-1-20250805"
	ModelAnthropicClaude4Opus   = "claude-opus-4-20250514"
	ModelAnthropicClaude4Sonnet = "claude-sonnet-4-20250514"
)

// =============================================================================
// Google Gemini Models
// https://ai.google.dev/gemini-api/docs/models
// =============================================================================

const (
	// Gemini 3.1 Series (Latest)
	ModelGoogleGemini31Pro       = "gemini-3.1-pro-preview"
	ModelGoogleGemini31FlashLite = "gemini-3.1-flash-lite-preview"

	// Gemini 3 Series
	ModelGoogleGemini3Pro   = "gemini-3-pro-preview"
	ModelGoogleGemini3Flash = "gemini-3-flash-preview"

	// Gemini 2.5 Series
	ModelGoogleGemini25Pro       = "gemini-2.5-pro"
	ModelGoogleGemini25Flash     = "gemini-2.5-flash"
	ModelGoogleGemini25FlashLite = "gemini-2.5-flash-lite"
)

// =============================================================================
// xAI Grok Models
// https://docs.x.ai/docs/models
// =============================================================================

const (
	// Grok 4.3 Series (Latest)
	ModelXAIGrok43       = "grok-4.3"
	ModelXAIGrok43Latest = "grok-4.3-latest"
	ModelXAIGrokLatest   = "grok-latest"

	// Grok 4.20 Series
	ModelXAIGrok420Reasoning    = "grok-4.20-0309-reasoning"
	ModelXAIGrok420NonReasoning = "grok-4.20-0309-non-reasoning"
	ModelXAIGrok420MultiAgent   = "grok-4.20-multi-agent-0309"

	// Grok 4.1 Series (retiring May 15, 2026)
	ModelXAIGrok41Fast             = "grok-4-1-fast"
	ModelXAIGrok41FastReasoning    = "grok-4-1-fast-reasoning"
	ModelXAIGrok41FastNonReasoning = "grok-4-1-fast-non-reasoning"

	// Grok 4 Series
	ModelXAIGrok4FastReasoning    = "grok-4-fast-reasoning"
	ModelXAIGrok4FastNonReasoning = "grok-4-fast-non-reasoning"
	ModelXAIGrok4                 = "grok-4"

	// Grok 3 Series
	ModelXAIGrok3     = "grok-3"
	ModelXAIGrok3Mini = "grok-3-mini"

	// Specialized Models
	ModelXAIGrokCodeFast1 = "grok-code-fast-1"
	ModelXAIGrok2Vision   = "grok-2-vision-1212"
	ModelXAIGrok2Image    = "grok-2-image-1212"
)

// =============================================================================
// Mistral AI Models
// https://docs.mistral.ai/getting-started/models
// =============================================================================

const (
	// Large Models
	ModelMistralLarge3  = "mistral-large-latest"
	ModelMistralMedium3 = "mistral-medium-latest"
	ModelMistralSmall   = "mistral-small-latest"

	// Multimodal
	ModelMistralPixtralLarge = "pixtral-large-latest"

	// Reasoning Models
	ModelMistralMagistral       = "magistral-latest"
	ModelMistralMagistralMedium = "magistral-medium-latest"
	ModelMistralMagistralSmall  = "magistral-small-latest"

	// Code Models
	ModelMistralCodestral = "codestral-latest"
	ModelMistralDevstral  = "devstral-latest"

	// Edge Models
	ModelMistralMinistral3B = "ministral-3b-latest"
	ModelMistralMinistral8B = "ministral-8b-latest"
)

// =============================================================================
// Meta Llama Models
// https://llama.developer.meta.com/docs/models/
// =============================================================================

const (
	// Llama 4 Series (Latest)
	ModelMetaLlama4Maverick = "llama-4-maverick"
	ModelMetaLlama4Scout    = "llama-4-scout"
	ModelMetaLlama4Behemoth = "llama-4-behemoth"

	// Llama 3.3 Series
	ModelMetaLlama33_70B = "llama-3.3-70b-instruct"

	// Llama 3.2 Series
	ModelMetaLlama32_90BVision = "llama-3.2-90b-vision-instruct"
	ModelMetaLlama32_11BVision = "llama-3.2-11b-vision-instruct"
	ModelMetaLlama32_3B        = "llama-3.2-3b-instruct"
	ModelMetaLlama32_1B        = "llama-3.2-1b-instruct"
)

// =============================================================================
// DeepSeek Models
// https://api-docs.deepseek.com/
// =============================================================================

const (
	// Main API Models
	ModelDeepSeekChat     = "deepseek-chat"     // DeepSeek-V3.2 non-thinking mode
	ModelDeepSeekReasoner = "deepseek-reasoner" // DeepSeek-V3.2 thinking mode

	// Versioned Models
	ModelDeepSeekV32    = "deepseek-v3.2"
	ModelDeepSeekV3     = "deepseek-v3"
	ModelDeepSeekR1     = "deepseek-r1"
	ModelDeepSeekR10528 = "deepseek-r1-0528"
)
