package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNativeSchemaPreservesContract(t *testing.T) {
	type input struct{ profile, schema string }
	type expected struct {
		native bool
		reason string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"OpenAI required object", input{"openai", `{"type":"object",
"properties":{"name":{"type":"string"}},"required":["name"],"additionalProperties":false}`},
			expected{native: true}},
		{"OpenAI optional stays optional", input{"openai", `{"type":"object",
"properties":{"name":{"type":"string"}},"additionalProperties":false}`},
			expected{reason: "OpenAI requires every property to be required"}},
		{"OpenAI nullable field", input{"openai", `{"type":"object",
"properties":{"name":{"type":["string","null"]}},"required":["name"],
"additionalProperties":false}`}, expected{native: true}},
		{"OpenAI nullable root", input{"openai", `{"type":["object","null"]}`},
			expected{reason: "OpenAI requires an object root without anyOf"}},
		{"OpenAI scalar root", input{"openai", `{"type":"integer"}`},
			expected{reason: "OpenAI requires an object root without anyOf"}},
		{"OpenAI free map", input{"openai", `{"type":"object","additionalProperties":true}`},
			expected{reason: "native schema requires additionalProperties false"}},
		{"Anthropic optional field", input{"anthropic", `{"type":"object",
"properties":{"name":{"type":"string"}},"additionalProperties":false}`},
			expected{native: true}},
		{"Anthropic local numeric bound", input{"anthropic", `{"type":"number","minimum":0}`},
			expected{reason: "anthropic does not enforce schema keyword minimum"}},
		{"Anthropic intersection without references", input{"anthropic",
			`{"allOf":[{"type":"string"},{"enum":["approved"]}]}`}, expected{native: true}},
		{"Anthropic intersection reference", input{"anthropic",
			`{"$defs":{"text":{"type":"string"}},"allOf":[{"$ref":"#/$defs/text"}]}`},
			expected{reason: "Anthropic does not support allOf with references"}},
		{"Anthropic nested intersection reference", input{"anthropic",
			`{"$defs":{"text":{"type":"string"}},"allOf":[{"type":"array",
"items":{"$ref":"#/$defs/text"}}]}`},
			expected{reason: "Anthropic does not support allOf with references"}},
		{"Anthropic sibling intersection reference", input{"anthropic",
			`{"$defs":{"text":{"type":"string"}},"$ref":"#/$defs/text",
"allOf":[{"enum":["approved"]}]}`},
			expected{reason: "Anthropic does not support allOf with references"}},
		{"Anthropic separate intersection and reference", input{"anthropic",
			`{"type":"object","additionalProperties":false,"$defs":{"text":{"type":"string"}},
"properties":{"name":{"$ref":"#/$defs/text"},"state":{"allOf":[{"type":"string"}]}}}`},
			expected{native: true}},
		{"Anthropic one array item", input{"anthropic", `{"type":"array",
"items":{"type":"string"},"minItems":1}`}, expected{native: true}},
		{"Anthropic two array items", input{"anthropic", `{"type":"array",
"items":{"type":"string"},"minItems":2}`},
			expected{reason: "Anthropic only enforces minItems 0 or 1"}},
		{"Gemini array root", input{"gemini", `{"type":"array",
"items":{"type":"integer","minimum":1},"minItems":2,"maxItems":4}`}, expected{native: true}},
		{"Gemini ignored string length", input{"gemini", `{"type":"string","minLength":2}`},
			expected{reason: "gemini does not enforce schema keyword minLength"}},
		{"Gemini date format", input{"gemini", `{"type":"string","format":"date"}`},
			expected{native: true}},
		{"llama.cpp integer bounds", input{"llamacpp", `{"type":"integer","minimum":3}`},
			expected{native: true}},
		{"llama.cpp number bounds", input{"llamacpp", `{"type":"number","minimum":3}`},
			expected{reason: "llama.cpp only enforces numeric bounds for integer types"}},
		{"llama.cpp default object semantics", input{"llamacpp", `{"type":"object"}`},
			expected{reason: "native object defaults differ from JSON Schema"}},
		{"llama.cpp open map", input{"llamacpp", `{"type":"object","additionalProperties":true}`},
			expected{reason: "llama.cpp cannot guarantee valid arbitrary property names"}},
		{"llama.cpp unanchored regex", input{"llamacpp", `{"type":"string","pattern":"abc"}`},
			expected{reason: "llama.cpp requires an anchored pattern"}},
		{"llama.cpp anchored regex", input{"llamacpp", `{"type":"string","pattern":"^[a-z]+$"}`},
			expected{native: true}},
		{"llama.cpp broken tuple support", input{"llamacpp", `{"type":"array",
"prefixItems":[{"type":"string"}]}`},
			expected{reason: "llamacpp does not enforce schema keyword prefixItems"}},
		{"xAI supported bound", input{"xai", `{"type":"string","maxLength":2048}`},
			expected{native: true}},
		{"xAI best effort bound", input{"xai", `{"type":"string","maxLength":2049}`},
			expected{reason: "xAI cannot enforce this constraint bound"}},
		{"xAI oneOf is not anyOf", input{"xai", `{"oneOf":[{"type":"string"},{"type":"string"}]}`},
			expected{reason: "xai does not enforce schema keyword oneOf"}},
		{"xAI best effort not", input{"xai", `{"not":{"type":"string"}}`},
			expected{reason: "xai does not enforce schema keyword not"}},
		{"runtime unknown extension", input{"xgrammar", `{"type":"string","customRule":true}`},
			expected{reason: "xgrammar does not enforce schema keyword customRule"}},
		{"provider unavailable", input{"", `{"type":"string"}`},
			expected{reason: "endpoint does not support native answer schemas"}},
		{"unknown profile", input{"other", `{"type":"string"}`},
			expected{reason: "unknown native schema profile"}},
		{"recursive Claude reference", input{"anthropic", `{"type":"object",
"properties":{"child":{"$ref":"#"}},"additionalProperties":false}`},
			expected{reason: "native schema profile does not support recursive references"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var schema map[string]any
			if !assert.NoError(t, json.Unmarshal([]byte(test.input.schema), &schema)) {
				return
			}
			before, err := json.Marshal(schema)
			assert.NoError(t, err)
			decision := nativeSchema(test.input.profile, schema)
			assert.Equal(t, test.expected, expected{decision.Native, decision.Reason})
			after, err := json.Marshal(schema)
			assert.NoError(t, err)
			assert.Equal(t, before, after)
		})
	}
}
