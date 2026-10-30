package gent

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// EventPublisher is an interface for dispatching events to subscribers.
// This is implemented by events.Registry and set on ExecutionContext by the Executor.
type EventPublisher interface {
	// Dispatch sends an event to all matching subscribers.
	Dispatch(execCtx *ExecutionContext, event Event)

	// MaxRecursion returns the maximum allowed event recursion depth.
	MaxRecursion() int
}

// ExecutionContext is the central context passed through all framework components.
//
// # Key Features
//
//   - Automatic event publishing: PublishXXX() methods record events and update stats
//   - Stats tracking: Stats are auto-updated from events and propagate hierarchically
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
// # Usage in Custom Loop
//
// Read the owned Task and Scratchpad to build a request. Use GenerateContentStream on this
// context to capture effective requests, chunks, responses, and usage before continuing.
// AppendRecord and UpdateHistory preserve evidence while changing the next working context.
// Custom events can be queued with PublishCommonEvent; check FlushProgress before external work.
// See docs/history.md for checkpoint state and fresh-worker bindings for custom loops.
//
// # Nested Agent Loops
//
// SpawnChild links a nested loop's statistics and cancellation to its parent. Run the child
// executor, handle its returned error, and call CompleteChild after it exits. An active child
// frame is observable but cannot be resumed after a worker restart; completed children are saved.
//
// # Thread Safety
//
// All ExecutionContext methods are safe for concurrent use. Multiple goroutines can publish
// events and update stats concurrently. Data() returns the configured LoopData; BasicLoopData
// is safe for concurrent use, and custom LoopData implementations must provide their own
// synchronization if they are accessed concurrently.
type ExecutionContext struct {
	finalContentSource *ContentSource

	mu                 sync.RWMutex
	run                *executionRun
	savedTask          EncodedValue
	active             []EntryId
	draftRevision      uint64
	state              map[string]EncodedValue
	phase              ExecutionPhase
	completedIteration int
	restored           bool
	runCanceled        bool
	currentToolPlan    *ToolPlan
	boundToolPlans     map[string]*ToolPlan

	// Go context for cancellation (created via context.WithCancelCause)
	goCtx  context.Context
	cancel context.CancelCauseFunc
	// deadlineCancel is set only when restore owns an added deadline context.
	deadlineCancel context.CancelFunc

	// Limits that trigger execution termination
	limits        []Limit
	exceededLimit *Limit // set when a limit is exceeded

	// Execution result (populated on termination)
	result *ExecutionResult

	// User's custom loop data (retained as interface for extensibility)
	data LoopData

	// Execution name (e.g., "main", "compaction", "tool:search")
	name string

	// Opaque context identity for reliable trace correlation.
	contextId       string
	parentContextId string

	// Per-context call sequences for generated model/tool call IDs.
	nextModelCallSeq uint64
	nextToolCallSeq  uint64
	modelReplay      modelReplay

	// Current position (auto-tracked)
	iteration int
	depth     int // nesting level (0 for root)

	// All events (append-only log)
	events []Event

	// Aggregates (auto-updated when certain events are published)
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
	acceptedValue     *EncodedValue
	refusal           *ModelRefusal
	err               error
	// terminalEvent is frozen by PublishAfterExecution and queued only at the terminal save.
	// run.mu protects it so a concurrent progress save cannot accept success without the outcome.
	terminalEvent *RecordedEvent

	// Event publisher for dispatching events to subscribers (set by Executor)
	eventPublisher EventPublisher
	eventDepth     map[uint64]int // tracks recursion depth per publishing goroutine

	// Streaming support
	streamHub   *streamHub
	stopStreams func() bool

	// Compaction configuration (optional)
	compactionTrigger  CompactionTrigger
	compactionStrategy CompactionStrategy
}

// NewExecutionContext creates a new root ExecutionContext with the given name and data.
//
// The provided context.Context is used for cancellation propagation. When limits are
// exceeded, the context is cancelled via context.WithCancelCause, which propagates
// to all child contexts and ongoing operations.
//
// Default limits are applied automatically. Use SetLimits to customize.
func NewExecutionContext(
	ctx context.Context, name string, data LoopData, options ...ContextOption,
) *ExecutionContext {
	ctx, cancel := context.WithCancelCause(ctx)
	execCtx := &ExecutionContext{
		goCtx:     ctx,
		cancel:    cancel,
		limits:    DefaultLimits(),
		name:      name,
		data:      data,
		depth:     0,
		events:    make([]Event, 0),
		startTime: time.Now(),
		streamHub: newStreamHub(),
	}
	// Create stats with back-reference for limit checking
	execCtx.stats = newExecutionStatsWithContext(execCtx)
	execCtx.initializeHistory(options)
	execCtx.stopStreams = context.AfterFunc(ctx, execCtx.CloseStreams)
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

// ContextId returns the opaque identity of this execution context.
func (ctx *ExecutionContext) ContextId() string {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return ctx.contextId
}

// ParentContextId returns the opaque identity of this context's parent.
// It is empty for root contexts.
func (ctx *ExecutionContext) ParentContextId() string {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return ctx.parentContextId
}

// SetEventPublisher sets the event publisher for dispatching events to subscribers.
// This is called by the Executor to enable event publishing.
func (ctx *ExecutionContext) SetEventPublisher(publisher EventPublisher) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.eventPublisher = publisher
}

// -----------------------------------------------------------------------------
// Context and Limits
// -----------------------------------------------------------------------------

// Interrupt stops work in this context and its children. It keeps the run open for recovery
// from the last accepted checkpoint. The cause is available through context.Cause.
func (ctx *ExecutionContext) Interrupt(cause error) {
	ctx.cancel(cause)
}

// Release stops execution work and closes its stream subscriptions after queued chunks drain.
// It does not change the result or save an outcome. Executors call it on every return path;
// callers that manage an ExecutionContext directly should defer it. Repeated calls are safe.
func (ctx *ExecutionContext) Release() {
	if ctx.stopStreams != nil {
		ctx.stopStreams()
	}
	ctx.cancel(errExecutionReleased)
	for _, child := range ctx.Children() {
		child.Release()
	}
	ctx.CloseStreams()
	if ctx.deadlineCancel != nil {
		ctx.deadlineCancel()
	}
}

