package gent

import "context"

// IndexableTool provides metadata for tool search indexing.
//
// Tools that need to be discovered via search should implement
// both Tool[I, O] and IndexableTool on the same receiver.
// The Name() and Description() methods are shared between both
// interfaces.
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

// SearchEngine indexes tools and searches them by query.
//
// Search engines are pluggable backends for ToolSearchToolChain.
// Each engine provides a different search strategy (e.g., BM25
// full-text search, regex pattern matching).
//
// # Contract
//
//   - Search returns ALL matching tool names ranked by relevance.
//     The ToolSearchToolChain handles pagination over results.
//   - Error messages from Search are surfaced to the LLM, so they
//     should be descriptive (e.g., "invalid regex: ...").
type SearchEngine interface {
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
	Search(ctx context.Context, query string) ([]string, error)
}
