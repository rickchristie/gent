package testutil

import (
	"github.com/rickchristie/gent"
)

// IndexableToolFunc wraps a [gent.ToolFunc] with
// [gent.IndexableTool] metadata. This allows existing tools
// to be used with [toolchain.SearchJSON] without modifying
// the original tool definitions.
//
// # Example
//
//	wrapped := testutil.NewIndexableToolFunc(
//	    myTool,
//	    "Orders",
//	    []string{"lookup", "orders"},
//	    []string{"order", "tracking"},
//	    []string{"find my order"},
//	)
type IndexableToolFunc[I, O any] struct {
	*gent.ToolFunc[I, O]
	domain           string
	categories       []string
	keywords         []string
	syntheticQueries []string
}

// NewIndexableToolFunc creates a new IndexableToolFunc that
// wraps the given ToolFunc with search metadata.
func NewIndexableToolFunc[I, O any](
	tool *gent.ToolFunc[I, O],
	domain string,
	categories []string,
	keywords []string,
	syntheticQueries []string,
) *IndexableToolFunc[I, O] {
	return &IndexableToolFunc[I, O]{
		ToolFunc:         tool,
		domain:           domain,
		categories:       categories,
		keywords:         keywords,
		syntheticQueries: syntheticQueries,
	}
}

// Domain returns the high-level domain this tool belongs to.
func (t *IndexableToolFunc[I, O]) Domain() string {
	return t.domain
}

// Categories returns classification categories.
func (t *IndexableToolFunc[I, O]) Categories() []string {
	return t.categories
}

// Keywords returns searchable keywords.
func (t *IndexableToolFunc[I, O]) Keywords() []string {
	return t.keywords
}

// SyntheticQueries returns example queries that should match
// this tool.
func (t *IndexableToolFunc[I, O]) SyntheticQueries() []string {
	return t.syntheticQueries
}
