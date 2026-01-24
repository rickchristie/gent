# Generic Stats and Limits System

This document describes the generic stats tracking and limits system in gent.

## Overview

The stats and limits system provides:

- **Generic counters and gauges** for tracking any metrics
- **Standard keys** prefixed with `gent:` for common metrics (tokens, costs, parse errors)
- **Configurable limits** to terminate execution based on thresholds
- **Automatic tracking** via Trace events - no manual wiring needed
- **Real-time aggregation** across parent/child contexts for accurate limit enforcement
- **Hook observability** - all stats accessible during hook execution

## Package: gent

### ExecutionStats

```go
// ExecutionStats contains generic counters and gauges for tracking execution metrics.
// All standard gent metrics use keys prefixed with "gent:" to avoid collisions
// with user-defined keys.
//
// ExecutionStats supports hierarchical aggregation: when a child context increments
// a counter or gauge, the increment propagates to the parent in real-time. This
// ensures limits on parent contexts are enforced correctly even with nested or
// parallel agent loops.
type ExecutionStats struct {
    mu       sync.RWMutex
    counters map[string]int64
    gauges   map[string]float64
    parent   *ExecutionStats // nil for root context
}
```

### Standard Keys

All standard keys are prefixed with `gent:` to avoid collisions with user-defined keys.
Users should use their own prefix (e.g., `myapp:`) for custom metrics.

```go
// Standard key prefix for all gent library keys.
const KeyPrefix = "gent:"

// Token tracking
const (
    KeyInputTokens     = "gent:input_tokens"
    KeyInputTokensFor  = "gent:input_tokens:"  // + model name
    KeyOutputTokens    = "gent:output_tokens"
    KeyOutputTokensFor = "gent:output_tokens:" // + model name
)

// Cost tracking (gauges)
const (
    KeyCost    = "gent:cost"
    KeyCostFor = "gent:cost:" // + model name
)

// Tool call tracking
const (
    KeyToolCalls    = "gent:tool_calls"
    KeyToolCallsFor = "gent:tool_calls:" // + tool name
)

// Format parse error tracking (errors parsing LLM output sections)
const (
    KeyFormatParseErrorTotal       = "gent:format_parse_error_total"
    KeyFormatParseErrorAt          = "gent:format_parse_error:"             // + iteration
    KeyFormatParseErrorConsecutive = "gent:format_parse_error_consecutive"
)

// Toolchain parse error tracking (errors parsing YAML/JSON tool calls)
const (
    KeyToolchainParseErrorTotal       = "gent:toolchain_parse_error_total"
    KeyToolchainParseErrorAt          = "gent:toolchain_parse_error:"             // + iteration
    KeyToolchainParseErrorConsecutive = "gent:toolchain_parse_error_consecutive"
)
```

### Iteration Tracking

Iteration count is managed separately from generic stats because it is executor-controlled
and must not be modified by user code.

```go
// On ExecutionContext (not ExecutionStats)

// Iteration returns the current iteration number (1-indexed).
// Returns 0 if no iteration has started.
// This value is controlled by the Executor and cannot be modified by user code.
func (ctx *ExecutionContext) Iteration() int
```

For limit checking, use the special key `gent:iterations` which the executor manages:

```go
const KeyIterations = "gent:iterations"
```

This key is **protected** - attempts to modify it via `SetCounter` or `ResetCounter`
will be silently ignored. Only the Executor can increment this counter.

### ExecutionStats Methods

All stats operations are on the `ExecutionStats` struct, accessed via `ctx.Stats()`.

#### Counter Operations

```go
// IncrCounter increments a counter by delta. Creates the counter if it doesn't exist.
// The increment propagates to parent stats in real-time for hierarchical aggregation.
func (s *ExecutionStats) IncrCounter(key string, delta int64)

// SetCounter sets a counter to a specific value.
// Note: gent:iterations is protected and cannot be set by user code.
func (s *ExecutionStats) SetCounter(key string, value int64)

// GetCounter returns the current value of a counter, or 0 if not set.
func (s *ExecutionStats) GetCounter(key string) int64

// ResetCounter sets a counter to 0.
// Note: gent:iterations is protected and cannot be reset by user code.
func (s *ExecutionStats) ResetCounter(key string)
```

