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
		BeforeModelCallEvent{},
		AfterModelCallEvent{},
		BeforeToolCallEvent{},
		AfterToolCallEvent{},
		ParseErrorEvent{},
		OutputValidationEvent{},
		OutputValidationStage(""),
		ErrorEvent{},
		LimitExceededEvent{},
		CompactionEvent{},
		CommonEvent{},
		ModelCallRequest{},
		AgentLoopResult{},
		ExecutionResult{},
		Limit{},
		StatKey(""),
		LimitType(""),
		ParseErrorType(""),
		TerminationReason(""),
		LoopAction(""),
	}
	values = append(values, PlannedToolCall{}, SavedToolCall{}, ToolPlan{}, EncodedValue{})
	values = append(values, HistoryRevision(0), EntryId(""))
	for _, value := range values {
		typ := reflect.TypeOf(value)
		registry.add(registeredValue{id: typ.PkgPath() + "/" + typ.Name(), version: 1, typ: typ})
	}
	chunkCodec := streamChunkCodec{registry: registry}
	registry.add(registeredValue{id: chunkCodec.Type(), version: chunkCodec.Version(),
		typ: reflect.TypeFor[StreamChunk](), codec: chunkCodec})
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
	case *OutputValidationEvent:
		return "output_validation"
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
	if recorded.Type == "execution_finished" {
		// Assign this event's sequence only inside its terminal save group. Other contexts can
		// still save progress while completion hooks finish, without exposing an unsaved outcome.
		err = ctx.run.err
		if err == nil {
			ctx.terminalEvent = recorded
		}
	} else {
		err = ctx.run.queueEvent(recorded)
	}
	ctx.run.mu.Unlock()
	if err != nil {
		return ctx.stopRecording(err)
	}
	return ctx.flushPendingLimit()
}

func (ctx *ExecutionContext) recordChunk(chunk *StreamChunk) error {
	value, err := ctx.run.codecs.Encode(*chunk)
	if err != nil {
		return ctx.stopRecording(err)
	}
	ctx.run.mu.Lock()
	recorded := &RecordedEvent{Type: "model_stream_chunk",
		ContextId: chunk.ContextId, ParentContextId: chunk.ParentContextId,
		Iteration: chunk.Iteration, Ts: chunk.Timestamp,
		AttemptId: ctx.run.attempt, ModelCallId: chunk.ModelCallId, Payload: &value}
	err = ctx.run.queueEvent(recorded)
	if err == nil {
		var frozen any
		frozen, err = ctx.run.codecs.DecodeEvent(recorded)
		if err == nil {
			chunk.RunId, chunk.EventNumber = recorded.RunId, recorded.EventNumber
			chunk.Timestamp = chunk.Timestamp.Round(0)
			if ctx.run.deferLive {
				if ctx.run.delayedChunks == nil {
					ctx.run.delayedChunks = make(map[uint64]delayedStreamChunk)
				}
				ctx.run.delayedChunks[recorded.EventNumber] = delayedStreamChunk{
					owner: ctx, chunk: frozen.(StreamChunk)}
			} else {
				ctx.emitStreamChunk(frozen.(StreamChunk))
			}
		}
	}
	ctx.run.mu.Unlock()
	if err != nil {
		return ctx.stopRecording(err)
	}
	return nil
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
	value, err := registry.Decode(*event.Payload)
	if chunk, ok := value.(StreamChunk); ok {
		chunk.RunId, chunk.EventNumber = event.RunId, event.EventNumber
		return chunk, err
	}
	return value, err
}
