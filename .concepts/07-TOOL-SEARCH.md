# Tool search tools: the definitive guide for LLM agent frameworks

**Dynamic tool search—retrieving relevant tools from large catalogs on demand rather than loading all tools into context—has emerged as the critical scaling bottleneck for production LLM agents.** Anthropic's Tool Search Tool demonstrates the stakes: switching from static tool loading to on-demand discovery cuts token usage by **85%** and lifts accuracy from 49% to 74% on real-world evaluations. The field has progressed rapidly from simple embedding similarity (2023) through hierarchical LLM-based search (2024) to active agent-driven tool discovery and generative retrieval (2025). This report synthesizes the complete landscape—academic foundations, production implementations, frontier methods, and concrete architectural patterns—for building a state-of-the-art `ToolSearchToolChain` in the Go-based Gent framework.

---

## The scaling wall: why tool search matters now

Every major LLM provider faces the same fundamental constraint. OpenAI enforces a **hard limit of 128 tools** per API call, with each tool definition consuming 96–500+ tokens. The Berkeley Function Calling Leaderboard's hardest test uses only 37 functions. Community reports and benchmarks consistently show **accuracy degrading significantly beyond 20–30 tools** with naive "send everything" approaches. Cursor caps MCP tools at 40. VS Code caps at 128 per chat request. These aren't arbitrary limits—they reflect genuine model performance degradation.

The Model Context Protocol (MCP), now adopted by OpenAI, Anthropic, Google, and Microsoft and donated to the Linux Foundation, amplifies this problem. A typical enterprise setup with 5 MCP servers (GitHub, Slack, Sentry, Grafana, Splunk) exposes 58 tools consuming **55,000 tokens** before any conversation begins. The math is simple: as tool ecosystems grow, static tool loading becomes untenable. Tool search isn't an optimization—it's an architectural necessity.

Among production frameworks, only two have first-class tool search: **Anthropic's Claude API** (server-side Tool Search Tool with regex/BM25) and **LlamaIndex** (client-side ObjectIndex with vector retrieval). LangChain offers middleware-based filtering but no true search. Semantic Kernel provides filter hooks but expects developers to build their own routing. AutoGen, CrewAI, DSPy, and Haystack have no tool search capabilities at all, relying entirely on the LLM selecting from statically-registered tool lists.

---

## Academic foundations: seven paradigms for tool retrieval

Research has established seven distinct paradigms for tool retrieval, each with different tradeoffs for production use.

**Embedding-based dense retrieval** is the foundational approach. ToolLLM (ICLR 2024 Spotlight) trained a neural API retriever on sentence-BERT embeddings over 16,464 real-world APIs from RapidAPI Hub, establishing the dominant benchmark. Gorilla (NeurIPS 2024) introduced **Retriever-Aware Training (RAT)**, where the LLM is fine-tuned with potentially incorrect retrieved documentation but correct ground-truth API calls, teaching it to judge retriever quality at inference time. This insight—that the LLM should be aware of retrieval imperfections—remains relevant for any pipeline approach.

**Hierarchical LLM-based search** was a breakthrough. AnyTool (ICML 2024) introduced a three-tier architecture: a meta-agent dynamically creates category agents, each identifying relevant tools from their domain, using GPT-4's function-calling feature directly for retrieval. AnyTool outperformed ToolLLM's neural retriever by **+35.4% average pass rate**, demonstrating that LLM-native hierarchical search dramatically outperforms embedding methods at scale with 16,000+ APIs—without any training.

**Tool-as-token approaches** represent a different philosophy. ToolkenGPT (NeurIPS 2023) represents each tool as a special token ("toolken") with a learned embedding inserted into the LLM's vocabulary head. During generation, the LLM predicts toolkens alongside regular tokens, implicitly handling tool selection through next-token prediction. ToolGen (2024) extends this to **generative retrieval**: each of 47,000+ tools maps to a unique virtual token, and a three-stage training process (tool memorization → retrieval training → agent training) unifies tool retrieval and execution into a single generative step, eliminating the separate retrieval pipeline entirely.

**Tool creation and curation** provides a complementary angle. CRAFT (ICLR 2024) creates task-specific toolsets by prompting GPT-4 to solve training examples, abstracting solutions into reusable code snippets, and building an embedding-based retrieval index. The key insight: retrieval quality improves dramatically when tools are curated and specialized rather than drawn from a generic catalog.

Two benchmarks specifically evaluate tool retrieval. API-Bank (EMNLP 2023) includes a "ToolSearcher" API that agents must use to find relevant tools by keyword, directly testing retrieval ability. MetaTool (ICLR 2024) evaluates whether LLMs can reliably select correct tools, concluding that **tool developers should rewrite descriptions using models appropriate for the downstream LLM**—a finding with immediate production implications.

