package search

import (
	"context"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// BleveIndex snippet tests
// ============================================================================

// snippetBleveAdapter stores content with highlighting enabled.
type snippetBleveAdapter struct{}

func (a *snippetBleveAdapter) Mapping() mapping.IndexMapping {
	text := bleve.NewTextFieldMapping()
	text.Analyzer = "standard"
	text.Store = true
	text.IncludeInAll = true

	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("content", text)
	indexMapping := bleve.NewIndexMapping()
	indexMapping.DefaultMapping = docMapping
	return indexMapping
}

func (a *snippetBleveAdapter) Convert(doc string) (any, error) {
	return map[string]string{"content": doc}, nil
}

func (a *snippetBleveAdapter) Query(queryText string) (query.Query, error) {
	q := bleve.NewMatchQuery(queryText)
	q.SetField("content")
	return q, nil
}

func TestBleveIndex_SnippetContainsHighlightedTerms(t *testing.T) {
	idx, err := NewBleveIndex(&snippetBleveAdapter{})
	require.NoError(t, err)
	defer idx.Close()

	ctx := context.Background()
	require.NoError(t, idx.Add(ctx, "billing",
		"billing payment invoice ledger entries"))
	require.NoError(t, idx.Add(ctx, "notify",
		"send notification email sms message"))

	results, err := idx.Search(ctx, "payment invoice", 5)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "billing", results[0].Id)

	// Bleve wraps matched terms in <mark> tags.
	assert.Equal(t,
		`billing <mark>payment</mark> <mark>invoice</mark> ledger entries`,
		results[0].Snippet)
}

func TestBleveIndex_SnippetFallsBackToIDWhenNoHighlight(t *testing.T) {
	// Adapter with Store=false — Bleve can't produce highlights without stored fields.
	noStoreAdapter := &noStoreBleveAdapter{}
	idx, err := NewBleveIndex(noStoreAdapter)
	require.NoError(t, err)
	defer idx.Close()

	ctx := context.Background()
	require.NoError(t, idx.Add(ctx, "my-doc-id", "some searchable content here"))

	results, err := idx.Search(ctx, "searchable", 1)
	require.NoError(t, err)
	require.Len(t, results, 1)

	// No highlights available — should fall back to document ID.
	assert.Equal(t, "my-doc-id", results[0].Snippet)
}

// noStoreBleveAdapter uses Store=false so Bleve can't produce highlights.
type noStoreBleveAdapter struct{}

func (a *noStoreBleveAdapter) Mapping() mapping.IndexMapping {
	text := bleve.NewTextFieldMapping()
	text.Analyzer = "standard"
	text.Store = false

	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("content", text)
	indexMapping := bleve.NewIndexMapping()
	indexMapping.DefaultMapping = docMapping
	return indexMapping
}

func (a *noStoreBleveAdapter) Convert(doc string) (any, error) {
	return map[string]string{"content": doc}, nil
}

func (a *noStoreBleveAdapter) Query(queryText string) (query.Query, error) {
	q := bleve.NewMatchQuery(queryText)
	q.SetField("content")
	return q, nil
}

func TestBleveIndex_SnippetWithMultipleMatchedTerms(t *testing.T) {
	idx, err := NewBleveIndex(&snippetBleveAdapter{})
	require.NoError(t, err)
	defer idx.Close()

	ctx := context.Background()
	require.NoError(t, idx.Add(ctx, "policy",
		"cancellation policy allows full refund within thirty days of purchase"))

	results, err := idx.Search(ctx, "cancellation refund", 5)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "policy", results[0].Id)

	assert.Equal(t,
		"<mark>cancellation</mark> policy allows full <mark>refund</mark> within thirty days of purchase",
		results[0].Snippet)
}

// ============================================================================
// FlatIndex snippet tests
// ============================================================================

func TestFlatIndex_SnippetIsChunkText(t *testing.T) {
	// When a document has one chunk, the snippet is the full chunk text.
	embedder := newMockEmbedder(map[string][]float32{
		"query":           l2norm([]float32{1, 0, 0}),
		"billing entries": l2norm([]float32{0.95, 0.05, 0}),
	})

	idx := NewFlatIndex(&singleChunkAdapter{}, embedder)
	ctx := context.Background()
	require.NoError(t, idx.Add(ctx, "billing", "billing entries"))

	results, err := idx.Search(ctx, "query", 5)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "billing entries", results[0].Snippet)
}

func TestFlatIndex_SnippetIsBestMatchingChunk(t *testing.T) {
	// When a document has multiple chunks, the snippet is the text of the
	// highest-scoring chunk — not just its ID or any other chunk.
	embedder := newMockEmbedder(map[string][]float32{
		"find refund policy":    l2norm([]float32{1, 0, 0}),
		"section about billing": l2norm([]float32{0.1, 0.9, 0}),
		"section about refunds": l2norm([]float32{0.9, 0.1, 0}),
		"section about shipping": l2norm([]float32{0, 0, 1}),
	})

	adapter := &multiChunkAdapter{}
	idx := NewFlatIndex(adapter, embedder)
	ctx := context.Background()

	require.NoError(t, idx.Add(ctx, "policy-doc",
		"section about billing|section about refunds|section about shipping"))

	results, err := idx.Search(ctx, "find refund policy", 5)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "policy-doc", results[0].Id)
	assert.Equal(t, "section about refunds", results[0].Snippet)
}

