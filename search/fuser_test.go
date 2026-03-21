package search

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWeightedLinearFuser(t *testing.T) {
	type input struct {
		weights          map[string]float64
		normalizeSources map[string]bool
		results          map[string][]SearchResult
		topK             int
	}

	type expected struct {
		ids    []string
		scores []float64
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "semantic only match scores correctly",
			input: input{
				weights:          map[string]float64{"bm25": 0.3, "semantic": 0.7},
				normalizeSources: map[string]bool{"bm25": true, "semantic": false},
				results: map[string][]SearchResult{
					"bm25":     {},
					"semantic": {{Id: "a", Score: 0.89}, {Id: "b", Score: 0.45}},
				},
				topK: 5,
			},
			expected: expected{
				ids:    []string{"a", "b"},
				scores: []float64{0.623, 0.315},
			},
		},
		{
			name: "both sources match with BM25 normalization",
			input: input{
				weights:          map[string]float64{"bm25": 0.3, "semantic": 0.7},
				normalizeSources: map[string]bool{"bm25": true, "semantic": false},
				results: map[string][]SearchResult{
					"bm25":     {{Id: "a", Score: 28.5}, {Id: "b", Score: 6.1}},
					"semantic": {{Id: "a", Score: 0.95}, {Id: "b", Score: 0.72}},
				},
				topK: 5,
			},
			// a: BM25 normalized = 1.0 (max), semantic = 0.95 → 0.3*1.0 + 0.7*0.95 = 0.965
			// b: BM25 normalized = 0.0 (min non-zero), semantic = 0.72 → 0.3*0.0 + 0.7*0.72 = 0.504
			expected: expected{
				ids:    []string{"a", "b"},
				scores: []float64{0.965, 0.504},
			},
		},
		{
			name: "document in one source only still scores",
			input: input{
				weights:          map[string]float64{"bm25": 0.3, "semantic": 0.7},
				normalizeSources: map[string]bool{"bm25": true, "semantic": false},
				results: map[string][]SearchResult{
					"bm25":     {{Id: "a", Score: 10.0}},
					"semantic": {{Id: "b", Score: 0.9}},
				},
				topK: 5,
			},
			// a: BM25 normalized = 1.0, semantic absent → 0.3*1.0 = 0.3
			// b: BM25 absent, semantic = 0.9 → 0.7*0.9 = 0.63
			expected: expected{
				ids:    []string{"b", "a"},
				scores: []float64{0.63, 0.3},
			},
		},
		{
			name: "topK limits results",
			input: input{
				weights:          map[string]float64{"s": 1.0},
				normalizeSources: map[string]bool{},
				results: map[string][]SearchResult{
					"s": {
						{Id: "a", Score: 0.9}, {Id: "b", Score: 0.8},
						{Id: "c", Score: 0.7}, {Id: "d", Score: 0.6},
					},
				},
				topK: 2,
			},
			expected: expected{
				ids:    []string{"a", "b"},
				scores: []float64{0.9, 0.8},
			},
		},
		{
			name: "both sources empty returns empty",
			input: input{
				weights:          map[string]float64{"bm25": 0.3, "semantic": 0.7},
				normalizeSources: map[string]bool{"bm25": true},
				results:          map[string][]SearchResult{"bm25": {}, "semantic": {}},
				topK:             5,
			},
			expected: expected{ids: []string{}, scores: []float64{}},
		},
		{
			name: "snippet comes from highest weighted contribution",
			input: input{
				weights:          map[string]float64{"bm25": 0.3, "semantic": 0.7},
				normalizeSources: map[string]bool{"bm25": true, "semantic": false},
				results: map[string][]SearchResult{
					"bm25":     {{Id: "a", Score: 5.0, Snippet: "bm25-snippet"}},
					"semantic": {{Id: "a", Score: 0.9, Snippet: "semantic-snippet"}},
				},
				topK: 5,
			},
			// BM25 contribution: 0.3 * 1.0 (normalized) = 0.3
			// Semantic contribution: 0.7 * 0.9 = 0.63 ← higher
			expected: expected{ids: []string{"a"}, scores: []float64{0.93}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fuser := &WeightedLinearFuser{
				Weights:          tc.input.weights,
				NormalizeSources: tc.input.normalizeSources,
			}
			results, err := fuser.Fuse(tc.input.results, tc.input.topK)
			require.NoError(t, err)
			assert.Len(t, results, len(tc.expected.ids))

			for i := range tc.expected.ids {
				assert.Equal(t, tc.expected.ids[i], results[i].Id, "id at position %d", i)
				assert.InDelta(t, tc.expected.scores[i], results[i].Score, 0.01,
					"score at position %d", i)
			}

			// Verify snippet for the "snippet comes from highest" test case.
			if tc.name == "snippet comes from highest weighted contribution" {
				assert.Equal(t, "semantic-snippet", results[0].Snippet)
			}
		})
	}
}
