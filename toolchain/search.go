package toolchain

import (
	"context"

	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/schema"
)

// SearchHintType controls how the tool registry summary
// is presented in the native search definition.
type SearchHintType int

const (
	// SearchHintDomainCategories shows domain/category
	// summaries with tool counts. Best for large tool sets.
	// This is the default (zero value).
	SearchHintDomainCategories SearchHintType = iota

	// SearchHintSimpleList shows a flat list of all tool
	// names. Best for small tool sets (<20 tools).
	SearchHintSimpleList
)

// SearchChain implements [gent.ToolChain] for dynamic tool
// discovery. Instead of showing all tools upfront, it exposes
// a single "Tool Registry Search" tool that the LLM uses to
// discover tools on demand.
//
// Definitions are selected from active history, so separate runs do not share discovery state.
//
// # Creating and Configuring
//
//	tc := toolchain.NewSearch(
//	    toolchain.SearchHintDomainCategories,
//	).
//	    WithPageSize(5).
//	    RegisterEngine(toolchain.NewBM25ToolSearcher()).
//	    RegisterEngine(toolchain.NewRegexToolSearcher())
//
// # Registering Tools
//
// Tools must implement both [gent.Tool] and
// [gent.IndexableTool]:
//
//	tc.RegisterTool(orderLookupTool).
//	    RegisterTool(emailTool).
//	    RegisterTool(billingTool)
//
// # Initialization
//
// Call Initialize() after registering all tools and engines:
//
//	if err := tc.Initialize(); err != nil {
//	    log.Fatal(err)
//	}
//
// # Usage with Agent
//
//	agent := react.NewAgent(model).
//	    WithToolChain(tc)
type SearchChain struct {
	mu sync.RWMutex

	// Tool registry (same pattern as Registry toolchain)
	tools     []any
	toolMap   map[string]any
	schemaMap map[string]*schema.Schema

	// IndexableTool metadata for search
	indexableTools []gent.IndexableTool

	// Search engines
	engines   []gent.ToolSearcher
	engineMap map[string]gent.ToolSearcher

	// Config
	hintType          SearchHintType
	searchType        SearchType
	pinnedToolNames   []string
	pageSize          int
	noResultsMessage  string
	recovery          map[string]gent.RecoveryMessageProvider
	printOutputSchema bool

	// Computed by Initialize()
	initialized          bool
	searchToolSchema     map[string]any
	compiledSearchSchema *schema.Schema
}

// NewSearch creates a new SearchChain toolchain with
// the given hint type and default settings.
func NewSearch(
	hintType SearchHintType,
) *SearchChain {
	return &SearchChain{
		printOutputSchema: true,
		recovery:          make(map[string]gent.RecoveryMessageProvider),
		hintType:          hintType,
		tools:             make([]any, 0),
		toolMap:           make(map[string]any),
		schemaMap:         make(map[string]*schema.Schema),
		engines:           make([]gent.ToolSearcher, 0),
		engineMap:         make(map[string]gent.ToolSearcher),
		pageSize:          3,
		noResultsMessage: "No tools found matching " +
			"your query. Try different keywords or " +
			"a broader search.",
	}
}

// WithPageSize sets the number of tools per search page.
func (c *SearchChain) WithPageSize(
	size int,
) *SearchChain {
	c.pageSize = size
	return c
}

// WithNoResultsMessage sets the message returned when no
// tools match a search query.
func (c *SearchChain) WithNoResultsMessage(
	msg string,
) *SearchChain {
	c.noResultsMessage = msg
	return c
}

// WithOutputSchema enables printing output schemas alongside
// input schemas in tool definitions.
func (c *SearchChain) WithOutputSchema(
	enabled bool,
) *SearchChain {
	c.printOutputSchema = enabled
	return c
}

// WithSearchType sets the search result mode. Default: [Search].
// [SearchGet] returns only name + description in search results
// and provides a separate get_tool_schema tool for fetching
// full schemas.
func (c *SearchChain) WithSearchType(
	st SearchType,
) *SearchChain {
	c.searchType = st
	return c
}

