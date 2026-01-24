package toolchain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
)

func TestJSON_Name(t *testing.T) {
	type input struct {
		customName string
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
			input:    input{customName: ""},
			expected: expected{name: "action"},
		},
		{
			name:     "custom name",
			input:    input{customName: "tools"},
			expected: expected{name: "tools"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewJSON()
			if tt.input.customName != "" {
				tc.WithSectionName(tt.input.customName)
			}

			assert.Equal(t, tt.expected.name, tc.Name())
		})
	}
}

func TestJSON_RegisterTool(t *testing.T) {
	tc := NewJSON()
	tool := gent.NewToolFunc(
		"test",
		"A test tool",
		nil,
		func(ctx context.Context, args map[string]any) (string, error) {
			return "result", nil
		},
		textFormatter,
	)

	tc.RegisterTool(tool)

	content := `{"tool": "test", "args": {}}`
	result, err := tc.Execute(context.Background(), nil, content)
	require.NoError(t, err)
	require.Len(t, result.Calls, 1)
	assert.Equal(t, "test", result.Calls[0].Name)
}

func TestJSON_Prompt(t *testing.T) {
	tc := NewJSON()
	tool := gent.NewToolFunc(
		"search",
		"Search the web",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
			},
		},
		func(ctx context.Context, args map[string]any) (string, error) {
			return "", nil
		},
		nil,
	)
	tc.RegisterTool(tool)

	prompt := tc.Prompt()

	assert.True(t, strings.Contains(prompt, "search"), "expected tool name in prompt")
	assert.True(t, strings.Contains(prompt, "Search the web"),
		"expected tool description in prompt")
	assert.True(t, strings.Contains(prompt, `"tool"`), "expected JSON format instruction in prompt")
}

func TestJSON_ParseSection_SingleCall(t *testing.T) {
	tc := NewJSON()

	content := `{"tool": "search", "args": {"query": "weather"}}`
	result, err := tc.ParseSection(content)
	require.NoError(t, err)

	calls := result.([]*gent.ToolCall)
	require.Len(t, calls, 1)
	assert.Equal(t, "search", calls[0].Name)
	assert.Equal(t, "weather", calls[0].Args["query"])
}

func TestJSON_ParseSection_MultipleCall(t *testing.T) {
	tc := NewJSON()

	content := `[
		{"tool": "search", "args": {"query": "weather"}},
		{"tool": "calendar", "args": {"date": "today"}}
	]`

	result, err := tc.ParseSection(content)
	require.NoError(t, err)

	calls := result.([]*gent.ToolCall)
	require.Len(t, calls, 2)
	assert.Equal(t, "search", calls[0].Name)
	assert.Equal(t, "calendar", calls[1].Name)
}

func TestJSON_ParseSection_EmptyContent(t *testing.T) {
	tc := NewJSON()

	result, err := tc.ParseSection("")
	require.NoError(t, err)

	calls := result.([]*gent.ToolCall)
	assert.Empty(t, calls)
}

func TestJSON_ParseSection_InvalidJSON(t *testing.T) {
	tc := NewJSON()

	content := `{invalid json}`
	_, err := tc.ParseSection(content)
	assert.ErrorIs(t, err, gent.ErrInvalidJSON)
}

func TestJSON_ParseSection_MissingToolName(t *testing.T) {
	tc := NewJSON()

	content := `{"args": {"query": "weather"}}`
	_, err := tc.ParseSection(content)
	assert.ErrorIs(t, err, gent.ErrMissingToolName)
}

func TestJSON_ParseSection_MissingToolNameInArray(t *testing.T) {
	tc := NewJSON()

	content := `[{"tool": "search", "args": {}}, {"args": {}}]`
	_, err := tc.ParseSection(content)
	assert.ErrorIs(t, err, gent.ErrMissingToolName)
}