var errExecutionReleased = errors.New("gent: execution resources released")

// Context returns the underlying context.Context for this execution.
// Use this when calling external APIs that require context.Context.
//
// The context is cancelled when:
//   - A configured limit is exceeded
//   - The parent context is cancelled
//   - Interrupt or CancelRun is called
//   - The execution terminates
func (ctx *ExecutionContext) Context() context.Context {
	return ctx.goCtx
}

// SetLimits configures the limits for this execution.
// Replaces any previously set limits, including defaults.
//
// Limits are evaluated on every stat update, except exact iteration limits
// ([SCIterations] and [SCIterations.Self]) which the executor evaluates after each completed
// iteration so MaxValue means the maximum number of loop iterations that may run. When a limit
// is exceeded, the context is cancelled and ExceededLimit() returns the exceeded limit.
//
// Must be called before execution starts.
func (ctx *ExecutionContext) SetLimits(limits []Limit) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.limits = cloneLimits(limits)
}

// Limits returns the configured limits.
func (ctx *ExecutionContext) Limits() []Limit {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return cloneLimits(ctx.limits)
}

// cloneLimits gives each context ownership of the slice backing array. Limit checks run from
// stats updates, so sharing caller-owned or parent-owned slices would let outside mutations race
// with concurrent execution.
func cloneLimits(limits []Limit) []Limit {
	if limits == nil {
		return nil
	}
	result := make([]Limit, len(limits))
	copy(result, limits)
	return result
}

// SetCompaction configures scratchpad compaction for this
// execution. The trigger decides when to compact, the
// strategy decides how.
//
// Must be called before execution starts. Both trigger and
// strategy must be non-nil.
//
// When compaction is configured, the executor checks the
// trigger at the start of each loop iteration (after the
// first). If the trigger fires, the strategy's Compact
// method is called.
//
// If Compact returns an error, execution terminates with
// TerminationCompactionFailed.
func (ctx *ExecutionContext) SetCompaction(
	trigger CompactionTrigger,
	strategy CompactionStrategy,
) {
	if (trigger == nil) != (strategy == nil) {
		panic(
			"gent: SetCompaction requires both " +
				"trigger and strategy to be non-nil, " +
				"or both nil to disable compaction",
		)
	}
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.compactionTrigger = trigger
	ctx.compactionStrategy = strategy
}

// CompactionTrigger returns the configured trigger, or nil.
func (ctx *ExecutionContext) CompactionTrigger() CompactionTrigger {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return ctx.compactionTrigger
}

// CompactionStrategy returns the configured strategy,
// or nil.
func (ctx *ExecutionContext) CompactionStrategy() CompactionStrategy {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return ctx.compactionStrategy
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
	if ctx.result == nil {
		return nil
	}
	copy := *ctx.result
	copy.Output = CloneContentParts(ctx.result.Output)
	copy.Refusal = cloneRefusal(ctx.result.Refusal)
	if ctx.result.ExceededLimit != nil {
		limit := *ctx.result.ExceededLimit
		copy.ExceededLimit = &limit
	}
	return &copy
}

// -----------------------------------------------------------------------------
// Limit Checking (internal)
// -----------------------------------------------------------------------------

// limitExceededInfo contains information about a limit that was
// exceeded.
type limitExceededInfo struct {
	limit        *Limit
	currentValue float64
	matchedKey   StatKey
}

// checkLimits checks if any limits are exceeded on this context.
// This is called by ExecutionStats when counters/gauges are updated.
// Each context in the hierarchy checks its own limits as stats propagate up.
func (ctx *ExecutionContext) checkLimits() {
	var info *limitExceededInfo

	ctx.updateContextState(func() {
		// Already exceeded, don't check again
		if ctx.exceededLimit != nil {
			return
		}

		// Check all limits
		info = ctx.evaluateLimitsLocked()
		if info == nil {
			return
		}

		ctx.exceededLimit = info.limit
	})

	if info == nil {
		return
	}

	// Publish event outside lock to avoid deadlock
	ctx.PublishLimitExceeded(*info.limit, info.currentValue, info.matchedKey)

	// Cancel context after publishing event
	ctx.cancel(fmt.Errorf("limit exceeded: %s > %v", info.limit.Key, info.limit.MaxValue))
}

// EnforceIterationLimit checks exact iteration limits using executor semantics.
//
// Iteration limits are a hard cap on completed loop iterations, so they use >= after
// AfterIteration instead of the generic stat limit's > comparison on stat update. The check
// walks up the context tree so aggregate SCIterations limits on a parent still observe child
// iterations as they propagate.
func (ctx *ExecutionContext) EnforceIterationLimit() bool {
	if ctx == nil {
		return false
	}
	if ctx.enforceLocalIterationLimit() {
		return true
	}

	ctx.mu.RLock()
	parent := ctx.parent
	ctx.mu.RUnlock()
	if parent == nil {
		return false
	}
	return parent.EnforceIterationLimit()
}

func (ctx *ExecutionContext) enforceLocalIterationLimit() bool {
	var info *limitExceededInfo

	ctx.updateContextState(func() {
		if ctx.exceededLimit != nil {
			return
		}
		info = ctx.evaluateIterationLimitsLocked()
		if info == nil {
			return
		}
		ctx.exceededLimit = info.limit
	})

	if info == nil {
		return false
	}
	ctx.PublishLimitExceeded(*info.limit, info.currentValue, info.matchedKey)
	ctx.cancel(fmt.Errorf(
		"limit exceeded: %s >= %v", info.limit.Key, info.limit.MaxValue,
	))
	return true
}

