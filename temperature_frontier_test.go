package gent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultSamplingParams_FrontierModels(t *testing.T) {
	type input struct {
		modelName string
	}
	type expected struct {
		params SamplingParams
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	forbidden := expected{params: SamplingParams{
		Temperature: SamplingParam{Directive: ParamForbidden},
		TopP:        SamplingParam{Directive: ParamForbidden},
	}}
	grok := expected{params: SamplingParams{
		Temperature: SamplingParam{Directive: ParamOverride, Value: 0.7},
		TopP:        SamplingParam{Directive: ParamOverride, Value: 0.95},
	}}
	gemini := expected{params: SamplingParams{
		Temperature: SamplingParam{Directive: ParamOverride, Value: 1},
		TopP:        SamplingParam{Directive: ParamOmit},
	}}
	cases := []testCase{
		{"Grok", input{ModelXAIGrok47}, grok},
		{"Grok with provider prefix", input{"xai/grok-4.7"}, grok},
		{"OpenAI", input{ModelOpenAIGPT6Astra}, forbidden},
		{"OpenAI with provider prefix", input{"openai/gpt-6-astra"}, forbidden},
		{"Google", input{ModelGoogleGemini4Argon}, gemini},
		{"Google with provider prefix", input{"google/gemini-4-argon"}, gemini},
		{"Anthropic", input{ModelAnthropicClaude51Fable}, forbidden},
		{"Anthropic with provider prefix", input{"anthropic/claude-fable-5-1"}, forbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params := DefaultSamplingParams(&mockNamerModel{name: tc.input.modelName})
			assert.Equal(t, tc.expected.params, params)
		})
	}
}
