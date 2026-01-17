package gent

import (
	"sync"
	"time"
)

// -----------------------------------------------------------------------------
// Execution Trace
// -----------------------------------------------------------------------------

// ExecutionTrace stores detailed debug and trace information for an execution run.
type ExecutionTrace struct {
	// Iterations contains trace data for each loop iteration.
	Iterations []IterationTrace

	// StartTime is when execution began.
	StartTime time.Time

	// EndTime is when execution completed.
	EndTime time.Time

	// TotalDuration is the total execution time.
	TotalDuration time.Duration

	// TerminationReason describes why execution ended.
	TerminationReason TerminationReason

	// FinalIteration is the last iteration number (1-indexed).
	FinalIteration int
}

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

// IterationTrace stores trace data for a single loop iteration.
type IterationTrace struct {
	// Iteration is the iteration number (1-indexed).
	Iteration int

	// StartTime is when this iteration began.
	StartTime time.Time

	// EndTime is when this iteration completed.
	EndTime time.Time

	// Duration is how long this iteration took.
	Duration time.Duration

	// Result is the AgentLoopResult from this iteration.
	Result *AgentLoopResult

	// Error is any error that occurred during this iteration (nil if successful).
	Error error

	// Metadata allows AgentLoop implementations to attach custom trace data.
	// For example, LLM call details, tool call traces, token counts, costs, etc.
	Metadata map[string]any
}

// -----------------------------------------------------------------------------
// Trace Collector
// -----------------------------------------------------------------------------

// TraceCollector is a helper that can be attached to LoopData to collect
// detailed trace information from within the AgentLoop.
//
// AgentLoop implementations can use this to record LLM calls, tool calls,
// token counts, costs, and other detailed information.
type TraceCollector struct {
	mu        sync.Mutex
	llmCalls  []LLMCallTrace
	toolCalls []ToolCallTrace
}

// NewTraceCollector creates a new TraceCollector.
func NewTraceCollector() *TraceCollector {
	return &TraceCollector{
		llmCalls:  make([]LLMCallTrace, 0),
		toolCalls: make([]ToolCallTrace, 0),
	}
}

// LLMCallTrace stores trace information for a single LLM API call.
type LLMCallTrace struct {
	// StartTime is when the call began.
	StartTime time.Time

	// EndTime is when the call completed.
	EndTime time.Time

	// Duration is how long the call took.
	Duration time.Duration

	// Model is the model identifier used.
	Model string

	// PromptTokens is the number of input tokens (if available).
	PromptTokens int

	// CompletionTokens is the number of output tokens (if available).
	CompletionTokens int

	// TotalTokens is the total token count (if available).
	TotalTokens int

	// Cost is the estimated cost in USD (if available).
	Cost float64

	// Prompt is the input prompt (may be truncated for large prompts).
	Prompt string

	// Response is the model response (may be truncated for large responses).
	Response string

	// Error is any error that occurred (nil if successful).
	Error error

	// Metadata allows storing additional provider-specific data.
	Metadata map[string]any
}

// ToolCallTrace stores trace information for a single tool call.
type ToolCallTrace struct {
	// StartTime is when the call began.
	StartTime time.Time

	// EndTime is when the call completed.
	EndTime time.Time

	// Duration is how long the call took.
	Duration time.Duration

	// ToolName is the name of the tool that was called.
	ToolName string

	// Input is the input provided to the tool.
	Input string

	// Output is the output from the tool.
	Output string

	// Error is any error that occurred (nil if successful).
	Error error

	// Metadata allows storing additional tool-specific data.
	Metadata map[string]any
}

// RecordLLMCall records an LLM call trace.
func (tc *TraceCollector) RecordLLMCall(trace LLMCallTrace) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.llmCalls = append(tc.llmCalls, trace)
}

// RecordToolCall records a tool call trace.
func (tc *TraceCollector) RecordToolCall(trace ToolCallTrace) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.toolCalls = append(tc.toolCalls, trace)
}

// GetLLMCalls returns all recorded LLM call traces.
func (tc *TraceCollector) GetLLMCalls() []LLMCallTrace {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	result := make([]LLMCallTrace, len(tc.llmCalls))
	copy(result, tc.llmCalls)
	return result
}

// GetToolCalls returns all recorded tool call traces.
func (tc *TraceCollector) GetToolCalls() []ToolCallTrace {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	result := make([]ToolCallTrace, len(tc.toolCalls))
	copy(result, tc.toolCalls)
	return result
}

// Clear resets the trace collector for reuse.
func (tc *TraceCollector) Clear() {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.llmCalls = make([]LLMCallTrace, 0)
	tc.toolCalls = make([]ToolCallTrace, 0)
}

// TotalCost returns the sum of costs from all LLM calls.
func (tc *TraceCollector) TotalCost() float64 {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	var total float64
	for _, call := range tc.llmCalls {
		total += call.Cost
	}
	return total
}

// TotalTokens returns the sum of tokens from all LLM calls.
func (tc *TraceCollector) TotalTokens() int {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	var total int
	for _, call := range tc.llmCalls {
		total += call.TotalTokens
	}
	return total
}

// ToMetadata converts the collector's data to a metadata map suitable for IterationTrace.
func (tc *TraceCollector) ToMetadata() map[string]any {
	return map[string]any{
		"llm_calls":    tc.GetLLMCalls(),
		"tool_calls":   tc.GetToolCalls(),
		"total_cost":   tc.TotalCost(),
		"total_tokens": tc.TotalTokens(),
	}
}

// -----------------------------------------------------------------------------
// Execution Result
// -----------------------------------------------------------------------------

// ExecutionResult contains the final result of an execution run.
type ExecutionResult struct {
	// Result is the final output from the AgentLoop (set when terminated successfully).
	// This is a slice of ContentPart to support multimodal outputs.
	Result []ContentPart

	// Trace contains detailed execution trace data.
	Trace *ExecutionTrace

	// Error is any error that occurred (nil if successful).
	Error error
}
