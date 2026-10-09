package gent

import (
	"fmt"
	"reflect"
)

// RecordedEventObserver receives accepted events only. It is separate from mutable execution
// hooks; a hook's registration order cannot change the request accepted by the recorder.
type RecordedEventObserver interface {
	// The registry interprets saved payloads without consulting mutable execution state.
	ObserveCommitted([]*RecordedEvent, *CodecRegistry) error
}

func registerFrameworkCodecs(registry *CodecRegistry) {
	values := []any{
		BaseEvent{},
		BeforeExecutionEvent{},
		AfterExecutionEvent{},
		BeforeIterationEvent{},
		AfterIterationEvent{},
		BeforeSystemPromptEvent{},
		BeforeModelCallEvent{},
		AfterModelCallEvent{},
		BeforeToolCallEvent{},
		AfterToolCallEvent{},
		ParseErrorEvent{},
		ValidatorCalledEvent{},
		ValidatorResultEvent{},
		ErrorEvent{},
		LimitExceededEvent{},
		CompactionEvent{},
		CommonEvent{},
		ModelCallRequest{},
		StreamChunk{},
		AgentLoopResult{},
		ExecutionResult{},
		Limit{},
		StatKey(""),
		LimitType(""),
		ParseErrorType(""),
		TerminationReason(""),
		LoopAction(""),
		ToolCall{},
		TerminationStatus(0),
		TerminationResult{},
		ValidationResult{},
	}
	values = append(values, PlannedToolCall{}, SavedToolCall{}, ToolPlan{}, EncodedValue{})
	values = append(values, HistoryRevision(0), EntryId(""))
	for _, value := range values {
		typ := reflect.TypeOf(value)
		registry.add(registeredValue{id: typ.PkgPath() + "/" + typ.Name(), version: 1, typ: typ})
	}
}

func recordedEventType(event Event) string {
	switch event.(type) {
	case *BeforeExecutionEvent:
		return "execution_started"
	case *AfterExecutionEvent:
		return "execution_finished"
	case *BeforeIterationEvent:
		return "iteration_started"
	case *AfterIterationEvent:
		return "iteration_finished"
	case *BeforeSystemPromptEvent:
		return "system_prompt"
	case *BeforeModelCallEvent:
		return "model_call_started"
	case *AfterModelCallEvent:
		return "model_call_finished"
	case *BeforeToolCallEvent:
		return "tool_call_started"
	case *AfterToolCallEvent:
		return "tool_call_finished"
	case *ParseErrorEvent:
		return "parse_error"
	case *ValidatorCalledEvent:
		return "validator_called"
	case *ValidatorResultEvent:
		return "validator_result"
	case *ErrorEvent:
		return "error"
	case *LimitExceededEvent:
		return "limit_exceeded"
	case *CompactionEvent:
		return "compaction"
	case *CommonEvent:
		return "common"
	default:
		return "custom"
	}
}

func (ctx *ExecutionContext) recordEvent(event Event) error {
	if err := ctx.RecordingError(); err != nil {
		return err
	}
	value, err := ctx.run.codecs.Encode(event)
	if err != nil {
		return ctx.stopRecording(fmt.Errorf("gent: record %T: %w", event, err))
	}
	base := eventBase(event)
	recorded := &RecordedEvent{Type: recordedEventType(event), ContextId: ctx.ContextId(),
		Iteration: ctx.Iteration(), AttemptId: ctx.AttemptId(), Payload: &value}
	if base != nil {
		if base.AttemptId != "" {
			recorded.AttemptId = base.AttemptId
		}
		recorded.EventName = base.EventName
		recorded.ContextId = base.ContextId
		recorded.ParentContextId = base.ParentContextId
		recorded.Iteration = base.Iteration
		recorded.Ts = base.Timestamp
	}
	switch e := event.(type) {
	case *BeforeModelCallEvent:
		recorded.ModelCallId = e.ModelCallId
	case *AfterModelCallEvent:
		recorded.ModelCallId = e.ModelCallId
	case *BeforeToolCallEvent:
		recorded.ToolCallId = e.ToolCallId
	case *AfterToolCallEvent:
		recorded.ToolCallId = e.ToolCallId
	}
	ctx.run.mu.Lock()
	ctx.run.pending = append(ctx.run.pending, recorded)
	ctx.run.mu.Unlock()
	return nil
}

func (ctx *ExecutionContext) recordChunk(chunk StreamChunk) error {
	value, err := ctx.run.codecs.Encode(chunk)
	if err != nil {
		return ctx.stopRecording(err)
	}
	ctx.run.mu.Lock()
	ctx.run.pending = append(ctx.run.pending, &RecordedEvent{Type: "model_stream_chunk",
		ContextId: chunk.ContextId, ParentContextId: chunk.ParentContextId,
		Iteration: chunk.Iteration,
		AttemptId: ctx.run.attempt, ModelCallId: chunk.ModelCallId, Payload: &value})
	ctx.run.mu.Unlock()
	// Synchronous acceptance supplies bounded backpressure. The producer cannot outrun storage,
	// and stream consumers receive a chunk only after its save succeeds.
	return ctx.FlushProgress()
}

func (ctx *ExecutionContext) DecodeEvent(event *RecordedEvent) (any, error) {
	return ctx.run.codecs.DecodeEvent(event)
}

// DecodeEvent reads a frozen event using the run's registered content types.
func (registry *CodecRegistry) DecodeEvent(event *RecordedEvent) (any, error) {
	if event == nil {
		return nil, fmt.Errorf("gent: nil recorded event")
	}
	if event.Change != nil {
		return cloneOwned(registry, *event.Change)
	}
	if event.Payload == nil {
		return nil, nil
	}
	return registry.Decode(*event.Payload)
}