func TestJSON_Execute_Success(t *testing.T) {
	tc := NewJSON()
	tool := gent.NewToolFunc(
		"search",
		"Search the web",
		nil,
		func(ctx context.Context, args map[string]any) (string, error) {
			query := args["query"].(string)
			return fmt.Sprintf("Results for: %s", query), nil
		},
		textFormatter,
	)
	tc.RegisterTool(tool)

	content := `{"tool": "search", "args": {"query": "weather"}}`
	result, err := tc.Execute(context.Background(), nil, content)
	require.NoError(t, err)
	require.Len(t, result.Calls, 1)
	require.NotNil(t, result.Results[0])

	assert.Equal(t, "Results for: weather", getTextContent(result.Results[0].Result))
	assert.NoError(t, result.Errors[0])
}

func TestJSON_Execute_UnknownTool(t *testing.T) {
	tc := NewJSON()

	content := `{"tool": "unknown", "args": {}}`
	result, err := tc.Execute(context.Background(), nil, content)
	require.NoError(t, err)
	assert.ErrorIs(t, result.Errors[0], gent.ErrUnknownTool)
}

func TestJSON_Execute_ToolError(t *testing.T) {
	tc := NewJSON()
	tool := gent.NewToolFunc(
		"failing",
		"A failing tool",
		nil,
		func(ctx context.Context, args map[string]any) (string, error) {
			return "", errors.New("tool execution failed")
		},
		nil,
	)
	tc.RegisterTool(tool)

	content := `{"tool": "failing", "args": {}}`
	result, err := tc.Execute(context.Background(), nil, content)
	require.NoError(t, err)

	require.Error(t, result.Errors[0])
	assert.Equal(t, "tool execution failed", result.Errors[0].Error())
}

func TestJSON_Execute_MultipleTools(t *testing.T) {
	tc := NewJSON()

	searchTool := gent.NewToolFunc(
		"search",
		"Search",
		nil,
		func(ctx context.Context, args map[string]any) (string, error) {
			return "search result", nil
		},
		textFormatter,
	)

	calendarTool := gent.NewToolFunc(
		"calendar",
		"Calendar",
		nil,
		func(ctx context.Context, args map[string]any) (string, error) {
			return "calendar result", nil
		},
		textFormatter,
	)

	tc.RegisterTool(searchTool)
	tc.RegisterTool(calendarTool)

	content := `[
		{"tool": "search", "args": {}},
		{"tool": "calendar", "args": {}}
	]`

	result, err := tc.Execute(context.Background(), nil, content)
	require.NoError(t, err)
	require.Len(t, result.Results, 2)

	assert.Equal(t, "search result", getTextContent(result.Results[0].Result))
	assert.Equal(t, "calendar result", getTextContent(result.Results[1].Result))
}

func TestJSON_Execute_SchemaValidation_Success(t *testing.T) {
	tc := NewJSON()

	tool := gent.NewToolFunc(
		"search",
		"Search the web",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
			},
			"required": []any{"query"},
		},
		func(ctx context.Context, args map[string]any) (string, error) {
			return fmt.Sprintf("Results for: %s", args["query"]), nil
		},
		textFormatter,
	)
	tc.RegisterTool(tool)

	content := `{"tool": "search", "args": {"query": "weather"}}`
	result, err := tc.Execute(context.Background(), nil, content)
	require.NoError(t, err)

	assert.NoError(t, result.Errors[0])
	require.NotNil(t, result.Results[0])
	assert.Equal(t, "Results for: weather", getTextContent(result.Results[0].Result))
}

func TestJSON_Execute_SchemaValidation_MissingRequired(t *testing.T) {
	tc := NewJSON()

	tool := gent.NewToolFunc(
		"search",
		"Search the web",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
			},
			"required": []any{"query"},
		},
		func(ctx context.Context, args map[string]any) (string, error) {
			return "should not reach here", nil
		},
		textFormatter,
	)
	tc.RegisterTool(tool)

	content := `{"tool": "search", "args": {}}`
	result, err := tc.Execute(context.Background(), nil, content)
	require.NoError(t, err)

	require.Error(t, result.Errors[0])
	assert.True(t, strings.Contains(result.Errors[0].Error(), "schema validation failed"),
		"expected schema validation error, got: %v", result.Errors[0])
	assert.Nil(t, result.Results[0])
}

