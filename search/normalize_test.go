package search

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTheoreticalMaxNormalize(t *testing.T) {
	type input struct {
		results     []SearchResult
		maxPossible float64
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
			name: "scores normalized against theoretical max",
			input: input{
				maxPossible: 20.0,
				results: []SearchResult{
					{Id: "a", Score: 10.0}, {Id: "b", Score: 5.0},
					{Id: "c", Score: 1.0},
				},
			},
			expected: expected{scores: []float64{0.5, 0.25, 0.05}},
		},
		{
			name: "score exceeding max is clamped to 1.0",
			input: input{
				maxPossible: 5.0,
				results: []SearchResult{
					{Id: "a", Score: 8.0}, {Id: "b", Score: 3.0},
				},
			},
			expected: expected{scores: []float64{1.0, 0.6}},
		},
		{
			name: "zero scores remain zero",
			input: input{
				maxPossible: 10.0,
				results: []SearchResult{
					{Id: "a", Score: 5.0}, {Id: "b", Score: 0},
				},
			},
			expected: expected{scores: []float64{0.5, 0}},
		},
		{
			name: "all zeros remain zero",
			input: input{
				maxPossible: 10.0,
				results: []SearchResult{
					{Id: "a", Score: 0}, {Id: "b", Score: 0},
				},
			},
			expected: expected{scores: []float64{0, 0}},
		},
		{
			name:     "empty input returns empty",
			input:    input{maxPossible: 10.0, results: []SearchResult{}},
			expected: expected{scores: []float64{}},
		},
		{
			name: "zero maxPossible returns unchanged",
			input: input{
				maxPossible: 0,
				results: []SearchResult{
					{Id: "a", Score: 5.0}, {Id: "b", Score: 3.0},
				},
			},
			expected: expected{scores: []float64{5.0, 3.0}},
		},
		{
			name: "negative maxPossible returns unchanged",
			input: input{
				maxPossible: -1.0,
				results: []SearchResult{
					{Id: "a", Score: 5.0},
				},
			},
			expected: expected{scores: []float64{5.0}},
		},
		{
			name: "near-zero noise scores stay near-zero",
			input: input{
				maxPossible: 40.0,
				results: []SearchResult{
					{Id: "a", Score: 0.05}, {Id: "b", Score: 0.02},
					{Id: "c", Score: 0.001},
				},
			},
			expected: expected{scores: []float64{0.00125, 0.0005, 0.000025}},
		},
		{
			name: "snippets and metadata are preserved",
			input: input{
				maxPossible: 10.0,
				results: []SearchResult{
					{
						Id: "a", Score: 5.0, Snippet: "snippet-a",
						Metadata: map[string]any{"key": "value"},
					},
				},
			},
			expected: expected{scores: []float64{0.5}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := theoreticalMaxNormalize(tc.input.results, tc.input.maxPossible)
			assert.Len(t, result, len(tc.expected.scores))
			for i, expectedScore := range tc.expected.scores {
				assert.InDelta(t, expectedScore, result[i].Score, 0.0001,
					"score for result %d (%s)", i, result[i].Id)
			}
			// Verify snippets and metadata are preserved.
			for i, r := range tc.input.results {
				if i < len(result) {
					assert.Equal(t, r.Snippet, result[i].Snippet,
						"snippet for result %d", i)
					assert.Equal(t, r.Metadata, result[i].Metadata,
						"metadata for result %d", i)
				}
			}
		})
	}
}

func TestNormalizeBM25(t *testing.T) {
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
			name: "with theoretical max in metadata uses theoretical-max normalization",
			input: input{results: []SearchResult{
				{
					Id: "a", Score: 10.0,
					Metadata: map[string]any{TheoreticalMaxKey: 20.0},
				},
				{
					Id: "b", Score: 5.0,
					Metadata: map[string]any{TheoreticalMaxKey: 20.0},
				},
			}},
			expected: expected{scores: []float64{0.5, 0.25}},
		},
		{
			name: "without theoretical max passes through raw scores",
			input: input{results: []SearchResult{
				{Id: "a", Score: 10.0},
				{Id: "b", Score: 5.0},
			}},
			expected: expected{scores: []float64{10.0, 5.0}},
		},
		{
			name:     "empty results returns empty",
			input:    input{results: []SearchResult{}},
			expected: expected{scores: []float64{}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := normalizeBM25(tc.input.results)
			assert.Len(t, result, len(tc.expected.scores))
			for i, expectedScore := range tc.expected.scores {
				assert.InDelta(t, expectedScore, result[i].Score, 0.0001,
					"score for result %d (%s)", i, result[i].Id)
			}
		})
	}
}
