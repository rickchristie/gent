package gent

import (
	"context"
	"errors"
	"time"
)

type ExecutionPhase string

const (
	PhaseInitial             ExecutionPhase = "initial"
	PhaseIterationReady      ExecutionPhase = "iteration_ready"
	PhaseDecisionReady       ExecutionPhase = "decision_ready"
	PhaseToolReady           ExecutionPhase = "tool_ready"
	PhaseToolCompleted       ExecutionPhase = "tool_completed"
	PhaseIterationCompleted  ExecutionPhase = "iteration_completed"
	PhaseCompactionCompleted ExecutionPhase = "compaction_completed"
	PhaseTerminal            ExecutionPhase = "terminal"
)

// CompatibilityManifest identifies code and configuration rebound by a replacement worker.
// Functions, credentials, provider clients, and live contexts are deliberately absent.
type CompatibilityManifest struct {
	Version    int               `json:"version"`
	Components map[string]string `json:"components"`
}

type Checkpoint struct {
	SchemaVersion int                   `json:"schemaVersion"`
	RunId         string                `json:"runId"`
	Revision      HistoryRevision       `json:"revision"`
	EventNumber   uint64                `json:"eventNumber"`
	Phase         ExecutionPhase        `json:"phase"`
	Contexts      []ContextCheckpoint   `json:"contexts"`
	Compatibility CompatibilityManifest `json:"compatibility"`
	Outcome       *SavedOutcome         `json:"outcome,omitempty"`
}

type ContextCheckpoint struct {
	ContextId          string                          `json:"contextId"`
	ParentContextId    string                          `json:"parentContextId,omitempty"`
	Name               string                          `json:"name"`
	Task               EncodedValue                    `json:"task"`
	Active             []EntryId                       `json:"active"`
	Iteration          int                             `json:"iteration"`
	CompletedIteration int                             `json:"completedIteration"`
	AttemptId          string                          `json:"attemptId"`
	Runtime            ContextRuntime                  `json:"runtime"`
	Limits             []Limit                         `json:"limits"`
	StartedAt          time.Time                       `json:"startedAt"`
	Deadline           *time.Time                      `json:"deadline,omitempty"`
	State              map[string]EncodedValue         `json:"state"`
	Completed          bool                            `json:"completed"`
	Extensions         map[string]SavedCheckpointState `json:"extensions,omitempty"`
}

type SavedCheckpointState struct {
	Type    string       `json:"type"`
	Version int          `json:"version"`
	Value   EncodedValue `json:"value"`
}

type SavedOutcome struct {
	Reason      TerminationReason `json:"reason"`
	Result      EncodedValue      `json:"result"`
	Error       *SavedError       `json:"error,omitempty"`
	CompletedAt time.Time         `json:"completedAt"`
}

type CheckpointState interface {
	CheckpointType() string
	CheckpointVersion() int
	SaveCheckpointState() (EncodedValue, error)
	RestoreCheckpointState(EncodedValue) error
}

type RestoreBindings struct {
	Recorder           Recorder
	Observers          []RecordedEventObserver
	Codecs             *CodecRegistry
	Data               LoopData
	Compatibility      CompatibilityManifest
	ToolRecovery       ToolRecovery
	Components         map[string]CheckpointState
	CompactionStrategy CompactionStrategy
	CompactionTrigger  CompactionTrigger
	// Configure prepares the actual restored context before compatibility/state validation.
	// Executor.Resume supplies its normal configuration here; no temporary context is needed.
	Configure func(*ExecutionContext) error
}

var (
	ErrIncompatibleCheckpoint = errors.New("gent: incompatible checkpoint")
	ErrUnsupportedRecovery    = errors.New("gent: unsupported recovery phase")
	ErrUncertainTool          = errors.New("gent: tool outcome is uncertain")
)

type ToolRecoveryStatus string

const (
	ToolRecoveryCompleted    ToolRecoveryStatus = "completed"
	ToolRecoveryRetryAllowed ToolRecoveryStatus = "retry_allowed"
	ToolRecoveryUnresolved   ToolRecoveryStatus = "unresolved"
)

type ToolRecoveryRequest struct {
	RunId      string
	ContextId  string
	ToolCallId string
	AttemptId  string
	Name       string
	Args       map[string]any
	Input      EncodedValue
}

type ToolRecoveryResult struct {
	Status ToolRecoveryStatus
	Result *ToolCallResult
}

type ToolRecovery interface {
	Recover(context.Context, ToolRecoveryRequest) (ToolRecoveryResult, error)
}

type ToolRecoveryFunc func(context.Context, ToolRecoveryRequest) (ToolRecoveryResult, error)

func (f ToolRecoveryFunc) Recover(ctx context.Context, request ToolRecoveryRequest) (
	ToolRecoveryResult, error,
) {
	return f(ctx, request)
}

type toolIdentityKey struct{}

// ToolIdentity lets a tool pass the stable logical ID to an external idempotency mechanism.
// AttemptId changes after takeover; ToolCallId does not.
type ToolIdentity struct {
	RunId      string
	ContextId  string
	ToolCallId string
	AttemptId  string
}

func ToolIdentityFromContext(ctx context.Context) (ToolIdentity, bool) {
	identity, ok := ctx.Value(toolIdentityKey{}).(ToolIdentity)
	return identity, ok
}
