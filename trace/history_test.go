package trace

import (
	"context"
	"errors"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

type batchCollector struct{ batches []*gent.SaveBatch }

func (c *batchCollector) Save(_ context.Context, batch *gent.SaveBatch) error {
	c.batches = append(c.batches, batch)
	return nil
}

func TestHistoryReplay_ChildInterruptionIsScoped(t *testing.T) {
	seq := newTestSequencer(t, "child-interruption", Config{})
	root := gent.NewExecutionContext(context.Background(), "root", nil,
		gent.WithRunId(seq.Snapshot().RunId), gent.WithObserver(seq))
	root.PublishBeforeExecution()
	child := root.SpawnChild("interrupted child", nil)
	sibling := root.SpawnChild("running sibling", nil)
	for _, ctx := range []*gent.ExecutionContext{root, child, sibling} {
		if ctx != root {
			ctx.PublishBeforeExecution()
		}
		ctx.IncrementIteration()
		ctx.PublishBeforeIteration()
		ctx.PublishBeforeModelCall("fixture", gent.NewModelCallRequest(nil))
		ctx.PublishBeforeToolCall("lookup", "P10")
	}
	assert.NoError(t, root.FlushProgress())
	assert.NoError(t, child.RecordInterruption(context.Canceled))
	type expected struct {
		modelCalls []StepStatus
		toolCalls  []StepStatus
		iterations []StepStatus
	}
	var actual expected
	snapshot := seq.Snapshot()
	for _, call := range snapshot.ModelCalls {
		actual.modelCalls = append(actual.modelCalls, call.Status)
	}
	for _, call := range snapshot.ToolCalls {
		actual.toolCalls = append(actual.toolCalls, call.Status)
	}
	for _, iteration := range snapshot.Iterations {
		actual.iterations = append(actual.iterations, iteration.Status)
	}
	assert.Equal(t, expected{
		modelCalls: []StepStatus{StepStatusRunning, StepStatusInterrupted, StepStatusRunning},
		toolCalls:  []StepStatus{StepStatusRunning, StepStatusInterrupted, StepStatusRunning},
		iterations: []StepStatus{StepStatusRunning, StepStatusInterrupted, StepStatusRunning},
	}, actual)
	bundle, err := root.Export()
	assert.NoError(t, err)
	loaded, err := RestoreSequencer(bundle, Config{})
	if assert.NoError(t, err) {
		assert.Equal(t, snapshot, loaded.Snapshot())
	}
}

func TestHistoryReplay_CursorsAndLostCompletion(t *testing.T) {
	collector := &batchCollector{}
	config := Config{MaxRecentEvents: 2, IncludeRunOutput: true,
		IncludeChunkText: true, IncludeStateChanges: true}
	seq := newTestSequencer(t, "cursor", config)
	ctx := gent.NewExecutionContext(context.Background(), "root", nil,
		gent.WithRunId(seq.Snapshot().RunId), gent.WithObserver(seq), gent.WithRecorder(collector))
	ctx.PublishBeforeExecution()
	for i := 1; i < 120; i++ {
		ctx.PublishCommonEvent("fixture:progress", "accepted progress", i)
	}
	assert.NoError(t, ctx.CommitCheckpoint(gent.PhaseInitial))
	for i := 0; i < 8; i++ {
		assert.NoError(t, ctx.EmitChunk(gent.StreamChunk{
			StreamId: "partial", MetaContent: []*gent.MetaContent{
				{
					Type: gent.MetaContentIntermediary, Text: "x",
				},
			},
		}))
	}
	bundle, err := ctx.Export()
	assert.NoError(t, err)
	assert.Equal(t, uint64(120), bundle.Checkpoint.EventNumber)
	assert.Equal(t, uint64(128), bundle.LastAppliedEventNumber)
	view := gent.RunView{SchemaVersion: gent.RunSchemaVersion, RunId: "cursor"}
	for _, batch := range collector.batches {
		view, err = gent.ApplyBatch(view, batch)
		assert.NoError(t, err)
	}
	assert.Equal(t, bundle, view)
	bytes, err := ctx.Codecs().Marshal(bundle)
	assert.NoError(t, err)
	var saved gent.RunView
	assert.NoError(t, ctx.Codecs().Unmarshal(bytes, &saved))
	fresh, err := RestoreSequencer(saved, config)
	if !assert.NoError(t, err) {
		return
	}
	assert.Equal(t, seq.Snapshot(), fresh.Snapshot())
	restored, err := gent.RestoreExecutionContext(context.Background(), saved,
		gent.RestoreBindings{Recorder: collector, Observers: []gent.RecordedEventObserver{fresh}})
	if !assert.NoError(t, err) {
		return
	}
	assert.NoError(t, restored.FlushProgress())
	assert.Equal(t, uint64(129), fresh.Snapshot().LastEventNumber)
	assert.NoError(t, restored.EmitChunk(gent.StreamChunk{
		StreamId: "partial", MetaContent: []*gent.MetaContent{
			{
				Type: gent.MetaContentIntermediary, Text: "y",
			},
		},
	}))
	events, err := fresh.EventsAfter(129)
	assert.NoError(t, err)
	assert.Equal(t, StitchStatusGap, ValidateStitch(128, events).Status)
	reloaded, err := restored.Export()
	assert.NoError(t, err)
	assert.Equal(t, uint64(130), reloaded.LastAppliedEventNumber)
	assert.Equal(t, uint64(129), reloaded.Activity[128].EventNumber)
	assert.NoError(t, restored.EmitChunk(gent.StreamChunk{
		StreamId: "partial", MetaContent: []*gent.MetaContent{
			{
				Type: gent.MetaContentIntermediary, Text: "z",
			},
		},
	}))
	events, err = fresh.EventsAfter(reloaded.LastAppliedEventNumber)
	assert.NoError(t, err)
	assert.Equal(t, StitchStatusOK, ValidateStitch(130, events).Status)
	assert.Equal(t, uint64(131), events[0].EventNumber)
	// Simulate a lost final publication: a periodic full read discovers completion without
	// requiring another event to expose a gap.
	output := []gent.ContentPart{gent.TextContent{Text: "saved answer"}}
	restored.SetTermination(gent.TerminationSuccess, output, nil)
	restored.PublishAfterExecution(gent.TerminationSuccess, nil)
	assert.NoError(t, restored.CommitCheckpoint(gent.PhaseTerminal))
	terminal, err := restored.Export()
	assert.NoError(t, err)
	assert.Equal(t, gent.PhaseTerminal, terminal.Checkpoint.Phase)
	assert.Equal(t, uint64(133), terminal.LastAppliedEventNumber)
	assert.NoError(t, gent.ValidateBundle(terminal))
	late, err := RestoreSequencer(terminal, config)
	assert.NoError(t, err)
	assert.Equal(t, fresh.Snapshot(), late.Snapshot())
	_, err = fresh.EventsAfter(0)
	assert.ErrorIs(t, err, ErrReplayUnavailable)
}

type interruptedProvider struct{ started chan struct{} }

func (m *interruptedProvider) GenerateContentStream(ctx context.Context, _ gent.ModelCallRequest) (
	gent.Stream, error,
) {
	stream := gent.NewStreamWithDuration()
	go func() {
		stream.SendMeta(&gent.MetaContent{
			Type: gent.MetaContentIntermediary, Text: "partial policy evidence",
		})
		close(m.started)
		<-ctx.Done()
		stream.Complete(&gent.ContentResponse{
			Choices: []*gent.ContentChoice{{CandidateContent: "partial policy evidence"}},
			Info:    &gent.GenerationInfo{InputTokens: 12, OutputTokens: 8, TotalTokens: 20},
		}, ctx.Err())
	}()
	return stream, nil
}

type completedProvider struct{ calls int }

func (m *completedProvider) GenerateContentStream(context.Context, gent.ModelCallRequest) (
	gent.Stream, error,
) {
	m.calls++
	return gent.NewCompletedStream(&gent.ContentResponse{
		Choices: []*gent.ContentChoice{{CandidateContent: "complete answer"}},
		Info:    &gent.GenerationInfo{InputTokens: 12, OutputTokens: 8, TotalTokens: 20},
	}, nil), nil
}

func TestHistoryReplay_ModelAttemptsAndKnownUsage(t *testing.T) {
	config := Config{
		IncludeChunkText:      true,
		IncludeModelRequests:  true,
		IncludeModelResponses: true,
	}
	seq := newTestSequencer(t, "attempt", config)
	worker, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := gent.NewExecutionContext(worker, "root", nil,
		gent.WithRunId(seq.Snapshot().RunId), gent.WithObserver(seq))
	ctx.SetLimits(nil)
	ctx.PublishBeforeExecution()
	ctx.IncrementIteration()
	ctx.PublishBeforeIteration()
	assert.NoError(t, ctx.CommitCheckpoint(gent.PhaseIterationReady))
	request := gent.NewModelCallRequest([]gent.ModelMessage{
		gent.TextParts(gent.ChatMessageTypeHuman, "policy question"),
	})
	model := &interruptedProvider{started: make(chan struct{})}
	stream, err := ctx.GenerateContentStream(model, gent.ModelCall{StreamId: "answer"}, request)
	if !assert.NoError(t, err) {
		return
	}
	<-model.started
	chunk := <-stream.Chunks()
	assert.Equal(t, "partial policy evidence",
		gent.MetaText(chunk.MetaContent, gent.MetaContentIntermediary))
	cancel()
	for range stream.Chunks() {
	}
	response, err := stream.Response()
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 20, response.Info.TotalTokens)
	assert.NoError(t, ctx.RecordInterruption(err))
	bundle, err := ctx.Export()
	assert.NoError(t, err)
	freshSeq, err := RestoreSequencer(bundle, config)
	assert.NoError(t, err)
	fresh, err := gent.RestoreExecutionContext(context.Background(), bundle,
		gent.RestoreBindings{Observers: []gent.RecordedEventObserver{freshSeq}})
	if !assert.NoError(t, err) {
		return
	}
	assert.Equal(t, int64(20), fresh.Stats().GetCounter(gent.SCTotalTokens))
	freshModel := &completedProvider{}
	stream, err = fresh.GenerateContentStream(
		freshModel,
		gent.ModelCall{StreamId: "answer"},
		request,
	)
	assert.NoError(t, err)
	for range stream.Chunks() {
	}
	_, err = stream.Response()
	assert.NoError(t, err)
	assert.Equal(t, 1, freshModel.calls)
	assert.Equal(t, int64(40), fresh.Stats().GetCounter(gent.SCTotalTokens))
	snapshot := freshSeq.Snapshot()
	if assert.Len(t, snapshot.ModelCalls, 2) {
		first, second := snapshot.ModelCalls[0], snapshot.ModelCalls[1]
		assert.Equal(t, first.Id, second.Id)
		assert.NotEqual(t, first.AttemptId, second.AttemptId)
		assert.NotEqual(t, StepStatusSucceeded, first.Status)
		assert.Equal(t, StepStatusSucceeded, second.Status)
		assert.Equal(t, "partial policy evidence",
			gent.MetaText(first.Stream.MetaContent, gent.MetaContentIntermediary))
		assert.Equal(t, "complete answer",
			gent.MetaText(second.Stream.MetaContent, gent.MetaContentIntermediary))
	}
	assert.Equal(t, 40, snapshot.Stats.TotalTokens)
	assert.True(t, errors.Is(ctx.Context().Err(), context.Canceled))
}
