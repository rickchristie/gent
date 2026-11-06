package gent

import (
	"errors"
	"fmt"
)

// ToolResponseKind selects the accepted execution, recovery, or safe error path. It is metadata
// for Gent and provider adapters. Gent does not print the kind as model-facing content.
type ToolResponseKind string

const (
	// ToolResponseExecution has an accepted execution response, including pre-call rejection.
	ToolResponseExecution ToolResponseKind = "execution"
	// ToolResponseRecovery has an accepted report about an interrupted call.
	ToolResponseRecovery ToolResponseKind = "recovery"
	// ToolResponseCallError has safe guidance for an operation with no accepted response.
	ToolResponseCallError ToolResponseKind = "call_error"
	// ToolResponseRecoveryError has safe guidance for a failed check of the original operation.
	ToolResponseRecoveryError ToolResponseKind = "recovery_error"
)

// ModelToolCallResponse is the prepared reply to one model-requested tool call. A record builder
// creates it together with the accepted outcome. Only safe content belongs in its parts. The
// provider adapter encodes content and call IDs without exposing framework metadata as JSON.
// Getters return read-only views. Retain the response while using a view.
type ModelToolCallResponse struct{ data modelToolResponseData }

type modelToolResponseData struct {
	ToolCallID      string
	Name            string
	Kind            ToolResponseKind
	ExecutionStatus ToolStatus
	RecoveryStatus  ToolRecoveryStatus
	Parts           []ContentPart
}

func (*ModelToolCallResponse) isContentPart() {}

// ToolCallID is the exact provider call ID, or the inner PTC correlation ID for an inner call.
func (r *ModelToolCallResponse) ToolCallID() string { return r.data.ToolCallID }

// Name is the model-requested function name, which can differ from a shared execution target.
func (r *ModelToolCallResponse) Name() string { return r.data.Name }

// Kind identifies the accepted response path. It does not classify business success.
func (r *ModelToolCallResponse) Kind() ToolResponseKind { return r.data.Kind }

// ExecutionStatus is present for execution and Completed recovery. Absent returns ("", false).
func (r *ModelToolCallResponse) ExecutionStatus() (ToolStatus, bool) {
	return r.data.ExecutionStatus, r.data.ExecutionStatus != ""
}

// RecoveryStatus is present only for an accepted recovery report. Absent returns ("", false).
func (r *ModelToolCallResponse) RecoveryStatus() (ToolRecoveryStatus, bool) {
	return r.data.RecoveryStatus, r.data.RecoveryStatus != ""
}

// Parts is ordered safe content with instructions already included once. It is read-only.
func (r *ModelToolCallResponse) Parts() []ContentPart { return r.data.Parts }

// WithParts creates a request-specific reply with unchanged identity and metadata. On success,
// it owns parts. It rejects nested calls and responses without changing r. This supports request
// hook edits while retaining saved history. Share unchanged read-only content; copy only values
// that must be changed independently.
func (r *ModelToolCallResponse) WithParts(parts ...ContentPart) (*ModelToolCallResponse, error) {
	if err := ValidateContentParts(parts); err != nil {
		return nil, err
	}
	data := r.data
	data.Parts = parts
	return &ModelToolCallResponse{data: data}, nil
}

// ToolCallRecord owns the input, accepted outcome, original diagnostics, and prepared model reply.
// Only Response belongs in model input. Completed records remain readable without old tool types.
// Returned pointers, maps, and slices are read-only and stay valid while the record is retained.
type ToolCallRecord struct{ data toolCallRecordData }

type toolCallRecordData struct {
	ToolCallID       string
	AttemptID        string
	Name             string
	Kind             ToolResponseKind
	Execution        *ToolExecutionResponse
	Recovery         *ToolRecoveryResponse
	Response         *ModelToolCallResponse
	DiscoveredTools  []string
	RawArguments     string
	Args             map[string]any
	Input            *EncodedValue
	ReturnedResult   *EncodedValue
	ReturnedRecovery *EncodedValue
	Error            *EncodedValue
}

// ToolCallID identifies Gent's logical execution, independently of the provider call ID.
func (r *ToolCallRecord) ToolCallID() string { return r.data.ToolCallID }

