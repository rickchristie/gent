package section

import (
	"context"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test types for JSON parsing
type SimpleStruct struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

type NestedStruct struct {
	ID    int          `json:"id"`
	Data  SimpleStruct `json:"data"`
	Tags  []string     `json:"tags"`
	Extra *string      `json:"extra,omitempty"`
}

type StructWithDescription struct {
	Field1 string `json:"field1" description:"The first field"`
	Field2 int    `json:"field2" description:"The second field"`
}

type StructWithOmitempty struct {
	Required string  `json:"required"`
	Optional string  `json:"optional,omitempty"`
	Pointer  *string `json:"pointer"`
}

type StructWithMap struct {
	Metadata map[string]string `json:"metadata"`
}

type StructWithTime struct {
	CreatedAt time.Time     `json:"created_at"`
	Duration  time.Duration `json:"duration"`
}

func TestJSON_Name(t *testing.T) {
	tests := []struct {
		name     string
		input    func() *JSON[SimpleStruct]
		expected string
	}{
		{
			name: "returns provided name",
			input: func() *JSON[SimpleStruct] {
				return NewJSON[SimpleStruct]("analysis")
			},
			expected: "analysis",
		},
		{
			name: "empty name",
			input: func() *JSON[SimpleStruct] {
				return NewJSON[SimpleStruct]("")
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			section := tt.input()
			assert.Equal(t, tt.expected, section.Name())
		})
	}
}

func TestJSON_Prompt(t *testing.T) {
	tests := []struct {
		name     string
		input    func() gent.TextOutputSection
		expected struct {
			containsSchema  bool
			containsPrompt  bool
			containsExample bool
			promptPrefix    string
		}
	}{
		{
			name: "default prompt with schema",
			input: func() gent.TextOutputSection {
				return NewJSON[SimpleStruct]("data")
			},
			expected: struct {
				containsSchema  bool
				containsPrompt  bool
				containsExample bool
				promptPrefix    string
			}{
				containsSchema:  true,
				containsPrompt:  false,
				containsExample: false,
				promptPrefix:    "data content must be valid JSON matching this schema:",
			},
		},
		{
			name: "with custom prompt",
			input: func() gent.TextOutputSection {
				return NewJSON[SimpleStruct]("data").WithGuidance("Analyze the data.")
			},
			expected: struct {
				containsSchema  bool
				containsPrompt  bool
				containsExample bool
				promptPrefix    string
			}{
				containsSchema:  true,
				containsPrompt:  true,
				containsExample: false,
				promptPrefix:    "Analyze the data.",
			},
		},
		{
			name: "with example",
			input: func() gent.TextOutputSection {
				return NewJSON[SimpleStruct]("data").WithExample(SimpleStruct{
					Name:  "test",
					Value: 42,
				})
			},
			expected: struct {
				containsSchema  bool
				containsPrompt  bool
				containsExample bool
				promptPrefix    string
			}{
				containsSchema:  true,
				containsPrompt:  false,
				containsExample: true,
				promptPrefix:    "data content must be valid JSON matching this schema:",
			},
		},
		{
			name: "with description fields",
			input: func() gent.TextOutputSection {
				return NewJSON[StructWithDescription]("data")
			},
			expected: struct {
				containsSchema  bool
				containsPrompt  bool
				containsExample bool
				promptPrefix    string
			}{
				containsSchema:  true,
				containsPrompt:  false,
				containsExample: false,
				promptPrefix:    "data content must be valid JSON matching this schema:",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			section := tt.input()
			prompt := section.Guidance()

			assert.Contains(t, prompt, tt.expected.promptPrefix)

			if tt.expected.containsSchema {
				assert.Contains(t, prompt, `"type"`)
				assert.Contains(t, prompt, `"properties"`)
			}

			if tt.expected.containsExample {
				assert.Contains(t, prompt, "Example:")
			}
		})
	}
}

func TestJSON_ParseSection(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected struct {
			result SimpleStruct
			hasErr bool
		}
	}{
		{
			name:  "valid JSON",
			input: `{"name": "test", "value": 42}`,
			expected: struct {
				result SimpleStruct
				hasErr bool
			}{
				result: SimpleStruct{Name: "test", Value: 42},
				hasErr: false,
			},
		},
		{
			name:  "valid JSON with whitespace",
			input: `  {"name": "test", "value": 42}  `,
			expected: struct {
				result SimpleStruct
				hasErr bool
			}{
				result: SimpleStruct{Name: "test", Value: 42},
				hasErr: false,
			},
		},
		{
			name:  "valid JSON multiline",
			input: "{\n  \"name\": \"test\",\n  \"value\": 42\n}",
			expected: struct {
				result SimpleStruct
				hasErr bool
			}{
				result: SimpleStruct{Name: "test", Value: 42},
				hasErr: false,
			},
		},
		{
			name:  "empty content returns zero value",
			input: "",
			expected: struct {
				result SimpleStruct
				hasErr bool
			}{
				result: SimpleStruct{},
				hasErr: false,
			},
		},
		{
			name:  "whitespace only returns zero value",
			input: "   \n\t  ",
			expected: struct {
				result SimpleStruct
				hasErr bool
			}{
				result: SimpleStruct{},
				hasErr: false,
			},
		},
		{
			name:  "invalid JSON",
			input: `{"name": "test", "value": }`,
			expected: struct {
				result SimpleStruct
				hasErr bool
			}{
				result: SimpleStruct{},
				hasErr: true,
			},
		},
		{
			name:  "not JSON at all",
			input: "Hello, world!",
			expected: struct {
				result SimpleStruct
				hasErr bool
			}{
				result: SimpleStruct{},
				hasErr: true,
			},
		},
		{
			name:  "partial JSON fields",
			input: `{"name": "test"}`,
			expected: struct {
				result SimpleStruct
				hasErr bool
			}{
				result: SimpleStruct{Name: "test", Value: 0},
				hasErr: false,
			},
		},
		{
			name:  "extra fields ignored",
			input: `{"name": "test", "value": 42, "extra": "ignored"}`,
			expected: struct {
				result SimpleStruct
				hasErr bool
			}{
				result: SimpleStruct{Name: "test", Value: 42},
				hasErr: false,
			},
		},
		{
			name:  "wrong type for field",
			input: `{"name": 123, "value": 42}`,
			expected: struct {
				result SimpleStruct
				hasErr bool
			}{
				result: SimpleStruct{},
				hasErr: true,
			},
		},
	}

	section := NewJSON[SimpleStruct]("data")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := section.ParseSection(nil, tt.input)

			if tt.expected.hasErr {
				assert.Error(t, err)
				assert.ErrorIs(t, err, gent.ErrInvalidJSON)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected.result, result)
			}
		})
	}
}

