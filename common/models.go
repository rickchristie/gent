package common

import (
	"fmt"
	"os"
	"path/filepath"
)

// ModelInfo describes an embedding model available for download. These are physical properties
// of the ONNX model file — they don't change regardless of how the model is used.
//
// # What is an embedding model?
//
// An embedding model converts text into a dense vector (array of numbers) that captures the
// text's meaning. Similar texts produce similar vectors, enabling semantic search — finding
// relevant results by meaning, not just keywords. For example, "affordable room for rent"
// and "cheap boarding house" produce vectors that are close together, even though they share
// no words.
//
// # Why multiple models?
//
// Different models make different tradeoffs. Smaller models (22M params, 23MB) are fast and
// use little memory but only work in English. Larger multilingual models (118M params, 113MB)
// work across 100+ languages but need more resources. The model list (gent model list) helps
// you choose.
type ModelInfo struct {
	// Name is the CLI identifier used for downloads (e.g., "multilingual-e5-small").
	Name string

	// HuggingFace is the model's identifier on huggingface.co (e.g., "intfloat/multilingual-e5-small").
	HuggingFace string

	// Params is the parameter count as a human-readable string (e.g., "118M"). More parameters
	// generally means higher quality but more memory and slower inference.
	Params string

	// Size is the approximate INT8 ONNX file size (e.g., "113MB"). This is the download size.
	// Runtime memory is typically 3-4x larger due to ONNX Runtime session overhead.
	Size string

	// Languages describes the model's language support (e.g., "100+ languages" or "English").
	Languages string

	// MaxTokenChunks is the model's absolute maximum input length in tokens. Inputs longer
	// than this are truncated during embedding, losing information silently. This is a physical
	// property of the model architecture and cannot be changed.
	//
	// For most BERT-based models this is 512. For MiniLM it's 256. For nomic it's 8192.
	// ChunkAdapters use this as the hard ceiling — no chunk should exceed this.
	MaxTokenChunks int

	// ModelFile is the filename of the ONNX model (e.g., "model_qint8_avx512_vnni.onnx").
	ModelFile string

	// ModelURL is the download URL for the ONNX model file.
	ModelURL string

	// TokenizerURL is the download URL for the tokenizer.json file.
	TokenizerURL string

	// Notes is additional information shown in the model list (e.g., "INT8 from Teradata fork").
	Notes string
}

