package gent

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// HookFirer is an interface for firing hooks from within framework components.
// This is implemented by hooks.Registry and set on ExecutionContext by the Executor.
type HookFirer interface {
	// Model call hooks
	FireBeforeModelCall(ctx context.Context, execCtx *ExecutionContext, event BeforeModelCallEvent)
	FireAfterModelCall(ctx context.Context, execCtx *ExecutionContext, event AfterModelCallEvent)

	// Tool call hooks
	FireBeforeToolCall(
		ctx context.Context,
		execCtx *ExecutionContext,
		event *BeforeToolCallEvent,
	) error
	FireAfterToolCall(ctx context.Context, execCtx *ExecutionContext, event AfterToolCallEvent)
}

// ExecutionContext is the ambient context passed through everything in the framework.
// It provides automatic tracing, state management, and support for nested agent loops.
//
// All framework components (Model, ToolChain, Hooks, AgentLoop) receive ExecutionContext,
// enabling automatic trace collection without manual wiring.
type ExecutionContext struct {
	mu sync.RWMutex

	// User's custom loop data (retained as interface for extensibility)
	data LoopData

	// Execution name (e.g., "main", "compaction", "tool:search")
	name string

	// Current position (auto-tracked)
	iteration int
	depth     int // nesting level (0 for root)

	// All trace events (append-only log)
	events []TraceEvent

	// Aggregates (auto-updated when certain events are traced)
	stats ExecutionStats

	// Nesting support
	parent   *ExecutionContext
	children []*ExecutionContext

	// Timing
	startTime time.Time
	endTime   time.Time

	// Termination
	terminationReason TerminationReason
	finalResult       []ContentPart
	err               error

	// Hook firer for model/tool call hooks (set by Executor)
	hookFirer HookFirer

	// Streaming support
	streamHub *streamHub
}

// ExecutionStats contains auto-aggregated metrics from trace events.
type ExecutionStats struct {
	TotalInputTokens    int
	TotalOutputTokens   int
	TotalCost           float64
	InputTokensByModel  map[string]int
	OutputTokensByModel map[string]int
	CostByModel         map[string]float64
	ToolCallCount       int
	ToolCallsByName     map[string]int
}

// NewExecutionContext creates a new root ExecutionContext with the given name and data.
func NewExecutionContext(name string, data LoopData) *ExecutionContext {
	return &ExecutionContext{
		name:      name,
		data:      data,
		depth:     0,
		events:    make([]TraceEvent, 0),
		startTime: time.Now(),
		stats: ExecutionStats{
			InputTokensByModel:  make(map[string]int),
			OutputTokensByModel: make(map[string]int),
			CostByModel:         make(map[string]float64),
			ToolCallsByName:     make(map[string]int),
		},
		streamHub: newStreamHub(),
	}
}

// -----------------------------------------------------------------------------
// Data Access
// -----------------------------------------------------------------------------

// Data returns the user's LoopData.
func (ctx *ExecutionContext) Data() LoopData {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return ctx.data
}

// Name returns the name of this execution context.
func (ctx *ExecutionContext) Name() string {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return ctx.name
}

// SetHookFirer sets the hook firer for model call hooks.
// This is called by the Executor to enable model call hook firing.
func (ctx *ExecutionContext) SetHookFirer(firer HookFirer) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.hookFirer = firer
}

// FireBeforeModelCall fires the BeforeModelCall hook if a hook firer is set.
func (ctx *ExecutionContext) FireBeforeModelCall(
	goCtx context.Context,
	event BeforeModelCallEvent,
) {
	ctx.mu.RLock()
	firer := ctx.hookFirer
	ctx.mu.RUnlock()

	if firer != nil {
		firer.FireBeforeModelCall(goCtx, ctx, event)
	}
}

// FireAfterModelCall fires the AfterModelCall hook if a hook firer is set.
func (ctx *ExecutionContext) FireAfterModelCall(
	goCtx context.Context,
	event AfterModelCallEvent,
) {
	ctx.mu.RLock()
	firer := ctx.hookFirer
	ctx.mu.RUnlock()

	if firer != nil {
		firer.FireAfterModelCall(goCtx, ctx, event)
	}
}

