package toolchain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/events"
	"github.com/rickchristie/gent/format"
	"github.com/rickchristie/gent/schema"
	"github.com/stretchr/testify/assert"
)

func testFormat() gent.TextFormat { return format.NewXML() }
func newExecCtx() *gent.ExecutionContext {
	return gent.NewExecutionContext(context.Background(), "tools", gent.NewBasicLoopData(nil))
}
func nativeCall(name, args string) *gent.ModelToolCallRequest {
	return &gent.ModelToolCallRequest{ID: "provider-id", Type: "function",
		FunctionCall: &gent.FunctionCall{Name: name, Arguments: args}}
}
func recordText(record *gent.ToolCallRecord) string {
	return gent.ContentTextData(record.Response().Parts())
}
func executeNativeTestCall(t *testing.T, chain gent.ToolChain, ctx *gent.ExecutionContext,
	call *gent.ModelToolCallRequest,
) *gent.ToolCallRecord {
	t.Helper()
	records, err := executeToolPlan(ctx, chain, []*gent.ModelToolCallRequest{call}, testFormat())
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	if !assert.Len(t, records, 1) {
		t.FailNow()
	}
	return records[0]
}
func nativeNames(t *testing.T, chain gent.ToolChain, ctx *gent.ExecutionContext) []string {
	t.Helper()
	definitions, err := chain.NativeTools(ctx)
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	var names []string
	for _, definition := range definitions {
		names = append(names, definition.Function.Name)
	}
	return names
}

func TestRegistryResponseContract(t *testing.T) {
	type input struct {
		response *gent.ToolExecutionResponse
		err      error
		perTool  string
	}
	type expected struct {
		kind       gent.ToolResponseKind
		status     gent.ToolStatus
		parts      []gent.ContentPart
		diagnostic bool
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	decline := gent.TextPart(`{"approved":false}`)
	secret := gent.TextPart("Rejected private content")
	cases := []testCase{
		{"domain decline", input{response: &gent.ToolExecutionResponse{Status: gent.ToolExecuted,
			Parts: []gent.ContentPart{decline}, Instructions: "Choose another option."}}, expected{
			gent.ToolResponseExecution, gent.ToolExecuted, []gent.ContentPart{decline,
				gent.TextPart("Choose another option.")}, false}},
		{"blocked excludes parts", input{response: &gent.ToolExecutionResponse{Status: gent.ToolBlocked,
			Parts: []gent.ContentPart{secret}, Instructions: "Ask for approval A123."}}, expected{
			gent.ToolResponseExecution, gent.ToolBlocked, []gent.ContentPart{
				gent.TextPart("BLOCKED"), gent.TextPart("Ask for approval A123.")}, false}},
		{"error", input{err: errors.New("private credential failure")}, expected{
			gent.ToolResponseCallError, "", []gent.ContentPart{gent.TextPart("TASK ERROR")}, true}},
		{"both returns", input{response: &gent.ToolExecutionResponse{Status: gent.ToolExecuted,
			Parts: []gent.ContentPart{secret}, Instructions: "Rejected guidance"},
			err: errors.New("private")},
			expected{gent.ToolResponseCallError, "", []gent.ContentPart{gent.TextPart("TASK ERROR")}, true}},
		{"per tool override", input{err: errors.New("private"), perTool: "TOOL ERROR"}, expected{
			gent.ToolResponseCallError, "", []gent.ContentPart{gent.TextPart("TOOL ERROR")}, true}},
		{"nil nil", input{}, expected{gent.ToolResponseCallError, "",
			[]gent.ContentPart{gent.TextPart("TASK ERROR")}, true}},
		{"invalid status", input{response: &gent.ToolExecutionResponse{Status: "invalid"}}, expected{
			gent.ToolResponseCallError, "", []gent.ContentPart{gent.TextPart("TASK ERROR")}, true}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx := gent.NewExecutionContext(context.Background(), "tools", gent.NewBasicLoopData(nil),
				gent.WithSteeringMessages(gent.SteeringMessages{ToolError: "TASK ERROR",
					ToolBlocked: "BLOCKED"}))
			defer ctx.Release()
			count := 0
			tool := gent.DefineTool("act", func(context.Context, struct{}) (
				*gent.ToolExecutionResponse, error,
			) {
				count++
				return test.input.response, test.input.err
			}).UnrecoverableErrorInstructions(test.input.perTool).MustBuild()
			chain := New().RegisterTool(tool)
			record := executeNativeTestCall(t, chain, ctx, nativeCall("act", "{}"))
			status, _ := record.Response().ExecutionStatus()
			assert.Equal(t, test.expected, expected{record.Kind(), status, record.Response().Parts(),
				record.Error() != nil})
			assert.Equal(t, 1, count)
			assert.Equal(t, "provider-id", record.Response().ToolCallID())
			assert.NotEqual(t, "provider-id", record.ToolCallID())
			assert.Equal(t, ctx.AttemptId(), record.AttemptID())
			if test.input.response != nil {
				value, err := ctx.Codecs().Decode(*record.ReturnedResult())
				assert.NoError(t, err)
				assert.Equal(t, test.input.response, value)
			} else {
				assert.Nil(t, record.ReturnedResult())
			}
			if test.input.err != nil {
				value, err := ctx.Codecs().Decode(*record.Error())
				assert.NoError(t, err)
				assert.EqualError(t, value.(error), test.input.err.Error())
			}
			saved, err := ctx.Export()
			assert.NoError(t, err)
			assert.NoError(t, gent.ValidateBundle(saved, ctx.Codecs()))
		})
	}
}