// ModelConfig describes how to use a downloaded model with the ONNX embedder. One model can
// have multiple configs — for example, nomic-embed-text-v1.5 has a 768d config and a 384d
// Matryoshka config that truncates vectors for lower memory usage.
//
// # Why separate ModelInfo and ModelConfig?
//
// ModelInfo is about the file on disk — what to download, how big it is. ModelConfig is about
// how to run it — what dimensions to output, what prefixes to add, how to pool. A single
// model file can be used in multiple ways (e.g., nomic at full 768d or truncated 384d).
//
// # Key concepts
//
// Dimensions: The size of the output vector. A 384-dimensional vector is 384 numbers.
// Higher dimensions capture more nuance but use more memory (384d = 1.5KB per vector,
// 768d = 3KB per vector).
//
// Pooling: How to combine per-token embeddings into one vector for the whole text. Mean
// pooling averages all tokens (most common). CLS pooling takes only the first token's
// embedding (used by BGE and Snowflake models).
//
// Prefixes: Some models (e5 family) were trained with specific text prefixes. Queries get
// "query: " prepended, documents get "passage: ". Without prefixes, retrieval quality
// drops 10-20%. Models without prefix requirements (MiniLM, GTE) leave these empty.
//
// OptimalChunkTokens: Research shows chunk size matters as much as model choice for retrieval
// quality. Smaller models (384d, 33M params) work best with 200-256 token chunks. Larger
// models (768d, 278M params) handle 512 tokens well. These defaults follow the evidence
// from Vectara (NAACL 2025) and Fraunhofer IAIS (2025).
type ModelConfig struct {
	// Model is the physical model this config uses. Stored as a value (not pointer) to prevent
	// accidental mutation of shared defaults.
	Model ModelInfo

	// ConfigName uniquely identifies this configuration (e.g., "nomic-embed-text-v1.5-384d").
	ConfigName string

	// Description is a short explanation of what this config is for.
	Description string

	// Dimensions is the final output vector dimensionality after any PostProcess truncation.
	// For most configs this equals the model's native hidden dim. For Matryoshka configs
	// (e.g., nomic-384d), this is the truncated size.
	Dimensions int

	// ModelDimensions is the ONNX model's native hidden dimension. If 0, defaults to
	// Dimensions. Set this when PostProcess truncates the output — the ONNX output tensor
	// must use the native size, then PostProcess reduces it.
	ModelDimensions int

	// Pooling determines how per-token embeddings are reduced to a single sentence vector.
	// PoolingMean (default) averages all tokens weighted by attention mask — used by e5,
	// MiniLM, GTE, nomic. PoolingCLS takes the first token's ([CLS]) embedding — used by
	// BGE and Snowflake Arctic.
	Pooling PoolingStrategy

	// QueryPrefix is prepended to query text before embedding. For e5 models: "query: ".
	// For nomic: "search_query: ". For BGE: "Represent this sentence for searching relevant
	// passages: ". Empty for models that don't use prefixes (MiniLM, GTE).
	QueryPrefix string

	// PassagePrefix is prepended to document/passage text before embedding. For e5 models:
	// "passage: ". For nomic: "search_document: ". Empty for BGE (query-only prefix) and
	// models without prefixes.
	PassagePrefix string

	// InputNames are the ONNX model's input tensor names, in order. Most BERT models use
	// ["input_ids", "attention_mask", "token_type_ids"]. XLMRoberta-based models (e5-base)
	// use only ["input_ids", "attention_mask"]. The embedder creates tensors matching these
	// names — wrong names cause ONNX Runtime errors.
	InputNames []string

	// OutputName is the ONNX model's output tensor name. Standard exports use
	// "last_hidden_state". Sentence-transformers Teradata exports use "token_embeddings".
	OutputName string

	// OptimalChunkTokens is the recommended chunk size in tokens for this config. Research
	// shows this matters as much as model choice for retrieval quality (Vectara NAACL 2025).
	//
	// Evidence-based defaults by model capacity:
	//   - 384d + small params (22-33M): 200-256 tokens
	//   - 384d + large params (118M+):  384 tokens
	//   - 768d models:                  512 tokens
	//
	// The ChunkAdapter uses this as the target chunk size. Users can override it.
	OptimalChunkTokens int

	// PostProcess is called after pooling and before L2 normalization. Most models don't
	// need this (nil). Nomic-embed-text-v1.5 requires LayerNorm before Matryoshka truncation.
	// Custom functions can compose: LayerNorm → truncate → return.
	PostProcess func([]float32) []float32

	// ModelOverheadMB is the approximate ONNX Runtime memory usage in MB, measured via RSS.
	// This is the fixed cost of loading the model — 3-4x the file size due to decompressed
	// weights and session buffers. Used by EstimateMemoryMB for capacity planning.
	ModelOverheadMB int

	// BestFor is a short description of the ideal use case for display in the CLI.
	BestFor string
}

// EstimateMemoryMB returns estimated total memory in MB for a given corpus size. textBytes
// is the total size of all source text before chunking.
func (c ModelConfig) EstimateMemoryMB(textBytes int) int {
	maxTokens := c.Model.MaxTokenChunks
	if maxTokens == 0 {
		maxTokens = 512
	}
	scalingFactor := 1.0 + float64(c.Dimensions)/float64(maxTokens)
	indexBytes := float64(textBytes) * scalingFactor
	return c.ModelOverheadMB + int(indexBytes/(1024*1024))
}

// HiddenDim returns the ONNX model's native hidden dimension. If ModelDimensions is 0,
// returns Dimensions (they're the same for most models).
func (c ModelConfig) HiddenDim() int {
	if c.ModelDimensions > 0 {
		return c.ModelDimensions
	}
	return c.Dimensions
}

