# Agent Executor Limit Test Standards

This document defines the testing standards for agent executor limit tests. These standards ensure comprehensive coverage of limit behavior and can be applied to any agent implementation.

## Overview

Executor limits control resource usage during agent execution. Each limit type must be thoroughly tested to ensure:
- Limits trigger at the correct threshold
- Limit events are published correctly
- Stats are tracked accurately
- Child context limits propagate to parents
- Prefix-based limits scope correctly

## Required Test Scenarios

**For EACH STAT/LIMIT TYPE**, the following test scenarios **MUST** exist:

### 1. Limit Exceeded at First Iteration

**Purpose:** Verify limit triggers correctly on the boundary case where the first iteration exceeds the limit.

**Setup:**
- Configure limit with low threshold
- First iteration produces value that exceeds limit

**Assertions:**
- Termination reason is `TerminationLimitExceeded`
- `ExceededLimit` matches the configured limit
- Iteration count is 1
- `LimitExceededEvent` is published at iteration 1

### 2. Limit Exceeded at Nth Iteration

**Purpose:** Verify limit triggers correctly after multiple successful iterations.

**Setup:**
- Configure limit with threshold that allows N-1 successful iterations
- Nth iteration exceeds the cumulative/consecutive threshold

**Assertions:**
- Termination reason is `TerminationLimitExceeded`
- `ExceededLimit` matches the configured limit
- Iteration count is N
- Events show N-1 successful iterations followed by limit exceeded at iteration N

### 3. Consecutive Counter Reset (Consecutive Limits Only)

**Purpose:** Verify consecutive counters reset on success, preventing false limit triggers.

**Setup:**
- Configure consecutive limit (e.g., `KeyFormatParseErrorConsecutive`)
- Alternate between error and success iterations
- Total errors exceed limit value, but never consecutively

**Assertions:**
- Termination reason is `TerminationSuccess` (limit NOT exceeded)
- No `LimitExceededEvent` published
- Total error count in stats exceeds limit value
- Consecutive counter never exceeds limit

### 4. Child Context Propagation

**Purpose:** Verify limits exceeded in child contexts propagate to parent.

**Setup:**
- Parent spawns child execution context
- Child performs action that exceeds limit (e.g., model call with high token count)
- Limit is configured on parent

**Assertions:**
- Parent termination reason is `TerminationLimitExceeded`
- `LimitExceededEvent` published on parent context
- Child events show the action that triggered the limit
- Parent stats include aggregated child stats

### 5. Prefix Limit Scoping (Prefix Limits Only)

**Purpose:** Verify prefix limits only trigger on matching keys, not sibling keys with same prefix.

**Setup:**
- At least 2 different suffixes on the same prefix stat
  - Example: `gent:tool_calls:search` and `gent:tool_calls:reschedule`
- Limit placed on only ONE suffix (e.g., `search` with max 2)
- Non-limited suffix exceeds the limit value (e.g., `reschedule` called 5 times)
- Limited suffix then exceeds its limit (e.g., `search` called 3 times)

**Assertions:**
- Execution continues while non-limited suffix exceeds value
- Termination occurs only when limited suffix exceeds
- `ExceededLimit.Key` matches the limited suffix
- Stats show both suffixes with their respective counts

## Global Tests (One-Off)

These tests verify cross-cutting behavior and only need to exist once, not per-stat:

### 6. Multiple Limits Race

**Purpose:** Verify behavior when multiple limits are exceeded simultaneously.

**Setup:**
- Configure two limits that would both be exceeded in the same iteration
- Example: Input token limit and output token limit both exceeded on same model call

**Assertions:**
- Termination reason is `TerminationLimitExceeded`
- First limit (by check order) is reported as the exceeded limit
- Only one `LimitExceededEvent` is published

### 7. Deep Propagation (Sanity Check)

**Purpose:** Verify limit propagation works through multiple context levels.

**Setup:**
- Parent → Child → Grandchild context hierarchy
- Grandchild action exceeds limit

**Assertions:**
- Limit exceeded propagates to root parent
- All intermediate contexts reflect cancelled state
- Events trace the propagation path

## Assertion Standards

### Rigorous Comparison Requirements

All assertions MUST follow these rigorous standards:

**1. Full Match, Not Partial**
- Compare entire values, never substrings or partial matches
- Use `assert.Equal()` for exact comparison, not `assert.Contains()`

```go
// CORRECT: Full string comparison
assert.Equal(t, "expected error message", err.Error())

// WRONG: Partial match
assert.Contains(t, err.Error(), "error")
```

**2. Complete Struct Comparison**
- Compare entire structs, not just individual fields
- If a struct has 5 fields, all 5 must be verified

```go
// CORRECT: Full struct comparison
assert.Equal(t, expectedLimit, *execCtx.ExceededLimit())

// WRONG: Only checking some fields
assert.Equal(t, expectedKey, execCtx.ExceededLimit().Key)
// Missing: Type, MaxValue fields not verified
```

**3. Timestamp Assertions**
- Assert timestamps are `>=` expected time, never just "not zero"
- Timestamps must be monotonically non-decreasing in event sequences

