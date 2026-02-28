package toolchain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/schema"
)

// SearchHintType controls how the tool registry summary
// is presented in the system prompt.
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

// SearchJSON implements [gent.ToolChain] for dynamic tool
// discovery. Instead of showing all tools upfront, it exposes
// a single "Tool Registry Search" tool that the LLM uses to
// discover tools on demand.
//
// This follows Anthropic's Tool Search Tool pattern, which
// reduces token usage by ~85% and improves accuracy from 49%
// to 74% when dealing with 20+ tools.
//
// # Creating and Configuring
//
//	tc := toolchain.NewSearchJSON(
//	    toolchain.SearchHintDomainCategories,
//	).
//	    WithPageSize(5).
//	    RegisterEngine(toolchain.NewBM25SearchEngine()).
//	    RegisterEngine(toolchain.NewRegexSearchEngine())
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
type SearchJSON struct {
	mu sync.RWMutex

	// TextSection
	sectionName string // default "action"

	// Tool registry (same pattern as JSON toolchain)
	tools     []any
	toolMap   map[string]any
	schemaMap map[string]*schema.Schema

	// IndexableTool metadata for search
	indexableTools []gent.IndexableTool

	// Search engines
	engines   []gent.SearchEngine
	engineMap map[string]gent.SearchEngine

	// Config
	hintType         SearchHintType
	pageSize         int
	noResultsMessage string

	// Computed by Initialize()
	initialized          bool
	searchToolPrompt     string
	searchToolSchema     map[string]any
	compiledSearchSchema *schema.Schema
}

// NewSearchJSON creates a new SearchJSON toolchain with
// the given hint type and default settings.
func NewSearchJSON(
	hintType SearchHintType,
) *SearchJSON {
	return &SearchJSON{
		sectionName: "action",
		hintType:    hintType,
		tools:       make([]any, 0),
		toolMap:     make(map[string]any),
		schemaMap:   make(map[string]*schema.Schema),
		engines:     make([]gent.SearchEngine, 0),
		engineMap:   make(map[string]gent.SearchEngine),
		pageSize:    3,
		noResultsMessage: "No tools found matching " +
			"your query. Try different keywords or " +
			"a broader search.",
	}
}

// WithSectionName sets the section name.
func (c *SearchJSON) WithSectionName(
	name string,
) *SearchJSON {
	c.sectionName = name
	return c
}

// WithPageSize sets the number of tools per search page.
func (c *SearchJSON) WithPageSize(
	size int,
) *SearchJSON {
	c.pageSize = size
	return c
}

// WithNoResultsMessage sets the message returned when no
// tools match a search query.
func (c *SearchJSON) WithNoResultsMessage(
	msg string,
) *SearchJSON {
	c.noResultsMessage = msg
	return c
}

// RegisterEngine adds a search engine. Call before
// Initialize().
func (c *SearchJSON) RegisterEngine(
	engine gent.SearchEngine,
) *SearchJSON {
	c.engines = append(c.engines, engine)
	c.engineMap[engine.Id()] = engine
	return c
}

// Name returns the section identifier.
func (c *SearchJSON) Name() string {
	return c.sectionName
}

// Guidance returns format instructions for JSON tool calls.
func (c *SearchJSON) Guidance() string {
	var sb strings.Builder
	sb.WriteString("Call tools using JSON format:\n")
	sb.WriteString(
		`{"tool": "tool_name", "args": {...}}`,
	)
	sb.WriteString(
		"\n\nFor multiple parallel calls, use an array:\n",
	)
	sb.WriteString(
		`[{"tool": "tool1", "args": {...}}, ` +
			`{"tool": "tool2", "args": {...}}]`,
	)
	return sb.String()
}

