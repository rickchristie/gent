package search

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockEmbedder returns deterministic vectors for testing. Each unique text gets a
// unique direction in 3D space. Similar texts (sharing prefix) get close vectors.
type mockEmbedder struct {
	vectors map[string][]float32
}

func newMockEmbedder(vectors map[string][]float32) *mockEmbedder {
	return &mockEmbedder{vectors: vectors}
}

func (m *mockEmbedder) EmbedQuery(_ context.Context, text string) ([]float32, error) {
	if v, ok := m.vectors[text]; ok {
		return v, nil
	}
	return []float32{0, 0, 0}, nil
}

func (m *mockEmbedder) EmbedDocument(_ context.Context, text string) ([]float32, error) {
	return m.EmbedQuery(context.Background(), text)
}

func (m *mockEmbedder) EmbedDocumentBatch(
	_ context.Context, texts []string,
) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i, text := range texts {
		v, _ := m.EmbedQuery(context.Background(), text)
		result[i] = v
	}
	return result, nil
}

func (m *mockEmbedder) Dimensions() int { return 3 }
func (m *mockEmbedder) Close() error    { return nil }

// singleChunkAdapter returns the document string as a single chunk.
type singleChunkAdapter struct{}

func (a *singleChunkAdapter) Convert(doc string) ([]string, error) { return []string{doc}, nil }

// multiChunkAdapter splits the document by "|" delimiter.
type multiChunkAdapter struct{}

func (a *multiChunkAdapter) Convert(doc string) ([]string, error) {
	var chunks []string
	start := 0
	for i := range doc {
		if doc[i] == '|' {
			chunks = append(chunks, doc[start:i])
			start = i + 1
		}
	}
	chunks = append(chunks, doc[start:])
	return chunks, nil
}

// l2norm normalizes a float32 vector to unit length.
func l2norm(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	norm := float32(math.Sqrt(sum))
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = x / norm
	}
	return out
}

func TestFlatIndex_SearchReturnsBestMatch(t *testing.T) {
	// Three documents: "billing" is closest to query "payments".
	embedder := newMockEmbedder(map[string][]float32{
		"payments":    l2norm([]float32{1, 0, 0}),
		"billing":     l2norm([]float32{0.95, 0.05, 0}),
		"notification": l2norm([]float32{0, 1, 0}),
		"checkout":    l2norm([]float32{0, 0, 1}),
	})

	idx := NewFlatIndex(&singleChunkAdapter{}, embedder)
	ctx := context.Background()

	require.NoError(t, idx.Add(ctx, "billing-tool", "billing"))
	require.NoError(t, idx.Add(ctx, "notify-tool", "notification"))
	require.NoError(t, idx.Add(ctx, "checkout-tool", "checkout"))

	results, err := idx.Search(ctx, "payments", 2)
	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Equal(t, "billing-tool", results[0].Id)
	assert.Greater(t, results[0].Score, results[1].Score)
}

func TestFlatIndex_MultiChunkDedup(t *testing.T) {
	// Document with 3 chunks. Only one result should appear per doc ID, using best chunk.
	embedder := newMockEmbedder(map[string][]float32{
		"query":  l2norm([]float32{1, 0, 0}),
		"chunk1": l2norm([]float32{0.1, 0.9, 0}),  // low similarity
		"chunk2": l2norm([]float32{0.95, 0.05, 0}), // high similarity
		"chunk3": l2norm([]float32{0, 0, 1}),        // no similarity
	})

	idx := NewFlatIndex(&multiChunkAdapter{}, embedder)
	ctx := context.Background()

	require.NoError(t, idx.Add(ctx, "doc-1", "chunk1|chunk2|chunk3"))

	results, err := idx.Search(ctx, "query", 5)
	require.NoError(t, err)

	// Only one result for doc-1 despite 3 chunks.
	assert.Len(t, results, 1)
	assert.Equal(t, "doc-1", results[0].Id)
	assert.Equal(t, "chunk2", results[0].Snippet) // best-matching chunk
}

func TestFlatIndex_AddReplacesExisting(t *testing.T) {
	embedder := newMockEmbedder(map[string][]float32{
		"query": l2norm([]float32{1, 0, 0}),
		"old":   l2norm([]float32{0, 1, 0}),
		"new":   l2norm([]float32{0.9, 0.1, 0}),
	})

	idx := NewFlatIndex(&singleChunkAdapter{}, embedder)
	ctx := context.Background()

	require.NoError(t, idx.Add(ctx, "doc", "old"))
	require.NoError(t, idx.Add(ctx, "doc", "new")) // replace

	results, err := idx.Search(ctx, "query", 5)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "new", results[0].Snippet) // should be the new content
}

