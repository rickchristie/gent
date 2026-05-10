package models

import (
	"context"
	"net/http"
	"os"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/anthropic"
	"github.com/tmc/langchaingo/llms/openai"
)

// TestSmokeAllProviders sends "Hello World!" to one representative model per
// provider and verifies a non-empty streaming response. Every subtest is
// skipped by default — remove the t.Skip() line for the provider you want to
// verify manually (e.g., after updating langchaingo or changing sampling
// params).
//
// Environment variables required per provider:
//
//	GENT_TEST_XAI_KEY       — xAI (Grok)
//	GENT_TEST_OPENAI_KEY    — OpenAI (GPT, o-series)
//	GENT_TEST_ANTHROPIC_KEY — Anthropic (Claude)
//	GENT_TEST_GEMINI_KEY    — Google (Gemini)
func TestSmokeAllProviders(t *testing.T) {
	type testCase struct {
		name    string
		envKey  string
		modelID string
		setup   func(t *testing.T, apiKey string) gent.StreamingModel
	}

	// openAICompat creates a model using the OpenAI-compatible client with an
	// optional base URL override.
	openAICompat := func(
		modelID, baseURL string,
	) func(*testing.T, string) gent.StreamingModel {
		return func(t *testing.T, apiKey string) gent.StreamingModel {
			opts := []openai.Option{
				openai.WithToken(apiKey),
				openai.WithModel(modelID),
				openai.WithHTTPClient(&http.Client{
					Transport: &ErrorCaptureTransport{},
				}),
			}
			if baseURL != "" {
				opts = append(opts, openai.WithBaseURL(baseURL))
			}
			llm, err := openai.New(opts...)
			require.NoError(t, err)
			return NewLCGWrapper(llm).WithModelName(modelID)
		}
	}

	// anthropicClient creates a model using the native Anthropic client.
	anthropicClient := func(
		modelID string,
	) func(*testing.T, string) gent.StreamingModel {
		return func(t *testing.T, apiKey string) gent.StreamingModel {
			llm, err := anthropic.New(
				anthropic.WithToken(apiKey),
				anthropic.WithModel(modelID),
				anthropic.WithHTTPClient(&http.Client{
					Transport: &ErrorCaptureTransport{},
				}),
			)
			require.NoError(t, err)
			return NewLCGWrapper(llm).WithModelName(modelID)
		}
	}

	const (
		baseURLXAI    = "https://api.x.ai/v1"
		baseURLGemini = "https://generativelanguage.googleapis.com/v1beta/openai/"
	)

	tests := []testCase{
		// ---- xAI Grok ----
		{
			name:    "xai/" + gent.ModelXAIGrok43,
			envKey:  "GENT_TEST_XAI_KEY",
			modelID: gent.ModelXAIGrok43,
			setup:   openAICompat(gent.ModelXAIGrok43, baseURLXAI),
		},
		{
			name:    "xai/" + gent.ModelXAIGrok420Reasoning,
			envKey:  "GENT_TEST_XAI_KEY",
			modelID: gent.ModelXAIGrok420Reasoning,
			setup:   openAICompat(gent.ModelXAIGrok420Reasoning, baseURLXAI),
		},

		// ---- OpenAI ----
		{
			name:    "openai/" + gent.ModelOpenAIGPT41Mini,
			envKey:  "GENT_TEST_OPENAI_KEY",
			modelID: gent.ModelOpenAIGPT41Mini,
			setup:   openAICompat(gent.ModelOpenAIGPT41Mini, ""),
		},
		{
			name:    "openai/" + gent.ModelOpenAIO4Mini,
			envKey:  "GENT_TEST_OPENAI_KEY",
			modelID: gent.ModelOpenAIO4Mini,
			setup:   openAICompat(gent.ModelOpenAIO4Mini, ""),
		},

		// ---- Anthropic Claude ----
		{
			name:    "anthropic/" + gent.ModelAnthropicClaude45Haiku,
			envKey:  "GENT_TEST_ANTHROPIC_KEY",
			modelID: gent.ModelAnthropicClaude45Haiku,
			setup:   anthropicClient(gent.ModelAnthropicClaude45Haiku),
		},
		{
			name:    "anthropic/" + gent.ModelAnthropicClaude46Sonnet,
			envKey:  "GENT_TEST_ANTHROPIC_KEY",
			modelID: gent.ModelAnthropicClaude46Sonnet,
			setup:   anthropicClient(gent.ModelAnthropicClaude46Sonnet),
		},

		// ---- Google Gemini ----
		{
			name:    "google/" + gent.ModelGoogleGemini25Flash,
			envKey:  "GENT_TEST_GEMINI_KEY",
			modelID: gent.ModelGoogleGemini25Flash,
			setup:   openAICompat(gent.ModelGoogleGemini25Flash, baseURLGemini),
		},
		{
			name:    "google/" + gent.ModelGoogleGemini3Flash,
			envKey:  "GENT_TEST_GEMINI_KEY",
			modelID: gent.ModelGoogleGemini3Flash,
			setup:   openAICompat(gent.ModelGoogleGemini3Flash, baseURLGemini),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if os.Getenv("GENT_SMOKE_TEST") == "" {
				t.Skip("set GENT_SMOKE_TEST=1 to run")
			}

			apiKey := os.Getenv(tt.envKey)
			if apiKey == "" {
				t.Skipf("%s not set", tt.envKey)
			}

			model := tt.setup(t, apiKey)
			ctx := context.Background()
			execCtx := gent.NewExecutionContext(ctx, "smoke-test", nil)

			// Apply model-appropriate sampling defaults (e.g., send 1.0
			// for reasoning models, omit top_p for Claude).
			params := gent.DefaultSamplingParams(model)
			var callOpts []llms.CallOption
			if v, ok := params.Temperature.EffectiveValue(); ok {
				callOpts = append(callOpts, llms.WithTemperature(v))
			}
			if v, ok := params.TopP.EffectiveValue(); ok {
				callOpts = append(callOpts, llms.WithTopP(v))
			}

			stream, err := model.GenerateContentStream(
				execCtx, "", "", []llms.MessageContent{
					llms.TextParts(
						llms.ChatMessageTypeHuman,
						"Hello World! Reply with a short greeting.",
					),
				},
				callOpts...,
			)
			require.NoError(t, err, "failed to start stream")

			response, err := stream.Response()
			require.NoError(t, err, "stream failed for %s", tt.modelID)
			require.NotEmpty(t, response.Choices, "expected non-empty choices")

			content := response.Choices[0].Content
			assert.NotEmpty(t, content, "expected non-empty response content")
			t.Logf("[%s] response: %s", tt.modelID, content)

			if response.Info != nil {
				t.Logf(
					"[%s] tokens: input=%d output=%d total=%d",
					tt.modelID,
					response.Info.InputTokens,
					response.Info.OutputTokens,
					response.Info.TotalTokens,
				)
			}
		})
	}
}
