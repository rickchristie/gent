# Embedded Semantic Search Package for Gent

## Overview

Build a **standalone Go package** (`embedsearch` or similar) that provides CPU-based semantic search using a pre-packaged `multilingual-e5-small` embedding model via ONNX Runtime. The package is domain-agnostic — it does not know about Gent, tools, policies, or agents. Gent's `ToolSearchToolChain` and `PolicySearch` middleware consume it as a dependency.

**Core bet:** Shipping a ~120MB embedding model inside the library eliminates all external dependencies (no Python, no sidecar, no API keys) and gives users production-grade semantic search out of the box on a $15/month EC2 instance.

---

## Architecture

```
embedsearch/                    ← Standalone package, no gent imports
├── embedder.go                 ← Embedder interface + default ONNX implementation
├── embedder_onnx.go            ← ONNX Runtime embedding via hugot or yalue/onnxruntime_go
├── index.go                    ← SearchIndex: stores vectors, performs search
├── index_brute.go              ← Brute-force cosine similarity (default, good to ~100K docs)
├── config.go                   ← Configuration structs with sensible defaults
├── document.go                 ← Document type (ID, text, metadata, vector)
├── result.go                   ← SearchResult type (ID, score, document)
├── hybrid.go                   ← Optional: RRF fusion of multiple SearchIndex results
├── model/                      ← Pre-packaged model assets
│   └── e5small/
│       ├── model_quantized.onnx    ← INT8 quantized (~120MB)
│       ├── tokenizer.json          ← HuggingFace tokenizer config
│       ├── special_tokens_map.json
│       └── tokenizer_config.json
└── internal/
    ├── tokenizer/              ← Tokenizer wrapper (daulet/tokenizers or hugot)
    ├── pooling/                ← Mean pooling + L2 normalization
    └── quantize/               ← Optional: binary/int8 vector quantization

gent/                           ← Gent framework, imports embedsearch
├── toolchain_search.go         ← ToolSearchToolChain uses embedsearch
├── policy_search.go            ← PolicySearch middleware uses embedsearch
└── ...
```

### Key Principle: Separation of Concerns

The `embedsearch` package is a **general-purpose embedded semantic search library**. Someone building a completely unrelated Go project (e.g., a document search API, a recommendation engine) should be able to `go get` this package and use it directly. It has no knowledge of LLMs, agents, tool calling, or any Gent concepts.

Gent consumes `embedsearch` the same way any other Go project would — through its public interfaces.

---

## Interfaces

### Embedder Interface

```go
// Package embedsearch provides embedded semantic search for Go applications.
// It ships with a pre-packaged multilingual-e5-small model for zero-setup
// CPU-based semantic search.
package embedsearch

import "context"

// Embedder converts text into dense vector representations.
//
// Implementations must be safe for concurrent use.
type Embedder interface {
    // Embed produces a vector representation of the input text.
    // The returned slice length equals Dimensions().
    Embed(ctx context.Context, text string) ([]float32, error)

    // EmbedBatch produces vector representations for multiple texts.
    // Returns one vector per input text, in the same order.
    // Implementations should optimize for batch processing where possible.
    EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)

    // Dimensions returns the output vector dimensionality.
    // For multilingual-e5-small this is 384.
    Dimensions() int

    // Close releases model resources (ONNX session, tokenizer).
    // Must be called when the Embedder is no longer needed.
    Close() error
}
```

### SearchIndex Interface

```go
// SearchIndex stores documents and retrieves them by semantic similarity.
//
// Implementations must be safe for concurrent use. The index owns the
// lifecycle of stored vectors but not the Embedder used to produce them.
type SearchIndex interface {
    // Add indexes a document. The text is embedded using the configured
    // Embedder and stored alongside the provided ID and metadata.
    // If a document with the same ID already exists, it is replaced.
    Add(ctx context.Context, doc Document) error

    // AddBatch indexes multiple documents. More efficient than calling
    // Add in a loop because it batches embedding calls.
    AddBatch(ctx context.Context, docs []Document) error

    // Search returns the top-k most semantically similar documents to the query.
    // The query is embedded using the configured Embedder, then compared
    // against all stored document vectors.
    //
    // Options control the number of results, minimum score threshold,
    // and optional metadata filters.
    Search(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error)

    // Remove deletes a document by ID. Returns false if not found.
    Remove(id string) bool

    // Len returns the number of indexed documents.
    Len() int

    // Close releases index resources. Does NOT close the Embedder.
    Close() error
}
```

