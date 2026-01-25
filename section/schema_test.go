package section

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGenerateJSONSchema_Primitives(t *testing.T) {
	tests := []struct {
		name     string
		input    reflect.Type
		expected map[string]any
	}{
		{
			name:     "string",
			input:    reflect.TypeOf(""),
			expected: map[string]any{"type": "string"},
		},
		{
			name:     "int",
			input:    reflect.TypeOf(0),
			expected: map[string]any{"type": "integer"},
		},
		{
			name:     "int64",
			input:    reflect.TypeOf(int64(0)),
			expected: map[string]any{"type": "integer"},
		},
		{
			name:     "uint",
			input:    reflect.TypeOf(uint(0)),
			expected: map[string]any{"type": "integer"},
		},
		{
			name:     "float32",
			input:    reflect.TypeOf(float32(0)),
			expected: map[string]any{"type": "number"},
		},
		{
			name:     "float64",
			input:    reflect.TypeOf(float64(0)),
			expected: map[string]any{"type": "number"},
		},
		{
			name:     "bool",
			input:    reflect.TypeOf(false),
			expected: map[string]any{"type": "boolean"},
		},
		{
			name:  "nil type",
			input: nil,
			expected: map[string]any{
				"type": "null",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateJSONSchema(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGenerateJSONSchema_Collections(t *testing.T) {
	tests := []struct {
		name     string
		input    reflect.Type
		expected map[string]any
	}{
		{
			name:  "slice of strings",
			input: reflect.TypeOf([]string{}),
			expected: map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
		},
		{
			name:  "slice of ints",
			input: reflect.TypeOf([]int{}),
			expected: map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "integer"},
			},
		},
		{
			name:  "array of strings",
			input: reflect.TypeOf([3]string{}),
			expected: map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
		},
		{
			name:  "map string to string",
			input: reflect.TypeOf(map[string]string{}),
			expected: map[string]any{
				"type":                 "object",
				"additionalProperties": map[string]any{"type": "string"},
			},
		},
		{
			name:  "map string to int",
			input: reflect.TypeOf(map[string]int{}),
			expected: map[string]any{
				"type":                 "object",
				"additionalProperties": map[string]any{"type": "integer"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateJSONSchema(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGenerateJSONSchema_SpecialTypes(t *testing.T) {
	tests := []struct {
		name     string
		input    reflect.Type
		expected map[string]any
	}{
		{
			name:  "time.Time",
			input: reflect.TypeOf(time.Time{}),
			expected: map[string]any{
				"type":   "string",
				"format": "date-time",
			},
		},
		{
			name:  "time.Duration",
			input: reflect.TypeOf(time.Duration(0)),
			expected: map[string]any{
				"type":        "string",
				"description": "Duration string (e.g., '1h30m', '2s')",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateJSONSchema(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGenerateJSONSchema_Pointers(t *testing.T) {
	t.Run("pointer to string", func(t *testing.T) {
		var s *string
		result := GenerateJSONSchema(reflect.TypeOf(s))
		assert.Equal(t, []string{"string", "null"}, result["type"])
	})

	t.Run("pointer to int", func(t *testing.T) {
		var i *int
		result := GenerateJSONSchema(reflect.TypeOf(i))
		assert.Equal(t, []string{"integer", "null"}, result["type"])
	})
}

type TestStructSimple struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

type TestStructWithDescription struct {
	Field1 string `json:"field1" description:"First field description"`
	Field2 int    `json:"field2" description:"Second field description"`
}

type TestStructWithOmitempty struct {
	Required string  `json:"required"`
	Optional string  `json:"optional,omitempty"`
	Pointer  *string `json:"pointer"`
}

type TestStructNested struct {
	ID     int              `json:"id"`
	Nested TestStructSimple `json:"nested"`
}

type TestStructWithPrivate struct {
	Public  string `json:"public"`
	private string //nolint:unused
}

type TestStructWithIgnored struct {
	Included string `json:"included"`
	Ignored  string `json:"-"`
}

func TestGenerateJSONSchema_Structs(t *testing.T) {
	t.Run("simple struct", func(t *testing.T) {
		result := GenerateJSONSchema(reflect.TypeOf(TestStructSimple{}))

		assert.Equal(t, "object", result["type"])

		props := result["properties"].(map[string]any)
		assert.Equal(t, map[string]any{"type": "string"}, props["name"])
		assert.Equal(t, map[string]any{"type": "integer"}, props["value"])

		required := result["required"].([]string)
		assert.Contains(t, required, "name")
		assert.Contains(t, required, "value")
	})

	t.Run("struct with description", func(t *testing.T) {
		result := GenerateJSONSchema(reflect.TypeOf(TestStructWithDescription{}))

		props := result["properties"].(map[string]any)

		field1 := props["field1"].(map[string]any)
		assert.Equal(t, "string", field1["type"])
		assert.Equal(t, "First field description", field1["description"])

		field2 := props["field2"].(map[string]any)
		assert.Equal(t, "integer", field2["type"])
		assert.Equal(t, "Second field description", field2["description"])
	})

	t.Run("struct with omitempty", func(t *testing.T) {
		result := GenerateJSONSchema(reflect.TypeOf(TestStructWithOmitempty{}))

		required := result["required"].([]string)
		assert.Contains(t, required, "required")
		assert.NotContains(t, required, "optional")
		assert.NotContains(t, required, "pointer")
	})

	t.Run("nested struct", func(t *testing.T) {
		result := GenerateJSONSchema(reflect.TypeOf(TestStructNested{}))

		props := result["properties"].(map[string]any)
		assert.Equal(t, map[string]any{"type": "integer"}, props["id"])

		nestedSchema := props["nested"].(map[string]any)
		assert.Equal(t, "object", nestedSchema["type"])

		nestedProps := nestedSchema["properties"].(map[string]any)
		assert.Equal(t, map[string]any{"type": "string"}, nestedProps["name"])
		assert.Equal(t, map[string]any{"type": "integer"}, nestedProps["value"])
	})

	t.Run("struct with private field", func(t *testing.T) {
		result := GenerateJSONSchema(reflect.TypeOf(TestStructWithPrivate{}))

		props := result["properties"].(map[string]any)
		assert.Contains(t, props, "public")
		assert.NotContains(t, props, "private")
	})

	t.Run("struct with ignored field", func(t *testing.T) {
		result := GenerateJSONSchema(reflect.TypeOf(TestStructWithIgnored{}))

		props := result["properties"].(map[string]any)
		assert.Contains(t, props, "included")
		assert.NotContains(t, props, "Ignored")
	})
}

func TestGenerateJSONSchema_NestedCollections(t *testing.T) {
	t.Run("slice of structs", func(t *testing.T) {
		result := GenerateJSONSchema(reflect.TypeOf([]TestStructSimple{}))

		assert.Equal(t, "array", result["type"])

		items := result["items"].(map[string]any)
		assert.Equal(t, "object", items["type"])

		props := items["properties"].(map[string]any)
		assert.Equal(t, map[string]any{"type": "string"}, props["name"])
	})

	t.Run("map of structs", func(t *testing.T) {
		result := GenerateJSONSchema(reflect.TypeOf(map[string]TestStructSimple{}))

		assert.Equal(t, "object", result["type"])

		addProps := result["additionalProperties"].(map[string]any)
		assert.Equal(t, "object", addProps["type"])

		props := addProps["properties"].(map[string]any)
		assert.Equal(t, map[string]any{"type": "string"}, props["name"])
	})

	t.Run("slice of slices", func(t *testing.T) {
		result := GenerateJSONSchema(reflect.TypeOf([][]string{}))

		assert.Equal(t, "array", result["type"])

		items := result["items"].(map[string]any)
		assert.Equal(t, "array", items["type"])

		innerItems := items["items"].(map[string]any)
		assert.Equal(t, "string", innerItems["type"])
	})
}

type TestStructWithUntaggedFields struct {
	TaggedField   string `json:"tagged_field"`
	UntaggedField string
}

func TestGenerateJSONSchema_UntaggedFields(t *testing.T) {
	result := GenerateJSONSchema(reflect.TypeOf(TestStructWithUntaggedFields{}))

	props := result["properties"].(map[string]any)

	// Tagged field uses the JSON tag name
	assert.Contains(t, props, "tagged_field")

	// Untagged field uses the Go field name
	assert.Contains(t, props, "UntaggedField")
}
