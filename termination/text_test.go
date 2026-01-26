package termination

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tmc/langchaingo/llms"
)

func TestText_Name(t *testing.T) {
	type input struct {
		sectionName string
	}

	type expected struct {
		name string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name:     "returns provided name",
			input:    input{sectionName: "answer"},
			expected: expected{name: "answer"},
		},
		{
			name:     "custom name",
			input:    input{sectionName: "result"},
			expected: expected{name: "result"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			term := NewText(tt.input.sectionName)

			assert.Equal(t, tt.expected.name, term.Name())
		})
	}
}

func TestText_Prompt(t *testing.T) {
	type input struct {
		customPrompt string
	}

	type expected struct {
		prompt string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name:     "default prompt",
			input:    input{customPrompt: ""},
			expected: expected{prompt: "Write your final answer here."},
		},
		{
			name:     "custom prompt",
			input:    input{customPrompt: "Custom prompt"},
			expected: expected{prompt: "Custom prompt"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			term := NewText("answer")
			if tt.input.customPrompt != "" {
				term.WithPrompt(tt.input.customPrompt)
			}

			assert.Equal(t, tt.expected.prompt, term.Prompt())
		})
	}
}

func TestText_ParseSection(t *testing.T) {
	type input struct {
		content string
	}

	type expected struct {
		result string
		err    error
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name:     "simple content",
			input:    input{content: "The weather is sunny today."},
			expected: expected{result: "The weather is sunny today.", err: nil},
		},
		{
			name:     "content with whitespace trimming",
			input:    input{content: "   Content with whitespace   "},
			expected: expected{result: "Content with whitespace", err: nil},
		},
		{
			name:     "empty content",
			input:    input{content: ""},
			expected: expected{result: "", err: nil},
		},
		{
			name:  "multiline content",
			input: input{content: "Line 1\nLine 2\nLine 3"},
			expected: expected{
				result: "Line 1\nLine 2\nLine 3",
				err:    nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			term := NewText("answer")

			result, err := term.ParseSection(nil, tt.input.content)

			assert.ErrorIs(t, err, tt.expected.err)
			assert.Equal(t, tt.expected.result, result)
		})
	}
}

func TestText_ShouldTerminate(t *testing.T) {
	type input struct {
		content string
	}

	type expected struct {
		shouldTerminate bool
		resultText      string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name:  "non-empty content terminates",
			input: input{content: "The final answer."},
			expected: expected{
				shouldTerminate: true,
				resultText:      "The final answer.",
			},
		},
		{
			name:  "empty content does not terminate",
			input: input{content: ""},
			expected: expected{
				shouldTerminate: false,
				resultText:      "",
			},
		},
		{
			name:  "whitespace only does not terminate",
			input: input{content: "   "},
			expected: expected{
				shouldTerminate: false,
				resultText:      "",
			},
		},
		{
			name:  "content with surrounding whitespace is trimmed",
			input: input{content: "  trimmed content  "},
			expected: expected{
				shouldTerminate: true,
				resultText:      "trimmed content",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			term := NewText("answer")

			result := term.ShouldTerminate(tt.input.content)

			if tt.expected.shouldTerminate {
				assert.NotNil(t, result)
				assert.Len(t, result, 1)
				tc, ok := result[0].(llms.TextContent)
				assert.True(t, ok, "expected TextContent, got %T", result[0])
				assert.Equal(t, tt.expected.resultText, tc.Text)
			} else {
				assert.Nil(t, result)
			}
		})
	}
}
