package toolchain

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/format"
	"github.com/rickchristie/gent/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testFormat returns a TextFormat for use in tests.
func testFormat() gent.TextFormat {
	return format.NewXML()
}

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
	type input struct {
		toolName string
		content  string
	}

	type expected struct {
		callCount int
		callName  string
		err       error
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "register and execute tool",
			input: input{
				toolName: "test",
				content:  `{"tool": "test", "args": {}}`,
			},
			expected: expected{
				callCount: 1,
				callName:  "test",
				err:       nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewJSON()
			tool := gent.NewToolFunc(
				tt.input.toolName,
				"A test tool",
				nil,
				func(ctx context.Context, args map[string]any) (string, error) {
					return "result", nil
				},
			)

			tc.RegisterTool(tool)

			result, err := tc.Execute(nil, tt.input.content, testFormat())

			if tt.expected.err != nil {
				assert.ErrorIs(t, err, tt.expected.err)
			} else {
				require.NoError(t, err)
				assert.Len(t, result.Raw.Calls, tt.expected.callCount)
				assert.Equal(t, tt.expected.callName, result.Raw.Calls[0].Name)
			}
		})
	}
}

func TestJSON_Prompt(t *testing.T) {
	tc := NewJSON()
	tool := gent.NewToolFunc(
		"test_tool",
		"A test tool",
		nil,
		func(ctx context.Context, args map[string]any) (string, error) {
			return "", nil
		},
	)
	tc.RegisterTool(tool)

	guidance := tc.Guidance()

	expectedGuidance := `Call tools using JSON format:
{"tool": "tool_name", "args": {...}}

For multiple parallel calls, use an array:
[{"tool": "tool1", "args": {...}}, {"tool": "tool2", "args": {...}}]`

	assert.Equal(t, expectedGuidance, guidance)
}

func TestJSON_AvailableToolsPrompt(t *testing.T) {
	type input struct {
		toolName        string
		toolDescription string
		toolSchema      map[string]any
	}

	type expected struct {
		catalog string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "tool with schema",
			input: input{
				toolName:        "search",
				toolDescription: "Search the web",
				toolSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{"type": "string"},
					},
				},
			},
			expected: expected{
				catalog: `Available tools:

- search: Search the web
  Parameters: {
    "properties": {
      "query": {
        "type": "string"
      }
    },
    "type": "object"
  }
`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewJSON()
			tool := gent.NewToolFunc(
				tt.input.toolName,
				tt.input.toolDescription,
				tt.input.toolSchema,
				func(ctx context.Context, args map[string]any) (string, error) {
					return "", nil
				},
			)
			tc.RegisterTool(tool)

			catalog := tc.AvailableToolsPrompt()

			assert.Equal(t, tt.expected.catalog, catalog)
		})
	}
}

func TestJSON_ParseSection(t *testing.T) {
	type input struct {
		content string
	}

	type expected struct {
		calls []*gent.ToolCall
		err   error
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "single call",
			input: input{
				content: `{"tool": "search", "args": {"query": "weather"}}`,
			},
			expected: expected{
				calls: []*gent.ToolCall{
					{Name: "search", Args: map[string]any{"query": "weather"}},
				},
				err: nil,
			},
		},
		{
			name: "multiple calls",
			input: input{
				content: `[
		{"tool": "search", "args": {"query": "weather"}},
		{"tool": "calendar", "args": {"date": "today"}}
	]`,
			},
			expected: expected{
				calls: []*gent.ToolCall{
					{Name: "search", Args: map[string]any{"query": "weather"}},
					{Name: "calendar", Args: map[string]any{"date": "today"}},
				},
				err: nil,
			},
		},
		{
			name: "empty content",
			input: input{
				content: "",
			},
			expected: expected{
				calls: []*gent.ToolCall{},
				err:   nil,
			},
		},
		{
			name: "invalid JSON",
			input: input{
				content: `{invalid json}`,
			},
			expected: expected{
				calls: nil,
				err:   gent.ErrInvalidJSON,
			},
		},
		{
			name: "missing tool name",
			input: input{
				content: `{"args": {"query": "weather"}}`,
			},
			expected: expected{
				calls: nil,
				err:   gent.ErrMissingToolName,
			},
		},
		{
			name: "missing tool name in array",
			input: input{
				content: `[{"tool": "search", "args": {}}, {"args": {}}]`,
			},
			expected: expected{
				calls: nil,
				err:   gent.ErrMissingToolName,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewJSON()

			result, err := tc.ParseSection(nil, tt.input.content)

			if tt.expected.err != nil {
				assert.ErrorIs(t, err, tt.expected.err)
				return
			}

			require.NoError(t, err)
			calls := result.([]*gent.ToolCall)
			assert.Len(t, calls, len(tt.expected.calls))

			for i, expectedCall := range tt.expected.calls {
				assert.Equal(t, expectedCall.Name, calls[i].Name)
				assert.Equal(t, expectedCall.Args, calls[i].Args)
			}
		})
	}
}

