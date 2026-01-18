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

	// TerminationMaxIterations means max iterations was exceeded.
	TerminationMaxIterations TerminationReason = "max_iterations"

	// TerminationError means an error occurred.
	TerminationError TerminationReason = "error"

	// TerminationContextCanceled means the context was canceled.
	TerminationContextCanceled TerminationReason = "context_canceled"

	// TerminationHookAbort means a hook returned an error.
	TerminationHookAbort TerminationReason = "hook_abort"
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
// When traced, auto-updates: TotalInputTokens, TotalOutputTokens, TotalCost, *ByModel maps.
type ModelCallTrace struct {
	BaseTrace

	// Model is the model identifier used.
	Model string

	// InputTokens is the number of input/prompt tokens used.
	InputTokens int

	// OutputTokens is the number of output/completion tokens generated.
	OutputTokens int

	// Cost is the estimated cost in USD (if available).
	Cost float64

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

// -----------------------------------------------------------------------------
// Execution Result
// -----------------------------------------------------------------------------

// ExecutionResult contains the final result of an execution run.
type ExecutionResult struct {
	// Result is the final output from the AgentLoop (set when terminated successfully).
	// This is a slice of ContentPart to support multimodal outputs.
	Result []ContentPart

	// Context is the ExecutionContext with full trace data.
	Context *ExecutionContext

	// Error is any error that occurred (nil if successful).
	Error error
}
