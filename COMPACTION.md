# Scratchpad Compaction — Design & Implementation Plan

## Overview

Add scratchpad context management to the gent framework. This provides a standard mechanism for
**when** to compact the scratchpad and **how** to compact it, integrated into the executor loop
so that `AgentLoop` implementations don't need compaction awareness.

The design splits compaction into two interfaces:
- **CompactionTrigger** — decides when compaction should run
- **CompactionStrategy** — decides how to compact the scratchpad

Users can mix and match triggers and strategies. The framework provides two standard strategies
(SlidingWindow, Summarization) and one standard trigger (StatThreshold).

---

## Phase 1: Iteration Type Changes

### 1.1 Add IterationOrigin and Metadata to Iteration

**File:** `agent.go`

Add `IterationOrigin` type and `IterationMetadataKey` type to `Iteration`:

```go
// IterationOrigin indicates how an Iteration was created.
// Used for debugging and observability — helps trace whether an
// iteration is original, was synthesized by compaction, or was
// modified in place.
type IterationOrigin string

const (
    // IterationOriginal is the zero value. The iteration was created
    // normally by the AgentLoop during execution.
    IterationOriginal IterationOrigin = ""

    // IterationCompactedSynthetic means this iteration was created
    // by a CompactionStrategy to summarize multiple iterations.
    // Example: a progressive summarization strategy creates a
    // single synthetic iteration containing the summary text.
    IterationCompactedSynthetic IterationOrigin = "compacted_synthetic"

    // IterationCompactedModified means this is an original iteration
    // that was modified in place by a CompactionStrategy.
    // Example: a strategy that trims tool call outputs from older
    // iterations marks them with this origin.
    IterationCompactedModified IterationOrigin = "compacted_modified"

    // IterationRetrievedHistory means this iteration was pulled back
    // from evicted history (e.g., via a retrieval-augmented strategy).
    IterationRetrievedHistory IterationOrigin = "retrieved_history"
)

// IterationMetadataKey is a typed key for Iteration metadata.
// The framework defines standard keys (IMK* constants). Users
// can define their own keys with a custom prefix.
type IterationMetadataKey string
```

Standard metadata key:

```go
// IMKImportanceScore is a float64 value in [-10, 10] that
// indicates how important this iteration is.
//
// Positive values = more important (prefer to keep/pin).
// Negative values = less important (prefer to discard).
// Zero or absent = neutral (default treatment).
//
// CompactionStrategy implementations should respect this value
// when deciding what to keep or discard. Iterations with high
// importance scores should be treated as "pinned" and preserved
// through compaction.
//
// Usage:
//
//     iter.SetMetadata(gent.IMKImportanceScore, 8.0)
//
//     score, ok := gent.GetImportanceScore(iter)
//
// The standard strategies treat any score >= ImportanceScorePinned
// (10.0) as pinned.
const IMKImportanceScore IterationMetadataKey = "gent:importance_score"
```

Updated `Iteration` struct:

```go
// Iteration represents a single iteration's message content.
//
// Messages must never contain nil elements. All code that
// constructs or modifies an Iteration must ensure every element
// in Messages is a valid, non-nil pointer.
type Iteration struct {
    Messages []*MessageContent

    // Origin indicates how this iteration was created.
    // Zero value (IterationOriginal) means it was created
    // normally by the AgentLoop.
    Origin IterationOrigin

    // Metadata contains optional key-value pairs for this
    // iteration. The framework defines standard keys (IMK*
    // constants); users can add custom keys.
    //
    // This field is nil by default and lazily initialized on
    // first write. Always use SetMetadata/GetMetadata helper
    // methods instead of accessing the map directly to avoid
    // nil map panics.
    //
    //   // WRONG — panics if Metadata is nil:
    //   iter.Metadata[gent.IMKImportanceScore] = 5.0
    //
    //   // CORRECT — safe, initializes map if needed:
    //   iter.SetMetadata(gent.IMKImportanceScore, 5.0)
    //
    //   // CORRECT — safe, returns zero value + false if nil/absent:
    //   val, ok := iter.GetMetadata(gent.IMKImportanceScore)
    Metadata map[IterationMetadataKey]any
}
```

Helper methods on `*Iteration`:

```go
// SetMetadata sets a metadata value, initializing the map if nil.
func (i *Iteration) SetMetadata(key IterationMetadataKey, value any) {
    if i.Metadata == nil {
        i.Metadata = make(map[IterationMetadataKey]any)
    }
    i.Metadata[key] = value
}

// GetMetadata returns a metadata value and whether it was present.
// Returns (nil, false) if Metadata is nil or the key is absent.
func (i *Iteration) GetMetadata(
    key IterationMetadataKey,
) (any, bool) {
    if i.Metadata == nil {
        return nil, false
    }
    val, ok := i.Metadata[key]
    return val, ok
}

// GetImportanceScore is a convenience function that returns the
// importance score for an iteration.
// Returns (0, false) if the key is absent or not a float64.
func GetImportanceScore(iter *Iteration) (float64, bool) {
    val, ok := iter.GetMetadata(IMKImportanceScore)
    if !ok {
        return 0, false
    }
    score, ok := val.(float64)
    return score, ok
}
```

### 1.1 Tests

**File:** `agent_test.go` (or new `iteration_test.go`)

