package gent

import (
	"context"
	"errors"
	"fmt"
)

var ErrToolPlanComplete = errors.New("gent: tool plan is complete")

type PlannedToolCall struct {
	Id   string
	Name string
	Args map[string]any
}

type SavedToolCall struct {
	Id        string
	Name      string
	Args      map[string]any
	Input     EncodedValue
	AttemptId string
	// Recovery is saved before its decision is used. A new attempt gets a new ReadyCall.
	Recovery *ToolRecoveryResponse
}

type ToolPlan struct {
	// CheckpointKey separates an inner program plan from its outer native call.
	CheckpointKey string
	Abandoned     bool
	Version       int
	Calls         []PlannedToolCall
	NextCall      int
	ReadyCall     *SavedToolCall
	Results       []*ToolCallResult
	State         EncodedValue
	// RecoverySteering survives an authorized retry until its result is saved.
	RecoverySteering string
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

func (ctx *ExecutionContext) isRetryingTool(event Event) bool {
	before, ok := event.(*BeforeToolCallEvent)
	if !ok {
		return false
	}
	plan := ctx.toolPlanFor(before.ToolCallId)
	return plan != nil && plan.ReadyCall != nil && plan.ReadyCall.AttemptId != ctx.AttemptId()
}

func (ctx *ExecutionContext) retryToolInput(id string) (any, error) {
	value := cloneWire(ctx.toolPlanFor(id).ReadyCall.Input)
	return ctx.Codecs().Decode(value)
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

// RecoverTool first reuses accepted evidence, then calls the registered tool's recovery method.
// The decision is saved before another attempt or result publication can start.
func (ctx *ExecutionContext) RecoverTool(plan *ToolPlan, recover ToolRecoveryFunc) (
	*ToolCallResult, bool, error,
) {
	if plan.ReadyCall == nil || plan.ReadyCall.AttemptId == ctx.AttemptId() {
		return nil, false, nil
	}
	ready := plan.ReadyCall
	bundle, err := ctx.Export()
	if err != nil {
		return nil, false, err
	}
	for i := len(bundle.Activity) - 1; i >= 0; i-- {
		event := bundle.Activity[i]
		if event.ToolCallId != ready.Id || event.Type != "tool_call_finished" {
			continue
		}
		value, err := ctx.DecodeEvent(event)
		if err != nil {
			return nil, false, err
		}
		if completed, ok := value.(*AfterToolCallEvent); ok {
			if result, ok := completed.Output.(*ToolCallResult); ok {
				return result, true, nil
			}
		}
	}
	if ready.Recovery == nil {
		if err := ctx.saveToolRecovery(plan, recover); err != nil {
			return nil, false, err
		}
	} else if err := validateToolRecovery(ready.Recovery); err != nil {
		return nil, false, err
	}
	response := ready.Recovery
	if response.Result == ToolRecoveryRetryAllowed {
		return nil, false, nil
	}
	input, err := ctx.Codecs().Decode(ready.Input)
	if err != nil {
		return nil, false, err
	}
	result := response.Output
	if response.Result == ToolRecoveryUnresolved {
		result = &ToolCallResult{Outcome: "unknown", Error: ErrUncertainTool,
			Text: fmt.Sprintf("Tool %s (call %s):\n%s",
				ready.Name, ready.Id, defaultRecoveryMessage),
			SummaryCaptured: true}
	} else {
		owned, err := ctx.Codecs().Clone(result)
		if err != nil {
			return nil, false, err
		}
		result = owned.(*ToolCallResult)
	}
	result.ToolCallId, result.AttemptId = ready.Id, ready.AttemptId
	args, err := ctx.Codecs().Clone(ready.Args)
	if err != nil {
		return nil, false, err
	}
	result.Name, result.Args, result.Input = ready.Name, args.(map[string]any), input
	result.SummaryCaptured = true
	ctx.PublishAfterToolCall(ready.Name, input, result, 0, result.Error,
		WithToolCallId(ready.Id), WithToolCallAttempt(ready.AttemptId))
	return result, true, ctx.RecordingError()
}

func (ctx *ExecutionContext) saveToolRecovery(plan *ToolPlan, recover ToolRecoveryFunc) error {
	ready := plan.ReadyCall
	args, err := ctx.Codecs().Clone(ready.Args)
	if err != nil {
		return err
	}
	response, err := recover(ctx.Context(), &ToolRecoveryRequest{
		RunId: ctx.RunId(), ContextId: ctx.ContextId(), ToolCallId: ready.Id,
		AttemptId: ready.AttemptId, Name: ready.Name, Args: args.(map[string]any),
		Input: cloneWire(ready.Input)})
	if err != nil {
		return err
	}
	if err := validateToolRecovery(response); err != nil {
		return err
	}
	owned, err := ctx.Codecs().Clone(response)
	if err != nil {
		return err
	}
	ready.Recovery = owned.(*ToolRecoveryResponse)
	plan.RecoverySteering = joinSteering(plan.RecoverySteering, ready.Recovery.SteeringMessage)
	if err := ctx.SetToolPlan(plan); err != nil {
		return ctx.stopRecording(err)
	}
	return ctx.CommitCheckpoint(PhaseToolReady)
}
