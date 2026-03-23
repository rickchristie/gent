package toolchain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/rickchristie/gent"
)

// ToolMeta holds metadata about a registered tool extracted via reflection.
type ToolMeta struct {
	name         string
	description  string
	policy       string
	schema       map[string]any
	outputSchema map[string]any
	tool         any          // The actual tool (Tool[I, O])
	inputType    reflect.Type // The input type I
}

// Name returns the tool's name.
func (m *ToolMeta) Name() string { return m.name }

// Description returns the tool's description.
func (m *ToolMeta) Description() string { return m.description }

// Policy returns the tool's usage policy.
func (m *ToolMeta) Policy() string { return m.policy }

// Schema returns the tool's parameter schema.
func (m *ToolMeta) Schema() map[string]any { return m.schema }

// OutputSchema returns the tool's output schema, auto-generated from
// the Call method's return type. May be nil if the output type cannot
// be introspected.
func (m *ToolMeta) OutputSchema() map[string]any { return m.outputSchema }

// Tool returns the actual tool.
func (m *ToolMeta) Tool() any { return m.tool }

// TransformArgsReflect transforms raw args (map[string]any) to the tool's typed input.
//
// It converts args to the tool's input type. The conversion handles type coercion from
// JSON Schema intermediary types to Go types:
//   - string -> time.Time: Parses using common date/time formats (RFC3339, date-only, etc.)
//   - string -> time.Duration: Parses using time.ParseDuration (e.g., "1h30m", "2h45m30s")
//   - If target field is `any`, the intermediary value is used as-is
//
// Returns the typed input as `any`. The actual type is the tool's input type I.
func TransformArgsReflect(tool any, args map[string]any) (any, error) {
	toolVal := reflect.ValueOf(tool)
	if !toolVal.IsValid() {
		return nil, errors.New("invalid tool value")
	}

	// Get the Call method to determine input type
	callMethod := toolVal.MethodByName("Call")
	if !callMethod.IsValid() {
		return nil, errors.New("tool does not have Call method")
	}

	// Get the input type from Call method signature: Call(ctx, input I) (*ToolResult[O], error)
	callType := callMethod.Type()
	if callType.NumIn() != 2 {
		return nil, fmt.Errorf(
			"Call method has unexpected signature: expected 2 params, got %d",
			callType.NumIn(),
		)
	}
	inputType := callType.In(1) // ctx is 0, input is 1

	// Get the actual struct type (dereference if pointer)
	structType := inputType
	if structType.Kind() == reflect.Ptr {
		structType = structType.Elem()
	}

	// Pre-process args to convert intermediary types to Go types based on target struct
	convertedArgs := convertArgsForType(args, structType)

	// Create new instance of input type and unmarshal args into it
	var inputVal reflect.Value
	if inputType.Kind() == reflect.Ptr {
		// If input is pointer type, create the underlying type and take pointer
		inputVal = reflect.New(inputType.Elem())
	} else {
		// For non-pointer types, create a pointer for unmarshaling, then get the value
		inputVal = reflect.New(inputType)
	}

	// Marshal args to JSON, then unmarshal into input
	argsJSON, err := json.Marshal(convertedArgs)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal args: %w", err)
	}
	if err := json.Unmarshal(argsJSON, inputVal.Interface()); err != nil {
		return nil, fmt.Errorf("failed to unmarshal args into input type: %w", err)
	}

	// Return the actual value (pointer or value depending on input type)
	if inputType.Kind() == reflect.Ptr {
		return inputVal.Interface(), nil
	}
	return inputVal.Elem().Interface(), nil
}

// ToolCallOutput holds the result of calling a tool via reflection.
// This is an internal type used by toolchain implementations to collect
// tool outputs before formatting.
type ToolCallOutput struct {
	Name         string             // Tool name
	Text         any                // Raw typed text output (will be formatted as JSON/YAML)
	Media        []gent.ContentPart // Media content (images, audio, etc.)
	Instructions string             // Optional follow-up instructions for the LLM
}