func (ctx *ExecutionContext) evaluateIterationLimitsLocked() *limitExceededInfo {
	for i := range ctx.limits {
		limit := &ctx.limits[i]
		if !isIterationLimitKey(limit.Key) || limit.Type != LimitExactKey {
			continue
		}
		val := ctx.stats.GetCounter(limit.Key)
		if float64(val) >= limit.MaxValue {
			return &limitExceededInfo{
				limit:        limit,
				currentValue: float64(val),
				matchedKey:   limit.Key,
			}
		}
	}
	return nil
}

// evaluateLimitsLocked evaluates all limits against current stats.
// Returns info about the first exceeded limit, or nil if all limits are within bounds.
// Must be called with lock held.
func (ctx *ExecutionContext) evaluateLimitsLocked() *limitExceededInfo {
	for i := range ctx.limits {
		limit := &ctx.limits[i]
		if info := ctx.checkLimitLocked(limit); info != nil {
			return info
		}
	}
	return nil
}

// checkLimitLocked checks if a single limit is exceeded.
// Returns info about the exceeded limit, or nil if not exceeded.
// Must be called with lock held.
func (ctx *ExecutionContext) checkLimitLocked(limit *Limit) *limitExceededInfo {
	switch limit.Type {
	case LimitExactKey:
		return ctx.checkExactKeyLimit(limit)
	case LimitKeyPrefix:
		return ctx.checkPrefixLimit(limit)
	default:
		return nil
	}
}

// checkExactKeyLimit checks if an exact key limit is exceeded.
// Returns info about the exceeded limit, or nil if not exceeded.
func (ctx *ExecutionContext) checkExactKeyLimit(
	limit *Limit,
) *limitExceededInfo {
	if isIterationLimitKey(limit.Key) {
		return nil
	}
	// Check counters first
	if val := ctx.stats.GetCounter(limit.Key); val > 0 {
		if float64(val) > limit.MaxValue {
			return &limitExceededInfo{
				limit:        limit,
				currentValue: float64(val),
				matchedKey:   limit.Key,
			}
		}
	}
	// Check gauges
	if val := ctx.stats.GetGauge(limit.Key); val > 0 {
		if val > limit.MaxValue {
			return &limitExceededInfo{
				limit:        limit,
				currentValue: val,
				matchedKey:   limit.Key,
			}
		}
	}
	return nil
}

func isIterationLimitKey(key StatKey) bool {
	return key == SCIterations || key == SCIterations.Self()
}

// checkPrefixLimit checks if any key with the given prefix exceeds
// the limit.
// Returns info about the exceeded limit, or nil if not exceeded.
func (ctx *ExecutionContext) checkPrefixLimit(
	limit *Limit,
) *limitExceededInfo {
	prefix := string(limit.Key)
	// Check all counters with matching prefix
	for key, val := range ctx.stats.Counters() {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			if float64(val) > limit.MaxValue {
				return &limitExceededInfo{
					limit:        limit,
					currentValue: float64(val),
					matchedKey:   StatKey(key),
				}
			}
		}
	}
	// Check all gauges with matching prefix
	for key, val := range ctx.stats.Gauges() {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			if val > limit.MaxValue {
				return &limitExceededInfo{
					limit:        limit,
					currentValue: val,
					matchedKey:   StatKey(key),
				}
			}
		}
	}
	return nil
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

// IncrementIteration increments the iteration counter.
// Called by the Executor at the start of each iteration before publishing BeforeIterationEvent.
func (ctx *ExecutionContext) IncrementIteration() {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.iteration++
}

// -----------------------------------------------------------------------------
// Event Publishing
// -----------------------------------------------------------------------------

// updateContextState executes the given function while holding the mutex lock.
// The lock is always released via defer, ensuring safe cleanup even on panic.
func (ctx *ExecutionContext) updateContextState(fn func()) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	fn()
}

// publish is the internal implementation for all event publishing.
// It records the event, updates stats, checks limits, and dispatches to subscribers.
func (ctx *ExecutionContext) publish(event Event) {
	ctx.publishWithEventIteration(event, nil)
}

// publishWithEventIteration publishes an event and optionally pins AfterModelCallEvent to the
// iteration where its asynchronous model call started.
func (ctx *ExecutionContext) publishWithEventIteration(event Event, iteration *int) {
	var publisher EventPublisher
	var goroutineID uint64
	var depthTracked bool
	eventSlot := -1
	source := ctx.BuildSourcePath()

	ctx.updateContextState(func() {
		publisher = ctx.eventPublisher
		if publisher != nil {
			goroutineID = currentGoroutineID()
			if ctx.eventDepth == nil {
				ctx.eventDepth = make(map[uint64]int)
			}
			if ctx.eventDepth[goroutineID] >= publisher.MaxRecursion() {
				panic(fmt.Sprintf("event recursion depth exceeded maximum (%d)",
					publisher.MaxRecursion()))
			}
			ctx.eventDepth[goroutineID]++
			depthTracked = true
		}

		// Populate base event fields
		ctx.populateBaseEvent(event, source)
		if iteration != nil {
			if e, ok := event.(*AfterModelCallEvent); ok {
				e.Iteration = *iteration
			}
		}

		// Reserve a hidden slot before stats/dispatch so final event ordering remains the publish
		// order. The slot is filled only after subscribers finish mutating before-events, keeping
		// Events() from exposing in-flight mutable event pointers to concurrent readers.
		eventSlot = len(ctx.events)
		ctx.events = append(ctx.events, nil)

	})
	if depthTracked {
		defer ctx.releaseEventDepth(goroutineID)
	}

	// Update stats based on event type (outside lock because incrCounterDirect calls checkLimits)
	ctx.updateStatsForEvent(event)

	// Dispatch to subscribers
	if publisher != nil && !ctx.isRetryingTool(event) {
		observed := event
		switch event.(type) {
		case *BeforeModelCallEvent, *BeforeToolCallEvent:
			// Request hooks intentionally modify the effective input before it is captured.
		default:
			var err error
			observed, err = ctx.copyEvent(event)
			if err != nil {
				ctx.stopRecording(err)
				return
			}
		}
		publisher.Dispatch(ctx, observed)
	}

	ctx.storePublishedEvent(eventSlot, event)
	if err := ctx.recordEvent(event); err != nil {
		return
	}
}