---

## Production implementations across the industry

### Anthropic's Tool Search Tool: the gold standard

Anthropic's implementation, released November 2025, is the most mature production solution. Tools are registered with `defer_loading: true`, so Claude initially sees only the Tool Search Tool itself (~500 tokens) plus any non-deferred tools. When Claude needs capabilities, it invokes the Tool Search Tool, which offers two built-in search variants: **regex-based** (Claude constructs Python regex patterns) and **BM25-based** (natural language queries). Results return as `tool_reference` content blocks that automatically expand into full definitions.

The performance data is compelling: **85% token reduction** (from ~77K to ~8.7K tokens for 50+ tools), with Opus 4 accuracy jumping from 49% → 74% and Opus 4.5 from 79.5% → 88.1%. Critically, custom implementations are supported—developers can implement their own client-side search using embeddings and return `tool_reference` blocks, making this pattern extensible. Anthropic also introduced **Programmatic Tool Calling**, where Claude writes Python code to orchestrate tool calls, keeping intermediate results out of context and yielding an additional **37% token reduction**.

### LlamaIndex's ObjectIndex: best open-source approach

LlamaIndex treats tool objects as indexable entities. The `ObjectIndex` class wraps any LlamaIndex index (VectorStoreIndex, SummaryIndex) around tool objects via `SimpleObjectNodeMapping`. Tool descriptions are embedded and stored in a vector index, enabling semantic retrieval at query time. A `ToolRetrieverRouterQueryEngine` dynamically routes to retrieved tools. This is the most complete open-source "tool search" implementation, though it requires careful handling of object serialization for persistence.

### Google's Agent Development Kit: hierarchical delegation

Google's ADK takes a multi-agent approach. Parent agents route to specialized sub-agents based on their description fields. An AutoFlow mechanism automatically transfers execution. The AgentTool pattern wraps agents as tools, enabling a coordinator agent to reason about using multiple specialists. This mirrors AnyTool's hierarchical approach but is implemented as an engineering framework rather than a research system.

### What most frameworks actually do

The honest picture: **most frameworks punt on tool search**. LangChain's `create_retriever_tool` creates a tool that searches documents, not tools—it's for RAG, not tool discovery. LangGraph offers middleware-based tool filtering, but all tools must be registered at `create_agent()` time. Semantic Kernel provides `FunctionChoiceBehavior` and invocation filters that can subset registered tools, but the routing logic is left entirely to developers. DSPy's contribution is orthogonal—rather than reducing tool count, it optimizes prompts to improve tool selection accuracy, showing ReAct scores improving from 24% to 51% after optimization.

---

## Cutting-edge methods pushing the frontier in 2024–2025

The most significant advances cluster around four themes: active tool discovery, enhanced retrieval representations, graph-based methods, and RL-trained tool selection.

### MCP-Zero and the active discovery paradigm

MCP-Zero (June 2025) represents a paradigm shift from passive tool injection to **active tool discovery**. Instead of embedding-based matching, the agent autonomously identifies capability gaps and generates structured tool requests on-demand. Three mechanisms work together: active tool request (agent articulates what it needs), hierarchical semantic routing (server → tool two-stage matching), and iterative capability extension. Results are dramatic: accurate selection from ~3,000 candidates across 248K tokens, with **98% reduction in token consumption** on API-Bank and consistent multi-turn performance. This agent-driven approach scales naturally as tool catalogs grow because the agent only discovers what it needs.

### Toolshed and ScaleMCP: production-grade enhanced retrieval

Toolshed (ICAART 2025) introduces "Toolshed Knowledge Bases"—vector databases storing enriched tool representations that go far beyond raw descriptions. Each tool document includes the tool name, description, argument schema, synthetic "reverse-HyDE" questions (generated by asking "what queries would a user ask that would need this tool?"), and key topics/intents. This enrichment alone achieves **46%, 56%, and 47% absolute improvements** on three benchmarks for Recall@5—without any model fine-tuning. ScaleMCP (May 2025) extends this with auto-synchronizing CRUD pipelines against MCP servers and a novel **Tool Document Weighted Average (TDWA) embedding** that dynamically weights component embeddings (name, description, params, synthetic queries) rather than naively concatenating text. On a 5,000 MCP server dataset, ScaleMCP achieves Recall@5 of ~0.94.

### Re-Invoke: unsupervised query enrichment

Re-Invoke (EMNLP 2024, Google Research) tackles the semantic gap between user queries and tool descriptions without any training data. During indexing, it generates diverse synthetic queries for each tool ("reverse-HyDE"). During inference, an intent extractor decomposes user queries into tool-related intents, and a multi-view similarity ranking strategy combines these signals. The result: **20% relative improvement in nDCG@5** for single-tool retrieval and 39% for multi-tool. This technique has been adopted by subsequent work (Toolshed, ScaleMCP) as a standard enrichment step.

