package testutil

import (
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestDefaultModelName(t *testing.T) {
	assert.Equal(t, gent.ModelXAIGrok43, DefaultModelName)
}

func TestAvailableModels_ProviderOptions(t *testing.T) {
	type input struct {
		envKey string
	}
	type expected struct {
		models []ModelOption
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}

	testCases := []testCase{
		{
			name:  "xai models exclude retired grok 4.1 ids",
			input: input{envKey: envKeyXAI},
			expected: expected{models: []ModelOption{
				{
					Label: "xAI grok-4.3", Name: gent.ModelXAIGrok43,
					EnvKey: envKeyXAI, BaseURL: baseURLXAI,
				},
				{
					Label:  "xAI grok-4.20-0309-reasoning",
					Name:   gent.ModelXAIGrok420Reasoning,
					EnvKey: envKeyXAI, BaseURL: baseURLXAI,
				},
				{
					Label:  "xAI grok-4.20-0309-non-reasoning",
					Name:   gent.ModelXAIGrok420NonReasoning,
					EnvKey: envKeyXAI, BaseURL: baseURLXAI,
				},
				{
					Label:  "xAI grok-4.20-multi-agent-0309",
					Name:   gent.ModelXAIGrok420MultiAgent,
					EnvKey: envKeyXAI, BaseURL: baseURLXAI,
				},
			}},
		},
		{
			name:  "openai models include latest gpt 5.5",
			input: input{envKey: envKeyOpenAI},
			expected: expected{models: []ModelOption{
				{Label: "OpenAI o3", Name: gent.ModelOpenAIO3, EnvKey: envKeyOpenAI},
				{Label: "OpenAI o4-mini", Name: gent.ModelOpenAIO4Mini, EnvKey: envKeyOpenAI},
				{
					Label: "OpenAI gpt-4.1", Name: gent.ModelOpenAIGPT41,
					EnvKey: envKeyOpenAI,
				},
				{
					Label: "OpenAI gpt-4.1-mini", Name: gent.ModelOpenAIGPT41Mini,
					EnvKey: envKeyOpenAI,
				},
				{Label: "OpenAI gpt-5", Name: gent.ModelOpenAIGPT5, EnvKey: envKeyOpenAI},
				{
					Label: "OpenAI gpt-5-mini", Name: gent.ModelOpenAIGPT5Mini,
					EnvKey: envKeyOpenAI,
				},
				{
					Label: "OpenAI gpt-5-nano", Name: gent.ModelOpenAIGPT5Nano,
					EnvKey: envKeyOpenAI,
				},
				{
					Label: "OpenAI gpt-5.5", Name: gent.ModelOpenAIGPT55,
					EnvKey: envKeyOpenAI,
				},
				{
					Label: "OpenAI gpt-5.4", Name: gent.ModelOpenAIGPT54,
					EnvKey: envKeyOpenAI,
				},
				{
					Label: "OpenAI gpt-5.4-mini", Name: gent.ModelOpenAIGPT54Mini,
					EnvKey: envKeyOpenAI,
				},
				{
					Label: "OpenAI gpt-5.4-nano", Name: gent.ModelOpenAIGPT54Nano,
					EnvKey: envKeyOpenAI,
				},
			}},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected.models, modelOptionsForEnvKey(tc.input.envKey))
		})
	}
}

func modelOptionsForEnvKey(envKey string) []ModelOption {
	models := make([]ModelOption, 0)
	for _, model := range AvailableModels {
		if model.EnvKey != envKey {
			continue
		}
		models = append(models, model)
	}
	return models
}
