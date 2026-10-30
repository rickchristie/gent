package models

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestProvidersNativeOutputRequest(t *testing.T) {
	type input struct{ provider string }
	type expected struct {
		path, header, auth, body string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	const schemaJSON = `{"type":"object","additionalProperties":false,
"properties":{"value":{"type":"integer"}},"required":["value"]}`
	const responses = `{"model":"fixture/model","input":[
{"role":"system","content":[{"type":"input_text","text":"Keep my words."}]},
{"role":"user","content":[{"type":"input_text","text":"Return JSON."}]}],
"stream":true,"store":false,"include":["reasoning.encrypted_content"],
"max_output_tokens":64,"tools":[],"text":{"format":{
"type":"json_schema","name":"gent_output","strict":true,"schema":SCHEMA}}}`
	const chat = `{"model":"fixture/model","messages":[
{"role":"system","content":"Keep my words."},{"role":"user","content":"Return JSON."}],
"stream":true,"stream_options":{"include_usage":true},"max_tokens":64,FORMAT}`
	const strictFormat = `"response_format":{"type":"json_schema",
"json_schema":{"name":"gent_output","strict":true,"schema":SCHEMA}}`
	cases := []testCase{
		{"OpenAI", input{"openai"}, expected{"/v1/responses", "Authorization", "Bearer fixture-key",
			responses}},
		{"xAI", input{"xai"}, expected{"/v1/responses", "Authorization", "Bearer fixture-key",
			responses}},
		{"Anthropic", input{"anthropic"}, expected{"/v1/messages", "X-Api-Key", "fixture-key",
			`{"model":"fixture/model","system":[{"type":"text","text":"Keep my words."}],
"messages":[{"role":"user","content":[{"type":"text","text":"Return JSON."}]}],
"stream":true,"max_tokens":64,"output_config":{"format":{"type":"json_schema","schema":SCHEMA}}}`}},
		{"Gemini", input{"gemini"}, expected{
			"/v1/models/fixture%2Fmodel:streamGenerateContent?alt=sse", "X-Goog-Api-Key", "fixture-key",
			`{"contents":[{"role":"user","parts":[{"text":"Return JSON."}]}],
"systemInstruction":{"parts":[{"text":"Keep my words."}]},"generationConfig":{
"maxOutputTokens":64,"thinkingConfig":{"includeThoughts":true},
"responseFormat":{"text":{"mimeType":"APPLICATION_JSON","schema":SCHEMA}}}}`}},
		{"DeepSeek", input{"deepseek"}, expected{"/v1/chat/completions", "Authorization",
			"Bearer fixture-key", strings.ReplaceAll(chat, "FORMAT",
				`"response_format":{"type":"json_object"}`)}},
		{"vLLM", input{"vllm"}, expected{"/v1/chat/completions", "Authorization", "Bearer fixture-key",
			strings.ReplaceAll(chat, "FORMAT", strictFormat)}},
		{"SGLang", input{"sglang"}, expected{"/v1/chat/completions", "Authorization",
			"Bearer fixture-key", strings.ReplaceAll(chat, "FORMAT", strictFormat)}},
		{"llama.cpp", input{"llamacpp"}, expected{"/v1/chat/completions", "Authorization",
			"Bearer fixture-key", strings.ReplaceAll(chat, "FORMAT",
				`"response_format":{"type":"json_schema","schema":SCHEMA}`)}},
		{"Ollama", input{"ollama"}, expected{"/v1/api/chat", "Authorization", "Bearer fixture-key",
			`{"model":"fixture/model","messages":[{"role":"system","content":"Keep my words."},
{"role":"user","content":"Return JSON."}],"stream":true,"options":{"num_predict":64},
"format":SCHEMA}`}},
		{"OpenRouter", input{"openrouter"}, expected{"/v1/chat/completions", "Authorization",
			"Bearer fixture-key", strings.ReplaceAll(chat, "FORMAT", strictFormat+
				`,"provider":{"only":["openai"],"require_parameters":true}`)}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var body map[string]any
			var path, auth string
			client := &http.Client{Transport: responseTransport(func(request *http.Request) (
				*http.Response, error,
			) {
				if request.Method == http.MethodGet {
					return fixtureHTTP(routerFixture, "application/json"), nil
				}
				path = request.URL.RequestURI()
				auth = request.Header.Get(test.expected.header)
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					return nil, err
				}
				return fixtureHTTP(providerResponse(test.input.provider, 3), "application/json"), nil
			})}
			request := gent.NewModelCallRequest([]*gent.ModelMessage{
				gent.TextParts(gent.ChatMessageTypeSystem, "Keep my words."),
				gent.TextParts(gent.ChatMessageTypeHuman, "Return JSON."),
			}, gent.WithMaxTokens(64))
			request.Options.ResponseSchema = providerOutputSchema()
			request.Options.ResponseMIMEType = "application/json"
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
			var expectedBody map[string]any
			assert.NoError(t, json.Unmarshal([]byte(strings.ReplaceAll(test.expected.body,
				"SCHEMA", schemaJSON)), &expectedBody))
			assert.Equal(t, expectedBody, body)
			assert.Equal(t, test.expected.path, path)
			assert.Equal(t, test.expected.auth, auth)
		})
	}
}

func TestAnthropicIntersectionSchemaRequest(t *testing.T) {
	type input struct{ schema string }
	type expected struct{ native bool }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"native intersection", input{`{"allOf":[{"type":"string"},{"enum":["approved"]}]}`},
			expected{true}},
		{"reference uses local validation", input{`{"$defs":{"text":{"type":"string"}},
"allOf":[{"$ref":"#/$defs/text"},{"enum":["approved"]}]}`}, expected{false}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var body map[string]any
			client := &http.Client{Transport: responseTransport(func(request *http.Request) (
				*http.Response, error,
			) {
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					return nil, err
				}
				return fixtureHTTP(providerResponse("anthropic", 3), "application/json"), nil
			})}
			request := gent.NewModelCallRequest([]*gent.ModelMessage{
				gent.TextParts(gent.ChatMessageTypeHuman, "Return the approved status as JSON."),
			}, gent.WithMaxTokens(64))
			assert.NoError(t, json.Unmarshal([]byte(test.input.schema), &request.Options.ResponseSchema))
			stream, err := fixtureModel("anthropic", client).GenerateContentStream(
				context.Background(), request)
			if !assert.NoError(t, err) {
				return
			}
			for range stream.Chunks() {
			}
			_, err = stream.Response()
			stream.Close()
			assert.NoError(t, err)
			var want map[string]any
			assert.NoError(t, json.Unmarshal([]byte(`{"model":"fixture/model","stream":true,
"max_tokens":64,"messages":[{"role":"user","content":[{"type":"text",
"text":"Return the approved status as JSON."}]}]}`), &want))
			if test.expected.native {
				want["output_config"] = map[string]any{"format": map[string]any{
					"type": "json_schema", "schema": request.Options.ResponseSchema}}
			}
			assert.Equal(t, want, body)
			actualSchema, err := json.Marshal(request.Options.ResponseSchema)
			assert.NoError(t, err)
			assert.JSONEq(t, test.input.schema, string(actualSchema))
		})
	}
}
