# Limit Exceeded Design: Context-Based Cancellation

## Overview

This design moves limit checking into `ExecutionContext` and uses Go's native `context.Context`
cancellation to propagate limit exceeded signals through the entire execution tree, including
nested child contexts and ongoing model calls.

## Naming Decision

For `ExecutionResult`, the final output field will be named `Output`:
```go
result := execCtx.Result()
result.Output  // the value returned by the agent on successful termination
```

Alternatives considered: `Value`, `Answer`, `Outcome`, `Response`, `Result` (redundant with struct name).

---

## API Changes

### 1. ExecutionContext Creation

**Before:**
```go
execCtx := gent.NewExecutionContext("main", data)
```

**After:**
```go
execCtx := gent.NewExecutionContext(ctx, "main", data)
execCtx.SetLimits(limits)  // optional, has defaults
```

### 2. Executor.Execute Signature

**Before:**
```go
func (e *Executor[Data]) Execute(ctx context.Context, data Data) (*ExecutionResult, error)
```

**After:**
```go
func (e *Executor[Data]) Execute(execCtx *gent.ExecutionContext)
```

### 3. Result Access

**Before:**
```go
result, err := executor.Execute(ctx, data)
if err != nil { ... }
```

**After:**
```go
executor.Execute(execCtx)
result := execCtx.Result()
if result.Error != nil { ... }
```

### 4. AgentLoop.Next Signature

**Before:**
```go
Next(ctx context.Context, execCtx *ExecutionContext) (*AgentLoopResult, error)
```

**After:**
```go
Next(execCtx *ExecutionContext) (*AgentLoopResult, error)
```

### 5. Model Interface Signatures

**Before:**
```go
GenerateContent(
    ctx context.Context,
    execCtx *ExecutionContext,
    streamId string,
    streamTopicId string,
    messages []llms.MessageContent,
) (*ContentResponse, error)
```

**After:**
```go
GenerateContent(
    execCtx *ExecutionContext,
    streamId string,
    streamTopicId string,
    messages []llms.MessageContent,
) (*ContentResponse, error)
```

Implementations use `execCtx.Context()` internally for HTTP calls.

---

## Data Structures

### ExecutionContext (updated fields)

```go
type ExecutionContext struct {
    // Existing fields...

    // New: context management
    ctx    context.Context
    cancel context.CancelCauseFunc

    // New: limits
    limits        []Limit
    exceededLimit *Limit  // set when a limit is exceeded

    // New: result (populated on termination)
    result *ExecutionResult
}
```

### ExecutionResult (simplified)

```go
type ExecutionResult struct {
    // TerminationReason indicates how execution ended.
    TerminationReason TerminationReason

    // Output is the value returned by the agent on successful termination.
    // Nil if terminated due to error, limit, or cancellation.
    Output any

    // Error is the error that caused termination, if any.
    Error error

    // ExceededLimit is the limit that was exceeded, if TerminationReason is
    // TerminationLimitExceeded.
    ExceededLimit *Limit
}
```

---

## Implementation Plan

### Phase 1: ExecutionContext Changes

**File: `context.go`**

1. Add new fields to ExecutionContext struct:
   - `ctx context.Context`
   - `cancel context.CancelCauseFunc`
   - `limits []Limit`
   - `exceededLimit *Limit`
   - `result *ExecutionResult`

2. Update `NewExecutionContext`:
   ```go
   func NewExecutionContext(ctx context.Context, name string, data LoopData) *ExecutionContext {
       ctx, cancel := context.WithCancelCause(ctx)
       return &ExecutionContext{
           ctx:       ctx,
           cancel:    cancel,
           name:      name,
           data:      data,
           limits:    DefaultLimits(),
           // ... other fields
       }
   }
   ```

