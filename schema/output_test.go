package schema

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSchemaValidateRootValues(t *testing.T) {
	type input struct {
		schema map[string]any
		value  any
	}
	type expected struct {
		err error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"string", input{map[string]any{"type": "string", "minLength": 2}, "ok"}, expected{}},
		{"boolean", input{map[string]any{"type": "boolean"}, false}, expected{}},
		{"large integer", input{map[string]any{"type": "integer",
			"minimum": json.Number("18446744073709551615"),
			"maximum": json.Number("18446744073709551615")},
			json.Number("18446744073709551615")}, expected{}},
		{"exact decimal", input{map[string]any{"type": "number",
			"enum": []any{json.Number("0.12345678901234567890123456789")}},
			json.Number("0.12345678901234567890123456789")}, expected{}},
		{"array", input{map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			[]any{"first", "second"}}, expected{}},
		{"explicit null", input{map[string]any{"type": "null"}, nil}, expected{}},
		{"nullable object", input{map[string]any{"anyOf": []any{
			map[string]any{"type": "object"}, map[string]any{"type": "null"},
		}}, nil}, expected{}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			compiled, err := Compile(tt.input.schema)
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, tt.expected.err, compiled.Validate(tt.input.value))
		})
	}
}

func TestSchemaCompiledSnapshotOwnership(t *testing.T) {
	input := Object(map[string]*Property{"name": String("Customer name")}, "name")
	input["additionalProperties"] = false
	compiled, err := Compile(input)
	if !assert.NoError(t, err) {
		return
	}
	expected := map[string]any{"type": "object", "additionalProperties": false,
		"required": []any{"name"}, "properties": map[string]any{
			"name": map[string]any{"type": "string", "description": "Customer name"},
		}}
	input["properties"].(map[string]any)["name"].(map[string]any)["type"] = "integer"
	returned := compiled.Raw()
	returned["required"].([]any)[0] = "changed"
	returned["properties"].(map[string]any)["name"].(map[string]any)["description"] = "changed"
	assert.Equal(t, expected, compiled.Raw())
	assert.NoError(t, compiled.Validate(map[string]any{"name": "Ada"}))
}
