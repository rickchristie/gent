# Gent Trace Sequencer Implementation Plan

This document is the single source of truth for implementing Gent's reusable event and
stream tracing layer. If implementation details conflict with an earlier proposal, this
document wins.

The feature exists because library users need to display a running Gent execution in a UI,
resume that UI safely, and replay enough recent state to recover from subscription gaps.
The solution must stay generic to Gent. It must not become chat-specific, ReAct-specific,
or application-specific.

## Decision Summary

Build a new package named `trace`.

The package will provide a `trace.Sequencer` that consumes Gent lifecycle events and
`gent.StreamChunk` values, assigns one monotonically increasing event number, publishes live
trace events to subscribers, and maintains an in-memory materialized snapshot.

The sequencer is production observability infrastructure, not a UI debug helper. The name is
`trace` because it describes what the library provides: ordered execution traces.

Core Gent will receive small, explicit correlation metadata additions so tracing is reliable
under nested contexts, asynchronous model completion, parallel model calls, and parallel tool
calls. The trace package must not infer correlation when Gent can provide it directly.

The canonical keys are `ContextId`, `ModelCallId`, and `ToolCallId`. `Source`, `StreamId`, and
`StreamTopicId` are preserved for display and grouping but are not reliable primary keys.

## Design Principles

1. Keep Gent generic.

   Trace types must not contain application, chat, message-list, or ReAct-only fields. Any
   `gent.AgentLoop` must be observable.

2. Prefer explicit correlation.

   Model calls and tool calls need stable IDs. `StreamTopicId` is never unique enough for
   correlation. `StreamId` is useful but still caller-provided, so `ModelCallId` is the
   canonical model-call key.

   Execution contexts also need stable IDs. `Source` is useful for humans but is not unique:
   two child contexts can share the same name under the same parent iteration. Trace keys must
   use `ContextId`, not `Source`.

3. Keep model text opaque.

   The trace package must not parse `<thinking>`, `<action>`, `<answer>`, Markdown headers,
   JSON, YAML, tool syntax, or any model-output protocol. Stream text is bytes of text with
   optional reasoning text.

4. Do not block the agent loop indefinitely.

   Live trace subscriptions use unbounded buffers, matching Gent's stream subscription model.
   This preserves execution progress. Slow subscribers are responsible for consuming or
   unsubscribing.

5. Make raw data opt-in.

   Requests, responses, prompts, tool args, tool output, common payloads, diffs, final output,
   and chunk text can be large or sensitive. They must be controlled by config and redaction.
   Error messages are also sensitive and must go through the redactor.

6. Use straightforward mutation, not event-sourcing framework complexity.

   The sequencer stores current snapshot state and mutates it when each trace event is recorded.
   Recent events provide short replay support. Durable storage and full replay are outside this
   package.

7. Preserve Gent semantics.

   Existing event subscribers, stats, limits, streaming, and executor behavior remain the source
   of truth. Trace observes and normalizes; it does not change agent-loop behavior.

8. Guarantee JSON-safe trace output.

   `trace.Event` and `trace.Snapshot` must always be JSON-marshalable. If a payload cannot be
   captured safely, store `PayloadCaptureError` instead of the original value.

## Non-Goals

The implementation will not provide a database, websocket server, HTTP API, UI renderer,
distributed trace backend, OpenTelemetry exporter, or persistent replay log.

The implementation will not parse or interpret model output sections.

The default subscription will not promise bounded memory for slow live subscribers. Production
fanout can use `SubscribeWithConfig` for bounded buffers and explicit overflow policy.

The implementation will not expose internal mutable snapshot pointers.

## Current Code References

Use these files as the implementation anchors:

| Area | File | Relevant Code |
| --- | --- | --- |
| Event structs | `events.go` | `BaseEvent`, lifecycle event structs |
| Subscriber interfaces | `subscribers.go` | `BeforeExecutionSubscriber`, etc. |
| Event dispatch | `events/registry.go` | `Registry.Dispatch` |
| Event publishing | `context.go` | `publishWithEventIteration`, `populateBaseEvent` |
| Child contexts | `context.go` | `SpawnChild`, `CompleteChild`, `BuildSourcePath` |
| Streams | `model.go` | `StreamChunk` |
| Stream fanout | `stream_hub.go` | unbounded chunk subscriptions |
| Executor lifecycle | `executor/executor.go` | before/after execution and iteration events |
| Canonical model wrapper | `models/lcg.go` | model events and chunk emission |
| ReAct stream IDs | `agents/react/agent.go` | `streamId := fmt.Sprintf("iter-%d", ...)` |

## Core Gent Changes

The trace package depends on these core changes. Implement them first.

### Execution Context Identity

Add opaque context identity to `ExecutionContext` and `gent.BaseEvent`.

`ContextId` is the canonical key for an execution context. `ParentContextId` links a context to
its parent. `Source` remains display-only and must not be used as a uniqueness key.

Add fields and accessors to `ExecutionContext`:

```go
type ExecutionContext struct {
    // existing fields...
    contextId       string
    parentContextId string

    nextModelCallSeq uint64
    nextToolCallSeq  uint64
}

func (ctx *ExecutionContext) ContextId() string
func (ctx *ExecutionContext) ParentContextId() string
```

`NewExecutionContext` must assign a non-empty root `ContextId`. `SpawnChild` must assign a
non-empty child `ContextId` and set `ParentContextId` to the parent context ID.

The generated context ID is opaque. Tests may assert non-empty and distinct, but callers must
not parse it. Recommended generated format:

```text
ctx:<sequence>
```

Use a package-level atomic sequence or an equivalent concurrency-safe generator. The ID only
needs to be unique within the process, but process-wide uniqueness is acceptable and simpler.

Add identity fields to `gent.BaseEvent`:

```go
type BaseEvent struct {
    EventName string
    Timestamp time.Time
    Iteration int
    Depth     int

    // Source is the hierarchical execution path that published the event.
    // Format matches ExecutionContext.BuildSourcePath(), for example:
    // "main/1" or "main/2/research/1".
    // Source is display-only; use ContextId for keys.
    Source string

    // ContextId is the opaque identity of the context that published the event.
    ContextId string

    // ParentContextId is empty only for root contexts.
    ParentContextId string
}
```

`ExecutionContext.populateBaseEvent` must set missing base metadata. It currently runs while
`ctx.mu` is held, so implementation must use locked fields or lock-safe private helpers instead
of calling public methods that take `ctx.mu` again.

```go
if base.Source == "" {
    base.Source = ctx.buildSourcePathForEventLocked()
}
if base.ContextId == "" {
    base.ContextId = ctx.contextId
}
if base.ParentContextId == "" {
    base.ParentContextId = ctx.parentContextId
}
```

Refactor the current repeated switch into a small private helper that returns `*BaseEvent` for
all built-in events. That keeps source population and future base fields in one place.

### StreamChunk Metadata

Extend `gent.StreamChunk`:

```go
type StreamChunk struct {
    Content          string
    ReasoningContent string
    Err              error

    Timestamp time.Time
    Iteration int
    Depth     int
    Source    string

    ContextId       string
    ParentContextId string
    ModelCallId     string
    StreamId        string
    StreamTopicId   string
}
```

`ExecutionContext.EmitChunk` must populate missing metadata before fanout:

```go
if chunk.Timestamp.IsZero() {
    chunk.Timestamp = time.Now()
}
if chunk.Source == "" {
    chunk.Source = ctx.BuildSourcePath()
}
if chunk.Iteration == 0 {
    chunk.Iteration = ctx.Iteration()
}
if chunk.Depth == 0 {
    chunk.Depth = ctx.Depth()
}
if chunk.ContextId == "" {
    chunk.ContextId = ctx.ContextId()
}
if chunk.ParentContextId == "" {
    chunk.ParentContextId = ctx.ParentContextId()
}
```

Child-to-parent stream propagation must preserve these fields. Parent `EmitChunk` must not
replace metadata already populated by the child.

### Model Call Correlation

Add correlation fields to model call events:

```go
type ModelCallRequest struct {
    // Messages is usually []llms.MessageContent, but stays any so custom model
    // implementations can capture their native request shape without importing trace.
    Messages any

    // Options contains meaningful call options when the model wrapper can resolve them.
    // For LangChainGo this should be llms.CallOptions after applying user options.
    Options any

    // OptionCaptureComplete is false when the wrapper cannot introspect every provider
    // option that may affect generation.
    OptionCaptureComplete bool

    // OptionCaptureNotes explains known omissions, for example provider-specific options
    // hidden inside a LangChainGo client.
    OptionCaptureNotes []string
}

type BeforeModelCallEvent struct {
    BaseEvent
    Model    string
    Provider string
    Request any

    ModelCallId   string
    StreamId      string
    StreamTopicId string
}

type AfterModelCallEvent struct {
    BaseEvent
    Model        string
    Provider     string
    Request      any
    Response     *ContentResponse
    InputTokens  int
    OutputTokens int
    Duration     time.Duration
    Error        error

    ModelCallId   string
    StreamId      string
    StreamTopicId string
}
```

Add source-compatible variadic options to model publish methods:

```go
type ModelCallEventOption func(*modelCallEventOptions)

func WithModelCallId(id string) ModelCallEventOption
func WithModelStream(streamId string, streamTopicId string) ModelCallEventOption
func WithModelCallSource(source string) ModelCallEventOption
func WithModelProvider(provider string) ModelCallEventOption

func (ctx *ExecutionContext) PublishBeforeModelCall(
    model string,
    request any,
    opts ...ModelCallEventOption,
) *BeforeModelCallEvent

func (ctx *ExecutionContext) PublishAfterModelCall(
    model string,
    request any,
    response *ContentResponse,
    duration time.Duration,
    err error,
    opts ...ModelCallEventOption,
) *AfterModelCallEvent

func (ctx *ExecutionContext) PublishAfterModelCallForIteration(
    iteration int,
    model string,
    request any,
    response *ContentResponse,
    duration time.Duration,
    err error,
    opts ...ModelCallEventOption,
) *AfterModelCallEvent
```