3. Add new methods:
   ```go
   // Context returns the context.Context for this execution.
   // Use this when calling external APIs that require context.
   func (ctx *ExecutionContext) Context() context.Context

   // SetLimits configures the limits for this execution.
   // Replaces any previously set limits including defaults.
   func (ctx *ExecutionContext) SetLimits(limits []Limit)

   // Limits returns the configured limits.
   func (ctx *ExecutionContext) Limits() []Limit

   // ExceededLimit returns the limit that was exceeded, or nil.
   func (ctx *ExecutionContext) ExceededLimit() *Limit

   // Result returns the execution result. Only valid after execution completes.
   func (ctx *ExecutionContext) Result() *ExecutionResult
   ```

4. Update `NewChild` to create child context:
   ```go
   func (ctx *ExecutionContext) NewChild(name string) *ExecutionContext {
       childCtx, childCancel := context.WithCancelCause(ctx.ctx)
       return &ExecutionContext{
           ctx:       childCtx,
           cancel:    childCancel,
           parent:    ctx,
           // ... inherit limits from parent or use own
       }
   }
   ```

5. Add internal method for limit cancellation:
   ```go
   func (ctx *ExecutionContext) cancelWithLimit(limit *Limit) {
       ctx.exceededLimit = limit
       ctx.cancel(fmt.Errorf("limit exceeded: %s > %v", limit.Key, limit.MaxValue))
   }
   ```

6. Update `SetTermination` to also populate `result`:
   ```go
   func (ctx *ExecutionContext) SetTermination(reason TerminationReason, output any, err error) {
       ctx.termReason = reason
       ctx.termResult = output
       ctx.termErr = err
       ctx.result = &ExecutionResult{
           TerminationReason: reason,
           Output:            output,
           Error:             err,
           ExceededLimit:     ctx.exceededLimit,
       }
   }
   ```

### Phase 2: Limit Checking in Stats

**File: `stats.go`**

1. Add reference to parent ExecutionContext in stats:
   ```go
   type ExecutionStats struct {
       // ... existing fields
       execCtx *ExecutionContext  // back-reference for limit checking
   }
   ```

2. Update stat modification methods to check limits:
   ```go
   func (s *ExecutionStats) IncrCounter(key string, delta int64) {
       s.mu.Lock()
       s.counters[key] += delta
       s.mu.Unlock()

       // Propagate to parent
       if s.parent != nil {
           s.parent.IncrCounter(key, delta)
       }

       // Check limits on root context
       if s.execCtx != nil {
           s.execCtx.checkLimitsIfRoot()
       }
   }
   ```

**File: `context.go`**

3. Add limit checking method:
   ```go
   func (ctx *ExecutionContext) checkLimitsIfRoot() {
       // Only check on root context (has aggregated stats)
       if ctx.parent != nil {
           return
       }

       // Already exceeded, don't check again
       if ctx.exceededLimit != nil {
           return
       }

       // Check all limits
       if limit := ctx.evaluateLimits(); limit != nil {
           ctx.cancelWithLimit(limit)
       }
   }

   func (ctx *ExecutionContext) evaluateLimits() *Limit {
       stats := ctx.Stats()
       for i := range ctx.limits {
           limit := &ctx.limits[i]
           if ctx.isLimitExceeded(stats, limit) {
               return limit
           }
       }
       return nil
   }
   ```

### Phase 3: Executor Changes

**File: `executor/executor.go`**

1. Remove limits from Config (moved to ExecutionContext):
   ```go
   type Config struct {
       // Limits removed - now on ExecutionContext
   }
   ```