// RegisterTool adds a tool to the chain. The tool must
// implement both Tool[I, O] and IndexableTool.
//
// Panics if:
//   - tool doesn't implement Tool[I, O] (invalid type)
//   - tool doesn't implement IndexableTool
//   - a tool with the same name is already registered
func (c *SearchJSON) RegisterTool(
	tool any,
) gent.ToolChain {
	meta, err := GetToolMeta(tool)
	if err != nil {
		panic(fmt.Sprintf(
			"SearchJSON.RegisterTool: "+
				"invalid tool type: %v", err,
		))
	}

	// Must implement IndexableTool
	indexable, ok := tool.(gent.IndexableTool)
	if !ok {
		panic(fmt.Sprintf(
			"SearchJSON.RegisterTool: tool %q "+
				"must implement IndexableTool",
			meta.Name(),
		))
	}

	// Check duplicate
	if _, exists := c.toolMap[meta.Name()]; exists {
		panic(fmt.Sprintf(
			"SearchJSON.RegisterTool: "+
				"duplicate tool name %q",
			meta.Name(),
		))
	}

	c.tools = append(c.tools, tool)
	c.toolMap[meta.Name()] = tool
	c.indexableTools = append(c.indexableTools, indexable)

	// Compile schema for validation
	if rawSchema := meta.Schema(); rawSchema != nil {
		compiled, err := schema.Compile(rawSchema)
		if err == nil && compiled != nil {
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
func (c *SearchJSON) Initialize() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.engines) == 0 {
		return errors.New(
			"SearchJSON.Initialize: " +
				"at least one search engine is required",
		)
	}

	// Index tools in each engine
	for _, eng := range c.engines {
		if err := eng.IndexAll(c.indexableTools); err != nil {
			return fmt.Errorf(
				"SearchJSON.Initialize: engine %q "+
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
			"SearchJSON.Initialize: failed to "+
				"compile search schema: %w", err,
		)
	}
	c.compiledSearchSchema = compiled

	// Build the prompt
	c.searchToolPrompt = buildSearchToolPrompt(
		c.indexableTools,
		c.engines,
		c.searchToolSchema,
		c.hintType,
	)

	c.initialized = true
	return nil
}

// AvailableToolsPrompt returns the pre-computed search tool
// prompt. Shows only the "Tool Registry Search" tool, not
// individual registered tools.
func (c *SearchJSON) AvailableToolsPrompt() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.searchToolPrompt
}

// ParseSection parses raw text content and returns
// []*gent.ToolCall. Identical logic to JSON.ParseSection.
func (c *SearchJSON) ParseSection(
	execCtx *gent.ExecutionContext,
	content string,
) (any, error) {
	result, err := c.doParse(content)
	if err != nil {
		if execCtx != nil {
			execCtx.PublishParseError(
				gent.ParseErrorTypeToolchain,
				content,
				err,
			)
		}
		return nil, err
	}

	if execCtx != nil {
		execCtx.Stats().ResetGauge(
			gent.SGToolchainParseErrorConsecutive,
		)
	}

	return result, nil
}

// doParse performs the actual JSON parsing logic. Identical
// to JSON.doParse.
func (c *SearchJSON) doParse(
	content string,
) ([]*gent.ToolCall, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return []*gent.ToolCall{}, nil
	}

	var calls []*gent.ToolCall

	if strings.HasPrefix(content, "[") {
		var rawCalls []struct {
			Tool string         `json:"tool"`
			Args map[string]any `json:"args"`
		}
		if err := json.Unmarshal(
			[]byte(content), &rawCalls,
		); err != nil {
			return nil, fmt.Errorf(
				"%w: %v", gent.ErrInvalidJSON, err,
			)
		}
		for _, rc := range rawCalls {
			if rc.Tool == "" {
				return nil, gent.ErrMissingToolName
			}
			calls = append(calls, &gent.ToolCall{
				Name: rc.Tool, Args: rc.Args,
			})
		}
	} else {
		var rawCall struct {
			Tool string         `json:"tool"`
			Args map[string]any `json:"args"`
		}
		if err := json.Unmarshal(
			[]byte(content), &rawCall,
		); err != nil {
			return nil, fmt.Errorf(
				"%w: %v", gent.ErrInvalidJSON, err,
			)
		}
		if rawCall.Tool == "" {
			return nil, gent.ErrMissingToolName
		}
		calls = append(calls, &gent.ToolCall{
			Name: rawCall.Tool, Args: rawCall.Args,
		})
	}

	return calls, nil
}

