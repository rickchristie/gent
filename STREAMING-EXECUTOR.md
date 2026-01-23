# Streaming Support via ExecutionContext

## Overview

This document describes the design for adding streaming support to gent's ExecutionContext,
enabling real-time observation of LLM responses across nested and parallel agent loops.

## Goals

1. **Unified streaming hub**: All LLM streams fan-in to ExecutionContext
2. **Hierarchical support**: Nested/parallel agent loops automatically propagate streams to parent
3. **Selective subscription**: Subscribe to specific streams, topics, or all streams
4. **Zero-config for users**: Streaming "just works" through ExecutionContext
5. **Concurrent-safe**: Safe for parallel agent loops emitting simultaneously

## Non-Goals

- Backpressure handling (we use unbounded buffers, emitters never block)
- Stream persistence (streams are ephemeral, use trace events for history)

---

## Architecture

### Stream Flow

```
┌─────────────────────────────────────────────────────────────────────────┐
│                              Subscriber                                  │
│                    execCtx.SubscribeToTopic("llm")                      │
└─────────────────────────────────────────────────────────────────────────┘
                                    ▲
                                    │ Fan-in
┌─────────────────────────────────────────────────────────────────────────┐
│                         Root ExecutionContext                            │
│                             name: "main"                                 │
│                                                                          │
│  ┌─────────────────────┐    ┌─────────────────────┐                     │
│  │   Child Context     │    │   Child Context     │                     │
│  │  name: "research"   │    │  name: "analysis"   │                     │
│  │                     │    │                     │                     │
│  │  LLM Call ──────────┼────┼──► EmitChunk()      │                     │
│  │  (stream-001)       │    │  (stream-002)       │                     │
│  └─────────────────────┘    └─────────────────────┘                     │
└─────────────────────────────────────────────────────────────────────────┘
```

### StreamChunk Structure

```go
// StreamChunk represents a single chunk of streamed content with metadata.
type StreamChunk struct {
    // Content fields (existing)
    Content          string
    ReasoningContent string
    Err              error

    // Stream identification (new)
    Source        string  // Hierarchical path: "main/1/research/2"
    StreamId      string  // Unique per LLM call (caller-provided)
    StreamTopicId string  // Semantic grouping (caller-provided)
}
```

### Source Path Format

The `Source` field contains the hierarchical execution path:

```
Format: [contextName]/[iteration]/[childName]/[childIteration]/...

Examples:
- "main/1"                     Root context, iteration 1
- "main/2/research/1"          Root iter 2, child "research" iter 1
- "main/1/orchestrator/3/worker/2"   Deeply nested execution
```

This allows subscribers to:
- Filter by context name
- Track which iteration produced the chunk
- Trace the full execution path

---

## API Design

### ExecutionContext Additions

