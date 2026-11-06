package gent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"

	"github.com/rickchristie/gent/internal/jsonfields"
)

// DataContent is captured domain data, separate from authored text. Values use portable maps,
// slices, scalars, and number types. It does not retain a named Go type or its methods.
// Use DataPart to construct it. Value returns a read-only view owned by the part.
type DataContent struct {
	value any
}

func (DataContent) isContentPart() {}

// Value returns captured data. Do not change the returned maps or slices. The view remains valid
// while the part is retained. Integer and floating-point meanings survive saved-state recovery.
func (p DataContent) Value() any { return p.value }

// String returns the JSON representation used by text-only destinations. Data remains a distinct
// part in Gent state; adapters can choose a native structured representation instead.
func (p DataContent) String() string {
	data, err := json.Marshal(p.value)
	if err != nil {
		panic(fmt.Errorf("gent: invalid captured data: %w", err))
	}
	return string(data)
}

// DataPart captures a Go value with its JSON field names, nulls, and numeric meaning. Gent owns
// serialization; the caller needs no output-type registration. Capture rejects cycles, functions,
// channels, non-finite numbers, and invalid custom JSON. It returns no part on failure.
// The capture is a stable portable value, not restoration of the original Go type. TextPart lets
// the caller choose exact text instead. Later changes to the original value do not change it.
// A custom JSON encoder owns its representation. Its numeric tokens determine captured numeric
// meaning; Gent does not infer that meaning from the encoder's private Go fields or elements.
func DataPart(value any) (ContentPart, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("gent: capture data: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var captured any
	if err := decoder.Decode(&captured); err != nil {
		return nil, err
	}
	var capture dataCapture
	captured, err = capture.numbers(captured, reflect.ValueOf(value))
	if err != nil {
		return nil, fmt.Errorf("gent: capture data: %w", err)
	}
	return DataContent{value: captured}, nil
}

// Cache field paths only for this capture. Repeated array elements share type inspection without
// keeping application types alive in a global cache.
type dataCapture struct {
	fields jsonfields.Resolver
}

// JSON handles field selection, embedded-field conflicts, omitempty, and custom encodings.
// Reflection supplies the one fact JSON cannot retain: a whole-valued float is still a float.
// This pass changes the newly decoded tree in place; it does not clone the source object again.
func (capture *dataCapture) numbers(value any, source reflect.Value) (any, error) {
	source = jsonfields.Source(source)
	switch v := value.(type) {
	case json.Number:
		if source.IsValid() {
			switch source.Kind() {
			case reflect.Float32, reflect.Float64:
				if n, err := v.Float64(); err == nil {
					return n, nil
				}
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32,
				reflect.Uint64, reflect.Uintptr:
				if n, err := strconv.ParseUint(string(v), 10, 64); err == nil {
					return n, nil
				}
			}
		}
		if n, err := v.Int64(); err == nil {
			return n, nil
		}
		if n, err := strconv.ParseUint(string(v), 10, 64); err == nil {
			return n, nil
		}
		// Retain a custom JSON number exactly, including arbitrary precision integer tokens.
		return v, nil
	case map[string]any:
		fields, err := capture.fields.Object(source)
		if err != nil {
			return nil, err
		}
		for key, item := range v {
			v[key], err = capture.numbers(item, fields[key])
			if err != nil {
				return nil, err
			}
		}
	case []any:
		for i, item := range v {
			var element reflect.Value
			if source.IsValid() && (source.Kind() == reflect.Slice || source.Kind() == reflect.Array) &&
				i < source.Len() {
				element = source.Index(i)
			}
			var err error
			v[i], err = capture.numbers(item, element)
			if err != nil {
				return nil, err
			}
		}
	}
	return value, nil
}
