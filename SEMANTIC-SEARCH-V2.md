# Embedded Semantic Search Package for Gent

## Overview

Build a **leaf Go package** (`search` or similar) that provides generic search infrastructure: a `SearchIndex[Doc]` interface, a `FlatIndex` (brute-force vector search via ONNX embeddings), a `BleveIndex` (BM25 full-text search), and a `FusedIndex` (combines multiple `SearchIndex` implementations via a `Fuser`). The package ships a pre-packaged `multilingual-e5-small` embedding model for zero-setup CPU-based semantic search.

**The package is a leaf dependency.** It does not import gent, does not know about tools, policies, or agents. Gent's `ToolSearchToolChain` and `PolicySearch` consume it through the generic interfaces. Someone building a completely unrelated Go project should be able to `go get` this package and use it directly.

**Core bet:** Shipping a ~120MB embedding model inside the library eliminates all external dependencies (no Python, no sidecar, no API keys) and gives users production-grade semantic search out of the box on a $15/month EC2 instance.

---

## Architecture

```
search/                             ← Leaf package, no gent imports
├── search.go                       ← SearchIndex[Doc] interface, SearchResult
├── embedder.go                     ← Embedder interface
├── embedder_onnx.go                ← ONNX Runtime Embedder (build tag: cgo)
├── embedder_stub.go                ← Stub Embedder (build tag: !cgo, returns error)
├── flat_index.go                   ← FlatIndex[Doc]: brute-force vector search
├── flat_adapter.go                 ← ChunkAdapter[Doc] interface
├── bleve_index.go                  ← BleveIndex[Doc]: BM25 full-text search
├── bleve_adapter.go                ← BleveAdapter[Doc] interface
├── bleve_helpers.go                ← Pre-built index mappings + query builders
├── fused_index.go                  ← FusedIndex[Doc]: combines N SearchIndex instances
├── fuser.go                        ← Fuser interface + WeightedLinearFuser
├── normalize.go                    ← Per-query min-max normalization
├── model/                          ← Pre-packaged model assets
│   └── e5small/
│       ├── model_quantized.onnx    ← INT8 quantized (~120MB)
│       ├── tokenizer.json          ← HuggingFace tokenizer config
│       ├── special_tokens_map.json
│       └── tokenizer_config.json
└── internal/
    ├── tokenizer/                  ← Tokenizer wrapper (daulet/tokenizers or hugot)
    └── pooling/                    ← Mean pooling + L2 normalization

gent/                               ← Gent framework, imports search
├── toolchain_search.go             ← ToolSearchToolChain uses search.FusedIndex
├── policy_search.go                ← PolicySearch middleware uses search.FusedIndex
└── ...
```

---

## Interfaces and Types

### SearchResult and SearchIndex

```go
package search

import "context"

// SearchResult is the output of any SearchIndex.Search() call.
type SearchResult struct {
    // Id uniquely identifies the matched document.
    Id string

    // Score is the relevance score. Semantics depend on the index type:
    //   - FlatIndex: cosine similarity in [-1.0, 1.0] (practically [0.0, 1.0])
    //   - BleveIndex: BM25 score (unbounded, query-dependent)
    //   - FusedIndex: fused score, depends on Fuser implementation
    Score float64

    // Snippet is the matching text that can be shown to the LLM for context.
    // For FlatIndex: the best-matching chunk text.
    // For BleveIndex: Bleve's built-in highlight/fragment.
    // Useful when the full document is large and showing the full text as
    // a search result would waste context tokens.
    Snippet string
}

// SearchIndex stores documents of type Doc and retrieves them by relevance.
//
// Implementations must be safe for concurrent use.
//
// The generic type parameter Doc allows different consumers to index
// different document types (e.g., IndexableTool, Policy) through the same
// interface, with type-specific adapters handling conversion to each
// backend's native format.
type SearchIndex[Doc any] interface {
    // Search returns the top-K most relevant documents for the query.
    Search(ctx context.Context, query string, topK int) ([]SearchResult, error)

    // Add indexes a single document. If a document with the same ID
    // already exists, it is replaced.
    Add(ctx context.Context, id string, doc Doc) error

    // Remove deletes a document by ID.
    Remove(id string) error

    // Swap atomically replaces the entire index contents.
    // All existing documents are removed and replaced with the provided set.
    // This is the preferred method for bulk updates (e.g., when the tool
    // registry changes) because it avoids inconsistent intermediate states.
    Swap(ctx context.Context, docs map[string]Doc) error
}
```

### Embedder

```go
// Embedder converts text into dense vector representations.
//
// The EmbedQuery and EmbedDocument distinction exists because some models
// (notably the e5 family) require different prefixes for queries vs documents.
// For e5-small, EmbedQuery prepends "query: " and EmbedDocument prepends
// "passage: ". Models that don't need prefixes implement both identically.
//
// Implementations must be safe for concurrent use.
type Embedder interface {
    // EmbedQuery produces a vector for a search query.
    // For e5 models, prepends "query: " to the text.
    EmbedQuery(ctx context.Context, text string) ([]float64, error)

    // EmbedDocument produces a vector for a document/passage being indexed.
    // For e5 models, prepends "passage: " to the text.
    EmbedDocument(ctx context.Context, text string) ([]float64, error)

    // EmbedDocumentBatch produces vectors for multiple documents.
    // More efficient than calling EmbedDocument in a loop because it
    // batches tokenization and inference.
    EmbedDocumentBatch(ctx context.Context, texts []string) ([][]float64, error)

    // Dimensions returns the output vector dimensionality.
    // For multilingual-e5-small this is 384.
    Dimensions() int

    // Close releases model resources (ONNX session, tokenizer).
    Close() error
}
```

