# Executor Test Standards

This document is the canonical standard for tests around the executor, execution context,
agent-loop lifecycle, stats, and limits.

Use it for tests in `executor/`, for agent-specific limit tests such as
`agents/<agent_name>/agent_executor_limits_test.go`, and for any new framework stat that can
trigger a limit.

General repository test rules still apply: use table-driven tests when there are multiple
scenarios, define named `input` and `expected` structs inside the test function, use
`testify/assert`, compare full known values, and run tests without `-v`.

## Goals

Executor tests must prove user-visible framework behavior, not implementation trivia.

- Test through public APIs whenever possible.
- Verify lifecycle order when order is part of the contract.
- Verify exact termination reasons, errors, stats, limits, and events.
- Cover first-iteration and later-iteration boundaries for resource limits.
- Cover parent/child context propagation where stats can cross context boundaries.
- Keep public API tests focused; do not force full event sequences into unrelated tests.

## Assertion Policy

Use the strongest assertion that matches the behavior under test.

- Use exact string comparisons when the full string is known.
- Compare complete structs when the complete expected value is known.
- Do not use `Contains`, `HasPrefix`, `NotNil`, `NotEmpty`, or `NotZero` as the only proof.
- For timestamps and durations, assert `>=` an expected lower bound.
- For lifecycle and limit tests, assert the complete event sequence in order.
- For simple API tests, assert the exact API contract without unrelated lifecycle noise.

Example exact error assertion:

```go
assert.Equal(t, "AgentLoop.Next (iteration 2): boom", err.Error())
```

Example timestamp assertion:

```go
assert.True(t, event.Timestamp.After(start) || event.Timestamp.Equal(start))
```

Example complete event assertion:

```go
expectedEvents := []gent.Event{
	tt.BeforeExec(0, 0),
	tt.BeforeIter(0, 1),
	tt.AfterIter(0, 1, tt.ContinueWithPrompt(expectedPrompt)),
	tt.AfterExec(0, 1, gent.TerminationSuccess),
}
tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
```

## Executor Core Coverage

These tests belong in `executor/` unless the behavior is specific to one agent.

### Public API

Cover the public executor API directly and keep these tests focused.

- `New()` creates an executor with nil config values and default event registry behavior.
- `New()` respects a custom event registry supplied in config.
- `WithEvents()` replaces the registry, returns the same executor, and routes events there.
- `Subscribe()` adds subscribers to the current registry, returns the same executor, and chains.
- Multiple subscribers that implement the same subscriber interface all receive the event.

### Lifecycle Events

Every executor lifecycle test that runs `Execute()` must assert the complete lifecycle sequence.

- `BeforeExecutionEvent` is published before the first iteration.
- Each iteration publishes `BeforeIterationEvent` before `AgentLoop.Next()`.
- Each completed iteration publishes `AfterIterationEvent` after `AgentLoop.Next()`.
- `AfterExecutionEvent` is published exactly once after success, error, cancellation, or limit.
- `AfterExecutionEvent` contains the final `TerminationReason` and final error.
- Event order is `BeforeExecution`, repeated iteration events, then `AfterExecution`.

### Successful Termination

Cover the normal `LATerminate` path with exact result assertions.

- Execution stops when `AgentLoop.Next()` returns `LATerminate`.
- `ExecutionContext.Result()` stores the exact `AgentLoopResult.Result` value.
- `TerminationReason()` is `TerminationSuccess`.
- `Error()` is nil.
- The result iteration count matches the number of completed iterations.

### Loop Errors

Cover errors returned by `AgentLoop.Next()`.

- `AfterIterationEvent` is published before termination is set.
- The termination reason is `TerminationError`.
- The error wraps the loop error with the exact iteration number.
- The error message format is `AgentLoop.Next (iteration N): <cause>`.
- If a limit is already exceeded, limit termination takes precedence over loop-error termination.

### Context Cancellation

Cover user cancellation separately from limit cancellation.