func TestJSON_Execute_SchemaValidation_WrongType(t *testing.T) {
	tc := NewJSON()

	tool := gent.NewToolFunc(
		"counter",
		"Count things",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"count": map[string]any{"type": "integer"},
			},
			"required": []any{"count"},
		},
		func(ctx context.Context, args map[string]any) (string, error) {
			return "should not reach here", nil
		},
		textFormatter,
	)
	tc.RegisterTool(tool)

	content := `{"tool": "counter", "args": {"count": "not a number"}}`
	result, err := tc.Execute(context.Background(), nil, content)
	require.NoError(t, err)

	require.Error(t, result.Errors[0])
	assert.True(t, strings.Contains(result.Errors[0].Error(), "schema validation failed"),
		"expected schema validation error, got: %v", result.Errors[0])
	assert.Nil(t, result.Results[0])
}

func TestJSON_Execute_NoSchema_NoValidation(t *testing.T) {
	tc := NewJSON()

	tool := gent.NewToolFunc(
		"flexible",
		"A flexible tool",
		nil,
		func(ctx context.Context, args map[string]any) (string, error) {
			return "success", nil
		},
		textFormatter,
	)
	tc.RegisterTool(tool)

	content := `{"tool": "flexible", "args": {"anything": "works", "numbers": 123}}`
	result, err := tc.Execute(context.Background(), nil, content)
	require.NoError(t, err)

	assert.NoError(t, result.Errors[0])
	assert.Equal(t, "success", getTextContent(result.Results[0].Result))
}

func TestJSON_Execute_SchemaValidation_MultipleProperties(t *testing.T) {
	tc := NewJSON()

	tool := gent.NewToolFunc(
		"booking",
		"Book a flight",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"origin":      map[string]any{"type": "string"},
				"destination": map[string]any{"type": "string"},
				"passengers":  map[string]any{"type": "integer"},
			},
			"required": []any{"origin", "destination", "passengers"},
		},
		func(ctx context.Context, args map[string]any) (string, error) {
			return "booked", nil
		},
		textFormatter,
	)
	tc.RegisterTool(tool)

	t.Run("valid args", func(t *testing.T) {
		content := `{"tool": "booking", "args": {"origin": "NYC", "destination": "LAX", ` +
			`"passengers": 2}}`
		result, err := tc.Execute(context.Background(), nil, content)
		require.NoError(t, err)
		assert.NoError(t, result.Errors[0])
	})

	t.Run("missing required field", func(t *testing.T) {
		content := `{"tool": "booking", "args": {"origin": "NYC", "destination": "LAX"}}`
		result, err := tc.Execute(context.Background(), nil, content)
		require.NoError(t, err)
		require.Error(t, result.Errors[0])
	})
}

func TestJSON_ParseSection_TimeFormatsAsString(t *testing.T) {
	type input struct {
		jsonVal string
	}

	type expected struct {
		value string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name:     "date only",
			input:    input{jsonVal: `"2026-01-20"`},
			expected: expected{value: "2026-01-20"},
		},
		{
			name:     "datetime ISO",
			input:    input{jsonVal: `"2026-01-20T10:30:00Z"`},
			expected: expected{value: "2026-01-20T10:30:00Z"},
		},
		{
			name:     "datetime with timezone",
			input:    input{jsonVal: `"2026-01-20T10:30:00+05:00"`},
			expected: expected{value: "2026-01-20T10:30:00+05:00"},
		},
		{
			name:     "time only",
			input:    input{jsonVal: `"10:30:00"`},
			expected: expected{value: "10:30:00"},
		},
		{
			name:     "duration string",
			input:    input{jsonVal: `"1h30m"`},
			expected: expected{value: "1h30m"},
		},
		{
			name:     "duration with seconds",
			input:    input{jsonVal: `"2h45m30s"`},
			expected: expected{value: "2h45m30s"},
		},
		{
			name:     "ISO 8601 duration",
			input:    input{jsonVal: `"PT1H30M"`},
			expected: expected{value: "PT1H30M"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewJSON()

			tool := gent.NewToolFunc(
				"test_tool",
				"Test tool",
				map[string]any{
					"type": "object",
					"properties": map[string]any{
						"value": map[string]any{"type": "string"},
					},
					"required": []any{"value"},
				},
				func(ctx context.Context, args map[string]any) (string, error) {
					return args["value"].(string), nil
				},
				textFormatter,
			)
			tc.RegisterTool(tool)

			content := fmt.Sprintf(`{"tool": "test_tool", "args": {"value": %s}}`, tt.input.jsonVal)

			result, err := tc.ParseSection(content)
			require.NoError(t, err)

			calls := result.([]*gent.ToolCall)
			val := calls[0].Args["value"]
			strVal, ok := val.(string)
			require.True(t, ok, "expected string, got %T: %v", val, val)
			assert.Equal(t, tt.expected.value, strVal)

			execResult, err := tc.Execute(context.Background(), nil, content)
			require.NoError(t, err)
			assert.NoError(t, execResult.Errors[0])
		})
	}
}

