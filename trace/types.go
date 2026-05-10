package trace

import (
	"time"

	"github.com/rickchristie/gent"
)

// SchemaVersion is the JSON schema version for trace snapshots and events.
const SchemaVersion = 1

// EventType is the normalized trace event type used by UIs and replay code.
type EventType string

const (
	EventTypeExecutionStarted  EventType = "execution_started"
	EventTypeExecutionFinished EventType = "execution_finished"
	EventTypeContextStarted    EventType = "context_started"
	EventTypeContextFinished   EventType = "context_finished"
	EventTypeIterationStarted  EventType = "iteration_started"
	EventTypeIterationFinished EventType = "iteration_finished"
	EventTypeSystemPrompt      EventType = "system_prompt"
	EventTypeModelCallStarted  EventType = "model_call_started"
	EventTypeModelCallFinished EventType = "model_call_finished"
	EventTypeModelStreamChunk  EventType = "model_stream_chunk"
	EventTypeToolCallStarted   EventType = "tool_call_started"
	EventTypeToolCallFinished  EventType = "tool_call_finished"
	EventTypeParseError        EventType = "parse_error"
	EventTypeValidatorCalled   EventType = "validator_called"
	EventTypeValidatorResult   EventType = "validator_result"
	EventTypeError             EventType = "error"
	EventTypeLimitExceeded     EventType = "limit_exceeded"
	EventTypeCompaction        EventType = "compaction"
	EventTypeCommon            EventType = "common"
	EventTypeCommonDiff        EventType = "common_diff"
)

// RunStatus is the materialized status of a traced run.
type RunStatus string

const (
	RunStatusPending       RunStatus = "pending"
	RunStatusRunning       RunStatus = "running"
	RunStatusSucceeded     RunStatus = "succeeded"
	RunStatusFailed        RunStatus = "failed"
	RunStatusCanceled      RunStatus = "canceled"
	RunStatusLimitExceeded RunStatus = "limit_exceeded"
)

// StepStatus is the materialized status of an execution step.
type StepStatus string

const (
	StepStatusRunning   StepStatus = "running"
	StepStatusSucceeded StepStatus = "succeeded"
	StepStatusFailed    StepStatus = "failed"
	StepStatusCanceled  StepStatus = "canceled"
)

// Event is a sequenced trace event.
type Event struct {
	EventNumber uint64    `json:"eventNumber"`
	RunId       string    `json:"runId"`
	Ts          time.Time `json:"ts"`
	Type        EventType `json:"type"`

	EventName string `json:"eventName,omitempty"`
	Iteration int    `json:"iteration,omitempty"`
	Depth     int    `json:"depth,omitempty"`
	Source    string `json:"source,omitempty"`

	ContextId       string `json:"contextId,omitempty"`
	ParentContextId string `json:"parentContextId,omitempty"`

	ModelCallId   string `json:"modelCallId,omitempty"`
	ToolCallId    string `json:"toolCallId,omitempty"`
	StreamId      string `json:"streamId,omitempty"`
	StreamTopicId string `json:"streamTopicId,omitempty"`

	Payload any    `json:"payload,omitempty"`
	Error   *Error `json:"error,omitempty"`
}

// Snapshot is the current materialized trace state for a run.
type Snapshot struct {
	SchemaVersion int       `json:"schemaVersion"`
	RunId         string    `json:"runId"`
	Status        RunStatus `json:"status"`

	StartedTs     time.Time  `json:"startedTs"`
	LastUpdatedTs time.Time  `json:"lastUpdatedTs"`
	CompletedTs   *time.Time `json:"completedTs,omitempty"`

	LastEventNumber uint64 `json:"lastEventNumber"`

	Result *RunResult `json:"result,omitempty"`
	Stats  RunStats   `json:"stats"`

	Contexts   []*Context   `json:"contexts,omitempty"`
	Iterations []*Iteration `json:"iterations,omitempty"`
	ModelCalls []*ModelCall `json:"modelCalls,omitempty"`
	ToolCalls  []*ToolCall  `json:"toolCalls,omitempty"`
	Errors     []*Error     `json:"errors,omitempty"`

	RecentEvents          []*Event `json:"recentEvents,omitempty"`
	RecentLifecycleEvents []*Event `json:"recentLifecycleEvents,omitempty"`
	RecentChunkEvents     []*Event `json:"recentChunkEvents,omitempty"`
}

