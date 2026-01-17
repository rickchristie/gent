package gent

import "github.com/tmc/langchaingo/llms"

// -----------------------------------------------------------------------------
// Executor Events
// -----------------------------------------------------------------------------

// BeforeExecutionEvent is emitted once before the first iteration begins.
type BeforeExecutionEvent[Data LoopData] struct {
	// Data is the initial LoopData provided to Execute.
	Data Data
}

// AfterExecutionEvent is emitted once after execution terminates.
type AfterExecutionEvent struct {
	// Result contains the final execution result.
	Result *ExecutionResult
	// Iterations contains trace data for all iterations that were executed.
	Iterations []IterationTrace
}

// BeforeIterationEvent is emitted before each AgentLoop.Iterate call.
type BeforeIterationEvent[Data LoopData] struct {
	// Iteration is the current iteration number (1-indexed).
	Iteration int
	// Data is the current LoopData.
	Data Data
}

// AfterIterationEvent is emitted after each AgentLoop.Iterate call.
type AfterIterationEvent[Data LoopData] struct {
	// Iteration is the current iteration number (1-indexed).
	Iteration int
	// Result is the AgentLoopResult from this iteration.
	Result *AgentLoopResult
	// Data is the current LoopData.
	Data Data
}

// ErrorEvent is emitted when an error occurs during execution.
type ErrorEvent struct {
	// Iteration is the iteration where the error occurred (0 if before first iteration).
	Iteration int
	// Err is the error that occurred.
	Err error
}

// -----------------------------------------------------------------------------
// Model Events
// -----------------------------------------------------------------------------

// BeforeGenerateContentEvent is emitted before calling Model.GenerateContent.
type BeforeGenerateContentEvent struct {
	// Messages is the input messages to be sent to the model.
	Messages []llms.MessageContent
	// Options is the resolved call options.
	Options *llms.CallOptions
}

// AfterGenerateContentEvent is emitted after Model.GenerateContent returns.
type AfterGenerateContentEvent struct {
	// Messages is the input messages that were sent to the model.
	Messages []llms.MessageContent
	// Options is the resolved call options.
	Options *llms.CallOptions
	// Response is the response from the model (nil if error occurred).
	Response *ContentResponse
	// Error is the error returned by the model (nil if successful).
	Error error
}
