package gent

import "fmt"

// The checkpoint starts the replay cursor. Accepted progress sets its end and event limit.
// Keep the allocation counter at its accepted value so new calls cannot reuse saved IDs.
type modelReplay struct {
	next        uint64
	end         uint64
	eventNumber uint64
	iteration   int
}

func (ctx *ExecutionContext) modelCallId(id string) string {
	if id != "" {
		return id
	}
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	if ctx.iteration == ctx.modelReplay.iteration && ctx.modelReplay.next < ctx.modelReplay.end {
		ctx.modelReplay.next++
		return fmt.Sprintf("%s:model:%d", ctx.contextId, ctx.modelReplay.next)
	}
	ctx.nextModelCallSeq++
	return fmt.Sprintf("%s:model:%d", ctx.contextId, ctx.nextModelCallSeq)
}

func (ctx *ExecutionContext) replayModelCall(id string) (Stream, error) {
	if ctx.modelReplay.eventNumber == 0 {
		return nil, nil
	}
	bundle, err := ctx.Export()
	if err != nil {
		return nil, err
	}
	iteration := ctx.Iteration()
	for i := len(bundle.Activity) - 1; i >= 0; i-- {
		event := bundle.Activity[i]
		if event.EventNumber > ctx.modelReplay.eventNumber || event.ContextId != ctx.ContextId() ||
			event.Iteration != iteration || event.ModelCallId != id {
			continue
		}
		if event.Type == "model_call_started" {
			// The last attempt did not finish. Retry with its logical ID.
			return nil, nil
		}
		if event.Type != "model_call_finished" {
			continue
		}
		value, err := ctx.DecodeEvent(event)
		if err != nil {
			return nil, fmt.Errorf("gent: restore model response: %w", err)
		}
		saved := value.(*AfterModelCallEvent)
		if saved.Response == nil || saved.Error != nil {
			return nil, nil
		}
		return ctx.savedModelStream(bundle.Activity[:i], saved)
	}
	return nil, nil
}

func (ctx *ExecutionContext) savedModelStream(
	events []*RecordedEvent, saved *AfterModelCallEvent,
) (Stream, error) {
	var chunks []StreamChunk
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if event.ContextId != saved.ContextId || event.ModelCallId != saved.ModelCallId {
			continue
		}
		if event.Type == "model_call_started" {
			break
		}
		if event.Type != "model_stream_chunk" {
			continue
		}
		value, err := ctx.DecodeEvent(event)
		if err != nil {
			return nil, fmt.Errorf("gent: restore model chunk: %w", err)
		}
		chunks = append(chunks, value.(StreamChunk))
	}
	stream := newStreamBuffer()
	for i := len(chunks) - 1; i >= 0; i-- {
		// These chunks were already accepted. Do not publish them or add usage a second time.
		stream.Send(chunks[i])
	}
	stream.Complete(saved.Response, nil)
	return stream, nil
}