func (ctx *ExecutionContext) storePublishedEvent(slot int, event Event) {
	if slot < 0 {
		return
	}
	owned, err := ctx.copyEvent(event)
	if err != nil {
		ctx.stopRecording(err)
		return
	}
	ctx.updateContextState(func() {
		if slot >= len(ctx.events) {
			return
		}
		ctx.events[slot] = owned
	})
}

func (ctx *ExecutionContext) releaseEventDepth(goroutineID uint64) {
	ctx.updateContextState(func() {
		depth := ctx.eventDepth[goroutineID]
		if depth <= 1 {
			delete(ctx.eventDepth, goroutineID)
			return
		}
		ctx.eventDepth[goroutineID] = depth - 1
	})
}

func currentGoroutineID() uint64 {
	// Go does not expose goroutine-local storage. Recursion protection needs to follow the
	// synchronous subscriber call stack, so we key depth by the current goroutine instead of a
	// process-wide counter that would turn concurrent publishes into false recursion.
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	line := string(buf[:n])
	const prefix = "goroutine "
	if !strings.HasPrefix(line, prefix) {
		return 0
	}
	line = strings.TrimPrefix(line, prefix)
	end := strings.IndexByte(line, ' ')
	if end < 0 {
		return 0
	}
	id, err := strconv.ParseUint(line[:end], 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// Publish records a custom event, updates stats, checks limits, and dispatches to subscribers.
// Use this for user-defined Event types. For framework events, use the typed PublishXXX methods.
func (ctx *ExecutionContext) Publish(event Event) error {
	ctx.publish(event)
	return ctx.FlushProgress()
}

// populateBaseEvent fills in BaseEvent fields.
// Must be called with lock held.
func (ctx *ExecutionContext) populateBaseEvent(event Event, source string) {
	base := eventBase(event)
	if base == nil {
		return
	}
	base.Timestamp = time.Now()
	base.Iteration = ctx.iteration
	base.Depth = ctx.depth
	if base.Source == "" {
		base.Source = source
	}
	if base.ContextId == "" {
		base.ContextId = ctx.contextId
	}
	if base.ParentContextId == "" {
		base.ParentContextId = ctx.parentContextId
	}
}

func eventBase(event Event) *BaseEvent {
	switch e := event.(type) {
	case *BeforeExecutionEvent:
		return &e.BaseEvent
	case *AfterExecutionEvent:
		return &e.BaseEvent
	case *BeforeIterationEvent:
		return &e.BaseEvent
	case *AfterIterationEvent:
		return &e.BaseEvent
	case *BeforeModelCallEvent:
		return &e.BaseEvent
	case *AfterModelCallEvent:
		return &e.BaseEvent
	case *BeforeToolCallEvent:
		return &e.BaseEvent
	case *AfterToolCallEvent:
		return &e.BaseEvent
	case *ParseErrorEvent:
		return &e.BaseEvent
	case *OutputValidationEvent:
		return &e.BaseEvent
	case *ErrorEvent:
		return &e.BaseEvent
	case *CommonEvent:
		return &e.BaseEvent
	case *StateChangeEvent:
		return &e.BaseEvent
	case *LimitExceededEvent:
		return &e.BaseEvent
	case *CompactionEvent:
		return &e.BaseEvent
	default:
		if withBase, ok := event.(interface{ EventBase() *BaseEvent }); ok {
			return withBase.EventBase()
		}
		return nil
	}
}

// updateStatsForEvent updates stats based on event type.
// Must be called with lock held.
func (ctx *ExecutionContext) updateStatsForEvent(event Event) {
	switch e := event.(type) {
	// Increment BEFORE events (for prevention/limits)
	case *BeforeIterationEvent:
		ctx.stats.incrCounterDirect(SCIterations, 1)
		// Reset per-iteration token gauges
		ctx.stats.ResetGauge(SGInputTokensLastIteration)
		ctx.stats.ResetGauge(SGOutputTokensLastIteration)
		ctx.stats.ResetGauge(SGTotalTokensLastIteration)
		ctx.stats.resetGaugesByPrefix(
			SGInputTokensLastIterationFor,
		)
		ctx.stats.resetGaugesByPrefix(
			SGOutputTokensLastIterationFor,
		)
		ctx.stats.resetGaugesByPrefix(
			SGTotalTokensLastIterationFor,
		)

	case *BeforeToolCallEvent:
		ctx.stats.incrCounterDirect(SCToolCalls, 1)
		if e.ToolName != "" {
			ctx.stats.incrCounterDirect(
				SCToolCallsFor+StatKey(e.ToolName), 1,
			)
		}

	// Increment AFTER events (for recording)
	case *AfterModelCallEvent:
		totalTokens := int64(e.InputTokens) + int64(e.OutputTokens)
		ctx.stats.incrCounterDirect(
			SCInputTokens, int64(e.InputTokens),
		)
		ctx.stats.incrCounterDirect(
			SCOutputTokens, int64(e.OutputTokens),
		)
		ctx.stats.incrCounterDirect(
			SCTotalTokens, totalTokens,
		)
		if e.Model != "" {
			ctx.stats.incrCounterDirect(
				SCInputTokensFor+StatKey(e.Model),
				int64(e.InputTokens),
			)
			ctx.stats.incrCounterDirect(
				SCOutputTokensFor+StatKey(e.Model),
				int64(e.OutputTokens),
			)
			ctx.stats.incrCounterDirect(
				SCTotalTokensFor+StatKey(e.Model),
				totalTokens,
			)
		}

		// Per-iteration gauges describe the active iteration. Late model completions
		// from a closed stream keep their original event iteration and must not update
		// gauges that may have already been reset for a newer iteration.
		if e.Iteration == ctx.Iteration() {
			ctx.stats.incrGaugeInternal(
				SGInputTokensLastIteration,
				float64(e.InputTokens),
			)
			ctx.stats.incrGaugeInternal(
				SGOutputTokensLastIteration,
				float64(e.OutputTokens),
			)
			ctx.stats.incrGaugeInternal(
				SGTotalTokensLastIteration,
				float64(totalTokens),
			)
			if e.Model != "" {
				ctx.stats.incrGaugeInternal(
					SGInputTokensLastIterationFor+
						StatKey(e.Model),
					float64(e.InputTokens),
				)
				ctx.stats.incrGaugeInternal(
					SGOutputTokensLastIterationFor+
						StatKey(e.Model),
					float64(e.OutputTokens),
				)
				ctx.stats.incrGaugeInternal(
					SGTotalTokensLastIterationFor+
						StatKey(e.Model),
					float64(totalTokens),
				)
			}
		}

	case *AfterToolCallEvent:
		if e.Error != nil {
			ctx.stats.incrCounterDirect(
				SCToolCallsErrorTotal, 1,
			)
			ctx.stats.incrGaugeInternal(
				SGToolCallsErrorConsecutive, 1,
			)
			if e.ToolName != "" {
				ctx.stats.incrCounterDirect(
					SCToolCallsErrorFor+StatKey(e.ToolName),
					1,
				)
				ctx.stats.incrGaugeInternal(
					SGToolCallsErrorConsecutiveFor+StatKey(e.ToolName),
					1,
				)
			}
		}

	case *ParseErrorEvent:
		switch e.ErrorType {
		case ParseErrorTypeToolchain:
			ctx.stats.incrCounterDirect(
				SCToolchainParseErrorTotal, 1,
			)
			ctx.stats.incrGaugeInternal(
				SGToolchainParseErrorConsecutive, 1,
			)
		}

	case *OutputValidationEvent:
		ctx.updateOutputStats(e)
	case *CompactionEvent:
		ctx.stats.incrCounterDirect(SCCompactions, 1)
	}
}

// Events returns a copy of all recorded events.
func (ctx *ExecutionContext) Events() []Event {
	ctx.mu.RLock()
	stored := append([]Event(nil), ctx.events...)
	ctx.mu.RUnlock()
	result := make([]Event, 0, len(stored))
	for _, event := range stored {
		// A nil entry is a publish currently being dispatched to subscribers. Before-event
		// subscribers can mutate event fields, so the event only becomes visible after the
		// final stable copy is stored.
		if event == nil {
			continue
		}
		owned, err := ctx.copyEvent(event)
		if err != nil {
			ctx.stopRecording(err)
			return nil
		}
		result = append(result, owned)
	}
	return result
}

func (ctx *ExecutionContext) copyEvent(event Event) (Event, error) {
	if changed, ok := event.(*StateChangeEvent); ok {
		change, err := cloneOwned(ctx.Codecs(), changed.Change)
		if err != nil {
			return nil, err
		}
		return &StateChangeEvent{
			BaseEvent: changed.BaseEvent,
			Change:    change,
		}, nil
	}
	value, err := ctx.Codecs().Clone(event)
	if err != nil {
		return nil, err
	}
	return value.(Event), nil
}

type modelCallEventOptions struct {
	modelCallId   string
	streamId      string
	streamTopicId string
	source        string
	provider      string
}

// ModelCallEventOption configures model call event metadata.
type ModelCallEventOption func(*modelCallEventOptions)

// WithModelCallId associates before/after model events with a specific model call.
func WithModelCallId(id string) ModelCallEventOption {
	return func(opts *modelCallEventOptions) {
		opts.modelCallId = id
	}
}

// WithModelStream associates model events with a stream ID and topic ID.
func WithModelStream(streamId string, streamTopicId string) ModelCallEventOption {
	return func(opts *modelCallEventOptions) {
		opts.streamId = streamId
		opts.streamTopicId = streamTopicId
	}
}

// WithModelCallSource pins model event source when completion happens asynchronously.
func WithModelCallSource(source string) ModelCallEventOption {
	return func(opts *modelCallEventOptions) {
		opts.source = source
	}
}

// WithModelProvider records the provider name when the model wrapper knows it.
func WithModelProvider(provider string) ModelCallEventOption {
	return func(opts *modelCallEventOptions) {
		opts.provider = provider
	}
}

type toolCallEventOptions struct {
	toolCallId string
	source     string
	attemptId  string
}

// WithToolCallAttempt identifies the physical attempt reconciled by a recovery adapter.
func WithToolCallAttempt(id string) ToolCallEventOption {
	return func(options *toolCallEventOptions) { options.attemptId = id }
}

// ToolCallEventOption configures tool call event metadata.
type ToolCallEventOption func(*toolCallEventOptions)

// WithToolCallId associates before/after tool events with a specific tool call.
func WithToolCallId(id string) ToolCallEventOption {
	return func(opts *toolCallEventOptions) {
		opts.toolCallId = id
	}
}

// WithToolCallSource pins tool event source when completion happens asynchronously.
func WithToolCallSource(source string) ToolCallEventOption {
	return func(opts *toolCallEventOptions) {
		opts.source = source
	}
}

func applyModelCallEventOptions(opts []ModelCallEventOption) modelCallEventOptions {
	var result modelCallEventOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&result)
		}
	}
	return result
}