// RegisterEngine adds a search engine. Call before
// Initialize().
func (c *SearchChain) RegisterEngine(
	engine gent.ToolSearcher,
) *SearchChain {
	c.engines = append(c.engines, engine)
	c.engineMap[engine.Id()] = engine
	return c
}

// Pin marks a tool to always be displayed in the
// native tool list with its full definition.
// The tool must be registered before Initialize() is
// called; validation happens at Initialize() time.
//
// Pinned tools are still indexed by search engines.
func (c *SearchChain) Pin(
	toolName string,
) *SearchChain {
	c.pinnedToolNames = append(
		c.pinnedToolNames, toolName,
	)
	return c
}

// RegisterTool adds a tool to the chain. The tool must
// implement both Tool[I, O] and IndexableTool.
//
// Panics if:
//   - tool doesn't implement Tool[I, O] (invalid type)
//   - tool doesn't implement IndexableTool
//   - a tool with the same name is already registered
func (c *SearchChain) RegisterTool(
	tool any, options ...gent.ToolOption,
) gent.ToolChain {
	meta, err := GetToolMeta(tool)
	if err != nil {
		panic(fmt.Sprintf(
			"SearchChain.RegisterTool: "+
				"invalid tool type: %v", err,
		))
	}

	// Must implement IndexableTool
	indexable, ok := tool.(gent.IndexableTool)
	if !ok {
		panic(fmt.Sprintf(
			"SearchChain.RegisterTool: tool %q "+
				"must implement IndexableTool",
			meta.Name(),
		))
	}

	// Check duplicate
	if _, exists := c.toolMap[meta.Name()]; exists {
		panic(fmt.Sprintf(
			"SearchChain.RegisterTool: "+
				"duplicate tool name %q",
			meta.Name(),
		))
	}

	config := gent.ToolOptions{}
	for _, option := range options {
		option(&config)
	}
	c.recovery[meta.Name()] = config.RecoveryMessage
	c.tools = append(c.tools, tool)
	c.toolMap[meta.Name()] = tool
	c.indexableTools = append(c.indexableTools, indexable)

	// Compile schema for validation
	if rawSchema := meta.Schema(); rawSchema != nil {
		compiled, err := schema.Compile(rawSchema)
		if err != nil {
			panic(fmt.Errorf("register tool %q schema: %w", meta.Name(), err))
		}
		if compiled != nil {
			c.schemaMap[meta.Name()] = compiled
		}
	}

	return c
}

// Initialize indexes all tools in search engines and builds
// prompt data. Must be called after all tools and engines
// are registered.
//
// Can be called multiple times to re-index.
func (c *SearchChain) Initialize() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.engines) == 0 {
		return errors.New(
			"SearchChain.Initialize: " +
				"at least one search engine is required",
		)
	}

	// Index tools in each engine
	for _, eng := range c.engines {
		if err := eng.IndexAll(c.indexableTools); err != nil {
			return fmt.Errorf(
				"SearchChain.Initialize: engine %q "+
					"IndexAll failed: %w",
				eng.Id(), err,
			)
		}
	}

	// Build search tool schema with dynamic engine enum
	c.searchToolSchema = buildSearchToolSchema(c.engines)

	// Compile search tool schema for validation
	compiled, err := schema.Compile(c.searchToolSchema)
	if err != nil {
		return fmt.Errorf(
			"SearchChain.Initialize: failed to "+
				"compile search schema: %w", err,
		)
	}
	c.compiledSearchSchema = compiled

	for _, name := range c.pinnedToolNames {
		if _, exists := c.toolMap[name]; !exists {
			return fmt.Errorf("pinned tool %q is not registered", name)
		}
	}

	c.initialized = true
	return nil
}

