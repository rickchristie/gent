package gent

import (
	"context"
	"errors"
	"reflect"

	"github.com/rickchristie/gent/schema"
)

var ErrToolPlanComplete = errors.New("gent: tool plan is complete")

type PlannedToolCall struct {
	ModelCallID  string
	ModelName    string
	RawArguments string
	Id           string
	Name         string
	Args         map[string]any
}

type SavedToolCall struct {
	Id        string
	Name      string
	Args      map[string]any
	Input     EncodedValue
	AttemptId string
}

type ToolPlan struct {
	// CheckpointKey separates an inner program plan from its outer native call.
	CheckpointKey string
	AttemptId     string
	Version       int
	Calls         []PlannedToolCall
	NextCall      int
	ReadyCall     *SavedToolCall
	Results       []*ToolCallRecord
	State         EncodedValue
}

func (ctx *ExecutionContext) NewToolCallId() string { return ctx.nextToolCallId() }

func (ctx *ExecutionContext) SetToolPlan(plan *ToolPlan) error {
	key := plan.CheckpointKey
	if key == "" {
		key = "gent:tool_plan"
	}
	return ctx.SetCheckpointState(key, plan)
}

func (ctx *ExecutionContext) SavedToolPlan() (*ToolPlan, bool, error) {
	value, ok, err := ctx.CheckpointState("gent:tool_plan")
	if err != nil || !ok {
		return nil, ok, err
	}
	plan, ok := value.(*ToolPlan)
	if !ok || plan == nil || plan.Version != 1 || plan.NextCall < 0 || plan.NextCall > len(
		plan.Calls,
	) {
		return nil, false, errors.New("gent: invalid saved tool plan")
	}
	if len(plan.Results) != plan.NextCall {
		return nil, false, errors.New("gent: tool plan results do not match its cursor")
	}
	seen := map[string]bool{}
	for _, call := range plan.Calls {
		if call.Id == "" || call.Name == "" || seen[call.Id] {
			return nil, false, errors.New("gent: invalid planned tool identity")
		}
		seen[call.Id] = true
	}
	if ready := plan.ReadyCall; ready != nil {
		if plan.NextCall >= len(plan.Calls) || ready.Id != plan.Calls[plan.NextCall].Id ||
			ready.Name != plan.Calls[plan.NextCall].Name || ready.AttemptId == "" {
			return nil, false, errors.New("gent: ready tool does not match planned call")
		}
	}
	return plan, true, nil
}

// SelectToolCall selects the default plan for sequential calls. Concurrent calls must use
// BindToolCall and pass their logical ID to PublishBeforeToolCall.
func (ctx *ExecutionContext) SelectToolCall(plan *ToolPlan) {
	ctx.run.mu.Lock()
	defer ctx.run.mu.Unlock()
	ctx.currentToolPlan = plan
}

// BindToolCall associates an explicit call ID with its plan until the returned function runs.
// Each concurrent call must own a separate plan and checkpoint key.
func (ctx *ExecutionContext) BindToolCall(plan *ToolPlan) func() {
	id := plan.Calls[plan.NextCall].Id
	ctx.run.mu.Lock()
	if ctx.boundToolPlans == nil {
		ctx.boundToolPlans = make(map[string]*ToolPlan)
	}
	ctx.boundToolPlans[id] = plan
	ctx.run.mu.Unlock()
	return func() {
		ctx.run.mu.Lock()
		delete(ctx.boundToolPlans, id)
		ctx.run.mu.Unlock()
	}
}

func (ctx *ExecutionContext) toolPlanFor(id string) *ToolPlan {
	ctx.run.mu.RLock()
	defer ctx.run.mu.RUnlock()
	if plan := ctx.boundToolPlans[id]; plan != nil {
		return plan
	}
	plan := ctx.currentToolPlan
	if plan != nil && plan.NextCall < len(plan.Calls) && plan.Calls[plan.NextCall].Id == id {
		return plan
	}
	return nil
}

func (ctx *ExecutionContext) selectedToolCallId() string {
	ctx.run.mu.RLock()
	defer ctx.run.mu.RUnlock()
	plan := ctx.currentToolPlan
	if plan == nil || plan.NextCall >= len(plan.Calls) {
		return ""
	}
	return plan.Calls[plan.NextCall].Id
}

func (ctx *ExecutionContext) saveToolReady(event *BeforeToolCallEvent) error {
	plan := ctx.toolPlanFor(event.ToolCallId)
	if plan == nil {
		return ctx.FlushProgress()
	}
	input, err := ctx.Codecs().Encode(event.Args)
	if err != nil {
		return ctx.stopRecording(err)
	}
	call := plan.Calls[plan.NextCall]
	plan.ReadyCall = &SavedToolCall{Id: call.Id, Name: call.Name, Args: call.Args,
		Input: input, AttemptId: ctx.AttemptId()}
	if err := ctx.SetToolPlan(plan); err != nil {
		return ctx.stopRecording(err)
	}
	return ctx.CommitCheckpoint(PhaseToolReady)
}

