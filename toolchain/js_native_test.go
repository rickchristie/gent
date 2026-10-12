package toolchain

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/format"
	"github.com/stretchr/testify/assert"
	"github.com/tmc/langchaingo/llms"
)

func TestNativeJavaScript_Execution(t *testing.T) {
	type input struct {
		codeOnly bool
		code     string
	}
	type expected struct {
		names                []string
		text                 string
		toolCalls, codeCalls int64
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	code := `const result = tool.call({tool: "lookup", args: {id: 7}});
console.log(result.output);`
	output := `<tool_call_log>
[1] lookup({"id":7}) -> "record"
</tool_call_log>
<output>
record
</output>`
	cases := []testCase{
		{"direct and code", input{false, code},
			expected{[]string{"lookup", nativeCodeTool}, output, 1, 1}},
		{"code only", input{true, code}, expected{[]string{nativeCodeTool}, output, 1, 1}},
		{"empty program", input{false, ""},
			expected{[]string{"lookup", nativeCodeTool}, "", 0, 0}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var inputs []map[string]any
			chain := NewJSON().RegisterTool(gent.NewToolFunc("lookup", "Read data", nil,
				func(_ context.Context, args map[string]any) (string, error) {
					inputs = append(inputs, args)
					return "record", nil
				}))
			wrapper := NewJsToolChainWrapper(chain).WithCodeGuidance("Use JavaScript.")
			if test.input.codeOnly {
				wrapper.WithDirectCallDisabled()
			}
			ctx := newExecCtx()
			assert.Equal(t, test.expected.names, nativeNames(t, wrapper, ctx))
			args, err := json.Marshal(map[string]any{"code": test.input.code})
			assert.NoError(t, err)
			result := executeNativeTestCall(t, wrapper, ctx, nativeCall(nativeCodeTool, string(args)))
			assert.Equal(t, test.expected.text, result.Text)
			assert.Equal(t, test.expected.toolCalls, ctx.Stats().GetCounter(gent.SCToolCalls))
			assert.Equal(t, test.expected.codeCalls, ctx.Stats().GetCounter(gent.SCCodeExecutions))
			assert.Equal(t, int64(0), ctx.Stats().GetCounter(gent.SCToolCallsFor+nativeCodeTool))
			assert.Equal(t, int(test.expected.toolCalls), len(inputs))
			_, active, err := ctx.CheckpointState("gent:toolchain_active")
			assert.NoError(t, err)
			assert.False(t, active)
			plan, found, err := ctx.SavedToolPlan()
			assert.NoError(t, err)
			assert.True(t, found)
			assert.Equal(t, []*gent.ToolCallResult{result}, plan.Results)
			assert.Equal(t, nativeCodeTool, plan.Calls[0].Name)
			if !test.input.codeOnly {
				direct := executeNativeTestCall(t, wrapper, ctx, nativeCall("lookup", "{}"))
				assert.Equal(t, "record", direct.Output)
			}
		})
	}
}

func TestNativeJavaScript_Descriptions(t *testing.T) {
	chain := NewJSON().RegisterTool(gent.NewToolFunc("lookup", "Read data", nil,
		func(context.Context, map[string]any) (string, error) { return "record", nil }))
	wrapper := NewJsToolChainWrapper(chain).WithDirectCallDisabled().WithCodeGuidance("Use JS.")
	definitions, err := wrapper.NativeTools(newExecCtx())
	assert.NoError(t, err)
	assert.Equal(t, []llms.Tool{{Type: "function", Function: &llms.FunctionDefinition{
		Name: nativeCodeTool, Description: `Run JavaScript on the application server.
Use tool.call or tool.parallelCall for registered tools.

Use JS.

Available tools inside code:
{"name":"lookup","description":"Read data","parameters":{"properties":{},"type":"object"}}
`, Parameters: map[string]any{"type": "object", "properties": map[string]any{
			"code": map[string]any{"type": "string"},
		}, "required": []string{"code"}, "additionalProperties": false},
	}}}, definitions)
}

