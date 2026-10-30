package gent

// SetFinalContentSource links the next successful terminal result to its provisional messages.
// The executor supplies AgentLoopResult.ContentSource. A custom executor may call this directly.
func (ctx *ExecutionContext) SetFinalContentSource(source *ContentSource) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.finalContentSource = cloneContentSource(source)
}

// queueFinalContent runs under run.mu at the terminal save boundary. It never flushes by itself:
// Acceptance without the terminal checkpoint could publish an answer that a retry repeats.
func (ctx *ExecutionContext) queueFinalContent() error {
	ctx.mu.RLock()
	reason, result := ctx.terminationReason, ctx.finalResult
	source := cloneContentSource(ctx.finalContentSource)
	stamp := ctx.endTime
	ctx.mu.RUnlock()
	if reason != TerminationSuccess {
		return nil
	}
	chunk := StreamChunk{Content: result, MessageId: ctx.ContextId() + ":answer",
		ContextId: ctx.ContextId(), ParentContextId: ctx.ParentContextId(),
		Iteration: ctx.Iteration(), Depth: ctx.Depth(), Source: ctx.BuildSourcePath(), Timestamp: stamp}
	if source != nil {
		chunk.ModelCallId, chunk.StreamId = source.ModelCallId, source.StreamId
		chunk.StreamTopicId = source.StreamTopicId
		chunk.SourceMessageIds = source.MessageIds
	}
	value, err := ctx.run.codecs.Encode(chunk)
	if err != nil {
		return err
	}
	return ctx.run.queueEvent(&RecordedEvent{Type: "execution_content",
		ContextId: chunk.ContextId, ParentContextId: chunk.ParentContextId,
		Iteration: chunk.Iteration, AttemptId: ctx.run.attempt, ModelCallId: chunk.ModelCallId,
		Ts: stamp, Payload: &value})
}