// AttemptID identifies the worker attempt whose outcome is recorded.
func (r *ToolCallRecord) AttemptID() string { return r.data.AttemptID }

// Name identifies the selected target, including a target called through shared execution.
func (r *ToolCallRecord) Name() string { return r.data.Name }

// Kind selects the accepted outcome. Diagnostic Error alone does not determine it.
func (r *ToolCallRecord) Kind() ToolResponseKind { return r.data.Kind }

// Execution is present only for ToolResponseExecution and is read-only.
func (r *ToolCallRecord) Execution() *ToolExecutionResponse { return r.data.Execution }

// Recovery is present only for ToolResponseRecovery and is read-only.
func (r *ToolCallRecord) Recovery() *ToolRecoveryResponse { return r.data.Recovery }

// Response is always present, including for error outcomes. It contains no private diagnostics.
func (r *ToolCallRecord) Response() *ModelToolCallResponse { return r.data.Response }

// DiscoveredTools returns the tool names described by this call, without registering bindings.
func (r *ToolCallRecord) DiscoveredTools() []string { return r.data.DiscoveredTools }

// RawArguments retains the original argument JSON, including malformed JSON.
func (r *ToolCallRecord) RawArguments() string { return r.data.RawArguments }

// Args returns parsed original arguments, or nil when parsing failed.
func (r *ToolCallRecord) Args() map[string]any { return r.data.Args }

// Input is the saved effective input after hooks, or nil before successful typed conversion.
func (r *ToolCallRecord) Input() *EncodedValue { return r.data.Input }

// ReturnedResult preserves the original returned response, including a rejected response.
func (r *ToolCallRecord) ReturnedResult() *EncodedValue { return r.data.ReturnedResult }

// ReturnedRecovery preserves the original recovery report, including a rejected report.
func (r *ToolCallRecord) ReturnedRecovery() *EncodedValue { return r.data.ReturnedRecovery }

// Error is private diagnostic evidence. Nil means no diagnostic error was recorded.
func (r *ToolCallRecord) Error() *EncodedValue { return r.data.Error }

// ModelMessage returns a tool-role message referring to the saved, read-only prepared response.
func (r *ToolCallRecord) ModelMessage() *ModelMessage {
	return &ModelMessage{Role: ChatMessageTypeTool, Parts: []ContentPart{r.data.Response}}
}

// ToolCallRecordBuilder joins one accepted outcome, private snapshots, and safe prepared parts.
// It does not call tools, select steering, encode provider messages, save state, or update stats.
// Setters take ownership of retained values. Use a builder for one record in one goroutine.
type ToolCallRecordBuilder struct {
	data       toolCallRecordData
	selections int
}

// NewToolCallRecordBuilder sets Gent's identity and target. ModelReply supplies provider identity.
func NewToolCallRecordBuilder(callID, attemptID, target string) *ToolCallRecordBuilder {
	return &ToolCallRecordBuilder{data: toolCallRecordData{
		ToolCallID: callID, AttemptID: attemptID, Name: target,
	}}
}

// Arguments retains supplied JSON and its parsed value. Parsing failure permits nil parsed data.
func (b *ToolCallRecordBuilder) Arguments(raw string,
	parsed map[string]any,
) *ToolCallRecordBuilder {
	b.data.RawArguments, b.data.Args = raw, parsed
	return b
}

// Input retains optional encoded effective input without decoding its original Go type.
func (b *ToolCallRecordBuilder) Input(value *EncodedValue) *ToolCallRecordBuilder {
	b.data.Input = value
	return b
}

// DiscoveredTools retains names described in the reply, without changing the runtime catalog.
func (b *ToolCallRecordBuilder) DiscoveredTools(names ...string) *ToolCallRecordBuilder {
	b.data.DiscoveredTools = names
	return b
}

// Execution selects an accepted execution response. Select exactly one outcome per builder.
func (b *ToolCallRecordBuilder) Execution(value *ToolExecutionResponse) *ToolCallRecordBuilder {
	b.selections++
	b.data.Kind, b.data.Execution = ToolResponseExecution, value
	return b
}

