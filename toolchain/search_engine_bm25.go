package toolchain

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/rickchristie/gent"
)

// BM25SearchEngine searches tools using BM25 full-text search
// via Bleve's in-memory index.
//
// Fields are indexed with different boosts:
//   - High (3.0): name, description
//   - Medium (2.0): keywords, categories
//   - Lower (1.0): domain, synthetic_queries
type BM25SearchEngine struct {
	index bleve.Index
}

// bleveToolDoc is the document structure indexed by Bleve.
type bleveToolDoc struct {
	Name             string `json:"name"`
	Description      string `json:"description"`
	Domain           string `json:"domain"`
	Keywords         string `json:"keywords"`
	Categories       string `json:"categories"`
	SyntheticQueries string `json:"synthetic_queries"`
}

// NewBM25SearchEngine creates a new BM25-based search engine.
func NewBM25SearchEngine() *BM25SearchEngine {
	return &BM25SearchEngine{}
}

// Id returns "bm25".
func (e *BM25SearchEngine) Id() string {
	return "bm25"
}

// SearchGuidance returns instructions for writing BM25 queries.
func (e *BM25SearchEngine) SearchGuidance() string {
	return "Use natural language queries for full-text " +
		"search across tool names, descriptions, " +
		"keywords, and categories. " +
		"Examples: \"order status\", " +
		"\"send notification to customer\", " +
		"\"billing payment\""
}

// IndexAll indexes all provided tools. Closes any existing
// index and creates a new one.
func (e *BM25SearchEngine) IndexAll(
	tools []gent.IndexableTool,
) error {
	// Close existing index if present (supports re-indexing)
	if e.index != nil {
		if err := e.index.Close(); err != nil {
			return fmt.Errorf("failed to close index: %w", err)
		}
		e.index = nil
	}

	indexMapping := e.buildMapping()
	index, err := bleve.NewMemOnly(indexMapping)
	if err != nil {
		return fmt.Errorf(
			"failed to create index: %w", err,
		)
	}

	for _, tool := range tools {
		doc := bleveToolDoc{
			Name:        tool.Name(),
			Description: tool.Description(),
			Domain:      tool.Domain(),
			Keywords:    strings.Join(tool.Keywords(), " "),
			Categories: strings.Join(
				tool.Categories(), " ",
			),
			SyntheticQueries: strings.Join(
				tool.SyntheticQueries(), " ",
			),
		}
		if err := index.Index(tool.Name(), doc); err != nil {
			// Close on error to avoid leaked index
			_ = index.Close()
			return fmt.Errorf(
				"failed to index tool %q: %w",
				tool.Name(), err,
			)
		}
	}

	e.index = index
	return nil
}

// buildMapping creates the Bleve index mapping.
// All fields use standard text analysis for BM25 scoring.
func (e *BM25SearchEngine) buildMapping() mapping.IndexMapping {
	textField := bleve.NewTextFieldMapping()
	textField.Analyzer = "standard"
	textField.Store = false
	textField.IncludeTermVectors = false

	toolDocMapping := bleve.NewDocumentMapping()
	toolDocMapping.AddFieldMappingsAt("name", textField)
	toolDocMapping.AddFieldMappingsAt(
		"description", textField,
	)
	toolDocMapping.AddFieldMappingsAt(
		"keywords", textField,
	)
	toolDocMapping.AddFieldMappingsAt(
		"categories", textField,
	)
	toolDocMapping.AddFieldMappingsAt("domain", textField)
	toolDocMapping.AddFieldMappingsAt(
		"synthetic_queries", textField,
	)

	indexMapping := bleve.NewIndexMapping()
	indexMapping.DefaultMapping = toolDocMapping

	return indexMapping
}

// Search finds tools matching the query using BM25 scoring.
func (e *BM25SearchEngine) Search(
	ctx context.Context,
	query string,
) ([]string, error) {
	if e.index == nil {
		return nil, errors.New(
			"search engine not initialized: " +
				"call IndexAll first",
		)
	}

	matchQuery := bleve.NewMatchQuery(query)
	req := bleve.NewSearchRequest(matchQuery)
	req.Size = 10000 // Return all matches

	results, err := e.index.SearchInContext(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	names := make([]string, len(results.Hits))
	for i, hit := range results.Hits {
		names[i] = hit.ID
	}
	return names, nil
}

// Compile-time check that BM25SearchEngine implements
// SearchEngine.
var _ gent.SearchEngine = (*BM25SearchEngine)(nil)
