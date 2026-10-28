package schema

import (
	"bytes"
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// FromType builds a schema from JSON field names and Go types. Input fields are required,
// including fields with omitempty. Use Field(...).Optional() to permit an absent input.
// Recursive types, embedded fields, and custom JSON encodings need an explicit schema.
func FromType(t reflect.Type) (map[string]any, error) {
	return inferType(t, false, map[reflect.Type]bool{})
}

// FromOutputType describes JSON output. It permits fields omitted by encoding/json.
func FromOutputType(t reflect.Type) (map[string]any, error) {
	return inferType(t, true, map[reflect.Type]bool{})
}

func inferType(t reflect.Type, output bool, active map[reflect.Type]bool) (map[string]any, error) {
	if active[t] {
		return nil, fmt.Errorf("recursive type %s needs an explicit schema", t)
	}
	active[t] = true
	defer delete(active, t)
	if t.Kind() == reflect.Pointer {
		value, err := inferType(t.Elem(), output, active)
		return nullable(value), err
	}
	if t == reflect.TypeFor[time.Time]() {
		return map[string]any{"type": "string", "format": "date-time"}, nil
	}
	if t == reflect.TypeFor[json.Number]() {
		return map[string]any{"type": "number"}, nil
	}
	marshaler := reflect.TypeFor[json.Marshaler]()
	unmarshaler := reflect.TypeFor[json.Unmarshaler]()
	textMarshaler := reflect.TypeFor[encoding.TextMarshaler]()
	textUnmarshaler := reflect.TypeFor[encoding.TextUnmarshaler]()
	if t.Implements(marshaler) || reflect.PointerTo(t).Implements(marshaler) ||
		t.Implements(unmarshaler) || reflect.PointerTo(t).Implements(unmarshaler) ||
		t.Implements(textMarshaler) || reflect.PointerTo(t).Implements(textMarshaler) ||
		t.Implements(textUnmarshaler) || reflect.PointerTo(t).Implements(textUnmarshaler) {
		return nil, fmt.Errorf("custom JSON type %s needs an explicit schema", t)
	}
	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}, nil
	case reflect.Bool:
		return map[string]any{"type": "boolean"}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return map[string]any{"type": "integer"}, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer", "minimum": 0}, nil
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}, nil
	case reflect.Interface:
		return map[string]any{}, nil
	case reflect.Struct:
		return inferStruct(t, output, active)
	case reflect.Slice, reflect.Array:
		if t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8 {
			return nullable(map[string]any{"type": "string", "contentEncoding": "base64"}), nil
		}
		items, err := inferType(t.Elem(), output, active)
		if err != nil {
			return nil, err
		}
		value := map[string]any{"type": "array", "items": items}
		if t.Kind() == reflect.Array {
			value["minItems"], value["maxItems"] = t.Len(), t.Len()
			return value, nil
		}
		return nullable(value), nil
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			return nil, fmt.Errorf("map type %s needs an explicit schema", t)
		}
		value, err := inferType(t.Elem(), output, active)
		if err != nil {
			return nil, err
		}
		return nullable(map[string]any{"type": "object", "additionalProperties": value}), nil
	default:
		return nil, fmt.Errorf("type %s needs an explicit schema", t)
	}
}

func inferStruct(
	t reflect.Type, output bool, active map[reflect.Type]bool,
) (map[string]any, error) {
	properties := map[string]any{}
	required := []string{}
	for i := range t.NumField() {
		field := t.Field(i)
		tag := strings.Split(field.Tag.Get("json"), ",")
		if tag[0] == "-" {
			continue
		}
		if field.Anonymous && tag[0] == "" {
			return nil, fmt.Errorf("embedded field %s needs an explicit schema", field.Name)
		}
		if !field.IsExported() {
			continue
		}
		name := tag[0]
		if name == "" {
			name = field.Name
		}
		if _, exists := properties[name]; exists {
			return nil, fmt.Errorf("duplicate JSON field %q", name)
		}
		optional := false
		for _, option := range tag[1:] {
			if option == "string" {
				return nil, fmt.Errorf("JSON string field %q needs an explicit schema", name)
			}
			optional = optional || option == "omitempty" || option == "omitzero"
		}
		value, err := inferType(field.Type, output, active)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", name, err)
		}
		properties[name] = value
		if !output || !optional {
			required = append(required, name)
		}
	}
	return map[string]any{"type": "object", "properties": properties,
		"required": required, "additionalProperties": false}, nil
}

func nullable(value map[string]any) map[string]any {
	return map[string]any{"anyOf": []any{value, map[string]any{"type": "null"}}}
}

func nonNull(value map[string]any) map[string]any {
	if options, ok := value["anyOf"].([]any); ok && len(options) == 2 {
		if null, ok := options[1].(map[string]any); ok && null["type"] == "null" {
			if first, ok := options[0].(map[string]any); ok {
				return first
			}
		}
	}
	return value
}

// Copy takes an owned JSON snapshot. It rejects values that cannot be sent to a model.
// Numbers use json.Number so schema constraints keep their exact JSON values.
func Copy(raw map[string]any) (map[string]any, error) {
	if raw == nil {
		return nil, nil
	}
	return copyJSON(raw)
}

// Convert named Go values to JSON types without rounding numeric constraints or enum values.
func copyJSON[T any](raw T) (T, error) {
	var result T
	data, err := json.Marshal(raw)
	if err != nil {
		return result, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		return result, err
	}
	return result, nil
}
