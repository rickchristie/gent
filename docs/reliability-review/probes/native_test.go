//go:build gent_review && cgo

package gent_test

import (
	"context"
	"os"
	"testing"

	"github.com/rickchristie/gent/common"
	"github.com/rickchristie/gent/search"
	"github.com/stretchr/testify/assert"
	ort "github.com/yalue/onnxruntime_go"
)

func reviewEmbedder(tb testing.TB) (search.Embedder, []byte) {
	tb.Helper()
	data, err := os.ReadFile("../../../search/testdata/quantized_matmul.onnx")
	if err != nil {
		tb.Fatal(err)
	}
	cfg := common.ModelConfig{Model: common.ModelInfo{MaxTokenChunks: 16}, Dimensions: 2,
		Pooling: common.PoolingMean, InputNames: []string{"input_ids"},
		OutputName: "last_hidden_state"}
	embedder, err := search.NewOnnxEmbedder(cfg, search.OnnxOptions{
		ModelData: data, NumThreads: 1, MaxConcurrency: 1,
		TokenizerData: []byte(`{
  "version": "1.0",
  "model": {"type": "WordLevel", "vocab": {"[UNK]": 0, "x": 1}, "unk_token": "[UNK]"},
  "pre_tokenizer": {"type": "Whitespace"}
}`),
	})
	if err != nil {
		tb.Fatal(err)
	}
	return embedder, data
}

func TestReviewEmbedderChecksCanceledContext(t *testing.T) {
	embedder, _ := reviewEmbedder(t)
	defer func() { assert.NoError(t, embedder.Close()) }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	succeeded := 0
	for i := 0; i < 32; i++ {
		_, err := embedder.EmbedText(ctx, "x")
		if err == nil {
			succeeded++
		} else {
			assert.ErrorIs(t, err, context.Canceled)
		}
	}
	assert.Equal(t, 0, succeeded, "canceled requests still run inference")
}

func BenchmarkReviewOnnx(b *testing.B) {
	embedder, data := reviewEmbedder(b)
	defer func() {
		if err := embedder.Close(); err != nil {
			b.Error(err)
		}
	}()
	input, err := ort.NewTensor(ort.NewShape(1, 1), []int64{1})
	if err != nil {
		b.Fatal(err)
	}
	defer func() {
		if err := input.Destroy(); err != nil {
			b.Error(err)
		}
	}()
	output, err := ort.NewEmptyTensor[float32](ort.NewShape(1, 1, 2))
	if err != nil {
		b.Fatal(err)
	}
	defer func() {
		if err := output.Destroy(); err != nil {
			b.Error(err)
		}
	}()
	options, err := ort.NewSessionOptions()
	if err != nil {
		b.Fatal(err)
	}
	defer func() {
		if err := options.Destroy(); err != nil {
			b.Error(err)
		}
	}()
	if err := options.AddSessionConfigEntry("session.x64quantprecision", "1"); err != nil {
		b.Fatal(err)
	}
	session, err := ort.NewAdvancedSessionWithONNXData(data, []string{"input_ids"},
		[]string{"last_hidden_state"}, []ort.Value{input}, []ort.Value{output}, options)
	if err != nil {
		b.Fatal(err)
	}
	defer func() {
		if err := session.Destroy(); err != nil {
			b.Error(err)
		}
	}()
	b.Run("Gent", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := embedder.EmbedText(context.Background(), "x"); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("ReusedSession", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if err := session.Run(); err != nil {
				b.Fatal(err)
			}
		}
	})
}