Existing callers continue to compile because the new parameter is variadic.

If `WithModelCallId` is not provided on `PublishBeforeModelCall`, Gent must generate a stable
opaque ID. The ID only needs to be unique within one execution tree. Callers must not parse it.

Recommended generated format:

```text
<context_id>:model:<sequence>
```

Example:

```text
ctx:1:model:1
ctx:2:model:1
```

`Provider` is optional. It should be populated by model wrappers that know the provider name,
for example `openai`, `anthropic`, `google`, `ollama`, or `bedrock`. `models.LCGWrapper` should
add `WithProvider(provider string)` and pass that value into model call events.

Model request capture must include messages and meaningful call options when available. For
`models.LCGWrapper`, pass a `gent.ModelCallRequest` instead of raw messages. The request should
include the final message list after subscriber modifications and a resolved `llms.CallOptions`
value after applying user options and wrapper-added options. Useful fields include tools or
function schemas, temperature, max tokens, top-p, stop words, reasoning effort, and any other
standard `llms.CallOptions` field available through LangChainGo.

LangChainGo does not expose every provider-specific setting in a uniformly introspectable way.
When a wrapper cannot capture hidden provider/client options, set `OptionCaptureComplete` to
false and explain the limitation in `OptionCaptureNotes`. Do not claim request capture is
complete unless the wrapper can prove it.

`models/lcg.go` must capture the ID, source, and context identity from the before event, put
them on every emitted chunk, and put them on the after event:

```go
request := gent.ModelCallRequest{
    Messages:              messages,
    Options:               resolvedOptions,
    OptionCaptureComplete: false,
    OptionCaptureNotes: []string{
        "LangChainGo may hide provider-specific client options outside llms.CallOptions",
    },
}

beforeEvent := execCtx.PublishBeforeModelCall(
    m.modelName,
    request,
    gent.WithModelStream(streamId, streamTopicId),
    gent.WithModelProvider(m.provider),
)
if modifiedRequest, ok := beforeEvent.Request.(gent.ModelCallRequest); ok {
    request = modifiedRequest
}
requestMessages, _ := request.Messages.([]llms.MessageContent)

modelCallId := beforeEvent.ModelCallId
sourcePath := beforeEvent.Source
callIteration := beforeEvent.Iteration
contextId := beforeEvent.ContextId
parentContextId := beforeEvent.ParentContextId

chunk := gent.StreamChunk{
    Content:         string(contentChunk),
    ModelCallId:     modelCallId,
    StreamId:        streamId,
    StreamTopicId:   streamTopicId,
    Source:          sourcePath,
    Iteration:       callIteration,
    Depth:           beforeEvent.Depth,
    ContextId:       contextId,
    ParentContextId: parentContextId,
}
execCtx.EmitChunk(chunk)

execCtx.PublishAfterModelCallForIteration(
    callIteration,
    m.modelName,
    request,
    response,
    duration,
    err,
    gent.WithModelCallId(modelCallId),
    gent.WithModelStream(streamId, streamTopicId),
    gent.WithModelCallSource(sourcePath),
    gent.WithModelProvider(m.provider),
)
```

The source override is important because the after event can be published asynchronously after
the execution context has advanced to a later iteration. Context IDs still come from the same
execution context and do not need an override.

Missing model-call ID behavior must be explicit. If `PublishAfterModelCall` or
`PublishAfterModelCallForIteration` is called without `WithModelCallId`, Gent may publish the
core after event for compatibility, but trace must not silently invent and complete a new model
call. The sequencer records an uncorrelated `model_call_finished` event with payload, usage,
duration, and redacted error if present. It only completes a `ModelCall` record when the event
has an explicit matching `ModelCallId`, or when fallback correlation is provably unambiguous by
active model call state. If correlation is ambiguous or missing, leave existing model calls
unchanged and do not attach usage or errors to the wrong record.

### Tool Call Correlation

Add correlation fields to tool call events:

```go
type BeforeToolCallEvent struct {
    BaseEvent
    ToolName string
    Args any

    ToolCallId string
}

type AfterToolCallEvent struct {
    BaseEvent
    ToolName string
    Args any
    Output any
    Duration time.Duration
    Error error

    ToolCallId string
}
```

Add source-compatible variadic options to tool publish methods:

```go
type ToolCallEventOption func(*toolCallEventOptions)

func WithToolCallId(id string) ToolCallEventOption
func WithToolCallSource(source string) ToolCallEventOption

func (ctx *ExecutionContext) PublishBeforeToolCall(
    toolName string,
    args any,
    opts ...ToolCallEventOption,
) *BeforeToolCallEvent

func (ctx *ExecutionContext) PublishAfterToolCall(
    toolName string,
    args any,
    output any,
    duration time.Duration,
    err error,
    opts ...ToolCallEventOption,
) *AfterToolCallEvent
```

If `WithToolCallId` is not provided on `PublishBeforeToolCall`, Gent must generate a stable
opaque ID. The ID only needs to be unique within one execution tree. Callers must not parse it.

Recommended generated format:

```text
<context_id>:tool:<sequence>
```

Internal toolchain implementations must propagate the ID from before to after. This is required
for parallel calls with the same tool name.

```go
beforeEvent := execCtx.PublishBeforeToolCall(call.Name, typedInput)
argsToUse := beforeEvent.Args

// Execute tool...

execCtx.PublishAfterToolCall(
    call.Name,
    argsToUse,
    output,
    duration,
    err,
    gent.WithToolCallId(beforeEvent.ToolCallId),
    gent.WithToolCallSource(beforeEvent.Source),
)
```

Update all internal call sites in `toolchain/json.go`, `toolchain/yaml.go`,
`toolchain/search.go`, and `toolchain/output.go` where there is a before/after pair.

### Child Context Event Publishing

`ExecutionContext.SpawnChild` and `ExecutionContext.CompleteChild` currently append
`CommonEvent` values directly to the parent event log. That bypasses subscribers. The trace
sequencer would miss child lifecycle events.

Change both methods to publish through the normal event path. Do not dispatch while holding
`ctx.mu`.

`SpawnChild` must also inherit the parent event publisher:

```go
child := &ExecutionContext{
    // existing fields...
    contextId:       newContextId(),
    parentContextId: ctx.contextId,
    eventPublisher: ctx.eventPublisher,
}
```

This makes root subscribers observe child context events by default. A child executor may still
replace the publisher through `SetEventPublisher`, preserving current configurability.

Expected behavior after this change:

```go
root := gent.NewExecutionContext(ctx, "main", data)
registry := events.NewRegistry().Subscribe(seq)
root.SetEventPublisher(registry)

child := root.SpawnChild("research", childData)
child.PublishBeforeIteration()

// seq receives the child spawn event and the child iteration event.
```

Child spawn and complete events remain Gent `CommonEvent` values for source compatibility, but
their data payload must include explicit context identity so trace can normalize them into
`context_started` and `context_finished` events:

```go
map[string]any{
    "child_context_id":  child.ContextId(),
    "parent_context_id": ctx.ContextId(),
    "child_name":        name,
    "child_source":      child.BuildSourcePath(),
    "child_depth":       child.Depth(),
}
```

`CompleteChild` must include the same child context fields plus termination reason and duration.
The trace sequencer must not treat these child lifecycle events as generic `common` events.

## Trace Package Public API

Create `github.com/rickchristie/gent/trace`.

### Construction

```go
package trace

type Sequencer struct {
    // unexported
}

func NewSequencer(runId string, cfg Config) *Sequencer
```

`runId` is supplied by the caller. The trace package does not generate external run IDs because
applications usually need to align them with their own storage, request, or UI IDs.

### Config

```go
type Config struct {
    IncludeModelRequests  bool
    IncludeModelResponses bool
    IncludeToolArgs       bool
    IncludeToolOutput     bool
    IncludeSystemPrompt   bool
    IncludeCommonPayload  bool
    IncludeDiffPayload    bool
    IncludeChunkText      bool
    IncludeRunOutput      bool

    MaxRecentEvents          int
    MaxRecentLifecycleEvents int
    MaxRecentChunkEvents     int
    MaxModelContentBytes     int
    MaxReasoningContentBytes int

    Redactor Redactor
}
```

Config defaults:

| Field | Zero Value Behavior |
| --- | --- |
| `IncludeModelRequests` | excluded |
| `IncludeModelResponses` | excluded |
| `IncludeToolArgs` | excluded |
| `IncludeToolOutput` | excluded |
| `IncludeSystemPrompt` | excluded |
| `IncludeCommonPayload` | excluded |
| `IncludeDiffPayload` | excluded |
| `IncludeChunkText` | excluded |
| `IncludeRunOutput` | excluded |
| `MaxRecentEvents` | use `DefaultMaxRecentEvents` |
| `MaxRecentLifecycleEvents` | use `DefaultMaxRecentLifecycleEvents` |
| `MaxRecentChunkEvents` | use `DefaultMaxRecentChunkEvents` |
| `MaxModelContentBytes` | use `DefaultMaxModelContentBytes` when chunk text is included |
| `MaxReasoningContentBytes` | use `DefaultMaxReasoningContentBytes` when chunk text is included |
| `Redactor` | no-op redactor |

