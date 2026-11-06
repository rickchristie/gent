package toolchain

import (
	"context"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

type integerToolInput struct {
	ID uint64 `json:"id"`
}

func TestProgramExecutionSettingsAndTypedNumbers(t *testing.T) {
	type input struct {
		mode gent.PTCIntegerMode
		code string
	}
	type expected struct {
		status gent.ToolStatus
		id     uint64
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	chain := New().RegisterTool(gent.DefineTool("fetch", func(context.Context, struct{}) (
		*gent.ToolExecutionResponse, error,
	) {
		parts, err := gent.NewParts().Data(integerToolInput{9007199254740993}).
			PDF([]byte("%PDF-fixture")).Build()
		return &gent.ToolExecutionResponse{Status: gent.ToolExecuted, Parts: parts,
			Instructions: "Required guidance."}, err
	}).MustBuild())
	chain.RegisterTool(gent.DefineTool("consume", func(_ context.Context, value integerToolInput) (
		*gent.ToolExecutionResponse, error,
	) {
		return capturedToolResponse(value, nil)
	}).MustBuild())
	wrapper := NewJsToolChainWrapper(chain)
	const code = `const reply = tool.call({tool:"fetch",args:{}});
tool.forward(reply.parts.filter(p=>p.kind==="bytes"));
tool.call({tool:"consume",args:{id:reply.parts[0].data.id}});`
	cases := []testCase{
		{"number", input{gent.PTCIntegerNumber, code},
			expected{gent.ToolExecuted, 9007199254740992}},
		{"bigint", input{gent.PTCIntegerBigInt, code},
			expected{gent.ToolExecuted, 9007199254740993}},
		{"outside uint64", input{gent.PTCIntegerBigInt,
			`tool.call({tool:"consume",args:{id:18446744073709551616n}});`},
			expected{gent.ToolInvalidArguments, 0}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			steering := gent.SteeringMessages{PTCBytesOmitted: "Custom byte guidance.",
				ToolError: "Custom operation guidance."}
			ctx := gent.NewExecutionContext(context.Background(), "numbers", gent.NewBasicLoopData(nil),
				gent.WithPTCIntegerMode(test.input.mode), gent.WithSteeringMessages(steering))
			defer ctx.Release()
			records, err := executeTestCode(wrapper, ctx, test.input.code, testFormat())
			if !assert.NoError(t, err) {
				return
			}
			var consumed *gent.ToolCallRecord
			for _, event := range ctx.Events() {
				if after, ok := event.(*gent.AfterToolCallEvent); ok && after.ToolName == "consume" {
					consumed = after.Output.(*gent.ToolCallRecord)
				}
			}
			if !assert.NotNil(t, consumed, "outer reply: %s", recordText(records[0])) {
				return
			}
			actual := expected{status: consumed.Execution().Status}
			if actual.status == gent.ToolExecuted {
				value, err := ctx.Codecs().Decode(*consumed.Input())
				assert.NoError(t, err)
				actual.id = value.(integerToolInput).ID
			}
			assert.Equal(t, test.expected, actual)
			if actual.status == gent.ToolExecuted {
				assert.Equal(t, gent.PDFPart([]byte("%PDF-fixture")), records[0].Response().Parts()[2])
			}
			saved, err := ctx.Export()
			assert.NoError(t, err)
			// Completed records need neither tool registrations nor the former typed input.
			fresh, err := gent.RestoreExecutionContext(context.Background(), saved, gent.RestoreBindings{})
			if !assert.NoError(t, err) {
				return
			}
			defer fresh.Release()
			assert.Equal(t, test.input.mode, fresh.PTCIntegerMode())
			assert.Equal(t, steering.Resolved(), fresh.SteeringMessages())
			plan, found, err := fresh.SavedToolPlan()
			assert.NoError(t, err)
			assert.True(t, found)
			assert.Equal(t, records, plan.Results)
		})
	}
}