2. Update Execute signature and implementation:
   ```go
   func (e *Executor[Data]) Execute(execCtx *gent.ExecutionContext) {
       // ... setup ...

       for {
           // Check context cancellation (handles both user cancel and limit exceeded)
           if execCtx.Context().Err() != nil {
               if execCtx.ExceededLimit() != nil {
                   execCtx.SetTermination(
                       gent.TerminationLimitExceeded,
                       nil,
                       execCtx.Context().Err(),
                   )
               } else {
                   execCtx.SetTermination(
                       gent.TerminationContextCanceled,
                       nil,
                       execCtx.Context().Err(),
                   )
               }
               return
           }

           // ... rest of loop, using execCtx.Context() where needed ...

           loopResult, loopErr := e.loop.Next(execCtx)

           // Handle errors - check if it was due to limit
           if loopErr != nil {
               if execCtx.ExceededLimit() != nil {
                   execCtx.SetTermination(gent.TerminationLimitExceeded, nil, loopErr)
               } else {
                   execCtx.SetTermination(gent.TerminationError, nil, loopErr)
               }
               return
           }

           // ... continue ...
       }
   }
   ```

3. Remove ExecuteWithContext or update similarly.

4. Remove checkLimits, checkExactKeyLimit, checkPrefixLimit methods (moved to context).

### Phase 4: AgentLoop Interface

**File: `loop.go`**

```go
type AgentLoop[Data LoopData] interface {
    Next(execCtx *ExecutionContext) (*AgentLoopResult, error)
}
```

### Phase 5: Model Interface

**File: `model.go`**

```go
type Model interface {
    GenerateContent(
        execCtx *ExecutionContext,
        streamId string,
        streamTopicId string,
        messages []llms.MessageContent,
    ) (*ContentResponse, error)
}

type StreamingModel interface {
    Model
    GenerateContentStream(
        execCtx *ExecutionContext,
        streamId string,
        streamTopicId string,
        messages []llms.MessageContent,
    ) (*ContentStream, error)
}
```

### Phase 6: Update All Implementations

**Files to update:**

1. `agents/react/agent.go` - Update Next() and callModel() signatures
2. `models/lcg.go` - Update GenerateContent(), use `execCtx.Context()` for HTTP calls
3. Any other AgentLoop implementations
4. Any other Model implementations

### Phase 7: Update Tests

1. Update all test mocks to match new signatures
2. Add tests for limit checking via context cancellation:
   - Test that limit exceeded during model call cancels the call
   - Test that child context cancellation doesn't affect parent
   - Test that parent cancellation propagates to children
   - Test Result() returns correct ExceededLimit

---

## Test Cases

### Unit Tests

```go
func TestExecutionContext_LimitExceeded_CancelsContext(t *testing.T) {
    ctx := context.Background()
    execCtx := gent.NewExecutionContext(ctx, "test", &mockData{})
    execCtx.SetLimits([]gent.Limit{
        {Type: gent.LimitExactKey, Key: "test_counter", MaxValue: 5},
    })

    // Increment counter past limit
    for i := 0; i < 10; i++ {
        execCtx.Stats().IncrCounter("test_counter", 1)
    }

    // Context should be cancelled
    assert.Error(t, execCtx.Context().Err())
    assert.NotNil(t, execCtx.ExceededLimit())
    assert.Equal(t, "test_counter", execCtx.ExceededLimit().Key)
}

func TestExecutionContext_ChildCancellation_PropagatesToChildren(t *testing.T) {
    ctx := context.Background()
    parent := gent.NewExecutionContext(ctx, "parent", &mockData{})
    child := parent.NewChild("child")

    // Cancel parent
    parent.cancel(errors.New("test"))

    // Child should also be cancelled
    assert.Error(t, child.Context().Err())
}

func TestExecutor_LimitExceeded_SetsProperResult(t *testing.T) {
    ctx := context.Background()
    execCtx := gent.NewExecutionContext(ctx, "test", &mockData{})
    execCtx.SetLimits([]gent.Limit{
        {Type: gent.LimitExactKey, Key: gent.KeyIterations, MaxValue: 2},
    })

    executor := executor.New(mockLoop, executor.Config{})
    executor.Execute(execCtx)

    result := execCtx.Result()
    assert.Equal(t, gent.TerminationLimitExceeded, result.TerminationReason)
    assert.NotNil(t, result.ExceededLimit)
    assert.NotNil(t, result.Error)
}
```

### Integration Tests

