package toolchain

import (
	"errors"
	"fmt"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/toolchain/jsruntime"
)

// A program is a sequence of native batches. Save each batch before it can start an effect.
// The outer plan remains under gent:tool_plan; inner checkpoints must never replace it.
type programState struct {
	AttemptId string
	CallId    string
	Script    string
	Completed []*gent.ToolCallRecord
}

// Save keys instead of shared plan pointers. Each worker can then save its own cursor and
// ready input without reading a plan that another worker is changing.
type parallelProgramCalls struct {
	Keys []string
}

func loadProgram(ctx *gent.ExecutionContext) (*programState, bool, error) {
	value, found, err := ctx.CheckpointState("gent:program")
	if err != nil || !found {
		return nil, found, err
	}
	state, ok := value.(*programState)
	if !ok {
		return nil, false, errors.New("invalid JavaScript recovery state")
	}
	return state, true, nil
}

func (w *JsToolChainWrapper) executeProgramCalls(ctx *gent.ExecutionContext,
	calls []*gent.ModelToolCallRequest, format gent.TextFormat,
) ([]*gent.ToolCallRecord, error) {
	if err := ctx.RecordingError(); err != nil {
		return nil, err
	}
	if err := ctx.Context().Err(); err != nil {
		return nil, err
	}
	program, found, err := loadProgram(ctx)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errors.New("JavaScript call has no active program")
	}
	if program.AttemptId != ctx.AttemptId() {
		return nil, errors.New("JavaScript attempt changed")
	}
	if len(calls) > 1 {
		return w.executeParallelProgramCalls(ctx, program, calls, format)
	}
	plan, err := w.wrapped.PrepareCalls(ctx, calls)
	if err != nil {
		return nil, err
	}
	plan.CheckpointKey = "gent:program_calls"
	if err := ctx.SetToolPlan(plan); err != nil {
		return nil, err
	}
	if err := ctx.CommitCheckpoint(gent.PhaseDecisionReady); err != nil {
		return nil, err
	}
	for plan.NextCall < len(plan.Calls) {
		if _, err := w.wrapped.ExecuteNext(ctx, plan, format); err != nil {
			return nil, err
		}
	}
	program.Completed = append(program.Completed, plan.Results...)
	if err := ctx.SetCheckpointState("gent:program", program); err != nil {
		return nil, err
	}
	ctx.ClearCheckpointState("gent:program_calls")
	if err := ctx.CommitCheckpoint(gent.PhaseToolCompleted); err != nil {
		return nil, err
	}
	return plan.Results, nil
}

func (w *JsToolChainWrapper) executeParallelProgramCalls(ctx *gent.ExecutionContext,
	program *programState, calls []*gent.ModelToolCallRequest, format gent.TextFormat,
) ([]*gent.ToolCallRecord, error) {
	chain, ok := w.wrapped.(gent.ParallelToolChain)
	if !ok || !chain.SupportsParallelCalls() {
		return nil, errors.New("tool.parallelCall requires a ParallelToolChain")
	}
	batch := &parallelProgramCalls{}
	plans := make([]*gent.ToolPlan, 0, len(calls))
	for _, call := range calls {
		plan, err := chain.PrepareCalls(ctx, []*gent.ModelToolCallRequest{call})
		if err != nil {
			return nil, err
		}
		if err := checkToolPlan(ctx, plan); err != nil {
			return nil, err
		}
		plan.CheckpointKey = "gent:program_call:" + plan.Calls[0].Id
		plans = append(plans, plan)
		batch.Keys = append(batch.Keys, plan.CheckpointKey)
	}
	for _, plan := range plans {
		if err := ctx.SetToolPlan(plan); err != nil {
			return nil, err
		}
	}
	if err := ctx.SetCheckpointState("gent:program_calls", batch); err != nil {
		return nil, err
	}
	if err := ctx.CommitCheckpoint(gent.PhaseDecisionReady); err != nil {
		return nil, err
	}
	// Keep the supplied chain as the receiver. A promoted base method must not bypass an
	// ExecuteNext override that checks whether a tool call is allowed.
	result, err := executeParallelToolPlans(ctx, chain, plans, format)
	if err != nil {
		return nil, err
	}
	program.Completed = append(program.Completed, result...)
	if err := ctx.SetCheckpointState("gent:program", program); err != nil {
		return nil, err
	}
	clearParallelProgramCalls(ctx, batch)
	if err := ctx.CommitCheckpoint(gent.PhaseToolCompleted); err != nil {
		return nil, err
	}
	return result, nil
}

