package search

// Chunk is a piece of text produced by a ChunkAdapter, ready for embedding. It carries the
// text content and optional metadata extracted during chunking (e.g., the Markdown heading
// hierarchy).
//
// The Text field contains the full content to be embedded, including any heading ancestors
// prepended by the chunker for context. The Metadata field stores structured information
// about where this chunk sits in the source document.
type Chunk struct {
	// Text is the chunk content to be embedded. For Markdown documents, this includes
	// heading ancestors prepended for context. For example, a paragraph under
	// "# Terms of Service > ## Refund Policy" gets those headings prepended:
	//
	//   # Terms of Service
	//   ## Refund Policy
	//   Full refunds are processed within 5-7 days...
	Text string

	// Snippet is the text to show in search results when this chunk matches. If empty,
	// Text is used as the snippet. This allows embedding-optimized text (e.g., synthetic
	// queries) to produce human-readable snippets from the actual document content.
	Snippet string

	// Metadata contains structured information about the chunk's position in the document.
	// For Markdown documents, the MarkdownChunker populates heading hierarchy:
	//   {"h1": "Terms of Service", "h2": "Refund Policy"}
	//
	// Currently used for future features (e.g. filtering). The primary
	// context mechanism is prepending heading ancestors to Text.
	Metadata map[string]string
}