func TestJSON_Execute(t *testing.T) {
	type mockTool struct {
		name        string
		description string
		schema      map[string]any
		fn          func(ctx context.Context, args map[string]any) (string, error)
	}

	type input struct {
		content string
	}

	type expected struct {
		callCount   int
		results     []string
		errors      []error
		executeErr  error
		resultNames []string
	}

	tests := []struct {
		name     string
		mocks    []mockTool
		input    input
		expected expected
	}{
		{
			name: "success",
			mocks: []mockTool{
				{
					name:        "search",
					description: "Search the web",
					fn: func(ctx context.Context, args map[string]any) (string, error) {
						query := args["query"].(string)
						return fmt.Sprintf("Results for: %s", query), nil
					},
				},
			},
			input: input{
				content: `{"tool": "search", "args": {"query": "weather"}}`,
			},
			expected: expected{
				callCount:   1,
				results:     []string{`Results for: weather`},
				errors:      []error{nil},
				resultNames: []string{"search"},
			},
		},
		{
			name:  "unknown tool",
			mocks: []mockTool{},
			input: input{
				content: `{"tool": "unknown", "args": {}}`,
			},
			expected: expected{
				callCount: 1,
				results:   []string{""},
				errors:    []error{gent.ErrUnknownTool},
			},
		},
		{
			name: "tool error",
			mocks: []mockTool{
				{
					name:        "failing",
					description: "A failing tool",
					fn: func(ctx context.Context, args map[string]any) (string, error) {
						return "", errors.New("tool execution failed")
					},
				},
			},
			input: input{
				content: `{"tool": "failing", "args": {}}`,
			},
			expected: expected{
				callCount: 1,
				results:   []string{""},
				errors:    []error{errors.New("tool execution failed")},
			},
		},
		{
			name: "multiple tools",
			mocks: []mockTool{
				{
					name:        "search",
					description: "Search",
					fn: func(ctx context.Context, args map[string]any) (string, error) {
						return "search result", nil
					},
				},
				{
					name:        "calendar",
					description: "Calendar",
					fn: func(ctx context.Context, args map[string]any) (string, error) {
						return "calendar result", nil
					},
				},
			},
			input: input{
				content: `[
		{"tool": "search", "args": {}},
		{"tool": "calendar", "args": {}}
	]`,
			},
			expected: expected{
				callCount:   2,
				results:     []string{`search result`, `calendar result`},
				errors:      []error{nil, nil},
				resultNames: []string{"search", "calendar"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewJSON()
			for _, mock := range tt.mocks {
				tool := gent.NewToolFunc(
					mock.name,
					mock.description,
					mock.schema,
					mock.fn,
				)
				tc.RegisterTool(tool)
			}

			result, err := tc.Execute(nil, tt.input.content, testFormat())

			if tt.expected.executeErr != nil {
				assert.ErrorIs(t, err, tt.expected.executeErr)
				return
			}

			require.NoError(t, err)
			assert.Len(t, result.Raw.Calls, tt.expected.callCount)

			for i, expectedResult := range tt.expected.results {
				if tt.expected.errors[i] != nil {
					assert.Error(t, result.Raw.Errors[i])
					if errors.Is(tt.expected.errors[i], gent.ErrUnknownTool) {
						assert.ErrorIs(t, result.Raw.Errors[i], gent.ErrUnknownTool)
					} else {
						assert.Equal(t, tt.expected.errors[i].Error(), result.Raw.Errors[i].Error())
					}
				} else {
					assert.NoError(t, result.Raw.Errors[i])
					assert.Equal(t, expectedResult, getOutputString(result.Raw.Results[i].Output))
					if len(tt.expected.resultNames) > i {
						assert.Equal(t, tt.expected.resultNames[i], result.Raw.Results[i].Name)
					}
				}
			}
		})
	}
}

func TestJSON_Execute_SchemaValidation(t *testing.T) {
	type mockTool struct {
		name        string
		description string
		schema      map[string]any
		fn          func(ctx context.Context, args map[string]any) (string, error)
	}

	type input struct {
		content string
	}

	type expected struct {
		result      string
		errContains string
		resultIsNil bool
		noToolError bool
	}

	tests := []struct {
		name     string
		mocks    []mockTool
		input    input
		expected expected
	}{
		{
			name: "valid args",
			mocks: []mockTool{
				{
					name:        "search",
					description: "Search the web",
					schema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"query": map[string]any{"type": "string"},
						},
						"required": []any{"query"},
					},
					fn: func(ctx context.Context, args map[string]any) (string, error) {
						return fmt.Sprintf("Results for: %s", args["query"]), nil
					},
				},
			},
			input: input{
				content: `{"tool": "search", "args": {"query": "weather"}}`,
			},
			expected: expected{
				result:      `Results for: weather`,
				noToolError: true,
			},
		},
		{
			name: "missing required field",
			mocks: []mockTool{
				{
					name:        "search",
					description: "Search the web",
					schema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"query": map[string]any{"type": "string"},
						},
						"required": []any{"query"},
					},
					fn: func(ctx context.Context, args map[string]any) (string, error) {
						return "should not reach here", nil
					},
				},
			},
			input: input{
				content: `{"tool": "search", "args": {}}`,
			},
			expected: expected{
				errContains: "schema validation failed",
				resultIsNil: true,
			},
		},
		{
			name: "wrong type",
			mocks: []mockTool{
				{
					name:        "counter",
					description: "Count things",
					schema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"count": map[string]any{"type": "integer"},
						},
						"required": []any{"count"},
					},
					fn: func(ctx context.Context, args map[string]any) (string, error) {
						return "should not reach here", nil
					},
				},
			},
			input: input{
				content: `{"tool": "counter", "args": {"count": "not a number"}}`,
			},
			expected: expected{
				errContains: "schema validation failed",
				resultIsNil: true,
			},
		},
		{
			name: "no schema allows any args",
			mocks: []mockTool{
				{
					name:        "flexible",
					description: "A flexible tool",
					schema:      nil,
					fn: func(ctx context.Context, args map[string]any) (string, error) {
						return "success", nil
					},
				},
			},
			input: input{
				content: `{"tool": "flexible", "args": {"anything": "works", "numbers": 123}}`,
			},
			expected: expected{
				result:      `success`,
				noToolError: true,
			},
		},
		{
			name: "multiple properties valid",
			mocks: []mockTool{
				{
					name:        "booking",
					description: "Book a flight",
					schema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"origin":      map[string]any{"type": "string"},
							"destination": map[string]any{"type": "string"},
							"passengers":  map[string]any{"type": "integer"},
						},
						"required": []any{"origin", "destination", "passengers"},
					},
					fn: func(ctx context.Context, args map[string]any) (string, error) {
						return "booked", nil
					},
				},
			},
			input: input{
				content: `{"tool": "booking", "args": {"origin": "NYC", "destination": "LAX", ` +
					`"passengers": 2}}`,
			},
			expected: expected{
				result:      `booked`,
				noToolError: true,
			},
		},
		{
			name: "multiple properties missing required",
			mocks: []mockTool{
				{
					name:        "booking",
					description: "Book a flight",
					schema: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"origin":      map[string]any{"type": "string"},
							"destination": map[string]any{"type": "string"},
							"passengers":  map[string]any{"type": "integer"},
						},
						"required": []any{"origin", "destination", "passengers"},
					},
					fn: func(ctx context.Context, args map[string]any) (string, error) {
						return "booked", nil
					},
				},
			},
			input: input{
				content: `{"tool": "booking", "args": {"origin": "NYC", "destination": "LAX"}}`,
			},
			expected: expected{
				errContains: "schema validation failed",
				resultIsNil: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewJSON()
			for _, mock := range tt.mocks {
				tool := gent.NewToolFunc(
					mock.name,
					mock.description,
					mock.schema,
					mock.fn,
				)
				tc.RegisterTool(tool)
			}

			result, err := tc.Execute(nil, tt.input.content, testFormat())
			require.NoError(t, err)

			if tt.expected.noToolError {
				assert.NoError(t, result.Raw.Errors[0])
				require.NotNil(t, result.Raw.Results[0])
				assert.Equal(t, tt.expected.result, getOutputString(result.Raw.Results[0].Output))
			} else {
				require.Error(t, result.Raw.Errors[0])
				assert.Contains(t, result.Raw.Errors[0].Error(), tt.expected.errContains)
				if tt.expected.resultIsNil {
					assert.Nil(t, result.Raw.Results[0])
				}
			}
		})
	}
}