func TestJSON_ParseSection_DateAsString(t *testing.T) {
	tc := NewJSON()

	tool := gent.NewToolFunc(
		"search_flights",
		"Search flights",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"origin":      map[string]any{"type": "string"},
				"destination": map[string]any{"type": "string"},
				"date":        map[string]any{"type": "string"},
			},
			"required": []any{"origin", "destination", "date"},
		},
		func(ctx context.Context, args map[string]any) (string, error) {
			return fmt.Sprintf("searching for %s", args["date"]), nil
		},
		textFormatter,
	)
	tc.RegisterTool(tool)

	content := `{"tool": "search_flights", "args": {"origin": "JFK", ` +
		`"destination": "LAX", "date": "2026-01-20"}}`

	result, err := tc.ParseSection(content)
	require.NoError(t, err)

	calls := result.([]*gent.ToolCall)
	require.Len(t, calls, 1)

	dateVal := calls[0].Args["date"]
	dateStr, ok := dateVal.(string)
	require.True(t, ok, "expected date to be string, got %T: %v", dateVal, dateVal)
	assert.Equal(t, "2026-01-20", dateStr)

	execResult, err := tc.Execute(context.Background(), nil, content)
	require.NoError(t, err)
	assert.NoError(t, execResult.Errors[0])
}

// JSONTimeTypedInput is used to test automatic type conversion to time.Time
type JSONTimeTypedInput struct {
	Date      time.Time `json:"date"`
	Timestamp time.Time `json:"timestamp"`
}

// JSONDurationTypedInput is used to test automatic type conversion to time.Duration
type JSONDurationTypedInput struct {
	Duration time.Duration `json:"duration"`
}

func TestJSON_Execute_TimeConversion(t *testing.T) {
	tc := NewJSON()

	tool := gent.NewToolFunc(
		"time_tool",
		"Tool with time.Time input",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"date":      map[string]any{"type": "string"},
				"timestamp": map[string]any{"type": "string"},
			},
			"required": []any{"date", "timestamp"},
		},
		func(ctx context.Context, input JSONTimeTypedInput) (string, error) {
			return input.Date.Format("2006-01-02") + "|" +
				input.Timestamp.Format(time.RFC3339), nil
		},
		textFormatter,
	)
	tc.RegisterTool(tool)

	content := `{"tool": "time_tool", "args": {"date": "2026-01-20", ` +
		`"timestamp": "2026-01-20T10:30:00Z"}}`

	result, err := tc.Execute(context.Background(), nil, content)
	require.NoError(t, err)
	require.NoError(t, result.Errors[0])

	expected := "2026-01-20|2026-01-20T10:30:00Z"
	output := getTextContent(result.Results[0].Result)
	assert.Equal(t, expected, output)
}