// CallToolWithTypedInputReflect calls a generic Tool[I, TextOutput] with already-typed input.
//
// The typedInput must be the correct type for the tool's input type I.
// Use TransformArgsReflect to convert map[string]any to the typed input first.
//
// Returns a ToolCallOutput containing the tool name, text output, and any media.
func CallToolWithTypedInputReflect(
	ctx context.Context,
	tool any,
	typedInput any,
) (*ToolCallOutput, error) {
	toolVal := reflect.ValueOf(tool)
	if !toolVal.IsValid() {
		return nil, errors.New("invalid tool value")
	}

	// Get the Call method
	callMethod := toolVal.MethodByName("Call")
	if !callMethod.IsValid() {
		return nil, errors.New("tool does not have Call method")
	}

	// Get Name method for result
	nameMethod := toolVal.MethodByName("Name")
	if !nameMethod.IsValid() {
		return nil, errors.New("tool does not have Name method")
	}
	nameResult := nameMethod.Call(nil)
	toolName := nameResult[0].String()

	// Call the method
	results := callMethod.Call([]reflect.Value{
		reflect.ValueOf(ctx),
		reflect.ValueOf(typedInput),
	})

	// Handle results: (*ToolResult[TextOutput], error)
	resultVal := results[0]
	errVal := results[1]

	// Check error
	if !errVal.IsNil() {
		return nil, errVal.Interface().(error)
	}

	// Extract from *ToolResult[TextOutput]
	if resultVal.IsNil() {
		return nil, errors.New("nil result from tool")
	}

	resultStruct := resultVal.Elem()
	textField := resultStruct.FieldByName("Text")
	mediaField := resultStruct.FieldByName("Media")
	instructionsField := resultStruct.FieldByName("Instructions")

	var media []gent.ContentPart
	if mediaField.IsValid() && !mediaField.IsNil() {
		media = mediaField.Interface().([]gent.ContentPart)
	}

	var instructions string
	if instructionsField.IsValid() {
		instructions = instructionsField.String()
	}

	return &ToolCallOutput{
		Name:         toolName,
		Text:         textField.Interface(),
		Media:        media,
		Instructions: instructions,
	}, nil
}

// CallToolReflect calls a generic Tool[I, TextOutput] using reflection.
//
// It converts args (map[string]any) to the tool's input type. The conversion handles
// type coercion from JSON Schema intermediary types to Go types:
//   - string -> time.Time: Parses using common date/time formats (RFC3339, date-only, etc.)
//   - string -> time.Duration: Parses using time.ParseDuration (e.g., "1h30m", "2h45m30s")
//   - If target field is `any`, the intermediary value is used as-is
//
// This is a convenience function that combines TransformArgsReflect and
// CallToolWithTypedInputReflect.
func CallToolReflect(
	ctx context.Context,
	tool any,
	args map[string]any,
) (*ToolCallOutput, error) {
	typedInput, err := TransformArgsReflect(tool, args)
	if err != nil {
		return nil, err
	}
	return CallToolWithTypedInputReflect(ctx, tool, typedInput)
}

// convertArgsForType converts intermediary arg values to match the expected Go types
// in the target struct. This handles conversions that Go's json package doesn't support:
//   - string -> time.Time (various formats)
//   - string -> time.Duration
func convertArgsForType(args map[string]any, structType reflect.Type) map[string]any {
	if args == nil || structType.Kind() != reflect.Struct {
		return args
	}

	result := make(map[string]any, len(args))
	for key, value := range args {
		result[key] = convertValueForField(value, structType, key)
	}
	return result
}

// convertValueForField converts a single value based on the target field's type.
func convertValueForField(value any, structType reflect.Type, fieldName string) any {
	// Find the field in the struct (check both exact name and json tag)
	field, found := findFieldByName(structType, fieldName)
	if !found {
		return value // Unknown field, return as-is
	}

	return convertValueToType(value, field.Type)
}