func TestJSON_ParseSection_DateAsString(t *testing.T) {
	type input struct {
		content string
	}

	type expected struct {
		dateValue string
		execErr   error
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "date preserved as string",
			input: input{
				content: `{"tool": "search_flights", "args": {"origin": "JFK", ` +
					`"destination": "LAX", "date": "2026-01-20"}}`,
			},
			expected: expected{
				dateValue: "2026-01-20",
				execErr:   nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
			)
			tc.RegisterTool(tool)

			result, err := tc.ParseSection(nil, tt.input.content)
			require.NoError(t, err)

			calls := result.([]*gent.ToolCall)
			require.Len(t, calls, 1)

			dateVal := calls[0].Args["date"]
			dateStr, ok := dateVal.(string)
			require.True(t, ok, "expected date to be string, got %T: %v", dateVal, dateVal)
			assert.Equal(t, tt.expected.dateValue, dateStr)

			execResult, err := tc.Execute(nil, tt.input.content, testFormat())
			require.NoError(t, err)
			assert.NoError(t, execResult.Raw.Errors[0])
		})
	}
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
			)
			tc.RegisterTool(tool)

			content := fmt.Sprintf(`{"tool": "test_tool", "args": {"value": %s}}`, tt.input.jsonVal)

			result, err := tc.ParseSection(nil, content)
			require.NoError(t, err)

			calls := result.([]*gent.ToolCall)
			val := calls[0].Args["value"]
			strVal, ok := val.(string)
			require.True(t, ok, "expected string, got %T: %v", val, val)
			assert.Equal(t, tt.expected.value, strVal)

			execResult, err := tc.Execute(nil, content, testFormat())
			require.NoError(t, err)
			assert.NoError(t, execResult.Raw.Errors[0])
		})
	}
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
	type input struct {
		content string
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
			name: "time values converted correctly",
			input: input{
				content: `{"tool": "time_tool", "args": {"date": "2026-01-20", ` +
					`"timestamp": "2026-01-20T10:30:00Z"}}`,
			},
			expected: expected{
				output: `2026-01-20|2026-01-20T10:30:00Z`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
			)
			tc.RegisterTool(tool)

			result, err := tc.Execute(nil, tt.input.content, testFormat())
			require.NoError(t, err)
			require.NoError(t, result.Raw.Errors[0])

			output := getOutputString(result.Raw.Results[0].Output)
			assert.Equal(t, tt.expected.output, output)
		})
	}
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
			expected: expected{output: `1h30m0s`},
		},
		{
			name:     "just minutes",
			input:    input{duration: "45m"},
			expected: expected{output: `45m0s`},
		},
		{
			name:     "with seconds",
			input:    input{duration: "2h15m30s"},
			expected: expected{output: `2h15m30s`},
		},
		{
			name:     "milliseconds",
			input:    input{duration: "500ms"},
			expected: expected{output: `500ms`},
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
			)
			tc.RegisterTool(tool)

			content := fmt.Sprintf(
				`{"tool": "duration_tool", "args": {"duration": "%s"}}`,
				tt.input.duration,
			)

			result, err := tc.Execute(nil, content, testFormat())
			require.NoError(t, err)
			require.NoError(t, result.Raw.Errors[0])

			output := getOutputString(result.Raw.Results[0].Output)
			assert.Equal(t, tt.expected.output, output)
		})
	}
}

