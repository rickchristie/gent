package toolchain

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/schema"
	"github.com/tmc/langchaingo/llms"
)

func TestJSON_Name(t *testing.T) {
	tc := NewJSON()
	if tc.Name() != "action" {
		t.Errorf("expected default name 'action', got '%s'", tc.Name())
	}

	tc.WithSectionName("tools")
	if tc.Name() != "tools" {
		t.Errorf("expected name 'tools', got '%s'", tc.Name())
	}
}

func TestJSON_RegisterTool(t *testing.T) {
	tc := NewJSON()
	tool := gent.NewToolFunc[map[string]any, string](
		"test",
		"A test tool",
		nil,
		func(ctx context.Context, args map[string]any) (string, error) {
			return "result", nil
		},
		textFormatter,
	)

	tc.RegisterTool(tool)

	// Verify registration by executing the tool
	content := `{"tool": "test", "args": {}}`
	result, err := tc.Execute(context.Background(), nil, content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(result.Calls))
	}

	if result.Calls[0].Name != "test" {
		t.Errorf("expected tool name 'test', got '%s'", result.Calls[0].Name)
	}
}

func TestJSON_Prompt(t *testing.T) {
	tc := NewJSON()
	tool := gent.NewToolFunc[map[string]any, string](
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

	if !contains(prompt, "search") {
		t.Error("expected tool name in prompt")
	}
	if !contains(prompt, "Search the web") {
		t.Error("expected tool description in prompt")
	}
	if !contains(prompt, `"tool"`) {
		t.Error("expected JSON format instruction in prompt")
	}
}

func TestJSON_ParseSection_SingleCall(t *testing.T) {
	tc := NewJSON()

	content := `{"tool": "search", "args": {"query": "weather"}}`
	result, err := tc.ParseSection(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := result.([]*gent.ToolCall)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}

	if calls[0].Name != "search" {
		t.Errorf("expected tool name 'search', got '%s'", calls[0].Name)
	}

	if calls[0].Args["query"] != "weather" {
		t.Errorf("expected query 'weather', got '%v'", calls[0].Args["query"])
	}
}

func TestJSON_ParseSection_MultipleCall(t *testing.T) {
	tc := NewJSON()

	content := `[
		{"tool": "search", "args": {"query": "weather"}},
		{"tool": "calendar", "args": {"date": "today"}}
	]`

	result, err := tc.ParseSection(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := result.([]*gent.ToolCall)
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}

	if calls[0].Name != "search" {
		t.Errorf("expected first tool 'search', got '%s'", calls[0].Name)
	}

	if calls[1].Name != "calendar" {
		t.Errorf("expected second tool 'calendar', got '%s'", calls[1].Name)
	}
}

func TestJSON_ParseSection_EmptyContent(t *testing.T) {
	tc := NewJSON()

	result, err := tc.ParseSection("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := result.([]*gent.ToolCall)
	if len(calls) != 0 {
		t.Errorf("expected 0 calls for empty content, got %d", len(calls))
	}
}

func TestJSON_ParseSection_InvalidJSON(t *testing.T) {
	tc := NewJSON()

	content := `{invalid json}`
	_, err := tc.ParseSection(content)
	if !errors.Is(err, gent.ErrInvalidJSON) {
		t.Errorf("expected ErrInvalidJSON, got: %v", err)
	}
}

func TestJSON_ParseSection_MissingToolName(t *testing.T) {
	tc := NewJSON()

	content := `{"args": {"query": "weather"}}`
	_, err := tc.ParseSection(content)
	if !errors.Is(err, gent.ErrMissingToolName) {
		t.Errorf("expected ErrMissingToolName, got: %v", err)
	}
}

func TestJSON_ParseSection_MissingToolNameInArray(t *testing.T) {
	tc := NewJSON()

	content := `[{"tool": "search", "args": {}}, {"args": {}}]`
	_, err := tc.ParseSection(content)
	if !errors.Is(err, gent.ErrMissingToolName) {
		t.Errorf("expected ErrMissingToolName, got: %v", err)
	}
}

func TestJSON_Execute_Success(t *testing.T) {
	tc := NewJSON()
	tool := gent.NewToolFunc[map[string]any, string](
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
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(result.Calls))
	}

	if result.Results[0] == nil {
		t.Fatal("expected non-nil result")
	}

	if getTextContent(result.Results[0].Result) != "Results for: weather" {
		t.Errorf("unexpected result: %v", result.Results[0].Result)
	}

	if result.Errors[0] != nil {
		t.Errorf("unexpected error in result: %v", result.Errors[0])
	}
}

func TestJSON_Execute_UnknownTool(t *testing.T) {
	tc := NewJSON()

	content := `{"tool": "unknown", "args": {}}`
	result, err := tc.Execute(context.Background(), nil, content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !errors.Is(result.Errors[0], gent.ErrUnknownTool) {
		t.Errorf("expected ErrUnknownTool, got: %v", result.Errors[0])
	}
}

func TestJSON_Execute_ToolError(t *testing.T) {
	tc := NewJSON()
	tool := gent.NewToolFunc[map[string]any, string](
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
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Errors[0] == nil {
		t.Error("expected error in result")
	}

	if result.Errors[0].Error() != "tool execution failed" {
		t.Errorf("unexpected error message: %s", result.Errors[0].Error())
	}
}

func TestJSON_Execute_MultipleTools(t *testing.T) {
	tc := NewJSON()

	searchTool := gent.NewToolFunc[map[string]any, string](
		"search",
		"Search",
		nil,
		func(ctx context.Context, args map[string]any) (string, error) {
			return "search result", nil
		},
		textFormatter,
	)

	calendarTool := gent.NewToolFunc[map[string]any, string](
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
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result.Results))
	}

	if getTextContent(result.Results[0].Result) != "search result" {
		t.Errorf("unexpected first result: %v", result.Results[0].Result)
	}

	if getTextContent(result.Results[1].Result) != "calendar result" {
		t.Errorf("unexpected second result: %v", result.Results[1].Result)
	}
}

func TestJSON_Execute_SchemaValidation_Success(t *testing.T) {
	tc := NewJSON()

	// Tool with a schema requiring "query" string field
	tool := gent.NewToolFunc[map[string]any, string](
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
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Errors[0] != nil {
		t.Errorf("expected no error, got: %v", result.Errors[0])
	}

	if result.Results[0] == nil {
		t.Fatal("expected non-nil result")
	}

	if getTextContent(result.Results[0].Result) != "Results for: weather" {
		t.Errorf("unexpected result: %v", result.Results[0].Result)
	}
}

func TestJSON_Execute_SchemaValidation_MissingRequired(t *testing.T) {
	tc := NewJSON()

	// Tool with a schema requiring "query" string field
	tool := gent.NewToolFunc[map[string]any, string](
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

	// Missing required "query" field
	content := `{"tool": "search", "args": {}}`
	result, err := tc.Execute(context.Background(), nil, content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Errors[0] == nil {
		t.Fatal("expected validation error")
	}

	if !contains(result.Errors[0].Error(), "schema validation failed") {
		t.Errorf("expected schema validation error, got: %v", result.Errors[0])
	}

	// Tool should not have been called
	if result.Results[0] != nil {
		t.Errorf("expected nil result when validation fails, got: %v", result.Results[0])
	}
}

func TestJSON_Execute_SchemaValidation_WrongType(t *testing.T) {
	tc := NewJSON()

	// Tool with a schema requiring "count" integer field
	tool := gent.NewToolFunc[map[string]any, string](
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

	// Wrong type: "count" should be integer but we pass string
	content := `{"tool": "counter", "args": {"count": "not a number"}}`
	result, err := tc.Execute(context.Background(), nil, content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Errors[0] == nil {
		t.Fatal("expected validation error for wrong type")
	}

	if !contains(result.Errors[0].Error(), "schema validation failed") {
		t.Errorf("expected schema validation error, got: %v", result.Errors[0])
	}

	// Tool should not have been called
	if result.Results[0] != nil {
		t.Errorf("expected nil result when validation fails, got: %v", result.Results[0])
	}
}

func TestJSON_Execute_NoSchema_NoValidation(t *testing.T) {
	tc := NewJSON()

	// Tool without schema - should accept any args
	tool := gent.NewToolFunc[map[string]any, string](
		"flexible",
		"A flexible tool",
		nil, // No schema
		func(ctx context.Context, args map[string]any) (string, error) {
			return "success", nil
		},
		textFormatter,
	)
	tc.RegisterTool(tool)

	// Any args should work
	content := `{"tool": "flexible", "args": {"anything": "works", "numbers": 123}}`
	result, err := tc.Execute(context.Background(), nil, content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Errors[0] != nil {
		t.Errorf("expected no error for tool without schema, got: %v", result.Errors[0])
	}

	if getTextContent(result.Results[0].Result) != "success" {
		t.Errorf("unexpected result: %v", result.Results[0].Result)
	}
}

func TestJSON_Execute_SchemaValidation_MultipleProperties(t *testing.T) {
	tc := NewJSON()

	// Tool with multiple required properties
	tool := gent.NewToolFunc[map[string]any, string](
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

	// Valid args
	content := `{"tool": "booking", "args": {"origin": "NYC", "destination": "LAX", "passengers": 2}}`
	result, err := tc.Execute(context.Background(), nil, content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Errors[0] != nil {
		t.Errorf("expected no error, got: %v", result.Errors[0])
	}

	// Missing one required field
	content = `{"tool": "booking", "args": {"origin": "NYC", "destination": "LAX"}}`
	result, err = tc.Execute(context.Background(), nil, content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Errors[0] == nil {
		t.Fatal("expected validation error for missing required field")
	}
}

// TestJSON_ParseSection_TimeFormatsAsString tests that JSON always parses
// time-related values as strings (JSON doesn't have native date types).
func TestJSON_ParseSection_TimeFormatsAsString(t *testing.T) {
	tests := []struct {
		name     string
		jsonVal  string
		expected string
	}{
		{
			name:     "date only",
			jsonVal:  `"2026-01-20"`,
			expected: "2026-01-20",
		},
		{
			name:     "datetime ISO",
			jsonVal:  `"2026-01-20T10:30:00Z"`,
			expected: "2026-01-20T10:30:00Z",
		},
		{
			name:     "datetime with timezone",
			jsonVal:  `"2026-01-20T10:30:00+05:00"`,
			expected: "2026-01-20T10:30:00+05:00",
		},
		{
			name:     "time only",
			jsonVal:  `"10:30:00"`,
			expected: "10:30:00",
		},
		{
			name:     "duration string",
			jsonVal:  `"1h30m"`,
			expected: "1h30m",
		},
		{
			name:     "duration with seconds",
			jsonVal:  `"2h45m30s"`,
			expected: "2h45m30s",
		},
		{
			name:     "ISO 8601 duration",
			jsonVal:  `"PT1H30M"`,
			expected: "PT1H30M",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewJSON()

			tool := gent.NewToolFunc[map[string]any, string](
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

			content := fmt.Sprintf(`{"tool": "test_tool", "args": {"value": %s}}`, tt.jsonVal)

			result, err := tc.ParseSection(content)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			calls := result.([]*gent.ToolCall)
			val := calls[0].Args["value"]
			strVal, ok := val.(string)
			if !ok {
				t.Fatalf("expected string, got %T: %v", val, val)
			}

			if strVal != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, strVal)
			}

			// Verify schema validation passes
			execResult, err := tc.Execute(context.Background(), nil, content)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if execResult.Errors[0] != nil {
				t.Errorf("expected no validation error, got: %v", execResult.Errors[0])
			}
		})
	}
}

// TestJSON_ParseSection_DateAsString tests that date strings in JSON
// are correctly parsed as strings when schema expects string type.
func TestJSON_ParseSection_DateAsString(t *testing.T) {
	tc := NewJSON()

	// Tool with schema expecting "date" as a string
	tool := gent.NewToolFunc[map[string]any, string](
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

	content := `{"tool": "search_flights", "args": {"origin": "JFK", "destination": "LAX", "date": "2026-01-20"}}`

	result, err := tc.ParseSection(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := result.([]*gent.ToolCall)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}

	// The date should be a string
	dateVal := calls[0].Args["date"]
	dateStr, ok := dateVal.(string)
	if !ok {
		t.Fatalf("expected date to be string, got %T: %v", dateVal, dateVal)
	}

	if dateStr != "2026-01-20" {
		t.Errorf("expected date '2026-01-20', got '%s'", dateStr)
	}

	// Also test that schema validation passes
	execResult, err := tc.Execute(context.Background(), nil, content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if execResult.Errors[0] != nil {
		t.Errorf("expected no validation error, got: %v", execResult.Errors[0])
	}
}

// -----------------------------------------------------------------------------
// Integration tests for full toolchain flow with type conversions
// -----------------------------------------------------------------------------

// JSONTimeTypedInput is used to test automatic type conversion to time.Time
type JSONTimeTypedInput struct {
	Date      time.Time `json:"date"`
	Timestamp time.Time `json:"timestamp"`
}

// JSONDurationTypedInput is used to test automatic type conversion to time.Duration
type JSONDurationTypedInput struct {
	Duration time.Duration `json:"duration"`
}

// TestJSON_Execute_TimeConversion tests that string dates in JSON are converted
// to time.Time when the Go input struct expects time.Time.
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
			// Verify that both fields were converted to time.Time
			return input.Date.Format("2006-01-02") + "|" +
				input.Timestamp.Format(time.RFC3339), nil
		},
		textFormatter,
	)
	tc.RegisterTool(tool)

	// Date-only and RFC3339 timestamp
	content := `{"tool": "time_tool", "args": {"date": "2026-01-20", "timestamp": "2026-01-20T10:30:00Z"}}`

	result, err := tc.Execute(context.Background(), nil, content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Errors[0] != nil {
		t.Fatalf("unexpected tool error: %v", result.Errors[0])
	}

	expected := "2026-01-20|2026-01-20T10:30:00Z"
	output := getTextContent(result.Results[0].Result)
	if output != expected {
		t.Errorf("expected '%s', got '%s'", expected, output)
	}
}

// TestJSON_Execute_DurationConversion tests that duration strings in JSON are
// converted to time.Duration when the Go input struct expects time.Duration.
func TestJSON_Execute_DurationConversion(t *testing.T) {
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

	tests := []struct {
		name     string
		duration string
		expected string
	}{
		{"hours and minutes", "1h30m", "1h30m0s"},
		{"just minutes", "45m", "45m0s"},
		{"with seconds", "2h15m30s", "2h15m30s"},
		{"milliseconds", "500ms", "500ms"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := fmt.Sprintf(
				`{"tool": "duration_tool", "args": {"duration": "%s"}}`,
				tt.duration,
			)

			result, err := tc.Execute(context.Background(), nil, content)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result.Errors[0] != nil {
				t.Fatalf("unexpected tool error: %v", result.Errors[0])
			}

			output := getTextContent(result.Results[0].Result)
			if output != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, output)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Comprehensive Prompt() Tests
// -----------------------------------------------------------------------------

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

	// Verify tool name and description
	if !contains(prompt, "search_flights") {
		t.Error("expected tool name 'search_flights' in prompt")
	}
	if !contains(prompt, "Search for available flights") {
		t.Error("expected tool description in prompt")
	}

	// Verify field descriptions are present
	if !contains(prompt, "Origin airport code (IATA)") {
		t.Error("expected 'origin' field description in prompt")
	}
	if !contains(prompt, "Destination airport code (IATA)") {
		t.Error("expected 'destination' field description in prompt")
	}
	if !contains(prompt, "Departure date in YYYY-MM-DD format") {
		t.Error("expected 'date' field description in prompt")
	}

	// Verify type information (JSON format)
	if !contains(prompt, `"type": "string"`) {
		t.Error("expected type information in prompt")
	}
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
		}, "name", "email"), // name and email are required
		func(ctx context.Context, input map[string]any) (string, error) {
			return "ok", nil
		},
		nil,
	)
	tc.RegisterTool(tool)

	prompt := tc.Prompt()

	// Verify required fields are listed (JSON format)
	if !contains(prompt, `"required"`) {
		t.Error("expected 'required' section in prompt")
	}
	if !contains(prompt, `"name"`) {
		t.Error("expected 'name' in required list")
	}
	if !contains(prompt, `"email"`) {
		t.Error("expected 'email' in required list")
	}
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

	// Verify enum values are listed (JSON format)
	if !contains(prompt, `"enum"`) {
		t.Error("expected 'enum' section in prompt")
	}
	if !contains(prompt, `"economy"`) {
		t.Error("expected 'economy' in enum list")
	}
	if !contains(prompt, `"business"`) {
		t.Error("expected 'business' in enum list")
	}
	if !contains(prompt, `"first"`) {
		t.Error("expected 'first' in enum list")
	}
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

	// Verify min/max constraints (JSON format)
	if !contains(prompt, `"minimum": 1`) {
		t.Error("expected '\"minimum\": 1' in prompt")
	}
	if !contains(prompt, `"maximum": 100`) {
		t.Error("expected '\"maximum\": 100' in prompt")
	}
	if !contains(prompt, `"type": "integer"`) {
		t.Error("expected '\"type\": \"integer\"' in prompt")
	}
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

	// Verify default value (JSON format)
	if !contains(prompt, `"default": 10`) {
		t.Error("expected '\"default\": 10' in prompt")
	}
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

	// Verify all type information (JSON format)
	if !contains(prompt, `"type": "string"`) {
		t.Error("expected '\"type\": \"string\"' in prompt")
	}
	if !contains(prompt, `"type": "integer"`) {
		t.Error("expected '\"type\": \"integer\"' in prompt")
	}
	if !contains(prompt, `"type": "number"`) {
		t.Error("expected '\"type\": \"number\"' in prompt")
	}
	if !contains(prompt, `"type": "boolean"`) {
		t.Error("expected '\"type\": \"boolean\"' in prompt")
	}
	if !contains(prompt, `"type": "array"`) {
		t.Error("expected '\"type\": \"array\"' in prompt")
	}

	// Verify descriptions
	if !contains(prompt, "Name of the item") {
		t.Error("expected string description in prompt")
	}
	if !contains(prompt, "Number of items") {
		t.Error("expected integer description in prompt")
	}
	if !contains(prompt, "Price in dollars") {
		t.Error("expected number description in prompt")
	}
	if !contains(prompt, "Whether the item is active") {
		t.Error("expected boolean description in prompt")
	}
	if !contains(prompt, "List of tags") {
		t.Error("expected array description in prompt")
	}
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

	// Verify both tools are present
	if !contains(prompt, "- search:") {
		t.Error("expected 'search' tool in prompt")
	}
	if !contains(prompt, "- calculate:") {
		t.Error("expected 'calculate' tool in prompt")
	}
	if !contains(prompt, "Search for information") {
		t.Error("expected 'search' tool description in prompt")
	}
	if !contains(prompt, "Perform calculations") {
		t.Error("expected 'calculate' tool description in prompt")
	}
	if !contains(prompt, "Search query") {
		t.Error("expected 'query' field description in prompt")
	}
	if !contains(prompt, "Mathematical expression") {
		t.Error("expected 'expression' field description in prompt")
	}
}

func TestJSON_Prompt_ToolWithNoSchema(t *testing.T) {
	tc := NewJSON()
	tool := gent.NewToolFunc(
		"simple_tool",
		"A simple tool without schema",
		nil, // No schema
		func(ctx context.Context, input map[string]any) (string, error) {
			return "ok", nil
		},
		nil,
	)
	tc.RegisterTool(tool)

	prompt := tc.Prompt()

	// Verify tool name and description are present
	if !contains(prompt, "simple_tool") {
		t.Error("expected tool name 'simple_tool' in prompt")
	}
	if !contains(prompt, "A simple tool without schema") {
		t.Error("expected tool description in prompt")
	}
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

	// Verify string constraints (JSON format)
	if !contains(prompt, `"minLength": 3`) {
		t.Error("expected '\"minLength\": 3' in prompt")
	}
	if !contains(prompt, `"maxLength": 20`) {
		t.Error("expected '\"maxLength\": 20' in prompt")
	}
	if !contains(prompt, `"format": "email"`) {
		t.Error("expected '\"format\": \"email\"' in prompt")
	}
	if !contains(prompt, `"pattern"`) {
		t.Error("expected 'pattern' in prompt")
	}
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

	// Verify JSON format instructions
	if !contains(prompt, `{"tool": "tool_name", "args": {...}}`) {
		t.Error("expected single tool format instruction")
	}
	if !contains(prompt, `[{"tool": "tool1", "args": {...}}, {"tool": "tool2", "args": {...}}]`) {
		t.Error("expected array format instruction for parallel calls")
	}
	if !contains(prompt, "Available tools:") {
		t.Error("expected 'Available tools:' header")
	}
}

// -----------------------------------------------------------------------------
// Helper functions
// -----------------------------------------------------------------------------

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
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