Table-driven tests for:
- `SetMetadata` initializes nil map and sets value
- `SetMetadata` on existing map updates value
- `GetMetadata` returns `(nil, false)` on nil map
- `GetMetadata` returns `(nil, false)` on missing key
- `GetMetadata` returns `(value, true)` on present key
- `GetImportanceScore` returns `(0, false)` on nil metadata
- `GetImportanceScore` returns `(0, false)` on wrong type
- `GetImportanceScore` returns `(score, true)` on valid float64

---

## Phase 2: Compaction Interfaces

### 2.1 CompactionTrigger and CompactionStrategy

**File:** `compaction.go` (root package, alongside `agent.go`)

```go
// CompactionTrigger decides WHEN scratchpad compaction should run.
//
// The executor checks the trigger at the start of each loop
// iteration (after the first), before BeforeIterationEvent is
// published. If ShouldCompact returns true, the configured
// CompactionStrategy.Compact is called.
//
// # Available Implementations
//
//   - compaction.NewStatThresholdTrigger: fires when stat
//     thresholds are exceeded (counter deltas or gauge absolutes)
//
// # Implementing Custom Triggers
//
//     type TokenBudgetTrigger struct {
//         maxTokens int64
//     }
//
//     func (t *TokenBudgetTrigger) ShouldCompact(
//         execCtx *ExecutionContext,
//     ) bool {
//         return execCtx.Stats().GetTotalTokens() > t.maxTokens
//     }
type CompactionTrigger interface {
    // ShouldCompact returns true if compaction should run now.
    // Called by the executor at the start of each loop iteration
    // (skipped on the first iteration).
    //
    // The ExecutionContext provides access to stats, loop data,
    // and iteration count for making the decision.
    ShouldCompact(execCtx *ExecutionContext) bool

    // NotifyCompacted is called after a successful compaction.
    // Implementations use this to update internal state (e.g.,
    // snapshot counter values for delta-based triggers).
    NotifyCompacted(execCtx *ExecutionContext)
}

// CompactionStrategy decides HOW to compact the scratchpad.
//
// The strategy reads the current scratchpad from
// execCtx.Data().GetScratchPad(), computes the compacted result,
// and calls execCtx.Data().SetScratchPad() with the new value.
// SetScratchPad automatically publishes a CommonDiffEvent and
// updates the SGScratchpadLength gauge.
//
// # Error Handling
//
// If Compact returns an error, the executor terminates execution
// with TerminationCompactionFailed. Compaction errors are treated
// as fatal because silent failure leads to unbounded context
// growth that degrades quality without any signal to the user.
//
// # Available Implementations
//
//   - compaction.NewSlidingWindow: keeps last N iterations
//   - compaction.NewSummarization: progressive summarization
//     with configurable keep-recent window
//
// # Implementing Custom Strategies
//
//     type MyStrategy struct{}
//
//     func (s *MyStrategy) Compact(
//         execCtx *ExecutionContext,
//     ) error {
//         scratchpad := execCtx.Data().GetScratchPad()
//         // ... compute newScratchpad ...
//         execCtx.Data().SetScratchPad(newScratchpad)
//         return nil
//     }
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
```

### 2.2 New TerminationReason

**File:** `events.go`

```go
// TerminationCompactionFailed means a CompactionStrategy returned
// an error. Inspect ExecutionResult.Error for details.
TerminationCompactionFailed TerminationReason = "compaction_failed"
```

### 2.3 New Event

**File:** `events.go`

```go
// CompactionEvent is published after a successful compaction.
// Stats updated: SCCompactions counter is incremented.
type CompactionEvent struct {
    BaseEvent

    // ScratchpadLengthBefore is the number of iterations before
    // compaction.
    ScratchpadLengthBefore int

    // ScratchpadLengthAfter is the number of iterations after
    // compaction.
    ScratchpadLengthAfter int

    // Duration is how long the compaction took.
    Duration time.Duration
}
```

**File:** `event_names.go`

```go
// Compaction
EventNameCompaction = "gent:compaction"
```

### 2.4 New Stat Key

**File:** `stats_keys.go`

```go
// Compaction tracking key (Counter).
//
// Auto-updated when CompactionEvent is published. Tracks the
// total number of successful compactions performed.
//
// Propagates to parent: the parent's SCCompactions reflects
// total compactions across the entire agent tree.
const SCCompactions StatKey = "gent:compactions"
```

### 2.5 updateStatsForEvent Addition

**File:** `context.go` (in `updateStatsForEvent`)

Add a new case in the event type switch:

```go
case *CompactionEvent:
    ctx.stats.incrCounterDirect(SCCompactions, 1)
```

### 2.6 populateBaseEvent Addition

**File:** `context.go` (in `populateBaseEvent`)

Add a new case for `*CompactionEvent`:

```go
case *CompactionEvent:
    e.Timestamp = time.Now()
    e.Iteration = ctx.iteration
    e.Depth = ctx.depth
```

### 2.7 PublishCompaction Convenience Method

**File:** `context.go`

```go
// PublishCompaction publishes a CompactionEvent.
// Stats updated: SCCompactions counter is incremented.
func (ctx *ExecutionContext) PublishCompaction(
    lengthBefore int,
    lengthAfter int,
    duration time.Duration,
) *CompactionEvent {
    event := &CompactionEvent{
        BaseEvent: BaseEvent{
            EventName: EventNameCompaction,
        },
        ScratchpadLengthBefore: lengthBefore,
        ScratchpadLengthAfter:  lengthAfter,
        Duration:               duration,
    }
    ctx.publish(event)
    return event
}
```

