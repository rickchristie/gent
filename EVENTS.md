# Unified Event System Implementation Plan

This document outlines the plan to merge the current Hooks and Traces systems into a unified Event system.

## Goals

1. **Simplify mental model** — One event system instead of separate hooks and traces
2. **Complete observability** — All events are recorded in `[]Event` for debugging
3. **Flexible subscriptions** — Subscribe to any event type, not just predefined hook points
4. **JSON-friendly** — All events have `EventName` for easy serialization/deserialization
5. **Type-safe** — Subscriber interfaces provide compile-time checking
6. **Ergonomic API** — `ctx.PublishXXX()` convenience methods with proper args and return values

## Current Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      ExecutionContext                        │
├─────────────────────────────────────────────────────────────┤
│  Hooks (via HookFirer)          │  Traces (via Trace())     │
│  ├─ FireBeforeExecution         │  ├─ ModelCallTrace        │
│  ├─ FireAfterExecution          │  ├─ ToolCallTrace         │
│  ├─ FireBeforeIteration         │  ├─ IterationStartTrace   │
│  ├─ FireAfterIteration          │  ├─ IterationEndTrace     │
│  ├─ FireBeforeModelCall         │  ├─ ParseErrorTrace       │
│  ├─ FireAfterModelCall          │  ├─ CommonTraceEvent      │
│  ├─ FireBeforeToolCall          │  └─ ...                   │
│  ├─ FireAfterToolCall           │                           │
│  └─ FireError                   │                           │
├─────────────────────────────────────────────────────────────┤
│  - Hooks: run subscriber functions, can modify state        │
│  - Traces: append to []TraceEvent, update stats, check limits│
└─────────────────────────────────────────────────────────────┘
```

**Problems:**
- Two separate systems for similar purposes
- Hook fires are not recorded (lost for debugging)
- Mental overhead: "Do I need a hook or a trace?"
- Inconsistent APIs

## New Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      ExecutionContext                        │
├─────────────────────────────────────────────────────────────┤
│  PublishXXX Methods (framework events):                      │
│  ├─ PublishBeforeExecution()    PublishAfterExecution()     │
│  ├─ PublishBeforeIteration()    PublishAfterIteration()     │
│  ├─ PublishBeforeModelCall()    PublishAfterModelCall()     │
│  ├─ PublishBeforeToolCall()     PublishAfterToolCall()      │
│  ├─ PublishParseError()                                      │
│  ├─ PublishValidatorCalled()    PublishValidatorResult()    │
│  ├─ PublishError()                                           │
│  └─ PublishCommonEvent() (user-defined)                     │
│                                                              │
│  Publish(event Event) — for fully custom events              │
├─────────────────────────────────────────────────────────────┤
│  What happens on publish:                                    │
│  1. Append to []Event                                        │
│  2. Update stats (based on event type and timing rules)      │
│  3. Check limits (cancel context if exceeded)                │
│  4. Run all matching subscribers                             │
└─────────────────────────────────────────────────────────────┘
```

## Detailed Design

### 1. Base Types (`events.go`)

```go
// Event is the marker interface for all events.
type Event interface {
    event() // marker method
}

// BaseEvent contains common fields for all events.
// Automatically populated by ExecutionContext.Publish().
type BaseEvent struct {
    // EventName identifies this event type (e.g., "gent:iteration:before").
    EventName string

    // Timestamp is when this event occurred.
    Timestamp time.Time

    // Iteration is the current iteration number (1-indexed, 0 if before first).
    Iteration int

    // Depth is the nesting depth (0 for root context).
    Depth int
}

func (BaseEvent) event() {}
```

### 2. Event Name Constants (`event_names.go`)

```go
// Framework event names
const (
    // Execution lifecycle
    EventNameExecutionBefore = "gent:execution:before"
    EventNameExecutionAfter  = "gent:execution:after"

    // Iteration lifecycle
    EventNameIterationBefore = "gent:iteration:before"
    EventNameIterationAfter  = "gent:iteration:after"

    // Model calls
    EventNameModelCallBefore = "gent:model_call:before"
    EventNameModelCallAfter  = "gent:model_call:after"

    // Tool calls
    EventNameToolCallBefore = "gent:tool_call:before"
    EventNameToolCallAfter  = "gent:tool_call:after"

    // Errors and validation
    EventNameParseError       = "gent:parse_error"
    EventNameValidatorCalled  = "gent:validator:called"
    EventNameValidatorResult  = "gent:validator:result"
    EventNameError            = "gent:error"
)
```

