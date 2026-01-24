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
					assert.True(t, ok, "expected *ValidationError, got %T", err)
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