```go
func TestNestedExecutor_ParentLimitExceeded_CancelsChild(t *testing.T) {
    // Parent has token limit
    // Child agent makes model calls
    // When parent token limit exceeded, child's model call should be cancelled
}
```

---

## Comprehensive Limit Testing Plan

This is one of the most critical features of the library. Users must be confident that limits
are enforced correctly regardless of how complex their agent topology is.

### Test Location

All limit-related tests will be in `executor/executor_limit_test.go`.

### Test Infrastructure

#### Mock Components

```go
// mockModel simulates a model that:
// - Increments token counters on each call
// - Can be configured to block until context cancelled
// - Tracks how many times it was called
type mockModel struct {
    tokensPerCall    int64
    callCount        atomic.Int64
    blockUntilCancel bool
    callStarted      chan struct{} // signals when call begins (for synchronization)
}

func (m *mockModel) GenerateContent(
    execCtx *gent.ExecutionContext,
    streamId, streamTopicId string,
    messages []llms.MessageContent,
) (*gent.ContentResponse, error) {
    m.callCount.Add(1)
    if m.callStarted != nil {
        m.callStarted <- struct{}{}
    }

    // Simulate token usage
    execCtx.Stats().IncrCounter("tokens:input", m.tokensPerCall)
    execCtx.Stats().IncrCounter("tokens:output", m.tokensPerCall)

    if m.blockUntilCancel {
        <-execCtx.Context().Done()
        return nil, execCtx.Context().Err()
    }

    return &gent.ContentResponse{Content: "response"}, nil
}

// mockAgentLoop that can:
// - Make multiple model calls per iteration
// - Spawn child agent loops (parallel or serial)
// - Terminate after N iterations
// - Increment custom counters/gauges
type mockAgentLoop struct {
    model             gent.Model
    modelCallsPerIter int
    maxIterations     int
    childLoops        []childLoopConfig
    customCounters    map[string]int64  // incremented each iteration
    customGauges      map[string]float64 // set each iteration
}

type childLoopConfig struct {
    loop     gent.AgentLoop
    parallel bool  // if true, run in goroutine
}
```

### Test Matrix

#### Dimension 1: AgentLoop Topology

| ID | Topology | Description |
|----|----------|-------------|
| T1 | Single | One AgentLoop, one model call per iteration |
| T2 | SingleMultiModel | One AgentLoop, 3 model calls per iteration |
| T3 | ParallelChildren | Parent spawns 3 child AgentLoops in parallel |
| T4 | SerialChildren | Parent executes 2 child AgentLoops serially per iteration |
| T5 | Mixed | Parent with 2 parallel children, each with serial sub-children |

#### Dimension 2: When Limit Exceeded

| ID | Timing | Description |
|----|--------|-------------|
| W1 | MidFirstIteration | Limit exceeded during first iteration (before it completes) |
| W2 | BetweenIterations | Limit exceeded exactly at iteration boundary |
| W3 | MidNthIteration | Limit exceeded during iteration N (N > 1) |
| W4 | InChildContext | Limit exceeded in child context, propagates to parent |
| W5 | InParallelChild | Limit exceeded in one parallel child, others should cancel |

#### Dimension 3: Limit Type

| ID | Type | Key Pattern |
|----|------|-------------|
| L1 | CounterExact | Exact counter key (e.g., `tokens:total`) |
| L2 | CounterPrefix | Prefix match (e.g., `tokens:` matches `tokens:input`, `tokens:output`) |
| L3 | GaugeExact | Exact gauge key (e.g., `memory:peak_mb`) |
| L4 | GaugePrefix | Prefix match for gauges |
| L5 | Iterations | Built-in iteration counter |
| L6 | ConsecutiveErrors | Built-in consecutive error counters |

### Test Cases

#### Group 1: Single AgentLoop Tests

