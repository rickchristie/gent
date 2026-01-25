package toolchain

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
)

func TestYAML_Name(t *testing.T) {
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
			tc := NewYAML()
			if tt.input.customName != "" {
				tc.WithSectionName(tt.input.customName)
			}

			assert.Equal(t, tt.expected.name, tc.Name())
		})
	}
}

func TestYAML_RegisterTool(t *testing.T) {
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
				content: `tool: test
args: {}`,
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
			tc := NewYAML()
			tool := gent.NewToolFunc(
				tt.input.toolName,
				"A test tool",
				nil,
				func(ctx context.Context, args map[string]any) (string, error) {
					return "result", nil
				},
				yamlTextFormatter,
			)

			tc.RegisterTool(tool)

			result, err := tc.Execute(nil, tt.input.content)

			if tt.expected.err != nil {
				assert.ErrorIs(t, err, tt.expected.err)
			} else {
				require.NoError(t, err)
				assert.Len(t, result.Calls, tt.expected.callCount)
				assert.Equal(t, tt.expected.callName, result.Calls[0].Name)
			}
		})
	}
}

func TestYAML_Prompt(t *testing.T) {
	type input struct {
		toolName        string
		toolDescription string
		toolSchema      map[string]any
	}

	type expected struct {
		prompt string
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
				prompt: `Call tools using YAML format:
tool: tool_name
args:
  param: value

For multiple parallel calls, use a list:
- tool: tool1
  args:
    param: value
- tool: tool2
  args:
    param: value

For strings with special characters (colons, quotes) or multiple lines, use double quotes:
- tool: send_email
  args:
    subject: "Unsubscribe Confirmation: Newsletter"
    body: "You have been unsubscribed.\n\nYou will no longer receive emails from us."

Available tools:

- search: Search the web
  Parameters:
    properties:
        query:
            type: string
    type: object
`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewYAML()
			tool := gent.NewToolFunc(
				tt.input.toolName,
				tt.input.toolDescription,
				tt.input.toolSchema,
				func(ctx context.Context, args map[string]any) (string, error) {
					return "", nil
				},
				nil,
			)
			tc.RegisterTool(tool)

			prompt := tc.Prompt()

			assert.Equal(t, tt.expected.prompt, prompt)
		})
	}
}

func TestYAML_ParseSection(t *testing.T) {
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
				content: `tool: search
args:
  query: weather`,
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
				content: `- tool: search
  args:
    query: weather
- tool: calendar
  args:
    date: today`,
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
			name: "invalid YAML",
			input: input{
				content: `tool: search
args:
    query: test
  invalid: indentation`,
			},
			expected: expected{
				calls: nil,
				err:   gent.ErrInvalidYAML,
			},
		},
		{
			name: "missing tool name",
			input: input{
				content: `args:
  query: weather`,
			},
			expected: expected{
				calls: nil,
				err:   gent.ErrMissingToolName,
			},
		},
		{
			name: "missing tool name in array",
			input: input{
				content: `- tool: search
  args: {}
- args: {}`,
			},
			expected: expected{
				calls: nil,
				err:   gent.ErrMissingToolName,
			},
		},
		{
			name: "multiline string args",
			input: input{
				content: `tool: write
args:
  content: |
    This is a multi-line
    string argument that
    spans multiple lines.`,
			},
			expected: expected{
				calls: []*gent.ToolCall{
					{
						Name: "write",
						Args: map[string]any{
							"content": "This is a multi-line\nstring argument that\nspans multiple lines.",
						},
					},
				},
				err: nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewYAML()

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

func TestYAML_Execute(t *testing.T) {
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
				content: `tool: search
args:
  query: weather`,
			},
			expected: expected{
				callCount:   1,
				results:     []string{"Results for: weather"},
				errors:      []error{nil},
				resultNames: []string{"search"},
			},
		},
		{
			name:  "unknown tool",
			mocks: []mockTool{},
			input: input{
				content: `tool: unknown
args: {}`,
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
				content: `tool: failing
args: {}`,
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
				content: `- tool: search
  args: {}
- tool: calendar
  args: {}`,
			},
			expected: expected{
				callCount:   2,
				results:     []string{"search result", "calendar result"},
				errors:      []error{nil, nil},
				resultNames: []string{"search", "calendar"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewYAML()
			for _, mock := range tt.mocks {
				tool := gent.NewToolFunc(
					mock.name,
					mock.description,
					mock.schema,
					mock.fn,
					yamlTextFormatter,
				)
				tc.RegisterTool(tool)
			}

			result, err := tc.Execute(nil, tt.input.content)

			if tt.expected.executeErr != nil {
				assert.ErrorIs(t, err, tt.expected.executeErr)
				return
			}

			require.NoError(t, err)
			assert.Len(t, result.Calls, tt.expected.callCount)

			for i, expectedResult := range tt.expected.results {
				if tt.expected.errors[i] != nil {
					assert.Error(t, result.Errors[i])
					if errors.Is(tt.expected.errors[i], gent.ErrUnknownTool) {
						assert.ErrorIs(t, result.Errors[i], gent.ErrUnknownTool)
					} else {
						assert.Equal(t, tt.expected.errors[i].Error(), result.Errors[i].Error())
					}
				} else {
					assert.NoError(t, result.Errors[i])
					assert.Equal(t, expectedResult, yamlGetTextContent(result.Results[i].Result))
					if len(tt.expected.resultNames) > i {
						assert.Equal(t, tt.expected.resultNames[i], result.Results[i].Name)
					}
				}
			}
		})
	}
}