### ChunkAdapter (for FlatIndex)

```go
// ChunkAdapter converts a domain document into one or more text chunks
// for embedding.
//
// Chunking is the caller's responsibility because only the caller knows
// the semantic boundaries of their data. A tool description is a single
// chunk. A 10-page policy document might be split into 20 chunks at
// section boundaries.
//
// Each returned chunk is embedded independently as a separate vector.
// On search, all chunks for a document are scored, but only the
// best-matching chunk's score (and text) is returned per document ID.
//
// For documents that don't need chunking, return a single-element slice.
//
// # Example for tools (no chunking needed)
//
//   type ToolChunkAdapter struct{}
//
//   func (a *ToolChunkAdapter) Convert(tool IndexableTool) ([]string, error) {
//       text := fmt.Sprintf("%s: %s\nKeywords: %s",
//           tool.Name(), tool.Description(),
//           strings.Join(tool.Keywords(), ", "))
//       return []string{text}, nil
//   }
//
// # Example for policies (chunking at section boundaries)
//
//   type PolicyChunkAdapter struct{}
//
//   func (a *PolicyChunkAdapter) Convert(policy Policy) ([]string, error) {
//       return policy.Sections, nil  // pre-chunked by the caller
//   }
type ChunkAdapter[Doc any] interface {
    Convert(doc Doc) ([]string, error)
}
```

### BleveAdapter (for BleveIndex)

```go
// BleveAdapter bridges a domain document type to Bleve's indexing
// and query system.
//
// The adapter handles three concerns:
//   - Mapping: defines Bleve's index schema (which fields exist, how
//     they're analyzed, keyword vs text, etc.)
//   - Convert: transforms a domain document into the shape Bleve expects
//     (typically a struct or map matching the field names in the mapping)
//   - Query: builds a Bleve search request from a raw query string,
//     including field-specific boosting, fuzzy matching, etc.
//
// # Example for tools
//
//   type ToolBleveAdapter struct{}
//
//   func (a *ToolBleveAdapter) Mapping() mapping.IndexMapping { ... }
//
//   func (a *ToolBleveAdapter) Convert(tool IndexableTool) (any, error) {
//       return map[string]string{
//           "name":        tool.Name(),
//           "description": tool.Description(),
//           "keywords":    strings.Join(tool.Keywords(), " "),
//       }, nil
//   }
//
//   func (a *ToolBleveAdapter) Query(q string) (*bleve.SearchRequest, error) {
//       // Field-specific boosted disjunction query
//       ...
//   }
type BleveAdapter[Doc any] interface {
    // Mapping returns the Bleve index mapping defining the schema.
    // Called once during BleveIndex initialization.
    Mapping() mapping.IndexMapping

    // Convert transforms a domain document into a Bleve-indexable value.
    // The returned value's fields must match the names in the Mapping.
    Convert(doc Doc) (any, error)

    // Query builds a Bleve search request from raw query text.
    // This is where field-specific boosting, fuzzy matching, and
    // query structure are defined.
    Query(query string) (*bleve.SearchRequest, error)
}
```

### Fuser

```go
// Fuser combines ranked results from multiple named search sources
// into a single ranked list.
//
// The input map is keyed by source name (matching the names used in
// FusedIndex's index map). Each value is that source's search results.
//
// Implementations must handle:
//   - Documents appearing in only one source (other sources scored 0)
//   - Empty result sets from one or more sources
//   - Different score scales across sources (e.g., BM25 vs cosine similarity)
type Fuser interface {
    Fuse(results map[string][]SearchResult, topK int) ([]SearchResult, error)
}
```

### WeightedLinearFuser

```go
// WeightedLinearFuser combines results using weighted linear score combination.
//
// For each document, the fused score is:
//
//   score = Σ weights[source] * normalize(source_score)
//
// Sources listed in NormalizeSources have per-query min-max normalization
// applied (required for BM25's unbounded scores). Sources not listed
// pass their scores through unchanged (correct for cosine similarity
// which is already in [0, 1]).
//
// # Why weighted linear over RRF (Reciprocal Rank Fusion)
//
// RRF uses rank position, not score magnitude. A document that's rank #1
// in semantic search (score 0.95) but absent from BM25 gets penalized
// because RRF treats "missing from one list" the same as "ranked last."
// Weighted linear correctly handles the "found by one method only" case:
// 0.7 * 0.95 = 0.665, which can still beat a mediocre dual-match.
//
// This matters for tool search where the most relevant tool often has
// zero keyword overlap with the agent's natural language query.
//
// # Example: 30% BM25 + 70% semantic
//
//   fuser := &WeightedLinearFuser{
//       Weights:          map[string]float64{"bm25": 0.3, "semantic": 0.7},
//       NormalizeSources: map[string]bool{"bm25": true, "semantic": false},
//   }
type WeightedLinearFuser struct {
    // Weights per source name. Should sum to 1.0 for interpretable output.
    Weights map[string]float64

    // NormalizeSources controls per-source min-max normalization.
    // Set true for sources with unbounded scores (BM25).
    // Set false for sources with bounded scores (cosine similarity).
    NormalizeSources map[string]bool
}
```

