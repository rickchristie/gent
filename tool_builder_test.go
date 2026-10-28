package gent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/format"
	"github.com/rickchristie/gent/schema"
	"github.com/rickchristie/gent/toolchain"
	"github.com/stretchr/testify/assert"
)

type builderInput struct {
	OrderID string `json:"order_id"`
	Limit   int    `json:"limit,omitempty"`
}

func TestDefineTool(t *testing.T) {
	field := schema.Field("order_id").Description("The order ID.").MinLength(1)
	output := map[string]any{"type": "string", "description": "Current status."}
	builder := gent.DefineTool("lookup_order",
		func(_ context.Context, input builderInput) (string, error) {
			return input.OrderID + ": shipped", nil
		}).
		Description("Read the order status.").Parameters(field, schema.Field("limit").Optional().Min(1)).
		Policy("Use the supplied order ID.").Domain("Orders").Categories("lookup").
		Keywords("order", "status").SyntheticQueries("Where is my order?").OutputSchema(output).
		DeduplicateSummary(func(input builderInput, _ string) string { return input.OrderID }).
		Recovery(func(context.Context, *gent.ToolRecoveryRequest) (*gent.ToolRecoveryResponse, error) {
			return &gent.ToolRecoveryResponse{Result: gent.ToolRecoveryRetryAllowed,
				SteeringMessage: "Use the current order status."}, nil
		})
	tool, err := builder.Build()
	if !assert.NoError(t, err) {
		return
	}
	// Changing setup values or returned metadata cannot change the registered tool.
	builder.Description("changed").Categories("changed")
	field.Description("changed")
	output["description"] = "changed"
	tool.Categories()[0] = "changed"
	tool.OutputSchema()["description"] = "changed"
	tool.ParameterSchema()["properties"].(map[string]any)["order_id"] = nil
	var want map[string]any
	decoder := json.NewDecoder(strings.NewReader(`{
  "type": "object", "additionalProperties": false, "required": ["order_id"],
  "properties": {
    "order_id": {"type": "string", "description": "The order ID.", "minLength": 1},
    "limit": {"type": "integer", "minimum": 1}
  }
}`))
	decoder.UseNumber()
	assert.NoError(t, decoder.Decode(&want))
	assert.Equal(t, want, tool.ParameterSchema())
	type metadata struct {
		name, description, policy, domain string
		categories, keywords, queries     []string
		strict                            bool
		output                            map[string]any
	}
	assert.Equal(t, metadata{"lookup_order", "Read the order status.", "Use the supplied order ID.",
		"Orders", []string{"lookup"}, []string{"order", "status"}, []string{"Where is my order?"},
		false, map[string]any{"type": "string", "description": "Current status."}},
		metadata{tool.Name(), tool.Description(), tool.Policy(), tool.Domain(), tool.Categories(),
			tool.Keywords(), tool.SyntheticQueries(), tool.Strict(), tool.OutputSchema()})
	result, err := tool.Call(context.Background(), builderInput{OrderID: "order-1001"})
	assert.NoError(t, err)
	assert.Equal(t, &gent.ToolResult[string]{Text: "order-1001: shipped"}, result)
	assert.Equal(t, "order-1001", tool.DeduplicateSummary(builderInput{OrderID: "order-1001"}, ""))
	response, err := tool.Recover(context.Background(), &gent.ToolRecoveryRequest{})
	assert.NoError(t, err)
	assert.Equal(t, &gent.ToolRecoveryResponse{Result: gent.ToolRecoveryRetryAllowed,
		SteeringMessage: "Use the current order status."}, response)
	// Built definitions can be read and called concurrently. The handler owns its own state.
	var group sync.WaitGroup
	for range 4 {
		group.Add(1)
		go func() {
			defer group.Done()
			assert.Equal(t, want, tool.ParameterSchema())
			_, callErr := tool.Call(context.Background(), builderInput{})
			assert.NoError(t, callErr)
		}()
	}
	group.Wait()
}

