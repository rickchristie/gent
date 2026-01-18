# Streaming Design Analysis for AgentLoop

## Current Architecture Summary

| Component | Current State | Streaming Need |
|-----------|--------------|----------------|
| `gent.Model.GenerateContent()` | Synchronous, returns complete `*ContentResponse` | Token-by-token, thinking content |
| `AgentLoop.Next()` | Synchronous, returns `*AgentLoopResult` | Model output, tool parsing, tool results |
| `Executor.Execute()` | Synchronous loop | Iteration lifecycle, child agents |
| `ExecutionContext.Trace()` | Appends to slice, not real-time | Real-time event emission |

LangChainGo's underlying `llms.Model` already supports streaming via callbacks (`llms.WithStreamingFunc`), but `LCGWrapper` doesn't expose this.

---

## Design Alternatives

### Option 1: Event Callback System (Observer Pattern)

Add event handlers to `ExecutionContext` that fire as events occur.

```go
// stream.go
type StreamEvent interface{ streamEvent() }

type TokenDeltaEvent struct {
    Token    string
    IsFinal  bool
}

type ThinkingDeltaEvent struct {
    Content string
}

type ToolCallStartEvent struct {
    ToolName string
    Input    any
}

type ToolCallEndEvent struct {
    ToolName string
    Output   any
    Error    error
}

type IterationStartEvent struct {
    Iteration int
}

// Add to ExecutionContext
type EventHandler func(event StreamEvent)

func (ctx *ExecutionContext) OnStream(handler EventHandler) {
    ctx.streamHandlers = append(ctx.streamHandlers, handler)
}

func (ctx *ExecutionContext) emitStream(event StreamEvent) {
    for _, h := range ctx.streamHandlers {
        h(event)
    }
}
```

**Usage:**
```go
execCtx := gent.NewExecutionContext("main", data)
execCtx.OnStream(func(event gent.StreamEvent) {
    switch e := event.(type) {
    case gent.TokenDeltaEvent:
        fmt.Print(e.Token)  // Stream to UI
    case gent.ThinkingDeltaEvent:
        showThinking(e.Content)
    }
})
executor.Execute(ctx, data)
```

| Pros | Cons |
|------|------|
| Non-breaking change to existing API | Callback nesting complexity |
| Easy to add new event types | Synchronous by default |
| Multiple subscribers supported | Error handling in callbacks is awkward |
| Simple testing via mock handlers | |
| Familiar pattern from hooks | |

---

### Option 2: Channel-Based Streaming

Return channels from streaming variants of methods.

```go
// model.go - Optional streaming interface
type StreamingModel interface {
    Model
    GenerateContentStream(
        ctx context.Context,
        execCtx *ExecutionContext,
        messages []llms.MessageContent,
        options ...llms.CallOption,
    ) (<-chan ContentChunk, <-chan error)
}

type ContentChunk struct {
    Type             ChunkType  // TokenDelta, ThinkingDelta, Complete
    Delta            string
    ReasoningDelta   string
    FinalResponse    *ContentResponse  // Only set when Type == Complete
}

// agent.go - Streaming AgentLoop
type StreamingAgentLoop[Data LoopData] interface {
    AgentLoop[Data]
    NextStream(ctx context.Context, execCtx *ExecutionContext) <-chan LoopEvent
}

type LoopEvent struct {
    Type      LoopEventType
    Token     string
    Thinking  string
    ToolCall  *ToolCallEvent
    Result    *AgentLoopResult  // Only when Type == Complete
    Error     error
}
```

**Usage:**
```go
events := loop.NextStream(ctx, execCtx)
for event := range events {
    switch event.Type {
    case TokenDelta:
        renderToken(event.Token)
    case ThinkingDelta:
        renderThinking(event.Thinking)
    case ToolCallStart:
        showToolSpinner(event.ToolCall.Name)
    case Complete:
        handleResult(event.Result)
    }
}
```

| Pros | Cons |
|------|------|
| Go-idiomatic (channels + select) | Channel lifecycle management |
| Natural backpressure | Must handle close properly |
| Easy to compose | More complex error propagation |
| Works well with goroutines | Need buffering decisions |

---

### Option 3: Iterator/Pull-Based Streaming

Return an iterator that can be pulled for events.

```go
type StreamIterator interface {
    Next() bool       // Advances to next event
    Event() StreamEvent
    Err() error
    Close() error
}

// On Model
func (m *LCGWrapper) GenerateContentStream(
    ctx context.Context,
    execCtx *ExecutionContext,
    messages []llms.MessageContent,
    options ...llms.CallOption,
) StreamIterator

// On Executor
func (e *Executor[Data]) ExecuteStream(
    ctx context.Context,
    data Data,
) StreamIterator
```

**Usage:**
```go
iter := executor.ExecuteStream(ctx, data)
defer iter.Close()

for iter.Next() {
    switch e := iter.Event().(type) {
    case TokenDeltaEvent:
        render(e.Token)
    }
}
if err := iter.Err(); err != nil {
    handleError(err)
}
```

| Pros | Cons |
|------|------|
| Consumer controls pace | Less Go-idiomatic |
| Easy to stop early | Harder to compose |
| Clear ownership | More boilerplate |
| Good for sync consumers | Doesn't parallelize well |

---

### Option 4: Enhanced Trace Event Streaming

Extend existing trace system to emit events in real-time.