// FireBeforeToolCall fires the BeforeToolCall hook if a hook firer is set.
// Returns an error if a hook aborts the tool call.
func (ctx *ExecutionContext) FireBeforeToolCall(
	goCtx context.Context,
	event *BeforeToolCallEvent,
) error {
	ctx.mu.RLock()
	firer := ctx.hookFirer
	ctx.mu.RUnlock()

	if firer != nil {
		return firer.FireBeforeToolCall(goCtx, ctx, event)
	}
	return nil
}

// FireAfterToolCall fires the AfterToolCall hook if a hook firer is set.
func (ctx *ExecutionContext) FireAfterToolCall(
	goCtx context.Context,
	event AfterToolCallEvent,
) {
	ctx.mu.RLock()
	firer := ctx.hookFirer
	ctx.mu.RUnlock()

	if firer != nil {
		firer.FireAfterToolCall(goCtx, ctx, event)
	}
}

// -----------------------------------------------------------------------------
// Iteration Management
// -----------------------------------------------------------------------------

// Iteration returns the current iteration number (1-indexed).
// Returns 0 if no iteration has started.
func (ctx *ExecutionContext) Iteration() int {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return ctx.iteration
}

// StartIteration begins a new iteration, recording an IterationStartTrace.
// Called by the Executor at the start of each iteration.
func (ctx *ExecutionContext) StartIteration() {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.iteration++
	ctx.appendEventLocked(IterationStartTrace{
		BaseTrace: ctx.baseTraceLocked(),
	})
}

// EndIteration completes the current iteration, recording an IterationEndTrace.
// Called by the Executor at the end of each iteration.
func (ctx *ExecutionContext) EndIteration(action LoopAction, duration time.Duration) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.appendEventLocked(IterationEndTrace{
		BaseTrace: ctx.baseTraceLocked(),
		Duration:  duration,
		Action:    action,
	})
}

// -----------------------------------------------------------------------------
// Tracing
// -----------------------------------------------------------------------------

// Trace records a trace event and auto-updates aggregates based on event type.
func (ctx *ExecutionContext) Trace(event TraceEvent) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.traceEventLocked(event)
}

// TraceCustom is a convenience method for recording custom trace events.
func (ctx *ExecutionContext) TraceCustom(name string, data map[string]any) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.appendEventLocked(CustomTrace{
		BaseTrace: ctx.baseTraceLocked(),
		Name:      name,
		Data:      data,
	})
}

// traceEventLocked records an event and updates stats. Must be called with lock held.
func (ctx *ExecutionContext) traceEventLocked(event TraceEvent) {
	// Update BaseTrace fields if the event has them
	event = ctx.populateBaseTrace(event)

	// Update stats based on event type
	switch e := event.(type) {
	case ModelCallTrace:
		ctx.stats.TotalInputTokens += e.InputTokens
		ctx.stats.TotalOutputTokens += e.OutputTokens
		ctx.stats.TotalCost += e.Cost
		if e.Model != "" {
			ctx.stats.InputTokensByModel[e.Model] += e.InputTokens
			ctx.stats.OutputTokensByModel[e.Model] += e.OutputTokens
			ctx.stats.CostByModel[e.Model] += e.Cost
		}
	case ToolCallTrace:
		ctx.stats.ToolCallCount++
		if e.ToolName != "" {
			ctx.stats.ToolCallsByName[e.ToolName]++
		}
	}

	ctx.appendEventLocked(event)
}

// populateBaseTrace fills in BaseTrace fields if not set.
func (ctx *ExecutionContext) populateBaseTrace(event TraceEvent) TraceEvent {
	switch e := event.(type) {
	case ModelCallTrace:
		if e.Timestamp.IsZero() {
			e.Timestamp = time.Now()
		}
		if e.Iteration == 0 {
			e.Iteration = ctx.iteration
		}
		if e.Depth == 0 {
			e.Depth = ctx.depth
		}
		return e
	case ToolCallTrace:
		if e.Timestamp.IsZero() {
			e.Timestamp = time.Now()
		}
		if e.Iteration == 0 {
			e.Iteration = ctx.iteration
		}
		if e.Depth == 0 {
			e.Depth = ctx.depth
		}
		return e
	case CustomTrace:
		if e.Timestamp.IsZero() {
			e.Timestamp = time.Now()
		}
		if e.Iteration == 0 {
			e.Iteration = ctx.iteration
		}
		if e.Depth == 0 {
			e.Depth = ctx.depth
		}
		return e
	}
	return event
}

