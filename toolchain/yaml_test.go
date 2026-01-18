package toolchain

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/tmc/langchaingo/llms"
)

func TestYAML_Name(t *testing.T) {
	tc := NewYAML()
	if tc.Name() != "action" {
		t.Errorf("expected default name 'action', got '%s'", tc.Name())
	}

	tc.WithSectionName("tools")
	if tc.Name() != "tools" {
		t.Errorf("expected name 'tools', got '%s'", tc.Name())
	}
}

func TestYAML_RegisterTool(t *testing.T) {
	tc := NewYAML()
	tool := gent.NewToolFunc[map[string]any, string](
		"test",
		"A test tool",
		nil,
		func(ctx context.Context, args map[string]any) (string, error) {
			return "result", nil
		},
		yamlTextFormatter,
	)

	tc.RegisterTool(tool)

	// Verify registration by executing the tool
	content := `tool: test
args: {}`
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

func TestYAML_Prompt(t *testing.T) {
	tc := NewYAML()
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

	if !containsYAML(prompt, "search") {
		t.Error("expected tool name in prompt")
	}
	if !containsYAML(prompt, "Search the web") {
		t.Error("expected tool description in prompt")
	}
	if !containsYAML(prompt, "tool:") {
		t.Error("expected YAML format instruction in prompt")
	}
}

func TestYAML_ParseSection_SingleCall(t *testing.T) {
	tc := NewYAML()

	content := `tool: search
args:
  query: weather`

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

func TestYAML_ParseSection_MultipleCall(t *testing.T) {
	tc := NewYAML()

	content := `- tool: search
  args:
    query: weather
- tool: calendar
  args:
    date: today`

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

func TestYAML_ParseSection_EmptyContent(t *testing.T) {
	tc := NewYAML()

	result, err := tc.ParseSection("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := result.([]*gent.ToolCall)
	if len(calls) != 0 {
		t.Errorf("expected 0 calls for empty content, got %d", len(calls))
	}
}

func TestYAML_ParseSection_InvalidYAML(t *testing.T) {
	tc := NewYAML()

	// Use truly invalid YAML with mixed tabs and spaces in an invalid way
	content := `tool: search
args:
    query: test
  invalid: indentation`
	_, err := tc.ParseSection(content)
	if !errors.Is(err, gent.ErrInvalidYAML) {
		t.Errorf("expected ErrInvalidYAML, got: %v", err)
	}
}

func TestYAML_ParseSection_MissingToolName(t *testing.T) {
	tc := NewYAML()

	content := `args:
  query: weather`
	_, err := tc.ParseSection(content)
	if !errors.Is(err, gent.ErrMissingToolName) {
		t.Errorf("expected ErrMissingToolName, got: %v", err)
	}
}

func TestYAML_ParseSection_MissingToolNameInArray(t *testing.T) {
	tc := NewYAML()

	content := `- tool: search
  args: {}
- args: {}`
	_, err := tc.ParseSection(content)
	if !errors.Is(err, gent.ErrMissingToolName) {
		t.Errorf("expected ErrMissingToolName, got: %v", err)
	}
}

func TestYAML_Execute_Success(t *testing.T) {
	tc := NewYAML()
	tool := gent.NewToolFunc[map[string]any, string](
		"search",
		"Search the web",
		nil,
		func(ctx context.Context, args map[string]any) (string, error) {
			query := args["query"].(string)
			return fmt.Sprintf("Results for: %s", query), nil
		},
		yamlTextFormatter,
	)
	tc.RegisterTool(tool)

	content := `tool: search
args:
  query: weather`

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

	if yamlGetTextContent(result.Results[0].Result) != "Results for: weather" {
		t.Errorf("unexpected result: %v", result.Results[0].Result)
	}

	if result.Errors[0] != nil {
		t.Errorf("unexpected error in result: %v", result.Errors[0])
	}
}

func TestYAML_Execute_UnknownTool(t *testing.T) {
	tc := NewYAML()

	content := `tool: unknown
args: {}`

	result, err := tc.Execute(context.Background(), nil, content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !errors.Is(result.Errors[0], gent.ErrUnknownTool) {
		t.Errorf("expected ErrUnknownTool, got: %v", result.Errors[0])
	}
}

func TestYAML_Execute_ToolError(t *testing.T) {
	tc := NewYAML()
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

	content := `tool: failing
args: {}`

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

func TestYAML_Execute_MultipleTools(t *testing.T) {
	tc := NewYAML()

	searchTool := gent.NewToolFunc[map[string]any, string](
		"search",
		"Search",
		nil,
		func(ctx context.Context, args map[string]any) (string, error) {
			return "search result", nil
		},
		yamlTextFormatter,
	)

	calendarTool := gent.NewToolFunc[map[string]any, string](
		"calendar",
		"Calendar",
		nil,
		func(ctx context.Context, args map[string]any) (string, error) {
			return "calendar result", nil
		},
		yamlTextFormatter,
	)

	tc.RegisterTool(searchTool)
	tc.RegisterTool(calendarTool)

	content := `- tool: search
  args: {}
- tool: calendar
  args: {}`

	result, err := tc.Execute(context.Background(), nil, content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result.Results))
	}

	if yamlGetTextContent(result.Results[0].Result) != "search result" {
		t.Errorf("unexpected first result: %v", result.Results[0].Result)
	}

	if yamlGetTextContent(result.Results[1].Result) != "calendar result" {
		t.Errorf("unexpected second result: %v", result.Results[1].Result)
	}
}

func TestYAML_ParseSection_MultilineStringArgs(t *testing.T) {
	tc := NewYAML()

	content := `tool: write
args:
  content: |
    This is a multi-line
    string argument that
    spans multiple lines.`

	result, err := tc.ParseSection(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := result.([]*gent.ToolCall)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}

	argContent, ok := calls[0].Args["content"].(string)
	if !ok {
		t.Fatal("expected content to be string")
	}

	if !containsYAML(argContent, "multi-line") {
		t.Error("expected multi-line content to be preserved")
	}

	if !containsYAML(argContent, "spans multiple lines") {
		t.Error("expected full content to be preserved")
	}
}

func TestYAML_Execute_SchemaValidation_Success(t *testing.T) {
	tc := NewYAML()

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
		yamlTextFormatter,
	)
	tc.RegisterTool(tool)

	content := `tool: search
args:
  query: weather`
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

	if yamlGetTextContent(result.Results[0].Result) != "Results for: weather" {
		t.Errorf("unexpected result: %v", result.Results[0].Result)
	}
}

func TestYAML_Execute_SchemaValidation_MissingRequired(t *testing.T) {
	tc := NewYAML()

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
		yamlTextFormatter,
	)
	tc.RegisterTool(tool)

	// Missing required "query" field
	content := `tool: search
args: {}`
	result, err := tc.Execute(context.Background(), nil, content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Errors[0] == nil {
		t.Fatal("expected validation error")
	}

	if !containsYAML(result.Errors[0].Error(), "schema validation failed") {
		t.Errorf("expected schema validation error, got: %v", result.Errors[0])
	}

	// Tool should not have been called
	if result.Results[0] != nil {
		t.Errorf("expected nil result when validation fails, got: %v", result.Results[0])
	}
}

func TestYAML_Execute_SchemaValidation_WrongType(t *testing.T) {
	tc := NewYAML()

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
		yamlTextFormatter,
	)
	tc.RegisterTool(tool)

	// Wrong type: "count" should be integer but we pass string
	content := `tool: counter
args:
  count: not a number`
	result, err := tc.Execute(context.Background(), nil, content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Errors[0] == nil {
		t.Fatal("expected validation error for wrong type")
	}

	if !containsYAML(result.Errors[0].Error(), "schema validation failed") {
		t.Errorf("expected schema validation error, got: %v", result.Errors[0])
	}

	// Tool should not have been called
	if result.Results[0] != nil {
		t.Errorf("expected nil result when validation fails, got: %v", result.Results[0])
	}
}

func TestYAML_Execute_NoSchema_NoValidation(t *testing.T) {
	tc := NewYAML()

	// Tool without schema - should accept any args
	tool := gent.NewToolFunc[map[string]any, string](
		"flexible",
		"A flexible tool",
		nil, // No schema
		func(ctx context.Context, args map[string]any) (string, error) {
			return "success", nil
		},
		yamlTextFormatter,
	)
	tc.RegisterTool(tool)

	// Any args should work
	content := `tool: flexible
args:
  anything: works
  numbers: 123`
	result, err := tc.Execute(context.Background(), nil, content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Errors[0] != nil {
		t.Errorf("expected no error for tool without schema, got: %v", result.Errors[0])
	}

	if yamlGetTextContent(result.Results[0].Result) != "success" {
		t.Errorf("unexpected result: %v", result.Results[0].Result)
	}
}

func TestYAML_Execute_SchemaValidation_MultipleProperties(t *testing.T) {
	tc := NewYAML()

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
		yamlTextFormatter,
	)
	tc.RegisterTool(tool)

	// Valid args
	content := `tool: booking
args:
  origin: NYC
  destination: LAX
  passengers: 2`
	result, err := tc.Execute(context.Background(), nil, content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Errors[0] != nil {
		t.Errorf("expected no error, got: %v", result.Errors[0])
	}

	// Missing one required field
	content = `tool: booking
args:
  origin: NYC
  destination: LAX`
	result, err = tc.Execute(context.Background(), nil, content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Errors[0] == nil {
		t.Fatal("expected validation error for missing required field")
	}
}

func containsYAML(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
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
