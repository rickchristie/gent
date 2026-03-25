package search

import "sort"

// Fuser combines ranked results from multiple named search sources into a single ranked list.
//
// The input map is keyed by source name (matching the names used in FusedIndex's index map).
// Each value is that source's search results.
//
// Implementations must handle:
//   - Documents appearing in only one source (other sources scored 0)
//   - Empty result sets from one or more sources
//   - Different score scales across sources (e.g., BM25 vs cosine similarity)
type Fuser interface {
	Fuse(results map[string][]SearchResult, topK int) ([]SearchResult, error)
}

// WeightedLinearFuser combines results using weighted linear score combination.
//
// For each document, the fused score is:
//
//	score = Σ weights[source] * normalize(source_score)
//
// Sources listed in NormalizeSources (with value true) are normalized before fusion. If the
// source's results carry a theoretical maximum (from BleveIndex with BleveIDFProvider),
// scores are divided by that maximum. Otherwise, raw scores pass through. Sources not listed
// or set to false pass their scores through unchanged (correct for cosine similarity which
// is already in [0, 1]).
//
// The snippet for each fused result comes from the source with the highest weighted
// contribution for that document.
//
// # Why weighted linear over RRF (Reciprocal Rank Fusion)
//
// RRF uses rank position, not score magnitude: score = Σ 1/(k + rank_i) with k=60.
// This creates a specific failure mode for tool search:
//
//   - A doc rank #1 in semantic (score 0.95) but absent from BM25:
//     1/(60+1) + 0 = 0.0164
//   - A doc rank #5 in both: 1/(60+5) + 1/(60+5) = 0.0308 — almost double
//
// RRF systematically favors "okay in both" over "perfect in one." For tool search this
// is exactly wrong — the whole point of semantic search is finding tools where keyword
// matching fails. Weighted linear handles "found by one method only" correctly:
// 0.7 × 0.95 = 0.665, which can still beat a mediocre dual-match.
//
// # Worked Examples
//
// Query: "check outstanding payments" (natural language, no exact matches)
//
//	Tool                 BM25Raw  Semantic  BM25Norm  Fused(0.3/0.7)
//	get_payment_history    3.8     0.85      1.00      0.895
//	get_billing_ledger     0.0     0.89      0.00      0.623
//	list_invoices          1.2     0.82      0.00      0.574
//
// get_billing_ledger has ZERO BM25 match but ranks #2 via semantic. Pure BM25 misses it.
//
// Query: "get_billing_ledger" (explicit tool name)
//
//	Tool                 BM25Raw  Semantic  BM25Norm  Fused
//	get_billing_ledger    28.5     0.95      1.00      0.965
//	get_payment_history    6.1     0.72      0.08      0.528
//
// 10x name boost → massive raw BM25 → overwhelmingly first place.
//
// Query: "customer wants to leave early" (colloquial agent reasoning)
//
//	Tool                 BM25Raw  Semantic  BM25Norm  Fused
//	early_termination      2.1     0.91      1.00      0.937
//	cancel_reservation     0.0     0.83      0.00      0.581
//	process_checkout       0.0     0.78      0.00      0.546
//
// Only one tool has any BM25 match ("early"). Semantic finds three relevant tools.
//
// # Example: 30% BM25 + 70% semantic
//
//	fuser := &WeightedLinearFuser{
//	    Weights:          map[string]float64{"bm25": 0.3, "semantic": 0.7},
//	    NormalizeSources: map[string]bool{"bm25": true, "semantic": false},
//	}
type WeightedLinearFuser struct {
	// Weights per source name. Should sum to 1.0 for interpretable output scores.
	Weights map[string]float64

	// NormalizeSources controls per-source normalization. Set true for sources with unbounded
	// scores (BM25). Set false for sources with bounded scores (cosine similarity).
	NormalizeSources map[string]bool
}

func (f *WeightedLinearFuser) Fuse(
	results map[string][]SearchResult, topK int,
) ([]SearchResult, error) {
	// Normalize sources that need it.
	normalized := make(map[string][]SearchResult, len(results))
	for name, sourceResults := range results {
		if f.NormalizeSources[name] {
			normalized[name] = normalizeBM25(sourceResults)
		} else {
			normalized[name] = sourceResults
		}
	}

	// Build raw score lookup for metadata (before normalization).
	rawScores := map[string]map[string]float64{} // docId → sourceName → rawScore
	for name, sourceResults := range results {
		for _, r := range sourceResults {
			if rawScores[r.Id] == nil {
				rawScores[r.Id] = map[string]float64{}
			}
			rawScores[r.Id][name] = r.Score
		}
	}

	// Merge: union of all document IDs, accumulate weighted scores.
	type mergedEntry struct {
		score       float64
		snippet     string
		bestContrib float64
		metadata    map[string]any
	}
	merged := map[string]*mergedEntry{}

	for name, sourceResults := range normalized {
		weight := f.Weights[name]
		for _, r := range sourceResults {
			entry, ok := merged[r.Id]
			if !ok {
				entry = &mergedEntry{metadata: map[string]any{}}
				merged[r.Id] = entry
			}
			contribution := weight * r.Score
			entry.score += contribution
			if contribution > entry.bestContrib {
				entry.bestContrib = contribution
				entry.snippet = r.Snippet
			}
			// Store per-source scores for debugging/analysis.
			raw := rawScores[r.Id][name]
			entry.metadata[name+"_raw"] = raw
			entry.metadata[name+"_normalized"] = r.Score
			entry.metadata[name+"_weighted"] = contribution
		}
	}

	// Sort by fused score descending.
	fused := make([]SearchResult, 0, len(merged))
	for id, entry := range merged {
		fused = append(fused, SearchResult{
			Id: id, Score: entry.score, Snippet: entry.snippet, Metadata: entry.metadata,
		})
	}
	sort.Slice(fused, func(i, j int) bool { return fused[i].Score > fused[j].Score })

	if topK > 0 && len(fused) > topK {
		fused = fused[:topK]
	}
	return fused, nil
}

// Compile-time check.
var _ Fuser = (*WeightedLinearFuser)(nil)
