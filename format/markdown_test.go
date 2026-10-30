package format

import (
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestMarkdown_FormatSections(t *testing.T) {
	type input struct {
		sections []gent.FormattedSection
	}

	type expected struct {
		output string
	}

	type testCase struct {
		name     string
		input    input
		expected expected
	}
	tests := []testCase{
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
				output: `# task
Do something useful.`,
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
				output: `# behavior
Be helpful.

# rules
Follow the rules.`,
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
				output: `# observation
## result
{"status": "ok"}

## instructions
Process the result.`,
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
				output: `# parent
Parent content first.
## child
Child content.`,
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
				output: `# level1
## level2
### level3
Deep content.`,
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
				output: `# intro
Introduction.

# tools
## search
Search result.

` +
					`## calculate
42

# conclusion
Done.`,
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
