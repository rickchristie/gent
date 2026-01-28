package gent

import "sync"

// ExecutionStats contains generic counters and gauges for tracking execution metrics.
// All standard gent metrics use keys prefixed with "gent:" to avoid collisions
// with user-defined keys.
//
// ExecutionStats supports hierarchical aggregation: when a child context increments
// a counter or gauge, the increment propagates to the parent in real-time. This
// ensures limits on parent contexts are enforced correctly even with nested or
// parallel agent loops.
//
// Limit checking is triggered automatically when stats are modified. The check
// happens on the root ExecutionContext which has the aggregated stats from all
// children.
//
// Thread Safety:
// - All methods are safe for concurrent use
// - When multiple child contexts run in parallel, all propagate to the parent
//   concurrently with proper mutex protection
type ExecutionStats struct {
	mu       sync.RWMutex
	counters map[string]int64
	gauges   map[string]float64
	parent   *ExecutionStats      // nil for root context
	execCtx  *ExecutionContext    // back-reference for limit checking (nil if standalone)
	rootCtx  *ExecutionContext    // root context for limit checking (cached)
}

// NewExecutionStats creates a new ExecutionStats instance without context association.
// Use this for standalone stats that don't need limit checking.
func NewExecutionStats() *ExecutionStats {
	return &ExecutionStats{
		counters: make(map[string]int64),
		gauges:   make(map[string]float64),
	}
}

// newExecutionStatsWithContext creates a new ExecutionStats with a back-reference
// to the ExecutionContext for limit checking.
func newExecutionStatsWithContext(ctx *ExecutionContext) *ExecutionStats {
	return &ExecutionStats{
		counters: make(map[string]int64),
		gauges:   make(map[string]float64),
		execCtx:  ctx,
		rootCtx:  ctx, // root context is itself
	}
}

// newExecutionStatsWithContextAndParent creates a new ExecutionStats with both
// a parent for hierarchical aggregation and a context back-reference.
func newExecutionStatsWithContextAndParent(ctx *ExecutionContext, parent *ExecutionStats) *ExecutionStats {
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

// newExecutionStatsWithParent creates a new ExecutionStats with a parent reference
// for hierarchical aggregation (without context association).
func newExecutionStatsWithParent(parent *ExecutionStats) *ExecutionStats {
	return &ExecutionStats{
		counters: make(map[string]int64),
		gauges:   make(map[string]float64),
		parent:   parent,
	}
}

// IncrCounter increments a counter by delta. Creates the counter if it doesn't exist.
// The increment propagates to parent stats in real-time for hierarchical aggregation.
//
// Note: gent:iterations is protected and can only be incremented via incrCounterInternal.
func (s *ExecutionStats) IncrCounter(key string, delta int64) {
	if isProtectedKey(key) {
		return // Silently ignore protected keys
	}
	s.incrCounterInternal(key, delta)
}

// incrCounterInternal increments a counter without protection checks.
// Used internally for updating stats from events.
// Each context checks its own limits, then propagates to parent.
func (s *ExecutionStats) incrCounterInternal(key string, delta int64) {
	s.mu.Lock()
	s.counters[key] += delta
	s.mu.Unlock()

	// Check limits on this context
	if s.execCtx != nil {
		s.execCtx.checkLimits()
	}

	// Propagate to parent (which will check its own limits)
	if s.parent != nil {
		s.parent.incrCounterInternal(key, delta)
	}
}

// SetCounter sets a counter to a specific value.
// Note: gent:iterations is protected and cannot be set by user code.
func (s *ExecutionStats) SetCounter(key string, value int64) {
	if isProtectedKey(key) {
		return // Silently ignore protected keys
	}
	s.mu.Lock()
	s.counters[key] = value
	s.mu.Unlock()
}

// GetCounter returns the current value of a counter, or 0 if not set.
func (s *ExecutionStats) GetCounter(key string) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.counters[key]
}

// ResetCounter sets a counter to 0.
// Note: gent:iterations is protected and cannot be reset by user code.
func (s *ExecutionStats) ResetCounter(key string) {
	if isProtectedKey(key) {
		return // Silently ignore protected keys
	}
	s.mu.Lock()
	s.counters[key] = 0
	s.mu.Unlock()
}

// IncrGauge increments a gauge by delta. Creates the gauge if it doesn't exist.
// The increment propagates to parent stats in real-time for hierarchical aggregation.
// Each context checks its own limits, then propagates to parent.
func (s *ExecutionStats) IncrGauge(key string, delta float64) {
	s.mu.Lock()
	s.gauges[key] += delta
	s.mu.Unlock()

	// Check limits on this context
	if s.execCtx != nil {
		s.execCtx.checkLimits()
	}

	// Propagate to parent (which will check its own limits)
	if s.parent != nil {
		s.parent.IncrGauge(key, delta)
	}
}

// SetGauge sets a gauge to a specific value.
// Note: SetGauge does NOT propagate to parent (unlike IncrGauge).
// Use IncrGauge for values that should aggregate hierarchically.
func (s *ExecutionStats) SetGauge(key string, value float64) {
	s.mu.Lock()
	s.gauges[key] = value
	s.mu.Unlock()

	// Check limits on this context
	if s.execCtx != nil {
		s.execCtx.checkLimits()
	}
}

// GetGauge returns the current value of a gauge, or 0.0 if not set.
func (s *ExecutionStats) GetGauge(key string) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.gauges[key]
}

// ResetGauge sets a gauge to 0.0.
func (s *ExecutionStats) ResetGauge(key string) {
	s.mu.Lock()
	s.gauges[key] = 0
	s.mu.Unlock()
}

// Counters returns a copy of all counters.
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

// GetTotalInputTokens returns the total input tokens across all models.
func (s *ExecutionStats) GetTotalInputTokens() int64 {
	return s.GetCounter(KeyInputTokens)
}

// GetTotalOutputTokens returns the total output tokens across all models.
func (s *ExecutionStats) GetTotalOutputTokens() int64 {
	return s.GetCounter(KeyOutputTokens)
}

// GetToolCallCount returns the total number of tool calls.
func (s *ExecutionStats) GetToolCallCount() int64 {
	return s.GetCounter(KeyToolCalls)
}

// GetFormatParseErrorTotal returns the total number of format parse errors.
func (s *ExecutionStats) GetFormatParseErrorTotal() int64 {
	return s.GetCounter(KeyFormatParseErrorTotal)
}

// GetToolchainParseErrorTotal returns the total number of toolchain parse errors.
func (s *ExecutionStats) GetToolchainParseErrorTotal() int64 {
	return s.GetCounter(KeyToolchainParseErrorTotal)
}

// GetIterations returns the current iteration count.
func (s *ExecutionStats) GetIterations() int64 {
	return s.GetCounter(KeyIterations)
}
