package common

import "math"

// LayerNorm applies parameter-free layer normalization to a vector: normalizes to zero mean
// and unit variance. This is required as a post-processing step for nomic-embed-text-v1.5
// before L2 normalization.
//
// Use as PostProcess in EmbedderConfig:
//
//	cfg := search.EmbedderConfig{
//	    PostProcess: search.LayerNorm,
//	}
func LayerNorm(v []float32) []float32 {
	n := len(v)
	if n == 0 {
		return v
	}

	// Compute mean.
	var sum float64
	for _, x := range v {
		sum += float64(x)
	}
	mean := sum / float64(n)

	// Compute variance.
	var variance float64
	for _, x := range v {
		d := float64(x) - mean
		variance += d * d
	}
	variance /= float64(n)

	// Normalize: (x - mean) / sqrt(variance + eps).
	const eps = 1e-5
	invStd := float32(1.0 / math.Sqrt(variance+eps))
	meanF := float32(mean)
	out := make([]float32, n)
	for i, x := range v {
		out[i] = (x - meanF) * invStd
	}
	return out
}
