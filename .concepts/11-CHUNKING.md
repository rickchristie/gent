# Ship recursive splitting at 512 tokens with zero overlap

**Recursive character text splitting at ~512 tokens with no overlap is the empirically strongest default for a general-purpose chunking library.** This conclusion draws from the largest systematic evaluations published in 2024–2025: the Vectara study (NAACL 2025, 25 configurations × 48 embedding models), the Chroma technical report (July 2024), and the "Chunk Twice, Embed Once" paper (arXiv:2506.17277, 1,080 configurations). Semantic chunking, despite its intuitive appeal, fails to justify its computational cost in head-to-head benchmarks. The overlap question has a clearer answer than most practitioners assume — boundary-aware splitting eliminates most of the need for it. What follows is the complete evidence base behind these recommendations, plus the specific Go library architecture you should build.

---

## The complete taxonomy of chunking strategies

Eleven distinct strategies exist in the literature today, spanning a spectrum from zero-intelligence splitting to full LLM-guided segmentation. Understanding each one matters because no single strategy dominates across all domains — the right default is the one that captures 80% of the benefit across the widest range of documents.

**Fixed-size token chunking** splits text into equal-length segments by character or token count. It is the simplest approach and a reasonable baseline. NVIDIA's 2024 benchmark found token-based approaches scored **0.603–0.645 accuracy** across five datasets. The strategy ignores all structural and semantic boundaries, which means it routinely cuts mid-sentence, producing incoherent chunks that degrade embedding quality.

**Sentence-boundary chunking** uses NLP sentence segmentation (regex, NLTK, spaCy) to split at sentence ends, then groups N consecutive sentences per chunk. Superlinked's 2024 HotpotQA tests showed SentenceSplitter actually **outperformed semantic chunking** when paired with ColBERT v2. The approach preserves complete thoughts at minimal compute cost. LlamaIndex defaults to this strategy with **1,024 tokens and 200-token overlap**.

**Recursive character text splitting** — LangChain's `RecursiveCharacterTextSplitter` — uses a hierarchy of separators (`\n\n` → `\n` → `. ` → ` ` → `""`) and recursively falls through them until chunks reach the target size. This preserves paragraphs first, then sentences, then words. Multiple practitioner guides call it **the recommended default for ~80% of RAG applications**. Chroma found it achieves **88.1–89.5% recall** at 400 tokens with text-embedding-3-large. LangChain defaults to **4,000 characters with 200-character overlap**.

**Semantic chunking** embeds every sentence, computes cosine similarity between consecutive sentence embeddings, and splits where similarity drops below a threshold. Greg Kamradt popularized this as "Level 4" in his five-level framework. Chroma's ClusterSemanticChunker hit **91.3% recall** — the highest among non-LLM methods — but this comes at **5–10× the ingestion compute** cost. The Vectara NAACL 2025 study concluded definitively: "The computational costs associated with semantic chunking are not justified by consistent performance gains." FloTorch's 2026 benchmark found semantic chunking produced fragments averaging only **43 tokens**, scoring just **54% end-to-end accuracy** versus recursive splitting's **69%**.

**Document structure-aware chunking** parses format-specific elements — Markdown headings, HTML tags, PDF layout. Chunks inherit metadata (parent heading hierarchy, section titles), enabling hierarchical filtering. Docling (IBM) and Unstructured.io lead here, with Unstructured's `by_title` strategy preserving section boundaries with a default `max_characters=500`. This works excellently for well-structured documents but fails on unformatted text.

**Late chunking**, introduced by Jina AI in September 2024, inverts the pipeline: encode the full document through a long-context transformer (8,192 tokens), then mean-pool token embeddings per chunk boundary. Each chunk embedding retains full document context — resolving anaphoric references like "the city" → "Berlin" across chunks. The approach showed **+3.63% relative nDCG@10 improvement** over naive chunking on BeIR datasets. Critically, **overlap becomes unnecessary with late chunking** because every chunk already sees the full document. Available via `late_chunking=True` in Jina's API. The "Beyond Chunk-Then-Embed" paper (arXiv:2602.16974, February 2025) found it improves in-corpus retrieval but degrades in-document (needle-in-haystack) retrieval.