- A canceled parent context terminates execution with `TerminationContextCanceled`.
- The stored error is the context cancellation cause or `context.Canceled`.
- Cancellation before the first iteration stops before `BeforeIterationEvent`.
- Cancellation after an iteration stops before the next iteration starts.
- Cancellation during `AgentLoop.Next()` is only observed after the loop returns.

### Stream Lifecycle

Cover stream cleanup as executor behavior, not only as stream implementation behavior.

- Streams are usable while execution is running.
- `CloseStreams()` is called exactly once when execution ends.
- Streams close on success, loop error, cancellation, limit termination, and compaction failure.
- If a test intentionally exercises panic cleanup, recover in the test and only assert deferred
  cleanup behavior. Do not imply that the executor converts panics to `TerminationError`.

### Duration Tracking

Duration tests must assert meaningful elapsed time.

- `AfterIterationEvent.Duration` is at least the measured delay in that iteration.
- Multiple iterations keep separate duration values.
- Avoid `duration > 0` as the only assertion.

### Subscribers

Subscriber tests must prove routing and ordering.

- `BeforeExecutionSubscriber` receives events before first iteration work.
- `AfterIterationSubscriber` receives events after each completed iteration.
- `AfterExecutionSubscriber` receives the final termination event.
- Subscribers registered through `WithEvents()` and `Subscribe()` both receive matching events.
- Multiple subscribers receive the same event without suppressing one another.

### Concurrency

Use race tests for behavior with shared stats, children, streams, or subscribers.

- Run `go test -race` for packages touched by concurrency changes.
- Cover concurrent child stat updates when changing stat propagation.
- Cover concurrent subscribers or stream consumers when changing event or stream dispatch.
- Do not rely on goroutine sleeps alone; use channels or `require.Eventually` with evidence.

## Limit Coverage

Limit tests belong in the agent package when the stat is produced by agent behavior. Executor-only
limit behavior belongs in `executor/`.

For each new stat or limit type, add tests that prove the stat increment/reset behavior and the
limit enforcement behavior. A stat is not fully covered until both are tested.

### Required Scenarios

For every cumulative counter limit:

- Limit exceeded at the first iteration.
- Limit exceeded at the Nth iteration after earlier iterations remain under the limit.
- Child context propagation when the counter can be produced by a child context.
- Exact stats on the context that triggered the limit and any parent that aggregates it.

For every consecutive gauge limit:

- Limit exceeded at the first iteration.
- Limit exceeded at the Nth consecutive failure.
- Consecutive gauge resets to zero on success before the next iteration can exceed it.
- Total error counters may exceed the limit while the consecutive gauge stays under it.

For every prefix or per-resource limit:

- Use at least two resource suffixes, such as two tool names or two model names.
- Make a non-limited suffix exceed the numeric threshold without terminating execution.
- Then make the limited suffix exceed and assert only that suffix triggers termination.
- Assert the exact matched key and stats for all suffixes involved.

For cross-cutting limit behavior:

- Multiple limits exceeded in the same update report the first limit by check order.
- Only one `LimitExceededEvent` is published for one exceeded-limit decision.
- Deep propagation works through parent, child, and grandchild contexts.
- Error and limit priority is explicit when both could apply.

### Limit Semantics

Most exact and prefix limits use this rule:

- A limit is exceeded when the current stat value is greater than `MaxValue`.
- The exact error format is `limit exceeded: <key> > <MaxValue>`.

Exact iteration limits use executor-owned semantics:

- Exact limits on `SCIterations` and `SCIterations.Self()` are checked after `AfterIteration`.
- For positive `MaxValue=N`, the executor allows N completed loop iterations.
- The executor then publishes `LimitExceededEvent` before iteration N+1 starts.
- The exact error format is `limit exceeded: <key> >= <MaxValue>`.
- A zero iteration limit is an edge case; because checks happen after `AfterIteration`, assert the
  current behavior directly if a test covers it.

### Stats Propagation

Use current stat propagation rules from `stats_keys.go` as the source of truth.

