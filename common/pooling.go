// Package common provides shared types and constants used across gent packages.
// This package has no CGo dependencies and can be safely imported by any package.
package common

// PoolingStrategy determines how token-level embeddings are reduced to a single vector.
//
// WARNING: Pooling mismatch is the #1 compatibility trap when adding new models. Using mean
// pooling on a CLS-trained model (or vice versa) does NOT crash — it silently degrades
// retrieval quality by 5–15% nDCG@10. The model concentrated its semantic signal differently
// during training, and the wrong pooling dilutes that signal.
//
// Mean pooling models: e5 family, MiniLM, paraphrase-multilingual, nomic, GTE.
// CLS pooling models: BGE (small/base/M3), Snowflake Arctic (s/m).
type PoolingStrategy int

const (
	// PoolingMean averages all token embeddings weighted by the attention mask. Used by
	// e5, MiniLM, GTE, and nomic models. This is the default.
	PoolingMean PoolingStrategy = iota

	// PoolingCLS takes the first token's ([CLS]) embedding. Used by BGE and Arctic models.
	// The model concentrates semantic signal in the CLS token during training — averaging
	// across all tokens (mean pooling) dilutes that signal, causing 5-15% nDCG@10 loss.
	PoolingCLS
)