// appendEventLocked appends an event to the log. Must be called with lock held.
func (ctx *ExecutionContext) appendEventLocked(event TraceEvent) {
	ctx.events = append(ctx.events, event)
}

// baseTraceLocked creates a BaseTrace with current context. Must be called with lock held.
func (ctx *ExecutionContext) baseTraceLocked() BaseTrace {
	return BaseTrace{
		Timestamp: time.Now(),
		Iteration: ctx.iteration,
		Depth:     ctx.depth,
	}
}

// Events returns a copy of all recorded trace events.
func (ctx *ExecutionContext) Events() []TraceEvent {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	result := make([]TraceEvent, len(ctx.events))
	copy(result, ctx.events)
	return result
}

// Stats returns a copy of the current aggregated stats.
func (ctx *ExecutionContext) Stats() ExecutionStats {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	// Deep copy maps
	stats := ExecutionStats{
		TotalInputTokens:    ctx.stats.TotalInputTokens,
		TotalOutputTokens:   ctx.stats.TotalOutputTokens,
		TotalCost:           ctx.stats.TotalCost,
		ToolCallCount:       ctx.stats.ToolCallCount,
		InputTokensByModel:  make(map[string]int),
		OutputTokensByModel: make(map[string]int),
		CostByModel:         make(map[string]float64),
		ToolCallsByName:     make(map[string]int),
	}
	for k, v := range ctx.stats.InputTokensByModel {
		stats.InputTokensByModel[k] = v
	}
	for k, v := range ctx.stats.OutputTokensByModel {
		stats.OutputTokensByModel[k] = v
	}
	for k, v := range ctx.stats.CostByModel {
		stats.CostByModel[k] = v
	}
	for k, v := range ctx.stats.ToolCallsByName {
		stats.ToolCallsByName[k] = v
	}
	return stats
}

// -----------------------------------------------------------------------------
// Nesting
// -----------------------------------------------------------------------------

// SpawnChild creates a child ExecutionContext for nested agent loops.
// The child is automatically linked to the parent and a ChildSpawnTrace is recorded.
func (ctx *ExecutionContext) SpawnChild(name string, data LoopData) *ExecutionContext {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	child := &ExecutionContext{
		name:      name,
		data:      data,
		depth:     ctx.depth + 1,
		parent:    ctx,
		events:    make([]TraceEvent, 0),
		startTime: time.Now(),
		stats: ExecutionStats{
			InputTokensByModel:  make(map[string]int),
			OutputTokensByModel: make(map[string]int),
			CostByModel:         make(map[string]float64),
			ToolCallsByName:     make(map[string]int),
		},
		streamHub: newStreamHub(),
	}

	ctx.children = append(ctx.children, child)
	ctx.appendEventLocked(ChildSpawnTrace{
		BaseTrace: ctx.baseTraceLocked(),
		ChildName: name,
	})

	return child
}

// CompleteChild finalizes a child context and records completion.
// This should be called via defer after SpawnChild.
func (ctx *ExecutionContext) CompleteChild(child *ExecutionContext) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	child.mu.Lock()
	child.endTime = time.Now()
	childDuration := child.endTime.Sub(child.startTime)
	childReason := child.terminationReason
	child.mu.Unlock()

	ctx.appendEventLocked(ChildCompleteTrace{
		BaseTrace:         ctx.baseTraceLocked(),
		ChildName:         child.name,
		TerminationReason: childReason,
		Duration:          childDuration,
	})

	// Aggregate child stats into parent
	childStats := child.Stats()
	ctx.stats.TotalInputTokens += childStats.TotalInputTokens
	ctx.stats.TotalOutputTokens += childStats.TotalOutputTokens
	ctx.stats.TotalCost += childStats.TotalCost
	ctx.stats.ToolCallCount += childStats.ToolCallCount
	for k, v := range childStats.InputTokensByModel {
		ctx.stats.InputTokensByModel[k] += v
	}
	for k, v := range childStats.OutputTokensByModel {
		ctx.stats.OutputTokensByModel[k] += v
	}
	for k, v := range childStats.CostByModel {
		ctx.stats.CostByModel[k] += v
	}
	for k, v := range childStats.ToolCallsByName {
		ctx.stats.ToolCallsByName[k] += v
	}
}

