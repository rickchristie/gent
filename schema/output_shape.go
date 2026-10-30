package schema

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// CheckOutputShape rejects lossy Go decoding: null scalars and padded or truncated fixed arrays.
// Use the same JSON field mapping as CheckOutputType. Schema validation is a separate step.
// The value must be a decoded JSON tree, and typ must first pass CheckOutputType during setup.
func CheckOutputShape(value any, typ reflect.Type) error {
	if outputJSONDecoder(typ) {
		return nil
	}
	if value == nil {
		if !outputNullable(typ) {
			return fmt.Errorf("null cannot be stored as %s", typ)
		}
		return nil
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if outputJSONDecoder(typ) {
		return nil
	}
	switch typ.Kind() {
	case reflect.Array, reflect.Slice:
		items, ok := value.([]any)
		if !ok {
			return nil // The decoder reports the type error, or handles []byte's base64 string.
		}
		if typ.Kind() == reflect.Array && len(items) != typ.Len() {
			return fmt.Errorf("array has %d elements; %s requires %d", len(items), typ, typ.Len())
		}
		for i, item := range items {
			if err := CheckOutputShape(item, typ.Elem()); err != nil {
				return fmt.Errorf("element %d: %w", i, err)
			}
		}
	case reflect.Map:
		if items, ok := value.(map[string]any); ok {
			for name, item := range items {
				if err := CheckOutputShape(item, typ.Elem()); err != nil {
					return fmt.Errorf("field %q: %w", name, err)
				}
			}
		}
	case reflect.Struct:
		return checkStructValues(value, typ)
	}
	return nil
}

func checkStructValues(value any, typ reflect.Type) error {
	items, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	fields := outputFields(typ)
	exact := make(map[string]outputField, len(fields))
	for _, field := range fields {
		exact[field.name] = field
	}
	names := make([]string, 0, len(items))
	for name := range items {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		field, found := exact[name]
		if !found {
			field, found = outputFoldedField(fields, name)
		}
		if found {
			if err := CheckOutputShape(items[name], field.typ); err != nil {
				return fmt.Errorf("field %q: %w", name, err)
			}
		}
	}
	return nil
}

func outputFoldedField(fields []outputField, name string) (outputField, bool) {
	for _, field := range fields {
		if strings.EqualFold(field.name, name) {
			return field, true
		}
	}
	return outputField{}, false
}