### 3. Event Types (`events.go`)

```go
// BeforeExecutionEvent is published once before the first iteration.
type BeforeExecutionEvent struct {
    BaseEvent
}

// AfterExecutionEvent is published once after execution ends.
type AfterExecutionEvent struct {
    BaseEvent
    TerminationReason TerminationReason
    Error             error
}

// BeforeIterationEvent is published before each iteration.
// Subscribers can modify LoopData for persistent context injection.
type BeforeIterationEvent struct {
    BaseEvent
}

// AfterIterationEvent is published after each iteration.
type AfterIterationEvent struct {
    BaseEvent
    Result   *AgentLoopResult
    Duration time.Duration
}

// BeforeModelCallEvent is published before each model API call.
// Subscribers can modify Request for ephemeral context injection.
type BeforeModelCallEvent struct {
    BaseEvent
    Model   string
    Request []llms.MessageContent // Mutable by subscribers
}

// AfterModelCallEvent is published after each model API call.
type AfterModelCallEvent struct {
    BaseEvent
    Model        string
    Request      any // The actual request sent (after modifications)
    Response     *ContentResponse
    InputTokens  int
    OutputTokens int
    Duration     time.Duration
    Error        error
}

// BeforeToolCallEvent is published before each tool execution.
// Subscribers can modify Args.
type BeforeToolCallEvent struct {
    BaseEvent
    ToolName string
    Args     any // Mutable by subscribers
}

// AfterToolCallEvent is published after each tool execution.
type AfterToolCallEvent struct {
    BaseEvent
    ToolName string
    Args     any
    Output   any
    Duration time.Duration
    Error    error
}

// ParseErrorEvent is published when parsing fails.
type ParseErrorEvent struct {
    BaseEvent
    ErrorType  string // "format", "toolchain", "termination", "section"
    RawContent string
    Error      error
}

// ValidatorCalledEvent is published when a validator is invoked.
type ValidatorCalledEvent struct {
    BaseEvent
    ValidatorName string
    Answer        any
}

// ValidatorResultEvent is published after validator completes.
type ValidatorResultEvent struct {
    BaseEvent
    ValidatorName string
    Answer        any
    Accepted      bool
    Feedback      []FormattedSection // Only set when rejected
}

// ErrorEvent is published when an error occurs.
type ErrorEvent struct {
    BaseEvent
    Error error
}

// CommonEvent is for user-defined events.
type CommonEvent struct {
    BaseEvent
    Description string
    Data        any
}
```

### 4. Subscriber Interfaces (`subscribers.go`)

```go
// Subscriber interfaces - implement any combination for type-safe subscriptions.

type BeforeExecutionSubscriber interface {
    OnBeforeExecution(execCtx *ExecutionContext, event *BeforeExecutionEvent)
}

type AfterExecutionSubscriber interface {
    OnAfterExecution(execCtx *ExecutionContext, event *AfterExecutionEvent)
}

type BeforeIterationSubscriber interface {
    OnBeforeIteration(execCtx *ExecutionContext, event *BeforeIterationEvent)
}

type AfterIterationSubscriber interface {
    OnAfterIteration(execCtx *ExecutionContext, event *AfterIterationEvent)
}

type BeforeModelCallSubscriber interface {
    OnBeforeModelCall(execCtx *ExecutionContext, event *BeforeModelCallEvent)
}

type AfterModelCallSubscriber interface {
    OnAfterModelCall(execCtx *ExecutionContext, event *AfterModelCallEvent)
}

type BeforeToolCallSubscriber interface {
    OnBeforeToolCall(execCtx *ExecutionContext, event *BeforeToolCallEvent)
}

type AfterToolCallSubscriber interface {
    OnAfterToolCall(execCtx *ExecutionContext, event *AfterToolCallEvent)
}

type ParseErrorSubscriber interface {
    OnParseError(execCtx *ExecutionContext, event *ParseErrorEvent)
}

type ValidatorCalledSubscriber interface {
    OnValidatorCalled(execCtx *ExecutionContext, event *ValidatorCalledEvent)
}

type ValidatorResultSubscriber interface {
    OnValidatorResult(execCtx *ExecutionContext, event *ValidatorResultEvent)
}

type ErrorSubscriber interface {
    OnError(execCtx *ExecutionContext, event *ErrorEvent)
}

type CommonEventSubscriber interface {
    OnCommonEvent(execCtx *ExecutionContext, event *CommonEvent)
}
```

