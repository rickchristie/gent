package toolchain

import (
	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/search"
)

// FusedToolSearcher combines BM25 keyword search and semantic vector search into a single
// ToolSearcher using weighted linear score combination.
//
// # Why Hybrid Search
//
// BM25 and semantic search fail in complementary ways. BM25 excels at exact term matching
// ("get_billing_ledger" → finds the tool by name) but fails when the agent uses different
// vocabulary ("check outstanding payments" → won't match "billing ledger"). Semantic search
// understands meaning regardless of vocabulary but struggles with short queries, abbreviations,
// and exact identifiers. Combining both covers each other's blind spots.
//
// # Why Weighted Linear over RRF
//
// RRF (Reciprocal Rank Fusion) uses rank position, not score magnitude. A tool that's rank #1
// in semantic search (score 0.95, highly relevant) but absent from BM25 gets penalized because
// RRF treats "missing from one list" the same as "ranked last." With weighted linear fusion,
// that tool scores 0.7 × 0.95 = 0.665 — high enough to beat a mediocre dual-match.
//
// This matters for tool search because the most relevant tool often has zero keyword overlap
// with the agent's natural language reasoning. RRF would systematically bury these results.
//
// # Default Weights (BM25 0.3, Semantic 0.7)
//
// Tool search benefits more from semantic matching than keyword matching because:
//   - Agents describe tool needs in natural language, not tool registry vocabulary
//   - Tool names are often abbreviated/domain-specific (get_billing_ledger vs "check payments")
//   - Synthetic queries help but can't cover all phrasings
//
// The 0.3/0.7 split gives BM25 enough weight to boost exact-name matches to the top (the 10x
// BM25 boost on name field produces very high raw scores that dominate even at 30% weight)
// while letting semantic search carry the ranking for natural language queries.
//
// For comparison, policy search would use 0.4/0.6 — policies use formal, findable terms
// with higher keyword overlap, so BM25 contributes more meaningfully there.
//
// # Score Normalization
//
// BM25 scores are unbounded and query-dependent (28.5 from one query is not comparable to 3.8
// from another). Cosine similarity is bounded [0, 1] and stable across queries. Before fusion,
// BM25 scores are normalized against the theoretical maximum BM25 score for the query
// (computed from IDF statistics), producing bounded [0, 1] values that don't inflate noise.
// Semantic scores pass through unchanged.
//
// # Usage
//
//	embedder, _ := search.NewOnnxEmbedder(search.EmbedderConfig{...})
//	searcher := toolchain.NewFusedToolSearcher(embedder)
//
//	tc := toolchain.NewSearchJSON(toolchain.SearchHintDomainCategories).
//	    RegisterEngine(searcher)
type FusedToolSearcher = IndexToolSearcher

const defaultFusedGuidance = "Use natural language queries to search for tools. The search " +
	"combines keyword matching and semantic understanding, so describe what you need in " +
	"plain language. Examples: \"look up customer billing\", \"send notification to " +
	"customer\", \"cancel or modify a reservation\""

// NewFusedToolSearcher creates a ToolSearcher that fuses BM25 keyword search (via Bleve) with
// semantic vector search (via the provided Embedder). Documents are indexed in both engines;
// queries run against both and results are merged using weighted linear score combination
// (BM25 0.3, semantic 0.7).
//
// The embedder must be initialized and ready to use. The caller owns the embedder's lifecycle
// and must call embedder.Close() when done.
func NewFusedToolSearcher(embedder search.Embedder) *FusedToolSearcher {
	bleveIdx, err := search.NewBleveIndex(&ToolBleveAdapter{})
	if err != nil {
		panic("toolchain: failed to create BleveIndex: " + err.Error())
	}

	flatIdx := search.NewFlatIndex(&ToolChunkAdapter{}, embedder)

	fuser := &search.WeightedLinearFuser{
		Weights:          map[string]float64{"bm25": 0.3, "semantic": 0.7},
		NormalizeSources: map[string]bool{"bm25": true, "semantic": false},
	}

	fusedIdx := search.NewFusedIndex(fuser,
		map[string]search.SearchIndex[gent.IndexableTool]{"bm25": bleveIdx, "semantic": flatIdx},
		map[string]int{"bm25": 20, "semantic": 20},
	)

	return NewIndexToolSearcher("hybrid", fusedIdx).WithSearchGuidance(defaultFusedGuidance)
}
