//go:build cgo

package search

import (
	"context"
	_ "embed"
	"math"
	"testing"

	"github.com/rickchristie/gent/common"
	"github.com/stretchr/testify/assert"
	ort "github.com/yalue/onnxruntime_go"
)

//go:generate go run ./testdata/generate_quantized_matmul.go ./testdata/quantized_matmul.onnx

//go:embed testdata/quantized_matmul.onnx
var quantizedMatMulModel []byte

func TestOnnxEmbedder_QuantizedMatMul(t *testing.T) {
	// Token 1 becomes two UINT8 activations of 255. Multiplying by signed weights
	// [127, 127] must produce 64770, which overflows the intermediate INT16 sum in
	// ORT's default AVX2 kernel. The second dimension stays below that limit.
	cfg := common.ModelConfig{
		Model:      common.ModelInfo{MaxTokenChunks: 16},
		Dimensions: 2,
		Pooling:    common.PoolingMean,
		InputNames: []string{"input_ids"},
		OutputName: "last_hidden_state",
	}
	embedder, err := NewOnnxEmbedder(cfg, OnnxOptions{
		ModelData: quantizedMatMulModel,
		TokenizerData: []byte(`{
  "version": "1.0",
  "model": {"type": "WordLevel", "vocab": {"[UNK]": 0, "x": 1}, "unk_token": "[UNK]"},
  "pre_tokenizer": {"type": "Whitespace"}
}`),
	})
	if err != nil && !ort.IsInitialized() {
		t.Skipf("ONNX Runtime unavailable (run gent setup onnx): %v", err)
	}
	if !assert.NoError(t, err) {
		return
	}
	t.Cleanup(func() { assert.NoError(t, embedder.Close()) })

	type input struct {
		text string
	}
	type expected struct {
		embedding []float32
		err       error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}

	// These values follow directly from integer matrix multiplication, independent
	// of the model downloads and CPU instruction set. Compare the entire vector.
	norm := float32(math.Sqrt(64770*64770 + 32640*32640))
	tests := []testCase{
		{
			name:     "zero activations",
			input:    input{text: "unknown"},
			expected: expected{embedding: []float32{0, 0}},
		},
		{
			name:     "large activations preserve the integer sum",
			input:    input{text: "x"},
			expected: expected{embedding: []float32{64770 / norm, 32640 / norm}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			embedding, err := embedder.EmbedText(context.Background(), tt.input.text)
			assert.Equal(t, tt.expected, expected{embedding: embedding, err: err})
		})
	}
}