### Instruction-Tool Retrieval: per-step dynamic context

ITR (December 2025) retrieves, per agent step, only the minimal system-prompt fragments and smallest necessary tool subset, composing a dynamic runtime system prompt with confidence-gated fallbacks. The compounding savings are substantial: **95% per-step context token reduction**, 32% improvement in correct tool routing, and 70% end-to-end cost reduction. This enables 2–20x more agent loops within context limits, making it particularly valuable for long-running autonomous agents.

### Graph-based and RL-based approaches

Agent-as-a-Graph (November 2025) combines knowledge graph structure with type-specific weighted reciprocal rank fusion for retrieval, representing tools and agents as nodes and edges. A critical finding: **"the low variance across embedding families indicates that gains stem primarily from structural design rather than encoder-specific characteristics"**—architecture matters more than which embedding model you choose.

On the RL front, AutoTool (submitted to ICLR 2026) uses dual-phase optimization with KL-regularized Plackett-Luce ranking for tool selection refinement across 1,000+ tools, achieving 6–8% gains across math, search, code, and multimodal tasks. NVIDIA's Nemotron Tool-N1 demonstrates that pure RL training can build tool-calling models that outperform GPT-4o. Salesforce's xLAM family currently holds #1 on the Berkeley Function Calling Leaderboard.

---

## The production architecture: building ToolSearchToolChain for Gent

Based on the complete research landscape, here is the recommended architecture organized as a pipeline with concrete Go design decisions.

### Tool description indexing: the EASYTOOL + Toolshed pattern

Raw tool descriptions are diverse, redundant, and incomplete. The EASYTOOL paper (NAACL 2025) showed that transforming tool docs into a unified, concise format lets ChatGPT + compressed descriptions outperform GPT-4 with raw docs. Combined with Toolshed's enrichment approach, each tool should be indexed as:

```go
type ToolIndexEntry struct {
    ToolID           string
    Name             string            // verb-noun: "SearchFiles", "GetWeather"
    Description      string            // concise 1-2 sentence purpose
    Category         string            // "file_operations", "communication"
    Tags             []string          // ["search", "filesystem", "grep"]
    ParameterSummary string            // "query (string, required), path (string, optional)"
    SyntheticQueries []string          // reverse-HyDE: "find files matching pattern"
    Capabilities     []string          // machine-readable capability tags
    Version          string
    ContentHash      string            // SHA-256 for change detection
    Embedding        []float32         // pre-computed TDWA vector
}
```

The composite embedding text should use markdown headers (research shows markdown structure improves retrieval from 77% to 84%) and weighted concatenation following ScaleMCP's TDWA approach. For the embedding model, start with **OpenAI's text-embedding-3-small** (API-based, fast to production) or **BGE-M3** (best open-source). The critical insight from Agent-as-a-Graph applies: your indexing architecture matters far more than which embedding model you choose.

### Query formulation: start simple, add complexity on demand

Three strategies exist, in order of complexity. Direct user message (zero latency, works for 80% of cases), hybrid formulation (use direct message for short queries, extract focused queries via lightweight LLM for multi-turn conversations), and full LLM-generated queries (highest quality, adds 200–500ms). Start with direct message, add hybrid formulation when multi-turn conversations accumulate context that diverges from the original query. ITR's per-step query extraction is the gold standard but should only be added when latency budgets allow.

### The two-stage retrieval pipeline

**Stage 1** maximizes recall: vector similarity search retrieves top-20 to top-50 candidates, optionally combined with BM25 keyword matching via Reciprocal Rank Fusion for hybrid retrieval. **Stage 2** maximizes precision through re-ranking. Cross-encoders (like `mxbai-rerank-base-v2`, Apache 2.0 licensed, specifically handles JSON and code) add 10–50ms latency with strong quality. LLM-based pointwise scoring adds 200–900ms but gives the highest quality—Intercom's production system parallelizes this by sharding candidates. For fewer than 100 tools, skip the re-ranker entirely; vector similarity is sufficient.

