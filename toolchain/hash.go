package toolchain

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"reflect"
	"sort"
)

// ToolCallResultHash computes a deterministic hash from the
// tool name and arguments. Map keys are sorted to ensure
// consistent hashing regardless of iteration order.
func ToolCallResultHash(
	name string, args map[string]any,
) string {
	h := fnv.New64a()
	h.Write([]byte(name))
	h.Write([]byte{0}) // separator
	// Sort keys for deterministic ordering
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte{0})
		// JSON marshal the value for consistent representation
		b, _ := json.Marshal(args[k])
		h.Write(b)
		h.Write([]byte{0})
	}
	return fmt.Sprintf("%016x", h.Sum64())
}

// CallDeduplicateSummaryReflect calls a tool's
// DeduplicateSummary method via reflection.
// Returns "" if the tool doesn't have the method or
// if input/output types don't match.
func CallDeduplicateSummaryReflect(
	tool any, input any, output any,
) string {
	toolVal := reflect.ValueOf(tool)
	if !toolVal.IsValid() {
		return ""
	}

	method := toolVal.MethodByName("DeduplicateSummary")
	if !method.IsValid() {
		return ""
	}

	methodType := method.Type()
	if methodType.NumIn() != 2 || methodType.NumOut() != 1 {
		return ""
	}

	// Validate return type is string
	if methodType.Out(0).Kind() != reflect.String {
		return ""
	}

	// Build input values, handling nil gracefully
	inputVal, err := safeReflectValue(input, methodType.In(0))
	if err != nil {
		return ""
	}
	outputVal, err := safeReflectValue(output, methodType.In(1))
	if err != nil {
		return ""
	}

	results := method.Call([]reflect.Value{inputVal, outputVal})
	return results[0].String()
}

// safeReflectValue converts a value to a reflect.Value
// assignable to the given target type. Returns an error if
// the value is not assignable.
func safeReflectValue(
	val any, targetType reflect.Type,
) (reflect.Value, error) {
	if val == nil {
		return reflect.Zero(targetType), nil
	}
	v := reflect.ValueOf(val)
	if !v.Type().AssignableTo(targetType) {
		return reflect.Value{}, errors.New(
			"type mismatch",
		)
	}
	return v, nil
}
