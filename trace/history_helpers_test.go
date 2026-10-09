package trace

import (
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func newTestSequencer(t *testing.T, id string, cfg Config) *Sequencer {
	t.Helper()
	seq, err := NewSequencer(id, cfg)
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	return seq
}

// Project a fixture representing an already persisted event. History tests exercise acceptance
// through real execution contexts; these cases isolate display/retention behavior.
func recordTestValue(t *testing.T, seq *Sequencer, kind string, value any) {
	t.Helper()
	registry := gent.NewCodecRegistry()
	encoded, err := registry.Encode(value)
	if !assert.NoError(t, err) {
		return
	}
	snapshot := seq.Snapshot()
	event := &gent.RecordedEvent{Type: kind, Payload: &encoded, RunId: snapshot.RunId,
		EventNumber: snapshot.LastEventNumber + 1, Ts: time.Now()}
	if typed, ok := value.(interface{ EventBase() *gent.BaseEvent }); ok {
		base := typed.EventBase()
		event.ContextId, event.Iteration = base.ContextId, base.Iteration
		event.AttemptId = base.AttemptId
		event.ParentContextId, event.EventName = base.ParentContextId, base.EventName
		if !base.Timestamp.IsZero() {
			event.Ts = base.Timestamp
		}
	}
	switch typed := value.(type) {
	case *gent.BeforeModelCallEvent:
		event.ModelCallId = typed.ModelCallId
	case *gent.AfterModelCallEvent:
		event.ModelCallId = typed.ModelCallId
	case *gent.BeforeToolCallEvent:
		event.ToolCallId = typed.ToolCallId
	case *gent.AfterToolCallEvent:
		event.ToolCallId = typed.ToolCallId
	case gent.StreamChunk:
		event.ModelCallId = typed.ModelCallId
		event.ContextId, event.ParentContextId = typed.ContextId, typed.ParentContextId
		event.Iteration = typed.Iteration
	}
	assert.NoError(t, seq.ObserveCommitted([]*gent.RecordedEvent{event}, registry))
}