### 2.8 SetCompaction on ExecutionContext

**File:** `context.go`

Add fields to `ExecutionContext`:

```go
type ExecutionContext struct {
    // ... existing fields ...

    // Compaction configuration (optional)
    compactionTrigger  CompactionTrigger
    compactionStrategy CompactionStrategy
}
```

Add configuration method:

```go
// SetCompaction configures scratchpad compaction for this
// execution. The trigger decides when to compact, the strategy
// decides how.
//
// Must be called before execution starts. Both trigger and
// strategy must be non-nil.
//
// When compaction is configured, the executor checks the trigger
// at the start of each loop iteration (after the first). If the
// trigger fires, the strategy's Compact method is called.
//
// If Compact returns an error, execution terminates with
// TerminationCompactionFailed.
//
// Example:
//
//     trigger := compaction.NewStatThresholdTrigger().
//         OnCounter(gent.SCIterations, 10).
//         OnGauge(gent.SGScratchpadLength, 20)
//
//     strategy := compaction.NewSlidingWindow(10)
//
//     execCtx.SetCompaction(trigger, strategy)
func (ctx *ExecutionContext) SetCompaction(
    trigger CompactionTrigger,
    strategy CompactionStrategy,
) {
    ctx.mu.Lock()
    defer ctx.mu.Unlock()
    ctx.compactionTrigger = trigger
    ctx.compactionStrategy = strategy
}

// CompactionTrigger returns the configured trigger, or nil.
func (ctx *ExecutionContext) CompactionTrigger() CompactionTrigger {
    ctx.mu.RLock()
    defer ctx.mu.RUnlock()
    return ctx.compactionTrigger
}

// CompactionStrategy returns the configured strategy, or nil.
func (ctx *ExecutionContext) CompactionStrategy() CompactionStrategy {
    ctx.mu.RLock()
    defer ctx.mu.RUnlock()
    return ctx.compactionStrategy
}
```

### 2.9 Executor Integration

**File:** `executor/executor.go`

The executor loop changes from:

```go
for {
    // Check context cancellation
    goCtx := execCtx.Context()
    if goCtx.Err() != nil { ... }

    // Start iteration
    execCtx.IncrementIteration()
    iterStart := time.Now()
    execCtx.PublishBeforeIteration()

    // Execute
    loopResult, loopErr := e.loop.Next(execCtx)
    ...
}
```

To:

```go
for {
    // Check context cancellation
    goCtx := execCtx.Context()
    if goCtx.Err() != nil { ... }

    // Compaction check (skip first iteration — nothing to compact)
    if execCtx.Iteration() > 0 {
        if err := e.compactIfNeeded(execCtx); err != nil {
            execCtx.SetTermination(
                gent.TerminationCompactionFailed,
                nil,
                fmt.Errorf(
                    "compaction (iteration %d): %w",
                    execCtx.Iteration(), err,
                ),
            )
            return
        }
    }

    // Start iteration
    execCtx.IncrementIteration()
    iterStart := time.Now()
    execCtx.PublishBeforeIteration()

    // Execute
    loopResult, loopErr := e.loop.Next(execCtx)
    ...
}
```

New private method:

```go
// compactIfNeeded checks the compaction trigger and runs the
// strategy if triggered.
func (e *Executor[Data]) compactIfNeeded(
    execCtx *gent.ExecutionContext,
) error {
    trigger := execCtx.CompactionTrigger()
    strategy := execCtx.CompactionStrategy()
    if trigger == nil || strategy == nil {
        return nil
    }

    if !trigger.ShouldCompact(execCtx) {
        return nil
    }

    lengthBefore := len(execCtx.Data().GetScratchPad())
    compactStart := time.Now()

    if err := strategy.Compact(execCtx); err != nil {
        return err
    }

    lengthAfter := len(execCtx.Data().GetScratchPad())
    duration := time.Since(compactStart)
    execCtx.PublishCompaction(lengthBefore, lengthAfter, duration)
    trigger.NotifyCompacted(execCtx)

    return nil
}
```

**Placement rationale:**
- **AFTER** cancellation check: if context is cancelled, skip compaction
- **BEFORE** `IncrementIteration()` and `PublishBeforeIteration()`: per-iteration gauges
  from the previous iteration (e.g., `SGTotalTokensLastIteration`) are still available
  for the trigger to inspect. `BeforeIterationEvent` resets them.
- **Skip first iteration** via `Iteration() > 0`: nothing to compact before the agent
  has run at all.

### 2.10 Tests for Phase 2

**File:** `compaction_test.go` (root package)

- `SetMetadata`/`GetMetadata` on Iteration (see Phase 1 tests)
- `GetImportanceScore` helper function

**File:** `executor/executor_compaction_test.go`

Table-driven integration tests using existing mock infrastructure (`internal/tt`):

```
input struct:
    trigger    CompactionTrigger  // mock or real
    strategy   CompactionStrategy // mock or real
    limits     []gent.Limit
    mockModel  *tt.MockModel
    mockFormat *tt.MockFormat
    ...

expected struct:
    terminationReason gent.TerminationReason
    error             string  // empty if no error
    compactions       int64   // SCCompactions counter value
    scratchpadLength  int     // final scratchpad length
    events            []gent.Event
```

