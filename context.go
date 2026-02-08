package gent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pmezard/go-difflib/difflib"
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
// # Usage in Custom AgentLoop
//
//	func (l *MyLoop) Next(execCtx *ExecutionContext) (*AgentLoopResult, error) {
//	    // Access loop data
//	    data := execCtx.Data().(*MyLoopData)
//
//	    // Check current iteration
//	    fmt.Printf("Iteration %d\n", execCtx.Iteration())
//
//	    // Record custom event
//	    execCtx.PublishCommonEvent("myapp:my_event", "Something happened", myData)
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
// All methods are safe for concurrent use. Multiple goroutines can publish events,
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
	err               error

	// Event publisher for dispatching events to subscribers (set by Executor)
	eventPublisher EventPublisher
	eventDepth     int // tracks recursion depth for event publishing

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
		events:    make([]Event, 0),
		startTime: time.Now(),
		streamHub: newStreamHub(),
	}
	// Create stats with back-reference for limit checking
	execCtx.stats = newExecutionStatsWithContext(execCtx)
	// Set execution context on LoopData for automatic event publishing
	if data != nil {
		data.SetExecutionContext(execCtx)
	}
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
	var publisher EventPublisher

	ctx.updateContextState(func() {
		// Check recursion depth
		if ctx.eventPublisher != nil && ctx.eventDepth >= ctx.eventPublisher.MaxRecursion() {
			panic(fmt.Sprintf("event recursion depth exceeded maximum (%d)",
				ctx.eventPublisher.MaxRecursion()))
		}
		ctx.eventDepth++

		// Populate base event fields
		ctx.populateBaseEvent(event)

		// Append to event log
		ctx.events = append(ctx.events, event)

		publisher = ctx.eventPublisher
	})

	// Update stats based on event type (outside lock because incrCounterDirect calls checkLimits)
	ctx.updateStatsForEvent(event)

	// Dispatch to subscribers
	if publisher != nil {
		publisher.Dispatch(ctx, event)
	}

	ctx.updateContextState(func() {
		ctx.eventDepth--
	})
}

// Publish records a custom event, updates stats, checks limits, and dispatches to subscribers.
// Use this for user-defined Event types. For framework events, use the typed PublishXXX methods.
func (ctx *ExecutionContext) Publish(event Event) {
	ctx.publish(event)
}

// populateBaseEvent fills in BaseEvent fields.
// Must be called with lock held.
func (ctx *ExecutionContext) populateBaseEvent(event Event) {
	switch e := event.(type) {
	case *BeforeExecutionEvent:
		e.Timestamp = time.Now()
		e.Iteration = ctx.iteration
		e.Depth = ctx.depth
	case *AfterExecutionEvent:
		e.Timestamp = time.Now()
		e.Iteration = ctx.iteration
		e.Depth = ctx.depth
	case *BeforeIterationEvent:
		e.Timestamp = time.Now()
		e.Iteration = ctx.iteration
		e.Depth = ctx.depth
	case *AfterIterationEvent:
		e.Timestamp = time.Now()
		e.Iteration = ctx.iteration
		e.Depth = ctx.depth
	case *BeforeModelCallEvent:
		e.Timestamp = time.Now()
		e.Iteration = ctx.iteration
		e.Depth = ctx.depth
	case *AfterModelCallEvent:
		e.Timestamp = time.Now()
		e.Iteration = ctx.iteration
		e.Depth = ctx.depth
	case *BeforeToolCallEvent:
		e.Timestamp = time.Now()
		e.Iteration = ctx.iteration
		e.Depth = ctx.depth
	case *AfterToolCallEvent:
		e.Timestamp = time.Now()
		e.Iteration = ctx.iteration
		e.Depth = ctx.depth
	case *ParseErrorEvent:
		e.Timestamp = time.Now()
		e.Iteration = ctx.iteration
		e.Depth = ctx.depth
	case *ValidatorCalledEvent:
		e.Timestamp = time.Now()
		e.Iteration = ctx.iteration
		e.Depth = ctx.depth
	case *ValidatorResultEvent:
		e.Timestamp = time.Now()
		e.Iteration = ctx.iteration
		e.Depth = ctx.depth
	case *ErrorEvent:
		e.Timestamp = time.Now()
		e.Iteration = ctx.iteration
		e.Depth = ctx.depth
	case *CommonEvent:
		e.Timestamp = time.Now()
		e.Iteration = ctx.iteration
		e.Depth = ctx.depth
	case *CommonDiffEvent:
		e.Timestamp = time.Now()
		e.Iteration = ctx.iteration
		e.Depth = ctx.depth
	case *LimitExceededEvent:
		e.Timestamp = time.Now()
		e.Iteration = ctx.iteration
		e.Depth = ctx.depth
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

		// Per-iteration gauge tracking (local-only, reset each
		// iteration)
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
		case ParseErrorTypeFormat:
			ctx.stats.incrCounterDirect(
				SCFormatParseErrorTotal, 1,
			)
			ctx.stats.incrGaugeInternal(
				SGFormatParseErrorConsecutive, 1,
			)
		case ParseErrorTypeToolchain:
			ctx.stats.incrCounterDirect(
				SCToolchainParseErrorTotal, 1,
			)
			ctx.stats.incrGaugeInternal(
				SGToolchainParseErrorConsecutive, 1,
			)
		case ParseErrorTypeTermination:
			ctx.stats.incrCounterDirect(
				SCTerminationParseErrorTotal, 1,
			)
			ctx.stats.incrGaugeInternal(
				SGTerminationParseErrorConsecutive, 1,
			)
		case ParseErrorTypeSection:
			ctx.stats.incrCounterDirect(
				SCSectionParseErrorTotal, 1,
			)
			ctx.stats.incrGaugeInternal(
				SGSectionParseErrorConsecutive, 1,
			)
		}

	case *ValidatorResultEvent:
		if !e.Accepted {
			ctx.stats.incrCounterDirect(
				SCAnswerRejectedTotal, 1,
			)
			if e.ValidatorName != "" {
				ctx.stats.incrCounterDirect(
					SCAnswerRejectedBy+StatKey(e.ValidatorName),
					1,
				)
			}
		}
	}
}

