package section

import (
	"context"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test types for YAML parsing (reusing some from json_test.go)
type YAMLSimpleStruct struct {
	Name  string `yaml:"name"`
	Value int    `yaml:"value"`
}

type YAMLNestedStruct struct {
	ID    int              `yaml:"id"`
	Data  YAMLSimpleStruct `yaml:"data"`
	Tags  []string         `yaml:"tags"`
	Extra *string          `yaml:"extra,omitempty"`
}

type YAMLStructWithDescription struct {
	Field1 string `yaml:"field1" description:"The first field"`
	Field2 int    `yaml:"field2" description:"The second field"`
}

type YAMLStructWithMap struct {
	Metadata map[string]string `yaml:"metadata"`
}

func TestYAML_Name(t *testing.T) {
	tests := []struct {
		name     string
		input    func() *YAML[YAMLSimpleStruct]
		expected string
	}{
		{
			name: "returns provided name",
			input: func() *YAML[YAMLSimpleStruct] {
				return NewYAML[YAMLSimpleStruct]("plan")
			},
			expected: "plan",
		},
		{
			name: "empty name",
			input: func() *YAML[YAMLSimpleStruct] {
				return NewYAML[YAMLSimpleStruct]("")
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

func TestYAML_Prompt(t *testing.T) {
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
				return NewYAML[YAMLSimpleStruct]("data")
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
				promptPrefix:    "data content must be valid YAML matching this schema:",
			},
		},
		{
			name: "with custom prompt",
			input: func() gent.TextOutputSection {
				return NewYAML[YAMLSimpleStruct]("data").WithGuidance("Create a plan.")
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
				promptPrefix:    "Create a plan.",
			},
		},
		{
			name: "with example",
			input: func() gent.TextOutputSection {
				return NewYAML[YAMLSimpleStruct]("data").WithExample(YAMLSimpleStruct{
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
				promptPrefix:    "data content must be valid YAML matching this schema:",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			section := tt.input()
			prompt := section.Guidance()

			assert.Contains(t, prompt, tt.expected.promptPrefix)

			if tt.expected.containsSchema {
				assert.Contains(t, prompt, "type:")
				assert.Contains(t, prompt, "properties:")
			}

			if tt.expected.containsExample {
				assert.Contains(t, prompt, "Example:")
			}
		})
	}
}

func TestYAML_ParseSection(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected struct {
			result YAMLSimpleStruct
			hasErr bool
		}
	}{
		{
			name:  "valid YAML",
			input: "name: test\nvalue: 42",
			expected: struct {
				result YAMLSimpleStruct
				hasErr bool
			}{
				result: YAMLSimpleStruct{Name: "test", Value: 42},
				hasErr: false,
			},
		},
		{
			name:  "valid YAML with trailing whitespace",
			input: "name: test\nvalue: 42  ",
			expected: struct {
				result YAMLSimpleStruct
				hasErr bool
			}{
				result: YAMLSimpleStruct{Name: "test", Value: 42},
				hasErr: false,
			},
		},
		{
			name:  "valid YAML with leading newlines",
			input: "\n\nname: test\nvalue: 42\n\n",
			expected: struct {
				result YAMLSimpleStruct
				hasErr bool
			}{
				result: YAMLSimpleStruct{Name: "test", Value: 42},
				hasErr: false,
			},
		},
		{
			name:  "empty content returns zero value",
			input: "",
			expected: struct {
				result YAMLSimpleStruct
				hasErr bool
			}{
				result: YAMLSimpleStruct{},
				hasErr: false,
			},
		},
		{
			name:  "whitespace only returns zero value",
			input: "   \n\t  ",
			expected: struct {
				result YAMLSimpleStruct
				hasErr bool
			}{
				result: YAMLSimpleStruct{},
				hasErr: false,
			},
		},
		{
			name:  "invalid YAML - bad indentation",
			input: "name: test\n value: 42",
			expected: struct {
				result YAMLSimpleStruct
				hasErr bool
			}{
				result: YAMLSimpleStruct{},
				hasErr: true,
			},
		},
		{
			name:  "partial YAML fields",
			input: "name: test",
			expected: struct {
				result YAMLSimpleStruct
				hasErr bool
			}{
				result: YAMLSimpleStruct{Name: "test", Value: 0},
				hasErr: false,
			},
		},
		{
			name:  "extra fields ignored",
			input: "name: test\nvalue: 42\nextra: ignored",
			expected: struct {
				result YAMLSimpleStruct
				hasErr bool
			}{
				result: YAMLSimpleStruct{Name: "test", Value: 42},
				hasErr: false,
			},
		},
		{
			name:  "quoted string value",
			input: "name: \"test with: colon\"\nvalue: 42",
			expected: struct {
				result YAMLSimpleStruct
				hasErr bool
			}{
				result: YAMLSimpleStruct{Name: "test with: colon", Value: 42},
				hasErr: false,
			},
		},
		{
			name:  "unquoted string with special chars",
			input: "name: test value\nvalue: 42",
			expected: struct {
				result YAMLSimpleStruct
				hasErr bool
			}{
				result: YAMLSimpleStruct{Name: "test value", Value: 42},
				hasErr: false,
			},
		},
	}

	section := NewYAML[YAMLSimpleStruct]("data")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := section.ParseSection(nil, tt.input)

			if tt.expected.hasErr {
				assert.Error(t, err)
				assert.ErrorIs(t, err, gent.ErrInvalidYAML)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected.result, result)
			}
		})
	}
}

