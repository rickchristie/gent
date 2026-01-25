package section

import (
	"reflect"
	"strings"
	"time"
)

// GenerateJSONSchema creates a JSON Schema from a Go type using reflection.
// Supports: primitives, pointers, structs, slices, maps, time.Time, time.Duration.
func GenerateJSONSchema(t reflect.Type) map[string]any {
	if t == nil {
		return map[string]any{"type": "null"}
	}

	// Handle pointer types
	if t.Kind() == reflect.Ptr {
		schema := GenerateJSONSchema(t.Elem())
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
			"items": GenerateJSONSchema(t.Elem()),
		}

	case reflect.Map:
		return map[string]any{
			"type":                 "object",
			"additionalProperties": GenerateJSONSchema(t.Elem()),
		}

	case reflect.Struct:
		return generateStructSchema(t)

	default:
		return map[string]any{}
	}
}

// generateStructSchema creates a JSON Schema for a struct type.
func generateStructSchema(t reflect.Type) map[string]any {
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

		fieldSchema := GenerateJSONSchema(field.Type)

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
}
