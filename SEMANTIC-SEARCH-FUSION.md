# Hybrid Search Fusion Implementation Guide

## Context

This document details the design and implementation of a hybrid search system that combines BM25 full-text search (via Bleve) with semantic vector search (via the `embedsearch` package). The hybrid search is a standalone, reusable component that both `ToolSearchToolChain` and `PolicySearch` will consume — but it has no knowledge of tools, policies, or agents.

This document assumes the `embedsearch` package (described in `SEMANTIC-SEARCH-IMPLEMENTATION.md`) already exists and provides:
- `Embedder` interface (converts text → `[]float32` vectors)
- `SearchIndex` interface (stores documents, searches by semantic similarity)
- `SearchResult` type (document + cosine similarity score)
- `Document` type (ID, text, metadata)
- Default implementation using multilingual-e5-small via ONNX Runtime

---

## Problem: Why Hybrid Search?

BM25 and semantic search fail in complementary ways:

**BM25 is great at:**
- Exact term matching ("get_billing_ledger" → finds `get_billing_ledger`)
- Rare/specific terms, IDs, codes, abbreviations
- When the user uses the exact vocabulary from the indexed content

**BM25 fails at:**
- Synonyms and paraphrases ("check outstanding payments" → won't find `get_billing_ledger`)
- Cross-language queries (Indonesian query → English tool descriptions)
- Intent-level matching ("customer wants to leave early" → won't find `early_termination`)

**Semantic search is great at:**
- Understanding meaning regardless of vocabulary ("check outstanding payments" → finds `get_billing_ledger` because it understands billing semantics)
- Cross-language matching (e5-small handles 100+ languages)
- Intent-level matching

**Semantic search fails at:**
- Very short queries (2-3 characters, abbreviations like "jkt")
- Exact identifiers and codes
- When the model's training data didn't cover domain-specific jargon

**Combining both covers each other's blind spots.** The challenge is merging two ranked lists with incompatible score scales.

---

## The Score Incompatibility Problem

**Cosine similarity (semantic search) is naturally bounded: [-1.0, 1.0].**
In practice, L2-normalized embeddings from e5-small produce scores in [0.0, 1.0]. These scores are stable and comparable across different queries:
- 0.9+ = near-identical meaning
- 0.5–0.7 = clearly related
- 0.2–0.35 = noise floor (shared language structure)
- Below 0.2 = unrelated

A semantic score of 0.7 means roughly the same thing whether you're searching tools or policies. You can threshold on it directly (`if score > 0.5` is a meaningful condition).

**BM25 scores are unbounded and query-dependent.**
A query with rare terms against a short document might score 28.5. A common-term query against a long document scores 2.1. The absolute number is meaningless across queries. A score of 15.0 could be excellent for one query and terrible for another. You cannot threshold on raw BM25 scores.

**This means BM25 scores must be normalized before fusion.** Semantic scores do not need normalization — they're already in a stable, bounded range.

### Per-Query Min-Max Normalization for BM25

For each query, normalize BM25 scores to [0, 1] using the result set from that query:

```
normalized = (score - min) / (max - min)
```

Rules:
- Only consider non-zero scores for min/max calculation (zero means "no match at all," not "worst match")
- If all scores are zero → all normalized scores are 0 (BM25 found nothing)
- If exactly one non-zero score → that score normalizes to 1.0 (it's the best and only match)
- If max == min (all non-zero scores identical) → all normalize to 1.0

This does NOT make BM25 scores "accurate" in absolute terms. It makes them **comparable within a single query** — "this is the best BM25 match for this query" (1.0) vs "this is the worst non-zero match" (0.0). That relative ranking is the only useful signal BM25 carries.

**Semantic scores need no normalization.** They're already bounded and cross-query comparable. Pass them through unchanged.

---

## Chosen Fusion Strategy: Weighted Linear Score Combination

After evaluating RRF, linear combination, cascaded/fallback, and score-gated fusion, we chose **weighted linear score combination** as the default because:

1. **It respects score magnitude.** A high-confidence semantic match with zero BM25 match still scores well. RRF would penalize it because it only appears in one ranked list.

2. **It handles the "found by one method only" case correctly.** When BM25 returns zero for a document, its contribution is simply 0, and the semantic score carries the result. This is critical for tool search where the most relevant tool often has zero keyword overlap with the query.

3. **It's simple to understand and tune.** One parameter (α) controls the balance. No complex rank arithmetic.

4. **It degrades gracefully.** If semantic search is down, BM25 results pass through (scaled by α). If BM25 returns all zeros, semantic results pass through (scaled by 1-α).

### Why NOT RRF

RRF (Reciprocal Rank Fusion) uses `score = Σ 1/(k + rank_i)` across retrieval methods. It treats rank position as the signal, not score magnitude. This creates a specific failure mode:

A document that's rank #1 in semantic (extremely relevant, score 0.95) but absent from BM25 gets: `1/(60+1) + 0 = 0.0164`. A document that's rank #5 in both gets: `1/(60+5) + 1/(60+5) = 0.0308` — almost double. **RRF systematically favors "okay in both" over "perfect in one."**

For tool search, this is exactly wrong. The whole point of semantic search is finding tools where keyword matching fails.

We do NOT implement RRF, but the `Fuser` interface allows users to bring their own if they prefer it for their use case.

---

## Architecture

```
hybridsearch/                      ← Standalone package, no gent imports
├── fuser.go                       ← Fuser interface + LinearFuser implementation
├── normalize.go                   ← Score normalization utilities
├── hybrid_index.go                ← HybridIndex: wraps Bleve + embedsearch.SearchIndex
├── hybrid_index_config.go         ← Configuration with sensible defaults
├── bleve_helpers.go               ← Bleve index mapping + default query builders
├── result.go                      ← HybridResult type (unified result from fusion)
└── doc.go                         ← Package documentation
```

The package depends on:
- `github.com/blevesearch/bleve/v2` — BM25 full-text search
- The `embedsearch` package — semantic vector search
- No gent imports. No knowledge of tools, policies, or agents.

---

## Interfaces and Types

### Fuser Interface

```go
package hybridsearch

// RankedResult is a single result from one search source.
// Used as input to Fuser. Score semantics depend on the source
// (BM25 scores are unbounded, cosine similarity is [-1, 1]).
type RankedResult struct {
    // ID uniquely identifies the document across sources.
    ID string

    // Score is the relevance score from the search source.
    Score float32
}

// FusedResult is the output of a Fuser: a document ID with a
// combined score from multiple search sources.
type FusedResult struct {
    // ID uniquely identifies the document.
    ID string

    // Score is the combined relevance score after fusion.
    // Range depends on the Fuser implementation.
    // LinearFuser produces scores in [0, 1].
    Score float32
}

// Fuser combines ranked results from multiple search sources
// into a single ranked list.
//
// Each input slice represents results from one source (e.g., BM25, semantic).
// The Fuser merges them, resolves duplicates, and returns a unified ranking.
//
// Implementations must handle:
//   - Documents appearing in only one source (the other score is 0)
//   - Empty result sets from one or more sources
//   - Different score scales across sources (e.g., BM25 vs cosine similarity)
type Fuser interface {
    // Fuse merges multiple ranked result lists into a single ranked list.
    // Results are returned sorted by score descending.
    // The topK parameter limits the number of returned results (0 = no limit).
    Fuse(topK int, results ...[]RankedResult) []FusedResult
}
```

### LinearFuser Implementation

```go
// LinearFuser combines results using weighted linear score combination.
//
// For each document, the fused score is:
//
//   score = weights[0]*norm(source0) + weights[1]*norm(source1) + ...
//
// where norm() applies per-source min-max normalization when
// NormalizeSources[i] is true (recommended for unbounded scores like BM25).
//
// Sources with bounded scores (like cosine similarity from semantic search)
// should have NormalizeSources[i] = false — their scores pass through unchanged.
//
// # Example: 30% BM25 + 70% Semantic
//
//   fuser := &LinearFuser{
//       Weights:          []float32{0.3, 0.7},
//       NormalizeSources: []bool{true, false},  // normalize BM25, not semantic
//   }
//   results := fuser.Fuse(5, bm25Results, semanticResults)
//
// # Score interpretation
//
// Output scores are in [0, 1] when all weights sum to 1.0.
// A score of 0.7+ generally indicates high relevance.
// A score below 0.3 is rarely useful.
type LinearFuser struct {
    // Weights per source. Must be same length as the number of sources
    // passed to Fuse(). Should sum to 1.0 for interpretable output scores.
    Weights []float32

    // NormalizeSources controls per-source min-max normalization.
    // Set to true for sources with unbounded scores (BM25).
    // Set to false for sources with bounded scores (cosine similarity).
    // Must be same length as Weights.
    NormalizeSources []bool
}

func (f *LinearFuser) Fuse(topK int, results ...[]RankedResult) []FusedResult {
    if len(results) != len(f.Weights) {
        panic("hybridsearch: number of result sets must match number of weights")
    }

    // Normalize sources that need it
    normalized := make([][]RankedResult, len(results))
    for i, source := range results {
        if f.NormalizeSources[i] {
            normalized[i] = minMaxNormalize(source)
        } else {
            normalized[i] = source
        }
    }

    // Merge: union of all document IDs
    scores := map[string]float32{}
    for i, source := range normalized {
        for _, r := range source {
            scores[r.ID] += f.Weights[i] * r.Score
        }
    }

    // Sort by score descending
    fused := make([]FusedResult, 0, len(scores))
    for id, score := range scores {
        fused = append(fused, FusedResult{ID: id, Score: score})
    }
    sort.Slice(fused, func(a, b int) bool {
        return fused[a].Score > fused[b].Score
    })

    // Apply topK limit
    if topK > 0 && len(fused) > topK {
        fused = fused[:topK]
    }

    return fused
}
```

### Normalization

```go
// minMaxNormalize applies per-query min-max normalization to a result set.
//
// Rules:
//   - Zero scores remain zero (they represent "no match", not "worst match")
//   - If all scores are zero, all outputs are zero
//   - If exactly one non-zero score, it normalizes to 1.0
//   - If all non-zero scores are equal, they all normalize to 1.0
//
// This is the correct normalization for BM25 scores where 0.0 means
// "this term didn't appear in the document at all" — a fundamentally
// different signal from "appeared but scored poorly."
func minMaxNormalize(results []RankedResult) []RankedResult {
    // Find min and max of non-zero scores
    var min, max float32
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

    // All zeros → return as-is
    if !hasNonZero {
        return results
    }

    normalized := make([]RankedResult, len(results))
    for i, r := range results {
        normalized[i] = RankedResult{ID: r.ID}
        if r.Score == 0 {
            normalized[i].Score = 0
        } else if max == min {
            // All non-zero scores are identical (or only one non-zero score)
            normalized[i].Score = 1.0
        } else {
            normalized[i].Score = (r.Score - min) / (max - min)
        }
    }

    return normalized
}
```

---

## HybridIndex: The Unified Search Interface

`HybridIndex` is the main type users interact with. It wraps a Bleve index and an `embedsearch.SearchIndex`, runs queries against both, and fuses results.

```go
// HybridIndex provides hybrid BM25 + semantic search over a document corpus.
//
// Documents are indexed in both Bleve (for BM25 keyword search) and
// embedsearch.SearchIndex (for semantic vector search). Queries run against
// both, and results are fused using the configured Fuser.
//
// HybridIndex is safe for concurrent use.
type HybridIndex struct {
    bleve     bleve.Index
    semantic  embedsearch.SearchIndex
    fuser     Fuser
    queryFunc func(string) query.Query // builds Bleve query from raw text
    docs      map[string]HybridDocument // stored for retrieval after fusion
    mu        sync.RWMutex
}

// HybridDocument is the input to HybridIndex.Add().
// It carries both structured fields (for BM25) and raw text (for embedding).
type HybridDocument struct {
    // ID uniquely identifies this document. Required.
    ID string

    // Fields are structured key-value pairs indexed by Bleve.
    // Keys must match field names in the Bleve index mapping.
    // Example: {"name": "get_billing_ledger", "description": "...", "keywords": "billing payment"}
    Fields map[string]string

    // EmbedText is the text that gets embedded for semantic search.
    // Typically a concatenation of the most semantically meaningful fields.
    // Example: "get_billing_ledger: Look up billing ledger entries. Keywords: billing, payment, invoice"
    EmbedText string
}

// HybridResult is the output of HybridIndex.Search().
type HybridResult struct {
    // Document is the original indexed document.
    Document HybridDocument

    // Score is the fused relevance score. Range depends on Fuser.
    // LinearFuser with weights summing to 1.0 produces [0, 1].
    Score float32
}

// HybridIndexConfig configures a HybridIndex.
type HybridIndexConfig struct {
    // BleveMapping defines how Bleve indexes document fields.
    // If nil, a default mapping is used (all fields as text, analyzed).
    BleveMapping mapping.IndexMapping

    // SemanticIndex is the embedsearch.SearchIndex for vector search.
    // Required. The caller owns its lifecycle.
    SemanticIndex embedsearch.SearchIndex

    // Fuser combines BM25 and semantic results.
    // Default: LinearFuser with weights {0.3, 0.7} (30% BM25, 70% semantic).
    Fuser Fuser

    // QueryFunc builds a Bleve query from raw query text.
    // Default: simple MatchQuery across all fields.
    // Override this for field-specific boosting (see Bleve Query section below).
    QueryFunc func(string) query.Query

    // BM25TopK is how many results to fetch from BM25 before fusion.
    // Should be >= final TopK to give the fuser enough candidates.
    // Default: 20.
    BM25TopK int

    // SemanticTopK is how many results to fetch from semantic search before fusion.
    // Default: 20.
    SemanticTopK int
}
```

### HybridIndex Methods

```go
func NewHybridIndex(cfg HybridIndexConfig) (*HybridIndex, error)

// Add indexes a document in both Bleve and the semantic index.
func (h *HybridIndex) Add(ctx context.Context, doc HybridDocument) error

// AddBatch indexes multiple documents. Batches embedding calls for efficiency.
func (h *HybridIndex) AddBatch(ctx context.Context, docs []HybridDocument) error

// Search runs the query against both BM25 and semantic search,
// fuses the results, and returns the top-K matches.
func (h *HybridIndex) Search(ctx context.Context, query string, topK int) ([]HybridResult, error)

// Remove deletes a document from both indices.
func (h *HybridIndex) Remove(id string) error

// Len returns the number of indexed documents.
func (h *HybridIndex) Len() int

// Close releases resources. Does NOT close the semantic index's Embedder.
func (h *HybridIndex) Close() error
```

### Search Implementation

```go
func (h *HybridIndex) Search(ctx context.Context, queryText string, topK int) ([]HybridResult, error) {
    // 1. BM25 search via Bleve
    bleveQuery := h.queryFunc(queryText)
    bleveReq := bleve.NewSearchRequestOptions(bleveQuery, h.cfg.BM25TopK, 0, false)
    bleveResults, err := h.bleve.Search(bleveReq)
    if err != nil {
        return nil, fmt.Errorf("hybridsearch: bleve search failed: %w", err)
    }

    bm25Ranked := make([]RankedResult, 0, len(bleveResults.Hits))
    for _, hit := range bleveResults.Hits {
        bm25Ranked = append(bm25Ranked, RankedResult{
            ID:    hit.ID,
            Score: float32(hit.Score),
        })
    }

    // 2. Semantic search via embedsearch
    semResults, err := h.semantic.Search(ctx, queryText, embedsearch.SearchOptions{
        TopK:     h.cfg.SemanticTopK,
        MinScore: 0.0, // let the fuser handle filtering
    })
    if err != nil {
        return nil, fmt.Errorf("hybridsearch: semantic search failed: %w", err)
    }

    semRanked := make([]RankedResult, 0, len(semResults))
    for _, r := range semResults {
        semRanked = append(semRanked, RankedResult{
            ID:    r.Document.ID,
            Score: r.Score,
        })
    }

    // 3. Fuse: BM25 is source 0, semantic is source 1
    fused := h.fuser.Fuse(topK, bm25Ranked, semRanked)

    // 4. Hydrate: look up full documents for fused results
    h.mu.RLock()
    defer h.mu.RUnlock()

    results := make([]HybridResult, 0, len(fused))
    for _, f := range fused {
        doc, ok := h.docs[f.ID]
        if !ok {
            continue // document was removed between search and hydration
        }
        results = append(results, HybridResult{
            Document: doc,
            Score:    f.Score,
        })
    }

    return results, nil
}
```

---

## Default Bleve Query Builders

The `hybridsearch` package provides query builder functions that consumers (ToolSearchToolChain, PolicySearch, or external users) can use. These are **not hardcoded** — they're passed via `HybridIndexConfig.QueryFunc`.

### ToolSearchQueryFunc

Designed for searching tool registries. Uses field-specific boosting with an exact-match name field at highest priority.

**Bleve index mapping for tools:**

```go
// NewToolSearchMapping creates a Bleve index mapping optimized for tool search.
//
// Fields:
//   - "name": keyword (exact match, no analysis)
//   - "name_analyzed": text (standard analyzer — for partial/fuzzy matching)
//   - "domain": keyword
//   - "categories": keyword
//   - "keywords": text (standard analyzer)
//   - "description": text (standard analyzer)
//   - "synthetic_queries": text (standard analyzer)
//
// The name is indexed TWICE: once as keyword for exact match (boost 10x)
// and once analyzed for partial matching ("billing" matches "get_billing_ledger").
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
```

**Default query builder for tools:**

```go
// NewToolSearchQueryFunc returns a Bleve query builder for tool search.
//
// The query is a disjunction (OR) of field-specific matches with boosting:
//   - Exact name match (boost 10.0) — catches "call get_billing_ledger"
//   - Keywords match (boost 3.0) — catches tool-registered keywords
//   - Fuzzy name match (boost 2.0, fuzziness 1) — catches partial name matches
//   - Synthetic queries match (boost 1.5) — catches natural language intent
//   - Description match (boost 1.0) — catches general topic overlap
//
// The boost hierarchy ensures:
//   1. Explicit tool name → overwhelmingly high BM25 score
//   2. Keyword hit → strong BM25 signal
//   3. Description/synthetic overlap → moderate BM25 signal
//   4. No match → BM25 score is 0, semantic search carries the result
func NewToolSearchQueryFunc() func(string) query.Query {
    return func(queryText string) query.Query {
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
        disj.SetMin(1) // at least one clause must match
        return disj
    }
}
```

### PolicySearchQueryFunc

Simpler than tool search — no "name" field for exact matching.

```go
// NewPolicySearchMapping creates a Bleve index mapping for policy/SOP search.
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

// NewPolicySearchQueryFunc returns a Bleve query builder for policy search.
//
// Boosting:
//   - Title match (boost 3.0) — policies are often searched by topic
//   - Keywords match (boost 2.5) — domain-specific terms
//   - Domain match (boost 2.0) — "billing" matches Billing-domain policies
//   - Content match (boost 1.0) — general text overlap
func NewPolicySearchQueryFunc() func(string) query.Query {
    return func(queryText string) query.Query {
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
        return disj
    }
}
```

---

## Default Configuration and Rationale

```go
func DefaultToolSearchFuser() Fuser {
    return &LinearFuser{
        Weights:          []float32{0.3, 0.7},
        NormalizeSources: []bool{true, false},
    }
}

func DefaultPolicySearchFuser() Fuser {
    return &LinearFuser{
        Weights:          []float32{0.4, 0.6},
        NormalizeSources: []bool{true, false},
    }
}
```

| Setting | ToolSearch | PolicySearch | Why |
|---------|-----------|--------------|-----|
| BM25 weight (α) | 0.3 | 0.4 | Tool search benefits more from semantic (agent uses different vocabulary than tool names). Policy search has higher keyword overlap (policies use formal, findable terms). |
| Semantic weight (1-α) | 0.7 | 0.6 | See above. |
| Normalize BM25 | true | true | BM25 scores are always unbounded. |
| Normalize semantic | false | false | Cosine similarity is already in [0, 1] and cross-query comparable. |
| BM25TopK | 20 | 20 | Overfetch so the fuser has enough candidates. At 50-200 tools, 20 is generous. |
| SemanticTopK | 20 | 20 | Same reasoning. |
| Final TopK | 5 | 3 | Tools: give the LLM a few options. Policies: inject fewer but more relevant chunks to avoid context bloat. |

---

## Worked Examples

### Example 1: Agent searches for tool using natural language

**Query:** "check outstanding payments"
**Indexed tools:** 5 tools in registry

| Tool | BM25 Raw | Semantic | BM25 Normalized | Fused (0.3 BM25 + 0.7 Sem) |
|------|----------|----------|-----------------|----------------------------|
| get_billing_ledger | 0.0 | 0.89 | 0.00 | **0.623** |
| get_payment_history | 3.8 | 0.85 | 1.00 | **0.895** |
| list_invoices | 1.2 | 0.82 | 0.00* | **0.574** |
| lookup_reservation | 0.0 | 0.41 | 0.00 | 0.287 |
| send_notification | 0.0 | 0.12 | 0.00 | 0.084 |

*Note: list_invoices BM25 of 1.2 normalizes to 0.0 because min-max treats it as the minimum non-zero score when max=3.8: (1.2-1.2)/(3.8-1.2) = 0.0.

**Result ranking:** get_payment_history (0.895) > get_billing_ledger (0.623) > list_invoices (0.574)

**Key observation:** get_billing_ledger has ZERO BM25 match (no keyword overlap with "outstanding payments") but still ranks #2 because semantic search gives it 0.89. Without hybrid search, pure BM25 would completely miss it.

### Example 2: Agent explicitly names a tool

**Query:** "get_billing_ledger"

| Tool | BM25 Raw | Semantic | BM25 Normalized | Fused |
|------|----------|----------|-----------------|-------|
| get_billing_ledger | 28.5 | 0.95 | 1.00 | **0.965** |
| get_payment_history | 6.1 | 0.72 | 0.08 | **0.528** |
| list_invoices | 4.2 | 0.61 | 0.00 | 0.427 |
| lookup_reservation | 0.0 | 0.22 | 0.00 | 0.154 |
| send_notification | 0.0 | 0.08 | 0.00 | 0.056 |

**Result:** Exact match gets 0.965 — overwhelmingly first place. The 10x boost on exact name match in Bleve creates a massive raw BM25 score (28.5) that normalizes to 1.0, plus strong semantic similarity.

### Example 3: Colloquial agent reasoning

**Query:** "customer wants to leave early"

| Tool | BM25 Raw | Semantic | BM25 Normalized | Fused |
|------|----------|----------|-----------------|-------|
| early_termination | 2.1 | 0.91 | 1.00 | **0.937** |
| cancel_reservation | 0.0 | 0.83 | 0.00 | 0.581 |
| process_checkout | 0.0 | 0.78 | 0.00 | 0.546 |
| get_billing_ledger | 0.0 | 0.35 | 0.00 | 0.245 |
| send_notification | 0.0 | 0.21 | 0.00 | 0.147 |

**Key observation:** Only one tool has any BM25 match at all ("early" appears in "early_termination"). Semantic search finds three relevant tools. The fused ranking correctly places early_termination first (both signals agree) and cancel_reservation second (strong semantic match despite zero BM25).

---

## How Gent Consumes This

### ToolSearchToolChain

```go
// In ToolSearchToolChain initialization:
hybridIdx, err := hybridsearch.NewHybridIndex(hybridsearch.HybridIndexConfig{
    BleveMapping:  hybridsearch.NewToolSearchMapping(),
    SemanticIndex: semanticIdx, // from embedsearch package
    Fuser:         hybridsearch.DefaultToolSearchFuser(),
    QueryFunc:     hybridsearch.NewToolSearchQueryFunc(),
})

// When registering a tool that implements IndexableTool:
hybridIdx.Add(ctx, hybridsearch.HybridDocument{
    ID: tool.Name(),
    Fields: map[string]string{
        "name":              tool.Name(),
        "name_analyzed":     tool.Name(), // same value, different Bleve field mapping
        "domain":            tool.Domain(),
        "categories":        strings.Join(tool.Categories(), " "),
        "keywords":          strings.Join(tool.Keywords(), " "),
        "description":       tool.Description(),
        "synthetic_queries": strings.Join(tool.SyntheticQueries(), "\n"),
    },
    EmbedText: fmt.Sprintf("%s: %s\nKeywords: %s\nQueries: %s",
        tool.Name(),
        tool.Description(),
        strings.Join(tool.Keywords(), ", "),
        strings.Join(tool.SyntheticQueries(), "; "),
    ),
})

// When searching for tools:
results, err := hybridIdx.Search(ctx, agentQuery, 5)
```

### PolicySearch (future)

```go
hybridIdx, err := hybridsearch.NewHybridIndex(hybridsearch.HybridIndexConfig{
    BleveMapping:  hybridsearch.NewPolicySearchMapping(),
    SemanticIndex: semanticIdx,
    Fuser:         hybridsearch.DefaultPolicySearchFuser(),
    QueryFunc:     hybridsearch.NewPolicySearchQueryFunc(),
})
```

Same `HybridIndex`, different mapping, query builder, and fuser weights.

---

## Custom Bleve Queries

Users can override the default Bleve query with their own. This is where your autocomplete-style fuzzy escalation pattern can be applied:

```go
// Example: user wants exact-first, then fuzzy fallback (like autocomplete)
cfg := hybridsearch.HybridIndexConfig{
    QueryFunc: func(queryText string) query.Query {
        // Try exact match first with high boost
        exact := bleve.NewTermQuery(queryText)
        exact.SetField("name")
        exact.SetBoost(20.0)

        // Also try analyzed match
        analyzed := bleve.NewMatchQuery(queryText)
        analyzed.SetField("name_analyzed")
        analyzed.SetBoost(5.0)

        // Fuzzy as fallback
        fuzzy := bleve.NewFuzzyQuery(queryText)
        fuzzy.SetField("description")
        fuzzy.SetFuzziness(1)
        fuzzy.SetBoost(1.0)

        return bleve.NewDisjunctionQuery(exact, analyzed, fuzzy)
    },
    // ...
}
```

Another example — conjunction queries for structured search:

```go
// User wants: domain must match AND (name or description matches)
cfg := hybridsearch.HybridIndexConfig{
    QueryFunc: func(queryText string) query.Query {
        // Parse "billing: check payment" → domain=billing, query="check payment"
        domain, text := parseDomainPrefix(queryText)

        domainQ := bleve.NewTermQuery(domain)
        domainQ.SetField("domain")

        textQ := bleve.NewMatchQuery(text)

        return bleve.NewConjunctionQuery(domainQ, textQ)
    },
}
```

---

## Testing Strategy

### Unit Tests for Normalization

```go
func TestMinMaxNormalize_AllZeros(t *testing.T) {
    input := []RankedResult{{ID: "a", Score: 0}, {ID: "b", Score: 0}}
    result := minMaxNormalize(input)
    assert.Equal(t, float32(0), result[0].Score)
    assert.Equal(t, float32(0), result[1].Score)
}

func TestMinMaxNormalize_SingleNonZero(t *testing.T) {
    input := []RankedResult{{ID: "a", Score: 5.0}, {ID: "b", Score: 0}}
    result := minMaxNormalize(input)
    assert.Equal(t, float32(1.0), result[0].Score) // only match → 1.0
    assert.Equal(t, float32(0), result[1].Score)   // zero stays zero
}

func TestMinMaxNormalize_Range(t *testing.T) {
    input := []RankedResult{
        {ID: "a", Score: 10.0},
        {ID: "b", Score: 5.0},
        {ID: "c", Score: 0},
        {ID: "d", Score: 2.5},
    }
    result := minMaxNormalize(input)
    assert.InDelta(t, float32(1.0), result[0].Score, 0.001)   // max → 1.0
    assert.InDelta(t, float32(0.333), result[1].Score, 0.01)  // (5-2.5)/(10-2.5)
    assert.Equal(t, float32(0), result[2].Score)               // zero stays zero
    assert.InDelta(t, float32(0.0), result[3].Score, 0.001)   // min non-zero → 0.0
}

func TestMinMaxNormalize_AllEqual(t *testing.T) {
    input := []RankedResult{{ID: "a", Score: 3.0}, {ID: "b", Score: 3.0}}
    result := minMaxNormalize(input)
    assert.Equal(t, float32(1.0), result[0].Score) // all equal → all 1.0
    assert.Equal(t, float32(1.0), result[1].Score)
}
```

### Unit Tests for LinearFuser

```go
func TestLinearFuser_SemanticOnlyMatch(t *testing.T) {
    fuser := &LinearFuser{
        Weights:          []float32{0.3, 0.7},
        NormalizeSources: []bool{true, false},
    }

    bm25 := []RankedResult{} // BM25 found nothing
    sem := []RankedResult{
        {ID: "tool_a", Score: 0.89},
        {ID: "tool_b", Score: 0.45},
    }

    results := fuser.Fuse(5, bm25, sem)

    assert.Equal(t, "tool_a", results[0].ID)
    assert.InDelta(t, 0.623, results[0].Score, 0.01) // 0.7 * 0.89
    assert.Equal(t, "tool_b", results[1].ID)
    assert.InDelta(t, 0.315, results[1].Score, 0.01) // 0.7 * 0.45
}

func TestLinearFuser_BothMatch(t *testing.T) {
    fuser := &LinearFuser{
        Weights:          []float32{0.3, 0.7},
        NormalizeSources: []bool{true, false},
    }

    bm25 := []RankedResult{
        {ID: "tool_a", Score: 28.5},
        {ID: "tool_b", Score: 6.1},
    }
    sem := []RankedResult{
        {ID: "tool_a", Score: 0.95},
        {ID: "tool_b", Score: 0.72},
    }

    results := fuser.Fuse(5, bm25, sem)

    // tool_a: 0.3 * 1.0 (normalized max) + 0.7 * 0.95 = 0.965
    assert.Equal(t, "tool_a", results[0].ID)
    assert.InDelta(t, 0.965, results[0].Score, 0.01)
}

func TestLinearFuser_TopKLimit(t *testing.T) {
    fuser := &LinearFuser{
        Weights:          []float32{0.3, 0.7},
        NormalizeSources: []bool{true, false},
    }

    sem := []RankedResult{
        {ID: "a", Score: 0.9},
        {ID: "b", Score: 0.8},
        {ID: "c", Score: 0.7},
        {ID: "d", Score: 0.6},
        {ID: "e", Score: 0.5},
    }

    results := fuser.Fuse(3, []RankedResult{}, sem)
    assert.Len(t, results, 3)
    assert.Equal(t, "a", results[0].ID)
    assert.Equal(t, "c", results[2].ID)
}
```

### Integration Test with Real Bleve + Semantic Index

```go
func TestHybridIndex_ToolSearch_EndToEnd(t *testing.T) {
    // Skip if embedding model not available
    embedder, err := embedsearch.NewDefaultEmbedder(embedsearch.DefaultEmbedderConfig())
    if err != nil {
        t.Skip("embedder not available: " + err.Error())
    }
    defer embedder.Close()

    semIdx := embedsearch.NewBruteForceIndex(embedsearch.SearchIndexConfig{
        Embedder: embedder,
    })

    hybridIdx, _ := hybridsearch.NewHybridIndex(hybridsearch.HybridIndexConfig{
        BleveMapping:  hybridsearch.NewToolSearchMapping(),
        SemanticIndex: semIdx,
        Fuser:         hybridsearch.DefaultToolSearchFuser(),
        QueryFunc:     hybridsearch.NewToolSearchQueryFunc(),
    })
    defer hybridIdx.Close()

    // Index tools
    tools := []hybridsearch.HybridDocument{
        {
            ID: "get_billing_ledger",
            Fields: map[string]string{
                "name": "get_billing_ledger", "name_analyzed": "get_billing_ledger",
                "keywords": "billing payment invoice ledger",
                "description": "Retrieve billing ledger entries for a customer",
                "synthetic_queries": "check payment status\nlook up invoices",
            },
            EmbedText: "get_billing_ledger: Retrieve billing ledger entries for a customer. Keywords: billing, payment, invoice, ledger",
        },
        {
            ID: "send_notification",
            Fields: map[string]string{
                "name": "send_notification", "name_analyzed": "send_notification",
                "keywords": "email sms push notification message",
                "description": "Send a notification to the customer via email, SMS, or push",
            },
            EmbedText: "send_notification: Send a notification to the customer via email, SMS, or push",
        },
    }
    for _, tool := range tools {
        hybridIdx.Add(ctx, tool)
    }

    // Test: natural language query should find billing tool
    results, _ := hybridIdx.Search(ctx, "check outstanding payments", 5)
    assert.Equal(t, "get_billing_ledger", results[0].Document.ID)

    // Test: exact name should find exact tool
    results, _ = hybridIdx.Search(ctx, "send_notification", 5)
    assert.Equal(t, "send_notification", results[0].Document.ID)
}
```

---

## Implementation Order

1. **`normalize.go`** — `minMaxNormalize()` function + tests. Pure logic, no dependencies.
2. **`fuser.go`** — `Fuser` interface + `LinearFuser` implementation + tests. Depends only on normalize.
3. **`bleve_helpers.go`** — `NewToolSearchMapping()`, `NewPolicySearchMapping()`, `NewToolSearchQueryFunc()`, `NewPolicySearchQueryFunc()`. Depends on Bleve.
4. **`hybrid_index.go`** — `HybridIndex` with `Add()`, `Search()`, `Remove()`. Wires together Bleve + embedsearch + Fuser.
5. **Integration tests** — End-to-end with real Bleve and embedsearch (skip if model not available).

---

## Key Libraries

| Library | Purpose | URL |
|---------|---------|-----|
| **Bleve v2** | BM25 full-text search | https://github.com/blevesearch/bleve |
| **embedsearch** | Semantic vector search (our package) | See SEMANTIC-SEARCH-IMPLEMENTATION.md |