package models

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestGeminiToolChoiceWithJSONOutput(t *testing.T) {
	type input struct {
		choice   any
		schema   bool
		jsonMode bool
		mime     string
		model    string
		selected string
		noTools  bool
	}
	type expected struct {
		config   map[string]any
		choice   map[string]any
		decision map[string]any
		err      error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	plain := map[string]any{"thinkingConfig": map[string]any{"includeThoughts": true}}
	structured := map[string]any{"thinkingConfig": map[string]any{"includeThoughts": true},
		"responseMimeType": "application/json", "responseJsonSchema": map[string]any{
			"type": "object", "additionalProperties": false,
			"properties": map[string]any{"value": map[string]any{"type": "integer"}},
			"required":   []any{"value"}}}
	forced := map[string]any{"native": false, "profile": "gemini",
		"reason": "forced tool choice has no answer candidate"}
	unsupported := map[string]any{"native": false, "profile": "gemini",
		"reason": "model does not support JSON output with tools"}
	native := map[string]any{"native": true, "profile": "gemini", "reason": ""}
	noSchema := map[string]any{"native": false, "profile": "gemini",
		"reason": "no output schema requested"}
	anyChoice := map[string]any{"functionCallingConfig": map[string]any{"mode": "ANY"}}
	named := gent.ToolChoice{Type: "function", Function: &gent.FunctionReference{Name: "lookup"}}
	namedChoice := map[string]any{"functionCallingConfig": map[string]any{
		"mode": "ANY", "allowedFunctionNames": []any{"lookup"}}}
	cases := []testCase{
		{"required schema", input{choice: "required", schema: true},
			expected{plain, anyChoice, forced, nil}},
		{"named schema", input{choice: named, schema: true},
			expected{plain, namedChoice, forced, nil}},
		{"named pointer schema", input{choice: &named, schema: true},
			expected{plain, namedChoice, forced, nil}},
		{"required JSON mode", input{choice: "required", jsonMode: true},
			expected{plain, anyChoice, noSchema, nil}},
		{"required JSON MIME", input{choice: "required", mime: "application/json"},
			expected{plain, anyChoice, noSchema, nil}},
		{"automatic schema", input{choice: "auto", schema: true}, expected{
			plain, map[string]any{"functionCallingConfig": map[string]any{"mode": "AUTO"}},
			unsupported, nil}},
		{"answer schema", input{choice: "none", schema: true}, expected{
			structured, map[string]any{"functionCallingConfig": map[string]any{"mode": "NONE"}},
			native, nil}},
		{"default schema", input{schema: true},
			expected{plain, nil, unsupported, nil}},
		{"automatic JSON mode", input{choice: "auto", jsonMode: true}, expected{
			plain, map[string]any{"functionCallingConfig": map[string]any{"mode": "AUTO"}},
			noSchema, nil}},
		{"default JSON MIME", input{mime: "application/json"}, expected{plain, nil, noSchema, nil}},
		{"no tools", input{schema: true, noTools: true}, expected{structured, nil, native, nil}},
		{"Gemini 3", input{schema: true, model: "gemini-3.8-flash"},
			expected{structured, nil, native, nil}},
		{"Gemini 4", input{schema: true, model: "gemini-4-argon"},
			expected{structured, nil, native, nil}},
		{"request selects Gemini 3", input{schema: true, selected: "models/gemini-3.8-flash"},
			expected{structured, nil, native, nil}},
		{"request selects Gemini 2", input{schema: true, model: "gemini-3.8-flash",
			selected: "gemini-2.5-flash"}, expected{plain, nil, unsupported, nil}},
		{"unknown alias", input{schema: true, model: "application-model"},
			expected{plain, nil, unsupported, nil}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			actual := expected{}
			client := &http.Client{Transport: responseTransport(func(request *http.Request) (
				*http.Response, error,
			) {
				var body map[string]any
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					return nil, err
				}
				actual.config, _ = body["generationConfig"].(map[string]any)
				actual.choice, _ = body["toolConfig"].(map[string]any)
				return fixtureHTTP(providerResponse("gemini", 0), "application/json"), nil
			})}
			request := gent.NewModelCallRequest([]*gent.ModelMessage{
				gent.TextParts(gent.ChatMessageTypeHuman, "Read the record."),
			})
			request.Options.Tools = []*gent.ToolDefinition{{Type: "function",
				Function: &gent.FunctionDefinition{Name: "lookup", Parameters: responseInputSchema()}}}
			if test.input.noTools {
				request.Options.Tools = nil
			}
			request.Options.Model = test.input.selected
			request.Options.ToolChoice = test.input.choice
			request.Options.JSONMode, request.Options.ResponseMIMEType = test.input.jsonMode, test.input.mime
			if test.input.schema {
				request.Options.ResponseSchema = providerOutputSchema()
			}
			before, err := json.Marshal(request.Options)
			if !assert.NoError(t, err) {
				return
			}
			model := test.input.model
			if model == "" {
				model = "gemini-2.5-flash"
			}
			stream, err := NewGemini(&Config{Model: model, HTTPClient: client}).GenerateContentStream(
				context.Background(), request)
			if !assert.NoError(t, err) {
				return
			}
			defer stream.Close()
			for range stream.Chunks() {
			}
			response, err := stream.Response()
			actual.err = err
			if !assert.NoError(t, err) {
				return
			}
			actual.decision = response.Info.RawGenerationInfo["output_schema"].(map[string]any)
			assert.Equal(t, test.expected, actual)
			after, err := json.Marshal(request.Options)
			assert.NoError(t, err)
			assert.Equal(t, string(before), string(after), "the next answer still needs its schema")
		})
	}
}
