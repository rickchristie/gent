package gent

import "context"

// IndexableTool provides metadata for tool search indexing.
//
// Tool embeds this metadata view. DefineTool supplies empty defaults and builder methods for
// each value. Search engines consume this view without needing a tool's generic input/output.
//
// # Example
//
//	type MyTool struct{}
//
//	func (t *MyTool) Name() string        { return "lookup_order" }
//	func (t *MyTool) Description() string { return "Look up order details" }
//	func (t *MyTool) Domain() string      { return "Orders" }
//	func (t *MyTool) Categories() []string { return []string{"lookup", "orders"} }
//	func (t *MyTool) Keywords() []string  { return []string{"order", "tracking"} }
//	func (t *MyTool) SyntheticQueries() []string {
//	    return []string{"find order by ID", "check order status"}
//	}
type IndexableTool interface {
	// Name returns the tool's identifier. Same method as Tool.Name().
	Name() string

	// Description returns a human-readable description. Same method
	// as Tool.Description().
	Description() string

	// Domain returns the high-level domain this tool belongs to
	// (e.g., "Customer", "Billing", "Communication").
	Domain() string

	// Categories returns classification categories for the tool
	// (e.g., "lookup", "mutation", "email").
	Categories() []string

	// Keywords returns searchable keywords for indexing.
	Keywords() []string

	// SyntheticQueries returns example queries that would match
	// this tool. These help search engines find the tool for
	// natural language queries.
	SyntheticQueries() []string
}

// ToolSearcher indexes tools and searches them by query.
//
// Search engines are pluggable backends for toolchain.SearchChain.
// Each engine provides a different search strategy (e.g., BM25
// full-text search, regex pattern matching).
//
// # Contract
//
//   - Search returns ALL matching tool names ranked by relevance.
//     SearchChain handles pagination over results.
//   - Expected query failures use Status and authored Instructions.
//   - Unexpected Go errors remain private and use safe execution steering.
type ToolSearcher interface {
	// Id returns the unique identifier for this engine.
	// Used as the query_type value in search requests.
	Id() string

	// SearchGuidance returns instructions for the LLM on how
	// to write effective queries for this engine.
	SearchGuidance() string

	// IndexAll indexes all provided tools. Can be called multiple
	// times to re-index.
	IndexAll(tools []IndexableTool) error

	// Search finds tools matching the query, returning tool names
	// ranked by relevance. Returns all matches (pagination is
	// handled by the caller).
	Search(ctx context.Context, query string) (*ToolSearchResult, error)
}

// ToolSearchResult contains ranked names and authored model guidance. Executed with no names
// means no matches. InvalidArguments can explain query syntax that JSON Schema cannot check.
// Names are meaningful only for Executed; raw backend errors never become model guidance.
type ToolSearchResult struct {
	Status       ToolStatus
	Names        []string
	Instructions string
}

// ToolSearchRequest selects a local engine and an optional page. Page zero selects page one.
type ToolSearchRequest struct {
	Query     string `json:"query"`
	QueryType string `json:"query_type"`
	Page      int    `json:"page,omitempty"`
}

// ToolExecuteRequest selects one shared execution target. Gent validates that target's schema.
type ToolExecuteRequest struct {
	Tool string         `json:"tool"`
	Args map[string]any `json:"args"`
}

// SearchableToolChain supplies shared local discovery independently of its search engine.
// The runtime catalog comes from current bindings, never from historical model definitions.
type SearchableToolChain interface {
	ToolChain
	SearchDefinition(*ExecutionContext) (*ToolSearchDefinition, error)
	Search(*ExecutionContext, *ToolSearchRequest) (*ToolSearchResult, error)
}

// ToolSearchDefinition is a consistent read-only view of discoverable definitions and pins.
// LocalSearch describes the shared search function. Definitions do not register executable code.
type ToolSearchDefinition struct {
	Tools       []*ToolDefinition
	PinnedNames []string
	LocalSearch *ToolDefinition
}