```go
// T1 + W1 + L1: Single loop, mid-first-iteration, counter exact
func TestLimit_Single_MidFirstIter_CounterExact(t *testing.T) {
    // Setup: limit of 100 tokens, model uses 60 tokens per call, 2 calls per iter
    // Expected: Second model call triggers limit, context cancelled, proper result
}

// T1 + W3 + L1: Single loop, mid-Nth-iteration, counter exact
func TestLimit_Single_MidNthIter_CounterExact(t *testing.T) {
    // Setup: limit of 500 tokens, model uses 100 tokens per call
    // Expected: Limit hit during iteration 3, iterations 1-2 complete successfully
}

// T2 + W1 + L1: Single loop with multiple model calls, first iteration
func TestLimit_SingleMultiModel_MidFirstIter_CounterExact(t *testing.T) {
    // Setup: 3 model calls per iteration, limit hit on 2nd call
    // Expected: 1st call succeeds, 2nd call triggers limit, 3rd call never starts
}

// T1 + W2 + L5: Single loop, iteration limit
func TestLimit_Single_IterationLimit(t *testing.T) {
    // Setup: iteration limit of 5
    // Expected: 5 iterations complete, 6th never starts
}

// T1 + L3: Single loop, gauge limit
func TestLimit_Single_GaugeExact(t *testing.T) {
    // Setup: gauge limit (e.g., memory), loop sets gauge each iteration
    // Expected: When gauge exceeds limit, execution stops
}

// T1 + L2: Single loop, counter prefix limit
func TestLimit_Single_CounterPrefix(t *testing.T) {
    // Setup: limit on "tokens:" prefix, model increments tokens:input and tokens:output
    // Expected: Aggregated token count triggers limit
}
```

#### Group 2: Parallel Children Tests

```go
// T3 + W4 + L1: Parallel children, limit in child
func TestLimit_ParallelChildren_LimitInChild_CounterExact(t *testing.T) {
    // Setup: Parent spawns 3 children in parallel, each incrementing tokens
    // Child A: 50 tokens/iter, 10 iterations
    // Child B: 100 tokens/iter, 5 iterations  <- will hit limit first
    // Child C: 30 tokens/iter, 15 iterations
    // Limit: 400 tokens total
    // Expected:
    //   - Child B hits limit around iteration 4
    //   - Parent context cancelled
    //   - Children A and C also cancelled (may be mid-iteration)
    //   - Parent result shows TerminationLimitExceeded
}

// T3 + W5 + L1: Parallel children, one hits limit, others cancel
func TestLimit_ParallelChildren_OneCancelsOthers(t *testing.T) {
    // Setup: 3 parallel children, one blocks on model call
    // Expected: When limit exceeded, blocked model call is cancelled
}

// T3 + L1: Parallel children, aggregated stats trigger limit
func TestLimit_ParallelChildren_AggregatedStats(t *testing.T) {
    // Setup: Each child uses 100 tokens, limit is 250
    // Expected: After ~2.5 children worth of tokens, limit triggers
    // Verifies that child stats are properly aggregated to parent
}
```

#### Group 3: Serial Children Tests

```go
// T4 + W4 + L1: Serial children, limit in second child
func TestLimit_SerialChildren_LimitInSecondChild(t *testing.T) {
    // Setup: Parent runs Child1 then Child2 each iteration
    // Child1: 100 tokens
    // Child2: 100 tokens
    // Limit: 350 tokens
    // Expected:
    //   - Iteration 1: Child1 (100) + Child2 (100) = 200 total
    //   - Iteration 2: Child1 (100) = 300 total, Child2 triggers limit at 400
    //   - Parent gets TerminationLimitExceeded
}

// T4 + L1: Serial children, first child exhausts limit
func TestLimit_SerialChildren_FirstChildExhausts(t *testing.T) {
    // Setup: First child uses most of the budget
    // Expected: Second child never starts or is immediately cancelled
}
```

#### Group 4: Mixed Topology Tests

