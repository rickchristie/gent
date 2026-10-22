//go:build cgo

package search

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rickchristie/gent/common"
	"github.com/stretchr/testify/assert"
)

func newLifetimeEmbedder(
	t testing.TB, postProcess func([]float32) []float32, maxConcurrency int,
) Embedder {
	t.Helper()
	embedder, err := NewOnnxEmbedder(common.ModelConfig{
		Model: common.ModelInfo{MaxTokenChunks: 16}, Dimensions: 2, Pooling: common.PoolingMean,
		InputNames: []string{"input_ids"}, OutputName: "last_hidden_state", PostProcess: postProcess,
	}, OnnxOptions{ModelData: quantizedMatMulModel, MaxConcurrency: maxConcurrency, NumThreads: 1,
		TokenizerData: []byte(`{
  "version": "1.0",
  "model": {"type": "WordLevel", "vocab": {"[UNK]": 0, "x": 1}, "unk_token": "[UNK]"},
  "pre_tokenizer": {"type": "Whitespace"}
}`)})
	if errors.Is(err, ErrOnnxUnavailable) {
		t.Skipf("ONNX Runtime unavailable (run gent setup onnx): %v", err)
	}
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	t.Cleanup(func() { assert.NoError(t, embedder.Close()) })
	return embedder
}

func TestOnnxEmbedderCancellationAndReuse(t *testing.T) {
	var block atomic.Bool
	entered, release := make(chan struct{}), make(chan struct{})
	embedder := newLifetimeEmbedder(t, func(value []float32) []float32 {
		if block.Load() {
			close(entered)
			<-release
		}
		return value
	}, 1)
	norm := float32(math.Sqrt(64770*64770 + 32640*32640))
	want := []float32{64770 / norm, 32640 / norm}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for i := 0; i < 32; i++ {
		value, err := embedder.EmbedText(ctx, "x")
		assert.Nil(t, value)
		assert.ErrorIs(t, err, context.Canceled)
	}
	block.Store(true)
	done := make(chan struct{})
	go func() {
		defer close(done)
		value, err := embedder.EmbedText(context.Background(), "x")
		assert.NoError(t, err)
		assert.Equal(t, want, value)
	}()
	<-entered
	waiter, stop := context.WithCancel(context.Background())
	defer stop()
	waited := make(chan error, 1)
	go func() {
		value, err := embedder.EmbedText(waiter, "x")
		assert.Nil(t, value)
		waited <- err
	}()
	stop()
	select {
	case err := <-waited:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Error("a canceled call must not wait for a busy session")
	}
	block.Store(false)
	close(release)
	<-done
	// A canceled call must not poison the reusable native session or its run options.
	for i := 0; i < 32; i++ {
		live, stop := context.WithCancel(context.Background())
		value, err := embedder.EmbedText(live, "x")
		stop()
		assert.NoError(t, err)
		assert.Equal(t, want, value)
	}
}

