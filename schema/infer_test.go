package schema

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type recursiveInput struct {
	Next *recursiveInput `json:"next"`
}

type customInput string

func (customInput) MarshalJSON() ([]byte, error) { return []byte(`"custom"`), nil }

func TestSchemaFromType(t *testing.T) {
	type record struct {
		Name    string    `json:"name,omitempty"`
		Count   *int      `json:"count"`
		Tags    []string  `json:"tags"`
		Created time.Time `json:"created"`
		Ignored string    `json:"-"`
	}
	type input struct {
		typeOf reflect.Type
		output bool
	}
	type expected struct {
		schema string
		err    string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"input presence differs from omitempty", input{reflect.TypeFor[record](), false}, expected{`{
  "type":"object", "additionalProperties":false,
  "required":["name","count","tags","created"],
  "properties":{
    "name":{"type":"string"},
    "count":{"anyOf":[{"type":"integer"},{"type":"null"}]},
    "tags":{"anyOf":[{"type":"array","items":{"type":"string"}},{"type":"null"}]},
    "created":{"type":"string","format":"date-time"}
  }
}`, ""}},
		{"output omissions", input{reflect.TypeFor[record](), true}, expected{`{
  "type":"object", "additionalProperties":false, "required":["count","tags","created"],
  "properties":{
    "name":{"type":"string"},
    "count":{"anyOf":[{"type":"integer"},{"type":"null"}]},
    "tags":{"anyOf":[{"type":"array","items":{"type":"string"}},{"type":"null"}]},
    "created":{"type":"string","format":"date-time"}
  }
}`, ""}},
		{"fixed array", input{reflect.TypeFor[[2]bool](), true}, expected{`{
  "type":"array","items":{"type":"boolean"},"minItems":2,"maxItems":2
}`, ""}},
		{"byte slice", input{reflect.TypeFor[[]byte](), true}, expected{`{
  "anyOf":[{"type":"string","contentEncoding":"base64"},{"type":"null"}]
}`, ""}},
		{"string map", input{reflect.TypeFor[map[string]int](), true}, expected{`{
  "anyOf":[{"type":"object","additionalProperties":{"type":"integer"}},{"type":"null"}]
}`, ""}},
		{"recursive type", input{reflect.TypeFor[recursiveInput](), false},
			expected{err: `field "next": recursive type schema.recursiveInput needs an explicit schema`}},
		{"custom encoding", input{reflect.TypeFor[customInput](), false},
			expected{err: "custom JSON type schema.customInput needs an explicit schema"}},
		{"text encoding", input{reflect.TypeFor[net.IP](), false},
			expected{err: "custom JSON type net.IP needs an explicit schema"}},
		{"JSON number", input{reflect.TypeFor[json.Number](), false},
			expected{`{"type":"number"}`, ""}},
		{"unsigned integer", input{reflect.TypeFor[uint](), false},
			expected{`{"type":"integer","minimum":0}`, ""}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			build := FromType
			if test.input.output {
				build = FromOutputType
			}
			raw, err := build(test.input.typeOf)
			if test.expected.err != "" {
				assert.EqualError(t, err, test.expected.err)
				assert.Nil(t, raw)
				return
			}
			assert.NoError(t, err)
			got, err := Copy(raw)
			assert.NoError(t, err)
			var want map[string]any
			decoder := json.NewDecoder(strings.NewReader(test.expected.schema))
			decoder.UseNumber()
			assert.NoError(t, decoder.Decode(&want))
			assert.Equal(t, want, got)
			_, err = Compile(raw)
			assert.NoError(t, err)
		})
	}
}

func TestCopyExactNumbers(t *testing.T) {
	type input struct{ value any }
	type expected struct {
		number json.Number
		err    error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"large unsigned integer", input{uint64(9007199254740993)},
			expected{number: json.Number("9007199254740993")}},
		{"maximum unsigned integer", input{uint64(18446744073709551615)},
			expected{number: json.Number("18446744073709551615")}},
		{"minimum signed integer", input{int64(-9223372036854775808)},
			expected{number: json.Number("-9223372036854775808")}},
		{"exact decimal", input{json.Number("0.12345678901234567890123456789")},
			expected{number: json.Number("0.12345678901234567890123456789")}},
		{"decimal exponent", input{json.Number("1234567890123456789e-20")},
			expected{number: json.Number("1234567890123456789e-20")}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			raw := map[string]any{
				"type": "object", "required": []string{"value"},
				"properties": map[string]any{"value": map[string]any{
					"type": "number", "minimum": test.input.value, "maximum": test.input.value,
					"enum": []any{test.input.value},
				}},
			}
			want := map[string]any{
				"type": "object", "required": []any{"value"},
				"properties": map[string]any{"value": map[string]any{
					"type": "number", "minimum": test.expected.number, "maximum": test.expected.number,
					"enum": []any{test.expected.number},
				}},
			}
			copied, err := Copy(raw)
			if !assert.Equal(t, test.expected.err, err) {
				return
			}
			assert.Equal(t, want, copied)
			validator, err := Compile(copied)
			if !assert.NoError(t, err) {
				return
			}
			assert.NoError(t, validator.Validate(map[string]any{"value": test.expected.number}))
		})
	}
}

func TestFieldConstraints(t *testing.T) {
	cwd, err := os.Getwd()
	if !assert.NoError(t, err) {
		return
	}
	location := "file://" + filepath.ToSlash(filepath.Join(cwd, "schema.json"))
	failure := func(detail string) string {
		return fmt.Sprintf(`schema validation failed: jsonschema validation failed with '%s#'
%s`, location, detail)
	}
	type address struct {
		City string `json:"city"`
	}
	type record struct {
		Address *address `json:"address"`
		Email   string   `json:"email"`
		Tags    []string `json:"tags"`
	}
	type input struct{ value map[string]any }
	type expected struct{ err string }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	raw, err := FromType(reflect.TypeFor[record]())
	assert.NoError(t, err)
	assert.NoError(t, ApplyFields(raw, []*FieldBuilder{
		Field("address.city").Optional().Enum("Paris", "Oslo"),
		Field("email").Format("email"), Field("tags").MinItems(1).MaxItems(2),
	}))
	validator, err := Compile(raw)
	assert.NoError(t, err)
	cases := []testCase{
		{"optional nested field", input{map[string]any{"address": map[string]any{},
			"email": "reader@example.com", "tags": []any{"one"}}}, expected{}},
		{"unknown enum", input{map[string]any{"address": map[string]any{"city": "Rome"},
			"email": "reader@example.com", "tags": []any{"one"}}},
			expected{failure(`- at '/address': 'anyOf' failed
  - at '/address/city': value must be one of 'Paris', 'Oslo'
  - at '/address': got object, want null`)}},
		{"format is checked", input{map[string]any{"address": nil,
			"email": "invalid", "tags": []any{"one"}}},
			expected{failure(`- at '/email': 'invalid' is not valid email: missing @`)}},
		{"minimum items", input{map[string]any{"address": nil,
			"email": "reader@example.com", "tags": []any{}}}, expected{failure(`- at '/tags': 'anyOf' failed
  - at '/tags': minItems: got 0, want 1
  - at '/tags': got array, want null`)}},
		{"null differs from absent", input{map[string]any{"address": nil,
			"email": "reader@example.com", "tags": nil}}, expected{}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			err := validator.Validate(test.input.value)
			if test.expected.err == "" {
				assert.NoError(t, err)
				return
			}
			assert.EqualError(t, err, test.expected.err)
		})
	}
}
