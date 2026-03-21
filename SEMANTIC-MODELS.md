# Embedding models for Go-based ONNX inference: a comprehensive compatibility guide

**All 15 candidate models have pre-built ONNX files on HuggingFace, but four use CLS pooling instead of the required mean pooling — a critical incompatibility for a unified pipeline.** The best all-around choice for mixed Indonesian-English workloads remains `intfloat/multilingual-e5-small` (118M params, 384d, ~113MB INT8), while `sentence-transformers/all-MiniLM-L6-v2` at just 23MB INT8 is ideal for lightweight English-only tool search. For Indonesian-specific retrieval, `LazarusNLP/all-indo-e5-small-v4` achieves **73.2 nDCG@10 on MIRACL Indonesian** — significantly outperforming the base multilingual-e5-small. Every model listed here is compatible with the `daulet/tokenizers` Go library, which wraps HuggingFace's Rust tokenizer and supports WordPiece, BPE, and SentencePiece via standard `tokenizer.json` files.

---

## Master compatibility matrix

The table below captures every critical dimension for the Go ONNX pipeline. Models marked with ⚠️ under Pooling use **CLS pooling** natively and will produce degraded results with mean pooling.

| Model | Params | Dim | Max Seq | License | Tokenizer | Pooling | Prefixes | ONNX FP32 | ONNX INT8 | MTEB Retr nDCG@10 | token_type_ids |
|---|---|---|---|---|---|---|---|---|---|---|---|
| **intfloat/multilingual-e5-small** | 118M | 384 | 512 | MIT | SentencePiece BPE | Mean | `query: ` / `passage: ` | ~470 MB | ~113 MB | ~46.6 (EN BEIR) | ⚠️ Depends on export |
| **intfloat/multilingual-e5-base** | 278M | 768 | 512 | MIT | SentencePiece BPE | Mean | `query: ` / `passage: ` | ~1,059 MB | ~266 MB | ~48.9 (EN BEIR) | Not needed |
| **intfloat/e5-small-v2** | 33M | 384 | 512 | MIT | WordPiece | Mean | `query: ` / `passage: ` | 133 MB | ~33 MB | ~49.0 (BEIR) | Required (zeros) |
| **intfloat/e5-base-v2** | 109M | 768 | 512 | MIT | WordPiece | Mean | `query: ` / `passage: ` | ~438 MB | ~110 MB | ~50.3 (BEIR) | Required (zeros) |
| **ST/all-MiniLM-L6-v2** | 22.7M | 384 | **256** | Apache 2.0 | WordPiece | Mean | None | 90.4 MB | **23 MB** | ~41.95 | Not needed (ST export) |
| **ST/all-MiniLM-L12-v2** | 33.4M | 384 | **256** | Apache 2.0 | WordPiece | Mean | None | 133 MB | 34.1 MB | ~42.69 | Not needed (ST export) |
| **ST/paraphrase-multilingual-MiniLM-L12-v2** | 118M | 384 | **128** | Apache 2.0 | SentencePiece BPE | Mean | None | ~471 MB | ~118 MB | N/A (multilingual) | Not needed |
| **BAAI/bge-small-en-v1.5** | 33.4M | 384 | 512 | MIT | WordPiece | ⚠️ CLS | Query prefix* | ~127 MB | ~32 MB | **51.68** | Required (zeros) |
| **BAAI/bge-base-en-v1.5** | 109.5M | 768 | 512 | MIT | WordPiece | ⚠️ CLS | Query prefix* | ~416 MB | ~105 MB | **53.25** | Required (zeros) |
| **BAAI/bge-m3** | 560M | 1024 | 8192 | MIT | SentencePiece BPE | ⚠️ CLS | None | ~2,300 MB | ~543 MB | 67.8 (MIRACL avg) | Not needed |
| **nomic-ai/nomic-embed-text-v1.5** | 137M | 768† | 8192 | Apache 2.0 | WordPiece | Mean | `search_query: ` / `search_document: ` | 547 MB | 137 MB | ~53.1 | Not needed |
| **Snowflake/arctic-embed-s** | 33M | 384 | 512 | Apache 2.0 | WordPiece | ⚠️ CLS | Query prefix* | ~132 MB | ~33 MB | **51.98** | Not needed |
| **Snowflake/arctic-embed-m** | 110M | 768 | 512 | Apache 2.0 | WordPiece | ⚠️ CLS | Query prefix* | ~436 MB | ~110 MB | **54.90** | Not needed |
| **thenlper/gte-small** | 33M | 384 | 512 | MIT | WordPiece | Mean | None | 133 MB | ~33 MB | ~49.46 | Can omit |
| **thenlper/gte-base** | 110M | 768 | 512 | MIT | WordPiece | Mean | None | 436 MB | ~110 MB | 52.31 | Can omit |

