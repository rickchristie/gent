package search

// FindKnee finds the knee point in a descending score distribution using the Kneedle
// algorithm (Satopaa et al., 2011). It returns the number of items to keep — the knee
// index + 1. If no knee is found, it returns len(scores).
//
// Scores must be sorted in descending order (convex-decreasing distribution). This is
// the natural shape of BM25 search results: a few high-scoring matches followed by a
// long tail of low-scoring noise.
//
// The sensitivity parameter S controls how much the difference curve must drop past a
// local maximum before declaring a knee:
//   - S=0: most aggressive, finds the first inflection point
//   - S=1: default, requires one x-step of decline — good general-purpose setting
//   - S>1: more conservative, requires a bigger drop before declaring a knee
//
// # Algorithm Overview
//
// The algorithm normalizes scores to [0,1], transforms the curve to increasing-concave
// form, computes a "difference curve" (distance from each point to the diagonal), finds
// where this curve peaks and then drops — that peak corresponds to the knee in the
// original data where scores transition from signal to noise.
//
// Reference: Satopaa, V., Albrecht, J., Irwin, D., Raghavan, B. (2011).
// "Finding a 'Kneedle' in a Haystack: Detecting Knee Points in System Behavior."
func FindKnee(scores []float64, sensitivity float64) int {
	n := len(scores)
	if n <= 2 {
		return n
	}

	// Step 1: Normalize x and y to [0, 1].
	yMin := scores[n-1] // sorted descending, last is min
	yMax := scores[0]   // first is max
	yRange := yMax - yMin
	if yRange == 0 {
		return n // all scores equal, no knee
	}

	xNorm := make([]float64, n)
	yNorm := make([]float64, n)
	for i := 0; i < n; i++ {
		xNorm[i] = float64(i) / float64(n-1)
		yNorm[i] = (scores[i] - yMin) / yRange
	}

	// Step 2: Transform for convex-decreasing → increasing-concave.
	// y_transformed = max(y_norm) - y_norm. Since max(y_norm) = 1.0:
	for i := 0; i < n; i++ {
		yNorm[i] = 1.0 - yNorm[i]
	}

	// Step 3: Compute difference curve (distance from diagonal y=x).
	yDiff := make([]float64, n)
	for i := 0; i < n; i++ {
		yDiff[i] = yNorm[i] - xNorm[i]
	}

	// Step 4: Find local maxima of the difference curve.
	maxima := localMaxima(yDiff)
	if len(maxima) == 0 {
		return n
	}

	// Step 5: Compute threshold for each maximum.
	// Threshold = peak_height - S * average_x_spacing.
	avgXSpacing := 1.0 / float64(n-1)
	thresholds := make([]float64, len(maxima))
	for i, idx := range maxima {
		thresholds[i] = yDiff[idx] - sensitivity*avgXSpacing
	}

	// Step 6: Find local minima (for detection state management).
	minima := localMinima(yDiff)
	minimaSet := make(map[int]bool, len(minima))
	for _, idx := range minima {
		minimaSet[idx] = true
	}

	// Step 7: Scan for knee — walk the difference curve looking for where
	// it drops below the threshold after a local maximum.
	maximaPtr := 0
	var threshold float64
	var thresholdIndex int
	detectionActive := true

	for i := maxima[0]; i < n-1; i++ {
		if maximaPtr < len(maxima) && i == maxima[maximaPtr] {
			threshold = thresholds[maximaPtr]
			thresholdIndex = i
			maximaPtr++
			detectionActive = true
		}
		if minimaSet[i] {
			threshold = 0
			detectionActive = false
		}
		// For convex-decreasing: knee maps directly to the threshold index.
		if detectionActive && yDiff[i+1] < threshold {
			return thresholdIndex + 1
		}
	}

	return n // no knee found
}

// KneeTruncate removes results below the knee point in a descending score distribution.
// Results must be sorted by score descending (as returned by BleveIndex.Search).
// Returns all results if no knee is found.
func KneeTruncate(results []SearchResult, sensitivity float64) []SearchResult {
	if len(results) <= 2 {
		return results
	}
	scores := make([]float64, len(results))
	for i, r := range results {
		scores[i] = r.Score
	}
	keep := FindKnee(scores, sensitivity)
	return results[:keep]
}

// localMaxima returns indices where values[i] >= values[i-1] AND values[i] >= values[i+1].
// Matches scipy.signal.argrelextrema with np.greater_equal.
func localMaxima(values []float64) []int {
	var result []int
	for i := 1; i < len(values)-1; i++ {
		if values[i] >= values[i-1] && values[i] >= values[i+1] {
			result = append(result, i)
		}
	}
	return result
}

// localMinima returns indices where values[i] <= values[i-1] AND values[i] <= values[i+1].
// Matches scipy.signal.argrelextrema with np.less_equal.
func localMinima(values []float64) []int {
	var result []int
	for i := 1; i < len(values)-1; i++ {
		if values[i] <= values[i-1] && values[i] <= values[i+1] {
			result = append(result, i)
		}
	}
	return result
}
