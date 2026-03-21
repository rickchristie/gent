package search

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// storedVector is a single embedded chunk with its parent document ID.
type storedVector struct {
	docID  string
	chunk  string    // original chunk text (for Snippet in results)
	vector []float32 // the embedding
}

// FlatIndex provides brute-force vector search using cosine similarity.
//
// Documents are converted to text chunks via a [ChunkAdapter], embedded via an [Embedder], and
// stored in a flat array. Search scans all vectors and returns deduplicated results (one per
// document ID, using the best-matching chunk).
//
// Performance characteristics (384-dim, single CPU core):
//   - 1K vectors: < 1ms search
//   - 10K vectors: ~5ms search
//   - 100K vectors: ~40ms search
//
// Safe for concurrent use via sync.RWMutex.
type FlatIndex[Doc any] struct {
	adapter  ChunkAdapter[Doc]
	embedder Embedder
	vectors  []storedVector
	mu       sync.RWMutex
}

// NewFlatIndex creates a FlatIndex with the given chunk adapter and embedder.
func NewFlatIndex[Doc any](adapter ChunkAdapter[Doc], embedder Embedder) *FlatIndex[Doc] {
	return &FlatIndex[Doc]{adapter: adapter, embedder: embedder}
}

// Search returns the top-K most semantically similar documents to the query. When a document
// has multiple chunks, only the best-matching chunk's score and text are returned (max-score
// dedup per document ID).
func (f *FlatIndex[Doc]) Search(
	ctx context.Context, query string, topK int,
) ([]SearchResult, error) {
	queryVec, err := f.embedder.EmbedQuery(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("search: query embedding failed: %w", err)
	}

	f.mu.RLock()

	type scored struct {
		docID string
		chunk string
		score float64
	}

	all := make([]scored, len(f.vectors))
	for i, sv := range f.vectors {
		all[i] = scored{
			docID: sv.docID,
			chunk: sv.chunk,
			score: cosineSimilarity(queryVec, sv.vector),
		}
	}

	f.mu.RUnlock()

	// Deduplicate: keep best chunk per document ID.
	best := map[string]scored{}
	for _, s := range all {
		if existing, ok := best[s.docID]; !ok || s.score > existing.score {
			best[s.docID] = s
		}
	}

	// Sort by score descending, return top-K.
	results := make([]SearchResult, 0, len(best))
	for _, s := range best {
		results = append(results, SearchResult{Id: s.docID, Score: s.score, Snippet: s.chunk})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if topK > 0 && len(results) > topK {
		results = results[:topK]
	}
	return results, nil
}

// Add indexes a single document. The document is converted to chunks via the ChunkAdapter,
// each chunk is embedded, and all resulting vectors are stored. If a document with the same
// ID already exists, it is replaced.
func (f *FlatIndex[Doc]) Add(ctx context.Context, id string, doc Doc) error {
	chunks, err := f.adapter.Convert(doc)
	if err != nil {
		return fmt.Errorf("search: chunk adapter failed: %w", err)
	}

	embeddings, err := f.embedder.EmbedDocumentBatch(ctx, chunks)
	if err != nil {
		return fmt.Errorf("search: embedding failed: %w", err)
	}

	newVectors := make([]storedVector, len(chunks))
	for i, chunk := range chunks {
		newVectors[i] = storedVector{docID: id, chunk: chunk, vector: embeddings[i]}
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.removeLocked(id)
	f.vectors = append(f.vectors, newVectors...)
	return nil
}

// Remove deletes all vectors for a document ID.
func (f *FlatIndex[Doc]) Remove(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removeLocked(id)
	return nil
}

// Swap atomically replaces the entire index. New vectors are built outside the lock, then
// swapped in atomically.
func (f *FlatIndex[Doc]) Swap(ctx context.Context, docs map[string]Doc) error {
	var newVectors []storedVector
	for id, doc := range docs {
		chunks, err := f.adapter.Convert(doc)
		if err != nil {
			return fmt.Errorf("search: chunk adapter failed for %s: %w", id, err)
		}
		embeddings, err := f.embedder.EmbedDocumentBatch(ctx, chunks)
		if err != nil {
			return fmt.Errorf("search: embedding failed for %s: %w", id, err)
		}
		for i, chunk := range chunks {
			newVectors = append(newVectors, storedVector{
				docID: id, chunk: chunk, vector: embeddings[i],
			})
		}
	}

	f.mu.Lock()
	f.vectors = newVectors
	f.mu.Unlock()
	return nil
}

// removeLocked removes all vectors for a document ID. Caller must hold f.mu write lock.
// Unused capacity is zeroed to release references.
func (f *FlatIndex[Doc]) removeLocked(id string) {
	n := 0
	for _, sv := range f.vectors {
		if sv.docID != id {
			f.vectors[n] = sv
			n++
		}
	}
	// Zero out unused tail to release memory.
	for i := n; i < len(f.vectors); i++ {
		f.vectors[i] = storedVector{}
	}
	f.vectors = f.vectors[:n]
}

// cosineSimilarity computes cosine similarity between two L2-normalized vectors.
// When vectors are L2-normalized, cosine similarity equals the dot product.
func cosineSimilarity(a, b []float32) float64 {
	var dot float32
	for i := range a {
		dot += a[i] * b[i]
	}
	return float64(dot)
}

// Compile-time check.
var _ SearchIndex[any] = (*FlatIndex[any])(nil)