func applyToolCallEventOptions(opts []ToolCallEventOption) toolCallEventOptions {
	var result toolCallEventOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&result)
		}
	}
	return result
}

func (ctx *ExecutionContext) nextModelCallId() string {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.nextModelCallSeq++
	return fmt.Sprintf("%s:model:%d", ctx.contextId, ctx.nextModelCallSeq)
}

func (ctx *ExecutionContext) nextToolCallId() string {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.nextToolCallSeq++
	return fmt.Sprintf("%s:tool:%d", ctx.contextId, ctx.nextToolCallSeq)
}

// -----------------------------------------------------------------------------
// PublishXXX Convenience Methods
// -----------------------------------------------------------------------------

// PublishBeforeExecution publishes a BeforeExecutionEvent.
func (ctx *ExecutionContext) PublishBeforeExecution() *BeforeExecutionEvent {
	event := &BeforeExecutionEvent{
		BaseEvent: BaseEvent{EventName: EventNameExecutionBefore},
		Name:      ctx.Name(),
	}
	ctx.publish(event)
	return event
}

// PublishAfterExecution runs the completion hooks and freezes an AfterExecutionEvent.
// CommitCheckpoint(PhaseTerminal) saves it with the outcome before live consumers see success.
func (ctx *ExecutionContext) PublishAfterExecution(
	reason TerminationReason,
	err error,
) *AfterExecutionEvent {
	event := &AfterExecutionEvent{
		BaseEvent:         BaseEvent{EventName: EventNameExecutionAfter},
		TerminationReason: reason,
		Error:             err,
		Output:            ctx.FinalResult(),
		Refusal:           ctx.Refusal(),
		Duration:          ctx.Duration(),
	}
	ctx.publish(event)
	return event
}