// ModelRegistry is the list of models available for download.
var ModelRegistry = []ModelInfo{
	{
		Name: "multilingual-e5-small", HuggingFace: "intfloat/multilingual-e5-small",
		Params: "118M", Size: "113MB", Languages: "100+ languages", MaxTokenChunks: 512,
		ModelFile: "model_qint8_avx512_vnni.onnx",
		ModelURL: "https://huggingface.co/intfloat/multilingual-e5-small/" +
			"resolve/main/onnx/model_qint8_avx512_vnni.onnx",
		TokenizerURL: "https://huggingface.co/intfloat/multilingual-e5-small/" +
			"resolve/main/onnx/tokenizer.json",
	},
	{
		Name: "multilingual-e5-base", HuggingFace: "intfloat/multilingual-e5-base",
		Params: "278M", Size: "266MB", Languages: "100+ languages", MaxTokenChunks: 512,
		ModelFile: "model_qint8_avx512_vnni.onnx",
		ModelURL: "https://huggingface.co/intfloat/multilingual-e5-base/" +
			"resolve/main/onnx/model_qint8_avx512_vnni.onnx",
		TokenizerURL: "https://huggingface.co/intfloat/multilingual-e5-base/" +
			"resolve/main/onnx/tokenizer.json",
	},
	{
		Name: "e5-small-v2", HuggingFace: "intfloat/e5-small-v2",
		Params: "33M", Size: "34MB", Languages: "English", MaxTokenChunks: 512,
		ModelFile: "model_qint8_avx512_vnni.onnx",
		ModelURL: "https://huggingface.co/intfloat/e5-small-v2/" +
			"resolve/main/onnx/model_qint8_avx512_vnni.onnx",
		TokenizerURL: "https://huggingface.co/intfloat/e5-small-v2/" +
			"resolve/main/tokenizer.json",
	},
	{
		Name: "all-MiniLM-L6-v2", HuggingFace: "sentence-transformers/all-MiniLM-L6-v2",
		Params: "22.7M", Size: "23MB", Languages: "English", MaxTokenChunks: 256,
		ModelFile: "model_qint8_avx512_vnni.onnx",
		ModelURL: "https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/" +
			"resolve/main/onnx/model_qint8_avx512_vnni.onnx",
		TokenizerURL: "https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/" +
			"resolve/main/tokenizer.json",
	},
	{
		Name: "bge-small-en-v1.5", HuggingFace: "BAAI/bge-small-en-v1.5",
		Params: "33.4M", Size: "34MB", Languages: "English", MaxTokenChunks: 512,
		ModelFile: "model_int8.onnx", Notes: "INT8 from Teradata fork",
		ModelURL: "https://huggingface.co/Teradata/bge-small-en-v1.5/" +
			"resolve/main/onnx/model_int8.onnx",
		TokenizerURL: "https://huggingface.co/BAAI/bge-small-en-v1.5/" +
			"resolve/main/tokenizer.json",
	},
	{
		Name: "bge-base-en-v1.5", HuggingFace: "BAAI/bge-base-en-v1.5",
		Params: "109.5M", Size: "110MB", Languages: "English", MaxTokenChunks: 512,
		ModelFile: "model_int8.onnx", Notes: "INT8 from Teradata fork",
		ModelURL: "https://huggingface.co/Teradata/bge-base-en-v1.5/" +
			"resolve/main/onnx/model_int8.onnx",
		TokenizerURL: "https://huggingface.co/BAAI/bge-base-en-v1.5/" +
			"resolve/main/tokenizer.json",
	},
	{
		Name: "nomic-embed-text-v1.5", HuggingFace: "nomic-ai/nomic-embed-text-v1.5",
		Params: "137M", Size: "137MB", Languages: "English", MaxTokenChunks: 8192,
		ModelFile: "model_int8.onnx",
		ModelURL: "https://huggingface.co/nomic-ai/nomic-embed-text-v1.5/" +
			"resolve/main/onnx/model_int8.onnx",
		TokenizerURL: "https://huggingface.co/nomic-ai/nomic-embed-text-v1.5/" +
			"resolve/main/tokenizer.json",
	},
	{
		Name: "snowflake-arctic-embed-s", HuggingFace: "Snowflake/snowflake-arctic-embed-s",
		Params: "33M", Size: "34MB", Languages: "English", MaxTokenChunks: 512,
		ModelFile: "model_int8.onnx",
		ModelURL: "https://huggingface.co/Snowflake/snowflake-arctic-embed-s/" +
			"resolve/main/onnx/model_int8.onnx",
		TokenizerURL: "https://huggingface.co/Snowflake/snowflake-arctic-embed-s/" +
			"resolve/main/tokenizer.json",
	},
	{
		Name: "snowflake-arctic-embed-m", HuggingFace: "Snowflake/snowflake-arctic-embed-m",
		Params: "110M", Size: "110MB", Languages: "English", MaxTokenChunks: 512,
		ModelFile: "model_int8.onnx",
		ModelURL: "https://huggingface.co/Snowflake/snowflake-arctic-embed-m/" +
			"resolve/main/onnx/model_int8.onnx",
		TokenizerURL: "https://huggingface.co/Snowflake/snowflake-arctic-embed-m/" +
			"resolve/main/tokenizer.json",
	},
	{
		Name: "bge-micro-v2", HuggingFace: "TaylorAI/bge-micro-v2",
		Params: "17M", Size: "17MB", Languages: "English", MaxTokenChunks: 512,
		ModelFile: "model_quantized.onnx",
		ModelURL: "https://huggingface.co/TaylorAI/bge-micro-v2/" +
			"resolve/main/onnx/model_quantized.onnx",
		TokenizerURL: "https://huggingface.co/TaylorAI/bge-micro-v2/" +
			"resolve/main/tokenizer.json",
	},
}

// ModelDir returns the standard download directory for a model: ~/.gent/models/<name>/.
func ModelDir(name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".gent", "models", name), nil
}

// FindModel looks up a model by name. Returns nil if not found.
func FindModel(name string) *ModelInfo {
	for i := range ModelRegistry {
		if ModelRegistry[i].Name == name {
			return &ModelRegistry[i]
		}
	}
	return nil
}
