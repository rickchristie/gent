# Search Package

Generic search infrastructure for semantic vector search with ONNX, BM25 full-text search with
Bleve, and hybrid fused search.

## Setup

### 1. Install the CLI

```bash
go install github.com/rickchristie/gent/cmd/gent@v0.1.0
printf '\nexport PATH="%s:$PATH"\n' "$(go env GOPATH)/bin" >> ~/.bashrc
source ~/.bashrc
```

`go install` writes `gent` to `$(go env GOPATH)/bin`. Add that directory to your shell profile
once so future terminals can run `gent`. Use `~/.zshrc` instead of `~/.bashrc` if you use zsh.

### 2. Install native CPU libraries

```bash
gent setup onnx
source ~/.bashrc  # or ~/.zshrc, then restart terminal
```

`gent setup onnx` installs `libtokenizers` and ONNX Runtime to `~/.gent/lib/`. When you accept
the profile update prompt, it appends these ONNX library paths to `~/.bashrc`:

```bash
export CGO_LDFLAGS="-L$HOME/.gent/lib"
export LD_LIBRARY_PATH="$HOME/.gent/lib:$LD_LIBRARY_PATH"
```

If you decline the prompt, add those lines manually, then run `source ~/.bashrc` or restart your
terminal. Use `~/.zshrc` instead of `~/.bashrc` if you use zsh.

### 3. Download an embedding model

```bash
gent model list
gent model download multilingual-e5-small
```

`gent model list` shows 10 downloadable physical model files and 11 runtime configurations. One
downloaded model may have multiple configurations, for example full-size and Matryoshka-truncated
variants.

## Usage

```go
package main

import (
    "fmt"
    "path/filepath"

    "github.com/rickchristie/gent/common"
    "github.com/rickchristie/gent/search"
)

func newEmbedder() (search.Embedder, error) {
    cfg := common.FindConfig("multilingual-e5-small")
    if cfg == nil {
        return nil, fmt.Errorf("unknown embedding config: %s", "multilingual-e5-small")
    }

    dir, err := common.ModelDir(cfg.Model.Name)
    if err != nil {
        return nil, err
    }

    return search.NewOnnxEmbedder(*cfg, search.OnnxOptions{
        ModelPath:      filepath.Join(dir, cfg.Model.ModelFile),
        TokenizerPath:  filepath.Join(dir, "tokenizer.json"),
        NumThreads:     4,
        MaxConcurrency: 4,
    })
}
```

The `gent model download <model-name>` command prints the exact `common.FindConfig` and
`search.OnnxOptions` snippet for the downloaded model.

### Post-processing Hook

Some models need custom post-processing between pooling and L2 normalization. This is configured
on `common.ModelConfig`, not `search.OnnxOptions`. Built-in configs already set the right
post-processing for registered models such as `nomic-embed-text-v1.5`.

```go
base := common.FindConfig("nomic-embed-text-v1.5-768d")
if base == nil {
    return nil, fmt.Errorf("unknown embedding config")
}

cfg := *base
cfg.PostProcess = func(v []float32) []float32 {
    v = common.LayerNorm(v)
    return v[:256]
}
```

## Running Tests

### Unit Tests

Unit tests do not require downloaded models or ONNX Runtime.

```bash
go test ./search \
  -run 'Test(TheoreticalMax|NormalizeBM25|WeightedLinear|FlatIndex|BleveIndex|FusedIndex)' \
  -count=1
```

### Integration Tests

Integration tests use real ONNX models. Test setup downloads all 10 registered physical models to
`~/.gent/models/` on first run. Tests skip gracefully if ONNX Runtime is not installed.

```bash
go test ./search -run TestOnnxEmbedder_AllModels -count=1 -timeout 300s
```

### Quantized inference on CPUs without VNNI

The registry includes full-range INT8 models, including `model_qint8_avx512_vnni.onnx`.
ONNX Runtime's default U8S8 matrix multiplication can saturate on AVX2/AVX512 CPUs without
VNNI, producing incorrect embeddings even when model downloads and library setup are correct.
The embedder enables `session.x64quantprecision=1` so ONNX Runtime uses its U8U8 conversion
on affected CPUs. This preserves the quantized weights and uses a slower, accurate kernel;
ONNX Runtime leaves unaffected CPUs alone. See the [ONNX Runtime quantization guidance][quantization].

[quantization]: https://onnxruntime.ai/docs/performance/model-optimizations/quantization.html

`TestOnnxEmbedder_QuantizedMatMul` uses a 460-byte checked-in model with a known integer result
to catch this overflow independently of downloaded model weights. Regenerate it with
`go generate ./search` from the repository root. The Go generator uses the existing protobuf
dependency, so no additional tooling is required. The test skips if ONNX Runtime is unavailable.

Real-model search quality checks should specify relevant policies, rather than unrelated
lower-ranked matches whose close scores can change with inference kernels. The cancellation
and loyalty policy tests request exactly their relevant results and still compare the full
response. Deterministic policy unit tests also verify complete responses with multiple results.

### ONNX Runtime Resolution Order

1. `search.OnnxOptions.OnnxLibraryPath`.
2. `GENT_ORT_LIB` environment variable for CI or containers.
3. `~/.gent/lib/`, the default location installed by `gent setup onnx`.
4. ONNX Runtime default library resolution.

## Container / CI

The setup command is interactive. For CI and containers, prefer installing native libraries into a
known location and setting `GENT_ORT_LIB` explicitly.

```dockerfile
COPY libtokenizers.a libonnxruntime.so.1.24.4 /usr/local/lib/
RUN ldconfig
ENV GENT_ORT_LIB=/usr/local/lib/libonnxruntime.so.1.24.4
```
