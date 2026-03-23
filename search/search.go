// Package search provides generic search infrastructure for Go applications.
//
// It is a leaf package with no knowledge of LLM agents, tools, or policies.
// Consumers (like gent's ToolSearchToolChain) use the generic interfaces with
// domain-specific adapters.
//
// # Core Types
//
//   - [SearchIndex] — generic interface for storing and retrieving documents by relevance.
//   - [FlatIndex] — brute-force vector search via an [Embedder] and [ChunkAdapter].
//   - [BleveIndex] — BM25 full-text search via Bleve and [BleveAdapter].
//   - [FusedIndex] — composes multiple SearchIndex implementations via a [Fuser].
//
// # Usage
//
//	// Create a FlatIndex for semantic search
//	flatIdx := search.NewFlatIndex[MyDoc](chunkAdapter, embedder)
//
//	// Create a BleveIndex for BM25 search
//	bleveIdx, err := search.NewBleveIndex[MyDoc](bleveAdapter)
//
//	// Compose into a FusedIndex
//	fusedIdx := search.NewFusedIndex[MyDoc](fuser,
//	    map[string]search.SearchIndex[MyDoc]{"bm25": bleveIdx, "semantic": flatIdx},
//	    map[string]int{"bm25": 20, "semantic": 20},
//	)
//
//	fusedIdx.Add(ctx, "doc-1", myDoc)
//	results, err := fusedIdx.Search(ctx, "my query", 5)
package search

import "context"

// SearchResult is the output of any SearchIndex.Search() call.
type SearchResult struct {
	// Id uniquely identifies the matched document.
	Id string

	// Score is the relevance score. Semantics depend on the index type:
	//   - FlatIndex: cosine similarity in [-1.0, 1.0] (practically [0.0, 1.0])
	//   - BleveIndex: BM25 score (unbounded, query-dependent)
	//   - FusedIndex: fused score, depends on Fuser implementation
	Score float64

	// Snippet is text that can be shown for context. For FlatIndex: the best-matching chunk
	// text. For BleveIndex: highlighted fragment, or document ID if highlights aren't configured.
	Snippet string

	// Metadata contains additional information about the search result. For FusedIndex, the
	// WeightedLinearFuser populates per-source scores:
	//   - "bm25_raw": raw BM25 score before normalization
	//   - "bm25_normalized": BM25 score after normalization
	//   - "bm25_weighted": BM25 contribution (weight × normalized)
	//   - "semantic_raw": raw cosine similarity score
	//   - "semantic_weighted": semantic contribution (weight × raw)
	//
	// These enable debugging and analysis of search quality.
	Metadata map[string]any
}

// SearchIndex stores documents of type Doc and retrieves them by relevance.
//
// Implementations must be safe for concurrent use.
//
// The generic type parameter Doc allows different consumers to index different document types
// (e.g., tools, policies) through the same interface, with type-specific adapters handling
// conversion to each backend's native format.
type SearchIndex[Doc any] interface {
	// Search returns the top-K most relevant documents for the query.
	Search(ctx context.Context, query string, topK int) ([]SearchResult, error)

	// Add indexes a single document. If a document with the same ID already exists,
	// it is replaced.
	Add(ctx context.Context, id string, doc Doc) error

	// Remove deletes a document by ID.
	Remove(id string) error

	// Swap atomically replaces the entire index contents. All existing documents are
	// removed and replaced with the provided set. This is the preferred method for bulk
	// updates (e.g., when the tool registry changes) because it avoids inconsistent
	// intermediate states.
	Swap(ctx context.Context, docs map[string]Doc) error
}
