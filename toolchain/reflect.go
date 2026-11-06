package toolchain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/internal/jsonfields"
)

// ToolMeta holds metadata about a registered tool extracted via reflection.
type ToolMeta struct {
	name         string
	description  string
	policy       string
	strict       bool
	schema       map[string]any
	outputSchema map[string]any
	tool         any          // The actual tool (Tool[I])
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

// OutputSchema returns the explicit or builder-generated output schema. Nil omits it.
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

	// Call receives context and typed input, then returns a response and an error.
	callType := callMethod.Type()
	if callType.NumIn() != 2 {
		return nil, fmt.Errorf(
			"Call method has unexpected signature: expected 2 params, got %d",
			callType.NumIn(),
		)
	}
	inputType := callType.In(1) // ctx is 0, input is 1

	// Preserve the supplied argument tree while preparing Go-specific temporal values. Containers
	// use the same conversion at every depth, including a map used as the top-level input.
	var fields jsonfields.Resolver
	convertedArgs := convertValueToType(args, inputType, &fields)

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
	decoder := json.NewDecoder(bytes.NewReader(argsJSON))
	decoder.UseNumber()
	if err := decoder.Decode(inputVal.Interface()); err != nil {
		return nil, fmt.Errorf("failed to unmarshal args into input type: %w", err)
	}

	// Return the actual value (pointer or value depending on input type)
	if inputType.Kind() == reflect.Ptr {
		return inputVal.Interface(), nil
	}
	return inputVal.Elem().Interface(), nil
}

// CallToolWithTypedInputReflect invokes the registered typed method and preserves both returns.
// It does not normalize errors or discard an invalid response/error pair; the record path does.
func CallToolWithTypedInputReflect(ctx context.Context, tool any, input any) (
	*gent.ToolExecutionResponse, error,
) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	method := reflect.ValueOf(tool).MethodByName("Call")
	value := reflect.ValueOf(input)
	if !value.IsValid() || !value.Type().AssignableTo(method.Type().In(1)) {
		return nil, fmt.Errorf("effective input %T does not match %s", input, method.Type().In(1))
	}
	values := method.Call([]reflect.Value{reflect.ValueOf(ctx), value})
	var response *gent.ToolExecutionResponse
	var err error
	if !values[0].IsNil() {
		response = values[0].Interface().(*gent.ToolExecutionResponse)
	}
	if !values[1].IsNil() {
		err = values[1].Interface().(error)
	}
	return response, err
}

// CallToolReflect converts arguments and invokes Call. Chains validate the schema before use.
func CallToolReflect(ctx context.Context, tool any, args map[string]any) (
	*gent.ToolExecutionResponse, error,
) {
	input, err := TransformArgsReflect(tool, args)
	if err != nil {
		return nil, err
	}
	return CallToolWithTypedInputReflect(ctx, tool, input)
}

