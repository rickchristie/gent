package format

import (
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
func (m *mockSection) ParseSection(content string) (any, error) {
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
			name: "empty section",
			input: input{
				sections: []string{"Thinking", "Answer"},
				output: `# Thinking

# Answer
The answer.`,
			},
			expected: expected{
				sections: map[string][]string{
					"thinking": {""},
					"answer":   {"The answer."},
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

			var sections []gent.TextOutputSection
			for _, name := range tt.input.sections {
				sections = append(sections, &mockSection{name: name, prompt: ""})
			}
			format.DescribeStructure(sections)

			result, err := format.Parse(tt.input.output)

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

			var sections []gent.TextOutputSection
			for _, name := range tt.input.sections {
				sections = append(sections, &mockSection{
					name:   name,
					prompt: "This prompt should be ignored",
				})
			}

			result := format.DescribeStructure(sections)

			assert.Equal(t, tt.expected.output, result)
		})
	}
}
