package toolchain

import (
	"errors"
	"fmt"
	"sync"

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
// Only shared search, shared execution, and pinned definitions are advertised. Search replies
// describe target schemas without promoting them into native definitions.
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
	tools      []any
	toolMap    map[string]any
	schemaMap  map[string]*schema.Schema
	deprecated map[string]bool

	// IndexableTool metadata for search
	indexableTools []gent.IndexableTool

	// Search engines
	engines   []gent.ToolSearcher
	engineMap map[string]gent.ToolSearcher

	// Config
	hintType          SearchHintType
	pinnedToolNames   []string
	pageSize          int
	noResultsMessage  string
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
		hintType:          hintType,
		tools:             make([]any, 0),
		toolMap:           make(map[string]any),
		schemaMap:         make(map[string]*schema.Schema),
		deprecated:        map[string]bool{},
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

// RegisterTool adds a tool with its search metadata. It must implement the full Tool[I].
//
// Panics if:
//   - tool doesn't implement Tool[I] (invalid type)
//   - a tool with the same name is already registered
func (c *SearchChain) RegisterTool(tool any) gent.ToolChain {
	meta, err := GetToolMeta(tool)
	if err != nil {
		panic(fmt.Sprintf(
			"SearchChain.RegisterTool: "+
				"invalid tool type: %v", err,
		))
	}

	indexable := tool.(gent.IndexableTool)

	if meta.Name() == searchToolName || meta.Name() == executeToolName {
		panic(fmt.Sprintf("SearchChain.RegisterTool: reserved tool name %q", meta.Name()))
	}

	// Check duplicate
	if _, exists := c.toolMap[meta.Name()]; exists {
		panic(fmt.Sprintf(
			"SearchChain.RegisterTool: "+
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

	if c.pageSize < 1 {
		return errors.New("SearchChain.Initialize: page size must be positive")
	}
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