func TestYAML_ParseSection_NestedStruct(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected struct {
			result YAMLNestedStruct
			hasErr bool
		}
	}{
		{
			name: "full nested struct",
			input: `id: 1
data:
  name: nested
  value: 100
tags:
  - a
  - b`,
			expected: struct {
				result YAMLNestedStruct
				hasErr bool
			}{
				result: YAMLNestedStruct{
					ID:   1,
					Data: YAMLSimpleStruct{Name: "nested", Value: 100},
					Tags: []string{"a", "b"},
				},
				hasErr: false,
			},
		},
		{
			name: "with optional pointer field",
			input: `id: 1
data:
  name: nested
  value: 100
tags: []
extra: value`,
			expected: struct {
				result YAMLNestedStruct
				hasErr bool
			}{
				result: YAMLNestedStruct{
					ID:    1,
					Data:  YAMLSimpleStruct{Name: "nested", Value: 100},
					Tags:  []string{},
					Extra: ptrString("value"),
				},
				hasErr: false,
			},
		},
		{
			name: "null optional pointer field",
			input: `id: 1
data:
  name: nested
  value: 100
tags: []
extra: null`,
			expected: struct {
				result YAMLNestedStruct
				hasErr bool
			}{
				result: YAMLNestedStruct{
					ID:    1,
					Data:  YAMLSimpleStruct{Name: "nested", Value: 100},
					Tags:  []string{},
					Extra: nil,
				},
				hasErr: false,
			},
		},
		{
			name: "inline array",
			input: `id: 1
data:
  name: test
  value: 0
tags: [a, b, c]`,
			expected: struct {
				result YAMLNestedStruct
				hasErr bool
			}{
				result: YAMLNestedStruct{
					ID:   1,
					Data: YAMLSimpleStruct{Name: "test", Value: 0},
					Tags: []string{"a", "b", "c"},
				},
				hasErr: false,
			},
		},
	}

	section := NewYAML[YAMLNestedStruct]("data")

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

func TestYAML_ParseSection_PrimitiveTypes(t *testing.T) {
	t.Run("string type", func(t *testing.T) {
		section := NewYAML[string]("data")
		result, err := section.ParseSection(nil, `hello`)
		require.NoError(t, err)
		assert.Equal(t, "hello", result)
	})

	t.Run("int type", func(t *testing.T) {
		section := NewYAML[int]("data")
		result, err := section.ParseSection(nil, `42`)
		require.NoError(t, err)
		assert.Equal(t, 42, result)
	})

	t.Run("float type", func(t *testing.T) {
		section := NewYAML[float64]("data")
		result, err := section.ParseSection(nil, `3.14`)
		require.NoError(t, err)
		assert.Equal(t, 3.14, result)
	})

	t.Run("bool type", func(t *testing.T) {
		section := NewYAML[bool]("data")
		result, err := section.ParseSection(nil, `true`)
		require.NoError(t, err)
		assert.Equal(t, true, result)
	})

	t.Run("slice type block style", func(t *testing.T) {
		section := NewYAML[[]string]("data")
		result, err := section.ParseSection(nil, "- a\n- b\n- c")
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b", "c"}, result)
	})

	t.Run("slice type inline style", func(t *testing.T) {
		section := NewYAML[[]string]("data")
		result, err := section.ParseSection(nil, `[a, b, c]`)
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b", "c"}, result)
	})

	t.Run("map type", func(t *testing.T) {
		section := NewYAML[map[string]int]("data")
		result, err := section.ParseSection(nil, "a: 1\nb: 2")
		require.NoError(t, err)
		assert.Equal(t, map[string]int{"a": 1, "b": 2}, result)
	})
}