Constants:

```go
const SchemaVersion = 1

const DefaultMaxRecentEvents = 5000
const DefaultMaxRecentLifecycleEvents = 500
const DefaultMaxRecentChunkEvents = 500
const DefaultMaxModelContentBytes = 64 * 1024
const DefaultMaxReasoningContentBytes = 64 * 1024

const NoRecentEvents = -1
const NoContentLimit = -1
```

Semantics:

`MaxRecentEvents < 0` disables `Snapshot.RecentEvents`.

`MaxRecentEvents == 0` uses `DefaultMaxRecentEvents`.

`MaxRecentLifecycleEvents < 0` disables `Snapshot.RecentLifecycleEvents`.

`MaxRecentLifecycleEvents == 0` uses `DefaultMaxRecentLifecycleEvents`.

`MaxRecentChunkEvents < 0` disables `Snapshot.RecentChunkEvents`.

`MaxRecentChunkEvents == 0` uses `DefaultMaxRecentChunkEvents`.

`Snapshot.RecentEvents` is the only contiguous replay buffer. It is used by `EventsAfter`.
`RecentLifecycleEvents` and `RecentChunkEvents` are UI convenience buffers and are not valid for
stitching because their contents are intentionally filtered.

`MaxModelContentBytes == NoContentLimit` disables the model content cap.

`MaxReasoningContentBytes == NoContentLimit` disables the reasoning content cap.

`MaxModelContentBytes == 0` uses `DefaultMaxModelContentBytes`.

`MaxReasoningContentBytes == 0` uses `DefaultMaxReasoningContentBytes`.

Chunk counters and chunk events are recorded even when `IncludeChunkText` is false. Only chunk
text accumulation and chunk-text payload fields are omitted.

### Redaction

```go
type Redactor interface {
    RedactModelRequest(req any) any
    RedactModelResponse(resp any) any
    RedactToolArgs(args any) any
    RedactToolOutput(output any) any
    RedactSystemPrompt(sections []gent.FormattedSection) any
    RedactCommonPayload(eventName string, payload any) any
    RedactDiffPayload(eventName string, before any, after any, diff string) any
    RedactRunOutput(output []gent.ContentPart) any
    RedactChunk(chunk gent.StreamChunk) gent.StreamChunk
    RedactError(err error) *Error
}
```

Provide a convenience struct for callers that only need to override some methods:

```go
type RedactorFuncs struct {
    ModelRequest  func(any) any
    ModelResponse func(any) any
    ToolArgs      func(any) any
    ToolOutput    func(any) any
    SystemPrompt  func([]gent.FormattedSection) any
    CommonPayload func(string, any) any
    DiffPayload   func(string, any, any, string) any
    RunOutput     func([]gent.ContentPart) any
    Chunk         func(gent.StreamChunk) gent.StreamChunk
    Error         func(error) *Error
}
```

If a non-error function is nil, `RedactorFuncs` returns the input unchanged. If `Error` is nil,
`RedactorFuncs` uses the default error redaction described below.

For errors, the no-op redactor returns:

```go
&Error{
    Message: err.Error(),
    Type:    fmt.Sprintf("%T", err),
}
```

If `RedactError` receives a non-nil error and returns nil, the sequencer must store a generic
redacted error so error counts and status are not lost:

```go
&Error{Message: "redacted error"}
```

Every error path must use `RedactError`, including model errors, tool errors, stream chunk
errors, parse errors, validator-related errors, limit errors, compaction errors, execution
errors, and payload capture errors.

Redactors must be fast, non-blocking, and concurrency-safe. The sequencer invokes redactors on
event dispatch and chunk consumption paths before taking the sequencer lock when possible, so a
custom redactor can be called concurrently from multiple goroutines. If a redactor needs shared
state, it must provide its own synchronization.

### Subscription

```go
func (s *Sequencer) Subscribe() (<-chan *Event, gent.UnsubscribeFunc)
func (s *Sequencer) SubscribeWithConfig(cfg SubscribeConfig) (<-chan *Event, gent.UnsubscribeFunc)

type SubscribeConfig struct {
    BufferSize     int
    OverflowPolicy OverflowPolicy
    OnOverflow     func(OverflowInfo)
}

type OverflowInfo struct {
    Policy             OverflowPolicy
    DroppedEventNumber uint64
    NewEventNumber     uint64
    SubscriberClosed   bool
}

type OverflowPolicy string

const (
    OverflowDropOldest      OverflowPolicy = "drop_oldest"
    OverflowDropNewest      OverflowPolicy = "drop_newest"
    OverflowCloseSubscriber OverflowPolicy = "close_subscriber"
)
```

`Subscribe` returns future events only. Consumers that need resume safety must subscribe first,
buffer live events, then call `Snapshot`.

`Subscribe` uses an unbounded buffer and never blocks `record`. This matches
`ExecutionContext.SubscribeAll`. Memory can grow if a subscriber does not consume events, so
callers must unsubscribe when they are done.

`SubscribeWithConfig` supports bounded subscriptions for websocket or UI fanout code. If
`BufferSize <= 0`, it behaves like `Subscribe`. If `BufferSize > 0`, sending to the subscriber
must still never block `record`; overflow is handled by `OverflowPolicy`.

If `OverflowPolicy` is empty, default to `OverflowCloseSubscriber`. Closing the slow subscriber
is safer than silently dropping trace events for resume-sensitive consumers.

Policy behavior:

| Policy | Behavior |
| --- | --- |
| `OverflowDropOldest` | Drop the oldest queued event and enqueue the new event |
| `OverflowDropNewest` | Drop the new event for this subscriber only |
| `OverflowCloseSubscriber` | Close this subscriber and stop sending to it |

If `OnOverflow` is non-nil, notify it after applying the overflow policy. Invoke the callback
asynchronously so a slow diagnostics hook cannot block `record` or the agent loop. The callback
is per-subscriber diagnostics only; it must not create global trace events because subscriber
transport failure is not part of the agent run. `OnOverflow` must be concurrency-safe because
multiple overflow notifications may run concurrently.

There is intentionally no blocking overflow policy. Trace publishing must not stall agent
execution.

Production applications that bridge trace events to websockets should usually subscribe with a
bounded buffer and use `OverflowCloseSubscriber` or app-level bounded fanout.

### Snapshot

```go
func (s *Sequencer) Snapshot() *Snapshot
```

`Snapshot` returns a copy safe to read while new events are being recorded. All trace-owned
structs and slices must be copied.

`Snapshot` and all `Event` values returned by subscriptions or replay helpers must be
JSON-marshalable. Every payload entering trace state must be converted to JSON-safe data before
it is stored. If a payload cannot be JSON-normalized, store a structured capture error instead
of the original value.

Recommended capture error type:

```go
type PayloadCaptureError struct {
    Type  string `json:"type"`
    Error string `json:"error"`
}
```

Recommended implementation:

```go
func jsonSafeValue(value any) any {
    if value == nil {
        return nil
    }
    data, err := json.Marshal(value)
    if err != nil {
        return PayloadCaptureError{Type: fmt.Sprintf("%T", value), Error: err.Error()}
    }
    var decoded any
    if err := json.Unmarshal(data, &decoded); err != nil {
        return PayloadCaptureError{Type: fmt.Sprintf("%T", value), Error: err.Error()}
    }
    return decoded
}
```

### Stream Consumption

```go
func (s *Sequencer) ObserveStreams(execCtx *gent.ExecutionContext) gent.UnsubscribeFunc
func (s *Sequencer) ConsumeChunk(chunk gent.StreamChunk)
func (s *Sequencer) ConsumeChunks(ctx context.Context, chunks <-chan gent.StreamChunk)
```

`ObserveStreams` subscribes to `execCtx.SubscribeAll()` and consumes chunks until the stream
subscription closes or the returned unsubscribe function is called.

`ObserveStreams` is idempotent per execution context ID. The sequencer must avoid duplicate
stream observation when both a root and child context are visible. Because child chunks
propagate to the parent stream hub, observing the root context is sufficient for the full
execution tree.

Auto-observation rule:

When `Sequencer.OnBeforeExecution` sees an execution context with no observed ancestor, it calls
`ObserveStreams(execCtx)`. This supports normal root execution and standalone child execution
without duplicating propagated child chunks.

### Close

```go
func (s *Sequencer) Close()
```

`Close` closes all live subscription buffers and unsubscribes stream observers. It does not
rewrite the snapshot status. Run status changes only from Gent events.

Calling `Close` more than once is safe.

### Resume And Replay Helpers

```go
func CanStitch(snapshotLastEventNumber uint64, firstBufferedEventNumber uint64) bool {
    return firstBufferedEventNumber <= snapshotLastEventNumber+1
}

func ValidateStitch(snapshotLastEventNumber uint64, events []*Event) StitchResult

func (s *Sequencer) EventsAfter(lastEventNumber uint64) ([]*Event, error)

type StitchStatus string

const (
    StitchStatusOK       StitchStatus = "ok"
    StitchStatusGap      StitchStatus = "gap"
    StitchStatusUnsorted StitchStatus = "unsorted"
)

type StitchResult struct {
    Status              StitchStatus `json:"status"`
    SnapshotLastEvent   uint64       `json:"snapshotLastEvent"`
    FirstBufferedEvent  uint64       `json:"firstBufferedEvent,omitempty"`
    MissingAfterEvent   uint64       `json:"missingAfterEvent,omitempty"`
    MissingBeforeEvent  uint64       `json:"missingBeforeEvent,omitempty"`
}
```