```go
// CORRECT: Meaningful timestamp assertion
assert.True(t, event.Timestamp.After(startTime) || event.Timestamp.Equal(startTime))
assert.True(t, event.Timestamp.After(prevEvent.Timestamp) ||
             event.Timestamp.Equal(prevEvent.Timestamp))

// WRONG: Vanity assertion
assert.False(t, event.Timestamp.IsZero()) // Only proves it was set, not correct
```

**4. NextPrompt Always Verified**
- `AfterIterationEvent.Result.NextPrompt` MUST be verified with actual expected content
- Never use empty NextPrompt in expected events
- Use `tt.ContinueWithPrompt()` with the exact expected observation string

```go
// CORRECT: Full NextPrompt verification
toolObs := tt.ToolObservation(format, toolChain, "search", "tool executed")
tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolObs))

// WRONG: Empty NextPrompt (no verification of what agent actually returns)
tt.AfterIter(0, 1, tt.Continue()) // Continue() should not exist
```

**5. Complete Event Sequence**
- Assert the ENTIRE event sequence, not just counts
- Event order matters and must be verified
- Missing or extra events indicate bugs

```go
// CORRECT: Full sequence with all events
expectedEvents := []gent.Event{
    tt.BeforeExec(0, 0),
    tt.BeforeIter(0, 1),
    tt.BeforeModelCall(0, 1, "model"),
    tt.AfterModelCall(0, 1, "model", 100, 50),
    tt.BeforeToolCall(0, 1, "search", nil),
    tt.AfterToolCall(0, 1, "search", nil, "result", nil),
    tt.AfterIter(0, 1, tt.ContinueWithPrompt(expectedObs)),
    tt.AfterExec(0, 1, gent.TerminationSuccess),
}
tt.AssertEventsEqual(t, expectedEvents, actualEvents)

// WRONG: Only counting events
assert.Equal(t, 8, len(actualEvents)) // Doesn't verify content or order
```

**6. No Vanity Assertions**
- Every assertion must verify real functionality
- Assertions like "not nil", "not empty", "not zero" alone are insufficient
- If something should be non-nil, assert its actual expected value

```go
// CORRECT: Verifies actual value
assert.Equal(t, gent.KeyIterations, execCtx.ExceededLimit().Key)

// WRONG: Vanity assertion (only proves it exists)
assert.NotNil(t, execCtx.ExceededLimit())
```

### ExecutionContext Assertions

Every limit test MUST assert the following:

```go
// Result assertions
assert.Equal(t, expectedTerminationReason, execCtx.TerminationReason())
assert.Equal(t, expectedExceededLimit, execCtx.ExceededLimit())

// Iteration count
assert.Equal(t, expectedIteration, execCtx.Iteration())

// Full stats comparison
expectedStats := ... // Build expected stats
assert.Equal(t, expectedStats, execCtx.Stats())

// Full event sequence comparison
expectedEvents := []gent.Event{
    tt.BeforeExec(0, 0),
    tt.BeforeIter(0, 1),
    // ... complete event sequence
    tt.AfterExec(0, N, expectedTerminationReason),
}
tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
```

### Child Context Assertions

For tests involving child contexts:

```go
// Verify child count
require.Len(t, execCtx.Children(), expectedChildCount)

// For each child, assert:
for i, child := range execCtx.Children() {
    // Result
    assert.Equal(t, expectedChildTermination[i], child.TerminationReason())

    // Iteration (if applicable)
    assert.Equal(t, expectedChildIteration[i], child.Iteration())

    // Stats
    assert.Equal(t, expectedChildStats[i], child.Stats())

    // Full event sequence
    tt.AssertEventsEqual(t, expectedChildEvents[i], tt.CollectLifecycleEvents(child))
}
```

### Event Assertion Requirements

- Events MUST be asserted as complete sequences, not counts
- Use `tt.AssertEventsEqual()` for full comparison
- `AfterIterationEvent.Result` MUST include `NextPrompt` verification
- Use `tt.ContinueWithPrompt()` or `tt.Terminate()`, never empty `NextPrompt`

## Code Organization

### File Naming

```
agents/<agent_name>/agent_executor_limits_test.go
```

### Test Function Naming

```go
// Pattern: TestExecutorLimits_<StatName>
func TestExecutorLimits_Iterations(t *testing.T) { ... }
func TestExecutorLimits_InputTokens(t *testing.T) { ... }
func TestExecutorLimits_ToolCalls(t *testing.T) { ... }
func TestExecutorLimits_FormatParseErrorTotal(t *testing.T) { ... }
func TestExecutorLimits_FormatParseErrorConsecutive(t *testing.T) { ... }

// For consecutive reset tests
func TestExecutorLimits_ConsecutiveReset_FormatParseError(t *testing.T) { ... }

// For prefix limit tests
func TestExecutorLimits_ToolCallsForTool(t *testing.T) { ... }
```

### Subtest Naming

```go
t.Run("stops when limit exceeded at first iteration", func(t *testing.T) { ... })
t.Run("stops when limit exceeded at Nth iteration", func(t *testing.T) { ... })
t.Run("consecutive counter resets on success", func(t *testing.T) { ... })
t.Run("child context limit propagates to parent", func(t *testing.T) { ... })
t.Run("prefix limit only triggers on matching suffix", func(t *testing.T) { ... })
```

