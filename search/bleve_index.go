package search

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/blevesearch/bleve/v2"
	blevesearch "github.com/blevesearch/bleve/v2/search"
)

// BleveIndex provides BM25 full-text search via Bleve.
//
// Documents are converted to Bleve's format via a [BleveAdapter], which controls the index
// schema, document conversion, and query construction. BleveIndex handles the Bleve lifecycle,
// concurrency, and conversion of Bleve results to [SearchResult].
//
// # Snippet Generation
//
// BleveIndex attempts to extract a highlighted snippet from Bleve's search results. If no
// highlights are available (e.g., the query or mapping doesn't support highlighting), the
// document ID is returned as the snippet fallback.
//
// Safe for concurrent use via sync.RWMutex (wrapping Bleve's own thread safety for Swap).
type BleveIndex[Doc any] struct {
	adapter BleveAdapter[Doc]
	index   bleve.Index
	mu      sync.RWMutex // protects index replacement in Swap
}

// NewBleveIndex creates a BleveIndex with the given adapter. The adapter's Mapping() is used
// to initialize the in-memory Bleve index.
func NewBleveIndex[Doc any](adapter BleveAdapter[Doc]) (*BleveIndex[Doc], error) {
	idx, err := bleve.NewMemOnly(adapter.Mapping())
	if err != nil {
		return nil, fmt.Errorf("search: bleve index creation failed: %w", err)
	}
	return &BleveIndex[Doc]{adapter: adapter, index: idx}, nil
}

// Search returns the top-K most relevant documents for the query. The adapter's Query() builds
// the Bleve query with field-specific boosting; BleveIndex controls the result count via topK.
func (b *BleveIndex[Doc]) Search(
	ctx context.Context, queryText string, topK int,
) ([]SearchResult, error) {
	q, err := b.adapter.Query(queryText)
	if err != nil {
		return nil, fmt.Errorf("search: bleve query build failed: %w", err)
	}

	req := bleve.NewSearchRequestOptions(q, topK, 0, false)
	req.Highlight = bleve.NewHighlight()

	b.mu.RLock()
	bleveResults, err := b.index.Search(req)
	b.mu.RUnlock()
	if err != nil {
		return nil, fmt.Errorf("search: bleve search failed: %w", err)
	}

	results := make([]SearchResult, 0, len(bleveResults.Hits))
	for _, hit := range bleveResults.Hits {
		snippet := extractSnippet(hit)
		results = append(results, SearchResult{
			Id: hit.ID, Score: hit.Score, Snippet: snippet,
		})
	}
	return results, nil
}

// Add indexes a single document. The adapter's Convert() transforms the domain document into
// Bleve's format. If a document with the same ID exists, Bleve replaces it.
func (b *BleveIndex[Doc]) Add(ctx context.Context, id string, doc Doc) error {
	bleveDoc, err := b.adapter.Convert(doc)
	if err != nil {
		return fmt.Errorf("search: bleve adapter convert failed: %w", err)
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.index.Index(id, bleveDoc)
}

// Remove deletes a document by ID.
func (b *BleveIndex[Doc]) Remove(id string) error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.index.Delete(id)
}

// Swap atomically replaces the entire index. A new Bleve index is built from the provided
// documents, then swapped in. The old index is closed.
func (b *BleveIndex[Doc]) Swap(ctx context.Context, docs map[string]Doc) error {
	newIdx, err := bleve.NewMemOnly(b.adapter.Mapping())
	if err != nil {
		return fmt.Errorf("search: bleve index creation failed: %w", err)
	}

	for id, doc := range docs {
		bleveDoc, err := b.adapter.Convert(doc)
		if err != nil {
			newIdx.Close()
			return fmt.Errorf("search: bleve adapter convert failed for %s: %w", id, err)
		}
		if err := newIdx.Index(id, bleveDoc); err != nil {
			newIdx.Close()
			return fmt.Errorf("search: bleve index failed for %s: %w", id, err)
		}
	}

	b.mu.Lock()
	old := b.index
	b.index = newIdx
	b.mu.Unlock()

	return old.Close()
}

// Close closes the underlying Bleve index.
func (b *BleveIndex[Doc]) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.index.Close()
}

// extractSnippet returns the first highlighted fragment from Bleve results, or the document
// ID as fallback when no highlights are available.
func extractSnippet(hit *blevesearch.DocumentMatch) string {
	for _, fragments := range hit.Fragments {
		for _, fragment := range fragments {
			trimmed := strings.TrimSpace(fragment)
			if trimmed != "" {
				return trimmed
			}
		}
	}
	return hit.ID
}

// Compile-time check.
var _ SearchIndex[any] = (*BleveIndex[any])(nil)