func TestYAML_Execute_SchemaValidation(t *testing.T) {
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
		result       string
		errContains  string
		resultIsNil  bool
		noToolError  bool
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
				content: `tool: search
args:
  query: weather`,
			},
			expected: expected{
				result:      "Results for: weather",
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
				content: `tool: search
args: {}`,
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
				content: `tool: counter
args:
  count: not a number`,
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
				content: `tool: flexible
args:
  anything: works
  numbers: 123`,
			},
			expected: expected{
				result:      "success",
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
				content: `tool: booking
args:
  origin: NYC
  destination: LAX
  passengers: 2`,
			},
			expected: expected{
				result:      "booked",
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
				content: `tool: booking
args:
  origin: NYC
  destination: LAX`,
			},
			expected: expected{
				errContains: "schema validation failed",
				resultIsNil: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewYAML()
			for _, mock := range tt.mocks {
				tool := gent.NewToolFunc(
					mock.name,
					mock.description,
					mock.schema,
					mock.fn,
					yamlTextFormatter,
				)
				tc.RegisterTool(tool)
			}

			result, err := tc.Execute(nil, tt.input.content)
			require.NoError(t, err)

			if tt.expected.noToolError {
				assert.NoError(t, result.Errors[0])
				require.NotNil(t, result.Results[0])
				assert.Equal(t, tt.expected.result, yamlGetTextContent(result.Results[0].Result))
			} else {
				require.Error(t, result.Errors[0])
				assert.Contains(t, result.Errors[0].Error(), tt.expected.errContains)
				if tt.expected.resultIsNil {
					assert.Nil(t, result.Results[0])
				}
			}
		})
	}
}

func TestYAML_ParseSection_DateAsString(t *testing.T) {
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
				content: `tool: search_flights
args:
  origin: JFK
  destination: LAX
  date: 2026-01-20`,
			},
			expected: expected{
				dateValue: "2026-01-20",
				execErr:   nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewYAML()

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
				yamlTextFormatter,
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

			execResult, err := tc.Execute(nil, tt.input.content)
			require.NoError(t, err)
			assert.NoError(t, execResult.Errors[0])
		})
	}
}