// PublishBeforeIteration publishes a BeforeIterationEvent.
// Stats updated: Iterations counter is incremented.
func (ctx *ExecutionContext) PublishBeforeIteration() *BeforeIterationEvent {
	event := &BeforeIterationEvent{
		BaseEvent: BaseEvent{EventName: EventNameIterationBefore},
	}
	ctx.publish(event)
	return event
}

// PublishAfterIteration publishes an AfterIterationEvent.
func (ctx *ExecutionContext) PublishAfterIteration(
	result *AgentLoopResult,
	duration time.Duration,
	iterationErrors ...error,
) *AfterIterationEvent {
	event := &AfterIterationEvent{
		BaseEvent: BaseEvent{EventName: EventNameIterationAfter},
		Result:    result,
		Duration:  duration,
	}
	if len(iterationErrors) > 0 {
		event.Error = iterationErrors[0]
	}
	ctx.publish(event)
	return event
}

// PublishBeforeModelCall publishes a BeforeModelCallEvent.
// Returns the event so callers can use the (potentially modified) Request field.
func (ctx *ExecutionContext) PublishBeforeModelCall(
	model string,
	request ModelCallRequest,
	opts ...ModelCallEventOption,
) *BeforeModelCallEvent {
	options := applyModelCallEventOptions(opts)
	if options.modelCallId == "" {
		options.modelCallId = ctx.nextModelCallId()
	}
	event := &BeforeModelCallEvent{
		BaseEvent: BaseEvent{
			EventName: EventNameModelCallBefore,
			Source:    options.source,
		},
		Model:         model,
		Provider:      options.provider,
		Request:       request,
		ModelCallId:   options.modelCallId,
		StreamId:      options.streamId,
		StreamTopicId: options.streamTopicId,
	}
	ctx.publish(event)
	return event
}

// PublishAfterModelCall publishes an AfterModelCallEvent.
// Stats updated: InputTokens, OutputTokens (and per-model variants).
func (ctx *ExecutionContext) PublishAfterModelCall(
	model string,
	request ModelCallRequest,
	response *ContentResponse,
	duration time.Duration,
	err error,
	opts ...ModelCallEventOption,
) *AfterModelCallEvent {
	options := applyModelCallEventOptions(opts)
	event := newAfterModelCallEvent(model, request, response, duration, err, options)
	ctx.publish(event)
	return event
}

// PublishAfterModelCallForIteration publishes an AfterModelCallEvent for the iteration where
// the model call started. Use this for asynchronous model calls that may complete after the
// execution context has advanced to a later iteration.
func (ctx *ExecutionContext) PublishAfterModelCallForIteration(
	iteration int,
	model string,
	request ModelCallRequest,
	response *ContentResponse,
	duration time.Duration,
	err error,
	opts ...ModelCallEventOption,
) *AfterModelCallEvent {
	options := applyModelCallEventOptions(opts)
	event := newAfterModelCallEvent(model, request, response, duration, err, options)
	ctx.publishWithEventIteration(event, &iteration)
	return event
}

func newAfterModelCallEvent(
	model string,
	request ModelCallRequest,
	response *ContentResponse,
	duration time.Duration,
	err error,
	options modelCallEventOptions,
) *AfterModelCallEvent {
	event := &AfterModelCallEvent{
		BaseEvent: BaseEvent{
			EventName: EventNameModelCallAfter,
			Source:    options.source,
		},
		Model:         model,
		Provider:      options.provider,
		Request:       request,
		Response:      response,
		Duration:      duration,
		Error:         err,
		ModelCallId:   options.modelCallId,
		StreamId:      options.streamId,
		StreamTopicId: options.streamTopicId,
	}
	if response != nil && response.Info != nil {
		event.InputTokens = response.Info.InputTokens
		event.OutputTokens = response.Info.OutputTokens
	}
	return event
}

// PublishBeforeToolCall publishes a BeforeToolCallEvent.
// Returns the event so callers can use the (potentially modified) Args field.
// Stats updated: ToolCalls (and per-tool variants).
func (ctx *ExecutionContext) PublishBeforeToolCall(
	toolName string,
	args any,
	opts ...ToolCallEventOption,
) *BeforeToolCallEvent {
	options := applyToolCallEventOptions(opts)
	if options.toolCallId == "" {
		options.toolCallId = ctx.selectedToolCallId()
	}
	if options.toolCallId == "" {
		options.toolCallId = ctx.nextToolCallId()
	}
	event := &BeforeToolCallEvent{
		BaseEvent: BaseEvent{
			EventName: EventNameToolCallBefore,
			Source:    options.source,
		},
		ToolName:   toolName,
		Args:       args,
		ToolCallId: options.toolCallId,
	}
	if ctx.isRetryingTool(event) {
		input, err := ctx.retryToolInput(event.ToolCallId)
		if err != nil {
			ctx.stopRecording(err)
			return event
		}
		event.Args = input
	}
	ctx.publish(event)
	if err := ctx.saveToolReady(event); err != nil {
		ctx.stopRecording(err)
	}
	return event
}

