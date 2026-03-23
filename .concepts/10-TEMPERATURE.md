# Temperature and sampling parameters for LLM agent loops

**The optimal default temperature for most agentic workflows is 0.0–0.2, but the real answer is more nuanced.** Every major coding-focused agent system — Cursor, Aider, SWE-agent — defaults to temperature 0, while general-purpose frameworks like LangChain use 0.7 and LlamaIndex uses 0.1. Frontier reasoning models (OpenAI's o-series, GPT-5, Gemini 3) are making manual temperature tuning increasingly irrelevant by either rejecting the parameter entirely or warning against changing it from 1.0. For Gent, a Go-based framework spanning production, the evidence points toward **a default of 0.2 with task-specific presets** — low enough for reliable tool calling and code generation, high enough to avoid the coherence-loss loops that temperature 0 can trigger.

---

## What every frontier lab actually recommends

All four major labs — Anthropic, OpenAI, Google, and xAI — default their APIs to **temperature 1.0**, but their practical guidance diverges sharply when it comes to agentic use.

**Anthropic** explicitly advises using temperature "closer to 0.0 for analytical / multiple choice" tasks and "closer to 1.0 for creative and generative tasks." They recommend adjusting temperature OR top_p, never both, and state that top_k is "recommended for advanced use cases only." Notably, Anthropic provides no dedicated "agentic temperature" guidance — their tool-use documentation is silent on the topic. Claude's temperature range is capped at **0.0–1.0**, half the range of other providers.

**OpenAI** follows similar general guidance (lower = more deterministic), but introduces a critical wrinkle: **reasoning models (o1, o3, o4-mini, GPT-5) do not support the temperature parameter at all.** Sending temperature to these models returns an HTTP 400 error. Instead, they expose `reasoning_effort` (low/medium/high) as the primary behavioral control. The OpenAI Agents SDK defaults all sampling parameters to `None`, deferring to API defaults, with documentation examples using **temperature 0.1–0.3** for agent tasks.

**Google** provides the most explicit agentic guidance. For pre-Gemini 3 models, their function calling documentation states: "Use a low temperature (e.g., 0) for more deterministic and reliable function calls." For **Gemini 3**, however, they reverse course entirely: "We strongly recommend keeping the temperature at its default value of 1.0." Lowering it below 1.0 can cause "looping or degraded performance in complex mathematical or reasoning tasks." Gemini 3 uses `thinking_level` (high/low) instead of temperature for behavioral control.

**xAI** offers no official agentic temperature recommendation, but their Grok 4.1 Fast model — specifically "optimized for high-performance agentic tool calling" — supports temperature normally (range 0.0–2.0). Community examples cluster around **0.3–0.5** for agent configurations, with 0.1 for coding tasks.

The clear trend: **reasoning-capable models are deprecating manual temperature control**, replacing it with reasoning-effort parameters. Any agent framework built today must detect model type and strip unsupported parameters.

## How production agent systems set their defaults

The empirical evidence from production frameworks reveals a striking bimodal distribution: coding agents cluster near zero, general frameworks sit at 0.7 or defer to API defaults.

**Cursor** uses temperature **0.0** (greedy decoding) for all operations, confirmed by forum posts and Fireworks AI's documentation of their speculative decoding pipeline. Temperature is not user-configurable. **Aider** also defaults to **temperature 0** in its `sendchat.py` source code, though users can override via CLI flag, config file, or environment variable. Aider intelligently sets `use_temperature: false` for reasoning models that don't support it. **SWE-agent** defaults to **temperature 0.0 with top_p 0.95** — the only major framework that explicitly couples both parameters. Trajectory files are named with `t-0.00__p-0.95` confirming these defaults in practice.

**LlamaIndex** stands out with a **default of 0.1**, deliberately chosen because the framework is "designed primarily for RAG/retrieval applications where accuracy and consistency are paramount." **LangChain** defaults to **0.7**, inheriting OpenAI's historical default and reflecting its general-purpose nature. **AutoGen** (Microsoft) sets no framework default but its documentation recommends "Set the LLM temperature to 0 to reduce randomness" for agent tasks, and most tutorial examples use temperature 0. **CrewAI** and **smolagents** both defer to provider defaults (typically 1.0) and expose temperature as an optional parameter.

The closed-source products — **Claude Code**, **Codex CLI**, and **Devin** — do not expose temperature to users at all. Claude Code has an open GitHub issue requesting configurable temperature, confirming it's internally managed. Codex CLI exposes `reasoning_effort` as its only tuning knob.

| Framework | Default Temp | Configurable | Top_p | Primary use case |
|-----------|-------------|-------------|-------|-----------------|
| Cursor | 0.0 | No | — | Code editing |
| Aider | 0.0 | Yes | API default | Code generation |
| SWE-agent | 0.0 | Yes | 0.95 | Bug fixing |
| LlamaIndex | 0.1 | Yes | 1.0 | RAG/retrieval |
| LangChain | 0.7 | Yes | 1.0 | General-purpose |
| AutoGen | API default | Yes | API default | Multi-agent |
| CrewAI | API default | Yes | API default | Multi-agent |

**No framework uses different temperatures for different agent phases by default**, though LangChain's `configurable_fields` mechanism supports this pattern programmatically.

## Temperature's real impact on reliability is smaller than you think

The most rigorous research reveals a counterintuitive picture: temperature matters less than model selection and prompt engineering, but the edges of the range (0.0 and >1.0) create real problems.

The **RIKER study** (March 2026, evaluating 172 billion tokens across 35 models at four temperature settings) found that temperature 0.0 yields the best overall accuracy in roughly **60% of cases**, but — critically — **coherence loss (infinite generation loops) can be 48× higher at T=0.0 compared to T=1.0.** This is a serious production concern for agent loops, where a stuck generation can block an entire pipeline. The study also found that higher temperatures actually *reduce* fabrication for the majority of models, challenging the common assumption that lower temperature always means fewer hallucinations.

A peer-reviewed **EMNLP 2024 paper** (Renze) testing GPT-3.5, GPT-4, and Llama 2 across temperatures 0.0–1.0 found **no statistically significant difference in problem-solving accuracy** within this range (Kruskal-Wallis test, p>0.05). Performance only degrades sharply **beyond 1.0**, becoming statistically random at 1.4. The practical implication: within the 0.0–1.0 range, temperature is not the lever most worth optimizing.

The **Mount Sinai clinical hallucination study** (Nature npj Digital Medicine, 2025) reinforced this: across 6 LLMs and 5,400 clinical prompts, "setting temperature to zero — hallucination rates remained similar to default settings." Prompt engineering reduced hallucinations from 65.9% to 44.2%, dwarfing temperature's effect.

For **code generation**, the picture is more nuanced. Package hallucination rates show a "clear increase as temperature increases," with the jump from 1.0 to 2.0 being especially severe — open-source models at maximum temperature generate more hallucinated packages than valid ones. **GPT-4 at maximum temperature still hallucinated at 4× lower rates than GPT-3.5**, reinforcing that model capability dominates parameter tuning. For single-shot code correctness (pass@1), temperature 0.0–0.2 is optimal; for multi-sample strategies (pass@k where k>1), temperatures of 0.4–0.8 improve diversity enough to find correct solutions.

## Adaptive temperature is the frontier, but not yet production-ready

Several research approaches dynamically vary temperature during generation, and the results are promising for future agent frameworks.

**Entropy-based Dynamic Temperature (EDT)** selects temperature at each decoding step based on the model's predictive entropy — low entropy (confident) triggers lower temperature, high entropy (uncertain) triggers higher temperature. It "significantly outperforms both fixed temperature and KL-divergence-based dynamic strategies" across summarization, QA, and translation, with "nearly negligible computational overhead."

**AdapT** (AAAI 2024) applies this specifically to code generation, categorizing tokens as "challenging" (beginning of code blocks) vs. "confident" and adjusting temperature accordingly. On HumanEval pass@15, CodeGeeX-13B improved from 36.0% to 40.9% — a **13.6% relative improvement**. This approach overcomes the fundamental tradeoff where increasing constant temperature improves pass@15 but reduces pass@5.

**IntroLLM** (February 2026) uses hierarchical reinforcement learning to learn temperature policies from downstream rewards. The model autonomously allocates larger exploration budgets to harder problems, showing "reasoning rhythm" with temperature peaks at logical pivots and valleys during routine computation.

None of these approaches are integrated into production agent frameworks today. For Gent, the practical takeaway is to **architect temperature as a per-step configurable parameter** from the start, making it possible to implement adaptive strategies later without refactoring.

## Other sampling parameters rarely need tuning in agent contexts

The evidence strongly supports keeping **top_p, top_k, frequency_penalty, and presence_penalty at their defaults** for most agent workloads, with a few important exceptions.

**Top_p** (nucleus sampling) should generally be left at 1.0 when temperature is being adjusted, following the universal vendor guidance to "alter temperature OR top_p, not both." The notable exceptions are model-specific: **DeepSeek-R1** explicitly requires top_p=0.95, **Qwen3** recommends top_p=0.95 (thinking mode) or 0.8 (non-thinking), and **SWE-agent** couples temperature 0.0 with top_p 0.95 in practice.

**Top_k** is even less commonly adjusted. OpenAI doesn't even expose it. Anthropic supports it but calls it "advanced use cases only." Qwen3 recommends top_k=20 across all modes. For most agent frameworks, **top_k can be safely ignored**.

**Frequency and presence penalties** default to 0.0 everywhere and are rarely touched in agentic contexts. The exception is long-running conversational agents where repetition becomes a problem — CrewAI's examples use frequency_penalty=0.1 and presence_penalty=0.1. Qwen3 uses presence_penalty=1.5 for creative writing benchmarks. For agent tool-calling loops, these penalties should remain at 0.

**Structured output enforcement is far more important than sampling parameters** for tool-calling reliability. The hierarchy of effectiveness: API-native structured outputs (OpenAI's `response_format` with JSON schema, Anthropic's `strict: true`) > constrained decoding (Outlines, llama.cpp grammar) > function calling with schemas > JSON mode > prompt-based approaches. Constrained decoding modifies logits directly, making it temperature-independent.

## What different models need differently

Model-specific requirements create a real engineering challenge for multi-model agent frameworks.

| Model family | Temperature | Top_p | Special handling |
|---|---|---|---|
| Claude (all versions) | 0.0–1.0 range only | Adjust temp OR top_p, not both | No frequency/presence penalty params |
| GPT-4o, GPT-4.1 | 0.0–2.0 | 1.0 default | Standard handling |
| o1, o3, o4-mini, GPT-5 | **Not supported** (fixed at 1) | **Not supported** | Must strip params; use reasoning_effort |
| Gemini (pre-3) | 0.0 recommended for tools | Standard | Standard handling |
| Gemini 3 | **Must be 1.0** | Standard | Use thinking_level; lower temp causes loops |
| DeepSeek-R1 | **0.6** (0.5–0.7 range) | **0.95** | Outside range causes repetitions |
| Qwen3 (thinking) | **0.6** | **0.95**, top_k=20 | Do NOT use greedy decoding |
| Qwen3 (non-thinking) | **0.7** | **0.8**, top_k=20 | presence_penalty=1.5 for creative |
| Grok 4 | 0.0–2.0 | Standard | Supports temp unlike OpenAI reasoning models |
| Mistral Small 3.2 | **0.15** | Standard | Very low default |

The critical implementation requirement: **Gent must detect model type and apply appropriate parameter handling.** Sending temperature to OpenAI's o-series returns an error. Sending temperature below 1.0 to Gemini 3 causes loops. DeepSeek-R1 needs a very specific range. A model registry mapping model IDs to their parameter constraints is essential.

## Specific recommendations for Gent

**Default temperature: 0.2.** This balances the coding-agent consensus (temperature 0) against the RIKER finding that T=0 causes 48× more coherence loops than T=1.0. A small amount of randomness (0.2) provides escape routes from degenerate patterns while maintaining high determinism for tool calling and structured output. This aligns with LlamaIndex's approach (0.1) and OpenAI's agent SDK examples (0.1–0.3).

**Task-based presets** should be a first-class feature:

- **Tool calling / structured output**: temperature 0.0–0.1, top_p 1.0, no penalties. Pair with structured output enforcement (JSON schema validation) as the primary reliability mechanism.
- **Code generation**: temperature 0.2, top_p 1.0. For retry-with-diversity, bump to 0.4–0.6 on subsequent attempts.
- **Customer service**: temperature 0.1, all penalties at 0. Maximum determinism for listing data, calculations, policy compliance.
- **Creative writing**: temperature 0.7–0.9, presence_penalty 0.5–1.0 to encourage narrative diversity. This is where higher temperature genuinely helps.
- **Planning and reasoning**: temperature 0.3–0.5, top_p 1.0. Moderate randomness aids multi-step exploration without sacrificing coherence.

**Model-aware parameter stripping** is non-negotiable. Implement a model registry in Go that maps model identifiers to their parameter constraints. At minimum, handle: OpenAI reasoning models (strip temperature, top_p, penalties), Gemini 3 (force temperature 1.0), DeepSeek-R1 (clamp to 0.5–0.7), and Claude (clamp to 0.0–1.0 range). Return or log warnings when user-requested parameters conflict with model constraints.

**Expose but default conservatively.** Follow the pattern of Aider and SWE-agent: set sensible defaults but make temperature configurable via both code and configuration. Expose temperature, top_p, and max_tokens. Skip top_k, frequency_penalty, and presence_penalty from the default API surface — make them available through an advanced configuration path. Add a `reasoning_effort` parameter for models that support it.

**Architect for future adaptive temperature.** Even though no production framework implements per-step temperature adaptation today, the research (EDT, AdapT, IntroLLM) consistently shows 10–15% improvements. Design Gent's inference pipeline so temperature can be overridden per-call without requiring a new client instance. This makes it trivial to later implement strategies like "lower temperature for tool-call generation, higher temperature for planning steps."

## Conclusion

The temperature landscape for LLM agents is simultaneously simpler and more complex than it appears. **Simpler** because within the 0.0–1.0 range, temperature has a surprisingly modest effect on accuracy — model selection and prompt engineering dominate. **More complex** because reasoning models are fragmenting the parameter space, model-specific requirements now vary dramatically, and the edges (T=0 coherence loops, T>1.0 degradation) create real production risks.

The strongest signal from the evidence is that **a moderate low temperature (0.1–0.3) paired with structural enforcement mechanisms (JSON schema validation, constrained decoding) outperforms extreme greedy decoding (T=0) for production agent loops.** Temperature 0.0 is not a safe default — it trades parsing reliability for coherence risk. The second strongest signal is that **the temperature parameter itself is being deprecated by frontier reasoning models** in favor of reasoning-effort controls, suggesting that Gent should treat temperature as one tuning mechanism among several rather than the primary behavioral control.

For Gent specifically, a default of **0.2** with model-aware parameter handling and task-based presets provides the best foundation.