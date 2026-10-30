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
	Value       *EncodedValue     `json:"value,omitempty"`
	Refusal     *EncodedValue     `json:"refusal,omitempty"`
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

// ToolRecoveryResult is a tool's decision about an interrupted operation.
type ToolRecoveryResult string

const (
	ToolRecoveryCompleted    ToolRecoveryResult = "completed"
	ToolRecoveryRetryAllowed ToolRecoveryResult = "retry_allowed"
	ToolRecoveryUnresolved   ToolRecoveryResult = "unresolved"
)

// ToolRecoveryRequest contains the original call identity and saved effective input.
// AttemptId identifies the interrupted attempt; ToolCallId is stable across attempts.
type ToolRecoveryRequest struct {
	RunId      string
	ContextId  string
	ToolCallId string
	AttemptId  string
	Name       string
	Args       map[string]any
	Input      EncodedValue
}

// ToolRecoveryResponse keeps the decision and its explanation together. Output is required
// only for Completed and must be nil otherwise. Gent supplies the saved call identity and input.
// SteeringMessage is saved with the decision and added to the resulting tool observation.
// For RetryAllowed it accompanies the retry's result. It never authorizes a retry on its own.
type ToolRecoveryResponse struct {
	Result          ToolRecoveryResult
	SteeringMessage string
	Output          *ToolCallResult
}

// ToolRecoveryFunc is the handler accepted by ToolBuilder.Recovery.
// Handlers must be safe to repeat if a process stops before the decision is saved.
type ToolRecoveryFunc func(context.Context, *ToolRecoveryRequest) (*ToolRecoveryResponse, error)

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
