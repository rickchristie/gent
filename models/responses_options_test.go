package models

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestResponsesManagedOptions(t *testing.T) {
	type input struct {
		options *gent.CallOptions
		repeats int
	}
	type expected struct {
		body map[string]any
		err  string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	base := map[string]any{"model": "fixture", "input": []any{}, "stream": true,
		"store": false, "tools": []any{}, "include": []any{"reasoning.encrypted_content"}}
	bodyWith := func(fields map[string]any) map[string]any {
		body := maps.Clone(base)
		maps.Copy(body, fields)
		return body
	}
	choice := gent.ToolChoice{Type: "function", Function: &gent.FunctionReference{Name: "lookup"}}
	choiceBody := bodyWith(map[string]any{
		"tool_choice": map[string]any{"type": "function", "name": "lookup"},
	})
	zero := float64(0)
	cases := []testCase{
		{"omitted options", input{&gent.CallOptions{}, 1}, expected{body: base}},
		{"explicit sampling zero", input{&gent.CallOptions{Temperature: &zero, TopP: &zero}, 1},
			expected{body: bodyWith(map[string]any{"temperature": float64(0), "top_p": float64(0)})}},
		{"tool choice value", input{&gent.CallOptions{ToolChoice: choice}, 1},
			expected{body: choiceBody}},
		{"tool choice pointer", input{&gent.CallOptions{ToolChoice: &choice}, 1},
			expected{body: choiceBody}},
		{"required tool choice", input{&gent.CallOptions{ToolChoice: "required"}, 1},
			expected{body: bodyWith(map[string]any{"tool_choice": "required"})}},
		{"generic effort", input{&gent.CallOptions{Metadata: map[string]any{
			"thinking_config": &gent.ThinkingConfig{Mode: "high"},
		}}, 1}, expected{body: bodyWith(map[string]any{
			"reasoning": map[string]any{"effort": "high"},
		})}},
		{"merge independent reasoning fields", input{&gent.CallOptions{Metadata: map[string]any{
			"thinking_config": &gent.ThinkingConfig{Mode: "high"},
			"responses":       map[string]any{"reasoning": map[string]any{"summary": "auto"}},
		}}, 128}, expected{body: bodyWith(map[string]any{
			"reasoning": map[string]any{"effort": "high", "summary": "auto"},
		})}},
		{"provider effort overrides generic effort", input{&gent.CallOptions{Metadata: map[string]any{
			"thinking_config": &gent.ThinkingConfig{Mode: "high"},
			"responses": map[string]any{
				"reasoning": map[string]any{"effort": "low", "summary": "auto"},
			},
		}}, 128}, expected{body: bodyWith(map[string]any{
			"reasoning": map[string]any{"effort": "low", "summary": "auto"},
		})}},
		{"provider summary only", input{&gent.CallOptions{Metadata: map[string]any{
			"responses": map[string]any{"reasoning": map[string]any{"summary": "auto"}},
		}}, 1}, expected{body: bodyWith(map[string]any{
			"reasoning": map[string]any{"summary": "auto"},
		})}},
		{"invalid reasoning object", input{&gent.CallOptions{Metadata: map[string]any{
			"responses": map[string]any{"reasoning": "high"},
		}}, 1}, expected{err: "Responses reasoning option must be an object"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var body map[string]any
			model := NewResponses(&ResponsesConfig{
				Provider: "openai", BaseURL: "https://fixture.test/v1", Model: "fixture",
				HTTPClient: &http.Client{Transport: responseTransport(func(
					request *http.Request,
				) (*http.Response, error) {
					if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
						return nil, err
					}
					return sseResponse(`{"type":"response.completed",
"response":{"status":"completed","output":[]}}`), nil
				})},
			})
			request := gent.NewModelCallRequest(nil, gent.WithOptions(test.input.options))
			for attempt := range test.input.repeats {
				body = nil
				ctx := gent.NewExecutionContext(context.Background(), "fixture", nil)
				stream, err := ctx.GenerateContentStream(model, gent.ModelCall{}, request)
				if err == nil {
					for range stream.Chunks() {
					}
					_, err = stream.Response()
					stream.Close()
				}
				actual := expected{body: body}
				if err != nil {
					actual.err = err.Error()
				}
				if !assert.Equal(t, test.expected, actual, "attempt %d", attempt) {
					return
				}
			}
		})
	}
}
