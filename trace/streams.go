package trace

import (
	"context"
	"time"

	"github.com/rickchristie/gent"
)

// ObserveStreams consumes chunks emitted by an execution context.
func (s *Sequencer) ObserveStreams(execCtx *gent.ExecutionContext) gent.UnsubscribeFunc {
	if execCtx == nil {
		return func() {}
	}
	contextId := execCtx.ContextId()

	s.mu.Lock()
	// SubscribeAll already includes descendant chunks. Observing both an ancestor and child would
	// double-record streams, so prefer the highest observed context in each execution tree.
	if s.hasObservedAncestorLocked(execCtx) {
		s.mu.Unlock()
		return func() {}
	}
	s.removeObservedDescendantsLocked(execCtx)
	if existing := s.streamObservers[contextId]; existing != nil {
		s.mu.Unlock()
		return func() {}
	}
	chunks, unsubscribe := execCtx.SubscribeAll()
	s.streamObservers[contextId] = &streamObserver{execCtx: execCtx, unsubscribe: unsubscribe}
	s.mu.Unlock()

	go s.ConsumeChunks(execCtx.Context(), chunks)
	return func() {
		s.mu.Lock()
		observer := s.streamObservers[contextId]
		delete(s.streamObservers, contextId)
		s.mu.Unlock()
		if observer != nil && observer.unsubscribe != nil {
			observer.unsubscribe()
		}
	}
}

// ConsumeChunks consumes chunks until ctx is canceled or chunks is closed.
func (s *Sequencer) ConsumeChunks(ctx context.Context, chunks <-chan gent.StreamChunk) {
	for {
		select {
		case <-ctx.Done():
			return
		case chunk, ok := <-chunks:
			if !ok {
				return
			}
			s.ConsumeChunk(chunk)
		}
	}
}

// ConsumeChunk records a streamed model chunk.
func (s *Sequencer) ConsumeChunk(chunk gent.StreamChunk) {
	if chunk.Timestamp.IsZero() {
		// EmitChunk normally populates Timestamp. This keeps manual callers safe.
		chunk.Timestamp = time.Now()
	}
	chunk = s.cfg.redactor.RedactChunk(chunk)
	redactedErr := redactError(s.cfg.redactor, chunk.Err)
	payload := map[string]any{
		"streamId":      chunk.StreamId,
		"streamTopicId": chunk.StreamTopicId,
	}
	if s.cfg.IncludeChunkText {
		if chunk.Content != "" {
			payload["content"] = chunk.Content
		}
		if chunk.ReasoningContent != "" {
			payload["reasoningContent"] = chunk.ReasoningContent
		}
	}
	event := &Event{
		Ts:              chunk.Timestamp,
		Type:            EventTypeModelStreamChunk,
		Iteration:       chunk.Iteration,
		Depth:           chunk.Depth,
		Source:          chunk.Source,
		ContextId:       chunk.ContextId,
		ParentContextId: chunk.ParentContextId,
		ModelCallId:     chunk.ModelCallId,
		StreamId:        chunk.StreamId,
		StreamTopicId:   chunk.StreamTopicId,
		Payload:         payload,
		Error:           redactedErr,
	}
	s.record(normalizedEvent{event: event, apply: (*Sequencer).applyModelChunk})
}

func (s *Sequencer) hasObservedAncestorLocked(execCtx *gent.ExecutionContext) bool {
	for parent := execCtx.Parent(); parent != nil; parent = parent.Parent() {
		if s.streamObservers[parent.ContextId()] != nil {
			return true
		}
	}
	return false
}

func (s *Sequencer) removeObservedDescendantsLocked(execCtx *gent.ExecutionContext) {
	for id, observer := range s.streamObservers {
		if observer == nil || observer.execCtx == nil {
			continue
		}
		if !isAncestor(execCtx, observer.execCtx) {
			continue
		}
		delete(s.streamObservers, id)
		if observer.unsubscribe != nil {
			observer.unsubscribe()
		}
	}
}

func isAncestor(ancestor *gent.ExecutionContext, child *gent.ExecutionContext) bool {
	for parent := child.Parent(); parent != nil; parent = parent.Parent() {
		if parent == ancestor {
			return true
		}
	}
	return false
}
