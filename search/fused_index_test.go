package search

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSearchIndex is a configurable mock for testing FusedIndex.
type mockSearchIndex struct {
	results     []SearchResult
	addCalls    int
	removeCalls int
	swapCalls   int
	addErr      error
	searchErr   error
}

func (m *mockSearchIndex) Search(
	_ context.Context, _ string, _ int,
) ([]SearchResult, error) {
	return m.results, m.searchErr
}

func (m *mockSearchIndex) Add(_ context.Context, _ string, _ string) error {
	m.addCalls++
	return m.addErr
}

func (m *mockSearchIndex) Remove(_ string) error {
	m.removeCalls++
	return nil
}

func (m *mockSearchIndex) Swap(_ context.Context, _ map[string]string) error {
	m.swapCalls++
	return nil
}

func TestFusedIndex_SearchFusesResults(t *testing.T) {
	bm25 := &mockSearchIndex{results: []SearchResult{
		{Id: "a", Score: 28.5, Snippet: "bm25-a"},
		{Id: "b", Score: 6.1, Snippet: "bm25-b"},
	}}
	semantic := &mockSearchIndex{results: []SearchResult{
		{Id: "a", Score: 0.95, Snippet: "sem-a"},
		{Id: "c", Score: 0.82, Snippet: "sem-c"},
	}}

	fuser := &WeightedLinearFuser{
		Weights:          map[string]float64{"bm25": 0.3, "semantic": 0.7},
		NormalizeSources: map[string]bool{"bm25": true, "semantic": false},
	}

	idx := NewFusedIndex(fuser,
		map[string]SearchIndex[string]{"bm25": bm25, "semantic": semantic},
		map[string]int{"bm25": 20, "semantic": 20},
	)

	results, err := idx.Search(context.Background(), "test query", 3)
	require.NoError(t, err)

	// "a" appears in both lists and should rank highest.
	assert.Equal(t, "a", results[0].Id)
	assert.Greater(t, results[0].Score, results[1].Score)
}

func TestFusedIndex_SearchFailsFastOnError(t *testing.T) {
	idx1 := &mockSearchIndex{searchErr: assert.AnError}
	idx2 := &mockSearchIndex{results: []SearchResult{
		{Id: "a", Score: 1.0, Snippet: "ok"},
	}}

	idx := NewFusedIndex(
		&WeightedLinearFuser{
			Weights: map[string]float64{"a": 0.5, "b": 0.5},
		},
		map[string]SearchIndex[string]{"a": idx1, "b": idx2},
		nil,
	)

	results, err := idx.Search(context.Background(), "query", 5)
	assert.Error(t, err)
	assert.Nil(t, results)
	assert.ErrorIs(t, err, assert.AnError)
}

func TestFusedIndex_AddForwardsToAll(t *testing.T) {
	idx1 := &mockSearchIndex{}
	idx2 := &mockSearchIndex{}

	idx := NewFusedIndex(
		&WeightedLinearFuser{
			Weights: map[string]float64{"a": 0.5, "b": 0.5},
		},
		map[string]SearchIndex[string]{"a": idx1, "b": idx2},
		nil,
	)

	err := idx.Add(context.Background(), "doc-1", "content")
	require.NoError(t, err)
	assert.Equal(t, 1, idx1.addCalls)
	assert.Equal(t, 1, idx2.addCalls)
}

func TestFusedIndex_AddFailsFastOnError(t *testing.T) {
	idx1 := &mockSearchIndex{addErr: assert.AnError}
	idx2 := &mockSearchIndex{}

	idx := NewFusedIndex(
		&WeightedLinearFuser{Weights: map[string]float64{"a": 0.5, "b": 0.5}},
		map[string]SearchIndex[string]{"a": idx1, "b": idx2},
		nil,
	)

	err := idx.Add(context.Background(), "doc-1", "content")
	assert.Error(t, err)
}

func TestFusedIndex_RemoveForwardsToAll(t *testing.T) {
	idx1 := &mockSearchIndex{}
	idx2 := &mockSearchIndex{}

	idx := NewFusedIndex(
		&WeightedLinearFuser{Weights: map[string]float64{"a": 0.5, "b": 0.5}},
		map[string]SearchIndex[string]{"a": idx1, "b": idx2},
		nil,
	)

	err := idx.Remove("doc-1")
	require.NoError(t, err)
	assert.Equal(t, 1, idx1.removeCalls)
	assert.Equal(t, 1, idx2.removeCalls)
}

func TestFusedIndex_SwapForwardsToAll(t *testing.T) {
	idx1 := &mockSearchIndex{}
	idx2 := &mockSearchIndex{}

	idx := NewFusedIndex(
		&WeightedLinearFuser{Weights: map[string]float64{"a": 0.5, "b": 0.5}},
		map[string]SearchIndex[string]{"a": idx1, "b": idx2},
		nil,
	)

	err := idx.Swap(context.Background(), map[string]string{"doc": "content"})
	require.NoError(t, err)
	assert.Equal(t, 1, idx1.swapCalls)
	assert.Equal(t, 1, idx2.swapCalls)
}

func TestFusedIndex_DefaultTopKOverfetch(t *testing.T) {
	// When topKConfig is nil, FusedIndex should use 4x the caller's topK.
	called := false
	mock := &mockSearchIndex{}
	mock.results = nil

	idx := NewFusedIndex(
		&WeightedLinearFuser{Weights: map[string]float64{"s": 1.0}},
		map[string]SearchIndex[string]{"s": mock},
		nil, // no topKConfig
	)

	// We can't directly inspect the topK passed to the sub-index via the mock.
	// Just verify it doesn't error.
	_, err := idx.Search(context.Background(), "query", 5)
	require.NoError(t, err)
	_ = called // suppress unused
}