#### Gauge Operations

```go
// IncrGauge increments a gauge by delta. Creates the gauge if it doesn't exist.
// The increment propagates to parent stats in real-time for hierarchical aggregation.
func (s *ExecutionStats) IncrGauge(key string, delta float64)

// SetGauge sets a gauge to a specific value.
func (s *ExecutionStats) SetGauge(key string, value float64)

// GetGauge returns the current value of a gauge, or 0.0 if not set.
func (s *ExecutionStats) GetGauge(key string) float64

// ResetGauge sets a gauge to 0.0.
func (s *ExecutionStats) ResetGauge(key string)
```

#### Bulk Access

```go
// Counters returns a copy of all counters.
func (s *ExecutionStats) Counters() map[string]int64

// Gauges returns a copy of all gauges.
func (s *ExecutionStats) Gauges() map[string]float64
```

#### Convenience Getters

These methods read from the generic maps using standard keys:

```go
// GetTotalInputTokens returns the total input tokens across all models.
func (s *ExecutionStats) GetTotalInputTokens() int64 {
    return s.GetCounter(KeyInputTokens)
}

// GetTotalOutputTokens returns the total output tokens across all models.
func (s *ExecutionStats) GetTotalOutputTokens() int64 {
    return s.GetCounter(KeyOutputTokens)
}

// GetTotalCost returns the total cost across all models.
func (s *ExecutionStats) GetTotalCost() float64 {
    return s.GetGauge(KeyCost)
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
```

### ExecutionContext Stats Access

```go
// Stats returns the execution stats for this context.
//
// Thread Safety:
// - Safe to call from any goroutine
// - All ExecutionStats methods handle their own locking
// - Safe to read during hook execution (hooks run synchronously)
func (ctx *ExecutionContext) Stats() *ExecutionStats
```

### Real-Time Parent Aggregation

When a child ExecutionContext is spawned, its ExecutionStats holds a reference to the
parent's ExecutionStats. All counter and gauge increments propagate to the parent
immediately:

```go
func (s *ExecutionStats) IncrCounter(key string, delta int64) {
    s.mu.Lock()
    if s.counters == nil {
        s.counters = make(map[string]int64)
    }
    s.counters[key] += delta
    s.mu.Unlock()

    // Propagate to parent in real-time
    if s.parent != nil {
        s.parent.IncrCounter(key, delta)
    }
}
```

This ensures:
- Parent context sees aggregated values from all descendants
- Limits on parent context are enforced correctly for nested/parallel loops
- If parent has 1M token limit, child token usage counts toward that limit immediately

## Package: executor

### Limit Configuration

```go
// LimitType specifies how to match keys for limit checking.
type LimitType string

const (
    // LimitExactKey matches an exact key.
    LimitExactKey LimitType = "exact"

    // LimitKeyPrefix matches any key with the given prefix.
    // Useful for checking limits across all models or tools.
    LimitKeyPrefix LimitType = "prefix"
)

// Limit defines a threshold that triggers execution termination.
type Limit struct {
    // Type specifies how to match keys (exact or prefix).
    Type LimitType

    // Key is the exact key or prefix to match.
    Key string

    // MaxValue is the threshold. Execution terminates when the value exceeds this.
    // For counters, the int64 value is compared as float64.
    MaxValue float64
}

// Config holds configuration options for the Executor.
type Config struct {
    // Limits defines thresholds that trigger execution termination.
    // Limits are checked after each iteration in order.
    // The first limit exceeded determines which limit is reported.
    Limits []Limit
}
```

### Default Limits

