package testutil

import (
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestDefaultModelName(t *testing.T) {
	assert.Equal(t, "grok-4.7", DefaultModelName)
}

func TestAvailableModels(t *testing.T) {
	// Compare the complete menu so older, duplicate, or extra entries cannot return unnoticed.
	expected := []ModelOption{
		{
			Label: "xAI grok-4.7", Name: "grok-4.7",
			EnvKey: "GENT_TEST_XAI_KEY", BaseURL: "https://api.x.ai/v1",
		},
		{
			Label: "OpenAI gpt-6-astra", Name: "gpt-6-astra",
			EnvKey: "GENT_TEST_OPENAI_KEY",
		},
		{
			Label: "Google gemini-4-argon", Name: "gemini-4-argon",
			EnvKey:  "GENT_TEST_GEMINI_KEY",
			BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai/",
		},
		{
			Label: "Anthropic claude-fable-5-1", Name: "claude-fable-5-1",
			EnvKey: "GENT_TEST_ANTHROPIC_KEY",
		},
	}
	assert.Equal(t, expected, AvailableModels)
}

func TestCreateModel_MissingKey(t *testing.T) {
	type input struct {
		modelName string
	}
	type expected struct {
		model gent.Model
		err   string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"default", input{}, expected{err: "GENT_TEST_XAI_KEY environment variable not set"}},
		{"xAI", input{"grok-4.7"}, expected{err: "GENT_TEST_XAI_KEY environment variable not set"}},
		{"OpenAI", input{"gpt-6-astra"}, expected{
			err: "GENT_TEST_OPENAI_KEY environment variable not set",
		}},
		{"Google", input{"gemini-4-argon"}, expected{
			err: "GENT_TEST_GEMINI_KEY environment variable not set",
		}},
		{"Anthropic", input{"claude-fable-5-1"}, expected{
			err: "GENT_TEST_ANTHROPIC_KEY environment variable not set",
		}},
	}
	for _, key := range []string{envKeyXAI, envKeyOpenAI, envKeyGemini, envKeyAnthropic} {
		t.Setenv(key, "")
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model, err := CreateModel(tc.input.modelName)
			assert.Equal(t, tc.expected.model, model)
			assert.EqualError(t, err, tc.expected.err)
		})
	}
}