```go
// -----------------------------------------------------------------------------
// Streaming Support
// -----------------------------------------------------------------------------

// UnsubscribeFunc is a function that cancels a stream subscription.
// After calling, the subscription channel will be closed and no more chunks
// will be delivered. Safe to call multiple times.
type UnsubscribeFunc func()

// SubscribeAll returns a channel receiving all chunks from this context
// and all descendant contexts, plus an unsubscribe function.
//
// The channel closes when either:
//   - The unsubscribe function is called
//   - The ExecutionContext terminates (CloseStreams is called)
//
// IMPORTANT: Memory consideration - chunks are buffered without limit to ensure
// emitters never block. The subscriber is responsible for consuming chunks in a
// timely manner. If the subscriber cannot keep up, memory usage will grow
// unboundedly. Consider unsubscribing if the subscriber falls too far behind.
func (ctx *ExecutionContext) SubscribeAll() (<-chan StreamChunk, UnsubscribeFunc)

// SubscribeToStream returns a channel receiving chunks for a specific streamId,
// plus an unsubscribe function.
//
// Returns (nil, nil) if streamId is empty.
//
// The channel closes when either:
//   - The unsubscribe function is called
//   - The ExecutionContext terminates (CloseStreams is called)
//
// IMPORTANT: Memory consideration - chunks are buffered without limit to ensure
// emitters never block. The subscriber is responsible for consuming chunks in a
// timely manner. If the subscriber cannot keep up, memory usage will grow
// unboundedly. Consider unsubscribing if the subscriber falls too far behind.
func (ctx *ExecutionContext) SubscribeToStream(streamId string) (<-chan StreamChunk, UnsubscribeFunc)

// SubscribeToTopic returns a channel receiving chunks for a specific topicId,
// plus an unsubscribe function.
//
// Multiple streams may share the same topic; caller handles interleaving.
// Returns (nil, nil) if topicId is empty.
//
// The channel closes when either:
//   - The unsubscribe function is called
//   - The ExecutionContext terminates (CloseStreams is called)
//
// IMPORTANT: Memory consideration - chunks are buffered without limit to ensure
// emitters never block. The subscriber is responsible for consuming chunks in a
// timely manner. If the subscriber cannot keep up, memory usage will grow
// unboundedly. Consider unsubscribing if the subscriber falls too far behind.
func (ctx *ExecutionContext) SubscribeToTopic(topicId string) (<-chan StreamChunk, UnsubscribeFunc)

// EmitChunk emits a streaming chunk to all relevant subscribers.
// Called by model wrappers during streaming. Automatically propagates to parent.
// Safe for concurrent use.
func (ctx *ExecutionContext) EmitChunk(chunk StreamChunk)

// CloseStreams closes all subscription channels. Called by Executor on termination.
// Safe to call multiple times.
func (ctx *ExecutionContext) CloseStreams()

// BuildSourcePath returns the hierarchical source path for this context.
// Format: "name/iteration" or "parent-path/name/iteration"
func (ctx *ExecutionContext) BuildSourcePath() string
```

### Model Interface Update

The base Model interface is updated to support stream emission for non-streaming calls:

```go
// Model is gent's model interface.
type Model interface {
    // GenerateContent generates content from a sequence of messages.
    //
    // Parameters:
    //   - ctx: Go context for cancellation
    //   - execCtx: ExecutionContext for tracing and stream fan-in (may be nil)
    //   - streamId: Unique identifier for this call (caller-provided)
    //   - streamTopicId: Topic for grouping related calls (caller-provided)
    //   - messages: Input messages
    //   - options: LLM call options
    //
    // Stream Emission Requirement:
    // When execCtx is provided, implementations MUST call execCtx.EmitChunk()
    // with the complete response content as a single chunk. This ensures
    // subscribers receive content regardless of whether the underlying model
    // supports streaming.
    //
    // The emitted chunk should have:
    //   - Content: The full response text
    //   - StreamId: The provided streamId
    //   - StreamTopicId: The provided streamTopicId
    //   - Source: Will be auto-populated by EmitChunk if empty
    //
    // The streamId MUST be unique across all concurrent calls. If duplicate
    // streamIds are used, subscribers will receive interleaved chunks.
    GenerateContent(
        ctx context.Context,
        execCtx *ExecutionContext,
        streamId string,
        streamTopicId string,
        messages []llms.MessageContent,
        options ...llms.CallOption,
    ) (*ContentResponse, error)
}
```

### StreamingModel Interface Update