func TestJSON_AvailableToolsPrompt_SchemaFeatures(t *testing.T) {
	type input struct {
		toolName        string
		toolDescription string
		schema          map[string]any
	}

	type expected struct {
		catalog string
	}

	baseCatalogPrefix := "Available tools:\n"

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "schema with descriptions",
			input: input{
				toolName:        "search_flights",
				toolDescription: "Search for available flights",
				schema: schema.Object(map[string]*schema.Property{
					"origin":      schema.String("Origin airport code (IATA)"),
					"destination": schema.String("Destination airport code (IATA)"),
					"date":        schema.String("Departure date in YYYY-MM-DD format"),
				}, "origin", "destination", "date"),
			},
			expected: expected{
				catalog: baseCatalogPrefix + `
- search_flights: Search for available flights
  Parameters: {
    "properties": {
      "date": {
        "description": "Departure date in YYYY-MM-DD format",
        "type": "string"
      },
      "destination": {
        "description": "Destination airport code (IATA)",
        "type": "string"
      },
      "origin": {
        "description": "Origin airport code (IATA)",
        "type": "string"
      }
    },
    "required": [
      "origin",
      "destination",
      "date"
    ],
    "type": "object"
  }
`,
			},
		},
		{
			name: "schema with required fields",
			input: input{
				toolName:        "create_user",
				toolDescription: "Create a new user",
				schema: schema.Object(map[string]*schema.Property{
					"name":  schema.String("User's full name"),
					"email": schema.String("User's email address"),
					"phone": schema.String("User's phone number"),
				}, "name", "email"),
			},
			expected: expected{
				catalog: baseCatalogPrefix + `
- create_user: Create a new user
  Parameters: {
    "properties": {
      "email": {
        "description": "User's email address",
        "type": "string"
      },
      "name": {
        "description": "User's full name",
        "type": "string"
      },
      "phone": {
        "description": "User's phone number",
        "type": "string"
      }
    },
    "required": [
      "name",
      "email"
    ],
    "type": "object"
  }
`,
			},
		},
		{
			name: "schema with enum values",
			input: input{
				toolName:        "book_flight",
				toolDescription: "Book a flight",
				schema: schema.Object(map[string]*schema.Property{
					"class": schema.String("Travel class").Enum("economy", "business", "first"),
				}),
			},
			expected: expected{
				catalog: baseCatalogPrefix + `
- book_flight: Book a flight
  Parameters: {
    "properties": {
      "class": {
        "description": "Travel class",
        "enum": [
          "economy",
          "business",
          "first"
        ],
        "type": "string"
      }
    },
    "type": "object"
  }
`,
			},
		},
		{
			name: "schema with min max",
			input: input{
				toolName:        "set_quantity",
				toolDescription: "Set item quantity",
				schema: schema.Object(map[string]*schema.Property{
					"quantity": schema.Integer("Number of items").Min(1).Max(100),
				}, "quantity"),
			},
			expected: expected{
				catalog: baseCatalogPrefix + `
- set_quantity: Set item quantity
  Parameters: {
    "properties": {
      "quantity": {
        "description": "Number of items",
        "maximum": 100,
        "minimum": 1,
        "type": "integer"
      }
    },
    "required": [
      "quantity"
    ],
    "type": "object"
  }
`,
			},
		},
		{
			name: "schema with default values",
			input: input{
				toolName:        "search",
				toolDescription: "Search for items",
				schema: schema.Object(map[string]*schema.Property{
					"query": schema.String("Search query"),
					"limit": schema.Integer("Maximum results").Default(10),
				}, "query"),
			},
			expected: expected{
				catalog: baseCatalogPrefix + `
- search: Search for items
  Parameters: {
    "properties": {
      "limit": {
        "default": 10,
        "description": "Maximum results",
        "type": "integer"
      },
      "query": {
        "description": "Search query",
        "type": "string"
      }
    },
    "required": [
      "query"
    ],
    "type": "object"
  }
`,
			},
		},
		{
			name: "schema with multiple types",
			input: input{
				toolName:        "complex_tool",
				toolDescription: "A tool with multiple types",
				schema: schema.Object(map[string]*schema.Property{
					"name":   schema.String("Name of the item"),
					"count":  schema.Integer("Number of items"),
					"price":  schema.Number("Price in dollars"),
					"active": schema.Boolean("Whether the item is active"),
					"tags":   schema.Array("List of tags", map[string]any{"type": "string"}),
				}, "name"),
			},
			expected: expected{
				catalog: baseCatalogPrefix + `
- complex_tool: A tool with multiple types
  Parameters: {
    "properties": {
      "active": {
        "description": "Whether the item is active",
        "type": "boolean"
      },
      "count": {
        "description": "Number of items",
        "type": "integer"
      },
      "name": {
        "description": "Name of the item",
        "type": "string"
      },
      "price": {
        "description": "Price in dollars",
        "type": "number"
      },
      "tags": {
        "description": "List of tags",
        "items": {
          "type": "string"
        },
        "type": "array"
      }
    },
    "required": [
      "name"
    ],
    "type": "object"
  }
`,
			},
		},
		{
			name: "tool with no schema",
			input: input{
				toolName:        "simple_tool",
				toolDescription: "A simple tool without schema",
				schema:          nil,
			},
			expected: expected{
				catalog: baseCatalogPrefix + `
- simple_tool: A simple tool without schema
`,
			},
		},
		{
			name: "schema with string constraints",
			input: input{
				toolName:        "validate_input",
				toolDescription: "Validate user input",
				schema: schema.Object(map[string]*schema.Property{
					"username": schema.String("Username").MinLength(3).MaxLength(20),
					"email":    schema.String("Email address").Format("email"),
					"code":     schema.String("Verification code").Pattern(`^[A-Z]{2}[0-9]{4}$`),
				}, "username", "email"),
			},
			expected: expected{
				catalog: baseCatalogPrefix + `
- validate_input: Validate user input
  Parameters: {
    "properties": {
      "code": {
        "description": "Verification code",
        "pattern": "^[A-Z]{2}[0-9]{4}$",
        "type": "string"
      },
      "email": {
        "description": "Email address",
        "format": "email",
        "type": "string"
      },
      "username": {
        "description": "Username",
        "maxLength": 20,
        "minLength": 3,
        "type": "string"
      }
    },
    "required": [
      "username",
      "email"
    ],
    "type": "object"
  }
`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewJSON()
			tool := gent.NewToolFunc(
				tt.input.toolName,
				tt.input.toolDescription,
				tt.input.schema,
				func(ctx context.Context, input map[string]any) (string, error) {
					return "ok", nil
				},
			)
			tc.RegisterTool(tool)

			catalog := tc.AvailableToolsPrompt()

			assert.Equal(t, tt.expected.catalog, catalog)
		})
	}
}

func TestJSON_AvailableToolsPrompt_MultipleTools(t *testing.T) {
	type mockTool struct {
		name        string
		description string
		schema      map[string]any
	}

	type input struct {
		tools []mockTool
	}

	type expected struct {
		catalog string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "two tools",
			input: input{
				tools: []mockTool{
					{
						name:        "search",
						description: "Search for information",
						schema: schema.Object(map[string]*schema.Property{
							"query": schema.String("Search query"),
						}, "query"),
					},
					{
						name:        "calculate",
						description: "Perform calculations",
						schema: schema.Object(map[string]*schema.Property{
							"expression": schema.String("Mathematical expression"),
						}, "expression"),
					},
				},
			},
			expected: expected{
				catalog: `Available tools:

- search: Search for information
  Parameters: {
    "properties": {
      "query": {
        "description": "Search query",
        "type": "string"
      }
    },
    "required": [
      "query"
    ],
    "type": "object"
  }

- calculate: Perform calculations
  Parameters: {
    "properties": {
      "expression": {
        "description": "Mathematical expression",
        "type": "string"
      }
    },
    "required": [
      "expression"
    ],
    "type": "object"
  }
`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewJSON()
			for _, mock := range tt.input.tools {
				tool := gent.NewToolFunc(
					mock.name,
					mock.description,
					mock.schema,
					func(ctx context.Context, input map[string]any) (string, error) {
						return "ok", nil
					},
				)
				tc.RegisterTool(tool)
			}

			catalog := tc.AvailableToolsPrompt()

			assert.Equal(t, tt.expected.catalog, catalog)
		})
	}
}