### 5. Registry (`events/registry.go`)

```go
package events

// Registry manages event subscribers and dispatches events.
type Registry struct {
    subscribers  []any
    maxRecursion int // default 10
}

// NewRegistry creates a new event registry with default settings.
func NewRegistry() *Registry {
    return &Registry{
        subscribers:  make([]any, 0),
        maxRecursion: 10,
    }
}

// Subscribe adds a subscriber. The subscriber can implement any combination
// of subscriber interfaces.
func (r *Registry) Subscribe(subscriber any) *Registry {
    r.subscribers = append(r.subscribers, subscriber)
    return r
}

// SetMaxRecursion sets the maximum event recursion depth.
// If a subscriber publishes an event that triggers another subscriber
// that publishes an event, etc., this limit prevents infinite loops.
func (r *Registry) SetMaxRecursion(max int) *Registry {
    r.maxRecursion = max
    return r
}

// MaxRecursion returns the configured max recursion depth.
func (r *Registry) MaxRecursion() int {
    return r.maxRecursion
}

// Dispatch sends an event to all matching subscribers.
// Called by ExecutionContext.Publish() after recording and stats.
func (r *Registry) Dispatch(execCtx *gent.ExecutionContext, event gent.Event) {
    switch e := event.(type) {
    case *gent.BeforeExecutionEvent:
        for _, s := range r.subscribers {
            if sub, ok := s.(gent.BeforeExecutionSubscriber); ok {
                sub.OnBeforeExecution(execCtx, e)
            }
        }
    case *gent.AfterExecutionEvent:
        for _, s := range r.subscribers {
            if sub, ok := s.(gent.AfterExecutionSubscriber); ok {
                sub.OnAfterExecution(execCtx, e)
            }
        }
    // ... cases for all event types
    }
}
```

### 6. ExecutionContext Changes (`context.go`)