### Configuration

```go
// Config controls the behavior of the default ONNX-based Embedder.
type EmbedderConfig struct {
    // ModelPath is the directory containing model_quantized.onnx and tokenizer.json.
    // If empty, uses the pre-packaged multilingual-e5-small model.
    ModelPath string

    // NumThreads controls ONNX Runtime intra-op parallelism.
    // Default: 4. Set to number of physical cores for latency optimization.
    // For high-concurrency serving, set to 1 and use multiple goroutines.
    NumThreads int

    // MaxSequenceLength is the maximum token length per input.
    // Inputs longer than this are truncated. Default: 512.
    // multilingual-e5-small supports up to 512.
    MaxSequenceLength int

    // QueryPrefix is prepended to query text before embedding.
    // For e5 models, this MUST be "query: " (with trailing space).
    // For document/corpus text, the prefix is "passage: ".
    // Default: "query: " — this is set automatically; override only if
    // using a non-e5 model that requires different prefixes.
    QueryPrefix string

    // PassagePrefix is prepended to document text before embedding.
    // For e5 models, this MUST be "passage: " (with trailing space).
    // Default: "passage: "
    PassagePrefix string
}

// DefaultEmbedderConfig returns configuration for the pre-packaged
// multilingual-e5-small model with INT8 quantization.
func DefaultEmbedderConfig() EmbedderConfig {
    return EmbedderConfig{
        ModelPath:         "", // uses embedded model
        NumThreads:        4,
        MaxSequenceLength: 512,
        QueryPrefix:       "query: ",
        PassagePrefix:     "passage: ",
    }
}

// SearchIndexConfig controls the behavior of the search index.
type SearchIndexConfig struct {
    // Embedder is used to convert text to vectors. Required.
    Embedder Embedder

    // InitialCapacity pre-allocates storage for this many documents.
    // Default: 256. Set higher if you know the corpus size upfront.
    InitialCapacity int
}

// SearchOptions controls individual search requests.
type SearchOptions struct {
    // TopK is the maximum number of results to return. Default: 5.
    TopK int

    // MinScore filters results below this cosine similarity threshold.
    // Range: -1.0 to 1.0. Default: 0.0 (return all positive similarities).
    MinScore float32

    // MetadataFilter optionally filters documents by metadata before ranking.
    // Return true to include the document in results.
    MetadataFilter func(metadata map[string]string) bool
}
```

### Document and Result Types

```go
// Document represents a piece of text to be indexed for semantic search.
type Document struct {
    // ID uniquely identifies this document. Used for updates and deletion.
    ID string

    // Text is the content to be embedded and searched.
    Text string

    // Metadata is optional key-value pairs for filtering and retrieval.
    // Stored alongside the vector but not embedded.
    Metadata map[string]string
}

// SearchResult is a document matched by semantic search, with its score.
type SearchResult struct {
    // Document is the matched document.
    Document Document

    // Score is the cosine similarity between the query and this document.
    // Range: -1.0 to 1.0. Higher is more similar.
    Score float32
}
```

---

## Implementation Details

### Model: multilingual-e5-small

**Why this specific model:**
- **118M parameters, 384 dimensions** — small enough for CPU, large enough for quality.
- **Scores 74.80 nDCG@10 on MIRACL-id** (Indonesian retrieval) — only 1.36 points behind the 5x larger e5-large (76.16). The scaling curve is remarkably flat for Indonesian.
- **Competitive on English** — the multilingual training does not degrade English performance. It outperforms all-MiniLM-L6-v2 (42 nDCG@10 MTEB retrieval) on English retrieval benchmarks.
- **Supports 100+ languages** — users get Indonesian, English, and every other language the model covers without configuration.
- **INT8 ONNX quantized size: ~120MB** — reasonable library overhead for zero-setup semantic search.
- **Max sequence length: 512 tokens** — sufficient for tool descriptions, policy chunks, and most retrieval use cases.
- **Apache 2.0 license** — no restrictions on commercial use or redistribution.

**Source:** https://huggingface.co/intfloat/multilingual-e5-small

### ONNX Model Preparation (One-Time, Done Before Packaging)

The model must be exported to ONNX and INT8-quantized before embedding in the library. This is a one-time step done by the library maintainer (you), not by end users.