**Agentic/LLM-based chunking** uses an LLM to identify semantic breakpoints. LumberChunker (EMNLP 2024 Findings) feeds paragraph groups to an LLM to find topic shifts, achieving **+7.37% DCG@20** over the best baseline. Chroma's LLMSemanticChunker reached the highest recall (**91.9%**) — but at extreme cost and latency. TopoChunker (2025) and AutoChunker (2025) push this further with multi-agent architectures. These are viable only for high-value documents where retrieval quality justifies the per-document LLM cost.

**Proposition-based chunking** from the Dense X Retrieval paper (EMNLP 2024) decomposes passages into atomic, self-contained factual statements. "Berlin is the capital of Germany" becomes a separate retrieval unit from "Berlin has over 3.85 million inhabitants." This maximizes information density per retrieved token — **Recall@20 improved +10.1%** over passage-level retrieval. But propositions average only ~10–20 tokens and lose broader context, making them best paired with parent-child retrieval.

**Parent-child chunking** creates small child chunks (100–400 tokens) for precise vector matching, linked to larger parent chunks (500–2,000 tokens) returned for LLM generation. This decouples retrieval precision from generation context. LlamaIndex's `HierarchicalNodeParser` supports three levels (2,048 → 512 → 128 tokens). Dify v0.15.0 and RAGFlow both ship this as a production feature.

---

## What the benchmarks actually show

The 2024–2025 period produced the first rigorous, large-scale empirical comparisons of chunking strategies. The results converge on several clear conclusions, though they also reveal important nuances by domain and query type.

**The Vectara study** (NAACL 2025, arXiv:2410.13070) is the most authoritative peer-reviewed evaluation: 25 chunking configurations tested with 48 embedding models across document retrieval, evidence retrieval, and answer generation tasks. Its central finding is that **chunking configuration had comparable or greater influence on retrieval quality than embedding model choice** — a tenfold variation in IoU across strategies. Fixed-size chunking consistently outperformed semantic chunking. The paper explicitly concludes that semantic chunking's overhead is "not justified."

**NVIDIA's 2024 benchmark** tested seven strategies across five datasets (FinanceBench, DigitalCorpora767, Earnings, KG-RAG, RAGBattlePacket). Page-level chunking won overall at **0.648 accuracy** with the lowest variance (0.107). Token-based approaches scored 0.603–0.645. The most actionable finding: **factoid queries perform best with 256–512 tokens, analytical queries need 1,024+ tokens**. Performance dropped with 2,048-token chunks on most datasets. The **15% overlap** was optimal (tested against 10% and 20%) with 1,024-token chunks.

**Chroma's technical report** (July 2024) introduced a token-level IoU metric and tested across clean and messy text corpora. RecursiveCharacterTextSplitter at 200 tokens with **no overlap** performed "consistently high across ALL metrics." OpenAI Assistants' default (800 tokens, 400 overlap — a 50% ratio) scored "below-average recall and the lowest scores across all other metrics." This directly contradicts the common assumption that more overlap is better.

The **"Chunk Twice, Embed Once" paper** (arXiv:2506.17277, June 2025) tested 25 chunking configurations × 48 embedding models in the chemistry domain — 1,080 unique configurations. **Recursive token-based chunking at 100 tokens with zero overlap (RT100-0)** was the consistently best strategy. High-overlap fixed-span chunking degraded precision without significant recall gains. The recursive family showed a clear lead with mean IoU@10 of 0.068.

The **Fraunhofer IAIS study** (arXiv:2505.21700, 2025) tested across NarrativeQA, NQ, NewsQA, COVID-QA, TechQA, and SQuAD with multiple embedding models. Smaller chunks (**64–128 tokens**) proved optimal for datasets with concise fact-based answers. Larger chunks (**512–1,024 tokens**) improved retrieval for datasets requiring broader context. Crucially, embedding models showed distinct sensitivities: Stella benefits from larger chunks (global context), while Snowflake performs better with smaller chunks (entity-based matching).