```go
// EventPublisher is implemented by the registry to dispatch events to subscribers.
type EventPublisher interface {
    Dispatch(execCtx *ExecutionContext, event Event)
    MaxRecursion() int
}

// ExecutionContext fields (additions/changes):
type ExecutionContext struct {
    // ... existing fields ...

    events         []Event        // was []TraceEvent
    eventPublisher EventPublisher // was hookFirer
    eventDepth     int            // tracks recursion depth
}

// Publish records a custom event, updates stats, checks limits, and notifies subscribers.
// Use this for user-defined events. For framework events, use the typed PublishXXX methods.
//
// The sequence is:
//  1. Populate BaseEvent fields (EventName validated, Timestamp, Iteration, Depth)
//  2. Append to []Event
//  3. Update stats based on event type
//  4. Check limits (may cancel context)
//  5. Dispatch to subscribers (if publisher set)
//
// Panics if recursion depth exceeds MaxRecursion.
func (ctx *ExecutionContext) Publish(event Event) {
    // ... implementation ...
}

// Events returns all recorded events.
func (ctx *ExecutionContext) Events() []Event {
    ctx.mu.RLock()
    defer ctx.mu.RUnlock()
    return append([]Event(nil), ctx.events...)
}

// ============================================================================
// PublishXXX convenience methods
// ============================================================================
// These methods create and publish framework events with type-safe parameters.
// Each returns a pointer to the published event so subscribers can access
// or modify fields (for Before* events).

// PublishBeforeExecution creates and publishes a BeforeExecutionEvent.
func (ctx *ExecutionContext) PublishBeforeExecution() *BeforeExecutionEvent {
    event := &BeforeExecutionEvent{
        BaseEvent: BaseEvent{EventName: EventNameExecutionBefore},
    }
    ctx.publish(event)
    return event
}

// PublishAfterExecution creates and publishes an AfterExecutionEvent.
func (ctx *ExecutionContext) PublishAfterExecution(
    reason TerminationReason,
    err error,
) *AfterExecutionEvent {
    event := &AfterExecutionEvent{
        BaseEvent:         BaseEvent{EventName: EventNameExecutionAfter},
        TerminationReason: reason,
        Error:             err,
    }
    ctx.publish(event)
    return event
}

// PublishBeforeIteration creates and publishes a BeforeIterationEvent.
func (ctx *ExecutionContext) PublishBeforeIteration() *BeforeIterationEvent {
    event := &BeforeIterationEvent{
        BaseEvent: BaseEvent{EventName: EventNameIterationBefore},
    }
    ctx.publish(event)
    return event
}

// PublishAfterIteration creates and publishes an AfterIterationEvent.
func (ctx *ExecutionContext) PublishAfterIteration(
    result *AgentLoopResult,
    duration time.Duration,
) *AfterIterationEvent {
    event := &AfterIterationEvent{
        BaseEvent: BaseEvent{EventName: EventNameIterationAfter},
        Result:    result,
        Duration:  duration,
    }
    ctx.publish(event)
    return event
}

// PublishBeforeModelCall creates and publishes a BeforeModelCallEvent.
// Returns the event so callers can use the (potentially modified) Request field.
func (ctx *ExecutionContext) PublishBeforeModelCall(
    model string,
    request []llms.MessageContent,
) *BeforeModelCallEvent {
    event := &BeforeModelCallEvent{
        BaseEvent: BaseEvent{EventName: EventNameModelCallBefore},
        Model:     model,
        Request:   request,
    }
    ctx.publish(event)
    return event
}

// PublishAfterModelCall creates and publishes an AfterModelCallEvent.
func (ctx *ExecutionContext) PublishAfterModelCall(
    model string,
    request any,
    response *ContentResponse,
    duration time.Duration,
    err error,
) *AfterModelCallEvent {
    event := &AfterModelCallEvent{
        BaseEvent: BaseEvent{EventName: EventNameModelCallAfter},
        Model:     model,
        Request:   request,
        Response:  response,
        Duration:  duration,
        Error:     err,
    }
    if response != nil && response.Info != nil {
        event.InputTokens = response.Info.InputTokens
        event.OutputTokens = response.Info.OutputTokens
    }
    ctx.publish(event)
    return event
}

// PublishBeforeToolCall creates and publishes a BeforeToolCallEvent.
// Returns the event so callers can use the (potentially modified) Args field.
func (ctx *ExecutionContext) PublishBeforeToolCall(
    toolName string,
    args any,
) *BeforeToolCallEvent {
    event := &BeforeToolCallEvent{
        BaseEvent: BaseEvent{EventName: EventNameToolCallBefore},
        ToolName:  toolName,
        Args:      args,
    }
    ctx.publish(event)
    return event
}

// PublishAfterToolCall creates and publishes an AfterToolCallEvent.
func (ctx *ExecutionContext) PublishAfterToolCall(
    toolName string,
    args any,
    output any,
    duration time.Duration,
    err error,
) *AfterToolCallEvent {
    event := &AfterToolCallEvent{
        BaseEvent: BaseEvent{EventName: EventNameToolCallAfter},
        ToolName:  toolName,
        Args:      args,
        Output:    output,
        Duration:  duration,
        Error:     err,
    }
    ctx.publish(event)
    return event
}

// PublishParseError creates and publishes a ParseErrorEvent.
func (ctx *ExecutionContext) PublishParseError(
    errorType string,
    rawContent string,
    err error,
) *ParseErrorEvent {
    event := &ParseErrorEvent{
        BaseEvent:  BaseEvent{EventName: EventNameParseError},
        ErrorType:  errorType,
        RawContent: rawContent,
        Error:      err,
    }
    ctx.publish(event)
    return event
}

// PublishValidatorCalled creates and publishes a ValidatorCalledEvent.
func (ctx *ExecutionContext) PublishValidatorCalled(
    validatorName string,
    answer any,
) *ValidatorCalledEvent {
    event := &ValidatorCalledEvent{
        BaseEvent:     BaseEvent{EventName: EventNameValidatorCalled},
        ValidatorName: validatorName,
        Answer:        answer,
    }
    ctx.publish(event)
    return event
}

// PublishValidatorResult creates and publishes a ValidatorResultEvent.
func (ctx *ExecutionContext) PublishValidatorResult(
    validatorName string,
    answer any,
    accepted bool,
    feedback []FormattedSection,
) *ValidatorResultEvent {
    event := &ValidatorResultEvent{
        BaseEvent:     BaseEvent{EventName: EventNameValidatorResult},
        ValidatorName: validatorName,
        Answer:        answer,
        Accepted:      accepted,
        Feedback:      feedback,
    }
    ctx.publish(event)
    return event
}

// PublishError creates and publishes an ErrorEvent.
func (ctx *ExecutionContext) PublishError(err error) *ErrorEvent {
    event := &ErrorEvent{
        BaseEvent: BaseEvent{EventName: EventNameError},
        Error:     err,
    }
    ctx.publish(event)
    return event
}

// PublishCommonEvent creates and publishes a CommonEvent for user-defined events.
// eventName should use the format "namespace:event_name" (e.g., "myapp:cache_hit").
func (ctx *ExecutionContext) PublishCommonEvent(
    eventName string,
    description string,
    data any,
) *CommonEvent {
    event := &CommonEvent{
        BaseEvent:   BaseEvent{EventName: eventName},
        Description: description,
        Data:        data,
    }
    ctx.publish(event)
    return event
}

// publish is the internal implementation shared by Publish() and PublishXXX methods.
func (ctx *ExecutionContext) publish(event Event) {
    ctx.mu.Lock()

    // Check recursion depth
    if ctx.eventPublisher != nil && ctx.eventDepth >= ctx.eventPublisher.MaxRecursion() {
        ctx.mu.Unlock()
        panic(fmt.Sprintf("event recursion depth exceeded maximum (%d)",
            ctx.eventPublisher.MaxRecursion()))
    }
    ctx.eventDepth++

    // Populate base fields
    ctx.populateBaseEvent(event)

    // Append to event log
    ctx.events = append(ctx.events, event)

    // Update stats based on event type
    ctx.updateStatsForEvent(event)

    ctx.mu.Unlock()

    // Check limits (outside lock to avoid deadlock)
    ctx.checkLimitsIfRoot()

    // Dispatch to subscribers
    if ctx.eventPublisher != nil {
        ctx.eventPublisher.Dispatch(ctx, event)
    }

    ctx.mu.Lock()
    ctx.eventDepth--
    ctx.mu.Unlock()
}
```

