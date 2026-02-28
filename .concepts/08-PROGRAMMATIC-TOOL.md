# Programmatic tool calling: the architectural shift rewriting LLM agents

**Programmatic tool calling (PTC) lets an LLM write a script that orchestrates multiple tool calls in a single execution step, and it delivers dramatic efficiency gains—37–99% token reduction and up to 20% higher task success rates—but it does not solve tool discovery.** Your mental model is correct: tool search handles discovery, programmatic calling handles execution orchestration, and the two compose cleanly as separate architectural layers. Anthropic validated this exact separation in November 2025 by shipping Tool Search Tool and Programmatic Tool Calling as complementary features in the same release. This report covers the precise technical details, every major implementation, the academic foundations, performance data, and the architectural implications for your Gent framework.

---

## How Anthropic's implementation actually works

Anthropic released PTC as a public beta on November 24, 2025 (blog: "Introducing advanced tool use on the Claude Developer Platform" by Bin Wu et al.), graduating to general availability with Claude Sonnet 4.6 and Opus 4.6 in February 2026. The core idea: instead of Claude requesting tools one at a time with each result returning to its context window, Claude writes Python code that calls multiple tools, processes outputs, and controls what information enters context.

The execution flow has five distinct phases. First, the developer defines tools in the API request with an `allowed_callers` field set to `["code_execution_20250825"]`, which opts each tool into programmatic calling. Second, **the API converts these JSON tool definitions into async Python functions** available inside a sandboxed container—the same containers used by the Code Execution tool. Third, Claude writes a Python script using `await` and `asyncio.gather` for parallel execution. Fourth, when the script hits a tool call, execution pauses, the API returns a `tool_use` block with a `caller` field indicating programmatic invocation, and the developer provides the tool result. Fifth, the script resumes—but crucially, **intermediate tool results never enter Claude's context window**. Only the script's final `stdout` does.

Here is the exact API shape:

```python
client.messages.create(
    model="claude-sonnet-4-5-20250929",
    max_tokens=4096,
    tools=[
        {"type": "code_execution_20250825", "name": "code_execution"},
        {
            "name": "get_expenses",
            "description": "Get expenses for an employee",
            "input_schema": {...},
            "allowed_callers": ["code_execution_20250825"]
        }
    ]
)
```

The sandbox is an Anthropic-managed container with no internet access, pre-installed scientific Python packages (numpy, pandas, pillow, openpyxl, pypdf, etc.), and support for both Python and Bash. Containers expire after approximately **4.5 minutes** of inactivity but can be reused via a returned `container` ID. Each tool call from code execution counts against standard rate limits, and tool results from programmatic invocations do not count toward token usage—only the final output does.

Notable limitations: tools with `strict: true` structured outputs are unsupported, web search and web fetch tools cannot be called programmatically, MCP connector-provided tools are excluded, and `tool_choice` cannot force a specific programmatic call. The `allowed_callers` field accepts `["direct"]` (traditional), `["code_execution_20250825"]` (programmatic only), or both—though Anthropic recommends choosing one mode per tool.

---

## Only four systems implement true programmatic tool calling

A critical finding from this research: **most systems marketed as having "code execution" do not implement programmatic tool calling**. OpenAI's Code Interpreter and Google Gemini's code execution are isolated computation sandboxes that cannot invoke external tools from within code. The LLM can use code execution alongside function calling, but must switch between them across turns via the traditional agent loop.

The systems that implement true PTC—where LLM-generated code directly invokes tools—are:

- **Anthropic PTC**: Python async functions in managed containers, `allowed_callers` opt-in, pause/resume execution model. Token savings of **37–85.6%** on complex tasks.

- **Cloudflare Code Mode** (September 2025): Converts MCP tool schemas into TypeScript type definitions, exposes just two meta-tools (`search()` and `execute()`), and runs code in **V8 isolates** (millisecond startup, no containers). Achieved **32–81% token reduction** and covers all 2,500+ Cloudflare API endpoints in ~1,000 tokens. Uses `codemode.*` bindings for tool dispatch via Workers RPC.

- **HuggingFace smolagents**: Open-source Python library where the default `CodeAgent` writes Python code with tool calls as function invocations. Tools are registered with `@tool` decorators and described in the system prompt. Supports sandboxing via E2B, Modal, Docker, or WebAssembly. Achieves **~30% fewer steps** than JSON-based tool calling.

- **Letta (MemGPT)**: Implements a `run_code_with_tools` capability letting agents write Python scripts that invoke any attached tool (MCP, custom, or built-in). Model-agnostic—works with any LLM at the harness layer.

OpenAI Code Interpreter runs in managed VMs (1–64GB memory tiers, 20-minute timeout) with no network access and no ability to call user-defined functions. Gemini's code execution runs for up to 30 seconds with a fixed library set and explicitly separates code execution from function calling. **Neither is PTC.** Cursor and Claude Code both use traditional iterative agent loops (ReAct pattern), though Claude Code's Bash tool enables writing scripts that achieve similar multi-step orchestration informally.

---

## The academic foundations trace back to 2022

