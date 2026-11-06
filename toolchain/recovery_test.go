package toolchain

import (
	"context"
	"errors"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestRestoreKeepsErrorGaugesWithoutCountingAgain(t *testing.T) {
	worker, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := gent.NewExecutionContext(worker, "errors", gent.NewBasicLoopData(nil))
	defer ctx.Release()
	calls := 0
	chain := New().RegisterTool(gent.DefineTool("act", func(context.Context, struct{}) (
		*gent.ToolExecutionResponse, error,
	) {
		calls++
		if calls == 4 {
			cancel()
			return nil, context.Canceled
		}
		if calls < 4 {
			return nil, errors.New("private operation error")
		}
		return &gent.ToolExecutionResponse{Status: gent.ToolBlocked,
			Instructions: "Request approval."}, nil
	}).Recovery(func(context.Context, *gent.ToolRecoveryRequest[struct{}]) (
		*gent.ToolRecoveryResponse, error,
	) {
		return nil, errors.New("private recovery error")
	}).MustBuild())
	var requests []*gent.ModelToolCallRequest
	for range 4 {
		requests = append(requests, nativeCall("act", "{}"))
	}
	plan, err := chain.PrepareCalls(ctx, requests)
	assert.NoError(t, err)
	for range 3 {
		_, err := chain.ExecuteNext(ctx, plan, testFormat())
		assert.NoError(t, err)
	}
	_, err = chain.ExecuteNext(ctx, plan, testFormat())
	assert.ErrorIs(t, err, context.Canceled)
	saved, err := ctx.Export()
	assert.NoError(t, err)
	codecs := gent.NewCodecRegistry()
	assert.NoError(t, chain.RegisterCodecs(codecs))
	fresh, err := gent.RestoreExecutionContext(context.Background(), saved,
		gent.RestoreBindings{Codecs: codecs})
	if !assert.NoError(t, err) {
		return
	}
	defer fresh.Release()
	plan, _, err = fresh.SavedToolPlan()
	assert.NoError(t, err)
	_, err = chain.ExecuteNext(fresh, plan, testFormat())
	assert.NoError(t, err)
	assert.Equal(t, 4, calls)
	assert.Equal(t, int64(3), fresh.Stats().GetCounter(gent.SCToolCallsErrorTotal))
	assert.Equal(t, int64(1), fresh.Stats().GetCounter(gent.SCToolRecoveryErrorTotal))
	assert.Equal(t, float64(3), fresh.Stats().GetGauge(gent.SGToolCallsErrorConsecutive))
	assert.Equal(t, float64(3), fresh.Stats().GetGauge(gent.SGToolCallsErrorConsecutiveFor+"act"))
	record := executeNativeTestCall(t, chain, fresh, nativeCall("act", "{}"))
	assert.Equal(t, gent.ToolBlocked, record.Execution().Status)
	assert.Equal(t, float64(0), fresh.Stats().GetGauge(gent.SGToolCallsErrorConsecutive))
	assert.Equal(t, float64(0), fresh.Stats().GetGauge(gent.SGToolCallsErrorConsecutiveFor+"act"))
	assert.Equal(t, int64(3), fresh.Stats().GetCounter(gent.SCToolCallsErrorTotal))
}

func TestDefaultRecoveryReportsSafeError(t *testing.T) {
	base, cancel := context.WithCancel(context.Background())
	ctx := gent.NewExecutionContext(base, "recover", gent.NewBasicLoopData(nil))
	defer ctx.Release()
	defer cancel()
	tool := gent.DefineTool("act", func(context.Context, struct{}) (
		*gent.ToolExecutionResponse, error,
	) {
		cancel()
		return nil, context.Canceled
	}).MustBuild()
	chain := New().RegisterTool(tool)
	plan, err := chain.PrepareCalls(ctx, []*gent.ModelToolCallRequest{nativeCall("act", "{}")})
	assert.NoError(t, err)
	_, err = chain.ExecuteNext(ctx, plan, testFormat())
	assert.ErrorIs(t, err, context.Canceled)
	saved, err := ctx.Export()
	assert.NoError(t, err)
	codecs := gent.NewCodecRegistry()
	assert.NoError(t, chain.RegisterCodecs(codecs))
	fresh, err := gent.RestoreExecutionContext(context.Background(), saved,
		gent.RestoreBindings{Codecs: codecs})
	if !assert.NoError(t, err) {
		return
	}
	defer fresh.Release()
	plan, _, err = fresh.SavedToolPlan()
	assert.NoError(t, err)
	record, err := chain.ExecuteNext(fresh, plan, testFormat())
	assert.NoError(t, err)
	assert.Equal(t, gent.ToolResponseRecoveryError, record.Kind())
	assert.Equal(t, gent.DefaultSteeringMessages().RecoveryError, recordText(record))
	diagnostic, err := codecs.Decode(*record.Error())
	assert.NoError(t, err)
	assert.EqualError(t, diagnostic.(error), `tool "act" has no recovery handler`)
}
