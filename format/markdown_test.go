package format

import (
	"context"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

// mockSection is a simple TextOutputSection for testing.
type mockSection struct {
	name     string
	guidance string
}

func (m *mockSection) Name() string     { return m.name }
func (m *mockSection) Guidance() string { return m.guidance }
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
					"Answer": {"The weather is sunny today."},
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
					"Thinking": {"I need to search for weather information."},
					"Action":   {`{"tool": "search", "args": {"query": "weather"}}`},
					"Answer":   {"The weather is sunny."},
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
					"Action": {"First action content.", "Second action content."},
				},
				err: nil,
			},
		},
		{
			name: "case insensitive returns original registered name",
			input: input{
				sections: []string{"Thinking"},
				output: `# THINKING
Case insensitive content.`,
			},
			expected: expected{
				sections: map[string][]string{
					"Thinking": {"Case insensitive content."},
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
					"Answer": {"The actual answer."},
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
					"Answer": {"The answer."},
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
					"Thinking": {
						`The customer is referring to their "number," likely meaning booking ` +
							`reference number. No tools can be called yet without identifiers. ` +
							`Proceed to final response.`,
					},
					"Answer": {
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
					"Answer": {"Content with whitespace around it."},
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
					"Thinking": {"Quick thought."},
					"Action":   {"do_something"},
					"Answer":   {"Done."},
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
					"Code": {"func main() {\n    // This is a comment with # symbol\n    fmt.Println(\"Hello\")\n}"},
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
					"Answer": {"The answer with extra spaces in header."},
				},
				err: nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			format := NewMarkdown()

			for _, name := range tt.input.sections {
				format.RegisterSection(&mockSection{name: name, guidance: ""})
			}

			result, err := format.Parse(nil, tt.input.output)

			assert.ErrorIs(t, err, tt.expected.err)
			assert.Equal(t, tt.expected.sections, result)
		})
	}
}