// RunResult contains final run output when configured.
type RunResult struct {
	TerminationReason gent.TerminationReason `json:"terminationReason"`
	Output            any                    `json:"output,omitempty"`
	Error             *Error                 `json:"error,omitempty"`
}

// Context is a materialized execution context record.
type Context struct {
	Id       string `json:"id"`
	ParentId string `json:"parentId,omitempty"`
	Name     string `json:"name,omitempty"`
	Source   string `json:"source,omitempty"`
	Depth    int    `json:"depth,omitempty"`

	Status StepStatus `json:"status"`

	StartedTs     time.Time  `json:"startedTs"`
	LastUpdatedTs time.Time  `json:"lastUpdatedTs"`
	CompletedTs   *time.Time `json:"completedTs,omitempty"`
	DurationMs    int64      `json:"durationMs,omitempty"`

	StartedEventNumber   uint64 `json:"startedEventNumber"`
	LastEventNumber      uint64 `json:"lastEventNumber"`
	CompletedEventNumber uint64 `json:"completedEventNumber,omitempty"`

	Error *Error `json:"error,omitempty"`
}

// Iteration is a materialized agent-loop iteration record.
type Iteration struct {
	Iteration int    `json:"iteration"`
	Depth     int    `json:"depth"`
	Source    string `json:"source,omitempty"`

	ContextId       string `json:"contextId"`
	ParentContextId string `json:"parentContextId,omitempty"`

	Status StepStatus `json:"status"`

	StartedTs     time.Time  `json:"startedTs"`
	LastUpdatedTs time.Time  `json:"lastUpdatedTs"`
	CompletedTs   *time.Time `json:"completedTs,omitempty"`
	DurationMs    int64      `json:"durationMs,omitempty"`

	StartedEventNumber   uint64 `json:"startedEventNumber"`
	LastEventNumber      uint64 `json:"lastEventNumber"`
	CompletedEventNumber uint64 `json:"completedEventNumber,omitempty"`

	ModelCallIds []string `json:"modelCallIds,omitempty"`
	ToolCallIds  []string `json:"toolCallIds,omitempty"`

	Result any    `json:"result,omitempty"`
	Error  *Error `json:"error,omitempty"`
}

// ModelCall is a materialized model call record.
type ModelCall struct {
	Id            string `json:"id"`
	Model         string `json:"model"`
	Provider      string `json:"provider,omitempty"`
	StreamId      string `json:"streamId,omitempty"`
	StreamTopicId string `json:"streamTopicId,omitempty"`
	Source        string `json:"source,omitempty"`
	Iteration     int    `json:"iteration,omitempty"`
	Depth         int    `json:"depth,omitempty"`

	ContextId       string `json:"contextId"`
	ParentContextId string `json:"parentContextId,omitempty"`

	Status StepStatus `json:"status"`

	StartedTs     time.Time  `json:"startedTs"`
	LastUpdatedTs time.Time  `json:"lastUpdatedTs"`
	CompletedTs   *time.Time `json:"completedTs,omitempty"`
	DurationMs    int64      `json:"durationMs,omitempty"`

	StartedEventNumber   uint64 `json:"startedEventNumber"`
	LastEventNumber      uint64 `json:"lastEventNumber"`
	CompletedEventNumber uint64 `json:"completedEventNumber,omitempty"`

	Request  any          `json:"request,omitempty"`
	Response any          `json:"response,omitempty"`
	Usage    *ModelUsage  `json:"usage,omitempty"`
	Stream   *ModelStream `json:"stream,omitempty"`
	Error    *Error       `json:"error,omitempty"`
}

