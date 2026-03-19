package gent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tmc/langchaingo/llms"
)

// mockNamerModel implements both Model and ModelNamer.
type mockNamerModel struct {
	name string
}

func (m *mockNamerModel) ModelName() string { return m.name }

func (m *mockNamerModel) GenerateContent(
	_ *ExecutionContext, _ string, _ string,
	_ []llms.MessageContent, _ ...llms.CallOption,
) (*ContentResponse, error) {
	return nil, nil
}

// mockPlainModel implements Model but not ModelNamer.
type mockPlainModel struct{}

func (m *mockPlainModel) GenerateContent(
	_ *ExecutionContext, _ string, _ string,
	_ []llms.MessageContent, _ ...llms.CallOption,
) (*ContentResponse, error) {
	return nil, nil
}

func TestDefaultSamplingParams(t *testing.T) {
	type input struct {
		model Model
	}

	type expectedParam struct {
		directive ParamDirective
		value     float64
	}

	type expected struct {
		temperature expectedParam
		topP        expectedParam
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		// --- Generic defaults ---
		{
			name:  "model without ModelNamer returns generic defaults",
			input: input{model: &mockPlainModel{}},
			expected: expected{
				temperature: expectedParam{directive: ParamOverride, value: 0.2},
				topP:        expectedParam{directive: ParamOverride, value: 1.0},
			},
		},
		{
			name:  "empty model name returns generic defaults",
			input: input{model: &mockNamerModel{name: ""}},
			expected: expected{
				temperature: expectedParam{directive: ParamOverride, value: 0.2},
				topP:        expectedParam{directive: ParamOverride, value: 1.0},
			},
		},
		{
			name:  "unknown model returns generic defaults",
			input: input{model: &mockNamerModel{name: "some-custom-model"}},
			expected: expected{
				temperature: expectedParam{directive: ParamOverride, value: 0.2},
				topP:        expectedParam{directive: ParamOverride, value: 1.0},
			},
		},

		// --- Claude (generic defaults) ---
		{
			name:  "claude uses generic defaults",
			input: input{model: &mockNamerModel{name: "claude-4-sonnet"}},
			expected: expected{
				temperature: expectedParam{directive: ParamOverride, value: 0.2},
				topP:        expectedParam{directive: ParamOverride, value: 1.0},
			},
		},
		{
			name:  "anthropic prefixed claude uses generic defaults",
			input: input{model: &mockNamerModel{name: "anthropic/claude-4-opus"}},
			expected: expected{
				temperature: expectedParam{directive: ParamOverride, value: 0.2},
				topP:        expectedParam{directive: ParamOverride, value: 1.0},
			},
		},

		// --- GPT non-reasoning (generic defaults) ---
		{
			name:  "gpt-4.1 uses generic defaults",
			input: input{model: &mockNamerModel{name: "gpt-4.1"}},
			expected: expected{
				temperature: expectedParam{directive: ParamOverride, value: 0.2},
				topP:        expectedParam{directive: ParamOverride, value: 1.0},
			},
		},
		{
			name:  "gpt-4o uses generic defaults",
			input: input{model: &mockNamerModel{name: "openai/gpt-4o"}},
			expected: expected{
				temperature: expectedParam{directive: ParamOverride, value: 0.2},
				topP:        expectedParam{directive: ParamOverride, value: 1.0},
			},
		},

		// --- Grok (generic defaults) ---
		{
			name:  "grok uses generic defaults",
			input: input{model: &mockNamerModel{name: "grok-4-1-fast"}},
			expected: expected{
				temperature: expectedParam{directive: ParamOverride, value: 0.2},
				topP:        expectedParam{directive: ParamOverride, value: 1.0},
			},
		},

		// --- OpenAI reasoning (both forbidden) ---
		{
			name:  "o1 has both params forbidden",
			input: input{model: &mockNamerModel{name: "o1"}},
			expected: expected{
				temperature: expectedParam{directive: ParamForbidden, value: 0},
				topP:        expectedParam{directive: ParamForbidden, value: 0},
			},
		},
		{
			name:  "o1-mini has both params forbidden",
			input: input{model: &mockNamerModel{name: "o1-mini"}},
			expected: expected{
				temperature: expectedParam{directive: ParamForbidden, value: 0},
				topP:        expectedParam{directive: ParamForbidden, value: 0},
			},
		},
		{
			name:  "o3 has both params forbidden",
			input: input{model: &mockNamerModel{name: "o3"}},
			expected: expected{
				temperature: expectedParam{directive: ParamForbidden, value: 0},
				topP:        expectedParam{directive: ParamForbidden, value: 0},
			},
		},
		{
			name:  "o3-mini has both params forbidden",
			input: input{model: &mockNamerModel{name: "o3-mini"}},
			expected: expected{
				temperature: expectedParam{directive: ParamForbidden, value: 0},
				topP:        expectedParam{directive: ParamForbidden, value: 0},
			},
		},
		{
			name:  "o4-mini has both params forbidden",
			input: input{model: &mockNamerModel{name: "o4-mini"}},
			expected: expected{
				temperature: expectedParam{directive: ParamForbidden, value: 0},
				topP:        expectedParam{directive: ParamForbidden, value: 0},
			},
		},
		{
			name:  "openai prefixed o3 has both params forbidden",
			input: input{model: &mockNamerModel{name: "openai/o3"}},
			expected: expected{
				temperature: expectedParam{directive: ParamForbidden, value: 0},
				topP:        expectedParam{directive: ParamForbidden, value: 0},
			},
		},
		{
			name:  "gpt-5 has both params forbidden",
			input: input{model: &mockNamerModel{name: "gpt-5"}},
			expected: expected{
				temperature: expectedParam{directive: ParamForbidden, value: 0},
				topP:        expectedParam{directive: ParamForbidden, value: 0},
			},
		},
		{
			name:  "gpt-5-mini has both params forbidden",
			input: input{model: &mockNamerModel{name: "openai/gpt-5-mini"}},
			expected: expected{
				temperature: expectedParam{directive: ParamForbidden, value: 0},
				topP:        expectedParam{directive: ParamForbidden, value: 0},
			},
		},

		// --- Gemini 3+ (temp override, top-p omit) ---
		{
			name:  "gemini-3 has temp 1.0 and top-p omitted",
			input: input{model: &mockNamerModel{name: "gemini-3"}},
			expected: expected{
				temperature: expectedParam{directive: ParamOverride, value: 1.0},
				topP:        expectedParam{directive: ParamOmit, value: 0},
			},
		},
		{
			name:  "gemini-3.5-pro has temp 1.0 and top-p omitted",
			input: input{model: &mockNamerModel{name: "google/gemini-3.5-pro"}},
			expected: expected{
				temperature: expectedParam{directive: ParamOverride, value: 1.0},
				topP:        expectedParam{directive: ParamOmit, value: 0},
			},
		},
		{
			name:  "gemini-2.5-pro uses generic defaults",
			input: input{model: &mockNamerModel{name: "google/gemini-2.5-pro"}},
			expected: expected{
				temperature: expectedParam{directive: ParamOverride, value: 0.2},
				topP:        expectedParam{directive: ParamOverride, value: 1.0},
			},
		},
		{
			name:  "gemini-2.0-flash uses generic defaults",
			input: input{model: &mockNamerModel{name: "gemini-2.0-flash"}},
			expected: expected{
				temperature: expectedParam{directive: ParamOverride, value: 0.2},
				topP:        expectedParam{directive: ParamOverride, value: 1.0},
			},
		},

		// --- DeepSeek-R1 (both override) ---
		{
			name:  "deepseek-r1 has temp 0.6 and top-p 0.95",
			input: input{model: &mockNamerModel{name: "deepseek-r1"}},
			expected: expected{
				temperature: expectedParam{directive: ParamOverride, value: 0.6},
				topP:        expectedParam{directive: ParamOverride, value: 0.95},
			},
		},
		{
			name:  "deepseek prefixed r1 has temp 0.6 and top-p 0.95",
			input: input{model: &mockNamerModel{name: "deepseek/deepseek-r1-0528"}},
			expected: expected{
				temperature: expectedParam{directive: ParamOverride, value: 0.6},
				topP:        expectedParam{directive: ParamOverride, value: 0.95},
			},
		},
		{
			name:  "deepseek-v3 uses generic defaults",
			input: input{model: &mockNamerModel{name: "deepseek/deepseek-v3-0324"}},
			expected: expected{
				temperature: expectedParam{directive: ParamOverride, value: 0.2},
				topP:        expectedParam{directive: ParamOverride, value: 1.0},
			},
		},

		// --- Qwen3 (both override) ---
		{
			name:  "qwen3 has temp 0.6 and top-p 0.95",
			input: input{model: &mockNamerModel{name: "qwen3-235b"}},
			expected: expected{
				temperature: expectedParam{directive: ParamOverride, value: 0.6},
				topP:        expectedParam{directive: ParamOverride, value: 0.95},
			},
		},
		{
			name:  "qwen-3 hyphenated has temp 0.6 and top-p 0.95",
			input: input{model: &mockNamerModel{name: "qwen-3-72b"}},
			expected: expected{
				temperature: expectedParam{directive: ParamOverride, value: 0.6},
				topP:        expectedParam{directive: ParamOverride, value: 0.95},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := DefaultSamplingParams(tc.input.model)
			assert.Equal(t, tc.expected.temperature.directive,
				result.Temperature.Directive, "temperature directive")
			assert.InDelta(t, tc.expected.temperature.value,
				result.Temperature.Value, 0.001, "temperature value")
			assert.Equal(t, tc.expected.topP.directive,
				result.TopP.Directive, "top-p directive")
			assert.InDelta(t, tc.expected.topP.value,
				result.TopP.Value, 0.001, "top-p value")
		})
	}
}

// TestDefaultTemperature verifies the backward-compatible wrapper still works correctly.
func TestDefaultTemperature(t *testing.T) {
	type input struct {
		model Model
	}

	type expected struct {
		temperature float64
		supported   bool
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name:     "generic model returns 0.2 true",
			input:    input{model: &mockPlainModel{}},
			expected: expected{temperature: 0.2, supported: true},
		},
		{
			name:     "reasoning model returns 0 false",
			input:    input{model: &mockNamerModel{name: "o3"}},
			expected: expected{temperature: 0, supported: false},
		},
		{
			name:     "gemini-3 returns 1.0 true",
			input:    input{model: &mockNamerModel{name: "gemini-3"}},
			expected: expected{temperature: 1.0, supported: true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			temp, supported := DefaultTemperature(tc.input.model)
			assert.InDelta(t, tc.expected.temperature, temp, 0.001)
			assert.Equal(t, tc.expected.supported, supported)
		})
	}
}
