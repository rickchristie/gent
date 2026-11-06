package toolchain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/schema"
)

const executeToolName = "execute_tool"

var sharedExecutionSchema = map[string]any{
	"type": "object", "properties": map[string]any{
		"tool": map[string]any{"type": "string", "minLength": 1},
		"args": map[string]any{"type": "object"},
	}, "required": []string{"tool", "args"}, "additionalProperties": false,
}

// RegisterDeprecatedTool retains execution and recovery while excluding discovery and pins.
func (c *SearchChain) RegisterDeprecatedTool(tool any) *SearchChain {
	c.RegisterTool(tool)
	name := tool.(gent.IndexableTool).Name()
	c.deprecated[name] = true
	c.indexableTools = c.indexableTools[:len(c.indexableTools)-1]
	return c
}

// SearchDefinition returns current discoverable definitions. Historical replies remain evidence,
// not new runtime bindings. Configure registration before concurrent calls.
func (c *SearchChain) SearchDefinition(ctx *gent.ExecutionContext) (
	*gent.ToolSearchDefinition, error,
) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.initialized {
		return nil, errors.New("SearchChain must be initialized")
	}
	var tools []any
	for _, tool := range c.tools {
		if !c.deprecated[tool.(gent.IndexableTool).Name()] {
			tools = append(tools, tool)
		}
	}
	definitions, err := nativeDefinitions(tools, c.printOutputSchema)
	if err != nil {
		return nil, err
	}
	var pins []string
	for _, name := range c.pinnedToolNames {
		if !c.deprecated[name] {
			pins = append(pins, name)
		}
	}
	return &gent.ToolSearchDefinition{Tools: definitions, PinnedNames: pins,
		LocalSearch: c.searchDefinition()}, nil
}

func (c *SearchChain) searchDefinition() *gent.ToolDefinition {
	hint := buildDomainSummary(c.indexableTools)
	if c.hintType == SearchHintSimpleList {
		hint = buildSimpleList(c.indexableTools)
	}
	var engines strings.Builder
	for _, engine := range c.engines {
		fmt.Fprintf(&engines, "\n%s: %s", engine.Id(), engine.SearchGuidance())
	}
	return &gent.ToolDefinition{Type: "function", Function: &gent.FunctionDefinition{
		Name: searchToolName, Parameters: c.searchToolSchema,
		Description: "Search the current tools. " +
			"Use execute_tool with the returned name and arguments.\n" +
			hint + "\nSearch engines:" + engines.String(),
	}}
}

func (c *SearchChain) NativeTools(ctx *gent.ExecutionContext) ([]*gent.ToolDefinition, error) {
	definition, err := c.SearchDefinition(ctx)
	if err != nil {
		return nil, err
	}
	tools := []*gent.ToolDefinition{definition.LocalSearch, {Type: "function",
		Function: &gent.FunctionDefinition{Name: executeToolName, Parameters: sharedExecutionSchema,
			Description: "Execute one selected tool. Its args must satisfy the schema from tool search."}}}
	pinned := map[string]bool{}
	for _, name := range definition.PinnedNames {
		pinned[name] = true
	}
	for _, tool := range definition.Tools {
		if pinned[tool.Function.Name] {
			tools = append(tools, tool)
		}
	}
	return tools, nil
}

