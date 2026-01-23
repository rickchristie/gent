# ExecutionContext and Trace System Design

This document describes the unified `ExecutionContext` that flows through the entire framework,
providing automatic tracing, state management, and support for nested agent loops.

## Goals

1. **Automatic tracing** - Framework components (Model, ToolChain, etc.) trace automatically; users
   don't think about it.
2. **Extensible state** - `LoopData` interface retained for custom AgentLoop implementations.
3. **Generic trace events** - Well-known types for common events, plus generic `CustomTrace` for
   anything else.
4. **Auto-aggregated stats** - Common metrics (tokens, costs, tool calls) updated automatically.
5. **Nested agent support** - Tree structure for child executions (compaction, sub-agents, etc.).

## Core Types

### ExecutionContext

The ambient context passed through everything in the framework.

```go
type ExecutionContext struct {
    // User's custom loop data (retained as interface for extensibility)
    data LoopData

    // Current position (auto-tracked)
    iteration int
    depth     int // nesting level

    // All trace events (append-only log)
    events []TraceEvent

    // Aggregates (auto-updated when certain events are traced)
    stats ExecutionStats

    // Nesting support
    parent   *ExecutionContext
    children []*ExecutionContext
    name     string // "main", "compaction", "tool:search", etc.

    // Timing
    startTime time.Time
    endTime   time.Time

    // Termination
    terminationReason TerminationReason
}

type ExecutionStats struct {
    TotalInputTokens   int
    TotalOutputTokens  int
    TotalCost          float64
    InputTokensByModel  map[string]int
    OutputTokensByModel map[string]int
    CostByModel         map[string]float64
    ToolCallCount       int
    ToolCallsByName     map[string]int
}

type TerminationReason string

const (
    TerminationSuccess         TerminationReason = "success"
    TerminationMaxIterations   TerminationReason = "max_iterations"
    TerminationError           TerminationReason = "error"
    TerminationContextCanceled TerminationReason = "context_canceled"
    TerminationHookAbort       TerminationReason = "hook_abort"
)
```

### ExecutionContext Methods

```go
// Trace records an event and auto-updates aggregates based on event type.
func (ctx *ExecutionContext) Trace(event TraceEvent)

// TraceCustom is a convenience method for custom trace events.
func (ctx *ExecutionContext) TraceCustom(name string, data map[string]any)

// Data returns the user's LoopData.
func (ctx *ExecutionContext) Data() LoopData

// Iteration returns the current iteration number (1-indexed).
func (ctx *ExecutionContext) Iteration() int

// Stats returns the current aggregated stats.
func (ctx *ExecutionContext) Stats() ExecutionStats

// Events returns all recorded trace events.
func (ctx *ExecutionContext) Events() []TraceEvent

// --- Iteration management (called by Executor) ---

// StartIteration begins a new iteration, recording an IterationStartTrace.
func (ctx *ExecutionContext) StartIteration()

// EndIteration completes the current iteration, recording an IterationEndTrace.
func (ctx *ExecutionContext) EndIteration(action LoopAction)

// --- Nesting ---

// SpawnChild creates a child ExecutionContext for nested agent loops.
// The child is automatically linked to the parent.
func (ctx *ExecutionContext) SpawnChild(name string, data LoopData) *ExecutionContext

// CompleteChild finalizes a child context and attaches it to the parent.
// This should be called via defer after SpawnChild.
func (ctx *ExecutionContext) CompleteChild(child *ExecutionContext)

// Parent returns the parent context, or nil if this is the root.
func (ctx *ExecutionContext) Parent() *ExecutionContext

// Children returns all child contexts.
func (ctx *ExecutionContext) Children() []*ExecutionContext

// Name returns the name of this execution context.
func (ctx *ExecutionContext) Name() string

// Depth returns the nesting depth (0 for root).
func (ctx *ExecutionContext) Depth() int
```

### LoopData Interface (Retained)

Users can implement their own LoopData to store custom state for their AgentLoop.

```go
type LoopData interface {
    // GetOriginalInput returns the original input that started the agent loop.
    GetOriginalInput() []ContentPart

    // GetIterationHistory returns all Iterations recorded, including compacted ones.
    GetIterationHistory() []*Iteration

    // AddIterationHistory adds a new Iteration to the full history only.
    AddIterationHistory(iter *Iteration)

    // GetIterations returns Iterations that will be used in the next iteration.
    // When compaction happens, some earlier iterations may be removed from this slice,
    // but they are preserved in GetIterationHistory.
    GetIterations() []*Iteration

    // SetIterations sets the iterations to be used in next iteration.
    SetIterations([]*Iteration)
}

// Iteration represents a single iteration's message content (renamed from IterationInfo).
type Iteration struct {
    Messages []MessageContent
}
```

## Trace Event System

### Base Types

