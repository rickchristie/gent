package gent

import (
	"time"
)

// -----------------------------------------------------------------------------
// Termination Reason
// -----------------------------------------------------------------------------

// TerminationReason indicates why execution terminated.
type TerminationReason string

const (
	// TerminationSuccess means the AgentLoop returned LATerminate.
	TerminationSuccess TerminationReason = "success"

	// TerminationError means an error occurred.
	TerminationError TerminationReason = "error"

	// TerminationContextCanceled means the context was canceled.
	TerminationContextCanceled TerminationReason = "context_canceled"

	// TerminationLimitExceeded means a configured limit was exceeded.
	// Inspect ExecutionResult.ExceededLimit for details about which limit was hit.
	TerminationLimitExceeded TerminationReason = "limit_exceeded"
)

// -----------------------------------------------------------------------------
// Execution Result
// -----------------------------------------------------------------------------

// ExecutionResult contains the final result of an execution run.
// Access this via ExecutionContext.Result() after execution completes.
type ExecutionResult struct {
	// TerminationReason indicates how execution ended.
	TerminationReason TerminationReason

	// Output is the final output from the AgentLoop (set when terminated successfully).
	// This is a slice of ContentPart to support multimodal outputs.
	// Nil if terminated due to error, limit, or cancellation.
	Output []ContentPart

	// Error is the error that caused termination, if any.
	// Nil for successful termination.
	Error error

	// ExceededLimit is non-nil if execution terminated due to a limit being exceeded.
	// Inspect this to determine which limit was hit and its threshold.
	ExceededLimit *Limit
}

// -----------------------------------------------------------------------------
// Event Interface
// -----------------------------------------------------------------------------

// Event is the marker interface for all events in the unified event system.
//
// Events are published via ExecutionContext.PublishXXX() methods and are used for:
//   - Observability: All events are recorded in []Event for debugging
//   - Subscriptions: Subscribers receive events via type-safe interfaces
//   - Stats: Certain events automatically update execution statistics
//   - Limits: Stats updates may trigger limit checks
//
// # Publishing Events
//
// Use the typed PublishXXX methods on ExecutionContext for framework events:
//
//	ctx.PublishBeforeIteration()
//	ctx.PublishAfterModelCall(model, request, response, duration, err)
//
// For custom events, use PublishCommonEvent:
//
//	ctx.PublishCommonEvent("myapp:cache_hit", "Cache lookup succeeded", cacheData)
//
// # Accessing Events
//
//	events := execCtx.Events()
//	for _, event := range events {
//	    switch e := event.(type) {
//	    case *AfterModelCallEvent:
//	        log.Printf("Model %s used %d tokens", e.Model, e.InputTokens)
//	    case *CommonEvent:
//	        log.Printf("Event %s: %s", e.EventName, e.Description)
//	    }
//	}
type Event interface {
	event() // marker method
}

// BaseEvent contains common fields for all events.
// These fields are automatically populated by ExecutionContext when publishing.
//
// When you publish an event via ctx.PublishXXX(), the BaseEvent fields are set:
//   - EventName: Set by the PublishXXX method (e.g., "gent:iteration:before")
//   - Timestamp: Current time
//   - Iteration: Current iteration number (1-indexed, 0 if before first iteration)
//   - Depth: Current nesting depth (0 for root context)
type BaseEvent struct {
	// EventName identifies this event type.
	// Framework events use "gent:" prefix (e.g., "gent:iteration:before").
	// User events should use their own namespace (e.g., "myapp:cache_hit").
	EventName string

	// Timestamp is when this event was published.
	Timestamp time.Time

	// Iteration is the iteration number when this event occurred (1-indexed).
	// 0 indicates the event occurred before the first iteration.
	Iteration int

	// Depth is the nesting depth when this event occurred.
	// 0 for root context, 1 for first-level child, etc.
	Depth int
}

func (BaseEvent) event() {}

// -----------------------------------------------------------------------------
// Execution Lifecycle Events
// -----------------------------------------------------------------------------

// BeforeExecutionEvent is published once before the first iteration starts.
// Subscribers can use this for initialization tasks.
type BeforeExecutionEvent struct {
	BaseEvent
}

// AfterExecutionEvent is published once after execution ends, regardless of how it ended.
// Subscribers can use this for cleanup and final reporting.
type AfterExecutionEvent struct {
	BaseEvent

	// TerminationReason indicates how execution ended.
	TerminationReason TerminationReason

	// Error is the error that caused termination, if any.
	// Nil for successful termination or limit exceeded.
	Error error
}

// -----------------------------------------------------------------------------
// Iteration Lifecycle Events
// -----------------------------------------------------------------------------

// BeforeIterationEvent is published before each iteration.
// Subscribers can modify LoopData via execCtx.Data() for persistent context injection.
type BeforeIterationEvent struct {
	BaseEvent
}

// AfterIterationEvent is published after each iteration completes.
type AfterIterationEvent struct {
	BaseEvent

	// Result is the AgentLoopResult returned by the iteration.
	Result *AgentLoopResult

	// Duration is how long this iteration took.
	Duration time.Duration
}

// -----------------------------------------------------------------------------
// Model Call Events
// -----------------------------------------------------------------------------

