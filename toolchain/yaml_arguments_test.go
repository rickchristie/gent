package toolchain

import (
	"context"
	"fmt"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/format"
	"github.com/stretchr/testify/assert"
	"github.com/tmc/langchaingo/llms"
)

func TestYAML_PlanArguments(t *testing.T) {
	type input struct{ content string }
	type expected struct {
		args map[string]any
		err  error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"nested date", input{`tool: lookup
args:
  filter:
    start: 2026-10-09`}, expected{map[string]any{
			"filter": map[string]any{"start": "2026-10-09T00:00:00Z"}}, nil}},
		{"quoted date", input{`tool: lookup
args:
  filter:
    start: "2026-10-09"`}, expected{map[string]any{
			"filter": map[string]any{"start": "2026-10-09"}}, nil}},
		{"dates in nested arrays", input{`tool: lookup
args:
  filters:
    - start: 2026-10-09
    - start: 2026-10-10T12:30:00.125+07:00`}, expected{map[string]any{
			"filters": []any{
				map[string]any{"start": "2026-10-09T00:00:00Z"},
				map[string]any{"start": "2026-10-10T12:30:00.125+07:00"},
			}}, nil}},
		{"schema string keeps raw scalar", input{`tool: lookup
args:
  start: 2026-10-09`}, expected{map[string]any{"start": "2026-10-09"}, nil}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			filter := map[string]any{"type": "object", "properties": map[string]any{
				"start": map[string]any{"type": "string"}}}
			schema := map[string]any{"type": "object", "properties": map[string]any{
				"filter": filter, "filters": map[string]any{"type": "array", "items": filter},
				"start": map[string]any{"type": "string"},
			}}
			var inputs []map[string]any
			chain := NewYAML()
			chain.RegisterTool(gent.NewToolFunc("lookup", "Read", schema,
				func(_ context.Context, args map[string]any) (string, error) {
					inputs = append(inputs, args)
					return "record", nil
				}))
			ctx := gent.NewExecutionContext(context.Background(), "yaml",
				gent.NewBasicLoopData(nil))
			plan, err := chain.Prepare(ctx, test.input.content)
			if !assert.NoError(t, err) || !assert.Len(t, plan.Calls, 1) {
				return
			}
			assert.Equal(t, test.expected.args, plan.Calls[0].Args)
			result, err := chain.ExecuteNext(ctx, plan, format.NewXML())
			assert.Equal(t, test.expected.err, err)
			want := &gent.ToolCallResult{ToolCallId: plan.Calls[0].Id, AttemptId: ctx.AttemptId(),
				Name: "lookup", Args: test.expected.args, Input: test.expected.args,
				Output: "record",
				Hash:   ToolCallResultHash("lookup", test.expected.args), SummaryCaptured: true,
				Text: `<lookup>
record
</lookup>`}
			assert.Equal(t, want, result)
			assert.Equal(t, []map[string]any{test.expected.args}, inputs)
			bundle, err := ctx.Export()
			assert.NoError(t, err)
			assert.NoError(t, gent.ValidateBundle(bundle, ctx.Codecs()))
		})
	}
}

func TestYAML_NativeArgumentsKeepJSONTypes(t *testing.T) {
	chain := NewYAML()
	chain.RegisterTool(gent.NewToolFunc("lookup", "Read", nil,
		func(context.Context, map[string]any) (string, error) { return "record", nil }))
	ctx := gent.NewExecutionContext(context.Background(), "native", gent.NewBasicLoopData(nil))
	plan, err := chain.PrepareCalls(ctx, []llms.ToolCall{{ID: "call-a", Type: "function",
		FunctionCall: &llms.FunctionCall{Name: "lookup",
			Arguments: `{"id":1e3,"start":"2026-10-09"}`}}})
	if !assert.NoError(t, err) || !assert.Len(t, plan.Calls, 1) {
		return
	}
	assert.Equal(t, map[string]any{"id": float64(1000), "start": "2026-10-09"}, plan.Calls[0].Args)
}

func TestYAML_RejectNonJSONArguments(t *testing.T) {
	type input struct{ value string }
	type expected struct{ err string }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	const errorPrefix = "invalid YAML in section content: encode tool arguments: "
	cases := []testCase{
		{"not a number", input{".nan"}, expected{
			errorPrefix + "json: unsupported value: NaN"}},
		{"infinity", input{".inf"}, expected{
			errorPrefix + "json: unsupported value: +Inf"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			chain := NewYAML()
			chain.RegisterTool(gent.NewToolFunc("lookup", "Read", nil,
				func(context.Context, map[string]any) (string, error) {
					t.Error("invalid arguments must not execute a tool")
					return "", nil
				}))
			ctx := gent.NewExecutionContext(context.Background(), "yaml",
				gent.NewBasicLoopData(nil))
			content := fmt.Sprintf(`tool: lookup
args: {value: %s}`, test.input.value)
			result, err := chain.Execute(ctx, content, format.NewXML())
			assert.Nil(t, result)
			assert.EqualError(t, err, test.expected.err)
		})
	}
}
