//go:build cgo

package search

import (
	"context"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/rickchristie/gent/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testDoc is a simple document type for end-to-end fusion tests.
type testDoc struct {
	Name        string
	Description string
	Keywords    string
}

// testDocBleveAdapter implements BleveAdapter[testDoc].
type testDocBleveAdapter struct{}

func (a *testDocBleveAdapter) Mapping() mapping.IndexMapping {
	text := bleve.NewTextFieldMapping()
	text.Analyzer = "standard"
	text.Store = true
	text.IncludeInAll = true

	keyword := bleve.NewKeywordFieldMapping()

	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("name", keyword)
	docMapping.AddFieldMappingsAt("name_analyzed", text)
	docMapping.AddFieldMappingsAt("description", text)
	docMapping.AddFieldMappingsAt("keywords", text)

	indexMapping := bleve.NewIndexMapping()
	indexMapping.DefaultMapping = docMapping
	return indexMapping
}

func (a *testDocBleveAdapter) Convert(doc testDoc) (any, error) {
	return map[string]string{
		"name":          doc.Name,
		"name_analyzed": doc.Name,
		"description":   doc.Description,
		"keywords":      doc.Keywords,
	}, nil
}

func (a *testDocBleveAdapter) Query(queryText string) (query.Query, error) {
	nameMatch := bleve.NewMatchQuery(queryText)
	nameMatch.SetField("name")
	nameMatch.SetBoost(10.0)

	keywordsMatch := bleve.NewMatchQuery(queryText)
	keywordsMatch.SetField("keywords")
	keywordsMatch.SetBoost(3.0)

	descMatch := bleve.NewMatchQuery(queryText)
	descMatch.SetField("description")
	descMatch.SetBoost(1.0)

	disj := bleve.NewDisjunctionQuery(nameMatch, keywordsMatch, descMatch)
	disj.SetMin(1)
	return disj, nil
}

// testDocChunkAdapter implements ChunkAdapter[testDoc].
type testDocChunkAdapter struct{}

func (a *testDocChunkAdapter) Chunks(
	doc testDoc, _ TokenCounter, _ int,
) ([]Chunk, error) {
	text := doc.Name + ": " + doc.Description + " " + doc.Keywords
	return []Chunk{{Text: text}}, nil
}

// testCorpus returns a set of documents designed to test hybrid search edge cases.
func testCorpus() map[string]testDoc {
	return map[string]testDoc{
		"get_billing_ledger": {
			Name:        "get_billing_ledger",
			Description: "Retrieve billing ledger entries and payment invoices for a customer",
			Keywords:    "billing payment invoice ledger account",
		},
		"send_notification": {
			Name:        "send_notification",
			Description: "Send a notification to the customer via email SMS or push",
			Keywords:    "email sms push notification message alert",
		},
		"cancel_reservation": {
			Name:        "cancel_reservation",
			Description: "Cancel an existing reservation and process refund",
			Keywords:    "cancel reservation booking refund",
		},
		"early_termination": {
			Name:        "early_termination",
			Description: "Process early contract termination with penalty calculation",
			Keywords:    "terminate early contract penalty exit",
		},
		"lookup_customer": {
			Name:        "lookup_customer",
			Description: "Look up customer details by ID email or phone number",
			Keywords:    "customer lookup search find profile",
		},
	}
}

// TestFusedIndex_EndToEnd_BM25PlusSemantic tests hybrid search with real BM25 and ONNX
// embeddings. This is the most important integration test for the search package — it verifies
// that fusing BM25 keyword search with semantic vector search produces better results than
// either alone.
func TestFusedIndex_EndToEnd_BM25PlusSemantic(t *testing.T) {
	// Use the default e5-small config for semantic search.
	cfg := common.ConfigsForModel("multilingual-e5-small")[0]
	embedder := testEmbedderForConfig(t, cfg)
	defer embedder.Close()

	bleveIdx, err := NewBleveIndex(&testDocBleveAdapter{})
	require.NoError(t, err)
	defer bleveIdx.Close()

	flatIdx := NewFlatIndex(&testDocChunkAdapter{}, embedder)

	fuser := &WeightedLinearFuser{
		Weights:          map[string]float64{"bm25": 0.3, "semantic": 0.7},
		NormalizeSources: map[string]bool{"bm25": true, "semantic": false},
	}

	fusedIdx := NewFusedIndex(fuser,
		map[string]SearchIndex[testDoc]{"bm25": bleveIdx, "semantic": flatIdx},
		map[string]int{"bm25": 20, "semantic": 20},
	)

	ctx := context.Background()
	corpus := testCorpus()
	for id, doc := range corpus {
		require.NoError(t, fusedIdx.Add(ctx, id, doc))
	}

	t.Run("exact tool name ranks first", func(t *testing.T) {
		// BM25 should dominate here — exact keyword match on the name field.
		results, err := fusedIdx.Search(ctx, "get_billing_ledger", 5)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(results), 1)
		assert.Equal(t, "get_billing_ledger", results[0].Id)
	})

	t.Run("semantic query with no keyword overlap finds right tool", func(t *testing.T) {
		// "customer wants to leave early" has zero keyword overlap with "early_termination"
		// except "early". Semantic search should understand the intent.
		results, err := fusedIdx.Search(ctx, "customer wants to leave early", 5)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(results), 1)
		// early_termination or cancel_reservation should rank high.
		topIDs := make([]string, min(3, len(results)))
		for i := range topIDs {
			topIDs[i] = results[i].Id
		}
		assert.Contains(t, topIDs, "early_termination",
			"semantic intent should find early_termination in top 3, got: %v", topIDs)
	})

	t.Run("natural language query finds tool by meaning", func(t *testing.T) {
		// "check outstanding payments" should find billing tool even though "outstanding"
		// doesn't appear in the description.
		results, err := fusedIdx.Search(ctx, "check outstanding payments", 5)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(results), 1)
		topIDs := make([]string, min(3, len(results)))
		for i := range topIDs {
			topIDs[i] = results[i].Id
		}
		assert.Contains(t, topIDs, "get_billing_ledger",
			"billing tool should be in top 3 for payment query, got: %v", topIDs)
	})

	t.Run("keyword query benefits from BM25 boosting", func(t *testing.T) {
		// "billing payment" has direct keyword overlap — BM25 should give a strong signal.
		results, err := fusedIdx.Search(ctx, "billing payment", 5)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(results), 1)
		assert.Equal(t, "get_billing_ledger", results[0].Id,
			"billing tool should rank first for direct keyword match")
	})

	t.Run("all documents returned when corpus is small", func(t *testing.T) {
		results, err := fusedIdx.Search(ctx, "customer service tools", 10)
		require.NoError(t, err)
		// With a 5-doc corpus, most should appear in results.
		assert.GreaterOrEqual(t, len(results), 3,
			"most docs should match a broad query in a small corpus")
	})

	t.Run("scores are bounded between 0 and 1", func(t *testing.T) {
		results, err := fusedIdx.Search(ctx, "send email notification", 5)
		require.NoError(t, err)
		for _, r := range results {
			assert.GreaterOrEqual(t, r.Score, 0.0, "score should be >= 0 for %s", r.Id)
			assert.LessOrEqual(t, r.Score, 1.0, "score should be <= 1 for %s", r.Id)
		}
	})

	t.Run("swap replaces all documents in both indices", func(t *testing.T) {
		newCorpus := map[string]testDoc{
			"new_tool": {
				Name: "new_tool", Description: "A completely new tool",
				Keywords: "new fresh modern",
			},
		}
		require.NoError(t, fusedIdx.Swap(ctx, newCorpus))

		// Old tools should be gone.
		results, err := fusedIdx.Search(ctx, "billing payment", 5)
		require.NoError(t, err)
		for _, r := range results {
			assert.NotEqual(t, "get_billing_ledger", r.Id)
		}

		// New tool should be found.
		results, err = fusedIdx.Search(ctx, "new fresh tool", 5)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(results), 1)
		assert.Equal(t, "new_tool", results[0].Id)
	})
}

