package gent

// RecoveryInfo is an owned snapshot of an interrupted operation. Input is the effective input
// after before-call hooks. A ready call may have completed even when no result was saved.
type RecoveryInfo struct {
	RunId      string
	ContextId  string
	ToolCallId string
	AttemptId  string
	Name       string
	Args       map[string]any
	Input      any
	Script     string
	Completed  []*ToolCallResult
	Uncertain  []*SavedToolCall
}

// RecoveryMessageProvider adds application guidance to the framework's recovery facts.
// The callback receives a non-nil snapshot. It must not change external state.
type RecoveryMessageProvider func(*RecoveryInfo) string

// ToolOptions contains optional behavior for a registered tool.
type ToolOptions struct {
	RecoveryMessage RecoveryMessageProvider
}

type ToolOption func(*ToolOptions)

func WithRecoveryMessage(provider RecoveryMessageProvider) ToolOption {
	return func(options *ToolOptions) { options.RecoveryMessage = provider }
}

const DefaultRecoveryMessage = `The operation was interrupted. Its outcome is unknown.
It may have completed. Check the current state before you decide whether to repeat it.`