The intellectual lineage of programmatic tool calling runs through four key papers, each building on the insight that LLMs are better at decomposing problems than executing solutions.

**PAL (Program-Aided Language Models, ICML 2023)** by Gao et al. established the foundational pattern: the LLM generates Python programs as intermediate reasoning steps, then offloads execution to an interpreter. PAL achieved a **15% absolute improvement** over PaLM-540B with chain-of-thought on GSM8K. **Program-of-Thought prompting (PoT, TMLR 2023)** by Chen et al. formalized the same insight for numerical reasoning, showing **~12% average gains** over chain-of-thought across eight math benchmarks. Both papers demonstrated that separating reasoning (LLM) from computation (interpreter) dramatically improves accuracy.

**CodeAct (ICML 2024)** by Wang et al. is the paper that directly generalized this pattern to tool orchestration. CodeAct proposes executable Python code as a unified action space for LLM agents, replacing JSON-based tool calling. The results are striking: **up to 20% higher success rate** across 17 LLMs on the M3ToolEval benchmark, with **30% fewer action steps** required. The key insight is that Python's native control flow (loops, conditionals, exception handling) and data flow (variables, data structures) allow composing multiple tool calls in a single step—something impossible with one-JSON-blob-per-turn approaches. CodeAct is now the default architecture for OpenHands (formerly OpenDevin), one of the most successful open-source coding agents. Anthropic's engineering blog explicitly credits CodeAct as inspiration.

**TaskMatrix.AI (2023)** by Liang et al. at Microsoft was among the earliest systems to generate code orchestrating multiple API calls. Its four-component architecture—foundation model, API platform, API selector, and API executor—presaged the tool search + programmatic execution composition pattern. The system generated solution outlines, retrieved relevant APIs, then produced executable code calling those APIs. **Chameleon (NeurIPS 2023)** by Lu et al. took a similar program-synthesis approach, achieving **86.5% on ScienceQA** (11.4% improvement over prior SOTA) by having an LLM synthesize programs composing vision models, web search, and Python functions.

The most recent benchmark evidence comes from **LOCA-bench (February 2026)**, which directly evaluates programmatic tool calling as a context engineering strategy. Its finding: "Programmatic tool calling is consistently strong across all models: it significantly improves accuracy while reducing trajectory length."

---

## Programmatic calling does not solve tool discovery

This is the critical architectural question, and the answer is unambiguous across every implementation examined: **programmatic tool calling requires tool definitions to exist in scope before code generation begins**. The agent must know tool names, signatures, input schemas, and output types to write code that calls them. A tool not in the `tools` array or sandbox scope simply does not exist—the generated code will fail with a `NameError`.

In Anthropic's PTC, the API converts JSON tool definitions into Python function stubs injected into the sandbox. In Cloudflare Code Mode, MCP tool schemas are compiled into TypeScript type definitions. In smolagents, tools registered with `@tool` decorators are described in the system prompt. In every case, **tool definitions must be provided before the LLM writes orchestration code**.

Anthropic validated the separation of concerns by shipping three complementary features simultaneously in November 2025:

- **Tool Search Tool** handles discovery—tools marked with `defer_loading: true` are excluded from initial context, and Claude uses regex/BM25 search to find relevant tools on demand. This reduced tool-definition tokens by **85%** (from ~72K to ~8.7K for 50+ MCP tools) while improving accuracy from **49% to 74%** (Opus 4) and **79.5% to 88.1%** (Opus 4.5).

- **Programmatic Tool Calling** handles execution orchestration—once tools are discovered and loaded, Claude writes code composing them.

- **Tool Use Examples** handles correct usage—providing input/output examples improved parameter handling accuracy from **72% to 90%**.

This three-layer architecture maps precisely to the ToolSearchToolChain concept: registration (MCP `tools/list`), discovery (semantic search/retrieval), injection (load definitions into context), and orchestration (write and execute code).

There is one nuance worth noting. Anthropic's "Code Execution with MCP" blog proposes auto-generating TypeScript files from MCP tool definitions and placing them on the sandbox filesystem. The agent can then `ls ./servers/` and `cat ./servers/google-drive/getDocument.ts` to discover tools within the execution environment. This technically integrates discovery into the code execution flow, but the definitions must still be pre-generated from MCP metadata—the agent cannot discover tools that haven't been registered.

---

## Performance data strongly favors programmatic execution for complex tasks

The quantitative evidence spans multiple independent sources and consistently shows large gains for multi-tool workflows, with diminishing returns for simple single-tool invocations.

**Token efficiency** is the most dramatic improvement. Anthropic's headline number is a **37% reduction** (43,588 → 27,297 tokens) on complex research tasks, but specific benchmarks show much larger gains: **85.6%** on team expense analysis (110,473 → 15,919 tokens) and up to **98.7%** when combined with progressive tool discovery (150,000 → 2,000 tokens). Cloudflare Code Mode achieved **99.9% reduction** for their full API (1.17M → ~1,000 tokens). The mechanism is straightforward—intermediate tool results (which can be enormous) stay in the sandbox rather than expanding the context window.

