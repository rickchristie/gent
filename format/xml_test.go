package format

import (
	"errors"
	"testing"

	"github.com/rickchristie/gent"
)

func TestXML_Parse_SingleSection(t *testing.T) {
	format := NewXML()
	format.DescribeStructure([]gent.TextOutputSection{
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
	format.DescribeStructure([]gent.TextOutputSection{
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
	format.DescribeStructure([]gent.TextOutputSection{
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
	format.DescribeStructure([]gent.TextOutputSection{
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
	format.DescribeStructure([]gent.TextOutputSection{
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
	format.DescribeStructure([]gent.TextOutputSection{
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
	format.DescribeStructure([]gent.TextOutputSection{
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
	format.DescribeStructure([]gent.TextOutputSection{
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
	format.DescribeStructure([]gent.TextOutputSection{
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
	format.DescribeStructure([]gent.TextOutputSection{
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
	format.DescribeStructure([]gent.TextOutputSection{
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

func TestXML_Parse_LLMWritesObservationSection(t *testing.T) {
	// Test case where LLM writes its own observation section and continues with text
	// outside of tags before the final answer. This tests that each section parses
	// correctly without capturing content from other sections.
	format := NewXML()
	format.DescribeStructure([]gent.TextOutputSection{
		&mockSection{name: "thinking", prompt: ""},
		&mockSection{name: "action", prompt: ""},
		&mockSection{name: "answer", prompt: ""},
	})

	output := `<thinking>
Reschedule succeeded: BK001 now on AA102 with seat 12A, $0 charge.
Flight details confirmed: AA102 JFK-LAX 2026-01-25 20:00-23:30 UTC.
Parallel tool calls now. After observation, provide <answer>.
</thinking>
<action>
- tool: get_booking_info
  args:
    booking_id: BK001
- tool: send_notification
  args:
    customer_id: C001
    method: email
    subject: "SkyWings Booking BK001 Rescheduled"
    message: "Dear John Smith, your booking has been rescheduled."
</action>
<observation>
[get_booking_info] {"booking_id":"BK001","customer_id":"C001","flight_number":"AA102"}
[send_notification] {"success":true,"method":"email"}
</observation>

A: Tool calls successful.

get_booking_info: BK001 now AA102, JFK-LAX 2026-01-25.

Final answer: Polite confirmation to customer.

Be professional.<answer>
Hello John,

Your booking **BK001** has been successfully rescheduled to **AA102**.

Best regards,
SkyWings Airlines Customer Service
</answer>`

	result, err := format.Parse(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check thinking section - should only contain the thinking content
	if len(result["thinking"]) != 1 {
		t.Fatalf("expected 1 thinking section, got %d", len(result["thinking"]))
	}
	thinking := result["thinking"][0]
	if !contains(thinking, "Reschedule succeeded") {
		t.Errorf("thinking should contain 'Reschedule succeeded': %s", thinking)
	}
	if contains(thinking, "<action>") {
		t.Errorf("thinking should NOT contain '<action>': %s", thinking)
	}

	// Check action section - should only contain the action content
	if len(result["action"]) != 1 {
		t.Fatalf("expected 1 action section, got %d", len(result["action"]))
	}
	action := result["action"][0]
	if !contains(action, "get_booking_info") {
		t.Errorf("action should contain 'get_booking_info': %s", action)
	}
	if contains(action, "<observation>") {
		t.Errorf("action should NOT contain '<observation>': %s", action)
	}

	// Check answer section - this is the critical test
	// The answer should ONLY contain the content between <answer> and </answer>
	// It should NOT contain content from before <answer> like "Be professional."
	if len(result["answer"]) != 1 {
		t.Fatalf("expected 1 answer section, got %d", len(result["answer"]))
	}
	answer := result["answer"][0]

	// Answer should start with "Hello John" (after trimming)
	if !hasPrefix(answer, "Hello John") {
		t.Errorf("answer should start with 'Hello John', got: %s", truncate(answer, 100))
	}

	// Answer should NOT contain content from before <answer>
	if contains(answer, "Be professional") {
		t.Errorf("answer should NOT contain 'Be professional': %s", answer)
	}
	if contains(answer, "</thinking>") {
		t.Errorf("answer should NOT contain '</thinking>': %s", answer)
	}
	if contains(answer, "<action>") {
		t.Errorf("answer should NOT contain '<action>': %s", answer)
	}
	if contains(answer, "Tool calls successful") {
		t.Errorf("answer should NOT contain 'Tool calls successful': %s", answer)
	}
}

func TestXML_Parse_StrictMode_ReturnsErrorOnAmbiguity(t *testing.T) {
	// In strict mode, if a section's content contains another section's tags,
	// Parse should return an error.
	format := NewXML().WithStrict(true)
	format.DescribeStructure([]gent.TextOutputSection{
		&mockSection{name: "thinking", prompt: ""},
		&mockSection{name: "answer", prompt: ""},
	})

	// This output has <answer> text inside <thinking>
	output := `<thinking>
After observation, provide <answer>.
</thinking>
<answer>
The actual answer.
</answer>`

	_, err := format.Parse(output)
	if err == nil {
		t.Fatal("expected error in strict mode when tags appear inside other sections")
	}

	if !errors.Is(err, ErrAmbiguousTags) {
		t.Errorf("expected ErrAmbiguousTags, got: %v", err)
	}
}

func TestXML_Parse_NonStrictMode_HandlesAmbiguity(t *testing.T) {
	// In non-strict mode (default), Parse should still work by finding the LAST
	// opening tag before each closing tag.
	format := NewXML() // strict=false by default
	format.DescribeStructure([]gent.TextOutputSection{
		&mockSection{name: "thinking", prompt: ""},
		&mockSection{name: "answer", prompt: ""},
	})

	// This output has <answer> text inside <thinking>
	output := `<thinking>
After observation, provide <answer>.
</thinking>
<answer>
The actual answer.
</answer>`

	result, err := format.Parse(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Thinking should contain the literal <answer> text since it's inside
	if len(result["thinking"]) != 1 {
		t.Fatalf("expected 1 thinking section, got %d", len(result["thinking"]))
	}
	// In non-strict mode, thinking contains everything between <thinking> and </thinking>
	thinking := result["thinking"][0]
	if !contains(thinking, "provide <answer>.") {
		t.Errorf("thinking should contain literal '<answer>' text: %s", thinking)
	}

	// Answer should ONLY contain "The actual answer."
	if len(result["answer"]) != 1 {
		t.Fatalf("expected 1 answer section, got %d", len(result["answer"]))
	}
	if result["answer"][0] != "The actual answer." {
		t.Errorf("expected 'The actual answer.', got: %s", result["answer"][0])
	}
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