func TestJSON_Guidance_FormatInstructions(t *testing.T) {
	type expected struct {
		guidance string
	}

	tests := []struct {
		name     string
		expected expected
	}{
		{
			name: "format instructions present",
			expected: expected{
				guidance: `Call tools using JSON format:
{"tool": "tool_name", "args": {...}}

For multiple parallel calls, use an array:
[{"tool": "tool1", "args": {...}}, {"tool": "tool2", "args": {...}}]`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewJSON()
			tool := gent.NewToolFunc(
				"test",
				"Test tool",
				nil,
				func(ctx context.Context, input map[string]any) (string, error) {
					return "ok", nil
				},
			)
			tc.RegisterTool(tool)

			guidance := tc.Guidance()

			assert.Equal(t, tt.expected.guidance, guidance)
		})
	}
}

// getOutputString extracts a string from a raw output value.
func getOutputString(output any) string {
	if output == nil {
		return ""
	}
	if s, ok := output.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", output)
}

// -----------------------------------------------------------------------------
// BeforeToolCallSubscriber Argument Modification Tests
// -----------------------------------------------------------------------------

// jsonArgModifySubscriber is a test subscriber that modifies tool arguments.
// For tools using map[string]any as input, Args is typed as map[string]any.
type jsonArgModifySubscriber struct {
	modifyFunc func(args map[string]any) map[string]any
	called     bool
	seenArgs   map[string]any
}

func (h *jsonArgModifySubscriber) OnBeforeToolCall(
	execCtx *gent.ExecutionContext,
	event *gent.BeforeToolCallEvent,
) {
	h.called = true
	// Type-assert Args to map[string]any (the tool's input type)
	args, ok := event.Args.(map[string]any)
	if !ok {
		return
	}
	h.seenArgs = make(map[string]any)
	for k, v := range args {
		h.seenArgs[k] = v
	}
	if h.modifyFunc != nil {
		event.Args = h.modifyFunc(args)
	}
}

func TestJSON_Execute_BeforeToolCallHook_ModifyArgs(t *testing.T) {
	type input struct {
		content    string
		modifyFunc func(args map[string]any) map[string]any
	}

	type expected struct {
		hookCalled   bool
		originalArgs map[string]any
		toolReceived map[string]any
		result       string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "subscriber modifies args in place",
			input: input{
				content: `{"tool": "test", "args": {"value": "original"}}`,
				modifyFunc: func(args map[string]any) map[string]any {
					args["value"] = "modified"
					return args
				},
			},
			expected: expected{
				hookCalled:   true,
				originalArgs: map[string]any{"value": "original"},
				toolReceived: map[string]any{"value": "modified"},
				result:       `received: modified`,
			},
		},
		{
			name: "subscriber replaces entire args map",
			input: input{
				content: `{"tool": "test", "args": {"value": "original"}}`,
				modifyFunc: func(args map[string]any) map[string]any {
					return map[string]any{"value": "completely new"}
				},
			},
			expected: expected{
				hookCalled:   true,
				originalArgs: map[string]any{"value": "original"},
				toolReceived: map[string]any{"value": "completely new"},
				result:       `received: completely new`,
			},
		},
		{
			name: "subscriber adds new args",
			input: input{
				content: `{"tool": "test", "args": {"value": "original"}}`,
				modifyFunc: func(args map[string]any) map[string]any {
					args["extra"] = "added"
					return args
				},
			},
			expected: expected{
				hookCalled:   true,
				originalArgs: map[string]any{"value": "original"},
				toolReceived: map[string]any{"value": "original", "extra": "added"},
				result:       `received: original`,
			},
		},
		{
			name: "subscriber removes args",
			input: input{
				content: `{"tool": "test", "args": {"value": "original", "remove": "this"}}`,
				modifyFunc: func(args map[string]any) map[string]any {
					delete(args, "remove")
					return args
				},
			},
			expected: expected{
				hookCalled:   true,
				originalArgs: map[string]any{"value": "original", "remove": "this"},
				toolReceived: map[string]any{"value": "original"},
				result:       `received: original`,
			},
		},
		{
			name: "subscriber does not modify args",
			input: input{
				content:    `{"tool": "test", "args": {"value": "unchanged"}}`,
				modifyFunc: nil,
			},
			expected: expected{
				hookCalled:   true,
				originalArgs: map[string]any{"value": "unchanged"},
				toolReceived: map[string]any{"value": "unchanged"},
				result:       `received: unchanged`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Track what the tool actually received
			var receivedArgs map[string]any

			tc := NewJSON()
			tool := gent.NewToolFunc(
				"test",
				"A test tool",
				nil,
				func(ctx context.Context, args map[string]any) (string, error) {
					receivedArgs = args
					val, _ := args["value"].(string)
					return fmt.Sprintf("received: %s", val), nil
				},
			)
			tc.RegisterTool(tool)

			// Create subscriber
			sub := &jsonArgModifySubscriber{modifyFunc: tt.input.modifyFunc}

			// Create execution context with events
			execCtx := gent.NewExecutionContext(context.Background(), "test", nil)
			registry := &jsonTestRegistry{subscriber: sub}
			execCtx.SetEventPublisher(registry)
			execCtx.IncrementIteration()

			// Execute
			result, err := tc.Execute(execCtx, tt.input.content, testFormat())

			require.NoError(t, err)
			assert.True(t, sub.called, "subscriber should have been called")
			assert.Equal(t, tt.expected.originalArgs, sub.seenArgs,
				"subscriber should have seen original args")
			assert.Equal(t, tt.expected.toolReceived, receivedArgs,
				"tool should have received modified args")
			assert.NoError(t, result.Raw.Errors[0])
			assert.Equal(t, tt.expected.result, getOutputString(result.Raw.Results[0].Output))
		})
	}
}