### 7. Stats Update Rules

```go
// updateStatsForEvent updates stats based on event type.
// Called within Publish() with lock held.
func (ctx *ExecutionContext) updateStatsForEvent(event Event) {
    switch e := event.(type) {

    // Increment BEFORE events (prevent N+1)
    case *BeforeIterationEvent:
        ctx.stats.incrCounterNoLimitCheck(KeyIterations, 1)

    case *BeforeToolCallEvent:
        ctx.stats.incrCounterNoLimitCheck(KeyToolCalls, 1)
        if e.ToolName != "" {
            ctx.stats.incrCounterNoLimitCheck(KeyToolCallsFor+e.ToolName, 1)
        }

    // Increment AFTER events (record what happened)
    case *AfterModelCallEvent:
        ctx.stats.incrCounterNoLimitCheck(KeyInputTokens, int64(e.InputTokens))
        ctx.stats.incrCounterNoLimitCheck(KeyOutputTokens, int64(e.OutputTokens))
        if e.Model != "" {
            ctx.stats.incrCounterNoLimitCheck(KeyInputTokensFor+e.Model, int64(e.InputTokens))
            ctx.stats.incrCounterNoLimitCheck(KeyOutputTokensFor+e.Model, int64(e.OutputTokens))
        }

    case *AfterToolCallEvent:
        if e.Error != nil {
            ctx.stats.incrCounterNoLimitCheck(KeyToolCallsErrorTotal, 1)
            ctx.stats.incrCounterNoLimitCheck(KeyToolCallsErrorConsecutive, 1)
            if e.ToolName != "" {
                ctx.stats.incrCounterNoLimitCheck(KeyToolCallsErrorFor+e.ToolName, 1)
                ctx.stats.incrCounterNoLimitCheck(KeyToolCallsErrorConsecutiveFor+e.ToolName, 1)
            }
        }

    case *ParseErrorEvent:
        iteration := fmt.Sprintf("%d", ctx.iteration)
        switch e.ErrorType {
        case "format":
            ctx.stats.incrCounterNoLimitCheck(KeyFormatParseErrorTotal, 1)
            ctx.stats.incrCounterNoLimitCheck(KeyFormatParseErrorAt+iteration, 1)
            ctx.stats.incrCounterNoLimitCheck(KeyFormatParseErrorConsecutive, 1)
        case "toolchain":
            ctx.stats.incrCounterNoLimitCheck(KeyToolchainParseErrorTotal, 1)
            ctx.stats.incrCounterNoLimitCheck(KeyToolchainParseErrorAt+iteration, 1)
            ctx.stats.incrCounterNoLimitCheck(KeyToolchainParseErrorConsecutive, 1)
        case "termination":
            ctx.stats.incrCounterNoLimitCheck(KeyTerminationParseErrorTotal, 1)
            ctx.stats.incrCounterNoLimitCheck(KeyTerminationParseErrorAt+iteration, 1)
            ctx.stats.incrCounterNoLimitCheck(KeyTerminationParseErrorConsecutive, 1)
        case "section":
            ctx.stats.incrCounterNoLimitCheck(KeySectionParseErrorTotal, 1)
            ctx.stats.incrCounterNoLimitCheck(KeySectionParseErrorAt+iteration, 1)
            ctx.stats.incrCounterNoLimitCheck(KeySectionParseErrorConsecutive, 1)
        }

    case *ValidatorResultEvent:
        if !e.Accepted {
            ctx.stats.incrCounterNoLimitCheck(KeyAnswerRejectedTotal, 1)
            if e.ValidatorName != "" {
                ctx.stats.incrCounterNoLimitCheck(KeyAnswerRejectedBy+e.ValidatorName, 1)
            }
        }
    }
}
```

