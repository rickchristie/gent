package format

import (
	"errors"
	"testing"

	"github.com/rickchristie/gent"
)

// mockSection is a simple TextOutputSection for testing.
type mockSection struct {
	name   string
	prompt string
}

func (m *mockSection) Name() string   { return m.name }
func (m *mockSection) Prompt() string { return m.prompt }
func (m *mockSection) ParseSection(content string) (any, error) {
	return content, nil
}

func TestMarkdown_Describe(t *testing.T) {
	format := NewMarkdown()

	sections := []gent.TextOutputSection{
		&mockSection{name: "Thinking", prompt: "Write your reasoning here."},
		&mockSection{name: "Action", prompt: "Write your tool call here."},
	}

	result := format.Describe(sections)

	if !contains(result, "# Thinking") {
		t.Error("expected Thinking header in describe output")
	}
	if !contains(result, "# Action") {
		t.Error("expected Action header in describe output")
	}
	if !contains(result, "Write your reasoning here.") {
		t.Error("expected Thinking prompt in describe output")
	}
	if !contains(result, "Write your tool call here.") {
		t.Error("expected Action prompt in describe output")
	}
}

func TestMarkdown_Describe_Empty(t *testing.T) {
	format := NewMarkdown()
	result := format.Describe(nil)
	if result != "" {
		t.Errorf("expected empty string for nil sections, got: %s", result)
	}
}

func TestMarkdown_Parse_SingleSection(t *testing.T) {
	format := NewMarkdown()
	format.Describe([]gent.TextOutputSection{
		&mockSection{name: "Answer", prompt: ""},
	})

	output := `# Answer
The weather is sunny today.`

	result, err := format.Parse(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result["answer"]) != 1 {
		t.Fatalf("expected 1 answer section, got %d", len(result["answer"]))
	}

	if result["answer"][0] != "The weather is sunny today." {
		t.Errorf("unexpected content: %s", result["answer"][0])
	}
}

func TestMarkdown_Parse_MultipleSections(t *testing.T) {
	format := NewMarkdown()
	format.Describe([]gent.TextOutputSection{
		&mockSection{name: "Thinking", prompt: ""},
		&mockSection{name: "Action", prompt: ""},
		&mockSection{name: "Answer", prompt: ""},
	})

	output := `# Thinking
I need to search for weather information.

# Action
{"tool": "search", "args": {"query": "weather"}}

# Answer
The weather is sunny.`

	result, err := format.Parse(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("expected 3 sections, got %d", len(result))
	}

	if result["thinking"][0] != "I need to search for weather information." {
		t.Errorf("unexpected thinking content: %s", result["thinking"][0])
	}

	expected := `{"tool": "search", "args": {"query": "weather"}}`
	if result["action"][0] != expected {
		t.Errorf("unexpected action content: %s", result["action"][0])
	}

	if result["answer"][0] != "The weather is sunny." {
		t.Errorf("unexpected answer content: %s", result["answer"][0])
	}
}

func TestMarkdown_Parse_MultipleSameSection(t *testing.T) {
	format := NewMarkdown()
	format.Describe([]gent.TextOutputSection{
		&mockSection{name: "Action", prompt: ""},
	})

	output := `# Action
First action content.

# Action
Second action content.`

	result, err := format.Parse(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result["action"]) != 2 {
		t.Fatalf("expected 2 action sections, got %d", len(result["action"]))
	}

	if result["action"][0] != "First action content." {
		t.Errorf("unexpected first action: %s", result["action"][0])
	}

	if result["action"][1] != "Second action content." {
		t.Errorf("unexpected second action: %s", result["action"][1])
	}
}

func TestMarkdown_Parse_CaseInsensitive(t *testing.T) {
	format := NewMarkdown()
	format.Describe([]gent.TextOutputSection{
		&mockSection{name: "Thinking", prompt: ""},
	})

	output := `# THINKING
Case insensitive content.`

	result, err := format.Parse(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result["thinking"]) != 1 {
		t.Fatalf("expected 1 thinking section, got %d", len(result["thinking"]))
	}
}

func TestMarkdown_Parse_ContentBeforeFirstHeader(t *testing.T) {
	format := NewMarkdown()
	format.Describe([]gent.TextOutputSection{
		&mockSection{name: "Answer", prompt: ""},
	})

	output := `Some content before the first header that should be ignored.

# Answer
The actual answer.`

	result, err := format.Parse(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result["answer"]) != 1 {
		t.Fatalf("expected 1 answer section, got %d", len(result["answer"]))
	}

	if result["answer"][0] != "The actual answer." {
		t.Errorf("unexpected answer content: %s", result["answer"][0])
	}
}

func TestMarkdown_Parse_EmptySection(t *testing.T) {
	format := NewMarkdown()
	format.Describe([]gent.TextOutputSection{
		&mockSection{name: "Thinking", prompt: ""},
		&mockSection{name: "Answer", prompt: ""},
	})

	output := `# Thinking

# Answer
The answer.`

	result, err := format.Parse(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["thinking"][0] != "" {
		t.Errorf("expected empty thinking section, got: %s", result["thinking"][0])
	}

	if result["answer"][0] != "The answer." {
		t.Errorf("unexpected answer content: %s", result["answer"][0])
	}
}

func TestMarkdown_Parse_WhitespaceTrimming(t *testing.T) {
	format := NewMarkdown()
	format.Describe([]gent.TextOutputSection{
		&mockSection{name: "Answer", prompt: ""},
	})

	output := `# Answer

   Content with whitespace around it.

`

	result, err := format.Parse(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["answer"][0] != "Content with whitespace around it." {
		t.Errorf("whitespace not trimmed properly: '%s'", result["answer"][0])
	}
}

func TestMarkdown_Parse_NoRecognizedSections(t *testing.T) {
	format := NewMarkdown()
	format.Describe([]gent.TextOutputSection{
		&mockSection{name: "Answer", prompt: ""},
	})

	output := `# Unknown
Some content.`

	_, err := format.Parse(output)
	if !errors.Is(err, gent.ErrNoSectionsFound) {
		t.Errorf("expected ErrNoSectionsFound, got: %v", err)
	}
}

func TestMarkdown_Parse_NoHeaders(t *testing.T) {
	format := NewMarkdown()
	format.Describe([]gent.TextOutputSection{
		&mockSection{name: "Answer", prompt: ""},
	})

	output := `Just some plain text without any headers.`

	_, err := format.Parse(output)
	if !errors.Is(err, gent.ErrNoSectionsFound) {
		t.Errorf("expected ErrNoSectionsFound, got: %v", err)
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
