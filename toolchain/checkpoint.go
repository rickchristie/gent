package toolchain

import (
	"encoding/json"
	"errors"
	"fmt"
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

func (c *YAML) RegisterCodecs(registry *gent.CodecRegistry) error {
	return registerToolCodecs(registry, c.tools)
}

func (c *JSON) RegisterCodecs(registry *gent.CodecRegistry) error {
	return registerToolCodecs(registry, c.tools)
}

func (c *SearchJSON) RegisterCodecs(registry *gent.CodecRegistry) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return registerToolCodecs(registry, c.tools)
}

func prepareToolPlan(
	ctx *gent.ExecutionContext,
	chain gent.ToolChain,
	content string,
) (*gent.ToolPlan, error) {
	if ctx == nil {
		return nil, errors.New("gent: a tool plan requires an execution context")
	}
	if codecs, ok := chain.(interface {
		RegisterCodecs(*gent.CodecRegistry) error
	}); ok {
		if err := codecs.RegisterCodecs(ctx.Codecs()); err != nil {
			return nil, err
		}
	}
	parsed, err := chain.ParseSection(ctx, content)
	if err != nil {
		return nil, err
	}
	calls := parsed.([]*gent.ToolCall)
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

func (c *YAML) Prepare(ctx *gent.ExecutionContext, content string) (*gent.ToolPlan, error) {
	return prepareToolPlan(ctx, c, content)
}
func (c *JSON) Prepare(ctx *gent.ExecutionContext, content string) (*gent.ToolPlan, error) {
	return prepareToolPlan(ctx, c, content)
}
func (c *SearchJSON) Prepare(ctx *gent.ExecutionContext, content string) (*gent.ToolPlan, error) {
	return prepareToolPlan(ctx, c, content)
}

type rawToolExecutor func(
	*gent.ExecutionContext,
	string,
	gent.TextFormat,
) (*gent.ToolChainResult, error)

func executeNextTool(
	ctx *gent.ExecutionContext, plan *gent.ToolPlan,
	format gent.TextFormat, execute rawToolExecutor,
) (*gent.ToolCallResult, error) {
	if plan == nil || plan.Version != 1 || plan.NextCall < 0 || plan.NextCall > len(plan.Calls) {
		return nil, errors.New("gent: invalid tool plan")
	}
	if plan.NextCall == len(plan.Calls) {
		return nil, gent.ErrToolPlanComplete
	}
	if err := ctx.RecordingError(); err != nil {
		return nil, err
	}
	if err := ctx.Context().Err(); err != nil {
		return nil, err
	}
	ctx.SelectToolCall(plan)
	defer ctx.SelectToolCall(nil)
	result, completed, err := ctx.RecoverTool(plan)
	if err != nil {
		return nil, err
	}
	call := plan.Calls[plan.NextCall]
	if !completed {
		content, err := json.Marshal([]map[string]any{{"tool": call.Name, "args": call.Args}})
		if err != nil {
			return nil, err
		}
		if err := ctx.SetToolPlan(plan); err != nil {
			return nil, err
		}
		output, err := execute(ctx, string(content), format)
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

func (c *YAML) ExecuteNext(
	ctx *gent.ExecutionContext,
	plan *gent.ToolPlan,
	format gent.TextFormat,
) (
	*gent.ToolCallResult, error,
) {
	return executeNextTool(ctx, plan, format, c.executeUnmanaged)
}

func (c *JSON) ExecuteNext(
	ctx *gent.ExecutionContext,
	plan *gent.ToolPlan,
	format gent.TextFormat,
) (
	*gent.ToolCallResult, error,
) {
	return executeNextTool(ctx, plan, format, c.executeUnmanaged)
}

func (c *SearchJSON) ExecuteNext(
	ctx *gent.ExecutionContext,
	plan *gent.ToolPlan,
	format gent.TextFormat,
) (
	*gent.ToolCallResult, error,
) {
	return executeNextTool(ctx, plan, format, c.executeUnmanaged)
}

func executeToolPlan(ctx *gent.ExecutionContext, chain gent.RecoverableToolChain,
	content string, format gent.TextFormat) (*gent.ToolChainResult, error) {
	plan, err := chain.Prepare(ctx, content)
	if err != nil {
		return nil, err
	}
	for plan.NextCall < len(plan.Calls) {
		if _, err := chain.ExecuteNext(ctx, plan, format); err != nil {
			return nil, fmt.Errorf("execute tool %d: %w", plan.NextCall, err)
		}
	}
	return &gent.ToolChainResult{Results: plan.Results}, nil
}

func (c *YAML) Execute(ctx *gent.ExecutionContext, content string, format gent.TextFormat) (
	*gent.ToolChainResult, error,
) {
	if ctx == nil {
		return c.executeUnmanaged(nil, content, format)
	}
	return executeToolPlan(ctx, c, content, format)
}

func (c *JSON) Execute(ctx *gent.ExecutionContext, content string, format gent.TextFormat) (
	*gent.ToolChainResult, error,
) {
	if ctx == nil {
		return c.executeUnmanaged(nil, content, format)
	}
	return executeToolPlan(ctx, c, content, format)
}

func (c *SearchJSON) Execute(ctx *gent.ExecutionContext, content string, format gent.TextFormat) (
	*gent.ToolChainResult, error,
) {
	if ctx == nil {
		return c.executeUnmanaged(nil, content, format)
	}
	return executeToolPlan(ctx, c, content, format)
}