## Migration Guide

### For Hook Implementors

**Before:**
```go
type MyHook struct{}

func (h *MyHook) OnBeforeIteration(
    execCtx *gent.ExecutionContext,
    event *gent.BeforeIterationEvent,
) {
    log.Printf("Starting iteration %d", event.Iteration)
}

// Register
registry := hooks.NewRegistry()
registry.Register(&MyHook{})
```

**After:**
```go
type MySubscriber struct{}

func (s *MySubscriber) OnBeforeIteration(
    execCtx *gent.ExecutionContext,
    event *gent.BeforeIterationEvent,
) {
    log.Printf("Starting iteration %d", event.Iteration)
}

// Register
registry := events.NewRegistry()
registry.Subscribe(&MySubscriber{})
```

### For Trace Consumers

**Before:**
```go
events := execCtx.Events()
for _, event := range events {
    switch e := event.(type) {
    case gent.ModelCallTrace:
        log.Printf("Model %s: %d tokens", e.Model, e.InputTokens)
    }
}
```

**After:**
```go
events := execCtx.Events()
for _, event := range events {
    switch e := event.(type) {
    case *gent.AfterModelCallEvent:
        log.Printf("Model %s: %d tokens", e.Model, e.InputTokens)
    }
}
```

### For Custom Trace Publishers

**Before:**
```go
execCtx.Trace(gent.CommonTraceEvent{
    EventId:     "myapp:cache_hit",
    Description: "Cache lookup succeeded",
    Data:        cacheData,
})
```