`CanStitch` is a small compatibility helper for callers that only have the first buffered event.
Implementation and documentation examples must use `ValidateStitch` instead.

`ValidateStitch` verifies that buffered events are sorted by `EventNumber`, have no internal
gaps, and can be applied to a snapshot without missing events. Events with
`EventNumber <= snapshotLastEventNumber` are allowed and ignored by replay callers. The first
event greater than `snapshotLastEventNumber` must be exactly `snapshotLastEventNumber + 1`.
An empty event slice is valid and returns `StitchStatusOK`.

`EventsAfter` returns a copy of all contiguous events in the sequencer's replay buffer with
`EventNumber > lastEventNumber`. If the requested event range has been evicted, return a clear
sentinel error:

```go
var ErrReplayUnavailable = errors.New("trace replay unavailable")
```

`EventsAfter` uses `Snapshot.RecentEvents`, not `RecentLifecycleEvents` or `RecentChunkEvents`.
It must never synthesize missing events from the materialized snapshot.

If `lastEventNumber == Snapshot.LastEventNumber`, `EventsAfter` returns an empty slice and nil
error.

UI resume flow:

```go
events, unsubscribe := seq.Subscribe()
defer unsubscribe()

var bufferedMu sync.Mutex
buffered := make([]*trace.Event, 0)
go func() {
    for event := range events {
        bufferedMu.Lock()
        buffered = append(buffered, event)
        bufferedMu.Unlock()
    }
}()

snapshot := seq.Snapshot()

bufferedMu.Lock()
eventsToApply := append([]*trace.Event(nil), buffered...)
bufferedMu.Unlock()

stitch := trace.ValidateStitch(snapshot.LastEventNumber, eventsToApply)
if stitch.Status != trace.StitchStatusOK {
    snapshot = seq.Snapshot()
    eventsToApply, _ = seq.EventsAfter(snapshot.LastEventNumber)
}

renderSnapshot(snapshot)
for _, event := range eventsToApply {
    if event.EventNumber > snapshot.LastEventNumber {
        applyEvent(event)
    }
}
```

If buffered events do not stitch and `EventsAfter` returns `ErrReplayUnavailable`, the
application should refetch the snapshot or request replay from a storage layer outside this
package.

## Trace Types

### Event

```go
type Event struct {
    EventNumber uint64    `json:"eventNumber"`
    RunId       string    `json:"runId"`
    Ts          time.Time `json:"ts"`
    Type        EventType `json:"type"`

    EventName string `json:"eventName,omitempty"`
    Iteration int    `json:"iteration,omitempty"`
    Depth     int    `json:"depth,omitempty"`
    Source    string `json:"source,omitempty"`

    ContextId       string `json:"contextId,omitempty"`
    ParentContextId string `json:"parentContextId,omitempty"`

    ModelCallId   string `json:"modelCallId,omitempty"`
    ToolCallId    string `json:"toolCallId,omitempty"`
    StreamId      string `json:"streamId,omitempty"`
    StreamTopicId string `json:"streamTopicId,omitempty"`

    Payload any    `json:"payload,omitempty"`
    Error   *Error `json:"error,omitempty"`
}
```

Use `EventName` to preserve the original Gent event name. Use `Type` for normalized trace UI
behavior.

Event type constants:

```go
type EventType string

const (
    EventTypeExecutionStarted  EventType = "execution_started"
    EventTypeExecutionFinished EventType = "execution_finished"
    EventTypeContextStarted    EventType = "context_started"
    EventTypeContextFinished   EventType = "context_finished"
    EventTypeIterationStarted  EventType = "iteration_started"
    EventTypeIterationFinished EventType = "iteration_finished"
    EventTypeSystemPrompt      EventType = "system_prompt"
    EventTypeModelCallStarted  EventType = "model_call_started"
    EventTypeModelCallFinished EventType = "model_call_finished"
    EventTypeModelStreamChunk  EventType = "model_stream_chunk"
    EventTypeToolCallStarted   EventType = "tool_call_started"
    EventTypeToolCallFinished  EventType = "tool_call_finished"
    EventTypeParseError        EventType = "parse_error"
    EventTypeValidatorCalled   EventType = "validator_called"
    EventTypeValidatorResult   EventType = "validator_result"
    EventTypeError             EventType = "error"
    EventTypeLimitExceeded     EventType = "limit_exceeded"
    EventTypeCompaction        EventType = "compaction"
    EventTypeCommon            EventType = "common"
    EventTypeCommonDiff        EventType = "common_diff"
)
```

### Snapshot

```go
type Snapshot struct {
    SchemaVersion int       `json:"schemaVersion"`
    RunId         string    `json:"runId"`
    Status        RunStatus `json:"status"`

    StartedTs    time.Time  `json:"startedTs"`
    LastUpdatedTs time.Time `json:"lastUpdatedTs"`
    CompletedTs  *time.Time `json:"completedTs,omitempty"`

    LastEventNumber uint64 `json:"lastEventNumber"`

    Result *RunResult `json:"result,omitempty"`
    Stats  RunStats   `json:"stats"`

    Contexts   []*Context   `json:"contexts,omitempty"`
    Iterations []*Iteration `json:"iterations,omitempty"`
    ModelCalls []*ModelCall `json:"modelCalls,omitempty"`
    ToolCalls  []*ToolCall  `json:"toolCalls,omitempty"`
    Errors     []*Error     `json:"errors,omitempty"`

    RecentEvents          []*Event `json:"recentEvents,omitempty"`
    RecentLifecycleEvents []*Event `json:"recentLifecycleEvents,omitempty"`
    RecentChunkEvents     []*Event `json:"recentChunkEvents,omitempty"`
}
```

Run status constants:

```go
type RunStatus string

const (
    RunStatusPending       RunStatus = "pending"
    RunStatusRunning       RunStatus = "running"
    RunStatusSucceeded     RunStatus = "succeeded"
    RunStatusFailed        RunStatus = "failed"
    RunStatusCanceled      RunStatus = "canceled"
    RunStatusLimitExceeded RunStatus = "limit_exceeded"
)
```

### RunResult

```go
type RunResult struct {
    TerminationReason gent.TerminationReason `json:"terminationReason"`
    Output            any                    `json:"output,omitempty"`
    Error             *Error                 `json:"error,omitempty"`
}
```

`Output` is only populated when `Config.IncludeRunOutput` is true.

### Context

```go
type Context struct {
    Id       string `json:"id"`
    ParentId string `json:"parentId,omitempty"`
    Name     string `json:"name,omitempty"`
    Source   string `json:"source,omitempty"`
    Depth    int    `json:"depth,omitempty"`

    Status StepStatus `json:"status"`

    StartedTs    time.Time  `json:"startedTs"`
    LastUpdatedTs time.Time `json:"lastUpdatedTs"`
    CompletedTs  *time.Time `json:"completedTs,omitempty"`
    DurationMs   int64      `json:"durationMs,omitempty"`

    StartedEventNumber   uint64 `json:"startedEventNumber"`
    LastEventNumber      uint64 `json:"lastEventNumber"`
    CompletedEventNumber uint64 `json:"completedEventNumber,omitempty"`

    Error *Error `json:"error,omitempty"`
}
```

Root contexts are created from `BeforeExecutionEvent`. Child contexts are created from
`EventNameChildSpawn` common events and completed from `EventNameChildComplete` common events.
The sequencer emits normalized `context_started` and `context_finished` trace events for these
context lifecycle changes.

### Iteration

```go
type Iteration struct {
    Iteration int    `json:"iteration"`
    Depth     int    `json:"depth"`
    Source    string `json:"source,omitempty"`

    ContextId       string `json:"contextId"`
    ParentContextId string `json:"parentContextId,omitempty"`

    Status StepStatus `json:"status"`

    StartedTs    time.Time  `json:"startedTs"`
    LastUpdatedTs time.Time `json:"lastUpdatedTs"`
    CompletedTs  *time.Time `json:"completedTs,omitempty"`
    DurationMs   int64      `json:"durationMs,omitempty"`

    StartedEventNumber   uint64 `json:"startedEventNumber"`
    LastEventNumber      uint64 `json:"lastEventNumber"`
    CompletedEventNumber uint64 `json:"completedEventNumber,omitempty"`

    ModelCallIds []string `json:"modelCallIds,omitempty"`
    ToolCallIds  []string `json:"toolCallIds,omitempty"`

    Result any    `json:"result,omitempty"`
    Error  *Error `json:"error,omitempty"`
}
```

Iterations are keyed by `(ContextId, Iteration)`. `Source` is display-only and must not be used
as a key.

### ModelCall

