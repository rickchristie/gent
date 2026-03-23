package search

// ChunkAdapter converts a domain document into one or more [Chunk] values for embedding.
//
// This is the most critical extension point for retrieval quality. Research shows that
// "chunking configuration had comparable or greater influence on retrieval quality than
// embedding model choice" (Vectara, NAACL 2025, 25 configurations × 48 embedding models).
//
// # Token-Aware Chunking
//
// The tokenCounter and maxTokens parameters enable token-aware chunking. tokenCounter returns
// the exact token count for a text using the model's actual tokenizer (~12μs per call, no
// neural network inference). maxTokens is the model's absolute maximum sequence length — any
// chunk exceeding this will be silently truncated during embedding, losing information.
//
// # Recommended Default
//
// Use [MarkdownChunker] for all text. It handles both Markdown-formatted and plain text,
// splits at heading boundaries with ancestor context, and degrades gracefully to
// paragraph/sentence/word splitting for non-Markdown content.
//
// # Examples
//
// Simple adapter for short documents (no chunking needed):
//
//	type SimpleAdapter struct{}
//	func (a *SimpleAdapter) Chunks(doc string, _ TokenCounter, _ int) ([]Chunk, error) {
//	    return []Chunk{{Text: doc}}, nil
//	}
//
// Token-aware adapter using MarkdownChunker:
//
//	type PolicyAdapter struct{}
//	func (a *PolicyAdapter) Chunks(
//	    policy Policy, tc TokenCounter, maxTokens int,
//	) ([]Chunk, error) {
//	    chunker := &search.MarkdownChunker{ChunkSize: 384, TokenCount: tc.TokenCount}
//	    return chunker.Chunk(policy.Content), nil
//	}
type ChunkAdapter[Doc any] interface {
	Chunks(doc Doc, tokenCounter TokenCounter, maxTokens int) ([]Chunk, error)
}
