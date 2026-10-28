package gent

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"slices"

	"github.com/rickchristie/gent/schema"
)

// ToolBuilder prepares a tool before registration. Build copies its schemas and metadata.
// A builder is for setup in one goroutine; built tools can be shared between runs.
type ToolBuilder[I, O any] struct {
	tool          definedTool[I, O]
	fields        []*schema.FieldBuilder
	parametersSet bool
	outputSet     bool
}

// DefineTool starts a tool definition. The function can be a bound method on a service.
// Use Build to check the definition, or MustBuild for fixed application configuration.
func DefineTool[I, O any](
	name string, call func(context.Context, I) (O, error),
) *ToolBuilder[I, O] {
	return &ToolBuilder[I, O]{tool: definedTool[I, O]{name: name, call: call}}
}

func (b *ToolBuilder[I, O]) Description(value string) *ToolBuilder[I, O] {
	b.tool.description = value
	return b
}

// Parameters adds constraints to inferred fields. It cannot be mixed with ParameterSchema.
func (b *ToolBuilder[I, O]) Parameters(fields ...*schema.FieldBuilder) *ToolBuilder[I, O] {
	b.fields = append(b.fields, fields...)
	return b
}

// ParameterSchema replaces inference with a complete JSON Schema. Nil omits schema constraints.
// Prefer an inferred struct{} input for a tool that must receive no arguments.
func (b *ToolBuilder[I, O]) ParameterSchema(value map[string]any) *ToolBuilder[I, O] {
	b.tool.parameters, b.parametersSet = value, true
	return b
}

// OutputSchema replaces output inference. Nil omits output documentation. It does not validate
// results: Tool.Call remains responsible for its output, including dynamic or custom JSON types.
func (b *ToolBuilder[I, O]) OutputSchema(value map[string]any) *ToolBuilder[I, O] {
	b.tool.output, b.outputSet = value, true
	return b
}

// Strict asks the model provider to enforce the parameter schema. Gent validates supplied schemas
// locally. The provider must support strict tools and the schema keywords that you select.
func (b *ToolBuilder[I, O]) Strict(value bool) *ToolBuilder[I, O] {
	b.tool.strict = value
	return b
}

func (b *ToolBuilder[I, O]) Policy(value string) *ToolBuilder[I, O] {
	b.tool.policy = value
	return b
}

func (b *ToolBuilder[I, O]) Domain(value string) *ToolBuilder[I, O] {
	b.tool.domain = value
	return b
}

func (b *ToolBuilder[I, O]) Categories(values ...string) *ToolBuilder[I, O] {
	b.tool.categories = slices.Clone(values)
	return b
}

func (b *ToolBuilder[I, O]) Keywords(values ...string) *ToolBuilder[I, O] {
	b.tool.keywords = slices.Clone(values)
	return b
}

func (b *ToolBuilder[I, O]) SyntheticQueries(values ...string) *ToolBuilder[I, O] {
	b.tool.queries = slices.Clone(values)
	return b
}

// Recovery sets the check for an interrupted operation. Without a handler, the result is
// Unresolved. A steering message alone never permits another attempt.
func (b *ToolBuilder[I, O]) Recovery(handler ToolRecoveryFunc) *ToolBuilder[I, O] {
	b.tool.recover = handler
	return b
}

// DeduplicateSummary sets the summary used for older identical calls. Leave it unset for
// changing data or operations with side effects.
func (b *ToolBuilder[I, O]) DeduplicateSummary(handler func(I, O) string) *ToolBuilder[I, O] {
	b.tool.summary = handler
	return b
}

var toolNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func (b *ToolBuilder[I, O]) Build() (Tool[I, O], error) {
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
	if !b.outputSet {
		tool.output, err = schema.FromOutputType(reflect.TypeFor[O]())
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
	tool.categories = slices.Clone(tool.categories)
	tool.keywords = slices.Clone(tool.keywords)
	tool.queries = slices.Clone(tool.queries)
	return &tool, nil
}

func checkedSchema(raw map[string]any) (map[string]any, error) {
	owned, err := schema.Copy(raw)
	if err != nil {
		return nil, err
	}
	if _, err := schema.Compile(owned); err != nil {
		return nil, err
	}
	return owned, nil
}

// MustBuild is for definitions fixed in application code. Use Build for user configuration.
func (b *ToolBuilder[I, O]) MustBuild() Tool[I, O] {
	tool, err := b.Build()
	if err != nil {
		panic(err)
	}
	return tool
}

type definedTool[I, O any] struct {
	name, description, policy, domain string
	categories, keywords, queries     []string
	parameters, output                map[string]any
	strict                            bool
	call                              func(context.Context, I) (O, error)
	summary                           func(I, O) string
	recover                           ToolRecoveryFunc
}

func (t *definedTool[I, O]) Name() string               { return t.name }
func (t *definedTool[I, O]) Description() string        { return t.description }
func (t *definedTool[I, O]) Policy() string             { return t.policy }
func (t *definedTool[I, O]) Domain() string             { return t.domain }
func (t *definedTool[I, O]) Strict() bool               { return t.strict }
func (t *definedTool[I, O]) Categories() []string       { return slices.Clone(t.categories) }
func (t *definedTool[I, O]) Keywords() []string         { return slices.Clone(t.keywords) }
func (t *definedTool[I, O]) SyntheticQueries() []string { return slices.Clone(t.queries) }

func (t *definedTool[I, O]) ParameterSchema() map[string]any { return copyToolSchema(t.parameters) }
func (t *definedTool[I, O]) OutputSchema() map[string]any    { return copyToolSchema(t.output) }

func (t *definedTool[I, O]) Call(ctx context.Context, input I) (*ToolResult[O], error) {
	output, err := t.call(ctx, input)
	if err != nil {
		return nil, err
	}
	return &ToolResult[O]{Text: output}, nil
}

func (t *definedTool[I, O]) Recover(ctx context.Context, request *ToolRecoveryRequest) (
	*ToolRecoveryResponse, error,
) {
	if t.recover == nil {
		return &ToolRecoveryResponse{Result: ToolRecoveryUnresolved}, nil
	}
	return t.recover(ctx, request)
}

func (t *definedTool[I, O]) DeduplicateSummary(input I, output O) string {
	if t.summary == nil {
		return ""
	}
	return t.summary(input, output)
}

// Build normalizes schemas to JSON values, so a recursive copy needs no reflection or encoding.
func copyToolSchema(raw map[string]any) map[string]any {
	if raw == nil {
		return nil
	}
	return copyToolSchemaValue(raw).(map[string]any)
}

func copyToolSchemaValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(value))
		for key, item := range value {
			result[key] = copyToolSchemaValue(item)
		}
		return result
	case []any:
		result := make([]any, len(value))
		for i, item := range value {
			result[i] = copyToolSchemaValue(item)
		}
		return result
	default:
		return value
	}
}
