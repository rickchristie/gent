package termination

import (
	"errors"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/tmc/langchaingo/llms"
)

func TestJSON_Name(t *testing.T) {
	term := NewJSON[string]()
	if term.Name() != "answer" {
		t.Errorf("expected default name 'answer', got '%s'", term.Name())
	}

	term.WithSectionName("result")
	if term.Name() != "result" {
		t.Errorf("expected name 'result', got '%s'", term.Name())
	}
}

func TestJSON_Prompt(t *testing.T) {
	term := NewJSON[string]()
	term.WithPrompt("Provide the result")

	prompt := term.Prompt()
	if !contains(prompt, "Provide the result") {
		t.Error("expected prompt text in output")
	}
	if !contains(prompt, "string") {
		t.Error("expected type in schema")
	}
}

func TestJSON_ParseSection_String(t *testing.T) {
	term := NewJSON[string]()

	result, err := term.ParseSection(`"hello world"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	str, ok := result.(string)
	if !ok {
		t.Fatalf("expected string, got %T", result)
	}

	if str != "hello world" {
		t.Errorf("unexpected result: %s", str)
	}
}

func TestJSON_ParseSection_Int(t *testing.T) {
	term := NewJSON[int]()

	result, err := term.ParseSection(`42`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	num, ok := result.(int)
	if !ok {
		t.Fatalf("expected int, got %T", result)
	}

	if num != 42 {
		t.Errorf("unexpected result: %d", num)
	}
}

func TestJSON_ParseSection_Bool(t *testing.T) {
	term := NewJSON[bool]()

	result, err := term.ParseSection(`true`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	b, ok := result.(bool)
	if !ok {
		t.Fatalf("expected bool, got %T", result)
	}

	if !b {
		t.Error("expected true")
	}
}

type SimpleStruct struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

func TestJSON_ParseSection_Struct(t *testing.T) {
	term := NewJSON[SimpleStruct]()

	result, err := term.ParseSection(`{"name": "test", "value": 123}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s, ok := result.(SimpleStruct)
	if !ok {
		t.Fatalf("expected SimpleStruct, got %T", result)
	}

	if s.Name != "test" {
		t.Errorf("unexpected name: %s", s.Name)
	}

	if s.Value != 123 {
		t.Errorf("unexpected value: %d", s.Value)
	}
}

type NestedStruct struct {
	Inner SimpleStruct `json:"inner"`
	Count int          `json:"count"`
}

func TestJSON_ParseSection_NestedStruct(t *testing.T) {
	term := NewJSON[NestedStruct]()

	result, err := term.ParseSection(`{"inner": {"name": "nested", "value": 1}, "count": 5}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s, ok := result.(NestedStruct)
	if !ok {
		t.Fatalf("expected NestedStruct, got %T", result)
	}

	if s.Inner.Name != "nested" {
		t.Errorf("unexpected inner name: %s", s.Inner.Name)
	}

	if s.Count != 5 {
		t.Errorf("unexpected count: %d", s.Count)
	}
}

type PointerStruct struct {
	Name  string  `json:"name"`
	Value *int    `json:"value,omitempty"`
	Extra *string `json:"extra,omitempty"`
}

func TestJSON_ParseSection_PointerFields(t *testing.T) {
	term := NewJSON[PointerStruct]()

	// With value present
	result, err := term.ParseSection(`{"name": "test", "value": 42}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := result.(PointerStruct)
	if s.Name != "test" {
		t.Errorf("unexpected name: %s", s.Name)
	}
	if s.Value == nil || *s.Value != 42 {
		t.Error("expected value pointer to be set")
	}
	if s.Extra != nil {
		t.Error("expected extra to be nil")
	}
}

func TestJSON_ParseSection_PointerFieldsNil(t *testing.T) {
	term := NewJSON[PointerStruct]()

	// With null value
	result, err := term.ParseSection(`{"name": "test", "value": null}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := result.(PointerStruct)
	if s.Value != nil {
		t.Error("expected value to be nil")
	}
}

type TimeStruct struct {
	Created time.Time     `json:"created"`
	TTL     time.Duration `json:"ttl"`
}

func TestJSON_ParseSection_TimeFields(t *testing.T) {
	term := NewJSON[TimeStruct]()

	result, err := term.ParseSection(`{"created": "2024-01-15T10:30:00Z", "ttl": 3600000000000}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := result.(TimeStruct)
	if s.Created.Year() != 2024 {
		t.Errorf("unexpected year: %d", s.Created.Year())
	}
	if s.TTL != time.Hour {
		t.Errorf("unexpected TTL: %v", s.TTL)
	}
}

func TestJSON_ParseSection_Slice(t *testing.T) {
	term := NewJSON[[]string]()

	result, err := term.ParseSection(`["one", "two", "three"]`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	slice, ok := result.([]string)
	if !ok {
		t.Fatalf("expected []string, got %T", result)
	}

	if len(slice) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(slice))
	}

	if slice[0] != "one" {
		t.Errorf("unexpected first element: %s", slice[0])
	}
}

func TestJSON_ParseSection_Map(t *testing.T) {
	term := NewJSON[map[string]int]()

	result, err := term.ParseSection(`{"a": 1, "b": 2}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m, ok := result.(map[string]int)
	if !ok {
		t.Fatalf("expected map[string]int, got %T", result)
	}

	if m["a"] != 1 {
		t.Errorf("unexpected value for 'a': %d", m["a"])
	}

	if m["b"] != 2 {
		t.Errorf("unexpected value for 'b': %d", m["b"])
	}
}

func TestJSON_ParseSection_InvalidJSON(t *testing.T) {
	term := NewJSON[string]()

	_, err := term.ParseSection(`{invalid json}`)
	if !errors.Is(err, gent.ErrInvalidJSON) {
		t.Errorf("expected ErrInvalidJSON, got: %v", err)
	}
}

func TestJSON_ParseSection_Empty(t *testing.T) {
	term := NewJSON[string]()

	result, err := term.ParseSection("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	str := result.(string)
	if str != "" {
		t.Errorf("expected empty string for empty content, got: '%s'", str)
	}
}

func TestJSON_WithExample(t *testing.T) {
	term := NewJSON[SimpleStruct]().
		WithExample(SimpleStruct{Name: "example", Value: 42})

	prompt := term.Prompt()
	if !contains(prompt, "example") {
		t.Error("expected example name in prompt")
	}
	if !contains(prompt, "42") {
		t.Error("expected example value in prompt")
	}
}

type DescribedStruct struct {
	ID   int    `json:"id" description:"The unique identifier"`
	Name string `json:"name" description:"The display name"`
}

func TestJSON_SchemaWithDescriptions(t *testing.T) {
	term := NewJSON[DescribedStruct]()

	prompt := term.Prompt()
	if !contains(prompt, "unique identifier") {
		t.Error("expected ID description in schema")
	}
	if !contains(prompt, "display name") {
		t.Error("expected Name description in schema")
	}
}

type OmitEmptyStruct struct {
	Required string  `json:"required"`
	Optional string  `json:"optional,omitempty"`
	Pointer  *string `json:"pointer"`
}

func TestJSON_SchemaRequired(t *testing.T) {
	term := NewJSON[OmitEmptyStruct]()

	prompt := term.Prompt()
	// Required field should be in the required array
	if !contains(prompt, `"required"`) {
		t.Error("expected required field in schema")
	}
}

type SliceStruct struct {
	Items []SimpleStruct `json:"items"`
}

func TestJSON_ParseSection_SliceOfStructs(t *testing.T) {
	term := NewJSON[SliceStruct]()

	result, err := term.ParseSection(`{
		"items": [
			{"name": "first", "value": 1},
			{"name": "second", "value": 2}
		]
	}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := result.(SliceStruct)
	if len(s.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(s.Items))
	}

	if s.Items[0].Name != "first" {
		t.Errorf("unexpected first item name: %s", s.Items[0].Name)
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

func TestJSON_ShouldTerminate_ValidJSON(t *testing.T) {
	term := NewJSON[SimpleStruct]()

	result := term.ShouldTerminate(`{"name": "test", "value": 42}`)
	if result == nil {
		t.Fatal("expected non-nil result for valid JSON")
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 content part, got %d", len(result))
	}

	tc, ok := result[0].(llms.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result[0])
	}

	// Should contain the re-serialized JSON
	if !contains(tc.Text, "test") || !contains(tc.Text, "42") {
		t.Errorf("unexpected text: %s", tc.Text)
	}
}

func TestJSON_ShouldTerminate_EmptyContent(t *testing.T) {
	term := NewJSON[string]()

	result := term.ShouldTerminate("")
	if result != nil {
		t.Error("expected nil result for empty content")
	}
}

func TestJSON_ShouldTerminate_InvalidJSON(t *testing.T) {
	term := NewJSON[SimpleStruct]()

	result := term.ShouldTerminate(`{invalid json}`)
	if result != nil {
		t.Error("expected nil result for invalid JSON")
	}
}

func TestJSON_ShouldTerminate_TypeMismatch(t *testing.T) {
	term := NewJSON[SimpleStruct]()

	// Valid JSON but wrong type
	result := term.ShouldTerminate(`"just a string"`)
	if result != nil {
		t.Error("expected nil result for type mismatch")
	}
}