Test cases:
1. **No compaction configured** — normal execution, SCCompactions = 0
2. **Trigger never fires** — normal execution, SCCompactions = 0
3. **Trigger fires, strategy succeeds** — CompactionEvent published,
   SCCompactions = 1, scratchpad reduced
4. **Trigger fires multiple times** — SCCompactions = N, multiple
   CompactionEvents
5. **Strategy returns error** — TerminationCompactionFailed, execution stops
6. **Strategy returns error on first trigger** — terminates early
7. **Compaction happens before BeforeIteration** — verify per-iteration
   gauges are still available when trigger checks them (not yet reset)
8. **NotifyCompacted called after success** — verify trigger state updated

---

## Phase 3: StatThresholdTrigger

### 3.1 Implementation

**File:** `compaction/stat_threshold_trigger.go`

```go
package compaction

import "github.com/rickchristie/gent"

// StatThresholdTrigger fires when any configured stat threshold
// is exceeded. Supports both counter and gauge thresholds with
// different semantics.
//
// # Counter Thresholds (Delta-Based)
//
// Counters only go up, so the trigger tracks the counter value
// at the time of last compaction. It fires when:
//
//     currentValue - lastCompactionValue >= threshold
//
// When ANY threshold triggers compaction, ALL counter snapshots
// are updated to current values. This prevents re-triggering.
//
// # Gauge Thresholds (Absolute)
//
// Gauges can go up and down, so the trigger checks the current
// value directly against the threshold:
//
//     currentValue >= threshold
//
// No snapshot tracking is needed — gauges naturally reflect
// current state (e.g., SGScratchpadLength decreases after
// compaction).
//
// # Match Modes
//
// Both counter and gauge thresholds support exact key and prefix
// matching, consistent with the Limit system.
//
// # Example
//
//     trigger := compaction.NewStatThresholdTrigger().
//         OnCounter(gent.SCIterations, 10).     // every 10 iterations
//         OnCounterPrefix(gent.SCInputTokensFor, 50000). // per-model
//         OnGauge(gent.SGScratchpadLength, 20). // scratchpad > 20
//         OnGauge(gent.SGTotalTokensLastIteration, 100000)
type StatThresholdTrigger struct {
    counterThresholds []counterThreshold
    gaugeThresholds   []gaugeThreshold
}

type counterThreshold struct {
    key       gent.StatKey
    matchMode gent.LimitType // LimitExactKey or LimitKeyPrefix
    delta     int64
    lastValue map[string]int64 // snapshot at last compaction
}

type gaugeThreshold struct {
    key       gent.StatKey
    matchMode gent.LimitType
    value     float64
}

// NewStatThresholdTrigger creates a new StatThresholdTrigger.
func NewStatThresholdTrigger() *StatThresholdTrigger {
    return &StatThresholdTrigger{}
}

// OnCounter adds an exact-key counter threshold.
// Fires when (current - lastCompaction) >= delta.
func (t *StatThresholdTrigger) OnCounter(
    key gent.StatKey,
    delta int64,
) *StatThresholdTrigger {
    t.counterThresholds = append(t.counterThresholds,
        counterThreshold{
            key:       key,
            matchMode: gent.LimitExactKey,
            delta:     delta,
            lastValue: make(map[string]int64),
        },
    )
    return t
}

// OnCounterPrefix adds a prefix counter threshold.
// Fires when any key matching the prefix has
// (current - lastCompaction) >= delta.
func (t *StatThresholdTrigger) OnCounterPrefix(
    prefix gent.StatKey,
    delta int64,
) *StatThresholdTrigger {
    t.counterThresholds = append(t.counterThresholds,
        counterThreshold{
            key:       prefix,
            matchMode: gent.LimitKeyPrefix,
            delta:     delta,
            lastValue: make(map[string]int64),
        },
    )
    return t
}

// OnGauge adds an exact-key gauge threshold.
// Fires when currentValue >= value.
func (t *StatThresholdTrigger) OnGauge(
    key gent.StatKey,
    value float64,
) *StatThresholdTrigger {
    t.gaugeThresholds = append(t.gaugeThresholds,
        gaugeThreshold{
            key:       key,
            matchMode: gent.LimitExactKey,
            value:     value,
        },
    )
    return t
}

// OnGaugePrefix adds a prefix gauge threshold.
// Fires when any key matching the prefix has
// currentValue >= value.
func (t *StatThresholdTrigger) OnGaugePrefix(
    prefix gent.StatKey,
    value float64,
) *StatThresholdTrigger {
    t.gaugeThresholds = append(t.gaugeThresholds,
        gaugeThreshold{
            key:       prefix,
            matchMode: gent.LimitKeyPrefix,
            value:     value,
        },
    )
    return t
}
```

`ShouldCompact` implementation:

