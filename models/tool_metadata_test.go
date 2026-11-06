package models

import (
	"encoding/json"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestGeminiToolOutputSchemaMapping(t *testing.T) {
	data, err := gent.DataPart(map[string]any{"id": "O1"})
	assert.NoError(t, err)
	domain := map[string]any{"type": "object", "properties": map[string]any{
		"id": map[string]any{"type": "string"}}}
	type input struct {
		schema map[string]any
		parts  []gent.ContentPart
	}
	type expected struct{ native bool }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"initial definition", input{domain, nil}, expected{true}},
		{"domain and guidance", input{domain, []gent.ContentPart{data, gent.TextPart("Check state.")}},
			expected{true}},
		{"mixed media", input{domain, []gent.ContentPart{data, gent.PDFPart([]byte{1})}}, expected{}},
		{"several data values", input{domain, []gent.ContentPart{data, data}}, expected{}},
		{"text reply", input{domain, []gent.ContentPart{gent.TextPart("Safe guidance.")}}, expected{}},
		{"non object schema", input{map[string]any{"type": "string"}, nil}, expected{}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			parameters := map[string]any{"type": "object"}
			options := gent.CallOptions{Tools: []*gent.ToolDefinition{{Type: "function",
				Function: &gent.FunctionDefinition{Name: "fetch", Description: "Fetch the order.",
					Parameters: parameters, OutputSchema: test.input.schema}}}}
			var messages []*gent.ModelMessage
			if test.input.parts != nil {
				messages = []*gent.ModelMessage{{Role: gent.ChatMessageTypeTool,
					Parts: []gent.ContentPart{mediaReply(t, test.input.parts...)}}}
			}
			body := map[string]any{}
			assert.NoError(t, geminiTools(body, &options, messages))
			declaration := map[string]any{"name": "fetch", "description": "Fetch the order.",
				"parametersJsonSchema": parameters}
			if test.expected.native {
				declaration["responseJsonSchema"] = test.input.schema
			} else {
				encoded, err := json.Marshal(test.input.schema)
				assert.NoError(t, err)
				declaration["description"] = "Fetch the order.\n\nDomain output schema (descriptive only):\n" +
					string(encoded)
			}
			assert.Equal(t, map[string]any{"tools": []any{map[string]any{
				"functionDeclarations": []any{declaration}}}}, body)
			// Endpoints without a native declaration receive the same descriptive metadata.
			fallback, err := describedTools(options.Tools)
			assert.NoError(t, err)
			schemaJSON, err := json.Marshal(test.input.schema)
			assert.NoError(t, err)
			assert.Equal(t, []*gent.ToolDefinition{{Type: "function", Function: &gent.FunctionDefinition{
				Name: "fetch", Parameters: parameters,
				Description: "Fetch the order.\n\nDomain output schema (descriptive only):\n" +
					string(schemaJSON),
			}}}, fallback)
		})
	}
}

func TestAnthropicToolErrorMetadata(t *testing.T) {
	type input struct {
		kind   gent.ToolResponseKind
		status gent.ToolStatus
	}
	type expected struct{ isError bool }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"executed", input{gent.ToolResponseExecution, gent.ToolExecuted}, expected{}},
		{"blocked", input{gent.ToolResponseExecution, gent.ToolBlocked}, expected{}},
		{"invalid arguments", input{gent.ToolResponseExecution, gent.ToolInvalidArguments},
			expected{true}},
		{"call error", input{gent.ToolResponseCallError, ""}, expected{true}},
		{"recovery error", input{gent.ToolResponseRecoveryError, ""}, expected{true}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			builder := gent.NewToolCallRecordBuilder("logical", "attempt", "target")
			switch test.input.kind {
			case gent.ToolResponseExecution:
				builder.Execution(&gent.ToolExecutionResponse{Status: test.input.status})
			case gent.ToolResponseCallError:
				builder.CallError()
			case gent.ToolResponseRecoveryError:
				builder.RecoveryError()
			}
			record, err := builder.ModelReply("provider", "execute_tool",
				gent.TextPart("Safe guidance.")).Build()
			assert.NoError(t, err)
			block, err := anthropicPart(record.Response())
			assert.NoError(t, err)
			want := map[string]any{"type": "tool_result", "tool_use_id": "provider",
				"content": []any{map[string]any{"type": "text", "text": "Safe guidance."}}}
			if test.expected.isError {
				want["is_error"] = true
			}
			assert.Equal(t, want, block)
		})
	}
}