// BeforeModelCallEvent is published before each model API call.
// Subscribers can modify Request for ephemeral context injection.
// The Request field is mutable - subscribers can append messages that will be
// sent to the model but won't be persisted in the conversation history.
type BeforeModelCallEvent struct {
	BaseEvent

	// Model is the model identifier being called.
	Model string

	// Request contains the messages to be sent to the model.
	// This is typically []llms.MessageContent but typed as any to avoid import.
	// Subscribers can modify this slice for ephemeral context injection.
	Request any
}

// AfterModelCallEvent is published after each model API call completes.
// Stats updated: InputTokens, OutputTokens (and per-model variants).
type AfterModelCallEvent struct {
	BaseEvent

	// Model is the model identifier that was called.
	Model string

	// Request contains the messages that were sent (after any modifications).
	Request any

	// Response is the full response from the model.
	Response *ContentResponse

	// InputTokens is the number of input/prompt tokens used.
	InputTokens int

	// OutputTokens is the number of output/completion tokens generated.
	OutputTokens int

	// Duration is how long the call took.
	Duration time.Duration

	// Error is any error that occurred (nil if successful).
	Error error
}

// -----------------------------------------------------------------------------
// Tool Call Events
// -----------------------------------------------------------------------------

// BeforeToolCallEvent is published before each tool execution.
// Subscribers can modify Args before the tool is called.
// Stats updated: ToolCalls (and per-tool variants).
type BeforeToolCallEvent struct {
	BaseEvent

	// ToolName is the name of the tool being called.
	ToolName string

	// Args contains the arguments for the tool.
	// Subscribers can modify this for interception/transformation.
	Args any
}

// AfterToolCallEvent is published after each tool execution completes.
// Stats updated: ToolCallsErrorTotal, ToolCallsErrorConsecutive (on error).
type AfterToolCallEvent struct {
	BaseEvent

	// ToolName is the name of the tool that was called.
	ToolName string

	// Args contains the arguments that were passed to the tool.
	Args any

	// Output is the output from the tool.
	Output any

	// Duration is how long the call took.
	Duration time.Duration

	// Error is any error that occurred (nil if successful).
	Error error
}

// -----------------------------------------------------------------------------
// Parse Error Event
// -----------------------------------------------------------------------------

// ParseErrorEvent is published when parsing fails.
// Stats updated: Based on ErrorType - format, toolchain, termination, or section errors.
type ParseErrorEvent struct {
	BaseEvent

	// ErrorType categorizes the parse error.
	// See ParseErrorType constants for possible values.
	ErrorType ParseErrorType

	// RawContent is the content that failed to parse.
	RawContent string

	// Error is the parse error that occurred.
	Error error
}

// -----------------------------------------------------------------------------
// Validator Events
// -----------------------------------------------------------------------------

// ValidatorCalledEvent is published when an answer validator is invoked.
type ValidatorCalledEvent struct {
	BaseEvent

	// ValidatorName is the name of the validator being called.
	ValidatorName string

	// Answer is the parsed answer being validated.
	Answer any
}

// ValidatorResultEvent is published after a validator completes.
// Stats updated: AnswerRejectedTotal, AnswerRejectedBy (when rejected).
type ValidatorResultEvent struct {
	BaseEvent

	// ValidatorName is the name of the validator.
	ValidatorName string

	// Answer is the parsed answer that was validated.
	Answer any

	// Accepted is true if the validator accepted the answer.
	Accepted bool

	// Feedback contains the rejection feedback sections.
	// Only set when Accepted is false.
	Feedback []FormattedSection
}

// -----------------------------------------------------------------------------
// Error Event
// -----------------------------------------------------------------------------

// ErrorEvent is published when an error occurs during execution.
type ErrorEvent struct {
	BaseEvent

	// Error is the error that occurred.
	Error error
}

// -----------------------------------------------------------------------------
// Limit Exceeded Event
// -----------------------------------------------------------------------------

// LimitExceededEvent is published when a configured limit is exceeded.
// This event is published at the exact moment the limit is exceeded,
// before context cancellation propagates.
//
// Stats updated: None (this is an observability event only).
type LimitExceededEvent struct {
	BaseEvent

	// Limit is the limit configuration that was exceeded.
	Limit Limit

	// CurrentValue is the value that exceeded the limit.
	// For counters, this is the int64 value cast to float64.
	// For gauges, this is the float64 value directly.
	CurrentValue float64

	// MatchedKey is the actual key that exceeded the limit.
	// For exact key limits, this equals Limit.Key.
	// For prefix limits, this is the specific key that matched and exceeded.
	MatchedKey string
}

// -----------------------------------------------------------------------------
// Common Event (User-Defined)
// -----------------------------------------------------------------------------

// CommonEvent is for user-defined events.
//
// Use ExecutionContext.PublishCommonEvent() to publish custom events:
//
//	ctx.PublishCommonEvent("myapp:cache_hit", "Cache lookup succeeded", cacheData)
//
// The EventName field in BaseEvent is set from the first argument.
// Use a namespaced format to avoid collisions (e.g., "myapp:event_name").
type CommonEvent struct {
	BaseEvent

	// Description is a human-readable description of what happened.
	Description string

	// Data contains event-specific data. Can be any type.
	Data any
}