func TestJSON_Execute_DurationConversion(t *testing.T) {
	type input struct {
		duration string
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
			name:     "hours and minutes",
			input:    input{duration: "1h30m"},
			expected: expected{output: "1h30m0s"},
		},
		{
			name:     "just minutes",
			input:    input{duration: "45m"},
			expected: expected{output: "45m0s"},
		},
		{
			name:     "with seconds",
			input:    input{duration: "2h15m30s"},
			expected: expected{output: "2h15m30s"},
		},
		{
			name:     "milliseconds",
			input:    input{duration: "500ms"},
			expected: expected{output: "500ms"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewJSON()

			tool := gent.NewToolFunc(
				"duration_tool",
				"Tool with time.Duration input",
				map[string]any{
					"type": "object",
					"properties": map[string]any{
						"duration": map[string]any{"type": "string"},
					},
					"required": []any{"duration"},
				},
				func(ctx context.Context, input JSONDurationTypedInput) (string, error) {
					return input.Duration.String(), nil
				},
				textFormatter,
			)
			tc.RegisterTool(tool)

			content := fmt.Sprintf(
				`{"tool": "duration_tool", "args": {"duration": "%s"}}`,
				tt.input.duration,
			)

			result, err := tc.Execute(context.Background(), nil, content)
			require.NoError(t, err)
			require.NoError(t, result.Errors[0])

			output := getTextContent(result.Results[0].Result)
			assert.Equal(t, tt.expected.output, output)
		})
	}
}

func TestJSON_Prompt_SchemaWithDescriptions(t *testing.T) {
	tc := NewJSON()
	tool := gent.NewToolFunc(
		"search_flights",
		"Search for available flights",
		schema.Object(map[string]*schema.Property{
			"origin":      schema.String("Origin airport code (IATA)"),
			"destination": schema.String("Destination airport code (IATA)"),
			"date":        schema.String("Departure date in YYYY-MM-DD format"),
		}, "origin", "destination", "date"),
		func(ctx context.Context, input map[string]any) (string, error) {
			return "ok", nil
		},
		nil,
	)
	tc.RegisterTool(tool)

	prompt := tc.Prompt()

	assert.True(t, strings.Contains(prompt, "search_flights"),
		"expected tool name 'search_flights' in prompt")
	assert.True(t, strings.Contains(prompt, "Search for available flights"),
		"expected tool description in prompt")
	assert.True(t, strings.Contains(prompt, "Origin airport code (IATA)"),
		"expected 'origin' field description in prompt")
	assert.True(t, strings.Contains(prompt, "Destination airport code (IATA)"),
		"expected 'destination' field description in prompt")
	assert.True(t, strings.Contains(prompt, "Departure date in YYYY-MM-DD format"),
		"expected 'date' field description in prompt")
	assert.True(t, strings.Contains(prompt, `"type": "string"`),
		"expected type information in prompt")
}

func TestJSON_Prompt_SchemaWithRequiredFields(t *testing.T) {
	tc := NewJSON()
	tool := gent.NewToolFunc(
		"create_user",
		"Create a new user",
		schema.Object(map[string]*schema.Property{
			"name":  schema.String("User's full name"),
			"email": schema.String("User's email address"),
			"phone": schema.String("User's phone number"),
		}, "name", "email"),
		func(ctx context.Context, input map[string]any) (string, error) {
			return "ok", nil
		},
		nil,
	)
	tc.RegisterTool(tool)

	prompt := tc.Prompt()

	assert.True(t, strings.Contains(prompt, `"required"`), "expected 'required' section in prompt")
	assert.True(t, strings.Contains(prompt, `"name"`), "expected 'name' in required list")
	assert.True(t, strings.Contains(prompt, `"email"`), "expected 'email' in required list")
}

func TestJSON_Prompt_SchemaWithEnumValues(t *testing.T) {
	tc := NewJSON()
	tool := gent.NewToolFunc(
		"book_flight",
		"Book a flight",
		schema.Object(map[string]*schema.Property{
			"class": schema.String("Travel class").Enum("economy", "business", "first"),
		}),
		func(ctx context.Context, input map[string]any) (string, error) {
			return "ok", nil
		},
		nil,
	)
	tc.RegisterTool(tool)

	prompt := tc.Prompt()

	assert.True(t, strings.Contains(prompt, `"enum"`), "expected 'enum' section in prompt")
	assert.True(t, strings.Contains(prompt, `"economy"`), "expected 'economy' in enum list")
	assert.True(t, strings.Contains(prompt, `"business"`), "expected 'business' in enum list")
	assert.True(t, strings.Contains(prompt, `"first"`), "expected 'first' in enum list")
}

