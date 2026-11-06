package models

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/internal/modelcontext"
	"github.com/stretchr/testify/assert"
)

func mediaReply(t *testing.T, parts ...gent.ContentPart) *gent.ModelToolCallResponse {
	t.Helper()
	record, err := gent.NewToolCallRecordBuilder("execution-1", "attempt-1", "fetch").
		Execution(&gent.ToolExecutionResponse{Status: gent.ToolExecuted, Parts: parts}).
		ModelReply("call-1", "fetch", parts...).Build()
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	return record.Response()
}

func TestNativeToolMediaEncoding(t *testing.T) {
	type input struct{ provider string }
	type expected struct{ value string }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"Responses", input{"openai"}, expected{`[
{"type":"input_text","text":"before"},
{"type":"input_text","text":"{\"id\":9}"},
{"type":"input_file","filename":"document.pdf","file_data":"data:application/pdf;base64,AQID"},
{"type":"input_text","text":"Check the date."}]`}},
		{"Anthropic", input{"anthropic"}, expected{`{
"type":"tool_result","tool_use_id":"call-1","content":[
{"type":"text","text":"before"}, {"type":"text","text":"{\"id\":9}"},
{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"AQID"}},
{"type":"text","text":"Check the date."}]}`}},
		{"Gemini", input{"gemini"}, expected{`[{"functionResponse":{
"name":"fetch","id":"call-1","response":{"result":["before",{"id":9},
{"$ref":"tool_part_2"},"Check the date."]},
"parts":[{"inlineData":{
"mimeType":"application/pdf","data":"AQID","displayName":"tool_part_2"}}]}}]`}},
	}
	data, err := gent.DataPart(map[string]any{"id": 9})
	assert.NoError(t, err)
	reply := mediaReply(t, gent.TextPart("before"), data, gent.PDFPart([]byte{1, 2, 3}),
		gent.TextPart("Check the date."))
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var value any
			var err error
			switch test.input.provider {
			case "openai":
				value, err = NewResponses(&ResponsesConfig{Provider: "openai"}).toolOutput(reply.Parts())
			case "anthropic":
				value, err = anthropicPart(reply)
			case "gemini":
				value, err = geminiToolResponse(reply, map[string]bool{"call-1": true})
			}
			assert.NoError(t, err)
			actual, err := json.Marshal(value)
			assert.NoError(t, err)
			assert.JSONEq(t, test.expected.value, string(actual))
		})
	}
}

func TestTextToolMediaFollowsCompleteBatch(t *testing.T) {
	first := mediaReply(t, gent.TextPart("first"), gent.PNGPart([]byte{1}))
	record, err := gent.NewToolCallRecordBuilder("execution-2", "attempt-1", "fetch").
		Execution(&gent.ToolExecutionResponse{Status: gent.ToolExecuted}).
		ModelReply("call-2", "fetch", gent.TextPart("second"), gent.PNGPart([]byte{2})).Build()
	assert.NoError(t, err)
	messages := []*gent.ModelMessage{{Role: gent.ChatMessageTypeTool,
		Parts: []gent.ContentPart{first}},
		record.ModelMessage()}
	got, err := separateToolMedia(messages)
	assert.NoError(t, err)
	firstText, err := first.WithParts(gent.TextPart("first"))
	assert.NoError(t, err)
	secondText, err := record.Response().WithParts(gent.TextPart("second"))
	assert.NoError(t, err)
	assert.Equal(t, []*gent.ModelMessage{
		{Role: gent.ChatMessageTypeTool, Parts: []gent.ContentPart{firstText}},
		{Role: gent.ChatMessageTypeTool, Parts: []gent.ContentPart{secondText}},
		{Role: gent.ChatMessageTypeHuman, Parts: []gent.ContentPart{
			gent.TextPart("Media from fetch (call call-1):"), gent.PNGPart([]byte{1}),
			gent.TextPart("Media from fetch (call call-2):"), gent.PNGPart([]byte{2})}},
	}, got)
	assert.Equal(t, []gent.ContentPart{gent.TextPart("first"), gent.PNGPart([]byte{1})}, first.Parts())
}

func TestMediaPreparationPreservesSources(t *testing.T) {
	type input struct {
		endpoint string
		status   int
	}
	type expected struct {
		part     gent.ContentPart
		requests int
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	const address = "https://media.example.test/image"
	png := []byte{137, 80, 78, 71, 13, 10, 26, 10, 0}
	cases := []testCase{
		{"direct URL", input{"openai", 200}, expected{gent.ImageURLPart(address), 0}},
		{"download", input{"gemini", 200}, expected{gent.PNGPart(png), 1}},
		{"expired URL", input{"ollama", 403},
			expected{gent.TextPart("Request a new image. URL: " + address), 1}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			client := &http.Client{Transport: responseTransport(func(request *http.Request) (
				*http.Response, error,
			) {
				requests++
				assert.Equal(t, address, request.URL.String())
				response := fixtureHTTP(string(png), "image/png")
				response.StatusCode = test.input.status
				return response, nil
			})}
			source := gent.ImageURLPart(address)
			original := gent.NewModelCallRequest([]*gent.ModelMessage{
				{Role: gent.ChatMessageTypeHuman, Parts: []gent.ContentPart{source}},
				{Role: gent.ChatMessageTypeTool, Parts: []gent.ContentPart{mediaReply(t, source)}}})
			original.Options.SteeringMessages.ImageURLLoadFailed = "Request a new image."
			prepared, err := prepareMedia(context.Background(), client, original, test.input.endpoint)
			assert.NoError(t, err)
			assert.Equal(t, []gent.ContentPart{test.expected.part}, prepared.Messages[0].Parts)
			assert.Equal(t, []gent.ContentPart{test.expected.part},
				prepared.Messages[1].Parts[0].(*gent.ModelToolCallResponse).Parts())
			assert.Equal(t, 2*test.expected.requests, requests)
			assert.Equal(t, []gent.ContentPart{source}, original.Messages[0].Parts)
		})
	}
}

