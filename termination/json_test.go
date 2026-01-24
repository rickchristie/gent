package termination

import (
	"strings"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
	"github.com/tmc/langchaingo/llms"
)

func TestJSON_Name(t *testing.T) {
	type input struct {
		sectionName string
	}

	type expected struct {
		name string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name:     "default name",
			input:    input{sectionName: ""},
			expected: expected{name: "answer"},
		},
		{
			name:     "custom name",
			input:    input{sectionName: "result"},
			expected: expected{name: "result"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			term := NewJSON[string]()
			if tt.input.sectionName != "" {
				term.WithSectionName(tt.input.sectionName)
			}

			assert.Equal(t, tt.expected.name, term.Name())
		})
	}
}

func TestJSON_Prompt(t *testing.T) {
	type input struct {
		promptText string
	}

	type expected struct {
		containsPrompt bool
		containsType   bool
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name:  "custom prompt with type info",
			input: input{promptText: "Provide the result"},
			expected: expected{
				containsPrompt: true,
				containsType:   true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			term := NewJSON[string]()
			term.WithPrompt(tt.input.promptText)

			prompt := term.Prompt()

			if tt.expected.containsPrompt {
				assert.True(t, strings.Contains(prompt, tt.input.promptText),
					"prompt should contain %q", tt.input.promptText)
			}
			if tt.expected.containsType {
				assert.True(t, strings.Contains(prompt, "string"),
					"prompt should contain type info 'string'")
			}
		})
	}
}

func TestJSON_ParseSection_Primitives(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		type input struct {
			content string
		}

		type expected struct {
			result string
			err    error
		}

		tests := []struct {
			name     string
			input    input
			expected expected
		}{
			{
				name:     "valid string",
				input:    input{content: `"hello world"`},
				expected: expected{result: "hello world", err: nil},
			},
			{
				name:     "empty content returns empty string",
				input:    input{content: ""},
				expected: expected{result: "", err: nil},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				term := NewJSON[string]()

				result, err := term.ParseSection(tt.input.content)

				assert.ErrorIs(t, err, tt.expected.err)
				str, ok := result.(string)
				assert.True(t, ok, "expected string, got %T", result)
				assert.Equal(t, tt.expected.result, str)
			})
		}
	})

	t.Run("int", func(t *testing.T) {
		term := NewJSON[int]()

		result, err := term.ParseSection(`42`)

		assert.NoError(t, err)
		num, ok := result.(int)
		assert.True(t, ok, "expected int, got %T", result)
		assert.Equal(t, 42, num)
	})

	t.Run("bool", func(t *testing.T) {
		term := NewJSON[bool]()

		result, err := term.ParseSection(`true`)

		assert.NoError(t, err)
		b, ok := result.(bool)
		assert.True(t, ok, "expected bool, got %T", result)
		assert.True(t, b)
	})
}

type SimpleStruct struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

type NestedStruct struct {
	Inner SimpleStruct `json:"inner"`
	Count int          `json:"count"`
}

type PointerStruct struct {
	Name  string  `json:"name"`
	Value *int    `json:"value,omitempty"`
	Extra *string `json:"extra,omitempty"`
}

type TimeStruct struct {
	Created time.Time     `json:"created"`
	TTL     time.Duration `json:"ttl"`
}

type SliceStruct struct {
	Items []SimpleStruct `json:"items"`
}

func TestJSON_ParseSection_Struct(t *testing.T) {
	type input struct {
		content string
	}

	type expected struct {
		result SimpleStruct
		err    error
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name:  "simple struct",
			input: input{content: `{"name": "test", "value": 123}`},
			expected: expected{
				result: SimpleStruct{Name: "test", Value: 123},
				err:    nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			term := NewJSON[SimpleStruct]()

			result, err := term.ParseSection(tt.input.content)

			assert.ErrorIs(t, err, tt.expected.err)
			s, ok := result.(SimpleStruct)
			assert.True(t, ok, "expected SimpleStruct, got %T", result)
			assert.Equal(t, tt.expected.result, s)
		})
	}
}

func TestJSON_ParseSection_NestedStruct(t *testing.T) {
	term := NewJSON[NestedStruct]()

	result, err := term.ParseSection(`{"inner": {"name": "nested", "value": 1}, "count": 5}`)

	assert.NoError(t, err)
	s, ok := result.(NestedStruct)
	assert.True(t, ok, "expected NestedStruct, got %T", result)
	assert.Equal(t, "nested", s.Inner.Name)
	assert.Equal(t, 1, s.Inner.Value)
	assert.Equal(t, 5, s.Count)
}

func TestJSON_ParseSection_PointerFields(t *testing.T) {
	type input struct {
		content string
	}

	type expected struct {
		name       string
		valueIsNil bool
		valueVal   int
		extraIsNil bool
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name:  "with value present",
			input: input{content: `{"name": "test", "value": 42}`},
			expected: expected{
				name:       "test",
				valueIsNil: false,
				valueVal:   42,
				extraIsNil: true,
			},
		},
		{
			name:  "with null value",
			input: input{content: `{"name": "test", "value": null}`},
			expected: expected{
				name:       "test",
				valueIsNil: true,
				valueVal:   0,
				extraIsNil: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			term := NewJSON[PointerStruct]()

			result, err := term.ParseSection(tt.input.content)

			assert.NoError(t, err)
			s := result.(PointerStruct)
			assert.Equal(t, tt.expected.name, s.Name)

			if tt.expected.valueIsNil {
				assert.Nil(t, s.Value)
			} else {
				assert.NotNil(t, s.Value)
				assert.Equal(t, tt.expected.valueVal, *s.Value)
			}

			if tt.expected.extraIsNil {
				assert.Nil(t, s.Extra)
			}
		})
	}
}