*BGE query prefix: `Represent this sentence for searching relevant passages: ` — Arctic uses the same prefix for queries; no prefix on documents.
†Nomic supports Matryoshka dimensions: 768, 512, 256, 128, 64.

---

## Pooling mismatch is the biggest compatibility trap

Four model families — **BGE (small/base/M3)** and **Snowflake Arctic (s/m)** — use CLS token pooling, not mean pooling. This is the single most important filter for the pipeline. Using mean pooling on a CLS-trained model doesn't crash, but it **silently degrades retrieval quality by 5–15% nDCG@10** because the model concentrated its semantic signal in the `[CLS]` token during training, and averaging across all tokens dilutes that signal.

Models that are fully compatible with mean pooling + L2 normalization: **all E5 variants, all MiniLM/paraphrase variants, nomic-embed-text-v1.5, and both GTE models**. If you want BGE or Arctic models, the Go inference code must support a CLS pooling path (take `last_hidden_state[:, 0, :]` instead of the masked mean).

The sentence-transformers ONNX exports helpfully include a pre-computed `sentence_embedding` output tensor (mean-pooled, but **not** L2-normalized), so for all-MiniLM and paraphrase models you can skip manual pooling and just normalize the output. The Optimum/standard ONNX exports output `last_hidden_state` and require manual pooling in Go.

---

## ONNX input/output names vary by export source

This is the second major gotcha. There are two ONNX "flavors" in the wild for the same model, with different input and output tensor names:

**Sentence-transformers export** (found in official ST repos):
- Inputs: `input_ids`, `attention_mask` (no `token_type_ids`)
- Outputs: `token_embeddings` [batch, seq, dim], `sentence_embedding` [batch, dim]

**Optimum/standard export** (from `optimum-cli export onnx`):
- Inputs: `input_ids`, `attention_mask`, `token_type_ids`
- Output: `last_hidden_state` [batch, seq, dim]

For a Go tool that downloads models, the code should **inspect ONNX input names at runtime** via `onnxruntime.InferenceSession.GetInputs()` to determine which format it received. If `token_type_ids` is listed as an input, pass a zeros tensor matching `input_ids` shape. If `sentence_embedding` is available as an output, use it directly (then just L2-normalize); otherwise apply mean pooling to `last_hidden_state`.