```go
// DefaultLimits returns a set of sensible default limits.
func DefaultLimits() []Limit {
    return []Limit{
        // Stop after 100 iterations
        {Type: LimitExactKey, Key: gent.KeyIterations, MaxValue: 100},

        // Stop after 3 consecutive format parse errors
        {Type: LimitExactKey, Key: gent.KeyFormatParseErrorConsecutive, MaxValue: 3},

        // Stop after 3 consecutive toolchain parse errors
        {Type: LimitExactKey, Key: gent.KeyToolchainParseErrorConsecutive, MaxValue: 3},
    }
}
```

### Limit Checking Behavior

1. **Timing**: Limits are checked after each iteration completes
2. **Order**: Limits are evaluated in the order they are defined
3. **First Match**: The first limit exceeded determines which limit is reported in the result
4. **Comparison**: Values must EXCEED the threshold (not equal) to trigger termination
5. **Termination Reason**: Always `TerminationLimitExceeded` when any limit is exceeded

### ExecutionResult

```go
// ExecutionResult contains the outcome of an execution.
type ExecutionResult struct {
    // Result contains the final output from the agent loop (if successful).
    Result []ContentPart

    // Context is the ExecutionContext used during execution.
    Context *ExecutionContext

    // ExceededLimit is non-nil if execution terminated due to a limit being exceeded.
    // Inspect this to determine which limit was hit and its threshold.
    ExceededLimit *Limit
}
```

### Example Usage

```go
exec := executor.New(loop, executor.Config{
    Limits: []executor.Limit{
        // Stop after 100 iterations
        {Type: executor.LimitExactKey, Key: gent.KeyIterations, MaxValue: 100},

        // Stop after 3 consecutive format parse errors
        {Type: executor.LimitExactKey, Key: gent.KeyFormatParseErrorConsecutive, MaxValue: 3},

        // Stop after 3 consecutive toolchain parse errors
        {Type: executor.LimitExactKey, Key: gent.KeyToolchainParseErrorConsecutive, MaxValue: 3},

        // Stop if total input tokens exceed 100k
        {Type: executor.LimitExactKey, Key: gent.KeyInputTokens, MaxValue: 100000},

        // Stop if ANY single model exceeds 50k input tokens
        {Type: executor.LimitKeyPrefix, Key: gent.KeyInputTokensFor, MaxValue: 50000},

        // Stop if total cost exceeds $10
        {Type: executor.LimitExactKey, Key: gent.KeyCost, MaxValue: 10.0},
    },
})

result, err := exec.Execute(ctx, data)
if err != nil {
    if result.Context.TerminationReason() == gent.TerminationLimitExceeded {
        limit := result.ExceededLimit
        fmt.Printf("Limit exceeded: key=%s, max=%f\n", limit.Key, limit.MaxValue)
    }
}
```

### Limit Checking Implementation

```go
// checkLimits evaluates all configured limits against current stats.
// Returns the exceeded limit if any, or nil if all limits are within bounds.
// Limits are checked in order; first match wins.
func (e *Executor) checkLimits(execCtx *ExecutionContext) *Limit {
    stats := execCtx.Stats()

    for i := range e.config.Limits {
        limit := &e.config.Limits[i]
        switch limit.Type {
        case LimitExactKey:
            if e.checkExactKeyLimit(stats, limit) {
                return limit
            }

        case LimitKeyPrefix:
            if e.checkPrefixLimit(stats, limit) {
                return limit
            }
        }
    }
    return nil
}

func (e *Executor) checkExactKeyLimit(stats *gent.ExecutionStats, limit *Limit) bool {
    // Check counters
    if val := stats.GetCounter(limit.Key); val > 0 {
        if float64(val) > limit.MaxValue {
            return true
        }
    }
    // Check gauges
    if val := stats.GetGauge(limit.Key); val > 0 {
        if val > limit.MaxValue {
            return true
        }
    }
    return false
}

func (e *Executor) checkPrefixLimit(stats *gent.ExecutionStats, limit *Limit) bool {
    for key, val := range stats.Counters() {
        if strings.HasPrefix(key, limit.Key) && float64(val) > limit.MaxValue {
            return true
        }
    }
    for key, val := range stats.Gauges() {
        if strings.HasPrefix(key, limit.Key) && val > limit.MaxValue {
            return true
        }
    }
    return false
}
```

