package gent

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStreamAccumulator(t *testing.T) {
	type input struct{ chunks []StreamChunk }
	type expected struct {
		content string
		meta    []*MetaContent
		err     error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	failure := errors.New("interrupted")
	cases := []testCase{
		{"empty", input{}, expected{}},
		{"provisional and final", input{[]StreamChunk{
			{
				MetaContent: []*MetaContent{
					{
						Type: MetaContentIntermediary, Text: "The total ", MessageId: "m1",
					},
				},
			},
			{MetaContent: []*MetaContent{{Type: MetaContentIntermediary, Text: "is 42.", MessageId: "m1"}}},
			{Content: "The total is 42."},
		}}, expected{"The total is 42.", []*MetaContent{
			{Type: MetaContentIntermediary, Text: "The total is 42.", MessageId: "m1"}}, nil}},
		{"ordered open types", input{[]StreamChunk{
			{MetaContent: []*MetaContent{{Type: MetaContentReasoning, Text: "Check.", MessageId: "r1"}}},
			{MetaContent: []*MetaContent{{Type: "progress", Text: "50%", MessageId: "p1"}}},
			{MetaContent: []*MetaContent{{Type: MetaContentReasoning, Text: "Done.", MessageId: "r2"}}},
		}}, expected{meta: []*MetaContent{
			{Type: MetaContentReasoning, Text: "Check.", MessageId: "r1"},
			{Type: "progress", Text: "50%", MessageId: "p1"},
			{Type: MetaContentReasoning, Text: "Done.", MessageId: "r2"}}}},
		{"interrupted preview", input{[]StreamChunk{
			{MetaContent: []*MetaContent{{Type: MetaContentIntermediary, Text: "Partial"}}},
			{Err: failure},
		}}, expected{
			meta: []*MetaContent{
				{
					Type: MetaContentIntermediary, Text: "Partial",
				},
			}, err: failure,
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			acc := NewStreamAccumulator()
			for _, chunk := range test.input.chunks {
				acc.Add(chunk)
			}
			assert.Equal(t, test.expected, expected{acc.Content(), acc.MetaContent(), acc.Error()})
			meta := acc.MetaContent()
			if len(meta) > 0 {
				meta[0].Text = "changed"
			}
			assert.Equal(t, test.expected.meta, acc.MetaContent())
			acc.Reset()
			assert.Equal(t, expected{}, expected{acc.Content(), acc.MetaContent(), acc.Error()})
		})
	}
}