**After:**
```go
execCtx.PublishCommonEvent(
    "myapp:cache_hit",
    "Cache lookup succeeded",
    cacheData,
)
```

### For Framework Implementors (Internal)

When publishing framework events from within gent packages (executor, models, toolchain, etc.):

**Before:**
```go
// In models/lcg.go - firing hook and recording trace separately
ctx.FireBeforeModelCall(model, messages)
// ... make call ...
ctx.Trace(gent.ModelCallTrace{Model: model, InputTokens: tokens})
```

**After:**
```go
// In models/lcg.go - unified event publishing
event := ctx.PublishBeforeModelCall(model, messages)
messages = event.Request // Use potentially modified request
// ... make call ...
ctx.PublishAfterModelCall(model, messages, response, duration, err)
```

## Implementation Tasks

### Phase 1: Core Event System

1. **Create `events.go`** — Base types, all event structs
   - BaseEvent with EventName, Timestamp, Iteration, Depth
   - All event types (Before/After pairs, ParseError, Validator*, Error, Common)
   - Event marker interface

2. **Create `event_names.go`** — Event name constants
   - All EventName* constants
   - Documentation for naming conventions

3. **Create `subscribers.go`** — Subscriber interfaces
   - All *Subscriber interfaces
   - Documentation for implementing multiple interfaces

### Phase 2: Registry

4. **Create `events/registry.go`** — Event registry
   - Registry struct with subscribers and maxRecursion
   - Subscribe(), SetMaxRecursion(), MaxRecursion()
   - Dispatch() with type switch for all event types

5. **Create `events/doc.go`** — Package documentation
   - Overview of event system
   - Examples of creating subscribers
   - Examples of custom events

### Phase 3: ExecutionContext Integration

6. **Update `context.go`** — ExecutionContext changes
   - Replace `[]TraceEvent` with `[]Event`
   - Replace `hookFirer` with `eventPublisher`
   - Add `eventDepth` for recursion tracking
   - Add `PublishXXX()` convenience methods for all event types
   - Implement internal `publish()` method
   - Implement `Publish()` for custom events
   - Implement `updateStatsForEvent()`
   - Update `populateBaseEvent()` (was populateBaseTrace)
   - Update `Events()` return type

7. **Remove old files**
   - Delete `hooks.go` (interfaces moved to subscribers.go)
   - Delete `trace.go` (types moved to events.go)
   - Delete `hooks/registry.go` (replaced by events/registry.go)

### Phase 4: Framework Updates

8. **Update `executor/executor.go`**
   - Replace hook firing with `ctx.PublishBeforeExecution()`, `ctx.PublishAfterExecution()`
   - Replace iteration hooks with `ctx.PublishBeforeIteration()`, `ctx.PublishAfterIteration()`

9. **Update `models/lcg.go`**
    - Replace FireBeforeModelCall/FireAfterModelCall with `ctx.PublishBeforeModelCall()`,
      `ctx.PublishAfterModelCall()`
    - Use returned event from PublishBeforeModelCall for potentially modified Request

10. **Update `toolchain/yaml.go` and `toolchain/json.go`**
    - Replace Fire* calls with `ctx.PublishBeforeToolCall()`, `ctx.PublishAfterToolCall()`
    - Replace Trace calls with `ctx.PublishParseError()`
    - Use returned event from PublishBeforeToolCall for potentially modified Args

11. **Update `termination/text.go` and `termination/json.go`**
    - Replace Trace calls with `ctx.PublishValidatorCalled()`, `ctx.PublishValidatorResult()`
    - Replace parse error traces with `ctx.PublishParseError()`

12. **Update `format/xml.go`** (and other formats)
    - Replace Trace calls with `ctx.PublishParseError()`

### Phase 5: Documentation Updates

13. **Update `package.go`** — Main package documentation
    - Update event system overview
    - Update examples to use PublishXXX() methods

14. **Update `termination.go`** — Termination interface docs
    - Document PublishValidatorXXX() requirements for validators
    - Update example code

