package output_test

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/output"
	"github.com/stretchr/testify/assert"
)

func setupSchema[T any](raw map[string]any) error {
	_, err := output.NewSchemaOutput[T](raw)
	return err
}

func TestSchemaOutputSetupChecksNestedTypes(t *testing.T) {
	type count struct {
		Count int `json:"count"`
	}
	type quotedCount struct {
		Count int `json:"count,string"`
	}
	type collection struct {
		Items []count `json:"items"`
	}
	type invalid struct {
		Lookup map[float64]string `json:"lookup"`
	}
	type one struct {
		Value int `json:"value"`
	}
	type two struct {
		Value int `json:"value"`
	}
	type ambiguous struct {
		one
		two
	}
	type input struct {
		schema string
		setup  func(map[string]any) error
	}
	type expected struct{ err string }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"float map key", input{`{"type":"object","additionalProperties":{"type":"string"}}`,
			setupSchema[map[float64]string]}, expected{
			"output: unsupported JSON map key type float64 in map[float64]string"}},
		{"invalid type without declared schema types", input{`{}`, setupSchema[invalid]}, expected{
			`output: field "lookup": unsupported JSON map key type float64 in map[float64]string`}},
		{"incompatible required field", input{`{"type":"object","required":["count"],
"properties":{"count":{"type":"string"}}}`, setupSchema[count]}, expected{
			`output: field "count": JSON schema type string cannot be stored as int`}},
		{"nested array field", input{`{"type":"object","properties":{"items":{
"type":"array","items":{"type":"object","properties":{"count":{"type":"string"}}}}}}`,
			setupSchema[collection]}, expected{`output: field "items": array element: ` +
			`field "count": JSON schema type string cannot be stored as int`}},
		{"map element", input{`{"type":"object","additionalProperties":{"type":"string"}}`,
			setupSchema[map[string]int]}, expected{
			"output: map value: JSON schema type string cannot be stored as int"}},
		{"struct additional property", input{`{"type":"object","required":["count"],
"additionalProperties":{"type":"string"}}`, setupSchema[count]}, expected{
			`output: field "count": JSON schema type string cannot be stored as int`}},
		{"struct pattern property", input{`{"type":"object","required":["count"],
"patternProperties":{"^c":{"type":"string"}}}`, setupSchema[count]}, expected{
			`output: field "count": JSON schema type string cannot be stored as int`}},
		{"array element", input{`{"type":"array","items":{"type":"boolean"}}`,
			setupSchema[[]int]}, expected{
			"output: array element: JSON schema type boolean cannot be stored as int"}},
		{"tuple element", input{`{"type":"array","prefixItems":[{"type":"string"}]}`,
			setupSchema[[1]int]}, expected{
			"output: array element: JSON schema type string cannot be stored as int"}},
		{"reference", input{`{"type":"array","items":{"$ref":"#/$defs/count"},
"$defs":{"count":{"type":"object","properties":{"count":{"type":"string"}}}}}`,
			setupSchema[[]count]}, expected{
			`output: array element: field "count": JSON schema type string cannot be stored as int`}},
		{"nested union", input{`{"type":"object","properties":{"count":{
"anyOf":[{"type":"integer"},{"type":"string"}]}}}`, setupSchema[count]}, expected{
			`output: field "count": JSON schema type string cannot be stored as int`}},
		{"quoted scalar", input{`{"type":"object","properties":{"count":{"type":"integer"}}}`,
			setupSchema[quotedCount]}, expected{
			`output: field "count": JSON schema type integer cannot be stored as int`}},
		{"unsupported interface", input{`{}`, setupSchema[error]}, expected{
			"output: unsupported JSON result type error"}},
		{"decoder interface has no concrete value", input{`{}`, setupSchema[json.Unmarshaler]},
			expected{"output: unsupported JSON result type json.Unmarshaler"}},
		{"ambiguous embedded JSON fields", input{`{"type":"object","required":["value"],
"properties":{"value":{"type":"integer"}}}`, setupSchema[ambiguous]}, expected{
			`output: JSON field "value" has no destination in output_test.ambiguous`}},
		{"enum without type", input{`{"enum":["one","two"]}`, setupSchema[int]}, expected{
			"output: JSON schema type string cannot be stored as int"}},
		{"text decoder needs string", input{`{"type":"object"}`, setupSchema[textCode]}, expected{
			"output: JSON schema type object cannot be stored as output_test.textCode"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var raw map[string]any
			decoder := json.NewDecoder(strings.NewReader(test.input.schema))
			decoder.UseNumber()
			if !assert.NoError(t, decoder.Decode(&raw)) {
				return
			}
			err := test.input.setup(raw)
			assert.EqualError(t, err, test.expected.err)
		})
	}
}

