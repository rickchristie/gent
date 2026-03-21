package search

// ChunkAdapter converts a domain document into one or more text chunks for embedding.
//
// Chunking is the caller's responsibility because only the caller knows the semantic boundaries
// of their data. A tool description is a single chunk. A 10-page policy document might be split
// into 20 chunks at section boundaries.
//
// Each returned chunk is embedded independently as a separate vector. On search, all chunks for
// a document are scored, but only the best-matching chunk's score (and text) is returned per
// document ID.
//
// For documents that don't need chunking, return a single-element slice.
//
// # Example for tools (no chunking)
//
//	type ToolChunkAdapter struct{}
//
//	func (a *ToolChunkAdapter) Convert(tool IndexableTool) ([]string, error) {
//	    text := fmt.Sprintf("%s: %s\nKeywords: %s",
//	        tool.Name(), tool.Description(), strings.Join(tool.Keywords(), ", "))
//	    return []string{text}, nil
//	}
//
// # Example for policies (chunking at section boundaries)
//
//	type PolicyChunkAdapter struct{}
//
//	func (a *PolicyChunkAdapter) Convert(policy Policy) ([]string, error) {
//	    return policy.Sections, nil  // pre-chunked by the caller
//	}
type ChunkAdapter[Doc any] interface {
	Convert(doc Doc) ([]string, error)
}
