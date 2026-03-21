package common

import (
	"fmt"
	"os"
	"path/filepath"
)

// ModelInfo describes an embedding model available for download. This is download/display info
// only — usage configuration is in ModelConfig.
type ModelInfo struct {
	Name         string // CLI identifier (e.g., "multilingual-e5-small")
	HuggingFace  string // HuggingFace model ID
	Params       string // parameter count (e.g., "118M")
	Size         string // approximate INT8 ONNX file size
	Languages    string // language support
	ModelFile    string // filename of the ONNX model
	ModelURL     string // download URL for ONNX model file
	TokenizerURL string // download URL for tokenizer.json
	Notes        string // additional notes shown in model list
}

// ModelConfig describes how to use a downloaded model with the ONNX embedder. One model can
// have multiple configs (e.g., nomic at full 768d and truncated 384d Matryoshka).
type ModelConfig struct {
	ModelName       string          // references ModelInfo.Name
	ConfigName      string          // unique config identifier
	Description     string          // what this config is for
	Dimensions      int             // final output vector dimensionality (after PostProcess)
	ModelDimensions int             // ONNX model's native hidden dim (0 = same as Dimensions)
	Pooling         PoolingStrategy // mean or CLS
	QueryPrefix     string          // prefix for queries
	PassagePrefix   string          // prefix for documents
	InputNames      []string        // ONNX input tensor names
	OutputName      string          // ONNX output tensor name
	MaxSeqLen       int             // maximum sequence length
	PostProcess     func([]float32) []float32 // applied after pooling, before L2 norm

	// Display/helper fields
	ModelOverheadMB int    // approximate ONNX Runtime memory in MB (measured via RSS)
	BestFor         string // short description of ideal use case
}

// EstimateMemoryMB returns estimated total memory in MB for a given corpus size. textBytes
// is the total size of all source text before chunking. Assumes no truncation (all text is
// chunked to fit MaxSeqLen).
func (c ModelConfig) EstimateMemoryMB(textBytes int) int {
	scalingFactor := 1.0 + float64(c.Dimensions)/float64(c.MaxSeqLen)
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
		Params: "118M", Size: "113MB", Languages: "100+ languages",
		ModelFile: "model_qint8_avx512_vnni.onnx",
		ModelURL: "https://huggingface.co/intfloat/multilingual-e5-small/" +
			"resolve/main/onnx/model_qint8_avx512_vnni.onnx",
		TokenizerURL: "https://huggingface.co/intfloat/multilingual-e5-small/" +
			"resolve/main/onnx/tokenizer.json",
	},
	{
		Name: "multilingual-e5-base", HuggingFace: "intfloat/multilingual-e5-base",
		Params: "278M", Size: "266MB", Languages: "100+ languages",
		ModelFile: "model_qint8_avx512_vnni.onnx",
		ModelURL: "https://huggingface.co/intfloat/multilingual-e5-base/" +
			"resolve/main/onnx/model_qint8_avx512_vnni.onnx",
		TokenizerURL: "https://huggingface.co/intfloat/multilingual-e5-base/" +
			"resolve/main/onnx/tokenizer.json",
	},
	{
		Name: "e5-small-v2", HuggingFace: "intfloat/e5-small-v2",
		Params: "33M", Size: "34MB", Languages: "English",
		ModelFile: "model_qint8_avx512_vnni.onnx",
		ModelURL: "https://huggingface.co/intfloat/e5-small-v2/" +
			"resolve/main/onnx/model_qint8_avx512_vnni.onnx",
		TokenizerURL: "https://huggingface.co/intfloat/e5-small-v2/" +
			"resolve/main/tokenizer.json",
	},
	{
		Name: "all-MiniLM-L6-v2", HuggingFace: "sentence-transformers/all-MiniLM-L6-v2",
		Params: "22.7M", Size: "23MB", Languages: "English",
		ModelFile: "model_qint8_avx512_vnni.onnx",
		ModelURL: "https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/" +
			"resolve/main/onnx/model_qint8_avx512_vnni.onnx",
		TokenizerURL: "https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/" +
			"resolve/main/tokenizer.json",
	},
	{
		Name: "bge-small-en-v1.5", HuggingFace: "BAAI/bge-small-en-v1.5",
		Params: "33.4M", Size: "34MB", Languages: "English",
		ModelFile: "model_int8.onnx", Notes: "INT8 from Teradata fork",
		ModelURL: "https://huggingface.co/Teradata/bge-small-en-v1.5/" +
			"resolve/main/onnx/model_int8.onnx",
		TokenizerURL: "https://huggingface.co/BAAI/bge-small-en-v1.5/" +
			"resolve/main/tokenizer.json",
	},
	{
		Name: "bge-base-en-v1.5", HuggingFace: "BAAI/bge-base-en-v1.5",
		Params: "109.5M", Size: "110MB", Languages: "English",
		ModelFile: "model_int8.onnx", Notes: "INT8 from Teradata fork",
		ModelURL: "https://huggingface.co/Teradata/bge-base-en-v1.5/" +
			"resolve/main/onnx/model_int8.onnx",
		TokenizerURL: "https://huggingface.co/BAAI/bge-base-en-v1.5/" +
			"resolve/main/tokenizer.json",
	},
	{
		Name: "nomic-embed-text-v1.5", HuggingFace: "nomic-ai/nomic-embed-text-v1.5",
		Params: "137M", Size: "137MB", Languages: "English",
		ModelFile: "model_int8.onnx",
		ModelURL: "https://huggingface.co/nomic-ai/nomic-embed-text-v1.5/" +
			"resolve/main/onnx/model_int8.onnx",
		TokenizerURL: "https://huggingface.co/nomic-ai/nomic-embed-text-v1.5/" +
			"resolve/main/tokenizer.json",
	},
	{
		Name: "snowflake-arctic-embed-s", HuggingFace: "Snowflake/snowflake-arctic-embed-s",
		Params: "33M", Size: "34MB", Languages: "English",
		ModelFile: "model_int8.onnx",
		ModelURL: "https://huggingface.co/Snowflake/snowflake-arctic-embed-s/" +
			"resolve/main/onnx/model_int8.onnx",
		TokenizerURL: "https://huggingface.co/Snowflake/snowflake-arctic-embed-s/" +
			"resolve/main/tokenizer.json",
	},
	{
		Name: "snowflake-arctic-embed-m", HuggingFace: "Snowflake/snowflake-arctic-embed-m",
		Params: "110M", Size: "110MB", Languages: "English",
		ModelFile: "model_int8.onnx",
		ModelURL: "https://huggingface.co/Snowflake/snowflake-arctic-embed-m/" +
			"resolve/main/onnx/model_int8.onnx",
		TokenizerURL: "https://huggingface.co/Snowflake/snowflake-arctic-embed-m/" +
			"resolve/main/tokenizer.json",
	},
	{
		Name: "bge-micro-v2", HuggingFace: "TaylorAI/bge-micro-v2",
		Params: "17M", Size: "17MB", Languages: "English",
		ModelFile: "model_quantized.onnx",
		ModelURL: "https://huggingface.co/TaylorAI/bge-micro-v2/" +
			"resolve/main/onnx/model_quantized.onnx",
		TokenizerURL: "https://huggingface.co/TaylorAI/bge-micro-v2/" +
			"resolve/main/tokenizer.json",
	},
}

// ModelDir returns the standard download directory for a model: ~/.gent/models/<name>/.
// Both the CLI and test infrastructure use this to ensure models are stored in one place.
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
