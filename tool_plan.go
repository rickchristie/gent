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
}

type ToolPlan struct {
	Version   int
	Calls     []PlannedToolCall
	NextCall  int
	ReadyCall *SavedToolCall
	Results   []*ToolCallResult
	State     EncodedValue
}

type RecoverableToolChain interface {
	ToolChain
	Prepare(*ExecutionContext, string) (*ToolPlan, error)
	ExecuteNext(*ExecutionContext, *ToolPlan, TextFormat) (*ToolCallResult, error)
}

func (ctx *ExecutionContext) NewToolCallId() string { return ctx.nextToolCallId() }

func (ctx *ExecutionContext) SetToolPlan(plan *ToolPlan) error {
	return ctx.SetCheckpointState("gent:tool_plan", plan)
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

// SelectToolCall associates an invocation with its durable plan. Built-in chains serialize calls
// within one execution context; child executions carry their own plans.
func (ctx *ExecutionContext) SelectToolCall(plan *ToolPlan) {
	ctx.run.mu.Lock()
	defer ctx.run.mu.Unlock()
	ctx.currentToolPlan = plan
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
	if _, ok := event.(*BeforeToolCallEvent); !ok {
		return false
	}
	ctx.run.mu.RLock()
	defer ctx.run.mu.RUnlock()
	plan := ctx.currentToolPlan
	return plan != nil && plan.ReadyCall != nil && plan.ReadyCall.AttemptId != ctx.run.attempt
}

func (ctx *ExecutionContext) retryToolInput() (any, error) {
	ctx.run.mu.RLock()
	value := cloneWire(ctx.currentToolPlan.ReadyCall.Input)
	ctx.run.mu.RUnlock()
	return ctx.Codecs().Decode(value)
}

func (ctx *ExecutionContext) saveToolReady(event *BeforeToolCallEvent) error {
	ctx.run.mu.RLock()
	plan := ctx.currentToolPlan
	ctx.run.mu.RUnlock()
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

// RecoverTool refuses to guess whether an external effect occurred. A completed saved result
// takes precedence over the recovery adapter; otherwise the adapter explicitly decides.
func (ctx *ExecutionContext) RecoverTool(plan *ToolPlan) (*ToolCallResult, bool, error) {
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
	if ctx.run.toolRecovery == nil {
		return nil, false, fmt.Errorf("%w: %s (%s)", ErrUncertainTool, ready.Name, ready.Id)
	}
	ownedArgs, err := ctx.Codecs().Clone(ready.Args)
	if err != nil {
		return nil, false, err
	}
	result, err := ctx.run.toolRecovery.Recover(ctx.Context(), ToolRecoveryRequest{
		RunId: ctx.RunId(), ContextId: ctx.ContextId(), ToolCallId: ready.Id,
		AttemptId: ready.AttemptId, Name: ready.Name, Args: ownedArgs.(map[string]any),
		Input: cloneWire(ready.Input)})
	if err != nil {
		return nil, false, err
	}
	switch result.Status {
	case ToolRecoveryCompleted:
		if result.Result == nil {
			return nil, false, errors.New("gent: completed tool recovery has no result")
		}
		owned, err := ctx.Codecs().Clone(result.Result)
		if err != nil {
			return nil, false, err
		}
		result.Result = owned.(*ToolCallResult)
		input, err := ctx.Codecs().Decode(ready.Input)
		if err != nil {
			return nil, false, err
		}
		result.Result.ToolCallId = ready.Id
		result.Result.AttemptId = ready.AttemptId
		ctx.PublishAfterToolCall(ready.Name, input, result.Result, 0, result.Result.Error,
			WithToolCallId(ready.Id), WithToolCallAttempt(ready.AttemptId))
		return result.Result, true, nil
	case ToolRecoveryRetryAllowed:
		return nil, false, nil
	default:
		return nil, false, fmt.Errorf("%w: %s (%s)", ErrUncertainTool, ready.Name, ready.Id)
	}
}