## Termination Reasons

```go
type TerminationReason string

const (
    // Success indicates the loop terminated normally via LATerminate.
    TerminationSuccess TerminationReason = "success"

    // ContextCanceled indicates the context was canceled.
    TerminationContextCanceled TerminationReason = "context_canceled"

    // HookAbort indicates a hook aborted execution.
    TerminationHookAbort TerminationReason = "hook_abort"

    // Error indicates an error occurred during execution.
    TerminationError TerminationReason = "error"

    // LimitExceeded indicates a configured limit was exceeded.
    // Inspect ExecutionResult.ExceededLimit for details.
    TerminationLimitExceeded TerminationReason = "limit_exceeded"
)
```

Note: `TerminationMaxIterations`, `TerminationParseError`, `TerminationTokenLimit`,
and `TerminationCostLimit` are removed. All limit-based terminations use
`TerminationLimitExceeded` with the specific limit available in `ExecutionResult.ExceededLimit`.

## Auto-Aggregation via Trace

Stats are automatically updated when trace events are recorded. This makes it easy
for model and toolchain authors - they just call `Trace()` and stats update automatically.

### ModelCallTrace Auto-Aggregation

When `execCtx.Trace(ModelCallTrace{...})` is called:

```go
stats.IncrCounter(KeyInputTokens, trace.InputTokens)
stats.IncrCounter(KeyInputTokensFor+trace.Model, trace.InputTokens)
stats.IncrCounter(KeyOutputTokens, trace.OutputTokens)
stats.IncrCounter(KeyOutputTokensFor+trace.Model, trace.OutputTokens)
stats.IncrGauge(KeyCost, trace.Cost)
stats.IncrGauge(KeyCostFor+trace.Model, trace.Cost)
```

### ToolCallTrace Auto-Aggregation

When `execCtx.Trace(ToolCallTrace{...})` is called:

```go
stats.IncrCounter(KeyToolCalls, 1)
stats.IncrCounter(KeyToolCallsFor+trace.ToolName, 1)
```

### ParseErrorTrace Auto-Aggregation

When `execCtx.Trace(ParseErrorTrace{...})` is called:

```go
iteration := strconv.Itoa(execCtx.Iteration())

if trace.ErrorType == "format" {
    stats.IncrCounter(KeyFormatParseErrorTotal, 1)
    stats.IncrCounter(KeyFormatParseErrorAt+iteration, 1)
    stats.IncrCounter(KeyFormatParseErrorConsecutive, 1)
} else if trace.ErrorType == "toolchain" {
    stats.IncrCounter(KeyToolchainParseErrorTotal, 1)
    stats.IncrCounter(KeyToolchainParseErrorAt+iteration, 1)
    stats.IncrCounter(KeyToolchainParseErrorConsecutive, 1)
}
```

### Resetting Consecutive Counters

On successful parse (no error), the agent loop or toolchain resets the consecutive counter:

```go
// In ReAct agent after successful format parse
execCtx.Stats().ResetCounter(KeyFormatParseErrorConsecutive)

// In toolchain after successful parse
execCtx.Stats().ResetCounter(KeyToolchainParseErrorConsecutive)
```

## Parse Error Handling

When parse errors occur, they are:

1. **Traced** as `ParseErrorTrace` events (which auto-updates stats)
2. **Fed back** to the agent as observations with full error details

### ParseErrorTrace Event

```go
type ParseErrorTrace struct {
    BaseTrace
    ErrorType  string // "format" or "toolchain"
    RawContent string // The content that failed to parse (no truncation)
    Error      error
}
```

### Format Parse Error Feedback