func TestYAML_ParseSection_TimeFormatsAsString(t *testing.T) {
	type input struct {
		yamlVal string
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
			input:    input{yamlVal: "2026-01-20"},
			expected: expected{value: "2026-01-20"},
		},
		{
			name:     "datetime with T",
			input:    input{yamlVal: "2026-01-20T10:30:00Z"},
			expected: expected{value: "2026-01-20T10:30:00Z"},
		},
		{
			name:     "datetime with space",
			input:    input{yamlVal: "2026-01-20 10:30:00"},
			expected: expected{value: "2026-01-20 10:30:00"},
		},
		{
			name:     "time only",
			input:    input{yamlVal: "10:30:00"},
			expected: expected{value: "10:30:00"},
		},
		{
			name:     "duration string",
			input:    input{yamlVal: "1h30m"},
			expected: expected{value: "1h30m"},
		},
		{
			name:     "duration with seconds",
			input:    input{yamlVal: "2h45m30s"},
			expected: expected{value: "2h45m30s"},
		},
		{
			name:     "ISO 8601 duration",
			input:    input{yamlVal: "PT1H30M"},
			expected: expected{value: "PT1H30M"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewYAML()

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
				yamlTextFormatter,
			)
			tc.RegisterTool(tool)

			content := fmt.Sprintf(`tool: test_tool
args:
  value: %s`, tt.input.yamlVal)

			result, err := tc.ParseSection(nil, content)
			require.NoError(t, err)

			calls := result.([]*gent.ToolCall)
			val := calls[0].Args["value"]
			strVal, ok := val.(string)
			require.True(t, ok, "expected string, got %T: %v", val, val)
			assert.Equal(t, tt.expected.value, strVal)

			execResult, err := tc.Execute(nil, content)
			require.NoError(t, err)
			assert.NoError(t, execResult.Errors[0])
		})
	}
}

func TestYAML_ParseSection_NoSchemaLetsYAMLDecide(t *testing.T) {
	type input struct {
		content string
	}

	type expected struct {
		acceptableTypes []string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "date without schema can be time.Time or string",
			input: input{
				content: `tool: untyped_tool
args:
  date: 2026-01-20`,
			},
			expected: expected{
				acceptableTypes: []string{"time.Time", "string"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewYAML()

			tool := gent.NewToolFunc(
				"untyped_tool",
				"Tool without schema",
				nil,
				func(ctx context.Context, args map[string]any) (string, error) {
					return "ok", nil
				},
				yamlTextFormatter,
			)
			tc.RegisterTool(tool)

			result, err := tc.ParseSection(nil, tt.input.content)
			require.NoError(t, err)

			calls := result.([]*gent.ToolCall)
			val := calls[0].Args["date"]

			_, isTime := val.(time.Time)
			_, isString := val.(string)

			assert.True(t, isTime || isString,
				"expected one of %v, got %T", tt.expected.acceptableTypes, val)
		})
	}
}

// TimeTypedInput is used to test automatic type conversion to time.Time
type TimeTypedInput struct {
	Date      time.Time `json:"date"`
	Timestamp time.Time `json:"timestamp"`
}

// DurationTypedInput is used to test automatic type conversion to time.Duration
type DurationTypedInput struct {
	Duration time.Duration `json:"duration"`
}

func TestYAML_Execute_TimeConversion(t *testing.T) {
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
				content: `tool: time_tool
args:
  date: 2026-01-20
  timestamp: 2026-01-20T10:30:00Z`,
			},
			expected: expected{
				output: "2026-01-20|2026-01-20T10:30:00Z",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewYAML()

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
				func(ctx context.Context, input TimeTypedInput) (string, error) {
					return input.Date.Format("2006-01-02") + "|" +
						input.Timestamp.Format(time.RFC3339), nil
				},
				yamlTextFormatter,
			)
			tc.RegisterTool(tool)

			result, err := tc.Execute(nil, tt.input.content)
			require.NoError(t, err)
			require.NoError(t, result.Errors[0])

			output := yamlGetTextContent(result.Results[0].Result)
			assert.Equal(t, tt.expected.output, output)
		})
	}
}

func TestYAML_Execute_DurationConversion(t *testing.T) {
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
			tc := NewYAML()

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
				func(ctx context.Context, input DurationTypedInput) (string, error) {
					return input.Duration.String(), nil
				},
				yamlTextFormatter,
			)
			tc.RegisterTool(tool)

			content := fmt.Sprintf(`tool: duration_tool
args:
  duration: %s`, tt.input.duration)

			result, err := tc.Execute(nil, content)
			require.NoError(t, err)
			require.NoError(t, result.Errors[0])

			output := yamlGetTextContent(result.Results[0].Result)
			assert.Equal(t, tt.expected.output, output)
		})
	}
}

