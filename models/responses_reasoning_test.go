package models

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestResponsesReasoningAndSavedEvidence(t *testing.T) {
	type input struct {
		streaming bool
		reasoning string
	}
	type chunkText struct{ content, reasoning, commentary string }
	type expected struct {
		choice *gent.ContentChoice
		chunks []chunkText
		err    error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	const visible = `{"id":"reason-1","type":"reasoning","summary":[
{"type":"summary_text","text":"I checked "},{"type":"summary_text","text":"the result."}],
"encrypted_content":"opaque","provider_extra":{"keep":true}}`
	const opaque = `{"id":"reason-1","type":"reasoning","summary":[],
"encrypted_content":"opaque","provider_extra":{"keep":true}}`
	const commentary = `{"id":"update-1","type":"message","role":"assistant","phase":"commentary",
"content":[{"type":"output_text","text":"Checking the result."}]}`
	const message = `{"type":"message","role":"assistant","phase":"final_answer","content":[
{"type":"output_text","text":"Done."}]}`
	choice := func(raw, summary string) *gent.ContentChoice {
		return &gent.ContentChoice{CandidateContent: []gent.ContentPart{
			gent.TextContent{Text: "Done."},
		},
			Completion: gent.ModelCompletion{
				Status:         gent.ModelStatusAnswer,
				ProviderReason: "completed",
			},
			MetaContent:     responseTestMeta(summary),
			CandidateSource: &gent.ContentSource{MessageIds: []string{"output:2"}},
			Continuation: &gent.ProviderContinuation{Provider: "openai",
				Items: []json.RawMessage{
					json.RawMessage(raw), json.RawMessage(commentary), json.RawMessage(message)}}}
	}
	cases := []testCase{
		{"JSON with summary", input{false, visible}, expected{
			choice: choice(visible, "I checked the result."),
		}},
		{"SSE with summary", input{true, visible}, expected{
			choice: choice(visible, "I checked the result."),
			chunks: []chunkText{{reasoning: "I checked "}, {reasoning: "the result."},
				{commentary: "Checking the result."}, {content: "Done."}},
		}},
		{"JSON with opaque data only", input{false, opaque}, expected{choice: choice(opaque, "")}},
		{"SSE with opaque data only", input{true, opaque}, expected{
			choice: choice(opaque, ""),
			chunks: []chunkText{{commentary: "Checking the result."}, {content: "Done."}},
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			final := `{"status":"completed","output":[` + test.input.reasoning + `,` +
				commentary + `,` + message + `]}`
			model := NewResponses(&ResponsesConfig{
				Provider: "openai", BaseURL: "https://fixture.test/v1", Model: "fixture",
				HTTPClient: &http.Client{Transport: responseTransport(func(
					*http.Request,
				) (*http.Response, error) {
					if !test.input.streaming {
						return &http.Response{StatusCode: 200,
							Header: http.Header{"Content-Type": {"application/json"}},
							Body:   io.NopCloser(strings.NewReader(final))}, nil
					}
					var payloads []string
					if test.input.reasoning == visible {
						payloads = append(payloads,
							`{"type":"response.reasoning_summary_text.delta","delta":"I checked "}`,
							`{"type":"response.reasoning_summary_text.delta","delta":"the result."}`)
					}
					payloads = append(payloads,
						`{"type":"response.output_item.added","output_index":1,"item":`+commentary+`}`,
						`{"type":"response.output_item.added","output_index":2,"item":`+message+`}`,
						`{"type":"response.output_text.delta","output_index":1,
"delta":"Checking the result."}`,
						`{"type":"response.output_text.delta","output_index":2,"delta":"Done."}`,
						`{"type":"response.completed","response":`+final+`}`)
					return sseResponse(payloads...), nil
				})},
			})
			ctx := gent.NewExecutionContext(context.Background(), "fixture", nil)
			started := time.Now()
			stream, err := ctx.GenerateContentStream(model, gent.ModelCall{
				Id: "fixture-call",
			}, gent.NewModelCallRequest(nil))
			if !assert.NoError(t, err) {
				return
			}
			defer stream.Close()
			var chunks []gent.StreamChunk
			actual := expected{}
			for chunk := range stream.Chunks() {
				assert.GreaterOrEqual(t, chunk.Timestamp, started)
				// Portable time values omit the process-local monotonic clock.
				chunk.Timestamp = chunk.Timestamp.Round(0)
				chunks = append(chunks, chunk)
				actual.chunks = append(actual.chunks, chunkText{
					gent.MetaText(chunk.MetaContent, gent.MetaContentIntermediary),
					gent.MetaText(chunk.MetaContent, gent.MetaContentReasoning),
					gent.MetaText(chunk.MetaContent, gent.MetaContentCommentary)})
			}
			result, err := stream.Response()
			if !assert.NoError(t, err) || !assert.Len(t, result.Choices, 1) {
				return
			}
			actual.choice, actual.err = result.Choices[0], err
			prefix := strings.TrimSuffix(result.Choices[0].CandidateSource.MessageIds[0], "output:2")
			for _, item := range test.expected.choice.MetaContent {
				item.MessageId = prefix + item.MessageId
			}
			test.expected.choice.CandidateSource = &gent.ContentSource{ModelCallId: "fixture-call",
				MessageIds: []string{prefix + "output:2"}}
			if !test.input.streaming {
				test.expected.chunks = []chunkText{}
				if test.input.reasoning == visible {
					test.expected.chunks = append(test.expected.chunks,
						chunkText{reasoning: "I checked "}, chunkText{reasoning: "the result."})
				}
				test.expected.chunks = append(test.expected.chunks,
					chunkText{commentary: "Checking the result."}, chunkText{content: "Done."})
			}
			assert.Equal(t, test.expected, actual)

			// Verify the portable record, including raw provider fields and reasoning chunks.
			view, err := ctx.Export()
			if !assert.NoError(t, err) {
				return
			}
			data, err := ctx.Codecs().Marshal(view)
			if !assert.NoError(t, err) {
				return
			}
			codecs := gent.NewCodecRegistry()
			var saved gent.RunView
			if !assert.NoError(t, codecs.Unmarshal(data, &saved)) {
				return
			}
			var savedChunks []gent.StreamChunk
			var savedResponses []*gent.ContentResponse
			for _, event := range saved.Activity {
				switch event.Type {
				case "model_call_finished":
					decoded, err := codecs.DecodeEvent(event)
					if !assert.NoError(t, err) {
						return
					}
					savedResponses = append(savedResponses, decoded.(*gent.AfterModelCallEvent).Response)
				case "model_stream_chunk":
					decoded, err := codecs.DecodeEvent(event)
					if !assert.NoError(t, err) {
						return
					}
					savedChunks = append(savedChunks, decoded.(gent.StreamChunk))
				}
			}
			assert.Equal(t, []*gent.ContentResponse{result}, savedResponses)
			assert.Equal(t, chunks, savedChunks)
		})
	}
}

func responseTestMeta(summary string) []*gent.MetaContent {
	var meta []*gent.MetaContent
	if summary != "" {
		meta = append(meta,
			&gent.MetaContent{
				Type:      gent.MetaContentReasoning,
				Content:   []gent.ContentPart{gent.TextContent{Text: "I checked "}},
				MessageId: "output:0",
			},
			&gent.MetaContent{
				Type:      gent.MetaContentReasoning,
				Content:   []gent.ContentPart{gent.TextContent{Text: "the result."}},
				MessageId: "output:0",
			})
	}
	return append(meta,
		&gent.MetaContent{
			Type: gent.MetaContentCommentary,
			Content: []gent.ContentPart{
				gent.TextContent{Text: "Checking the result."},
			},
			MessageId: "output:1",
		},
		&gent.MetaContent{
			Type:      gent.MetaContentIntermediary,
			Content:   []gent.ContentPart{gent.TextContent{Text: "Done."}},
			MessageId: "output:2",
		})
}
