package section

import (
	"context"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestText_Name(t *testing.T) {
	tests := []struct {
		name     string
		input    func() *Text
		expected string
	}{
		{
			name: "default name",
			input: func() *Text {
				return NewText()
			},
			expected: "text",
		},
		{
			name: "custom name",
			input: func() *Text {
				return NewText().WithSectionName("thinking")
			},
			expected: "thinking",
		},
		{
			name: "empty name",
			input: func() *Text {
				return NewText().WithSectionName("")
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			section := tt.input()
			assert.Equal(t, tt.expected, section.Name())
		})
	}
}

func TestText_Prompt(t *testing.T) {
	tests := []struct {
		name     string
		input    func() *Text
		expected string
	}{
		{
			name: "default prompt",
			input: func() *Text {
				return NewText()
			},
			expected: "Write your response here.",
		},
		{
			name: "custom prompt",
			input: func() *Text {
				return NewText().WithPrompt("Think step by step about the problem.")
			},
			expected: "Think step by step about the problem.",
		},
		{
			name: "empty prompt",
			input: func() *Text {
				return NewText().WithPrompt("")
			},
			expected: "",
		},
		{
			name: "multiline prompt",
			input: func() *Text {
				return NewText().WithPrompt("Line 1\nLine 2\nLine 3")
			},
			expected: "Line 1\nLine 2\nLine 3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			section := tt.input()
			assert.Equal(t, tt.expected, section.Prompt())
		})
	}
}

func TestText_ParseSection(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected struct {
			result string
			err    error
		}
	}{
		{
			name:  "simple text",
			input: "Hello, world!",
			expected: struct {
				result string
				err    error
			}{
				result: "Hello, world!",
				err:    nil,
			},
		},
		{
			name:  "text with leading/trailing whitespace",
			input: "   Hello, world!   ",
			expected: struct {
				result string
				err    error
			}{
				result: "Hello, world!",
				err:    nil,
			},
		},
		{
			name:  "text with newlines",
			input: "\n\nHello\nWorld\n\n",
			expected: struct {
				result string
				err    error
			}{
				result: "Hello\nWorld",
				err:    nil,
			},
		},
		{
			name:  "empty string",
			input: "",
			expected: struct {
				result string
				err    error
			}{
				result: "",
				err:    nil,
			},
		},
		{
			name:  "whitespace only",
			input: "   \n\t  ",
			expected: struct {
				result string
				err    error
			}{
				result: "",
				err:    nil,
			},
		},
		{
			name:  "multiline content",
			input: "Line 1\nLine 2\nLine 3",
			expected: struct {
				result string
				err    error
			}{
				result: "Line 1\nLine 2\nLine 3",
				err:    nil,
			},
		},
		{
			name:  "content with special characters",
			input: "Hello <world> & \"friends\" 'everyone'",
			expected: struct {
				result string
				err    error
			}{
				result: "Hello <world> & \"friends\" 'everyone'",
				err:    nil,
			},
		},
		{
			name:  "content with unicode",
			input: "Hello 世界 🌍",
			expected: struct {
				result string
				err    error
			}{
				result: "Hello 世界 🌍",
				err:    nil,
			},
		},
		{
			name:  "JSON-like content passes through",
			input: `{"key": "value"}`,
			expected: struct {
				result string
				err    error
			}{
				result: `{"key": "value"}`,
				err:    nil,
			},
		},
		{
			name:  "YAML-like content passes through",
			input: "key: value\nlist:\n  - item1\n  - item2",
			expected: struct {
				result string
				err    error
			}{
				result: "key: value\nlist:\n  - item1\n  - item2",
				err:    nil,
			},
		},
	}

	section := NewText()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := section.ParseSection(nil, tt.input)

			assert.Equal(t, tt.expected.err, err)
			assert.Equal(t, tt.expected.result, result)
		})
	}
}

func TestText_ParseSection_WithExecutionContext(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected struct {
			result string
			err    error
		}
	}{
		{
			name:  "parses with execution context",
			input: "Hello, world!",
			expected: struct {
				result string
				err    error
			}{
				result: "Hello, world!",
				err:    nil,
			},
		},
		{
			name:  "empty content with execution context",
			input: "",
			expected: struct {
				result string
				err    error
			}{
				result: "",
				err:    nil,
			},
		},
	}

	section := NewText()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			execCtx := gent.NewExecutionContext(context.Background(), "test", nil)
			execCtx.StartIteration()

			result, err := section.ParseSection(execCtx, tt.input)

			assert.Equal(t, tt.expected.err, err)
			assert.Equal(t, tt.expected.result, result)

			// Text section should never trace errors (it never fails)
			stats := execCtx.Stats()
			assert.Equal(t, int64(0), stats.GetCounter(gent.KeySectionParseErrorTotal))
			assert.Equal(t, int64(0), stats.GetCounter(gent.KeySectionParseErrorConsecutive))
		})
	}
}

func TestText_MethodChaining(t *testing.T) {
	section := NewText().
		WithSectionName("thinking").
		WithPrompt("Think carefully about the problem.")

	assert.Equal(t, "thinking", section.Name())
	assert.Equal(t, "Think carefully about the problem.", section.Prompt())
}

func TestText_ImplementsTextOutputSection(t *testing.T) {
	var _ gent.TextOutputSection = (*Text)(nil)
}
