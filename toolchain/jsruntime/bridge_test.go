package jsruntime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func bridgeRecord(t *testing.T, kind gent.ToolResponseKind, execution *gent.ToolExecutionResponse,
	recovery *gent.ToolRecoveryResponse, parts []gent.ContentPart,
) *gent.ToolCallRecord {
	t.Helper()
	builder := gent.NewToolCallRecordBuilder("logical", "attempt", "lookup")
	switch kind {
	case gent.ToolResponseExecution:
		builder.Execution(execution)
	case gent.ToolResponseRecovery:
		builder.Recovery(recovery)
	case gent.ToolResponseCallError:
		builder.CallError()
	case gent.ToolResponseRecoveryError:
		builder.RecoveryError()
	}
	record, err := builder.ModelReply("provider", "lookup", parts...).Build()
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	return record
}

func TestBridgePreservesDataPropertyNames(t *testing.T) {
	type input struct{ mode gent.PTCIntegerMode }
	type expected struct{ integerType string }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"number", input{gent.PTCIntegerNumber}, expected{"number"}},
		{"bigint", input{gent.PTCIntegerBigInt}, expected{"bigint"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			data := map[string]any{
				"__proto__": map[string]any{"id": 7}, "constructor": 3, "value": 9,
				"nested": []any{map[string]any{"__proto__": map[string]any{"id": 11}, "value": 13}},
			}
			part, err := gent.DataPart(data)
			if !assert.NoError(t, err) {
				return
			}
			record := bridgeRecord(t, gent.ToolResponseExecution, &gent.ToolExecutionResponse{
				Status: gent.ToolExecuted, Parts: []gent.ContentPart{part}}, nil, []gent.ContentPart{part})
			rt := New(DefaultConfig())
			var arguments string
			RegisterToolBridge(rt, func(calls []*gent.ModelToolCallRequest) (
				[]*gent.ToolCallRecord, error,
			) {
				if calls[0].FunctionCall.Name == "next" {
					arguments = calls[0].FunctionCall.Arguments
				}
				return []*gent.ToolCallRecord{record}, nil
			}, test.input.mode)
			result, err := rt.Execute(context.Background(), `const d=tool.call({tool:"fetch",args:{}})
.parts[0].data;
console.log(Object.hasOwn(d,"__proto__"), Object.keys(d).sort(), d.id);
console.log(Object.hasOwn(d.nested[0],"__proto__"), d.nested[0].id);
console.log(typeof d.__proto__.id, typeof d.nested[0].__proto__.id);
tool.call({tool:"next",args:d});`)
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, []string{
				`true ["__proto__","constructor","nested","value"] undefined`,
				"true undefined", test.expected.integerType + " " + test.expected.integerType,
			}, result.ConsoleLog)
			encoded, err := json.Marshal(data)
			assert.NoError(t, err)
			assert.Equal(t, string(encoded), arguments)
		})
	}
}