- Counter keys with the `SC` prefix propagate to parent contexts.
- Each counter has a `$self:` variant from `StatKey.Self()` for local-only limits.
- Gauge keys with the `SG` prefix are local-only and do not propagate.
- Calling `Self()` on a gauge is unnecessary because gauges are already local-only.
- `SCIterations` propagates as an aggregate counter across the execution tree.
- Use `SCIterations.Self()` for per-context iteration limits.

When a test involves children, assert both the parent aggregate and child-local value when either
could affect the result. This prevents confusing aggregate limits with per-context limits.

### Event Assertions For Limits

Limit tests must assert complete lifecycle events unless the test is explicitly focused on a
lower-level stat helper.

- Include `BeforeExecutionEvent` and final `AfterExecutionEvent`.
- Include every `BeforeIterationEvent` and `AfterIterationEvent` up to termination.
- Include the exact `LimitExceededEvent` with limit, current value, and matched key.
- Include `AfterModelCallEvent`, `BeforeToolCallEvent`, or parse-error events when they create the
  stat that triggers the limit.
- Verify `AfterIterationEvent.Result.NextPrompt` with exact expected content.

Use `tt.ContinueWithPrompt()` instead of any empty continue helper when the prompt is known.

## Code Organization

Use predictable locations and names so coverage gaps are easy to find.

- Executor core tests: `executor/*_test.go`.
- Agent limit tests: `agents/<agent_name>/agent_executor_limits_test.go`.
- Test function pattern for limits: `TestExecutorLimits_<StatName>`.
- Consecutive reset pattern: `TestExecutorLimits_ConsecutiveReset_<StatName>`.
- Prefix limit pattern: `TestExecutorLimits_<StatName>For<Resource>`.

Preferred subtest names:

```go
t.Run("stops when limit exceeded at first iteration", func(t *testing.T) { ... })
t.Run("stops when limit exceeded at Nth iteration", func(t *testing.T) { ... })
t.Run("consecutive counter resets on success", func(t *testing.T) { ... })
t.Run("child context limit propagates to parent", func(t *testing.T) { ... })
t.Run("prefix limit only triggers on matching suffix", func(t *testing.T) { ... })
```

## Helper Functions

Use the `internal/tt` helpers for expected lifecycle events and agent results.

Common event builders:

```go
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
```

Common result and observation builders:

```go
tt.ContinueWithPrompt(nextPrompt)
tt.Terminate(text)
tt.ToolObservation(format, toolChain, toolName, output)
tt.FormatParseErrorObservation(format, err, rawResponse)
tt.ToolchainErrorObservation(format, err)
tt.TerminationParseErrorObservation(format, err, content)
tt.ValidatorFeedbackObservation(format, feedbackSections...)
```

Common limit builders:

```go
tt.ExactLimit(key, maxValue)
tt.PrefixLimit(key, maxValue)
```

## New Stat Checklist

When adding or changing a framework stat:

- Add stat increment tests.
- Add reset tests when the stat is consecutive or last-iteration scoped.
- Add first-iteration limit test.
- Add Nth-iteration limit test.
- Add child propagation test for counters.
- Add local-only test for gauges or `Self()` limits.
- Add prefix scoping test for prefix stats.
- Add multiple-limit priority test if the stat can be updated with other limited stats.
- Update docs if the stat's propagation or limit semantics are unusual.
- Run `go test ./... -count=1 -timeout 300s`.
- Run `go test -race` for packages touched by concurrency-sensitive stat behavior.

## New Agent Checklist

When implementing executor limit tests for a new agent:

- Create `agents/<agent_name>/agent_executor_limits_test.go`.
- Cover every counter, gauge, and prefix stat the agent can produce.
- Cover child context propagation for agent behavior that can spawn child contexts.
- Use exact expected stats, limits, termination reasons, errors, and lifecycle events.
- Verify `NextPrompt` content for every continued iteration.
- Run `go test ./agents/<agent_name>/... -count=1 -timeout 300s`.