// Events returns a copy of all recorded events.
func (ctx *ExecutionContext) Events() []Event {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	result := make([]Event, len(ctx.events))
	copy(result, ctx.events)
	return result
}

// -----------------------------------------------------------------------------
// PublishXXX Convenience Methods
// -----------------------------------------------------------------------------

// PublishBeforeExecution publishes a BeforeExecutionEvent.
func (ctx *ExecutionContext) PublishBeforeExecution() *BeforeExecutionEvent {
	event := &BeforeExecutionEvent{
		BaseEvent: BaseEvent{EventName: EventNameExecutionBefore},
	}
	ctx.publish(event)
	return event
}

// PublishAfterExecution publishes an AfterExecutionEvent.
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
) *AfterIterationEvent {
	event := &AfterIterationEvent{
		BaseEvent: BaseEvent{EventName: EventNameIterationAfter},
		Result:    result,
		Duration:  duration,
	}
	ctx.publish(event)
	return event
}

// PublishBeforeModelCall publishes a BeforeModelCallEvent.
// Returns the event so callers can use the (potentially modified) Request field.
func (ctx *ExecutionContext) PublishBeforeModelCall(
	model string,
	request any,
) *BeforeModelCallEvent {
	event := &BeforeModelCallEvent{
		BaseEvent: BaseEvent{EventName: EventNameModelCallBefore},
		Model:     model,
		Request:   request,
	}
	ctx.publish(event)
	return event
}

// PublishAfterModelCall publishes an AfterModelCallEvent.
// Stats updated: InputTokens, OutputTokens (and per-model variants).
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

// PublishBeforeToolCall publishes a BeforeToolCallEvent.
// Returns the event so callers can use the (potentially modified) Args field.
// Stats updated: ToolCalls (and per-tool variants).
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

// PublishAfterToolCall publishes an AfterToolCallEvent.
// Stats updated: ToolCallsErrorTotal, ToolCallsErrorConsecutive (on error).
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

// PublishValidatorCalled publishes a ValidatorCalledEvent.
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

// PublishValidatorResult publishes a ValidatorResultEvent.
// Stats updated: AnswerRejectedTotal, AnswerRejectedBy (when rejected).
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