// PublishAfterToolCall publishes an AfterToolCallEvent.
// Stats updated: ToolCallsErrorTotal, ToolCallsErrorConsecutive (on error).
func (ctx *ExecutionContext) PublishAfterToolCall(
	toolName string,
	args any,
	output any,
	duration time.Duration,
	err error,
	opts ...ToolCallEventOption,
) *AfterToolCallEvent {
	options := applyToolCallEventOptions(opts)
	ctx.addRecoverySteering(options.toolCallId, output)
	event := &AfterToolCallEvent{
		BaseEvent: BaseEvent{
			EventName: EventNameToolCallAfter,
			Source:    options.source,
			AttemptId: options.attemptId,
		},
		ToolName:   toolName,
		Args:       args,
		Output:     output,
		Duration:   duration,
		Error:      err,
		ToolCallId: options.toolCallId,
	}
	ctx.publish(event)
	return event
}

// PublishParseError publishes a ParseErrorEvent.
// Stats updated: Based on errorType - format, toolchain, termination, or section errors.
func (ctx *ExecutionContext) PublishParseError(
	errorType ParseErrorType,
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

// PublishError publishes an ErrorEvent.
func (ctx *ExecutionContext) PublishError(err error) *ErrorEvent {
	event := &ErrorEvent{
		BaseEvent: BaseEvent{EventName: EventNameError},
		Error:     err,
	}
	ctx.publish(event)
	return event
}

// PublishLimitExceeded publishes a LimitExceededEvent.
// This is called automatically when a limit is exceeded during
// checkLimits().
func (ctx *ExecutionContext) PublishLimitExceeded(
	limit Limit,
	currentValue float64,
	matchedKey StatKey,
) *LimitExceededEvent {
	event := &LimitExceededEvent{
		BaseEvent: BaseEvent{
			EventName: EventNameLimitExceeded,
		},
		Limit:        limit,
		CurrentValue: currentValue,
		MatchedKey:   matchedKey,
	}
	ctx.publish(event)
	return event
}

// PublishCompaction publishes a CompactionEvent.
// Stats updated: SCCompactions counter is incremented.
func (ctx *ExecutionContext) PublishCompaction(
	lengthBefore int,
	lengthAfter int,
	duration time.Duration,
) *CompactionEvent {
	event := &CompactionEvent{
		BaseEvent: BaseEvent{
			EventName: EventNameCompaction,
		},
		ScratchpadLengthBefore: lengthBefore,
		ScratchpadLengthAfter:  lengthAfter,
		Duration:               duration,
	}
	ctx.publish(event)
	return event
}

// PublishCommonEvent publishes a CommonEvent for user-defined events.
// The eventName should use the format "namespace:event_name" (e.g., "myapp:cache_hit").
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

// Stats returns the execution stats for this context.
//
// Thread Safety:
//   - Safe to call from any goroutine
//   - All ExecutionStats methods handle their own locking
//   - Safe to read during subscriber execution (subscribers run synchronously)
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
	var task *Task
	if data != nil {
		task = data.GetTask()
	}
	savedTask, err := ctx.Codecs().Encode(task)
	if err != nil {
		ctx.stopRecording(err)
	}
	ctx.run.mu.Lock()
	ctx.run.allocation++
	childId := fmt.Sprintf("%s:child:%s:%d", ctx.run.id, ctx.run.attempt, ctx.run.allocation)
	ctx.run.mu.Unlock()
	ctx.mu.Lock()
	childContextId := childId
	parentContextId := ctx.contextId
	parentPublisher := ctx.eventPublisher

	// Create child context that is cancelled when parent is cancelled
	childGoCtx, childCancel := context.WithCancelCause(ctx.goCtx)

	child := &ExecutionContext{
		run:             ctx.run,
		state:           map[string]EncodedValue{},
		phase:           PhaseInitial,
		goCtx:           childGoCtx,
		cancel:          childCancel,
		limits:          cloneLimits(ctx.limits), // Children own their limit slice.
		name:            name,
		contextId:       childContextId,
		parentContextId: parentContextId,
		data:            data,
		savedTask:       savedTask,
		depth:           ctx.depth + 1,
		parent:          ctx,
		events:          make([]Event, 0),
		startTime:       time.Now(),

		// Inherit the publisher so root subscribers observe nested executor lifecycle events.
		// A child executor may still replace this if it intentionally wants a different registry.
		eventPublisher: parentPublisher,
		streamHub:      newStreamHub(),
	}
	// Create stats with back-reference to child for limit checking
	// Stats also link to parent stats for real-time aggregation
	child.stats = newExecutionStatsWithContextAndParent(child, ctx.stats)
	child.stopStreams = context.AfterFunc(child.Context(), child.CloseStreams)

	ctx.children = append(ctx.children, child)
	ctx.mu.Unlock()

	ctx.PublishCommonEvent(
		EventNameChildSpawn,
		"Child context spawned",
		map[string]any{
			"child_context_id":  child.ContextId(),
			"parent_context_id": parentContextId,
			"child_name":        name,
			"child_source":      child.BuildSourcePath(),
			"child_depth":       child.Depth(),
		},
	)

	return child
}

// CompleteChild finalizes a child context and records completion.
// This should be called via defer after SpawnChild.
//
// Stats aggregation happens in real time through the parent reference. Accept the completion
// event before releasing child work and stream subscriptions.
func (ctx *ExecutionContext) CompleteChild(child *ExecutionContext) error {
	defer child.Release()
	child.mu.Lock()
	child.endTime = time.Now()
	childDuration := child.endTime.Sub(child.startTime)
	childReason := child.terminationReason
	childName := child.name
	childContextId := child.contextId
	parentContextId := child.parentContextId
	childDepth := child.depth
	childError := child.err
	child.mu.Unlock()
	childSource := child.BuildSourcePath()

	data := map[string]any{
		"child_context_id": childContextId, "parent_context_id": parentContextId,
		"child_name": childName, "child_source": childSource, "child_depth": childDepth,
		"termination_reason": childReason, "duration": childDuration,
	}
	if childError != nil {
		data["error"] = childError
	}
	ctx.PublishCommonEvent(
		EventNameChildComplete,
		"Child context completed",
		data,
	)
	return ctx.FlushProgress()
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
func (ctx *ExecutionContext) SetTermination(
	reason TerminationReason, result []ContentPart, err error,
) {
	ctx.setTermination(reason, result, nil, err)
}

// SetRefusal records a native refusal outcome without accepting content. The executor commits
// this outcome before returning it, just as it commits a successful typed result.
func (ctx *ExecutionContext) SetRefusal(refusal *ModelRefusal) {
	ctx.setTermination(TerminationRefused, nil, refusal, nil)
}

func (ctx *ExecutionContext) setTermination(
	reason TerminationReason, result []ContentPart, refusal *ModelRefusal, err error,
) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.terminationReason = reason
	if reason != TerminationSuccess {
		ctx.acceptedValue = nil
	}
	ctx.finalResult = CloneContentParts(result)
	ctx.refusal = cloneRefusal(refusal)
	ctx.err = err
	ctx.endTime = time.Now()
	ctx.result = &ExecutionResult{TerminationReason: reason, Output: ctx.finalResult,
		Refusal: ctx.refusal, Error: err, ExceededLimit: ctx.exceededLimit}
}