**Latency improvements** depend on task parallelizability. When Claude orchestrates 20+ tool calls in a single code block, it eliminates 19+ inference passes. The LLMCompiler paper (ICML 2024) by Kim et al. measured **up to 3.7× latency speedup** versus ReAct on parallel-friendly tasks. However, for sequential-dependency workflows where each tool call depends on the previous result, Anthropic's cookbook showed only **1.4% time improvement**—the bottleneck shifts from inference to tool execution.

**Accuracy gains** are consistent but moderate. CodeAct showed up to **20% higher success rate** on complex multi-tool tasks (M3ToolEval), with gains scaling with task complexity—simple single-tool tasks showed minimal difference. Anthropic reported improvements on GAIA benchmarks (**46.5% → 51.2%**) and dramatic improvements on agentic search: BrowseComp accuracy jumped from **33.3% to 46.6%** for Sonnet 4.6 when combining PTC with dynamic filtering. The accuracy gains likely come from Python's native error handling enabling self-debugging, and from control flow reducing the chance of the LLM losing track of multi-step plans.

**Cost savings** follow directly from token reduction and fewer inference calls. LLMCompiler measured **up to 6.7× cost savings** versus ReAct. A useful heuristic from LiteLLM: calling 10 tools directly uses approximately 10× the tokens of calling them programmatically and returning a summary.

The tradeoff is real: PTC adds overhead for simple tasks (container creation, code generation), reduces explainability (code blocks are harder to audit than structured tool calls), and introduces cascading failure modes where a bug in generated code can break an entire multi-tool workflow rather than failing on a single step.

---

## The correct architecture for Gent: a five-layer pipeline

Based on this research, the architecture that composes tool search and programmatic calling follows a clear pipeline, which your ToolSearchToolChain concept correctly anticipates:

**Layer 1 — Tool Registration** happens before the LLM is involved. MCP servers expose `tools/list`, API catalogs enumerate endpoints, and the framework builds a searchable registry of all available tools with their schemas, descriptions, and metadata. This is a system-level concern handled by Gent's framework code.

**Layer 2 — Tool Discovery** is LLM-involved but lightweight. When a user request arrives, the agent queries the tool registry (via semantic search, BM25, or regex) to find the 3–10 most relevant tools from potentially hundreds. Anthropic's Tool Search Tool is the production reference implementation. This step reduces context from ~72K tokens (50+ tools) to ~8.7K tokens (relevant subset).

**Layer 3 — Tool Injection** loads discovered tool definitions into the execution context. In Anthropic's model, this means including them in the `tools` array with `allowed_callers`. In a sandbox model, this means generating function stubs or type definitions. Usage examples for complex tools can be injected here.

**Layer 4 — Orchestration** is where the LLM writes code. Given the injected tool definitions, the LLM generates a Python (or Go, TypeScript, etc.) script that composes multiple tool calls with loops, conditionals, parallel execution, and data transformation. This is the programmatic tool calling step.

**Layer 5 — Execution** runs the generated code in a sandboxed runtime. Tool calls within the code are intercepted and routed to real backends. Intermediate results stay in the sandbox. Only the final output enters the LLM's context for reasoning.

For your Go-based Gent framework, the key implementation decisions are: what language the LLM generates (Python is best-supported given training data, but Cloudflare's TypeScript approach works well too), how you sandbox execution (V8 isolates are lightweight; containers are heavier but more flexible), and how tool functions are injected into the runtime (function stubs generated from JSON schemas). The Rhai scripting language (used by the open-source `tool-orchestrator` project) is worth considering for Go integration given its Rust heritage and lightweight embedding.

---

## Conclusion: discovery and orchestration are orthogonal concerns

The research conclusively shows that programmatic tool calling and tool discovery solve different problems and compose as independent architectural layers. Every production implementation—Anthropic, Cloudflare, smolagents, Letta—requires tools to be defined before the LLM can write code calling them. The emerging consensus architecture is exactly what you hypothesized: search narrows the tool set, definitions get injected, and the LLM writes a program to orchestrate execution.

Three insights go beyond the obvious. First, **there is an emerging third concern—correct usage**—that sits between discovery and orchestration. Knowing a tool exists and knowing its schema is insufficient; the LLM also needs usage patterns and examples, which Anthropic addresses with their Tool Use Examples feature. Second, **the token savings compound multiplicatively** when combining tool search (85% reduction in definitions) with programmatic execution (37–85% reduction in intermediate results), explaining the 98.7% combined reduction Anthropic measured. Third, **programmatic calling's accuracy advantage scales with task complexity**—it shows minimal gains on simple single-tool invocations but up to 20% improvement on complex multi-tool compositions, suggesting your framework should support both modes and select dynamically based on task analysis.

For Gent's ToolSearchToolChain, the architecture is validated. Tool search is upstream, programmatic calling is downstream, and the interface between them is a set of tool definitions that flow from discovery into the code execution environment. The pattern is not speculative—it is the production architecture at Anthropic, Cloudflare, and HuggingFace as of early 2026.