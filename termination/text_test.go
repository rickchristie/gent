package termination

import "testing"

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
