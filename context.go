package gent

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// HookFirer is an interface for firing hooks from within framework
// components. This is implemented by hooks.Registry and set on
// ExecutionContext by the Executor.
type HookFirer interface {
	// Model call hooks
	FireBeforeModelCall(
		execCtx *ExecutionContext,
		event *BeforeModelCallEvent,
	)
	FireAfterModelCall(
		execCtx *ExecutionContext,
		event *AfterModelCallEvent,
	)

	// Tool call hooks
	FireBeforeToolCall(
		execCtx *ExecutionContext,
		event *BeforeToolCallEvent,
	)
	FireAfterToolCall(
		execCtx *ExecutionContext,
		event *AfterToolCallEvent,
	)
}

// ExecutionContext is the central context passed through all framework components.
//
// # Key Features
//
//   - Automatic tracing: Trace() records events with timestamps and iteration info
//   - Stats tracking: Stats are auto-updated from traces and propagate hierarchically
//   - Limit enforcement: Execution terminates when limits are exceeded
//   - Cancellation: Propagates to all child contexts and ongoing operations
//   - Nested loops: Child contexts share stats with parents for aggregate limits
//
// # Usage in Tools
//
//	func (t *MyTool) Call(ctx context.Context, input Input) (*ToolResult[Output], error) {
//	    // Use standard context for external calls (inherits cancellation)
//	    result, err := externalAPI.Call(ctx, input)
//	    return &ToolResult[Output]{Text: result}, err
//	}
//
// # Usage in Custom AgentLoop
//
//	func (l *MyLoop) Next(execCtx *ExecutionContext) (*AgentLoopResult, error) {
//	    // Access loop data
//	    data := execCtx.Data().(*MyLoopData)
//
//	    // Check current iteration
//	    fmt.Printf("Iteration %d\n", execCtx.Iteration())
//
//	    // Record custom trace
//	    execCtx.Trace(CustomTrace{Name: "my_event", Data: map[string]any{"key": "value"}})
//
//	    // Use standard context for model calls
//	    response, err := model.GenerateContent(execCtx, ...)
//	}
//
// # Nested Agent Loops
//
// For tools that run sub-agents, create a child context:
//
//	func (t *SubAgentTool) Call(ctx context.Context, input Input) (*ToolResult[Output], error) {
//	    // Create child context (stats propagate to parent)
//	    childCtx := parentExecCtx.NewChild("sub-agent", childData)
//
//	    // Run sub-agent
//	    exec := executor.New(subAgent, config)
//	    exec.Execute(childCtx)
//
//	    return &ToolResult[Output]{Text: childCtx.FinalResult()}, nil
//	}
//
// # Thread Safety
//
// All methods are safe for concurrent use. Multiple goroutines can trace events,
// update stats, and access data concurrently.
type ExecutionContext struct {
	mu sync.RWMutex

	// Go context for cancellation (created via context.WithCancelCause)
	goCtx  context.Context
	cancel context.CancelCauseFunc

	// Limits that trigger execution termination
	limits        []Limit
	exceededLimit *Limit // set when a limit is exceeded

	// Execution result (populated on termination)
	result *ExecutionResult

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
	stats *ExecutionStats

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

// NewExecutionContext creates a new root ExecutionContext with the given name and data.
//
// The provided context.Context is used for cancellation propagation. When limits are
// exceeded, the context is cancelled via context.WithCancelCause, which propagates
// to all child contexts and ongoing operations.
//
// Default limits are applied automatically. Use SetLimits to customize.
func NewExecutionContext(ctx context.Context, name string, data LoopData) *ExecutionContext {
	ctx, cancel := context.WithCancelCause(ctx)
	execCtx := &ExecutionContext{
		goCtx:     ctx,
		cancel:    cancel,
		limits:    DefaultLimits(),
		name:      name,
		data:      data,
		depth:     0,
		events:    make([]TraceEvent, 0),
		startTime: time.Now(),
		streamHub: newStreamHub(),
	}
	// Create stats with back-reference for limit checking
	execCtx.stats = newExecutionStatsWithContext(execCtx)
	return execCtx
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

// -----------------------------------------------------------------------------
// Context and Limits
// -----------------------------------------------------------------------------

// Context returns the underlying context.Context for this execution.
// Use this when calling external APIs that require context.Context.
//
// The context is cancelled when:
//   - A configured limit is exceeded
//   - The parent context is cancelled
//   - The execution terminates
func (ctx *ExecutionContext) Context() context.Context {
	return ctx.goCtx
}

// SetLimits configures the limits for this execution.
// Replaces any previously set limits, including defaults.
//
// Limits are evaluated on every stat update. When a limit is exceeded,
// the context is cancelled and ExceededLimit() returns the exceeded limit.
//
// Must be called before execution starts.
func (ctx *ExecutionContext) SetLimits(limits []Limit) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.limits = limits
}

// Limits returns the configured limits.
func (ctx *ExecutionContext) Limits() []Limit {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	result := make([]Limit, len(ctx.limits))
	copy(result, ctx.limits)
	return result
}

// ExceededLimit returns the limit that was exceeded, or nil if no limit was exceeded.
func (ctx *ExecutionContext) ExceededLimit() *Limit {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return ctx.exceededLimit
}

// Result returns the execution result. Only valid after execution completes.
// Returns nil if execution has not completed.
func (ctx *ExecutionContext) Result() *ExecutionResult {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return ctx.result
}

// -----------------------------------------------------------------------------
// Limit Checking (internal)
// -----------------------------------------------------------------------------

// checkLimitsIfRoot checks limits only on the root context.
// This is called by ExecutionStats when counters/gauges are updated.
// Limit checking happens on the root because it has the aggregated stats from all children.
func (ctx *ExecutionContext) checkLimitsIfRoot() {
	// Only check on root context (has aggregated stats)
	if ctx.parent != nil {
		return
	}

	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	// Already exceeded, don't check again
	if ctx.exceededLimit != nil {
		return
	}

	// Check all limits
	if limit := ctx.evaluateLimitsLocked(); limit != nil {
		ctx.exceededLimit = limit
		ctx.cancel(fmt.Errorf("limit exceeded: %s > %v", limit.Key, limit.MaxValue))
	}
}

// evaluateLimitsLocked evaluates all limits against current stats.
// Returns the first exceeded limit, or nil if all limits are within bounds.
// Must be called with lock held.
func (ctx *ExecutionContext) evaluateLimitsLocked() *Limit {
	for i := range ctx.limits {
		limit := &ctx.limits[i]
		if ctx.isLimitExceededLocked(limit) {
			return limit
		}
	}
	return nil
}

// isLimitExceededLocked checks if a single limit is exceeded.
// Must be called with lock held.
func (ctx *ExecutionContext) isLimitExceededLocked(limit *Limit) bool {
	switch limit.Type {
	case LimitExactKey:
		return ctx.checkExactKeyLimit(limit)
	case LimitKeyPrefix:
		return ctx.checkPrefixLimit(limit)
	default:
		return false
	}
}

// checkExactKeyLimit checks if an exact key limit is exceeded.
func (ctx *ExecutionContext) checkExactKeyLimit(limit *Limit) bool {
	// Check counters first
	if val := ctx.stats.GetCounter(limit.Key); val > 0 {
		if float64(val) > limit.MaxValue {
			return true
		}
	}
	// Check gauges
	if val := ctx.stats.GetGauge(limit.Key); val > 0 {
		if val > limit.MaxValue {
			return true
		}
	}
	return false
}

// checkPrefixLimit checks if any key with the given prefix exceeds the limit.
func (ctx *ExecutionContext) checkPrefixLimit(limit *Limit) bool {
	// Check all counters with matching prefix
	for key, val := range ctx.stats.Counters() {
		if len(key) >= len(limit.Key) && key[:len(limit.Key)] == limit.Key {
			if float64(val) > limit.MaxValue {
				return true
			}
		}
	}
	// Check all gauges with matching prefix
	for key, val := range ctx.stats.Gauges() {
		if len(key) >= len(limit.Key) && key[:len(limit.Key)] == limit.Key {
			if val > limit.MaxValue {
				return true
			}
		}
	}
	return false
}

// FireBeforeModelCall fires the BeforeModelCall hook if a hook firer
// is set. Hooks may modify event.Request for ephemeral context
// injection.
func (ctx *ExecutionContext) FireBeforeModelCall(
	event *BeforeModelCallEvent,
) {
	ctx.mu.RLock()
	firer := ctx.hookFirer
	ctx.mu.RUnlock()

	if firer != nil {
		firer.FireBeforeModelCall(ctx, event)
	}
}

// FireAfterModelCall fires the AfterModelCall hook if a hook firer
// is set.
func (ctx *ExecutionContext) FireAfterModelCall(
	event *AfterModelCallEvent,
) {
	ctx.mu.RLock()
	firer := ctx.hookFirer
	ctx.mu.RUnlock()

	if firer != nil {
		firer.FireAfterModelCall(ctx, event)
	}
}

// FireBeforeToolCall fires the BeforeToolCall hook if a hook firer
// is set.
func (ctx *ExecutionContext) FireBeforeToolCall(
	event *BeforeToolCallEvent,
) {
	ctx.mu.RLock()
	firer := ctx.hookFirer
	ctx.mu.RUnlock()

	if firer != nil {
		firer.FireBeforeToolCall(ctx, event)
	}
}

// FireAfterToolCall fires the AfterToolCall hook if a hook firer
// is set.
func (ctx *ExecutionContext) FireAfterToolCall(
	event *AfterToolCallEvent,
) {
	ctx.mu.RLock()
	firer := ctx.hookFirer
	ctx.mu.RUnlock()

	if firer != nil {
		firer.FireAfterToolCall(ctx, event)
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
// This also updates the protected KeyIterations stat counter for limit checking.
func (ctx *ExecutionContext) StartIteration() {
	ctx.mu.Lock()
	ctx.iteration++
	// Increment the protected iteration counter in stats (using no-limit-check to avoid deadlock)
	ctx.stats.incrCounterNoLimitCheck(KeyIterations, 1)
	ctx.appendEventLocked(IterationStartTrace{
		BaseTrace: ctx.baseTraceLocked(),
	})
	ctx.mu.Unlock()

	// Trigger limit check after releasing lock to avoid deadlock
	ctx.checkLimitsIfRoot()
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
	ctx.traceEventLocked(event)
	ctx.mu.Unlock()

	// Trigger limit check after releasing lock to avoid deadlock
	ctx.checkLimitsIfRoot()
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
// Note: Uses internal no-limit-check versions to avoid deadlock. Caller must trigger
// limit checks after releasing the lock.
func (ctx *ExecutionContext) traceEventLocked(event TraceEvent) {
	// Update BaseTrace fields if the event has them
	event = ctx.populateBaseTrace(event)

	// Update stats based on event type (auto-aggregation)
	// Use no-limit-check versions to avoid deadlock (limit check done after lock release)
	switch e := event.(type) {
	case ModelCallTrace:
		ctx.stats.incrCounterNoLimitCheck(KeyInputTokens, int64(e.InputTokens))
		ctx.stats.incrCounterNoLimitCheck(KeyOutputTokens, int64(e.OutputTokens))
		if e.Model != "" {
			ctx.stats.incrCounterNoLimitCheck(KeyInputTokensFor+e.Model, int64(e.InputTokens))
			ctx.stats.incrCounterNoLimitCheck(KeyOutputTokensFor+e.Model, int64(e.OutputTokens))
		}
	case ToolCallTrace:
		ctx.stats.incrCounterNoLimitCheck(KeyToolCalls, 1)
		if e.ToolName != "" {
			ctx.stats.incrCounterNoLimitCheck(KeyToolCallsFor+e.ToolName, 1)
		}
		if e.Error != nil {
			// Track error stats
			ctx.stats.incrCounterNoLimitCheck(KeyToolCallsErrorTotal, 1)
			ctx.stats.incrCounterNoLimitCheck(KeyToolCallsErrorConsecutive, 1)
			if e.ToolName != "" {
				ctx.stats.incrCounterNoLimitCheck(KeyToolCallsErrorFor+e.ToolName, 1)
				ctx.stats.incrCounterNoLimitCheck(KeyToolCallsErrorConsecutiveFor+e.ToolName, 1)
			}
		}
		// Note: Consecutive error counters are reset by toolchain implementations
		// when a tool call succeeds, matching the pattern used by parse error stats.
	case ParseErrorTrace:
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

// Stats returns the execution stats for this context.
//
// Thread Safety:
//   - Safe to call from any goroutine
//   - All ExecutionStats methods handle their own locking
//   - Safe to read during hook execution (hooks run synchronously)
func (ctx *ExecutionContext) Stats() *ExecutionStats {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return ctx.stats
}

// -----------------------------------------------------------------------------
// Nesting
// -----------------------------------------------------------------------------

// SpawnChild creates a child ExecutionContext for nested agent loops.
// The child is automatically linked to the parent and a ChildSpawnTrace is recorded.
// The child's stats are linked to the parent's stats for real-time aggregation.
//
// The child context inherits the parent's context.Context, so cancelling the parent
// (e.g., due to limit exceeded) automatically cancels all children.
func (ctx *ExecutionContext) SpawnChild(name string, data LoopData) *ExecutionContext {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	// Create child context that is cancelled when parent is cancelled
	childGoCtx, childCancel := context.WithCancelCause(ctx.goCtx)

	child := &ExecutionContext{
		goCtx:     childGoCtx,
		cancel:    childCancel,
		limits:    ctx.limits, // Inherit parent limits
		name:      name,
		data:      data,
		depth:     ctx.depth + 1,
		parent:    ctx,
		events:    make([]TraceEvent, 0),
		startTime: time.Now(),
		streamHub: newStreamHub(),
	}
	// Create stats with back-reference to child for limit checking
	// Stats also link to parent stats for real-time aggregation
	child.stats = newExecutionStatsWithContextAndParent(child, ctx.stats)

	ctx.children = append(ctx.children, child)
	ctx.appendEventLocked(ChildSpawnTrace{
		BaseTrace: ctx.baseTraceLocked(),
		ChildName: name,
	})

	return child
}

// CompleteChild finalizes a child context and records completion.
// This should be called via defer after SpawnChild.
//
// Note: Stats aggregation happens in real-time via parent reference,
// so this method only records the completion trace event.
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
//
// This also populates the Result() field with an ExecutionResult containing
// all termination information.
func (ctx *ExecutionContext) SetTermination(reason TerminationReason, result []ContentPart, err error) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.terminationReason = reason
	ctx.finalResult = result
	ctx.err = err
	ctx.endTime = time.Now()

	// Populate the result for easy access via Result()
	ctx.result = &ExecutionResult{
		TerminationReason: reason,
		Output:            result,
		Error:             err,
		ExceededLimit:     ctx.exceededLimit,
	}
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
