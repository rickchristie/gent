package search

// minMaxNormalize applies per-query min-max normalization to a result set.
//
// Rules:
//   - Zero scores remain zero ("no match at all", not "worst match")
//   - All zeros → all outputs are zero
//   - Exactly one non-zero → normalizes to 1.0
//   - All non-zero scores equal → all normalize to 1.0
//
// This is the correct normalization for BM25 scores where 0.0 means "this term didn't appear
// in the document at all" — a fundamentally different signal from "appeared but scored poorly."
func minMaxNormalize(results []SearchResult) []SearchResult {
	var min, max float64
	var hasNonZero bool
	for _, r := range results {
		if r.Score > 0 {
			if !hasNonZero {
				min = r.Score
				max = r.Score
				hasNonZero = true
			} else {
				if r.Score < min {
					min = r.Score
				}
				if r.Score > max {
					max = r.Score
				}
			}
		}
	}

	if !hasNonZero {
		return results
	}

	normalized := make([]SearchResult, len(results))
	for i, r := range results {
		normalized[i] = SearchResult{Id: r.Id, Snippet: r.Snippet}
		if r.Score == 0 {
			normalized[i].Score = 0
		} else if max == min {
			normalized[i].Score = 1.0
		} else {
			normalized[i].Score = (r.Score - min) / (max - min)
		}
	}
	return normalized
}
