package toolchain

import (
	"errors"
	"fmt"
	"strings"

	"github.com/rickchristie/gent"
)

// A program is a sequence of native batches. Save each batch before it can start an effect.
// The outer plan remains under gent:tool_plan; inner checkpoints must never replace it.
type programState struct {
	AttemptId string
	CallId    string
	Script    string
	Completed []*gent.ToolCallResult
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
	calls []gent.ModelToolCall, format gent.TextFormat,
) (*gent.ToolChainResult, error) {
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
	ctx.ClearCheckpointState("gent:program_calls", "gent:program_calls:search_seen")
	if err := ctx.CommitCheckpoint(gent.PhaseToolCompleted); err != nil {
		return nil, err
	}
	return &gent.ToolChainResult{Results: plan.Results}, nil
}

func (w *JsToolChainWrapper) executeParallelProgramCalls(ctx *gent.ExecutionContext,
	program *programState, calls []gent.ModelToolCall, format gent.TextFormat,
) (*gent.ToolChainResult, error) {
	chain, ok := w.wrapped.(gent.ParallelToolChain)
	if !ok || !chain.SupportsParallelCalls() {
		return nil, errors.New("tool.parallelCall requires a ParallelToolChain")
	}
	batch := &parallelProgramCalls{}
	plans := make([]*gent.ToolPlan, 0, len(calls))
	for _, call := range calls {
		plan, err := chain.PrepareCalls(ctx, []gent.ModelToolCall{call})
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
	program.Completed = append(program.Completed, result.Results...)
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
		ctx.ClearCheckpointState(key, key+":search_seen")
	}
	ctx.ClearCheckpointState("gent:program_calls")
}

func (w *JsToolChainWrapper) recoverProgram(ctx *gent.ExecutionContext,
	outer *gent.ToolPlan, program *programState,
) (*gent.ToolCallResult, error) {
	call := outer.Calls[outer.NextCall]
	if program.CallId != call.Id {
		return nil, errors.New("JavaScript recovery call does not match")
	}
	results := append([]*gent.ToolCallResult(nil), program.Completed...)
	info := &gent.RecoveryInfo{RunId: ctx.RunId(), ContextId: ctx.ContextId(),
		ToolCallId: call.Id, AttemptId: program.AttemptId, Name: call.Name,
		Args: call.Args, Script: program.Script}
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
			partial, err := w.recoverProgramCalls(ctx, inner, info)
			if err != nil {
				return nil, err
			}
			results = append(results, partial...)
		}
	}
	for _, result := range results {
		if result.Outcome == "" {
			info.Completed = append(info.Completed, result)
		}
	}
	owned, err := ctx.Codecs().Clone(info)
	if err != nil {
		return nil, err
	}
	guidance := w.RecoveryMessage(owned.(*gent.RecoveryInfo))
	var text strings.Builder
	fmt.Fprintf(&text, `JavaScript execution was interrupted. The script will not be restarted.
Script:
%s`, program.Script)
	for _, result := range results {
		fmt.Fprintf(&text, "\n\n%s", result.Text)
	}
	text.WriteString("\n\nReview these results and make a new plan.")
	if guidance != "" {
		text.WriteString("\n\nRecovery guidance:\n" + guidance)
	}
	result := &gent.ToolCallResult{Name: call.Name, ToolCallId: call.Id,
		AttemptId: program.AttemptId, Args: call.Args, Outcome: "unknown",
		Output: &gent.ToolChainResult{Results: results}, Error: gent.ErrUncertainTool,
		Media: collectMedia(&gent.ToolChainResult{Results: results}),
		Text:  text.String(), Instructions: guidance, SummaryCaptured: true}
	if parallel != nil {
		clearParallelProgramCalls(ctx, parallel)
	}
	ctx.ClearCheckpointState("gent:program", "gent:program_calls", "gent:program_calls:search_seen")
	return completePlannedTool(ctx, outer, result)
}

func (w *JsToolChainWrapper) recoverProgramCalls(ctx *gent.ExecutionContext,
	plan *gent.ToolPlan, info *gent.RecoveryInfo,
) ([]*gent.ToolCallResult, error) {
	results := append([]*gent.ToolCallResult(nil), plan.Results...)
	if plan.ReadyCall != nil {
		result, completed, err := ctx.RecoverTool(plan)
		if err != nil && !errors.Is(err, gent.ErrUncertainTool) {
			return nil, err
		}
		if !completed {
			result, err = unknownTool(ctx, plan.ReadyCall, w.wrapped)
			if err != nil {
				return nil, err
			}
		}
		if result.Outcome == "unknown" {
			info.Uncertain = append(info.Uncertain, plan.ReadyCall)
		}
		results = append(results, result)
		plan.NextCall++
	}
	for plan.NextCall < len(plan.Calls) {
		results = append(results, skippedTool(plan))
		plan.NextCall++
	}
	return results, nil
}
