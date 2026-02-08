package gent

import "sync"

// ExecutionStats contains counters and gauges for tracking execution
// metrics. All standard gent metrics use keys prefixed with "gent:"
// to avoid collisions with user-defined keys.
//
// # Use Cases
//
// Stats serve three purposes:
//
//  1. Termination limits: Checked on every update to stop runaway
//     agent loops (e.g., max iterations, max tokens, consecutive
//     errors). See [Limit] and [DefaultLimits].
//
//  2. Compaction decisions: Read during scratchpad compaction to
//     inform what to keep or discard (e.g., token counts, iteration
//     counts).
//
//  3. Event-driven actions: Read by event subscribers for milestones
//     like saving state, logging, or triggering alerts.
//
// # Counters vs Gauges
//
// Counters are monotonically increasing (only go up). They always
// propagate from child to parent contexts. Each counter key also has
// a $self:-prefixed counterpart that tracks only the local context's
// direct increments (no propagation). Use [StatKey.Self] to get the
// local-only variant for limits.
//
// Gauges can go up and down (via [IncrGauge], [SetGauge],
// [ResetGauge]). They never propagate to parent contexts. Use gauges
// for values that reset or fluctuate, such as consecutive error counts.
//
// # Hierarchical Aggregation (Counters Only)
//
// When a child context increments a counter, the increment propagates
// to the parent in real-time. This ensures limits on parent contexts
// are enforced correctly even with nested or parallel agent loops.
// The $self:-prefixed counterpart does NOT propagate, allowing
// per-context limits.
//
// Gauges are always local to the context that sets them. They do not
// propagate to parent contexts.
//
// # Limit Checking
//
// Limit checking is triggered automatically when stats are modified.
// Both counters and gauges are checked against configured limits.
//
// # Thread Safety
//
// All methods are safe for concurrent use. When multiple child
// contexts run in parallel, all propagate to the parent concurrently
// with proper mutex protection.
type ExecutionStats struct {
	mu       sync.RWMutex
	counters map[string]int64
	gauges   map[string]float64
	parent   *ExecutionStats   // nil for root context
	execCtx  *ExecutionContext // back-ref for limit checking
	rootCtx  *ExecutionContext // root context (cached)
}

// NewExecutionStats creates a new ExecutionStats instance without
// context association. Use this for standalone stats that don't need
// limit checking.
func NewExecutionStats() *ExecutionStats {
	return &ExecutionStats{
		counters: make(map[string]int64),
		gauges:   make(map[string]float64),
	}
}

// newExecutionStatsWithContext creates a new ExecutionStats with a
// back-reference to the ExecutionContext for limit checking.
func newExecutionStatsWithContext(
	ctx *ExecutionContext,
) *ExecutionStats {
	return &ExecutionStats{
		counters: make(map[string]int64),
		gauges:   make(map[string]float64),
		execCtx:  ctx,
		rootCtx:  ctx, // root context is itself
	}
}

// newExecutionStatsWithContextAndParent creates a new ExecutionStats
// with both a parent for hierarchical aggregation and a context
// back-reference.
func newExecutionStatsWithContextAndParent(
	ctx *ExecutionContext,
	parent *ExecutionStats,
) *ExecutionStats {
	// Find root context by traversing parent chain
	rootCtx := ctx
	for rootCtx.parent != nil {
		rootCtx = rootCtx.parent
	}

	return &ExecutionStats{
		counters: make(map[string]int64),
		gauges:   make(map[string]float64),
		parent:   parent,
		execCtx:  ctx,
		rootCtx:  rootCtx,
	}
}

// IncrCounter increments a counter by delta. Creates the counter if
// it doesn't exist. The increment propagates to parent stats in
// real-time for hierarchical aggregation. A $self:-prefixed
// counterpart is also incremented locally (no propagation).
//
// Panics if delta is negative (counters only go up).
// Panics if key has the reserved $self: prefix.
// Protected keys (e.g., SCIterations) are silently ignored.
func (s *ExecutionStats) IncrCounter(key StatKey, delta int64) {
	if delta < 0 {
		panic("gent: IncrCounter called with negative delta")
	}
	if key.IsSelf() {
		panic(
			"gent: IncrCounter called with reserved " +
				selfPrefix + " prefix",
		)
	}
	if isProtectedKey(key) {
		return // Silently ignore protected keys
	}
	s.incrCounterDirect(key, delta)
}

// incrCounterDirect increments a counter as a direct operation.
// Writes to both the base key and the $self: counterpart.
// Then propagates only the base key to parent.
//
// All counters propagate, including protected keys like
// SCIterations. Protection only blocks user code from calling
// IncrCounter() — it does not affect propagation.
//
// Used by IncrCounter (public) and updateStatsForEvent (framework).
func (s *ExecutionStats) incrCounterDirect(
	key StatKey,
	delta int64,
) {
	sKey := string(key)

	s.mu.Lock()
	s.counters[sKey] += delta
	s.counters[selfPrefix+sKey] += delta
	s.mu.Unlock()

	// Check limits on this context
	if s.execCtx != nil {
		s.execCtx.checkLimits()
	}

	// Propagate to parent (base key only, not $self:)
	if s.parent != nil {
		s.parent.incrCounterPropagated(key, delta)
	}
}