func TestJSON_ParseSection_NestedStruct(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected struct {
			result NestedStruct
			hasErr bool
		}
	}{
		{
			name:  "full nested struct",
			input: `{"id": 1, "data": {"name": "nested", "value": 100}, "tags": ["a", "b"]}`,
			expected: struct {
				result NestedStruct
				hasErr bool
			}{
				result: NestedStruct{
					ID:   1,
					Data: SimpleStruct{Name: "nested", Value: 100},
					Tags: []string{"a", "b"},
				},
				hasErr: false,
			},
		},
		{
			name:  "with optional pointer field",
			input: `{"id": 1, "data": {"name": "nested", "value": 100}, "tags": [], "extra": "value"}`,
			expected: struct {
				result NestedStruct
				hasErr bool
			}{
				result: NestedStruct{
					ID:    1,
					Data:  SimpleStruct{Name: "nested", Value: 100},
					Tags:  []string{},
					Extra: ptrString("value"),
				},
				hasErr: false,
			},
		},
		{
			name:  "null optional pointer field",
			input: `{"id": 1, "data": {"name": "nested", "value": 100}, "tags": [], "extra": null}`,
			expected: struct {
				result NestedStruct
				hasErr bool
			}{
				result: NestedStruct{
					ID:    1,
					Data:  SimpleStruct{Name: "nested", Value: 100},
					Tags:  []string{},
					Extra: nil,
				},
				hasErr: false,
			},
		},
		{
			name:  "empty tags array",
			input: `{"id": 1, "data": {"name": "test", "value": 0}, "tags": []}`,
			expected: struct {
				result NestedStruct
				hasErr bool
			}{
				result: NestedStruct{
					ID:   1,
					Data: SimpleStruct{Name: "test", Value: 0},
					Tags: []string{},
				},
				hasErr: false,
			},
		},
	}

	section := NewJSON[NestedStruct]("data")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := section.ParseSection(nil, tt.input)

			if tt.expected.hasErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected.result, result)
			}
		})
	}
}