func (ctx *ExecutionContext) ToolContext(id string) context.Context {
	return context.WithValue(ctx.Context(), toolIdentityKey{}, ToolIdentity{RunId: ctx.RunId(),
		ContextId: ctx.ContextId(), ToolCallId: id, AttemptId: ctx.AttemptId()})
}

// RecoverTool reuses accepted evidence or reports on an interrupted plan. It never executes
// a pending call. Even a call that never reached dispatch returns control to the model.
func (ctx *ExecutionContext) RecoverTool(plan *ToolPlan, recover ToolRecoveryFunc[EncodedValue],
	expectedSchema *schema.Schema,
) (
	*ToolCallRecord, bool, error,
) {
	if plan.AttemptId == ctx.AttemptId() {
		return nil, false, nil
	}
	call := plan.Calls[plan.NextCall]
	if result, found, err := ctx.SavedToolCallRecord(call.Id); err != nil || found {
		return result, found, err
	}
	attempt := plan.AttemptId
	response := &ToolRecoveryResponse{Status: ToolRecoveryNotStarted}
	var diagnostic error
	var input *EncodedValue
	if ready := plan.ReadyCall; ready != nil {
		attempt, input = ready.AttemptId, &ready.Input
		request := NewToolRecoveryRequest(ToolIdentity{RunId: ctx.RunId(), ContextId: ctx.ContextId(),
			ToolCallId: ready.Id, AttemptId: ready.AttemptId}, ready.Name, ready.Args, ready.Input)
		recoveryContext := context.WithValue(ctx.Context(), recoveryCodecsKey{}, ctx.Codecs())
		response, diagnostic = recover(recoveryContext, request)
	}
	if ctx.Context().Err() != nil {
		return nil, false, ctx.Context().Err()
	}
	returned, err := ctx.SnapshotDiagnostic(response)
	if err != nil {
		return nil, false, err
	}
	if diagnostic == nil {
		diagnostic = ValidateToolRecovery(response)
		if diagnostic == nil && response.Execution != nil {
			accepted := *response
			execution := *response.Execution
			if execution.Status != ToolExecuted {
				execution.Parts = nil
			}
			if execution.Status == ToolInvalidArguments {
				execution.Instructions = expectedSchema.Correction(call.Name, execution.Instructions)
			}
			accepted.Execution = &execution
			response = &accepted
		}
	}
	savedError, err := ctx.SnapshotDiagnostic(diagnostic)
	if err != nil {
		return nil, false, err
	}
	builder := NewToolCallRecordBuilder(call.Id, attempt, call.Name).
		Arguments(call.RawArguments, call.Args).Input(input).ReturnedRecovery(returned).Error(savedError)
	var parts []ContentPart
	if diagnostic != nil {
		builder.RecoveryError()
		parts = []ContentPart{TextPart(ctx.SteeringMessages().RecoveryError)}
	} else {
		builder.Recovery(response)
		parts = PrepareRecoveryParts(response, ctx.SteeringMessages())
	}
	result, err := builder.ModelReply(call.ModelCallID, call.ModelName, parts...).Build()
	if err != nil {
		return nil, false, err
	}
	ctx.PublishAfterToolCall(call.Name, input, result, 0, diagnostic,
		WithToolCallId(call.Id), WithToolCallAttempt(attempt))
	return result, true, ctx.RecordingError()
}

// SavedToolCallRecord reads the last accepted record for a logical tool-call ID. A progress save
// can contain this record before the control checkpoint advances. Check it before recovery so
// the prepared reply, diagnostics, and accounting remain unchanged. Pending events are excluded.
// Reading a record does not publish events or update counters. The returned record is read-only.
func (ctx *ExecutionContext) SavedToolCallRecord(id string) (*ToolCallRecord, bool, error) {
	var saved *RecordedEvent
	ctx.run.mu.RLock()
	for i := len(ctx.run.view.Activity) - 1; i >= 0; i-- {
		event := ctx.run.view.Activity[i]
		if event.ToolCallId == id && event.Type == "tool_call_finished" {
			saved = event
			break
		}
	}
	ctx.run.mu.RUnlock()
	if saved == nil {
		return nil, false, nil
	}
	// Accepted event payloads are immutable. Decode only the selected record, not the whole run.
	value, err := ctx.DecodeEvent(saved)
	if err != nil {
		return nil, false, err
	}
	completed, ok := value.(*AfterToolCallEvent)
	if !ok {
		return nil, false, errors.New("gent: saved tool event has an invalid payload")
	}
	result, ok := completed.Output.(*ToolCallRecord)
	if !ok || result == nil {
		return nil, false, errors.New("gent: saved tool event has no call record")
	}
	return result, true, nil
}

// SnapshotDiagnostic captures optional private evidence. Encoded snapshots are retained without
// decoding their old Go types. Ordinary errors use the registered error codec.
func (ctx *ExecutionContext) SnapshotDiagnostic(value any) (*EncodedValue, error) {
	if value == nil {
		return nil, nil
	}
	v := reflect.ValueOf(value)
	if v.Kind() == reflect.Pointer && v.IsNil() {
		return nil, nil
	}
	encoded, err := ctx.Codecs().Encode(value)
	if err != nil {
		return nil, err
	}
	if encoded.Type == "nil" {
		return nil, nil
	}
	return &encoded, nil
}
