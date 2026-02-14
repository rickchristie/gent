// Package compaction provides standard CompactionTrigger and
// CompactionStrategy implementations for managing scratchpad
// size in gent agent loops.
//
// # Triggers
//
//   - [StatThresholdTrigger]: fires when stat thresholds
//     are exceeded (counter deltas or gauge absolutes)
//
// # Strategies
//
//   - [SlidingWindowStrategy]: keeps last N iterations
//   - [SummarizationStrategy]: progressive summarization
//     with configurable keep-recent window
package compaction