```go
// StreamingModel extends Model with streaming capabilities.
type StreamingModel interface {
    Model

    // GenerateContentStream generates content with streaming support.
    //
    // Parameters:
    //   - ctx: Go context for cancellation
    //   - execCtx: ExecutionContext for tracing and stream fan-in (may be nil)
    //   - streamId: Unique identifier for this stream (caller-provided)
    //   - streamTopicId: Topic for grouping related streams (caller-provided)
    //   - messages: Input messages
    //   - options: LLM call options
    //
    // Stream Emission Requirement:
    // When execCtx is provided, implementations MUST call execCtx.EmitChunk()
    // for each chunk as it arrives from the LLM. This enables real-time
    // observation of responses across the execution tree.
    //
    // Each emitted chunk should have:
    //   - Content/ReasoningContent: The chunk's content delta
    //   - StreamId: The provided streamId
    //   - StreamTopicId: The provided streamTopicId
    //   - Source: Will be auto-populated by EmitChunk if empty
    //   - Err: Set if an error occurred (final chunk only)
    //
    // The streamId MUST be unique across all concurrent streams. If duplicate
    // streamIds are used, subscribers will receive interleaved chunks from
    // multiple streams, which may cause confusion.
    //
    // The streamTopicId groups related streams. Subscribers to a topic receive
    // chunks from all streams with that topic.
    GenerateContentStream(
        ctx context.Context,
        execCtx *ExecutionContext,
        streamId string,
        streamTopicId string,
        messages []llms.MessageContent,
        options ...llms.CallOption,
    ) (Stream, error)
}
```

---

## Implementation Details

### Internal Stream Hub

```go
// streamHub manages stream subscriptions and chunk distribution.
// All methods are concurrent-safe.
type streamHub struct {
    mu sync.RWMutex

    // Subscription channels (unbounded buffers)
    allSubscribers    []*streamSubscription
    byStreamId        map[string][]*streamSubscription
    byTopicId         map[string][]*streamSubscription

    // State
    closed bool
}

type streamSubscription struct {
    ch     chan StreamChunk
    buffer *unboundedBuffer[StreamChunk]  // Non-blocking sends
}

func newStreamHub() *streamHub {
    return &streamHub{
        byStreamId: make(map[string][]*streamSubscription),
        byTopicId:  make(map[string][]*streamSubscription),
    }
}

func (h *streamHub) subscribeAll() <-chan StreamChunk
func (h *streamHub) subscribeToStream(streamId string) <-chan StreamChunk
func (h *streamHub) subscribeToTopic(topicId string) <-chan StreamChunk
func (h *streamHub) emit(chunk StreamChunk)
func (h *streamHub) close()
```

### Unbounded Buffer

To ensure emitters never block (even with slow subscribers), we use an unbounded buffer:

```go
// unboundedBuffer provides non-blocking sends with unlimited buffering.
// This ensures producers (LLM streams) never block waiting for consumers.
type unboundedBuffer[T any] struct {
    mu       sync.Mutex
    items    []T
    notEmpty chan struct{}
    closed   bool
}

func (b *unboundedBuffer[T]) Send(item T)      // Never blocks
func (b *unboundedBuffer[T]) Receive() <-chan T // Returns read channel
func (b *unboundedBuffer[T]) Close()
```

### Chunk Propagation to Parent

When a child context emits a chunk, it automatically propagates to the parent:

```go
func (ctx *ExecutionContext) EmitChunk(chunk StreamChunk) {
    // Populate source path if not set
    if chunk.Source == "" {
        chunk.Source = ctx.BuildSourcePath()
    }

    // Emit to local subscribers
    ctx.streamHub.emit(chunk)

    // Propagate to parent (chunk.Source already contains full path)
    if ctx.parent != nil {
        ctx.parent.EmitChunk(chunk)
    }
}
```

This ensures that subscribing at the root level receives all chunks from the entire
execution tree, while subscribing at a child level only receives that subtree.

---

## Concurrency Safety Review

### Current State

ExecutionContext already uses `sync.RWMutex` for all field access. Review of existing
methods shows proper locking:

| Method | Lock Type | Notes |
|--------|-----------|-------|
| `Data()` | RLock | Read-only |
| `Name()` | RLock | Read-only |
| `SetHookFirer()` | Lock | Write |
| `Iteration()` | RLock | Read-only |
| `StartIteration()` | Lock | Write + event append |
| `EndIteration()` | Lock | Write + event append |
| `Trace()` | Lock | Write + stats update |
| `Events()` | RLock | Returns copy |
| `Stats()` | RLock | Returns deep copy |
| `SpawnChild()` | Lock | Write + child creation |
| `CompleteChild()` | Lock (both) | Parent and child locks |
| `SetTermination()` | Lock | Write |

