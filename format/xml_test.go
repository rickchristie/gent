package format

import (
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestXML_FormatSections(t *testing.T) {
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
				output: `<task>
Do something useful.
</task>`,
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
				output: `<behavior>
Be helpful.
</behavior>
<rules>
Follow the rules.
</rules>`,
			},
		},
		{
			name: "section with children uses nested tags",
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
				output: `<observation>
<result>
{"status": "ok"}
</result>
` +
					`<instructions>
Process the result.
</instructions>
</observation>`,
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
				output: `<parent>
Parent content first.
<child>
Child content.
</child>
</parent>`,
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
				output: `<level1>
<level2>
<level3>
Deep content.
</level3>
</level2>
</level1>`,
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
				output: `<intro>
Introduction.
</intro>
` +
					`<tools>
<search>
Search result.
</search>
<calculate>
42
</calculate>
</tools>
` +
					`<conclusion>
Done.
</conclusion>`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			format := NewXML()
			result := format.FormatSections(tt.input.sections)
			assert.Equal(t, tt.expected.output, result)
		})
	}
}
