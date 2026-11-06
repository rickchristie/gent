package toolchain

import (
	"context"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestProgramDefinitionsAndDirectPolicy(t *testing.T) {
	type input struct{ codeOnly bool }
	type expected struct {
		names  []string
		status gent.ToolStatus
		calls  int
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"normal", input{}, expected{[]string{"read", "run_code"}, gent.ToolExecuted, 1}},
		{"code only", input{true}, expected{[]string{"run_code"}, gent.ToolBlocked, 0}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			tool := gent.DefineTool("read", func(context.Context, struct{}) (
				*gent.ToolExecutionResponse, error,
			) {
				calls++
				return capturedToolResponse("read", nil)
			}).Description("Read records.").MustBuild()
			wrapper := NewJsToolChainWrapper(New().RegisterTool(tool))
			if test.input.codeOnly {
				wrapper.WithDirectCallDisabled()
			}
			ctx := newExecCtx()
			defer ctx.Release()
			assert.Equal(t, test.expected.names, nativeNames(t, wrapper, ctx))
			record := executeNativeTestCall(t, wrapper, ctx, nativeCall("read", "{}"))
			assert.Equal(t, test.expected.status, record.Execution().Status)
			assert.Equal(t, test.expected.calls, calls)
			// The supported script helper remains available in either native exposure mode.
			records, err := executeTestCode(wrapper, ctx, `tool.call({tool:"read",args:{}});`, testFormat())
			assert.NoError(t, err)
			assert.Equal(t, gent.ToolExecuted, records[0].Execution().Status)
			assert.Equal(t, test.expected.calls+1, calls)
		})
	}
}