func TestJSON_ParseSection_PrimitiveTypes(t *testing.T) {
	t.Run("string type", func(t *testing.T) {
		section := NewJSON[string]("data")
		result, err := section.ParseSection(nil, `"hello"`)
		require.NoError(t, err)
		assert.Equal(t, "hello", result)
	})

	t.Run("int type", func(t *testing.T) {
		section := NewJSON[int]("data")
		result, err := section.ParseSection(nil, `42`)
		require.NoError(t, err)
		assert.Equal(t, 42, result)
	})

	t.Run("float type", func(t *testing.T) {
		section := NewJSON[float64]("data")
		result, err := section.ParseSection(nil, `3.14`)
		require.NoError(t, err)
		assert.Equal(t, 3.14, result)
	})

	t.Run("bool type", func(t *testing.T) {
		section := NewJSON[bool]("data")
		result, err := section.ParseSection(nil, `true`)
		require.NoError(t, err)
		assert.Equal(t, true, result)
	})

	t.Run("slice type", func(t *testing.T) {
		section := NewJSON[[]string]("data")
		result, err := section.ParseSection(nil, `["a", "b", "c"]`)
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b", "c"}, result)
	})

	t.Run("map type", func(t *testing.T) {
		section := NewJSON[map[string]int]("data")
		result, err := section.ParseSection(nil, `{"a": 1, "b": 2}`)
		require.NoError(t, err)
		assert.Equal(t, map[string]int{"a": 1, "b": 2}, result)
	})
}

func TestJSON_ParseSection_TracesErrors(t *testing.T) {
	tests := []struct {
		name  string
		input struct {
			content     string
			presetGauge bool
		}
		expected struct {
			hasErr                bool
			totalErrors           int64
			consecutiveErrors     float64
		}
	}{
		{
			name: "traces error on invalid JSON",
			input: struct {
				content     string
				presetGauge bool
			}{
				content:     `{"invalid": }`,
				presetGauge: false,
			},
			expected: struct {
				hasErr                bool
				totalErrors           int64
				consecutiveErrors     float64
				}{
				hasErr:                true,
				totalErrors:           1,
				consecutiveErrors:     1,
			},
		},
		{
			name: "resets consecutive gauge on success",
			input: struct {
				content     string
				presetGauge bool
			}{
				content:     `{"name": "test", "value": 42}`,
				presetGauge: true,
			},
			expected: struct {
				hasErr                bool
				totalErrors           int64
				consecutiveErrors     float64
				}{
				hasErr:                false,
				totalErrors:           0,
				consecutiveErrors:     0,
			},
		},
	}

	section := NewJSON[SimpleStruct]("data")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			execCtx := gent.NewExecutionContext(
				context.Background(), "test", nil,
			)
			execCtx.IncrementIteration()

			if tt.input.presetGauge {
				execCtx.Stats().IncrGauge(
					gent.SGSectionParseErrorConsecutive, 1,
				)
			}

			_, err := section.ParseSection(execCtx, tt.input.content)

			if tt.expected.hasErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			stats := execCtx.Stats()
			assert.Equal(t, tt.expected.totalErrors,
				stats.GetCounter(gent.SCSectionParseErrorTotal))
			assert.Equal(t, tt.expected.consecutiveErrors,
				stats.GetGauge(gent.SGSectionParseErrorConsecutive))
		})
	}
}

func TestJSON_ParseSection_WithMap(t *testing.T) {
	section := NewJSON[StructWithMap]("data")

	input := `{"metadata": {"key1": "value1", "key2": "value2"}}`
	result, err := section.ParseSection(nil, input)

	require.NoError(t, err)
	parsed := result.(StructWithMap)
	assert.Equal(t, "value1", parsed.Metadata["key1"])
	assert.Equal(t, "value2", parsed.Metadata["key2"])
}

func TestJSON_MethodChaining(t *testing.T) {
	section := NewJSON[SimpleStruct]("analysis").
		WithGuidance("Analyze the data.").
		WithExample(SimpleStruct{Name: "example", Value: 100})

	assert.Equal(t, "analysis", section.Name())
	assert.Contains(t, section.Guidance(), "Analyze the data.")
	assert.Contains(t, section.Guidance(), "Example:")
	assert.Contains(t, section.Guidance(), "example")
}

func TestJSON_ImplementsTextOutputSection(t *testing.T) {
	var _ gent.TextOutputSection = (*JSON[any])(nil)
}

// Helper function
func ptrString(s string) *string {
	return &s
}
