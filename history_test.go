package gent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type historyRecorder struct {
	mu      sync.Mutex
	batches []*SaveBatch
	err     error
	entered chan struct{}
	release chan struct{}
}

func (r *historyRecorder) Save(ctx context.Context, batch *SaveBatch) error {
	if r.entered != nil {
		close(r.entered)
		select {
		case <-r.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	owned, err := cloneOwned(NewCodecRegistry(), batch)
	if err != nil {
		return err
	}
	r.batches = append(r.batches, owned)
	return r.err
}

func TestOwnedHistoryAndSelectionChanges(t *testing.T) {
	task := NewTask(TextContent{Text: "original task"})
	ctx := NewExecutionContext(
		context.Background(),
		"fixture",
		NewBasicLoopData(task),
		WithRunId("history-fixture"),
	)
	task.Messages[0].Parts[0] = TextContent{Text: "changed task"}
	original := &HistoryEntry{Origin: RecordOriginal, ExpireAfterIteration: 3,
		Messages: []*MessageContent{{Role: ChatMessageTypeHuman,
			Parts: []ContentPart{TextContent{Text: "original policy"}}}}}
	want := &HistoryEntry{Origin: RecordOriginal, ExpireAfterIteration: 3,
		Messages: []*MessageContent{{Role: ChatMessageTypeHuman,
			Parts: []ContentPart{TextContent{Text: "original policy"}}}}}
	assert.NoError(t, ctx.AppendRecord(original, true))
	original.Messages[0].Parts[0] = TextContent{Text: "edited policy"}
	assert.Equal(t, HistorySnapshot{}, ctx.History())
	assert.NoError(t, ctx.CommitCheckpoint(PhaseInitial))
	actualTask, err := ctx.Task()
	assert.NoError(t, err)
	assert.Equal(t, NewTask(TextContent{Text: "original task"}), actualTask)
	records, err := ctx.Scratchpad()
	assert.NoError(t, err)
	assert.Equal(t, want, recordContent(records[0]))
	records[0].Messages[0].Parts[0] = TextContent{Text: "viewer changed value"}
	again, err := ctx.Scratchpad()
	assert.NoError(t, err)
	assert.Equal(t, want, recordContent(again[0]))
	first := ctx.ContextDraft()
	assert.NoError(t, ctx.ExpireRecords(3))
	assert.NoError(t, ctx.CommitCheckpoint(PhaseIterationCompleted))
	assert.Len(t, ctx.History().Entries, 1)
	active, err := ctx.History().ContextAt(ctx.ContextId(), 1)
	assert.NoError(t, err)
	assert.Equal(t, first.Active, active)
	active, err = ctx.History().ContextAt(ctx.ContextId(), 2)
	assert.NoError(t, err)
	assert.Empty(t, active)
	assert.ErrorIs(t, ctx.UpdateHistory(HistoryUpdate{ExpectedDraftRevision: first.Revision}),
		ErrHistoryConflict)
	bundle, err := ctx.Export()
	assert.NoError(t, err)
	assert.NoError(t, ValidateBundle(bundle))
	encoded, err := ctx.Codecs().Marshal(bundle)
	assert.NoError(t, err)
	var decoded RunView
	assert.NoError(t, ctx.Codecs().Unmarshal(encoded, &decoded))
	restored, err := RestoreExecutionContext(context.Background(), decoded, RestoreBindings{})
	if !assert.NoError(t, err) {
		return
	}
	assert.Equal(t, ctx.ContextDraft().Active, restored.ContextDraft().Active)
	assert.Equal(t, ctx.History(), restored.History())
	assert.NoError(t, restored.FlushProgress())
	continued, err := restored.Export()
	assert.NoError(t, err)
	assert.Equal(t, bundle.LastAppliedEventNumber+1, continued.LastAppliedEventNumber)
}

func TestRunCommitVisibility(t *testing.T) {
	recorder := &historyRecorder{entered: make(chan struct{}), release: make(chan struct{})}
	ctx := NewExecutionContext(
		context.Background(),
		"fixture",
		NewBasicLoopData(NewTask(TextContent{Text: "task"})),
		WithRunId("blocked-save"), WithRecorder(recorder),
	)
	observed := make(chan Event, 1)
	ctx.SetEventPublisher(historyPublisher(func(_ *ExecutionContext, event Event) {
		observed <- event
	}))
	assert.NoError(t, ctx.AppendRecord(&HistoryEntry{Origin: RecordOriginal}, true))
	start := time.Now()
	done := make(chan error, 1)
	go func() { done <- ctx.CommitCheckpoint(PhaseInitial) }()
	<-recorder.entered
	before, err := ctx.Export()
	assert.NoError(t, err)
	assert.Equal(t, uint64(0), before.LastAppliedEventNumber)
	assert.Equal(t, HistorySnapshot{}, before.History)
	select {
	case event := <-observed:
		t.Fatalf("subscriber observed an unacknowledged change: %v", event)
	default:
	}
	close(recorder.release)
	assert.NoError(t, <-done)
	assert.Equal(t, uint64(1), (<-observed).(*StateChangeEvent).EventNumber)
	after, err := ctx.Export()
	assert.NoError(t, err)
	assert.Equal(t, uint64(1), after.LastAppliedEventNumber)
	assert.GreaterOrEqual(t, after.Activity[0].Ts, start)
	assert.Equal(t, after.History.Revision, after.Checkpoint.Revision)
	assert.Equal(t, after.LastAppliedEventNumber, after.Checkpoint.EventNumber)
}

func TestRunRetryAndReplay(t *testing.T) {
	saveErr := errors.New("acknowledgment lost")
	recorder := &historyRecorder{err: saveErr}
	ctx := NewExecutionContext(context.Background(), "fixture", nil,
		WithRunId("retry-fixture"), WithRecorder(recorder))
	ctx.PublishCommonEvent("fixture:progress", "progress", nil)
	err := ctx.FlushProgress()
	assert.ErrorIs(t, err, saveErr)
	var persistenceErr *PersistenceError
	assert.ErrorAs(t, err, &persistenceErr)
	before, err := ctx.Export()
	assert.NoError(t, err)
	assert.Equal(t, uint64(0), before.LastAppliedEventNumber)
	assert.ErrorIs(t, ctx.FlushProgress(), saveErr)
	assert.Len(t, recorder.batches, 1)
	recorder.mu.Lock()
	recorder.err = nil
	recorder.mu.Unlock()
	receipt, err := ctx.RetrySave(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, uint64(1), receipt.LastAppliedEventNumber)
	assert.Equal(t, recorder.batches[0], recorder.batches[1])
	replayed, err := ApplyBatch(before, recorder.batches[0])
	assert.NoError(t, err)
	duplicate, err := ApplyBatch(replayed, recorder.batches[1])
	assert.NoError(t, err)
	assert.Equal(t, replayed, duplicate)
	actual, err := ctx.Export()
	assert.NoError(t, err)
	assert.Equal(t, actual, replayed)
}

func TestApplyBatchRejectsInvalidDeltas(t *testing.T) {
	type input struct {
		runId    string
		previous uint64
		number   uint64
	}
	type expected struct{ err error }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"wrong run", input{"other", 0, 1}, expected{ErrWrongRun}},
		{"missing predecessor", input{"fixture", 1, 2}, expected{ErrEventGap}},
		{"number gap", input{"fixture", 0, 2}, expected{ErrEventGap}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			view := RunView{SchemaVersion: RunSchemaVersion, RunId: "fixture"}
			batch := &SaveBatch{SchemaVersion: RunSchemaVersion, RunId: tt.input.runId,
				PreviousEventNumber: tt.input.previous, Events: []*RecordedEvent{{
					RunId: tt.input.runId, EventNumber: tt.input.number,
					Type: "progress", Ts: time.Now()}}}
			var err error
			batch.BatchId, err = BatchDigest(batch)
			assert.NoError(t, err)
			_, err = ApplyBatch(view, batch)
			assert.ErrorIs(t, err, tt.expected.err)
			assert.Equal(t, RunView{SchemaVersion: RunSchemaVersion, RunId: "fixture"}, view)
		})
	}
}
