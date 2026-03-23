package search

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFindKnee(t *testing.T) {
	type input struct {
		scores      []float64
		sensitivity float64
	}

	type expected struct {
		keep int
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		// --- Ported from kneed library (Satopaa et al., 2011) ---
		// github.com/arvkevi/kneed test_sample.py

		{
			// convex_decreasing: rapid initial drop then long tail.
			// Knee at index 2 (score 20) — after this, scores flatten.
			// Python: KneeLocator(x, y, curve="convex", direction="decreasing")
			//         knee == 2
			name: "convex decreasing basic",
			input: input{
				scores:      []float64{100, 40, 20, 15, 10, 5, 4, 3, 2, 1},
				sensitivity: 1.0,
			},
			expected: expected{keep: 3},
		},
		{
			// flat_maxima with S=0: most aggressive, finds first inflection.
			// Python: KneeLocator(x, y, curve="convex", direction="decreasing", S=0.0)
			//         knee == 1.0
			name: "flat maxima S=0 finds first inflection",
			input: input{
				scores: []float64{
					1,
					0.787701317715959,
					0.7437774524158126,
					0.6559297218155198,
					0.5065885797950219,
					0.36749633967789164,
					0.2547584187408492,
					0.16251830161054173,
					0.10395314787701318,
					0.06734992679355783,
					0.043923865300146414,
					0.027818448023426062,
					0.01903367496339678,
					0.013177159590043924,
					0.010248901903367497,
					0.007320644216691069,
					0.005856515373352855,
					0.004392386530014641,
				},
				sensitivity: 0.0,
			},
			expected: expected{keep: 2},
		},
		{
			// flat_maxima with S=1: requires full x-step of decline.
			// Python: KneeLocator(x, y, curve="convex", direction="decreasing", S=1.0)
			//         knee == 8.0
			name: "flat maxima S=1 finds global knee",
			input: input{
				scores: []float64{
					1,
					0.787701317715959,
					0.7437774524158126,
					0.6559297218155198,
					0.5065885797950219,
					0.36749633967789164,
					0.2547584187408492,
					0.16251830161054173,
					0.10395314787701318,
					0.06734992679355783,
					0.043923865300146414,
					0.027818448023426062,
					0.01903367496339678,
					0.013177159590043924,
					0.010248901903367497,
					0.007320644216691069,
					0.005856515373352855,
					0.004392386530014641,
				},
				sensitivity: 1.0,
			},
			expected: expected{keep: 9},
		},
		{
			// bumpy convex decreasing: 90 data points with non-monotonic noise.
			// Python: KneeLocator(x, y, curve="convex", direction="decreasing",
			//                     interp_method="interp1d")
			//         knee == 26
			name: "bumpy convex decreasing",
			input: input{
				scores: []float64{
					7305.0, 6979.0, 6666.6, 6463.2, 6326.5, 6048.8, 6032.8,
					5762.0, 5742.8, 5398.2, 5256.8, 5227.0, 5001.7, 4942.0,
					4854.2, 4734.6, 4558.7, 4491.1, 4411.6, 4333.0, 4234.6,
					4139.1, 4056.8, 4022.5, 3868.0, 3808.3, 3745.3, 3692.3,
					3645.6, 3618.3, 3574.3, 3504.3, 3452.4, 3401.2, 3382.4,
					3340.7, 3301.1, 3247.6, 3190.3, 3180.0, 3154.2, 3089.5,
					3045.6, 2989.0, 2993.6, 2941.3, 2875.6, 2866.3, 2834.1,
					2785.1, 2759.7, 2763.2, 2720.1, 2660.1, 2690.2, 2635.7,
					2632.9, 2574.6, 2556.0, 2545.7, 2513.4, 2491.6, 2496.0,
					2466.5, 2442.7, 2420.5, 2381.5, 2388.1, 2340.6, 2335.0,
					2318.9, 2319.0, 2308.2, 2262.2, 2235.8, 2259.3, 2221.0,
					2202.7, 2184.3, 2170.1, 2160.0, 2127.7, 2134.7, 2102.0,
					2101.4, 2066.4, 2074.3, 2063.7, 2048.1, 2031.9,
				},
				sensitivity: 1.0,
			},
			expected: expected{keep: 27},
		},

		// --- Edge cases ---

		{
			name:     "empty scores returns 0",
			input:    input{scores: []float64{}, sensitivity: 1.0},
			expected: expected{keep: 0},
		},
		{
			name:     "single score returns 1",
			input:    input{scores: []float64{5.0}, sensitivity: 1.0},
			expected: expected{keep: 1},
		},
		{
			name:     "two scores returns 2",
			input:    input{scores: []float64{10.0, 5.0}, sensitivity: 1.0},
			expected: expected{keep: 2},
		},
		{
			// Python: KneeLocator(range(10), [1]*10)
			//         knee is None
			name:     "all equal scores returns all",
			input:    input{scores: []float64{5, 5, 5, 5, 5}, sensitivity: 1.0},
			expected: expected{keep: 5},
		},
		{
			// Linearly decreasing: no clear knee because the rate of decrease
			// is constant. The difference curve is flat, so no local maximum
			// stands out.
			name: "linearly decreasing returns all",
			input: input{
				scores:      []float64{10, 9, 8, 7, 6, 5, 4, 3, 2, 1},
				sensitivity: 1.0,
			},
			expected: expected{keep: 10},
		},

		// --- BM25 search result patterns ---

		{
			// One strong match, rest is noise. Knee at index 1 — the transition
			// point between steep drop and flat tail is included in the kept set.
			name: "single strong match with noise tail",
			input: input{
				scores: []float64{
					2.81, 0.07, 0.06, 0.05, 0.04, 0.03, 0.02, 0.01,
				},
				sensitivity: 1.0,
			},
			expected: expected{keep: 2},
		},
		{
			// Three good matches then noise. Knee at index 3 — the point where
			// scores drop from 0.90 to 0.05 is the maximum curvature point.
			name: "cluster of good matches then noise",
			input: input{
				scores: []float64{
					1.50, 1.20, 0.90, 0.05, 0.04, 0.03, 0.02, 0.01,
				},
				sensitivity: 1.0,
			},
			expected: expected{keep: 4},
		},
		{
			// All scores are very close (noise only) — no clear knee.
			name: "all noise scores returns all",
			input: input{
				scores: []float64{
					0.052, 0.048, 0.045, 0.043, 0.041, 0.039,
				},
				sensitivity: 1.0,
			},
			expected: expected{keep: 6},
		},

		// --- Sensitivity parameter tests ---

		{
			// S=3 with this data produces the same knee as S=1 because there's only
			// one local maximum in the difference curve. Higher sensitivity matters
			// when there are multiple local maxima (e.g., bumpy data).
			name: "higher sensitivity same knee with single maximum",
			input: input{
				scores:      []float64{100, 40, 20, 15, 10, 5, 4, 3, 2, 1},
				sensitivity: 3.0,
			},
			expected: expected{keep: 3},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := FindKnee(tc.input.scores, tc.input.sensitivity)
			assert.Equal(t, tc.expected.keep, result)
		})
	}
}