func TestFlatIndex_SnippetUpdatesOnReplace(t *testing.T) {
	embedder := newMockEmbedder(map[string][]float32{
		"query":       l2norm([]float32{1, 0, 0}),
		"old content": l2norm([]float32{0.5, 0.5, 0}),
		"new content": l2norm([]float32{0.9, 0.1, 0}),
	})

	idx := NewFlatIndex(&singleChunkAdapter{}, embedder)
	ctx := context.Background()

	require.NoError(t, idx.Add(ctx, "doc", "old content"))
	require.NoError(t, idx.Add(ctx, "doc", "new content"))

	results, err := idx.Search(ctx, "query", 5)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "new content", results[0].Snippet)
}

func TestFlatIndex_SnippetAfterSwap(t *testing.T) {
	embedder := newMockEmbedder(map[string][]float32{
		"query":           l2norm([]float32{1, 0, 0}),
		"swapped content": l2norm([]float32{0.9, 0.1, 0}),
	})

	idx := NewFlatIndex(&singleChunkAdapter{}, embedder)
	ctx := context.Background()

	require.NoError(t, idx.Add(ctx, "old", "old content"))
	require.NoError(t, idx.Swap(ctx, map[string]string{"new": "swapped content"}))

	results, err := idx.Search(ctx, "query", 5)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "swapped content", results[0].Snippet)
}

// ============================================================================
// FusedIndex snippet tests
// ============================================================================

func TestFusedIndex_SnippetComesFromHighestContributor(t *testing.T) {
	// The fused snippet should come from the source with the highest weighted contribution.
	// With 0.3 BM25 + 0.7 semantic, semantic's snippet wins when its contribution is higher.
	bm25 := &mockSearchIndex{results: []SearchResult{
		{Id: "billing", Score: 5.0, Snippet: "billing <mark>payment</mark> invoice",
			Metadata: map[string]any{TheoreticalMaxKey: 10.0}},
	}}
	semantic := &mockSearchIndex{results: []SearchResult{
		{Id: "billing", Score: 0.9, Snippet: "billing payment invoice ledger entries"},
	}}

	fuser := &WeightedLinearFuser{
		Weights:          map[string]float64{"bm25": 0.3, "semantic": 0.7},
		NormalizeSources: map[string]bool{"bm25": true, "semantic": false},
	}

	idx := NewFusedIndex(fuser,
		map[string]SearchIndex[string]{"bm25": bm25, "semantic": semantic},
		map[string]int{"bm25": 20, "semantic": 20},
	)

	results, err := idx.Search(context.Background(), "payment", 5)
	require.NoError(t, err)
	require.Len(t, results, 1)

	// BM25 normalized = 5/10 = 0.5, contribution: 0.3 * 0.5 = 0.15
	// Semantic contribution: 0.7 * 0.9 = 0.63 ← higher
	// Snippet should come from semantic (higher contribution).
	assert.Equal(t, "billing payment invoice ledger entries", results[0].Snippet)
}

func TestFusedIndex_SnippetFromBM25WhenItDominates(t *testing.T) {
	// When BM25 has a much higher normalized score, its snippet wins.
	bm25 := &mockSearchIndex{results: []SearchResult{
		{Id: "tool", Score: 28.5, Snippet: "<mark>exact_tool_name</mark>"},
	}}
	semantic := &mockSearchIndex{results: []SearchResult{
		{Id: "tool", Score: 0.3, Snippet: "some vague semantic match"},
	}}

	fuser := &WeightedLinearFuser{
		Weights:          map[string]float64{"bm25": 0.3, "semantic": 0.7},
		NormalizeSources: map[string]bool{"bm25": true, "semantic": false},
	}

	idx := NewFusedIndex(fuser,
		map[string]SearchIndex[string]{"bm25": bm25, "semantic": semantic},
		map[string]int{"bm25": 20, "semantic": 20},
	)

	results, err := idx.Search(context.Background(), "exact_tool_name", 5)
	require.NoError(t, err)
	require.Len(t, results, 1)

	// BM25 contribution: 0.3 * 1.0 = 0.3
	// Semantic contribution: 0.7 * 0.3 = 0.21
	// BM25 wins → its snippet is used.
	assert.Equal(t, "<mark>exact_tool_name</mark>", results[0].Snippet)
}

func TestFusedIndex_SnippetFromOnlyMatchingSource(t *testing.T) {
	// Document only found by semantic search — snippet comes from semantic.
	bm25 := &mockSearchIndex{results: []SearchResult{}}
	semantic := &mockSearchIndex{results: []SearchResult{
		{Id: "tool", Score: 0.85, Snippet: "the semantic chunk text"},
	}}

	fuser := &WeightedLinearFuser{
		Weights:          map[string]float64{"bm25": 0.3, "semantic": 0.7},
		NormalizeSources: map[string]bool{"bm25": true, "semantic": false},
	}

	idx := NewFusedIndex(fuser,
		map[string]SearchIndex[string]{"bm25": bm25, "semantic": semantic},
		map[string]int{"bm25": 20, "semantic": 20},
	)

	results, err := idx.Search(context.Background(), "natural language query", 5)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "the semantic chunk text", results[0].Snippet)
}