---

## FlatIndex Implementation

### Core Data Structure

```go
// storedVector is a single embedded chunk with its parent document ID.
type storedVector struct {
    DocID  string     // which document this chunk belongs to
    Chunk  string     // original chunk text (for Snippet in results)
    Vector []float64  // the embedding
}

// FlatIndex provides brute-force vector search using cosine similarity.
//
// Documents are converted to text chunks via a ChunkAdapter, embedded
// via an Embedder, and stored in a flat array. Search scans all vectors
// and returns deduplicated results (one per document ID, using the
// best-matching chunk).
//
// Performance characteristics (384-dim, single CPU core):
//   - 1K vectors: < 1ms search
//   - 10K vectors: ~5ms search
//   - 100K vectors: ~40ms search
//
// The brute-force ceiling is ~100K vectors (~40ms on t3.small).
// Beyond that, switch to an HNSW-based implementation.
//
// Safe for concurrent use via sync.RWMutex.
type FlatIndex[Doc any] struct {
    adapter  ChunkAdapter[Doc]
    embedder Embedder
    vectors  []storedVector
    mu       sync.RWMutex
}
```

### Why Chunk Text Must Be Stored

Embedding is a **lossy one-way compression.** A 384-dimensional vector is 3,072 bytes representing potentially thousands of bytes of text. There is no inverse function — you cannot reconstruct text from a vector. Therefore FlatIndex stores the original chunk text alongside each vector to populate the `Snippet` field in search results.

### Add with Chunking

```go
func (f *FlatIndex[Doc]) Add(ctx context.Context, id string, doc Doc) error {
    chunks, err := f.adapter.Convert(doc)
    if err != nil {
        return fmt.Errorf("search: chunk adapter failed: %w", err)
    }

    embeddings, err := f.embedder.EmbedDocumentBatch(ctx, chunks)
    if err != nil {
        return fmt.Errorf("search: embedding failed: %w", err)
    }

    f.mu.Lock()
    defer f.mu.Unlock()

    // Remove existing vectors for this document ID (handles re-Add/update)
    f.removeLocked(id)

    // Store one vector per chunk
    for i, chunk := range chunks {
        f.vectors = append(f.vectors, storedVector{
            DocID:  id,
            Chunk:  chunk,
            Vector: embeddings[i],
        })
    }

    return nil
}
```

### Search with Deduplication

When a document has multiple chunks, multiple vectors may match. The deduplication strategy is **max-score dedup**: keep only the highest-scoring chunk per document ID and use that chunk's text as the Snippet.

This gives exactly the right behavior: FlatIndex returns document IDs (not chunk IDs), and the Snippet tells the LLM *which part* of the document was relevant.

```go
func (f *FlatIndex[Doc]) Search(ctx context.Context, query string, topK int) ([]SearchResult, error) {
    queryVec, err := f.embedder.EmbedQuery(ctx, query)
    if err != nil {
        return nil, fmt.Errorf("search: query embedding failed: %w", err)
    }

    f.mu.RLock()

    // Score ALL vectors
    type scored struct {
        docID string
        chunk string
        score float64
    }

    all := make([]scored, len(f.vectors))
    for i, sv := range f.vectors {
        all[i] = scored{
            docID: sv.DocID,
            chunk: sv.Chunk,
            score: cosineSimilarity(queryVec, sv.Vector),
        }
    }

    f.mu.RUnlock()

    // Deduplicate: keep best chunk per document ID
    best := map[string]scored{}
    for _, s := range all {
        if existing, ok := best[s.docID]; !ok || s.score > existing.score {
            best[s.docID] = s
        }
    }

    // Sort by score descending, return top-K
    results := make([]SearchResult, 0, len(best))
    for _, s := range best {
        results = append(results, SearchResult{
            Id:      s.docID,
            Score:   s.score,
            Snippet: s.chunk,
        })
    }
    sort.Slice(results, func(i, j int) bool {
        return results[i].Score > results[j].Score
    })
    if topK > 0 && len(results) > topK {
        results = results[:topK]
    }

    return results, nil
}
```

### Remove and Swap

Remove must delete ALL vectors for a document ID (one per chunk):