```go
type ToolSearchPipeline struct {
    registry  *ToolRegistry
    index     VectorIndex       // chromem-go for <10K tools
    reranker  Reranker          // optional cross-encoder
    cache     *lru.Cache        // query → results
    config    SearchConfig
}

func (p *ToolSearchPipeline) Search(ctx context.Context, query string, opts SearchOpts) ([]Tool, error) {
    // Check cache first
    if cached, ok := p.cache.Get(normalizeQuery(query)); ok {
        return cached.([]Tool), nil
    }
    // Stage 1: broad retrieval with optional metadata pre-filtering
    candidates, _ := p.index.Search(queryEmbedding, topK: 30, filters: opts.MetadataFilters)
    // Stage 2: re-rank if enabled and enough candidates
    if p.config.EnableReranking && len(candidates) > p.config.MaxTools {
        candidates = p.reranker.Rerank(ctx, query, candidates[:20])
    }
    // Apply score threshold and return top-K
    results := filterByThreshold(candidates, p.config.ScoreThreshold)[:opts.MaxTools]
    p.cache.Add(normalizeQuery(query), results)
    return results, nil
}
```

### Agent loop integration: the middleware pattern

The recommended integration follows a middleware pattern inspired by Go's `net/http` middleware and the ITR paper's per-step approach. A `ToolSearchMiddleware` intercepts each model call, formulates a search query from the current agent context, retrieves relevant tools, merges them with always-included critical tools, and injects only the selected tool definitions into the LLM request. This keeps context lean—5–10 dynamically selected tools consume 500–3,000 tokens versus 10,000–30,000 for 100 statically loaded tools.

The complementary MemTool pattern (2025) handles the other direction: removing tools from context when they're no longer needed, treating the context window as RAM and tools as loaded modules. Reasoning LLMs achieve 90–94% tool-removal efficiency with this approach.

### Registry management: versioning and live updates

The tool registry should use `sync.RWMutex` for concurrent access—reads don't block each other, writes are exclusive. Every tool gets a content hash (SHA-256); embeddings are only recomputed when the hash changes. For MCP integration, ScaleMCP's auto-synchronizing CRUD pipeline pattern is the reference implementation: periodically poll MCP servers, diff against current index by hash, and perform incremental updates. Event-driven cache invalidation ensures stale search results are purged when tools change.

### Go-specific technology choices

For embedded vector search, **chromem-go** is the clear recommendation: zero third-party dependencies, Chroma-like API, built-in support for OpenAI/Ollama/Cohere embedders, and excellent performance for tool-scale datasets (1K docs in 0.3ms, 100K in 40ms). For reference implementations of Go agent patterns, **rhettg/agent** provides an elegant `net/http`-inspired middleware design most architecturally aligned with Gent, while **cloudwego/eino** (ByteDance) offers the most mature graph-based agent composition. For embeddings, use `sashabaranov/go-openai` for API-based embedding or chromem-go's built-in Ollama support for local inference.

---

## Recommended implementation roadmap

The phased approach below moves from immediate production value to frontier capabilities:

- **Phase 1 (immediate value)**: Implement the ToolRegistry with chromem-go vector index, EASYTOOL-compressed tool descriptions, direct-message query formulation, and cosine similarity retrieval with top-K selection. This alone handles the 20–100 tool range effectively.

- **Phase 2 (enhanced retrieval)**: Add reverse-HyDE synthetic query enrichment during indexing (Re-Invoke/Toolshed pattern), hybrid vector + BM25 retrieval with Reciprocal Rank Fusion, cross-encoder re-ranking for precision-critical use cases, and LRU caching with content-hash-based invalidation.

- **Phase 3 (agent-driven discovery)**: Implement the MCP-Zero active discovery pattern where the agent generates structured tool requests rather than passively matching. Add per-step ITR-style dynamic context management and tool removal (MemTool pattern). Integrate MCP server auto-synchronization following ScaleMCP's CRUD pipeline.

- **Phase 4 (frontier)**: Explore graph-based tool relationships (Agent-as-a-Graph), tool usage transition graphs for predictive tool loading, and fine-tuned tool selection models (xLAM-style) for highest accuracy.

---

## Conclusion

The tool search landscape has matured rapidly from a research curiosity to a production necessity. Three insights stand out as non-obvious. First, **indexing architecture dominates embedding model choice**—the Agent-as-a-Graph finding that variance across embedding families is negligible (std ≈ 0.02) means energy is better spent on Toolshed-style document enrichment than embedding model selection. Second, **active tool discovery outperforms passive retrieval**—MCP-Zero's agent-driven approach achieves 98% token reduction versus Toolshed's 46-56% recall improvement, suggesting the paradigm shift from "search for tools" to "agent requests capabilities" is the future. Third, the **"less is more" principle is universal**: every study from AnyTool's hierarchical filtering to Anthropic's Tool Search Tool confirms that reducing the number of tools presented to an LLM improves both accuracy and speed. For Gent's `ToolSearchToolChain`, the practical starting point is a vector-indexed registry with enriched tool documents and a middleware-based injection pattern—simple enough to ship in days, with a clear upgrade path to frontier methods as the tool catalog grows.