func TestJSON_Prompt_SchemaWithMinMax(t *testing.T) {
	tc := NewJSON()
	tool := gent.NewToolFunc(
		"set_quantity",
		"Set item quantity",
		schema.Object(map[string]*schema.Property{
			"quantity": schema.Integer("Number of items").Min(1).Max(100),
		}, "quantity"),
		func(ctx context.Context, input map[string]any) (string, error) {
			return "ok", nil
		},
		nil,
	)
	tc.RegisterTool(tool)

	prompt := tc.Prompt()

	assert.True(t, strings.Contains(prompt, `"minimum": 1`),
		"expected '\"minimum\": 1' in prompt")
	assert.True(t, strings.Contains(prompt, `"maximum": 100`),
		"expected '\"maximum\": 100' in prompt")
	assert.True(t, strings.Contains(prompt, `"type": "integer"`),
		"expected '\"type\": \"integer\"' in prompt")
}

func TestJSON_Prompt_SchemaWithDefaultValues(t *testing.T) {
	tc := NewJSON()
	tool := gent.NewToolFunc(
		"search",
		"Search for items",
		schema.Object(map[string]*schema.Property{
			"query": schema.String("Search query"),
			"limit": schema.Integer("Maximum results").Default(10),
		}, "query"),
		func(ctx context.Context, input map[string]any) (string, error) {
			return "ok", nil
		},
		nil,
	)
	tc.RegisterTool(tool)

	prompt := tc.Prompt()

	assert.True(t, strings.Contains(prompt, `"default": 10`),
		"expected '\"default\": 10' in prompt")
}

func TestJSON_Prompt_SchemaWithMultipleTypes(t *testing.T) {
	tc := NewJSON()
	tool := gent.NewToolFunc(
		"complex_tool",
		"A tool with multiple types",
		schema.Object(map[string]*schema.Property{
			"name":   schema.String("Name of the item"),
			"count":  schema.Integer("Number of items"),
			"price":  schema.Number("Price in dollars"),
			"active": schema.Boolean("Whether the item is active"),
			"tags":   schema.Array("List of tags", map[string]any{"type": "string"}),
		}, "name"),
		func(ctx context.Context, input map[string]any) (string, error) {
			return "ok", nil
		},
		nil,
	)
	tc.RegisterTool(tool)

	prompt := tc.Prompt()

	assert.True(t, strings.Contains(prompt, `"type": "string"`),
		"expected '\"type\": \"string\"' in prompt")
	assert.True(t, strings.Contains(prompt, `"type": "integer"`),
		"expected '\"type\": \"integer\"' in prompt")
	assert.True(t, strings.Contains(prompt, `"type": "number"`),
		"expected '\"type\": \"number\"' in prompt")
	assert.True(t, strings.Contains(prompt, `"type": "boolean"`),
		"expected '\"type\": \"boolean\"' in prompt")
	assert.True(t, strings.Contains(prompt, `"type": "array"`),
		"expected '\"type\": \"array\"' in prompt")
	assert.True(t, strings.Contains(prompt, "Name of the item"),
		"expected string description in prompt")
	assert.True(t, strings.Contains(prompt, "Number of items"),
		"expected integer description in prompt")
	assert.True(t, strings.Contains(prompt, "Price in dollars"),
		"expected number description in prompt")
	assert.True(t, strings.Contains(prompt, "Whether the item is active"),
		"expected boolean description in prompt")
	assert.True(t, strings.Contains(prompt, "List of tags"),
		"expected array description in prompt")
}