func TestJSON_Execute_BeforeToolCallHook_MultipleTools(t *testing.T) {
	type input struct {
		content    string
		modifyFunc func(toolName string, args map[string]any) map[string]any
	}

	type expected struct {
		tool1Received map[string]any
		tool2Received map[string]any
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "subscriber modifies args for each tool independently",
			input: input{
				content: `[
					{"tool": "tool1", "args": {"value": "first"}},
					{"tool": "tool2", "args": {"value": "second"}}
				]`,
				modifyFunc: func(toolName string, args map[string]any) map[string]any {
					args["modified_by"] = toolName + "_hook"
					return args
				},
			},
			expected: expected{
				tool1Received: map[string]any{"value": "first", "modified_by": "tool1_hook"},
				tool2Received: map[string]any{"value": "second", "modified_by": "tool2_hook"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tool1Received, tool2Received map[string]any

			tc := NewJSON()
			tc.RegisterTool(gent.NewToolFunc(
				"tool1",
				"First tool",
				nil,
				func(ctx context.Context, args map[string]any) (string, error) {
					tool1Received = args
					return "ok1", nil
				},
			))
			tc.RegisterTool(gent.NewToolFunc(
				"tool2",
				"Second tool",
				nil,
				func(ctx context.Context, args map[string]any) (string, error) {
					tool2Received = args
					return "ok2", nil
				},
			))

			// Create subscriber that tracks tool name
			sub := &jsonMultiToolSubscriber{modifyFunc: tt.input.modifyFunc}

			execCtx := gent.NewExecutionContext(context.Background(), "test", nil)
			registry := &jsonTestRegistry{multiSubscriber: sub}
			execCtx.SetEventPublisher(registry)
			execCtx.IncrementIteration()

			result, err := tc.Execute(execCtx, tt.input.content, testFormat())

			require.NoError(t, err)
			assert.NoError(t, result.Raw.Errors[0])
			assert.NoError(t, result.Raw.Errors[1])
			assert.Equal(t, tt.expected.tool1Received, tool1Received)
			assert.Equal(t, tt.expected.tool2Received, tool2Received)
		})
	}
}

// jsonMultiToolSubscriber is a test subscriber that modifies args based on tool name.
// For tools using map[string]any as input, Args is typed as map[string]any.
type jsonMultiToolSubscriber struct {
	modifyFunc func(toolName string, args map[string]any) map[string]any
}

func (h *jsonMultiToolSubscriber) OnBeforeToolCall(
	execCtx *gent.ExecutionContext,
	event *gent.BeforeToolCallEvent,
) {
	if h.modifyFunc != nil {
		// Type-assert Args to map[string]any (the tool's input type)
		args, ok := event.Args.(map[string]any)
		if !ok {
			return
		}
		event.Args = h.modifyFunc(event.ToolName, args)
	}
}

// jsonTestRegistry implements EventPublisher for testing.
type jsonTestRegistry struct {
	subscriber      *jsonArgModifySubscriber
	multiSubscriber *jsonMultiToolSubscriber
}

func (r *jsonTestRegistry) Dispatch(execCtx *gent.ExecutionContext, event gent.Event) {
	switch e := event.(type) {
	case *gent.BeforeToolCallEvent:
		if r.subscriber != nil {
			r.subscriber.OnBeforeToolCall(execCtx, e)
		}
		if r.multiSubscriber != nil {
			r.multiSubscriber.OnBeforeToolCall(execCtx, e)
		}
	}
}

func (r *jsonTestRegistry) MaxRecursion() int {
	return 10
}

func TestJSON_ParseSection_TracesErrors(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected struct {
			shouldError          bool
			toolchainErrorTotal  int64
			toolchainErrorConsec float64
			toolchainErrorAtIter int64
		}
	}{
		{
			name:  "parse error publishes ParseErrorEvent",
			input: "{invalid json",
			expected: struct {
				shouldError          bool
				toolchainErrorTotal  int64
				toolchainErrorConsec float64
				toolchainErrorAtIter int64
			}{
				shouldError:          true,
				toolchainErrorTotal:  1,
				toolchainErrorConsec: 1,
				toolchainErrorAtIter: 1,
			},
		},
		{
			name:  "successful parse resets consecutive gauge",
			input: `{"tool": "test", "args": {"query": "hello"}}`,
			expected: struct {
				shouldError          bool
				toolchainErrorTotal  int64
				toolchainErrorConsec float64
				toolchainErrorAtIter int64
			}{
				shouldError:          false,
				toolchainErrorTotal:  0,
				toolchainErrorConsec: 0,
				toolchainErrorAtIter: 0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewJSON()

			// Create execution context with iteration 1
			execCtx := gent.NewExecutionContext(
				context.Background(), "test", nil,
			)
			execCtx.IncrementIteration()

			// If we expect success, first set consecutive to 1 to verify reset
			if !tt.expected.shouldError {
				execCtx.Stats().IncrGauge(
					gent.SGToolchainParseErrorConsecutive, 1,
				)
			}

			_, err := tc.ParseSection(execCtx, tt.input)

			if tt.expected.shouldError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			stats := execCtx.Stats()
			assert.Equal(t, tt.expected.toolchainErrorTotal,
				stats.GetCounter(gent.SCToolchainParseErrorTotal),
				"toolchain error total mismatch")
			assert.Equal(t, tt.expected.toolchainErrorConsec,
				stats.GetGauge(gent.SGToolchainParseErrorConsecutive),
				"toolchain error consecutive mismatch")
			assert.Equal(t, tt.expected.toolchainErrorAtIter,
				stats.GetCounter(gent.SCToolchainParseErrorAt+"1"),
				"toolchain error at iteration mismatch")
		})
	}
}

// instructionsTool is a test tool that returns Instructions in its result.
type instructionsTool struct {
	name         string
	description  string
	schema       map[string]any
	result       string
	instructions string
}

func (t *instructionsTool) Name() string                   { return t.name }
func (t *instructionsTool) Description() string            { return t.description }
func (t *instructionsTool) ParameterSchema() map[string]any { return t.schema }
func (t *instructionsTool) Call(
	ctx context.Context,
	input map[string]any,
) (*gent.ToolResult[string], error) {
	return &gent.ToolResult[string]{
		Text:         t.result,
		Instructions: t.instructions,
	}, nil
}