type textCode struct{ value string }

func (code *textCode) UnmarshalText(value []byte) error {
	code.value = string(value)
	return nil
}

func (code textCode) MarshalText() ([]byte, error) { return []byte(code.value), nil }

type decimalKey float64

func (key *decimalKey) UnmarshalText(value []byte) error {
	parsed, err := strconv.ParseFloat(string(value), 64)
	*key = decimalKey(parsed)
	return err
}

func (key decimalKey) MarshalText() ([]byte, error) {
	return []byte(strconv.FormatFloat(float64(key), 'f', -1, 64)), nil
}

// The explicit schema is the custom representation's contract, not the struct field layout.
type packedCode struct{ Value string }

func (code *packedCode) UnmarshalJSON(value []byte) error {
	return json.Unmarshal(value, &code.Value)
}

func (code packedCode) MarshalJSON() ([]byte, error) { return json.Marshal(code.Value) }

type linkedCount struct {
	Count int          `json:"count"`
	Next  *linkedCount `json:"next,omitempty"`
}

func processSchema[T any](raw string) func(*gent.ExecutionContext, string) (any, error) {
	return func(ctx *gent.ExecutionContext, candidate string) (any, error) {
		var document map[string]any
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&document); err != nil {
			return nil, err
		}
		out, err := output.NewSchemaOutput[T](document)
		if err != nil {
			return nil, err
		}
		return out.Process(ctx, text(candidate))
	}
}

func TestSchemaOutputValueErrorsRemainSteering(t *testing.T) {
	type input struct {
		candidate string
		process   func(*gent.ExecutionContext, string) (any, error)
	}
	type expected struct{ result any }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"integer overflow", input{`128`, processSchema[int8](`{"type":"integer"}`)},
			expected{&gent.OutputResult[int8]{Steering: text(
				"The JSON cannot be stored as int8: json: cannot unmarshal number 128 " +
					"into Go value of type int8. Return corrected JSON.")}}},
		{"integer map key syntax", input{`{"x":"value"}`, processSchema[map[int]string](
			`{"type":"object","additionalProperties":{"type":"string"}}`)},
			expected{&gent.OutputResult[map[int]string]{Steering: text(
				"The JSON cannot be stored as map[int]string: json: cannot unmarshal number x " +
					"into Go value of type int. Return corrected JSON.")}}},
		{"fixed array length", input{`[1]`, processSchema[[2]int](
			`{"type":"array","items":{"type":"integer"}}`)},
			expected{&gent.OutputResult[[2]int]{Steering: text(
				"The JSON cannot be stored as [2]int: array has 1 elements; [2]int requires 2. " +
					"Return corrected JSON.")}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			result, err := test.input.process(execution(t), test.input.candidate)
			assert.NoError(t, err)
			assert.Equal(t, test.expected, expected{result})
		})
	}
}

