package output_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/output"
	"github.com/rickchristie/gent/schema"
	"github.com/stretchr/testify/assert"
)

type order struct {
	ID       uint64 `json:"id"`
	Quantity int8   `json:"quantity"`
	Note     string `json:"note,omitempty"`
}

func TestJsonOutputPointerValidation(t *testing.T) {
	type input struct {
		text string
	}
	type expected struct {
		result *gent.OutputResult[*order]
		calls  int
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	directory, err := os.Getwd()
	if !assert.NoError(t, err) {
		return
	}
	location := "file://" + filepath.ToSlash(filepath.Join(directory, "schema.json")) + "#"
	schemaFailure := func(detail string) *gent.OutputResult[*order] {
		return &gent.OutputResult[*order]{Steering: text(fmt.Sprintf(
			`%s: jsonschema validation failed with '%s'
- at '': %s. Return corrected JSON.`,
			"The JSON does not match the schema: schema validation failed", location, detail))}
	}
	cases := []testCase{
		{"exact integer and omitted field", input{`{"id":18446744073709551615,"quantity":2}`},
			expected{calls: 1, result: &gent.OutputResult[*order]{Accepted: true,
				Value:   &order{ID: 18446744073709551615, Quantity: 2},
				Content: text(`{"id":18446744073709551615,"quantity":2}`)}}},
		{"missing required field", input{`{"id":1}`},
			expected{result: schemaFailure("missing property 'quantity'")}},
		{"unknown field", input{`{"id":1,"quantity":2,"extra":true}`},
			expected{result: schemaFailure("additional properties 'extra' not allowed")}},
		{"pointer does not permit null", input{`null`}, expected{result: &gent.OutputResult[*order]{
			Steering: text("The JSON does not match the schema: schema validation failed: " +
				"args is null or missing, expected object with required properties: id, quantity. " +
				"Return corrected JSON.")}}},
		{"integer overflow", input{`{"id":1,"quantity":128}`}, expected{
			result: &gent.OutputResult[*order]{Steering: text(
				"The JSON cannot be stored as *output_test.order: json: cannot unmarshal number 128 " +
					"into Go struct field order.quantity of type int8. Return corrected JSON.")}}},
		{"multiple documents", input{`{"id":1,"quantity":2} true`}, expected{
			result: &gent.OutputResult[*order]{Steering: text(
				"Invalid JSON: more than one JSON value. Return one complete JSON value.")}}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			out, err := output.NewJsonOutput[*order]()
			if !assert.NoError(t, err) {
				return
			}
			calls := 0
			out.SetCustomValidator("order rules", func(
				_ *gent.ExecutionContext, value *order,
			) (*gent.OutputValidationResult, error) {
				calls++
				assert.Equal(t, tt.expected.result.Value, value)
				return &gent.OutputValidationResult{Accepted: true}, nil
			})
			result, err := out.Process(execution(t), text(tt.input.text))
			assert.NoError(t, err)
			assert.Equal(t, tt.expected.result, result)
			assert.Equal(t, tt.expected.calls, calls)
		})
	}
}

func jsonCandidate[T any](value string) func(*gent.ExecutionContext) (any, error) {
	return func(ctx *gent.ExecutionContext) (any, error) {
		out, err := output.NewJsonOutput[T]()
		if err != nil {
			return nil, err
		}
		return out.Process(ctx, text(value))
	}
}

func TestJsonOutputResultTypes(t *testing.T) {
	type input struct {
		process func(*gent.ExecutionContext) (any, error)
	}
	type expected struct {
		result any
		err    error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"struct value", input{jsonCandidate[order](`{"id":1,"quantity":2}`)}, expected{
			result: &gent.OutputResult[order]{Accepted: true, Value: order{ID: 1, Quantity: 2},
				Content: text(`{"id":1,"quantity":2}`)}}},
		{"string root", input{jsonCandidate[string](`"text"`)}, expected{
			result: &gent.OutputResult[string]{Accepted: true, Value: "text", Content: text(`"text"`)}}},
		{"array root", input{jsonCandidate[[]bool](`[true,false]`)}, expected{
			result: &gent.OutputResult[[]bool]{Accepted: true, Value: []bool{true, false},
				Content: text(`[true,false]`)}}},
		{"exact dynamic number", input{jsonCandidate[map[string]any](`{"id":9007199254740993}`)},
			expected{result: &gent.OutputResult[map[string]any]{Accepted: true,
				Value:   map[string]any{"id": json.Number("9007199254740993")},
				Content: text(`{"id":9007199254740993}`)}}},
		{"exact scalar decimal", input{jsonCandidate[json.Number](`0.123456789012345678901`)},
			expected{result: &gent.OutputResult[json.Number]{Accepted: true,
				Value:   json.Number("0.123456789012345678901"),
				Content: text(`0.123456789012345678901`)}}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.input.process(execution(t))
			assert.Equal(t, tt.expected.err, err)
			assert.Equal(t, tt.expected.result, result)
		})
	}
}

func TestSchemaOutputExplicitNullAndOwnedRequests(t *testing.T) {
	raw := schema.Nullable(schema.String("Optional label").Schema())
	out, err := output.NewSchemaOutput[*string](raw)
	if !assert.NoError(t, err) {
		return
	}
	raw["anyOf"] = []any{map[string]any{"type": "integer"}}
	first := gent.NewModelCallRequest(nil)
	assert.NoError(t, out.Configure(&first))
	first.Options.ResponseSchema["anyOf"] = []any{map[string]any{"type": "boolean"}}
	second := gent.NewModelCallRequest(nil)
	assert.NoError(t, out.Configure(&second))
	assert.Equal(t, map[string]any{"anyOf": []any{
		map[string]any{"type": "string", "description": "Optional label"},
		map[string]any{"type": "null"},
	}}, second.Options.ResponseSchema)
	result, err := out.Process(execution(t), text(`null`))
	assert.NoError(t, err)
	assert.Equal(t, &gent.OutputResult[*string]{Accepted: true, Content: text(`null`)}, result)
}
