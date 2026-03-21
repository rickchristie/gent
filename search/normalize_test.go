package search

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMinMaxNormalize(t *testing.T) {
	type input struct {
		results []SearchResult
	}

	type expected struct {
		scores []float64
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "all zeros remain zero",
			input: input{results: []SearchResult{
				{Id: "a", Score: 0}, {Id: "b", Score: 0},
			}},
			expected: expected{scores: []float64{0, 0}},
		},
		{
			name: "single non-zero normalizes to 1.0",
			input: input{results: []SearchResult{
				{Id: "a", Score: 5.0}, {Id: "b", Score: 0},
			}},
			expected: expected{scores: []float64{1.0, 0}},
		},
		{
			name: "all equal non-zero scores normalize to 1.0",
			input: input{results: []SearchResult{
				{Id: "a", Score: 3.0}, {Id: "b", Score: 3.0},
			}},
			expected: expected{scores: []float64{1.0, 1.0}},
		},
		{
			name: "range normalization with zeros preserved",
			input: input{results: []SearchResult{
				{Id: "a", Score: 10.0}, {Id: "b", Score: 5.0},
				{Id: "c", Score: 0}, {Id: "d", Score: 2.5},
			}},
			// min non-zero = 2.5, max = 10.0, range = 7.5
			// a: (10-2.5)/7.5 = 1.0
			// b: (5-2.5)/7.5 = 0.333
			// c: 0 (zero preserved)
			// d: (2.5-2.5)/7.5 = 0.0
			expected: expected{scores: []float64{1.0, 0.333, 0, 0}},
		},
		{
			name:     "empty input returns empty",
			input:    input{results: []SearchResult{}},
			expected: expected{scores: []float64{}},
		},
		{
			name: "snippets are preserved through normalization",
			input: input{results: []SearchResult{
				{Id: "a", Score: 10.0, Snippet: "snippet-a"},
				{Id: "b", Score: 5.0, Snippet: "snippet-b"},
			}},
			expected: expected{scores: []float64{1.0, 0}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := minMaxNormalize(tc.input.results)
			assert.Len(t, result, len(tc.expected.scores))
			for i, expectedScore := range tc.expected.scores {
				assert.InDelta(t, expectedScore, result[i].Score, 0.01,
					"score for result %d (%s)", i, result[i].Id)
			}
			// Verify snippets are preserved.
			for i, r := range tc.input.results {
				if i < len(result) {
					assert.Equal(t, r.Snippet, result[i].Snippet,
						"snippet for result %d", i)
				}
			}
		})
	}
}
