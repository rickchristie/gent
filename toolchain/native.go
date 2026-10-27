package toolchain

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rickchristie/gent"
)

type parsedCall struct {
	Id            string
	Name          string
	Args          map[string]any
	CheckpointKey string
}

func nativeDefinitions(tools []any, outputSchema bool) ([]gent.ToolDefinition, error) {
	definitions := make([]gent.ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		meta, err := GetToolMeta(tool)
		if err != nil {
			return nil, err
		}
		description := meta.Description()
		if policy := meta.Policy(); policy != "" {
			description += "\n\nPolicy: " + policy
		}
		if outputSchema && meta.OutputSchema() != nil {
			encoded, err := json.Marshal(meta.OutputSchema())
			if err != nil {
				return nil, fmt.Errorf("tool %q output schema: %w", meta.Name(), err)
			}
			description += "\n\nReturns: " + string(encoded)
		}
		parameters := meta.Schema()
		if parameters == nil {
			parameters = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		definitions = append(definitions, gent.ToolDefinition{Type: "function",
			Function: &gent.FunctionDefinition{Name: meta.Name(), Description: description,
				Parameters: parameters}})
	}
	return definitions, nil
}

// copyNativeTools keeps request hooks from changing the registered tool schemas.
func copyNativeTools(
	ctx *gent.ExecutionContext, tools []gent.ToolDefinition,
) ([]gent.ToolDefinition, error) {
	owned, err := ctx.Codecs().Clone(tools)
	if err != nil {
		return nil, err
	}
	return owned.([]gent.ToolDefinition), nil
}

func (c *Registry) NativeTools(ctx *gent.ExecutionContext) ([]gent.ToolDefinition, error) {
	tools, err := nativeDefinitions(c.tools, c.printOutputSchema)
	if err != nil {
		return nil, err
	}
	return copyNativeTools(ctx, tools)
}

func prepareNativeCalls(
	ctx *gent.ExecutionContext, chain gent.ToolChain, calls []gent.ModelToolCall,
	validate func(*parsedCall) error,
) (*gent.ToolPlan, error) {
	if err := chain.RegisterCodecs(ctx.Codecs()); err != nil {
		return nil, err
	}
	parsed := make([]*parsedCall, 0, len(calls))
	for _, call := range calls {
		decoded, err := decodeNativeCall(call)
		if err == nil && validate != nil {
			err = validate(decoded)
		}
		if err != nil {
			content := ""
			if call.FunctionCall != nil {
				content = call.FunctionCall.Arguments
			}
			ctx.PublishParseError(gent.ParseErrorTypeToolchain, content, err)
			return nil, err
		}
		parsed = append(parsed, decoded)
	}
	ctx.Stats().ResetGauge(gent.SGToolchainParseErrorConsecutive)
	return prepareParsedToolPlan(ctx, parsed)
}

func decodeNativeCall(call gent.ModelToolCall) (*parsedCall, error) {
	if call.Type != "" && call.Type != "function" {
		return nil, fmt.Errorf("unsupported tool call type %q", call.Type)
	}
	if call.FunctionCall == nil || call.FunctionCall.Name == "" {
		return nil, gent.ErrMissingToolName
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(call.FunctionCall.Arguments), &args); err != nil {
		return nil, fmt.Errorf("%w: %v", gent.ErrInvalidJSON, err)
	}
	if args == nil {
		return nil, fmt.Errorf("%w: tool arguments must be an object", gent.ErrInvalidJSON)
	}
	return &parsedCall{Name: call.FunctionCall.Name, Args: args}, nil
}

func (c *Registry) PrepareCalls(
	ctx *gent.ExecutionContext, calls []gent.ModelToolCall,
) (*gent.ToolPlan, error) {
	return prepareNativeCalls(ctx, c, calls, nil)
}

func (c *SearchChain) PrepareCalls(
	ctx *gent.ExecutionContext, calls []gent.ModelToolCall,
) (*gent.ToolPlan, error) {
	return prepareNativeCalls(ctx, c, calls, nil)
}

func (c *SearchChain) NativeTools(ctx *gent.ExecutionContext) ([]gent.ToolDefinition, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.initialized {
		return nil, fmt.Errorf("SearchChain must be initialized before native tool calls")
	}
	records, err := ctx.Scratchpad()
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, name := range c.pinnedToolNames {
		names[name] = true
	}
	for _, record := range records {
		if record.ExpireAfterIteration > 0 && ctx.Iteration() >= record.ExpireAfterIteration {
			continue
		}
		for _, message := range record.Messages {
			value, ok := message.GetMetadata(gent.MMKToolChainResult)
			if result, valid := value.(*gent.ToolChainResult); ok && valid {
				addDiscoveredTools(names, result)
			}
		}
	}
	var available []any
	for _, tool := range c.tools {
		meta, err := GetToolMeta(tool)
		if err != nil {
			return nil, err
		}
		if names[meta.Name()] {
			available = append(available, tool)
		}
	}
	definitions, err := nativeDefinitions(available, c.printOutputSchema)
	if err != nil {
		return nil, err
	}
	tools := c.nativeSearchTools()
	tools = append(tools, definitions...)
	return copyNativeTools(ctx, tools)
}

func addDiscoveredTools(names map[string]bool, result *gent.ToolChainResult) {
	for _, call := range result.Results {
		if call.Error != nil {
			continue
		}
		for _, name := range call.DiscoveredTools {
			names[name] = true
		}
		if nested, ok := call.Output.(*gent.ToolChainResult); ok {
			addDiscoveredTools(names, nested)
		}
	}
}

func (c *SearchChain) nativeSearchTools() []gent.ToolDefinition {
	hint := buildDomainSummary(c.indexableTools)
	if c.hintType == SearchHintSimpleList {
		hint = buildSimpleList(c.indexableTools)
	}
	var engines strings.Builder
	for _, engine := range c.engines {
		fmt.Fprintf(&engines, "\n%s: %s", engine.Id(), engine.SearchGuidance())
	}
	description := "Search the tool registry. Use the exact names and argument schemas returned."
	if c.searchType == SearchGet {
		description += " Call get_tool_schema to load a tool before using it."
	}
	description += "\n\nRegistry:\n" + hint + "\nSearch engines:" + engines.String()
	tools := []gent.ToolDefinition{{Type: "function", Function: &gent.FunctionDefinition{
		Name: searchToolName, Description: description, Parameters: c.searchToolSchema,
	}}}
	if c.searchType == SearchGet {
		tools = append(tools, gent.ToolDefinition{Type: "function", Function: &gent.FunctionDefinition{
			Name: getSchemaToolName, Description: "Load a tool's schema, policy, and instructions.",
			Parameters: map[string]any{"type": "object", "properties": map[string]any{
				"tool_name": map[string]any{"type": "string"},
			}, "required": []string{"tool_name"}},
		}})
	}
	return tools
}

var _ gent.ToolChain = (*Registry)(nil)

var _ gent.ToolChain = (*SearchChain)(nil)
