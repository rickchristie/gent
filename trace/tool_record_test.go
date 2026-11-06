package trace

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestTraceRetainsToolRecordAndPreparedModelParts(t *testing.T) {
	parts, err := gent.NewParts().Data(map[string]any{"id": "O1"}).
		PDF([]byte{1, 2, 3}).Text("Check the date.").Build()
	assert.NoError(t, err)
	record, err := gent.NewToolCallRecordBuilder("execution", "attempt", "receipt").
		Arguments(`{"id":"O1"}`, map[string]any{"id": "O1"}).
		Execution(&gent.ToolExecutionResponse{Status: gent.ToolExecuted, Parts: parts}).
		ModelReply("provider", "receipt", parts...).Build()
	assert.NoError(t, err)
	config := Config{IncludeModelRequests: true, IncludeToolOutput: true}
	seq := newTestSequencer(t, "tool-record", config)
	ctx := gent.NewExecutionContext(context.Background(), "trace", nil,
		gent.WithRunId(seq.Snapshot().RunId), gent.WithObserver(seq))
	defer ctx.Release()
	ctx.PublishBeforeExecution()
	ctx.PublishBeforeToolCall("receipt", map[string]any{"id": "O1"},
		gent.WithToolCallId("execution"))
	ctx.PublishAfterToolCall("receipt", map[string]any{"id": "O1"}, record, time.Second, nil,
		gent.WithToolCallId("execution"))
	ctx.PublishBeforeModelCall("fixture", gent.NewModelCallRequest([]*gent.ModelMessage{
		record.ModelMessage(),
	}))
	assert.NoError(t, ctx.FlushProgress())
	snapshot := seq.Snapshot()
	if !assert.Len(t, snapshot.ToolCalls, 1) || !assert.Len(t, snapshot.ModelCalls, 1) {
		return
	}
	const replyJSON = `{
"ToolCallID":"provider","Name":"receipt","Kind":"execution",
"ExecutionStatus":"executed","RecoveryStatus":"",
"Parts":[{"Data":{"id":"O1"}}, {"MIMEType":"application/pdf","Data":"AQID"},
{"Text":"Check the date."}]}`
	var reply any
	assert.NoError(t, json.Unmarshal([]byte(replyJSON), &reply))
	wantOutput := map[string]any{
		"ToolCallID": "execution", "AttemptID": "attempt", "Name": "receipt", "Kind": "execution",
		"Execution": map[string]any{"Status": "executed", "Instructions": "",
			"Parts": reply.(map[string]any)["Parts"]},
		"Response": reply, "RawArguments": `{"id":"O1"}`, "Args": map[string]any{"id": "O1"},
		"Recovery": nil, "DiscoveredTools": nil, "Input": nil,
		"ReturnedResult": nil, "ReturnedRecovery": nil, "Error": nil,
	}
	assert.Equal(t, wantOutput, snapshot.ToolCalls[0].Output)
	request := snapshot.ModelCalls[0].Request.(map[string]any)
	messages := request["Messages"].([]any)
	assert.Equal(t, []any{reply}, messages[0].(map[string]any)["Parts"])
	view, err := ctx.Export()
	assert.NoError(t, err)
	restored, err := RestoreSequencer(view, config)
	if assert.NoError(t, err) {
		assert.Equal(t, snapshot, restored.Snapshot())
	}
}