```go
// T5: Complex nested topology
func TestLimit_MixedTopology_DeepNesting(t *testing.T) {
    // Setup:
    //   Parent
    //   ├── Child A (parallel)
    //   │   ├── Grandchild A1 (serial)
    //   │   └── Grandchild A2 (serial)
    //   └── Child B (parallel)
    //       ├── Grandchild B1 (serial)
    //       └── Grandchild B2 (serial)
    // Each grandchild increments tokens
    // Limit on parent
    // Expected: All contexts cancelled when any grandchild triggers limit
}
```

#### Group 5: Edge Cases

```go
// Limit exactly at boundary
func TestLimit_ExactBoundary(t *testing.T) {
    // Setup: limit 100, increment by exactly 100
    // Expected: 100 is allowed, 101 triggers (or exactly at boundary based on > vs >=)
}

// Multiple limits, first one wins
func TestLimit_MultipleLimit_FirstWins(t *testing.T) {
    // Setup: iteration limit 10, token limit 500
    // Expected: Whichever is hit first is reported in ExceededLimit
}

// Zero/negative limits
func TestLimit_ZeroLimit(t *testing.T) {
    // Setup: limit of 0
    // Expected: First stat update triggers limit
}

// Limit check doesn't double-cancel
func TestLimit_NoDuplicateCancel(t *testing.T) {
    // Setup: After limit exceeded, more stat updates occur
    // Expected: cancel() only called once, no panics
}

// Parent cancelled (not limit), children handle gracefully
func TestLimit_ParentCancelledNotLimit(t *testing.T) {
    // Setup: Parent context cancelled externally (e.g., timeout)
    // Expected: TerminationContextCanceled (not LimitExceeded), ExceededLimit is nil
}

// Result is correct after limit exceeded
func TestLimit_ResultCorrect(t *testing.T) {
    // Verify all fields of ExecutionResult are set correctly:
    // - TerminationReason == TerminationLimitExceeded
    // - ExceededLimit points to correct Limit
    // - Error is not nil and contains useful info
    // - Output is nil (no successful termination)
}
```

#### Group 6: Consecutive Error Limits

```go
// Format parse error consecutive limit
func TestLimit_ConsecutiveFormatErrors(t *testing.T) {
    // Setup: Model returns unparseable output, consecutive error limit is 3
    // Expected: After 3 consecutive parse errors, limit exceeded
}

// Consecutive errors reset on success
func TestLimit_ConsecutiveErrors_ResetOnSuccess(t *testing.T) {
    // Setup: 2 errors, 1 success, 2 errors, 1 success...
    // Expected: Never hits consecutive limit of 3
}

// Toolchain parse error limit
func TestLimit_ConsecutiveToolchainErrors(t *testing.T) {
    // Similar to format errors but for toolchain parsing
}
```

#### Group 7: Gauge-Specific Tests

```go
// Gauge increases and decreases
func TestLimit_Gauge_IncreasesAndDecreases(t *testing.T) {
    // Setup: Gauge fluctuates, limit is peak value
    // Expected: Limit triggers when peak exceeded, even if later decreased
}

// Gauge set (not increment)
func TestLimit_Gauge_SetValue(t *testing.T) {
    // Setup: Gauge set directly (not incremented)
    // Expected: Limit checked on SetGauge
}
```

### Test Assertions Checklist

For each test, verify:

