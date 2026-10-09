package compaction

import "github.com/rickchristie/gent"

// SlidingWindowStrategy keeps the last N entries in the
// scratchpad, discarding older ones. Pinned entries
// (those with importance score >= ImportanceScorePinned) are
// always preserved regardless of the window size — they are
// "bonus slots" that do not count toward the window.
//
// Example:
//
//	// Keep last 10 entries (plus any pinned)
//	strategy := compaction.NewSlidingWindow(10)
type SlidingWindowStrategy struct {
	windowSize int
}

// NewSlidingWindow creates a SlidingWindowStrategy that
// keeps the last windowSize entries.
// Panics if windowSize < 1.
func NewSlidingWindow(
	windowSize int,
) *SlidingWindowStrategy {
	if windowSize < 1 {
		panic(
			"gent: SlidingWindow windowSize must be >= 1",
		)
	}
	return &SlidingWindowStrategy{windowSize: windowSize}
}

// Compact implements gent.CompactionStrategy.
func (s *SlidingWindowStrategy) Compact(
	_ *gent.ExecutionContext, input gent.CompactionInput,
) (gent.HistoryUpdate, error) {
	result := gent.HistoryUpdate{ExpectedDraftRevision: input.Revision}
	unpinned := 0
	for _, entry := range input.Entries {
		if !isPinned(entry) {
			unpinned++
		}
	}
	// Pinned entries are bonus slots. Keep relative order, including pins between retained records.
	discard := max(0, unpinned-s.windowSize)
	for _, entry := range input.Entries {
		if !isPinned(entry) && discard > 0 {
			discard--
			continue
		}
		result.Active = append(result.Active, entry.Id)
	}
	return result, nil
}

// isPinned returns true if the entry has an importance
// score >= ImportanceScorePinned (10.0).
func isPinned(entry *gent.HistoryEntry) bool {
	score, ok := gent.GetImportanceScore(entry)
	return ok && score >= gent.ImportanceScorePinned
}

// Compile-time check.
var _ gent.CompactionStrategy = (*SlidingWindowStrategy)(nil)