// findFieldByName finds a struct field by name or json tag.
func findFieldByName(structType reflect.Type, name string) (reflect.StructField, bool) {
	for i := range structType.NumField() {
		field := structType.Field(i)

		// Check json tag first
		jsonTag := field.Tag.Get("json")
		if jsonTag != "" {
			// Handle "fieldname,omitempty" style tags
			tagName := jsonTag
			if comma := findComma(jsonTag); comma >= 0 {
				tagName = jsonTag[:comma]
			}
			if tagName == name {
				return field, true
			}
		}

		// Check field name (case-insensitive for JSON compatibility)
		if equalFold(field.Name, name) {
			return field, true
		}
	}
	return reflect.StructField{}, false
}

// findComma returns the index of the first comma in s, or -1 if not found.
func findComma(s string) int {
	for i := range len(s) {
		if s[i] == ',' {
			return i
		}
	}
	return -1
}

// equalFold reports whether s and t are equal under case-folding.
func equalFold(s, t string) bool {
	if len(s) != len(t) {
		return false
	}
	for i := range len(s) {
		c1, c2 := s[i], t[i]
		if c1 != c2 {
			// Convert to lowercase
			if c1 >= 'A' && c1 <= 'Z' {
				c1 += 'a' - 'A'
			}
			if c2 >= 'A' && c2 <= 'Z' {
				c2 += 'a' - 'A'
			}
			if c1 != c2 {
				return false
			}
		}
	}
	return true
}

// convertValueToType converts a value to match the target type.
func convertValueToType(value any, targetType reflect.Type) any {
	if value == nil {
		return nil
	}

	// Handle pointer types - get the underlying type
	elemType := targetType
	if targetType.Kind() == reflect.Ptr {
		elemType = targetType.Elem()
	}

	// Check for time.Time
	if elemType == reflect.TypeOf(time.Time{}) {
		if str, ok := value.(string); ok {
			if t, err := parseTime(str); err == nil {
				return t.Format(time.RFC3339Nano)
			}
		}
		// If value is already time.Time (from YAML), format it for JSON
		if t, ok := value.(time.Time); ok {
			return t.Format(time.RFC3339Nano)
		}
		return value
	}

	// Check for time.Duration
	if elemType == reflect.TypeOf(time.Duration(0)) {
		if str, ok := value.(string); ok {
			if d, err := time.ParseDuration(str); err == nil {
				// JSON unmarshal expects nanoseconds as int64
				return d.Nanoseconds()
			}
		}
		return value
	}

	// Handle nested structs
	if elemType.Kind() == reflect.Struct {
		if m, ok := value.(map[string]any); ok {
			return convertArgsForType(m, elemType)
		}
	}

	// Handle slices
	if elemType.Kind() == reflect.Slice {
		if arr, ok := value.([]any); ok {
			elemElemType := elemType.Elem()
			result := make([]any, len(arr))
			for i, item := range arr {
				result[i] = convertValueToType(item, elemElemType)
			}
			return result
		}
	}

	return value
}

// parseTime attempts to parse a time string using common formats.
// Supports: RFC3339, RFC3339Nano, date-only, datetime with space, and more.
func parseTime(s string) (time.Time, error) {
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",      // ISO without timezone
		"2006-01-02 15:04:05",      // Datetime with space
		"2006-01-02 15:04:05Z07:00", // Datetime with space and timezone
		"2006-01-02",               // Date only
		"2006-01-02T15:04",         // ISO without seconds
		"2006-01-02 15:04",         // Datetime without seconds
	}

	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse time: %s", s)
}