### Optimal chunk size: the evidence-based range

There is no single optimal chunk size, but there is a clear evidence-based range:

- **Factoid/fact-based queries**: 128–512 tokens
- **Analytical/multi-hop queries**: 512–1,024 tokens
- **General-purpose default**: **256–512 tokens** (the consensus safe zone)
- **Context cliff**: Quality drops sharply beyond ~2,500 tokens

The practical starting point recommended across Microsoft Azure (**512 tokens, 25% overlap**), Arize AI (**300–500 tokens**), and PremAI (**512 tokens, 50–100 token overlap**) converges on this range. For a library default, **512 tokens (~2,000 characters in English)** sits at the center of the evidence.

---

## The overlap question has a clearer answer than you think

Overlap is the most debated parameter in chunking configuration, but the empirical evidence is more decisive than the discourse suggests. **With boundary-aware splitting, overlap provides minimal benefit and introduces real costs.**

The Chroma report found RecursiveCharacterTextSplitter at 200 tokens with **zero overlap** outperformed many strategies that used overlap, including OpenAI's 50%-overlap default. The "Chunk Twice, Embed Once" paper's top performer was RT100-0 — **zero overlap**. Their explicit recommendation: "Avoid high-overlap fixed-span chunking, which degrades precision without significant recall gains." NVIDIA found 15% overlap optimal when using fixed-size token splitting at 1,024 tokens — but this was content-independent splitting where overlap acts as a bandaid for mid-sentence cuts.

**The index size cost is roughly linear**: 20% overlap produces ~25% more chunks; 50% overlap doubles the index; 60% overlap creates ~2.5× more vectors. More vectors means higher embedding API costs, more RAM for indexes, increased retrieval latency, and critically — **near-duplicate vectors that flood top-k results**. OptyxStack (March 2026) documented this failure mode explicitly: "Top-10 is filled with near-identical chunks from one section" when overlap is too high, destroying document diversity in retrieval results.

The key insight is that **overlap and boundary-aware splitting solve the same problem** — information loss at chunk boundaries. If your splitting already respects sentence boundaries, there is no mid-sentence cut to bridge. The primary remaining case for overlap is when critical information spans a paragraph boundary, but this is rare enough that parent-child retrieval handles it better than overlap.

For your Go library: **default to zero overlap**. Make it configurable (0–20% range). Document that overlap is most useful with naive fixed-size splitting and provides diminishing returns with recursive/sentence-boundary splitting. With late chunking (if users adopt Jina-style approaches), overlap is provably unnecessary — the Jina paper showed "overlapping chunks generally neither improves nor harms retrieval performance with late chunking."

---

## Language-specific chunking is real but manageable

Different languages do need different treatment, though the core recursive strategy remains universal. The differences manifest primarily in sentence boundary detection, tokenization ratios, and chunk sizing.

**Indonesian (Bahasa Indonesia)** presents specific challenges. Sentence boundary detection is under-researched — the best system (Bi-LSTM) achieves **F1 of 98.49%** on Indonesian news, while the rule-based SKBI system uses 34 rules for **F1 of 96.89%**. Key ambiguities include titles (H., Ir., Dr., Prof.), currency formatting (Rp. 100.000,-), and Indonesian's use of periods as thousand separators (opposite to English convention). Code-switching between Indonesian and English is extremely common in informal text and social media, making consistent sentence detection harder. However, Indonesian is relatively tokenizer-friendly — Latin script, SVO word order, spaces between words. With BPE tokenizers, Indonesian text tokenizes at similar ratios to other Latin-script languages. A practical approach: maintain an abbreviation exception list and use multilingual embedding models (BGE-M3, Cohere embed-v3/v4).

