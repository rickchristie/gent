package search

import (
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search/query"
)

// BleveAdapter bridges a domain document type to Bleve's indexing and query system.
//
// The adapter handles three concerns:
//   - Mapping: defines Bleve's index schema (which fields exist, how they're analyzed)
//   - Convert: transforms a domain document into the shape Bleve expects
//   - Query: builds a Bleve query from raw query text, including field-specific boosting
//
// BleveIndex calls Query() to get a Bleve query, then wraps it in a SearchRequest with the
// caller's topK. The adapter controls query structure and boosting; BleveIndex controls
// pagination.
//
// # Snippet Generation
//
// BleveIndex uses Bleve's built-in highlighting to populate SearchResult.Snippet. If the
// adapter's Mapping does not configure a highlighter or the query doesn't produce highlights,
// the document ID is used as the snippet fallback.
//
// # Example for tools
//
//	type ToolBleveAdapter struct{}
//
//	func (a *ToolBleveAdapter) Mapping() mapping.IndexMapping { ... }
//
//	func (a *ToolBleveAdapter) Convert(tool IndexableTool) (any, error) {
//	    return map[string]string{
//	        "name":        tool.Name(),
//	        "description": tool.Description(),
//	        "keywords":    strings.Join(tool.Keywords(), " "),
//	    }, nil
//	}
//
//	func (a *ToolBleveAdapter) Query(q string) (query.Query, error) {
//	    nameMatch := bleve.NewMatchQuery(q)
//	    nameMatch.SetField("name")
//	    nameMatch.SetBoost(10.0)
//	    descMatch := bleve.NewMatchQuery(q)
//	    descMatch.SetField("description")
//	    return bleve.NewDisjunctionQuery(nameMatch, descMatch), nil
//	}
type BleveAdapter[Doc any] interface {
	// Mapping returns the Bleve index mapping defining the schema.
	// Called once during BleveIndex initialization.
	Mapping() mapping.IndexMapping

	// Convert transforms a domain document into a Bleve-indexable value. The returned
	// value's fields must match the names in the Mapping.
	Convert(doc Doc) (any, error)

	// Query builds a Bleve query from raw query text. This is where field-specific boosting,
	// fuzzy matching, and query structure are defined. BleveIndex wraps the returned query
	// in a SearchRequest with the caller's topK — the adapter should not set result size.
	Query(queryText string) (query.Query, error)
}
