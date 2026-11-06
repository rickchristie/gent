package toolchain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/schema"
)

type parsedCall struct {
	Id           string
	ModelCallID  string
	ModelName    string
	Name         string
	RawArguments string
	Args         map[string]any
	parseError   error
	discovered   []string
}

func nativeDefinitions(tools []any, outputSchema bool) ([]*gent.ToolDefinition, error) {
	definitions := make([]*gent.ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		meta, err := GetToolMeta(tool)
		if err != nil {
			return nil, err
		}
		description := meta.Description()
		if policy := meta.Policy(); policy != "" {
			description += "\n\nPolicy: " + policy
		}
		parameters := meta.Schema()
		if parameters == nil {
			parameters = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		definition := &gent.FunctionDefinition{Name: meta.Name(), Description: description,
			Parameters: parameters, Strict: meta.strict}
		if outputSchema {
			definition.OutputSchema = meta.OutputSchema()
		}
		definitions = append(definitions, &gent.ToolDefinition{Type: "function", Function: definition})
	}
	return definitions, nil
}

// Request hooks can edit nested schemas. Preparation creates the independent request view at
// the managed boundary; catalog reads themselves need no defensive copy.
func (c *Registry) NativeTools(ctx *gent.ExecutionContext) ([]*gent.ToolDefinition, error) {
	var available []any
	for _, tool := range c.tools {
		if !c.deprecated[tool.(gent.IndexableTool).Name()] {
			available = append(available, tool)
		}
	}
	return nativeDefinitions(available, c.printOutputSchema)
}

func prepareNativeCalls(ctx *gent.ExecutionContext, chain gent.ToolChain,
	calls []*gent.ModelToolCallRequest,
) (*gent.ToolPlan, error) {
	if err := chain.RegisterCodecs(ctx.Codecs()); err != nil {
		return nil, err
	}
	plan := &gent.ToolPlan{Version: 1, AttemptId: ctx.AttemptId()}
	for _, call := range calls {
		if call.FunctionCall == nil || call.FunctionCall.Name == "" {
			return nil, gent.ErrMissingToolName
		}
		id := ctx.NewToolCallId()
		modelID := call.ID
		if modelID == "" {
			modelID = id
		}
		args, _ := decodeArguments(call.FunctionCall.Arguments)
		plan.Calls = append(plan.Calls, gent.PlannedToolCall{Id: id,
			ModelCallID: modelID, ModelName: call.FunctionCall.Name, Name: call.FunctionCall.Name,
			RawArguments: call.FunctionCall.Arguments, Args: args})
	}
	return plan, nil
}

func decodeArguments(raw string) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.UseNumber()
	var args map[string]any
	if err := decoder.Decode(&args); err != nil {
		return nil, err
	}
	if args == nil {
		return nil, fmt.Errorf("tool arguments must be an object")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("tool arguments contain more than one value")
	}
	return args, nil
}

func (c *Registry) PrepareCalls(ctx *gent.ExecutionContext,
	calls []*gent.ModelToolCallRequest,
) (*gent.ToolPlan, error) {
	return prepareNativeCalls(ctx, c, calls)
}

func (c *SearchChain) PrepareCalls(ctx *gent.ExecutionContext,
	calls []*gent.ModelToolCallRequest,
) (*gent.ToolPlan, error) {
	plan, err := prepareNativeCalls(ctx, c, calls)
	if err != nil {
		return nil, err
	}
	outerSchema := schema.MustCompile(sharedExecutionSchema)
	for i := range plan.Calls {
		call := &plan.Calls[i]
		if call.Name != executeToolName || outerSchema.Validate(call.Args) != nil {
			continue
		}
		call.Name = call.Args["tool"].(string)
		call.Args = call.Args["args"].(map[string]any)
	}
	return plan, nil
}
