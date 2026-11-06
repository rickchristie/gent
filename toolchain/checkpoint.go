package toolchain

import (
	"context"
	"errors"
	"reflect"

	"github.com/rickchristie/gent"
)

func registerToolCodecs(registry *gent.CodecRegistry, tools []any) error {
	for _, tool := range tools {
		input := reflect.ValueOf(tool).MethodByName("Call").Type().In(1)
		if err := registry.RegisterValueType(input); err != nil {
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
	if err := registry.RegisterValueType(reflect.TypeFor[gent.ToolExecuteRequest]()); err != nil {
		return err
	}
	if err := registry.RegisterValueType(reflect.TypeFor[gent.ToolSearchRequest]()); err != nil {
		return err
	}
	return registerToolCodecs(registry, c.tools)
}

type rawToolExecutor func(*gent.ExecutionContext, []*parsedCall, gent.TextFormat) (
	[]*gent.ToolCallRecord, error,
)

func executeNextTool(ctx *gent.ExecutionContext, plan *gent.ToolPlan, format gent.TextFormat,
	chain gent.ToolChain, execute rawToolExecutor, recover gent.ToolRecoveryFunc[gent.EncodedValue],
) (*gent.ToolCallRecord, error) {
	if err := checkToolPlan(ctx, plan); err != nil {
		return nil, err
	}
	unbind := ctx.BindToolCall(plan)
	defer unbind()
	expectedSchema := chain.GetToolSchema(plan.Calls[plan.NextCall].Name)
	result, completed, err := ctx.RecoverTool(plan, recover, expectedSchema)
	if err != nil {
		return nil, err
	}
	if completed {
		return completePlannedTool(ctx, plan, result)
	}
	if err := ctx.SetToolPlan(plan); err != nil {
		return nil, err
	}
	call := plan.Calls[plan.NextCall]
	args, parseErr := decodeArguments(call.RawArguments)
	if call.ModelName != call.Name {
		args, parseErr = call.Args, nil
	}
	calls := []*parsedCall{{Id: call.Id, Name: call.Name, Args: args,
		ModelCallID: call.ModelCallID, ModelName: call.ModelName, RawArguments: call.RawArguments,
		parseError: parseErr}}
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
	if len(output) != 1 {
		return nil, errors.New("gent: planned call must produce one record")
	}
	return completePlannedTool(ctx, plan, output[0])
}

func checkToolPlan(ctx *gent.ExecutionContext, plan *gent.ToolPlan) error {
	if plan.Version != 1 || plan.NextCall < 0 || plan.NextCall > len(plan.Calls) {
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

func completePlannedTool(ctx *gent.ExecutionContext, plan *gent.ToolPlan,
	result *gent.ToolCallRecord,
) (*gent.ToolCallRecord, error) {
	plan.Results = append(plan.Results, result)
	plan.NextCall++
	plan.ReadyCall = nil
	if err := ctx.SetToolPlan(plan); err != nil {
		return nil, err
	}
	if err := ctx.CommitCheckpoint(gent.PhaseToolCompleted); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Registry) ExecuteNext(ctx *gent.ExecutionContext, plan *gent.ToolPlan,
	format gent.TextFormat,
) (*gent.ToolCallRecord, error) {
	return executeNextTool(ctx, plan, format, c, c.executeParsed, c.Recover)
}

func (c *SearchChain) ExecuteNext(ctx *gent.ExecutionContext, plan *gent.ToolPlan,
	format gent.TextFormat,
) (*gent.ToolCallRecord, error) {
	return executeNextTool(ctx, plan, format, c, c.executeParsed,
		func(recoveryCtx context.Context, request *gent.ToolRecoveryRequest[gent.EncodedValue]) (
			*gent.ToolRecoveryResponse, error,
		) {
			if request.Name() != searchToolName {
				return c.Recover(recoveryCtx, request)
			}
			bound, err := gent.BindToolRecoveryInput(recoveryCtx,
				reflect.TypeFor[*gent.ToolRecoveryRequest[gent.ToolSearchRequest]](), request)
			if err != nil {
				return nil, err
			}
			input := bound.(*gent.ToolRecoveryRequest[gent.ToolSearchRequest]).Input()
			response, err := c.localSearchTool(ctx, func([]string) {}).Call(recoveryCtx, input)
			if err != nil {
				return nil, err
			}
			return &gent.ToolRecoveryResponse{Status: gent.ToolRecoveryCompleted, Execution: response,
				Instructions: "The local search was repeated against the current tool catalog."}, nil
		})
}
