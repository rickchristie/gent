package search

import (
	"context"
	"fmt"
)

// FusedIndex combines multiple [SearchIndex] implementations using a [Fuser].
//
// Each sub-index is registered with a name that corresponds to entries in the Fuser's
// weight/normalization config and the topK overrides.
//
// Document lifecycle (Add/Remove/Swap) is forwarded to ALL sub-indices. Each sub-index uses
// its own adapter to convert the Doc into its native format.
//
// Search fans out to all sub-indices (each with its own topK for overfetching), collects
// results, passes them to the Fuser, and returns the fused ranking.
//
// Safe for concurrent use (delegates to sub-index thread safety).
type FusedIndex[Doc any] struct {
	indexes    map[string]SearchIndex[Doc]
	topKConfig map[string]int
	fuser      Fuser
}

// NewFusedIndex creates a FusedIndex that combines the given sub-indices using the fuser.
// topKConfig sets per-source overfetch limits (how many results to request from each sub-index
// before fusion). If a source is missing from topKConfig, it defaults to 4x the caller's topK.
func NewFusedIndex[Doc any](
	fuser Fuser, indexes map[string]SearchIndex[Doc], topKConfig map[string]int,
) *FusedIndex[Doc] {
	return &FusedIndex[Doc]{indexes: indexes, topKConfig: topKConfig, fuser: fuser}
}

// Search fans out to all sub-indices and fuses the results.
// TODO: parallel fanout — at our scale sequential is <5ms, but this is an easy optimization.
func (f *FusedIndex[Doc]) Search(
	ctx context.Context, query string, topK int,
) ([]SearchResult, error) {
	results := make(map[string][]SearchResult, len(f.indexes))
	for name, index := range f.indexes {
		sourceTopK := f.topKConfig[name]
		if sourceTopK == 0 {
			sourceTopK = topK * 4
		}
		indexResults, err := index.Search(ctx, query, sourceTopK)
		if err != nil {
			return nil, fmt.Errorf("search: %s search failed: %w", name, err)
		}
		results[name] = indexResults
	}
	return f.fuser.Fuse(results, topK)
}

// Add forwards the document to all sub-indices. Fails fast on first error.
func (f *FusedIndex[Doc]) Add(ctx context.Context, id string, doc Doc) error {
	for name, index := range f.indexes {
		if err := index.Add(ctx, id, doc); err != nil {
			return fmt.Errorf("search: add to %s failed: %w", name, err)
		}
	}
	return nil
}

// Remove forwards the deletion to all sub-indices. Fails fast on first error.
func (f *FusedIndex[Doc]) Remove(id string) error {
	for name, index := range f.indexes {
		if err := index.Remove(id); err != nil {
			return fmt.Errorf("search: remove from %s failed: %w", name, err)
		}
	}
	return nil
}

// Swap forwards the atomic replacement to all sub-indices. Fails fast on first error.
// Note: if the first sub-index succeeds and the second fails, state is inconsistent.
// Use Swap again with the correct data to recover.
func (f *FusedIndex[Doc]) Swap(ctx context.Context, docs map[string]Doc) error {
	for name, index := range f.indexes {
		if err := index.Swap(ctx, docs); err != nil {
			return fmt.Errorf("search: swap on %s failed: %w", name, err)
		}
	}
	return nil
}

// Compile-time check.
var _ SearchIndex[any] = (*FusedIndex[any])(nil)