func TestJSON_Prompt_MultipleTools(t *testing.T) {
	tc := NewJSON()

	tool1 := gent.NewToolFunc(
		"search",
		"Search for information",
		schema.Object(map[string]*schema.Property{
			"query": schema.String("Search query"),
		}, "query"),
		func(ctx context.Context, input map[string]any) (string, error) {
			return "ok", nil
		},
		nil,
	)

	tool2 := gent.NewToolFunc(
		"calculate",
		"Perform calculations",
		schema.Object(map[string]*schema.Property{
			"expression": schema.String("Mathematical expression"),
		}, "expression"),
		func(ctx context.Context, input map[string]any) (string, error) {
			return "ok", nil
		},
		nil,
	)

	tc.RegisterTool(tool1)
	tc.RegisterTool(tool2)

	prompt := tc.Prompt()

	assert.True(t, strings.Contains(prompt, "- search:"), "expected 'search' tool in prompt")
	assert.True(t, strings.Contains(prompt, "- calculate:"), "expected 'calculate' tool in prompt")
	assert.True(t, strings.Contains(prompt, "Search for information"),
		"expected 'search' tool description in prompt")
	assert.True(t, strings.Contains(prompt, "Perform calculations"),
		"expected 'calculate' tool description in prompt")
	assert.True(t, strings.Contains(prompt, "Search query"),
		"expected 'query' field description in prompt")
	assert.True(t, strings.Contains(prompt, "Mathematical expression"),
		"expected 'expression' field description in prompt")
}

func TestJSON_Prompt_ToolWithNoSchema(t *testing.T) {
	tc := NewJSON()
	tool := gent.NewToolFunc(
		"simple_tool",
		"A simple tool without schema",
		nil,
		func(ctx context.Context, input map[string]any) (string, error) {
			return "ok", nil
		},
		nil,
	)
	tc.RegisterTool(tool)

	prompt := tc.Prompt()

	assert.True(t, strings.Contains(prompt, "simple_tool"),
		"expected tool name 'simple_tool' in prompt")
	assert.True(t, strings.Contains(prompt, "A simple tool without schema"),
		"expected tool description in prompt")
}

func TestJSON_Prompt_SchemaWithStringConstraints(t *testing.T) {
	tc := NewJSON()
	tool := gent.NewToolFunc(
		"validate_input",
		"Validate user input",
		schema.Object(map[string]*schema.Property{
			"username": schema.String("Username").MinLength(3).MaxLength(20),
			"email":    schema.String("Email address").Format("email"),
			"code":     schema.String("Verification code").Pattern(`^[A-Z]{2}[0-9]{4}$`),
		}, "username", "email"),
		func(ctx context.Context, input map[string]any) (string, error) {
			return "ok", nil
		},
		nil,
	)
	tc.RegisterTool(tool)

	prompt := tc.Prompt()

	assert.True(t, strings.Contains(prompt, `"minLength": 3`),
		"expected '\"minLength\": 3' in prompt")
	assert.True(t, strings.Contains(prompt, `"maxLength": 20`),
		"expected '\"maxLength\": 20' in prompt")
	assert.True(t, strings.Contains(prompt, `"format": "email"`),
		"expected '\"format\": \"email\"' in prompt")
	assert.True(t, strings.Contains(prompt, `"pattern"`), "expected 'pattern' in prompt")
}

func TestJSON_Prompt_FormatInstructions(t *testing.T) {
	tc := NewJSON()
	tool := gent.NewToolFunc(
		"test",
		"Test tool",
		nil,
		func(ctx context.Context, input map[string]any) (string, error) {
			return "ok", nil
		},
		nil,
	)
	tc.RegisterTool(tool)

	prompt := tc.Prompt()

	assert.True(t, strings.Contains(prompt, `{"tool": "tool_name", "args": {...}}`),
		"expected single tool format instruction")
	assert.True(t, strings.Contains(prompt,
		`[{"tool": "tool1", "args": {...}}, {"tool": "tool2", "args": {...}}]`),
		"expected array format instruction for parallel calls")
	assert.True(t, strings.Contains(prompt, "Available tools:"),
		"expected 'Available tools:' header")
}

// textFormatter converts a string to []gent.ContentPart.
func textFormatter(s string) []gent.ContentPart {
	return []gent.ContentPart{llms.TextContent{Text: s}}
}

// getTextContent extracts the text from a []gent.ContentPart (assumes single TextContent).
func getTextContent(parts []gent.ContentPart) string {
	if len(parts) == 0 {
		return ""
	}
	if tc, ok := parts[0].(llms.TextContent); ok {
		return tc.Text
	}
	return ""
}
