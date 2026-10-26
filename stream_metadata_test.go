package gent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStreamMetadata(t *testing.T) {
	type input struct{ completed bool }
	type expected struct {
		response *ContentResponse
		chunks   []StreamChunk
		err      error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	meta := []*MetaContent{{Type: MetaContentReasoning, Text: "Checked."},
		{Type: MetaContentCommentary, Text: "Checking."},
		{Type: MetaContentIntermediary, Text: "Done."}}
	response := &ContentResponse{
		Choices: []*ContentChoice{
			{
				CandidateContent: "Done.", MetaContent: meta,
			},
		},
	}
	chunks := []StreamChunk{{MetaContent: meta[:1]}, {MetaContent: meta[1:2]}, {MetaContent: meta[2:]}}
	cases := []testCase{
		{"completed provider", input{true}, expected{response, chunks, nil}},
		{"streaming provider", input{false}, expected{response, chunks, nil}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var stream Stream
			if test.input.completed {
				stream = NewCompletedStream(response, nil)
			} else {
				provider := NewStreamWithDuration()
				for _, item := range meta {
					assert.True(t, provider.SendMeta(item))
				}
				assert.Equal(t, meta, provider.AccumulatedMetaContent())
				provider.CompleteWithGenerationInfo(nil, nil)
				stream = provider
			}
			defer stream.Close()
			acc := NewStreamAccumulator()
			actual := expected{}
			for chunk := range stream.Chunks() {
				acc.Add(chunk)
				actual.chunks = append(actual.chunks, chunk)
			}
			actual.response, actual.err = stream.Response()
			assert.Equal(t, test.expected, actual)
			assert.Equal(t, "", acc.Content())
			assert.Equal(t, response, acc.Response())
		})
	}
}