func TestProviderImageFailureUsesSafeGuidance(t *testing.T) {
	type input struct {
		code   string
		status int
	}
	type expected struct {
		requests int
		failure  bool
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"expired image", input{"failed_to_download_image", 400}, expected{2, false}},
		{"authentication", input{"invalid_api_key", 401}, expected{1, true}},
		{"rate limit", input{"rate_limit_exceeded", 429}, expected{1, true}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			var bodies []map[string]any
			client := &http.Client{Transport: responseTransport(func(request *http.Request) (
				*http.Response, error,
			) {
				requests++
				var body map[string]any
				assert.NoError(t, json.NewDecoder(request.Body).Decode(&body))
				bodies = append(bodies, body)
				if requests == 1 {
					response := fixtureHTTP(`{"error":{"code":"`+test.input.code+
						`","message":"PRIVATE PROVIDER DIAGNOSTIC"}}`, "application/json")
					response.StatusCode = test.input.status
					return response, nil
				}
				return fixtureHTTP(providerResponse("openai", 3), "application/json"), nil
			})}
			model := NewResponses(&ResponsesConfig{Provider: "openai", Model: "fixture", HTTPClient: client})
			ctx := gent.NewExecutionContext(context.Background(), "media", nil)
			defer ctx.Release()
			request := gent.NewModelCallRequest([]*gent.ModelMessage{{Role: gent.ChatMessageTypeHuman,
				Parts: []gent.ContentPart{gent.ImageURLPart("https://media.example.test/expired")}}})
			stream, err := ctx.GenerateContentStream(model, gent.ModelCall{}, request)
			if !assert.NoError(t, err) {
				return
			}
			for range stream.Chunks() {
			}
			_, err = stream.Response()
			stream.Close()
			assert.Equal(t, test.expected.failure, err != nil)
			assert.Equal(t, test.expected.requests, requests)
			if test.expected.failure {
				return
			}
			encoded, err := json.Marshal(bodies[1]["input"])
			assert.NoError(t, err)
			text := "The image URL failed to load. Decide whether to request a new URL. " +
				"URL: https://media.example.test/expired"
			want, err := json.Marshal([]map[string]any{{"role": "user",
				"content": []map[string]any{{"type": "input_text", "text": text}}}})
			assert.NoError(t, err)
			assert.JSONEq(t, string(want), string(encoded))
			events := ctx.Events()
			var evidence []map[string]any
			for _, event := range events {
				if common, ok := event.(*gent.CommonEvent); ok && common.EventName == "gent:media_prepared" {
					evidence = append(evidence, common.Data.(map[string]any))
				}
			}
			if assert.Len(t, evidence, 1) {
				assert.Equal(t, "provider_image_url_load_failed", evidence[0]["reason"])
				assert.Equal(t, gent.ImageURLPart("https://media.example.test/expired"),
					evidence[0]["original"])
			}
			assert.False(t, strings.Contains(string(encoded), "PRIVATE"))
		})
	}
}

func TestUnsupportedMediaSavesPrivatePreparationEvidence(t *testing.T) {
	source := gent.BytesPart("application/zip", []byte{1, 2, 3})
	replacement := gent.TextPart("Ask for a text summary. MIME type: application/zip.")
	var evidence []map[string]any
	ctx := modelcontext.WithRecorder(context.Background(), func(value map[string]any) error {
		evidence = append(evidence, value)
		return nil
	})
	reply := mediaReply(t, source)
	original := gent.NewModelCallRequest([]*gent.ModelMessage{
		{Role: gent.ChatMessageTypeTool, Parts: []gent.ContentPart{reply}},
	})
	original.Options.SteeringMessages.UnsupportedMedia = "Ask for a text summary."
	prepared, err := prepareMedia(ctx, http.DefaultClient, original, "openai")
	assert.NoError(t, err)
	expectedReply, err := reply.WithParts(replacement)
	assert.NoError(t, err)
	assert.Equal(t, []*gent.ModelMessage{
		{Role: gent.ChatMessageTypeTool, Parts: []gent.ContentPart{expectedReply}},
	}, prepared.Messages)
	assert.Equal(t, []map[string]any{{
		"original": source,
		"prepared": replacement,
		"reason":   "unsupported_media",
		"path":     "messages[0].parts[0].parts[0]",
		"error":    (*gent.SavedError)(nil),
	}}, evidence)
	assert.Equal(t, []gent.ContentPart{source}, reply.Parts())
}