The **multilingual-e5-small** has a uniquely tricky case: its architecture is `BertModel` but its tokenizer is `XLMRobertaTokenizer` (which doesn't produce `token_type_ids`). The pre-built ONNX in the main repo works with just `input_ids` + `attention_mask`. However, an Optimum re-export may incorrectly require `token_type_ids` — pass zeros if needed. The Teradata-exported ONNX versions are validated to work with only two inputs. The **multilingual-e5-base** uses `XLMRobertaModel` architecture and cleanly requires only `input_ids` + `attention_mask` with no ambiguity.

---

## ONNX availability and recommended sources per model

Every model has pre-built ONNX on HuggingFace, but quality varies. The best pre-built sources, verified for correctness:

**Tier 1 — Official ONNX in main repo with multiple quantization variants:**
- `sentence-transformers/all-MiniLM-L6-v2`: onnx/ folder contains FP32, FP16, INT8 (ARM64, AVX512, AVX512-VNNI), UINT8 (AVX2)
- `sentence-transformers/all-MiniLM-L12-v2`: same structure, same quantization variants
- `nomic-ai/nomic-embed-text-v1.5`: onnx/ folder with FP32, FP16, INT8, UINT8, Q4, Q4F16 variants

**Tier 2 — Official ONNX in main repo (FP32 + one quantized variant):**
- `intfloat/multilingual-e5-small`: onnx/model.onnx (470 MB) + model_O4.onnx
- `intfloat/multilingual-e5-base`: onnx/ folder with model_O4.onnx
- `intfloat/e5-small-v2`: model.onnx (133 MB) at root + onnx/model_O4.onnx
- `BAAI/bge-small-en-v1.5`: onnx/ subfolder (merged via community PR)
- `BAAI/bge-base-en-v1.5`: onnx/ subfolder
- `BAAI/bge-m3`: onnx/ folder (>2 GB, uses external data file)
- `thenlper/gte-small`: onnx/ folder with FP32 + qint8 variant
- `Snowflake/snowflake-arctic-embed-s` and `-m`: onnx/ folders with ONNX tag

**Tier 3 — Community ONNX repos (well-tested, include INT8):**
- `Teradata/multilingual-e5-small`: FP32 (449 MB), INT8 (113 MB), UINT8 (113 MB)
- `Teradata/multilingual-e5-base`: FP32 (1,059 MB), INT8 (266 MB)
- `Teradata/bge-small-en-v1.5`: FP32 (127 MB), INT8 (32 MB)
- `Teradata/bge-base-en-v1.5`: FP32 (416 MB), INT8 (105 MB)
- `Teradata/bge-m3`: INT8 (543 MB), UINT8 (543 MB)
- `Xenova/*` repos: ONNX for Transformers.js, generally reliable

For the Go tool, **Teradata repos are the best source for pre-quantized INT8 models** when the official repo lacks INT8 variants.

---

## BGE-M3 works in ONNX but only dense mode is practical

**BGE-M3 can be exported to standard ONNX**, and pre-built ONNX exists in the official repo. The standard Optimum export outputs `last_hidden_state`, from which you extract the CLS token (`output[:, 0, :]`) and L2-normalize for the dense embedding. This dense-only mode is what matters for the Go tool.

Custom ONNX exports (from `aapot/bge-m3-onnx` and `philipchung/bge-m3-onnx`) include all three retrieval modes with output tensors named `dense_vecs`, `sparse_vecs`, and `colbert_vecs`, but these require custom wrapper code and are unnecessary for a standard embedding pipeline.

The practical concerns for CPU deployment are significant. The FP32 ONNX exceeds **2.3 GB** (hitting the ONNX protobuf 2 GB limit, requiring a split `model.onnx` + `model.onnx_data`). INT8 is ~543 MB. RAM usage will be **2.5+ GB** at FP32 or ~600 MB at INT8. Inference latency on CPU is **10–50× slower** than the 33M-parameter models depending on sequence length. For a Go CLI tool, BGE-M3 is feasible only if the user explicitly requests it and has the compute budget. It uses CLS pooling, so it doesn't meet the mean pooling requirement without a dedicated code path.

---

## Nomic-embed-text-v1.5's rotary embeddings work fine in ONNX

The nomic-embed model uses a **modified BERT architecture** ("nomic-bert") with Rotary Positional Embeddings (RoPE) and SwiGLU activations — neither of which exist in standard BERT. This initially raised ONNX concerns, but **the pre-built ONNX files work correctly** because the RoPE operations are baked into the ONNX graph using standard operators. ONNX Runtime now has a native `RotaryEmbedding` operator.

The model requires `trust_remote_code=True` in Python due to custom modeling code, but this is irrelevant for ONNX inference since the custom architecture is fully captured in the exported graph. The Xenova-exported ONNX files have been validated by HuggingFace staff. The INT8 ONNX is **137 MB** — reasonable for CPU inference.

For Matryoshka dimension reduction, the post-processing pipeline in Go is: run ONNX inference → get 768d embedding → apply layer normalization → truncate to desired dimension (e.g., 256) → L2-normalize. This happens entirely outside the ONNX model.

One caveat: nomic-embed uses mean pooling and is compatible with the required pipeline, but its **137M parameters place it in the same size class as multilingual-e5-small** (118M) while being English-only. Its advantage is Matryoshka support (deploy a 256d index at ~⅓ the memory cost) and slightly better English retrieval (~53.1 vs ~46.6 BEIR nDCG@10).

---

## Indonesian retrieval: LazarusNLP models outperform base multilingual-e5

For the mixed Indonesian-English SOP/policy document search use case, there are three tiers of options:

**Indonesian-specific models (highest quality on Indonesian):**

| Model | Params | Dim | Max Seq | MIRACL-ID nDCG@10 | Tokenizer | ONNX | License |
|---|---|---|---|---|---|---|---|
| LazarusNLP/all-indo-e5-small-v4 | 118M | 384 | 128 | **73.23** | SentencePiece BPE | ✅ Available | Apache 2.0 |
| LazarusNLP/all-nusabert-base-v4 | 111M | 768 | 512 | 71.24 | WordPiece | Needs export | Apache 2.0 |
| LazarusNLP/all-indobert-base-v4 | 125M | 768 | 512 | 70.97 | WordPiece | Needs export | Apache 2.0 |

**Multilingual models (good Indonesian + English in one model):**

| Model | MIRACL-ID nDCG@10 | EN BEIR nDCG@10 | Notes |
|---|---|---|---|
| intfloat/multilingual-e5-small | ~50.7–74.8* | ~46.6 | Current default; scores vary by eval methodology |
| intfloat/multilingual-e5-base | ~51.0–75.2* | ~48.9 | 2.4× larger ONNX for marginal gain |
| BAAI/bge-m3 | Strong (part of MIRACL avg 67.8) | N/A | 560M params, CLS pooling, very large |

*The Indonesian MIRACL scores vary significantly by evaluation methodology: the mE5 paper reports ~50.7 (zero-shot, test set), while LazarusNLP's evaluation shows ~74.8 (dev set, with proper prefixes). The true production performance likely falls between these figures.*

The `LazarusNLP/all-indo-e5-small-v4` is the standout recommendation: it's fine-tuned from multilingual-e5-small on Indonesian-specific data (including MIRACL and mMARCO training sets), has **ONNX already available in its repo**, uses the same SentencePiece BPE tokenizer and architecture, and achieves 73.2 nDCG@10 on MIRACL Indonesian. Its max sequence length is only **128 tokens**, which is limiting for long policy documents — truncation or chunking is mandatory.

For the `LazarusNLP/congen-indobert-lite-base` (12M parameters, MIRACL-ID 51.01), it's impressively small but too weak on retrieval for production use.

---

## Models under 50MB INT8 for lightweight tool search

Three models fit the sub-50MB INT8 constraint while maintaining reasonable English embedding quality:

| Model | Quantized ONNX | Params | Pooling | Prefixes | License | Notes |
|---|---|---|---|---|---|---|
| **TaylorAI/bge-micro-v2** | **17.4 MB** | ~17M | Mean | None | MIT | Distilled from bge-small, used in .NET SmartComponents |
| **TaylorAI/gte-tiny** | **22.9 MB** | ~14M | Mean | None | MIT | Distilled from gte-small |
| **ST/all-MiniLM-L6-v2** | **23 MB** | 22.7M | Mean | None | Apache 2.0 | Most widely used, best ecosystem support |
| **Snowflake/arctic-embed-xs** | ~23 MB (est.) | 22M | CLS | Query prefix | Apache 2.0 | Best retrieval quality at this size, but needs ONNX export |

For pure tool search (short English descriptions, simple semantic matching), **`TaylorAI/bge-micro-v2`** at 17.4 MB is the most compelling: MIT license, mean pooling, no prefixes needed, pre-built quantized ONNX in the repo, and surprisingly strong performance for its size. `all-MiniLM-L6-v2` at 23 MB is the safe default with the broadest community validation.

---

## Detailed per-model ONNX compatibility notes

**Models with zero known ONNX issues (standard BERT, clean export):**
- `intfloat/e5-small-v2` and `e5-base-v2`: Standard BERT/MiniLM → flawless ONNX. Pass `token_type_ids` as zeros.
- `BAAI/bge-small-en-v1.5` and `bge-base-en-v1.5`: Standard BERT → clean export. CLS pooling.
- `thenlper/gte-small` and `gte-base`: Standard BERT → most straightforward models in the list. No prefixes, mean pooling, clean ONNX.
- `Snowflake/snowflake-arctic-embed-s` and `-m`: Standard BERT → validated at atol 1e-4 by HF staff. CLS pooling.
- `TaylorAI/bge-micro-v2` and `gte-tiny`: Distilled BERT → clean ONNX with pre-built quantized files.

**Models with minor caveats:**
- `sentence-transformers/all-MiniLM-L6-v2` and `-L12-v2`: **Max sequence length is 256, not 512.** The tokenizer config says 512 but the model was trained with 256. Truncate to 256 in Go or results diverge from reference.
- `sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2`: **Max sequence length is 128.** Uses SentencePiece BPE (not WordPiece like the English MiniLM siblings). ~471 MB FP32 due to 250K vocab.
- `intfloat/multilingual-e5-base`: XLMRobertaModel architecture — clean, no `token_type_ids` needed.

**Models with significant caveats:**
- `intfloat/multilingual-e5-small`: BertModel architecture + XLMRobertaTokenizer = **architecture-tokenizer mismatch**. The pre-built ONNX in the main repo and Teradata export both work with just `input_ids` + `attention_mask`. But an Optimum re-export may add `token_type_ids` as a required input (HuggingFace Optimum issue #1758). Solution: check input names at runtime, pass zeros if needed.
- `nomic-ai/nomic-embed-text-v1.5`: Non-standard "nomic-bert" architecture with RoPE + SwiGLU. **Pre-built ONNX works correctly** (RoPE baked in). But you cannot re-export this model yourself without the custom modeling code. Always use the official pre-built ONNX files.
- `BAAI/bge-m3`: >2 GB FP32 ONNX requires split files (`model.onnx` + `model.onnx_data`). Both files must be present. Dense-only extraction from `last_hidden_state` CLS token works fine. INT8 at 543 MB is the practical choice for CPU.

---

## daulet/tokenizers covers all models

The `daulet/tokenizers` Go library (v0.9.0+, MIT license) wraps HuggingFace's Rust `tokenizers` crate v0.20.0 and supports **all tokenizer types** found across these models. It loads the standard `tokenizer.json` file that every model on HuggingFace provides.

Key capabilities confirmed: **WordPiece** (BERT family — e5-v2, BGE, MiniLM, GTE, Arctic, nomic), **BPE** (GPT-2 family), and **SentencePiece/Unigram** (XLM-RoBERTa family — multilingual-e5, paraphrase-multilingual, BGE-M3, LazarusNLP indo-e5). Loading works via `tokenizers.FromFile("tokenizer.json")` or `tokenizers.FromPretrained("model-id")`. The `EncodeWithOptions` method returns `IDs`, `TypeIDs`, `AttentionMask`, and `Tokens` — everything needed for ONNX input tensor construction. Benchmarks show **~12.6μs per encode** on Apple Silicon.

There are no known incompatibilities between `daulet/tokenizers` and any of the 15+ models investigated. The only requirement is that `tokenizer.json` exists in the model repo (it does for all models listed here).

---

## Recommendations by use case

**For English tool search (lightweight, fast):** Use `sentence-transformers/all-MiniLM-L6-v2` (23 MB INT8, no prefixes, mean pooling, Apache 2.0). If even smaller is needed, `TaylorAI/bge-micro-v2` at 17.4 MB. Both use WordPiece and have zero ONNX issues.

**For mixed Indonesian-English policy/SOP search:** Use `intfloat/multilingual-e5-small` as the default (118M params, 384d, ~113 MB INT8, mean pooling, MIT). For significantly better Indonesian retrieval, add `LazarusNLP/all-indo-e5-small-v4` as an optional download — same architecture/tokenizer but tuned for Indonesian. Note the 128-token limit on LazarusNLP models requires document chunking.

**For memory representation search (English, quality matters):** Use `intfloat/e5-small-v2` (33 MB INT8, 384d, BEIR ~49.0) for size-constrained setups, or `nomic-ai/nomic-embed-text-v1.5` (137 MB INT8, Matryoshka 768→128d) for maximum flexibility. Nomic's Matryoshka support lets users trade dimension for speed/memory at deployment time without reindexing.

**Best overall default (one model for all tasks):** `intfloat/multilingual-e5-small` remains the strongest choice. It covers 100 languages including Indonesian, uses mean pooling, has pre-built ONNX with INT8 at ~113 MB, MIT license, and the E5 prefix scheme (`query: ` / `passage: `) is simple to implement. Its English retrieval (~46.6 BEIR) is weaker than English-only alternatives, but this is the inherent cost of multilingual coverage.

## Conclusion

The key engineering insight from this analysis is that **ONNX compatibility is not the real risk — pooling method mismatch is.** Every model exports to ONNX successfully, but the split between mean pooling (E5, MiniLM, GTE, nomic) and CLS pooling (BGE, Arctic) means the Go inference code must either commit to one pooling strategy and filter models accordingly, or support both with per-model configuration. Given the mean pooling requirement, the practical candidate set narrows to 10 models (excluding BGE and Arctic families).

The `token_type_ids` question resolves cleanly: sentence-transformers ONNX exports omit it entirely, while Optimum exports include it but accept all-zeros. The safest Go implementation inspects ONNX input names at load time and conditionally provides a zeros tensor. For the ONNX output side, detecting whether `sentence_embedding` (pre-pooled) or `last_hidden_state` (needs pooling) is available handles both export flavors.

For Indonesian specifically, the gap between generic multilingual models (~50.7 MIRACL-ID in the mE5 paper's zero-shot evaluation) and fine-tuned Indonesian models (73.2 from LazarusNLP) is large enough to justify offering `all-indo-e5-small-v4` as a dedicated option for users who need Indonesian retrieval quality.