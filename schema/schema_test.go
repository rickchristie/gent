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
					"  - 'args.active' (boolean): Is active\n" +
					"  - 'args.age' (integer): User age\n" +
					"  - 'args.name' (string): User name\n" +
					"  - 'args.score' (number): User score\n",
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
					"  - 'args.count' (integer): " +
					"Optional count\n" +
					"  - 'args.details' (required, string):" +
					" Description\n" +
					"  - 'args.order_id' (required, string):" +
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
				output: "  - 'args.tags' (required," +
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
				output: "  - 'args.users'" +
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
				output: "  - 'args.address'" +
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
					"  - 'args.enabled' (required," +
					" boolean): Is enabled\n" +
					"  - 'args.price' (number):" +
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
				output: "  - 'args.id' (string)\n",
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
					"  - 'args.count' (integer):" +
					" Optional count\n" +
					"  - 'args.details' (required, string):" +
					" Description of the issue\n" +
					"  - 'args.order_id' (required, string):" +
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
					"  - 'args.count' (integer):" +
					" Optional count\n" +
					"  - 'args.details' (required, string):" +
					" Description of the issue\n" +
					"  - 'args.order_id' (required, string):" +
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
					" for 'args.count'\n" +
					"Expected fields:\n" +
					"  - 'args.count' (integer):" +
					" Optional count\n" +
					"  - 'args.details' (required, string):" +
					" Description of the issue\n" +
					"  - 'args.order_id' (required, string):" +
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
					"  - 'args.count' (integer):" +
					" Optional count\n" +
					"  - 'args.details' (required, string):" +
					" Description of the issue\n" +
					"  - 'args.order_id' (required, string):" +
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
					"  - 'args.meta' (string): Metadata\n" +
					"  - 'args.tags' (required," +
					" array of string): Tag list\n",
			},
		},
		{
			name: "nested object missing sub-field",
			input: input{
				schema: MustCompile(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id": map[string]any{
							"type": "string",
						},
						"address": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"street": map[string]any{
									"type": "string",
								},
								"city": map[string]any{
									"type": "string",
								},
							},
							"required": []any{
								"street", "city",
							},
						},
					},
					"required": []any{
						"id", "address",
					},
				}),
				toolName: "update_address",
				data: map[string]any{
					"id": "1",
					"address": map[string]any{
						"street": "123 Main St",
					},
				},
			},
			expected: expected{
				output: "" +
					"Invalid args for tool" +
					" 'update_address'.\n" +
					"Errors:\n" +
					"  - missing property 'city'" +
					" for 'args.address'\n" +
					"Expected fields:\n" +
					"  - 'args.address'" +
					" (required, object)\n" +
					"  - 'args.id'" +
					" (required, string)\n",
			},
		},
		{
			name: "array of objects missing " +
				"item field",
			input: input{
				schema: MustCompile(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id": map[string]any{
							"type": "string",
						},
						"items": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"name": map[string]any{
										"type": "string",
									},
									"qty": map[string]any{
										"type": "integer",
									},
								},
								"required": []any{
									"name", "qty",
								},
							},
						},
					},
					"required": []any{
						"id", "items",
					},
				}),
				toolName: "create_order",
				data: map[string]any{
					"id": "O1",
					"items": []any{
						map[string]any{
							"name": "Widget",
						},
					},
				},
			},
			expected: expected{
				output: "" +
					"Invalid args for tool" +
					" 'create_order'.\n" +
					"Errors:\n" +
					"  - missing property 'qty'" +
					" for 'args.items[]'\n" +
					"Expected fields:\n" +
					"  - 'args.id'" +
					" (required, string)\n" +
					"  - 'args.items'" +
					" (required," +
					" array of object)\n",
			},
		},
		{
			name: "map of objects missing " +
				"value field",
			input: input{
				schema: MustCompile(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id": map[string]any{
							"type": "string",
						},
						"quantities": map[string]any{
							"type": "object",
							"additionalProperties": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"amount": map[string]any{
										"type": "integer",
									},
									"unit": map[string]any{
										"type": "string",
									},
								},
								"required": []any{
									"amount", "unit",
								},
							},
						},
					},
					"required": []any{
						"id", "quantities",
					},
				}),
				toolName: "update_stock",
				data: map[string]any{
					"id": "S1",
					"quantities": map[string]any{
						"apples": map[string]any{
							"amount": 5,
						},
					},
				},
			},
			expected: expected{
				output: "" +
					"Invalid args for tool" +
					" 'update_stock'.\n" +
					"Errors:\n" +
					"  - missing property 'unit'" +
					" for 'args.quantities.apples'\n" +
					"Expected fields:\n" +
					"  - 'args.id'" +
					" (required, string)\n" +
					"  - 'args.quantities'" +
					" (required, object)\n",
			},
		},
		{
			name: "object contains object " +
				"missing deep field",
			input: input{
				schema: MustCompile(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id": map[string]any{
							"type": "string",
						},
						"address": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"street": map[string]any{
									"type": "string",
								},
								"geo": map[string]any{
									"type": "object",
									"properties": map[string]any{
										"lat": map[string]any{
											"type": "number",
										},
										"lng": map[string]any{
											"type": "number",
										},
									},
									"required": []any{
										"lat", "lng",
									},
								},
							},
							"required": []any{
								"street", "geo",
							},
						},
					},
					"required": []any{
						"id", "address",
					},
				}),
				toolName: "update_geo",
				data: map[string]any{
					"id": "1",
					"address": map[string]any{
						"street": "Main St",
						"geo": map[string]any{
							"lat": 1.0,
						},
					},
				},
			},
			expected: expected{
				output: "" +
					"Invalid args for tool" +
					" 'update_geo'.\n" +
					"Errors:\n" +
					"  - missing property 'lng'" +
					" for 'args.address.geo'\n" +
					"Expected fields:\n" +
					"  - 'args.address'" +
					" (required, object)\n" +
					"  - 'args.id'" +
					" (required, string)\n",
			},
		},
		{
			name: "array of object contains " +
				"array of object",
			input: input{
				schema: MustCompile(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id": map[string]any{
							"type": "string",
						},
						"orders": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"order_id": map[string]any{
										"type": "string",
									},
									"items": map[string]any{
										"type": "array",
										"items": map[string]any{
											"type": "object",
											"properties": map[string]any{
												"name": map[string]any{
													"type": "string",
												},
												"qty": map[string]any{
													"type": "integer",
												},
											},
											"required": []any{
												"name", "qty",
											},
										},
									},
								},
								"required": []any{
									"order_id", "items",
								},
							},
						},
					},
					"required": []any{
						"id", "orders",
					},
				}),
				toolName: "create_shipment",
				data: map[string]any{
					"id": "S1",
					"orders": []any{
						map[string]any{
							"order_id": "O1",
							"items": []any{
								map[string]any{
									"name": "Widget",
								},
							},
						},
					},
				},
			},
			expected: expected{
				output: "" +
					"Invalid args for tool" +
					" 'create_shipment'.\n" +
					"Errors:\n" +
					"  - missing property 'qty'" +
					" for 'args.orders[].items[]'\n" +
					"Expected fields:\n" +
					"  - 'args.id'" +
					" (required, string)\n" +
					"  - 'args.orders'" +
					" (required, array of object)\n",
			},
		},
		{
			name: "map of object contains " +
				"map of object",
			input: input{
				schema: MustCompile(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id": map[string]any{
							"type": "string",
						},
						"regions": map[string]any{
							"type": "object",
							"additionalProperties": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"zones": map[string]any{
										"type": "object",
										"additionalProperties": map[string]any{
											"type": "object",
											"properties": map[string]any{
												"code": map[string]any{
													"type": "string",
												},
												"population": map[string]any{
													"type": "integer",
												},
											},
											"required": []any{
												"code",
												"population",
											},
										},
									},
								},
								"required": []any{
									"zones",
								},
							},
						},
					},
					"required": []any{
						"id", "regions",
					},
				}),
				toolName: "update_regions",
				data: map[string]any{
					"id": "R1",
					"regions": map[string]any{
						"us": map[string]any{
							"zones": map[string]any{
								"west": map[string]any{
									"population": 1000,
								},
							},
						},
					},
				},
			},
			expected: expected{
				output: "" +
					"Invalid args for tool" +
					" 'update_regions'.\n" +
					"Errors:\n" +
					"  - missing property 'code'" +
					" for" +
					" 'args.regions.us.zones.west'\n" +
					"Expected fields:\n" +
					"  - 'args.id'" +
					" (required, string)\n" +
					"  - 'args.regions'" +
					" (required, object)\n",
			},
		},
		{
			name: "array of object contains " +
				"map of object",
			input: input{
				schema: MustCompile(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id": map[string]any{
							"type": "string",
						},
						"items": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"name": map[string]any{
										"type": "string",
									},
									"attributes": map[string]any{
										"type": "object",
										"additionalProperties": map[string]any{
											"type": "object",
											"properties": map[string]any{
												"value": map[string]any{
													"type": "string",
												},
												"unit": map[string]any{
													"type": "string",
												},
											},
											"required": []any{
												"value", "unit",
											},
										},
									},
								},
								"required": []any{
									"name",
									"attributes",
								},
							},
						},
					},
					"required": []any{
						"id", "items",
					},
				}),
				toolName: "update_products",
				data: map[string]any{
					"id": "P1",
					"items": []any{
						map[string]any{
							"name": "Widget",
							"attributes": map[string]any{
								"weight": map[string]any{
									"value": "5",
								},
							},
						},
					},
				},
			},
			expected: expected{
				output: "" +
					"Invalid args for tool" +
					" 'update_products'.\n" +
					"Errors:\n" +
					"  - missing property 'unit'" +
					" for" +
					" 'args.items[].attributes" +
					".weight'\n" +
					"Expected fields:\n" +
					"  - 'args.id'" +
					" (required, string)\n" +
					"  - 'args.items'" +
					" (required, array of object)\n",
			},
		},
		{
			name: "map of object contains " +
				"array of object",
			input: input{
				schema: MustCompile(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id": map[string]any{
							"type": "string",
						},
						"categories": map[string]any{
							"type": "object",
							"additionalProperties": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"products": map[string]any{
										"type": "array",
										"items": map[string]any{
											"type": "object",
											"properties": map[string]any{
												"name": map[string]any{
													"type": "string",
												},
												"price": map[string]any{
													"type": "number",
												},
											},
											"required": []any{
												"name",
												"price",
											},
										},
									},
								},
								"required": []any{
									"products",
								},
							},
						},
					},
					"required": []any{
						"id", "categories",
					},
				}),
				toolName: "update_catalog",
				data: map[string]any{
					"id": "C1",
					"categories": map[string]any{
						"electronics": map[string]any{
							"products": []any{
								map[string]any{
									"name": "Phone",
								},
							},
						},
					},
				},
			},
			expected: expected{
				output: "" +
					"Invalid args for tool" +
					" 'update_catalog'.\n" +
					"Errors:\n" +
					"  - missing property 'price'" +
					" for" +
					" 'args.categories.electronics" +
					".products[]'\n" +
					"Expected fields:\n" +
					"  - 'args.categories'" +
					" (required, object)\n" +
					"  - 'args.id'" +
					" (required, string)\n",
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

func TestSchema_ExampleObject(t *testing.T) {
	type input struct {
		schema *Schema
	}

	type expected struct {
		output map[string]any
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
						"name":   String("Name"),
						"count":  Integer("Count"),
						"price":  Number("Price"),
						"active": Boolean("Active"),
					},
				)),
			},
			expected: expected{
				output: map[string]any{
					"name":   "...",
					"count":  0,
					"price":  0,
					"active": false,
				},
			},
		},
		{
			name: "array of objects",
			input: input{
				schema: MustCompile(Object(
					map[string]*Property{
						"items": Array(
							"Items",
							Object(
								map[string]*Property{
									"name": String("Name"),
									"qty":  Integer("Qty"),
								},
								"name", "qty",
							),
						),
					},
				)),
			},
			expected: expected{
				output: map[string]any{
					"items": []any{
						map[string]any{
							"name": "...",
							"qty":  0,
						},
					},
				},
			},
		},
		{
			name: "array of strings",
			input: input{
				schema: MustCompile(Object(
					map[string]*Property{
						"tags": Array(
							"Tags",
							map[string]any{
								"type": "string",
							},
						),
					},
				)),
			},
			expected: expected{
				output: map[string]any{
					"tags": []any{"..."},
				},
			},
		},
		{
			name: "nested object",
			input: input{
				schema: MustCompile(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"address": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"street": map[string]any{
									"type": "string",
								},
								"city": map[string]any{
									"type": "string",
								},
							},
						},
					},
				}),
			},
			expected: expected{
				output: map[string]any{
					"address": map[string]any{
						"street": "...",
						"city":   "...",
					},
				},
			},
		},
		{
			name: "map of objects via " +
				"additionalProperties",
			input: input{
				schema: MustCompile(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"quantities": map[string]any{
							"type": "object",
							"additionalProperties": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"amount": map[string]any{
										"type": "integer",
									},
									"unit": map[string]any{
										"type": "string",
									},
								},
							},
						},
					},
				}),
			},
			expected: expected{
				output: map[string]any{
					"quantities": map[string]any{
						"<key>": map[string]any{
							"amount": 0,
							"unit":   "...",
						},
					},
				},
			},
		},
		{
			name: "object contains object",
			input: input{
				schema: MustCompile(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id": map[string]any{
							"type": "string",
						},
						"address": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"street": map[string]any{
									"type": "string",
								},
								"geo": map[string]any{
									"type": "object",
									"properties": map[string]any{
										"lat": map[string]any{
											"type": "number",
										},
										"lng": map[string]any{
											"type": "number",
										},
									},
								},
							},
						},
					},
				}),
			},
			expected: expected{
				output: map[string]any{
					"id": "...",
					"address": map[string]any{
						"street": "...",
						"geo": map[string]any{
							"lat": 0,
							"lng": 0,
						},
					},
				},
			},
		},
		{
			name: "array of object contains " +
				"array of object",
			input: input{
				schema: MustCompile(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"orders": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"order_id": map[string]any{
										"type": "string",
									},
									"items": map[string]any{
										"type": "array",
										"items": map[string]any{
											"type": "object",
											"properties": map[string]any{
												"name": map[string]any{
													"type": "string",
												},
												"qty": map[string]any{
													"type": "integer",
												},
											},
										},
									},
								},
							},
						},
					},
				}),
			},
			expected: expected{
				output: map[string]any{
					"orders": []any{
						map[string]any{
							"order_id": "...",
							"items": []any{
								map[string]any{
									"name": "...",
									"qty":  0,
								},
							},
						},
					},
				},
			},
		},
		{
			name: "map of object contains " +
				"map of object",
			input: input{
				schema: MustCompile(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"regions": map[string]any{
							"type": "object",
							"additionalProperties": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"zones": map[string]any{
										"type": "object",
										"additionalProperties": map[string]any{
											"type": "object",
											"properties": map[string]any{
												"code": map[string]any{
													"type": "string",
												},
												"population": map[string]any{
													"type": "integer",
												},
											},
										},
									},
								},
							},
						},
					},
				}),
			},
			expected: expected{
				output: map[string]any{
					"regions": map[string]any{
						"<key>": map[string]any{
							"zones": map[string]any{
								"<key>": map[string]any{
									"code":       "...",
									"population": 0,
								},
							},
						},
					},
				},
			},
		},
		{
			name: "array of object contains " +
				"map of object",
			input: input{
				schema: MustCompile(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"items": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"name": map[string]any{
										"type": "string",
									},
									"attributes": map[string]any{
										"type": "object",
										"additionalProperties": map[string]any{
											"type": "object",
											"properties": map[string]any{
												"value": map[string]any{
													"type": "string",
												},
												"unit": map[string]any{
													"type": "string",
												},
											},
										},
									},
								},
							},
						},
					},
				}),
			},
			expected: expected{
				output: map[string]any{
					"items": []any{
						map[string]any{
							"name": "...",
							"attributes": map[string]any{
								"<key>": map[string]any{
									"unit":  "...",
									"value": "...",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "map of object contains " +
				"array of object",
			input: input{
				schema: MustCompile(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"categories": map[string]any{
							"type": "object",
							"additionalProperties": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"products": map[string]any{
										"type": "array",
										"items": map[string]any{
											"type": "object",
											"properties": map[string]any{
												"name": map[string]any{
													"type": "string",
												},
												"price": map[string]any{
													"type": "number",
												},
											},
										},
									},
								},
							},
						},
					},
				}),
			},
			expected: expected{
				output: map[string]any{
					"categories": map[string]any{
						"<key>": map[string]any{
							"products": []any{
								map[string]any{
									"name":  "...",
									"price": 0,
								},
							},
						},
					},
				},
			},
		},
		{
			name: "nil schema",
			input: input{
				schema: nil,
			},
			expected: expected{
				output: nil,
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
				output: nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.input.schema.ExampleObject()
			assert.Equal(
				t, tt.expected.output, result,
			)
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
