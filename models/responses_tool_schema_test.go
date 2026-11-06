package models

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestResponsesToolOutputSchemaDelivery(t *testing.T) {
	domain := map[string]any{"type": "object", "properties": map[string]any{
		"id": map[string]any{"type": "string"}}}
	parameters := map[string]any{"type": "object", "properties": map[string]any{}}
	data := mustTestDataPart(map[string]any{"id": "O1"})
	dataPointer := data.(gent.DataContent)
	textPointer := gent.TextContent{Text: `{"id":"O1"}`}
	const description = "Fetch the order."
	const schemaDescription = `Fetch the order.

Domain output schema (descriptive only):
{"properties":{"id":{"type":"string"}},"type":"object"}`

	type reply struct {
		name  string
		parts []gent.ContentPart
	}
	type input struct {
		provider string
		schema   map[string]any
		replies  []reply
	}
	type expected struct {
		native      bool
		description string
		outputs     []any
		err         error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	native := func(outputs ...any) expected {
		return expected{native: true, description: description, outputs: outputs}
	}
	fallback := func(outputs ...any) expected {
		return expected{description: schemaDescription, outputs: outputs}
	}
	parts := func(parts ...gent.ContentPart) []reply {
		return []reply{{name: "fetch", parts: parts}}
	}
	cases := []testCase{
		{"initial definition", input{"openai", domain, nil}, native()},
		{"no schema", input{"openai", nil, parts(data)},
			expected{description: description, outputs: []any{`{"id":"O1"}`}}},
		{"domain data", input{"openai", domain, parts(data)}, native(`{"id":"O1"}`)},
		{"data pointer", input{"openai", domain, parts(&dataPointer)}, native(`{"id":"O1"}`)},
		{"JSON text", input{"openai", domain, parts(gent.TextPart(`{"id":"O1"}`))},
			native(`{"id":"O1"}`)},
		{"JSON text pointer", input{"openai", domain, parts(&textPointer)}, native(`{"id":"O1"}`)},
		{"array schema", input{"openai", map[string]any{"type": "array",
			"items": map[string]any{"type": "string"}}, parts(mustTestDataPart([]string{"O1"}))},
			native(`["O1"]`)},
		{"string schema", input{"openai", map[string]any{"type": "string"},
			parts(mustTestDataPart("O1"))}, native(`"O1"`)},
		{"null schema", input{"openai", map[string]any{"type": "null"},
			parts(mustTestDataPart(nil))}, native(`null`)},
		{"no domain validation", input{"openai", domain, parts(mustTestDataPart(7))}, native(`7`)},
		{"guidance after data", input{"openai", domain, parts(data, gent.TextPart("Check state."))},
			fallback(`{"id":"O1"}
Check state.`)},
		{"text before data", input{"openai", domain, parts(gent.TextPart("Order:"), data)},
			fallback(`Order:
{"id":"O1"}`)},
		{"several data values", input{"openai", domain, parts(data, data)},
			fallback(`{"id":"O1"}
{"id":"O1"}`)},
		{"empty reply", input{"openai", domain, parts()}, fallback("")},
		{"safe error text", input{"openai", domain, parts(gent.TextPart("Check the service."))},
			fallback("Check the service.")},
		{"invalid JSON text", input{"openai", domain, parts(gent.TextPart(`{"id":`))},
			fallback(`{"id":`)},
		{"mixed PDF", input{"openai", domain, parts(data, gent.PDFPart([]byte{1}))},
			fallback([]any{
				map[string]any{"type": "input_text", "text": `{"id":"O1"}`},
				map[string]any{"type": "input_file", "filename": "document.pdf",
					"file_data": "data:application/pdf;base64,AQ=="},
			})},
		{"image URL", input{"openai", domain, parts(gent.ImageURLPart("https://test.invalid/i"))},
			fallback([]any{map[string]any{
				"type": "input_image", "image_url": "https://test.invalid/i",
			}})},
		{"unrelated text reply", input{"openai", domain, []reply{
			{"other", []gent.ContentPart{gent.TextPart("Other reply.")}},
			{"fetch", []gent.ContentPart{data}},
		}}, native("Other reply.", `{"id":"O1"}`)},
		{"earlier text reply", input{"openai", domain, []reply{
			{"fetch", []gent.ContentPart{gent.TextPart("Earlier guidance.")}},
			{"fetch", []gent.ContentPart{data}},
		}}, fallback("Earlier guidance.", `{"id":"O1"}`)},
		{"later text reply", input{"openai", domain, []reply{
			{"fetch", []gent.ContentPart{data}},
			{"fetch", []gent.ContentPart{gent.TextPart("Later guidance.")}},
		}}, fallback(`{"id":"O1"}`, "Later guidance.")},
		{"several JSON replies", input{"openai", domain, []reply{
			{"fetch", []gent.ContentPart{data}}, {"fetch", []gent.ContentPart{data}},
		}}, native(`{"id":"O1"}`, `{"id":"O1"}`)},
		{"xAI", input{"xai", domain, parts(data)}, fallback(`{"id":"O1"}`)},
		{"other Responses endpoint", input{"custom", domain, parts(data)}, fallback(`{"id":"O1"}`)},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			messages := []*gent.ModelMessage{{Role: gent.ChatMessageTypeHuman,
				Parts: []gent.ContentPart{gent.TextPart("Read the order.")}}}
			wantInput := []any{map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "input_text", "text": "Read the order."},
			}}}
			for i, reply := range test.input.replies {
				id := fmt.Sprintf("call-%d", i)
				record, err := gent.NewToolCallRecordBuilder("logical-"+id, "attempt", reply.name).
					Execution(&gent.ToolExecutionResponse{Status: gent.ToolExecuted, Parts: reply.parts}).
					ModelReply(id, reply.name, reply.parts...).Build()
				if !assert.NoError(t, err) {
					return
				}
				messages = append(messages, &gent.ModelMessage{Role: gent.ChatMessageTypeAI,
					Parts: []gent.ContentPart{gent.ModelToolCallRequest{ID: id,
						FunctionCall: &gent.FunctionCall{Name: reply.name, Arguments: "{}"}}}},
					record.ModelMessage())
				wantInput = append(wantInput,
					map[string]any{"type": "function_call", "call_id": id,
						"name": reply.name, "arguments": "{}"},
					map[string]any{"type": "function_call_output", "call_id": id,
						"output": test.expected.outputs[i]})
			}
			var requests [][]byte
			client := &http.Client{Transport: responseTransport(func(request *http.Request) (
				*http.Response, error,
			) {
				data, err := io.ReadAll(request.Body)
				if err != nil {
					return nil, err
				}
				requests = append(requests, data)
				return sseResponse(`{"type":"response.completed",
"response":{"status":"completed","output":[]}}`), nil
			})}
			model := NewResponses(&ResponsesConfig{Provider: test.input.provider,
				BaseURL: "https://fixture.test/v1", Model: "fixture", HTTPClient: client})
			stream, err := model.GenerateContentStream(context.Background(), &gent.ModelCallRequest{
				Messages: messages, Options: gent.CallOptions{Tools: []*gent.ToolDefinition{{
					Type: "function", Function: &gent.FunctionDefinition{
						Name: "fetch", Description: description, Parameters: parameters,
						OutputSchema: test.input.schema,
					},
				}}},
			})
			if !assert.NoError(t, err) {
				return
			}
			defer stream.Close()
			for range stream.Chunks() {
			}
			_, err = stream.Response()
			assert.Equal(t, test.expected.err, err)
			definition := map[string]any{"type": "function", "name": "fetch",
				"description": test.expected.description, "parameters": parameters, "strict": false}
			if test.expected.native {
				definition["output_schema"] = test.input.schema
			}
			want, err := json.Marshal(map[string]any{
				"model": "fixture", "input": wantInput, "tools": []any{definition},
				"stream": true, "store": false, "include": []string{"reasoning.encrypted_content"},
			})
			assert.NoError(t, err)
			if assert.Len(t, requests, 1) {
				assert.JSONEq(t, string(want), string(requests[0]))
			}
		})
	}
}
