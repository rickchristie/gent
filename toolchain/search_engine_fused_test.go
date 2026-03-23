//go:build cgo

package toolchain

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/common"
	"github.com/rickchristie/gent/search"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testFusedSearcher(t *testing.T) *IndexToolSearcher {
	t.Helper()
	cfg := common.ConfigsForModel("multilingual-e5-small")[0]

	if !common.ModelDownloaded(&cfg.Model) {
		t.Skip("multilingual-e5-small model not downloaded")
	}
	dir, err := common.ModelDir(cfg.Model.Name)
	if err != nil {
		t.Skipf("cannot determine model dir: %v", err)
	}

	embedder, err := search.NewOnnxEmbedder(cfg, search.OnnxOptions{
		ModelPath:      filepath.Join(dir, cfg.Model.ModelFile),
		TokenizerPath:  filepath.Join(dir, "tokenizer.json"),
		NumThreads:     2,
		MaxConcurrency: 2,
	})
	if err != nil {
		t.Skipf("ONNX embedder not available (run gent setup onnx): %v", err)
	}
	t.Cleanup(func() { embedder.Close() })

	return NewFusedToolSearcher(embedder)
}

func fusedTestTools() []gent.IndexableTool {
	return []gent.IndexableTool{
		&testIndexableTool{
			name: "get_billing_ledger", description: "Retrieve billing ledger entries",
			domain: "Billing", categories: []string{"lookup", "billing"},
			keywords:         []string{"billing", "payment", "invoice", "ledger"},
			syntheticQueries: []string{"check payment status", "look up invoices"},
		},
		&testIndexableTool{
			name: "send_notification", description: "Send notification via email or SMS",
			domain: "Communication", categories: []string{"send", "notification"},
			keywords:         []string{"email", "sms", "notification", "message"},
			syntheticQueries: []string{"notify customer", "send email"},
		},
		&testIndexableTool{
			name: "cancel_reservation", description: "Cancel an existing reservation",
			domain: "Reservations", categories: []string{"mutation", "reservation"},
			keywords:         []string{"cancel", "reservation", "booking"},
			syntheticQueries: []string{"cancel a booking", "remove reservation"},
		},
		&testIndexableTool{
			name: "early_termination", description: "Process early contract termination",
			domain: "Contracts", categories: []string{"mutation", "contract"},
			keywords:         []string{"terminate", "early", "contract", "penalty"},
			syntheticQueries: []string{"end contract early", "early checkout"},
		},
		&testIndexableTool{
			name: "lookup_customer", description: "Look up customer details by ID or email",
			domain: "Customer", categories: []string{"lookup", "customer"},
			keywords:         []string{"customer", "lookup", "search", "profile"},
			syntheticQueries: []string{"find customer", "customer details"},
		},
	}
}

func TestFusedToolSearcher_IdAndGuidance(t *testing.T) {
	if os.Getenv("GENT_SKIP_ONNX") != "" {
		t.Skip("GENT_SKIP_ONNX set")
	}
	searcher := testFusedSearcher(t)
	assert.Equal(t, "hybrid", searcher.Id())
	assert.Contains(t, searcher.SearchGuidance(), "natural language")
}

func TestFusedToolSearcher_ExactNameRanksFirst(t *testing.T) {
	searcher := testFusedSearcher(t)
	require.NoError(t, searcher.IndexAll(fusedTestTools()))

	results, err := searcher.Search(context.Background(), "get_billing_ledger")
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(results), 1)
	assert.Equal(t, "get_billing_ledger", results[0],
		"exact tool name should rank first via BM25 boost")
}

func TestFusedToolSearcher_SemanticQueryFindsRelevantTool(t *testing.T) {
	searcher := testFusedSearcher(t)
	require.NoError(t, searcher.IndexAll(fusedTestTools()))

	// "customer wants to leave early" has minimal keyword overlap with "early_termination"
	// but strong semantic similarity.
	results, err := searcher.Search(context.Background(), "customer wants to leave early")
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(results), 1)
	topResults := results[:min(3, len(results))]
	assert.Contains(t, topResults, "early_termination",
		"semantic search should find early_termination in top 3, got: %v", topResults)
}

func TestFusedToolSearcher_NaturalLanguageQuery(t *testing.T) {
	searcher := testFusedSearcher(t)
	require.NoError(t, searcher.IndexAll(fusedTestTools()))

	results, err := searcher.Search(context.Background(),
		"look up customer invoice and payment history")
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(results), 1)
	topResults := results[:min(3, len(results))]
	assert.Contains(t, topResults, "get_billing_ledger",
		"billing tool should be in top 3 for payment query, got: %v", topResults)
}

func TestFusedToolSearcher_ReindexReplacesAll(t *testing.T) {
	searcher := testFusedSearcher(t)
	require.NoError(t, searcher.IndexAll(fusedTestTools()))

	// Re-index with just one tool.
	newTools := []gent.IndexableTool{
		&testIndexableTool{
			name: "new_tool", description: "A completely new tool",
			domain: "New", categories: []string{"new"},
			keywords: []string{"new", "fresh"}, syntheticQueries: []string{"new tool"},
		},
	}
	require.NoError(t, searcher.IndexAll(newTools))

	results, err := searcher.Search(context.Background(), "billing payment")
	require.NoError(t, err)
	for _, r := range results {
		assert.NotEqual(t, "get_billing_ledger", r, "old tools should be gone after re-index")
	}

	results, err = searcher.Search(context.Background(), "new fresh tool")
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(results), 1)
	assert.Equal(t, "new_tool", results[0])
}
