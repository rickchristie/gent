package models

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestProvidersRejectUnsupportedInputAndOutput(t *testing.T) {
	type input struct {
		provider string
		parts    []gent.ContentPart
		modality string
	}
	type expected struct {
		err      string
		requests int
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	var cases []testCase
	for _, provider := range testProviders {
		name := provider
		switch provider {
		case "openai", "xai":
			name = "Responses"
		case "anthropic":
			name = "Anthropic"
		case "gemini":
			name = "Gemini"
		case "ollama":
			name = "Ollama"
		}
		cases = append(cases, testCase{provider + "/file", input{provider: provider,
			parts: []gent.ContentPart{gent.TextContent{Text: "Read the archive."},
				&gent.BinaryContent{MIMEType: "application/zip", Data: []byte{1, 2, 3}}}},
			expected{fmt.Sprintf("unsupported %s media %q", name, "application/zip"), 0}})
		outputErr := fmt.Sprintf("%s does not support output modality %q", provider, "audio")
		switch provider {
		case "openai", "xai", "gemini":
			outputErr = fmt.Sprintf("unsupported %s output modality %q", name, "audio")
		case "openrouter":
			outputErr = `OpenRouter model does not support "audio" output`
		}
		cases = append(cases, testCase{provider + "/output", input{provider: provider,
			parts: []gent.ContentPart{gent.TextContent{Text: "Read."}}, modality: "audio"},
			expected{outputErr, 0}})
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			client := &http.Client{Transport: responseTransport(func(request *http.Request) (
				*http.Response, error,
			) {
				if request.Method == http.MethodGet {
					return fixtureHTTP(routerFixture, "application/json"), nil
				}
				requests++
				return fixtureHTTP(providerResponse(test.input.provider, 3), "application/json"), nil
			})}
			request := gent.NewModelCallRequest([]*gent.ModelMessage{
				{Role: gent.ChatMessageTypeHuman, Parts: test.input.parts}})
			if test.input.modality != "" {
				request.Options.ResponseModalities = []string{test.input.modality}
			}
			stream, err := fixtureModel(test.input.provider, client).GenerateContentStream(
				context.Background(), request)
			assert.Nil(t, stream)
			assert.EqualError(t, err, test.expected.err)
			assert.Equal(t, test.expected.requests, requests)
		})
	}
}

func TestProvidersMapOwnedImageInput(t *testing.T) {
	type input struct{ provider string }
	type expected struct{ field, messages string }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	const chat = `[{"role":"system","content":"Keep my instruction."},{"role":"user","content":[
{"type":"text","text":"Before."},
{"type":"image_url","image_url":{"url":"data:image/png;base64,AQID"}},
{"type":"text","text":"After."}]}]`
	const responses = `[{"role":"system",
"content":[{"type":"input_text","text":"Keep my instruction."}]},
{"role":"user","content":[{"type":"input_text","text":"Before."},
{"type":"input_image","image_url":"data:image/png;base64,AQID"},
{"type":"input_text","text":"After."}]}]`
	var cases []testCase
	for _, provider := range testProviders {
		want := expected{"messages", chat}
		switch provider {
		case "openai", "xai":
			want = expected{"input", responses}
		case "anthropic":
			want = expected{"messages", `[{"role":"user","content":[{"type":"text","text":"Before."},
{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AQID"}},
{"type":"text","text":"After."}]}]`}
		case "gemini":
			want = expected{"contents", `[{"role":"user","parts":[{"text":"Before."},
{"inlineData":{"mimeType":"image/png","data":"AQID"}},{"text":"After."}]}]`}
		case "ollama":
			// Ollama has one text string and an ordered image array. The adapter retains both.
			want = expected{"messages", `[{"role":"system","content":"Keep my instruction."},
{"role":"user","content":"Before.After.","images":["AQID"]}]`}
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
			request := gent.NewModelCallRequest([]*gent.ModelMessage{
				gent.TextParts(gent.ChatMessageTypeSystem, "Keep my instruction."),
				{Role: gent.ChatMessageTypeHuman, Parts: []gent.ContentPart{
					&gent.TextContent{Text: "Before."},
					&gent.BinaryContent{MIMEType: "image/png", Data: []byte{1, 2, 3}},
					gent.TextContent{Text: "After."}}}})
			stream, err := fixtureModel(test.input.provider, client).GenerateContentStream(
				context.Background(), request)
			if !assert.NoError(t, err) {
				return
			}
			for range stream.Chunks() {
			}
			_, err = stream.Response()
			stream.Close()
			assert.NoError(t, err)
			assert.JSONEq(t, test.expected.messages, string(body[test.expected.field]))
			switch test.input.provider {
			case "anthropic":
				assert.JSONEq(t, `[{"type":"text","text":"Keep my instruction."}]`, string(body["system"]))
			case "gemini":
				assert.JSONEq(t, `{"parts":[{"text":"Keep my instruction."}]}`,
					string(body["systemInstruction"]))
			}
		})
	}
}
