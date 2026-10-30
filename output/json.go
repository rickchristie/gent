package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/schema"
)

// JsonOutput infers its schema once from T. Root pointers select the Go return type; they do not
// make the JSON root nullable. JSON field names and omission tags determine field presence.
type JsonOutput[T any] struct{ jsonOutput[T] }

// SchemaOutput uses the same explicit schema builders as tool parameters. T remains the direct
// result type. The schema is compiled and copied before the output is shared between executions.
type SchemaOutput[T any] struct{ jsonOutput[T] }

type jsonOutput[T any] struct {
	schema       *schema.Schema
	instructions string
	validator    customValidator[T]
}

func NewJsonOutput[T any](fields ...*schema.FieldBuilder) (*JsonOutput[T], error) {
	typ := reflect.TypeFor[T]()
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	raw, err := schema.FromOutputType(typ)
	if err != nil {
		return nil, fmt.Errorf("output: infer JSON schema: %w", err)
	}
	if err := schema.ApplyFields(raw, fields); err != nil {
		return nil, fmt.Errorf("output: configure JSON schema: %w", err)
	}
	processor, err := newJSONOutput[T](raw)
	if err != nil {
		return nil, err
	}
	return &JsonOutput[T]{jsonOutput: processor}, nil
}

func NewSchemaOutput[T any](raw map[string]any) (*SchemaOutput[T], error) {
	processor, err := newJSONOutput[T](raw)
	if err != nil {
		return nil, err
	}
	return &SchemaOutput[T]{jsonOutput: processor}, nil
}

func newJSONOutput[T any](raw map[string]any) (jsonOutput[T], error) {
	if raw == nil {
		return jsonOutput[T]{}, errors.New("output: JSON schema is required")
	}
	compiled, err := schema.Compile(raw)
	if err != nil {
		return jsonOutput[T]{}, fmt.Errorf("output: compile JSON schema: %w", err)
	}
	if err := compiled.CheckOutputType(reflect.TypeFor[T]()); err != nil {
		return jsonOutput[T]{}, fmt.Errorf("output: %w", err)
	}
	encoded, err := json.Marshal(compiled.Raw())
	if err != nil {
		return jsonOutput[T]{}, fmt.Errorf("output: describe JSON schema: %w", err)
	}
	return jsonOutput[T]{schema: compiled,
		instructions: "Return one JSON value that matches this schema: " + string(encoded)}, nil
}

func (o *JsonOutput[T]) SetCustomValidator(
	name string, callback gent.OutputCustomValidator[T],
) *JsonOutput[T] {
	o.validator = customValidator[T]{name: name, call: callback}
	return o
}

func (o *SchemaOutput[T]) SetCustomValidator(
	name string, callback gent.OutputCustomValidator[T],
) *SchemaOutput[T] {
	o.validator = customValidator[T]{name: name, call: callback}
	return o
}

func (o *jsonOutput[T]) RecoveryConfig() any {
	return map[string]any{"schema": o.schema.Raw(), "validator": o.validator.name}
}

func (o *jsonOutput[T]) Instructions() string { return o.instructions }

func (o *jsonOutput[T]) Configure(request *gent.ModelCallRequest) error {
	if err := o.validator.check(); err != nil {
		return err
	}
	request.Options.ResponseSchema = o.schema.Raw()
	request.Options.ResponseMIMEType = "application/json"
	request.Options.ResponseModalities = []string{"text"}
	return nil
}

func (o *jsonOutput[T]) Process(
	ctx *gent.ExecutionContext, content []gent.ContentPart,
) (processed *gent.OutputResult[T], processErr error) {
	local := true
	defer func() {
		if local {
			publishLocalResult(ctx, "json", processed, processErr)
		}
	}()
	if err := o.validator.check(); err != nil {
		return nil, err
	}
	if err := ctx.Context().Err(); err != nil {
		return nil, err
	}
	text, _, ok := textContent(content)
	if !ok {
		return reject[T]("The answer must contain JSON text only. Return one JSON value."), nil
	}
	var document any
	if err := decodeJSON(text, &document); err != nil {
		return reject[T](fmt.Sprintf("Invalid JSON: %v. Return one complete JSON value.", err)), nil
	}
	if err := o.schema.Validate(document); err != nil {
		return reject[T](fmt.Sprintf("The JSON does not match the schema: %v. Return corrected JSON.",
			err)), nil
	}
	if err := schema.CheckOutputShape(document, reflect.TypeFor[T]()); err != nil {
		return reject[T](fmt.Sprintf("The JSON cannot be stored as %s: %v. Return corrected JSON.",
			reflect.TypeFor[T](), err)), nil
	}
	// Schema validation uses an exact-number JSON tree to check presence and constraints. Go
	// decoding has a separate job: apply standard field/custom JSON mappings and range checks.
	// Both results are used; there is no answer-section parse or discarded validation pass.
	var value T
	if err := decodeJSON(text, &value); err != nil {
		return reject[T](fmt.Sprintf("The JSON cannot be stored as %s: %v. Return corrected JSON.",
			reflect.TypeFor[T](), err)), nil
	}
	// Delivery is derived from T, so custom JSON mappings cannot leave the displayed answer
	// different from the value returned to the caller. Validate that representation as well.
	delivery, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("output: encode JSON result: %w", err)
	}
	var delivered any
	if err := decodeJSON(string(delivery), &delivered); err != nil {
		return nil, err
	}
	if err := o.schema.Validate(delivered); err != nil {
		return reject[T](fmt.Sprintf(
			"The stored JSON value does not match the schema: %v. Return corrected JSON.", err)), nil
	}
	local = false
	ctx.PublishOutputValidation(gent.OutputValidationLocal, "json", true, nil, nil)
	if result, err := o.validator.validate(ctx, value); result != nil || err != nil {
		return result, err
	}
	return &gent.OutputResult[T]{Accepted: true, Value: value,
		Content: []gent.ContentPart{gent.TextContent{Text: string(delivery)}}}, nil
}

func decodeJSON(text string, target any) error {
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return err
		}
		return errors.New("more than one JSON value")
	}
	return nil
}
