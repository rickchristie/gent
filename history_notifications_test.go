package gent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type historyPublisher func(*ExecutionContext, Event)

func (p historyPublisher) MaxRecursion() int { return 10 }
func (p historyPublisher) Dispatch(ctx *ExecutionContext, event Event) {
	p(ctx, event)
}

func TestCommittedChangesAllowReentrantPublication(t *testing.T) {
	type input struct{ child bool }
	type expected struct{ records int }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"root change", input{false}, expected{2}},
		{"child change flushed by root", input{true}, expected{2}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			root := NewExecutionContext(context.Background(), "root", nil)
			owner := root
			if tt.input.child {
				owner = root.SpawnChild("child", nil)
			}
			var changes []HistoryChange
			var received []Event
			owner.SetEventPublisher(historyPublisher(func(ctx *ExecutionContext, event Event) {
				change, ok := event.(*StateChangeEvent)
				if !ok {
					return
				}
				assert.Same(t, owner, ctx)
				accepted := ctx.History()
				assert.Equal(t, change.Change, accepted.Changes[len(accepted.Changes)-1])
				owned, err := cloneOwned(ctx.Codecs(), change.Change)
				assert.NoError(t, err)
				changes = append(changes, owned)
				copy, err := ctx.copyEvent(event)
				assert.NoError(t, err)
				received = append(received, copy)
				if len(changes) == 1 {
					record := &HistoryEntry{Origin: RecordOriginal}
					assert.NoError(t, ctx.AppendRecord(record, true))
					assert.NoError(t, ctx.FlushProgress())
				}
				// Subscriber-owned values cannot rewrite the accepted entry or another read.
				change.Change.AppendedEntries[0].Id = "subscriber mutation"
			}))
			assert.NoError(t, owner.AppendRecord(&HistoryEntry{Origin: RecordOriginal}, true))
			done := make(chan error, 1)
			go func() { done <- root.CommitCheckpoint(PhaseInitial) }()
			select {
			case err := <-done:
				assert.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("committed subscriber blocked while publishing its next change")
			}
			assert.Len(t, owner.History().Entries, tt.expected.records)
			assert.Equal(t, changes, owner.History().Changes)
			var observed []Event
			for _, event := range owner.Events() {
				if _, ok := event.(*StateChangeEvent); ok {
					observed = append(observed, event)
				}
			}
			assert.Equal(t, received, observed)
			assert.NoError(t, owner.RecordingError())
		})
	}
}

func TestChildTasksAreOwnedBeforeConcurrentCheckpoints(t *testing.T) {
	root := NewExecutionContext(context.Background(), "root", nil)
	var workers sync.WaitGroup
	for i := 0; i < 20; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			child := root.SpawnChild("child", NewBasicLoopData(&Task{Text: "saved child task"}))
			assert.NoError(t, root.CompleteChild(child))
		}()
	}
	for i := 0; i < 20; i++ {
		assert.NoError(t, root.CommitCheckpoint(PhaseIterationCompleted))
		bundle, err := root.Export()
		assert.NoError(t, err)
		for _, child := range bundle.Checkpoint.Contexts[1:] {
			task, err := decodeAs[*Task](root.Codecs(), child.Task)
			assert.NoError(t, err)
			assert.Equal(t, &Task{Text: "saved child task"}, task)
		}
	}
	workers.Wait()
	assert.NoError(t, root.CommitCheckpoint(PhaseIterationCompleted))
	bundle, err := root.Export()
	assert.NoError(t, err)
	assert.Len(t, bundle.Checkpoint.Contexts, 21)
	assert.NoError(t, ValidateBundle(bundle))
}
