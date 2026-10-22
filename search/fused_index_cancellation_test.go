package search

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Hold real FlatIndex work after FusedIndex has acquired its lock. Tests use only public index
// methods, so they also check that cancellation cannot publish a queued document update.
type heldIndexEmbedder struct {
	*mockEmbedder
	entered chan struct{}
	release chan struct{}
}

func (e *heldIndexEmbedder) hold(ctx context.Context) error {
	e.entered <- struct{}{}
	select {
	case <-e.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *heldIndexEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	if text == "held query" {
		if err := e.hold(ctx); err != nil {
			return nil, err
		}
	}
	return e.mockEmbedder.EmbedQuery(ctx, text)
}

func (e *heldIndexEmbedder) EmbedTextBatch(
	ctx context.Context, texts []string,
) ([][]float32, error) {
	if len(texts) == 1 && texts[0] == "held document" {
		if err := e.hold(ctx); err != nil {
			return nil, err
		}
	}
	return e.mockEmbedder.EmbedTextBatch(ctx, texts)
}

func newHeldFusedIndex(t *testing.T) (*FusedIndex[string], *heldIndexEmbedder, func()) {
	t.Helper()
	embedder := &heldIndexEmbedder{
		mockEmbedder: newMockEmbedder(map[string][]float32{
			"held query": {1, 0, 0}, "held document": {1, 0, 0}, "query": {1, 0, 0},
			"unexpected": {1, 0, 0}, "after": {1, 0, 0},
		}),
		entered: make(chan struct{}, 2), release: make(chan struct{}),
	}
	index := NewFusedIndex[string](
		&WeightedLinearFuser{Weights: map[string]float64{"flat": 1}},
		map[string]SearchIndex[string]{"flat": NewFlatIndex[string](&singleChunkAdapter{}, embedder)},
		nil,
	)
	release := sync.OnceFunc(func() { close(embedder.release) })
	t.Cleanup(release)
	return index, embedder, release
}

type fusedOperationResult struct {
	results []SearchResult
	err     error
}

func runFusedOperation(ctx context.Context, index *FusedIndex[string], name string,
) fusedOperationResult {
	switch name {
	case "search":
		results, err := index.Search(ctx, "query", 10)
		return fusedOperationResult{results, err}
	case "add":
		return fusedOperationResult{err: index.Add(ctx, "unexpected", "unexpected")}
	case "swap":
		return fusedOperationResult{err: index.Swap(ctx, map[string]string{"unexpected": "unexpected"})}
	default:
		panic("unknown test operation: " + name)
	}
}

func waitForFusedOperation(t *testing.T, done <-chan fusedOperationResult) fusedOperationResult {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-time.After(2 * time.Second):
		t.Fatal("index operation did not return")
		return fusedOperationResult{}
	}
}

func TestFusedIndexCanceledLockWait(t *testing.T) {
	type input struct {
		holder    string
		operation string
		cancel    string
	}
	type expected struct {
		result fusedOperationResult
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	var tests []testCase
	for _, holder := range []string{"add", "swap", "search"} {
		for _, operation := range []string{"search", "add", "swap"} {
			if holder == "search" && operation == "search" {
				continue // Readers must run together; a separate test checks this case.
			}
			for _, cancel := range []string{"before", "waiting", "expired", "deadline"} {
				err := context.Canceled
				if cancel == "expired" || cancel == "deadline" {
					err = context.DeadlineExceeded
				}
				tests = append(tests, testCase{
					name:     fmt.Sprintf("%s behind %s canceled %s", operation, holder, cancel),
					input:    input{holder, operation, cancel},
					expected: expected{fusedOperationResult{err: err}},
				})
			}
		}
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			index, embedder, release := newHeldFusedIndex(t)
			holderDone := make(chan fusedOperationResult, 1)
			go func() {
				switch tc.input.holder {
				case "add":
					holderDone <- fusedOperationResult{
						err: index.Add(context.Background(), "held", "held document")}
				case "swap":
					holderDone <- fusedOperationResult{err: index.Swap(context.Background(),
						map[string]string{"held": "held document"})}
				case "search":
					results, err := index.Search(context.Background(), "held query", 10)
					holderDone <- fusedOperationResult{results, err}
				}
			}()
			select {
			case <-embedder.entered:
			case <-time.After(2 * time.Second):
				t.Fatal("holder did not enter its subindex")
			}
			var ctx context.Context
			var cancel context.CancelFunc
			switch tc.input.cancel {
			case "expired":
				ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			case "deadline":
				ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
			default:
				ctx, cancel = context.WithCancel(context.Background())
			}
			defer cancel()
			if tc.input.cancel == "before" {
				cancel()
			}
			started := make(chan struct{})
			done := make(chan fusedOperationResult, 1)
			go func() {
				close(started)
				done <- runFusedOperation(ctx, index, tc.input.operation)
			}()
			<-started
			if tc.input.cancel == "waiting" {
				select {
				case result := <-done:
					t.Fatalf("operation passed a held lock: %+v", result)
				case <-time.After(20 * time.Millisecond):
				}
				cancel()
			}
			// The holder remains active until after this assertion. An ordinary blocking lock,
			// even with an early context check, cannot return after cancellation during the wait.
			actual := waitForFusedOperation(t, done)
			assert.Equal(t, tc.expected, expected{actual})
			if tc.input.holder == "search" {
				// Removing the canceled writer must let other readers proceed even while the
				// first reader still holds its permit. A detached blocking waiter would fail here.
				live, stop := context.WithTimeout(context.Background(), 2*time.Second)
				results, err := index.Search(live, "query", 10)
				stop()
				assert.Equal(t, fusedOperationResult{results: []SearchResult{}},
					fusedOperationResult{results, err})
			}
			release()
			held := waitForFusedOperation(t, holderDone)
			var want []SearchResult
			if tc.input.holder == "search" {
				want = []SearchResult{}
				assert.Equal(t, fusedOperationResult{results: want}, held)
			} else {
				assert.Equal(t, fusedOperationResult{}, held)
				want = []SearchResult{{Id: "held", Score: 1, Snippet: "held document",
					Metadata: map[string]any{"flat_raw": float64(1),
						"flat_normalized": float64(1), "flat_weighted": float64(1)}}}
			}
			live, stop := context.WithTimeout(context.Background(), 2*time.Second)
			defer stop()
			results, err := index.Search(live, "query", 10)
			assert.Equal(t, fusedOperationResult{want, nil}, fusedOperationResult{results, err})
			// Both lock modes must remain usable; a canceled waiter must not keep any permits.
			if !assert.NoError(t, index.Add(live, "after", "after")) {
				return
			}
			if !assert.NoError(t, index.Swap(live, map[string]string{"after": "after"})) {
				return
			}
			assert.NoError(t, index.Remove("after"))
		})
	}
}

func TestFusedIndexConcurrentSearches(t *testing.T) {
	index, embedder, release := newHeldFusedIndex(t)
	done := make(chan fusedOperationResult, 2)
	for i := 0; i < 2; i++ {
		go func() {
			results, err := index.Search(context.Background(), "held query", 10)
			done <- fusedOperationResult{results, err}
		}()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-embedder.entered:
		case <-time.After(2 * time.Second):
			t.Fatal("one search blocked another search")
		}
	}
	release()
	for i := 0; i < 2; i++ {
		assert.Equal(t, fusedOperationResult{results: []SearchResult{}}, waitForFusedOperation(t, done))
	}
}
