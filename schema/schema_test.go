package schema

import (
	"testing"
)

func TestCompile_NilSchema(t *testing.T) {
	s, err := Compile(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s != nil {
		t.Error("expected nil schema for nil input")
	}
}

func TestCompile_ValidSchema(t *testing.T) {
	raw := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
	}

	s, err := Compile(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil schema")
	}
	if s.Raw() == nil {
		t.Error("expected non-nil raw schema")
	}
}

func TestSchema_Validate_Valid(t *testing.T) {
	raw := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
			"age":  map[string]any{"type": "integer"},
		},
		"required": []any{"name"},
	}

	s, err := Compile(raw)
	if err != nil {
		t.Fatalf("failed to compile: %v", err)
	}

	// Valid data
	data := map[string]any{
		"name": "John",
		"age":  30,
	}

	err = s.Validate(data)
	if err != nil {
		t.Errorf("expected valid data to pass, got: %v", err)
	}
}

func TestSchema_Validate_MissingRequired(t *testing.T) {
	raw := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
		"required": []any{"name"},
	}

	s, err := Compile(raw)
	if err != nil {
		t.Fatalf("failed to compile: %v", err)
	}

	// Missing required field
	data := map[string]any{}

	err = s.Validate(data)
	if err == nil {
		t.Error("expected validation error for missing required field")
	}

	// Check it's a ValidationError
	if _, ok := err.(*ValidationError); !ok {
		t.Errorf("expected *ValidationError, got %T", err)
	}
}

func TestSchema_Validate_WrongType(t *testing.T) {
	raw := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"count": map[string]any{"type": "integer"},
		},
	}

	s, err := Compile(raw)
	if err != nil {
		t.Fatalf("failed to compile: %v", err)
	}

	// Wrong type
	data := map[string]any{
		"count": "not an integer",
	}

	err = s.Validate(data)
	if err == nil {
		t.Error("expected validation error for wrong type")
	}
}

func TestSchema_Validate_NilSchema(t *testing.T) {
	var s *Schema
	err := s.Validate(map[string]any{"foo": "bar"})
	if err != nil {
		t.Errorf("nil schema should always pass validation, got: %v", err)
	}
}

func TestMustCompile_Valid(t *testing.T) {
	raw := map[string]any{
		"type": "object",
	}

	s := MustCompile(raw)
	if s == nil {
		t.Error("expected non-nil schema")
	}
}

func TestMustCompile_Nil(t *testing.T) {
	s := MustCompile(nil)
	if s != nil {
		t.Error("expected nil schema for nil input")
	}
}

// Test the builder functions

func TestObject_Basic(t *testing.T) {
	schema := Object(map[string]*Property{
		"name": String("The name"),
		"age":  Integer("The age"),
	}, "name")

	if schema["type"] != "object" {
		t.Error("expected type 'object'")
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties map")
	}

	if len(props) != 2 {
		t.Errorf("expected 2 properties, got %d", len(props))
	}

	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatal("expected required array")
	}

	if len(required) != 1 || required[0] != "name" {
		t.Errorf("expected required=['name'], got %v", required)
	}
}

func TestString_WithConstraints(t *testing.T) {
	prop := String("A description").
		MinLength(1).
		MaxLength(100).
		Pattern("^[a-z]+$").
		Format("email")

	built := prop.build()

	if built["type"] != "string" {
		t.Error("expected type 'string'")
	}
	if built["description"] != "A description" {
		t.Error("expected description")
	}
	if built["minLength"] != 1 {
		t.Error("expected minLength 1")
	}
	if built["maxLength"] != 100 {
		t.Error("expected maxLength 100")
	}
	if built["pattern"] != "^[a-z]+$" {
		t.Error("expected pattern")
	}
	if built["format"] != "email" {
		t.Error("expected format")
	}
}

func TestInteger_WithConstraints(t *testing.T) {
	prop := Integer("A count").Min(0).Max(100)

	built := prop.build()

	if built["type"] != "integer" {
		t.Error("expected type 'integer'")
	}
	if built["minimum"] != float64(0) {
		t.Errorf("expected minimum 0, got %v", built["minimum"])
	}
	if built["maximum"] != float64(100) {
		t.Errorf("expected maximum 100, got %v", built["maximum"])
	}
}

func TestNumber_Basic(t *testing.T) {
	prop := Number("A price")
	built := prop.build()

	if built["type"] != "number" {
		t.Error("expected type 'number'")
	}
}

func TestBoolean_Basic(t *testing.T) {
	prop := Boolean("A flag")
	built := prop.build()

	if built["type"] != "boolean" {
		t.Error("expected type 'boolean'")
	}
}

func TestArray_Basic(t *testing.T) {
	items := map[string]any{"type": "string"}
	prop := Array("A list", items)
	built := prop.build()

	if built["type"] != "array" {
		t.Error("expected type 'array'")
	}
	if built["items"] == nil {
		t.Error("expected items")
	}
}

func TestProperty_Enum(t *testing.T) {
	prop := String("A status").Enum("pending", "active", "closed")
	built := prop.build()

	enum, ok := built["enum"].([]any)
	if !ok {
		t.Fatal("expected enum array")
	}

	if len(enum) != 3 {
		t.Errorf("expected 3 enum values, got %d", len(enum))
	}
}

func TestProperty_Default(t *testing.T) {
	prop := String("A field").Default("default_value")
	built := prop.build()

	if built["default"] != "default_value" {
		t.Error("expected default value")
	}
}

func TestValidationError_Error(t *testing.T) {
	originalErr := &ValidationError{Err: nil}
	msg := originalErr.Error()
	if msg != "schema validation failed: <nil>" {
		t.Errorf("unexpected error message: %s", msg)
	}
}

func TestValidationError_Unwrap(t *testing.T) {
	inner := &ValidationError{}
	outer := &ValidationError{Err: inner}

	unwrapped := outer.Unwrap()
	if unwrapped != inner {
		t.Error("Unwrap should return inner error")
	}
}

// Test schema validation with builder-created schemas

func TestBuilderSchema_Validation(t *testing.T) {
	raw := Object(map[string]*Property{
		"name":  String("User name").MinLength(1),
		"email": String("Email address").Format("email"),
		"age":   Integer("Age").Min(0).Max(150),
	}, "name", "email")

	s, err := Compile(raw)
	if err != nil {
		t.Fatalf("failed to compile builder schema: %v", err)
	}

	// Valid data
	err = s.Validate(map[string]any{
		"name":  "John",
		"email": "john@example.com",
		"age":   30,
	})
	if err != nil {
		t.Errorf("expected valid data to pass: %v", err)
	}

	// Missing required email
	err = s.Validate(map[string]any{
		"name": "John",
	})
	if err == nil {
		t.Error("expected validation error for missing email")
	}
}