func TestMarkdown_DescribeStructure(t *testing.T) {
	type input struct {
		name     string
		guidance string
	}

	type expected struct {
		output string
	}

	tests := []struct {
		name     string
		input    []input
		expected expected
	}{
		{
			name:  "empty sections returns empty string",
			input: nil,
			expected: expected{
				output: "",
			},
		},
		{
			name: "single section includes guidance",
			input: []input{
				{name: "Answer", guidance: "Write your final answer here."},
			},
			expected: expected{
				output: "Format your response using markdown headers for each section:\n\n" +
					"# Answer\n" +
					"Write your final answer here.\n\n",
			},
		},
		{
			name: "multiple sections include guidance",
			input: []input{
				{name: "Thinking", guidance: "Think through the problem."},
				{name: "Action", guidance: "Call a tool to take action."},
			},
			expected: expected{
				output: "Format your response using markdown headers for each section:\n\n" +
					"# Thinking\n" +
					"Think through the problem.\n\n" +
					"# Action\n" +
					"Call a tool to take action.\n\n",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			format := NewMarkdown()

			for _, s := range tt.input {
				format.RegisterSection(&mockSection{
					name:     s.name,
					guidance: s.guidance,
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
			shouldError       bool
			formatErrorTotal  int64
			formatErrorConsec float64
		}
	}{
		{
			name:  "parse error publishes ParseErrorEvent",
			input: "no markdown headers here",
			expected: struct {
				shouldError       bool
				formatErrorTotal  int64
				formatErrorConsec float64
				}{
				shouldError:       true,
				formatErrorTotal:  1,
				formatErrorConsec: 1,
			},
		},
		{
			name:  "successful parse resets consecutive gauge",
			input: "# Answer\nhello world",
			expected: struct {
				shouldError       bool
				formatErrorTotal  int64
				formatErrorConsec float64
				}{
				shouldError:       false,
				formatErrorTotal:  0,
				formatErrorConsec: 0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			format := NewMarkdown()
			format.RegisterSection(
				&mockSection{name: "Answer", guidance: "Answer here"},
			)

			// Create execution context with iteration 1
			execCtx := gent.NewExecutionContext(
				context.Background(), "test", nil,
			)
			execCtx.IncrementIteration()

			// If we expect success, first set consecutive to 1 to verify reset
			if !tt.expected.shouldError {
				execCtx.Stats().IncrGauge(
					gent.SGFormatParseErrorConsecutive, 1,
				)
			}

			_, err := format.Parse(execCtx, tt.input)

			if tt.expected.shouldError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			stats := execCtx.Stats()
			assert.Equal(t, tt.expected.formatErrorTotal,
				stats.GetCounter(gent.SCFormatParseErrorTotal),
				"format error total mismatch")
			assert.Equal(t, tt.expected.formatErrorConsec,
				stats.GetGauge(gent.SGFormatParseErrorConsecutive),
				"format error consecutive mismatch")
		})
	}
}

func TestMarkdown_FormatSections(t *testing.T) {
	type input struct {
		sections []gent.FormattedSection
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
			name: "single section with content",
			input: input{
				sections: []gent.FormattedSection{
					{Name: "task", Content: "Do something useful."},
				},
			},
			expected: expected{
				output: "# task\nDo something useful.",
			},
		},
		{
			name: "multiple flat sections",
			input: input{
				sections: []gent.FormattedSection{
					{Name: "behavior", Content: "Be helpful."},
					{Name: "rules", Content: "Follow the rules."},
				},
			},
			expected: expected{
				output: "# behavior\nBe helpful.\n\n# rules\nFollow the rules.",
			},
		},
		{
			name: "section with children uses increasing depth",
			input: input{
				sections: []gent.FormattedSection{
					{
						Name: "observation",
						Children: []gent.FormattedSection{
							{Name: "result", Content: `{"status": "ok"}`},
							{Name: "instructions", Content: "Process the result."},
						},
					},
				},
			},
			expected: expected{
				output: "# observation\n## result\n{\"status\": \"ok\"}\n\n## instructions\nProcess the result.",
			},
		},
		{
			name: "section with both content and children",
			input: input{
				sections: []gent.FormattedSection{
					{
						Name:    "parent",
						Content: "Parent content first.",
						Children: []gent.FormattedSection{
							{Name: "child", Content: "Child content."},
						},
					},
				},
			},
			expected: expected{
				output: "# parent\nParent content first.\n## child\nChild content.",
			},
		},
		{
			name: "deeply nested sections",
			input: input{
				sections: []gent.FormattedSection{
					{
						Name: "level1",
						Children: []gent.FormattedSection{
							{
								Name: "level2",
								Children: []gent.FormattedSection{
									{Name: "level3", Content: "Deep content."},
								},
							},
						},
					},
				},
			},
			expected: expected{
				output: "# level1\n## level2\n### level3\nDeep content.",
			},
		},
		{
			name: "multiple sections with mixed nesting",
			input: input{
				sections: []gent.FormattedSection{
					{Name: "intro", Content: "Introduction."},
					{
						Name: "tools",
						Children: []gent.FormattedSection{
							{Name: "search", Content: "Search result."},
							{Name: "calculate", Content: "42"},
						},
					},
					{Name: "conclusion", Content: "Done."},
				},
			},
			expected: expected{
				output: "# intro\nIntroduction.\n\n# tools\n## search\nSearch result.\n\n" +
					"## calculate\n42\n\n# conclusion\nDone.",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			format := NewMarkdown()
			result := format.FormatSections(tt.input.sections)
			assert.Equal(t, tt.expected.output, result)
		})
	}
}

func TestMarkdown_RegisterSection(t *testing.T) {
	type input struct {
		name     string
		guidance string
	}

	type expected struct {
		output string
	}

	tests := []struct {
		name     string
		sections []input
		expected expected
	}{
		{
			name: "single section",
			sections: []input{
				{name: "Answer", guidance: "Write your answer."},
			},
			expected: expected{
				output: "Format your response using markdown headers for each section:\n\n" +
					"# Answer\n" +
					"Write your answer.\n\n",
			},
		},
		{
			name: "multiple sections",
			sections: []input{
				{name: "Thinking", guidance: "Think here."},
				{name: "Action", guidance: "Act here."},
			},
			expected: expected{
				output: "Format your response using markdown headers for each section:\n\n" +
					"# Thinking\n" +
					"Think here.\n\n" +
					"# Action\n" +
					"Act here.\n\n",
			},
		},
		{
			name: "idempotent registration",
			sections: []input{
				{name: "Answer", guidance: "Write your answer."},
				{name: "Answer", guidance: "Different guidance."},
				{name: "Answer", guidance: "Another guidance."},
			},
			expected: expected{
				output: "Format your response using markdown headers for each section:\n\n" +
					"# Answer\n" +
					"Write your answer.\n\n",
			},
		},
		{
			name: "case insensitive idempotency",
			sections: []input{
				{name: "Answer", guidance: "First guidance."},
				{name: "answer", guidance: "Second guidance."},
				{name: "ANSWER", guidance: "Third guidance."},
			},
			expected: expected{
				output: "Format your response using markdown headers for each section:\n\n" +
					"# Answer\n" +
					"First guidance.\n\n",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			format := NewMarkdown()

			for _, s := range tt.sections {
				format.RegisterSection(&mockSection{name: s.name, guidance: s.guidance})
			}

			result := format.DescribeStructure()

			assert.Equal(t, tt.expected.output, result)
		})
	}
}
