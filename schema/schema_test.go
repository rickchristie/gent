package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompile(t *testing.T) {
	type input struct {
		raw map[string]any
	}

	type expected struct {
		isNil   bool
		hasErr  bool
		rawIsNil bool
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name:  "nil schema returns nil",
			input: input{raw: nil},
			expected: expected{
				isNil:    true,
				hasErr:   false,
				rawIsNil: true,
			},
		},
		{
			name: "valid schema compiles",
			input: input{
				raw: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{"type": "string"},
					},
				},
			},
			expected: expected{
				isNil:    false,
				hasErr:   false,
				rawIsNil: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := Compile(tt.input.raw)

			if tt.expected.hasErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			if tt.expected.isNil {
				assert.Nil(t, s)
			} else {
				assert.NotNil(t, s)
				if !tt.expected.rawIsNil {
					assert.NotNil(t, s.Raw())
				}
			}
		})
	}
}

func TestSchema_Validate(t *testing.T) {
	type input struct {
		schema map[string]any
		data   map[string]any
	}

	type expected struct {
		hasErr          bool
		isValidationErr bool
		errMsg          string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "valid data passes",
			input: input{
				schema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{"type": "string"},
						"age":  map[string]any{"type": "integer"},
					},
					"required": []any{"name"},
				},
				data: map[string]any{
					"name": "John",
					"age":  30,
				},
			},
			expected: expected{
				hasErr:          false,
				isValidationErr: false,
			},
		},
		{
			name: "missing required field fails",
			input: input{
				schema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{"type": "string"},
					},
					"required": []any{"name"},
				},
				data: map[string]any{},
			},
			expected: expected{
				hasErr:          true,
				isValidationErr: true,
			},
		},
		{
			name: "wrong type fails",
			input: input{
				schema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"count": map[string]any{"type": "integer"},
					},
				},
				data: map[string]any{
					"count": "not an integer",
				},
			},
			expected: expected{
				hasErr:          true,
				isValidationErr: true,
			},
		},
		{
			name: "nil data with required fields",
			input: input{
				schema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"keyword": map[string]any{
							"type": "string",
						},
					},
					"required": []any{"keyword"},
				},
				data: nil,
			},
			expected: expected{
				hasErr:          true,
				isValidationErr: true,
				errMsg: "schema validation failed: " +
					"args is null or missing, " +
					"expected object with " +
					"required properties: keyword",
			},
		},
		{
			name: "nil data with no required fields",
			input: input{
				schema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"optional": map[string]any{
							"type": "string",
						},
					},
				},
				data: nil,
			},
			expected: expected{
				hasErr:          true,
				isValidationErr: true,
				errMsg: "schema validation failed: " +
					"args is null or missing, " +
					"expected object with " +
					"required properties: (none)",
			},
		},
		{
			name: "nil data with multiple required fields",
			input: input{
				schema: Object(map[string]*Property{
					"query": String("Search query"),
					"limit": Integer("Max results"),
				}, "query", "limit"),
				data: nil,
			},
			expected: expected{
				hasErr:          true,
				isValidationErr: true,
				errMsg: "schema validation failed: " +
					"args is null or missing, " +
					"expected object with " +
					"required properties: query, limit",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := Compile(tt.input.schema)
			require.NoError(t, err)

			err = s.Validate(tt.input.data)

			if tt.expected.hasErr {
				assert.Error(t, err)
				if tt.expected.isValidationErr {
					_, ok := err.(*ValidationError)
					assert.True(t, ok,
						"expected *ValidationError, got %T", err)
				}
				if tt.expected.errMsg != "" {
					assert.Equal(t, tt.expected.errMsg, err.Error())
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSchema_Validate_NilSchema(t *testing.T) {
	var s *Schema
	err := s.Validate(map[string]any{"foo": "bar"})
	assert.NoError(t, err, "nil schema should always pass validation")
}

func TestMustCompile(t *testing.T) {
	type input struct {
		raw map[string]any
	}

	type expected struct {
		isNil bool
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name:     "valid schema returns non-nil",
			input:    input{raw: map[string]any{"type": "object"}},
			expected: expected{isNil: false},
		},
		{
			name:     "nil input returns nil",
			input:    input{raw: nil},
			expected: expected{isNil: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := MustCompile(tt.input.raw)

			if tt.expected.isNil {
				assert.Nil(t, s)
			} else {
				assert.NotNil(t, s)
			}
		})
	}
}

func TestObject_Basic(t *testing.T) {
	schema := Object(map[string]*Property{
		"name": String("The name"),
		"age":  Integer("The age"),
	}, "name")

	assert.Equal(t, "object", schema["type"])

	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok, "expected properties map")
	assert.Len(t, props, 2)

	required, ok := schema["required"].([]string)
	require.True(t, ok, "expected required array")
	assert.Equal(t, []string{"name"}, required)
}

func TestString_WithConstraints(t *testing.T) {
	prop := String("A description").
		MinLength(1).
		MaxLength(100).
		Pattern("^[a-z]+$").
		Format("email")

	built := prop.build()

	assert.Equal(t, "string", built["type"])
	assert.Equal(t, "A description", built["description"])
	assert.Equal(t, 1, built["minLength"])
	assert.Equal(t, 100, built["maxLength"])
	assert.Equal(t, "^[a-z]+$", built["pattern"])
	assert.Equal(t, "email", built["format"])
}

func TestInteger_WithConstraints(t *testing.T) {
	prop := Integer("A count").Min(0).Max(100)

	built := prop.build()

	assert.Equal(t, "integer", built["type"])
	assert.Equal(t, float64(0), built["minimum"])
	assert.Equal(t, float64(100), built["maximum"])
}

func TestNumber_Basic(t *testing.T) {
	prop := Number("A price")
	built := prop.build()

	assert.Equal(t, "number", built["type"])
	assert.Equal(t, "A price", built["description"])
}

func TestBoolean_Basic(t *testing.T) {
	prop := Boolean("A flag")
	built := prop.build()

	assert.Equal(t, "boolean", built["type"])
	assert.Equal(t, "A flag", built["description"])
}

func TestArray_Basic(t *testing.T) {
	items := map[string]any{"type": "string"}
	prop := Array("A list", items)
	built := prop.build()

	assert.Equal(t, "array", built["type"])
	assert.Equal(t, "A list", built["description"])
	assert.NotNil(t, built["items"])
}

func TestProperty_Enum(t *testing.T) {
	prop := String("A status").Enum("pending", "active", "closed")
	built := prop.build()

	enum, ok := built["enum"].([]any)
	require.True(t, ok, "expected enum array")
	assert.Equal(t, []any{"pending", "active", "closed"}, enum)
}

func TestProperty_Default(t *testing.T) {
	prop := String("A field").Default("default_value")
	built := prop.build()

	assert.Equal(t, "default_value", built["default"])
}

func TestValidationError_Error(t *testing.T) {
	originalErr := &ValidationError{Err: nil}
	msg := originalErr.Error()
	assert.Equal(t, "schema validation failed: <nil>", msg)
}

func TestValidationError_Unwrap(t *testing.T) {
	inner := &ValidationError{}
	outer := &ValidationError{Err: inner}

	unwrapped := outer.Unwrap()
	assert.Equal(t, inner, unwrapped)
}

func TestBuilderSchema_Validation(t *testing.T) {
	type input struct {
		data map[string]any
	}

	type expected struct {
		hasErr bool
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "valid data passes",
			input: input{
				data: map[string]any{
					"name":  "John",
					"email": "john@example.com",
					"age":   30,
				},
			},
			expected: expected{hasErr: false},
		},
		{
			name: "missing required email fails",
			input: input{
				data: map[string]any{
					"name": "John",
				},
			},
			expected: expected{hasErr: true},
		},
	}

	raw := Object(map[string]*Property{
		"name":  String("User name").MinLength(1),
		"email": String("Email address").Format("email"),
		"age":   Integer("Age").Min(0).Max(150),
	}, "name", "email")

	s, err := Compile(raw)
	require.NoError(t, err)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := s.Validate(tt.input.data)

			if tt.expected.hasErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSchema_DescribeFields(t *testing.T) {
	type input struct {
		schema *Schema
	}

	type expected struct {
		output string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "simple types",
			input: input{
				schema: MustCompile(Object(
					map[string]*Property{
						"name":    String("User name"),
						"age":     Integer("User age"),
						"score":   Number("User score"),
						"active":  Boolean("Is active"),
					},
				)),
			},
			expected: expected{
				output: "" +
					"  - 'active' (boolean): Is active\n" +
					"  - 'age' (integer): User age\n" +
					"  - 'name' (string): User name\n" +
					"  - 'score' (number): User score\n",
			},
		},
		{
			name: "required vs optional",
			input: input{
				schema: MustCompile(Object(
					map[string]*Property{
						"order_id": String("The order ID"),
						"details":  String("Description"),
						"count":    Integer("Optional count"),
					},
					"order_id", "details",
				)),
			},
			expected: expected{
				output: "" +
					"  - 'count' (integer): " +
					"Optional count\n" +
					"  - 'details' (required, string):" +
					" Description\n" +
					"  - 'order_id' (required, string):" +
					" The order ID\n",
			},
		},
		{
			name: "array of string items",
			input: input{
				schema: MustCompile(Object(
					map[string]*Property{
						"tags": Array(
							"List of tags",
							map[string]any{"type": "string"},
						),
					},
					"tags",
				)),
			},
			expected: expected{
				output: "  - 'tags' (required," +
					" array of string): List of tags\n",
			},
		},
		{
			name: "array of object items",
			input: input{
				schema: MustCompile(Object(
					map[string]*Property{
						"users": Array(
							"List of users",
							Object(map[string]*Property{
								"name": String("Name"),
							}),
						),
					},
				)),
			},
			expected: expected{
				output: "  - 'users'" +
					" (array of object):" +
					" List of users\n",
			},
		},
		{
			name: "object property",
			input: input{
				schema: &Schema{
					raw: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"address": map[string]any{
								"type": "object",
								"description": "Mailing address",
								"properties": map[string]any{
									"street": map[string]any{
										"type": "string",
									},
								},
							},
						},
						"required": []string{"address"},
					},
				},
			},
			expected: expected{
				output: "  - 'address'" +
					" (required, object):" +
					" Mailing address\n",
			},
		},
		{
			name: "no properties",
			input: input{
				schema: MustCompile(
					map[string]any{"type": "object"},
				),
			},
			expected: expected{
				output: "",
			},
		},
		{
			name: "nil schema",
			input: input{
				schema: nil,
			},
			expected: expected{
				output: "",
			},
		},
		{
			name: "boolean and number types",
			input: input{
				schema: MustCompile(Object(
					map[string]*Property{
						"enabled": Boolean("Is enabled"),
						"price":   Number("Item price"),
					},
					"enabled",
				)),
			},
			expected: expected{
				output: "" +
					"  - 'enabled' (required," +
					" boolean): Is enabled\n" +
					"  - 'price' (number):" +
					" Item price\n",
			},
		},
		{
			name: "property without description",
			input: input{
				schema: &Schema{
					raw: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"id": map[string]any{
								"type": "string",
							},
						},
					},
				},
			},
			expected: expected{
				output: "  - 'id' (string)\n",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.input.schema.DescribeFields()
			assert.Equal(t, tt.expected.output, result)
		})
	}
}