func (c *SearchChain) executeParsed(
	execCtx *gent.ExecutionContext, calls []*parsedCall, textFormat gent.TextFormat,
) (*gent.ToolChainResult, error) {
	if textFormat == nil {
		panic("textFormat must not be nil")
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	ctx := execCtx.Context()
	results := make([]*gent.ToolCallResult, len(calls))
	for i, call := range calls {
		results[i] = &gent.ToolCallResult{
			Name: call.Name,
			Args: call.Args,
			Hash: ToolCallResultHash(call.Name, call.Args),
		}
	}

	seenTools := make(map[string]bool)

	saved, ok, err := execCtx.CheckpointState("gent:search_seen")
	if err != nil {
		return nil, err
	}
	if ok {
		var valid bool
		seenTools, valid = saved.(map[string]bool)
		if !valid {
			return nil, fmt.Errorf("invalid SearchChain checkpoint state %T", saved)
		}
	}

	for i, call := range calls {
		if call.Name == searchToolName {
			c.executeSearch(
				ctx, execCtx, call, results[i],
				textFormat, seenTools,
			)
		} else if call.Name == getSchemaToolName &&
			c.searchType == SearchGet {
			c.executeGetSchema(
				execCtx, call, results[i], textFormat,
			)
		} else {
			registry := &Registry{tools: c.tools, toolMap: c.toolMap, schemaMap: c.schemaMap}
			output, err := registry.executeParsed(execCtx, []*parsedCall{call}, textFormat)
			if err != nil {
				return nil, err
			}
			results[i] = output.Results[0]
		}
	}

	if err := execCtx.SetCheckpointState("gent:search_seen", seenTools); err != nil {
		return nil, err
	}

	return &gent.ToolChainResult{Results: results}, nil
}

// executeSearch handles the built-in search tool call.
// seenTools tracks tool names that have already had their
// full definition printed in a prior search within the same
// Execute call. Duplicate tools get an abbreviated reference.
func (c *SearchChain) executeSearch(
	ctx context.Context,
	execCtx *gent.ExecutionContext,
	call *parsedCall,
	result *gent.ToolCallResult,
	textFormat gent.TextFormat,
	seenTools map[string]bool,
) {
	// Validate search args
	if c.compiledSearchSchema != nil {
		if err := c.compiledSearchSchema.Validate(
			call.Args,
		); err != nil {
			result.Error = err
			result.Text = formatSectionText(
				textFormat, call.Name,
				fmt.Sprintf("Error: %v", err),
			)
			publishFailedToolAttempt(execCtx, call.Name, call.Args, err, result)
			return
		}
	}

	query, _ := call.Args["query"].(string)
	queryType, _ := call.Args["query_type"].(string)
	page := 1
	if p, ok := call.Args["page"].(float64); ok && p > 1 {
		page = int(p)
	}

	// Publish BeforeToolCall
	argsToUse := call.Args
	var toolCallId string
	var toolCallSource string

	beforeEvent := execCtx.PublishBeforeToolCall(
		call.Name, call.Args,
	)
	toolCallId = beforeEvent.ToolCallId
	toolCallSource = beforeEvent.Source
	result.ToolCallId = toolCallId
	result.AttemptId = execCtx.AttemptId()
	result.Input = beforeEvent.Args
	result.SummaryCaptured = true
	if err := execCtx.RecordingError(); err != nil {
		result.Error = err
		return
	}
	if err := execCtx.Context().Err(); err != nil {
		result.Error = err
		return
	}
	if beforeArgs, ok :=
		beforeEvent.Args.(map[string]any); ok {
		argsToUse = beforeArgs
		query, _ = argsToUse["query"].(string)
		queryType, _ = argsToUse["query_type"].(string)
		if p, ok :=
			argsToUse["page"].(float64); ok && p > 1 {
			page = int(p)
		}
	}

	startTime := time.Now()

	// Look up engine
	engine, ok := c.engineMap[queryType]
	if !ok {
		err := fmt.Errorf(
			"unknown query_type %q", queryType,
		)
		result.Error = err
		result.Text = formatSectionText(
			textFormat, call.Name,
			fmt.Sprintf("Error: %v", err),
		)
		duration := time.Since(startTime)

		execCtx.PublishAfterToolCall(
			call.Name, argsToUse,
			result, duration, err,
			gent.WithToolCallId(toolCallId),
			gent.WithToolCallSource(toolCallSource),
		)

		return
	}

	// Execute search
	searchResults, err := engine.Search(ctx, query)
	duration := time.Since(startTime)

	if err != nil {
		result.Error = err
		result.Text = formatSectionText(
			textFormat, call.Name,
			fmt.Sprintf("Error: %v", err),
		)

		execCtx.PublishAfterToolCall(
			call.Name, argsToUse,
			result, duration, err,
			gent.WithToolCallId(toolCallId),
			gent.WithToolCallSource(toolCallSource),
		)

		return
	}

	// Paginate results
	totalResults := len(searchResults)
	startIdx := (page - 1) * c.pageSize
	endIdx := startIdx + c.pageSize
	if startIdx >= totalResults {
		// Page beyond results
		result.Output = c.noResultsMessage
		result.Text = formatSectionText(
			textFormat, call.Name,
			c.noResultsMessage,
		)

		execCtx.Stats().ResetGauge(
			gent.SGToolCallsErrorConsecutive,
		)
		execCtx.Stats().ResetGauge(
			gent.SGToolCallsErrorConsecutiveFor +
				gent.StatKey(call.Name),
		)
		execCtx.PublishAfterToolCall(
			call.Name, argsToUse,
			result, duration, nil,
			gent.WithToolCallId(toolCallId),
			gent.WithToolCallSource(toolCallSource),
		)

		return
	}
	if endIdx > totalResults {
		endIdx = totalResults
	}

	pageNames := searchResults[startIdx:endIdx]
	if c.searchType == Search {
		result.DiscoveredTools = append([]string(nil), pageNames...)
	}

	// Split matched tools into new (full def) vs duplicate
	// (abbreviated reference for tools already printed).
	var newTools []any
	var dupNames []string
	for _, name := range pageNames {
		tool, exists := c.toolMap[name]
		if !exists {
			continue
		}
		if seenTools[name] {
			dupNames = append(dupNames, name)
		} else {
			newTools = append(newTools, tool)
		}
	}

	// Mark all tools from this result as seen
	for _, name := range pageNames {
		seenTools[name] = true
	}

	// Format output
	var output strings.Builder
	if c.searchType == SearchGet {
		output.WriteString(formatToolBrief(newTools))
	} else {
		output.WriteString(formatToolDefinitions(
			newTools, c.printOutputSchema,
		))
	}
	for _, name := range dupNames {
		output.WriteString(formatToolDedup(name))
	}

	totalPages := (totalResults + c.pageSize - 1) / c.pageSize
	fmt.Fprintf(
		&output,
		"\nShowing page %d of %d (%d total results)",
		page, totalPages, totalResults,
	)

	outputStr := output.String()
	result.Output = outputStr
	result.Text = formatSectionText(
		textFormat, call.Name, outputStr,
	)

	// Reset consecutive error gauges on success

	execCtx.Stats().ResetGauge(
		gent.SGToolCallsErrorConsecutive,
	)
	execCtx.Stats().ResetGauge(
		gent.SGToolCallsErrorConsecutiveFor +
			gent.StatKey(call.Name),
	)
	execCtx.PublishAfterToolCall(
		call.Name, argsToUse,
		result, duration, nil,
		gent.WithToolCallId(toolCallId),
		gent.WithToolCallSource(toolCallSource),
	)

}

// executeGetSchema handles the built-in get_tool_schema tool.
// Returns the full definition (schema, policy, output schema)
// for a single tool by name.
func (c *SearchChain) executeGetSchema(
	execCtx *gent.ExecutionContext,
	call *parsedCall,
	result *gent.ToolCallResult,
	textFormat gent.TextFormat,
) {
	startTime := time.Now()
	toolName, _ := call.Args["tool_name"].(string)
	var toolCallId string
	var toolCallSource string

	beforeEvent := execCtx.PublishBeforeToolCall(call.Name, call.Args)
	toolCallId = beforeEvent.ToolCallId
	toolCallSource = beforeEvent.Source
	result.ToolCallId = toolCallId
	result.AttemptId = execCtx.AttemptId()
	result.Input = beforeEvent.Args
	if args, ok := beforeEvent.Args.(map[string]any); ok {
		call.Args = args
		toolName, _ = args["tool_name"].(string)
	}
	result.SummaryCaptured = true
	if err := execCtx.RecordingError(); err != nil {
		result.Error = err
		return
	}
	if err := execCtx.Context().Err(); err != nil {
		result.Error = err
		return
	}

	if toolName == "" {
		err := fmt.Errorf("tool_name is required")
		result.Error = err
		result.Text = formatSectionText(
			textFormat, call.Name,
			fmt.Sprintf("Error: %v", err),
		)

		execCtx.PublishAfterToolCall(
			call.Name, call.Args, result, 0, err,
			gent.WithToolCallId(toolCallId),
			gent.WithToolCallSource(toolCallSource),
		)

		return
	}

	tool, ok := c.toolMap[toolName]
	if !ok {
		err := fmt.Errorf("tool not found: %s", toolName)
		result.Error = err
		result.Text = formatSectionText(
			textFormat, call.Name,
			fmt.Sprintf("Error: %v", err),
		)

		execCtx.PublishAfterToolCall(
			call.Name, call.Args, result, 0, err,
			gent.WithToolCallId(toolCallId),
			gent.WithToolCallSource(toolCallSource),
		)

		return
	}

	duration := time.Since(startTime)
	outputStr := formatToolDefinitions(
		[]any{tool}, c.printOutputSchema,
	)
	result.DiscoveredTools = []string{toolName}

	result.Output = outputStr
	result.Text = formatSectionText(
		textFormat, call.Name, outputStr,
	)

	execCtx.Stats().ResetGauge(
		gent.SGToolCallsErrorConsecutive,
	)
	execCtx.Stats().ResetGauge(
		gent.SGToolCallsErrorConsecutiveFor +
			gent.StatKey(call.Name),
	)
	execCtx.PublishAfterToolCall(
		call.Name, call.Args,
		result, duration, nil,
		gent.WithToolCallId(toolCallId),
		gent.WithToolCallSource(toolCallSource),
	)

}

// DeduplicateSummary returns an abbreviated observation text
// for a tool call whose output duplicates an earlier call.
//
// For built-in tools (search_tool_registry, get_tool_schema),
// it returns a summary directly. For regular tools, it
// delegates to the tool's DeduplicateSummary via reflection.
func (c *SearchChain) DeduplicateSummary(
	result *gent.ToolCallResult,
) string {
	if result.Name == searchToolName {
		return c.searchDedupSummary(result)
	}
	if result.Name == getSchemaToolName {
		return c.getSchemaDedupSummary(result)
	}
	tool, ok := c.toolMap[result.Name]
	if !ok {
		return ""
	}
	return CallDeduplicateSummaryReflect(
		tool, result.Input, result.Output,
	)
}

// searchDedupSummary generates a dedup summary for the
// built-in search_tool_registry tool.
func (c *SearchChain) searchDedupSummary(
	result *gent.ToolCallResult,
) string {
	query, _ := result.Args["query"].(string)
	outputStr, _ := result.Output.(string)
	if outputStr == "" {
		return ""
	}
	// Count tool names in the output by scanning for
	// "- toolname:" pattern lines.
	var toolNames []string
	for _, line := range strings.Split(outputStr, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- ") {
			name := strings.TrimPrefix(line, "- ")
			if idx := strings.Index(name, ":"); idx > 0 {
				toolNames = append(
					toolNames, name[:idx],
				)
			}
		}
	}
	if len(toolNames) == 0 {
		return fmt.Sprintf(
			"Searched for: %q. "+
				"Full results available below.",
			query,
		)
	}
	return fmt.Sprintf(
		"Searched for: %q. Found %d tools: [%s]. "+
			"Full results available below.",
		query, len(toolNames),
		strings.Join(toolNames, ", "),
	)
}

// getSchemaDedupSummary generates a dedup summary for
// the built-in get_tool_schema tool.
func (c *SearchChain) getSchemaDedupSummary(
	result *gent.ToolCallResult,
) string {
	toolName, _ := result.Args["tool_name"].(string)
	if toolName == "" {
		return ""
	}
	return fmt.Sprintf(
		"Loaded schema for: %s. "+
			"Full schemas available below.",
		toolName,
	)
}

// GetToolSchema returns the compiled schema for the
// named tool, or nil if not found. Thread-safe.
func (c *SearchChain) GetToolSchema(
	name string,
) *schema.Schema {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.schemaMap[name]
}

// Compile-time check that SearchChain implements ToolChain.
var _ gent.ToolChain = (*SearchChain)(nil)