// Execute parses tool calls and executes them.
// Handles both the built-in search tool and regular tools.
//
// Panics if textFormat is nil.
func (c *SearchJSON) Execute(
	execCtx *gent.ExecutionContext,
	content string,
	textFormat gent.TextFormat,
) (*gent.ToolChainResult, error) {
	if textFormat == nil {
		panic("textFormat must not be nil")
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	var ctx context.Context
	if execCtx != nil {
		ctx = execCtx.Context()
	} else {
		ctx = context.Background()
	}

	parsed, err := c.ParseSection(execCtx, content)
	if err != nil {
		return nil, err
	}

	calls := parsed.([]*gent.ToolCall)
	raw := &gent.RawToolChainResult{
		Calls:   calls,
		Results: make([]*gent.RawToolCallResult, len(calls)),
		Errors:  make([]error, len(calls)),
	}

	var sections []gent.FormattedSection
	var allMedia []gent.ContentPart
	seenTools := make(map[string]bool)

	for i, call := range calls {
		if call.Name == searchToolName {
			c.executeSearch(
				ctx, execCtx, call, raw, i,
				&sections, seenTools,
			)
		} else {
			c.executeRegularTool(
				ctx, execCtx, call, raw, i,
				&sections, &allMedia,
			)
		}
	}

	return &gent.ToolChainResult{
		Text:  textFormat.FormatSections(sections),
		Media: allMedia,
		Raw:   raw,
	}, nil
}

// executeSearch handles the built-in search tool call.
// seenTools tracks tool names that have already had their
// full definition printed in a prior search within the same
// Execute call. Duplicate tools get an abbreviated reference.
func (c *SearchJSON) executeSearch(
	ctx context.Context,
	execCtx *gent.ExecutionContext,
	call *gent.ToolCall,
	raw *gent.RawToolChainResult,
	idx int,
	sections *[]gent.FormattedSection,
	seenTools map[string]bool,
) {
	// Validate search args
	if c.compiledSearchSchema != nil {
		if err := c.compiledSearchSchema.Validate(
			call.Args,
		); err != nil {
			raw.Errors[idx] = err
			*sections = append(
				*sections, gent.FormattedSection{
					Name: call.Name,
					Content: fmt.Sprintf(
						"Error: %v", err,
					),
				},
			)
			if execCtx != nil {
				execCtx.PublishAfterToolCall(
					call.Name, call.Args,
					nil, 0, err,
				)
			}
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
	if execCtx != nil {
		beforeEvent := execCtx.PublishBeforeToolCall(
			call.Name, call.Args,
		)
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
	}

	startTime := time.Now()

	// Look up engine
	engine, ok := c.engineMap[queryType]
	if !ok {
		err := fmt.Errorf(
			"unknown query_type %q", queryType,
		)
		raw.Errors[idx] = err
		*sections = append(
			*sections, gent.FormattedSection{
				Name:    call.Name,
				Content: fmt.Sprintf("Error: %v", err),
			},
		)
		duration := time.Since(startTime)
		if execCtx != nil {
			execCtx.PublishAfterToolCall(
				call.Name, argsToUse,
				nil, duration, err,
			)
		}
		return
	}

	// Execute search
	results, err := engine.Search(ctx, query)
	duration := time.Since(startTime)

	if err != nil {
		raw.Errors[idx] = err
		*sections = append(
			*sections, gent.FormattedSection{
				Name:    call.Name,
				Content: fmt.Sprintf("Error: %v", err),
			},
		)
		if execCtx != nil {
			execCtx.PublishAfterToolCall(
				call.Name, argsToUse,
				nil, duration, err,
			)
		}
		return
	}

	// Paginate results
	totalResults := len(results)
	startIdx := (page - 1) * c.pageSize
	endIdx := startIdx + c.pageSize
	if startIdx >= totalResults {
		// Page beyond results
		raw.Results[idx] = &gent.RawToolCallResult{
			Name:   call.Name,
			Output: c.noResultsMessage,
		}
		*sections = append(
			*sections, gent.FormattedSection{
				Name:    call.Name,
				Content: c.noResultsMessage,
			},
		)
		if execCtx != nil {
			execCtx.Stats().ResetGauge(
				gent.SGToolCallsErrorConsecutive,
			)
			execCtx.Stats().ResetGauge(
				gent.SGToolCallsErrorConsecutiveFor +
					gent.StatKey(call.Name),
			)
			execCtx.PublishAfterToolCall(
				call.Name, argsToUse,
				c.noResultsMessage, duration, nil,
			)
		}
		return
	}
	if endIdx > totalResults {
		endIdx = totalResults
	}

	pageNames := results[startIdx:endIdx]

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
	output.WriteString(formatToolDefinitions(newTools))
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
	raw.Results[idx] = &gent.RawToolCallResult{
		Name:   call.Name,
		Output: outputStr,
	}
	*sections = append(
		*sections, gent.FormattedSection{
			Name:    call.Name,
			Content: outputStr,
		},
	)

	// Reset consecutive error gauges on success
	if execCtx != nil {
		execCtx.Stats().ResetGauge(
			gent.SGToolCallsErrorConsecutive,
		)
		execCtx.Stats().ResetGauge(
			gent.SGToolCallsErrorConsecutiveFor +
				gent.StatKey(call.Name),
		)
		execCtx.PublishAfterToolCall(
			call.Name, argsToUse,
			outputStr, duration, nil,
		)
	}
}

// executeRegularTool handles regular registered tool calls.
// Identical flow to JSON.Execute per-tool logic.
func (c *SearchJSON) executeRegularTool(
	ctx context.Context,
	execCtx *gent.ExecutionContext,
	call *gent.ToolCall,
	raw *gent.RawToolChainResult,
	idx int,
	sections *[]gent.FormattedSection,
	allMedia *[]gent.ContentPart,
) {
	tool, ok := c.toolMap[call.Name]
	if !ok {
		raw.Errors[idx] = fmt.Errorf(
			"%w: %s", gent.ErrUnknownTool, call.Name,
		)
		*sections = append(
			*sections, gent.FormattedSection{
				Name: call.Name,
				Content: fmt.Sprintf(
					"Error: %v", raw.Errors[idx],
				),
			},
		)
		if execCtx != nil {
			execCtx.PublishAfterToolCall(
				call.Name, call.Args,
				nil, 0, raw.Errors[idx],
			)
		}
		return
	}

	// Validate args against schema
	if compiled, has := c.schemaMap[call.Name]; has {
		if err := compiled.Validate(call.Args); err != nil {
			raw.Errors[idx] = err
			*sections = append(
				*sections, gent.FormattedSection{
					Name: call.Name,
					Content: fmt.Sprintf(
						"Error: %v", err,
					),
				},
			)
			if execCtx != nil {
				execCtx.PublishAfterToolCall(
					call.Name, call.Args,
					nil, 0, err,
				)
			}
			return
		}
	}

	// Transform raw args to typed input
	typedInput, err := TransformArgsReflect(
		tool, call.Args,
	)
	if err != nil {
		raw.Errors[idx] = err
		*sections = append(
			*sections, gent.FormattedSection{
				Name: call.Name,
				Content: fmt.Sprintf(
					"Error: %v", err,
				),
			},
		)
		if execCtx != nil {
			execCtx.PublishAfterToolCall(
				call.Name, call.Args, nil, 0, err,
			)
		}
		return
	}

	// Publish BeforeToolCall (may modify args)
	inputToUse := typedInput
	if execCtx != nil {
		beforeEvent := execCtx.PublishBeforeToolCall(
			call.Name, typedInput,
		)
		inputToUse = beforeEvent.Args
	}

	startTime := time.Now()
	output, err := CallToolWithTypedInputReflect(
		ctx, tool, inputToUse,
	)
	duration := time.Since(startTime)

	if err != nil {
		raw.Errors[idx] = err
		*sections = append(
			*sections, gent.FormattedSection{
				Name: call.Name,
				Content: fmt.Sprintf(
					"Error: %v", err,
				),
			},
		)
	} else {
		// Successful tool call
		if execCtx != nil {
			execCtx.Stats().ResetGauge(
				gent.SGToolCallsErrorConsecutive,
			)
			execCtx.Stats().ResetGauge(
				gent.SGToolCallsErrorConsecutiveFor +
					gent.StatKey(call.Name),
			)
		}

		raw.Results[idx] = &gent.RawToolCallResult{
			Name:   output.Name,
			Output: output.Text,
		}

		jsonData, marshalErr := json.Marshal(output.Text)
		if marshalErr != nil {
			*sections = append(
				*sections, gent.FormattedSection{
					Name: call.Name,
					Content: "error: failed to " +
						"marshal output",
				},
			)
		} else {
			if output.Instructions != "" {
				*sections = append(
					*sections,
					gent.FormattedSection{
						Name: call.Name,
						Children: []gent.FormattedSection{
							{
								Name:    "result",
								Content: string(jsonData),
							},
							{
								Name:    "instructions",
								Content: output.Instructions,
							},
						},
					},
				)
			} else {
				*sections = append(
					*sections, gent.FormattedSection{
						Name:    call.Name,
						Content: string(jsonData),
					},
				)
			}
		}

		if len(output.Media) > 0 {
			*allMedia = append(
				*allMedia, output.Media...,
			)
		}
	}

	var outputVal any
	if output != nil {
		outputVal = output.Text
	}
	if execCtx != nil {
		execCtx.PublishAfterToolCall(
			call.Name, inputToUse,
			outputVal, duration, err,
		)
	}
}

// Compile-time check that SearchJSON implements ToolChain.
var _ gent.ToolChain = (*SearchJSON)(nil)
