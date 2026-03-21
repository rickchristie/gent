package toolchain

import (
	"context"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/search"
)

// IndexToolSearchEngine implements [gent.ToolSearchEngine] by delegating to a
// [search.SearchIndex]. It bridges gent's tool search interface with the generic search
// package, allowing any SearchIndex implementation (FlatIndex, BleveIndex, FusedIndex) to
// serve as a tool search backend.
//
// This is a thin adapter — all search logic lives in the search package. The engine converts
// between gent's IndexableTool-based API and the search package's generic API.
//
// # Usage
//
//	// Semantic search via FlatIndex
//	embedder, _ := search.NewOnnxEmbedder(cfg)
//	flatIdx := search.NewFlatIndex[gent.IndexableTool](&toolchain.ToolChunkAdapter{}, embedder)
//	engine := toolchain.NewIndexToolSearchEngine("semantic", flatIdx)
//
//	// Hybrid BM25 + semantic via FusedIndex
//	bleveIdx, _ := search.NewBleveIndex[gent.IndexableTool](&toolchain.ToolBleveAdapter{})
//	fusedIdx := search.NewFusedIndex[gent.IndexableTool](fuser,
//	    map[string]search.SearchIndex[gent.IndexableTool]{"bm25": bleveIdx, "semantic": flatIdx},
//	    map[string]int{"bm25": 20, "semantic": 20},
//	)
//	engine := toolchain.NewIndexToolSearchEngine("hybrid", fusedIdx)
type IndexToolSearchEngine struct {
	id             string
	index          search.SearchIndex[gent.IndexableTool]
	searchGuidance string
	topK           int
}

const defaultIndexSearchGuidance = "Use natural language queries to search for tools. " +
	"Describe what you need in plain language. Examples: \"look up customer billing\", " +
	"\"send notification to customer\", \"cancel or modify a reservation\""

// NewIndexToolSearchEngine creates a ToolSearchEngine backed by a SearchIndex.
// The id is used as the query_type value in search requests (e.g., "semantic", "hybrid").
func NewIndexToolSearchEngine(
	id string, index search.SearchIndex[gent.IndexableTool],
) *IndexToolSearchEngine {
	return &IndexToolSearchEngine{
		id:             id,
		index:          index,
		searchGuidance: defaultIndexSearchGuidance,
		topK:           100, // return enough results for SearchJSON pagination
	}
}

// WithSearchGuidance sets custom search guidance text for the LLM.
func (e *IndexToolSearchEngine) WithSearchGuidance(guidance string) *IndexToolSearchEngine {
	e.searchGuidance = guidance
	return e
}

// WithTopK sets the maximum number of results to retrieve from the underlying index.
// Default: 100 (high because SearchJSON handles pagination over the full result set).
func (e *IndexToolSearchEngine) WithTopK(topK int) *IndexToolSearchEngine {
	e.topK = topK
	return e
}

func (e *IndexToolSearchEngine) Id() string             { return e.id }
func (e *IndexToolSearchEngine) SearchGuidance() string { return e.searchGuidance }

// IndexAll indexes all tools by calling Swap on the underlying SearchIndex. This atomically
// replaces the entire index contents, which is correct for the SearchJSON.Initialize() flow
// where all tools are registered before IndexAll is called.
func (e *IndexToolSearchEngine) IndexAll(tools []gent.IndexableTool) error {
	docs := make(map[string]gent.IndexableTool, len(tools))
	for _, tool := range tools {
		docs[tool.Name()] = tool
	}
	return e.index.Swap(context.Background(), docs)
}

// Search returns tool names ranked by relevance from the underlying SearchIndex.
func (e *IndexToolSearchEngine) Search(ctx context.Context, query string) ([]string, error) {
	results, err := e.index.Search(ctx, query, e.topK)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(results))
	for i, r := range results {
		names[i] = r.Id
	}
	return names, nil
}

// Compile-time check.
var _ gent.ToolSearchEngine = (*IndexToolSearchEngine)(nil)
