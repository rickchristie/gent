package search

import "github.com/rickchristie/gent/common"

// Re-export pooling types from common so callers can use search.PoolingMean without importing
// common directly.
type PoolingStrategy = common.PoolingStrategy

const (
	PoolingMean = common.PoolingMean
	PoolingCLS  = common.PoolingCLS
)

// EmbedderConfig controls the behavior of the ONNX-based Embedder.
//
// The embedder works with any BERT-family sentence embedding model exported to ONNX
// (e5, MiniLM, BGE, Nomic, GTE, etc.). Model-specific parameters (dimensions, prefixes,
// sequence length, pooling, ONNX I/O names) are configurable with multilingual-e5-small
// defaults.
type EmbedderConfig struct {
	// ModelPath is the file path to the ONNX model (e.g., "/opt/models/model.onnx").
	// Either ModelPath or ModelData must be provided.
	ModelPath string

	// TokenizerPath is the file path to the HuggingFace tokenizer.json file.
	// Either TokenizerPath or TokenizerData must be provided.
	TokenizerPath string

	// ModelData is the raw ONNX model bytes. Alternative to ModelPath for users who load
	// model data themselves (e.g., from S3, embedded in binary). If ModelPath is also set,
	// ModelPath takes precedence.
	ModelData []byte

	// TokenizerData is the raw tokenizer.json bytes. Alternative to TokenizerPath. If
	// TokenizerPath is also set, TokenizerPath takes precedence.
	TokenizerData []byte

	// OnnxLibraryPath is the path to the ONNX Runtime shared library (libonnxruntime.so on
	// Linux, onnxruntime.dll on Windows). If empty, the default system path is used.
	OnnxLibraryPath string

	// Dimensions is the final output vector dimensionality. Default: 384. For Matryoshka
	// configs where PostProcess truncates, this is the truncated size (e.g., 384), and
	// ModelDimensions is the model's native hidden dim (e.g., 768).
	Dimensions int

	// ModelDimensions is the ONNX model's native hidden dimension. If 0, defaults to
	// Dimensions. Set this when PostProcess truncates the output (e.g., nomic Matryoshka).
	ModelDimensions int

	// Pooling determines how token embeddings are reduced to a sentence vector. Default:
	// PoolingMean. Set to PoolingCLS for BGE and Arctic models.
	Pooling PoolingStrategy

	// NumThreads controls ONNX Runtime intra-op parallelism. Default: 4.
	NumThreads int

	// MaxConcurrency limits concurrent ONNX inference calls via a semaphore. Default: 4.
	MaxConcurrency int

	// MaxSequenceLength is the maximum token length per input. Inputs longer than this are
	// truncated. Default: 512. Set to 256 for MiniLM, 8192 for Nomic, etc.
	MaxSequenceLength int

	// QueryPrefix is prepended to query text before embedding. Default: "query: " (e5).
	// Set to "" for models that don't use prefixes (MiniLM, GTE).
	// Set to "search_query: " for Nomic.
	QueryPrefix string

	// PassagePrefix is prepended to document text before embedding. Default: "passage: " (e5).
	// Set to "" for models that don't use prefixes.
	// Set to "search_document: " for Nomic.
	PassagePrefix string

	// InputNames are the ONNX model's input tensor names. Default:
	// ["input_ids", "attention_mask", "token_type_ids"] (standard for BERT-family models).
	InputNames []string

	// OutputName is the ONNX model's output tensor name. Default: "last_hidden_state".
	// Sentence-transformers exports use "token_embeddings" instead.
	OutputName string

	// PostProcess is called after pooling and before L2 normalization. If nil, no
	// post-processing is applied (default for most models). The search package exports
	// reusable post-processors:
	//   - search.LayerNorm — parameter-free layer normalization (for nomic-embed-text-v1.5)
	//
	// Custom post-processors can compose exported building blocks:
	//
	//   PostProcess: func(v []float32) []float32 {
	//       v = search.LayerNorm(v)
	//       return v[:256] // Matryoshka truncation
	//   }
	PostProcess func([]float32) []float32
}

// DefaultEmbedderConfig returns configuration for multilingual-e5-small with INT8 quantization.
// Override fields as needed for other models.
func DefaultEmbedderConfig() EmbedderConfig {
	return EmbedderConfig{
		Dimensions:        384,
		Pooling:           PoolingMean,
		NumThreads:        4,
		MaxConcurrency:    4,
		MaxSequenceLength: 512,
		QueryPrefix:       "query: ",
		PassagePrefix:     "passage: ",
		InputNames:        []string{"input_ids", "attention_mask", "token_type_ids"},
		OutputName:        "last_hidden_state",
	}
}
