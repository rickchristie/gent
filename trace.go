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
type TraceEvent interface {
	traceEvent() // marker method
}

// BaseTrace contains common fields auto-populated by ExecutionContext.Trace().
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

// CustomTrace allows recording arbitrary trace data for custom AgentLoop implementations.
type CustomTrace struct {
	BaseTrace

	// Name identifies this custom trace type.
	Name string

	// Data contains arbitrary trace data.
	Data map[string]any
}

func (CustomTrace) traceEvent() {}

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