```go
type ModelCall struct {
    Id            string `json:"id"`
    Model         string `json:"model"`
    Provider      string `json:"provider,omitempty"`
    StreamId      string `json:"streamId,omitempty"`
    StreamTopicId string `json:"streamTopicId,omitempty"`
    Source        string `json:"source,omitempty"`
    Iteration     int    `json:"iteration,omitempty"`
    Depth         int    `json:"depth,omitempty"`

    ContextId       string `json:"contextId"`
    ParentContextId string `json:"parentContextId,omitempty"`

    Status StepStatus `json:"status"`

    StartedTs    time.Time  `json:"startedTs"`
    LastUpdatedTs time.Time `json:"lastUpdatedTs"`
    CompletedTs  *time.Time `json:"completedTs,omitempty"`
    DurationMs   int64      `json:"durationMs,omitempty"`

    StartedEventNumber   uint64 `json:"startedEventNumber"`
    LastEventNumber      uint64 `json:"lastEventNumber"`
    CompletedEventNumber uint64 `json:"completedEventNumber,omitempty"`

    Request  any          `json:"request,omitempty"`
    Response any          `json:"response,omitempty"`
    Usage    *ModelUsage  `json:"usage,omitempty"`
    Stream   *ModelStream `json:"stream,omitempty"`
    Error    *Error       `json:"error,omitempty"`
}

type ModelStream struct {
    Content          string `json:"content,omitempty"`
    ReasoningContent string `json:"reasoningContent,omitempty"`

    ChunkCount int `json:"chunkCount"`

    ContentTruncated          bool `json:"contentTruncated,omitempty"`
    ReasoningContentTruncated bool `json:"reasoningContentTruncated,omitempty"`

    LastChunkEventNumber uint64 `json:"lastChunkEventNumber,omitempty"`
}

type ModelUsage struct {
    InputTokens       int `json:"inputTokens"`
    OutputTokens      int `json:"outputTokens"`
    ReasoningTokens   int `json:"reasoningTokens,omitempty"`
    CachedInputTokens int `json:"cachedInputTokens,omitempty"`
    TotalTokens       int `json:"totalTokens"`
}
```

When `Config.IncludeModelRequests` is true, `ModelCall.Request` should contain a JSON-safe,
redacted `gent.ModelCallRequest` when the model wrapper provides one. This is the primary UI
surface for prompt debugging: messages plus captured call options. If the model wrapper provides
only raw messages or a custom request shape, store the redacted JSON-safe value as-is.

Model stream content is accumulated only when `IncludeChunkText` is true.

Model content and reasoning limits are byte limits, but stored strings must remain valid UTF-8.
When truncating, cut at the last valid UTF-8 boundary at or before the byte limit. Do not store
invalid UTF-8 and do not rely on JSON marshaling to replace broken bytes later. If the limit
falls before any complete rune, store an empty string and set the truncation flag.

If a chunk has `Err`, record an error on the event and on the model call when it can be
correlated.

Correlation order for chunks:

1. Attach by `ModelCallId` when present.
2. If no `ModelCallId`, attach by `StreamId` only when exactly one model call in the snapshot
   has that `StreamId` and is not completed.
3. If neither rule is safe, keep the chunk as a trace event but do not attach it to a model
   call. Do not attach by `StreamTopicId`.

### ToolCall

```go
type ToolCall struct {
    Id        string `json:"id"`
    Name      string `json:"name"`
    Source    string `json:"source,omitempty"`
    Iteration int    `json:"iteration,omitempty"`
    Depth     int    `json:"depth,omitempty"`

    ContextId       string `json:"contextId"`
    ParentContextId string `json:"parentContextId,omitempty"`

    Status StepStatus `json:"status"`

    StartedTs    time.Time  `json:"startedTs"`
    LastUpdatedTs time.Time `json:"lastUpdatedTs"`
    CompletedTs  *time.Time `json:"completedTs,omitempty"`
    DurationMs   int64      `json:"durationMs,omitempty"`

    StartedEventNumber   uint64 `json:"startedEventNumber"`
    LastEventNumber      uint64 `json:"lastEventNumber"`
    CompletedEventNumber uint64 `json:"completedEventNumber,omitempty"`

    Args   any    `json:"args,omitempty"`
    Output any    `json:"output,omitempty"`
    Error  *Error `json:"error,omitempty"`
}
```

Tool calls are keyed by `ToolCallId`. Fallback correlation by name, context ID, and iteration is
only allowed when it is unambiguous. Parallel calls with the same name require `ToolCallId`.

Missing tool-call ID behavior must be explicit. If `PublishAfterToolCall` is called without
`WithToolCallId`, Gent may publish the core after event for compatibility, but trace must not
silently invent and complete a new tool call. The sequencer records an uncorrelated
`tool_call_finished` event with payload, duration, output when configured, and redacted error if
present. It only completes a `ToolCall` record when the event has an explicit matching
`ToolCallId`, or when fallback correlation by name, context ID, and iteration is provably
unambiguous. If correlation is ambiguous or missing, leave existing tool calls unchanged.

### StepStatus

```go
type StepStatus string

const (
    StepStatusRunning   StepStatus = "running"
    StepStatusSucceeded StepStatus = "succeeded"
    StepStatusFailed    StepStatus = "failed"
    StepStatusCanceled  StepStatus = "canceled"
)
```

### Error

```go
type Error struct {
    Message string `json:"message"`
    Type    string `json:"type,omitempty"`

    EventNumber uint64    `json:"eventNumber,omitempty"`
    Ts          time.Time `json:"ts,omitempty"`
    EventName   string    `json:"eventName,omitempty"`
    Source      string    `json:"source,omitempty"`
    Iteration   int       `json:"iteration,omitempty"`
    Depth       int       `json:"depth,omitempty"`

    ContextId       string `json:"contextId,omitempty"`
    ParentContextId string `json:"parentContextId,omitempty"`

    ModelCallId string `json:"modelCallId,omitempty"`
    ToolCallId  string `json:"toolCallId,omitempty"`
}
```

Every `Error` must be produced through `Redactor.RedactError`. The sequencer then fills trace
metadata fields such as event number, source, context ID, model call ID, and tool call ID when
they are empty.

### RunStats

```go
type RunStats struct {
    ContextCount   int `json:"contextCount"`
    IterationCount int `json:"iterationCount"`
    ModelCallCount int `json:"modelCallCount"`
    ToolCallCount  int `json:"toolCallCount"`

    InputTokens       int `json:"inputTokens"`
    OutputTokens      int `json:"outputTokens"`
    ReasoningTokens   int `json:"reasoningTokens"`
    CachedInputTokens int `json:"cachedInputTokens"`
    TotalTokens       int `json:"totalTokens"`

    DurationMs      int64 `json:"durationMs"`
    ModelDurationMs int64 `json:"modelDurationMs"`
    ToolDurationMs  int64 `json:"toolDurationMs"`

    ParseErrorCount         int `json:"parseErrorCount"`
    ValidatorRejectionCount int `json:"validatorRejectionCount"`
    ErrorCount              int `json:"errorCount"`
    LimitExceededCount      int `json:"limitExceededCount"`
    CompactionCount         int `json:"compactionCount"`
}
```

`RunStats` is derived from sequenced events, not by directly copying `ExecutionStats`. This keeps
snapshot state consistent with `LastEventNumber`.

## Sequencer Internals

### Required Fields

The exact private layout may vary, but it must support these responsibilities:

```go
type Sequencer struct {
    mu sync.RWMutex

    runId string
    cfg   Config

    closed bool
    nextEventNumber uint64

    snapshot Snapshot

    recentEvents []*Event
    recentLifecycleEvents []*Event
    recentChunkEvents []*Event

    maxRecentEvents int
    maxRecentLifecycleEvents int
    maxRecentChunkEvents int

    maxModelContentBytes int
    maxReasoningContentBytes int

    contextById map[string]*Context
    modelById map[string]*ModelCall
    modelIdsByStreamId map[string][]string
    toolById map[string]*ToolCall
    iterationByKey map[string]*Iteration

    liveSubscribers map[uint64]*liveSubscriber
    nextLiveSubscriberId uint64

    streamObservers map[string]gent.UnsubscribeFunc
}
```

Maps and snapshot slices point at the authoritative records while `mu` is held. `Snapshot()`
must clone them before returning.

### Record Path

All event number assignment must happen in exactly one method. Recommended name:

```go
func (s *Sequencer) record(input normalizedEvent) *Event
```

Rules:

1. Normalize and redact outside the lock when possible.
2. Lock `s.mu`.
3. If closed, return nil.
4. Increment `nextEventNumber`.
5. Build `trace.Event` with the new number.
6. Apply the event to the snapshot.
7. Set `Snapshot.LastEventNumber` to the event number.
8. Set `Snapshot.LastUpdatedTs` to the event timestamp.
9. Append to `RecentEvents`, `RecentLifecycleEvents`, and `RecentChunkEvents` as applicable.
10. Cap each recent-event buffer according to config.
11. Send the event to all live subscriber buffers using each subscriber's overflow policy.
12. Unlock.

Do not assign event numbers in subscriber methods, chunk methods, or snapshot mutation helpers.

### Ordering Semantics

The sequencer guarantees that event numbers are strictly increasing in the order calls enter
`record`.

Gent events and stream chunks may be published from different goroutines. The sequencer does
not claim to reconstruct external wall-clock causality between concurrent goroutines. It only
provides one total order for UI replay and snapshot stitching.

Use Gent event timestamps for lifecycle events. Use `StreamChunk.Timestamp` for chunk events.
If a chunk timestamp is zero, `ConsumeChunk` must set it to `time.Now()` before recording.

### Snapshot Mutation

Every recorded event updates `Snapshot.LastEventNumber`. Step records also update their own
`LastEventNumber`.

Root `BeforeExecutionEvent` creates or updates a `Context` record and emits
`context_started`. Root `AfterExecutionEvent` completes that context and emits
`context_finished`. Child spawn and complete common events do the same for child contexts.

When a before event starts a step, create the step with status `running`.

When an after event completes a step, set status to `succeeded` if error is nil, otherwise
`failed`. Set `CompletedTs`, `DurationMs`, and `CompletedEventNumber`.

When an error event can be associated with a model call, tool call, iteration, or run, attach
the error there and append it to `Snapshot.Errors`.

Limit exceeded events set run status to `limit_exceeded` and append an error-like record to
`Snapshot.Errors` with the limit details in the message.

After execution sets final run status from `gent.TerminationReason`:

