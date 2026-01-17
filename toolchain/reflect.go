package toolchain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/rickchristie/gent"
)

// ToolMeta holds metadata about a registered tool extracted via reflection.
type ToolMeta struct {
	name        string
	description string
	schema      map[string]any
	tool        any          // The actual tool (Tool[I, O])
	inputType   reflect.Type // The input type I
}

// Name returns the tool's name.
func (m *ToolMeta) Name() string { return m.name }

// Description returns the tool's description.
func (m *ToolMeta) Description() string { return m.description }

// Schema returns the tool's parameter schema.
func (m *ToolMeta) Schema() map[string]any { return m.schema }

// Tool returns the actual tool.
func (m *ToolMeta) Tool() any { return m.tool }

// CallToolReflect calls a generic Tool[I, O] using reflection.
// It converts args (map[string]any) to the tool's input type and calls the tool.
func CallToolReflect(
	ctx context.Context,
	tool any,
	args map[string]any,
) (*gent.ToolCallResult, error) {
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

	// Get the input type from Call method signature: Call(ctx, input I) (*ToolResult[O], error)
	callType := callMethod.Type()
	if callType.NumIn() != 2 {
		return nil, fmt.Errorf(
			"Call method has unexpected signature: expected 2 params, got %d",
			callType.NumIn(),
		)
	}
	inputType := callType.In(1) // ctx is 0, input is 1

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
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal args: %w", err)
	}
	if err := json.Unmarshal(argsJSON, inputVal.Interface()); err != nil {
		return nil, fmt.Errorf("failed to unmarshal args into input type: %w", err)
	}

	// Get the actual value to pass (pointer or value depending on input type)
	var inputToPass reflect.Value
	if inputType.Kind() == reflect.Ptr {
		inputToPass = inputVal
	} else {
		inputToPass = inputVal.Elem()
	}

	// Call the method
	results := callMethod.Call([]reflect.Value{
		reflect.ValueOf(ctx),
		inputToPass,
	})

	// Handle results: (*ToolResult[O], error)
	resultVal := results[0]
	errVal := results[1]

	// Check error
	if !errVal.IsNil() {
		return nil, errVal.Interface().(error)
	}

	// Extract from *ToolResult[O]
	if resultVal.IsNil() {
		return nil, errors.New("nil result from tool")
	}

	resultStruct := resultVal.Elem()
	outputField := resultStruct.FieldByName("Output")
	resultField := resultStruct.FieldByName("Result")

	return &gent.ToolCallResult{
		Name:   toolName,
		Output: outputField.Interface(),
		Result: resultField.Interface().([]gent.ContentPart),
	}, nil
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

	return &ToolMeta{
		name:        name,
		description: description,
		schema:      schema,
		tool:        tool,
		inputType:   inputType,
	}, nil
}