```go
func (t *StatThresholdTrigger) ShouldCompact(
    execCtx *gent.ExecutionContext,
) bool {
    stats := execCtx.Stats()

    // Check counter thresholds (delta-based)
    for i := range t.counterThresholds {
        ct := &t.counterThresholds[i]
        if t.counterExceeded(stats, ct) {
            return true
        }
    }

    // Check gauge thresholds (absolute)
    for i := range t.gaugeThresholds {
        gt := &t.gaugeThresholds[i]
        if t.gaugeExceeded(stats, gt) {
            return true
        }
    }

    return false
}

func (t *StatThresholdTrigger) counterExceeded(
    stats *gent.ExecutionStats,
    ct *counterThreshold,
) bool {
    switch ct.matchMode {
    case gent.LimitExactKey:
        current := stats.GetCounter(ct.key)
        last := ct.lastValue[string(ct.key)]
        return current-last >= ct.delta
    case gent.LimitKeyPrefix:
        prefix := string(ct.key)
        for key, current := range stats.Counters() {
            if len(key) >= len(prefix) &&
                key[:len(prefix)] == prefix {
                last := ct.lastValue[key]
                if current-last >= ct.delta {
                    return true
                }
            }
        }
    }
    return false
}

func (t *StatThresholdTrigger) gaugeExceeded(
    stats *gent.ExecutionStats,
    gt *gaugeThreshold,
) bool {
    switch gt.matchMode {
    case gent.LimitExactKey:
        return stats.GetGauge(gt.key) >= gt.value
    case gent.LimitKeyPrefix:
        prefix := string(gt.key)
        for key, val := range stats.Gauges() {
            if len(key) >= len(prefix) &&
                key[:len(prefix)] == prefix {
                if val >= gt.value {
                    return true
                }
            }
        }
    }
    return false
}
```

`NotifyCompacted` implementation — snapshot ALL counter values:

```go
func (t *StatThresholdTrigger) NotifyCompacted(
    execCtx *gent.ExecutionContext,
) {
    stats := execCtx.Stats()
    counters := stats.Counters()

    for i := range t.counterThresholds {
        ct := &t.counterThresholds[i]
        switch ct.matchMode {
        case gent.LimitExactKey:
            ct.lastValue[string(ct.key)] =
                counters[string(ct.key)]
        case gent.LimitKeyPrefix:
            prefix := string(ct.key)
            for key, val := range counters {
                if len(key) >= len(prefix) &&
                    key[:len(prefix)] == prefix {
                    ct.lastValue[key] = val
                }
            }
        }
    }
}
```

### 3.2 Tests

**File:** `compaction/stat_threshold_trigger_test.go`

Table-driven tests with explicit input/expected structs:

```
input struct:
    thresholds   []thresholdConfig
    statsSetup   func(stats *gent.ExecutionStats)
    priorCompact bool // whether NotifyCompacted was called before

expected struct:
    shouldCompact bool
```

Test cases for counter thresholds:
1. **Counter below delta** — `ShouldCompact` returns false
2. **Counter equals delta** — returns true (>= comparison)
3. **Counter exceeds delta** — returns true
4. **Counter delta after compaction** — snapshot updated,
   needs another full delta to re-trigger
5. **Multiple counter thresholds, one fires** — returns true
6. **Multiple counter thresholds, none fire** — returns false

Test cases for gauge thresholds:
7. **Gauge below value** — returns false
8. **Gauge equals value** — returns true
9. **Gauge exceeds value** — returns true
10. **Gauge naturally decreased** — returns false after decrease

Test cases for prefix matching:
11. **Counter prefix matches one key** — returns true
12. **Counter prefix matches no keys** — returns false
13. **Gauge prefix matches one key** — returns true

Test cases for NotifyCompacted:
14. **Exact counter snapshot updated** — verify lastValue reflects
    current counter after compaction
15. **Prefix counter snapshot updated** — verify all matching keys
    snapshotted

Combined scenario:
16. **Iteration + token trigger walkthrough** — the example from
    our discussion:
    - Configure: SCIterations delta=10, SCInputTokens delta=100000
    - Iter 5, 120K tokens → token fires, compact
    - Iter 10, 200K tokens → neither fires (delta 5 iter, 80K tokens)
    - Iter 15, 250K tokens → iteration fires (delta 10), compact

---

## Phase 4: SlidingWindow Strategy

### 4.1 Implementation

**File:** `compaction/sliding_window.go`

```go
package compaction

import "github.com/rickchristie/gent"

// SlidingWindowStrategy keeps the last N iterations in the
// scratchpad, discarding older ones. Pinned iterations (those
// with importance score >= ImportanceScorePinned) are always preserved
// regardless of the window size — they are "bonus slots" that
// do not count toward the window.
//
// Example:
//
//     // Keep last 10 iterations (plus any pinned)
//     strategy := compaction.NewSlidingWindow(10)
type SlidingWindowStrategy struct {
    windowSize int
}

// NewSlidingWindow creates a SlidingWindowStrategy that keeps the
// last windowSize iterations. Panics if windowSize < 1.
func NewSlidingWindow(windowSize int) *SlidingWindowStrategy {
    if windowSize < 1 {
        panic("gent: SlidingWindow windowSize must be >= 1")
    }
    return &SlidingWindowStrategy{windowSize: windowSize}
}

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

    // Rebuild: pinned + kept (preserves relative order)
    result := make([]*gent.Iteration, 0,
        len(pinned)+len(kept))
    for _, iter := range scratchpad {
        if isPinned(iter) {
            result = append(result, iter)
        } else {
            // Only include if it's in the kept set
            for _, k := range kept {
                if iter == k {
                    result = append(result, iter)
                    break
                }
            }
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
```

**Implementation note:** The rebuild loop preserves the original relative order of iterations
(pinned iterations stay in their original positions relative to other iterations). This is
important because message ordering matters for LLM context.

### 4.2 Tests

**File:** `compaction/sliding_window_test.go`

Table-driven with:

```
input struct:
    windowSize int
    scratchpad []*gent.Iteration // with various importance scores

expected struct:
    scratchpadLength int
    // Full iteration identity checks (pointer equality)
    keptIterations   []*gent.Iteration
}
```

Test cases:
1. **Scratchpad within window** — no change
2. **Scratchpad exceeds window** — oldest non-pinned dropped
3. **All iterations pinned** — no change (all are bonus slots)
4. **Mixed pinned/unpinned** — pinned preserved as bonus,
   last N unpinned kept
5. **Pinned iterations preserve relative order** — a pinned
   iteration at index 2 stays between the right neighbors
6. **Window size 1** — only most recent unpinned kept
7. **Importance score below threshold** — NOT pinned (only >= 10.0 is pinned)
8. **Negative importance score** — NOT pinned
9. **No metadata (nil)** — NOT pinned
10. **Panics on windowSize < 1**

---

## Phase 5: SummarizationStrategy

### 5.1 Implementation

**File:** `compaction/summarization.go`

```go
package compaction

import (
    "context"
    "fmt"
    "strings"

    "github.com/rickchristie/gent"
    "github.com/tmc/langchaingo/llms"
)

// SummarizationStrategy compacts the scratchpad by summarizing
// older iterations into a single synthetic iteration. Recent
// iterations are preserved untouched.
//
// This implements both "progressive summarization" and "summary
// buffer hybrid" patterns:
//   - KeepRecent = 0: pure progressive (summarize everything)
//   - KeepRecent > 0: hybrid (keep last N, summarize the rest)
//
// Pinned iterations (importance score >= 10.0) are always preserved
// untouched, regardless of their position.
//
// # Multi-Modal Handling
//
// Non-text content parts (images, audio, etc.) in non-pinned
// iterations are dropped during summarization. Only text content
// is extracted and passed to the summarization model. If
// multi-modal content is important, pin the iteration.
//
// # Model Usage
//
// The strategy calls the injected Model to generate summaries.
// Token usage from these calls is tracked in the
// ExecutionContext's stats (via the model's standard event
// publishing). The summarization prompt can be customized.
//
// # Example
//
//     strategy := compaction.NewSummarization(model).
//         WithKeepRecent(5)
type SummarizationStrategy struct {
    model      gent.Model
    keepRecent int
    prompt     string
}

// NewSummarization creates a SummarizationStrategy with the
// given model.
func NewSummarization(
    model gent.Model,
) *SummarizationStrategy {
    return &SummarizationStrategy{
        model:      model,
        keepRecent: 0,
        prompt:     DefaultSummarizationPrompt,
    }
}

// WithKeepRecent sets the number of recent iterations to
// preserve without summarization. Default is 0 (pure
// progressive summarization).
func (s *SummarizationStrategy) WithKeepRecent(
    n int,
) *SummarizationStrategy {
    s.keepRecent = n
    return s
}

// WithPrompt sets a custom summarization prompt.
// The prompt receives the existing summary (if any) and the
// new messages to summarize.
func (s *SummarizationStrategy) WithPrompt(
    prompt string,
) *SummarizationStrategy {
    s.prompt = prompt
    return s
}

// DefaultSummarizationPrompt is the default prompt used for
// summarization.
const DefaultSummarizationPrompt = `You are a conversation
summarizer. Your job is to produce a concise summary that
preserves all important information for an AI agent to continue
its work.

%s

## New Messages to Incorporate

%s

## Instructions

Produce an updated summary that:
1. Preserves key decisions, findings, and action items
2. Retains specific values (numbers, names, paths, etc.)
3. Notes any errors encountered and their resolutions
4. Keeps tool call results that may be referenced later
5. Is concise but does not lose critical context

Write ONLY the summary, no preamble.`
```

`Compact` implementation:

```go
func (s *SummarizationStrategy) Compact(
    execCtx *gent.ExecutionContext,
) error {
    scratchpad := execCtx.Data().GetScratchPad()

    // Separate: pinned, existing summary, summarizable, recent
    var (
        pinned          []*gent.Iteration
        existingSummary *gent.Iteration
        toSummarize     []*gent.Iteration
        toKeep          []*gent.Iteration
    )

    // First pass: extract pinned and existing summary
    var nonPinned []*gent.Iteration
    for _, iter := range scratchpad {
        if isPinned(iter) {
            pinned = append(pinned, iter)
            continue
        }
        if iter.Origin == gent.IterationCompactedSynthetic {
            existingSummary = iter
            continue
        }
        nonPinned = append(nonPinned, iter)
    }

    // Split non-pinned into toSummarize and toKeep
    if s.keepRecent > 0 && len(nonPinned) > s.keepRecent {
        splitIdx := len(nonPinned) - s.keepRecent
        toSummarize = nonPinned[:splitIdx]
        toKeep = nonPinned[splitIdx:]
    } else if s.keepRecent == 0 && len(nonPinned) > 0 {
        toSummarize = nonPinned
    } else {
        // Nothing to summarize
        return nil
    }

    if len(toSummarize) == 0 {
        return nil
    }

    // Build summarization input
    existingText := ""
    if existingSummary != nil {
        existingText = "## Existing Summary\n\n" +
            extractText(existingSummary)
    } else {
        existingText = "## Existing Summary\n\nNone (first " +
            "compaction)."
    }

    newMessages := extractTextFromIterations(toSummarize)
    fullPrompt := fmt.Sprintf(
        s.prompt, existingText, newMessages,
    )

    // Call model for summarization
    messages := []llms.MessageContent{
        {
            Role: llms.ChatMessageTypeHuman,
            Parts: []llms.ContentPart{
                llms.TextContent{Text: fullPrompt},
            },
        },
    }
    streamId := fmt.Sprintf(
        "compaction-summarization-%d",
        execCtx.Iteration(),
    )
    response, err := s.model.GenerateContent(
        execCtx,
        streamId,
        "compaction",
        messages,
    )
    if err != nil {
        return fmt.Errorf("summarization model call: %w", err)
    }

    summaryText := response.Choices[0].Content

    // Create synthetic summary iteration
    synthetic := &gent.Iteration{
        Messages: []*gent.MessageContent{
            {
                Role: llms.ChatMessageGeneric,
                Parts: []gent.ContentPart{
                    llms.TextContent{
                        Text: summaryText,
                    },
                },
            },
        },
        Origin: gent.IterationCompactedSynthetic,
    }

    // Rebuild scratchpad: pinned + synthetic + toKeep
    // preserving relative order of pinned iterations
    result := make([]*gent.Iteration, 0,
        1+len(pinned)+len(toKeep))
    result = append(result, synthetic)
    result = append(result, pinned...)
    result = append(result, toKeep...)

    execCtx.Data().SetScratchPad(result)
    return nil
}
```