| Gent TerminationReason | Trace RunStatus |
| --- | --- |
| `TerminationSuccess` | `succeeded` |
| `TerminationError` | `failed` |
| `TerminationContextCanceled` | `canceled` |
| `TerminationLimitExceeded` | `limit_exceeded` |
| `TerminationCompactionFailed` | `failed` |

### Event Payloads

Payloads must be small, JSON-safe structs or maps. Do not store raw Gent event pointers. Every
payload must pass through `jsonSafeValue` before it is stored on a trace event or snapshot.

Recommended payload mapping:

| Trace Type | Payload |
| --- | --- |
| `execution_started` | nil |
| `execution_finished` | termination reason, optional run output, error |
| `context_started` | context ID, parent context ID, name, source, depth |
| `context_finished` | context ID, parent context ID, termination reason, duration ms, error |
| `iteration_started` | nil |
| `iteration_finished` | loop action, duration ms, optional result |
| `system_prompt` | section count and names; sections only when `IncludeSystemPrompt` |
| `model_call_started` | model, provider, stream IDs, optional request with messages and options |
| `model_call_finished` | model, provider, duration ms, usage, optional response, error |
| `model_stream_chunk` | stream IDs, content fields only when `IncludeChunkText`, error |
| `tool_call_started` | tool name, optional args |
| `tool_call_finished` | tool name, duration ms, optional output, error |
| `parse_error` | parse error type, optional raw content, error |
| `validator_called` | validator name; answer omitted by default |
| `validator_result` | validator name, accepted, feedback names/counts |
| `error` | error |
| `limit_exceeded` | limit, current value, matched key |
| `compaction` | scratchpad lengths and duration ms |
| `common` | event name, description, optional payload |
| `common_diff` | event name, optional before/after/diff payload |

`ParseErrorEvent.RawContent` can be large and may contain model output. Do not include it unless
there is an explicit config field added later. For the initial implementation, omit raw parse
content and record only error type and error message.

## Subscriber Implementation

`Sequencer` implements all current Gent subscriber interfaces:

```go
var _ gent.BeforeExecutionSubscriber = (*Sequencer)(nil)
var _ gent.AfterExecutionSubscriber = (*Sequencer)(nil)
var _ gent.BeforeIterationSubscriber = (*Sequencer)(nil)
var _ gent.AfterIterationSubscriber = (*Sequencer)(nil)
var _ gent.BeforeSystemPromptSubscriber = (*Sequencer)(nil)
var _ gent.BeforeModelCallSubscriber = (*Sequencer)(nil)
var _ gent.AfterModelCallSubscriber = (*Sequencer)(nil)
var _ gent.BeforeToolCallSubscriber = (*Sequencer)(nil)
var _ gent.AfterToolCallSubscriber = (*Sequencer)(nil)
var _ gent.ParseErrorSubscriber = (*Sequencer)(nil)
var _ gent.ValidatorCalledSubscriber = (*Sequencer)(nil)
var _ gent.ValidatorResultSubscriber = (*Sequencer)(nil)
var _ gent.ErrorSubscriber = (*Sequencer)(nil)
var _ gent.LimitExceededSubscriber = (*Sequencer)(nil)
var _ gent.CompactionSubscriber = (*Sequencer)(nil)
var _ gent.CommonEventSubscriber = (*Sequencer)(nil)
var _ gent.CommonDiffEventSubscriber = (*Sequencer)(nil)
```

Do not expose `Subscriber() any`. The sequencer itself is the subscriber:

```go
seq := trace.NewSequencer("run-123", trace.Config{})
registry := events.NewRegistry().Subscribe(seq)
exec := executor.New(agent, executor.Config{Events: registry})
exec.Execute(execCtx)
```

Subscriber methods may record more than one trace event for one Gent event when that preserves
clear UI semantics. Required multi-event normalization:

| Gent Event | Trace Events In Order |
| --- | --- |
| `BeforeExecutionEvent` | `context_started`, then `execution_started` |
| `AfterExecutionEvent` | `execution_finished`, then `context_finished` |
| `CommonEvent` with `EventNameChildSpawn` | `context_started` only |
| `CommonEvent` with `EventNameChildComplete` | `context_finished` only |

Child context lifecycle common events must not also be recorded as `common` trace events.

## Usage Examples

### Basic Execution Trace

```go
seq := trace.NewSequencer("run-123", trace.Config{
    IncludeChunkText: true,
})

registry := events.NewRegistry().Subscribe(seq)
exec := executor.New(agent, executor.Config{Events: registry})

exec.Execute(execCtx)

snapshot := seq.Snapshot()
fmt.Println(snapshot.LastEventNumber)
```

### Live UI Subscription With Resume

```go
seq := trace.NewSequencer("run-123", trace.Config{
    IncludeChunkText:  true,
    MaxRecentEvents:   1000,
    IncludeToolOutput: false,
})

eventsCh, unsubscribe := seq.Subscribe()
defer unsubscribe()

var bufferedMu sync.Mutex
buffered := make([]*trace.Event, 0)
go func() {
    for event := range eventsCh {
        bufferedMu.Lock()
        buffered = append(buffered, event)
        bufferedMu.Unlock()
    }
}()

snapshot := seq.Snapshot()

bufferedMu.Lock()
eventsToApply := append([]*trace.Event(nil), buffered...)
bufferedMu.Unlock()

stitch := trace.ValidateStitch(snapshot.LastEventNumber, eventsToApply)
if stitch.Status != trace.StitchStatusOK {
    snapshot = seq.Snapshot()
    eventsToApply, _ = seq.EventsAfter(snapshot.LastEventNumber)
}

renderSnapshot(snapshot)
for _, event := range eventsToApply {
    if event.EventNumber > snapshot.LastEventNumber {
        applyEvent(event)
    }
}
```

### Bounded Websocket Fanout

```go
eventsCh, unsubscribe := seq.SubscribeWithConfig(trace.SubscribeConfig{
    BufferSize:     1024,
    OverflowPolicy: trace.OverflowCloseSubscriber,
})
defer unsubscribe()

for event := range eventsCh {
    if err := websocket.WriteJSON(event); err != nil {
        unsubscribe()
        return err
    }
}
```

### Redacted Raw Data

```go
seq := trace.NewSequencer("run-123", trace.Config{
    IncludeModelRequests:  true,
    IncludeModelResponses: true,
    IncludeToolArgs:       true,
    IncludeToolOutput:     true,
    Redactor: trace.RedactorFuncs{
        ModelRequest: func(req any) any {
            return redactMessages(req)
        },
        ToolArgs: func(args any) any {
            return redactSecrets(args)
        },
        ToolOutput: func(output any) any {
            return truncateLargeOutput(output)
        },
        Error: func(err error) *trace.Error {
            return &trace.Error{Message: redactErrorMessage(err.Error())}
        },
    },
})
```

### Manual Stream Observation

This is only needed when stream chunks should be observed without executing through a registry
that includes the sequencer.

```go
seq := trace.NewSequencer("run-123", trace.Config{IncludeChunkText: true})

unsubscribeStreams := seq.ObserveStreams(execCtx)
defer unsubscribeStreams()

// Chunks emitted through execCtx.EmitChunk are now sequenced.
```

## Implementation File Plan

Core Gent changes:

| File | Work |
| --- | --- |
| `events.go` | Add context IDs, source, provider, model IDs, tool IDs, stream metadata |
| `model.go` | Add `ModelCallRequest`; add timestamp, context IDs, iteration, depth to chunks |
| `context.go` | Generate context IDs, populate metadata, generated IDs, event options |
| `context.go` | Publish child spawn/complete through normal event path |
| `subscribers.go` | No API change expected |
| `events/registry.go` | No API change expected |
| `models/lcg.go` | Add provider config; propagate model call ID/context/source to chunks |
| `toolchain/*.go` | Propagate tool call ID/context/source from before to after |
| `internal/tt/mocks.go` | Propagate model call IDs for tests where needed |

Trace package files:

| File | Work |
| --- | --- |
| `trace/doc.go` | Package documentation and basic example |
| `trace/types.go` | Public trace structs and constants |
| `trace/config.go` | Config defaulting |
| `trace/redactor.go` | Redactor interfaces, error redaction, and helpers |
| `trace/sequencer.go` | Constructor, subscribe, bounded subscribe, overflow callback, record path |
| `trace/subscribers.go` | Gent subscriber method implementations |
| `trace/streams.go` | stream observation and chunk consumption |
| `trace/snapshot.go` | snapshot mutation and clone helpers |
| `trace/payload.go` | payload normalization and capture-error helpers |
| `trace/stitch.go` | `CanStitch`, `ValidateStitch`, and replay errors |

Keep functions small and direct. Prefer intent-revealing helpers such as
`applyModelCallStarted`, `applyModelChunk`, and `cloneModelCall` over a generic reflection
dispatcher.

## Test Plan

The trace feature is used to troubleshoot agent behavior. A broken trace can hide the real
agent bug or invent a fake one. The test suite must therefore prove the trace layer is reliable,
not merely exercise a few public methods.

Passing tests must give high confidence that the next release will work for real users without
follow-up bug-fix cycles. Every public behavior in this document needs direct tests or an
integration test that covers it through real Gent execution paths.

### Test Reliability Bar

1. Tests must be deterministic. Do not rely on arbitrary sleeps, goroutine scheduling luck, or
   wall-clock timing to prove ordering or non-blocking behavior.
2. Use table-driven tests with named `input`, `mocks`, and `expected` structs for multi-scenario
   functions.
3. Expected outputs must be full matches. Do not use `Contains`, prefix checks, or partial
   struct assertions when the complete expected output is known.