func TestJSON_Execute_TracesToolCallErrors(t *testing.T) {
	t.Run("tool error increments all error counters", func(t *testing.T) {
		tc := NewJSON()
		tool := gent.NewToolFunc(
			"failing_tool",
			"A test tool",
			nil,
			func(ctx context.Context, args map[string]any) (string, error) {
				return "", errors.New("tool execution failed")
			},
		)
		tc.RegisterTool(tool)

		execCtx := gent.NewExecutionContext(context.Background(), "test", nil)
		execCtx.IncrementIteration()

		_, err := tc.Execute(execCtx, `{"tool": "failing_tool", "args": {}}`, testFormat())
		require.NoError(t, err)

		stats := execCtx.Stats()
		assert.Equal(t, int64(1),
			stats.GetCounter(gent.SCToolCallsErrorTotal),
			"error total mismatch")
		assert.Equal(t, int64(1),
			stats.GetCounter(gent.SCToolCallsErrorFor+"failing_tool"),
			"error for tool mismatch")
		assert.Equal(t, float64(1),
			stats.GetGauge(gent.SGToolCallsErrorConsecutive),
			"error consecutive mismatch")
		assert.Equal(t, float64(1),
			stats.GetGauge(gent.SGToolCallsErrorConsecutiveFor+"failing_tool"),
			"error consecutive for tool mismatch")
	})

	t.Run("successful tool after failure resets consecutive gauges", func(t *testing.T) {
		tc := NewJSON()
		callCount := 0
		tool := gent.NewToolFunc(
			"test_tool",
			"A test tool",
			nil,
			func(ctx context.Context, args map[string]any) (string, error) {
				callCount++
				if callCount == 1 {
					return "", errors.New("first call fails")
				}
				return "success", nil
			},
		)
		tc.RegisterTool(tool)

		execCtx := gent.NewExecutionContext(context.Background(), "test", nil)

		// First iteration: tool fails
		execCtx.IncrementIteration()
		_, err := tc.Execute(execCtx, `{"tool": "test_tool", "args": {}}`, testFormat())
		require.NoError(t, err)

		stats := execCtx.Stats()
		assert.Equal(t, int64(1),
			stats.GetCounter(gent.SCToolCallsErrorTotal),
			"after first call: error total mismatch")
		assert.Equal(t, float64(1),
			stats.GetGauge(gent.SGToolCallsErrorConsecutive),
			"after first call: error consecutive mismatch")
		assert.Equal(t, float64(1),
			stats.GetGauge(gent.SGToolCallsErrorConsecutiveFor+"test_tool"),
			"after first call: error consecutive for tool mismatch")

		// Second iteration: tool succeeds
		execCtx.IncrementIteration()
		_, err = tc.Execute(
			execCtx,
			`{"tool": "test_tool", "args": {}}`,
			testFormat(),
		)
		require.NoError(t, err)

		// Total should remain 1, but consecutive should be reset to 0
		assert.Equal(t, int64(1),
			stats.GetCounter(gent.SCToolCallsErrorTotal),
			"after second call: error total should not change")
		assert.Equal(t, float64(0),
			stats.GetGauge(gent.SGToolCallsErrorConsecutive),
			"after second call: error consecutive should be reset")
		assert.Equal(t, float64(0),
			stats.GetGauge(gent.SGToolCallsErrorConsecutiveFor+"test_tool"),
			"after second call: error consecutive for tool should be reset")
	})

	t.Run("multiple consecutive failures accumulate", func(t *testing.T) {
		tc := NewJSON()
		tool := gent.NewToolFunc(
			"always_fails",
			"A test tool",
			nil,
			func(ctx context.Context, args map[string]any) (string, error) {
				return "", errors.New("always fails")
			},
		)
		tc.RegisterTool(tool)

		execCtx := gent.NewExecutionContext(context.Background(), "test", nil)

		// Three consecutive failures
		for i := 1; i <= 3; i++ {
			execCtx.IncrementIteration()
			_, err := tc.Execute(execCtx, `{"tool": "always_fails", "args": {}}`, testFormat())
			require.NoError(t, err)

			stats := execCtx.Stats()
			assert.Equal(t, int64(i),
				stats.GetCounter(gent.SCToolCallsErrorTotal),
				"after iteration %d: error total mismatch", i)
			assert.Equal(t, float64(i),
				stats.GetGauge(gent.SGToolCallsErrorConsecutive),
				"after iteration %d: error consecutive mismatch", i)
		}
	})
}

func TestJSON_Execute_TracesToolCallErrors_UnknownTool(t *testing.T) {
	type input struct {
		content string
	}

	type expected struct {
		errorTotal       int64
		errorFor         int64
		errorConsecutive float64
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "unknown tool increments error counters",
			input: input{
				content: `{"tool": "nonexistent", "args": {}}`,
			},
			expected: expected{
				errorTotal:       1,
				errorFor:         1,
				errorConsecutive: 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewJSON()
			// Don't register any tools

			execCtx := gent.NewExecutionContext(
				context.Background(), "test", nil,
			)
			execCtx.IncrementIteration()

			_, err := tc.Execute(
				execCtx, tt.input.content, testFormat(),
			)
			require.NoError(t, err)

			stats := execCtx.Stats()
			assert.Equal(t, tt.expected.errorTotal,
				stats.GetCounter(gent.SCToolCallsErrorTotal),
				"error total mismatch")
			assert.Equal(t, tt.expected.errorFor,
				stats.GetCounter(gent.SCToolCallsErrorFor+"nonexistent"),
				"error for tool mismatch")
			assert.Equal(t, tt.expected.errorConsecutive,
				stats.GetGauge(gent.SGToolCallsErrorConsecutive),
				"error consecutive mismatch")
		})
	}
}

func TestJSON_Execute_TracesToolCallErrors_SchemaValidation(t *testing.T) {
	type input struct {
		content string
	}

	type expected struct {
		errorTotal       int64
		errorFor         int64
		errorConsecutive float64
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "schema validation error increments error counters",
			input: input{
				content: `{"tool": "validated_tool", "args": {}}`,
			},
			expected: expected{
				errorTotal:       1,
				errorFor:         1,
				errorConsecutive: 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewJSON()
			tool := gent.NewToolFunc(
				"validated_tool",
				"A tool with required schema",
				map[string]any{
					"type": "object",
					"properties": map[string]any{
						"required_field": map[string]any{"type": "string"},
					},
					"required": []any{"required_field"},
				},
				func(ctx context.Context, args map[string]any) (string, error) {
					return "should not reach", nil
				},
			)
			tc.RegisterTool(tool)

			execCtx := gent.NewExecutionContext(
				context.Background(), "test", nil,
			)
			execCtx.IncrementIteration()

			_, err := tc.Execute(
				execCtx, tt.input.content, testFormat(),
			)
			require.NoError(t, err)

			stats := execCtx.Stats()
			assert.Equal(t, tt.expected.errorTotal,
				stats.GetCounter(gent.SCToolCallsErrorTotal),
				"error total mismatch")
			assert.Equal(t, tt.expected.errorFor,
				stats.GetCounter(gent.SCToolCallsErrorFor+"validated_tool"),
				"error for tool mismatch")
			assert.Equal(t, tt.expected.errorConsecutive,
				stats.GetGauge(gent.SGToolCallsErrorConsecutive),
				"error consecutive mismatch")
		})
	}
}