func TestYAML_Prompt_SchemaFeatures(t *testing.T) {
	type input struct {
		toolName        string
		toolDescription string
		schema          map[string]any
	}

	type expected struct {
		prompt string
	}

	basePromptPrefix := `Call tools using YAML format:
tool: tool_name
args:
  param: value

For multiple parallel calls, use a list:
- tool: tool1
  args:
    param: value
- tool: tool2
  args:
    param: value

For strings with special characters (colons, quotes) or multiple lines, use double quotes:
- tool: send_email
  args:
    subject: "Unsubscribe Confirmation: Newsletter"
    body: "You have been unsubscribed.\n\nYou will no longer receive emails from us."

Available tools:
`

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
				prompt: basePromptPrefix + `
- search_flights: Search for available flights
  Parameters:
    properties:
        date:
            description: Departure date in YYYY-MM-DD format
            type: string
        destination:
            description: Destination airport code (IATA)
            type: string
        origin:
            description: Origin airport code (IATA)
            type: string
    required:
        - origin
        - destination
        - date
    type: object
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
				prompt: basePromptPrefix + `
- create_user: Create a new user
  Parameters:
    properties:
        email:
            description: User's email address
            type: string
        name:
            description: User's full name
            type: string
        phone:
            description: User's phone number
            type: string
    required:
        - name
        - email
    type: object
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
				prompt: basePromptPrefix + `
- book_flight: Book a flight
  Parameters:
    properties:
        class:
            description: Travel class
            enum:
                - economy
                - business
                - first
            type: string
    type: object
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
				prompt: basePromptPrefix + `
- set_quantity: Set item quantity
  Parameters:
    properties:
        quantity:
            description: Number of items
            maximum: 100
            minimum: 1
            type: integer
    required:
        - quantity
    type: object
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
				prompt: basePromptPrefix + `
- search: Search for items
  Parameters:
    properties:
        limit:
            default: 10
            description: Maximum results
            type: integer
        query:
            description: Search query
            type: string
    required:
        - query
    type: object
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
				prompt: basePromptPrefix + `
- complex_tool: A tool with multiple types
  Parameters:
    properties:
        active:
            description: Whether the item is active
            type: boolean
        count:
            description: Number of items
            type: integer
        name:
            description: Name of the item
            type: string
        price:
            description: Price in dollars
            type: number
        tags:
            description: List of tags
            items:
                type: string
            type: array
    required:
        - name
    type: object
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
				prompt: basePromptPrefix + `
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
				prompt: basePromptPrefix + `
- validate_input: Validate user input
  Parameters:
    properties:
        code:
            description: Verification code
            pattern: ^[A-Z]{2}[0-9]{4}$
            type: string
        email:
            description: Email address
            format: email
            type: string
        username:
            description: Username
            maxLength: 20
            minLength: 3
            type: string
    required:
        - username
        - email
    type: object
`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewYAML()
			tool := gent.NewToolFunc(
				tt.input.toolName,
				tt.input.toolDescription,
				tt.input.schema,
				func(ctx context.Context, input map[string]any) (string, error) {
					return "ok", nil
				},
				nil,
			)
			tc.RegisterTool(tool)

			prompt := tc.Prompt()

			assert.Equal(t, tt.expected.prompt, prompt)
		})
	}
}

func TestYAML_Prompt_MultipleTools(t *testing.T) {
	type mockTool struct {
		name        string
		description string
		schema      map[string]any
	}

	type input struct {
		tools []mockTool
	}

	type expected struct {
		prompt string
	}

	basePromptPrefix := `Call tools using YAML format:
tool: tool_name
args:
  param: value

For multiple parallel calls, use a list:
- tool: tool1
  args:
    param: value
- tool: tool2
  args:
    param: value

For strings with special characters (colons, quotes) or multiple lines, use double quotes:
- tool: send_email
  args:
    subject: "Unsubscribe Confirmation: Newsletter"
    body: "You have been unsubscribed.\n\nYou will no longer receive emails from us."

Available tools:
`

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
				prompt: basePromptPrefix + `
- search: Search for information
  Parameters:
    properties:
        query:
            description: Search query
            type: string
    required:
        - query
    type: object

- calculate: Perform calculations
  Parameters:
    properties:
        expression:
            description: Mathematical expression
            type: string
    required:
        - expression
    type: object
`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewYAML()
			for _, mock := range tt.input.tools {
				tool := gent.NewToolFunc(
					mock.name,
					mock.description,
					mock.schema,
					func(ctx context.Context, input map[string]any) (string, error) {
						return "ok", nil
					},
					nil,
				)
				tc.RegisterTool(tool)
			}

			prompt := tc.Prompt()

			assert.Equal(t, tt.expected.prompt, prompt)
		})
	}
}

func TestYAML_Prompt_FormatInstructions(t *testing.T) {
	type expected struct {
		prompt string
	}

	tests := []struct {
		name     string
		expected expected
	}{
		{
			name: "format instructions present",
			expected: expected{
				prompt: `Call tools using YAML format:
tool: tool_name
args:
  param: value

For multiple parallel calls, use a list:
- tool: tool1
  args:
    param: value
- tool: tool2
  args:
    param: value

For strings with special characters (colons, quotes) or multiple lines, use double quotes:
- tool: send_email
  args:
    subject: "Unsubscribe Confirmation: Newsletter"
    body: "You have been unsubscribed.\n\nYou will no longer receive emails from us."

Available tools:

- test: Test tool
`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewYAML()
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

			assert.Equal(t, tt.expected.prompt, prompt)
		})
	}
}

// yamlTextFormatter converts a string to []gent.ContentPart.
func yamlTextFormatter(s string) []gent.ContentPart {
	return []gent.ContentPart{llms.TextContent{Text: s}}
}

// yamlGetTextContent extracts the text from a []gent.ContentPart.
func yamlGetTextContent(parts []gent.ContentPart) string {
	if len(parts) == 0 {
		return ""
	}
	if tc, ok := parts[0].(llms.TextContent); ok {
		return tc.Text
	}
	return ""
}

func TestYAML_ParseSection_TracesErrors(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected struct {
			shouldError              bool
			toolchainErrorTotal      int64
			toolchainErrorConsec     int64
			toolchainErrorAtIter     int64
		}
	}{
		{
			name:  "parse error traces ParseErrorTrace",
			input: "invalid: yaml: [",
			expected: struct {
				shouldError              bool
				toolchainErrorTotal      int64
				toolchainErrorConsec     int64
				toolchainErrorAtIter     int64
			}{
				shouldError:          true,
				toolchainErrorTotal:  1,
				toolchainErrorConsec: 1,
				toolchainErrorAtIter: 1,
			},
		},
		{
			name:  "successful parse resets consecutive counter",
			input: "tool: test\nargs:\n  query: hello",
			expected: struct {
				shouldError              bool
				toolchainErrorTotal      int64
				toolchainErrorConsec     int64
				toolchainErrorAtIter     int64
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
			tc := NewYAML()

			// Create execution context with iteration 1
			execCtx := gent.NewExecutionContext(context.Background(), "test", nil)
			execCtx.StartIteration()

			// If we expect success, first set consecutive to 1 to verify reset
			if !tt.expected.shouldError {
				execCtx.Stats().IncrCounter(gent.KeyToolchainParseErrorConsecutive, 1)
			}

			_, err := tc.ParseSection(execCtx, tt.input)

			if tt.expected.shouldError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			stats := execCtx.Stats()
			assert.Equal(t, tt.expected.toolchainErrorTotal,
				stats.GetCounter(gent.KeyToolchainParseErrorTotal),
				"toolchain error total mismatch")
			assert.Equal(t, tt.expected.toolchainErrorConsec,
				stats.GetCounter(gent.KeyToolchainParseErrorConsecutive),
				"toolchain error consecutive mismatch")
			assert.Equal(t, tt.expected.toolchainErrorAtIter,
				stats.GetCounter(gent.KeyToolchainParseErrorAt+"1"),
				"toolchain error at iteration mismatch")
		})
	}
}
