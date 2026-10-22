package format

import (
	"fmt"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/section"
	"github.com/stretchr/testify/assert"
)

func TestXMLDefaultParsing(t *testing.T) {
	type input struct{ text string }
	type expected struct {
		sections map[string][]string
		err      error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	tests := []testCase{
		{"answer", input{`<answer>done</answer>`}, expected{map[string][]string{
			"answer": {"done"},
		}, nil}},
		{"case and repeated tags", input{`<Answer>a</ANSWER><answer>b</answer>`},
			expected{map[string][]string{"answer": {"a", "b"}}, nil}},
		{"nested tags", input{`<outer><inner>x</inner></outer>`}, expected{map[string][]string{
			"outer": {"<inner>x</inner>"}, "inner": {"x"},
		}, nil}},
		{"no tags", input{"plain text"}, expected{nil, gent.ErrNoSectionsFound}},
		{"unclosed tag", input{`<answer>x`}, expected{nil,
			fmt.Errorf("%w: <answer> (1 unpaired)", ErrUnclosedTag)}},
	}
	parser := NewXML()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sections, err := parser.Parse(nil, tc.input.text)
			assert.Equal(t, tc.expected, expected{sections, err})
		})
	}
	assert.Equal(t, "", parser.DescribeStructure(), "discovery must not register sections")
}

func TestXMLLiteralSectionName(t *testing.T) {
	parser := NewXML().RegisterSection(section.NewText("answer[0]"))
	result, err := parser.Parse(nil, `<answer[0]>done</answer[0]>`)
	assert.NoError(t, err)
	assert.Equal(t, map[string][]string{"answer[0]": {"done"}}, result)
}

func FuzzXMLParsing(f *testing.F) {
	f.Add("answer", `<answer>done</answer>`)
	f.Add("answer[0]", `<answer[0]>done</answer[0]>`)
	f.Add("outer", `<outer><inner>x</outer>`)
	f.Fuzz(func(t *testing.T, name, text string) {
		// Bound work per generated input so fuzzing checks parsing rather than input size limits.
		if len(name) > 128 || len(text) > 4096 {
			t.Skip()
		}
		for _, parser := range []*XML{NewXML(), NewXML().WithStrict(true)} {
			_, err := parser.Parse(nil, text)
			if err != nil {
				t.Log(err)
			}
			parser.RegisterSection(section.NewText(name))
			_, err = parser.Parse(nil, text)
			if err != nil {
				t.Log(err)
			}
		}
	})
}
