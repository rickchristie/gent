package models

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func sizedSchema(count int, optional bool) map[string]any {
	properties := make(map[string]any, count)
	required := make([]any, 0, count)
	for i := range count {
		name := fmt.Sprintf("field_%d", i)
		properties[name] = map[string]any{"type": "string"}
		if !optional {
			required = append(required, name)
		}
	}
	return map[string]any{"type": "object", "properties": properties,
		"required": required, "additionalProperties": false}
}

func TestNativeSchemaLimits(t *testing.T) {
	type input struct {
		profile string
		schema  map[string]any
	}
	type expected struct {
		native bool
		reason string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	enumSchema := func(count, width int) map[string]any {
		values := make([]any, count)
		for i := range count {
			values[i] = fmt.Sprintf("%0*d", width, i)
		}
		schema := sizedSchema(1, false)
		schema["properties"].(map[string]any)["field_0"] = map[string]any{
			"type": "string", "enum": values}
		return schema
	}
	unionSchema := func(count int) map[string]any {
		schema := sizedSchema(count, false)
		for _, property := range schema["properties"].(map[string]any) {
			property.(map[string]any)["type"] = []any{"string", "null"}
		}
		return schema
	}
	stringSchema := func(count int) map[string]any {
		schema := sizedSchema(1, false)
		schema["properties"].(map[string]any)["field_0"] = map[string]any{
			"type": "string", "const": strings.Repeat("a", count-7)}
		return schema
	}
	cases := []testCase{
		{"OpenAI enum boundary", input{"openai", enumSchema(1000, 4)}, expected{true, ""}},
		{"OpenAI enum overflow", input{"openai", enumSchema(1001, 4)},
			expected{false, "OpenAI supports at most 1000 enum values"}},
		{"OpenAI large enum strings", input{"openai", enumSchema(251, 60)},
			expected{false, "OpenAI large enum exceeds 15000 characters"}},
		{"OpenAI string boundary", input{"openai", stringSchema(120000)}, expected{true, ""}},
		{"OpenAI string overflow", input{"openai", stringSchema(120001)},
			expected{false, "OpenAI schema names and values exceed 120000 characters"}},
		{"Claude union boundary", input{"anthropic", unionSchema(16)}, expected{true, ""}},
		{"Claude union overflow", input{"anthropic", unionSchema(17)},
			expected{false, "Anthropic supports at most 16 union parameters across strict schemas"}},
		{"OpenAI property boundary", input{"openai", sizedSchema(5000, false)}, expected{true, ""}},
		{"OpenAI property overflow", input{"openai", sizedSchema(5001, false)},
			expected{false, "OpenAI supports at most 5000 schema properties"}},
		{"Claude optional boundary", input{"anthropic", sizedSchema(24, true)}, expected{true, ""}},
		{"Claude optional overflow", input{"anthropic", sizedSchema(25, true)},
			expected{false, "Anthropic supports at most 24 optional parameters across strict schemas"}},
		{"Claude complex enum", input{"anthropic", map[string]any{"enum": []any{map[string]any{}}}},
			expected{false, "Anthropic does not support complex enum values"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			actual := nativeSchema(test.input.profile, test.input.schema)
			assert.Equal(t, test.expected, expected{actual.Native, actual.Reason})
		})
	}
}

func TestAnthropicCombinedSchemaLimitKeepsExplicitToolContract(t *testing.T) {
	type input struct{ optional, toolCount int }
	type expected struct {
		native bool
		err    string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"combined boundary", input{23, 1}, expected{native: true}},
		{"output uses local checks", input{24, 1}, expected{}},
		{"strict tool schema cannot be weakened", input{25, 1},
			expected{err: "Anthropic supports at most 24 optional parameters across strict schemas"}},
		{"strict tool count boundary", input{0, 20}, expected{native: true}},
		{"strict tool count overflow", input{0, 21},
			expected{err: "Anthropic supports at most 20 strict tools"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			posts := 0
			client := &http.Client{Transport: responseTransport(func(request *http.Request) (
				*http.Response, error,
			) {
				posts++
				var body map[string]any
				assert.NoError(t, json.NewDecoder(request.Body).Decode(&body))
				_, native := body["output_config"]
				assert.Equal(t, test.expected.native, native)
				for _, tool := range body["tools"].([]any) {
					assert.Equal(t, true, tool.(map[string]any)["strict"])
				}
				return fixtureHTTP(providerResponse("anthropic", 3), "application/json"), nil
			})}
			request := gent.NewModelCallRequest([]*gent.ModelMessage{
				gent.TextParts(gent.ChatMessageTypeHuman, "Read.")})
			request.Options.ResponseSchema = sizedSchema(1, true)
			for i := range test.input.toolCount {
				request.Options.Tools = append(request.Options.Tools, gent.ToolDefinition{
					Type: "function", Function: &gent.FunctionDefinition{
						Name: fmt.Sprintf("tool_%d", i), Strict: true,
						Parameters: sizedSchema(test.input.optional, true)}})
			}
			stream, err := fixtureModel("anthropic", client).GenerateContentStream(
				context.Background(), request)
			if test.expected.err != "" {
				assert.EqualError(t, err, test.expected.err)
				assert.Nil(t, stream)
				assert.Zero(t, posts)
				return
			}
			if !assert.NoError(t, err) {
				return
			}
			for range stream.Chunks() {
			}
			_, err = stream.Response()
			stream.Close()
			assert.NoError(t, err)
			assert.Equal(t, 1, posts)
		})
	}
}
