package toolchain

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestProgramAutomaticResultsAndForwarding(t *testing.T) {
	type input struct{ forward bool }
	type expected struct {
		byteCount int
		omissions int
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"omitted", input{}, expected{0, 1}},
		{"forwarded", input{true}, expected{1, 0}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			tool := gent.DefineTool("receipt", func(context.Context, struct{}) (
				*gent.ToolExecutionResponse, error,
			) {
				parts, err := gent.NewParts().Data(map[string]any{"id": "O1"}).PDF([]byte("%PDF-saved")).Build()
				if err != nil {
					return nil, err
				}
				return &gent.ToolExecutionResponse{Status: gent.ToolExecuted, Parts: parts,
					Instructions: "Check the receipt."}, nil
			}).MustBuild()
			wrapper := NewJsToolChainWrapper(New().RegisterTool(tool))
			ctx := newExecCtx()
			defer ctx.Release()
			code := `const reply=tool.call({tool:"receipt",args:{}});`
			if test.input.forward {
				code += `tool.forward(reply.parts.filter(p=>p.kind==="bytes"));`
			}
			records, err := executeTestCode(wrapper, ctx, code, testFormat())
			if !assert.NoError(t, err) {
				return
			}
			outer := records[0]
			bytes, omissions := 0, 0
			var data []any
			for _, part := range outer.Response().Parts() {
				switch value := part.(type) {
				case gent.BinaryContent:
					bytes++
					assert.Equal(t, []byte("%PDF-saved"), value.Data)
				case gent.DataContent:
					data = append(data, value.Value())
				case gent.TextContent:
					if value.Text == gent.DefaultSteeringMessages().PTCBytesOmitted+
						" MIME type: application/pdf." {
						omissions++
					}
				}
			}
			assert.Equal(t, test.expected, expected{bytes, omissions})
			assert.Equal(t, []any{map[string]any{"id": "O1"}}, data)
			innerCount := 0
			for _, event := range ctx.Events() {
				after, ok := event.(*gent.AfterToolCallEvent)
				if !ok || after.ToolName != "receipt" {
					continue
				}
				innerCount++
				record := after.Output.(*gent.ToolCallRecord)
				assert.Equal(t, gent.PDFPart([]byte("%PDF-saved")), record.Response().Parts()[1])
				assert.Equal(t, gent.TextPart("Check the receipt."), record.Response().Parts()[2])
			}
			assert.Equal(t, 1, innerCount)
			assert.Equal(t, gent.TextPart("<output>\nCode executed successfully.\n</output>"),
				outer.Response().Parts()[len(outer.Response().Parts())-1])
		})
	}
}

func TestProgramErrorGuidanceIsAutomatic(t *testing.T) {
	ctx := gent.NewExecutionContext(context.Background(), "script", gent.NewBasicLoopData(nil),
		gent.WithSteeringMessages(gent.SteeringMessages{ToolError: "SAFE ERROR"}))
	defer ctx.Release()
	chain := New().RegisterTool(gent.DefineTool("fail", func(context.Context, struct{}) (
		*gent.ToolExecutionResponse, error,
	) {
		return &gent.ToolExecutionResponse{Status: gent.ToolExecuted,
			Parts: []gent.ContentPart{gent.TextPart("PRIVATE RETURN")}}, errors.New("PRIVATE ERROR")
	}).MustBuild())
	records, err := executeTestCode(NewJsToolChainWrapper(chain), ctx,
		`const r=tool.call({tool:"fail",args:{}});console.log(r.kind,r.executionStatus);`, testFormat())
	assert.NoError(t, err)
	parts := records[0].Response().Parts()
	assert.Equal(t, []gent.ContentPart{gent.TextPart("SAFE ERROR"),
		gent.TextPart("<output>\ncall_error undefined\n</output>")}, parts[1:])
	assert.Equal(t, int64(1), ctx.Stats().GetCounter(gent.SCToolCallsErrorTotal))
}

func TestProgramPrevalidationAndRuntimeCorrection(t *testing.T) {
	type input struct {
		code          string
		prevalidation bool
	}
	type expected struct {
		calls  int
		status gent.ToolStatus
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"literal", input{`tool.call({tool:"read",args:{id:3}});`, true},
			expected{0, gent.ToolInvalidArguments}},
		{"dynamic", input{`const value=3;tool.call({tool:"read",args:{id:value}});`, false},
			expected{0, gent.ToolExecuted}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx := newExecCtx()
			defer ctx.Release()
			calls := 0
			tool := gent.DefineTool("read", func(context.Context, validationInput) (
				*gent.ToolExecutionResponse, error,
			) {
				calls++
				return capturedToolResponse("read", nil)
			}).MustBuild()
			records, err := executeTestCode(NewJsToolChainWrapper(New().RegisterTool(tool)), ctx,
				test.input.code, testFormat())
			assert.NoError(t, err)
			assert.Equal(t, test.expected, expected{calls, records[0].Execution().Status})
			// Both paths use the same complete schema correction core.
			var correction string
			if test.input.prevalidation {
				correction = records[0].Execution().Instructions
			} else {
				for _, event := range ctx.Events() {
					if after, ok := event.(*gent.AfterToolCallEvent); ok && after.ToolName == "read" {
						correction = after.Output.(*gent.ToolCallRecord).Execution().Instructions
					}
				}
				expectedSchema := `{
  "additionalProperties": false,
  "properties": {
    "id": {
      "type": "string"
    }
  },
  "required": [
    "id"
  ],
  "type": "object"
}`
				assert.Equal(t, "Invalid arguments for tool \"read\".\nargs.id: expected string.\n"+
					"Expected input schema:\n"+expectedSchema, correction)
			}
			assert.NotEmpty(t, correction)
		})
	}
}

func TestProgramTimeoutAndSuccessfulReset(t *testing.T) {
	ctx := newExecCtx()
	defer ctx.Release()
	wrapper := NewJsToolChainWrapper(New()).WithCodeTimeout(time.Millisecond)
	records, err := executeTestCode(wrapper, ctx, `while(true){}`, testFormat())
	assert.NoError(t, err)
	assert.Equal(t, gent.ToolExecuted, records[0].Execution().Status)
	assert.Equal(t, int64(1), ctx.Stats().GetCounter(gent.SCCodeExecutionsError))
	assert.Equal(t, float64(1), ctx.Stats().GetGauge(gent.SGCodeExecutionsErrorConsecutive))
	wrapper.WithCodeTimeout(time.Second)
	records, err = executeTestCode(wrapper, ctx, `console.log("done");`, testFormat())
	assert.NoError(t, err)
	assert.Equal(t, "<output>\ndone\n</output>", recordText(records[0]))
	assert.Equal(t, float64(0), ctx.Stats().GetGauge(gent.SGCodeExecutionsErrorConsecutive))
}