15. **Update `toolchain.go`** — ToolChain interface docs (if exists)
    - Document PublishXXX() requirements

16. **Update `format.go`** — TextFormat interface docs (if exists)
    - Document PublishParseError() requirements

17. **Update `events/doc.go`** — Complete package docs
    - Subscriber implementation guide
    - Custom event guide (using PublishCommonEvent)
    - Migration examples

### Phase 6: Tests

18. **Create `events_test.go`** — Event type tests
    - PublishXXX method tests (correct EventName set)
    - BaseEvent population tests

19. **Create `events/registry_test.go`** — Registry tests
    - Subscribe tests
    - Dispatch tests for each event type
    - MaxRecursion tests

20. **Update `context_test.go`** — ExecutionContext tests
    - PublishXXX method tests
    - Stats update tests
    - Recursion limit tests
    - Events() tests

21. **Update `executor/executor_test.go`**
    - Update to use new event system

22. **Update `termination/*_test.go`**
    - Update validator tracing tests to use events

23. **Update `toolchain/*_test.go`**
    - Update hook tests to use event subscribers

24. **Update integration tests**
    - `integrationtest/airline/*`
    - `integrationtest/loggers/*`

### Phase 7: Cleanup

25. **Remove deprecated code**
    - Remove any remaining references to old types
    - Remove hooks package (now events package)

26. **Final review**
    - Verify all docs are updated
    - Verify all tests pass
    - Verify no references to old types

## File Structure After Migration

```
gent/
├── agent.go              # AgentLoop, LoopData, BasicLoopData
├── context.go            # ExecutionContext with PublishXXX() methods
├── events.go             # BaseEvent, all event types
├── event_names.go        # EventName* constants
├── subscribers.go        # Subscriber interfaces
├── stats.go              # ExecutionStats (unchanged)
├── stats_keys.go         # Stat key constants (unchanged)
├── limit.go              # Limit types (unchanged)
├── termination.go        # Termination interface
├── ...
├── events/
│   ├── doc.go            # Package documentation
│   └── registry.go       # Event registry
├── executor/
│   └── executor.go       # Uses ctx.PublishXXX() for events
├── models/
│   └── lcg.go            # Uses ctx.PublishXXX() for model events
├── toolchain/
│   ├── yaml.go           # Uses ctx.PublishXXX() for tool events
│   └── json.go           # Uses ctx.PublishXXX() for tool events
├── termination/
│   ├── text.go           # Uses ctx.PublishXXX() for validator events
│   └── json.go           # Uses ctx.PublishXXX() for validator events
└── format/
    └── xml.go            # Uses ctx.PublishParseError() for parse errors
```

## Testing Strategy

### Unit Tests

1. **PublishXXX methods** — Verify EventName is set correctly for each method
2. **Registry dispatch** — Verify correct subscriber called for each event type
3. **Stats updates** — Verify correct stats incremented for each event type
4. **Recursion limit** — Verify panic at max depth
5. **Concurrency** — Verify thread-safety of publish()

### Integration Tests

1. **Full execution flow** — Verify all events recorded in correct order
2. **Subscriber modifications** — Verify BeforeModelCall request modification works
3. **Limit triggering** — Verify limits work with new event timing
4. **Nested contexts** — Verify event propagation to parent

### Migration Tests

1. **JSON serialization** — Verify events serialize correctly with EventName

## Rollout Plan

1. Implement Phase 1-2 (core types)
2. Implement Phase 3 (ExecutionContext integration)
3. Implement Phase 4-5 (framework updates, docs)
4. Implement Phase 6 (tests)
5. Phase 7 cleanup

This is a breaking change. The old `Trace()` method and hook interfaces will be removed.

## Design Decisions

1. **No backward compatibility shim** — `Trace()` will be removed entirely, not aliased to `Publish()`.
   Users must migrate to `PublishXXX()` methods.

2. **CommonEvent requires EventName** — `PublishCommonEvent(eventName, description, data)` requires
   the caller to specify an event name. No pattern-based subscribers (e.g., "myapp:*").

3. **No async publishing** — All event publishing is synchronous. Keep it simple.
