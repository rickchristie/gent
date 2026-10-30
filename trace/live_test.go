package trace

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

type liveRecorder func(context.Context, *gent.SaveBatch) error

func (save liveRecorder) Save(ctx context.Context, batch *gent.SaveBatch) error {
	return save(ctx, batch)
}

func receiveLive(t *testing.T, events <-chan *Event) *Event {
	t.Helper()
	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("subscription closed before expected event")
		}
		return event
	case <-time.After(10 * time.Second):
		t.Fatal("live event did not arrive")
		return nil
	}
}

func TestLiveFeedBeforeSave(t *testing.T) {
	type input struct{ saveError error }
	type expected struct{ saved uint64 }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	for _, test := range []testCase{
		{"commit", input{}, expected{2}},
		{"failure", input{errors.New("save failed")}, expected{0}},
	} {
		t.Run(test.name, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			recorder := liveRecorder(func(context.Context, *gent.SaveBatch) error {
				close(entered)
				<-release
				return test.input.saveError
			})
			seq := newTestSequencer(t, "live", Config{IncludeChunkText: true})
			committed := newTestSequencer(t, "live", Config{IncludeChunkText: true})
			ctx := gent.NewExecutionContext(context.Background(), "test", nil,
				gent.WithRunId("live"), gent.WithRecorder(recorder), gent.WithObserver(committed))
			defer ctx.Release()
			feed, err := seq.Follow(ctx, gent.DeliveryConfig{})
			assert.NoError(t, err)
			defer func() { assert.NoError(t, feed.Close()) }()
			sub, err := seq.SubscribeWithLimits(gent.DeliveryConfig{})
			assert.NoError(t, err)
			defer sub.Close()
			ctx.PublishBeforeExecution()
			assert.NoError(t, ctx.EmitChunk(gent.StreamChunk{MetaContent: []*gent.MetaContent{{
				Type:    gent.MetaContentIntermediary,
				Content: []gent.ContentPart{gent.TextContent{Text: "Live preview"}}}}}))
			var numbers []uint64
			for range 2 {
				numbers = append(numbers, receiveLive(t, sub.Events()).EventNumber)
			}
			assert.Equal(t, []uint64{1, 2}, numbers)
			done := make(chan error, 1)
			go func() { done <- ctx.FlushProgress() }()
			select {
			case <-entered:
			case <-time.After(10 * time.Second):
				t.Fatal("save was not called")
			}
			assert.Equal(t, uint64(2), seq.Snapshot().LastEventNumber)
			assert.Equal(t, uint64(0), committed.Snapshot().LastEventNumber)
			view, err := ctx.Export()
			assert.NoError(t, err)
			assert.Equal(t, uint64(0), view.LastAppliedEventNumber)
			unblock()
			assert.ErrorIs(t, <-done, test.input.saveError)
			ctx.Release()
			assert.NoError(t, feed.Wait())
			assert.Equal(t, test.expected.saved, committed.Snapshot().LastEventNumber)
			view, err = ctx.Export()
			assert.NoError(t, err)
			assert.Equal(t, test.expected.saved, view.LastAppliedEventNumber)
			seq.Close()
			var extra []*Event
			for event := range sub.Events() {
				extra = append(extra, event)
			}
			assert.Nil(t, extra)
			assert.NoError(t, sub.Err())
		})
	}
}

func TestLiveFeedRestore(t *testing.T) {
	ctx := gent.NewExecutionContext(context.Background(), "test", nil, gent.WithRunId("restore"))
	defer ctx.Release()
	ctx.PublishBeforeExecution()
	assert.NoError(t, ctx.CommitCheckpoint(gent.PhaseInitial))
	saved, err := ctx.Export()
	assert.NoError(t, err)
	seq, err := RestoreSequencer(saved, Config{})
	assert.NoError(t, err)
	defer seq.Close()
	sub, err := seq.SubscribeWithLimits(gent.DeliveryConfig{})
	assert.NoError(t, err)
	defer sub.Close()
	var feed *LiveFeed
	restored, err := gent.RestoreExecutionContext(context.Background(), saved, gent.RestoreBindings{
		Configure: func(fresh *gent.ExecutionContext) error {
			var err error
			feed, err = seq.Follow(fresh, gent.DeliveryConfig{})
			return err
		},
	})
	if !assert.NoError(t, err) {
		return
	}
	defer restored.Release()
	defer func() { assert.NoError(t, feed.Close()) }()
	event := receiveLive(t, sub.Events())
	assert.Equal(t, saved.LastAppliedEventNumber+1, event.EventNumber)
	assert.Equal(t, EventType("attempt_started"), event.Type)
	assert.NoError(t, restored.FlushProgress())
	restored.Release()
	assert.NoError(t, feed.Wait())
	view, err := restored.Export()
	assert.NoError(t, err)
	replay, err := RestoreSequencer(view, Config{})
	assert.NoError(t, err)
	defer replay.Close()
	assert.Equal(t, replay.Snapshot(), seq.Snapshot())
}

func TestLiveSubscriptionLimits(t *testing.T) {
	type input struct {
		cfg   gent.DeliveryConfig
		count int
	}
	type expected struct{ err error }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	for _, test := range []testCase{
		{"count", input{gent.DeliveryConfig{MaxEvents: 1}, 2},
			expected{gent.ErrDeliveryOverflow}},
		{"bytes", input{gent.DeliveryConfig{MaxBytes: 1}, 1},
			expected{gent.ErrDeliveryOverflow}},
		{"timeout", input{gent.DeliveryConfig{Timeout: time.Millisecond}, 1},
			expected{gent.ErrDeliveryTimeout}},
	} {
		t.Run(test.name, func(t *testing.T) {
			seq := newTestSequencer(t, "limits", Config{})
			slow, err := seq.SubscribeWithLimits(test.input.cfg)
			assert.NoError(t, err)
			defer slow.Close()
			fast, err := seq.SubscribeWithLimits(gent.DeliveryConfig{})
			assert.NoError(t, err)
			defer fast.Close()
			for range test.input.count {
				recordTestValue(t, seq, "common", &gent.CommonEvent{})
			}
			select {
			case <-slow.Done():
			case <-time.After(10 * time.Second):
				t.Fatal("slow subscriber did not stop")
			}
			assert.ErrorIs(t, slow.Err(), test.expected.err)
			var numbers []uint64
			for range test.input.count {
				numbers = append(numbers, receiveLive(t, fast.Events()).EventNumber)
			}
			assert.Equal(t, uint64(test.input.count), seq.Snapshot().LastEventNumber)
			assert.Equal(t, StitchStatusOK, ValidateStitch(0, seq.Snapshot().RecentEvents).Status)
			assert.Equal(t, seq.Snapshot().RecentEvents[len(numbers)-1].EventNumber,
				numbers[len(numbers)-1])
			assert.NoError(t, fast.Err())
		})
	}
}