func TestJSON_Execute_TracesToolCallErrors_MultipleTools(t *testing.T) {
	t.Run("multiple tools with mixed success/failure in single execute", func(t *testing.T) {
		tc := NewJSON()
		tc.RegisterTool(gent.NewToolFunc(
			"success_tool",
			"A successful tool",
			nil,
			func(ctx context.Context, args map[string]any) (string, error) {
				return "success", nil
			},
		))
		tc.RegisterTool(gent.NewToolFunc(
			"failing_tool",
			"A failing tool",
			nil,
			func(ctx context.Context, args map[string]any) (string, error) {
				return "", errors.New("tool failed")
			},
		))

		execCtx := gent.NewExecutionContext(context.Background(), "test", nil)
		execCtx.IncrementIteration()

		content := `[
			{"tool": "success_tool", "args": {}},
			{"tool": "failing_tool", "args": {}}
		]`
		_, err := tc.Execute(execCtx, content, testFormat())
		require.NoError(t, err)

		stats := execCtx.Stats()
		assert.Equal(t, int64(1),
			stats.GetCounter(gent.SCToolCallsErrorTotal),
			"error total mismatch")
		assert.Equal(t, int64(0),
			stats.GetCounter(gent.SCToolCallsErrorFor+"success_tool"),
			"error for success_tool mismatch")
		assert.Equal(t, int64(1),
			stats.GetCounter(gent.SCToolCallsErrorFor+"failing_tool"),
			"error for failing_tool mismatch")
		assert.Equal(t, float64(1),
			stats.GetGauge(gent.SGToolCallsErrorConsecutive),
			"error consecutive mismatch")
		assert.Equal(t, float64(0),
			stats.GetGauge(gent.SGToolCallsErrorConsecutiveFor+"success_tool"),
			"error consecutive for success_tool mismatch")
		assert.Equal(t, float64(1),
			stats.GetGauge(gent.SGToolCallsErrorConsecutiveFor+"failing_tool"),
			"error consecutive for failing_tool mismatch")
	})

	t.Run("per-tool consecutive resets independently across executions", func(t *testing.T) {
		tc := NewJSON()
		tool1CallCount := 0
		tool2CallCount := 0
		tc.RegisterTool(gent.NewToolFunc(
			"tool1",
			"First tool",
			nil,
			func(ctx context.Context, args map[string]any) (string, error) {
				tool1CallCount++
				if tool1CallCount == 1 {
					return "", errors.New("tool1 fails first time")
				}
				return "tool1 success", nil
			},
		))
		tc.RegisterTool(gent.NewToolFunc(
			"tool2",
			"Second tool",
			nil,
			func(ctx context.Context, args map[string]any) (string, error) {
				tool2CallCount++
				// Tool2 always fails
				return "", errors.New("tool2 always fails")
			},
		))

		execCtx := gent.NewExecutionContext(context.Background(), "test", nil)

		// First iteration: both tools fail
		execCtx.IncrementIteration()
		_, err := tc.Execute(execCtx, `[
			{"tool": "tool1", "args": {}},
			{"tool": "tool2", "args": {}}
		]`, testFormat())
		require.NoError(t, err)

		stats := execCtx.Stats()
		assert.Equal(t, int64(2),
			stats.GetCounter(gent.SCToolCallsErrorTotal),
			"iteration 1: total errors")
		assert.Equal(t, float64(2),
			stats.GetGauge(gent.SGToolCallsErrorConsecutive),
			"iteration 1: consecutive errors")
		assert.Equal(t, float64(1),
			stats.GetGauge(gent.SGToolCallsErrorConsecutiveFor+"tool1"),
			"iteration 1: tool1 consecutive")
		assert.Equal(t, float64(1),
			stats.GetGauge(gent.SGToolCallsErrorConsecutiveFor+"tool2"),
			"iteration 1: tool2 consecutive")

		// Second iteration: tool1 succeeds, tool2 still fails
		execCtx.IncrementIteration()
		_, err = tc.Execute(execCtx, `[
			{"tool": "tool1", "args": {}},
			{"tool": "tool2", "args": {}}
		]`, testFormat())
		require.NoError(t, err)

		// Total increments by 1 (only tool2 fails)
		// Global consecutive resets then increments to 1
		// tool1 consecutive should be reset
		// tool2 consecutive should be 2
		assert.Equal(t, int64(3),
			stats.GetCounter(gent.SCToolCallsErrorTotal),
			"iteration 2: total errors")
		assert.Equal(t, float64(1),
			stats.GetGauge(gent.SGToolCallsErrorConsecutive),
			"iteration 2: consecutive errors (reset by tool1 success, then +1 by tool2)")
		assert.Equal(t, float64(0),
			stats.GetGauge(gent.SGToolCallsErrorConsecutiveFor+"tool1"),
			"iteration 2: tool1 consecutive reset")
		assert.Equal(t, float64(2),
			stats.GetGauge(gent.SGToolCallsErrorConsecutiveFor+"tool2"),
			"iteration 2: tool2 consecutive accumulated")
	})
}

func TestJSON_Execute_WithInstructions(t *testing.T) {
	type input struct {
		content      string
		result       string
		instructions string
	}

	type expected struct {
		text string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "tool without instructions has flat output",
			input: input{
				content:      `{"tool": "test", "args": {}}`,
				result:       "search result",
				instructions: "",
			},
			expected: expected{
				text: "<test>\n\"search result\"\n</test>",
			},
		},
		{
			name: "tool with instructions has nested sections",
			input: input{
				content:      `{"tool": "test", "args": {}}`,
				result:       "customer info",
				instructions: "Remember to verify customer ID before proceeding.",
			},
			expected: expected{
				text: "<test>\n<result>\n\"customer info\"\n</result>\n" +
					"<instructions>\nRemember to verify customer ID before proceeding.\n" +
					"</instructions>\n</test>",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewJSON()
			tool := &instructionsTool{
				name:         "test",
				description:  "A test tool",
				result:       tt.input.result,
				instructions: tt.input.instructions,
			}
			tc.RegisterTool(tool)

			result, err := tc.Execute(nil, tt.input.content, testFormat())

			require.NoError(t, err)
			assert.Equal(t, tt.expected.text, result.Text)
		})
	}
}