type validationInput struct {
	ID string `json:"id"`
}

const completeInputSchema = `{
  "additionalProperties": false,
  "properties": {
    "id": {
      "minLength": 1,
      "type": "string"
    }
  },
  "required": [
    "id"
  ],
  "type": "object"
}`

func TestRegistryInputCorrection(t *testing.T) {
	type input struct {
		arguments string
		hook      any
	}
	type expected struct {
		calls   int
		details string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"missing", input{arguments: `{}`}, expected{0, "args.id: required field is missing."}},
		{"wrong type", input{arguments: `{"id":3}`}, expected{0, "args.id: expected string."}},
		{"extra field", input{arguments: `{"id":"O1","secret":"hidden"}`}, expected{
			0, "args.secret: additional field is not permitted."}},
		{"hook revalidation", input{`{"id":"O1"}`, validationInput{}}, expected{
			0, "args.id: must satisfy minLength (1)."}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			tool := gent.DefineTool("lookup", func(_ context.Context, input validationInput) (
				*gent.ToolExecutionResponse, error,
			) {
				calls++
				return capturedToolResponse(input, nil)
			}).
				Parameters(schema.Field("id").MinLength(1)).MustBuild()
			ctx := newExecCtx()
			defer ctx.Release()
			if test.input.hook != nil {
				ctx.SetEventPublisher(events.NewRegistry().
					Subscribe(&replaceInput{value: test.input.hook}))
			}
			record := executeNativeTestCall(t, New().RegisterTool(tool), ctx,
				nativeCall("lookup", test.input.arguments))
			assert.Equal(t, test.expected.calls, calls)
			want := gent.DefaultSteeringMessages().InvalidArguments + "\n" +
				"Invalid arguments for tool \"lookup\".\n" + test.expected.details +
				"\nExpected input schema:\n" + completeInputSchema
			assert.Equal(t, want, recordText(record))
			assert.Equal(t, gent.ToolInvalidArguments, record.Execution().Status)
			assert.Equal(t, int64(1), ctx.Stats().GetCounter(gent.SCToolCallsInvalidArgumentsTotal))
			assert.Equal(t, int64(0), ctx.Stats().GetCounter(gent.SCToolCallsErrorTotal))
		})
	}
}

type replaceInput struct{ value any }

func (h *replaceInput) OnBeforeToolCall(_ *gent.ExecutionContext, event *gent.BeforeToolCallEvent) {
	event.Args = h.value
}

func TestRegistryTemporalInputAndEffectiveSnapshot(t *testing.T) {
	type temporalInput struct {
		At    time.Time     `json:"at"`
		Delay time.Duration `json:"delay"`
	}
	want := temporalInput{At: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), Delay: 90 * time.Minute}
	ctx := newExecCtx()
	defer ctx.Release()
	var received temporalInput
	tool := gent.DefineTool("wait", func(_ context.Context, input temporalInput) (
		*gent.ToolExecutionResponse, error,
	) {
		received = input
		return capturedToolResponse("ok", nil)
	}).MustBuild()
	record := executeNativeTestCall(t, New().RegisterTool(tool), ctx,
		nativeCall("wait", `{"at":"2026-01-02T03:04:05Z","delay":"1h30m"}`))
	assert.Equal(t, want, received)
	restored, err := ctx.Codecs().Decode(*record.Input())
	assert.NoError(t, err)
	assert.Equal(t, want, restored)
}

