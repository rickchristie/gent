package models

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/internal/modelcontext"
	"github.com/stretchr/testify/assert"
)

// Application messages and compaction summaries have no provider continuation. Check their
// complete native message list: successful replay of provider output does not test this path.
func TestProvidersAssistantHistory(t *testing.T) {
	type input struct{ provider string }
	type expected struct {
		field, messages string
		err             error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	const chat = `[{"role":"system","content":"Read history."},
{"role":"user","content":"Keep the area."},
{"role":"assistant","content":"Area: {\"area\":\"Tebet\"}."},
{"role":"user","content":"Which area?"}]`
	const responses = `[{"role":"system","content":[{"type":"input_text","text":"Read history."}]},
{"role":"user","content":[{"type":"input_text","text":"Keep the area."}]},
{"role":"assistant","content":[{"type":"output_text","text":"Area: "},
{"type":"output_text","text":"{\"area\":\"Tebet\"}"},{"type":"output_text","text":"."}]},
{"role":"user","content":[{"type":"input_text","text":"Which area?"}]}]`
	var cases []testCase
	for _, provider := range testProviders {
		want := expected{field: "messages", messages: chat}
		switch provider {
		case "openai", "xai":
			want.field, want.messages = "input", responses
		case "anthropic":
			want.messages = `[{"role":"user","content":[{"type":"text","text":"Keep the area."}]},
{"role":"assistant","content":[{"type":"text","text":"Area: "},
{"type":"text","text":"{\"area\":\"Tebet\"}"},{"type":"text","text":"."}]},
{"role":"user","content":[{"type":"text","text":"Which area?"}]}]`
		case "gemini":
			want.field = "contents"
			want.messages = `[{"role":"user","parts":[{"text":"Keep the area."}]},
{"role":"model","parts":[{"text":"Area: "},{"text":"{\"area\":\"Tebet\"}"},{"text":"."}]},
{"role":"user","parts":[{"text":"Which area?"}]}]`
		}
		cases = append(cases, testCase{provider, input{provider}, want})
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var body map[string]json.RawMessage
			client := &http.Client{Transport: responseTransport(func(request *http.Request) (
				*http.Response, error,
			) {
				if request.Method == http.MethodGet {
					return fixtureHTTP(routerFixture, "application/json"), nil
				}
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					return nil, err
				}
				return fixtureHTTP(providerResponse(test.input.provider, 3), "application/json"), nil
			})}
			data, err := gent.DataPart(map[string]string{"area": "Tebet"})
			if !assert.NoError(t, err) {
				return
			}
			request := gent.NewModelCallRequest([]*gent.ModelMessage{
				gent.TextParts(gent.ChatMessageTypeSystem, "Read history."),
				gent.TextParts(gent.ChatMessageTypeHuman, "Keep the area."),
				{Role: gent.ChatMessageTypeAI, Parts: []gent.ContentPart{
					&gent.TextContent{Text: "Area: "}, data, gent.TextContent{Text: "."}}},
				gent.TextParts(gent.ChatMessageTypeHuman, "Which area?"),
			})
			stream, err := fixtureModel(test.input.provider, client).GenerateContentStream(
				context.Background(), request)
			if !assert.Equal(t, test.expected.err, err) {
				return
			}
			defer stream.Close()
			for range stream.Chunks() {
			}
			_, err = stream.Response()
			assert.Equal(t, test.expected.err, err)
			assert.JSONEq(t, test.expected.messages, string(body[test.expected.field]))
			if test.input.provider == "anthropic" {
				assert.JSONEq(t, `[{"type":"text","text":"Read history."}]`, string(body["system"]))
			}
			if test.input.provider == "gemini" {
				assert.JSONEq(t, `{"parts":[{"text":"Read history."}]}`, string(body["systemInstruction"]))
			}
		})
	}
}