// ModelStream is the materialized stream state for a model call.
type ModelStream struct {
	Content          string `json:"content,omitempty"`
	ReasoningContent string `json:"reasoningContent,omitempty"`

	ChunkCount int `json:"chunkCount"`

	ContentTruncated          bool `json:"contentTruncated,omitempty"`
	ReasoningContentTruncated bool `json:"reasoningContentTruncated,omitempty"`

	LastChunkEventNumber uint64 `json:"lastChunkEventNumber,omitempty"`
}

// ModelUsage contains normalized token usage.
type ModelUsage struct {
	InputTokens       int `json:"inputTokens"`
	OutputTokens      int `json:"outputTokens"`
	ReasoningTokens   int `json:"reasoningTokens,omitempty"`
	CachedInputTokens int `json:"cachedInputTokens,omitempty"`
	TotalTokens       int `json:"totalTokens"`
}

// ToolCall is a materialized tool call record.
type ToolCall struct {
	Id        string `json:"id"`
	Name      string `json:"name"`
	Source    string `json:"source,omitempty"`
	Iteration int    `json:"iteration,omitempty"`
	Depth     int    `json:"depth,omitempty"`

	ContextId       string `json:"contextId"`
	ParentContextId string `json:"parentContextId,omitempty"`

	Status StepStatus `json:"status"`

	StartedTs     time.Time  `json:"startedTs"`
	LastUpdatedTs time.Time  `json:"lastUpdatedTs"`
	CompletedTs   *time.Time `json:"completedTs,omitempty"`
	DurationMs    int64      `json:"durationMs,omitempty"`

	StartedEventNumber   uint64 `json:"startedEventNumber"`
	LastEventNumber      uint64 `json:"lastEventNumber"`
	CompletedEventNumber uint64 `json:"completedEventNumber,omitempty"`

	Args   any    `json:"args,omitempty"`
	Output any    `json:"output,omitempty"`
	Error  *Error `json:"error,omitempty"`
}

// Error is a redacted, JSON-safe error representation.
type Error struct {
	Message string `json:"message"`
	Type    string `json:"type,omitempty"`

	EventNumber uint64    `json:"eventNumber,omitempty"`
	Ts          time.Time `json:"ts,omitempty"`
	EventName   string    `json:"eventName,omitempty"`
	Source      string    `json:"source,omitempty"`
	Iteration   int       `json:"iteration,omitempty"`
	Depth       int       `json:"depth,omitempty"`

	ContextId       string `json:"contextId,omitempty"`
	ParentContextId string `json:"parentContextId,omitempty"`

	ModelCallId string `json:"modelCallId,omitempty"`
	ToolCallId  string `json:"toolCallId,omitempty"`
}

// RunStats contains counts and durations derived from sequenced trace events.
type RunStats struct {
	ContextCount   int `json:"contextCount"`
	IterationCount int `json:"iterationCount"`
	ModelCallCount int `json:"modelCallCount"`
	ToolCallCount  int `json:"toolCallCount"`

	InputTokens       int `json:"inputTokens"`
	OutputTokens      int `json:"outputTokens"`
	ReasoningTokens   int `json:"reasoningTokens"`
	CachedInputTokens int `json:"cachedInputTokens"`
	TotalTokens       int `json:"totalTokens"`

	DurationMs      int64 `json:"durationMs"`
	ModelDurationMs int64 `json:"modelDurationMs"`
	ToolDurationMs  int64 `json:"toolDurationMs"`

	ParseErrorCount         int `json:"parseErrorCount"`
	ValidatorRejectionCount int `json:"validatorRejectionCount"`
	ErrorCount              int `json:"errorCount"`
	LimitExceededCount      int `json:"limitExceededCount"`
	CompactionCount         int `json:"compactionCount"`
}

// PayloadCaptureError replaces payloads that cannot be made JSON-safe.
type PayloadCaptureError struct {
	Type  string `json:"type"`
	Error string `json:"error"`
}
