package gent

import (
	"context"

	"github.com/tmc/langchaingo/llms"
)

// AgentLoop is responsible for:
//  1. Constructing the entire prompt to be sent to the LLM model.
//  2. Calling the LLM model with the constructed prompt.
//  3. Parsing LLM output and processes it (e.g., tool calls, termination, saving data, etc).
//  4. Decide whether to continue the loop or terminate with results.
//
// Implementations may reuse [ToolChain], [Termination], [TextOutputFormat] or create their own
// custom logic to handle all of the above.
//
// The executor will call [AgentLoop.Next] repeatedly until it returns [LATerminate] result.
type AgentLoop[Data LoopData] interface {
	// Next performs one iteration of the agent loop.
	// The ExecutionContext provides access to LoopData via execCtx.Data() and enables automatic
	// tracing. All framework components (Model, ToolChain) will trace automatically when given
	// the ExecutionContext.
	Next(ctx context.Context, execCtx *ExecutionContext) *AgentLoopResult
}

// LoopData is the data that is being passed through each AgentLoop execution. Each AgentLoop
// implementation may define their own Data interface.
//
// The interface methods within this interface allows custom AgentLoop implementations to work with
// provided hooks, for logging, metrics, etc.
type LoopData interface {
	// GetOriginalInput returns the original input provided by the user that started the agent loop.
	GetOriginalInput() []ContentPart

	// GetIterationHistory returns all [Iteration] recorded, including those that may be
	// compacted away from GetIterations.
	GetIterationHistory() []*Iteration

	// AddIterationHistory adds a new [Iteration] to the full history only.
	AddIterationHistory(iter *Iteration)

	// GetIterations returns all [Iteration] recorded, that will be used in next iteration.
	// When compaction happens, some earlier iterations may be removed from this slice, but they
	// are preserved in GetIterationHistory.
	GetIterations() []*Iteration

	// SetIterations sets the iterations to be used in next iteration.
	// [AgentLoop] implementations are free to call [GetIterations] and modify it how they want,
	// then call this method to set the modified iterations to be used in the next iteration.
	SetIterations([]*Iteration)
}

// Iteration represents a single iteration's message content.
type Iteration struct {
	Messages []MessageContent
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