4. Snapshot tests must compare complete snapshots after normalizing expected timestamps that are
   intentionally dynamic.
5. Timestamp assertions must use `>= expectedTime` or explicit injected timestamps. Do not assert
   only that a timestamp is non-zero.
6. Concurrency tests must use barriers, channels, wait groups, and bounded completion timeouts.
   They must not use sleeps as synchronization.
7. Every test that stores or returns trace events or snapshots must verify JSON marshalability.
8. Every error path must verify redaction. Tests must fail if a raw error message leaks when a
   redactor is configured.
9. Tests must call public APIs where possible. Private helpers may be tested only for pure logic
   that cannot be reached clearly through public behavior.
10. Tests must be run with `-count=1` so stale cache results cannot hide regressions.
11. Race-sensitive code must have race-detector coverage on targeted trace and core packages.

### Required Test Helpers

Create small test helpers to keep assertions complete and readable:

```go
func assertJSONSafe(t *testing.T, value any)
func assertEventNumbersContiguous(t *testing.T, events []*trace.Event)
func assertSnapshotInvariants(t *testing.T, snapshot *trace.Snapshot)
func drainTraceEvents(t *testing.T, ch <-chan *trace.Event, count int) []*trace.Event
func requireNoEvent(t *testing.T, ch <-chan *trace.Event)
```

`assertSnapshotInvariants` must verify at least:

1. `SchemaVersion == trace.SchemaVersion`.
2. `RunId` is non-empty.
3. `LastUpdatedTs >= StartedTs`.
4. `LastEventNumber` equals the largest event number in `RecentEvents` when recent events are
   enabled and no recent events were evicted.
5. Every `Context`, `Iteration`, `ModelCall`, `ToolCall`, and `Error` has a valid context ID
   when the source Gent event had one.
6. Every step's `StartedEventNumber <= LastEventNumber`.
7. Completed steps have `CompletedTs`, `DurationMs`, and `CompletedEventNumber` set.
8. `RunStats` counts match the records stored in the snapshot.
9. `RecentEvents` is strictly increasing and contiguous.
10. `RecentLifecycleEvents` and `RecentChunkEvents` are strictly increasing.
11. The snapshot is JSON-marshalable.

### Core Gent Unit Tests

Core tests prove Gent emits enough identity and correlation metadata for trace to be reliable.

1. Root context identity:
   `NewExecutionContext` assigns a non-empty `ContextId` and empty `ParentContextId`.
2. Child context identity:
   `SpawnChild` assigns a non-empty distinct `ContextId` and sets `ParentContextId` to the
   parent's `ContextId`.
3. Same-name child collision prevention:
   two children with the same name under the same parent iteration get distinct context IDs.
4. Base event metadata:
   every built-in `PublishXXX` method populates timestamp, iteration, depth, source,
   `ContextId`, and `ParentContextId`.
5. Base event override preservation:
   source overrides used for late model completion are preserved while missing context IDs are
   populated from the execution context.
6. Source remains display-only:
   two same-name child contexts can have colliding or similar sources without sharing context IDs.
7. Chunk metadata:
   `EmitChunk` populates timestamp, iteration, depth, source, `ContextId`, and
   `ParentContextId` when missing.
8. Chunk propagation:
   child-to-parent stream propagation preserves child context metadata and does not overwrite it
   with parent metadata.
9. Stream subscriptions:
   `SubscribeAll`, `SubscribeToStream`, and `SubscribeToTopic` still receive the correct chunks
   after metadata fields are added.
10. Model call ID generation:
   `PublishBeforeModelCall` generates a non-empty `ModelCallId` when one is not provided.
11. Model call option preservation:
   explicit model call ID, provider, stream ID, topic ID, and source are preserved on before and
   after events.
12. Late model completion:
   `PublishAfterModelCallForIteration` preserves the started iteration and source.
13. Tool call ID generation:
   `PublishBeforeToolCall` generates a non-empty `ToolCallId` when one is not provided.
14. Tool call option preservation:
   explicit tool call ID and source are preserved on before and after events.
15. Child event dispatch:
   `SpawnChild` dispatches child spawn through subscribers, not only the internal event log.
16. Child completion dispatch:
   `CompleteChild` dispatches child completion through subscribers.
17. Event publisher inheritance:
   child contexts inherit the parent event publisher unless a child executor replaces it.
18. Child lifecycle payloads:
   child spawn and complete common events include child context ID, parent context ID, child name,
   child source, child depth, termination reason, and duration as appropriate.
19. Model wrapper metadata:
   `models.LCGWrapper` emits before events, chunks, after events, and error chunks with the same
   model call ID and context metadata.
20. Model provider metadata:
    `models.LCGWrapper.WithProvider` stores the provider on before and after model events.
21. Model request capture:
    `models.LCGWrapper` publishes `gent.ModelCallRequest` with messages and resolved standard
    `llms.CallOptions` rather than raw messages only.
22. Model request limitation notes:
    `models.LCGWrapper` sets `OptionCaptureComplete=false` and documents hidden provider option
    limitations when it cannot introspect provider-specific client settings.
23. Toolchain correlation:
    JSON, YAML, search, and output toolchains propagate `ToolCallId` from before to after events.
24. Toolchain error correlation:
    validation errors, missing tool errors, execution errors, and success paths all publish after
    tool events with the correct `ToolCallId` when a before event was published.

### Trace Config And Redactor Tests

1. Zero-value config applies all documented defaults.
2. Negative recent-event caps disable the corresponding buffers.
3. `NoContentLimit` disables model content truncation.
4. Zero content limits use documented defaults.
5. `RedactorFuncs` returns original non-error values when functions are nil.
6. Default error redaction preserves error message and concrete type.
7. Custom `RedactError` controls every stored error message.
8. Nil return from `RedactError` stores `redacted error` and does not drop error stats.
9. Redacted model requests, model responses, tool args, tool output, system prompts, common
   payloads, diffs, run output, chunks, and errors are stored exactly as returned.
10. Redacted payloads still pass JSON safety normalization.
11. Custom redactors are called concurrently without sequencer races or deadlocks.
12. Redactor invocation count matches the number of configured payloads and errors captured.

### Trace Event Number And Ordering Tests

1. Sequential events receive event numbers `1..N` with no gaps.
2. Mixed lifecycle events and chunks receive one shared event-number sequence.
3. Concurrent publishers receive unique strictly increasing event numbers.
4. Concurrent tests verify sorted output after collection, not goroutine completion order.
5. `Snapshot.LastEventNumber` always equals the highest recorded event number.
6. Every step record's `LastEventNumber` equals the last event that mutated that step.
7. Closed sequencer does not assign new event numbers.

Concurrency tests must start goroutines behind a barrier:

```go
start := make(chan struct{})
for i := 0; i < workers; i++ {
    go func(i int) {
        <-start
        seq.ConsumeChunk(chunkFor(i))
    }(i)
}
close(start)
```

### Trace Snapshot Mutation Tests

1. Before execution records `context_started` then `execution_started`.
2. After execution records `execution_finished` then `context_finished`.
3. Success termination sets run status to `succeeded` and stores final result only when enabled.
4. Error termination sets run status to `failed` and stores redacted error.
5. Context cancellation sets run status to `canceled`.
6. Limit termination sets run status to `limit_exceeded`.
7. Compaction failure sets run status to `failed`.
8. Child spawn creates a child `Context` and no generic `common` trace event.
9. Child complete completes the same child `Context` and no generic `common` trace event.
10. Iteration start creates an `Iteration` keyed by `(ContextId, Iteration)`.
11. Iteration finish completes the exact matching iteration.
12. Same iteration number in two contexts creates two distinct iteration records.
13. Model call start creates a `ModelCall` keyed by `ModelCallId`.
14. Model call finish completes the exact matching model call.
15. Model finish without matching ID records an uncorrelated finish event.
16. Uncorrelated model finish does not create or complete a `ModelCall` record.
17. Model usage stores input, output, cached input, reasoning, and total tokens.
18. Model duration contributes to `RunStats.ModelDurationMs`.
19. Tool call start creates a `ToolCall` keyed by `ToolCallId`.
20. Tool call finish completes the exact matching tool call.
21. Tool finish without matching ID records an uncorrelated finish event.
22. Uncorrelated tool finish does not create or complete a `ToolCall` record.
23. Tool duration contributes to `RunStats.ToolDurationMs`.
24. Parse errors increment parse error stats and append a redacted error.
25. Validator rejection increments validator rejection stats.
26. Error events increment error stats and append a redacted error.
27. Limit events increment limit stats and append a redacted error-like record.
28. Compaction events increment compaction stats and store exact lengths and duration.
29. Common events store payload only when configured.
30. Common diff events store payload only when configured.
31. Unmarshalable payloads become `PayloadCaptureError`.
32. All snapshot mutation tests call `assertSnapshotInvariants`.

### Model Stream Tests

1. Chunks attach to model calls by `ModelCallId`.
2. Chunks attach by `StreamId` only when exactly one active model call has that stream ID.
3. Chunks do not attach by `StreamTopicId`.
4. Ambiguous stream ID chunks are emitted as trace events but do not mutate a model stream.
5. Interleaved chunks from two model calls with the same topic remain separated.
6. Interleaved chunks from two model calls with different topics remain separated.
7. Content chunks accumulate content in order when `IncludeChunkText` is true.
8. Reasoning chunks accumulate reasoning content in order when `IncludeChunkText` is true.
9. Mixed content and reasoning chunks preserve separate buffers.
10. `IncludeChunkText=false` records chunk count and last event number but no text.
11. Content cap truncates content at the configured byte limit and sets `ContentTruncated`.
12. Reasoning cap truncates reasoning at the configured byte limit and sets truncation flag.
13. Content truncation preserves valid UTF-8 when the byte cap falls in the middle of a rune.
14. Reasoning truncation preserves valid UTF-8 when the byte cap falls in the middle of a rune.
15. `NoContentLimit` records complete content.
16. Chunk errors create redacted event errors.
17. Correlated chunk errors attach to the model call.
18. Uncorrelated chunk errors are still present in `Snapshot.Errors`.
19. Stream events preserve stream ID, topic ID, source, context ID, iteration, and depth.

