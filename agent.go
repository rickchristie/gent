package gent

import (
	"sync"
	"time"
)

// Loop runs one iteration of the agent's think-act-observe cycle.
//
// # Responsibilities
//
//  1. Construct the prompt to be sent to the LLM model
//  2. Call the LLM model with the constructed prompt
//  3. Parse LLM output (tool calls, termination, other sections)
//  4. Execute any tool calls and collect observations
//  5. Decide whether to continue iterating or terminate
//
// # Available Implementations
//
//   - agents/react: ReAct-style agent with thinking, action, and answer sections
//
// # Implementing Custom Loops
//
// Custom loops can use provided building blocks ([ToolChain], [Termination], [TextFormat])
// or implement everything from scratch. Use ExecutionContext.GenerateContentStream for managed
// model calls, AppendRecord/UpdateHistory for context edits, and explicit checkpoint barriers.
// Custom continuation state implements CheckpointState; see docs/history.md and examples/history.
//
// The executor calls Next() repeatedly until it returns [LATerminate] or a limit is exceeded.
type Loop interface {
	// Next performs one iteration of the agent loop.
	//
	// The ExecutionContext provides:
	//   - Access to LoopData via execCtx.Data()
	//   - Automatic tracing for all framework components
	//   - Context for cancellation via execCtx.Context()
	//   - Stats for limit checking via execCtx.Stats()
	//
	// Use execCtx.Context() when calling external APIs that require context.Context.
	// The context is cancelled when limits are exceeded or the parent context is cancelled.
	//
	// Return values:
	//   - LAContinue: Continue to next iteration with NextPrompt as observation
	//   - LATerminate: Stop execution with Result as final output
	//   - error: Iteration failed, execution terminates with error
	Next(execCtx *ExecutionContext) (*AgentLoopResult, error)
}

// AgentLoop keeps the existing generic API. Data does not restrict the execution context.
//
// Deprecated: Use Loop. It has the same Next method and needs no type argument.
type AgentLoop[Data LoopData] interface {
	Loop
}

// LoopData supplies the initial task and can carry application-specific state for an AgentLoop.
// ExecutionContext freezes that task and owns history and the active context selection. History
// changes go through its APIs so evidence, selection changes, and checkpoints can commit together.
// Custom data implements CheckpointState when its mutable fields must survive worker replacement.
type LoopData interface {
	GetTask() *Task
}

// BasicLoopData is the default implementation of [LoopData].
//
// It stores the initial task with concurrency-safe access. ExecutionContext owns history and
// scratchpad. Use BasicLoopData directly or embed it in custom structs for additional loop data.
//
// # Direct Usage
//
//	data := gent.NewBasicLoopData(task)
//	execCtx := gent.NewExecutionContext(ctx, "my-agent", data)
//
// # Embedding for Custom Data
//
// Embed BasicLoopData to add custom fields while retaining all standard LoopData methods:
//
//	type MyLoopData struct {
//	    gent.BasicLoopData
//	    SessionID   string
//	    UserContext map[string]any
//	}
//
//	func NewMyLoopData(task *gent.Task, sessionID string) *MyLoopData {
//	    return &MyLoopData{
//	        BasicLoopData: *gent.NewBasicLoopData(task),
//	        SessionID:     sessionID,
//	        UserContext:   make(map[string]any),
//	    }
//	}
//
// The embedded struct automatically satisfies [LoopData] and works with all agent loops.
type BasicLoopData struct {
	mu   sync.RWMutex
	task *Task
}

// NewBasicLoopData copies the task envelope. ExecutionContext freezes its complete typed content.
func NewBasicLoopData(task *Task) *BasicLoopData {
	return &BasicLoopData{task: copyTask(task)}
}

func copyTask(task *Task) *Task {
	if task == nil {
		return nil
	}
	result := *task
	result.Media = append([]ContentPart(nil), task.Media...)
	return &result
}

// GetTask supplies the initial task. Managed loops use ExecutionContext.Task for owned snapshots.
func (d *BasicLoopData) GetTask() *Task {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return copyTask(d.task)
}

// RecordOrigin indicates how a HistoryEntry was created: an original observation, a summary,
// a replacement derived from earlier content, retrieved history, or a system injection.
type RecordOrigin string

