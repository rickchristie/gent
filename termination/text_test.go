package termination

import (
	"testing"

	"github.com/tmc/langchaingo/llms"
)

func TestText_Name(t *testing.T) {
	term := NewText()
	if term.Name() != "answer" {
		t.Errorf("expected default name 'answer', got '%s'", term.Name())
	}

	term.WithSectionName("result")
	if term.Name() != "result" {
		t.Errorf("expected name 'result', got '%s'", term.Name())
	}
}

func TestText_Prompt(t *testing.T) {
	term := NewText()
	if term.Prompt() != "Write your final answer here." {
		t.Errorf("unexpected default prompt: %s", term.Prompt())
	}

	term.WithPrompt("Custom prompt")
	if term.Prompt() != "Custom prompt" {
		t.Errorf("expected custom prompt, got: %s", term.Prompt())
	}
}

func TestText_ParseSection(t *testing.T) {
	term := NewText()

	result, err := term.ParseSection("The weather is sunny today.")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	str, ok := result.(string)
	if !ok {
		t.Fatalf("expected string result, got %T", result)
	}

	if str != "The weather is sunny today." {
		t.Errorf("unexpected result: %s", str)
	}
}

func TestText_ParseSection_Trimming(t *testing.T) {
	term := NewText()

	result, err := term.ParseSection("   Content with whitespace   ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	str := result.(string)
	if str != "Content with whitespace" {
		t.Errorf("whitespace not trimmed: '%s'", str)
	}
}

func TestText_ParseSection_Empty(t *testing.T) {
	term := NewText()

	result, err := term.ParseSection("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	str := result.(string)
	if str != "" {
		t.Errorf("expected empty string, got: '%s'", str)
	}
}

func TestText_ParseSection_Multiline(t *testing.T) {
	term := NewText()

	content := `Line 1
Line 2
Line 3`

	result, err := term.ParseSection(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	str := result.(string)
	if str != content {
		t.Errorf("multiline content not preserved: '%s'", str)
	}
}

func TestText_ShouldTerminate_WithContent(t *testing.T) {
	term := NewText()

	result := term.ShouldTerminate("The final answer.")
	if result == nil {
		t.Fatal("expected non-nil result for non-empty content")
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 content part, got %d", len(result))
	}

	tc, ok := result[0].(llms.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result[0])
	}

	if tc.Text != "The final answer." {
		t.Errorf("unexpected text: %s", tc.Text)
	}
}

func TestText_ShouldTerminate_EmptyContent(t *testing.T) {
	term := NewText()

	result := term.ShouldTerminate("")
	if result != nil {
		t.Error("expected nil result for empty content")
	}
}

func TestText_ShouldTerminate_WhitespaceOnly(t *testing.T) {
	term := NewText()

	result := term.ShouldTerminate("   ")
	if result != nil {
		t.Error("expected nil result for whitespace-only content")
	}
}

func TestText_ShouldTerminate_TrimsWhitespace(t *testing.T) {
	term := NewText()

	result := term.ShouldTerminate("  trimmed content  ")
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	tc := result[0].(llms.TextContent)
	if tc.Text != "trimmed content" {
		t.Errorf("expected trimmed content, got: '%s'", tc.Text)
	}
}