### Tool Call Tests

1. Tool calls with different IDs and the same name remain distinct.
2. Parallel tool calls with the same name complete the correct records by `ToolCallId`.
3. Tool args are omitted by default.
4. Tool output is omitted by default.
5. Tool args are included and redacted when configured.
6. Tool output is included and redacted when configured.
7. Tool errors are redacted and attached to the correct tool call.
8. Tool success sets status `succeeded`; tool error sets status `failed`.
9. Fallback correlation by tool name, context ID, and iteration works only when unambiguous.
10. Ambiguous fallback correlation does not attach to the wrong tool call.
11. Missing after-event ID records an uncorrelated finish event without mutating tool records.

### Subscription And Backpressure Tests

1. `Subscribe` receives only future events.
2. Multiple subscribers each receive every future event.
3. Unsubscribed channels close and stop receiving events.
4. `Close` closes all live subscriber channels.
5. `Close` is idempotent.
6. Slow unbounded subscribers do not block event recording.
7. Bounded `OverflowDropOldest` drops oldest queued events for that subscriber only.
8. Bounded `OverflowDropNewest` drops new events for that subscriber only.
9. Bounded `OverflowCloseSubscriber` closes that subscriber and leaves others active.
10. Empty bounded overflow policy defaults to `OverflowCloseSubscriber`.
11. Bounded overflow does not affect `Snapshot` or replay buffers.
12. `OnOverflow` receives policy, dropped event number, new event number, and close status.
13. `OnOverflow` is not called when no overflow occurs.
14. Slow or concurrent `OnOverflow` tests prove callback behavior without blocking `record`.
15. Subscriber events are JSON-marshalable.

Non-blocking tests must prove completion with a bounded timeout around the producer call, not by
sleeping and guessing:

```go
done := make(chan struct{})
go func() {
    seq.ConsumeChunk(chunk)
    close(done)
}()
require.Eventually(t, func() bool {
    select {
    case <-done:
        return true
    default:
        return false
    }
}, 100*time.Millisecond, time.Millisecond)
```

### Replay And Resume Tests

1. `CanStitch` returns true for equal, older, and next first event numbers.
2. `CanStitch` returns false when the first event is greater than snapshot last plus one.
3. `ValidateStitch` returns OK for empty buffered events.
4. `ValidateStitch` returns OK for buffers that overlap the snapshot and continue contiguously.
5. `ValidateStitch` returns gap when the first needed event is missing.
6. `ValidateStitch` returns gap when an internal event number is missing.
7. `ValidateStitch` returns unsorted when buffered events are out of order.
8. `EventsAfter` returns an empty slice when `lastEventNumber == Snapshot.LastEventNumber`.
9. `EventsAfter` returns exact event copies when the range is retained.
10. `EventsAfter` returns `ErrReplayUnavailable` when the requested range was evicted.
11. `EventsAfter` never uses `RecentLifecycleEvents` or `RecentChunkEvents` to fill gaps.
12. Resume flow test subscribes, buffers, snapshots, validates, applies events, and ends with the
    same state as a fresh snapshot.

### JSON Safety Tests

1. Every trace event type marshals with `encoding/json`.
2. Full snapshot marshals with `encoding/json` after every major event type.
3. Included model request that cannot marshal becomes `PayloadCaptureError`.
4. Included model response that cannot marshal becomes `PayloadCaptureError`.
5. Included tool args that cannot marshal become `PayloadCaptureError`.
6. Included tool output that cannot marshal becomes `PayloadCaptureError`.
7. Included common payload that cannot marshal becomes `PayloadCaptureError`.
8. Included diff payload that cannot marshal becomes `PayloadCaptureError`.
9. Included run output that cannot marshal becomes `PayloadCaptureError`.
10. Redactor output that cannot marshal also becomes `PayloadCaptureError`.
11. `PayloadCaptureError` includes the original Go type and error message.

### Stream Observation Tests

1. `ObserveStreams` subscribes to `ExecutionContext.SubscribeAll` and records emitted chunks.
2. `ObserveStreams` is idempotent per context ID.
3. Observing a root and then a child does not duplicate child chunks.
4. Observing a child without observing the root records child chunks.
5. Returned stream unsubscribe stops future chunk consumption.
6. `Close` calls all stream observer unsubscribe functions.
7. `ConsumeChunks` stops when the context is canceled.
8. `ConsumeChunks` stops when the input channel closes.

### Snapshot Copy And Race Tests

1. Mutating a returned snapshot does not mutate sequencer state.
2. Mutating returned events from `EventsAfter` does not mutate replay buffers.
3. Mutating events received from a subscriber does not mutate snapshot state.
4. Concurrent `Snapshot`, `Subscribe`, `EventsAfter`, `ConsumeChunk`, and subscriber event
   methods are race-free under `go test -race`.
5. Snapshot copies remain internally consistent while events are concurrently recorded.

### End-To-End Integration Tests

1. ReAct success path:
   run a small ReAct execution with a mock streaming model and one successful tool. Assert the
   exact final snapshot contains root context, execution, iteration, model call, chunks, tool
   call, stats, and success status.
2. ReAct model error path:
   mock a model stream error. Assert redacted error appears on event, model call, snapshot
   errors, stats, and final run status.
3. ReAct tool error path:
   mock a tool error. Assert tool call status, redacted error, stats, and iteration outcome.
4. Parse error path:
   mock malformed model output. Assert parse error event, redacted error, stats, and continued
   loop behavior where applicable.
5. Validator rejection path:
   use a validator that rejects once then accepts. Assert rejection stats and final success.
6. Limit exceeded path:
   configure a small limit and assert limit event, redacted error, run status, and result.
7. Compaction path:
   configure compaction and assert compaction event, stats, and scratchpad lengths.
8. Nested child context:
   run a child agent loop and assert root sequencer sees child context events and child chunks.
9. Same-name child contexts:
   run two children with the same name in one parent iteration and assert no context, iteration,
   model call, or tool call collision.
10. Parallel model streams:
   run two interleaved model streams with the same topic and assert separation by model call ID.
11. Parallel tool calls:
   run parallel calls to the same tool name and assert separation by tool call ID.
12. Resume flow:
   subscribe, buffer live events, take snapshot, validate stitch, apply buffered events, and
   compare with a fresh snapshot.
13. Bounded websocket style flow:
    subscribe with bounded overflow, stall the consumer, assert overflow behavior and verify the
    sequencer snapshot remains complete.
14. Model request detail flow:
    run a model call with temperature, max tokens, stop words, and tool/function schemas when
    available; assert `IncludeModelRequests=true` captures messages and resolved options.

### Regression Test Matrix

Each fix found during implementation must add a regression test before changing code. The minimum
matrix below must stay covered:

| Risk | Required Regression Coverage |
| --- | --- |
| Context collision | same-name sibling child contexts |
| Model collision | parallel same-topic streams |
| Tool collision | parallel same-name tool calls |
| Sensitive leak | every error type with custom `RedactError` |
| Replay gap | evicted `RecentEvents` range |
| Subscriber stall | bounded and unbounded slow consumers |
| Snapshot mutation | caller mutates returned snapshot and events |
| JSON failure | unmarshalable payloads and redactor outputs |
| Async completion | model after event arrives after iteration advances |
| Incomplete request capture | model request includes messages, options, and capture notes |
| Missing after ID | after event without matching ID stays uncorrelated |
| Broken UTF-8 | content caps truncate at valid UTF-8 boundaries |

### Verification Commands

Use the repository standard timeout and write all command output to `/tmp`:

```bash
go test ./... -count=1 -timeout 300s > /tmp/gent-trace-go-test.txt 2>&1
```

Run race coverage for the packages touched by trace concurrency:

```bash
go test ./... -race -count=1 -timeout 300s > /tmp/gent-trace-race-test.txt 2>&1
```

If a failure occurs, inspect the relevant `/tmp` output file instead of rerunning immediately.
Do not mark the feature complete until both commands pass.

## Implementation Order

1. Add core context IDs, metadata fields, and publish options.
2. Update `EmitChunk` metadata population.
3. Fix child context event publishing, identity payloads, and event publisher inheritance.
4. Update `models/lcg.go` provider, model-call correlation, and request capture.
5. Update internal toolchain tool-call correlation.
6. Add core tests for metadata and correlation.
7. Create `trace` package public types and config defaulting.
8. Implement redaction, error redaction, and JSON-safe payload helpers.
9. Implement sequencer record path, subscriptions, overflow diagnostics, replay buffers, and close.
10. Implement subscriber methods and context event normalization.
11. Implement stream observation and chunk consumption.
12. Implement snapshot mutation helpers and uncorrelated finish-event handling.
13. Implement UTF-8-safe content truncation.
14. Implement stitch validation and replay helpers.
15. Add trace unit tests.
16. Add integration tests.
17. Run full verification command and inspect `/tmp/gent-trace-go-test.txt`.

Do not skip core context identity or correlation changes. Implementing `trace.Sequencer` without
them would make the feature unreliable under exactly the parallel and nested cases this feature
is meant to support.