func (ctx *ExecutionContext) Refusal() *ModelRefusal {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return cloneRefusal(ctx.refusal)
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
	return CloneContentParts(ctx.finalResult)
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
//   - The unsubscribe function is called, discarding any queued chunks for this subscriber
//   - The ExecutionContext terminates (CloseStreams is called), after queued chunks drain
//
// Delivery uses the finite DeliveryConfig defaults. Overflow or a delivery timeout closes
// this subscription and discards queued chunks. Use the matching WithConfig method to set
// limits and read the closure error. A slow subscriber does not block execution or Save.
func (ctx *ExecutionContext) SubscribeAll() (<-chan StreamChunk, UnsubscribeFunc) {
	return ctx.streamHub.subscribeAll()
}

// SubscribeAllDraining returns a channel receiving all chunks from this context and all
// descendant contexts. Unlike SubscribeAll, its unsubscribe function drains already queued chunks
// before the channel closes. Use this only for internal observers that keep consuming until the
// channel closes; normal callers should use SubscribeAll so unsubscribe can abandon stale chunks.
func (ctx *ExecutionContext) SubscribeAllDraining() (<-chan StreamChunk, UnsubscribeFunc) {
	return ctx.streamHub.subscribeAllDraining()
}

// SubscribeToStream returns a channel receiving chunks for a specific streamId,
// plus an unsubscribe function.
//
// Returns (nil, nil) if streamId is empty.
//
// The channel closes when either:
//   - The unsubscribe function is called, discarding any queued chunks for this subscriber
//   - The ExecutionContext terminates (CloseStreams is called), after queued chunks drain
//
// Delivery uses the finite DeliveryConfig defaults. Overflow or a delivery timeout closes
// this subscription and discards queued chunks. Use the matching WithConfig method to set
// limits and read the closure error. A slow subscriber does not block execution or Save.
func (ctx *ExecutionContext) SubscribeToStream(
	streamId string,
) (<-chan StreamChunk, UnsubscribeFunc) {
	return ctx.streamHub.subscribeToStream(streamId)
}

// SubscribeToTopic returns a channel receiving chunks for a specific topicId,
// plus an unsubscribe function.
//
// Multiple streams may share the same topic; caller handles interleaving.
// Returns (nil, nil) if topicId is empty.
//
// The channel closes when either:
//   - The unsubscribe function is called, discarding any queued chunks for this subscriber
//   - The ExecutionContext terminates (CloseStreams is called), after queued chunks drain
//
// Delivery uses the finite DeliveryConfig defaults. Overflow or a delivery timeout closes
// this subscription and discards queued chunks. Use the matching WithConfig method to set
// limits and read the closure error. A slow subscriber does not block execution or Save.
func (ctx *ExecutionContext) SubscribeToTopic(
	topicId string,
) (<-chan StreamChunk, UnsubscribeFunc) {
	return ctx.streamHub.subscribeToTopic(topicId)
}

// EmitChunk freezes and publishes a live chunk, then checks pending-save thresholds.
// Delivery does not wait for a consumer. If a threshold is reached, the grouped save blocks
// until commit. Otherwise the event stays pending until the next explicit save boundary.
// Called by model wrappers during streaming. Automatically propagates to parent while preserving
// child-populated metadata; parent contexts only fill fields that are still empty.
// Safe for concurrent use.
//
// If chunk.Source is empty, it will be populated with BuildSourcePath().
func (ctx *ExecutionContext) EmitChunk(chunk StreamChunk) error {
	if err := ctx.emitChunk(&chunk); err != nil {
		return err
	}
	return ctx.flushPendingLimit()
}

func (ctx *ExecutionContext) emitChunk(chunk *StreamChunk) error {
	if len(chunk.Content) > 0 || chunk.MessageId != "" || len(chunk.SourceMessageIds) != 0 {
		return fmt.Errorf("gent: Content is reserved for terminal publication; emit MetaContent")
	}
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
	if err := ctx.recordChunk(chunk); err != nil {
		return err
	}
	return nil
}

func (ctx *ExecutionContext) emitStreamChunk(chunk StreamChunk) {

	// Emit to local subscribers
	ctx.streamHub.emit(chunk)

	// Propagate to parent (chunk.Source already contains full path)
	// Parent is immutable after creation, so no lock needed
	ctx.mu.RLock()
	parent := ctx.parent
	ctx.mu.RUnlock()

	if parent != nil {
		parent.emitStreamChunk(chunk)
	}
}

// CloseStreams starts a bounded drain of this context's chunk subscriptions and, for the root,
// all live event subscriptions. Cancellation and Release call it. Repeated calls are safe.
func (ctx *ExecutionContext) CloseStreams() {
	ctx.streamHub.close()
	if ctx.Parent() == nil {
		ctx.run.mu.Lock()
		ctx.run.liveClosed = true
		for sub := range ctx.run.liveSubscribers {
			sub.queue.Close()
		}
		clear(ctx.run.liveSubscribers)
		ctx.run.mu.Unlock()
	}
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