```go
func (f *FlatIndex[Doc]) Remove(id string) error {
    f.mu.Lock()
    defer f.mu.Unlock()
    f.removeLocked(id)
    return nil
}

func (f *FlatIndex[Doc]) removeLocked(id string) {
    filtered := f.vectors[:0]
    for _, sv := range f.vectors {
        if sv.DocID != id {
            filtered = append(filtered, sv)
        }
    }
    f.vectors = filtered
}

// Swap builds all new vectors before taking the lock, then atomically replaces.
func (f *FlatIndex[Doc]) Swap(ctx context.Context, docs map[string]Doc) error {
    var newVectors []storedVector
    for id, doc := range docs {
        chunks, err := f.adapter.Convert(doc)
        if err != nil {
            return fmt.Errorf("search: chunk adapter failed for %s: %w", id, err)
        }
        embeddings, err := f.embedder.EmbedDocumentBatch(ctx, chunks)
        if err != nil {
            return fmt.Errorf("search: embedding failed for %s: %w", id, err)
        }
        for i, chunk := range chunks {
            newVectors = append(newVectors, storedVector{
                DocID:  id,
                Chunk:  chunk,
                Vector: embeddings[i],
            })
        }
    }

    f.mu.Lock()
    f.vectors = newVectors
    f.mu.Unlock()

    return nil
}
```

### Cosine Similarity

```go
// cosineSimilarity computes cosine similarity between two L2-normalized vectors.
// When vectors are L2-normalized, cosine similarity equals the dot product.
func cosineSimilarity(a, b []float64) float64 {
    var dot float64
    for i := range a {
        dot += a[i] * b[i]
    }
    return dot
}
```

---

## FusedIndex Implementation

```go
// FusedIndex combines multiple SearchIndex implementations using a Fuser.
//
// Each sub-index is registered with a name that corresponds to entries in
// the Fuser's weight/normalization config and the topK overrides.
//
// Document lifecycle (Add/Remove/Swap) is forwarded to ALL sub-indices.
// Each sub-index uses its own adapter to convert the Doc into its native format.
//
// Search fans out to all sub-indices (each with its own topK for overfetching),
// collects results, passes them to the Fuser, and returns the fused ranking.
type FusedIndex[Doc any] struct {
    indexes    map[string]SearchIndex[Doc]
    topKConfig map[string]int
    fuser      Fuser
}

func NewFusedIndex[Doc any](fuser Fuser, indexes map[string]SearchIndex[Doc], topKConfig map[string]int) *FusedIndex[Doc] {
    return &FusedIndex[Doc]{
        indexes:    indexes,
        topKConfig: topKConfig,
        fuser:      fuser,
    }
}

func (f *FusedIndex[Doc]) Add(ctx context.Context, id string, doc Doc) error {
    for name, index := range f.indexes {
        if err := index.Add(ctx, id, doc); err != nil {
            return fmt.Errorf("search: add to %s failed: %w", name, err)
        }
    }
    return nil
}

func (f *FusedIndex[Doc]) Remove(id string) error {
    for name, index := range f.indexes {
        if err := index.Remove(id); err != nil {
            return fmt.Errorf("search: remove from %s failed: %w", name, err)
        }
    }
    return nil
}

func (f *FusedIndex[Doc]) Swap(ctx context.Context, docs map[string]Doc) error {
    for name, index := range f.indexes {
        if err := index.Swap(ctx, docs); err != nil {
            return fmt.Errorf("search: swap on %s failed: %w", name, err)
        }
    }
    return nil
}

func (f *FusedIndex[Doc]) Search(ctx context.Context, query string, topK int) ([]SearchResult, error) {
    results := make(map[string][]SearchResult)
    for name, index := range f.indexes {
        sourceTopK := f.topKConfig[name]
        if sourceTopK == 0 {
            sourceTopK = topK * 4 // default overfetch: 4x final topK
        }
        indexResults, err := index.Search(ctx, query, sourceTopK)
        if err != nil {
            return nil, fmt.Errorf("search: %s search failed: %w", name, err)
        }
        results[name] = indexResults
    }

    return f.fuser.Fuse(results, topK)
}
```

---

## Score Normalization

### The Problem

BM25 scores are unbounded and query-dependent (28.5 from one query is not comparable to 3.8 from another). Cosine similarity is bounded [0, 1] and stable across queries. Before fusion, BM25 scores must be normalized.

### Per-Query Min-Max Normalization

```go
// minMaxNormalize normalizes search results to [0, 1] using min-max
// across non-zero scores in the result set.
//
// Rules:
//   - Zero scores remain zero ("no match at all", not "worst match")
//   - All zeros → all outputs are zero
//   - Exactly one non-zero → normalizes to 1.0
//   - All non-zero scores equal → all normalize to 1.0
func minMaxNormalize(results []SearchResult) []SearchResult {
    var min, max float64
    var hasNonZero bool
    for _, r := range results {
        if r.Score > 0 {
            if !hasNonZero {
                min = r.Score
                max = r.Score
                hasNonZero = true
            } else {
                if r.Score < min {
                    min = r.Score
                }
                if r.Score > max {
                    max = r.Score
                }
            }
        }
    }

    if !hasNonZero {
        return results
    }

    normalized := make([]SearchResult, len(results))
    for i, r := range results {
        normalized[i] = SearchResult{Id: r.Id, Snippet: r.Snippet}
        if r.Score == 0 {
            normalized[i].Score = 0
        } else if max == min {
            normalized[i].Score = 1.0
        } else {
            normalized[i].Score = (r.Score - min) / (max - min)
        }
    }
    return normalized
}
```

