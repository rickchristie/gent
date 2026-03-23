package search

// normalizeBM25 normalizes BM25 search results to [0, 1]. If results contain a theoretical
// maximum score (set by BleveIndex when the adapter implements BleveIDFProvider), scores are
// normalized against that maximum. Otherwise, raw scores pass through unchanged.
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