**CJK languages** require fundamentally different handling. Token-to-text ratios vary dramatically: Mandarin Chinese uses **1.76×** the tokens of equivalent English text, Japanese **2.12×**, Korean **2.36×**. A single CJK character like 猫 becomes 3 tokens in cl100k_base. Character-based chunk sizing breaks down entirely — 500 Chinese characters contain far fewer tokens than 500 English characters. Chinese and Japanese don't use spaces between words and use different sentence-ending punctuation (`。` not `.`). Microsoft's recommended solution is a hybrid splitter using **token length** (not characters) with CJK-specific sentence boundary markers (`。！？`). For a Go library, the critical requirement is to always use token-based or rune-based sizing, not byte-based, and support configurable sentence-ending characters.

**Morphologically rich languages** (Turkish, Finnish, Hungarian) carry more information per token — a single Turkish word can encode an entire English phrase through agglutination. Cross-lingual embeddings perform poorly: English-Finnish drops to **28% accuracy** versus English-Spanish at 83%. The practical implication is that **chunk sizes in tokens can be slightly smaller** for these languages since each token carries more semantic weight.

For a Go library shipping across languages, the viable approach is:

- Use `rivo/uniseg` (Go's UAX #29 implementation) as the default sentence boundary detector — it handles Unicode correctly across all scripts without language-specific training data
- Make sentence-ending characters configurable (defaulting to `.!?。！？`)
- Size chunks by rune count or pluggable token counting, never by byte count
- Optionally support `neurosnap/sentences` (NLTK punkt port, 17 languages) for higher-accuracy sentence detection

---

## Model-specific interactions shape the optimal chunk size

Embedding model architecture creates important but often overlooked interactions with chunk sizing. While there is no direct mathematical relationship between embedding dimensions and optimal chunk size, practical sweet spots emerge from the information bottleneck principle: longer chunks compress more information into a fixed-dimension vector.

**By model dimension:**
- **384d models** (MiniLM, bge-small): Best with **128–256 token** chunks — sentence-level content
- **768d models** (BERT-base, bge-base, E5-base): Sweet spot at **256–512 tokens**
- **1,024d models** (Jina v3, Cohere v3): Good across **256–1,024 tokens**
- **1,536–3,072d models** (OpenAI v3 models): Handle **512–1,000 tokens** well; Microsoft found 1,024 dimensions is the "sweet spot" for text-embedding-3-large (nearly identical to 3,072d at one-third storage)

**Long-context models don't eliminate the need for chunking.** Jina's research showed **+24.47% average improvement** from chunking at 512 tokens versus embedding full documents with their 8,192-token model. The "lost in the middle" problem affects embedding models too — information buried in long inputs gets diluted. Even with 8,192-token models, the optimal chunk size for retrieval remains **~256–512 tokens**. The long context window is best exploited through late chunking (full document context, per-chunk pooling) rather than simply embedding longer text.

**Cohere embed-v3 imposes a hard 512-token limit**, forcing chunks to ≤512 tokens regardless of preference. Cohere embed-v4 expanded to 128K tokens but still recommends smaller chunks for RAG. Voyage AI's `voyage-context-3` is notable: it produces contextualized chunk embeddings with only **2.06% variance across chunk sizes** (versus 4.34% for voyage-3-large), making it the least sensitive to chunk size among current models.

---

## What production systems actually use

Framework defaults reveal what the industry has converged on through large-scale usage feedback:

| System | Default Strategy | Default Size | Overlap |
|--------|-----------------|-------------|---------|
| LangChain | RecursiveCharacterTextSplitter | 4,000 chars | 200 chars |
| LlamaIndex | SentenceSplitter | 1,024 tokens | 200 tokens |
| Haystack | DocumentSplitter (by word) | 150 words | Configurable |
| Unstructured.io | Element-based (by_title) | 500 chars | Configurable |
| Microsoft Azure | Token-based | 512 tokens | 128 tokens (25%) |
| Pinecone | Fixed-size (recommended start) | Match model window | 10–20% |
| Weaviate | Pre-chunking recommended | 150 words | 25 words |

**Anthropic's contextual retrieval** (September 2024) does not prescribe a specific chunk size but uses **~800-token chunks** in their experiments. Their innovation is orthogonal to chunking strategy: prepend LLM-generated context (50–100 tokens) to each chunk before embedding, yielding a **35% reduction in retrieval failure** with embeddings alone, **67% with reranking** added. Cost: ~$1.02 per million document tokens with prompt caching. For knowledge bases under ~200K tokens (~500 pages), Anthropic recommends skipping RAG entirely and putting the full corpus in the prompt.

**OpenAI has no official chunking recommendation.** Their Assistants file search defaults to 800 tokens with 400 overlap — which Chroma's benchmarks showed is a poor default. Community practice centers on ~1,000 tokens with 20% overlap using RecursiveCharacterTextSplitter with tiktoken.

**Jina AI** recommends late chunking with 128–512 token boundaries and explicitly states **no overlap is needed** when using late chunking. Their approach is the most promising path to eliminating the entire overlap question.

---

## What to build in Go

Based on the full evidence base, here is the architecture for a Go chunking library that ships one excellent default while allowing configurability where it matters.

**The default strategy should be recursive text splitting** with a separator hierarchy of `["\n\n", "\n", ". ", "! ", "? ", " "]`. This matches the approach that won or placed highly in every benchmark. Default chunk size should be **~2,000 characters (~500 tokens)** with **zero overlap**. Size measurement should use character/rune count by default (model-agnostic, zero dependencies) with a pluggable `LenFunc` interface for exact token counting.

**Sentence boundary detection** should use `github.com/rivo/uniseg` (UAX #29 implementation) — it is standards-compliant, zero external dependencies, supports all Unicode scripts, and handles common abbreviation cases. For users needing higher accuracy, expose an interface for plugging in `github.com/neurosnap/sentences` (NLTK punkt port, 17 languages including Turkish, but not Indonesian).

**Token counting** should default to the `len(text)/4` approximation for English (well-known heuristic: ~4 characters per BPE token). For exact counting, `github.com/tiktoken-go/tokenizer` provides a pure-Go port of OpenAI's tokenizer supporting cl100k_base and o200k_base, though it adds ~4MB to binary size. Ship this as an optional adapter package, not a core dependency.

The recommended Go architecture:

```go
type Chunker interface {
    Chunk(text string) []Chunk
}

type Chunk struct {
    Text     string
    Start    int              // byte offset in original
    End      int              // byte offset in original
    Index    int              // sequence number
    Metadata map[string]string
}

type Config struct {
    ChunkSize    int              // default: 2000 (chars) ≈ 500 tokens
    ChunkOverlap int              // default: 0
    Separators   []string         // default: ["\n\n", "\n", ". ", "! ", "? ", " "]
    LenFunc      func(string) int // default: utf8.RuneCountInString
    TrimSpace    bool             // default: true
}
```

**Ship two strategies**: (1) recursive text splitting as the default, and (2) Markdown-aware splitting as the first optional strategy (split on headings, preserve code blocks and tables). The `langchaingo/textsplitter` package already implements both, though building your own gives you control over the dependency tree.

**What not to build**: semantic chunking (requires an embedding model at ingestion — wrong layer for a chunking library), LLM-based chunking (requires API calls), and late chunking (requires model inference). These belong in the RAG pipeline layer above the chunking library.

**For CJK support**, ensure chunk sizing counts runes, not bytes. Add configurable sentence-ending characters defaulting to `.!?。！？`. For Indonesian, the recursive splitting approach works well — the main addition is an abbreviation exception list (Dr., Ir., H., Rp., Prof., M.) to prevent false sentence breaks.

---
# Optimal Token Chunks

**`MaxTokenChunks` on ModelInfo** is straightforward — it's the model's hard ceiling, same value as `MaxSeqLen` on ModelConfig. Having it on ModelInfo makes it visible in the CLI list without needing to look up configs.

**`OptimalChunkTokens` on ModelConfig** requires more nuance. The research doesn't give exact per-model numbers, but the pattern is clear: smaller-capacity models (fewer params, 384d) work better with shorter chunks, and 768d models handle ~512 tokens well. The universal safe default is 512, but we can do better per-config.

Here's the table:

| Config Name | Dims | Max Seq | Params | OptimalChunkTokens | Reasoning |
|---|---|---|---|---|---|
| multilingual-e5-small | 384 | 512 | 118M | **384** | 384d but larger model (118M) handles more context than 33M peers |
| multilingual-e5-base | 768 | 512 | 278M | **512** | 768d, large model, full capacity |
| e5-small-v2 | 384 | 512 | 33M | **256** | 384d, small model (33M), less compression capacity |
| all-MiniLM-L6-v2 | 384 | **256** | 22.7M | **200** | Hard ceiling is 256; leave room for prefix tokens and padding |
| bge-small-en-v1.5 | 384 | 512 | 33.4M | **256** | 384d, small model; also BGE's long query prefix eats ~15 tokens |
| bge-base-en-v1.5 | 768 | 512 | 109.5M | **512** | 768d, base model, full capacity |
| nomic-v1.5-768d | 768 | 8192 | 137M | **512** | Despite 8192 max, retrieval quality peaks at ~512 |
| nomic-v1.5-384d | 384 | 8192 | 137M | **384** | Truncated dims mean less capacity per chunk; same model though |
| snowflake-arctic-embed-s | 384 | 512 | 33M | **256** | 384d, small; Fraunhofer found Snowflake prefers smaller chunks |
| snowflake-arctic-embed-m | 768 | 512 | 110M | **512** | 768d, base model |
| bge-micro-v2 | 384 | 512 | 17M | **200** | Smallest model, least compression capacity |

And for ModelInfo:

| Model Name | MaxTokenChunks |
|---|---|
| multilingual-e5-small | 512 |
| multilingual-e5-base | 512 |
| e5-small-v2 | 512 |
| all-MiniLM-L6-v2 | 256 |
| bge-small-en-v1.5 | 512 |
| bge-base-en-v1.5 | 512 |
| nomic-embed-text-v1.5 | 8192 |
| snowflake-arctic-embed-s | 512 |
| snowflake-arctic-embed-m | 512 |
| bge-micro-v2 | 512 |

One important caveat: these `OptimalChunkTokens` values are educated recommendations, not measured optima from per-model benchmarks. The Fraunhofer study tested a few models specifically, but most of these models weren't individually benchmarked at different chunk sizes. The values follow the pattern `384d + small params → 200-256, 384d + large params → 384, 768d → 512`, which is the strongest signal from the research. Users should treat them as good starting points, not gospel.

The `OptimalChunkTokens` would be used by the `ChunkAdapter` as a default when the user doesn't specify a chunk size — it picks the value from the model config. This way users get per-model-tuned chunking out of the box without thinking about it.

---

## Conclusion: evidence-based defaults for maximum impact

The chunking literature has matured significantly in 2024–2025, moving from anecdotal recommendations to rigorous benchmarks. Three findings stand above the noise. First, **recursive text splitting is the Pareto-optimal default** — it captures most of the benefit of structure-aware splitting at minimal complexity, outperforming both naive fixed-size and expensive semantic approaches in end-to-end evaluations. Second, **overlap is overrated** when you split at natural boundaries; zero overlap with recursive splitting matches or beats 10–20% overlap with naive splitting, while avoiding the near-duplicate vector problem that plagues high-overlap configurations. Third, **chunk size matters as much as embedding model choice**, and the safe default range is 256–512 tokens, with 512 being the best single number for mixed query types.

For your Go library, the highest-leverage decisions are: ship recursive splitting as the sole default, size by runes at ~2,000 characters, default overlap to zero, use `rivo/uniseg` for Unicode-correct boundaries, and expose `LenFunc` and `Separators` as the two configuration knobs that actually matter. This gives users 80%+ of the theoretical maximum retrieval quality with zero configuration, while preserving escape hatches for the 20% of cases where domain-specific tuning pays off.