// PublishCommonDiffEvent publishes a CommonDiffEvent for state change tracking.
// The eventName should use the format "namespace:event_name" (e.g., "myapp:config_change").
//
// The diff is auto-generated by JSON-marshaling both before and after values, then computing
// a unified diff. This is useful for hook implementers who want to log state changes.
//
// If marshaling fails for either value, the error message(s) are included in the Diff field.
func (ctx *ExecutionContext) PublishCommonDiffEvent(
	eventName string,
	before any,
	after any,
) *CommonDiffEvent {
	diff := computeDiff(before, after)
	event := &CommonDiffEvent{
		BaseEvent: BaseEvent{EventName: eventName},
		Before:    before,
		After:     after,
		Diff:      diff,
	}
	ctx.publish(event)
	return event
}

// PublishIterationHistoryChange publishes a CommonDiffEvent for iteration history changes.
// This is a convenience method for tracking changes to LoopData.GetIterationHistory().
func (ctx *ExecutionContext) PublishIterationHistoryChange(
	before []*Iteration,
	after []*Iteration,
) *CommonDiffEvent {
	return ctx.PublishCommonDiffEvent(EventNameIterationHistoryChange, before, after)
}

// PublishScratchPadChange publishes a CommonDiffEvent for scratchpad changes.
// This is a convenience method for tracking changes to LoopData.GetScratchPad().
func (ctx *ExecutionContext) PublishScratchPadChange(
	before []*Iteration,
	after []*Iteration,
) *CommonDiffEvent {
	return ctx.PublishCommonDiffEvent(EventNameScratchPadChange, before, after)
}

// computeDiff generates a unified diff between JSON representations of before and after.
// If marshaling fails, returns error message(s) prefixed with "<marshal error: ...>".
func computeDiff(before, after any) string {
	var errors []string

	beforeJSON, err := json.MarshalIndent(before, "", "  ")
	if err != nil {
		errors = append(errors, fmt.Sprintf("<marshal error (before): %v>", err))
	}

	afterJSON, err := json.MarshalIndent(after, "", "  ")
	if err != nil {
		errors = append(errors, fmt.Sprintf("<marshal error (after): %v>", err))
	}

	if len(errors) > 0 {
		return strings.Join(errors, "\n")
	}

	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(beforeJSON)),
		B:        difflib.SplitLines(string(afterJSON)),
		FromFile: "before",
		ToFile:   "after",
		Context:  3,
	}

	result, err := difflib.GetUnifiedDiffString(diff)
	if err != nil {
		return fmt.Sprintf("<diff error: %v>", err)
	}

	return result
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
		events:    make([]Event, 0),
		startTime: time.Now(),
		streamHub: newStreamHub(),
	}
	// Create stats with back-reference to child for limit checking
	// Stats also link to parent stats for real-time aggregation
	child.stats = newExecutionStatsWithContextAndParent(child, ctx.stats)

	// Set execution context on LoopData for automatic event publishing
	if data != nil {
		data.SetExecutionContext(child)
	}

	ctx.children = append(ctx.children, child)

	// Record child spawn event
	spawnEvent := &CommonEvent{
		BaseEvent: BaseEvent{
			EventName: EventNameChildSpawn,
			Timestamp: time.Now(),
			Iteration: ctx.iteration,
			Depth:     ctx.depth,
		},
		Description: "Child context spawned",
		Data:        map[string]any{"child_name": name},
	}
	ctx.events = append(ctx.events, spawnEvent)

	return child
}

// CompleteChild finalizes a child context and records completion.
// This should be called via defer after SpawnChild.
//
// Note: Stats aggregation happens in real-time via parent reference,
// so this method only records the completion event.
func (ctx *ExecutionContext) CompleteChild(child *ExecutionContext) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	child.mu.Lock()
	child.endTime = time.Now()
	childDuration := child.endTime.Sub(child.startTime)
	childReason := child.terminationReason
	childName := child.name
	child.mu.Unlock()

	// Record child complete event
	completeEvent := &CommonEvent{
		BaseEvent: BaseEvent{
			EventName: EventNameChildComplete,
			Timestamp: time.Now(),
			Iteration: ctx.iteration,
			Depth:     ctx.depth,
		},
		Description: "Child context completed",
		Data: map[string]any{
			"child_name":         childName,
			"termination_reason": childReason,
			"duration":           childDuration,
		},
	}
	ctx.events = append(ctx.events, completeEvent)
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