const (
	// RecordOriginal is the zero value. The record was
	// created normally by the AgentLoop during execution.
	RecordOriginal RecordOrigin = ""

	// RecordCompactedSynthetic means this record summarizes earlier records, which may span
	// multiple executor iterations.
	RecordCompactedSynthetic RecordOrigin = "compacted_synthetic"

	// RecordCompactedModified means this record was derived by modifying earlier content.
	// The replacement gets a new history entry; the original entry remains unchanged.
	RecordCompactedModified RecordOrigin = "compacted_modified"

	// RecordRetrievedHistory means this record was retrieved from earlier history for reuse
	// in working context (e.g., via a retrieval-augmented strategy).
	RecordRetrievedHistory RecordOrigin = "retrieved_history"

	// RecordSystemInjected means this record was injected
	// by a system hook (e.g., pre-search policy suggestions).
	// It is not produced by the agent or user.
	RecordSystemInjected RecordOrigin = "system_injected"
)

// RecordMetadataKey is a typed key for HistoryEntry metadata.
// The framework defines standard keys (IMK* constants). Users
// can define their own keys with a custom prefix.
type RecordMetadataKey string

// IMKImportanceScore is a float64 value in [-10, 10] that
// indicates how important this record is.
//
// Positive values = more important (prefer to keep).
// Negative values = less important (prefer to discard).
// Zero or absent = neutral (default treatment).
//
// The standard strategies treat any score >=
// [ImportanceScorePinned] (10.0) as "pinned" and preserve
// those records in the active selection through compaction.
//
// Usage:
//
//	// Keep this record in the active selection through compaction:
//	record.SetMetadata(
//	    gent.IMKImportanceScore,
//	    gent.ImportanceScorePinned,
//	)
//
//	score, ok := gent.GetImportanceScore(record)
const IMKImportanceScore RecordMetadataKey = "gent:importance_score"

// ImportanceScorePinned is the minimum importance score for
// a record to be considered "pinned" by the standard
// compaction strategies. Pinned records remain selected
// through compaction; all original history entries are retained regardless of their score.
//
// The standard strategies use: score >= ImportanceScorePinned.
const ImportanceScorePinned float64 = 10.0

// HistoryEntry preserves a message group, a summary, or a compaction decision with stable identity.
// Message entries are selected, expired, and compacted together. One Next call can produce several
// entries, and a summary can represent several iterations. A compaction decision stores provenance
// in Compaction instead of Messages. Iteration counters and limits count Next calls, never entries.
//
// Execution owns accepted entries. Getters and recorder/observer callbacks return owned snapshots;
// mutating those pointers never updates execution. Use UpdateHistory to append a replacement.
//
// Messages must never contain nil elements. All code that
// constructs or modifies a HistoryEntry must ensure every
// element in Messages is a valid, non-nil pointer.
type HistoryEntry struct {
	Id          EntryId           `json:"id"`
	Order       uint64            `json:"order"`
	RunId       string            `json:"runId"`
	ContextId   string            `json:"contextId"`
	Iteration   int               `json:"iteration"`
	AttemptId   string            `json:"attemptId"`
	Kind        HistoryEntryKind  `json:"kind"`
	ModelCallId string            `json:"modelCallId,omitempty"`
	ToolCallIds []string          `json:"toolCallIds,omitempty"`
	Compaction  *CompactionRecord `json:"compaction,omitempty"`
	Replaces    []EntryId         `json:"replaces,omitempty"`
	CreatedAt   time.Time         `json:"createdAt"`

	Messages []*MessageContent

	// Origin indicates how this record was created.
	// Zero value (RecordOriginal) means it was created
	// normally by the AgentLoop.
	Origin RecordOrigin

	// ExpireAfterIteration is the executor iteration number
	// at which this record expires from working context. Zero (default) means
	// no expiry. Since executor iterations are 1-indexed,
	// zero is unambiguous.
	//
	// When set to a positive value, the record is
	// included when execCtx.Iteration() < ExpireAfterIteration
	// and skipped when execCtx.Iteration() >= ExpireAfterIteration.
	//
	// Example: ExpireAfterIteration = 3 means the record
	// is included in iterations 1 and 2, and skipped from
	// iteration 3 onward. Its original history entry remains available.
	ExpireAfterIteration int

	// Metadata contains optional key-value pairs for this
	// record. The framework defines standard keys (IMK*
	// constants); users can add custom keys.
	//
	// This field is exported for JSON marshalling. Use
	// SetMetadata/GetMetadata helper methods for access
	// instead of writing to the map directly.
	//
	// This field is nil by default and lazily initialized on
	// first write. Always use SetMetadata/GetMetadata helper
	// methods instead of accessing the map directly to avoid
	// nil map panics.
	//
	//   // WRONG — panics if Metadata is nil:
	//   iter.Metadata[gent.IMKImportanceScore] = 5.0
	//
	//   // CORRECT — safe, initializes map if needed:
	//   iter.SetMetadata(gent.IMKImportanceScore, 5.0)
	//
	//   // CORRECT — safe, returns zero value + false:
	//   val, ok := iter.GetMetadata(gent.IMKImportanceScore)
	Metadata map[RecordMetadataKey]any
}