func TestDefineToolBuildErrors(t *testing.T) {
	type input struct {
		configure func(*gent.ToolBuilder[builderInput, string])
	}
	type expected struct{ err string }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"unknown field", input{func(b *gent.ToolBuilder[builderInput, string]) {
			b.Parameters(schema.Field("order"))
		}}, expected{`tool "lookup" parameters: unknown field "order"`}},
		{"wrong constraint", input{func(b *gent.ToolBuilder[builderInput, string]) {
			b.Parameters(schema.Field("order_id").Min(1))
		}}, expected{`tool "lookup" parameters: field "order_id": minimum requires a number`}},
		{"reversed range", input{func(b *gent.ToolBuilder[builderInput, string]) {
			b.Parameters(schema.Field("limit").Min(10).Max(1))
		}}, expected{`tool "lookup" parameters: field "limit": minimum exceeds maximum`}},
		{"wrong enum", input{func(b *gent.ToolBuilder[builderInput, string]) {
			b.Parameters(schema.Field("limit").Enum("one"))
		}}, expected{
			`tool "lookup" parameters: field "limit": enum value one does not match the field schema`}},
		{"duplicate field", input{func(b *gent.ToolBuilder[builderInput, string]) {
			b.Parameters(schema.Field("limit"), schema.Field("limit"))
		}}, expected{`tool "lookup" parameters: duplicate field "limit"`}},
		{"mixed schemas", input{func(b *gent.ToolBuilder[builderInput, string]) {
			b.ParameterSchema(nil).Parameters(schema.Field("limit"))
		}}, expected{`tool "lookup": Parameters and ParameterSchema cannot be combined`}},
		{"non-object parameters", input{func(b *gent.ToolBuilder[builderInput, string]) {
			b.ParameterSchema(map[string]any{"type": "string"})
		}}, expected{`tool "lookup": parameter schema must have type object`}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			builder := gent.DefineTool("lookup", func(context.Context, builderInput) (string, error) {
				return "", nil
			})
			test.input.configure(builder)
			tool, err := builder.Build()
			assert.Nil(t, tool)
			assert.EqualError(t, err, test.expected.err)
		})
	}
}

func TestDefineToolEnumValues(t *testing.T) {
	type status string
	type code int
	const shipped status = "shipped"
	const invalid code = 1
	type orderInput struct {
		Status status `json:"status"`
	}
	type input struct{ value any }
	type expected struct {
		schema map[string]any
		err    string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	want := map[string]any{
		"type": "object", "additionalProperties": false, "required": []any{"status"},
		"properties": map[string]any{"status": map[string]any{
			"type": "string", "minLength": json.Number("1"), "enum": []any{"shipped"},
		}},
	}
	cases := []testCase{
		{"plain string", input{"shipped"}, expected{schema: want}},
		{"named string constant", input{shipped}, expected{schema: want}},
		{"named integer does not fit string", input{invalid}, expected{
			err: `tool "lookup" parameters: field "status": enum value 1 does not match the field schema`,
		}},
		{"named string fails constraint", input{status("")}, expected{
			err: `tool "lookup" parameters: field "status": enum value  does not match the field schema`,
		}},
		{"invalid JSON value", input{func() {}}, expected{
			err: `tool "lookup" parameters: field "status": invalid enum: json: unsupported type: func()`,
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			tool, err := gent.DefineTool("lookup", func(context.Context, orderInput) (string, error) {
				return "done", nil
			}).Parameters(schema.Field("status").MinLength(1).Enum(test.input.value)).Build()
			if test.expected.err != "" {
				assert.EqualError(t, err, test.expected.err)
				assert.Nil(t, tool)
				return
			}
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, test.expected.schema, tool.ParameterSchema())
		})
	}
}

func TestDefineToolExactNumbers(t *testing.T) {
	type orderID uint64
	const id orderID = 9007199254740993
	type record struct {
		ID orderID `json:"id"`
	}
	type input struct{ source string }
	type expected struct {
		schema map[string]any
		err    error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	explicit := map[string]any{
		"type": "object", "required": []any{"id"},
		"properties": map[string]any{"id": map[string]any{
			"type": "integer", "enum": []any{json.Number("9007199254740993")},
		}},
	}
	inferred := map[string]any{
		"type": "object", "additionalProperties": false, "required": []any{"id"},
		"properties": map[string]any{"id": map[string]any{
			"type": "integer", "minimum": json.Number("0"),
			"enum": []any{json.Number("9007199254740993")},
		}},
	}
	cases := []testCase{
		{"explicit input schema", input{"input"}, expected{schema: explicit}},
		{"inferred field enum", input{"field"}, expected{schema: inferred}},
		{"explicit output schema", input{"output"}, expected{schema: explicit}},
	}
	cwd, err := os.Getwd()
	if !assert.NoError(t, err) {
		return
	}
	location := "file://" + filepath.ToSlash(filepath.Join(cwd, "schema.json"))
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			values := []any{uint64(id)}
			raw := map[string]any{
				"type": "object", "required": []string{"id"},
				"properties": map[string]any{"id": map[string]any{
					"type": "integer", "enum": values,
				}},
			}
			builder := gent.DefineTool("lookup", func(_ context.Context, r record) (record, error) {
				return r, nil
			})
			switch test.input.source {
			case "input":
				builder.ParameterSchema(raw)
			case "field":
				values[0] = id
				builder.Parameters(schema.Field("id").Enum(values...))
			case "output":
				builder.OutputSchema(raw)
			}
			tool, err := builder.Build()
			if !assert.Equal(t, test.expected.err, err) {
				return
			}
			getSchema := tool.ParameterSchema
			if test.input.source == "output" {
				getSchema = tool.OutputSchema
			}
			// Neither setup values nor a returned schema may change the saved numeric constraint.
			values[0] = uint64(0)
			returned := getSchema()
			assert.Equal(t, test.expected.schema, returned)
			returned["properties"].(map[string]any)["id"].(map[string]any)["enum"].([]any)[0] = 0
			assert.Equal(t, test.expected.schema, getSchema())
			data, err := json.Marshal(getSchema())
			assert.NoError(t, err)
			want, err := json.Marshal(test.expected.schema)
			assert.NoError(t, err)
			assert.Equal(t, string(want), string(data))
			validator, err := schema.Compile(getSchema())
			if !assert.NoError(t, err) {
				return
			}
			assert.NoError(t, validator.Validate(map[string]any{"id": uint64(id)}))
			assert.EqualError(t, validator.Validate(map[string]any{"id": uint64(id - 1)}),
				fmt.Sprintf(`schema validation failed: jsonschema validation failed with '%s#'
- at '/id': value must be 9007199254740993`, location))
		})
	}
}