func TestJSON_ParseSection_TimeFields(t *testing.T) {
	term := NewJSON[TimeStruct]()

	result, err := term.ParseSection(`{"created": "2024-01-15T10:30:00Z", "ttl": 3600000000000}`)

	assert.NoError(t, err)
	s := result.(TimeStruct)
	assert.Equal(t, 2024, s.Created.Year())
	assert.Equal(t, time.Month(1), s.Created.Month())
	assert.Equal(t, 15, s.Created.Day())
	assert.Equal(t, time.Hour, s.TTL)
}

func TestJSON_ParseSection_Slice(t *testing.T) {
	term := NewJSON[[]string]()

	result, err := term.ParseSection(`["one", "two", "three"]`)

	assert.NoError(t, err)
	slice, ok := result.([]string)
	assert.True(t, ok, "expected []string, got %T", result)
	assert.Equal(t, []string{"one", "two", "three"}, slice)
}

func TestJSON_ParseSection_Map(t *testing.T) {
	term := NewJSON[map[string]int]()

	result, err := term.ParseSection(`{"a": 1, "b": 2}`)

	assert.NoError(t, err)
	m, ok := result.(map[string]int)
	assert.True(t, ok, "expected map[string]int, got %T", result)
	assert.Equal(t, map[string]int{"a": 1, "b": 2}, m)
}

func TestJSON_ParseSection_InvalidJSON(t *testing.T) {
	term := NewJSON[string]()

	_, err := term.ParseSection(`{invalid json}`)

	assert.ErrorIs(t, err, gent.ErrInvalidJSON)
}

func TestJSON_ParseSection_SliceOfStructs(t *testing.T) {
	term := NewJSON[SliceStruct]()

	result, err := term.ParseSection(`{
		"items": [
			{"name": "first", "value": 1},
			{"name": "second", "value": 2}
		]
	}`)

	assert.NoError(t, err)
	s := result.(SliceStruct)
	assert.Len(t, s.Items, 2)
	assert.Equal(t, "first", s.Items[0].Name)
	assert.Equal(t, 1, s.Items[0].Value)
	assert.Equal(t, "second", s.Items[1].Name)
	assert.Equal(t, 2, s.Items[1].Value)
}

func TestJSON_WithExample(t *testing.T) {
	term := NewJSON[SimpleStruct]().
		WithExample(SimpleStruct{Name: "example", Value: 42})

	prompt := term.Prompt()

	assert.True(t, strings.Contains(prompt, "example"),
		"prompt should contain example name")
	assert.True(t, strings.Contains(prompt, "42"),
		"prompt should contain example value")
}

type DescribedStruct struct {
	ID   int    `json:"id" description:"The unique identifier"`
	Name string `json:"name" description:"The display name"`
}

func TestJSON_SchemaWithDescriptions(t *testing.T) {
	term := NewJSON[DescribedStruct]()

	prompt := term.Prompt()

	assert.True(t, strings.Contains(prompt, "unique identifier"),
		"prompt should contain ID description")
	assert.True(t, strings.Contains(prompt, "display name"),
		"prompt should contain Name description")
}

type OmitEmptyStruct struct {
	Required string  `json:"required"`
	Optional string  `json:"optional,omitempty"`
	Pointer  *string `json:"pointer"`
}

func TestJSON_SchemaRequired(t *testing.T) {
	term := NewJSON[OmitEmptyStruct]()

	prompt := term.Prompt()

	assert.True(t, strings.Contains(prompt, `"required"`),
		"prompt should contain required field")
}

func TestJSON_ShouldTerminate(t *testing.T) {
	type input struct {
		content string
	}

	type expected struct {
		shouldTerminate bool
		containsName    bool
		containsValue   bool
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name:  "valid JSON terminates",
			input: input{content: `{"name": "test", "value": 42}`},
			expected: expected{
				shouldTerminate: true,
				containsName:    true,
				containsValue:   true,
			},
		},
		{
			name:  "empty content does not terminate",
			input: input{content: ""},
			expected: expected{
				shouldTerminate: false,
				containsName:    false,
				containsValue:   false,
			},
		},
		{
			name:  "invalid JSON does not terminate",
			input: input{content: `{invalid json}`},
			expected: expected{
				shouldTerminate: false,
				containsName:    false,
				containsValue:   false,
			},
		},
		{
			name:  "type mismatch does not terminate",
			input: input{content: `"just a string"`},
			expected: expected{
				shouldTerminate: false,
				containsName:    false,
				containsValue:   false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			term := NewJSON[SimpleStruct]()

			result := term.ShouldTerminate(tt.input.content)

			if tt.expected.shouldTerminate {
				assert.NotNil(t, result)
				assert.Len(t, result, 1)
				tc, ok := result[0].(llms.TextContent)
				assert.True(t, ok, "expected TextContent, got %T", result[0])
				if tt.expected.containsName {
					assert.True(t, strings.Contains(tc.Text, "test"),
						"result should contain 'test'")
				}
				if tt.expected.containsValue {
					assert.True(t, strings.Contains(tc.Text, "42"),
						"result should contain '42'")
				}
			} else {
				assert.Nil(t, result)
			}
		})
	}
}
