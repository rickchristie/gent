package gent_test

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	genttest "github.com/rickchristie/gent/internal/tt"
	"github.com/stretchr/testify/assert"
)

func TestCheckpointDeliveryCapacityAfterReceive(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	type input struct{ capacity int }
	type expected struct {
		numbers []uint64
		err     error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	for _, test := range []testCase{
		{"one slot", input{1}, expected{[]uint64{1, 2}, nil}},
		{"two slots", input{2}, expected{[]uint64{1, 2}, nil}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := gent.NewExecutionContext(context.Background(), "root", nil)
			defer ctx.Release()
			sub, err := ctx.SubscribeAllWithConfig(gent.DeliveryConfig{MaxEvents: test.input.capacity})
			if !assert.NoError(t, err) {
				return
			}
			defer sub.Close()
			assert.NoError(t, ctx.EmitChunk(checkpointChunk("First.")))
			// A consumer can publish its next event as soon as it receives the current event.
			runtime.Gosched()
			first := checkpointReceive(t, sub.Chunks())
			assert.NoError(t, ctx.EmitChunk(checkpointChunk("Later.")))
			ctx.CloseStreams()
			numbers := []uint64{first.EventNumber}
			for chunk := range sub.Chunks() {
				numbers = append(numbers, chunk.EventNumber)
			}
			<-sub.Done()
			assert.Equal(t, test.expected, expected{numbers, sub.Err()})
		})
	}
}

func TestCheckpointPendingLimitsDuringSave(t *testing.T) {
	type input struct {
		terminal bool
		policy   gent.CheckpointPolicy
	}
	type expected struct {
		completedBeforeSave bool
		newEvents           uint64
		err                 error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	countLimit := gent.CheckpointPolicy{MaxEvents: 2}
	byteLimit := gent.CheckpointPolicy{MaxBytes: 1}
	want := expected{false, 34, nil}
	for _, test := range []testCase{
		{"progress count", input{false, countLimit}, want},
		{"terminal count", input{true, countLimit}, want},
		{"progress bytes", input{false, byteLimit}, want},
		{"terminal bytes", input{true, byteLimit}, want},
	} {
		t.Run(test.name, func(t *testing.T) {
			entered, release := make(chan struct{}, 1), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			var block atomic.Bool
			store := newCheckpointStore("pending-limit")
			store.before = func(*gent.SaveBatch) error {
				if block.CompareAndSwap(true, false) {
					entered <- struct{}{}
					<-release
				}
				return nil
			}
			root := gent.NewExecutionContext(context.Background(), "root", nil,
				gent.WithRunId("pending-limit"), gent.WithRecorder(store),
				gent.WithCheckpointPolicy(test.input.policy))
			defer root.Release()
			child := root.SpawnChild("child", nil)
			assert.NoError(t, root.CommitCheckpoint(gent.PhaseInitial))
			initial, err := root.Export()
			if !assert.NoError(t, err) {
				return
			}
			events, err := root.SubscribeEvents(gent.DeliveryConfig{})
			if !assert.NoError(t, err) {
				return
			}
			defer events.Close()
			chunks, err := root.SubscribeAllWithConfig(gent.DeliveryConfig{})
			if !assert.NoError(t, err) {
				return
			}
			defer chunks.Close()
			var workers sync.WaitGroup
			defer func() { unblock(); workers.Wait() }()
			if test.input.terminal {
				child.SetTermination(gent.TerminationSuccess,
					[]gent.ContentPart{gent.TextContent{Text: "Accepted."}}, nil)
				child.SetAcceptedValue(genttest.Value("Accepted."))
				child.PublishAfterExecution(gent.TerminationSuccess, nil)
			}
			block.Store(true)
			saveDone := make(chan error, 1)
			workers.Add(1)
			go func() {
				defer workers.Done()
				if test.input.terminal {
					saveDone <- child.CommitCheckpoint(gent.PhaseTerminal)
					return
				}
				for range 2 {
					if err := child.EmitChunk(checkpointChunk("Before save.")); err != nil {
						saveDone <- err
						return
					}
				}
				saveDone <- nil
			}()
			checkpointReceive(t, entered)
			producerDone := make(chan error, 1)
			workers.Add(1)
			go func() {
				defer workers.Done()
				for range 32 {
					if err := root.EmitChunk(checkpointChunk("Concurrent progress.")); err != nil {
						producerDone <- err
						return
					}
				}
				producerDone <- nil
			}()
			actual := expected{}
			select {
			case actual.err = <-producerDone:
				actual.completedBeforeSave = true
			case <-time.After(100 * time.Millisecond):
			}
			if test.input.terminal {
				select {
				case event := <-events.Events():
					t.Fatalf("event passed the terminal save: %v", event)
				default:
				}
				select {
				case chunk := <-chunks.Chunks():
					t.Fatalf("chunk passed the terminal save: %v", chunk)
				default:
				}
			}
			unblock()
			assert.NoError(t, checkpointReceive(t, saveDone))
			if !actual.completedBeforeSave {
				actual.err = checkpointReceive(t, producerDone)
			}
			assert.NoError(t, root.FlushProgress())
			view, err := root.Export()
			assert.NoError(t, err)
			assert.NoError(t, gent.ValidateBundle(view))
			actual.newEvents = view.LastAppliedEventNumber - initial.LastAppliedEventNumber
			assert.Equal(t, test.expected, actual)
			root.CloseStreams()
			var liveEvents []*gent.RecordedEvent
			for event := range events.Events() {
				liveEvents = append(liveEvents, event)
			}
			var liveChunks []gent.StreamChunk
			for chunk := range chunks.Chunks() {
				liveChunks = append(liveChunks, chunk)
			}
			assert.Equal(t, view.Activity[initial.LastAppliedEventNumber:], liveEvents)
			assert.Equal(t, contentChunks(t, view.Activity, root.Codecs()), liveChunks)
			assert.NoError(t, events.Err())
			assert.NoError(t, chunks.Err())
		})
	}
}