### WeightedLinearFuser Implementation

```go
func (f *WeightedLinearFuser) Fuse(results map[string][]SearchResult, topK int) ([]SearchResult, error) {
    // Normalize sources that need it
    normalized := make(map[string][]SearchResult, len(results))
    for name, sourceResults := range results {
        if f.NormalizeSources[name] {
            normalized[name] = minMaxNormalize(sourceResults)
        } else {
            normalized[name] = sourceResults
        }
    }

    // Merge: union of all document IDs, accumulate weighted scores.
    // Track the best snippet per document (from the highest-contributing source).
    type mergedEntry struct {
        score   float64
        snippet string
        bestRaw float64
    }
    merged := map[string]*mergedEntry{}

    for name, sourceResults := range normalized {
        weight := f.Weights[name]
        for _, r := range sourceResults {
            entry, ok := merged[r.Id]
            if !ok {
                entry = &mergedEntry{}
                merged[r.Id] = entry
            }
            contribution := weight * r.Score
            entry.score += contribution
            if contribution > entry.bestRaw {
                entry.bestRaw = contribution
                entry.snippet = r.Snippet
            }
        }
    }

    // Sort by fused score descending
    fused := make([]SearchResult, 0, len(merged))
    for id, entry := range merged {
        fused = append(fused, SearchResult{
            Id:      id,
            Score:   entry.score,
            Snippet: entry.snippet,
        })
    }
    sort.Slice(fused, func(i, j int) bool {
        return fused[i].Score > fused[j].Score
    })

    if topK > 0 && len(fused) > topK {
        fused = fused[:topK]
    }

    return fused, nil
}
```

---

## Pre-Built Bleve Helpers

### Tool Search Mapping and Query

```go
// NewToolSearchMapping creates a Bleve index mapping optimized for tool search.
//
// The name is indexed TWICE: once as keyword (exact match, boost 10x)
// and once analyzed ("billing" partially matches "get_billing_ledger").
func NewToolSearchMapping() mapping.IndexMapping {
    indexMapping := bleve.NewIndexMapping()

    keyword := bleve.NewKeywordFieldMapping()
    text := bleve.NewTextFieldMapping()
    text.Analyzer = "standard"

    docMapping := bleve.NewDocumentMapping()
    docMapping.AddFieldMappingsAt("name", keyword)
    docMapping.AddFieldMappingsAt("name_analyzed", text)
    docMapping.AddFieldMappingsAt("domain", keyword)
    docMapping.AddFieldMappingsAt("categories", keyword)
    docMapping.AddFieldMappingsAt("keywords", text)
    docMapping.AddFieldMappingsAt("description", text)
    docMapping.AddFieldMappingsAt("synthetic_queries", text)

    indexMapping.DefaultMapping = docMapping
    return indexMapping
}

// NewToolSearchQueryFunc returns a function that builds a Bleve search request
// with field-specific boosting:
//   - Exact name match (10.0)
//   - Keywords match (3.0)
//   - Fuzzy name match (2.0, fuzziness 1)
//   - Synthetic queries match (1.5)
//   - Description match (1.0)
func NewToolSearchQueryFunc(topK int) func(string) (*bleve.SearchRequest, error) {
    return func(queryText string) (*bleve.SearchRequest, error) {
        exactName := bleve.NewMatchQuery(queryText)
        exactName.SetField("name")
        exactName.SetBoost(10.0)

        keywordsMatch := bleve.NewMatchQuery(queryText)
        keywordsMatch.SetField("keywords")
        keywordsMatch.SetBoost(3.0)

        fuzzyName := bleve.NewFuzzyQuery(queryText)
        fuzzyName.SetField("name_analyzed")
        fuzzyName.SetBoost(2.0)
        fuzzyName.SetFuzziness(1)

        syntheticMatch := bleve.NewMatchQuery(queryText)
        syntheticMatch.SetField("synthetic_queries")
        syntheticMatch.SetBoost(1.5)

        descMatch := bleve.NewMatchQuery(queryText)
        descMatch.SetField("description")
        descMatch.SetBoost(1.0)

        disj := bleve.NewDisjunctionQuery(
            exactName, keywordsMatch, fuzzyName, syntheticMatch, descMatch,
        )
        disj.SetMin(1)

        req := bleve.NewSearchRequestOptions(disj, topK, 0, false)
        return req, nil
    }
}
```

### Policy Search Mapping and Query