func TestFlatIndex_Remove(t *testing.T) {
	embedder := newMockEmbedder(map[string][]float32{
		"query": l2norm([]float32{1, 0, 0}),
		"a":     l2norm([]float32{0.9, 0.1, 0}),
		"b":     l2norm([]float32{0, 1, 0}),
	})

	idx := NewFlatIndex(&singleChunkAdapter{}, embedder)
	ctx := context.Background()

	require.NoError(t, idx.Add(ctx, "doc-a", "a"))
	require.NoError(t, idx.Add(ctx, "doc-b", "b"))
	require.NoError(t, idx.Remove("doc-a"))

	results, err := idx.Search(ctx, "query", 5)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "doc-b", results[0].Id)
}

func TestFlatIndex_RemoveClearsMemory(t *testing.T) {
	embedder := newMockEmbedder(map[string][]float32{
		"a": l2norm([]float32{1, 0, 0}),
		"b": l2norm([]float32{0, 1, 0}),
	})

	idx := NewFlatIndex(&multiChunkAdapter{}, embedder)
	ctx := context.Background()

	// Add doc with 2 chunks, then remove. Backing array tail should be zeroed.
	require.NoError(t, idx.Add(ctx, "doc", "a|b"))
	assert.Len(t, idx.vectors, 2)

	require.NoError(t, idx.Remove("doc"))
	assert.Len(t, idx.vectors, 0)

	// Verify the backing array capacity is still there but zeroed.
	fullSlice := idx.vectors[:cap(idx.vectors)]
	for i, sv := range fullSlice {
		assert.Empty(t, sv.docID, "docID at index %d should be zeroed", i)
		assert.Empty(t, sv.chunk, "chunk at index %d should be zeroed", i)
		assert.Nil(t, sv.vector, "vector at index %d should be nil", i)
	}
}

func TestFlatIndex_Swap(t *testing.T) {
	embedder := newMockEmbedder(map[string][]float32{
		"query": l2norm([]float32{1, 0, 0}),
		"old":   l2norm([]float32{0, 1, 0}),
		"new":   l2norm([]float32{0.9, 0.1, 0}),
	})

	idx := NewFlatIndex(&singleChunkAdapter{}, embedder)
	ctx := context.Background()

	require.NoError(t, idx.Add(ctx, "old-doc", "old"))

	// Swap replaces everything.
	require.NoError(t, idx.Swap(ctx, map[string]string{"new-doc": "new"}))

	results, err := idx.Search(ctx, "query", 5)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "new-doc", results[0].Id)
}

func TestFlatIndex_EmptyIndex(t *testing.T) {
	embedder := newMockEmbedder(map[string][]float32{
		"query": l2norm([]float32{1, 0, 0}),
	})

	idx := NewFlatIndex(&singleChunkAdapter{}, embedder)
	results, err := idx.Search(context.Background(), "query", 5)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestFlatIndex_TopKLimitsResults(t *testing.T) {
	embedder := newMockEmbedder(map[string][]float32{
		"query": l2norm([]float32{1, 0, 0}),
		"a":     l2norm([]float32{0.9, 0.1, 0}),
		"b":     l2norm([]float32{0.8, 0.2, 0}),
		"c":     l2norm([]float32{0.7, 0.3, 0}),
	})

	idx := NewFlatIndex(&singleChunkAdapter{}, embedder)
	ctx := context.Background()

	require.NoError(t, idx.Add(ctx, "a", "a"))
	require.NoError(t, idx.Add(ctx, "b", "b"))
	require.NoError(t, idx.Add(ctx, "c", "c"))

	results, err := idx.Search(ctx, "query", 2)
	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Equal(t, "a", results[0].Id)
	assert.Equal(t, "b", results[1].Id)
}

func TestCosineSimilarity(t *testing.T) {
	type input struct {
		a, b []float32
	}

	type expected struct {
		score float64
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name:     "identical normalized vectors score 1.0",
			input:    input{a: l2norm([]float32{1, 0, 0}), b: l2norm([]float32{1, 0, 0})},
			expected: expected{score: 1.0},
		},
		{
			name:     "orthogonal vectors score 0.0",
			input:    input{a: l2norm([]float32{1, 0, 0}), b: l2norm([]float32{0, 1, 0})},
			expected: expected{score: 0.0},
		},
		{
			name:     "opposite vectors score -1.0",
			input:    input{a: l2norm([]float32{1, 0, 0}), b: l2norm([]float32{-1, 0, 0})},
			expected: expected{score: -1.0},
		},
		{
			name:  "similar vectors score high",
			input: input{a: l2norm([]float32{1, 0.1, 0}), b: l2norm([]float32{1, 0, 0})},
			expected: expected{score: 0.995}, // close to 1.0
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			score := cosineSimilarity(tc.input.a, tc.input.b)
			assert.InDelta(t, tc.expected.score, score, 0.01)
		})
	}
}