// Parent returns the parent context, or nil if this is the root.
func (ctx *ExecutionContext) Parent() *ExecutionContext {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return ctx.parent
}

// Children returns all child contexts.
func (ctx *ExecutionContext) Children() []*ExecutionContext {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	result := make([]*ExecutionContext, len(ctx.children))
	copy(result, ctx.children)
	return result
}

// Depth returns the nesting depth (0 for root).
func (ctx *ExecutionContext) Depth() int {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return ctx.depth
}

// -----------------------------------------------------------------------------
// Termination
// -----------------------------------------------------------------------------

// SetTermination sets the termination reason and final result.
// Called by the Executor when execution ends.
func (ctx *ExecutionContext) SetTermination(reason TerminationReason, result []ContentPart, err error) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.terminationReason = reason
	ctx.finalResult = result
	ctx.err = err
	ctx.endTime = time.Now()
}

// TerminationReason returns why execution terminated.
func (ctx *ExecutionContext) TerminationReason() TerminationReason {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return ctx.terminationReason
}

// FinalResult returns the final result (if terminated successfully).
func (ctx *ExecutionContext) FinalResult() []ContentPart {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return ctx.finalResult
}

// Error returns the error (if terminated with error).
func (ctx *ExecutionContext) Error() error {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return ctx.err
}

// StartTime returns when execution began.
func (ctx *ExecutionContext) StartTime() time.Time {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return ctx.startTime
}

// EndTime returns when execution completed.
func (ctx *ExecutionContext) EndTime() time.Time {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return ctx.endTime
}

// Duration returns the total execution duration.
// If execution is still in progress, returns duration since start.
func (ctx *ExecutionContext) Duration() time.Duration {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	if ctx.endTime.IsZero() {
		return time.Since(ctx.startTime)
	}
	return ctx.endTime.Sub(ctx.startTime)
}

// -----------------------------------------------------------------------------
// Streaming Support
// -----------------------------------------------------------------------------

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
func (ctx *ExecutionContext) SubscribeAll() (<-chan StreamChunk, UnsubscribeFunc) {
	return ctx.streamHub.subscribeAll()
}

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
func (ctx *ExecutionContext) SubscribeToStream(streamId string) (<-chan StreamChunk, UnsubscribeFunc) {
	return ctx.streamHub.subscribeToStream(streamId)
}
 
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
func (ctx *ExecutionContext) SubscribeToTopic(topicId string) (<-chan StreamChunk, UnsubscribeFunc) {
	return ctx.streamHub.subscribeToTopic(topicId)
}

// EmitChunk emits a streaming chunk to all relevant subscribers.
// Called by model wrappers during streaming. Automatically propagates to parent.
// Safe for concurrent use.
//
// If chunk.Source is empty, it will be populated with BuildSourcePath().
func (ctx *ExecutionContext) EmitChunk(chunk StreamChunk) {
	// Populate source path if not set
	if chunk.Source == "" {
		chunk.Source = ctx.BuildSourcePath()
	}

	// Emit to local subscribers
	ctx.streamHub.emit(chunk)

	// Propagate to parent (chunk.Source already contains full path)
	// Parent is immutable after creation, so no lock needed
	ctx.mu.RLock()
	parent := ctx.parent
	ctx.mu.RUnlock()

	if parent != nil {
		parent.EmitChunk(chunk)
	}
}

// CloseStreams closes all subscription channels. Called by Executor on termination.
// Safe to call multiple times.
func (ctx *ExecutionContext) CloseStreams() {
	ctx.streamHub.close()
}

// BuildSourcePath returns the hierarchical source path for this context.
// Format: "name/iteration" or "parent-path/name/iteration"
func (ctx *ExecutionContext) BuildSourcePath() string {
	ctx.mu.RLock()
	name := ctx.name
	iteration := ctx.iteration
	parent := ctx.parent
	ctx.mu.RUnlock()

	if parent == nil {
		return fmt.Sprintf("%s/%d", name, iteration)
	}

	return fmt.Sprintf("%s/%s/%d", parent.BuildSourcePath(), name, iteration)
}
