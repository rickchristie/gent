package compaction

import "github.com/rickchristie/gent"

// StatThresholdTrigger fires when any configured stat
// threshold is exceeded. Supports both counter and gauge
// thresholds with different semantics.
//
// # Counter Thresholds (Delta-Based)
//
// Counters only go up, so the trigger tracks the counter
// value at the time of last compaction. It fires when:
//
//	currentValue - lastCompactionValue >= threshold
//
// When ANY threshold triggers compaction, ALL counter
// snapshots are updated to current values. This prevents
// re-triggering.
//
// # Gauge Thresholds (Absolute)
//
// Gauges can go up and down, so the trigger checks the
// current value directly against the threshold:
//
//	currentValue >= threshold
//
// No snapshot tracking is needed — gauges naturally reflect
// current state (e.g., SGScratchpadLength decreases after
// compaction).
//
// # Match Modes
//
// Both counter and gauge thresholds support exact key and
// prefix matching, consistent with the Limit system.
//
// # Example
//
//	trigger := compaction.NewStatThresholdTrigger().
//	    OnCounter(gent.SCIterations, 10).
//	    OnCounterPrefix(gent.SCInputTokensFor, 50000).
//	    OnGauge(gent.SGScratchpadLength, 20).
//	    OnGauge(
//	        gent.SGTotalTokensLastIteration, 100000,
//	    )
type StatThresholdTrigger struct {
	counterThresholds []counterThreshold
	gaugeThresholds   []gaugeThreshold
}

type counterThreshold struct {
	key       gent.StatKey
	matchMode gent.LimitType
	delta     int64
	lastValue map[string]int64
}

type gaugeThreshold struct {
	key       gent.StatKey
	matchMode gent.LimitType
	value     float64
}

// NewStatThresholdTrigger creates a new
// StatThresholdTrigger.
func NewStatThresholdTrigger() *StatThresholdTrigger {
	return &StatThresholdTrigger{}
}

// OnCounter adds an exact-key counter threshold.
// Fires when (current - lastCompaction) >= delta.
func (t *StatThresholdTrigger) OnCounter(
	key gent.StatKey,
	delta int64,
) *StatThresholdTrigger {
	t.counterThresholds = append(
		t.counterThresholds,
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
	t.counterThresholds = append(
		t.counterThresholds,
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
	t.gaugeThresholds = append(
		t.gaugeThresholds,
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
	t.gaugeThresholds = append(
		t.gaugeThresholds,
		gaugeThreshold{
			key:       prefix,
			matchMode: gent.LimitKeyPrefix,
			value:     value,
		},
	)
	return t
}

// ShouldCompact implements gent.CompactionTrigger.
func (t *StatThresholdTrigger) ShouldCompact(
	execCtx *gent.ExecutionContext,
) bool {
	stats := execCtx.Stats()

	for i := range t.counterThresholds {
		ct := &t.counterThresholds[i]
		if t.counterExceeded(stats, ct) {
			return true
		}
	}

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

// NotifyCompacted implements gent.CompactionTrigger.
// Snapshots ALL counter values for delta tracking.
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

// Compile-time check.
var _ gent.CompactionTrigger = (*StatThresholdTrigger)(nil)