func TestRegistryValidationAndCallCounters(t *testing.T) {
	type input struct {
		status  gent.ToolStatus
		failure bool
	}
	type expected struct {
		errors, invalid int64
		consecutive     float64
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"first error", input{failure: true}, expected{1, 0, 1}},
		{"second error", input{failure: true}, expected{2, 0, 2}},
		{"invalid does not reset", input{status: gent.ToolInvalidArguments}, expected{2, 1, 2}},
		{"blocked resets", input{status: gent.ToolBlocked}, expected{2, 1, 0}},
		{"later error", input{failure: true}, expected{3, 1, 1}},
		{"executed resets", input{status: gent.ToolExecuted}, expected{3, 1, 0}},
	}
	ctx := newExecCtx()
	defer ctx.Release()
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			tool := gent.DefineTool("work", func(context.Context, struct{}) (
				*gent.ToolExecutionResponse, error,
			) {
				if test.input.failure {
					return nil, errors.New("private error")
				}
				return &gent.ToolExecutionResponse{Status: test.input.status}, nil
			}).MustBuild()
			executeNativeTestCall(t, New().RegisterTool(tool), ctx, nativeCall("work", "{}"))
			assert.Equal(t, test.expected, expected{ctx.Stats().GetCounter(gent.SCToolCallsErrorTotal),
				ctx.Stats().GetCounter(gent.SCToolCallsInvalidArgumentsTotal),
				ctx.Stats().GetGauge(gent.SGToolCallsErrorConsecutive)})
		})
	}
}

func TestRegistryDefinitionsAndRegistration(t *testing.T) {
	tool := gent.DefineTool("read", func(context.Context, validationInput) (
		*gent.ToolExecutionResponse, error,
	) {
		return capturedToolResponse("ok", nil)
	}).Description("Read an order").Policy("Read only").OutputSchemaFrom(validationInput{}).MustBuild()
	chain := New()
	chain.RegisterTool(tool)
	ctx := newExecCtx()
	defer ctx.Release()
	definitions, err := chain.NativeTools(ctx)
	assert.NoError(t, err)
	assert.Equal(t, []*gent.ToolDefinition{{Type: "function", Function: &gent.FunctionDefinition{
		Name: "read", Description: "Read an order\n\nPolicy: Read only",
		Parameters:   tool.ParameterSchema(),
		OutputSchema: tool.OutputSchema(),
	}}}, definitions)
	assert.Panics(t, func() { chain.RegisterTool(tool) })
	assert.Panics(t, func() { New().RegisterTool("invalid") })
	chain.RegisterDeprecatedTool(gent.DefineTool("old", func(context.Context, struct{}) (
		*gent.ToolExecutionResponse, error,
	) {
		return capturedToolResponse("old", nil)
	}).MustBuild())
	assert.Equal(t, []string{"read"}, nativeNames(t, chain, ctx))
	assert.Equal(t, "old", recordText(executeNativeTestCall(t, chain, ctx, nativeCall("old", "{}"))))
	missing := executeNativeTestCall(t, chain, ctx, nativeCall("removed", "{}"))
	assert.Equal(t, gent.DefaultSteeringMessages().ToolUnavailable, recordText(missing))
}

func TestRegistryMalformedJSON(t *testing.T) {
	type input struct{ raw string }
	type expected struct {
		status gent.ToolStatus
		calls  int
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"truncated", input{`{"id":`}, expected{gent.ToolInvalidArguments, 0}},
		{"array", input{`[]`}, expected{gent.ToolInvalidArguments, 0}},
		{"null", input{`null`}, expected{gent.ToolInvalidArguments, 0}},
		{"two values", input{`{} {}`}, expected{gent.ToolInvalidArguments, 0}},
		{"exact integer", input{`{"id":9007199254740993}`}, expected{gent.ToolExecuted, 1}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			tool := gent.DefineTool("number", func(_ context.Context, args map[string]any) (
				*gent.ToolExecutionResponse, error,
			) {
				calls++
				assert.Equal(t, json.Number("9007199254740993"), args["id"])
				return capturedToolResponse(fmt.Sprint(args["id"]), nil)
			}).ParameterSchema(nil).MustBuild()
			ctx := newExecCtx()
			defer ctx.Release()
			record := executeNativeTestCall(t, New().RegisterTool(tool), ctx, nativeCall("number",
				test.input.raw))
			assert.Equal(t, test.expected, expected{record.Execution().Status, calls})
			assert.Equal(t, test.input.raw, record.RawArguments())
		})
	}
}
