package gent

import "github.com/tmc/langchaingo/llms"

// NativeToolChain adds native model calls without changing the text-based ToolChain contract.
// Both paths use the same tool plans, validation, events, and saved results.
type NativeToolChain interface {
	ToolChain

	// NativeTools returns the tools available to this execution. Search chains keep discovered
	// tools in the execution context so concurrent runs do not share their model context.
	NativeTools(*ExecutionContext) ([]llms.Tool, error)

	// PrepareCalls decodes native arguments and builds a plan. It does not execute tools.
	// The plan uses Gent call IDs for recovery. Model call IDs stay in the message history.
	PrepareCalls(*ExecutionContext, []llms.ToolCall) (*ToolPlan, error)

	ExecuteNext(*ExecutionContext, *ToolPlan, TextFormat) (*ToolCallResult, error)
}
