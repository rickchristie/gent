# Context, Insights, and Reasoning from SEMANTIC-*.md Not in Code

This document captures knowledge from SEMANTIC-MODELS.md, SEMANTIC-SEARCH.md,
SEMANTIC-SEARCH-FUSION.md, and SEMANTIC-SEARCH-V2.md that is not yet reflected
in code comments or documentation. These files will be removed — this is the
preservation record.

---

## 1. Model Research & Compatibility (SEMANTIC-MODELS.md)

### 1.1 Pooling Mismatch is the #1 Compatibility Trap

Using mean pooling on a CLS-trained model doesn't crash, but it **silently
degrades retrieval quality by 5–15% nDCG@10** because the model concentrated
its semantic signal in the `[CLS]` token during training, and averaging across
all tokens dilutes that signal.

Four model families use CLS pooling: **BGE (small/base/M3)** and **Snowflake
Arctic (s/m)**. All E5 variants, MiniLM, paraphrase variants, nomic-embed, and
GTE models use mean pooling.

**Key insight:** ONNX compatibility is not the real risk — pooling method
mismatch is. Every model exports to ONNX successfully, but the mean/CLS split
means the inference code must either commit to one pooling strategy or support
both with per-model configuration.

### 1.2 Two ONNX Export Flavors with Different Tensor Names

**Sentence-transformers export:**
- Inputs: `input_ids`, `attention_mask` (no `token_type_ids`)
- Outputs: `token_embeddings` [batch, seq, dim], `sentence_embedding` [batch, dim]

**Optimum/standard export:**
- Inputs: `input_ids`, `attention_mask`, `token_type_ids`
- Output: `last_hidden_state` [batch, seq, dim]

For Go: inspect ONNX input names at runtime to determine which format was
received. If `sentence_embedding` is available as an output, use it directly
(pre-pooled, just L2-normalize); otherwise apply mean pooling to
`last_hidden_state`. If `token_type_ids` is listed as input, pass a zeros
tensor matching `input_ids` shape.

### 1.3 multilingual-e5-small Architecture-Tokenizer Mismatch

