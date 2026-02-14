package gent

import (
	"github.com/tmc/langchaingo/llms"
)

// AgentLoop orchestrates a single iteration of the agent's think-act-observe cycle.
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
// or implement everything from scratch. Example:
//
//	type MyLoop struct {
//	    model      gent.Model
//	    toolchain  gent.ToolChain
//	    termination gent.Termination
//	    format     gent.TextFormat
//	}
//
//	func (l *MyLoop) Next(execCtx *gent.ExecutionContext) (*gent.AgentLoopResult, error) {
//	    data := execCtx.Data().(*MyLoopData)
//
//	    // 1. Build prompt from task and scratchpad
//	    messages := l.buildMessages(data)
//
//	    // 2. Call model
//	    response, err := l.model.GenerateContent(execCtx, "gpt-4", systemPrompt, messages)
//	    if err != nil {
//	        return nil, err
//	    }
//
//	    // 3. Parse output
//	    sections, err := l.format.Parse(execCtx, response.Text)
//	    if err != nil {
//	        // Feed parse error back to LLM
//	        return &gent.AgentLoopResult{
//	            Action:     gent.LAContinue,
//	            NextPrompt: fmt.Sprintf("Parse error: %v", err),
//	        }, nil
//	    }
//
//	    // 4. Check for tool calls
//	    if actionContent, ok := sections[l.toolchain.Name()]; ok {
//	        result, _ := l.toolchain.Execute(execCtx, actionContent[0], l.format)
//	        data.AddIterationHistory(...)
//	        return &gent.AgentLoopResult{Action: gent.LAContinue, NextPrompt: result.Text}, nil
//	    }
//
//	    // 5. Check for termination
//	    if answerContent, ok := sections[l.termination.Name()]; ok {
//	        result := l.termination.ShouldTerminate(execCtx, answerContent[0])
//	        if result.Status == gent.TerminationAnswerAccepted {
//	            return &gent.AgentLoopResult{Action: gent.LATerminate, Result: result.Content}, nil
//	        }
//	    }
//
//	    return &gent.AgentLoopResult{Action: gent.LAContinue, NextPrompt: "Continue..."}, nil
//	}
//
// The executor calls Next() repeatedly until it returns [LATerminate] or a limit is exceeded.
type AgentLoop[Data LoopData] interface {
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

// LoopData is the data that is being passed through each [AgentLoop] execution. Each [AgentLoop]
// implementation may define their own Data interface.
//
// The interface methods within this interface allows custom [AgentLoop] implementations to work with
// provided hooks, for logging, metrics, etc.
type LoopData interface {
	// GetTask returns the original input provided by the user that started the agent loop.
	// The Task.Text should be pre-formatted by the client (including chat history if applicable).
	GetTask() *Task

	// GetIterationHistory returns all [Iteration] recorded, including those that may be
	// compacted away from GetScratchPad.
	GetIterationHistory() []*Iteration

	// AddIterationHistory adds a new [Iteration] to the full history only.
	AddIterationHistory(iter *Iteration)

	// GetScratchPad returns all [Iteration] recorded, that will be used in next iteration.
	// When compaction happens, some earlier iterations may be removed from this slice, but they
	// are preserved in GetIterationHistory.
	GetScratchPad() []*Iteration

	// SetScratchPad sets the iterations to be used in next iteration.
	// [AgentLoop] implementations are free to call [GetScratchPad] and modify it how they want,
	// then call this method to set the modified iterations to be used in the next iteration.
	SetScratchPad([]*Iteration)

	// SetExecutionContext sets the ExecutionContext for this LoopData.
	// Called automatically by [NewExecutionContext] to enable automatic event publishing
	// when iteration history or scratchpad changes.
	//
	// Implementations that embed [BasicLoopData] get this for free via method promotion.
	// Custom implementations can use this to publish [CommonDiffEvent] on state changes,
	// or implement as a no-op if event publishing is not needed.
	SetExecutionContext(ctx *ExecutionContext)
}

// BasicLoopData is the default implementation of [LoopData].
//
// It provides basic storage for task, iteration history, and scratchpad. This is used by
// the built-in agent loops (e.g., agents/react) and can be used directly or embedded in
// custom structs for additional data.
//
// # Direct Usage
//
//	data := gent.NewLoopData(task)
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
//	        BasicLoopData: *gent.NewLoopData(task),
//	        SessionID:     sessionID,
//	        UserContext:   make(map[string]any),
//	    }
//	}
//
// The embedded struct automatically satisfies [LoopData] and works with all agent loops.
type BasicLoopData struct {
	task             *Task
	iterationHistory []*Iteration
	scratchpad       []*Iteration
	execCtx          *ExecutionContext
}

// NewBasicLoopData creates a new [BasicLoopData] with the given task.
func NewBasicLoopData(task *Task) *BasicLoopData {
	return &BasicLoopData{
		task:             task,
		iterationHistory: make([]*Iteration, 0),
		scratchpad:       make([]*Iteration, 0),
	}
}

// GetTask returns the original input provided by the user.
func (d *BasicLoopData) GetTask() *Task {
	return d.task
}

// GetIterationHistory returns all Iteration recorded, including compacted ones.
func (d *BasicLoopData) GetIterationHistory() []*Iteration {
	return d.iterationHistory
}

// AddIterationHistory adds a new Iteration to the full history.
// Publishes a CommonDiffEvent if ExecutionContext is set.
func (d *BasicLoopData) AddIterationHistory(iter *Iteration) {
	before := d.iterationHistory
	d.iterationHistory = append(d.iterationHistory, iter)
	if d.execCtx != nil {
		d.execCtx.PublishIterationHistoryChange(before, d.iterationHistory)
	}
}

// GetScratchPad returns all Iteration that will be used in next iteration.
func (d *BasicLoopData) GetScratchPad() []*Iteration {
	return d.scratchpad
}

// SetScratchPad sets the iterations to be used in next iteration.
// Sets the SGScratchpadLength gauge and publishes a CommonDiffEvent
// if ExecutionContext is set.
func (d *BasicLoopData) SetScratchPad(iterations []*Iteration) {
	before := d.scratchpad
	d.scratchpad = iterations
	if d.execCtx != nil {
		d.execCtx.Stats().SetGauge(
			SGScratchpadLength, float64(len(iterations)),
		)
		d.execCtx.PublishScratchPadChange(before, iterations)
	}
}

// SetExecutionContext sets the ExecutionContext for automatic event publishing.
func (d *BasicLoopData) SetExecutionContext(ctx *ExecutionContext) {
	d.execCtx = ctx
}

// Compile-time check that BasicLoopData implements LoopData.
var _ LoopData = (*BasicLoopData)(nil)

// IterationOrigin indicates how an Iteration was created.
// Used for debugging and observability — helps trace whether
// an iteration is original, was synthesized by compaction, or
// was modified in place.
type IterationOrigin string

const (
	// IterationOriginal is the zero value. The iteration was
	// created normally by the AgentLoop during execution.
	IterationOriginal IterationOrigin = ""

	// IterationCompactedSynthetic means this iteration was
	// created by a CompactionStrategy to summarize multiple
	// iterations.
	IterationCompactedSynthetic IterationOrigin = "compacted_synthetic"

	// IterationCompactedModified means this is an original
	// iteration that was modified in place by a
	// CompactionStrategy.
	IterationCompactedModified IterationOrigin = "compacted_modified"

	// IterationRetrievedHistory means this iteration was pulled
	// back from evicted history (e.g., via a
	// retrieval-augmented strategy).
	IterationRetrievedHistory IterationOrigin = "retrieved_history"
)

// IterationMetadataKey is a typed key for Iteration metadata.
// The framework defines standard keys (IMK* constants). Users
// can define their own keys with a custom prefix.
type IterationMetadataKey string

// IMKImportanceScore is a float64 value in [-10, 10] that
// indicates how important this iteration is.
//
// Positive values = more important (prefer to keep).
// Negative values = less important (prefer to discard).
// Zero or absent = neutral (default treatment).
//
// The standard strategies treat any score >=
// [ImportanceScorePinned] (10.0) as "pinned" and preserve
// those iterations through compaction.
//
// Usage:
//
//	// Pin this iteration (will survive compaction):
//	iter.SetMetadata(
//	    gent.IMKImportanceScore,
//	    gent.ImportanceScorePinned,
//	)
//
//	score, ok := gent.GetImportanceScore(iter)
const IMKImportanceScore IterationMetadataKey = "gent:importance_score"

// ImportanceScorePinned is the minimum importance score for
// an iteration to be considered "pinned" by the standard
// compaction strategies. Pinned iterations are always
// preserved through compaction.
//
// The standard strategies use: score >= ImportanceScorePinned.
const ImportanceScorePinned float64 = 10.0

// Iteration represents a single iteration's message content.
//
// Messages must never contain nil elements. All code that
// constructs or modifies an Iteration must ensure every
// element in Messages is a valid, non-nil pointer.
type Iteration struct {
	Messages []*MessageContent

	// Origin indicates how this iteration was created.
	// Zero value (IterationOriginal) means it was created
	// normally by the AgentLoop.
	Origin IterationOrigin

	// Metadata contains optional key-value pairs for this
	// iteration. The framework defines standard keys (IMK*
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
	Metadata map[IterationMetadataKey]any
}

// SetMetadata sets a metadata value, initializing the map
// if nil.
func (i *Iteration) SetMetadata(
	key IterationMetadataKey,
	value any,
) {
	if i.Metadata == nil {
		i.Metadata = make(map[IterationMetadataKey]any)
	}
	i.Metadata[key] = value
}

// GetMetadata returns a metadata value and whether it was
// present. Returns (nil, false) if Metadata is nil or the
// key is absent.
func (i *Iteration) GetMetadata(
	key IterationMetadataKey,
) (any, bool) {
	if i.Metadata == nil {
		return nil, false
	}
	val, ok := i.Metadata[key]
	return val, ok
}

// GetImportanceScore is a convenience function that returns
// the importance score for an iteration.
// Returns (0, false) if the key is absent or not a float64.
func GetImportanceScore(iter *Iteration) (float64, bool) {
	val, ok := iter.GetMetadata(IMKImportanceScore)
	if !ok {
		return 0, false
	}
	score, ok := val.(float64)
	return score, ok
}

// MessageContent is wrapper around [llms.MessageContent] used in AgentLoop.
type MessageContent struct {
	Role  llms.ChatMessageType
	Parts []ContentPart
}

// ContentPart is just a wrapper interface around [llms.ContentPart], just in case we want to add
// new interface method later, it will be easy.
type ContentPart interface {
	llms.ContentPart
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
		parts = append(parts, llms.TextContent{Text: t.Text})
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
}