Text extraction helpers:

```go
// extractText extracts all text content from an iteration,
// dropping non-text content parts.
func extractText(iter *gent.Iteration) string {
    var parts []string
    for _, msg := range iter.Messages {
        for _, part := range msg.Parts {
            if tc, ok := part.(llms.TextContent); ok {
                parts = append(parts, tc.Text)
            }
        }
    }
    return strings.Join(parts, "\n")
}

// extractTextFromIterations extracts text from multiple
// iterations, formatting them with iteration markers.
func extractTextFromIterations(
    iterations []*gent.Iteration,
) string {
    var sb strings.Builder
    for i, iter := range iterations {
        fmt.Fprintf(&sb, "### Message %d\n\n", i+1)
        sb.WriteString(extractText(iter))
        sb.WriteString("\n\n")
    }
    return sb.String()
}
```

### 5.2 Tests

**File:** `compaction/summarization_test.go`

These tests use a mock `gent.Model` (from `internal/tt`).

```
input struct:
    keepRecent       int
    customPrompt     string    // empty = default
    scratchpad       []*gent.Iteration
    modelResponse    string    // what the mock model returns

expected struct:
    error             string   // empty if no error
    scratchpadLength  int
    syntheticCount    int      // iterations with CompactedSynthetic origin
    keptOriginals     int      // non-synthetic, non-pinned iterations
    pinnedCount       int      // pinned iterations
    summaryContains   string   // substring of synthetic iteration text
}
```

Test cases:
1. **Nothing to summarize** (empty scratchpad) — no change
2. **All iterations within keepRecent** — no change
3. **Pure progressive (keepRecent=0)** — all non-pinned summarized
   into 1 synthetic, result = [synthetic] + [pinned]
4. **Hybrid (keepRecent=3)** — oldest non-pinned summarized,
   last 3 kept, result = [synthetic] + [pinned] + [3 recent]
5. **Existing synthetic summary** — existing summary included in
   prompt as "Existing Summary", replaced by new synthetic
6. **Pinned iterations preserved** — pinned iterations moved to
   result, not included in summarization input
7. **Multi-modal content dropped** — iterations with image parts
   only have text extracted
8. **Text-only iterations** — all text extracted correctly
9. **Model error** — returns error (which triggers
   TerminationCompactionFailed in executor)
10. **Custom prompt** — verify custom prompt is used
11. **Token tracking** — verify SCInputTokens/SCOutputTokens
    incremented from summarization model call

---

## Phase 6: Mock Additions

### 6.1 Mock CompactionTrigger

**File:** `internal/tt/mocks.go`

```go
// MockCompactionTrigger is a configurable mock that implements
// gent.CompactionTrigger.
type MockCompactionTrigger struct {
    shouldCompact []bool // sequence of return values
    callIdx       int
    notifiedCount int
}

func NewMockCompactionTrigger() *MockCompactionTrigger {
    return &MockCompactionTrigger{}
}

// WithShouldCompact sets the sequence of ShouldCompact return
// values. Panics if exhausted.
func (t *MockCompactionTrigger) WithShouldCompact(
    values ...bool,
) *MockCompactionTrigger {
    t.shouldCompact = values
    return t
}

func (t *MockCompactionTrigger) ShouldCompact(
    _ *gent.ExecutionContext,
) bool {
    if t.callIdx >= len(t.shouldCompact) {
        panic("MockCompactionTrigger: exhausted ShouldCompact")
    }
    result := t.shouldCompact[t.callIdx]
    t.callIdx++
    return result
}

func (t *MockCompactionTrigger) NotifyCompacted(
    _ *gent.ExecutionContext,
) {
    t.notifiedCount++
}

// NotifiedCount returns how many times NotifyCompacted was
// called.
func (t *MockCompactionTrigger) NotifiedCount() int {
    return t.notifiedCount
}
```

### 6.2 Mock CompactionStrategy