Its architecture is `BertModel` but its tokenizer is `XLMRobertaTokenizer`
(which doesn't produce `token_type_ids`). The pre-built ONNX works with just
`input_ids` + `attention_mask`. However, an Optimum re-export may incorrectly
require `token_type_ids` (HuggingFace Optimum issue #1758) — pass zeros if
needed.

The `multilingual-e5-base` uses `XLMRobertaModel` and cleanly requires only
`input_ids` + `attention_mask` with no ambiguity.

### 1.4 MiniLM Max Sequence Length is 256, Not 512

`all-MiniLM-L6-v2` and `-L12-v2`: the tokenizer config says 512 but the model
was trained with **256**. Truncate to 256 in Go or results diverge from
reference. The `paraphrase-multilingual-MiniLM-L12-v2` has a max of **128**.

### 1.5 Complete Model Evaluation Matrix

15 models evaluated across 12 dimensions (params, dim, max seq, license,
tokenizer type, pooling, prefixes, ONNX FP32/INT8 sizes, MTEB retrieval
nDCG@10, token_type_ids requirement):

| Model | Params | Dim | Max Seq | Pooling | INT8 Size | MTEB Retr |
|---|---|---|---|---|---|---|
| multilingual-e5-small | 118M | 384 | 512 | Mean | ~113 MB | ~46.6 EN |
| multilingual-e5-base | 278M | 768 | 512 | Mean | ~266 MB | ~48.9 EN |
| e5-small-v2 | 33M | 384 | 512 | Mean | ~33 MB | ~49.0 |
| e5-base-v2 | 109M | 768 | 512 | Mean | ~110 MB | ~50.3 |
| all-MiniLM-L6-v2 | 22.7M | 384 | **256** | Mean | **23 MB** | ~41.95 |
| all-MiniLM-L12-v2 | 33.4M | 384 | **256** | Mean | 34.1 MB | ~42.69 |
| paraphrase-multi-MiniLM-L12-v2 | 118M | 384 | **128** | Mean | ~118 MB | N/A |
| bge-small-en-v1.5 | 33.4M | 384 | 512 | **CLS** | ~32 MB | **51.68** |
| bge-base-en-v1.5 | 109.5M | 768 | 512 | **CLS** | ~105 MB | **53.25** |
| bge-m3 | 560M | 1024 | 8192 | **CLS** | ~543 MB | 67.8 avg |
| nomic-embed-text-v1.5 | 137M | 768† | 8192 | Mean | 137 MB | ~53.1 |
| arctic-embed-s | 33M | 384 | 512 | **CLS** | ~33 MB | **51.98** |
| arctic-embed-m | 110M | 768 | 512 | **CLS** | ~110 MB | **54.90** |
| gte-small | 33M | 384 | 512 | Mean | ~33 MB | ~49.46 |
| gte-base | 110M | 768 | 512 | Mean | ~110 MB | 52.31 |

†Nomic supports Matryoshka dimensions: 768, 512, 256, 128, 64.

Models under 50MB INT8 for lightweight tool search:
- **TaylorAI/bge-micro-v2**: 17.4 MB, ~17M params, mean pooling, MIT
- **TaylorAI/gte-tiny**: 22.9 MB, ~14M params, mean pooling, MIT
- **all-MiniLM-L6-v2**: 23 MB, 22.7M params, mean pooling, Apache 2.0

### 1.6 ONNX Source Tiers

- **Tier 1** (official, multiple quantization variants): sentence-transformers
  MiniLM models, nomic-embed-text-v1.5
- **Tier 2** (official, FP32 + one quantized): e5 variants, BGE, GTE, Arctic
- **Tier 3** (community, well-tested INT8): **Teradata repos are the best
  source for pre-quantized INT8 models** when official repos lack INT8

### 1.7 BGE-M3 Practical Limits for CPU

FP32 ONNX exceeds **2.3 GB** (hitting the ONNX protobuf 2 GB limit, requiring
split `model.onnx` + `model.onnx_data`). RAM usage: **2.5+ GB** at FP32, ~600
MB at INT8. Inference latency: **10–50× slower** than the 33M-parameter models.
Feasible only if the user explicitly requests it and has the compute budget. It
uses CLS pooling, so it doesn't meet the mean pooling requirement without a
dedicated code path.

### 1.8 Nomic-embed Rotary Embeddings and Matryoshka Pipeline

Nomic uses a modified BERT architecture ("nomic-bert") with Rotary Positional
Embeddings (RoPE) and SwiGLU activations. The pre-built ONNX files work
correctly because RoPE operations are baked into the ONNX graph. Requires
`trust_remote_code=True` in Python but irrelevant for ONNX inference. Always
use the official pre-built ONNX files (cannot re-export without custom modeling
code).

**Matryoshka dimension reduction pipeline (post-ONNX):**
Run ONNX inference → get 768d embedding → apply layer normalization → truncate
to desired dimension (e.g., 256) → L2-normalize. This happens entirely outside
the ONNX model. Deploying a 256d index costs ~⅓ the memory of 768d.

### 1.9 Indonesian Retrieval: LazarusNLP Models

`LazarusNLP/all-indo-e5-small-v4` achieves **73.23 nDCG@10 on MIRACL
Indonesian** — significantly outperforming base multilingual-e5-small (~50.7 in
zero-shot evaluation). Fine-tuned from multilingual-e5-small on
Indonesian-specific data (MIRACL + mMARCO training sets), same tokenizer and
architecture. Max sequence length is only **128 tokens** — chunking mandatory
for long documents.

The MIRACL-ID scores for multilingual-e5-small vary significantly by evaluation
methodology: mE5 paper reports ~50.7 (zero-shot, test set), LazarusNLP's
evaluation shows ~74.8 (dev set, with proper prefixes). True production
performance likely falls between.

### 1.10 daulet/tokenizers Universal Compatibility

The `daulet/tokenizers` Go library (v0.9.0+, MIT) wraps HuggingFace's Rust
`tokenizers` crate v0.20.0. Supports **WordPiece** (BERT family), **BPE**
(GPT-2 family), and **SentencePiece/Unigram** (XLM-RoBERTa family). Loads
standard `tokenizer.json`. Benchmarks: **~12.6μs per encode** on Apple Silicon.
No known incompatibilities with any of the 15+ models investigated.

---

## 2. Design Reasoning (SEMANTIC-SEARCH.md, SEMANTIC-SEARCH-V2.md)

### 2.1 E5 Prefix Impact Quantified

The e5 family **requires** `"query: "` and `"passage: "` prefixes. Without
them, retrieval quality degrades by **10-20% nDCG@10**. Other models differ:
- BGE: no prefix needed
- Nomic: `"search_query: "` / `"search_document: "`
- MiniLM: no prefix needed

### 2.2 Model Selection: Indonesian Scaling Curve

multilingual-e5-small scores 74.80 nDCG@10 on MIRACL-id — only **1.36 points
behind the 5x larger e5-large** (76.16). The scaling curve is remarkably flat
for Indonesian, making the small model an outsized value.

### 2.3 Library Choice: yalue/onnxruntime_go over hugot

Two options were evaluated:
- **hugot** (569 stars): complete pipeline (tokenization + ONNX + pooling),
  higher-level, uses CGo
- **yalue/onnxruntime_go** (422 stars) + **daulet/tokenizers**: lower-level,
  more control, requires implementing tokenization and mean pooling manually

We chose yalue + daulet for more control over the inference pipeline.

### 2.4 ONNX Thread Contention Guidance

Default `NumThreads: 4` works well on 2+ vCPU instances. For high-concurrency
(50+ concurrent embedding requests), set `NumThreads: 1` and let Go's goroutine
scheduler handle concurrency. Use a semaphore (buffered channel) to limit
concurrent ONNX inference calls.

### 2.5 NUMA Awareness on Multi-Socket Servers

On multi-socket EC2 instances (e.g., m5.4xlarge), ONNX Runtime may allocate
memory on one NUMA node and schedule threads on another, causing a **3x+
latency penalty**. For single-socket instances (t3, c6i.xlarge) this is not an
issue. For multi-socket: set `GOMP_CPU_AFFINITY` or `OMP_PLACES`, or pin with
`taskset`.

### 2.6 Model Warm-Up: 5-10x First-Call Latency

The first inference call after loading is **5-10x slower** than subsequent calls
due to ONNX Runtime JIT compilation and memory allocation. A warm-up embedding
during initialization prevents latency spikes on first user request.

### 2.7 Embedding is Lossy One-Way Compression

A 384-dimensional vector is 3,072 bytes representing potentially thousands of
bytes of text. There is no inverse function — you cannot reconstruct text from a
vector. This is why FlatIndex stores the original chunk text alongside each
vector to populate the Snippet field in search results.

### 2.8 Contiguous Slice Optimization (Not Yet Implemented)

For better cache locality, store vectors in a contiguous `[]float32` slice
rather than `[][]float32` (pointer chasing). Current code uses `[]storedVector`
(struct per vector). This matters at scale (100K+ vectors).

### 2.9 FusedIndex Partial Failure Recovery

If the first sub-index in a FusedIndex succeeds on Add() and the second fails,
state is inconsistent. Current mitigation: fail fast, return error. Recovery
path: `Swap()` can reset to a consistent state. This isn't documented in code.

---

## 3. Score Theory & Fusion Reasoning (SEMANTIC-SEARCH-FUSION.md)

### 3.1 BM25 Score Behavior

BM25 scores are unbounded and query-dependent. A query with rare terms against a
short document might score 28.5. A common-term query against a long document
scores 2.1. **The absolute number is meaningless across queries.** A score of
15.0 could be excellent for one query and terrible for another. You cannot
threshold on raw BM25 scores.

Versus cosine similarity: a semantic score of 0.7 means roughly the same thing
whether you're searching tools or policies. You can threshold on it directly
(`if score > 0.5` is a meaningful condition).

### 3.2 Cosine Similarity Score Interpretation Guide

- **0.9+** = near-identical meaning
- **0.5–0.7** = clearly related
- **0.2–0.35** = noise floor (shared language structure, not semantic relevance)
- **Below 0.2** = unrelated

Meaningful retrieval results typically have scores > 0.3. Scores below 0.2 are
rarely useful. Suggested threshold for strict filtering: 0.3+.

### 3.3 Zero-Score Semantics in Normalization

In per-query min-max normalization, zero scores remain zero. Zero means "no
match at all" (the term didn't appear in the document) — a fundamentally
different signal from "appeared but scored poorly." This is why min/max is
computed only over non-zero scores.

### 3.4 RRF Failure Mode — Concrete Math

RRF (Reciprocal Rank Fusion) uses `score = Σ 1/(k + rank_i)` with k=60. This
creates a specific failure mode for tool search:

- A document rank #1 in semantic (score 0.95) but **absent** from BM25 gets:
  `1/(60+1) + 0 = 0.0164`
- A document rank #5 in **both** lists gets:
  `1/(60+5) + 1/(60+5) = 0.0308` — almost **double**

RRF systematically favors "okay in both" over "perfect in one." For tool search,
this is exactly wrong — the whole point of semantic search is finding tools
where keyword matching fails. Weighted linear correctly handles the "found by
one method only" case: `0.7 × 0.95 = 0.665`, which can still beat a mediocre
dual-match.

### 3.5 Worked Fusion Examples

**Example 1: "check outstanding payments"** (natural language query)

| Tool | BM25 Raw | Semantic | BM25 Norm | Fused (0.3/0.7) |
|---|---|---|---|---|
| get_payment_history | 3.8 | 0.85 | 1.00 | **0.895** |
| get_billing_ledger | 0.0 | 0.89 | 0.00 | **0.623** |
| list_invoices | 1.2 | 0.82 | 0.00 | **0.574** |

Key: get_billing_ledger has ZERO BM25 match but ranks #2 via semantic. Pure
BM25 would completely miss it.

**Example 2: "get_billing_ledger"** (explicit tool name)

| Tool | BM25 Raw | Semantic | BM25 Norm | Fused |
|---|---|---|---|---|
| get_billing_ledger | 28.5 | 0.95 | 1.00 | **0.965** |
| get_payment_history | 6.1 | 0.72 | 0.08 | **0.528** |

10x name boost in Bleve creates massive raw BM25 (28.5) → normalizes to 1.0,
overwhelmingly first place.

**Example 3: "customer wants to leave early"** (colloquial agent reasoning)

| Tool | BM25 Raw | Semantic | BM25 Norm | Fused |
|---|---|---|---|---|
| early_termination | 2.1 | 0.91 | 1.00 | **0.937** |
| cancel_reservation | 0.0 | 0.83 | 0.00 | 0.581 |
| process_checkout | 0.0 | 0.78 | 0.00 | 0.546 |

Only one tool has any BM25 match at all ("early" appears in
"early_termination"). Semantic search finds three relevant tools.

### 3.6 Default Weight Rationale: Tool Search vs Policy Search

**Tool search (0.3 BM25 / 0.7 semantic):** Agents describe tool needs in
natural language, not registry vocabulary. Tool names are
abbreviated/domain-specific ("get_billing_ledger" vs "check payments"). Semantic
search carries most of the ranking signal.

**Policy search (0.4 BM25 / 0.6 semantic):** Policies use formal, findable
terms with higher keyword overlap. BM25 contributes more meaningfully. The 0.3
BM25 weight for tools still gives BM25 enough power to boost exact-name matches
to top (10x name boost × 0.3 weight dominates).

### 3.7 PolicySearch as Pre-Iteration Middleware (Future Design)

PolicySearch was designed to run as a pre-iteration middleware in the agent loop:

1. Extract the latest user message or agent reasoning as the query
2. Search the policy index via embedsearch
3. Inject top-3 relevant policy chunks into the context as a system-level
   section

This auto-injection approach hasn't been implemented yet. Current
implementation uses explicit policy search/get tools instead.

---

## 4. Operational Knowledge

### 4.1 Performance Characteristics

**Embedding latency** (single text, CPU):
- ~15-25ms on t3.small (no VNNI)
- 2-3x faster on c6i.large (Ice Lake with AVX-512 VNNI for INT8)

**Search latency** (384-dim, single CPU core):
- 1K vectors: < 1ms
- 10K vectors: ~5ms
- 100K vectors: ~40ms

### 4.2 Memory Profile

| Documents | Chunks (avg 3/doc) | Vector memory | With text | Total |
|---|---|---|---|---|
| 200 tools | 200 | 0.6 MB | 0.1 MB | ~1 MB |
| 500 policies | 1,500 | 4.5 MB | 3 MB | ~8 MB |
| 10K docs | 30K | 90 MB | 60 MB | ~150 MB |
| 100K docs | 300K | 900 MB | 600 MB | ~1.5 GB |

Each 384-dimensional float32 vector is 1,536 bytes.

### 4.3 Cold Start Re-Indexing Times

Re-embedding cost: ~15-20ms per document on t3.small.

| Corpus | Cold start time |
|---|---|
| 200 tools | ~4 seconds |
| 500 policies | ~10 seconds |
| 10K docs | ~3 minutes |

### 4.4 EC2 Instance Sizing

**Minimum: t3.small ($15/month)** — 2 vCPU, 2 GiB RAM. Model (INT8): ~120MB
runtime. ONNX Runtime: ~50MB. Go app + indices: ~200MB. Total: ~500MB, leaving
1.5GB headroom. Single-query latency: ~15-25ms.

**Recommended: c6i.large ($60/month)** — 2 vCPU (Ice Lake with AVX-512 VNNI),
4 GiB RAM. INT8 inference 2-3x faster than t3.

### 4.5 ONNX Model Preparation Recipes

**CLI approach:**
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

**Python approach:**
```python
from optimum.onnxruntime import ORTModelForFeatureExtraction, ORTQuantizer
from optimum.onnxruntime.configuration import AutoQuantizationConfig

model = ORTModelForFeatureExtraction.from_pretrained(
    "intfloat/multilingual-e5-small", export=True
)
model.save_pretrained("./e5-small-onnx/")

quantizer = ORTQuantizer.from_pretrained("./e5-small-onnx/")
qconfig = AutoQuantizationConfig.avx512_vnni(is_static=False)
quantizer.quantize(
    save_dir="./e5-small-onnx-int8/",
    quantization_config=qconfig,
)
```

**Output files:** model_quantized.onnx (~120MB), tokenizer.json,
special_tokens_map.json, tokenizer_config.json.

### 4.6 Model Distribution Options

- **Option A: go:embed** — simplest, increases binary by ~120MB. Every binary
  that imports the package includes weights even if unused.
- **Option B: Lazy download on first use** — check cache dir
  (`~/.cache/embedsearch/`), download if missing with SHA256 verification. What
  Ollama/llama.cpp do.
- **Option C: go:embed behind build tag** — `//go:build embed_model` for
  self-contained binary, lazy download otherwise.

Recommendation: start with Option A, add Option B if binary size becomes a
concern.

### 4.7 Key Library Links

| Library | Purpose |
|---|---|
| hugot | Go ONNX transformer pipelines (alternative to yalue) |
| yalue/onnxruntime_go | Low-level ONNX Runtime Go bindings (our choice) |
| daulet/tokenizers | Go bindings for HuggingFace Rust tokenizer |
| chromem-go | In-memory vector DB with brute-force search (reference) |
| Bleve v2 | BM25 full-text search |
| HuggingFace Optimum | ONNX export + INT8 quantization |
| LazarusNLP benchmarks | Indonesian retrieval numbers (MIRACL-id, TyDiQA-id) |

Hub URLs:
- https://github.com/knights-analytics/hugot
- https://github.com/yalue/onnxruntime_go
- https://github.com/daulet/tokenizers
- https://github.com/philippgille/chromem-go
- https://github.com/blevesearch/bleve
- https://huggingface.co/docs/optimum/en/onnxruntime/usage_guides/quantization
- https://github.com/LazarusNLP/indonesian-sentence-embeddings