func TestNativeJavaScript_CallValidation(t *testing.T) {
	type input struct {
		call     llms.ToolCall
		codeOnly bool
	}
	type expected struct{ errorText string }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"missing code", input{nativeCall(nativeCodeTool, "{}"), false},
			expected{"run_code requires one string argument named code"}},
		{"extra argument", input{nativeCall(nativeCodeTool, `{"code":"","other":1}`), false},
			expected{"run_code requires one string argument named code"}},
		{"direct disabled", input{nativeCall("lookup", "{}"), true},
			expected{`call "lookup" through run_code`}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			wrapper := NewJsToolChainWrapper(NewJSON())
			if test.input.codeOnly {
				wrapper.WithDirectCallDisabled()
			}
			ctx := newExecCtx()
			plan, err := wrapper.PrepareCalls(ctx, []llms.ToolCall{test.input.call})
			assert.Nil(t, plan)
			assert.EqualError(t, err, test.expected.errorText)
			assert.Equal(t, int64(1), ctx.Stats().GetCounter(gent.SCToolchainParseErrorTotal))
			_, err = wrapper.PrepareCalls(ctx, []llms.ToolCall{test.input.call})
			assert.EqualError(t, err, test.expected.errorText)
			assert.Equal(t, float64(2), ctx.Stats().GetGauge(gent.SGToolchainParseErrorConsecutive))
		})
	}
}

func TestNativeJavaScript_SearchDiscovery(t *testing.T) {
	chain := setupSearchJSON([]*indexableToolFunc{newIndexableTool("lookup", "Read", "Sales",
		nil, nil, func(context.Context, map[string]any) (string, error) { return "ok", nil })},
		[]gent.ToolSearcher{&mockToolSearcher{id: "test",
			searchFn: func(context.Context, string) ([]string, error) { return []string{"lookup"}, nil }}})
	wrapper := NewJsToolChainWrapper(chain)
	ctx := newExecCtx()
	assert.Equal(t, []string{searchToolName, nativeCodeTool}, nativeNames(t, wrapper, ctx))
	code := `tool.call({
  tool: "tool_registry_search", args: {query: "record", query_type: "test"}
});`
	args, err := json.Marshal(map[string]string{"code": code})
	assert.NoError(t, err)
	result := executeNativeTestCall(t, wrapper, ctx, nativeCall(nativeCodeTool, string(args)))
	appendNativeSearchResult(t, ctx, result, 0)
	assert.Equal(t, []string{searchToolName, "lookup", nativeCodeTool}, nativeNames(t, wrapper, ctx))
	assert.Equal(t, []string{searchToolName, nativeCodeTool}, nativeNames(t, wrapper, newExecCtx()))
}

func TestNativeJavaScript_TimeoutAndReset(t *testing.T) {
	wrapper := NewJsToolChainWrapper(NewJSON()).WithCodeTimeout(time.Millisecond)
	ctx := newExecCtx()
	result := executeNativeTestCall(t, wrapper, ctx,
		nativeCall(nativeCodeTool, `{"code":"while (true) {}"}`))
	assert.NotEmpty(t, result.Text)
	assert.Equal(t, int64(1), ctx.Stats().GetCounter(gent.SCCodeExecutionsError))
	assert.Equal(t, float64(1), ctx.Stats().GetGauge(gent.SGCodeExecutionsErrorConsecutive))
	wrapper.WithCodeTimeout(time.Second)
	result = executeNativeTestCall(t, wrapper, ctx,
		nativeCall(nativeCodeTool, `{"code":"console.log('done');"}`))
	assert.Equal(t, `<output>
done
</output>`, result.Text)
	assert.Equal(t, float64(0), ctx.Stats().GetGauge(gent.SGCodeExecutionsErrorConsecutive))
	_, err := wrapper.ExecuteNext(ctx, &gent.ToolPlan{Version: 0}, format.NewXML())
	assert.EqualError(t, err, "gent: invalid tool plan")
}
