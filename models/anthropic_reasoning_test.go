package models_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/models"
	"github.com/stretchr/testify/assert"
)

func TestAnthropicNativeThinkingChannels(t *testing.T) {
	type input struct {
		thinking string
		text     string
	}
	type expected struct {
		choices []*gent.ContentChoice
		chunks  []gent.StreamChunk
		err     error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"native thinking block", input{"Checked the record.", "Done."}, expected{
			choices: []*gent.ContentChoice{{CandidateContent: []gent.ContentPart{
				gent.TextContent{Text: "Done."},
			},
				MetaContent: []*gent.MetaContent{{
					Type: gent.MetaContentReasoning,
					Content: []gent.ContentPart{
						gent.TextContent{Text: "Checked the record."},
					},
					MessageId: "reasoning",
				},
					{
						Type:    gent.MetaContentIntermediary,
						Content: []gent.ContentPart{gent.TextContent{Text: "Done."}},
					}},
				Completion: gent.ModelCompletion{Status: gent.ModelStatusAnswer, ProviderReason: "end_turn"}}},
			chunks: []gent.StreamChunk{
				{
					MetaContent: []*gent.MetaContent{
						{
							Type: gent.MetaContentReasoning,
							Content: []gent.ContentPart{
								gent.TextContent{Text: "Checked the record."},
							},
							MessageId: "reasoning",
						},
					},
				}, {
					MetaContent: []*gent.MetaContent{
						{
							Type: gent.MetaContentIntermediary,
							Content: []gent.ContentPart{
								gent.TextContent{Text: "Done."},
							},
						},
					},
				}},
		}},
		{"ordinary text with tags", input{text: "Example: <thinking>quoted text</thinking>"},
			expected{
				choices: []*gent.ContentChoice{{CandidateContent: []gent.ContentPart{
					gent.TextContent{Text: "Example: <thinking>quoted text</thinking>"},
				},
					MetaContent: []*gent.MetaContent{{Type: gent.MetaContentIntermediary,
						Content: []gent.ContentPart{
							gent.TextContent{Text: "Example: <thinking>quoted text</thinking>"},
						}}},
					Completion: gent.ModelCompletion{Status: gent.ModelStatusAnswer, ProviderReason: "end_turn"}}},
				chunks: []gent.StreamChunk{
					{
						MetaContent: []*gent.MetaContent{
							{
								Type: gent.MetaContentIntermediary,
								Content: []gent.ContentPart{
									gent.TextContent{Text: "Example: <thinking>quoted text</thinking>"},
								},
							},
						},
					},
				},
			}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var body strings.Builder
			emit := func(kind string, value map[string]any) {
				value["type"] = kind
				data, err := json.Marshal(value)
				if !assert.NoError(t, err) {
					t.FailNow()
				}
				body.WriteString(fmt.Sprintf(`event: %s
data: %s

`, kind, data))
			}
			emit("message_start", map[string]any{"message": map[string]any{
				"id": "msg-1", "role": "assistant", "type": "message", "model": "fixture",
				"content": []any{},
				"usage":   map[string]int{"input_tokens": 10, "output_tokens": 0}}})
			index := 0
			if test.input.thinking != "" {
				emit("content_block_start", map[string]any{"index": index,
					"content_block": map[string]any{"type": "thinking", "thinking": ""}})
				emit("content_block_delta", map[string]any{"index": index,
					"delta": map[string]any{
						"type": "thinking_delta", "thinking": test.input.thinking}})
				emit("content_block_delta", map[string]any{"index": index,
					"delta": map[string]any{"type": "signature_delta", "signature": "opaque"}})
				emit("content_block_stop", map[string]any{"index": index})
				index++
			}
			emit("content_block_start", map[string]any{"index": index,
				"content_block": map[string]any{"type": "text", "text": ""}})
			emit("content_block_delta", map[string]any{"index": index,
				"delta": map[string]any{"type": "text_delta", "text": test.input.text}})
			emit("content_block_stop", map[string]any{"index": index})
			emit("message_delta", map[string]any{"delta": map[string]any{"stop_reason": "end_turn"},
				"usage": map[string]int{"output_tokens": 10}})
			emit("message_stop", map[string]any{})
			client := &http.Client{Transport: nativeStopTransport(func(*http.Request) (
				*http.Response, error,
			) {
				return &http.Response{StatusCode: 200,
					Header: http.Header{"Content-Type": {"text/event-stream"}},
					Body:   io.NopCloser(strings.NewReader(body.String()))}, nil
			})}
			provider := models.NewAnthropic(&models.Config{APIKey: "fixture",
				Model: "fixture", HTTPClient: client})
			request := gent.NewModelCallRequest([]*gent.ModelMessage{
				gent.TextParts(gent.ChatMessageTypeHuman, "Read the record.")})
			stream, err := provider.GenerateContentStream(
				context.Background(), request)
			if !assert.NoError(t, err) {
				return
			}
			defer stream.Close()
			actual := expected{}
			for chunk := range stream.Chunks() {
				actual.chunks = append(actual.chunks, chunk)
			}
			response, err := stream.Response()
			if !assert.NoError(t, err) {
				return
			}
			var blocks []map[string]any
			if test.input.thinking != "" {
				blocks = append(blocks, map[string]any{"type": "thinking",
					"thinking": test.input.thinking, "signature": "opaque"})
			}
			blocks = append(blocks, map[string]any{"type": "text", "text": test.input.text})
			continuation := &gent.ProviderContinuation{Provider: "anthropic"}
			for index, block := range blocks {
				raw, err := json.Marshal(block)
				assert.NoError(t, err)
				continuation.Items = append(continuation.Items, raw)
				id := fmt.Sprintf("block:%d", index)
				test.expected.choices[0].MetaContent[index].MessageId = id
				test.expected.chunks[index].MetaContent[0].MessageId = id
			}
			test.expected.choices[0].Continuation = continuation
			test.expected.choices[0].CandidateSource = &gent.ContentSource{
				MessageIds: []string{fmt.Sprintf("block:%d", len(blocks)-1)}}
			actual.choices, actual.err = response.Choices, err
			assert.Equal(t, test.expected, actual)
		})
	}
}