```go
// TraceEvent is the marker interface for all trace events.
type TraceEvent interface {
    traceEvent() // marker method
}

// BaseTrace contains common fields auto-populated by ExecutionContext.Trace().
type BaseTrace struct {
    Timestamp time.Time
    Iteration int
    Depth     int
}
```

### Well-Known Trace Types

These have specific fields and trigger automatic stat updates.

```go
// ModelCallTrace records an LLM API call.
// Auto-updates: TotalInputTokens, TotalOutputTokens, TotalCost, *ByModel maps.
type ModelCallTrace struct {
    BaseTrace
    Model        string
    InputTokens  int
    OutputTokens int
    Cost         float64
    Duration     time.Duration
    Error        error
}

// ToolCallTrace records a tool execution.
// Auto-updates: ToolCallCount, ToolCallsByName.
type ToolCallTrace struct {
    BaseTrace
    ToolName string
    Input    any
    Output   any
    Duration time.Duration
    Error    error
}

// IterationStartTrace marks the beginning of an iteration.
type IterationStartTrace struct {
    BaseTrace
}

// IterationEndTrace marks the end of an iteration.
type IterationEndTrace struct {
    BaseTrace
    Duration time.Duration
    Action   LoopAction
}

// ChildSpawnTrace records when a child ExecutionContext is created.
type ChildSpawnTrace struct {
    BaseTrace
    ChildName string
}

// ChildCompleteTrace records when a child ExecutionContext completes.
type ChildCompleteTrace struct {
    BaseTrace
    ChildName         string
    TerminationReason TerminationReason
    Duration          time.Duration
}
```

### Generic Trace Type

For custom AgentLoop implementations to record their own events.

```go
// CustomTrace allows recording arbitrary trace data.
type CustomTrace struct {
    BaseTrace
    Name string
    Data map[string]any
}
```

## Framework Integration

All framework components accept `ExecutionContext` and trace automatically.

### Model Interface

```go
type Model interface {
    Generate(
        ctx context.Context,
        execCtx *ExecutionContext,
        messages []MessageContent,
    ) (*ModelResponse, error)
}

// Implementation automatically traces:
func (m *modelImpl) Generate(
    ctx context.Context,
    execCtx *ExecutionContext,
    messages []MessageContent,
) (*ModelResponse, error) {
    start := time.Now()
    resp, err := m.underlying.Generate(ctx, messages)

    // Automatic tracing - user doesn't think about this
    execCtx.Trace(ModelCallTrace{
        Model:        m.name,
        InputTokens:  resp.Usage.InputTokens,
        OutputTokens: resp.Usage.OutputTokens,
        Cost:         m.calculateCost(resp.Usage),
        Duration:     time.Since(start),
        Error:        err,
    })

    return resp, err
}
```

### ToolChain Interface

```go
type ToolChain interface {
    Execute(
        ctx context.Context,
        execCtx *ExecutionContext,
        call ToolCall,
    ) (*ToolResult, error)
}

// Implementation automatically traces tool calls.
```

### TextOutputFormat Interface

```go
type TextOutputFormat interface {
    Parse(
        ctx context.Context,
        execCtx *ExecutionContext,
        output string,
    ) (*ParsedOutput, error)
}
```

### AgentLoop Interface

```go
type AgentLoop[Data LoopData] interface {
    // Next performs one iteration of the agent loop (renamed from Iterate).
    Next(ctx context.Context, execCtx *ExecutionContext) *AgentLoopResult
}

type AgentLoopResult struct {
    Action     LoopAction
    NextPrompt string       // Only set when Action is LAContinue
    Result     []ContentPart // Only set when Action is LATerminate
}
```

### Hooks

Hooks receive `ExecutionContext` as a separate parameter from the event, making it clear that
the context is always available and allowing hooks to spawn child contexts if needed.

```go
type Hook interface {
    Fire(ctx context.Context, execCtx *ExecutionContext, event HookEvent) error
}

type HookEvent interface {
    hookEvent() // marker method
}

type BeforeIterationEvent struct {
    Iteration int
}

type AfterIterationEvent struct {
    Iteration int
    Result    *AgentLoopResult
    Duration  time.Duration
}

type BeforeExecutionEvent struct{}

type AfterExecutionEvent struct {
    Result            []ContentPart
    TerminationReason TerminationReason
    Error             error
}
```

## Nested Agent Loops

When an AgentLoop needs to call another AgentLoop (e.g., for compaction), it spawns a child
context. The child's traces are automatically attached to the parent.

### Example: Compaction Sub-Agent

