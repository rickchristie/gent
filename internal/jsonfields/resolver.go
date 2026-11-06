package jsonfields

import (
	"encoding"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// Resolver shares field paths within one capture or conversion. It avoids repeated type
// inspection without keeping application types alive in a global cache. The zero value is ready.
type Resolver struct {
	fields map[reflect.Type][]Field
}

// Fields returns the selected JSON fields in declaration order, including embedded paths.
func (resolver *Resolver) Fields(typ reflect.Type) []Field {
	if fields, found := resolver.fields[typ]; found {
		return fields
	}
	if resolver.fields == nil {
		resolver.fields = make(map[reflect.Type][]Field)
	}
	fields := Fields(typ)
	resolver.fields[typ] = fields
	return fields
}

// DecodeField uses JSON names, with exact matches before folded matches, as encoding/json does.
// A renamed Go field must not take precedence over another field with the requested JSON name.
func (resolver *Resolver) DecodeField(typ reflect.Type, name string) (Field, bool) {
	fields := resolver.Fields(typ)
	for _, field := range fields {
		if field.Name == name {
			return field, true
		}
	}
	for _, field := range fields {
		if strings.EqualFold(field.Name, name) {
			return field, true
		}
	}
	return Field{}, false
}

// Object maps encoded property names to ordinary source values from Source. JSON field selection
// and map-key encoding must agree for capture and validation; Go names alone are insufficient.
func (resolver *Resolver) Object(source reflect.Value) (map[string]reflect.Value, error) {
	if !source.IsValid() {
		return nil, nil
	}
	switch source.Kind() {
	case reflect.Map:
		fields := make(map[string]reflect.Value, source.Len())
		iter := source.MapRange()
		for iter.Next() {
			name, err := mapKey(iter.Key())
			if err != nil {
				return nil, err
			}
			fields[name] = iter.Value()
		}
		return fields, nil
	case reflect.Struct:
		fields := resolver.Fields(source.Type())
		values := make(map[string]reflect.Value, len(fields))
		for _, field := range fields {
			value := source
			for _, index := range field.Index {
				if value.Kind() == reflect.Pointer {
					if value.IsNil() {
						value = reflect.Value{}
						break
					}
					value = value.Elem()
				}
				value = value.Field(index)
			}
			values[field.Name] = value
		}
		return values, nil
	default:
		return nil, nil
	}
}

// JSON uses string kinds first, then TextMarshaler, then integer formatting. In particular,
// a named integer key can supply a text encoding instead of its decimal value.
func mapKey(key reflect.Value) (string, error) {
	if key.Kind() == reflect.String {
		return key.String(), nil
	}
	if key.CanInterface() {
		if encoded, ok := key.Interface().(encoding.TextMarshaler); ok {
			if key.Kind() == reflect.Pointer && key.IsNil() {
				return "", nil
			}
			text, err := encoded.MarshalText()
			return string(text), err
		}
	}
	switch key.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(key.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(key.Uint(), 10), nil
	default:
		return "", fmt.Errorf("unsupported JSON map key %s", key.Type())
	}
}
