package termination

import (
	"context"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
	"github.com/tmc/langchaingo/llms"
)

// mockValidator is a test validator that can accept or reject answers.
type mockValidator struct {
	name     string
	accepted bool
	feedback []gent.FormattedSection
}

func (m *mockValidator) Name() string { return m.name }
func (m *mockValidator) Validate(_ *gent.ExecutionContext, _ any) *gent.ValidationResult {
	return &gent.ValidationResult{
		Accepted: m.accepted,
		Feedback: m.feedback,
	}
}

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
				term.WithGuidance(tt.input.customPrompt)
			}

			assert.Equal(t, tt.expected.prompt, term.Guidance())
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
		status     gent.TerminationStatus
		resultText string
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
				status:     gent.TerminationAnswerAccepted,
				resultText: "The final answer.",
			},
		},
		{
			name:  "empty content does not terminate",
			input: input{content: ""},
			expected: expected{
				status:     gent.TerminationContinue,
				resultText: "",
			},
		},
		{
			name:  "whitespace only does not terminate",
			input: input{content: "   "},
			expected: expected{
				status:     gent.TerminationContinue,
				resultText: "",
			},
		},
		{
			name:  "content with surrounding whitespace is trimmed",
			input: input{content: "  trimmed content  "},
			expected: expected{
				status:     gent.TerminationAnswerAccepted,
				resultText: "trimmed content",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			term := NewText("answer")
			execCtx := gent.NewExecutionContext(context.Background(), "test", nil)

			result := term.ShouldTerminate(execCtx, tt.input.content)

			assert.Equal(t, tt.expected.status, result.Status)
			if tt.expected.status == gent.TerminationAnswerAccepted {
				assert.Len(t, result.Content, 1)
				tc, ok := result.Content[0].(llms.TextContent)
				assert.True(t, ok, "expected TextContent, got %T", result.Content[0])
				assert.Equal(t, tt.expected.resultText, tc.Text)
			}
		})
	}
}

func TestText_SetValidator(t *testing.T) {
	t.Run("validator accepts answer", func(t *testing.T) {
		term := NewText("answer")
		term.SetValidator(&mockValidator{
			name:     "test_validator",
			accepted: true,
		})

		execCtx := gent.NewExecutionContext(context.Background(), "test", nil)
		result := term.ShouldTerminate(execCtx, "valid answer")

		assert.Equal(t, gent.TerminationAnswerAccepted, result.Status)
		assert.Len(t, result.Content, 1)
	})

	t.Run("validator rejects answer and increments stats", func(t *testing.T) {
		term := NewText("answer")
		term.SetValidator(&mockValidator{
			name:     "test_validator",
			accepted: false,
			feedback: []gent.FormattedSection{
				{Name: "error", Content: "Invalid answer format"},
			},
		})

		execCtx := gent.NewExecutionContext(context.Background(), "test", nil)
		result := term.ShouldTerminate(execCtx, "invalid answer")

		assert.Equal(t, gent.TerminationAnswerRejected, result.Status)
		assert.Len(t, result.Content, 1)

		// Verify stats were incremented
		assert.Equal(t, int64(1), execCtx.Stats().GetCounter(gent.KeyAnswerRejectedTotal))
		assert.Equal(t, int64(1), execCtx.Stats().GetCounter(gent.KeyAnswerRejectedBy+"test_validator"))
	})

	t.Run("no validator means answer accepted", func(t *testing.T) {
		term := NewText("answer")
		// No validator set

		execCtx := gent.NewExecutionContext(context.Background(), "test", nil)
		result := term.ShouldTerminate(execCtx, "any answer")

		assert.Equal(t, gent.TerminationAnswerAccepted, result.Status)
	})

	t.Run("nil execCtx panics", func(t *testing.T) {
		term := NewText("answer")

		assert.Panics(t, func() {
			term.ShouldTerminate(nil, "answer")
		})
	})
}
