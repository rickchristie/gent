package models

// GitHubCopilotModel is a model ID string for the GitHub Models API.
// Model IDs use the format "publisher/model-name".
//
// This list may not be exhaustive. To get the full, up-to-date
// catalog, query the GitHub Models REST API:
//
//	curl -H "Authorization: Bearer $GITHUB_TOKEN" \
//	  https://models.github.ai/catalog/models
//
// Each returned object has an "id" field with the model ID string.
// See: https://docs.github.com/en/rest/models/catalog
type GitHubCopilotModel = string

// -------------------------------------------------------------------
// OpenAI (publisher: openai)
// -------------------------------------------------------------------

const (
	// GPT-4.1 family
	GHCopilotGPT41     GitHubCopilotModel = "openai/gpt-4.1"
	GHCopilotGPT41Mini GitHubCopilotModel = "openai/gpt-4.1-mini"
	GHCopilotGPT41Nano GitHubCopilotModel = "openai/gpt-4.1-nano"

	// GPT-4o family
	GHCopilotGPT4o     GitHubCopilotModel = "openai/gpt-4o"
	GHCopilotGPT4oMini GitHubCopilotModel = "openai/gpt-4o-mini"

	// GPT-5 family
	GHCopilotGPT5     GitHubCopilotModel = "openai/gpt-5"
	GHCopilotGPT5Mini GitHubCopilotModel = "openai/gpt-5-mini"
	GHCopilotGPT5Nano GitHubCopilotModel = "openai/gpt-5-nano"

	// OpenAI reasoning models
	GHCopilotO1        GitHubCopilotModel = "openai/o1"
	GHCopilotO1Mini    GitHubCopilotModel = "openai/o1-mini"
	GHCopilotO1Preview GitHubCopilotModel = "openai/o1-preview"
	GHCopilotO3        GitHubCopilotModel = "openai/o3"
	GHCopilotO3Mini    GitHubCopilotModel = "openai/o3-mini"
	GHCopilotO4Mini    GitHubCopilotModel = "openai/o4-mini"
)

// -------------------------------------------------------------------
// Anthropic (publisher: anthropic)
// -------------------------------------------------------------------

const (
	GHCopilotClaude4Opus    GitHubCopilotModel = "anthropic/claude-4-opus"
	GHCopilotClaude4Sonnet  GitHubCopilotModel = "anthropic/claude-4-sonnet"
	GHCopilotClaude37Sonnet GitHubCopilotModel = "anthropic/claude-3.7-sonnet"
	GHCopilotClaude35Sonnet GitHubCopilotModel = "anthropic/claude-3.5-sonnet"
	GHCopilotClaude35Haiku  GitHubCopilotModel = "anthropic/claude-3.5-haiku"
)

// -------------------------------------------------------------------
// Google (publisher: google)
// -------------------------------------------------------------------

const (
	GHCopilotGemini25Pro   GitHubCopilotModel = "google/gemini-2.5-pro"
	GHCopilotGemini25Flash GitHubCopilotModel = "google/gemini-2.5-flash"
	GHCopilotGemini20Flash GitHubCopilotModel = "google/gemini-2.0-flash"
)

// -------------------------------------------------------------------
// Meta Llama (publisher: meta-llama)
// -------------------------------------------------------------------

const (
	GHCopilotLlama33_70B GitHubCopilotModel = "meta-llama/llama-3.3-70b-instruct"

	GHCopilotLlama31_405B GitHubCopilotModel = "meta-llama/meta-llama-3.1-405b-instruct"
	GHCopilotLlama31_8B   GitHubCopilotModel = "meta-llama/meta-llama-3.1-8b-instruct"

	GHCopilotLlama32_11BVision GitHubCopilotModel = "meta-llama/llama-3.2-11b-vision-instruct"
	GHCopilotLlama32_90BVision GitHubCopilotModel = "meta-llama/llama-3.2-90b-vision-instruct"

	GHCopilotLlama4Maverick GitHubCopilotModel = "meta-llama/llama-4-maverick-17b-128e-instruct-fp8"
	GHCopilotLlama4Scout    GitHubCopilotModel = "meta-llama/llama-4-scout-17b-16e-instruct"
)

// -------------------------------------------------------------------
// xAI Grok (publisher: xai)
// -------------------------------------------------------------------

const (
	GHCopilotGrok3     GitHubCopilotModel = "xai/grok-3"
	GHCopilotGrok3Mini GitHubCopilotModel = "xai/grok-3-mini"
)

// -------------------------------------------------------------------
// DeepSeek (publisher: deepseek)
// -------------------------------------------------------------------

const (
	GHCopilotDeepSeekR1     GitHubCopilotModel = "deepseek/deepseek-r1"
	GHCopilotDeepSeekR1_528 GitHubCopilotModel = "deepseek/deepseek-r1-0528"
	GHCopilotDeepSeekV3     GitHubCopilotModel = "deepseek/deepseek-v3-0324"
)

// -------------------------------------------------------------------
// Mistral AI (publisher: mistralai)
// -------------------------------------------------------------------

const (
	GHCopilotMistralMedium GitHubCopilotModel = "mistralai/mistral-medium-3"
	GHCopilotMistralSmall  GitHubCopilotModel = "mistralai/mistral-small-3.1"
	GHCopilotMinistral3B   GitHubCopilotModel = "mistralai/ministral-3b"
)

// -------------------------------------------------------------------
// Cohere (publisher: cohere)
// -------------------------------------------------------------------

const (
	GHCopilotCohereCommandA GitHubCopilotModel = "cohere/command-a"
)

// -------------------------------------------------------------------
// Microsoft Phi (publisher: azureml)
// -------------------------------------------------------------------

const (
	GHCopilotPhi4              GitHubCopilotModel = "azureml/Phi-4"
	GHCopilotPhi4Mini          GitHubCopilotModel = "azureml/Phi-4-mini-instruct"
	GHCopilotPhi4Multimodal    GitHubCopilotModel = "azureml/Phi-4-multimodal-instruct"
	GHCopilotPhi4Reasoning     GitHubCopilotModel = "azureml/Phi-4-reasoning"
	GHCopilotPhi4MiniReasoning GitHubCopilotModel = "azureml/Phi-4-mini-reasoning"
)

// -------------------------------------------------------------------
// AI21 Labs (publisher: ai21)
// -------------------------------------------------------------------

const (
	GHCopilotJamba15Large GitHubCopilotModel = "ai21/jamba-1-5-large"
)