// convertValueToType converts a value to match the target type.
func convertValueToType(value any, targetType reflect.Type, fields *jsonfields.Resolver) any {
	if value == nil {
		return nil
	}

	// Handle pointer types - get the underlying type
	elemType := targetType
	for elemType.Kind() == reflect.Ptr {
		elemType = elemType.Elem()
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

	// The custom decoder owns its wire representation. Do not convert apparent temporal fields
	// or elements inside it; those wire positions need not match the underlying Go type.
	if jsonfields.CustomDecoder(targetType) {
		return value
	}

	// Handle nested structs
	if elemType.Kind() == reflect.Struct {
		if m, ok := value.(map[string]any); ok {
			result := make(map[string]any, len(m))
			for name, item := range m {
				result[name] = item
				if field, found := fields.DecodeField(elemType, name); found {
					result[name] = convertValueToType(item, field.Type, fields)
				}
			}
			return result
		}
	}

	if elemType.Kind() == reflect.Map {
		if values, ok := value.(map[string]any); ok {
			result := make(map[string]any, len(values))
			for key, item := range values {
				result[key] = convertValueToType(item, elemType.Elem(), fields)
			}
			return result
		}
	}

	if elemType.Kind() == reflect.Slice || elemType.Kind() == reflect.Array {
		if arr, ok := value.([]any); ok {
			elemElemType := elemType.Elem()
			result := make([]any, len(arr))
			for i, item := range arr {
				result[i] = convertValueToType(item, elemElemType, fields)
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
		"2006-01-02T15:04:05",       // ISO without timezone
		"2006-01-02 15:04:05",       // Datetime with space
		"2006-01-02 15:04:05Z07:00", // Datetime with space and timezone
		"2006-01-02",                // Date only
		"2006-01-02T15:04",          // ISO without seconds
		"2006-01-02 15:04",          // Datetime without seconds
	}

	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse time: %s", s)
}

// toolDefinition checks the non-generic part of gent.Tool before reflection reads Call.
type toolDefinition interface {
	gent.IndexableTool
	Policy() string
	Strict() bool
	ParameterSchema() map[string]any
	OutputSchema() map[string]any
	UnrecoverableErrorInstructions() string
}

// GetToolMeta validates the complete Tool[I] contract before dispatch. Reflection is limited to
// binding I; normal response handling uses the explicit shared response types.
func GetToolMeta(tool any) (*ToolMeta, error) {
	definition, ok := tool.(toolDefinition)
	if !ok {
		return nil, errors.New("tool must implement the full gent.Tool interface")
	}
	value := reflect.ValueOf(tool)
	call := value.MethodByName("Call")
	if !call.IsValid() {
		return nil, errors.New("tool has no Call method")
	}
	t := call.Type()
	if t.IsVariadic() || t.NumIn() != 2 || t.In(0) != reflect.TypeFor[context.Context]() ||
		t.NumOut() != 2 || t.Out(0) != reflect.TypeFor[*gent.ToolExecutionResponse]() ||
		t.Out(1) != reflect.TypeFor[error]() {
		return nil, errors.New("Call must return (*gent.ToolExecutionResponse, error)")
	}
	recovery := value.MethodByName("Recover")
	if !recovery.IsValid() {
		return nil, errors.New("tool has no typed Recover method")
	}
	rt := recovery.Type()
	if rt.IsVariadic() || rt.NumIn() != 2 || rt.In(0) != reflect.TypeFor[context.Context]() ||
		rt.NumOut() != 2 || rt.Out(0) != reflect.TypeFor[*gent.ToolRecoveryResponse]() ||
		rt.Out(1) != reflect.TypeFor[error]() || rt.In(1).Kind() != reflect.Pointer ||
		!strings.HasPrefix(rt.In(1).Elem().Name(), "ToolRecoveryRequest[") ||
		rt.In(1).Elem().PkgPath() != reflect.TypeFor[gent.ToolRecoveryResponse]().PkgPath() {
		return nil, errors.New("Recover must accept *gent.ToolRecoveryRequest[I]")
	}
	input, ok := rt.In(1).MethodByName("Input")
	if !ok || input.Type.Out(0) != t.In(1) {
		return nil, errors.New("Recover input must match Call input")
	}
	return &ToolMeta{name: definition.Name(), description: definition.Description(),
		policy: definition.Policy(), strict: definition.Strict(), schema: definition.ParameterSchema(),
		outputSchema: definition.OutputSchema(), tool: tool, inputType: t.In(1)}, nil
}

func recoverToolReflect(ctx context.Context, tool any,
	request *gent.ToolRecoveryRequest[gent.EncodedValue],
) (
	*gent.ToolRecoveryResponse, error,
) {
	method := reflect.ValueOf(tool).MethodByName("Recover")
	input, err := gent.BindToolRecoveryInput(ctx, method.Type().In(1), request)
	if err != nil {
		return nil, err
	}
	values := method.Call([]reflect.Value{reflect.ValueOf(ctx), reflect.ValueOf(input)})
	var response *gent.ToolRecoveryResponse
	if !values[0].IsNil() {
		response = values[0].Interface().(*gent.ToolRecoveryResponse)
	}
	if !values[1].IsNil() {
		err = values[1].Interface().(error)
	}
	return response, err
}