// incrCounterPropagated increments a counter as a propagated
// operation from a child context. Writes only to the base key
// (NOT the $self: counterpart). Then continues propagating to parent.
func (s *ExecutionStats) incrCounterPropagated(
	key StatKey,
	delta int64,
) {
	sKey := string(key)

	s.mu.Lock()
	s.counters[sKey] += delta
	s.mu.Unlock()

	// Check limits on this context
	if s.execCtx != nil {
		s.execCtx.checkLimits()
	}

	// Continue propagating to parent
	if s.parent != nil {
		s.parent.incrCounterPropagated(key, delta)
	}
}

// GetCounter returns the current value of a counter, or 0 if not set.
func (s *ExecutionStats) GetCounter(key StatKey) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.counters[string(key)]
}

// IncrGauge increments a gauge by delta (positive or negative).
// Creates the gauge if it doesn't exist.
//
// Gauges never propagate to parent contexts. They are local to the
// execution context that modifies them.
func (s *ExecutionStats) IncrGauge(key StatKey, delta float64) {
	s.mu.Lock()
	s.gauges[string(key)] += delta
	s.mu.Unlock()

	// Check limits on this context
	if s.execCtx != nil {
		s.execCtx.checkLimits()
	}
}

// incrGaugeInternal increments a gauge without any public API checks.
// Used internally by updateStatsForEvent for framework gauge updates.
func (s *ExecutionStats) incrGaugeInternal(
	key StatKey,
	delta float64,
) {
	s.mu.Lock()
	s.gauges[string(key)] += delta
	s.mu.Unlock()

	// Check limits on this context
	if s.execCtx != nil {
		s.execCtx.checkLimits()
	}
}

// SetGauge sets a gauge to a specific value.
// Gauges never propagate to parent contexts.
func (s *ExecutionStats) SetGauge(key StatKey, value float64) {
	s.mu.Lock()
	s.gauges[string(key)] = value
	s.mu.Unlock()

	// Check limits on this context
	if s.execCtx != nil {
		s.execCtx.checkLimits()
	}
}

// GetGauge returns the current value of a gauge, or 0.0 if not set.
func (s *ExecutionStats) GetGauge(key StatKey) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.gauges[string(key)]
}

// ResetGauge sets a gauge to 0.0.
func (s *ExecutionStats) ResetGauge(key StatKey) {
	s.mu.Lock()
	s.gauges[string(key)] = 0
	s.mu.Unlock()
}

// resetGaugesByPrefix resets all gauges matching the given prefix
// to 0. Used internally for resetting per-iteration gauges at
// iteration boundaries.
func (s *ExecutionStats) resetGaugesByPrefix(
	prefix StatKey,
) {
	prefixStr := string(prefix)
	s.mu.Lock()
	for key := range s.gauges {
		if len(key) >= len(prefixStr) &&
			key[:len(prefixStr)] == prefixStr {
			s.gauges[key] = 0
		}
	}
	s.mu.Unlock()
}

// Counters returns a copy of all counters. This includes both
// propagated keys (e.g., "gent:input_tokens") and $self:-prefixed
// local-only keys (e.g., "$self:gent:input_tokens"). Use
// [StatKey.IsSelf] to filter if needed.
func (s *ExecutionStats) Counters() map[string]int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]int64, len(s.counters))
	for k, v := range s.counters {
		result[k] = v
	}
	return result
}

// Gauges returns a copy of all gauges.
func (s *ExecutionStats) Gauges() map[string]float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]float64, len(s.gauges))
	for k, v := range s.gauges {
		result[k] = v
	}
	return result
}

// GetTotalInputTokens returns the total input tokens across all
// models (aggregated from children).
func (s *ExecutionStats) GetTotalInputTokens() int64 {
	return s.GetCounter(SCInputTokens)
}

// GetTotalOutputTokens returns the total output tokens across all
// models (aggregated from children).
func (s *ExecutionStats) GetTotalOutputTokens() int64 {
	return s.GetCounter(SCOutputTokens)
}

// GetTotalTokens returns the total tokens (input + output) across
// all models (aggregated from children).
func (s *ExecutionStats) GetTotalTokens() int64 {
	return s.GetCounter(SCTotalTokens)
}

// GetToolCallCount returns the total number of tool calls
// (aggregated from children).
func (s *ExecutionStats) GetToolCallCount() int64 {
	return s.GetCounter(SCToolCalls)
}

// GetFormatParseErrorTotal returns the total number of format parse
// errors (aggregated from children).
func (s *ExecutionStats) GetFormatParseErrorTotal() int64 {
	return s.GetCounter(SCFormatParseErrorTotal)
}

// GetIterations returns the current iteration count (aggregated
// from children).
func (s *ExecutionStats) GetIterations() int64 {
	return s.GetCounter(SCIterations)
}
