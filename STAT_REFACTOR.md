# Stats Refactor: Counters, Gauges, and Propagation Semantics

## Problem Statement

The current stats system has unclear semantics around counters vs gauges and propagation:

1. **Counters are used for values that go down.** Consecutive error stats (e.g.,
   `KeyFormatParseErrorConsecutive`) are stored as counters but get reset to 0 on success via
   `ResetCounter()`. Counters should only go up.

2. **Propagation is broken for consecutive stats.** `IncrCounter` propagates +1 to the parent on
   error, but `ResetCounter` does NOT propagate the reset. The parent accumulates increments
   without ever seeing resets, making its "consecutive" count meaningless (it's really a total).

3. **Consecutive stats should not propagate at all.** "Consecutive errors" is inherently local
   to a single execution loop. A child's format parse errors say nothing about the parent's
   format parser. Parallel children interleave their errors, making the parent's "consecutive"
   count nonsensical.

4. **No way to set limits on local-only counter values.** All counters propagate, so at the
   parent level you can only see aggregated values. There's no way to say "limit THIS context's
   tool calls to 10" without also counting children's tool calls.

5. **`SetCounter` exists but is never used.** Dead code with unclear semantics (doesn't
   propagate, doesn't check limits).

## Design

### Core Principles

1. **Counters only go up.** No `SetCounter`, no `ResetCounter`, no negative deltas.
   Passing a negative delta to `IncrCounter` panics.

2. **Counters always propagate.** Every counter increment propagates to the parent hierarchy.
   This is the common case (total tokens, total tool calls, total errors).

3. **Every counter has a `$self:` counterpart.** When a counter is incremented, a
   `$self:`-prefixed version is also incremented locally (no propagation). This enables
   per-context limits.

4. **Gauges can go up and down.** `IncrGauge` (positive or negative delta), `SetGauge`, and
   `ResetGauge` are all valid operations.

5. **Gauges never propagate.** Gauges are local to the execution context. This is correct for
   consecutive error tracking (inherently local) and future use cases like scratchpad length.

6. **`$self:` is a reserved prefix.** User code cannot directly increment `$self:`-prefixed
   keys. They are managed internally by the stats system.

### StatKey Type

```go
type StatKey string

// Self returns the local-only variant of this key.
// When used in limits, matches only the current context's value,
// excluding children.
//
// Only meaningful for counter keys. Gauge keys are already local-only.
func (k StatKey) Self() StatKey {
    if strings.HasPrefix(string(k), selfPrefix) {
        return k
    }
    return StatKey(selfPrefix) + k
}

// IsSelf returns true if this key is a $self:-prefixed local-only key.
// Useful for filtering when iterating over Counters().
func (k StatKey) IsSelf() bool {
    return strings.HasPrefix(string(k), selfPrefix)
}

const selfPrefix = "$self:"
```

### Key Naming Convention

For internal standard stats, we use a naming convention to distinguish counter keys from gauge
keys at the constant level:

- `SC` prefix = **S**tat **C**ounter (e.g., `SCInputTokens`, `SCToolCalls`)
- `SG` prefix = **S**tat **G**auge (e.g., `SGFormatParseErrorConsecutive`)

```go
// Counters (only go up, always propagated, have $self: counterpart)
const (
    SCIterations    StatKey = "gent:iterations"
    SCInputTokens   StatKey = "gent:input_tokens"
    SCInputTokensFor StatKey = "gent:input_tokens:" // + model name
    // ...
)

// Gauges (can go up/down, never propagated, no $self: counterpart)
const (
    SGFormatParseErrorConsecutive    StatKey = "gent:format_parse_error_consecutive"
    SGToolCallsErrorConsecutive     StatKey = "gent:tool_calls_error_consecutive"
    SGToolCallsErrorConsecutiveFor  StatKey = "gent:tool_calls_error_consecutive:" // + tool
    // ...
)
```

### Counter Behavior

```
IncrCounter("gent:input_tokens", 100)

Context hierarchy:       What happens:
                         ┌─────────────────────────────────────┐
  Root                   │ gent:input_tokens += 100            │
   │                     │ (no $self: write - this is           │
   │                     │  propagated, not a direct call)      │
   │                     └─────────────────────────────────────┘
   │                                    ▲ propagate
   │                     ┌─────────────────────────────────────┐
   └── Child             │ gent:input_tokens += 100            │
                         │ $self:gent:input_tokens += 100      │
                         │ (direct call - writes both)          │
                         └─────────────────────────────────────┘

Result at Root:
  gent:input_tokens = 100               (own + children, aggregated)
  $self:gent:input_tokens = 0           (own only - root didn't call model)

Result at Child:
  gent:input_tokens = 100               (own + children, aggregated)
  $self:gent:input_tokens = 100         (own only)
```

### Gauge Behavior

```
IncrGauge("gent:format_parse_error_consecutive", 1)   // on error
ResetGauge("gent:format_parse_error_consecutive")      // on success

Context hierarchy:       What happens:
                         ┌─────────────────────────────────────┐
  Root                   │ (nothing - gauges don't propagate)   │
   │                     └─────────────────────────────────────┘
   │
   │                     ┌─────────────────────────────────────┐
   └── Child             │ gent:format_parse_error_consecutive │
                         │   = 0 (was 2, then reset on success) │
                         └─────────────────────────────────────┘
```

### Limit Examples

```go
// Total input tokens across entire agent tree (root + all children)
Limit{Type: LimitExactKey, Key: SCInputTokens, MaxValue: 100000}

// Input tokens for THIS context only (excluding children)
Limit{Type: LimitExactKey, Key: SCInputTokens.Self(), MaxValue: 50000}

// Total iterations across entire agent tree
Limit{Type: LimitExactKey, Key: SCIterations, MaxValue: 100}

// Iterations for THIS context only
Limit{Type: LimitExactKey, Key: SCIterations.Self(), MaxValue: 20}

// 3 consecutive format parse errors in THIS context (gauge, always local)
Limit{Type: LimitExactKey, Key: SGFormatParseErrorConsecutive, MaxValue: 3}

// ANY tool called more than 50 times across entire tree
Limit{Type: LimitKeyPrefix, Key: SCToolCallsFor, MaxValue: 50}

// ANY tool called more than 10 times in THIS context only
Limit{Type: LimitKeyPrefix, Key: SCToolCallsFor.Self(), MaxValue: 10}
```

### API Changes

#### Removed Methods

| Method | Reason | Migration |
|--------|--------|-----------|
| `SetCounter(key, value)` | Counters only go up | Remove (never used) |
| `ResetCounter(key)` | Counters only go up | Use `ResetGauge()` for consecutive stats |

#### Changed Methods

| Method | Change |
|--------|--------|
| `IncrCounter(key, delta)` | Panics on negative delta. Key type: `StatKey`. Also writes `$self:` counterpart locally. |
| `GetCounter(key)` | Key type: `StatKey` |
| `IncrGauge(key, delta)` | No longer propagates to parent. Key type: `StatKey`. |
| `SetGauge(key, value)` | Key type: `StatKey`. (Already didn't propagate.) |
| `ResetGauge(key)` | Key type: `StatKey`. (Already didn't propagate.) |
| `GetGauge(key)` | Key type: `StatKey` |

#### New Methods

| Method | Purpose |
|--------|---------|
| `StatKey.Self()` | Returns `$self:`-prefixed local-only variant |
| `StatKey.IsSelf()` | Returns true if key has `$self:` prefix |

#### Limit Struct Change

```go
type Limit struct {
    Type     LimitType
    Key      StatKey   // changed from string
    MaxValue float64
}
```

### Internal Implementation

#### `incrCounterInternal` Split

The current `incrCounterInternal` must distinguish between direct increments (which write
`$self:`) and propagated increments (which don't):

```go
// incrCounterDirect is called when this context directly increments a counter.
// Writes to both the base key and the $self: counterpart.
// Then propagates the base key to parent.
func (s *ExecutionStats) incrCounterDirect(key StatKey, delta int64) {
    sKey := string(key)

    s.mu.Lock()
    s.counters[sKey] += delta
    s.counters[selfPrefix+sKey] += delta
    s.mu.Unlock()

    if s.execCtx != nil {
        s.execCtx.checkLimits()
    }

    if s.parent != nil && !isProtectedKey(key) {
        s.parent.incrCounterPropagated(key, delta)
    }
}

// incrCounterPropagated is called when a child propagates a counter increment.
// Writes ONLY to the base key (not $self:).
// Then continues propagating to parent.
func (s *ExecutionStats) incrCounterPropagated(key StatKey, delta int64) {
    sKey := string(key)

    s.mu.Lock()
    s.counters[sKey] += delta
    s.mu.Unlock()

    if s.execCtx != nil {
        s.execCtx.checkLimits()
    }

    if s.parent != nil && !isProtectedKey(key) {
        s.parent.incrCounterPropagated(key, delta)
    }
}
```

#### Reserved Prefix Protection

```go
func (s *ExecutionStats) IncrCounter(key StatKey, delta int64) {
    if delta < 0 {
        panic("gent: IncrCounter called with negative delta")
    }
    if key.IsSelf() {
        panic("gent: IncrCounter called with reserved $self: prefix")
    }
    if isProtectedKey(key) {
        return
    }
    s.incrCounterDirect(key, delta)
}
```

#### Gauge Changes

```go
func (s *ExecutionStats) IncrGauge(key StatKey, delta float64) {
    s.mu.Lock()
    s.gauges[string(key)] += delta
    s.mu.Unlock()

    if s.execCtx != nil {
        s.execCtx.checkLimits()
    }
    // No propagation - gauges are local only
}
```

#### `updateStatsForEvent` Changes

Consecutive stats move from `incrCounterInternal` to gauge operations:

```go
case *AfterToolCallEvent:
    if e.Error != nil {
        ctx.stats.incrCounterDirect(SCToolCallsErrorTotal, 1)
        ctx.stats.incrGaugeInternal(SGToolCallsErrorConsecutive, 1) // gauge now
        if e.ToolName != "" {
            ctx.stats.incrCounterDirect(SCToolCallsErrorFor+StatKey(e.ToolName), 1)
            ctx.stats.incrGaugeInternal(
                SGToolCallsErrorConsecutiveFor+StatKey(e.ToolName), 1,
            ) // gauge now
        }
    }
```

#### Consecutive Error Reset Migration

All `ResetCounter` calls for consecutive stats become `ResetGauge`:

```go
// Before:
execCtx.Stats().ResetCounter(gent.KeyFormatParseErrorConsecutive)

// After:
execCtx.Stats().ResetGauge(gent.SGFormatParseErrorConsecutive)
```

### Limit Checking

No changes to limit checking logic. Limits already check both counter and gauge maps by key
lookup. The only change is `Limit.Key` becomes `StatKey` instead of `string`.

`$self:` keys are stored in the same counters map, so existing key lookup works. Prefix limits
on `$self:` keys work naturally:

```go
// Prefix "gent:tool_calls:" matches "gent:tool_calls:search" but NOT
// "$self:gent:tool_calls:search" (different prefix).

// Prefix "$self:gent:tool_calls:" matches "$self:gent:tool_calls:search"
// but NOT "gent:tool_calls:search".
```

No collision between propagated and self keys in prefix matching.

### KeyIterations Behavior Change

`KeyIterations` (renamed to `SCIterations`) currently does NOT propagate due to the
`isProtectedKey` check in the propagation path. Under this refactor:

- **Protected** stays: user code cannot call `IncrCounter(SCIterations, 1)` (silently ignored).
- **Propagation changes**: `SCIterations` now propagates like all other counters. The
  `isProtectedKey` check is removed from the propagation path and only remains in the public
  `IncrCounter` method.
- **Migration**: Existing limits on `KeyIterations` (e.g., `MaxValue: 100`) now mean "100 total
  iterations across the entire agent tree." Use `SCIterations.Self()` for per-context limits.
- **DefaultLimits**: Change to use `SCIterations.Self()` since the default intent is per-context.

### What About `*ErrorAt` Keys?

Keys like `KeyFormatParseErrorAt + iteration` (e.g., `gent:format_parse_error:3`) currently
propagate. The iteration number is context-local, so at the parent these keys are semantically
ambiguous (child iteration 3 != parent iteration 3).

**Decision**: Keep them as counters (they only go up). They propagate, which means the parent
sees `gent:format_parse_error:3 = 1` from both itself and children, which is slightly confusing
but harmless — no limits are set on these keys. They're purely for observability/debugging.

## Full Key Classification

### Counters (SC prefix, only go up, always propagated, have `$self:` counterpart)

| Old Constant | New Constant | Propagates |
|---|---|---|
| `KeyIterations` | `SCIterations` | Yes (changed!) |
| `KeyInputTokens` | `SCInputTokens` | Yes |
| `KeyInputTokensFor` | `SCInputTokensFor` | Yes |
| `KeyOutputTokens` | `SCOutputTokens` | Yes |
| `KeyOutputTokensFor` | `SCOutputTokensFor` | Yes |
| `KeyToolCalls` | `SCToolCalls` | Yes |
| `KeyToolCallsFor` | `SCToolCallsFor` | Yes |
| `KeyToolCallsErrorTotal` | `SCToolCallsErrorTotal` | Yes |
| `KeyToolCallsErrorFor` | `SCToolCallsErrorFor` | Yes |
| `KeyFormatParseErrorTotal` | `SCFormatParseErrorTotal` | Yes |
| `KeyFormatParseErrorAt` | `SCFormatParseErrorAt` | Yes |
| `KeyToolchainParseErrorTotal` | `SCToolchainParseErrorTotal` | Yes |
| `KeyToolchainParseErrorAt` | `SCToolchainParseErrorAt` | Yes |
| `KeyTerminationParseErrorTotal` | `SCTerminationParseErrorTotal` | Yes |
| `KeyTerminationParseErrorAt` | `SCTerminationParseErrorAt` | Yes |
| `KeySectionParseErrorTotal` | `SCSectionParseErrorTotal` | Yes |
| `KeySectionParseErrorAt` | `SCSectionParseErrorAt` | Yes |
| `KeyAnswerRejectedTotal` | `SCAnswerRejectedTotal` | Yes |
| `KeyAnswerRejectedBy` | `SCAnswerRejectedBy` | Yes |

### Gauges (SG prefix, can go up/down, never propagated, no `$self:` counterpart)

| Old Constant | New Constant | Was Counter |
|---|---|---|
| `KeyFormatParseErrorConsecutive` | `SGFormatParseErrorConsecutive` | Yes |
| `KeyToolchainParseErrorConsecutive` | `SGToolchainParseErrorConsecutive` | Yes |
| `KeySectionParseErrorConsecutive` | `SGSectionParseErrorConsecutive` | Yes |
| `KeyTerminationParseErrorConsecutive` | `SGTerminationParseErrorConsecutive` | Yes |
| `KeyToolCallsErrorConsecutive` | `SGToolCallsErrorConsecutive` | Yes |
| `KeyToolCallsErrorConsecutiveFor` | `SGToolCallsErrorConsecutiveFor` | Yes |

## Implementation Plan

### Phase 1: Introduce `StatKey` type and rename constants

1. Define `StatKey` type with `Self()` and `IsSelf()` methods in `stats.go`.
2. Rename all key constants in `stats_keys.go`:
   - Counter keys: `Key*` -> `SC*`
   - Gauge keys (consecutive): `Key*` -> `SG*`
   - All become `StatKey` type instead of `string`.
3. Add old names as deprecated aliases for backward compat during refactor.
4. Change `Limit.Key` from `string` to `StatKey`.
5. Update all references across the codebase.

**Files changed**: `stats_keys.go`, `limit.go`, `stats.go`, `context.go`, and all files
referencing stat keys.

### Phase 2: Counter semantics — remove `SetCounter`/`ResetCounter`, enforce positive delta

1. Remove `SetCounter` method (never used in production code).
2. Remove `ResetCounter` method.
3. Add panic on negative delta in `IncrCounter`.
4. Move consecutive error stats from counters to gauges in `updateStatsForEvent`.
5. Change all `ResetCounter` call sites (format, toolchain, section, termination implementations)
   to `ResetGauge`.
6. Update tests that use `IncrCounter` on consecutive keys to use `IncrGauge`.

**Files changed**: `stats.go`, `context.go`, `format/xml.go`, `format/markdown.go`,
`toolchain/yaml.go`, `toolchain/json.go`, `termination/json.go`, `section/yaml.go`,
`section/json.go`, `internal/tt/mocks.go`, `agents/react/agent_test.go`, all limit test files.

### Phase 3: Add `$self:` tracking to counters

1. Split `incrCounterInternal` into `incrCounterDirect` and `incrCounterPropagated`.
2. `incrCounterDirect` writes both base key and `$self:` key, then propagates via
   `incrCounterPropagated`.
3. `incrCounterPropagated` writes only base key, then continues propagating.
4. `IncrCounter` (public) calls `incrCounterDirect`.
5. `updateStatsForEvent` calls `incrCounterDirect` for counter stats.
6. Add `$self:` prefix protection (panic if user tries to increment `$self:` directly).

**Files changed**: `stats.go`, `context.go`.

### Phase 4: Remove gauge propagation

1. Remove parent propagation from `IncrGauge`.
2. Add internal `incrGaugeInternal` for framework use (limit checking, no propagation).
3. Update `updateStatsForEvent` to use `incrGaugeInternal` for consecutive gauge stats.

**Files changed**: `stats.go`, `context.go`.

### Phase 5: `KeyIterations` propagation change

1. Remove `KeyIterations` from `protectedKeys` map (propagation is no longer blocked there).
2. Keep protection in public `IncrCounter` — user code still can't increment it.
3. `updateStatsForEvent` calls `incrCounterDirect(SCIterations, 1)` which propagates.
4. Update `DefaultLimits` to use `SCIterations.Self()` for per-context iteration limit.
5. Update propagation tests.

**Files changed**: `stats_keys.go`, `stats.go`, `limit.go`, `events_test.go`,
`executor/executor_limit_test.go`.

### Phase 6: Update documentation and tests

1. Update `ExecutionStats` doc comment with counter vs gauge semantics.
2. Update `stats_keys.go` doc comments for SC/SG convention.
3. Update CLAUDE.md Stats section.
4. Update interface doc comments (`format.go`, `section.go`, `toolchain.go`) —
   `ResetCounter` references become `ResetGauge`.
5. Add tests:
   - `$self:` key is correctly tracked on direct increment.
   - `$self:` key is NOT written on propagated increment.
   - Negative counter delta panics.
   - `$self:` prefix rejected by public `IncrCounter`.
   - Gauge does not propagate to parent.
   - `StatKey.Self()` is idempotent.
   - `StatKey.IsSelf()` works correctly.
   - Limits on `$self:` keys work correctly.
   - Limits on gauge keys work correctly.
   - DefaultLimits uses `SCIterations.Self()` for per-context limit.
6. Remove deprecated aliases added in Phase 1.

**Files changed**: `stats.go`, `stats_keys.go`, `CLAUDE.md`, `format.go`, `section.go`,
`toolchain.go`, `events_test.go`, `executor/executor_limit_test.go`,
`agents/react/agent_executor_limits_test.go`.

### Phase 7: Cleanup

1. Remove old `Key*` deprecated aliases from `stats_keys.go`.
2. Remove any `// TODO` markers added during refactor.
3. Final review: verify no `ResetCounter` or `SetCounter` calls remain.
4. Final review: verify no gauge propagation paths remain.
5. Run full test suite.
