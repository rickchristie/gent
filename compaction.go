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
//	) (bool, error) {
//	    return execCtx.Stats().GetTotalTokens() > t.maxTokens, nil
//	}
type CompactionTrigger interface {
	// ShouldCompact returns true if compaction should run now.
	// Called by the executor at the start of each loop
	// iteration (skipped on the first iteration).
	//
	// The ExecutionContext provides access to stats, loop
	// data, and iteration count for making the decision.
	ShouldCompact(execCtx *ExecutionContext) (bool, error)

	// NotifyCompacted is called after a successful
	// compaction. Implementations use this to update
	// internal state (e.g., snapshot counter values for
	// delta-based triggers).
	NotifyCompacted(execCtx *ExecutionContext) error
}

// CompactionStrategy decides HOW to compact the scratchpad.
//
// The strategy receives owned records and their IDs, then proposes appended summary entries and
// a resulting selection. The executor accepts the decision, selection, trigger baseline, stats,
// and checkpoint together. Original entries remain available even when absent from the selection.
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
//   - compaction.NewSlidingWindow: keeps last N entries
//   - compaction.NewSummarization: progressive summarization
//     with configurable keep-recent window
//
// # Implementing Custom Strategies
//
//	type MyStrategy struct{}
//
//	func (s *MyStrategy) Compact(
//	    execCtx *ExecutionContext, input CompactionInput,
//	) (HistoryUpdate, error) {
//	    return HistoryUpdate{
//	        ExpectedDraftRevision: input.Revision, Active: input.EntryIds(),
//	    }, nil
//	}
type CompactionStrategy interface {
	// Compact performs scratchpad compaction.
	//
	// Return a proposal without mutating the context's selected records.
	//
	// Returning a non-nil error terminates execution.
	Compact(execCtx *ExecutionContext, input CompactionInput) (HistoryUpdate, error)
}

// CompactionInput is an owned view of exactly the entries selected for this decision.
type CompactionInput struct {
	Revision uint64
	Entries  []*HistoryEntry
	// Required groups must remain selected until the next main model call consumes them.
	// UpdateHistory preserves them even when a custom strategy omits them from its proposal.
	Required []EntryId
}

// EntryIds returns the exact ordered input selection for this revision.
func (input CompactionInput) EntryIds() []EntryId {
	ids := make([]EntryId, len(input.Entries))
	for i, entry := range input.Entries {
		ids[i] = entry.Id
	}
	return ids
}