func TestOnnxEmbedderCancellationDuringClose(t *testing.T) {
	type result struct {
		value []float32
		err   error
	}
	type input struct {
		maxConcurrency int
		deadline       bool
	}
	type expected struct {
		returnedBeforeClose bool
		active              []result
		waiting             result
		afterClose          result
		closeErrors         []error
		tokenCount          int
		postProcessCalls    int32
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	norm := float32(math.Sqrt(64770*64770 + 32640*32640))
	var tests []testCase
	for _, maxConcurrency := range []int{2, 4} {
		active := make([]result, maxConcurrency-1)
		for i := range active {
			active[i] = result{[]float32{64770 / norm, 32640 / norm}, nil}
		}
		for _, deadline := range []bool{false, true} {
			name, err := "cancel", context.Canceled
			if deadline {
				name, err = "deadline", context.DeadlineExceeded
			}
			tests = append(tests, testCase{
				name:  fmt.Sprintf("%s with %d permits", name, maxConcurrency),
				input: input{maxConcurrency, deadline},
				expected: expected{
					returnedBeforeClose: true,
					active:              active,
					waiting:             result{err: err},
					afterClose:          result{err: errors.New("search: embedder is closed")},
					closeErrors:         []error{nil, nil, nil, nil, nil},
					postProcessCalls:    int32(maxConcurrency), // Warmup and the held calls.
				},
			})
		}
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var hold atomic.Bool
			var calls atomic.Int32
			entered := make(chan struct{}, tc.input.maxConcurrency)
			release := make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			embedder := newLifetimeEmbedder(t, func(value []float32) []float32 {
				calls.Add(1)
				if hold.Load() {
					entered <- struct{}{}
					<-release
				}
				return value
			}, tc.input.maxConcurrency)
			waitResult := func(done <-chan result) result {
				t.Helper()
				select {
				case value := <-done:
					return value
				case <-time.After(2 * time.Second):
					t.Fatal("embedding call did not return")
					return result{}
				}
			}
			hold.Store(true)
			activeDone := make(chan result, tc.input.maxConcurrency-1)
			for i := 0; i < cap(activeDone); i++ {
				go func() {
					value, err := embedder.EmbedText(context.Background(), "x")
					activeDone <- result{value, err}
				}()
			}
			// With four permits, three calls must overlap before Close can start waiting.
			for i := 0; i < cap(activeDone); i++ {
				select {
				case <-entered:
				case <-time.After(2 * time.Second):
					t.Fatal("active call did not enter post-processing")
				}
			}
			closed := make(chan error, 4)
			for i := 0; i < cap(closed); i++ {
				go func() { closed <- embedder.Close() }()
			}
			select {
			case err := <-closed:
				t.Fatalf("close returned during active use: %v", err)
			case <-time.After(20 * time.Millisecond):
			}
			ctx, cancel := context.WithCancel(context.Background())
			if tc.input.deadline {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
			}
			defer cancel()
			waitingDone := make(chan result, 1)
			go func() {
				value, err := embedder.EmbedText(ctx, "x")
				waitingDone <- result{value, err}
			}()
			// Spare inference permits expose the old lifetime-lock wait behind Close. The
			// held calls must stay active until the queued call has observed cancellation.
			if !tc.input.deadline {
				select {
				case value := <-waitingDone:
					t.Fatalf("queued call returned before cancellation: %+v", value)
				case <-time.After(20 * time.Millisecond):
				}
				cancel()
			}
			<-ctx.Done()
			var actual expected
			select {
			case actual.waiting = <-waitingDone:
				actual.returnedBeforeClose = true
			case <-time.After(time.Second):
				unblock()
				actual.waiting = waitResult(waitingDone)
			}
			unblock()
			for i := 0; i < cap(activeDone); i++ {
				actual.active = append(actual.active, waitResult(activeDone))
			}
			// Close needs every permit. Completion proves that canceled waiters did not keep
			// a permit. Concurrent and repeated close must release the native resources once.
			for i := 0; i < cap(closed); i++ {
				select {
				case err := <-closed:
					actual.closeErrors = append(actual.closeErrors, err)
				case <-time.After(2 * time.Second):
					t.Fatal("close did not return after active use ended")
				}
			}
			actual.closeErrors = append(actual.closeErrors, embedder.Close())
			live, stop := context.WithTimeout(context.Background(), 2*time.Second)
			defer stop()
			actual.afterClose.value, actual.afterClose.err = embedder.EmbedText(live, "x")
			actual.tokenCount = embedder.TokenCount("x")
			actual.postProcessCalls = calls.Load()
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestOnnxEmbedderCloseDuringUse(t *testing.T) {
	// A native use-after-free can stop the process before Go can report a failure. Keep this
	// regression in its own process; the child also uses the race build when the parent does.
	if os.Getenv("GENT_NATIVE_CLOSE_CHILD") != "1" {
		newLifetimeEmbedder(t, nil, 1)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestOnnxEmbedderCloseDuringUse$")
		cmd.Env = append(os.Environ(), "GENT_NATIVE_CLOSE_CHILD=1")
		output, err := cmd.CombinedOutput()
		assert.NoError(t, err, "%s", output)
		return
	}
	for attempt := 0; attempt < 8; attempt++ {
		var block atomic.Bool
		entered, release := make(chan struct{}), make(chan struct{})
		embedder := newLifetimeEmbedder(t, func(value []float32) []float32 {
			if block.Load() {
				close(entered)
				<-release
			}
			return value
		}, 1)
		block.Store(true)
		done := make(chan error, 1)
		go func() {
			_, err := embedder.EmbedText(context.Background(), "x")
			done <- err
		}()
		<-entered
		var wg sync.WaitGroup
		start := make(chan struct{})
		for worker := 0; worker < 4; worker++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				for i := 0; i < 64; i++ {
					count := embedder.TokenCount(strings.Repeat("x ", 128))
					assert.True(t, count == 0 || count == 128, "unexpected count: %d", count)
				}
			}()
		}
		close(start)
		closed := make(chan error, 1)
		go func() { closed <- embedder.Close() }()
		select {
		case err := <-closed:
			assert.NoError(t, err)
			t.Fatal("close returned while an embedding still owned native resources")
		case <-time.After(10 * time.Millisecond):
		}
		close(release)
		assert.NoError(t, <-done)
		assert.NoError(t, <-closed)
		wg.Wait()
		assert.NoError(t, embedder.Close())
		value, err := embedder.EmbedText(context.Background(), "x")
		assert.Nil(t, value)
		assert.EqualError(t, err, "search: embedder is closed")
		assert.Equal(t, 0, embedder.TokenCount("x"))
	}
}

func BenchmarkOnnxEmbedderReuse(b *testing.B) {
	embedder := newLifetimeEmbedder(b, nil, 1)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := embedder.EmbedText(context.Background(), "x"); err != nil {
			b.Fatal(err)
		}
	}
}

func TestOnnxEmbedderConcurrentCallsKeepValues(t *testing.T) {
	embedder := newLifetimeEmbedder(t, nil, 4)
	norm := float32(math.Sqrt(64770*64770 + 32640*32640))
	var wg sync.WaitGroup
	start := make(chan struct{})
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			text, want := "x x", []float32{64770 / norm, 32640 / norm}
			if worker%2 == 0 {
				text, want = "unknown", []float32{0, 0}
			}
			for i := 0; i < 64; i++ {
				ctx, cancel := context.WithCancel(context.Background())
				value, err := embedder.EmbedText(ctx, text)
				cancel()
				assert.NoError(t, err)
				assert.Equal(t, want, value)
			}
		}()
	}
	close(start)
	wg.Wait()
}

func BenchmarkOnnxEmbedderModel(b *testing.B) {
	cfg := common.FindConfig("multilingual-e5-small")
	embedder := testEmbedderForConfig(b, *cfg)
	b.Cleanup(func() { assert.NoError(b, embedder.Close()) })
	const query = "Find the return policy for a damaged package."
	first, err := embedder.EmbedQuery(context.Background(), query)
	if !assert.NoError(b, err) {
		return
	}
	b.Run("serial", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			value, err := embedder.EmbedQuery(context.Background(), query)
			if !assert.NoError(b, err) || !assert.Equal(b, first, value) {
				return
			}
		}
	})
	b.Run("concurrent", func(b *testing.B) {
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				value, err := embedder.EmbedQuery(context.Background(), query)
				if !assert.NoError(b, err) || !assert.Equal(b, first, value) {
					return
				}
			}
		})
	})
}