```bash
# 1. Install tools
pip install optimum[onnxruntime] onnxruntime

# 2. Export to ONNX
optimum-cli export onnx \
    --model intfloat/multilingual-e5-small \
    --task feature-extraction \
    ./e5-small-onnx/

# 3. Quantize to INT8
optimum-cli onnxruntime quantize \
    --onnx_model ./e5-small-onnx/ \
    --avx512_vnni \
    -o ./e5-small-onnx-int8/

# 4. Verify output files exist:
#    - model_quantized.onnx (~120MB)
#    - tokenizer.json
#    - special_tokens_map.json
#    - tokenizer_config.json
```

**Alternative quantization with Python if optimum-cli doesn't work:**

```python
from optimum.onnxruntime import ORTModelForFeatureExtraction, ORTQuantizer
from optimum.onnxruntime.configuration import AutoQuantizationConfig

model = ORTModelForFeatureExtraction.from_pretrained(
    "intfloat/multilingual-e5-small", export=True
)
model.save_pretrained("./e5-small-onnx/")

quantizer = ORTQuantizer.from_pretrained("./e5-small-onnx/")
qconfig = AutoQuantizationConfig.avx512_vnni(is_static=False)
quantizer.quantize(save_dir="./e5-small-onnx-int8/", quantization_config=qconfig)
```

### Embedding the Model Files in Go

**Option A: Go embed directive (simplest, increases binary size by ~120MB)**

```go
//go:embed model/e5small/model_quantized.onnx
var modelBytes []byte

//go:embed model/e5small/tokenizer.json
var tokenizerBytes []byte
```

**Concern:** This makes the compiled binary ~120MB larger. Every Go binary that imports `embedsearch` will include the model weights even if they never call the embedder.

**Option B: Lazy download on first use (recommended)**

Store a SHA256 hash in the library. On first call to `NewDefaultEmbedder()`, check if the model exists in a well-known cache directory (`~/.cache/embedsearch/multilingual-e5-small-int8/`). If not, download from a hosted location (GitHub Releases, S3, or HuggingFace Hub). This is what Ollama, llama.cpp, and most ML libraries do.

```go
func NewDefaultEmbedder(cfg EmbedderConfig) (Embedder, error) {
    if cfg.ModelPath == "" {
        modelDir, err := ensureModelCached(
            "multilingual-e5-small-int8",
            "sha256:abc123...",  // known hash
            "https://huggingface.co/intfloat/multilingual-e5-small/resolve/main/onnx/model_quantized.onnx",
        )
        if err != nil {
            return nil, fmt.Errorf("embedsearch: model setup failed: %w", err)
        }
        cfg.ModelPath = modelDir
    }
    return newONNXEmbedder(cfg)
}
```

**Option C: go:embed behind a build tag**

```go
//go:build embed_model

//go:embed model/e5small/*
var embeddedModel embed.FS
```

Users who want a self-contained binary build with `-tags embed_model`. Others get lazy download. This gives both zero-setup and small-binary options.

**Recommendation:** Start with Option A (go:embed) for simplicity since you've already committed to the ~120MB tradeoff. Add Option B later if binary size becomes a concern. The interface stays the same regardless.

### Go ONNX Runtime Integration

**Primary choice: `knights-analytics/hugot`** (569 stars)

Hugot provides the complete pipeline: tokenization + ONNX inference + pooling. It wraps ONNX Runtime and HuggingFace tokenizers in a single Go API.

- GitHub: https://github.com/knights-analytics/hugot
- Supports `FeatureExtractionPipeline` which outputs `[][]float32` embeddings
- Handles tokenization internally via Rust HuggingFace tokenizer bindings
- Supports CPU and CUDA execution providers
- Can download models from HuggingFace Hub directly
- **CGo dependency:** Yes (ONNX Runtime C API + Rust tokenizer FFI)

```go
import "github.com/knights-analytics/hugot"
import "github.com/knights-analytics/hugot/pipelines"

session, err := hugot.NewSession(
    hugot.WithOnnxLibraryPath("/usr/lib/libonnxruntime.so"),
)
defer session.Destroy()

pipeline, err := session.NewPipeline(
    session.NewFeatureExtractionConfig().
        WithModelPath("./model/e5small/").
        WithOnnxFilename("model_quantized.onnx"),
)

// Embed a batch of texts
result, err := pipeline.RunPipeline([]string{
    "query: cari kos murah di sudirman",
    "passage: Kos Putri Sudirman - affordable boarding house near Jalan Sudirman",
})
// result.Embeddings is [][]float32
```

