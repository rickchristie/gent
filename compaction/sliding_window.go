package compaction

import "github.com/rickchristie/gent"

// SlidingWindowStrategy keeps the last N iterations in the
// scratchpad, discarding older ones. Pinned iterations
// (those with importance score >= ImportanceScorePinned) are
// always preserved regardless of the window size — they are
// "bonus slots" that do not count toward the window.
//
// Example:
//
//	// Keep last 10 iterations (plus any pinned)
//	strategy := compaction.NewSlidingWindow(10)
type SlidingWindowStrategy struct {
	windowSize int
}

// NewSlidingWindow creates a SlidingWindowStrategy that
// keeps the last windowSize iterations.
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
	execCtx *gent.ExecutionContext,
) error {
	scratchpad := execCtx.Data().GetScratchPad()
	if len(scratchpad) <= s.windowSize {
		return nil
	}

	// Separate pinned from non-pinned
	var pinned []*gent.Iteration
	var unpinned []*gent.Iteration
	for _, iter := range scratchpad {
		if isPinned(iter) {
			pinned = append(pinned, iter)
		} else {
			unpinned = append(unpinned, iter)
		}
	}

	// If unpinned fits in window, nothing to discard
	if len(unpinned) <= s.windowSize {
		return nil
	}

	// Keep last windowSize unpinned iterations
	kept := unpinned[len(unpinned)-s.windowSize:]

	// Build a set for fast lookup
	keptSet := make(map[*gent.Iteration]bool, len(kept))
	for _, k := range kept {
		keptSet[k] = true
	}

	// Rebuild preserving original relative order
	result := make(
		[]*gent.Iteration, 0, len(pinned)+len(kept),
	)
	for _, iter := range scratchpad {
		if isPinned(iter) || keptSet[iter] {
			result = append(result, iter)
		}
	}

	execCtx.Data().SetScratchPad(result)
	return nil
}

// isPinned returns true if the iteration has an importance
// score >= ImportanceScorePinned (10.0).
func isPinned(iter *gent.Iteration) bool {
	score, ok := gent.GetImportanceScore(iter)
	return ok && score >= gent.ImportanceScorePinned
}

// Compile-time check.
var _ gent.CompactionStrategy = (*SlidingWindowStrategy)(nil)
