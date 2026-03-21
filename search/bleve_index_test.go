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

// testBleveAdapter is a simple BleveAdapter for string documents.
type testBleveAdapter struct{}

func (a *testBleveAdapter) Mapping() mapping.IndexMapping {
	indexMapping := bleve.NewIndexMapping()
	text := bleve.NewTextFieldMapping()
	text.Analyzer = "standard"
	text.Store = true
	text.IncludeInAll = true

	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("content", text)
	indexMapping.DefaultMapping = docMapping
	return indexMapping
}

func (a *testBleveAdapter) Convert(doc string) (any, error) {
	return map[string]string{"content": doc}, nil
}

func (a *testBleveAdapter) Query(queryText string) (query.Query, error) {
	q := bleve.NewMatchQuery(queryText)
	q.SetField("content")
	return q, nil
}

func TestBleveIndex_SearchReturnsMatches(t *testing.T) {
	idx, err := NewBleveIndex(&testBleveAdapter{})
	require.NoError(t, err)
	defer idx.Close()

	ctx := context.Background()
	require.NoError(t, idx.Add(ctx, "billing", "billing payment invoice ledger"))
	require.NoError(t, idx.Add(ctx, "notify", "send notification email sms"))
	require.NoError(t, idx.Add(ctx, "checkout", "process checkout refund"))

	results, err := idx.Search(ctx, "payment invoice", 5)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(results), 1)
	assert.Equal(t, "billing", results[0].Id)
	assert.Greater(t, results[0].Score, 0.0)
}

func TestBleveIndex_SnippetFallsBackToID(t *testing.T) {
	// With standard highlight, Bleve may or may not produce fragments depending on the
	// analyzer. If no fragments, we fall back to document ID.
	idx, err := NewBleveIndex(&testBleveAdapter{})
	require.NoError(t, err)
	defer idx.Close()

	ctx := context.Background()
	require.NoError(t, idx.Add(ctx, "my-doc", "some content"))

	results, err := idx.Search(ctx, "content", 1)
	require.NoError(t, err)
	require.Len(t, results, 1)
	// Snippet should be non-empty — either a highlight or the document ID fallback.
	assert.NotEmpty(t, results[0].Snippet)
}

func TestBleveIndex_AddReplacesExisting(t *testing.T) {
	idx, err := NewBleveIndex(&testBleveAdapter{})
	require.NoError(t, err)
	defer idx.Close()

	ctx := context.Background()
	require.NoError(t, idx.Add(ctx, "doc", "old content alpha"))
	require.NoError(t, idx.Add(ctx, "doc", "new content beta"))

	// Searching for "alpha" should not find the old version.
	results, err := idx.Search(ctx, "alpha", 5)
	require.NoError(t, err)
	assert.Empty(t, results)

	// Searching for "beta" should find the new version.
	results, err = idx.Search(ctx, "beta", 5)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "doc", results[0].Id)
}

func TestBleveIndex_Remove(t *testing.T) {
	idx, err := NewBleveIndex(&testBleveAdapter{})
	require.NoError(t, err)
	defer idx.Close()

	ctx := context.Background()
	require.NoError(t, idx.Add(ctx, "doc", "searchable content"))
	require.NoError(t, idx.Remove("doc"))

	results, err := idx.Search(ctx, "searchable", 5)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestBleveIndex_Swap(t *testing.T) {
	idx, err := NewBleveIndex(&testBleveAdapter{})
	require.NoError(t, err)
	defer idx.Close()

	ctx := context.Background()
	require.NoError(t, idx.Add(ctx, "old", "old content alpha"))

	err = idx.Swap(ctx, map[string]string{"new": "new content beta"})
	require.NoError(t, err)

	// Old doc should be gone.
	results, err := idx.Search(ctx, "alpha", 5)
	require.NoError(t, err)
	assert.Empty(t, results)

	// New doc should be there.
	results, err = idx.Search(ctx, "beta", 5)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "new", results[0].Id)
}

func TestBleveIndex_EmptyIndex(t *testing.T) {
	idx, err := NewBleveIndex(&testBleveAdapter{})
	require.NoError(t, err)
	defer idx.Close()

	results, err := idx.Search(context.Background(), "anything", 5)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestBleveIndex_TopKLimitsResults(t *testing.T) {
	idx, err := NewBleveIndex(&testBleveAdapter{})
	require.NoError(t, err)
	defer idx.Close()

	ctx := context.Background()
	require.NoError(t, idx.Add(ctx, "a", "search keyword match"))
	require.NoError(t, idx.Add(ctx, "b", "search keyword find"))
	require.NoError(t, idx.Add(ctx, "c", "search keyword lookup"))

	results, err := idx.Search(ctx, "search keyword", 2)
	require.NoError(t, err)
	assert.Len(t, results, 2)
}
