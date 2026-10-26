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
	return &gent.ToolChainResult{Results: plan.Results}, nil
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
	if found {
		inner, ok := value.(*gent.ToolPlan)
		if !ok {
			return nil, errors.New("invalid JavaScript call plan")
		}
		results = append(results, inner.Results...)
		if inner.ReadyCall != nil {
			result, completed, recoverErr := ctx.RecoverTool(inner)
			if recoverErr != nil && !errors.Is(recoverErr, gent.ErrUncertainTool) {
				return nil, recoverErr
			}
			if !completed {
				result, err = unknownTool(ctx, inner.ReadyCall, w.wrapped)
				if err != nil {
					return nil, err
				}
			}
			if result.Outcome == "unknown" {
				info.Uncertain = append(info.Uncertain, inner.ReadyCall)
			}
			results = append(results, result)
			inner.NextCall++
		}
		for inner.NextCall < len(inner.Calls) {
			results = append(results, skippedTool(inner))
			inner.NextCall++
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
	ctx.ClearCheckpointState("gent:program", "gent:program_calls")
	return completePlannedTool(ctx, outer, result)
}
