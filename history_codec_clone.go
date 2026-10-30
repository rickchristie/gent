package gent

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"time"
)

// A direct copy is used only where its result has the same type and value as a portable round
// trip. Custom codecs, errors, and times retain their existing normalization. An unsupported
// shape falls back to the portable path, which supplies its established validation error.
func (r *CodecRegistry) cloneValue(
	v reflect.Value, visiting map[any]bool, depth int,
) (reflect.Value, bool, error) {
	if depth > 100 {
		return reflect.Value{}, false, nil
	}
	if !v.IsValid() {
		return v, true, nil
	}
	if v.Kind() == reflect.Interface {
		if v.IsNil() {
			return reflect.Zero(v.Type()), true, nil
		}
		return r.cloneValue(v.Elem(), visiting, depth+1)
	}
	registration, registered := r.lookupType(v.Type())
	if v.Type() == reflect.TypeFor[json.RawMessage]() {
		if !v.IsNil() && !json.Valid(v.Bytes()) {
			return reflect.Value{}, false, nil
		}
		return reflect.ValueOf(json.RawMessage(bytes.Clone(v.Bytes()))), true, nil
	}
	if v.Type() == reflect.TypeFor[EncodedValue]() {
		value := v.Interface().(EncodedValue)
		// The portable wrapper compacts a nested JSON payload. Preserve that behavior without
		// re-encoding its type and version or parsing its fields again.
		if len(value.Data) > 0 {
			var buffer bytes.Buffer
			if err := json.Compact(&buffer, value.Data); err != nil {
				return reflect.Value{}, false, nil
			}
			var escaped bytes.Buffer
			json.HTMLEscape(&escaped, buffer.Bytes())
			value.Data = escaped.Bytes()
		} else {
			value.Data = nil
		}
		return reflect.ValueOf(value), true, nil
	}
	_, isError := v.Interface().(error)
	if (registered && registration.codec != nil) || isError ||
		v.Type() == reflect.TypeFor[time.Time]() {
		value, err := r.cloneThroughCodec(v.Interface())
		return reflect.ValueOf(value), true, err
	}
	if v.Kind() == reflect.Pointer || v.Kind() == reflect.Map || v.Kind() == reflect.Slice {
		if !v.IsNil() {
			key := struct {
				typ     reflect.Type
				pointer uintptr
			}{v.Type(), v.Pointer()}
			if visiting[key] {
				return reflect.Value{}, false, nil
			}
			visiting[key] = true
			defer delete(visiting, key)
		}
	}
	switch v.Kind() {
	case reflect.Struct:
		if !registered {
			return reflect.Value{}, false, nil
		}
		return r.cloneStruct(v, visiting, depth)
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
		if v.Type().Name() != "" && !registered {
			return reflect.Value{}, false, nil
		}
		if _, err := r.typeName(v.Type().Elem()); err != nil {
			return reflect.Value{}, false, nil
		}
		return r.cloneContainer(v, visiting, depth)
	case reflect.Float32, reflect.Float64:
		if math.IsNaN(v.Float()) || math.IsInf(v.Float(), 0) {
			return reflect.Value{}, false, nil
		}
		fallthrough
	case reflect.Bool, reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32,
		reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		// json.Number also needs JSON syntax validation. Plain strings and numeric values are
		// immutable; returning their reflect.Value does not expose a mutable reference.
		if v.Type() == reflect.TypeFor[json.Number]() {
			value, err := r.cloneThroughCodec(v.Interface())
			return reflect.ValueOf(value), true, err
		}
		return v, registered, nil
	default:
		return reflect.Value{}, false, nil
	}
}

func (r *CodecRegistry) cloneStruct(
	v reflect.Value, visiting map[any]bool, depth int,
) (reflect.Value, bool, error) {
	result := reflect.New(v.Type()).Elem()
	for i := range v.NumField() {
		field := v.Type().Field(i)
		if field.PkgPath != "" {
			return reflect.Value{}, false, nil
		}
		if field.Type.Kind() == reflect.Func && field.Tag.Get("json") == "-" {
			if !v.Field(i).IsNil() {
				return reflect.Value{}, false, nil
			}
			continue
		}
		if ok, err := r.cloneInto(result.Field(i), v.Field(i), visiting, depth+1); !ok || err != nil {
			return reflect.Value{}, ok, err
		}
	}
	return result, true, nil
}

func (r *CodecRegistry) cloneInto(
	target, source reflect.Value, visiting map[any]bool, depth int,
) (bool, error) {
	value, ok, err := r.cloneValue(source, visiting, depth)
	if !ok || err != nil {
		return ok, err
	}
	if err := setDecoded(target, value); err != nil {
		return false, nil
	}
	return true, nil
}

func (r *CodecRegistry) cloneContainer(
	v reflect.Value, visiting map[any]bool, depth int,
) (reflect.Value, bool, error) {
	if v.Kind() == reflect.Map {
		return r.cloneMap(v, visiting, depth)
	}
	if v.Kind() != reflect.Array && v.IsNil() {
		return reflect.Zero(v.Type()), true, nil
	}
	if v.Kind() == reflect.Pointer {
		result := reflect.New(v.Type().Elem())
		ok, err := r.cloneInto(result.Elem(), v.Elem(), visiting, depth+1)
		return result.Convert(v.Type()), ok, err
	}
	if v.Kind() == reflect.Slice && v.Type().Elem() == reflect.TypeFor[byte]() {
		if err := r.checkBinarySize(v.Len()); err != nil {
			return reflect.Value{}, true, err
		}
		return reflect.ValueOf(bytes.Clone(v.Bytes())).Convert(v.Type()), true, nil
	}
	var result reflect.Value
	if v.Kind() == reflect.Array {
		result = reflect.New(v.Type()).Elem()
	} else {
		result = reflect.MakeSlice(v.Type(), v.Len(), v.Len())
	}
	for i := range v.Len() {
		if ok, err := r.cloneInto(result.Index(i), v.Index(i), visiting, depth+1); !ok || err != nil {
			return reflect.Value{}, ok, err
		}
	}
	return result, true, nil
}

func (r *CodecRegistry) cloneMap(
	v reflect.Value, visiting map[any]bool, depth int,
) (reflect.Value, bool, error) {
	if _, err := r.typeName(v.Type().Key()); err != nil {
		return reflect.Value{}, false, nil
	}
	if v.IsNil() {
		return reflect.Zero(v.Type()), true, nil
	}
	result := reflect.MakeMapWithSize(v.Type(), v.Len())
	iter := v.MapRange()
	for iter.Next() {
		key := reflect.New(v.Type().Key()).Elem()
		value := reflect.New(v.Type().Elem()).Elem()
		if ok, err := r.cloneInto(key, iter.Key(), visiting, depth+1); !ok || err != nil {
			return reflect.Value{}, ok, err
		}
		if ok, err := r.cloneInto(value, iter.Value(), visiting, depth+1); !ok || err != nil {
			return reflect.Value{}, ok, err
		}
		if !key.Comparable() || result.MapIndex(key).IsValid() {
			return reflect.Value{}, false, nil
		}
		result.SetMapIndex(key, value)
	}
	return result, true, nil
}