```go
func NewPolicySearchMapping() mapping.IndexMapping {
    indexMapping := bleve.NewIndexMapping()

    keyword := bleve.NewKeywordFieldMapping()
    text := bleve.NewTextFieldMapping()
    text.Analyzer = "standard"

    docMapping := bleve.NewDocumentMapping()
    docMapping.AddFieldMappingsAt("title", text)
    docMapping.AddFieldMappingsAt("domain", keyword)
    docMapping.AddFieldMappingsAt("keywords", text)
    docMapping.AddFieldMappingsAt("content", text)

    indexMapping.DefaultMapping = docMapping
    return indexMapping
}

// Boost: title (3.0), keywords (2.5), domain (2.0), content (1.0)
func NewPolicySearchQueryFunc(topK int) func(string) (*bleve.SearchRequest, error) {
    return func(queryText string) (*bleve.SearchRequest, error) {
        titleMatch := bleve.NewMatchQuery(queryText)
        titleMatch.SetField("title")
        titleMatch.SetBoost(3.0)

        keywordsMatch := bleve.NewMatchQuery(queryText)
        keywordsMatch.SetField("keywords")
        keywordsMatch.SetBoost(2.5)

        domainMatch := bleve.NewMatchQuery(queryText)
        domainMatch.SetField("domain")
        domainMatch.SetBoost(2.0)

        contentMatch := bleve.NewMatchQuery(queryText)
        contentMatch.SetField("content")
        contentMatch.SetBoost(1.0)

        disj := bleve.NewDisjunctionQuery(
            titleMatch, keywordsMatch, domainMatch, contentMatch,
        )
        disj.SetMin(1)

        req := bleve.NewSearchRequestOptions(disj, topK, 0, false)
        return req, nil
    }
}
```

---

## Embedding Model: multilingual-e5-small

### Why This Specific Model

- **118M parameters, 384 dimensions** — small enough for CPU, large enough for quality.
- **74.80 nDCG@10 on MIRACL-id** — only 1.36 points behind the 5x larger e5-large (76.16).
- **Competitive on English** — outperforms all-MiniLM-L6-v2 on English retrieval.
- **100+ languages** — Indonesian, English, and more without configuration.
- **INT8 ONNX quantized: ~120MB** — reasonable for zero-setup embedded search.
- **Max sequence length: 512 tokens.**
- **Apache 2.0 license.**

**Source:** https://huggingface.co/intfloat/multilingual-e5-small

### ONNX Model Preparation (One-Time)

```bash
pip install optimum[onnxruntime] onnxruntime

optimum-cli export onnx \
    --model intfloat/multilingual-e5-small \
    --task feature-extraction \
    ./e5-small-onnx/

optimum-cli onnxruntime quantize \
    --onnx_model ./e5-small-onnx/ \
    --avx512_vnni \
    -o ./e5-small-onnx-int8/
```

**Alternative Python approach:**

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

### Model Distribution

**Option A: go:embed (simplest, +120MB binary)**

```go
//go:embed model/e5small/model_quantized.onnx
var modelBytes []byte
```

**Option B: Lazy download on first use**

```go
func NewDefaultEmbedder(cfg EmbedderConfig) (Embedder, error) {
    if cfg.ModelPath == "" {
        modelDir, err := ensureModelCached(
            "multilingual-e5-small-int8",
            "sha256:abc123...",
            "https://huggingface.co/intfloat/multilingual-e5-small/resolve/main/onnx/model_quantized.onnx",
        )
        if err != nil {
            return nil, fmt.Errorf("search: model setup failed: %w", err)
        }
        cfg.ModelPath = modelDir
    }
    return newONNXEmbedder(cfg)
}
```

**Option C: go:embed behind build tag `embed_model`**

**Recommendation:** Start with Option A. Add Option B later if binary size becomes a concern.

### Go ONNX Runtime Integration

