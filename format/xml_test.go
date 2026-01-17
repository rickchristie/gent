package format

import (
	"errors"
	"testing"

	"github.com/rickchristie/gent"
)

func TestXML_Describe(t *testing.T) {
	format := NewXML()

	sections := []gent.TextOutputSection{
		&mockSection{name: "thinking", prompt: "Write your reasoning here."},
		&mockSection{name: "action", prompt: "Write your tool call here."},
	}

	result := format.Describe(sections)

	if !contains(result, "<thinking>") {
		t.Error("expected <thinking> tag in describe output")
	}
	if !contains(result, "</thinking>") {
		t.Error("expected </thinking> tag in describe output")
	}
	if !contains(result, "<action>") {
		t.Error("expected <action> tag in describe output")
	}
	if !contains(result, "</action>") {
		t.Error("expected </action> tag in describe output")
	}
	if !contains(result, "Write your reasoning here.") {
		t.Error("expected thinking prompt in describe output")
	}
}

func TestXML_Describe_Empty(t *testing.T) {
	format := NewXML()
	result := format.Describe(nil)
	if result != "" {
		t.Errorf("expected empty string for nil sections, got: %s", result)
	}
}

func TestXML_Parse_SingleSection(t *testing.T) {
	format := NewXML()
	format.Describe([]gent.TextOutputSection{
		&mockSection{name: "answer", prompt: ""},
	})

	output := `<answer>
The weather is sunny today.
</answer>`

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

func TestXML_Parse_MultipleSections(t *testing.T) {
	format := NewXML()
	format.Describe([]gent.TextOutputSection{
		&mockSection{name: "thinking", prompt: ""},
		&mockSection{name: "action", prompt: ""},
		&mockSection{name: "answer", prompt: ""},
	})

	output := `<thinking>
I need to search for weather information.
</thinking>

<action>
{"tool": "search", "args": {"query": "weather"}}
</action>

<answer>
The weather is sunny.
</answer>`

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

func TestXML_Parse_MultipleSameSection(t *testing.T) {
	format := NewXML()
	format.Describe([]gent.TextOutputSection{
		&mockSection{name: "action", prompt: ""},
	})

	output := `<action>
First action content.
</action>

<action>
Second action content.
</action>`

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

func TestXML_Parse_CaseInsensitive(t *testing.T) {
	format := NewXML()
	format.Describe([]gent.TextOutputSection{
		&mockSection{name: "thinking", prompt: ""},
	})

	output := `<THINKING>
Case insensitive content.
</THINKING>`

	result, err := format.Parse(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result["thinking"]) != 1 {
		t.Fatalf("expected 1 thinking section, got %d", len(result["thinking"]))
	}

	if result["thinking"][0] != "Case insensitive content." {
		t.Errorf("unexpected content: %s", result["thinking"][0])
	}
}

func TestXML_Parse_ContentOutsideTags(t *testing.T) {
	format := NewXML()
	format.Describe([]gent.TextOutputSection{
		&mockSection{name: "answer", prompt: ""},
	})

	output := `Some content before tags that should be ignored.

<answer>
The actual answer.
</answer>

Some content after tags.`

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

func TestXML_Parse_EmptySection(t *testing.T) {
	format := NewXML()
	format.Describe([]gent.TextOutputSection{
		&mockSection{name: "thinking", prompt: ""},
		&mockSection{name: "answer", prompt: ""},
	})

	output := `<thinking></thinking>
<answer>The answer.</answer>`

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

func TestXML_Parse_SameLineTag(t *testing.T) {
	format := NewXML()
	format.Describe([]gent.TextOutputSection{
		&mockSection{name: "answer", prompt: ""},
	})

	output := `<answer>Short answer.</answer>`

	result, err := format.Parse(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["answer"][0] != "Short answer." {
		t.Errorf("unexpected content: %s", result["answer"][0])
	}
}

func TestXML_Parse_WhitespaceTrimming(t *testing.T) {
	format := NewXML()
	format.Describe([]gent.TextOutputSection{
		&mockSection{name: "answer", prompt: ""},
	})

	output := `<answer>

   Content with whitespace around it.

</answer>`

	result, err := format.Parse(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["answer"][0] != "Content with whitespace around it." {
		t.Errorf("whitespace not trimmed properly: '%s'", result["answer"][0])
	}
}

func TestXML_Parse_NoRecognizedTags(t *testing.T) {
	format := NewXML()
	format.Describe([]gent.TextOutputSection{
		&mockSection{name: "answer", prompt: ""},
	})

	output := `<unknown>Some content.</unknown>`

	_, err := format.Parse(output)
	if !errors.Is(err, gent.ErrNoSectionsFound) {
		t.Errorf("expected ErrNoSectionsFound, got: %v", err)
	}
}

func TestXML_Parse_NoTags(t *testing.T) {
	format := NewXML()
	format.Describe([]gent.TextOutputSection{
		&mockSection{name: "answer", prompt: ""},
	})

	output := `Just some plain text without any tags.`

	_, err := format.Parse(output)
	if !errors.Is(err, gent.ErrNoSectionsFound) {
		t.Errorf("expected ErrNoSectionsFound, got: %v", err)
	}
}

func TestXML_Parse_NestedContent(t *testing.T) {
	format := NewXML()
	format.Describe([]gent.TextOutputSection{
		&mockSection{name: "code", prompt: ""},
	})

	output := `<code>
func main() {
    fmt.Println("<hello>")
}
</code>`

	result, err := format.Parse(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := `func main() {
    fmt.Println("<hello>")
}`

	if result["code"][0] != expected {
		t.Errorf("unexpected content: %s", result["code"][0])
	}
}