```go
// New trace event types
type TokenDeltaTrace struct {
    BaseTrace
    Token string
}

type ThinkingDeltaTrace struct {
    BaseTrace
    Content string
}

// ExecutionContext gets streaming mode
func NewStreamingExecutionContext(
    name string,
    data LoopData,
) (*ExecutionContext, <-chan TraceEvent) {
    eventCh := make(chan TraceEvent, 100)
    ctx := &ExecutionContext{
        // ... existing fields
        eventStream: eventCh,
    }
    return ctx, eventCh
}

// Modified Trace() emits to channel if streaming
func (ctx *ExecutionContext) Trace(event TraceEvent) {
    ctx.mu.Lock()
    defer ctx.mu.Unlock()
    ctx.traceEventLocked(event)

    if ctx.eventStream != nil {
        select {
        case ctx.eventStream <- event:
        default:
            // Buffer full - could log warning
        }
    }
}
```

**Usage:**
```go
execCtx, events := gent.NewStreamingExecutionContext("main", data)

go func() {
    for event := range events {
        switch e := event.(type) {
        case gent.TokenDeltaTrace:
            renderToken(e.Token)
        case gent.ToolCallTrace:
            renderToolCall(e)
        }
    }
}()

result := executor.Execute(ctx, data)
```

| Pros | Cons |
|------|------|
| Leverages existing infrastructure | Need new trace types for fine-grained events |
| Minimal changes to existing code | May miss some events if buffer fills |
| Events already well-defined | Adds complexity to ExecutionContext |
| Easy to serialize/transport | |

---

### Option 5: Response Writer Pattern

Pass a writer interface for streaming output (good for HTTP/SSE).

```go
type StreamWriter interface {
    WriteEvent(event StreamEvent) error
    Flush() error
    Close() error
}

type SSEStreamWriter struct {
    w http.ResponseWriter
}

func (s *SSEStreamWriter) WriteEvent(event StreamEvent) error {
    data, _ := json.Marshal(event)
    fmt.Fprintf(s.w, "data: %s\n\n", data)
    return nil
}

// Executor method
func (e *Executor[Data]) ExecuteStreaming(
    ctx context.Context,
    data Data,
    writer StreamWriter,
) *ExecutionResult
```

| Pros | Cons |
|------|------|
| Direct HTTP integration | Ties to specific output format |
| Natural for SSE/WebSocket | Less flexible for non-web use |
| No channel management | One consumer only |

---

## Recommended Approach: Hybrid Design

Combining **Option 4** (Trace Event Streaming) + **Option 1** (Callbacks) + **Streaming Model extension**:

### Layer 1: StreamingModel Interface (Low-level)

```go
// model.go
type StreamingModel interface {
    Model
    GenerateContentStream(
        ctx context.Context,
        execCtx *ExecutionContext,
        messages []llms.MessageContent,
        options ...llms.CallOption,
    ) (*StreamingResponse, error)
}

type StreamingResponse struct {
    Events <-chan ContentStreamEvent
    Wait   func() (*ContentResponse, error)  // Block until complete
}

type ContentStreamEvent struct {
    TokenDelta     string
    ReasoningDelta string
    Error          error
}
```

### Layer 2: Real-time Trace Events (Framework Core)

```go
// New trace types in trace.go
type TokenDeltaTrace struct {
    BaseTrace
    Token string
}

type ReasoningDeltaTrace struct {
    BaseTrace
    Content string
}

type ToolCallStartTrace struct {
    BaseTrace
    ToolName string
    Input    any
}

// ExecutionContext streams traces if configured
type ExecutionContextConfig struct {
    EnableStreaming bool
    BufferSize      int
}

func NewExecutionContextWithConfig(
    name string,
    data LoopData,
    config ExecutionContextConfig,
) (*ExecutionContext, <-chan TraceEvent)
```

### Layer 3: Simple Callback API (User-facing)

```go
// Convenience wrapper that hides channels
type StreamHandler struct {
    OnToken         func(token string)
    OnReasoning     func(content string)
    OnToolStart     func(name string, input any)
    OnToolEnd       func(name string, output any, err error)
    OnIterationEnd  func(iteration int, action LoopAction)
    OnChildSpawn    func(name string)
}

func (e *Executor[Data]) ExecuteWithStreaming(
    ctx context.Context,
    data Data,
    handler StreamHandler,
) *ExecutionResult {
    execCtx, events := NewStreamingExecutionContext("main", data, ...)

    go func() {
        for event := range events {
            switch e := event.(type) {
            case TokenDeltaTrace:
                if handler.OnToken != nil {
                    handler.OnToken(e.Token)
                }
            // ... dispatch other events
            }
        }
    }()

    return e.executeInternal(ctx, execCtx)
}
```

---

## Comparison Matrix

| Criteria | Callbacks | Channels | Iterator | Trace Stream | Writer |
|----------|-----------|----------|----------|--------------|--------|
| **Maintainability** | ⭐⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐ |
| **Ease of Use** | ⭐⭐⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐⭐ |
| **Testability** | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐ |
| **Go-idiomatic** | ⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐ |
| **Backward Compat** | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐ |
| **Web Integration** | ⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ |

---

## Final Recommendation

**Use the Hybrid approach** with:
1. **Trace Event Streaming** as the foundation (extends existing architecture naturally)
2. **StreamHandler callbacks** as the simple user-facing API
3. **StreamingModel interface** for models that support token streaming

This gives you:
- **Maintainability**: One event system for both tracing and streaming
- **Ease of use**: Simple callback struct for common cases
- **Testability**: Mock handlers, captured events, no channel coordination
- **Backward compatibility**: Existing code unchanged, opt-in streaming
- **Flexibility**: Power users can consume the raw channel

---

## Open Questions

1. Should `StreamingModel` be a separate interface or a method on `Model`?
2. How to handle streaming when model doesn't support it (fallback to buffered)?
3. Should trace events be typed more granularly (separate types vs enum)?
4. Buffer size defaults and overflow handling strategy?
5. How to handle streaming across child agent boundaries?