**Alternative: `yalue/onnxruntime_go`** (422 stars) + `daulet/tokenizers`

Lower-level but more control. Requires implementing tokenization and mean pooling yourself.

- ONNX Runtime Go: https://github.com/yalue/onnxruntime_go
- Tokenizer Go: https://github.com/daulet/tokenizers

```go
import ort "github.com/yalue/onnxruntime_go"
import "github.com/daulet/tokenizers"

// Initialize tokenizer
tk, err := tokenizers.FromFile("./model/e5small/tokenizer.json")
defer tk.Close()

// Tokenize
encoded := tk.Encode("query: find booking tools", true)
inputIDs := toInt64Slice(encoded.IDs)       // []int64
attentionMask := toInt64Slice(encoded.AttentionMask) // []int64
tokenTypeIDs := makeZeros(len(inputIDs))    // []int64, all zeros for e5

// Create ONNX tensors and run inference
// ... (requires manual tensor creation, session.Run, mean pooling, L2 norm)
```

**Recommendation:** Use **hugot** as the default implementation. It's more batteries-included and actively maintained. Expose the `Embedder` interface so users CAN plug in `yalue/onnxruntime_go` or any other backend if they need lower-level control.

### Mean Pooling + L2 Normalization

If using `yalue/onnxruntime_go` directly (not hugot), you must implement mean pooling yourself. The ONNX model outputs token-level embeddings `[batch_size, seq_len, hidden_dim]`. Mean pooling averages across the sequence dimension, masked by the attention mask:

```go
// meanPool computes mean pooling over token embeddings.
// tokenEmbeddings shape: [seqLen][hiddenDim]
// attentionMask shape: [seqLen]
// Returns: [hiddenDim]
func meanPool(tokenEmbeddings [][]float32, attentionMask []int64) []float32 {
    hiddenDim := len(tokenEmbeddings[0])
    result := make([]float32, hiddenDim)
    var maskSum float32

    for i, mask := range attentionMask {
        if mask == 1 {
            maskSum++
            for j := 0; j < hiddenDim; j++ {
                result[j] += tokenEmbeddings[i][j]
            }
        }
    }

    for j := 0; j < hiddenDim; j++ {
        result[j] /= maskSum
    }

    return l2Normalize(result)
}

// l2Normalize normalizes a vector to unit length.
func l2Normalize(v []float32) []float32 {
    var norm float32
    for _, x := range v {
        norm += x * x
    }
    norm = float32(math.Sqrt(float64(norm)))
    for i := range v {
        v[i] /= norm
    }
    return v
}
```

### Brute-Force Cosine Similarity Search

The default `SearchIndex` implementation uses brute-force cosine similarity. This is optimal for your scale (< 100K documents).

```go
// cosineSimilarity computes the cosine similarity between two L2-normalized vectors.
// When vectors are already L2-normalized, cosine similarity equals the dot product.
func cosineSimilarity(a, b []float32) float32 {
    var dot float32
    for i := range a {
        dot += a[i] * b[i]
    }
    return dot
}
```

