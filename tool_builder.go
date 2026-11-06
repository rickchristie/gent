package gent

import (
	"context"
	"fmt"
	"reflect"
	"regexp"

	"github.com/rickchristie/gent/schema"
)

// ToolBuilder prepares a tool before registration. Setters transfer ownership of retained values.
// A builder is for setup in one goroutine; built tools can be shared between runs.
type ToolBuilder[I any] struct {
	tool          definedTool[I]
	fields        []*schema.FieldBuilder
	parametersSet bool
	outputType    reflect.Type
	outputError   error
}

// DefineTool starts a tool definition. The function can be a bound method on a service.
// Use Build to check the definition, or MustBuild for fixed application configuration.
func DefineTool[I any](
	name string, call func(context.Context, I) (*ToolExecutionResponse, error),
) *ToolBuilder[I] {
	return &ToolBuilder[I]{tool: definedTool[I]{name: name, call: call}}
}

// Description explains when to call the tool. The model receives it with the parameter schema.
func (b *ToolBuilder[I]) Description(value string) *ToolBuilder[I] {
	b.tool.description = value
	return b
}

// Parameters adds constraints to inferred fields. It cannot be mixed with ParameterSchema.
func (b *ToolBuilder[I]) Parameters(fields ...*schema.FieldBuilder) *ToolBuilder[I] {
	b.fields = append(b.fields, fields...)
	return b
}

// ParameterSchema replaces inference with a complete JSON Schema. Nil omits schema constraints.
// Prefer an inferred struct{} input for a tool that must receive no arguments.
func (b *ToolBuilder[I]) ParameterSchema(value map[string]any) *ToolBuilder[I] {
	b.tool.parameters, b.parametersSet = value, true
	return b
}

// OutputSchema sets optional domain-data guidance. Nil omits it. The last schema setter wins.
// It does not validate results. Tool.Call remains responsible for its output, including dynamic
// or custom JSON types.
func (b *ToolBuilder[I]) OutputSchema(value map[string]any) *ToolBuilder[I] {
	b.tool.output, b.outputType, b.outputError = value, nil, nil
	return b
}

// Strict asks the model provider to enforce the parameter schema. Gent validates supplied schemas
// locally. The provider must support strict tools and the schema keywords that you select.
func (b *ToolBuilder[I]) Strict(value bool) *ToolBuilder[I] {
	b.tool.strict = value
	return b
}

// Policy adds caller-authored usage guidance to the model definition. It does not enforce access.
func (b *ToolBuilder[I]) Policy(value string) *ToolBuilder[I] {
	b.tool.policy = value
	return b
}

// Domain groups tools for discovery, for example "shipping". It does not change execution.
func (b *ToolBuilder[I]) Domain(value string) *ToolBuilder[I] {
	b.tool.domain = value
	return b
}

// Categories supplies discovery labels. Keep them stable and specific to the tool's purpose.
func (b *ToolBuilder[I]) Categories(values ...string) *ToolBuilder[I] {
	b.tool.categories = values
	return b
}

// Keywords supplies terms that callers are likely to use when searching for this tool.
func (b *ToolBuilder[I]) Keywords(values ...string) *ToolBuilder[I] {
	b.tool.keywords = values
	return b
}

// SyntheticQueries supplies example requests for search indexing. These are not execution rules.
func (b *ToolBuilder[I]) SyntheticQueries(values ...string) *ToolBuilder[I] {
	b.tool.queries = values
	return b
}

// Recovery sets a repeatable inspection handler. Without one, unfinished calls receive safe
// recovery-error guidance. No recovery report authorizes automatic execution.
func (b *ToolBuilder[I]) Recovery(handler ToolRecoveryFunc[I]) *ToolBuilder[I] {
	b.tool.recover = handler
	return b
}

var toolNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// Build checks the name, callback, parameter schema, and optional output-schema declaration.
// Schema declaration errors are setup errors. No runtime domain output validation is installed.
func (b *ToolBuilder[I]) Build() (Tool[I], error) {
	if !toolNamePattern.MatchString(b.tool.name) {
		return nil, fmt.Errorf("invalid tool name %q: use 1 to 64 letters, digits, _ or -", b.tool.name)
	}
	if b.tool.call == nil {
		return nil, fmt.Errorf("tool %q: call handler is required", b.tool.name)
	}
	if b.parametersSet && len(b.fields) != 0 {
		return nil, fmt.Errorf("tool %q: Parameters and ParameterSchema cannot be combined", b.tool.name)
	}
	tool := b.tool
	var err error
	if !b.parametersSet {
		input := reflect.TypeFor[I]()
		for input.Kind() == reflect.Pointer {
			input = input.Elem()
		}
		if input.Kind() != reflect.Struct {
			return nil, fmt.Errorf("tool %q: non-struct input needs ParameterSchema", tool.name)
		}
		tool.parameters, err = schema.FromType(input)
		if err != nil {
			return nil, fmt.Errorf("tool %q parameters: %w", tool.name, err)
		}
		if err := schema.ApplyFields(tool.parameters, b.fields); err != nil {
			return nil, fmt.Errorf("tool %q parameters: %w", tool.name, err)
		}
	}
	if b.outputError != nil {
		return nil, b.outputError
	}
	if b.outputType != nil {
		tool.output, err = schema.FromOutputType(b.outputType)
		if err != nil {
			return nil, fmt.Errorf("tool %q output: %w", tool.name, err)
		}
	}
	tool.parameters, err = checkedSchema(tool.parameters)
	if err != nil {
		return nil, fmt.Errorf("tool %q parameters: %w", tool.name, err)
	}
	if tool.parameters != nil && tool.parameters["type"] != "object" {
		return nil, fmt.Errorf("tool %q: parameter schema must have type object", tool.name)
	}
	tool.output, err = checkedSchema(tool.output)
	if err != nil {
		return nil, fmt.Errorf("tool %q output: %w", tool.name, err)
	}
	return &tool, nil
}

func checkedSchema(raw map[string]any) (map[string]any, error) {
	compiled, err := schema.Compile(raw)
	if err != nil {
		return nil, err
	}
	// Reuse the compiler's normalized JSON tree. This preserves exact numeric constraints
	// without a second normalization or a defensive copy of the schema.
	return compiled.Raw(), nil
}

// MustBuild is for definitions fixed in application code. Use Build for user configuration.
func (b *ToolBuilder[I]) MustBuild() Tool[I] {
	tool, err := b.Build()
	if err != nil {
		panic(err)
	}
	return tool
}

type definedTool[I any] struct {
	name, description, policy, domain string
	categories, keywords, queries     []string
	parameters, output                map[string]any
	strict                            bool
	call                              func(context.Context, I) (*ToolExecutionResponse, error)
	errorInstructions                 string
	recover                           ToolRecoveryFunc[I]
}

func (t *definedTool[I]) Name() string               { return t.name }
func (t *definedTool[I]) Description() string        { return t.description }
func (t *definedTool[I]) Policy() string             { return t.policy }
func (t *definedTool[I]) Domain() string             { return t.domain }
func (t *definedTool[I]) Strict() bool               { return t.strict }
func (t *definedTool[I]) Categories() []string       { return t.categories }
func (t *definedTool[I]) Keywords() []string         { return t.keywords }
func (t *definedTool[I]) SyntheticQueries() []string { return t.queries }

func (t *definedTool[I]) ParameterSchema() map[string]any { return t.parameters }
func (t *definedTool[I]) OutputSchema() map[string]any    { return t.output }

func (t *definedTool[I]) UnrecoverableErrorInstructions() string { return t.errorInstructions }

func (t *definedTool[I]) Call(ctx context.Context, input I) (*ToolExecutionResponse, error) {
	return t.call(ctx, input)
}

func (t *definedTool[I]) Recover(ctx context.Context, request *ToolRecoveryRequest[I]) (
	*ToolRecoveryResponse, error,
) {
	if t.recover == nil {
		return nil, fmt.Errorf("tool %q has no recovery handler", t.name)
	}
	return t.recover(ctx, request)
}

// OutputSchemaFrom infers domain-data guidance from the Go type, not example field values.
// Typed nil pointers provide a type. Untyped nil is an inference error reported by Build.
// Custom encodings can require an explicit OutputSchema. Runtime results are not validated.
func (b *ToolBuilder[I]) OutputSchemaFrom(example any) *ToolBuilder[I] {
	b.tool.output, b.outputType, b.outputError = nil, reflect.TypeOf(example), nil
	if b.outputType == nil {
		b.outputError = fmt.Errorf("tool %q: output example has no type", b.tool.name)
	}
	return b
}

// UnrecoverableErrorInstructions sets authored model guidance for an unexpected Call error.
// Empty text selects execution guidance, then the built-in default. Raw errors remain private.
func (b *ToolBuilder[I]) UnrecoverableErrorInstructions(value string) *ToolBuilder[I] {
	b.tool.errorInstructions = value
	return b
}
