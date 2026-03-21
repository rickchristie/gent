package search

import "context"

// Embedder converts text into dense vector representations.
//
// The EmbedQuery and EmbedDocument distinction exists because some models (notably the e5 family)
// require different prefixes for queries vs documents. For e5-small, EmbedQuery prepends "query: "
// and EmbedDocument prepends "passage: ". Models that don't need prefixes implement both
// identically.
//
// Implementations must be safe for concurrent use.
type Embedder interface {
	// EmbedQuery produces a vector for a search query. For e5 models, prepends "query: ".
	EmbedQuery(ctx context.Context, text string) ([]float32, error)

	// EmbedDocument produces a vector for a document/passage being indexed.
	// For e5 models, prepends "passage: ".
	EmbedDocument(ctx context.Context, text string) ([]float32, error)

	// EmbedDocumentBatch produces vectors for multiple documents. More efficient than calling
	// EmbedDocument in a loop because it batches tokenization and inference.
	EmbedDocumentBatch(ctx context.Context, texts []string) ([][]float32, error)

	// Dimensions returns the output vector dimensionality. For multilingual-e5-small this is 384.
	Dimensions() int

	// Close releases model resources (ONNX session, tokenizer). Must be called when the
	// Embedder is no longer needed.
	Close() error
}