```go
func (loop *ReActLoop) Next(
    ctx context.Context,
    execCtx *ExecutionContext,
) *AgentLoopResult {
    // ... normal iteration logic ...

    if needsCompaction {
        compactedIterations, err := loop.runCompaction(ctx, execCtx)
        if err != nil {
            return &AgentLoopResult{Action: LATerminate, Error: err}
        }
        execCtx.Data().SetIterations(compactedIterations)
    }

    // ... continue with iteration ...
}

func (loop *ReActLoop) runCompaction(
    ctx context.Context,
    execCtx *ExecutionContext,
) ([]*Iteration, error) {
    // Create child context - defer ensures CompleteChild is always called
    childData := NewCompactionLoopData(execCtx.Data())
    childCtx := execCtx.SpawnChild("compaction", childData)
    defer execCtx.CompleteChild(childCtx)

    // Run compaction agent with the child context
    result, err := loop.executor.Execute(ctx, childCtx, loop.compactionAgent)
    if err != nil {
        return nil, err
    }

    return extractCompactedIterations(result), nil
}
```

### Trace Tree Structure

```
ExecutionContext "main"
├── IterationStartTrace {iteration: 1}
├── ModelCallTrace {model: "claude-3", tokens: 1500}
├── IterationEndTrace {iteration: 1, action: continue}
├── IterationStartTrace {iteration: 2}
├── ModelCallTrace {model: "claude-3", tokens: 2000}
├── ToolCallTrace {tool: "search", duration: 500ms}
├── IterationEndTrace {iteration: 2, action: continue}
├── IterationStartTrace {iteration: 3}
├── ModelCallTrace {model: "claude-3", tokens: 800}
├── ChildSpawnTrace {name: "compaction"}
│   └── ExecutionContext "compaction" (child)
│       ├── IterationStartTrace {iteration: 1}
│       ├── ModelCallTrace {model: "claude-3-haiku", tokens: 3000}
│       ├── IterationEndTrace {iteration: 1, action: continue}
│       ├── IterationStartTrace {iteration: 2}
│       ├── ModelCallTrace {model: "claude-3-haiku", tokens: 500}
│       └── IterationEndTrace {iteration: 2, action: terminate}
├── ChildCompleteTrace {name: "compaction", reason: success}
├── IterationEndTrace {iteration: 3, action: continue}
├── IterationStartTrace {iteration: 4}
├── ModelCallTrace {model: "claude-3", tokens: 1200}
└── IterationEndTrace {iteration: 4, action: terminate}

Stats (auto-aggregated, includes children):
  TotalInputTokens: 7000
  TotalOutputTokens: ...
  InputTokensByModel: {"claude-3": 3500, "claude-3-haiku": 3500}
  ToolCallCount: 1
  ToolCallsByName: {"search": 1}
```

## Executor Integration

The Executor creates and manages the root ExecutionContext.

```go
func (e *Executor[Data]) Execute(
    ctx context.Context,
    data Data,
) (*ExecutionResult, error) {
    execCtx := NewExecutionContext("main", data)

    // Fire before execution hook
    if err := e.hooks.Fire(ctx, execCtx, BeforeExecutionEvent{}); err != nil {
        return nil, err
    }

    for {
        execCtx.StartIteration()

        // Fire before iteration hook
        if err := e.hooks.Fire(ctx, execCtx, BeforeIterationEvent{
            Iteration: execCtx.Iteration(),
        }); err != nil {
            execCtx.EndIteration(LATerminate)
            break
        }

        start := time.Now()
        result := e.agentLoop.Next(ctx, execCtx)
        duration := time.Since(start)

        execCtx.EndIteration(result.Action)

        // Fire after iteration hook
        if err := e.hooks.Fire(ctx, execCtx, AfterIterationEvent{
            Iteration: execCtx.Iteration(),
            Result:    result,
            Duration:  duration,
        }); err != nil {
            break
        }

        if result.Action == LATerminate {
            break
        }

        // Check max iterations, context cancellation, etc.
    }

    // Fire after execution hook
    e.hooks.Fire(ctx, execCtx, AfterExecutionEvent{
        Result:            execCtx.FinalResult(),
        TerminationReason: execCtx.TerminationReason(),
        Error:             execCtx.Error(),
    })

    return &ExecutionResult{
        Result:  execCtx.FinalResult(),
        Context: execCtx, // Full trace available here
        Error:   execCtx.Error(),
    }, nil
}
```

## Summary

| Component | Role |
|-----------|------|
| `ExecutionContext` | Unified context flowing through everything; holds state, traces, stats |
| `LoopData` | Interface for custom AgentLoop state (user-extensible) |
| `TraceEvent` | Base interface for all trace events |
| `ModelCallTrace`, `ToolCallTrace`, etc. | Well-known trace types with auto-aggregation |
| `CustomTrace` | Generic trace for custom events |
| `ExecutionStats` | Auto-aggregated metrics (tokens, costs, tool calls) |

Key design principles:
- **ExecutionContext flows through everything** - Model, ToolChain, Hooks, AgentLoop
- **Framework components trace automatically** - Users get tracing for free
- **LoopData remains as interface** - For custom AgentLoop state
- **Tree structure for nesting** - Child contexts attach to parent via SpawnChild/CompleteChild
- **Defer pattern for children** - CompleteChild called via defer ensures cleanup
