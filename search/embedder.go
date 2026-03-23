package search

import "context"

// TokenCounter counts how many tokens a text produces when tokenized by a specific model's
// tokenizer. Token counts are model-specific — the same text produces different token counts
// with different tokenizers (SentencePiece BPE vs WordPiece, different vocabulary sizes).
//
// TokenCount is cheap (~12μs) compared to embedding (~15-200ms) because it only runs the
// tokenizer, not the neural network. ChunkAdapters use this to make token-aware chunking
// decisions without the cost of embedding.
type TokenCounter interface {
	// TokenCount returns the number of tokens the text produces when tokenized.
	TokenCount(text string) int
}

// TokenCounterFunc is an adapter that lets an ordinary function satisfy the
// [TokenCounter] interface. Useful in tests to avoid constructing a full embedder.
type TokenCounterFunc func(text string) int

func (f TokenCounterFunc) TokenCount(text string) int { return f(text) }

// Embedder converts text into dense vector representations.
//
// The EmbedQuery and EmbedText distinction exists because some models (notably the e5 family)
// require different prefixes for queries vs documents. For e5-small, EmbedQuery prepends
// "query: " and EmbedText prepends "passage: ". Models that don't need prefixes implement
// both identically.
//
// Embedder also implements [TokenCounter] so that ChunkAdapters can make token-aware chunking
// decisions using the same tokenizer that will process the text during embedding.
//
// Implementations must be safe for concurrent use.
type Embedder interface {
	TokenCounter

	// EmbedQuery produces a vector for a search query. For e5 models, prepends "query: ".
	EmbedQuery(ctx context.Context, text string) ([]float32, error)

	// EmbedText produces a vector for a text passage being indexed. For e5 models, prepends
	// "passage: ". Renamed from EmbedDocument — the embedder doesn't know about documents,
	// it just embeds text with the passage prefix.
	EmbedText(ctx context.Context, text string) ([]float32, error)

	// EmbedTextBatch produces vectors for multiple text passages. More efficient than calling
	// EmbedText in a loop because it batches tokenization and inference.
	EmbedTextBatch(ctx context.Context, texts []string) ([][]float32, error)

	// MaxTokens returns the model's maximum sequence length in tokens. Inputs longer than
	// this are truncated during embedding. ChunkAdapters use this as the hard ceiling for
	// chunk sizes.
	MaxTokens() int

	// Dimensions returns the output vector dimensionality. For multilingual-e5-small this
	// is 384.
	Dimensions() int

	// Close releases model resources (ONNX session, tokenizer). Must be called when the
	// Embedder is no longer needed.
	Close() error
}
