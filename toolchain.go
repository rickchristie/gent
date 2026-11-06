package gent

import (
	"context"

	"github.com/rickchristie/gent/schema"
)

// ToolChain selects and executes native tool calls. Implementations can embed toolchain.Registry
// and override NativeTools to control visibility while retaining validation, hooks, and recovery.
// RegisterCodecs and RecoveryConfig describe saved state and binding compatibility.
type ToolChain interface {
	RegisterTool(any) ToolChain
	NativeTools(*ExecutionContext) ([]*ToolDefinition, error)
	PrepareCalls(*ExecutionContext, []*ModelToolCallRequest) (*ToolPlan, error)
	ExecuteNext(*ExecutionContext, *ToolPlan, TextFormat) (*ToolCallRecord, error)
	GetToolSchema(string) *schema.Schema
	Recover(context.Context, *ToolRecoveryRequest[EncodedValue]) (*ToolRecoveryResponse, error)
	RegisterCodecs(*CodecRegistry) error
	RecoveryConfig() any
}

// ParallelToolChain declares whether ExecuteNext can run concurrently with independent plans
// in the same context. Each plan must have a distinct checkpoint key. The JavaScript wrapper
// always calls ExecuteNext on the supplied chain, including overrides on an embedding type.
type ParallelToolChain interface {
	ToolChain
	SupportsParallelCalls() bool
}