**Performance characteristics:**
- 1K documents: < 1ms search
- 10K documents: ~5ms search
- 100K documents: ~40ms search (384-dim float32)
- Thread-safe via `sync.RWMutex` (reads don't block each other)

### E5 Model Query/Passage Prefixes — CRITICAL

**The e5 family of models REQUIRES specific prefixes.** Without them, retrieval quality degrades significantly (10-20% nDCG@10 loss).

- **Queries** (what you're searching for) must be prefixed with `"query: "`
- **Documents/passages** (what you're searching through) must be prefixed with `"passage: "`

The `Embedder` implementation must handle this automatically. The `SearchIndex.Add()` method adds `"passage: "` prefix, and `SearchIndex.Search()` adds `"query: "` prefix. Users should never need to think about this.

```go
func (idx *bruteForceIndex) Add(ctx context.Context, doc Document) error {
    // Prefix is added internally — user provides raw text
    prefixedText := idx.embedder.PassagePrefix() + doc.Text
    vec, err := idx.embedder.Embed(ctx, prefixedText)
    // ...
}

func (idx *bruteForceIndex) Search(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error) {
    prefixedQuery := idx.embedder.QueryPrefix() + query
    queryVec, err := idx.embedder.Embed(ctx, prefixedQuery)
    // ...
}
```

**Design decision:** The prefix logic lives in the `SearchIndex`, not in the `Embedder`. This way the `Embedder` interface stays generic (it just embeds text), and the prefix convention is a search-level concern. If someone uses a non-e5 model that doesn't need prefixes, they set `QueryPrefix: ""` and `PassagePrefix: ""`.

**Alternative design:** Have the `Embedder` expose `EmbedQuery()` and `EmbedPassage()` methods. This is more explicit but couples the interface to retrieval-specific models. Hugot's `FeatureExtractionPipeline` doesn't distinguish query vs passage, so the prefix handling would need to wrap it regardless.

---

## Gent Integration Points

### ToolSearchToolChain

Gent's `ToolSearchToolChain` wraps `embedsearch.SearchIndex` and integrates with Bleve for hybrid BM25 + semantic search.

```
User registers tool → ToolSearchToolChain.RegisterTool(tool)
    ├── Adds tool description to Bleve index (BM25)
    └── Adds tool description to embedsearch.SearchIndex (semantic)

Agent needs a tool → ToolSearchToolChain.Search(query)
    ├── BM25 search via Bleve → ranked list A
    ├── Semantic search via embedsearch → ranked list B
    └── Reciprocal Rank Fusion (RRF) → merged results
```

The text indexed per tool should be a concatenation of:
```
{tool.Name}: {tool.Description}
Keywords: {tool.SearchableTool.Keywords}
Example queries: {tool.SearchableTool.SyntheticQueries}
```

This gives the embedding model maximum signal for semantic matching. The `SearchableTool` metadata struct (already designed) provides the synthetic queries and keywords.

### PolicySearch Middleware

PolicySearch runs as a **pre-iteration middleware** in the agent loop. Before each LLM call:

1. Extract the latest user message or agent reasoning as the query
2. Search the policy index via `embedsearch.SearchIndex`
3. Inject top-K relevant policy chunks into the context as a system-level section

```
Agent loop iteration begins
    → PolicySearch middleware runs
        → Embeds current context summary
        → Retrieves top-3 relevant policies
        → Injects as "Relevant Policies" section in iteration context
    → LLM generates response with policy guidance available
```

The middleware consumes `embedsearch.SearchIndex` — it does not know or care that it's powered by e5-small and ONNX Runtime.

---

## Gotchas, Guards, and Potential Issues

### 1. ONNX Runtime Shared Library Must Be Present

ONNX Runtime is a C library. The Go bindings (`hugot` or `yalue/onnxruntime_go`) require `libonnxruntime.so` (Linux), `onnxruntime.dll` (Windows), or `libonnxruntime.dylib` (macOS) at runtime.

**Guard:** Check for the shared library at `NewDefaultEmbedder()` time. Return a clear error message:

```go
if !onnxRuntimeAvailable() {
    return nil, fmt.Errorf(
        "embedsearch: ONNX Runtime library not found. "+
        "Install it: https://github.com/microsoft/onnxruntime/releases "+
        "or set ONNXRUNTIME_LIB_PATH environment variable",
    )
}
```

**Mitigation options:**
- Bundle the ONNX Runtime `.so` file with the model assets (~25MB for CPU-only)
- Provide a `Makefile` target or setup script that downloads it
- Document clearly in README
- Consider a build tag `//go:build !cgo` that falls back to a pure-Go (slower) embedding or returns a "CGo required" error

### 2. CGo Cross-Compilation Complexity

Both `hugot` and `yalue/onnxruntime_go` use CGo. This means:
- `CGO_ENABLED=1` is required at build time
- Cross-compilation (e.g., building Linux binary on macOS) requires a cross-compiler
- Docker-based builds are the simplest solution for CI/CD

**Guard:** The `embedsearch` package should compile without CGo if the user doesn't need the default ONNX embedder — they can provide their own `Embedder` implementation (e.g., calling an external API).

```go
// Provide a build-tag-guarded implementation:
// embedder_onnx.go      → //go:build cgo
// embedder_stub.go      → //go:build !cgo
//
// The stub returns an error: "ONNX embedder requires CGo. Build with CGO_ENABLED=1
// or provide a custom Embedder implementation."
```

### 3. ONNX Runtime Thread Contention

ONNX Runtime uses its own thread pool for intra-op parallelism. If you create multiple `Embedder` instances or run many goroutines calling `Embed()` concurrently, the threads will compete.

**Guard:**
- Default `NumThreads: 4` is conservative and works well on 2+ vCPU instances
- For high-concurrency (e.g., 50+ concurrent embedding requests), set `NumThreads: 1` and let Go's goroutine scheduler handle concurrency
- Use a **semaphore** (buffered channel) to limit concurrent ONNX inference calls:

```go
type onnxEmbedder struct {
    session  *ort.Session
    sem      chan struct{}  // limits concurrent inference
}

func (e *onnxEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
    select {
    case e.sem <- struct{}{}:
        defer func() { <-e.sem }()
    case <-ctx.Done():
        return nil, ctx.Err()
    }
    // ... run inference
}
```

### 4. Model Warm-Up Latency

The first inference call after loading is 5-10x slower than subsequent calls (ONNX Runtime JIT compilation, memory allocation). This can cause a latency spike on the first user request.

**Guard:** Run a warm-up embedding during initialization:

```go
func newONNXEmbedder(cfg EmbedderConfig) (Embedder, error) {
    // ... load model and tokenizer ...

    // Warm up: run a dummy inference to trigger JIT compilation
    _, err := embedder.Embed(context.Background(), "warmup")
    if err != nil {
        return nil, fmt.Errorf("embedsearch: warm-up failed: %w", err)
    }

    return embedder, nil
}
```

### 5. Token Truncation Happens Silently

If input text exceeds `MaxSequenceLength` (512 tokens for e5-small), tokens beyond the limit are **silently dropped**. For tool descriptions this is rarely an issue (most are < 100 tokens). For policy documents, this can silently lose critical information.

**Guard:**
- Log a warning when truncation occurs (at debug level, not per-request)
- Document that users should chunk policy documents to < 400 tokens for safety margin (512 minus prefix tokens)
- Provide a helper function for chunking:

```go
// ChunkText splits text into chunks suitable for embedding.
// Each chunk will be at most maxTokens tokens when tokenized.
// Chunks overlap by overlapTokens to avoid losing context at boundaries.
func ChunkText(text string, maxTokens, overlapTokens int) []string
```

### 6. Memory Growth with Large Indices

Each 384-dimensional float32 vector is 1,536 bytes. For large corpora:
- 10K documents: ~15 MB (vectors only)
- 100K documents: ~150 MB (vectors only)
- Plus the original text stored alongside each vector

**Guard:**
- Document the memory profile clearly
- For the brute-force index, store vectors in a contiguous `[]float32` slice (better cache locality) rather than `[][]float32` (pointer chasing)
- Consider adding an optional disk-backed persistence mode (e.g., gob serialization) so indices can be saved/loaded without re-embedding

### 7. Index Persistence Across Restarts

If the agent restarts, all embeddings are lost from an in-memory index. Re-embedding a large policy corpus on every startup wastes time.

**Guard:** Provide `Save(path string) error` and `Load(path string) error` methods on `SearchIndex`:

```go
// Save persists the index to disk. The format is gob-encoded and includes
// all document vectors, texts, and metadata.
func (idx *bruteForceIndex) Save(path string) error

// Load restores a previously saved index. The Embedder is NOT restored —
// it must be provided separately via the config.
func LoadIndex(path string, embedder Embedder) (SearchIndex, error)
```

### 8. Cosine Similarity Semantics for Negative Scores

Cosine similarity ranges from -1.0 to 1.0. Negative scores mean the query and document are semantically *opposite*. The default `MinScore: 0.0` filters these out, but users might be confused if they set `MinScore: -1.0` and see negative results.

**Guard:** Document that for retrieval use cases, meaningful results typically have scores > 0.3. Scores below 0.2 are rarely useful. Provide suggested thresholds in documentation.

### 9. Not All Embedding Models Use the Same Prefixes

The e5 family requires `"query: "` and `"passage: "` prefixes. Other models:
- BGE models: no prefix needed
- Nomic embed: uses `"search_query: "` and `"search_document: "` prefixes
- MiniLM: no prefix needed

**Guard:** Make prefixes configurable (already in `EmbedderConfig`). Document the correct prefixes for popular models. The default config has e5 prefixes since that's the default model.

### 10. NUMA Awareness on Multi-Socket Servers

On multi-socket EC2 instances (e.g., m5.4xlarge with 2 sockets), ONNX Runtime may allocate memory on one NUMA node and schedule threads on another, causing a 3x+ latency penalty.

**Guard:** For single-socket instances (t3, c6i.xlarge, m7i.xlarge) this is not an issue. For multi-socket instances, set the `GOMP_CPU_AFFINITY` or `OMP_PLACES` environment variable, or pin the Go process with `taskset`. Document this for advanced users.

---

## Limitations

### What This Package Does NOT Do

1. **No ANN indexing** — Brute-force only. Suitable for up to ~100K documents. For millions of documents, users need external solutions (FAISS via go-faiss, Weaviate, etc.). The `SearchIndex` interface allows swapping implementations.

2. **No GPU acceleration** — CPU-only by default. The ONNX Runtime Go bindings support CUDA execution providers, but the pre-packaged model is INT8-quantized for CPU. Users wanting GPU can provide their own model path with FP16 weights.

3. **No cross-encoder reranking** — The package provides bi-encoder (embedding) search only. Cross-encoder reranking (which is 35-40% more accurate but 100x slower) is out of scope. Users can implement reranking on top of the returned `SearchResult` list.

4. **No built-in chunking** — The package embeds and searches text as given. Chunking documents into appropriately sized pieces is the caller's responsibility. Provide a helper function but don't auto-chunk.

5. **No hybrid BM25 built-in** — BM25 is handled by Bleve at the Gent layer. The `embedsearch` package is pure semantic search. The `hybrid.go` file provides RRF fusion utilities but does not include a BM25 implementation.

6. **No streaming/incremental indexing** — Adding documents blocks until embedding is complete. For bulk indexing of large corpora, users should use `AddBatch()` and consider running it in a background goroutine.

### Default Configuration Rationale

| Setting | Default | Why |
|---------|---------|-----|
| Model | multilingual-e5-small INT8 | Best accuracy-per-byte for multilingual. 74.80 nDCG@10 on Indonesian, competitive English. ~120MB. |
| Dimensions | 384 | Fixed by model architecture. Good balance of quality and memory. |
| NumThreads | 4 | Works well on t3.small (2 vCPU) through c6i.xlarge (4 vCPU). Diminishing returns beyond 4 for small models. |
| MaxSequenceLength | 512 | Model maximum. Longer inputs are truncated. |
| TopK | 5 | For tool search (50-200 tools) and policy search (few hundred chunks), 5 results provide good coverage without noise. |
| MinScore | 0.0 | Filters out negative (opposite meaning) results. For stricter filtering, users set 0.3+. |
| QueryPrefix | "query: " | Required by e5 model family for retrieval quality. |
| PassagePrefix | "passage: " | Required by e5 model family for retrieval quality. |
| Search algorithm | Brute-force cosine | Optimal for < 100K docs. No tuning parameters. Always finds the true top-K. |

---

## AWS EC2 Instance Sizing

### Minimum: t3.small ($15/month)

- 2 vCPU (Intel Xeon Skylake/Cascade Lake), 2 GiB RAM
- Model (INT8): ~120MB runtime memory
- ONNX Runtime: ~50MB
- Go app + indices: ~200MB
- **Total: ~500MB, leaving 1.5GB headroom**
- Single-query latency: ~15-25ms (no VNNI on t3)
- Sufficient for: ToolSearch (200 tools) + PolicySearch (500 policy chunks)

### Recommended: t3.medium ($30/month) or c6i.large ($60/month)

- t3.medium: 2 vCPU, 4 GiB RAM — more headroom for larger policy corpora
- c6i.large: 2 vCPU (Ice Lake with AVX-512 VNNI), 4 GiB RAM — INT8 inference is 2-3x faster than t3

### For the AI Fantasy World project: c6i.xlarge ($120/month)

- 4 vCPU, 8 GiB RAM — room for LLM API overhead, larger indices, and the sandbox tier

---

## Key Library Links

| Library | Purpose | URL |
|---------|---------|-----|
| **hugot** | Go ONNX transformer pipelines (embedding, tokenization) | https://github.com/knights-analytics/hugot |
| **onnxruntime_go** | Low-level ONNX Runtime Go bindings | https://github.com/yalue/onnxruntime_go |
| **daulet/tokenizers** | Go bindings for HuggingFace Rust tokenizer | https://github.com/daulet/tokenizers |
| **chromem-go** | In-memory vector DB with brute-force search | https://github.com/philippgille/chromem-go |
| **multilingual-e5-small** | The embedding model (HuggingFace) | https://huggingface.co/intfloat/multilingual-e5-small |
| **ONNX Runtime releases** | Download libonnxruntime.so | https://github.com/microsoft/onnxruntime/releases |
| **HuggingFace Optimum** | ONNX export + INT8 quantization | https://huggingface.co/docs/optimum/en/onnxruntime/usage_guides/quantization |
| **Bleve** | BM25 full-text search (used by Gent, not this package) | https://github.com/blevesearch/bleve |
| **LazarusNLP Indonesian benchmarks** | MIRACL-id, TyDiQA-id numbers | https://github.com/LazarusNLP/indonesian-sentence-embeddings |

---

## Implementation Order

### Phase 1: Core Package (embedsearch)

1. Define interfaces (`Embedder`, `SearchIndex`) and types (`Document`, `SearchResult`, `SearchOptions`)
2. Implement `onnxEmbedder` using hugot with the pre-packaged model
3. Implement `bruteForceIndex` with cosine similarity search
4. Add `Save()`/`Load()` persistence via gob encoding
5. Add warm-up, concurrency semaphore, truncation warning
6. Write tests with a small test corpus (10-20 documents, English + Indonesian)

### Phase 2: Gent Integration

7. Wire `ToolSearchToolChain` to use `embedsearch.SearchIndex` alongside Bleve
8. Implement RRF fusion between BM25 and semantic results
9. Build `PolicySearch` middleware that auto-injects relevant policies per iteration
10. Add `PolicySearch` as an explicit tool (fallback for when auto-injection misses)

### Phase 3: Polish

11. Add `ChunkText()` helper for policy document chunking
12. Add build tags for CGo/non-CGo builds
13. Benchmark on t3.small and document latency/memory profiles
14. Add `go:embed` or lazy download for model distribution

---

## Testing Strategy

### Unit Tests

```go
func TestEmbedder_DimensionsMatch(t *testing.T) {
    embedder, _ := NewDefaultEmbedder(DefaultEmbedderConfig())
    defer embedder.Close()

    vec, err := embedder.Embed(ctx, "hello world")
    require.NoError(t, err)
    assert.Len(t, vec, 384)
}

func TestEmbedder_SimilarTextsCloseVectors(t *testing.T) {
    embedder, _ := NewDefaultEmbedder(DefaultEmbedderConfig())
    defer embedder.Close()

    v1, _ := embedder.Embed(ctx, "query: find cheap boarding house")
    v2, _ := embedder.Embed(ctx, "query: cari kos murah")       // Indonesian equivalent
    v3, _ := embedder.Embed(ctx, "query: quantum physics papers") // unrelated

    sim12 := cosineSimilarity(v1, v2)
    sim13 := cosineSimilarity(v1, v3)

    assert.Greater(t, sim12, sim13, "semantically similar texts should have higher similarity")
    assert.Greater(t, sim12, float32(0.5), "cross-lingual similarity should be meaningful")
}

func TestSearchIndex_RetrievesRelevantDocs(t *testing.T) {
    idx := NewBruteForceIndex(SearchIndexConfig{Embedder: embedder})

    idx.Add(ctx, Document{ID: "1", Text: "cancel reservation policy"})
    idx.Add(ctx, Document{ID: "2", Text: "early checkout procedure and refund"})
    idx.Add(ctx, Document{ID: "3", Text: "wifi password reset instructions"})

    results, _ := idx.Search(ctx, "how to cancel a booking", SearchOptions{TopK: 2})

    assert.Len(t, results, 2)
    // Results 1 and 2 should rank above 3
    resultIDs := []string{results[0].Document.ID, results[1].Document.ID}
    assert.Contains(t, resultIDs, "1")
    assert.Contains(t, resultIDs, "2")
}
```

### Integration Tests

- Test with real ONNX model loaded (skip if model not present: `t.Skip("model not available")`)
- Test persistence: Save → Load → Search produces same results
- Test concurrent access: 100 goroutines searching simultaneously
- Benchmark: time per embedding, time per search at 1K/10K/100K documents