func TestBridgeUsesCommonReply(t *testing.T) {
	type input struct {
		kind      gent.ToolResponseKind
		execution *gent.ToolExecutionResponse
		recovery  *gent.ToolRecoveryResponse
	}
	type expected struct{ value map[string]any }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	base := func(kind string) map[string]any {
		return map[string]any{
			"toolCallId": "provider", "name": "lookup", "kind": kind,
			"parts": []any{map[string]any{"kind": "text", "text": "Safe guidance"}},
		}
	}
	executed := base("execution")
	executed["executionStatus"] = "executed"
	blocked := base("execution")
	blocked["executionStatus"] = "blocked"
	invalid := base("execution")
	invalid["executionStatus"] = "invalid_arguments"
	completed := base("recovery")
	completed["executionStatus"] = "executed"
	completed["recoveryStatus"] = "completed"
	unresolved := base("recovery")
	unresolved["recoveryStatus"] = "unresolved"
	notStarted := base("recovery")
	notStarted["recoveryStatus"] = "not_started"
	cases := []testCase{
		{"executed", input{gent.ToolResponseExecution, &gent.ToolExecutionResponse{
			Status: gent.ToolExecuted}, nil}, expected{executed}},
		{"blocked", input{gent.ToolResponseExecution, &gent.ToolExecutionResponse{
			Status: gent.ToolBlocked}, nil}, expected{blocked}},
		{"invalid", input{gent.ToolResponseExecution, &gent.ToolExecutionResponse{
			Status: gent.ToolInvalidArguments}, nil}, expected{invalid}},
		{"completed", input{gent.ToolResponseRecovery, nil, &gent.ToolRecoveryResponse{
			Status: gent.ToolRecoveryCompleted, Execution: &gent.ToolExecutionResponse{
				Status: gent.ToolExecuted}}}, expected{completed}},
		{"unresolved", input{gent.ToolResponseRecovery, nil, &gent.ToolRecoveryResponse{
			Status: gent.ToolRecoveryUnresolved}}, expected{unresolved}},
		{"not started", input{gent.ToolResponseRecovery, nil, &gent.ToolRecoveryResponse{
			Status: gent.ToolRecoveryNotStarted}}, expected{notStarted}},
		{"call error", input{kind: gent.ToolResponseCallError}, expected{base("call_error")}},
		{"recovery error", input{kind: gent.ToolResponseRecoveryError}, expected{base("recovery_error")}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			record := bridgeRecord(t, test.input.kind, test.input.execution, test.input.recovery,
				[]gent.ContentPart{gent.TextPart("Safe guidance")})
			rt := New(DefaultConfig())
			RegisterToolBridge(rt, func(calls []*gent.ModelToolCallRequest) ([]*gent.ToolCallRecord, error) {
				assert.Equal(t, []*gent.ModelToolCallRequest{{Type: "function",
					FunctionCall: &gent.FunctionCall{
						Name: "lookup", Arguments: `{"id":"O1"}`}}}, calls)
				return []*gent.ToolCallRecord{record}, nil
			}, gent.PTCIntegerNumber)
			_, err := rt.Execute(context.Background(),
				`var reply=tool.call({tool:"lookup",args:{id:"O1"}});`)
			assert.NoError(t, err)
			assert.Equal(t, test.expected.value, rt.VM().Get("reply").Export())
		})
	}
}

func TestBridgeIntegerModesAndOriginalData(t *testing.T) {
	type input struct {
		mode gent.PTCIntegerMode
		code string
	}
	type expected struct {
		logs      []string
		arguments string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"number", input{gent.PTCIntegerNumber,
			`console.log(typeof d.id,typeof d.small,typeof d.fraction);
console.log(d.id); tool.call({tool:"next",args:{id:d.id}});`}, expected{
			[]string{"number number number", "9007199254740992"}, `{"id":9007199254740992}`}},
		{"bigint", input{gent.PTCIntegerBigInt,
			`console.log(typeof d.id,typeof d.small,typeof d.fraction);
console.log({id:d.id,small:d.small});tool.call({tool:"next",args:{id:d.id}});`}, expected{
			[]string{"bigint bigint number", `{"id":9007199254740993,"small":3}`},
			`{"id":9007199254740993}`}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			part, err := gent.DataPart(struct {
				ID       uint64  `json:"id"`
				Small    int     `json:"small"`
				Fraction float64 `json:"fraction"`
			}{9007199254740993, 3, 1.5})
			assert.NoError(t, err)
			record := bridgeRecord(t, gent.ToolResponseExecution, &gent.ToolExecutionResponse{
				Status: gent.ToolExecuted, Parts: []gent.ContentPart{part}}, nil, []gent.ContentPart{part})
			rt := New(DefaultConfig())
			var arguments string
			bridge := RegisterToolBridge(rt, func(calls []*gent.ModelToolCallRequest) (
				[]*gent.ToolCallRecord, error,
			) {
				if calls[0].FunctionCall.Name == "next" {
					arguments = calls[0].FunctionCall.Arguments
				}
				return []*gent.ToolCallRecord{record}, nil
			}, test.input.mode)
			result, err := rt.Execute(context.Background(),
				`const d=tool.call({tool:"lookup",args:{}}).parts[0].data;`+test.input.code)
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, test.expected, expected{result.ConsoleLog, arguments})
			parts := bridge.Parts(gent.SteeringMessages{})
			assert.Equal(t, part, parts[1])
			assert.Equal(t, `{"fraction":1.5,"id":9007199254740993,"small":3}`,
				parts[1].(gent.DataContent).String())
		})
	}
}