func TestResponsesAssistantMediaUsesSteering(t *testing.T) {
	type input struct {
		provider string
		part     gent.ContentPart
	}
	type expected struct {
		text     string
		requests int
		err      error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	var cases []testCase
	for _, provider := range []string{"openai", "xai"} {
		cases = append(cases,
			testCase{provider + "/image URL", input{provider,
				gent.ImageURLPart("https://images.test/expired.png")},
				expected{"Use the saved text. MIME type: image URL: https://images.test/expired.png.",
					1, nil}},
			testCase{provider + "/image bytes", input{provider,
				gent.BytesPart("image/png", []byte{1, 2, 3})},
				expected{"Use the saved text. MIME type: image/png.", 1, nil}},
			testCase{provider + "/PDF pointer", input{provider,
				&gent.BinaryContent{MIMEType: "application/pdf", Data: []byte("%PDF-1.7")}},
				expected{"Use the saved text. MIME type: application/pdf.", 1, nil}},
		)
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var body map[string]json.RawMessage
			requests := 0
			client := &http.Client{Transport: responseTransport(func(request *http.Request) (
				*http.Response, error,
			) {
				requests++
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					return nil, err
				}
				return fixtureHTTP(providerResponse(test.input.provider, 3), "application/json"), nil
			})}
			var evidence []map[string]any
			ctx := modelcontext.WithRecorder(context.Background(), func(value map[string]any) error {
				evidence = append(evidence, value)
				return nil
			})
			original := gent.NewModelCallRequest([]*gent.ModelMessage{
				{Role: gent.ChatMessageTypeAI, Parts: []gent.ContentPart{
					gent.TextPart("Before."), test.input.part, gent.TextPart("After.")}},
			})
			original.Options.SteeringMessages.UnsupportedMedia = "Use the saved text."
			// A saved snapshot detects changes to caller-owned parts during request preparation.
			before, err := json.Marshal(original)
			if !assert.NoError(t, err) {
				return
			}
			stream, err := fixtureModel(test.input.provider, client).GenerateContentStream(ctx, original)
			if !assert.Equal(t, test.expected.err, err) {
				return
			}
			defer stream.Close()
			for range stream.Chunks() {
			}
			_, err = stream.Response()
			assert.Equal(t, test.expected.err, err)
			assert.Equal(t, test.expected.requests, requests)
			want, err := json.Marshal([]any{map[string]any{"role": "assistant", "content": []any{
				map[string]any{"type": "output_text", "text": "Before."},
				map[string]any{"type": "output_text", "text": test.expected.text},
				map[string]any{"type": "output_text", "text": "After."},
			}}})
			assert.NoError(t, err)
			assert.JSONEq(t, string(want), string(body["input"]))
			assert.Equal(t, []map[string]any{{"original": test.input.part,
				"prepared": gent.TextPart(test.expected.text), "reason": "unsupported_media",
				"path": "messages[0].parts[1]", "error": (*gent.SavedError)(nil),
			}}, evidence)
			after, err := json.Marshal(original)
			assert.NoError(t, err)
			assert.Equal(t, string(before), string(after))
		})
	}
}

func TestProvidersContinuationSkipsUnusedMedia(t *testing.T) {
	type input struct{ provider string }
	type expected struct {
		requests int
		evidence []map[string]any
		err      error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	var cases []testCase
	for _, provider := range testProviders {
		cases = append(cases, testCase{provider, input{provider}, expected{2, nil, nil}})
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			actual := expected{}
			client := &http.Client{Transport: responseTransport(func(request *http.Request) (
				*http.Response, error,
			) {
				if request.URL.Host == "images.test" {
					t.Error("provider replay must not load unused media")
				}
				if request.Method == http.MethodGet {
					return fixtureHTTP(routerFixture, "application/json"), nil
				}
				actual.requests++
				return fixtureHTTP(providerResponse(test.input.provider, 3), "application/json"), nil
			})}
			ctx := modelcontext.WithRecorder(context.Background(), func(value map[string]any) error {
				actual.evidence = append(actual.evidence, value)
				return nil
			})
			model := fixtureModel(test.input.provider, client)
			messages := []*gent.ModelMessage{gent.TextParts(gent.ChatMessageTypeHuman, "Read.")}
			for turn := range 2 {
				stream, err := model.GenerateContentStream(ctx, gent.NewModelCallRequest(messages))
				if !assert.NoError(t, err) {
					return
				}
				for range stream.Chunks() {
				}
				response, err := stream.Response()
				stream.Close()
				actual.err = err
				if !assert.NoError(t, err) || !assert.Len(t, response.Choices, 1) {
					return
				}
				if turn == 0 {
					continuation := response.Choices[0].Continuation
					if !assert.NotNil(t, continuation) {
						return
					}
					messages = append(messages, &gent.ModelMessage{Role: gent.ChatMessageTypeAI,
						Continuation: continuation, Parts: []gent.ContentPart{
							gent.ImageURLPart("https://images.test/expired.png"),
							gent.BytesPart("application/zip", []byte{1, 2, 3}),
						}}, gent.TextParts(gent.ChatMessageTypeHuman, "Read again."))
				}
			}
			assert.Equal(t, test.expected, actual)
		})
	}
}
