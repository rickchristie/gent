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