```go
// MockCompactionStrategy is a configurable mock that implements
// gent.CompactionStrategy.
type MockCompactionStrategy struct {
    compactFunc func(execCtx *gent.ExecutionContext) error
    callCount   int
}

func NewMockCompactionStrategy() *MockCompactionStrategy {
    return &MockCompactionStrategy{}
}

// WithCompactFunc sets the Compact implementation.
func (s *MockCompactionStrategy) WithCompactFunc(
    fn func(execCtx *gent.ExecutionContext) error,
) *MockCompactionStrategy {
    s.compactFunc = fn
    return s
}

// WithError creates a strategy that always returns the given
// error.
func (s *MockCompactionStrategy) WithError(
    err error,
) *MockCompactionStrategy {
    s.compactFunc = func(_ *gent.ExecutionContext) error {
        return err
    }
    return s
}

func (s *MockCompactionStrategy) Compact(
    execCtx *gent.ExecutionContext,
) error {
    s.callCount++
    if s.compactFunc != nil {
        return s.compactFunc(execCtx)
    }
    return nil
}

// CallCount returns how many times Compact was called.
func (s *MockCompactionStrategy) CallCount() int {
    return s.callCount
}
```

---

## File Summary

### New Files

| File | Package | Contents |
|------|---------|----------|
| `compaction.go` | `gent` | `CompactionTrigger`, `CompactionStrategy` interfaces, `IterationOrigin`, `IterationMetadataKey`, `IMKImportanceScore`, `GetImportanceScore` |
| `compaction/doc.go` | `compaction` | Package documentation |
| `compaction/stat_threshold_trigger.go` | `compaction` | `StatThresholdTrigger` |
| `compaction/stat_threshold_trigger_test.go` | `compaction` | Tests |
| `compaction/sliding_window.go` | `compaction` | `SlidingWindowStrategy`, `isPinned` helper |
| `compaction/sliding_window_test.go` | `compaction` | Tests |
| `compaction/summarization.go` | `compaction` | `SummarizationStrategy`, text extraction helpers |
| `compaction/summarization_test.go` | `compaction` | Tests |

### Modified Files

| File | Changes |
|------|---------|
| `agent.go` | Add `Origin`, `Metadata` to `Iteration`; add `SetMetadata`, `GetMetadata` methods |
| `events.go` | Add `TerminationCompactionFailed`, `CompactionEvent` |
| `event_names.go` | Add `EventNameCompaction` |
| `stats_keys.go` | Add `SCCompactions` |
| `context.go` | Add `compactionTrigger`/`compactionStrategy` fields, `SetCompaction`, `CompactionTrigger`, `CompactionStrategy` methods, `PublishCompaction`, `populateBaseEvent` case, `updateStatsForEvent` case |
| `executor/executor.go` | Add `compactIfNeeded` method, call it in loop |
| `internal/tt/mocks.go` | Add `MockCompactionTrigger`, `MockCompactionStrategy` |

### New Test Files

| File | Contents |
|------|---------|
| `iteration_test.go` | `SetMetadata`/`GetMetadata`/`GetImportanceScore` tests |
| `executor/executor_compaction_test.go` | Executor compaction integration tests |

---

## Implementation Order

1. **Phase 1** — Iteration changes (`agent.go`, `iteration_test.go`)
2. **Phase 2** — Interfaces and executor integration (`compaction.go`, `events.go`,
   `event_names.go`, `stats_keys.go`, `context.go`, `executor/executor.go`,
   mocks, `executor/executor_compaction_test.go`)
3. **Phase 3** — StatThresholdTrigger (`compaction/stat_threshold_trigger.go` + tests)
4. **Phase 4** — SlidingWindow (`compaction/sliding_window.go` + tests)
5. **Phase 5** — Summarization (`compaction/summarization.go` + tests)

Each phase is independently testable. Phases 3-5 can be developed in parallel since they
only depend on Phase 2 interfaces.

---

## Design Decisions & Rationale

### Why compaction is fatal on error
Silent compaction failure leads to unbounded context growth that degrades quality without any
signal to the user. Failing immediately surfaces the issue during development and testing.

### Why the strategy calls SetScratchPad (not the executor)
`LoopData.SetScratchPad` already publishes `CommonDiffEvent` and updates `SGScratchpadLength`.
Having the executor be an intermediary adds no value — the side effects belong in `SetScratchPad`.

### Why compaction runs before BeforeIteration (not after AfterIteration)
1. No need to check for termination (LATerminate exits the loop before reaching this point)
2. Per-iteration gauges from the previous iteration are still available for the trigger
   (not yet reset by `BeforeIterationEvent`)
3. First iteration naturally skips compaction (`Iteration() == 0`)
4. Semantically clear: "prepare context, then start iteration"

### Why counters use delta-based triggering
Counters only go up. Without delta tracking, once a counter exceeds the threshold it would
trigger compaction every iteration. The delta approach means "compact every N additional
iterations/tokens" which is the natural intent.

### Why gauges use absolute triggering
Gauges can go up and down. After compaction, gauge values (like `SGScratchpadLength`) naturally
decrease, so the trigger won't re-fire until the gauge grows back above the threshold.

### Why pinned iterations are "bonus slots"
If pinned iterations counted toward the window size, pinning could starve the window of
recent context. Bonus slots ensure the window always has room for recent iterations.

### Why multi-modal content is dropped (not described)
Adding a ContentPartFilter hook adds complexity for a rare use case. Users who need
multi-modal-aware summarization can implement their own strategy. Pinning is the escape hatch
for preserving important multi-modal iterations.
