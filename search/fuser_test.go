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
					"bm25": {
						{Id: "a", Score: 20.0, Metadata: map[string]any{TheoreticalMaxKey: 25.0}},
						{Id: "b", Score: 5.0, Metadata: map[string]any{TheoreticalMaxKey: 25.0}},
					},
					"semantic": {{Id: "a", Score: 0.95}, {Id: "b", Score: 0.72}},
				},
				topK: 5,
			},
			// a: BM25 normalized = 20/25 = 0.8, semantic = 0.95
			//    → 0.3*0.8 + 0.7*0.95 = 0.24 + 0.665 = 0.905
			// b: BM25 normalized = 5/25 = 0.2, semantic = 0.72
			//    → 0.3*0.2 + 0.7*0.72 = 0.06 + 0.504 = 0.564
			expected: expected{
				ids:    []string{"a", "b"},
				scores: []float64{0.905, 0.564},
			},
		},
		{
			name: "document in one source only still scores",
			input: input{
				weights:          map[string]float64{"bm25": 0.3, "semantic": 0.7},
				normalizeSources: map[string]bool{"bm25": true, "semantic": false},
				results: map[string][]SearchResult{
					"bm25": {
						{Id: "a", Score: 10.0, Metadata: map[string]any{TheoreticalMaxKey: 20.0}},
					},
					"semantic": {{Id: "b", Score: 0.9}},
				},
				topK: 5,
			},
			// a: BM25 normalized = 10/20 = 0.5, semantic absent → 0.3*0.5 = 0.15
			// b: BM25 absent, semantic = 0.9 → 0.7*0.9 = 0.63
			expected: expected{
				ids:    []string{"b", "a"},
				scores: []float64{0.63, 0.15},
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
			name: "weights don't sum to 1.0",
			input: input{
				weights:          map[string]float64{"a": 0.5, "b": 0.3},
				normalizeSources: map[string]bool{},
				results: map[string][]SearchResult{
					"a": {{Id: "x", Score: 0.8}, {Id: "y", Score: 0.6}},
					"b": {{Id: "x", Score: 0.7}, {Id: "y", Score: 0.4}},
				},
				topK: 5,
			},
			// x: 0.5*0.8 + 0.3*0.7 = 0.4 + 0.21 = 0.61
			// y: 0.5*0.6 + 0.3*0.4 = 0.3 + 0.12 = 0.42
			expected: expected{
				ids:    []string{"x", "y"},
				scores: []float64{0.61, 0.42},
			},
		},
		{
			name: "source with no matching weight is ignored",
			input: input{
				weights:          map[string]float64{"known": 1.0},
				normalizeSources: map[string]bool{},
				results: map[string][]SearchResult{
					"known":   {{Id: "a", Score: 0.8}},
					"unknown": {{Id: "b", Score: 0.9}},
				},
				topK: 5,
			},
			// "unknown" has weight 0.0 (missing from map), so b: 0.0*0.9 = 0.0
			// a: 1.0*0.8 = 0.8
			// b still appears but with score 0.0
			expected: expected{
				ids:    []string{"a", "b"},
				scores: []float64{0.8, 0.0},
			},
		},
		{
			name: "topK zero returns all results",
			input: input{
				weights:          map[string]float64{"s": 1.0},
				normalizeSources: map[string]bool{},
				results: map[string][]SearchResult{
					"s": {
						{Id: "a", Score: 0.9},
						{Id: "b", Score: 0.8},
						{Id: "c", Score: 0.7},
						{Id: "d", Score: 0.6},
						{Id: "e", Score: 0.5},
					},
				},
				topK: 0,
			},
			expected: expected{
				ids:    []string{"a", "b", "c", "d", "e"},
				scores: []float64{0.9, 0.8, 0.7, 0.6, 0.5},
			},
		},
		{
			name: "three sources fused",
			input: input{
				weights: map[string]float64{
					"bm25": 0.2, "semantic": 0.5, "fuzzy": 0.3,
				},
				normalizeSources: map[string]bool{"bm25": true},
				results: map[string][]SearchResult{
					"bm25": {
						{Id: "a", Score: 20.0, Metadata: map[string]any{TheoreticalMaxKey: 25.0}},
						{Id: "b", Score: 10.0, Metadata: map[string]any{TheoreticalMaxKey: 25.0}},
					},
					"semantic": {{Id: "a", Score: 0.9}, {Id: "c", Score: 0.85}},
					"fuzzy":    {{Id: "b", Score: 0.7}, {Id: "c", Score: 0.6}},
				},
				topK: 5,
			},
			// BM25 normalized: a=20/25=0.8, b=10/25=0.4
			// a: 0.2*0.8 + 0.5*0.9 = 0.16 + 0.45 = 0.61
			// b: 0.2*0.4 + 0.3*0.7 = 0.08 + 0.21 = 0.29
			// c: 0.5*0.85 + 0.3*0.6 = 0.425 + 0.18 = 0.605
			expected: expected{
				ids:    []string{"a", "c", "b"},
				scores: []float64{0.61, 0.605, 0.29},
			},
		},
		{
			name: "duplicate IDs within single source",
			input: input{
				weights:          map[string]float64{"s": 1.0},
				normalizeSources: map[string]bool{},
				results: map[string][]SearchResult{
					"s": {
						{Id: "a", Score: 0.5},
						{Id: "b", Score: 0.3},
						{Id: "a", Score: 0.4},
					},
				},
				topK: 5,
			},
			// a appears twice: 1.0*0.5 + 1.0*0.4 = 0.9
			// b: 1.0*0.3 = 0.3
			expected: expected{
				ids:    []string{"a", "b"},
				scores: []float64{0.9, 0.3},
			},
		},
		{
			name: "snippet comes from highest weighted contribution",
			input: input{
				weights:          map[string]float64{"bm25": 0.3, "semantic": 0.7},
				normalizeSources: map[string]bool{"bm25": true, "semantic": false},
				results: map[string][]SearchResult{
					"bm25": {
						{Id: "a", Score: 5.0, Snippet: "bm25-snippet",
							Metadata: map[string]any{TheoreticalMaxKey: 10.0}},
					},
					"semantic": {{Id: "a", Score: 0.9, Snippet: "semantic-snippet"}},
				},
				topK: 5,
			},
			// BM25 normalized = 5/10 = 0.5, contribution: 0.3 * 0.5 = 0.15
			// Semantic contribution: 0.7 * 0.9 = 0.63 ← higher
			expected: expected{ids: []string{"a"}, scores: []float64{0.78}},
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