// SetMetadata sets a metadata value, initializing the map
// if nil.
func (i *HistoryEntry) SetMetadata(
	key RecordMetadataKey,
	value any,
) {
	if i.Metadata == nil {
		i.Metadata = make(map[RecordMetadataKey]any)
	}
	i.Metadata[key] = value
}

// GetMetadata returns a metadata value and whether it was
// present. Returns (nil, false) if Metadata is nil or the
// key is absent.
func (i *HistoryEntry) GetMetadata(
	key RecordMetadataKey,
) (any, bool) {
	if i.Metadata == nil {
		return nil, false
	}
	val, ok := i.Metadata[key]
	return val, ok
}

// GetImportanceScore is a convenience function that returns
// the importance score for a record.
// Returns (0, false) if the key is absent or not a float64.
func GetImportanceScore(iter *HistoryEntry) (float64, bool) {
	val, ok := iter.GetMetadata(IMKImportanceScore)
	if !ok {
		return 0, false
	}
	score, ok := val.(float64)
	return score, ok
}

// MessageContentMetadataKey is a typed key for MessageContent
// metadata. Framework keys use the "gent:" prefix (MMK*
// constants). Users may define custom keys in their own
// namespace.
type MessageContentMetadataKey string

// MMKToolChainResult stores the [*ToolChainResult] on an
// observation [MessageContent]. Used by ScratchpadToMessages
// to deduplicate repeated stateless tool calls.
const MMKToolChainResult MessageContentMetadataKey = "gent:tool_chain_result"

// MessageContent is wrapper around [ModelMessage] used
// in AgentLoop.
type MessageContent struct {
	Continuation *ProviderContinuation
	Role         ChatMessageType
	Parts        []ContentPart

	// Metadata contains optional key-value pairs for this
	// message. The framework defines standard keys (MMK*
	// constants); users can add custom keys.
	//
	// This field is nil by default and lazily initialized on
	// first write. Always use SetMetadata/GetMetadata helper
	// methods instead of accessing the map directly to avoid
	// nil map panics.
	Metadata map[MessageContentMetadataKey]any
}

// SetMetadata sets a metadata value, initializing the map
// if nil.
func (m *MessageContent) SetMetadata(
	key MessageContentMetadataKey,
	value any,
) {
	if m.Metadata == nil {
		m.Metadata = make(map[MessageContentMetadataKey]any)
	}
	m.Metadata[key] = value
}

// GetMetadata returns a metadata value and whether it was
// present. Returns (nil, false) if Metadata is nil or the
// key is absent.
func (m *MessageContent) GetMetadata(
	key MessageContentMetadataKey,
) (any, bool) {
	if m.Metadata == nil {
		return nil, false
	}
	val, ok := m.Metadata[key]
	return val, ok
}

// Task represents the input to an agent loop.
// The client is responsible for formatting the Text field (including chat history if applicable).
type Task struct {
	// Text is the formatted task description/instructions.
	// For chat tasks, this should include the formatted message history.
	Text string

	// Media contains images, audio, or other non-text content.
	Media []ContentPart
}

// AsContentParts returns the task as a slice of ContentParts for building LLM messages.
// The text is returned first (if non-empty), followed by any media.
func (t *Task) AsContentParts() []ContentPart {
	var parts []ContentPart
	if t.Text != "" {
		parts = append(parts, TextContent{Text: t.Text})
	}
	return append(parts, t.Media...)
}

type LoopAction string

const (
	LAContinue  LoopAction = "c"
	LATerminate LoopAction = "t"
)

type AgentLoopResult struct {
	// Action indicates whether to continue or terminate the loop.
	// The [AgentLoop] is completely free to determine whether to continue or terminate.
	Action LoopAction

	// NextPrompt is only set when Action is [LAContinue].
	NextPrompt string

	// Result is only set when Action is [LATerminate].
	// This is a slice of ContentPart to support multimodal outputs.
	Result []ContentPart

	// ContentSource optionally identifies the provisional messages replaced by this result.
	ContentSource *ContentSource
}
