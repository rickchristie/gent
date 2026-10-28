package toolchain

import (
	"errors"
	"reflect"

	"github.com/rickchristie/gent"
)

func registerToolCodecs(registry *gent.CodecRegistry, tools []any) error {
	for _, tool := range tools {
		call := reflect.ValueOf(tool).MethodByName("Call").Type()
		if err := registry.RegisterValueType(call.In(1)); err != nil {
			return err
		}
		result := call.Out(0).Elem()
		field, ok := result.FieldByName("Text")
		if !ok {
			return errors.New("tool result has no Text field")
		}
		if err := registry.RegisterValueType(field.Type); err != nil {
			return err
		}
	}
	return nil
}

func (c *Registry) RegisterCodecs(registry *gent.CodecRegistry) error {
	return registerToolCodecs(registry, c.tools)
}

func (c *SearchChain) RegisterCodecs(registry *gent.CodecRegistry) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return registerToolCodecs(registry, c.tools)
}

func prepareParsedToolPlan(
	ctx *gent.ExecutionContext, calls []*parsedCall,
) (*gent.ToolPlan, error) {
	plan := &gent.ToolPlan{Version: 1}
	for _, call := range calls {
		plan.Calls = append(plan.Calls, gent.PlannedToolCall{Id: ctx.NewToolCallId(),
			Name: call.Name, Args: call.Args})
	}
	if err := ctx.SetCheckpointState("gent:search_seen", map[string]bool{}); err != nil {
		return nil, err
	}
	return plan, nil
}

type rawToolExecutor func(
	*gent.ExecutionContext,
	[]*parsedCall,
	gent.TextFormat,
) (*gent.ToolChainResult, error)

func executeNextTool(
	ctx *gent.ExecutionContext, plan *gent.ToolPlan,
	format gent.TextFormat, chain gent.ToolChain, execute rawToolExecutor,
) (*gent.ToolCallResult, error) {
	if err := checkToolPlan(ctx, plan); err != nil {
		return nil, err
	}
	if plan.Abandoned {
		return completePlannedTool(ctx, plan, skippedTool(plan))
	}
	unbind := ctx.BindToolCall(plan)
	defer unbind()
	result, completed, err := ctx.RecoverTool(plan, chain.Recover)
	if err != nil {
		return nil, err
	}
	call := plan.Calls[plan.NextCall]
	if !completed {
		if err := ctx.SetToolPlan(plan); err != nil {
			return nil, err
		}
		calls := []*parsedCall{{Id: call.Id, Name: call.Name, Args: call.Args,
			CheckpointKey: plan.CheckpointKey}}
		output, err := execute(ctx, calls, format)
		if recordErr := ctx.RecordingError(); recordErr != nil {
			return nil, recordErr
		}
		if ctx.IsWorkerInterrupted() {
			return nil, ctx.Context().Err()
		}
		if err != nil {
			return nil, err
		}
		if output == nil || len(output.Results) != 1 {
			return nil, errors.New("gent: one planned tool call must produce one result")
		}
		result = output.Results[0]
		result.ToolCallId = call.Id
		result.AttemptId = ctx.AttemptId()
		// The tool executor captured the summary before it saved the result. Keep that value,
		// including an empty summary, so recovery and live execution use the same evidence.
	}
	return completePlannedTool(ctx, plan, result)
}

func checkToolPlan(ctx *gent.ExecutionContext, plan *gent.ToolPlan) error {
	if plan == nil || plan.Version != 1 || plan.NextCall < 0 || plan.NextCall > len(plan.Calls) {
		return errors.New("gent: invalid tool plan")
	}
	if plan.NextCall == len(plan.Calls) {
		return gent.ErrToolPlanComplete
	}
	if err := ctx.RecordingError(); err != nil {
		return err
	}
	return ctx.Context().Err()
}

func completePlannedTool(
	ctx *gent.ExecutionContext, plan *gent.ToolPlan, result *gent.ToolCallResult,
) (*gent.ToolCallResult, error) {
	if result.Outcome == "unknown" {
		plan.Abandoned = true
	}
	plan.Results = append(plan.Results, result)
	plan.NextCall++
	plan.ReadyCall = nil
	plan.RecoverySteering = ""
	if err := ctx.SetToolPlan(plan); err != nil {
		return nil, err
	}
	if err := ctx.CommitCheckpoint(gent.PhaseToolCompleted); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Registry) ExecuteNext(
	ctx *gent.ExecutionContext,
	plan *gent.ToolPlan,
	format gent.TextFormat,
) (
	*gent.ToolCallResult, error,
) {
	return executeNextTool(ctx, plan, format, c, c.executeParsed)
}

func (c *SearchChain) ExecuteNext(
	ctx *gent.ExecutionContext,
	plan *gent.ToolPlan,
	format gent.TextFormat,
) (
	*gent.ToolCallResult, error,
) {
	return executeNextTool(ctx, plan, format, c, c.executeParsed)
}
