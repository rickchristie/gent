package format

import (
	"context"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

// mockSection is a simple TextOutputSection for testing.
type mockSection struct {
	name   string
	prompt string
}

func (m *mockSection) Name() string   { return m.name }
func (m *mockSection) Prompt() string { return m.prompt }
func (m *mockSection) ParseSection(_ *gent.ExecutionContext, content string) (any, error) {
	return content, nil
}

func TestMarkdown_Parse(t *testing.T) {
	type input struct {
		sections []string
		output   string
	}

	type expected struct {
		sections map[string][]string
		err      error
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "single section",
			input: input{
				sections: []string{"Answer"},
				output: `# Answer
The weather is sunny today.`,
			},
			expected: expected{
				sections: map[string][]string{
					"answer": {"The weather is sunny today."},
				},
				err: nil,
			},
		},
		{
			name: "multiple sections",
			input: input{
				sections: []string{"Thinking", "Action", "Answer"},
				output: `# Thinking
I need to search for weather information.

# Action
{"tool": "search", "args": {"query": "weather"}}

# Answer
The weather is sunny.`,
			},
			expected: expected{
				sections: map[string][]string{
					"thinking": {"I need to search for weather information."},
					"action":   {`{"tool": "search", "args": {"query": "weather"}}`},
					"answer":   {"The weather is sunny."},
				},
				err: nil,
			},
		},
		{
			name: "multiple same section",
			input: input{
				sections: []string{"Action"},
				output: `# Action
First action content.

# Action
Second action content.`,
			},
			expected: expected{
				sections: map[string][]string{
					"action": {"First action content.", "Second action content."},
				},
				err: nil,
			},
		},
		{
			name: "case insensitive",
			input: input{
				sections: []string{"Thinking"},
				output: `# THINKING
Case insensitive content.`,
			},
			expected: expected{
				sections: map[string][]string{
					"thinking": {"Case insensitive content."},
				},
				err: nil,
			},
		},
		{
			name: "content before first header ignored",
			input: input{
				sections: []string{"Answer"},
				output: `Some content before the first header that should be ignored.

# Answer
The actual answer.`,
			},
			expected: expected{
				sections: map[string][]string{
					"answer": {"The actual answer."},
				},
				err: nil,
			},
		},
		{
			name: "empty section excluded from results",
			input: input{
				sections: []string{"Thinking", "Answer"},
				output: `# Thinking

# Answer
The answer.`,
			},
			expected: expected{
				sections: map[string][]string{
					"answer": {"The answer."},
				},
				err: nil,
			},
		},
		{
			name: "empty action with answer should terminate not loop",
			input: input{
				sections: []string{"Thinking", "Action", "Answer"},
				output: `# Thinking
The customer is referring to their "number," likely meaning booking reference number. No tools can be called yet without identifiers. Proceed to final response.

# Action

# Answer
Hello! I apologize for any inconvenience. Please provide your booking reference number.`,
			},
			expected: expected{
				sections: map[string][]string{
					"thinking": {
						`The customer is referring to their "number," likely meaning booking ` +
							`reference number. No tools can be called yet without identifiers. ` +
							`Proceed to final response.`,
					},
					"answer": {
						"Hello! I apologize for any inconvenience. Please provide your " +
							"booking reference number.",
					},
				},
				err: nil,
			},
		},
		{
			name: "whitespace trimming",
			input: input{
				sections: []string{"Answer"},
				output: `# Answer

   Content with whitespace around it.

`,
			},
			expected: expected{
				sections: map[string][]string{
					"answer": {"Content with whitespace around it."},
				},
				err: nil,
			},
		},
		{
			name: "no recognized sections returns error",
			input: input{
				sections: []string{"Answer"},
				output: `# Unknown
Some content.`,
			},
			expected: expected{
				sections: nil,
				err:      gent.ErrNoSectionsFound,
			},
		},
		{
			name: "no headers returns error",
			input: input{
				sections: []string{"Answer"},
				output:   `Just some plain text without any headers.`,
			},
			expected: expected{
				sections: nil,
				err:      gent.ErrNoSectionsFound,
			},
		},
		{
			name: "inline multiple sections",
			input: input{
				sections: []string{"Thinking", "Action", "Answer"},
				output: `# Thinking
Quick thought.
# Action
do_something
# Answer
Done.`,
			},
			expected: expected{
				sections: map[string][]string{
					"thinking": {"Quick thought."},
					"action":   {"do_something"},
					"answer":   {"Done."},
				},
				err: nil,
			},
		},
		{
			name: "content with literal hash characters",
			input: input{
				sections: []string{"Code"},
				output: `# Code
func main() {
    // This is a comment with # symbol
    fmt.Println("Hello")
}`,
			},
			expected: expected{
				sections: map[string][]string{
					"code": {"func main() {\n    // This is a comment with # symbol\n    fmt.Println(\"Hello\")\n}"},
				},
				err: nil,
			},
		},
		{
			name: "header with extra spaces",
			input: input{
				sections: []string{"Answer"},
				output: `#    Answer
The answer with extra spaces in header.`,
			},
			expected: expected{
				sections: map[string][]string{
					"answer": {"The answer with extra spaces in header."},
				},
				err: nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			format := NewMarkdown()

			for _, name := range tt.input.sections {
				format.RegisterSection(&mockSection{name: name, prompt: ""})
			}

			result, err := format.Parse(nil, tt.input.output)

			assert.ErrorIs(t, err, tt.expected.err)
			assert.Equal(t, tt.expected.sections, result)
		})
	}
}

