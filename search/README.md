# Search Package

Generic search infrastructure: semantic vector search (ONNX), BM25 full-text search (Bleve), and hybrid fused search.

## Setup

<!-- TODO: Replace "go run ./cmd/gent" with "gent" once published as a standalone tool -->

### 1. Install native libraries

```bash
go run ./cmd/gent setup onnx
source ~/.bashrc  # or ~/.zshrc, then restart terminal
```

### 2. Download an embedding model

```bash
go run ./cmd/gent model list
go run ./cmd/gent model download multilingual-e5-small
```

After setup, build and test with no flags:

```bash
go build ./...
go test ./...
```

## Usage

```go
embedder, err := search.NewOnnxEmbedder(search.EmbedderConfig{
    ModelPath:     "~/.gent/models/multilingual-e5-small/model_qint8_avx512_vnni.onnx",
    TokenizerPath: "~/.gent/models/multilingual-e5-small/tokenizer.json",
    Dimensions:    384,
    Pooling:       search.PoolingMean,
    QueryPrefix:   "query: ",
    PassagePrefix: "passage: ",
})
defer embedder.Close()
```

The `gent model download` command prints the exact config to copy-paste.

### Post-processing hook

Some models need custom post-processing between pooling and L2 normalization. Use the `PostProcess` field:

```go
// nomic-embed-text-v1.5 needs layer normalization
cfg := search.EmbedderConfig{
    // ...
    PostProcess: search.LayerNorm,
}

// Custom: layer norm + Matryoshka truncation to 256 dims
cfg := search.EmbedderConfig{
    // ...
    PostProcess: func(v []float32) []float32 {
        v = search.LayerNorm(v)
        return v[:256]
    },
}
```

## Running Tests

### Unit tests (no model or ONNX Runtime needed)

```bash
go test ./search/ -run 'TestMinMax|TestWeightedLinear|TestFlatIndex|TestBleveIndex|TestFusedIndex'
```

### Integration tests (real ONNX models)

All 11 registered models are auto-downloaded to `.model/` on first test run. Tests skip gracefully if ONNX Runtime is not installed.

```bash
go test ./search/ -run TestOnnxEmbedder_AllModels -v -timeout 300s
```

### ONNX Runtime resolution order

1. `EmbedderConfig.OnnxLibraryPath` (explicit)
2. `GENT_ORT_LIB` environment variable (CI/containers)
3. `~/.gent/lib/` (default, installed by setup tool)

## Container / CI

```dockerfile
RUN go run ./cmd/gent setup onnx && source ~/.bashrc
```

Or copy pre-built libraries directly:

```dockerfile
COPY libtokenizers.a libonnxruntime.so.1.24.4 /usr/local/lib/
RUN ldconfig
ENV GENT_ORT_LIB=/usr/local/lib/libonnxruntime.so.1.24.4
```