When format parsing fails, the error is traced and fed back as an observation:

```go
// In ReAct agent
parsed, parseErr := r.format.Parse(responseContent)
if parseErr != nil {
    // Trace error (auto-updates stats)
    execCtx.Trace(gent.ParseErrorTrace{
        ErrorType:  "format",
        RawContent: responseContent,
        Error:      parseErr,
    })

    // Feed back to agent with full context
    observation := fmt.Sprintf(`Format parse error: %v

Your response could not be parsed. Please ensure your response follows the expected format.

Your raw response was:
%s

Please try again with proper formatting.`, parseErr, responseContent)

    return &gent.LoopResult{
        Action:     gent.LAContinue,
        NextPrompt: []gent.ContentPart{llms.TextContent{Text: wrapObservation(observation)}},
    }, nil
}

// On success, reset consecutive counter
execCtx.Stats().ResetCounter(gent.KeyFormatParseErrorConsecutive)
```

## Memory Considerations

### Per-Iteration Keys

Keys like `gent:format_parse_error:{iteration}` grow with each iteration that has errors.

**Trade-off**: We accept slightly higher memory usage in exchange for better debuggability.
Having per-iteration error data makes it easy to identify which iterations had problems.

**Mitigation**: Keys are only created when errors occur. For successful executions
with no parse errors, these keys don't exist and consume no memory.

### Custom Keys

Users defining custom keys should be mindful of key cardinality. Avoid patterns like
`myapp:request:{uuid}` that create unbounded unique keys. Prefer aggregated keys
like `myapp:request_count` or bounded keys like `myapp:request:{iteration}`.

## User-Defined Metrics

Users can define their own metrics using any key that doesn't start with `gent:`:

```go
// In a custom loop or hook
execCtx.Stats().IncrCounter("myapp:retries", 1)
execCtx.Stats().SetGauge("myapp:confidence", 0.95)

// With limits
executor.New(loop, executor.Config{
    Limits: []executor.Limit{
        {Type: executor.LimitExactKey, Key: "myapp:retries", MaxValue: 5},
    },
})
```

## Migration Notes

### Files to Modify

1. **gent/stats.go** (new file)
   - Define `ExecutionStats` struct with mutex and parent reference
   - Implement counter/gauge methods with parent propagation
   - Implement convenience getters
   - Define protected keys handling

2. **gent/stats_keys.go** (new file)
   - Define all standard key constants

3. **gent/context.go**
   - Remove old `ExecutionStats` struct
   - Update `NewExecutionContext` to initialize new stats
   - Update `SpawnChild` to set parent reference on child stats
   - Remove `CompleteChild` stats aggregation (now real-time)
   - Update `Stats()` to return `*ExecutionStats`
   - Keep `Iteration()` as separate method

4. **gent/trace.go**
   - Add `ParseErrorTrace` event type
   - Update `traceEventLocked` to auto-aggregate stats based on event type

5. **gent/types.go**
   - Update `TerminationReason` constants (remove specific ones, keep generic)
   - Update `ExecutionResult` to add `ExceededLimit *Limit` field

6. **executor/executor.go**
   - Replace `MaxIterations` with `Limits` system
   - Add `LimitType`, `Limit` types
   - Add `DefaultLimits()` helper
   - Add limit checking after each iteration
   - Update termination to set `ExceededLimit` on result

7. **executor/limit.go** (new file)
   - Define `LimitType`, `Limit` types
   - Implement `checkLimits`, `checkExactKeyLimit`, `checkPrefixLimit`
   - Implement `DefaultLimits()`

8. **agents/react/agent.go**
   - Add format parse error tracing
   - Feed parse errors back as observations
   - Reset consecutive counter on success

9. **toolchain/yaml.go** and **toolchain/json.go**
   - Add toolchain parse error tracing
   - Reset consecutive counter on success

10. **models/*.go**
    - Verify Trace calls include all required fields for auto-aggregation
