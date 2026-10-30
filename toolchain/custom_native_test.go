package toolchain_test

import (
	"context"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents/react"
	"github.com/rickchristie/gent/events"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/internal/tt"
	"github.com/rickchristie/gent/toolchain"
	"github.com/stretchr/testify/assert"
)

type selectedTools struct {
	*toolchain.Registry
	name string
}

func (c *selectedTools) NativeTools(ctx *gent.ExecutionContext) ([]gent.ToolDefinition, error) {
	tools, err := c.Registry.NativeTools(ctx)
	if err != nil {
		return nil, err
	}
	var selected []gent.ToolDefinition
	for _, tool := range tools {
		if tool.Function.Name == c.name {
			selected = append(selected, tool)
		}
	}
	return selected, nil
}

func (c *selectedTools) RecoveryConfig() any {
	return map[string]any{"tools": c.Registry.RecoveryConfig(), "selected": c.name}
}

type selectedInput struct {
	ID string `json:"id"`
}

type selectedHook struct{ calls int }

func (h *selectedHook) OnBeforeToolCall(
	_ *gent.ExecutionContext, event *gent.BeforeToolCallEvent,
) {
	h.calls++
	event.Args = &selectedInput{ID: "checked"}
}

func TestCustomNativeSelectionKeepsExecutionHooks(t *testing.T) {
	type expected struct {
		inputs []*selectedInput
		hooks  int
		calls  int64
		tools  [][]gent.ToolDefinition
		result []gent.ContentPart
		err    error
	}
	var inputs []*selectedInput
	chain := &selectedTools{Registry: toolchain.New(), name: "lookup"}
	for _, name := range []string{"lookup", "hidden"} {
		inputSchema := map[string]any{
			"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}},
			"required": []string{"id"}, "additionalProperties": false,
		}
		chain.RegisterTool(gent.DefineTool(name,
			func(_ context.Context, input *selectedInput) (string, error) {
				inputs = append(inputs, input)
				return "record", nil
			}).Description("Read a record").ParameterSchema(inputSchema).MustBuild())
	}
	model := tt.NewMockModel().AddToolCall("lookup", `{"id":"raw"}`, 1, 1).
		AddResponse("Done.", 1, 1)
	agent := react.NewAgent(model).WithToolChain(chain).WithRecoveryVersion("selection/v1")
	hook := &selectedHook{}
	ctx := gent.NewExecutionContext(context.Background(), "selected",
		gent.NewBasicLoopData(gent.NewTask(gent.TextContent{Text: "Read the record."})))
	definitions, err := chain.Registry.NativeTools(ctx)
	if !assert.NoError(t, err) {
		return
	}
	_, result, err := executor.NewRunner(agent, executor.DefaultConfig()).
		WithEvents(events.NewRegistry().Subscribe(hook)).Execute(ctx)
	if !assert.NoError(t, err) {
		return
	}
	var requests [][]gent.ToolDefinition
	for _, request := range model.CapturedRequests {
		requests = append(requests, request.Options.Tools)
	}
	assert.Equal(t, expected{
		inputs: []*selectedInput{{ID: "checked"}}, hooks: 1, calls: 1,
		tools:  [][]gent.ToolDefinition{definitions[:1], definitions[:1]},
		result: []gent.ContentPart{gent.TextContent{Text: "Done."}},
	}, expected{
		inputs: inputs, hooks: hook.calls, calls: ctx.Stats().GetCounter(gent.SCToolCalls),
		tools: requests, result: result.Output, err: err,
	})
}
