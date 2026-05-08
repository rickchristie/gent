I want to add a reusable debug/event sequencing layer to Gent.

Goal:
Create a Gent-standard event sequencer that consumes:
1. Gent lifecycle events from Executor/Event subscribers.
2. Gent StreamChunk values from ExecutionContext stream subscriptions.
It should normalize both into one ordered event stream with a monotonically increasing EventNumber, and maintain a materialized debug snapshot that can be used for UI resume/replay.

Context:
Consumers want to display agent execution live in a debugging UI. The UI subscribes to live sequenced events, then fetches the latest materialized snapshot, then stitches by EventNumber:
- Subscribe first and buffer live events.
- Fetch snapshot.
- If first buffered event number <= snapshot.LastEventNumber + 1, render snapshot and apply buffered events with EventNumber > snapshot.LastEventNumber.
- If there is a gap, refetch snapshot or request replay.
Please design and implement this as reusable library code, not application-specific code.

Requirements:
1. Add generic structs, no application/chat-specific fields.
2. Keep it usable for any Gent AgentLoop, including nested child contexts and parallel model calls.
3. Preserve StreamId, StreamTopicId, Source, Iteration, Depth, timestamps, model/tool ids, token stats, duration, errors.
4. Do not assume StreamTopicId is unique. StreamId should identify a model stream/model call where possible.
5. Do not parse model content tags like <thinking>, <action>, or <answer>. Treat model stream text as opaque.
6. Support both live event publishing and in-memory snapshot buildup from the same sequenced events.
7. Keep raw request/response/payload fields optional and configurable because they can be large or sensitive.
8. Provide tests for ordering, resume safety, model chunks, lifecycle events, tool events, error events, child context source paths, and parallel/interleaved streams.

Proposed package:
`debug` or `trace` package. Pick the name that best fits Gent conventions.
API shape idea:
```go
type Sequencer struct {
    // owns event number assignment and snapshot mutation
}
type SequencerConfig struct {
    IncludeRawRequests  bool
    IncludeRawResponses bool
    IncludeChunkText    bool
    MaxRecentEvents     int
    MaxModelContentBytes int
    MaxReasoningContentBytes int
    Redactor Redactor
}
type Redactor interface {
    RedactModelRequest(req any) any
    RedactModelResponse(resp any) any
    RedactToolArgs(args any) any
    RedactToolOutput(output any) any
    RedactChunk(chunk gent.StreamChunk) gent.StreamChunk
}
func NewSequencer(runId string, cfg SequencerConfig) *Sequencer
func (s *Sequencer) Subscriber() any

Subscriber() should return an object implementing Gent subscriber interfaces, including:
- BeforeExecutionSubscriber
- AfterExecutionSubscriber
- BeforeIterationSubscriber
- AfterIterationSubscriber
- BeforeModelCallSubscriber
- AfterModelCallSubscriber
- BeforeToolCallSubscriber
- AfterToolCallSubscriber
- ParseErrorSubscriber
- ValidatorCalledSubscriber
- ValidatorResultSubscriber
- ErrorSubscriber
- LimitExceededSubscriber
- CompactionSubscriber
- CommonEventSubscriber
- CommonDiffEventSubscriber if useful
```

For stream chunks, expose one of these:
func (s *Sequencer) ConsumeChunk(chunk gent.StreamChunk)
or:
func (s *Sequencer) ConsumeChunks(ctx context.Context, chunks <-chan gent.StreamChunk)

Need live output subscription:
func (s *Sequencer) Events() <-chan *DebugEvent
func (s *Sequencer) Snapshot() *DebugInfo
func (s *Sequencer) Close()

If an events channel can block, document buffering/backpressure behavior. Prefer not to block the agent loop indefinitely.

Proposed reusable structs:

```
type DebugInfo struct {
    SchemaVersion int `json:"schemaVersion"`
    RunId string `json:"runId"`
    Status RunStatus `json:"status"`
    StartedTs time.Time `json:"startedTs"`
    LastUpdatedTs time.Time `json:"lastUpdatedTs"`
    CompletedTs *time.Time `json:"completedTs,omitempty"`
    LastEventNumber uint64 `json:"lastEventNumber"`
    Result *RunResult `json:"result,omitempty"`
    Stats *RunStats `json:"stats"`
    Iterations []*IterationDebug `json:"iterations"`
    ModelCalls []*ModelCallDebug `json:"modelCalls"`
    ToolCalls []*ToolCallDebug `json:"toolCalls"`
    Errors []*DebugError `json:"errors,omitempty"`
    RecentEvents []*DebugEvent `json:"recentEvents,omitempty"`
}
type DebugEvent struct {
    EventNumber uint64 `json:"eventNumber"`
    RunId string `json:"runId"`
    Ts time.Time `json:"ts"`
    Type DebugEventType `json:"type"`
    Iteration int `json:"iteration,omitempty"`
    Depth int `json:"depth,omitempty"`
    ModelCallId string `json:"modelCallId,omitempty"`
    ToolCallId string `json:"toolCallId,omitempty"`
    StreamId string `json:"streamId,omitempty"`
    StreamTopicId string `json:"streamTopicId,omitempty"`
    Source string `json:"source,omitempty"`
    Payload any `json:"payload,omitempty"`
}
```

Important:
The sequencer must be safe when Gent publishes events and stream chunks concurrently. EventNumber assignment must happen in exactly one place and must be monotonic. If needed, use one internal goroutine that serializes normalized input events before assigning EventNumber.