### New Methods Concurrency

| Method | Lock Type | Notes |
|--------|-----------|-------|
| `SubscribeAll()` | Lock | Adds subscriber to hub |
| `SubscribeToStream()` | Lock | Adds subscriber to hub |
| `SubscribeToTopic()` | Lock | Adds subscriber to hub |
| `EmitChunk()` | RLock + hub lock | Read parent, hub handles distribution |
| `CloseStreams()` | Lock | Closes hub |
| `BuildSourcePath()` | RLock | Read name, iteration, parent |

### Potential Issues and Mitigations

1. **Parent chain traversal during emit**
   - Risk: Parent could be modified while traversing
   - Mitigation: Parent is immutable after creation (set only in SpawnChild)

2. **Subscriber channel sends**
   - Risk: Blocking on slow consumers
   - Mitigation: Unbounded buffer ensures non-blocking sends

3. **Hub closure race**
   - Risk: Emit after close
   - Mitigation: Hub tracks closed state, ignores emits after close

4. **Child stats aggregation**
   - Risk: Reading child stats while child still executing
   - Mitigation: CompleteChild locks both parent and child

---

## Usage Examples

### Basic CLI Streaming

```go
func main() {
    execCtx := gent.NewExecutionContext("main", data)

    // Subscribe before execution starts
    chunks, unsubscribe := execCtx.SubscribeToTopic("llm-response")
    defer unsubscribe()  // Clean up subscription

    go func() {
        for chunk := range chunks {
            fmt.Print(chunk.Content)
        }
        fmt.Println() // Newline after stream ends
    }()

    exec := executor.New(loop, config)
    result := exec.Execute(ctx, data)
}
```

### Subscribing to Specific Stream

```go
// When you know the exact streamId that will be used
chunks, unsubscribe := execCtx.SubscribeToStream("final-response")
defer unsubscribe()

go func() {
    for chunk := range chunks {
        // Only receives chunks from stream "final-response"
        updateUI(chunk.Content)
    }
}()
```

### Early Unsubscribe

```go
// Unsubscribe when a condition is met
chunks, unsubscribe := execCtx.SubscribeAll()

go func() {
    totalBytes := 0
    for chunk := range chunks {
        fmt.Print(chunk.Content)
        totalBytes += len(chunk.Content)

        // Stop receiving if we've seen enough
        if totalBytes > 10000 {
            unsubscribe()  // Channel will close, loop exits
            fmt.Println("\n[Truncated - too much output]")
            return
        }
    }
}()
```

### Nested Agent Loops

```go
// In an orchestrator agent loop
func (o *OrchestratorLoop) Next(ctx context.Context, execCtx *ExecutionContext) {
    // Spawn parallel workers
    worker1Ctx := execCtx.SpawnChild("worker-1", worker1Data)
    worker2Ctx := execCtx.SpawnChild("worker-2", worker2Data)

    var wg sync.WaitGroup
    wg.Add(2)

    go func() {
        defer wg.Done()
        defer execCtx.CompleteChild(worker1Ctx)
        worker1Exec.ExecuteWithContext(ctx, worker1Ctx)
    }()

    go func() {
        defer wg.Done()
        defer execCtx.CompleteChild(worker2Ctx)
        worker2Exec.ExecuteWithContext(ctx, worker2Ctx)
    }()

    wg.Wait()
    // ...
}

// Root subscriber sees chunks from both workers:
// - Source: "main/1/worker-1/1" (from worker 1)
// - Source: "main/1/worker-2/1" (from worker 2)
```

### Filtering by Source

```go
chunks, unsubscribe := execCtx.SubscribeAll()
defer unsubscribe()

go func() {
    for chunk := range chunks {
        // Filter to only show chunks from "research" child
        if strings.Contains(chunk.Source, "/research/") {
            fmt.Printf("[Research] %s", chunk.Content)
        }
    }
}()
```

### Multiple Subscribers