**Primary: `knights-analytics/hugot`** (https://github.com/knights-analytics/hugot)
- Complete pipeline: tokenization + ONNX inference + pooling
- CGo required (ONNX Runtime + Rust tokenizer)

**Alternative: `yalue/onnxruntime_go`** (https://github.com/yalue/onnxruntime_go) + `daulet/tokenizers` (https://github.com/daulet/tokenizers)
- Lower-level, requires implementing mean pooling manually

### Mean Pooling + L2 Normalization

Required if using yalue/onnxruntime_go directly (hugot handles this internally):

```go
func meanPool(tokenEmbeddings [][]float64, attentionMask []int64) []float64 {
    hiddenDim := len(tokenEmbeddings[0])
    result := make([]float64, hiddenDim)
    var maskSum float64

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

func l2Normalize(v []float64) []float64 {
    var norm float64
    for _, x := range v {
        norm += x * x
    }
    norm = math.Sqrt(norm)
    for i := range v {
        v[i] /= norm
    }
    return v
}
```

---

## How Gent Consumes This Package

```go
// Create shared Embedder
embedder, err := search.NewDefaultEmbedder(search.DefaultEmbedderConfig())

// Create FlatIndex for semantic search
flatIdx := search.NewFlatIndex(toolChunkAdapter, embedder)

// Create BleveIndex for BM25
bleveIdx, err := search.NewBleveIndex(toolBleveAdapter)

// Compose into FusedIndex
fusedIdx := search.NewFusedIndex(
    &search.WeightedLinearFuser{
        Weights:          map[string]float64{"bm25": 0.3, "semantic": 0.7},
        NormalizeSources: map[string]bool{"bm25": true, "semantic": false},
    },
    map[string]search.SearchIndex[IndexableTool]{
        "bm25":     bleveIdx,
        "semantic": flatIdx,
    },
    map[string]int{"bm25": 20, "semantic": 20},
)

// Register a tool — goes to both indices via adapters
fusedIdx.Add(ctx, tool.Name(), tool)

// Search — fuses BM25 + semantic results
results, err := fusedIdx.Search(ctx, "check outstanding payments", 5)
```

---

## Gotchas, Guards, and Potential Issues

### 1. ONNX Runtime Shared Library Must Be Present

**Guard:** Check at `NewDefaultEmbedder()` time with a clear error message pointing to https://github.com/microsoft/onnxruntime/releases.

### 2. CGo Cross-Compilation

**Guard:** Build tags: `embedder_onnx.go` (//go:build cgo) and `embedder_stub.go` (//go:build !cgo) that returns an error message.

### 3. ONNX Runtime Thread Contention

**Guard:** Semaphore (buffered channel) limiting concurrent inference calls. Default `NumThreads: 4`.

### 4. Model Warm-Up Latency

**Guard:** Run a dummy embedding during `NewDefaultEmbedder()` initialization.

### 5. Token Truncation

Inputs exceeding 512 tokens are silently truncated. Tool descriptions (50-200 tokens) are fine. Policy chunks should be < 400 tokens.

**Guard:** Log warning on truncation. Provide `ChunkText()` helper.

### 6. Memory Profile

| Documents | Chunks (avg 3/doc) | Vector memory | With chunk text | Total |
|-----------|-------------------|---------------|-----------------|-------|
| 200 tools | 200 | 0.6 MB | 0.1 MB | ~1 MB |
| 500 policies | 1,500 | 4.5 MB | 3 MB | ~8 MB |
| 10K docs | 30K | 90 MB | 60 MB | ~150 MB |
| 100K docs | 300K | 900 MB | 600 MB | ~1.5 GB |

### 7. FusedIndex Partial Failure on Add

If first sub-index succeeds and second fails, state is inconsistent.

**Guard:** Fail fast, return error. Document that `Swap()` can reset to consistent state.

### 8. Cosine Similarity Score Interpretation

- **0.9+**: near-identical meaning
- **0.5–0.7**: clearly related
- **0.2–0.35**: noise floor
- **Below 0.2**: unrelated

### 9. Re-Indexing Cost on Cold Start

Index is in-memory only. Re-embedding cost: ~15-20ms per document on t3.small.

| Corpus | Cold start time |
|--------|----------------|
| 200 tools | ~4 seconds |
| 500 policies | ~10 seconds |
| 10K docs | ~3 minutes |

**Guard:** Make search optional in agent loop. Fall back to BM25-only if embedding fails.

---

## Default Configuration Rationale

| Setting | Tool Search | Policy Search | Why |
|---------|------------|---------------|-----|
| BM25 weight | 0.3 | 0.4 | Tools need more semantic (vocabulary mismatch). Policies have higher keyword overlap. |
| Semantic weight | 0.7 | 0.6 | See above. |
| Normalize BM25 | true | true | Always unbounded. |
| Normalize semantic | false | false | Already [0, 1]. |
| Source overfetch topK | 20 | 20 | Enough candidates for fusion. |
| Final topK | 5 | 3 | Tools: give LLM options. Policies: fewer, more focused. |

---

## Key Library Links

| Library | Purpose | URL |
|---------|---------|-----|
| **hugot** | Go ONNX transformer pipelines | https://github.com/knights-analytics/hugot |
| **onnxruntime_go** | Low-level ONNX Runtime Go bindings | https://github.com/yalue/onnxruntime_go |
| **daulet/tokenizers** | Go HuggingFace Rust tokenizer bindings | https://github.com/daulet/tokenizers |
| **multilingual-e5-small** | The embedding model | https://huggingface.co/intfloat/multilingual-e5-small |
| **ONNX Runtime** | Native library releases | https://github.com/microsoft/onnxruntime/releases |
| **HuggingFace Optimum** | ONNX export + quantization | https://huggingface.co/docs/optimum/en/onnxruntime/usage_guides/quantization |
| **Bleve** | BM25 full-text search | https://github.com/blevesearch/bleve |
| **LazarusNLP benchmarks** | Indonesian retrieval numbers | https://github.com/LazarusNLP/indonesian-sentence-embeddings |

---

## Implementation Order

### Phase 1: Core Interfaces and Types

1. `SearchResult`, `SearchIndex[Doc]` interface
2. `Embedder` interface with `EmbedQuery`/`EmbedDocument`/`EmbedDocumentBatch`
3. `ChunkAdapter[Doc]`, `BleveAdapter[Doc]` interfaces
4. `Fuser` interface, `WeightedLinearFuser` + `minMaxNormalize`

### Phase 2: Index Implementations

5. `FlatIndex[Doc]` — chunking, dedup, brute-force cosine search, Swap
6. `BleveIndex[Doc]` — wrapping Bleve with adapter pattern
7. `FusedIndex[Doc]` — composing N indices with a Fuser
8. Pre-built Bleve helpers (mappings + query builders)

### Phase 3: Embedder Implementation

9. ONNX Embedder via hugot with pre-packaged e5-small
10. Build tags for CGo/non-CGo
11. Warm-up, semaphore, truncation warning

### Phase 4: Utilities and Testing

12. `ChunkText()` helper
13. Unit tests (normalization, fusion, cosine similarity — no model needed)
14. Integration tests (real ONNX model — skip if unavailable)
15. Benchmarks at 1K/10K/100K scales

---

## Testing Strategy

### Unit Tests (No Model Required)

```go
func TestMinMaxNormalize_AllZeros(t *testing.T) {
    input := []SearchResult{{Id: "a", Score: 0}, {Id: "b", Score: 0}}
    result := minMaxNormalize(input)
    assert.Equal(t, 0.0, result[0].Score)
    assert.Equal(t, 0.0, result[1].Score)
}

func TestMinMaxNormalize_SingleNonZero(t *testing.T) {
    input := []SearchResult{{Id: "a", Score: 5.0}, {Id: "b", Score: 0}}
    result := minMaxNormalize(input)
    assert.Equal(t, 1.0, result[0].Score)
    assert.Equal(t, 0.0, result[1].Score)
}

func TestWeightedLinearFuser_SemanticOnlyMatch(t *testing.T) {
    fuser := &WeightedLinearFuser{
        Weights:          map[string]float64{"bm25": 0.3, "semantic": 0.7},
        NormalizeSources: map[string]bool{"bm25": true, "semantic": false},
    }

    results := map[string][]SearchResult{
        "bm25":     {},
        "semantic": {
            {Id: "tool_a", Score: 0.89, Snippet: "billing ledger tool"},
            {Id: "tool_b", Score: 0.45, Snippet: "invoice tool"},
        },
    }

    fused, err := fuser.Fuse(results, 5)
    require.NoError(t, err)
    assert.Equal(t, "tool_a", fused[0].Id)
    assert.InDelta(t, 0.623, fused[0].Score, 0.01)
}

func TestFusedIndex_SearchFanout(t *testing.T) {
    mockBM25 := &mockIndex{results: []SearchResult{
        {Id: "a", Score: 28.5, Snippet: "exact match"},
        {Id: "b", Score: 6.1, Snippet: "partial match"},
    }}
    mockSemantic := &mockIndex{results: []SearchResult{
        {Id: "a", Score: 0.95, Snippet: "semantic match a"},
        {Id: "c", Score: 0.82, Snippet: "semantic match c"},
    }}

    fused := NewFusedIndex(
        &WeightedLinearFuser{
            Weights:          map[string]float64{"bm25": 0.3, "semantic": 0.7},
            NormalizeSources: map[string]bool{"bm25": true, "semantic": false},
        },
        map[string]SearchIndex[string]{"bm25": mockBM25, "semantic": mockSemantic},
        map[string]int{"bm25": 20, "semantic": 20},
    )

    results, err := fused.Search(ctx, "test query", 3)
    require.NoError(t, err)
    assert.Equal(t, "a", results[0].Id)
}
```

### Integration Tests (Require Model)

```go
func TestFlatIndex_SemanticRetrieval(t *testing.T) {
    embedder, err := search.NewDefaultEmbedder(search.DefaultEmbedderConfig())
    if err != nil {
        t.Skip("embedder not available: " + err.Error())
    }
    defer embedder.Close()

    adapter := &singleChunkAdapter{}
    idx := search.NewFlatIndex(adapter, embedder)

    idx.Add(ctx, "billing", "Retrieve billing ledger entries for a customer")
    idx.Add(ctx, "notify", "Send a notification via email or SMS")
    idx.Add(ctx, "checkout", "Process early checkout and refund")

    results, err := idx.Search(ctx, "check outstanding payments", 2)
    require.NoError(t, err)
    assert.Equal(t, "billing", results[0].Id)
}

func TestFlatIndex_MultiChunkDedup(t *testing.T) {
    embedder, err := search.NewDefaultEmbedder(search.DefaultEmbedderConfig())
    if err != nil {
        t.Skip("embedder not available: " + err.Error())
    }
    defer embedder.Close()

    adapter := &multiChunkAdapter{}
    idx := search.NewFlatIndex(adapter, embedder)
    idx.Add(ctx, "policy-1", longPolicyDocument)

    results, err := idx.Search(ctx, "cancellation procedure", 5)
    require.NoError(t, err)

    // policy-1 appears only ONCE despite multiple chunks
    policyCount := 0
    for _, r := range results {
        if r.Id == "policy-1" {
            policyCount++
        }
    }
    assert.Equal(t, 1, policyCount)
    assert.NotEmpty(t, results[0].Snippet) // best-matching chunk text
}

func TestFlatIndex_SwapAtomicReplacement(t *testing.T) {
    // ...setup...
    idx.Add(ctx, "old-tool", oldTool)
    idx.Swap(ctx, map[string]string{"new-tool": newTool})

    results, _ := idx.Search(ctx, "old tool query", 5)
    for _, r := range results {
        assert.NotEqual(t, "old-tool", r.Id)
    }
}

func TestFlatIndex_CrossLingual(t *testing.T) {
    // ...setup...
    idx.Add(ctx, "kos", "Affordable boarding house near Sudirman")

    results, _ := idx.Search(ctx, "cari kos murah di sudirman", 5)
    assert.Equal(t, "kos", results[0].Id)
    assert.Greater(t, results[0].Score, 0.5)
}
```