func TestDefineToolHandlerAndDefaults(t *testing.T) {
	type input struct {
		name    string
		handler func(context.Context, struct{}) (string, error)
	}
	type expected struct{ err string }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	handler := func(context.Context, struct{}) (string, error) { return "ok", nil }
	cases := []testCase{
		{"default settings", input{"lookup", handler}, expected{}},
		{"missing handler", input{name: "lookup"},
			expected{`tool "lookup": call handler is required`}},
		{"invalid name", input{"not a name", handler},
			expected{`invalid tool name "not a name": use 1 to 64 letters, digits, _ or -`}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			tool, err := gent.DefineTool(test.input.name, test.input.handler).Build()
			if test.expected.err != "" {
				assert.EqualError(t, err, test.expected.err)
				assert.Nil(t, tool)
				return
			}
			if !assert.NoError(t, err) {
				return
			}
			response, err := tool.Recover(context.Background(), &gent.ToolRecoveryRequest{})
			assert.NoError(t, err)
			assert.Equal(t, &gent.ToolRecoveryResponse{Result: gent.ToolRecoveryUnresolved}, response)
			assert.Equal(t, "", tool.DeduplicateSummary(struct{}{}, "ok"))
			assert.Equal(t, map[string]any{"type": "string"}, tool.OutputSchema())
		})
	}
}

func TestDefineToolValidatesBeforeCall(t *testing.T) {
	cwd, err := os.Getwd()
	if !assert.NoError(t, err) {
		return
	}
	schemaURL := "file://" + filepath.ToSlash(filepath.Join(cwd, "schema.json"))
	type input struct{ args string }
	type expected struct {
		calls int
		text  string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"valid", input{`{"order_id":"order-1001","limit":1}`}, expected{1, `<lookup>
shipped
</lookup>`}},
		{"missing required field", input{`{"order_id":"order-1001"}`}, expected{0, fmt.Sprintf(`<lookup>
Error: schema validation failed: jsonschema validation failed with '%s#'
- at '': missing property 'limit'
</lookup>`, schemaURL)}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			tool := gent.DefineTool("lookup", func(context.Context, builderInput) (string, error) {
				calls++
				return "shipped", nil
			}).Strict(true).Parameters(schema.Field("order_id").MinLength(1)).MustBuild()
			chain := toolchain.New().WithOutputSchema(false)
			chain.RegisterTool(tool)
			ctx := gent.NewExecutionContext(context.Background(), "builder", nil)
			defer ctx.Release()
			definitions, err := chain.NativeTools(ctx)
			assert.NoError(t, err)
			assert.Equal(t, []gent.ToolDefinition{{Type: "function", Function: &gent.FunctionDefinition{
				Name: "lookup", Strict: true, Parameters: tool.ParameterSchema(),
			}}}, definitions)
			plan, err := chain.PrepareCalls(ctx, []gent.ModelToolCall{{Type: "function",
				FunctionCall: &gent.FunctionCall{Name: "lookup", Arguments: test.input.args}}})
			if !assert.NoError(t, err) {
				return
			}
			result, err := chain.ExecuteNext(ctx, plan, format.NewXML())
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, test.expected, expected{calls, result.Text})
			var validation *schema.ValidationError
			assert.Equal(t, test.expected.calls == 0, errors.As(result.Error, &validation))
		})
	}
}