func clearParallelProgramCalls(ctx *gent.ExecutionContext, batch *parallelProgramCalls) {
	for _, key := range batch.Keys {
		ctx.ClearCheckpointState(key)
	}
	ctx.ClearCheckpointState("gent:program_calls")
}

// An accepted outer reply closes the program even when its last control checkpoint is older.
// Remove saved inner plans without recovering their already accepted effects again.
func clearProgram(ctx *gent.ExecutionContext) error {
	value, _, err := ctx.CheckpointState("gent:program_calls")
	if err != nil {
		return err
	}
	if batch, ok := value.(*parallelProgramCalls); ok {
		clearParallelProgramCalls(ctx, batch)
	}
	ctx.ClearCheckpointState("gent:program", "gent:program_calls")
	return nil
}

func (w *JsToolChainWrapper) recoverProgram(ctx *gent.ExecutionContext,
	outer *gent.ToolPlan, program *programState, format gent.TextFormat,
) (*gent.ToolCallRecord, error) {
	call := outer.Calls[outer.NextCall]
	if program.CallId != call.Id {
		return nil, errors.New("JavaScript recovery call does not match")
	}
	results := append([]*gent.ToolCallRecord(nil), program.Completed...)
	value, found, err := ctx.CheckpointState("gent:program_calls")
	if err != nil {
		return nil, err
	}
	var parallel *parallelProgramCalls
	if found {
		var plans []*gent.ToolPlan
		switch saved := value.(type) {
		case *gent.ToolPlan:
			plans = append(plans, saved)
		case *parallelProgramCalls:
			parallel = saved
			for _, key := range saved.Keys {
				value, found, err := ctx.CheckpointState(key)
				if err != nil {
					return nil, err
				}
				plan, ok := value.(*gent.ToolPlan)
				if !found || !ok {
					return nil, errors.New("invalid parallel JavaScript call plan")
				}
				plans = append(plans, plan)
			}
		default:
			return nil, errors.New("invalid JavaScript call plan")
		}
		for _, inner := range plans {
			partial, err := w.recoverProgramCalls(ctx, inner, format)
			if err != nil {
				return nil, err
			}
			results = append(results, partial...)
		}
	}
	report := &gent.ToolRecoveryResponse{Status: gent.ToolRecoveryUnresolved,
		Parts: jsruntime.ProgramParts(results, ctx.SteeringMessages(), nil),
		Instructions: fmt.Sprintf(`JavaScript execution was interrupted. The script was not restarted.
Script:
%s
Review the results and make a new plan.`, program.Script)}
	snapshot, err := ctx.SnapshotDiagnostic(report)
	if err != nil {
		return nil, err
	}
	result, err := gent.NewToolCallRecordBuilder(call.Id, ctx.AttemptId(), call.Name).
		Arguments(call.RawArguments, call.Args).Recovery(report).ReturnedRecovery(snapshot).
		ModelReply(call.ModelCallID, call.ModelName,
			gent.PrepareRecoveryParts(report, ctx.SteeringMessages())...).Build()
	if err != nil {
		return nil, err
	}
	ctx.PublishAfterToolCall(call.Name, nil, result, 0, nil,
		gent.WithToolCallId(call.Id), gent.WithToolCallAttempt(program.AttemptId))
	if err := ctx.RecordingError(); err != nil {
		return nil, err
	}
	if parallel != nil {
		clearParallelProgramCalls(ctx, parallel)
	}
	ctx.ClearCheckpointState("gent:program", "gent:program_calls")
	return completePlannedTool(ctx, outer, result)
}

// Recovery observes each unfinished inner call. No script or business call is replayed.
func (w *JsToolChainWrapper) recoverProgramCalls(ctx *gent.ExecutionContext,
	plan *gent.ToolPlan, format gent.TextFormat,
) ([]*gent.ToolCallRecord, error) {
	for plan.NextCall < len(plan.Calls) {
		if _, err := w.wrapped.ExecuteNext(ctx, plan, format); err != nil {
			return nil, err
		}
	}
	return plan.Results, nil
}
