package models

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestResponsesMetadataAndLastMessage(t *testing.T) {
	type input struct{ output string }
	type expected struct {
		candidate string
		source    *gent.ContentSource
		metadata  []*gent.MetaContent
		err       error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"unknown phase", input{`[{"type":"message","phase":"progress",
"content":[{"type":"output_text","text":"Working."}]}]`}, expected{metadata: []*gent.MetaContent{
			{Type: "progress", Text: "Working.", MessageId: "output:0"}}}},
		{"unknown phase matches a display type", input{`[{"type":"message","phase":"intermediary",
"content":[{"type":"output_text","text":"Working."}]}]`}, expected{metadata: []*gent.MetaContent{
			{Type: gent.MetaContentIntermediary, Text: "Working.", MessageId: "output:0"}}}},
		{"last message is commentary", input{`[
{"type":"message","content":[{"type":"output_text","text":"Maybe."}]},
{"type":"message","phase":"commentary","content":[{"type":"output_text","text":"Checking."}]}]`},
			expected{metadata: []*gent.MetaContent{
				{Type: gent.MetaContentIntermediary, Text: "Maybe.", MessageId: "output:0"},
				{Type: gent.MetaContentCommentary, Text: "Checking.", MessageId: "output:1"}}}},
		{"one candidate from multiple messages", input{`[
{"type":"message","content":[{"type":"output_text","text":"First."}]},
{"type":"message","phase":"final_answer","content":[{"type":"output_text","text":"Last."}]}]`},
			expected{candidate: "Last.", source: &gent.ContentSource{MessageIds: []string{"output:1"}},
				metadata: []*gent.MetaContent{
					{Type: gent.MetaContentIntermediary, Text: "First.", MessageId: "output:0"},
					{Type: gent.MetaContentIntermediary, Text: "Last.", MessageId: "output:1"}}}},
		{"visible reasoning with opaque state", input{`[{"type":"reasoning",
"encrypted_content":"opaque","content":[{"type":"reasoning_text","text":"Checked."}]}]`},
			expected{metadata: []*gent.MetaContent{
				{Type: gent.MetaContentReasoning, Text: "Checked.", MessageId: "output:0"}}}},
		{"text is never parsed as reasoning", input{`[{"type":"message",
"content":[{"type":"output_text","text":"<think>Example</think>"}]}]`},
			expected{candidate: "<think>Example</think>",
				source: &gent.ContentSource{MessageIds: []string{"output:0"}},
				metadata: []*gent.MetaContent{{Type: gent.MetaContentIntermediary,
					Text: "<think>Example</think>", MessageId: "output:0"}}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var output []json.RawMessage
			if !assert.NoError(t, json.Unmarshal([]byte(test.input.output), &output)) {
				return
			}
			model := NewResponses(&ResponsesConfig{Provider: "openai", BaseURL: "https://fixture.test/v1",
				HTTPClient: &http.Client{Transport: responseTransport(func(*http.Request) (
					*http.Response, error,
				) {
					body := `{"status":"completed","output":` + test.input.output + `}`
					return &http.Response{StatusCode: 200,
						Header: http.Header{"Content-Type": {"application/json"}},
						Body:   io.NopCloser(strings.NewReader(body))}, nil
				})}})
			stream, err := model.GenerateContentStream(context.Background(), gent.NewModelCallRequest(nil))
			if !assert.NoError(t, err) {
				return
			}
			var metadata []*gent.MetaContent
			for chunk := range stream.Chunks() {
				assert.Equal(t, "", chunk.Content)
				metadata = append(metadata, chunk.MetaContent...)
			}
			result, err := stream.Response()
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, test.expected.metadata, metadata)
			choice := result.Choices[0]
			assert.Equal(t, test.expected, expected{choice.CandidateContent,
				choice.CandidateSource, choice.MetaContent, err})
			assert.Equal(t, &gent.ProviderContinuation{Provider: "openai", Items: output},
				choice.Continuation)
		})
	}
}