Model call correlation:
Gent’s current lifecycle model events may not include StreamId/StreamTopicId, while StreamChunk has them. Please add or design a correlation mechanism so DebugInfo.ModelCalls can associate:
- BeforeModelCall request
- streamed chunks
- AfterModelCall response/stats
with the same ModelCallDebug record.

Possible approaches:
1. Add streamId/streamTopicId/source metadata to BeforeModelCallEvent and AfterModelCallEvent.
2. Add a model call id to ExecutionContext around GenerateContentStream.
3. Let the sequencer infer from StreamId, but explicit correlation is preferred.

Please choose the cleanest design for Gent.

Proposed model call debug:
```
type ModelCallDebug struct {
    Id string `json:"id"`
    StreamId string `json:"streamId,omitempty"`
    StreamTopicId string `json:"streamTopicId,omitempty"`
    Source string `json:"source,omitempty"`
    Iteration int `json:"iteration"`
    Depth int `json:"depth"`
    Status StepStatus `json:"status"`
    Model string `json:"model"`
    StartedTs time.Time `json:"startedTs"`
    LastUpdatedTs time.Time `json:"lastUpdatedTs"`
    CompletedTs *time.Time `json:"completedTs,omitempty"`
    DurationMs int64 `json:"durationMs,omitempty"`
    StartedEventNumber uint64 `json:"startedEventNumber"`
    LastEventNumber uint64 `json:"lastEventNumber"`
    CompletedEventNumber uint64 `json:"completedEventNumber,omitempty"`
    Request any `json:"request,omitempty"`
    Response any `json:"response,omitempty"`
    Stream *ModelStreamDebug `json:"stream,omitempty"`
    Usage *ModelUsage `json:"usage,omitempty"`
    Error *DebugError `json:"error,omitempty"`
}
type ModelStreamDebug struct {
    Content string `json:"content,omitempty"`
    ReasoningContent string `json:"reasoningContent,omitempty"`
    ChunkCount int `json:"chunkCount"`
    LastChunkEventNumber uint64 `json:"lastChunkEventNumber,omitempty"`
}
```

Tool call debug:
```
type ToolCallDebug struct {
    Id string `json:"id"`
    Name string `json:"name"`
    Iteration int `json:"iteration"`
    Depth int `json:"depth"`
    Status StepStatus `json:"status"`
    StartedTs time.Time `json:"startedTs"`
    LastUpdatedTs time.Time `json:"lastUpdatedTs"`
    CompletedTs *time.Time `json:"completedTs,omitempty"`
    DurationMs int64 `json:"durationMs,omitempty"`
    StartedEventNumber uint64 `json:"startedEventNumber"`
    LastEventNumber uint64 `json:"lastEventNumber"`
    CompletedEventNumber uint64 `json:"completedEventNumber,omitempty"`
    Args any `json:"args,omitempty"`
    Output any `json:"output,omitempty"`
    Error *DebugError `json:"error,omitempty"`
}
```

Iteration debug:
```
type IterationDebug struct {
    Iteration int `json:"iteration"`
    Depth int `json:"depth"`
    Source string `json:"source,omitempty"`
    Status StepStatus `json:"status"`
    StartedTs time.Time `json:"startedTs"`
    LastUpdatedTs time.Time `json:"lastUpdatedTs"`
    CompletedTs *time.Time `json:"completedTs,omitempty"`
    StartedEventNumber uint64 `json:"startedEventNumber"`
    LastEventNumber uint64 `json:"lastEventNumber"`
    CompletedEventNumber uint64 `json:"completedEventNumber,omitempty"`
    ModelCallIds []string `json:"modelCallIds,omitempty"`
    ToolCallIds []string `json:"toolCallIds,omitempty"`
    Result any `json:"result,omitempty"`
    Error *DebugError `json:"error,omitempty"`
}
```

Stats:
```
type RunStats struct {
    IterationCount int `json:"iterationCount"`
    ModelCallCount int `json:"modelCallCount"`
    ToolCallCount int `json:"toolCallCount"`
    InputTokens int `json:"inputTokens"`
    OutputTokens int `json:"outputTokens"`
    ReasoningTokens int `json:"reasoningTokens"`
    CachedInputTokens int `json:"cachedInputTokens"`
    TotalTokens int `json:"totalTokens"`
    DurationMs int64 `json:"durationMs"`
    ModelDurationMs int64 `json:"modelDurationMs"`
    ToolDurationMs int64 `json:"toolDurationMs"`
    ParseErrorCount int `json:"parseErrorCount"`
    ValidatorRejectionCount int `json:"validatorRejectionCount"`
}
```

Tests:
1. Sequencer assigns EventNumber monotonically.
2. Snapshot.LastEventNumber matches last applied event.
3. RecentEvents is capped by MaxRecentEvents.
4. Before/after iteration creates and completes IterationDebug.
5. Before/after model call creates and completes ModelCallDebug with request, response, usage, duration.
6. Stream chunks append to the correct ModelCallDebug.Stream by StreamId.
7. Interleaved chunks from two parallel StreamIds remain separated.
8. Tool call before/after creates and completes ToolCallDebug.
9. Error and limit events update status/errors.
10. Snapshot copies are safe to read while events are being consumed.
11. Resume stitching helper if implemented:
func CanStitch(snapshotLastEventNumber uint64, firstBufferedEventNumber uint64) bool
or a more complete contiguous buffered-events validator.

Please keep the implementation small and idiomatic. Prefer explicit structs and straightforward mutation over an over-general event-sourcing framework.