func TestMarkdown_DescribeStructure(t *testing.T) {
	type input struct {
		sections []string
	}

	type expected struct {
		output string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "empty sections returns empty string",
			input: input{
				sections: nil,
			},
			expected: expected{
				output: "",
			},
		},
		{
			name: "single section",
			input: input{
				sections: []string{"Answer"},
			},
			expected: expected{
				output: "Format your response using markdown headers for each section:\n\n" +
					"# Answer\n" +
					"... Answer content here ...\n\n",
			},
		},
		{
			name: "multiple sections ignores prompts",
			input: input{
				sections: []string{"Thinking", "Action"},
			},
			expected: expected{
				output: "Format your response using markdown headers for each section:\n\n" +
					"# Thinking\n" +
					"... Thinking content here ...\n\n" +
					"# Action\n" +
					"... Action content here ...\n\n",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			format := NewMarkdown()

			for _, name := range tt.input.sections {
				format.RegisterSection(&mockSection{
					name:   name,
					prompt: "This prompt should be ignored",
				})
			}

			result := format.DescribeStructure()

			assert.Equal(t, tt.expected.output, result)
		})
	}
}

func TestMarkdown_Parse_TracesErrors(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected struct {
			shouldError           bool
			formatErrorTotal      int64
			formatErrorConsec     int64
			formatErrorAtIter     int64
		}
	}{
		{
			name:  "parse error traces ParseErrorTrace",
			input: "no markdown headers here",
			expected: struct {
				shouldError           bool
				formatErrorTotal      int64
				formatErrorConsec     int64
				formatErrorAtIter     int64
			}{
				shouldError:       true,
				formatErrorTotal:  1,
				formatErrorConsec: 1,
				formatErrorAtIter: 1,
			},
		},
		{
			name:  "successful parse resets consecutive counter",
			input: "# Answer\nhello world",
			expected: struct {
				shouldError           bool
				formatErrorTotal      int64
				formatErrorConsec     int64
				formatErrorAtIter     int64
			}{
				shouldError:       false,
				formatErrorTotal:  0,
				formatErrorConsec: 0,
				formatErrorAtIter: 0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			format := NewMarkdown()
			format.RegisterSection(&mockSection{name: "answer", prompt: "Answer here"})

			// Create execution context with iteration 1
			execCtx := gent.NewExecutionContext(context.Background(), "test", nil)
			execCtx.StartIteration()

			// If we expect success, first set consecutive to 1 to verify reset
			if !tt.expected.shouldError {
				execCtx.Stats().IncrCounter(gent.KeyFormatParseErrorConsecutive, 1)
			}

			_, err := format.Parse(execCtx, tt.input)

			if tt.expected.shouldError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			stats := execCtx.Stats()
			assert.Equal(t, tt.expected.formatErrorTotal,
				stats.GetCounter(gent.KeyFormatParseErrorTotal),
				"format error total mismatch")
			assert.Equal(t, tt.expected.formatErrorConsec,
				stats.GetCounter(gent.KeyFormatParseErrorConsecutive),
				"format error consecutive mismatch")
			assert.Equal(t, tt.expected.formatErrorAtIter,
				stats.GetCounter(gent.KeyFormatParseErrorAt+"1"),
				"format error at iteration mismatch")
		})
	}
}

func TestMarkdown_RegisterSection(t *testing.T) {
	type expected struct {
		output string
	}

	tests := []struct {
		name     string
		sections []string
		expected expected
	}{
		{
			name:     "single section",
			sections: []string{"Answer"},
			expected: expected{
				output: "Format your response using markdown headers for each section:\n\n" +
					"# Answer\n" +
					"... Answer content here ...\n\n",
			},
		},
		{
			name:     "multiple sections",
			sections: []string{"Thinking", "Action"},
			expected: expected{
				output: "Format your response using markdown headers for each section:\n\n" +
					"# Thinking\n" +
					"... Thinking content here ...\n\n" +
					"# Action\n" +
					"... Action content here ...\n\n",
			},
		},
		{
			name:     "idempotent registration",
			sections: []string{"Answer", "Answer", "Answer"},
			expected: expected{
				output: "Format your response using markdown headers for each section:\n\n" +
					"# Answer\n" +
					"... Answer content here ...\n\n",
			},
		},
		{
			name:     "case insensitive idempotency",
			sections: []string{"Answer", "answer", "ANSWER"},
			expected: expected{
				output: "Format your response using markdown headers for each section:\n\n" +
					"# Answer\n" +
					"... Answer content here ...\n\n",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			format := NewMarkdown()

			for _, name := range tt.sections {
				format.RegisterSection(&mockSection{name: name, prompt: ""})
			}

			result := format.DescribeStructure()

			assert.Equal(t, tt.expected.output, result)
		})
	}
}