## Helper Functions

Use the `tt` package helpers for building expected events and results:

```go
// Event builders
tt.BeforeExec(depth, iteration)
tt.AfterExec(depth, iteration, terminationReason)
tt.BeforeIter(depth, iteration)
tt.AfterIter(depth, iteration, result)
tt.BeforeModelCall(depth, iteration, model)
tt.AfterModelCall(depth, iteration, model, inputTokens, outputTokens)
tt.BeforeToolCall(depth, iteration, toolName, args)
tt.AfterToolCall(depth, iteration, toolName, args, output, err)
tt.LimitExceeded(depth, iteration, limit, currentValue, matchedKey)
tt.ParseError(depth, iteration, errorType, rawContent)

// Result builders
tt.ContinueWithPrompt(nextPrompt)
tt.Terminate(text)

// Observation builders (for NextPrompt)
tt.ToolObservation(format, toolChain, toolName, output)
tt.FormatParseErrorObservation(format, err, rawResponse)
tt.ToolchainErrorObservation(format, err)
tt.TerminationParseErrorObservation(format, err, content)
tt.ValidatorFeedbackObservation(format, feedbackSections...)

// Limit builders
tt.ExactLimit(key, maxValue)
tt.PrefixLimit(key, maxValue)
```

## Special Behavior: Iterations

The `gent:iterations` stat has unique behavior that differs from other stats:

### No Child-to-Parent Propagation

Unlike other stats, **iterations do NOT propagate from child to parent contexts**. This is intentional:

- When a parent at iteration N spawns a child agent loop, the child starts at iteration 0
- Child iterations (1, 2, 3...) stay local to the child context
- Parent iteration count remains at N, unaffected by child iterations
- This prevents child execution from corrupting parent iteration tracking

### Test Implications

When testing child context propagation with iteration limits:
- Child iteration limit tests should verify child terminates at correct iteration
- Parent should NOT see child iteration increments in its stats
- Other stats (tokens, tool calls) SHOULD propagate normally

```go
// Example: Parent iteration should not include child iterations
parentExecCtx := ... // at iteration 2
childExecCtx := parentExecCtx.SpawnChild(...)
// Child runs 3 iterations
assert.Equal(t, int64(3), childExecCtx.Stats().GetIterations())
assert.Equal(t, int64(2), parentExecCtx.Stats().GetIterations()) // NOT 5
```

## Stat Keys Reference

Standard stat keys that require limit tests:

### Cumulative Stats
- `gent:iterations` - Iteration count
- `gent:input_tokens` - Total input tokens
- `gent:output_tokens` - Total output tokens
- `gent:tool_calls` - Total tool calls
- `gent:format_parse_error_total` - Total format parse errors
- `gent:toolchain_parse_error_total` - Total toolchain parse errors
- `gent:termination_parse_error_total` - Total termination parse errors
- `gent:section_parse_error_total` - Total section parse errors
- `gent:tool_call_error_total` - Total tool call errors
- `gent:answer_rejected_total` - Total validator rejections

### Consecutive Stats
- `gent:format_parse_error_consecutive` - Consecutive format parse errors
- `gent:toolchain_parse_error_consecutive` - Consecutive toolchain parse errors
- `gent:termination_parse_error_consecutive` - Consecutive termination parse errors
- `gent:section_parse_error_consecutive` - Consecutive section parse errors
- `gent:tool_call_error_consecutive` - Consecutive tool call errors

### Prefix Stats (per-resource tracking)
- `gent:input_tokens:` - Input tokens per model
- `gent:output_tokens:` - Output tokens per model
- `gent:tool_calls:` - Calls per tool
- `gent:tool_call_error:` - Errors per tool
- `gent:tool_call_error_consecutive:` - Consecutive errors per tool
- `gent:answer_rejected:` - Rejections per validator

## Checklist for New Agent Implementation

When implementing limit tests for a new agent:

- [ ] Create `agent_executor_limits_test.go` in agent package
- [ ] For each cumulative stat:
  - [ ] Test: Limit exceeded at first iteration
  - [ ] Test: Limit exceeded at Nth iteration
  - [ ] Test: Child context propagation
- [ ] For each consecutive stat:
  - [ ] Test: Limit exceeded at first iteration
  - [ ] Test: Limit exceeded at Nth iteration
  - [ ] Test: Consecutive counter resets on success
  - [ ] Test: Child context propagation
- [ ] For each prefix stat:
  - [ ] Test: Limit exceeded at first iteration
  - [ ] Test: Limit exceeded at Nth iteration
  - [ ] Test: Prefix limit scoping (multiple suffixes)
  - [ ] Test: Child context propagation
- [ ] Global tests:
  - [ ] Test: Multiple limits race
  - [ ] Test: Deep propagation (optional)
- [ ] All assertions follow standards (full event comparison, NextPrompt verification)
- [ ] Run `go test ./agents/<agent_name>/... -v` to verify all pass