// Search performs a repeatable local catalog read. Accepted replies stay in history; an unsaved
// search can be repeated. This permission never applies to domain tool calls or PTC programs.
func (c *SearchChain) Search(ctx *gent.ExecutionContext,
	request *gent.ToolSearchRequest,
) (*gent.ToolSearchResult, error) {
	c.mu.RLock()
	engine, pageSize, initialized := c.engineMap[request.QueryType], c.pageSize, c.initialized
	c.mu.RUnlock()
	if !initialized {
		return nil, errors.New("SearchChain must be initialized")
	}
	if engine == nil {
		return &gent.ToolSearchResult{Status: gent.ToolInvalidArguments,
			Instructions: "query_type must name an available search engine."}, nil
	}
	started := time.Now()
	result, err := engine.Search(ctx.Context(), request.Query)
	if err == nil && result == nil {
		err = errors.New("search engine returned no result")
	}
	if err == nil {
		err = gent.ValidateToolExecution(&gent.ToolExecutionResponse{Status: result.Status})
	}
	trace := &gent.ToolSearchTrace{Query: request.Query, QueryType: request.QueryType,
		Flow: "shared_local_search", Duration: time.Since(started)}
	if err != nil {
		trace.Error = gent.SaveError(err)
		ctx.RecordToolSearch(trace)
		return nil, err
	}
	if result.Status != gent.ToolExecuted {
		ctx.RecordToolSearch(trace)
		return result, nil
	}
	var names []string
	for _, name := range result.Names {
		if c.toolMap[name] != nil && !c.deprecated[name] {
			names = append(names, name)
		}
	}
	page := max(request.Page, 1)
	if pageSize < 1 {
		return nil, errors.New("search page size must be positive")
	}
	start := len(names)
	if page <= len(names)/pageSize+min(len(names)%pageSize, 1) {
		start = (page - 1) * pageSize
	}
	selected := names[start:min(start+pageSize, len(names))]
	trace.Names = selected
	ctx.RecordToolSearch(trace)
	instructions := result.Instructions
	if len(selected) == 0 {
		instructions = joinText(instructions, c.noResultsMessage)
	}
	instructions = joinText(instructions, fmt.Sprintf("Page %d; %d matching tools.", page, len(names)))
	return &gent.ToolSearchResult{
		Status: gent.ToolExecuted, Names: selected, Instructions: instructions,
	}, nil
}

func (c *SearchChain) executeParsed(ctx *gent.ExecutionContext, calls []*parsedCall,
	format gent.TextFormat,
) ([]*gent.ToolCallRecord, error) {
	registry := &Registry{tools: c.tools, toolMap: c.toolMap, schemaMap: c.schemaMap}
	var records []*gent.ToolCallRecord
	for _, call := range calls {
		selected := registry
		if call.Name == searchToolName || call.Name == executeToolName {
			selected = New()
			selected.RegisterTool(c.localSearchTool(ctx, func(names []string) { call.discovered = names }))
			selected.RegisterTool(gent.DefineTool(executeToolName,
				func(context.Context, gent.ToolExecuteRequest) (*gent.ToolExecutionResponse, error) {
					return &gent.ToolExecutionResponse{Status: gent.ToolInvalidArguments,
						Instructions: "args must be an object and tool must select an available target."}, nil
				}).ParameterSchema(sharedExecutionSchema).MustBuild())
		}
		result, err := selected.executeCall(ctx, call)
		if err != nil {
			return records, err
		}
		records = append(records, result)
	}
	return records, nil
}

func (c *SearchChain) localSearchTool(ctx *gent.ExecutionContext,
	discovered func([]string),
) gent.Tool[gent.ToolSearchRequest] {
	return gent.DefineTool(searchToolName, func(_ context.Context, request gent.ToolSearchRequest) (
		*gent.ToolExecutionResponse, error,
	) {
		result, err := c.Search(ctx, &request)
		if err != nil {
			return nil, err
		}
		response := &gent.ToolExecutionResponse{Status: result.Status, Instructions: result.Instructions}
		if result.Status != gent.ToolExecuted {
			return response, nil
		}
		discovered(result.Names)
		var tools []any
		for _, name := range result.Names {
			tools = append(tools, c.toolMap[name])
		}
		definitions, err := nativeDefinitions(tools, c.printOutputSchema)
		if err != nil {
			return nil, err
		}
		response.Parts, err = gent.NewParts().Data(definitions).Build()
		return response, err
	}).ParameterSchema(c.searchToolSchema).MustBuild()
}

func (c *SearchChain) GetToolSchema(name string) *schema.Schema {
	if name == searchToolName {
		return c.compiledSearchSchema
	}
	if name == executeToolName {
		return schema.MustCompile(sharedExecutionSchema)
	}
	return c.schemaMap[name]
}

func joinText(first, next string) string {
	if first == "" {
		return next
	}
	if next == "" {
		return first
	}
	return first + "\n\n" + next
}

var _ gent.SearchableToolChain = (*SearchChain)(nil)
