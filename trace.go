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
// Trace Events
// -----------------------------------------------------------------------------

// TraceEvent is the marker interface for all trace events.
//
// # Creating Custom Traces
//
// Use [CommonTraceEvent] for application-specific trace data:
//
//	execCtx.Trace(CommonTraceEvent{
//	    EventId:     "myapp:cache_hit",
//	    Description: "Cache lookup succeeded",
//	    Data:        CacheHitData{Key: cacheKey, TTL: ttl},
//	})
//
// For strongly-typed custom traces, embed [BaseTrace]:
//
//	type MyCacheTrace struct {
//	    gent.BaseTrace
//	    Key string
//	    Hit bool
//	}
//
//	func (MyCacheTrace) traceEvent() {} // Implement marker method
//
//	// Use it
//	execCtx.Trace(MyCacheTrace{Key: "user:123", Hit: true})
//
// # Accessing Traces
//
//	events := execCtx.Events()
//	for _, event := range events {
//	    switch e := event.(type) {
//	    case gent.ModelCallTrace:
//	        log.Printf("Model %s used %d tokens", e.Model, e.InputTokens)
//	    case gent.CommonTraceEvent:
//	        log.Printf("Event %s: %s", e.EventId, e.Description)
//	    case MyCacheTrace:
//	        log.Printf("Cache %s: hit=%v", e.Key, e.Hit)
//	    }
//	}
type TraceEvent interface {
	traceEvent() // marker method
}

// BaseTrace contains common fields auto-populated by ExecutionContext.Trace().
//
// When you call execCtx.Trace(event), the BaseTrace fields are automatically set:
//   - Timestamp: Current time
//   - Iteration: Current iteration number (1-indexed)
//   - Depth: Current nesting depth (0 for root context)
//
// Embed this in custom trace types for consistent behavior.
type BaseTrace struct {
	// Timestamp is when this event occurred.
	Timestamp time.Time

	// Iteration is the iteration number when this event occurred (1-indexed).
	Iteration int

	// Depth is the nesting depth when this event occurred (0 for root).
	Depth int
}

func (BaseTrace) traceEvent() {}

// -----------------------------------------------------------------------------
// Well-Known Trace Types
// -----------------------------------------------------------------------------

// ModelCallTrace records an LLM API call.
// When traced, auto-updates: TotalInputTokens, TotalOutputTokens, *ByModel maps.
type ModelCallTrace struct {
	BaseTrace

	// Model is the model identifier used.
	Model string

	// Request contains the messages sent to the model ([]llms.MessageContent).
	Request any

	// Response contains the full response from the model.
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

func (ModelCallTrace) traceEvent() {}

// ToolCallTrace records a tool execution.
// When traced, auto-updates: ToolCallCount, ToolCallsByName.
type ToolCallTrace struct {
	BaseTrace

	// ToolName is the name of the tool that was called.
	ToolName string

	// Input is the input provided to the tool.
	Input any

	// Output is the output from the tool.
	Output any

	// Duration is how long the call took.
	Duration time.Duration

	// Error is any error that occurred (nil if successful).
	Error error
}

func (ToolCallTrace) traceEvent() {}

// IterationStartTrace marks the beginning of an iteration.
type IterationStartTrace struct {
	BaseTrace
}

func (IterationStartTrace) traceEvent() {}

// IterationEndTrace marks the end of an iteration.
type IterationEndTrace struct {
	BaseTrace

	// Duration is how long this iteration took.
	Duration time.Duration

	// Action is the loop action returned by the AgentLoop.
	Action LoopAction
}

func (IterationEndTrace) traceEvent() {}

// ChildSpawnTrace records when a child ExecutionContext is created.
type ChildSpawnTrace struct {
	BaseTrace

	// ChildName is the name of the spawned child context.
	ChildName string
}

func (ChildSpawnTrace) traceEvent() {}

// ChildCompleteTrace records when a child ExecutionContext completes.
type ChildCompleteTrace struct {
	BaseTrace

	// ChildName is the name of the completed child context.
	ChildName string

	// TerminationReason is why the child execution ended.
	TerminationReason TerminationReason

	// Duration is how long the child execution took.
	Duration time.Duration
}

func (ChildCompleteTrace) traceEvent() {}

// CommonTraceEvent records informational events during execution.
//
// This is used by the framework for events like termination validation, and can
// be used by applications for custom events. The EventId field enables filtering
// and categorization of events.
//
// # EventId Conventions
//
// Use namespaced IDs to avoid collisions:
//   - Framework events: "gent:termination:validator_accepted"
//   - Application events: "myapp:cache_hit", "myapp:external_api_called"
//
// # Usage
//
//	execCtx.Trace(gent.CommonTraceEvent{
//	    EventId:     "myapp:order_lookup",
//	    Description: "Retrieved order from database",
//	    Data:        OrderData{OrderId: "12345", Status: "shipped"},
//	})
type CommonTraceEvent struct {
	BaseTrace

	// EventId identifies this event type. Use namespaced IDs like
	// "gent:termination:validator_accepted" or "myapp:cache_hit".
	EventId string

	// Description is a human-readable description of what happened.
	Description string

	// Data contains event-specific data. Can be any type.
	Data any
}

func (CommonTraceEvent) traceEvent() {}

// -----------------------------------------------------------------------------
// Well-Known EventIds for CommonTraceEvent
// -----------------------------------------------------------------------------

// EventId constants for termination validation events.
// These are traced by Termination implementations when validators are invoked.
const (
	// EventIdValidatorCalled is traced when a validator is invoked.
	// Data: ValidatorCalledData
	EventIdValidatorCalled = "gent:termination:validator_called"

	// EventIdValidatorAccepted is traced when a validator accepts the answer.
	// Data: ValidatorResultData
	EventIdValidatorAccepted = "gent:termination:validator_accepted"

	// EventIdValidatorRejected is traced when a validator rejects the answer.
	// Data: ValidatorResultData
	EventIdValidatorRejected = "gent:termination:validator_rejected"
)

// ValidatorCalledData contains data for EventIdValidatorCalled events.
type ValidatorCalledData struct {
	// ValidatorName is the name of the validator being called.
	ValidatorName string

	// Answer is the parsed answer being validated.
	Answer any
}

// ValidatorResultData contains data for validator result events
// (EventIdValidatorAccepted and EventIdValidatorRejected).
type ValidatorResultData struct {
	// ValidatorName is the name of the validator.
	ValidatorName string

	// Answer is the parsed answer that was validated.
	Answer any

	// Accepted is true if the validator accepted the answer.
	Accepted bool

	// Feedback contains the rejection feedback (only set when rejected).
	Feedback []FormattedSection
}

// ParseErrorTrace records a parse error (format or toolchain).
// When traced, auto-updates parse error counters based on ErrorType.
type ParseErrorTrace struct {
	BaseTrace

	// ErrorType is "format" for format parse errors or "toolchain" for toolchain parse errors.
	ErrorType string

	// RawContent is the content that failed to parse (no truncation).
	RawContent string

	// Error is the parse error that occurred.
	Error error
}

func (ParseErrorTrace) traceEvent() {}

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
