//go:build cgo

package search

import (
	"context"
	"math"
	"testing"

	"github.com/rickchristie/gent/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOnnxEmbedder_AllModels(t *testing.T) {
	for _, cfg := range common.ConfigRegistry {
		t.Run(cfg.ConfigName, func(t *testing.T) {
			embedder := testEmbedderForConfig(t, cfg)
			defer embedder.Close()

			t.Run("dimensions", func(t *testing.T) {
				assert.Equal(t, cfg.Dimensions, embedder.Dimensions())
				vec, err := embedder.EmbedQuery(context.Background(), "hello world")
				require.NoError(t, err)
				assert.Len(t, vec, cfg.Dimensions)
			})

			t.Run("l2_normalized", func(t *testing.T) {
				vec, err := embedder.EmbedQuery(context.Background(), "test normalization")
				require.NoError(t, err)
				var norm float64
				for _, x := range vec {
					norm += float64(x) * float64(x)
				}
				norm = math.Sqrt(norm)
				assert.InDelta(t, 1.0, norm, 0.01)
			})

			t.Run("similar_texts_close_vectors", func(t *testing.T) {
				ctx := context.Background()
				v1, err := embedder.EmbedQuery(ctx, "find cheap boarding house")
				require.NoError(t, err)
				v2, err := embedder.EmbedQuery(ctx, "affordable room for rent")
				require.NoError(t, err)
				v3, err := embedder.EmbedQuery(ctx, "quantum physics research papers")
				require.NoError(t, err)

				sim12 := dotProduct(v1, v2)
				sim13 := dotProduct(v1, v3)
				assert.Greater(t, sim12, sim13,
					"similar texts should score higher (%.3f vs %.3f)", sim12, sim13)
			})

			t.Run("query_vs_document_prefixes", func(t *testing.T) {
				if cfg.QueryPrefix == "" && cfg.PassagePrefix == "" {
					t.Skip("model has no prefixes")
				}
				ctx := context.Background()
				qVec, err := embedder.EmbedQuery(ctx, "check outstanding payments")
				require.NoError(t, err)
				dVec, err := embedder.EmbedText(ctx, "check outstanding payments")
				require.NoError(t, err)

				sim := dotProduct(qVec, dVec)
				assert.Greater(t, sim, 0.3,
					"same text with different prefix should be positively similar (%.3f)", sim)
				assert.Less(t, sim, 1.0, "different prefixes should differ slightly")
			})

			t.Run("batch_matches_single", func(t *testing.T) {
				ctx := context.Background()
				texts := []string{"billing payment", "send notification", "process checkout"}
				batch, err := embedder.EmbedTextBatch(ctx, texts)
				require.NoError(t, err)
				require.Len(t, batch, 3)

				for i, text := range texts {
					single, err := embedder.EmbedText(ctx, text)
					require.NoError(t, err)
					sim := dotProduct(batch[i], single)
					assert.InDelta(t, 1.0, sim, 0.001,
						"batch[%d] should match single for %q", i, text)
				}
			})

			t.Run("flat_index_end_to_end", func(t *testing.T) {
				adapter := &testSingleChunkAdapter{}
				idx := NewFlatIndex(adapter, embedder)
				ctx := context.Background()

				require.NoError(t, idx.Add(ctx, "billing",
					"Retrieve billing ledger entries and payment invoices for a customer"))
				require.NoError(t, idx.Add(ctx, "notify",
					"Send a notification via email or SMS"))
				require.NoError(t, idx.Add(ctx, "wifi",
					"Reset the wifi password for a guest room"))

				results, err := idx.Search(ctx,
					"look up customer invoice and payment history", 3)
				require.NoError(t, err)
				require.GreaterOrEqual(t, len(results), 1)
				assert.Equal(t, "billing", results[0].Id,
					"billing tool should rank first for invoice query")
			})

			// Cross-lingual test only for multilingual models.
			model := common.FindModel(cfg.Model.Name)
			if model != nil && model.Languages == "100+ languages" {
				t.Run("cross_lingual", func(t *testing.T) {
					adapter := &testSingleChunkAdapter{}
					idx := NewFlatIndex(adapter, embedder)
					ctx := context.Background()

					require.NoError(t, idx.Add(ctx, "kos",
						"Affordable boarding house near Sudirman"))
					require.NoError(t, idx.Add(ctx, "hotel",
						"Luxury 5-star hotel in central Jakarta"))

					results, err := idx.Search(ctx, "cari kos murah di sudirman", 2)
					require.NoError(t, err)
					require.GreaterOrEqual(t, len(results), 1)
					assert.Equal(t, "kos", results[0].Id)
					assert.Greater(t, results[0].Score, 0.5)
				})
			}
		})
	}
}

// dotProduct computes dot product between two vectors (cosine similarity for L2-normalized).
func dotProduct(a, b []float32) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var dot float64
	for i := range n {
		dot += float64(a[i]) * float64(b[i])
	}
	return dot
}

type testSingleChunkAdapter struct{}

func (a *testSingleChunkAdapter) Chunks(
	doc string, _ TokenCounter, _ int,
) ([]Chunk, error) {
	return []Chunk{{Text: doc}}, nil
}
