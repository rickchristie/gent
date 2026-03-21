// Package common provides shared types and constants used across gent packages.
// This package has no CGo dependencies and can be safely imported by any package.
package common

// PoolingStrategy determines how token-level embeddings are reduced to a single vector.
type PoolingStrategy int

const (
	// PoolingMean averages all token embeddings weighted by the attention mask. Used by
	// e5, MiniLM, GTE, and nomic models. This is the default.
	PoolingMean PoolingStrategy = iota

	// PoolingCLS takes the first token's ([CLS]) embedding. Used by BGE and Arctic models.
	// The model concentrates semantic signal in the CLS token during training.
	PoolingCLS
)
