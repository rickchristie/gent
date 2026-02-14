package gent

// CompactionTrigger decides WHEN scratchpad compaction should
// run.
//
// The executor checks the trigger at the start of each loop
// iteration (after the first), before BeforeIterationEvent is
// published. If ShouldCompact returns true, the configured
// CompactionStrategy.Compact is called.
//
// # Available Implementations
//
//   - compaction.NewStatThresholdTrigger: fires when stat
//     thresholds are exceeded (counter deltas or gauge
//     absolutes)
//
// # Implementing Custom Triggers
//
//	type TokenBudgetTrigger struct {
//	    maxTokens int64
//	}
//
//	func (t *TokenBudgetTrigger) ShouldCompact(
//	    execCtx *ExecutionContext,
//	) bool {
//	    return execCtx.Stats().GetTotalTokens() > t.maxTokens
//	}
type CompactionTrigger interface {
	// ShouldCompact returns true if compaction should run now.
	// Called by the executor at the start of each loop
	// iteration (skipped on the first iteration).
	//
	// The ExecutionContext provides access to stats, loop
	// data, and iteration count for making the decision.
	ShouldCompact(execCtx *ExecutionContext) bool

	// NotifyCompacted is called after a successful
	// compaction. Implementations use this to update
	// internal state (e.g., snapshot counter values for
	// delta-based triggers).
	NotifyCompacted(execCtx *ExecutionContext)
}

// CompactionStrategy decides HOW to compact the scratchpad.
//
// The strategy reads the current scratchpad from
// execCtx.Data().GetScratchPad(), computes the compacted
// result, and calls execCtx.Data().SetScratchPad() with the
// new value. SetScratchPad automatically publishes a
// CommonDiffEvent and updates the SGScratchpadLength gauge.
//
// # Error Handling
//
// If Compact returns an error, the executor terminates
// execution with TerminationCompactionFailed. Compaction
// errors are treated as fatal because silent failure leads
// to unbounded context growth that degrades quality without
// any signal to the user.
//
// # Available Implementations
//
//   - compaction.NewSlidingWindow: keeps last N iterations
//   - compaction.NewSummarization: progressive summarization
//     with configurable keep-recent window
//
// # Implementing Custom Strategies
//
//	type MyStrategy struct{}
//
//	func (s *MyStrategy) Compact(
//	    execCtx *ExecutionContext,
//	) error {
//	    scratchpad := execCtx.Data().GetScratchPad()
//	    // ... compute newScratchpad ...
//	    execCtx.Data().SetScratchPad(newScratchpad)
//	    return nil
//	}
type CompactionStrategy interface {
	// Compact performs scratchpad compaction.
	//
	// Reads scratchpad via execCtx.Data().GetScratchPad(),
	// computes compacted result, calls
	// execCtx.Data().SetScratchPad() with the new value.
	//
	// Returning a non-nil error terminates execution.
	Compact(execCtx *ExecutionContext) error
}