func TestSchema_FormatForLLM(t *testing.T) {
	type input struct {
		schema   *Schema
		toolName string
		data     map[string]any
	}

	type expected struct {
		output string
	}

	caseSchema := MustCompile(Object(
		map[string]*Property{
			"order_id": String("The order ID"),
			"details":  String("Description of the issue"),
			"count":    Integer("Optional count"),
		},
		"order_id", "details",
	))

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "missing single required field",
			input: input{
				schema:   caseSchema,
				toolName: "create_case",
				data: map[string]any{
					"order_id": "ORD-123",
				},
			},
			expected: expected{
				output: "" +
					"Invalid args for tool 'create_case'.\n" +
					"Errors:\n" +
					"  - missing property 'details'\n" +
					"Expected fields:\n" +
					"  - 'count' (integer):" +
					" Optional count\n" +
					"  - 'details' (required, string):" +
					" Description of the issue\n" +
					"  - 'order_id' (required, string):" +
					" The order ID\n",
			},
		},
		{
			name: "missing multiple required fields",
			input: input{
				schema:   caseSchema,
				toolName: "create_case",
				data:     map[string]any{},
			},
			expected: expected{
				output: "" +
					"Invalid args for tool 'create_case'.\n" +
					"Errors:\n" +
					"  - missing properties" +
					" 'order_id', 'details'\n" +
					"Expected fields:\n" +
					"  - 'count' (integer):" +
					" Optional count\n" +
					"  - 'details' (required, string):" +
					" Description of the issue\n" +
					"  - 'order_id' (required, string):" +
					" The order ID\n",
			},
		},
		{
			name: "wrong type",
			input: input{
				schema:   caseSchema,
				toolName: "create_case",
				data: map[string]any{
					"order_id": "ORD-123",
					"details":  "some issue",
					"count":    "not-an-integer",
				},
			},
			expected: expected{
				output: "" +
					"Invalid args for tool 'create_case'.\n" +
					"Errors:\n" +
					"  - got string, want integer" +
					" for 'count'\n" +
					"Expected fields:\n" +
					"  - 'count' (integer):" +
					" Optional count\n" +
					"  - 'details' (required, string):" +
					" Description of the issue\n" +
					"  - 'order_id' (required, string):" +
					" The order ID\n",
			},
		},
		{
			name: "valid data returns empty string",
			input: input{
				schema:   caseSchema,
				toolName: "create_case",
				data: map[string]any{
					"order_id": "ORD-123",
					"details":  "some issue",
				},
			},
			expected: expected{
				output: "",
			},
		},
		{
			name: "nil data returns helpful message",
			input: input{
				schema:   caseSchema,
				toolName: "create_case",
				data:     nil,
			},
			expected: expected{
				output: "" +
					"Invalid args for tool 'create_case'.\n" +
					"Errors:\n" +
					"  - args is null or missing," +
					" expected object with" +
					" required properties:" +
					" order_id, details\n" +
					"Expected fields:\n" +
					"  - 'count' (integer):" +
					" Optional count\n" +
					"  - 'details' (required, string):" +
					" Description of the issue\n" +
					"  - 'order_id' (required, string):" +
					" The order ID\n",
			},
		},
		{
			name: "complex schema with array and nested",
			input: input{
				schema: MustCompile(Object(
					map[string]*Property{
						"tags": Array(
							"Tag list",
							map[string]any{"type": "string"},
						),
						"meta": String("Metadata"),
					},
					"tags",
				)),
				toolName: "tag_item",
				data:     map[string]any{},
			},
			expected: expected{
				output: "" +
					"Invalid args for tool 'tag_item'.\n" +
					"Errors:\n" +
					"  - missing property 'tags'\n" +
					"Expected fields:\n" +
					"  - 'meta' (string): Metadata\n" +
					"  - 'tags' (required," +
					" array of string): Tag list\n",
			},
		},
		{
			name: "nil schema returns empty string",
			input: input{
				schema:   nil,
				toolName: "anything",
				data:     map[string]any{"foo": "bar"},
			},
			expected: expected{
				output: "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.input.schema.FormatForLLM(
				tt.input.toolName,
				tt.input.data,
			)
			assert.Equal(t, tt.expected.output, result)
		})
	}
}

func TestSchema_FormatForLLM_NoFilePaths(t *testing.T) {
	s := MustCompile(Object(
		map[string]*Property{
			"name": String("Name"),
		},
		"name",
	))

	result := s.FormatForLLM("test_tool", map[string]any{})
	assert.NotContains(t, result, "file:///")
	assert.NotContains(t, result, "schema.json")
}