func TestYAML_ParseSection_TracesErrors(t *testing.T) {
	tests := []struct {
		name  string
		input struct {
			content       string
			presetCounter bool
		}
		expected struct {
			hasErr                bool
			totalErrors           int64
			consecutiveErrors     int64
			iterationErrorCounter int64
		}
	}{
		{
			name: "traces error on invalid YAML",
			input: struct {
				content       string
				presetCounter bool
			}{
				content:       ":\n  - invalid:\n    :",
				presetCounter: false,
			},
			expected: struct {
				hasErr                bool
				totalErrors           int64
				consecutiveErrors     int64
				iterationErrorCounter int64
			}{
				hasErr:                true,
				totalErrors:           1,
				consecutiveErrors:     1,
				iterationErrorCounter: 1,
			},
		},
		{
			name: "resets consecutive counter on success",
			input: struct {
				content       string
				presetCounter bool
			}{
				content:       "name: test\nvalue: 42",
				presetCounter: true,
			},
			expected: struct {
				hasErr                bool
				totalErrors           int64
				consecutiveErrors     int64
				iterationErrorCounter int64
			}{
				hasErr:                false,
				totalErrors:           0,
				consecutiveErrors:     0,
				iterationErrorCounter: 0,
			},
		},
	}

	section := NewYAML[YAMLSimpleStruct]("data")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			execCtx := gent.NewExecutionContext(context.Background(), "test", nil)
			execCtx.StartIteration()

			if tt.input.presetCounter {
				execCtx.Stats().IncrCounter(gent.KeySectionParseErrorConsecutive, 1)
			}

			_, err := section.ParseSection(execCtx, tt.input.content)

			if tt.expected.hasErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			stats := execCtx.Stats()
			assert.Equal(t, tt.expected.totalErrors,
				stats.GetCounter(gent.KeySectionParseErrorTotal))
			assert.Equal(t, tt.expected.consecutiveErrors,
				stats.GetCounter(gent.KeySectionParseErrorConsecutive))
			assert.Equal(t, tt.expected.iterationErrorCounter,
				stats.GetCounter(gent.KeySectionParseErrorAt+"1"))
		})
	}
}

func TestYAML_ParseSection_WithMap(t *testing.T) {
	section := NewYAML[YAMLStructWithMap]("data")

	input := `metadata:
  key1: value1
  key2: value2`
	result, err := section.ParseSection(nil, input)

	require.NoError(t, err)
	parsed := result.(YAMLStructWithMap)
	assert.Equal(t, "value1", parsed.Metadata["key1"])
	assert.Equal(t, "value2", parsed.Metadata["key2"])
}

func TestYAML_ParseSection_MultilineStrings(t *testing.T) {
	type MultilineStruct struct {
		Text string `yaml:"text"`
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "literal block scalar with clip (default)",
			input: `text: |
  Line 1
  Line 2
  Line 3`,
			expected: "Line 1\nLine 2\nLine 3",
		},
		{
			name: "literal block scalar strip",
			input: `text: |-
  Line 1
  Line 2
  Line 3`,
			expected: "Line 1\nLine 2\nLine 3",
		},
		{
			name: "folded block scalar",
			input: `text: >
  This is a
  long line
  that wraps`,
			expected: "This is a long line that wraps",
		},
		{
			name: "quoted multiline",
			input: `text: "Line 1\nLine 2\nLine 3"`,
			expected: "Line 1\nLine 2\nLine 3",
		},
	}

	section := NewYAML[MultilineStruct]("data")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := section.ParseSection(nil, tt.input)
			require.NoError(t, err)
			parsed := result.(MultilineStruct)
			assert.Equal(t, tt.expected, parsed.Text)
		})
	}
}

func TestYAML_MethodChaining(t *testing.T) {
	section := NewYAML[YAMLSimpleStruct]("plan").
		WithGuidance("Create a plan.").
		WithExample(YAMLSimpleStruct{Name: "example", Value: 100})

	assert.Equal(t, "plan", section.Name())
	assert.Contains(t, section.Guidance(), "Create a plan.")
	assert.Contains(t, section.Guidance(), "Example:")
	assert.Contains(t, section.Guidance(), "example")
}

func TestYAML_ImplementsTextOutputSection(t *testing.T) {
	var _ gent.TextOutputSection = (*YAML[any])(nil)
}