// GetToolMeta extracts metadata from a generic Tool[I, O] using reflection.
func GetToolMeta(tool any) (*ToolMeta, error) {
	toolVal := reflect.ValueOf(tool)
	if !toolVal.IsValid() {
		return nil, errors.New("invalid tool value")
	}

	// Get Name
	nameMethod := toolVal.MethodByName("Name")
	if !nameMethod.IsValid() {
		return nil, errors.New("tool does not have Name method")
	}
	name := nameMethod.Call(nil)[0].String()

	// Get Description
	descMethod := toolVal.MethodByName("Description")
	if !descMethod.IsValid() {
		return nil, errors.New("tool does not have Description method")
	}
	description := descMethod.Call(nil)[0].String()

	// Get Policy
	policyMethod := toolVal.MethodByName("Policy")
	if !policyMethod.IsValid() {
		return nil, errors.New("tool does not have Policy method")
	}
	policy := policyMethod.Call(nil)[0].String()

	// Get ParameterSchema
	schemaMethod := toolVal.MethodByName("ParameterSchema")
	if !schemaMethod.IsValid() {
		return nil, errors.New("tool does not have ParameterSchema method")
	}
	schemaResult := schemaMethod.Call(nil)[0]
	var schema map[string]any
	if !schemaResult.IsNil() {
		schema = schemaResult.Interface().(map[string]any)
	}

	// Get input type from Call method
	callMethod := toolVal.MethodByName("Call")
	if !callMethod.IsValid() {
		return nil, errors.New("tool does not have Call method")
	}
	callType := callMethod.Type()
	if callType.NumIn() != 2 {
		return nil, fmt.Errorf("Call method has unexpected signature")
	}
	inputType := callType.In(1)

	// Extract output schema from Call return type: (*ToolResult[O], error).
	// ToolResult[O] has a Text field of type O — we generate schema from O.
	var outputSchema map[string]any
	if callType.NumOut() >= 1 {
		resultType := callType.Out(0) // *ToolResult[O]
		if resultType.Kind() == reflect.Ptr {
			resultType = resultType.Elem()
		}
		// ToolResult has a Text field whose type is O.
		if textField, ok := resultType.FieldByName("Text"); ok {
			outputSchema = schemaFromType(textField.Type)
		}
	}

	return &ToolMeta{
		name:         name,
		description:  description,
		policy:       policy,
		schema:       schema,
		outputSchema: outputSchema,
		tool:         tool,
		inputType:    inputType,
	}, nil
}

// schemaFromType generates a JSON Schema from a Go reflect.Type.
// Handles common types: string, numeric, bool, struct (with json tags),
// slice, pointer. Returns nil for unsupported types.
func schemaFromType(t reflect.Type) map[string]any {
	if t == nil {
		return nil
	}
	// Dereference pointers.
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16,
		reflect.Int32, reflect.Int64:
		return map[string]any{"type": "integer"}
	case reflect.Uint, reflect.Uint8, reflect.Uint16,
		reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Slice:
		items := schemaFromType(t.Elem())
		if items == nil {
			return map[string]any{"type": "array"}
		}
		return map[string]any{"type": "array", "items": items}
	case reflect.Struct:
		return schemaFromStruct(t)
	case reflect.Interface:
		return nil // can't introspect interface{}
	default:
		return nil
	}
}

// schemaFromStruct generates a JSON Schema object from a Go struct type.
// Uses json struct tags for property names. Skips unexported fields and
// fields with json:"-".
func schemaFromStruct(t reflect.Type) map[string]any {
	props := map[string]any{}
	for i := range t.NumField() {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		jsonTag := field.Tag.Get("json")
		if jsonTag == "-" {
			continue
		}
		name := field.Name
		if jsonTag != "" {
			parts := strings.SplitN(jsonTag, ",", 2)
			if parts[0] != "" {
				name = parts[0]
			}
		}
		fieldSchema := schemaFromType(field.Type)
		if fieldSchema != nil {
			props[name] = fieldSchema
		}
	}
	if len(props) == 0 {
		return map[string]any{"type": "object"}
	}
	return map[string]any{
		"type":       "object",
		"properties": props,
	}
}