func TestSchemaOutputSupportedMappings(t *testing.T) {
	type quoted struct {
		Count int `json:"count,string"`
	}
	type left struct {
		Value int `json:"left"`
	}
	type right struct {
		Value bool `json:"right"`
	}
	type embedded struct {
		left
		right
	}
	type dash struct {
		Value int `json:"-,"`
	}
	type input struct {
		candidate string
		process   func(*gent.ExecutionContext, string) (any, error)
	}
	type expected struct{ result any }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"quoted field", input{`{"count":"7"}`, processSchema[quoted](
			`{"type":"object","properties":{"count":{"type":"string"}},"required":["count"]}`)},
			expected{&gent.OutputResult[quoted]{Accepted: true, Value: quoted{Count: 7},
				Content: text(`{"count":"7"}`)}}},
		{"custom JSON", input{`"packed"`, processSchema[packedCode](`{"type":"string"}`)},
			expected{&gent.OutputResult[packedCode]{Accepted: true, Value: packedCode{Value: "packed"},
				Content: text(`"packed"`)}}},
		{"custom text", input{`"text"`, processSchema[textCode](`{"type":"string"}`)},
			expected{&gent.OutputResult[textCode]{Accepted: true, Value: textCode{value: "text"},
				Content: text(`"text"`)}}},
		{"custom map key", input{`{"1.25":"value"}`, processSchema[map[decimalKey]string](
			`{"type":"object","additionalProperties":{"type":"string"}}`)},
			expected{&gent.OutputResult[map[decimalKey]string]{Accepted: true,
				Value: map[decimalKey]string{1.25: "value"}, Content: text(`{"1.25":"value"}`)}}},
		{"integer map key", input{`{"7":"value"}`, processSchema[map[int]string](
			`{"type":"object","additionalProperties":{"type":"string"}}`)},
			expected{&gent.OutputResult[map[int]string]{Accepted: true,
				Value: map[int]string{7: "value"}, Content: text(`{"7":"value"}`)}}},
		{"embedded JSON names", input{`{"left":7,"right":true}`, processSchema[embedded](
			`{"type":"object","properties":{"left":{"type":"integer"},"right":{"type":"boolean"}}}`)},
			expected{&gent.OutputResult[embedded]{Accepted: true,
				Value: embedded{left{7}, right{true}}, Content: text(`{"left":7,"right":true}`)}}},
		{"embedded scalar null", input{`{"left":null,"right":true}`, processSchema[embedded](
			`{"type":"object","properties":{"left":{},"right":{"type":"boolean"}}}`)},
			expected{&gent.OutputResult[embedded]{Steering: text(
				`The JSON cannot be stored as output_test.embedded: field "left": ` +
					"null cannot be stored as int. Return corrected JSON.")}}},
		{"recursive schema and type", input{`{"count":1,"next":{"count":2}}`,
			processSchema[*linkedCount](`{"$ref":"#/$defs/node","$defs":{"node":{
"type":"object","properties":{"count":{"type":"integer"},"next":{"$ref":"#/$defs/node"}},
"required":["count"],"additionalProperties":false}}}`)},
			expected{&gent.OutputResult[*linkedCount]{Accepted: true,
				Value:   &linkedCount{Count: 1, Next: &linkedCount{Count: 2}},
				Content: text(`{"count":1,"next":{"count":2}}`)}}},
		{"binary string", input{`"AQID"`, processSchema[[]byte](`{"type":"string"}`)},
			expected{&gent.OutputResult[[]byte]{Accepted: true, Value: []byte{1, 2, 3},
				Content: text(`"AQID"`)}}},
		{"literal dash field", input{`{"-":7}`, processSchema[dash](
			`{"type":"object","required":["-"],"properties":{"-":{"type":"integer"}}}`)},
			expected{&gent.OutputResult[dash]{Accepted: true, Value: dash{7},
				Content: text(`{"-":7}`)}}},
		{"literal dash scalar null", input{`{"-":null}`, processSchema[dash](
			`{"type":"object","required":["-"],"properties":{"-":{}}}`)},
			expected{&gent.OutputResult[dash]{Steering: text(
				`The JSON cannot be stored as output_test.dash: field "-": ` +
					"null cannot be stored as int. Return corrected JSON.")}}},
		{"standard custom type", input{`"2026-10-30T12:00:00Z"`,
			processSchema[time.Time](`{"type":"string","format":"date-time"}`)},
			expected{&gent.OutputResult[time.Time]{Accepted: true,
				Value:   time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC),
				Content: text(`"2026-10-30T12:00:00Z"`)}}},
		{"integer const with decimal notation", input{`1`, processSchema[int](`{"const":1.0}`)},
			expected{&gent.OutputResult[int]{Accepted: true, Value: 1, Content: text(`1`)}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			result, err := test.input.process(execution(t), test.input.candidate)
			assert.NoError(t, err)
			assert.Equal(t, test.expected, expected{result})
		})
	}
}
