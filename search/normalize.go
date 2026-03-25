package search

// normalizeBM25 normalizes BM25 search results to [0, 1]. BM25 scores are unbounded and
// query-dependent: a query with rare terms against a short document might score 28.5, while
// a common-term query against a long document scores 2.1. The absolute number is meaningless
// across queries — a score of 15.0 could be excellent for one query and terrible for another.
// You cannot threshold on raw BM25 scores.
//
// In contrast, cosine similarity (from semantic search) is naturally bounded [0, 1] and
// stable across queries — a score of 0.7 means roughly the same thing regardless of the
// query. Cosine scores can be thresholded directly (e.g., > 0.3 for meaningful results).
//
// This normalization enables fusion: BM25 and cosine scores become comparable. If results
// contain a theoretical maximum score (set by BleveIndex when the adapter implements
// BleveIDFProvider), scores are normalized against that maximum. Otherwise, raw scores
// pass through unchanged.
func normalizeBM25(results []SearchResult) []SearchResult {
	if len(results) > 0 && results[0].Metadata != nil {
		if maxPossible, ok := results[0].Metadata[TheoreticalMaxKey].(float64); ok {
			return theoreticalMaxNormalize(results, maxPossible)
		}
	}
	return results
}

// theoreticalMaxNormalize normalizes BM25 scores against the theoretical maximum score
// for the query, producing values in [0, 1] where 1.0 represents a hypothetical perfect
// document.
//
// Theoretical-max normalization uses an absolute reference point — so noise stays noise.
// When all BM25 scores are tiny (common with semantic-style queries), they normalize to
// tiny values instead of being inflated to 1.0.
//
// Zero scores remain zero: in BM25, zero means "this term didn't appear in the document
// at all" — a fundamentally different signal from "appeared but scored poorly."
func theoreticalMaxNormalize(
	results []SearchResult, maxPossible float64,
) []SearchResult {
	if maxPossible <= 0 || len(results) == 0 {
		return results
	}

	normalized := make([]SearchResult, len(results))
	for i, r := range results {
		normalized[i] = SearchResult{
			Id: r.Id, Snippet: r.Snippet, Metadata: r.Metadata,
		}
		if r.Score > 0 {
			normalized[i].Score = r.Score / maxPossible
			if normalized[i].Score > 1.0 {
				normalized[i].Score = 1.0
			}
		}
	}
	return normalized
}