```go
// Multiple subscribers to the same topic
uiChunks, uiUnsub := execCtx.SubscribeToTopic("llm-response")
logChunks, logUnsub := execCtx.SubscribeToTopic("llm-response")
defer uiUnsub()
defer logUnsub()

// UI subscriber - shows real-time output
go func() {
    for chunk := range uiChunks {
        updateUI(chunk.Content)
    }
}()

// Log subscriber - writes to file
go func() {
    for chunk := range logChunks {
        logFile.WriteString(fmt.Sprintf("[%s] %s", chunk.Source, chunk.Content))
    }
}()
```

---

## Implementation Plan

### Phase 1: Core Streaming Infrastructure

1. **Add StreamChunk metadata fields** (`model.go`)
   - Add `Source`, `StreamId`, `StreamTopicId` to StreamChunk

2. **Implement unboundedBuffer** (`internal/buffer/unbounded.go`)
   - Generic unbounded buffer for non-blocking sends
   - Full test coverage

3. **Implement streamHub** (`stream_hub.go`)
   - Subscription management
   - Concurrent-safe emit
   - Close handling

4. **Update ExecutionContext** (`context.go`)
   - Add streamHub field
   - Implement Subscribe* methods
   - Implement EmitChunk with parent propagation
   - Implement BuildSourcePath
   - Update CloseStreams

### Phase 2: Model Integration

5. **Update StreamingModel interface** (`model.go`)
   - Add streamId, streamTopicId parameters

6. **Update LCGWrapper** (`models/lcg_wrapper.go`)
   - Call execCtx.EmitChunk for each chunk
   - Populate chunk metadata

7. **Update ReActLoop** (`agents/react.go`)
   - Use GenerateContentStream when model supports it
   - Define streamId and streamTopicId for model calls

### Phase 3: Executor Integration

8. **Update Executor** (`executor/executor.go`)
   - Call execCtx.CloseStreams() on termination
   - Ensure streams close even on error/cancellation

### Phase 4: CLI Test

9. **Create airline CLI test** (`integrationtest/airline/cliairlinetest/main.go`)
   - Extract test setup from airline_test.go
   - Subscribe to streams and print real-time output

### Phase 5: Documentation & Testing

10. **Comprehensive tests**
    - Concurrent emit safety
    - Parent propagation
    - Subscription filtering
    - Close handling

11. **Update documentation**
    - README streaming section
    - Example code

---

## Design Decisions

The following questions have been resolved:

### 1. Unsubscribe Support

**Decision**: Yes, return an unsubscribe function with every subscribe call.

```go
chunks, unsubscribe := execCtx.SubscribeToTopic("llm-response")
defer unsubscribe()  // Clean up when done
```

This allows subscribers to:
- Cancel early if they no longer need updates
- Clean up resources before context termination
- Avoid memory buildup from unused subscriptions

### 2. EmitChunk for Non-Streaming Model Calls

**Decision**: Yes, both Model and StreamingModel implementations MUST call EmitChunk.

- `Model.GenerateContent()`: Emit single chunk with complete response
- `StreamingModel.GenerateContentStream()`: Emit chunks as they arrive

This ensures subscribers receive content uniformly regardless of whether the
underlying model supports streaming. A subscriber doesn't need to know or care
whether the model is streaming-capable.

### 3. Memory Limits on Unbounded Buffer

**Decision**: Consumer is responsible for memory management.

The unbounded buffer ensures emitters (LLM streams) never block, which is critical
for parallel agent loops. However, this means memory can grow without limit if
consumers don't keep up.

**Mitigation**: The subscribe method documentation clearly states:
- Chunks are buffered without limit
- Subscriber is responsible for timely consumption
- Consider unsubscribing if falling too far behind

This puts the responsibility on the subscriber, who is best positioned to know
their consumption capacity and take appropriate action.

---

## Timeline Estimate

- Phase 1: Core infrastructure
- Phase 2: Model integration
- Phase 3: Executor integration
- Phase 4: CLI test
- Phase 5: Testing & docs

Total: Moderate effort, can be done incrementally.
