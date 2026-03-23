package search

import "github.com/rickchristie/gent/common"

// Re-export pooling types from common so callers can use search.PoolingMean without importing
// common directly.
type PoolingStrategy = common.PoolingStrategy

const (
	PoolingMean = common.PoolingMean
	PoolingCLS  = common.PoolingCLS
)

// OnnxOptions contains runtime-specific settings for the ONNX embedder. These are independent
// of the model semantics (which come from [common.ModelConfig]) and control how the ONNX
// Runtime executes on this machine.
//
// # Usage
//
//	cfg := common.FindConfig("multilingual-e5-small")
//	embedder, err := search.NewOnnxEmbedder(*cfg, search.OnnxOptions{
//	    ModelPath:     "~/.gent/models/multilingual-e5-small/model.onnx",
//	    TokenizerPath: "~/.gent/models/multilingual-e5-small/tokenizer.json",
//	})
type OnnxOptions struct {
	// ModelPath is the file path to the ONNX model file. Required unless ModelData is set.
	ModelPath string

	// TokenizerPath is the file path to the HuggingFace tokenizer.json. Required unless
	// TokenizerData is set.
	TokenizerPath string

	// ModelData is the raw ONNX model bytes. Alternative to ModelPath. ModelPath takes
	// precedence if both are set.
	ModelData []byte

	// TokenizerData is the raw tokenizer.json bytes. Alternative to TokenizerPath.
	TokenizerData []byte

	// OnnxLibraryPath overrides the ONNX Runtime shared library path. If empty, resolved
	// automatically: GENT_ORT_LIB env → ~/.gent/lib/ → system default.
	OnnxLibraryPath string

	// NumThreads controls ONNX Runtime intra-op parallelism. Default: 4.
	NumThreads int

	// MaxConcurrency limits concurrent ONNX inference calls via a semaphore. Default: 4.
	MaxConcurrency int
}
