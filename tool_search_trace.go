package gent

import "time"

// ToolSearchTrace is private diagnostic evidence for local searches used by the next request.
// It is never model content. An unsaved search trace can be lost on interruption; the local
// catalog read can be repeated without a separate search recovery state machine.
type ToolSearchTrace struct {
	Query     string
	QueryType string
	Names     []string
	Duration  time.Duration
	Flow      string
	Error     error
}

// RecordToolSearch queues read-only diagnostics for the next BeforeModelCallEvent. It does not
// add a conversation message or force a separate checkpoint. Normal progress-save rules apply.
func (ctx *ExecutionContext) RecordToolSearch(trace *ToolSearchTrace) {
	ctx.mu.Lock()
	ctx.toolSearches = append(ctx.toolSearches, trace)
	ctx.mu.Unlock()
}

func (ctx *ExecutionContext) takeToolSearches() []*ToolSearchTrace {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	result := ctx.toolSearches
	ctx.toolSearches = nil
	return result
}
