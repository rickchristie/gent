package gent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tmc/langchaingo/llms"
)

func sortEncodedItems(items []encodedMapItem) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Key.Type != items[j].Key.Type {
			return items[i].Key.Type < items[j].Key.Type
		}
		return bytes.Compare(items[i].Key.Data, items[j].Key.Data) < 0
	})
}

func (r *CodecRegistry) resolveType(name string, depth int) (reflect.Type, error) {
	if depth > 100 {
		return nil, errors.New("gent: type nesting exceeds 100")
	}
	switch name {
	case "any":
		return reflect.TypeFor[any](), nil
	case "content":
		return reflect.TypeFor[ContentPart](), nil
	case "llms-content":
		return reflect.TypeFor[llms.ContentPart](), nil
	case "error-interface":
		return reflect.TypeFor[error](), nil
	}
	if value, ok := r.lookupId(name); ok {
		return value.typ, nil
	}
	if strings.HasPrefix(name, "*") {
		typ, err := r.resolveType(name[1:], depth+1)
		if err != nil {
			return nil, err
		}
		return reflect.PointerTo(typ), nil
	}
	if strings.HasPrefix(name, "[]") {
		typ, err := r.resolveType(name[2:], depth+1)
		if err != nil {
			return nil, err
		}
		return reflect.SliceOf(typ), nil
	}
	if strings.HasPrefix(name, "[") {
		end := strings.Index(name, "]")
		if end < 2 {
			return nil, errors.New("gent: invalid array type")
		}
		length, err := strconv.Atoi(name[1:end])
		if err != nil || length < 0 || length > 1000000 {
			return nil, errors.New("gent: invalid array size")
		}
		element, err := r.resolveType(name[end+1:], depth+1)
		if err != nil {
			return nil, err
		}
		return reflect.ArrayOf(length, element), nil
	}
	if strings.HasPrefix(name, "map:") {
		var pair []string
		if err := json.Unmarshal([]byte(name[4:]), &pair); err != nil {
			return nil, err
		}
		if len(pair) != 2 {
			return nil, errors.New("gent: invalid map type")
		}
		key, err := r.resolveType(pair[0], depth+1)
		if err != nil {
			return nil, err
		}
		if !key.Comparable() {
			return nil, errors.New("gent: map key is not comparable")
		}
		element, err := r.resolveType(pair[1], depth+1)
		if err != nil {
			return nil, err
		}
		return reflect.MapOf(key, element), nil
	}
	return nil, fmt.Errorf("gent: unknown codec %q", name)
}

func (r *CodecRegistry) decode(value EncodedValue, depth int) (reflect.Value, error) {
	if depth > 100 {
		return reflect.Value{}, errors.New("gent: value nesting exceeds 100")
	}
	if !json.Valid(value.Data) {
		return reflect.Value{}, errors.New("gent: invalid encoded JSON")
	}
	registration, registered := r.lookupId(value.Type)
	wantVersion := 1
	if registered {
		wantVersion = registration.version
	}
	if value.Version != wantVersion {
		return reflect.Value{}, fmt.Errorf(
			"gent: unsupported %s version %d",
			value.Type,
			value.Version,
		)
	}
	switch value.Type {
	case "encoded-value":
		var saved EncodedValue
		err := json.Unmarshal(value.Data, &saved)
		return reflect.ValueOf(saved), err
	case "raw-json":
		return reflect.ValueOf(append(json.RawMessage(nil), value.Data...)), nil
	case "raw-json-nil":
		if string(value.Data) != "null" {
			return reflect.Value{}, errors.New("gent: invalid nil JSON encoding")
		}
		return reflect.ValueOf(json.RawMessage(nil)), nil
	case "nil":
		if string(value.Data) != "null" {
			return reflect.Value{}, errors.New("gent: invalid nil encoding")
		}
		return reflect.Value{}, nil
	case "time":
		var t time.Time
		err := json.Unmarshal(value.Data, &t)
		return reflect.ValueOf(t), err
	case "error":
		var saved *SavedError
		if err := json.Unmarshal(value.Data, &saved); err != nil {
			return reflect.Value{}, err
		}
		if saved == nil {
			return reflect.Value{}, nil
		}
		return reflect.ValueOf(saved), nil
	case "pointer", "slice", "array", "map":
		return r.decodeContainer(value, depth)
	}
	if !registered {
		return reflect.Value{}, fmt.Errorf("gent: unknown codec %q", value.Type)
	}
	if registration.codec != nil {
		v, err := registration.codec.Decode(append(json.RawMessage(nil), value.Data...))
		if err != nil {
			return reflect.Value{}, err
		}
		if reflect.TypeOf(v) != registration.typ {
			return reflect.Value{}, fmt.Errorf(
				"gent: codec %s decoded %T, expected %s",
				value.Type,
				v,
				registration.typ,
			)
		}
		return reflect.ValueOf(v), nil
	}
	result := reflect.New(registration.typ).Elem()
	switch result.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map, reflect.Pointer:
		var container EncodedValue
		if err := json.Unmarshal(value.Data, &container); err != nil {
			return reflect.Value{}, err
		}
		decoded, err := r.decode(container, depth+1)
		if err != nil {
			return reflect.Value{}, err
		}
		if !decoded.IsValid() || !decoded.Type().ConvertibleTo(result.Type()) {
			return reflect.Value{}, fmt.Errorf("gent: invalid named container %s", value.Type)
		}
		return decoded.Convert(result.Type()), nil
	}
	if result.Kind() != reflect.Struct {
		err := json.Unmarshal(value.Data, result.Addr().Interface())
		return result, err
	}
	var fields map[string]EncodedValue
	if err := json.Unmarshal(value.Data, &fields); err != nil {
		return reflect.Value{}, err
	}
	for name, field := range fields {
		target := result.FieldByName(name)
		if !target.IsValid() || !target.CanSet() {
			return reflect.Value{}, fmt.Errorf("gent: unknown field %s.%s", value.Type, name)
		}
		decoded, err := r.decode(field, depth+1)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("%s.%s: %w", value.Type, name, err)
		}
		if err := setDecoded(target, decoded); err != nil {
			return reflect.Value{}, err
		}
	}
	for i := 0; i < result.NumField(); i++ {
		field := result.Type().Field(i)
		if field.Type.Kind() == reflect.Func && field.Tag.Get("json") == "-" {
			continue
		}
		if _, ok := fields[field.Name]; !ok {
			return reflect.Value{}, fmt.Errorf("gent: missing field %s.%s", value.Type, field.Name)
		}
	}
	return result, nil
}

