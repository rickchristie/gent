package toolchain

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/rickchristie/gent"
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
	tool := gent.NewToolFunc("test", "A test tool", nil, nil)

	tc.RegisterTool(tool)

	if len(tc.Tools()) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tc.Tools()))
	}

	if tc.Tools()[0].Name() != "test" {
		t.Errorf("expected tool name 'test', got '%s'", tc.Tools()[0].Name())
	}
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

	calls := result.([]gent.ToolCall)
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

	calls := result.([]gent.ToolCall)
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

	calls := result.([]gent.ToolCall)
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
	tool := gent.NewToolFunc(
		"search",
		"Search the web",
		nil,
		func(ctx context.Context, args map[string]any) (string, error) {
			query := args["query"].(string)
			return fmt.Sprintf("Results for: %s", query), nil
		},
	)
	tc.RegisterTool(tool)

	content := `{"tool": "search", "args": {"query": "weather"}}`
	result, err := tc.Execute(context.Background(), content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(result.Calls))
	}

	if result.Results[0] != "Results for: weather" {
		t.Errorf("unexpected result: %s", result.Results[0])
	}

	if result.Errors[0] != nil {
		t.Errorf("unexpected error in result: %v", result.Errors[0])
	}
}

func TestJSON_Execute_UnknownTool(t *testing.T) {
	tc := NewJSON()

	content := `{"tool": "unknown", "args": {}}`
	result, err := tc.Execute(context.Background(), content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !errors.Is(result.Errors[0], gent.ErrUnknownTool) {
		t.Errorf("expected ErrUnknownTool, got: %v", result.Errors[0])
	}
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
	)
	tc.RegisterTool(tool)

	content := `{"tool": "failing", "args": {}}`
	result, err := tc.Execute(context.Background(), content)
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

	searchTool := gent.NewToolFunc(
		"search",
		"Search",
		nil,
		func(ctx context.Context, args map[string]any) (string, error) {
			return "search result", nil
		},
	)

	calendarTool := gent.NewToolFunc(
		"calendar",
		"Calendar",
		nil,
		func(ctx context.Context, args map[string]any) (string, error) {
			return "calendar result", nil
		},
	)

	tc.RegisterTool(searchTool)
	tc.RegisterTool(calendarTool)

	content := `[
		{"tool": "search", "args": {}},
		{"tool": "calendar", "args": {}}
	]`

	result, err := tc.Execute(context.Background(), content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result.Results))
	}

	if result.Results[0] != "search result" {
		t.Errorf("unexpected first result: %s", result.Results[0])
	}

	if result.Results[1] != "calendar result" {
		t.Errorf("unexpected second result: %s", result.Results[1])
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
