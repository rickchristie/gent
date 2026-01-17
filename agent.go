package gent

import (
	"context"

	"github.com/tmc/langchaingo/llms"
)

// AgentLoop is responsible for:
//  1. Constructing the prompt to be sent to the LLM model.
//  2. Calling the LLM model with the constructed prompt.
//  3. Freely use [ToolChain], [Termination], [Compaction], and your own custom logic to determine
//     to continue the loop or to terminate with results.
//
// The executor will call [AgentLoop.Iterate] repeatedly until it returns [LATerminate] result.
type AgentLoop[Data LoopData] interface {
	Iterate(ctx context.Context, data LoopData) *AgentLoopResult
}

// LoopData is the data that is being passed through each AgentLoop execution. Each AgentLoop
// implementation may define their own Data interface.
//
// The interface methods within this interface allows custom AgentLoop implementations to work with
// provided hooks, for logging, metrics, etc.
type LoopData interface {
	// GetOriginalInput returns the original input provided by the user that started the agent loop.
	GetOriginalInput() []ContentPart
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
	Result string
}
