package gent

// RecordInterruption preserves worker interruption without creating a terminal run outcome.
func (ctx *ExecutionContext) RecordInterruption(cause error) error {
	value, err := ctx.Codecs().Encode(cause)
	if err != nil {
		return ctx.stopRecording(err)
	}
	ctx.run.mu.Lock()
	ctx.run.pending = append(ctx.run.pending, &RecordedEvent{Type: "attempt_interrupted",
		ContextId: ctx.ContextId(), Iteration: ctx.Iteration(),
		AttemptId: ctx.run.attempt, Payload: &value})
	ctx.run.mu.Unlock()
	return ctx.FlushProgress()
}
