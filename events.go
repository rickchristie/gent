package gent

import "time"

// -----------------------------------------------------------------------------
// Hook Event Interface
// -----------------------------------------------------------------------------

// HookEvent is a marker interface for all hook events.
type HookEvent interface {
	hookEvent()
}

// -----------------------------------------------------------------------------
// Executor Events
// -----------------------------------------------------------------------------

// BeforeExecutionEvent is emitted once before the first iteration begins.
type BeforeExecutionEvent struct{}

func (BeforeExecutionEvent) hookEvent() {}

// AfterExecutionEvent is emitted once after execution terminates.
type AfterExecutionEvent struct {
	// TerminationReason indicates why execution ended.
	TerminationReason TerminationReason

	// Error is the error if execution failed (nil on success).
	Error error
}

func (AfterExecutionEvent) hookEvent() {}

// BeforeIterationEvent is emitted before each AgentLoop.Next call.
type BeforeIterationEvent struct {
	// Iteration is the current iteration number (1-indexed).
	Iteration int
}

func (BeforeIterationEvent) hookEvent() {}

// AfterIterationEvent is emitted after each AgentLoop.Next call.
type AfterIterationEvent struct {
	// Iteration is the current iteration number (1-indexed).
	Iteration int

	// Result is the AgentLoopResult from this iteration.
	Result *AgentLoopResult

	// Duration is how long this iteration took.
	Duration time.Duration
}

func (AfterIterationEvent) hookEvent() {}

// ErrorEvent is emitted when an error occurs during execution.
type ErrorEvent struct {
	// Iteration is the iteration where the error occurred (0 if before first iteration).
	Iteration int

	// Err is the error that occurred.
	Err error
}

func (ErrorEvent) hookEvent() {}

// -----------------------------------------------------------------------------
// Model Call Events
// -----------------------------------------------------------------------------

// BeforeModelCallEvent is emitted before each model API call.
type BeforeModelCallEvent struct {
	// Model is the model identifier.
	Model string

	// Request contains the messages being sent to the model.
	Request any
}

func (BeforeModelCallEvent) hookEvent() {}

// AfterModelCallEvent is emitted after each model API call completes.
type AfterModelCallEvent struct {
	// Model is the model identifier.
	Model string

	// Request contains the messages that were sent to the model.
	Request any

	// Response contains the full response from the model.
	Response *ContentResponse

	// Duration is how long the call took.
	Duration time.Duration

	// Error is any error that occurred (nil if successful).
	Error error
}

func (AfterModelCallEvent) hookEvent() {}

// -----------------------------------------------------------------------------
// Tool Call Events
// -----------------------------------------------------------------------------

// BeforeToolCallEvent is emitted before each tool call execution.
// Hooks can modify Args to change the input before execution.
type BeforeToolCallEvent struct {
	// ToolName is the name of the tool being called.
	ToolName string

	// Args contains the arguments that will be passed to the tool.
	// Hooks can modify this map to change the arguments.
	Args map[string]any
}

func (BeforeToolCallEvent) hookEvent() {}

// AfterToolCallEvent is emitted after each tool call execution.
type AfterToolCallEvent struct {
	// ToolName is the name of the tool that was called.
	ToolName string

	// Args contains the arguments that were passed to the tool.
	Args map[string]any

	// Output contains the tool's output (nil if error occurred).
	Output any

	// Duration is how long the tool call took.
	Duration time.Duration

	// Error is any error that occurred (nil if successful).
	Error error
}

func (AfterToolCallEvent) hookEvent() {}