func TestKneeTruncate(t *testing.T) {
	type input struct {
		results     []SearchResult
		sensitivity float64
	}

	type expected struct {
		ids []string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "truncates at knee point",
			input: input{
				results: []SearchResult{
					{Id: "a", Score: 2.81},
					{Id: "b", Score: 0.07},
					{Id: "c", Score: 0.06},
					{Id: "d", Score: 0.05},
				},
				sensitivity: 1.0,
			},
			expected: expected{ids: []string{"a", "b"}},
		},
		{
			name: "keeps all when no knee found",
			input: input{
				results: []SearchResult{
					{Id: "a", Score: 10},
					{Id: "b", Score: 9},
					{Id: "c", Score: 8},
				},
				sensitivity: 1.0,
			},
			expected: expected{ids: []string{"a", "b", "c"}},
		},
		{
			name: "empty results returns empty",
			input: input{
				results:     []SearchResult{},
				sensitivity: 1.0,
			},
			expected: expected{ids: []string{}},
		},
		{
			name: "preserves snippets and metadata",
			input: input{
				results: []SearchResult{
					{Id: "a", Score: 5.0, Snippet: "snip-a",
						Metadata: map[string]any{"k": "v"}},
					{Id: "b", Score: 0.01, Snippet: "snip-b"},
				},
				sensitivity: 1.0,
			},
			expected: expected{ids: []string{"a", "b"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := KneeTruncate(tc.input.results, tc.input.sensitivity)
			ids := make([]string, len(result))
			for i, r := range result {
				ids[i] = r.Id
			}
			assert.Equal(t, tc.expected.ids, ids)

			// Verify snippets/metadata preserved.
			for i, r := range result {
				assert.Equal(t, tc.input.results[i].Snippet, r.Snippet)
				assert.Equal(t, tc.input.results[i].Metadata, r.Metadata)
			}
		})
	}
}