// Recovery selects an accepted recovery report. Build checks its status and Execution pairing.
func (b *ToolCallRecordBuilder) Recovery(value *ToolRecoveryResponse) *ToolCallRecordBuilder {
	b.selections++
	b.data.Kind, b.data.Recovery = ToolResponseRecovery, value
	return b
}

// CallError selects safe error guidance without fabricating an execution response.
func (b *ToolCallRecordBuilder) CallError() *ToolCallRecordBuilder {
	b.selections++
	b.data.Kind = ToolResponseCallError
	return b
}

// RecoveryError selects a failed recovery check, without claiming the original action failed.
func (b *ToolCallRecordBuilder) RecoveryError() *ToolCallRecordBuilder {
	b.selections++
	b.data.Kind = ToolResponseRecoveryError
	return b
}

// ModelReply supplies already prepared safe content. Build derives metadata from the outcome.
// Do not pass raw errors or rejected values. Required guidance must already be included once.
func (b *ToolCallRecordBuilder) ModelReply(id, name string,
	parts ...ContentPart,
) *ToolCallRecordBuilder {
	b.data.Response = &ModelToolCallResponse{data: modelToolResponseData{
		ToolCallID: id, Name: name, Parts: parts,
	}}
	return b
}

// ReturnedResult retains optional original-response diagnostics outside model content.
func (b *ToolCallRecordBuilder) ReturnedResult(value *EncodedValue) *ToolCallRecordBuilder {
	b.data.ReturnedResult = value
	return b
}

// ReturnedRecovery retains optional original-recovery diagnostics outside model content.
func (b *ToolCallRecordBuilder) ReturnedRecovery(value *EncodedValue) *ToolCallRecordBuilder {
	b.data.ReturnedRecovery = value
	return b
}

// Error retains optional error evidence. It does not change the selected outcome or reply.
func (b *ToolCallRecordBuilder) Error(value *EncodedValue) *ToolCallRecordBuilder {
	b.data.Error = value
	return b
}

// Build checks identity, outcome pairing, and allowed content, then derives response metadata.
// It returns no record on failure. It does not validate parts against a domain OutputSchema.
func (b *ToolCallRecordBuilder) Build() (*ToolCallRecord, error) {
	if b.selections != 1 {
		return nil, errors.New("gent: select exactly one tool outcome")
	}
	if err := validateRecordOutcome(&b.data); err != nil {
		return nil, err
	}
	response := b.data.Response
	if response == nil || response.ToolCallID() == "" || response.Name() == "" {
		return nil, errors.New("gent: model reply identity is required")
	}
	if err := ValidateContentParts(response.Parts()); err != nil {
		return nil, err
	}
	response.data.Kind, response.data.ExecutionStatus, response.data.RecoveryStatus =
		recordStatuses(&b.data)
	return &ToolCallRecord{data: b.data}, nil
}

func validateRecordOutcome(data *toolCallRecordData) error {
	if data.ToolCallID == "" || data.AttemptID == "" || data.Name == "" {
		return errors.New("gent: tool record identity is required")
	}
	switch data.Kind {
	case ToolResponseExecution:
		if data.Recovery != nil {
			return errors.New("gent: execution cannot contain recovery")
		}
		return ValidateToolExecution(data.Execution)
	case ToolResponseRecovery:
		if data.Execution != nil {
			return errors.New("gent: recovery execution must be nested")
		}
		return ValidateToolRecovery(data.Recovery)
	case ToolResponseCallError, ToolResponseRecoveryError:
		if data.Execution != nil || data.Recovery != nil {
			return errors.New("gent: error outcome cannot contain an accepted response")
		}
		return nil
	default:
		return fmt.Errorf("gent: invalid tool response kind %q", data.Kind)
	}
}

func recordStatuses(data *toolCallRecordData) (ToolResponseKind, ToolStatus, ToolRecoveryStatus) {
	if data.Execution != nil {
		return data.Kind, data.Execution.Status, ""
	}
	if data.Recovery == nil {
		return data.Kind, "", ""
	}
	if data.Recovery.Execution == nil {
		return data.Kind, "", data.Recovery.Status
	}
	return data.Kind, data.Recovery.Execution.Status, data.Recovery.Status
}
