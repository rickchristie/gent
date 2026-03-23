//go:build cgo

package policy

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/rickchristie/gent/search"
	"github.com/stretchr/testify/require"
)

// buildFusedIndex creates a FusedIndex for policies with the same configuration as
// PolicySearchTool (40% BM25, 60% semantic). The confidence threshold is set to 0 so
// score analysis tests can see all BM25 scores unfiltered.
func buildFusedIndex(
	t *testing.T, embedder search.Embedder, policies []*Policy,
) search.SearchIndex[*Policy] {
	t.Helper()

	bleveIdx, err := search.NewBleveIndex(&PolicyBleveAdapter{},
		search.WithTheoreticalMaxConfidenceThreshold(0),
	)
	require.NoError(t, err)

	flatIdx := search.NewFlatIndex(&PolicyChunkAdapter{}, embedder)

	fuser := &search.WeightedLinearFuser{
		Weights:          map[string]float64{"bm25": 0.4, "semantic": 0.6},
		NormalizeSources: map[string]bool{"bm25": true, "semantic": false},
	}

	fusedIdx := search.NewFusedIndex(fuser,
		map[string]search.SearchIndex[*Policy]{"bm25": bleveIdx, "semantic": flatIdx},
		map[string]int{"bm25": 20, "semantic": 20},
	)

	docs := make(map[string]*Policy, len(policies))
	for _, p := range policies {
		docs[p.Id] = p
	}
	require.NoError(t, fusedIdx.Swap(context.Background(), docs))
	return fusedIdx
}

// printScoreReport prints a detailed score breakdown for debugging search quality.
func printScoreReport(query string, results []search.SearchResult) {
	fmt.Printf("\n=== Query: %q ===\n", query)
	fmt.Printf("%-30s %8s %8s %8s %8s %8s %8s\n",
		"POLICY", "FUSED", "BM25raw", "BM25nrm", "BM25wtd", "SEMraw", "SEMwtd")
	for _, r := range results {
		bm25Raw, _ := r.Metadata["bm25_raw"].(float64)
		bm25Norm, _ := r.Metadata["bm25_normalized"].(float64)
		bm25Wtd, _ := r.Metadata["bm25_weighted"].(float64)
		semRaw, _ := r.Metadata["semantic_raw"].(float64)
		semWtd, _ := r.Metadata["semantic_weighted"].(float64)
		fmt.Printf("%-30s %8.4f %8.4f %8.4f %8.4f %8.4f %8.4f\n",
			r.Id, r.Score, bm25Raw, bm25Norm, bm25Wtd, semRaw, semWtd)
	}
}

func TestFusedIndex_AirlineScoreAnalysis(t *testing.T) {
	if os.Getenv("GENT_SKIP_ONNX") != "" {
		t.Skip("GENT_SKIP_ONNX set")
	}
	embedder := testEmbedder(t)
	defer embedder.Close()

	policies := airlinePolicies()
	idx := buildFusedIndex(t, embedder, policies)
	ctx := context.Background()

	queries := []struct {
		name  string
		query string
	}{
		// Semantic queries (BM25 should NOT dominate).
		{"exact_id", "cancellation-refund"},
		{"keyword_cancel", "cancel refund ticket"},
		{"semantic_leave_early", "customer wants to leave the trip early and go home"},
		{"baggage", "how many bags can I check and what does it cost"},
		{"child_flying", "my 10 year old needs to fly alone to visit grandparents"},
		{"pet_travel", "bringing a dog on the plane in a carrier"},
		{"overbooked", "flight is overbooked and they want to bump passengers"},
		{"just_booked", "I just bought the ticket an hour ago, can I get my money back"},
		{"broad", "frequent flyer benefits when changing flights"},

		// BM25-critical queries (BM25 SHOULD dominate or strongly contribute).
		// Identifier: embedding model can't encode a regulation code.
		{"identifier_eu261", "EC261/2004"},
		// Rare acronym: model likely never saw "APIS" in aviation context.
		{"acronym_apis", "APIS requirements"},
		// Very short query: single token, embedding is low-information.
		{"short_wifi", "WiFi"},
		{"short_esta", "ESTA"},
		// Numeric discrimination: "under 5" vs "under 15" are semantically identical.
		{"numeric_age", "children under 5 years old"},
	}

	for _, q := range queries {
		t.Run(q.name, func(t *testing.T) {
			results, err := idx.Search(ctx, q.query, 10)
			require.NoError(t, err)
			printScoreReport(q.query, results)
		})
	}
}

func TestFusedIndex_EcommerceScoreAnalysis(t *testing.T) {
	if os.Getenv("GENT_SKIP_ONNX") != "" {
		t.Skip("GENT_SKIP_ONNX set")
	}
	embedder := testEmbedder(t)
	defer embedder.Close()

	policies := ecommercePolicies()
	idx := buildFusedIndex(t, embedder, policies)
	ctx := context.Background()

	queries := []struct {
		name  string
		query string
	}{
		// Semantic queries.
		{"return", "customer wants to return a product they bought last week"},
		{"price_match", "customer found a lower price on Amazon for the same item"},
		{"damaged", "item arrived broken and damaged in shipping"},
		{"gift_card", "does the gift card expire"},
		{"loyalty", "how do I earn and redeem loyalty points"},

		// BM25-critical queries.
		// Rare acronym: "RMA" is domain-specific, embedding model won't encode it.
		{"acronym_rma", "RMA"},
		// Identifier: tax form number is an arbitrary code.
		{"identifier_1099k", "1099-K"},
		// Numeric discrimination: 15-day vs 30-day return window.
		{"numeric_15day", "15 day return electronics"},
		// Semantic confusion: standard-return vs electronics-return-15day.
		{"confusion_electronics_return", "return a laptop"},
	}

	for _, q := range queries {
		t.Run(q.name, func(t *testing.T) {
			results, err := idx.Search(ctx, q.query, 10)
			require.NoError(t, err)
			printScoreReport(q.query, results)
		})
	}
}
