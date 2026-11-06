package models

import (
	"context"
	"errors"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

// Record assertion failures without failing the enclosing test. This proves that the exact
// helper used by live tests rejects invalid streams even when the final response is valid.
type liveCheckResult struct{ failures int }

func (r *liveCheckResult) Helper()               {}
func (r *liveCheckResult) Errorf(string, ...any) { r.failures++ }

type liveCheckModel struct {
	chunks []gent.StreamChunk
	err    error
}

func (liveCheckModel) PrepareRequest(ctx *gent.ExecutionContext, request *gent.ModelCallRequest,
	chain gent.ToolChain,
) (*gent.ModelCallRequest, error) {
	return gent.PrepareModelRequest(ctx, request, chain)
}

func (model liveCheckModel) GenerateContentStream(context.Context, *gent.ModelCallRequest) (
	gent.Stream, error,
) {
	stream := gent.NewStreamWithDuration()
	for _, chunk := range model.chunks {
		stream.Send(chunk)
	}
	stream.Complete(&gent.ModelCallResponse{Choices: []*gent.ContentChoice{{
		CandidateContent: []gent.ContentPart{gent.TextPart("gent-live-ok")},
		Completion:       gent.ModelCompletion{Status: gent.ModelStatusAnswer},
	}}, Info: &gent.GenerationInfo{InputTokens: 1, OutputTokens: 1, TotalTokens: 2}}, model.err)
	return stream, nil
}

func TestLiveTextCheckRejectsInvalidStreams(t *testing.T) {
	type input struct{ chunks []gent.StreamChunk }
	type expected struct{ failures int }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	preview := func(text string) gent.StreamChunk {
		return gent.StreamChunk{MetaContent: []*gent.MetaContent{{Type: gent.MetaContentIntermediary,
			Content: []gent.ContentPart{gent.TextPart(text)}}}}
	}
	cases := []testCase{
		{"complete preview", input{[]gent.StreamChunk{preview("gent-"), preview("live-ok")}},
			expected{0}},
		{"no chunks", input{}, expected{1}},
		{"empty chunk", input{[]gent.StreamChunk{{}}}, expected{1}},
		{"wrong preview", input{[]gent.StreamChunk{preview("wrong")}}, expected{1}},
		{"error chunk", input{[]gent.StreamChunk{preview("gent-live-ok"),
			{Err: errors.New("lost preview")}}}, expected{1}},
		{"unvalidated output", input{[]gent.StreamChunk{preview("gent-live-ok"),
			{Content: []gent.ContentPart{gent.TextPart("unvalidated")}}}}, expected{1}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			result := &liveCheckResult{}
			checkLiveText(result, context.Background(), liveCheckModel{chunks: test.input.chunks})
			assert.Equal(t, test.expected, expected{result.failures})
		})
	}
}