1. **Termination Reason**: `result.TerminationReason == TerminationLimitExceeded`
2. **Exceeded Limit**: `result.ExceededLimit` points to correct limit
3. **Error Present**: `result.Error != nil`
4. **Output Nil**: `result.Output == nil` (didn't terminate successfully)
5. **Context Cancelled**: `execCtx.Context().Err() != nil`
6. **Stats Accurate**: Final stats reflect actual work done before cancellation
7. **No Goroutine Leaks**: All child goroutines cleaned up
8. **Model Call Count**: Verify expected number of model calls made

### Synchronization Helpers

```go
// waitForModelCall waits for a model call to start, with timeout
func waitForModelCall(t *testing.T, m *mockModel, timeout time.Duration) {
    select {
    case <-m.callStarted:
        return
    case <-time.After(timeout):
        t.Fatal("timeout waiting for model call to start")
    }
}

// runParallelChildren starts N child executors in goroutines, returns wait function
func runParallelChildren(
    execCtx *gent.ExecutionContext,
    children []gent.AgentLoop,
) func() {
    var wg sync.WaitGroup
    for _, child := range children {
        wg.Add(1)
        go func(loop gent.AgentLoop) {
            defer wg.Done()
            childCtx := execCtx.NewChild("child")
            exec := executor.New(loop, executor.Config{})
            exec.Execute(childCtx)
        }(child)
    }
    return wg.Wait
}
```

---

## Phase 8: Documentation Updates

### Files to Update

| File | Updates Required |
|------|------------------|
| `README.md` | Update API examples, add limits section |
| `TRACE.md` | Update ExecutionContext API, trace examples |
| `STREAMING-EXECUTOR.md` | Update Execute signature examples |
| `STREAMING-INITIAL.md` | Update any code examples |
| Any other `.md` files | Search for old API patterns |

### Documentation Checklist

1. **API Changes**
   - [ ] Update all `NewExecutionContext` calls to include `ctx` parameter
   - [ ] Update all `Execute(ctx, data)` to `Execute(execCtx)`
   - [ ] Update all `Next(ctx, execCtx)` to `Next(execCtx)`
   - [ ] Update all `GenerateContent(ctx, execCtx, ...)` to `GenerateContent(execCtx, ...)`
   - [ ] Update result access from `result, err := ...` to `execCtx.Result()`

2. **New Concepts**
   - [ ] Document `SetLimits` and `DefaultLimits()`
   - [ ] Document limit types (ExactKey, KeyPrefix)
   - [ ] Document how limits propagate in nested contexts
   - [ ] Document `ExceededLimit()` and when it's set

3. **Examples**
   - [ ] Add example: basic execution with limits
   - [ ] Add example: custom limits
   - [ ] Add example: nested agents with shared limits
   - [ ] Add example: handling limit exceeded

4. **Migration Notes**
   - [ ] Document breaking changes clearly
   - [ ] Provide before/after code snippets
   - [ ] List all affected interfaces

### Documentation Search Patterns

Run these to find all places needing updates:

```bash
# Find old NewExecutionContext calls
grep -r "NewExecutionContext(" --include="*.md" --include="*.go"

# Find old Execute signatures
grep -r "Execute(ctx" --include="*.md" --include="*.go"

# Find old Next signatures
grep -r "Next(ctx context.Context" --include="*.md" --include="*.go"

# Find old GenerateContent signatures
grep -r "GenerateContent(ctx context.Context" --include="*.md" --include="*.go"

# Find ExecutionResult usage
grep -r "ExecutionResult" --include="*.md" --include="*.go"
```

---

## Migration Guide

### For Executor Users

**Before:**
```go
result, err := executor.Execute(ctx, data)
if err != nil {
    log.Error(err)
}
fmt.Println(result.Result)
```

**After:**
```go
execCtx := gent.NewExecutionContext(ctx, "main", data)
// Optional: customize limits
execCtx.SetLimits(customLimits)

executor.Execute(execCtx)

result := execCtx.Result()
if result.Error != nil {
    if result.ExceededLimit != nil {
        log.Warnf("Limit exceeded: %s", result.ExceededLimit.Key)
    } else {
        log.Error(result.Error)
    }
}
fmt.Println(result.Output)
```

### For AgentLoop Implementors

**Before:**
```go
func (a *MyAgent) Next(ctx context.Context, execCtx *gent.ExecutionContext) (*gent.AgentLoopResult, error) {
    // Use ctx for cancellation
    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    default:
    }
    // ...
}
```

**After:**
```go
func (a *MyAgent) Next(execCtx *gent.ExecutionContext) (*gent.AgentLoopResult, error) {
    // Use execCtx.Context() for cancellation
    select {
    case <-execCtx.Context().Done():
        return nil, execCtx.Context().Err()
    default:
    }
    // ...
}
```

### For Model Implementors

**Before:**
```go
func (m *MyModel) GenerateContent(
    ctx context.Context,
    execCtx *gent.ExecutionContext,
    // ...
) (*gent.ContentResponse, error) {
    resp, err := m.client.Call(ctx, req)
    // ...
}
```

**After:**
```go
func (m *MyModel) GenerateContent(
    execCtx *gent.ExecutionContext,
    // ...
) (*gent.ContentResponse, error) {
    resp, err := m.client.Call(execCtx.Context(), req)
    // ...
}
```

---

## File Change Summary

| File | Changes |
|------|---------|
| `context.go` | Add ctx/cancel fields, SetLimits, Result(), limit checking |
| `stats.go` | Add execCtx back-reference, trigger limit checks on updates |
| `limit.go` | Move DefaultLimits, keep Limit types |
| `loop.go` | Update AgentLoop.Next signature |
| `model.go` | Update Model/StreamingModel signatures |
| `trace.go` | Update ExecutionResult struct |
| `executor/executor.go` | Remove limits from config, update Execute signature |
| `agents/react/agent.go` | Update Next and model call signatures |
| `models/lcg.go` | Update signatures, use execCtx.Context() |
| `*_test.go` | Update all test mocks and assertions |

---

## Open Questions

1. **Should child contexts inherit parent limits or have their own?**
   - Proposal: Children inherit parent limits by default, can override with SetLimits.
   - Parent's aggregated stats are what matter for token limits, so parent's limits apply.

2. **Should limit checking be synchronous or batched?**
   - Proposal: Synchronous (check on every stat update) for simplicity.
   - Can optimize later if performance is an issue.

3. **Should we keep ExecuteWithContext?**
   - Proposal: Remove it. The new Execute(execCtx) handles all cases.
   - For nested agents, create a child context and pass it.

---

## Implementation Order

The phases must be implemented in order due to dependencies:

```
Phase 1: ExecutionContext Changes
    │
    ├──► Phase 2: Limit Checking in Stats (depends on Phase 1)
    │
    └──► Phase 3: Executor Changes (depends on Phase 1)
              │
              ├──► Phase 4: AgentLoop Interface (depends on Phase 3)
              │         │
              │         └──► Phase 6a: Update react/agent.go
              │
              └──► Phase 5: Model Interface (depends on Phase 3)
                        │
                        └──► Phase 6b: Update models/lcg.go

Phase 7: Update Tests (after all implementation phases)
    │
    └──► Phase 8: Documentation Updates (after tests pass)
```

### Suggested Implementation Steps

1. **Step 1**: Implement Phase 1 (context.go changes)
   - Run existing tests (many will fail due to signature changes)

2. **Step 2**: Implement Phase 2 (stats.go limit checking)
   - Add unit tests for limit checking in isolation

3. **Step 3**: Implement Phase 3 (executor changes)
   - Update executor, tests will still fail

4. **Step 4**: Implement Phase 4 + Phase 6a (AgentLoop + react agent)
   - Update interface and implementation together

5. **Step 5**: Implement Phase 5 + Phase 6b (Model + LCG wrapper)
   - Update interface and implementation together

6. **Step 6**: Phase 7 - Fix all existing tests
   - Update mocks, assertions, test setup

7. **Step 7**: Phase 7 - Add comprehensive limit tests
   - Implement the test matrix from this document

8. **Step 8**: Documentation updates
   - Update all markdown files
   - Verify examples compile

### Verification Commands

After each step:
```bash
# Check for compile errors
go build ./...

# Run tests
go test ./...

# Check for outdated documentation patterns
grep -r "Execute(ctx" --include="*.md"
grep -r "NewExecutionContext(\"" --include="*.md"
```
