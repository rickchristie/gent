package search

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

type cancelingEmbedder struct {
	*mockEmbedder
	cancel context.CancelFunc
}

func (e *cancelingEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	value, err := e.mockEmbedder.EmbedQuery(ctx, text)
	e.cancel()
	return value, err
}

func (e *cancelingEmbedder) EmbedTextBatch(
	ctx context.Context, texts []string,
) ([][]float32, error) {
	value, err := e.mockEmbedder.EmbedTextBatch(ctx, texts)
	e.cancel()
	return value, err
}

func TestFlatIndexCancellationDoesNotPublishUpdates(t *testing.T) {
	type input struct {
		operation func(context.Context, *FlatIndex[string]) error
		before    bool
	}
	type expected struct {
		results []SearchResult
		err     error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	search := func(ctx context.Context, index *FlatIndex[string]) error {
		results, err := index.Search(ctx, "query", 10)
		assert.Nil(t, results)
		return err
	}
	add := func(ctx context.Context, index *FlatIndex[string]) error {
		return index.Add(ctx, "new", "new")
	}
	swap := func(ctx context.Context, index *FlatIndex[string]) error {
		return index.Swap(ctx, map[string]string{"new": "new"})
	}
	want := expected{[]SearchResult{{Id: "old", Score: 1, Snippet: "old"}}, context.Canceled}
	tests := []testCase{
		{"search canceled before work", input{search, true}, want},
		{"search canceled during embedding", input{search, false}, want},
		{"add canceled before work", input{add, true}, want},
		{"add canceled during embedding", input{add, false}, want},
		{"swap canceled before work", input{swap, true}, want},
		{"swap canceled during embedding", input{swap, false}, want},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			embedder := &cancelingEmbedder{newMockEmbedder(map[string][]float32{
				"old": {1, 0, 0}, "new": {0, 1, 0}, "query": {1, 0, 0},
			}), func() {}}
			index := NewFlatIndex[string](&singleChunkAdapter{}, embedder)
			assert.NoError(t, index.Add(context.Background(), "old", "old"))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			embedder.cancel = cancel
			if tc.input.before {
				cancel()
			}
			err := tc.input.operation(ctx, index)
			assert.ErrorIs(t, err, tc.expected.err)
			results, err := index.Search(context.Background(), "query", 10)
			assert.NoError(t, err)
			assert.Equal(t, tc.expected.results, results)
		})
	}
}
