package jsonfields

import (
	"encoding"
	"encoding/json"
	"reflect"
)

// Source returns the ordinary Go value that corresponds to an encoded JSON value. A custom
// encoder owns its complete representation, so its fields and indices cannot supply metadata
// for that representation. Return an invalid value at that boundary or for a nil pointer.
// Check addressability because encoding/json uses pointer-receiver methods only when possible.
func Source(value reflect.Value) reflect.Value {
	jsonEncoder := reflect.TypeFor[json.Marshaler]()
	textEncoder := reflect.TypeFor[encoding.TextMarshaler]()
	for value.IsValid() {
		typ := value.Type()
		if typ.Implements(jsonEncoder) || typ.Implements(textEncoder) {
			return reflect.Value{}
		}
		if value.Kind() != reflect.Pointer && value.CanAddr() {
			pointer := value.Addr().Type()
			if pointer.Implements(jsonEncoder) || pointer.Implements(textEncoder) {
				return reflect.Value{}
			}
		}
		if value.Kind() != reflect.Pointer && value.Kind() != reflect.Interface {
			return value
		}
		if value.IsNil() {
			return reflect.Value{}
		}
		value = value.Elem()
	}
	return value
}

// CustomDecoder reports whether JSON decoding owns this type's representation. Go permits
// UnmarshalJSON on either the value or its pointer; decoding creates an addressable destination.
func CustomDecoder(typ reflect.Type) bool {
	jsonDecoder := reflect.TypeFor[json.Unmarshaler]()
	textDecoder := reflect.TypeFor[encoding.TextUnmarshaler]()
	for {
		if typ.Implements(jsonDecoder) || typ.Implements(textDecoder) ||
			reflect.PointerTo(typ).Implements(jsonDecoder) ||
			reflect.PointerTo(typ).Implements(textDecoder) {
			return true
		}
		if typ.Kind() != reflect.Pointer {
			return false
		}
		typ = typ.Elem()
	}
}
