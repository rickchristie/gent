package models

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"image/color"
	"net/http"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestResponsesImageAnswerParts(t *testing.T) {
	type input struct {
		text  string
		first bool
	}
	type expected struct {
		parts  []gent.ContentPart
		source *gent.ContentSource
		err    error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	img := imageFixture(t, 2, color.RGBA{B: 255, A: 255})
	cases := []testCase{
		{"empty text before image", input{"", true}, expected{[]gent.ContentPart{img},
			&gent.ContentSource{MessageIds: []string{"output:1"}}, nil}},
		{"empty text after image", input{"", false}, expected{[]gent.ContentPart{img},
			&gent.ContentSource{MessageIds: []string{"output:0"}}, nil}},
		{"text before image", input{"Here is the image.", true}, expected{
			[]gent.ContentPart{gent.TextPart("Here is the image."), img},
			&gent.ContentSource{MessageIds: []string{"output:0", "output:1"}}, nil}},
		{"text after image", input{"Here is the image.", false}, expected{
			[]gent.ContentPart{img, gent.TextPart("Here is the image.")},
			&gent.ContentSource{MessageIds: []string{"output:0", "output:1"}}, nil}},
		{"whitespace remains content", input{" ", false}, expected{
			[]gent.ContentPart{img, gent.TextPart(" ")},
			&gent.ContentSource{MessageIds: []string{"output:0", "output:1"}}, nil}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			imageItem := map[string]any{"type": "image_generation_call", "id": "unsaved-image",
				"status": "completed", "output_format": "jpeg",
				"result": base64.StdEncoding.EncodeToString(img.Data)}
			message := map[string]any{"type": "message", "role": "assistant", "content": []any{
				map[string]any{"type": "output_text", "text": test.input.text}}}
			items := []any{imageItem, message}
			if test.input.first {
				items = []any{message, imageItem}
			}
			payload, err := json.Marshal(map[string]any{"status": "completed", "output": items})
			if !assert.NoError(t, err) {
				return
			}
			client := &http.Client{Transport: responseTransport(func(*http.Request) (*http.Response, error) {
				return fixtureHTTP(string(payload), "application/json"), nil
			})}
			stream, err := fixtureModel("openai", client).GenerateContentStream(context.Background(),
				gent.NewModelCallRequest([]*gent.ModelMessage{
					gent.TextParts(gent.ChatMessageTypeHuman, "Generate an image.")}))
			if !assert.Equal(t, test.expected.err, err) {
				return
			}
			defer stream.Close()
			for range stream.Chunks() {
			}
			response, err := stream.Response()
			if !assert.Equal(t, test.expected.err, err) || !assert.Len(t, response.Choices, 1) {
				return
			}
			choice := response.Choices[0]
			assert.Equal(t, test.expected, expected{choice.CandidateContent, choice.CandidateSource, err})
			assert.Equal(t, gent.ModelCompletion{Status: gent.ModelStatusAnswer,
				ProviderReason: "completed"}, choice.Completion)
			original, err := json.Marshal(items)
			assert.NoError(t, err)
			saved, err := json.Marshal(choice.Continuation.Items)
			assert.NoError(t, err)
			assert.JSONEq(t, string(original), string(saved))
		})
	}
}

func TestResponsesImageReplay(t *testing.T) {
	type input struct {
		provider, item string
	}
	type expected struct {
		messages string
		requests int
		err      string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	const raw = `{"type":"image_generation_call","id":"unsaved-image","status":"completed",
"output_format":"jpeg","result":"AQID"}`
	const image = `[{"role":"user","content":[
{"type":"input_text","text":"Image from the previous assistant response:"},
{"type":"input_image","image_url":"data:image/jpeg;base64,AQID"}]}]`
	cases := []testCase{
		{"OpenAI owned bytes", input{"openai", raw}, expected{image, 1, ""}},
		{"xAI native item", input{"xai", raw}, expected{"[" + raw + "]", 1, ""}},
		{"OpenAI default PNG", input{"openai", `{"type":"image_generation_call","result":"AQID"}`},
			expected{`[{"role":"user","content":[
{"type":"input_text","text":"Image from the previous assistant response:"},
{"type":"input_image","image_url":"data:image/png;base64,AQID"}]}]`, 1, ""}},
		{"OpenAI missing bytes", input{"openai", `{"type":"image_generation_call","id":"unsaved"}`},
			expected{requests: 0, err: "saved image generation item has no image bytes"}},
		{"invalid item", input{"openai", `{`},
			expected{requests: 0, err: "invalid saved provider item"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var body map[string]json.RawMessage
			actual := expected{}
			client := &http.Client{Transport: responseTransport(func(request *http.Request) (
				*http.Response, error,
			) {
				actual.requests++
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					return nil, err
				}
				return fixtureHTTP(providerResponse(test.input.provider, 3), "application/json"), nil
			})}
			saved := &gent.ProviderContinuation{Provider: test.input.provider,
				Items: []json.RawMessage{json.RawMessage(test.input.item)}}
			stream, err := fixtureModel(test.input.provider, client).GenerateContentStream(
				context.Background(), gent.NewModelCallRequest([]*gent.ModelMessage{
					{Role: gent.ChatMessageTypeAI, Continuation: saved},
				}))
			if err == nil {
				defer stream.Close()
				for range stream.Chunks() {
				}
				_, err = stream.Response()
			}
			if err != nil {
				actual.err = err.Error()
			}
			if len(body) > 0 {
				assert.JSONEq(t, test.expected.messages, string(body["input"]))
				actual.messages = test.expected.messages
			}
			assert.Equal(t, test.expected, actual)
			assert.Equal(t, test.input.item, string(saved.Items[0]))
		})
	}
}