func TestBridgeByteForwarding(t *testing.T) {
	type input struct{ code string }
	type expected struct {
		forwarded bool
		logs      []string
		fails     bool
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"omitted", input{`console.log(reply.parts[1].data instanceof Uint8Array);`},
			expected{false, []string{"true"}, false}},
		{"forwarded", input{`tool.forward(reply.parts.filter(p=>p.kind==="bytes"));`},
			expected{true, []string{}, false}},
		{"foreign part", input{`tool.forward([{kind:"bytes",data:new Uint8Array([1])}]);`},
			expected{false, nil, true}},
	}
	data, err := gent.DataPart(map[string]any{"id": "O1"})
	assert.NoError(t, err)
	original := []gent.ContentPart{data, gent.PDFPart([]byte{0, 255, 7}),
		gent.TextPart("Required guidance")}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			record := bridgeRecord(t, gent.ToolResponseExecution, &gent.ToolExecutionResponse{
				Status: gent.ToolExecuted, Parts: original}, nil, original)
			rt := New(DefaultConfig())
			bridge := RegisterToolBridge(rt, func([]*gent.ModelToolCallRequest) (
				[]*gent.ToolCallRecord, error,
			) {
				return []*gent.ToolCallRecord{record}, nil
			}, gent.PTCIntegerNumber)
			result, err := rt.Execute(context.Background(),
				`const reply=tool.call({tool:"lookup",args:{}});`+test.input.code)
			assert.Equal(t, test.expected.fails, err != nil)
			if result != nil {
				assert.Equal(t, test.expected.logs, result.ConsoleLog)
			}
			want := []gent.ContentPart{gent.TextPart("Tool lookup (call provider):"), data,
				gent.TextPart("BYTES OMITTED MIME type: application/pdf."), gent.TextPart("Required guidance")}
			if test.expected.forwarded {
				want[2] = original[1]
			}
			assert.Equal(t, want, bridge.Parts(gent.SteeringMessages{PTCBytesOmitted: "BYTES OMITTED"}))
			assert.Equal(t, original, record.Response().Parts())
		})
	}
}

func TestBridgeParallelOrderAndPrivateFailure(t *testing.T) {
	rt := New(DefaultConfig())
	RegisterToolBridge(rt, func(calls []*gent.ModelToolCallRequest) ([]*gent.ToolCallRecord, error) {
		var records []*gent.ToolCallRecord
		for _, call := range calls {
			records = append(records, bridgeRecord(t, gent.ToolResponseExecution,
				&gent.ToolExecutionResponse{Status: gent.ToolExecuted}, nil,
				[]gent.ContentPart{gent.TextPart(call.FunctionCall.Name)}))
		}
		return records, nil
	}, gent.PTCIntegerNumber)
	result, err := rt.Execute(context.Background(), `const replies=tool.parallelCall([
{tool:"first",args:{}},{tool:"second",args:{}}]);
console.log(replies.map(r=>r.parts[0].text));`)
	assert.NoError(t, err)
	assert.Equal(t, []string{`["first","second"]`}, result.ConsoleLog)
	// A persistence failure must not become a catchable tool reply or allow another tool call.
	calls := 0
	rt = New(DefaultConfig())
	RegisterToolBridge(rt, func([]*gent.ModelToolCallRequest) ([]*gent.ToolCallRecord, error) {
		calls++
		return nil, errors.New("private storage error")
	}, gent.PTCIntegerNumber)
	_, err = rt.Execute(context.Background(), `try{tool.call({tool:"first",args:{}})}catch(e){}
tool.call({tool:"second",args:{}});`)
	assert.Error(t, err)
	assert.Equal(t, 1, calls)
}

func TestNativeBigIntJSONBehavior(t *testing.T) {
	rt := New(DefaultConfig())
	result, err := rt.Execute(context.Background(), `try{JSON.stringify({id:3n})}catch(e){
console.log(e instanceof TypeError);}`)
	assert.NoError(t, err)
	assert.Equal(t, []string{"true"}, result.ConsoleLog)
}