func setDecoded(target, value reflect.Value) error {
	if !value.IsValid() {
		switch target.Kind() {
		case reflect.Interface, reflect.Pointer, reflect.Slice, reflect.Map:
			target.SetZero()
			return nil
		default:
			return fmt.Errorf("gent: nil is not assignable to %s", target.Type())
		}
	}
	if !value.Type().AssignableTo(target.Type()) {
		return fmt.Errorf("gent: %s is not assignable to %s", value.Type(), target.Type())
	}
	target.Set(value)
	return nil
}

func (r *CodecRegistry) decodeContainer(value EncodedValue, depth int) (reflect.Value, error) {
	var c encodedContainer
	if err := json.Unmarshal(value.Data, &c); err != nil {
		return reflect.Value{}, err
	}
	element, err := r.resolveType(c.Element, depth+1)
	if err != nil {
		return reflect.Value{}, err
	}
	if c.Nil && (len(c.Values) != 0 || len(c.Items) != 0) {
		return reflect.Value{}, errors.New("gent: nil container has content")
	}
	var result reflect.Value
	switch value.Type {
	case "pointer":
		if c.Nil {
			return reflect.Zero(reflect.PointerTo(element)), nil
		}
		if len(c.Values) != 1 {
			return reflect.Value{}, errors.New("gent: invalid pointer encoding")
		}
		result = reflect.New(element)
		v, err := r.decode(c.Values[0], depth+1)
		if err != nil {
			return reflect.Value{}, err
		}
		return result, setDecoded(result.Elem(), v)
	case "map":
		key, err := r.resolveType(c.Key, depth+1)
		if err != nil {
			return reflect.Value{}, err
		}
		if !key.Comparable() {
			return reflect.Value{}, errors.New("gent: map key is not comparable")
		}
		typ := reflect.MapOf(key, element)
		if c.Nil {
			return reflect.Zero(typ), nil
		}
		result = reflect.MakeMapWithSize(typ, len(c.Items))
		for _, item := range c.Items {
			k, err := r.decode(item.Key, depth+1)
			if err != nil {
				return reflect.Value{}, err
			}
			v, err := r.decode(item.Value, depth+1)
			if err != nil {
				return reflect.Value{}, err
			}
			mapKey, mapValue := reflect.New(key).Elem(), reflect.New(element).Elem()
			if err := setDecoded(mapKey, k); err != nil {
				return reflect.Value{}, err
			}
			if err := setDecoded(mapValue, v); err != nil {
				return reflect.Value{}, err
			}
			if !mapKey.Comparable() {
				return reflect.Value{}, errors.New("gent: unhashable map key")
			}
			if result.MapIndex(mapKey).IsValid() {
				return reflect.Value{}, errors.New("gent: duplicate map key")
			}
			result.SetMapIndex(mapKey, mapValue)
		}
		return result, nil
	case "array":
		result = reflect.New(reflect.ArrayOf(len(c.Values), element)).Elem()
	default:
		if c.Nil {
			return reflect.Zero(reflect.SliceOf(element)), nil
		}
		result = reflect.MakeSlice(reflect.SliceOf(element), len(c.Values), len(c.Values))
	}
	for i, item := range c.Values {
		v, err := r.decode(item, depth+1)
		if err != nil {
			return reflect.Value{}, err
		}
		if err := setDecoded(result.Index(i), v); err != nil {
			return reflect.Value{}, err
		}
	}
	return result, nil
}
