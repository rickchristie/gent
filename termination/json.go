package termination

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/rickchristie/gent"
)

// JSON parses JSON content into type T.
// Supports: primitives, pointers, structs, slices, maps, time.Time, time.Duration.
type JSON[T any] struct {
	sectionName string
	prompt      string
	example     *T
}

// NewJSON creates a new JSON termination with default section name "answer".
func NewJSON[T any]() *JSON[T] {
	return &JSON[T]{
		sectionName: "answer",
		prompt:      "",
	}
}

// WithSectionName sets the section name for this termination.
func (t *JSON[T]) WithSectionName(name string) *JSON[T] {
	t.sectionName = name
	return t
}

// WithPrompt sets the prompt instructions for this termination.
func (t *JSON[T]) WithPrompt(prompt string) *JSON[T] {
	t.prompt = prompt
	return t
}

// WithExample sets an example value to include in the prompt.
func (t *JSON[T]) WithExample(example T) *JSON[T] {
	t.example = &example
	return t
}

// Name returns the section identifier.
func (t *JSON[T]) Name() string {
	return t.sectionName
}

// Prompt returns the instructions including JSON schema derived from T.
func (t *JSON[T]) Prompt() string {
	var sb strings.Builder

	if t.prompt != "" {
		sb.WriteString(t.prompt)
		sb.WriteString("\n\n")
	}

	sb.WriteString("Respond with valid JSON matching this schema:\n")

	var zero T
	schema := generateJSONSchema(reflect.TypeOf(zero))
	schemaJSON, err := json.MarshalIndent(schema, "", "  ")
	if err == nil {
		sb.Write(schemaJSON)
	}

	if t.example != nil {
		sb.WriteString("\n\nExample:\n")
		exampleJSON, err := json.MarshalIndent(t.example, "", "  ")
		if err == nil {
			sb.Write(exampleJSON)
		}
	}

	return sb.String()
}

// ParseSection parses the JSON content into type T.
func (t *JSON[T]) ParseSection(content string) (any, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		var zero T
		return zero, nil
	}

	var result T
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, fmt.Errorf("%w: %v", gent.ErrInvalidJSON, err)
	}

	return result, nil
}

// generateJSONSchema creates a JSON Schema from a Go type using reflection.
func generateJSONSchema(t reflect.Type) map[string]any {
	if t == nil {
		return map[string]any{"type": "null"}
	}

	// Handle pointer types
	if t.Kind() == reflect.Ptr {
		schema := generateJSONSchema(t.Elem())
		// Pointers are nullable
		if typeVal, ok := schema["type"].(string); ok {
			schema["type"] = []string{typeVal, "null"}
		}
		return schema
	}

	// Handle special types
	if t == reflect.TypeFor[time.Time]() {
		return map[string]any{
			"type":   "string",
			"format": "date-time",
		}
	}

	if t == reflect.TypeFor[time.Duration]() {
		return map[string]any{
			"type":        "string",
			"description": "Duration string (e.g., '1h30m', '2s')",
		}
	}

	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return map[string]any{"type": "integer"}

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}

	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}

	case reflect.Bool:
		return map[string]any{"type": "boolean"}

	case reflect.Slice, reflect.Array:
		return map[string]any{
			"type":  "array",
			"items": generateJSONSchema(t.Elem()),
		}

	case reflect.Map:
		return map[string]any{
			"type":                 "object",
			"additionalProperties": generateJSONSchema(t.Elem()),
		}

	case reflect.Struct:
		properties := make(map[string]any)
		required := make([]string, 0)

		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)

			// Skip unexported fields
			if !field.IsExported() {
				continue
			}

			// Get JSON tag
			jsonTag := field.Tag.Get("json")
			if jsonTag == "-" {
				continue
			}

			fieldName := field.Name
			omitempty := false

			if jsonTag != "" {
				parts := strings.Split(jsonTag, ",")
				if parts[0] != "" {
					fieldName = parts[0]
				}
				for _, part := range parts[1:] {
					if part == "omitempty" {
						omitempty = true
					}
				}
			}

			fieldSchema := generateJSONSchema(field.Type)

			// Add description from struct tag if present
			if desc := field.Tag.Get("description"); desc != "" {
				fieldSchema["description"] = desc
			}

			properties[fieldName] = fieldSchema

			// Required if not omitempty and not a pointer
			if !omitempty && field.Type.Kind() != reflect.Ptr {
				required = append(required, fieldName)
			}
		}

		schema := map[string]any{
			"type":       "object",
			"properties": properties,
		}

		if len(required) > 0 {
			schema["required"] = required
		}

		return schema

	default:
		return map[string]any{}
	}
}
