# The complete guide to LLM agent architectures

Large language model agents represent a paradigm shift from passive text generators to autonomous systems that reason, plan, and act. **The most important insight from production deployments is counterintuitive: simpler architectures consistently outperform complex multi-agent systems.** Anthropic's Claude Code—responsible for 90% of its own codebase—runs on a single-threaded loop, not an elaborate agent hierarchy. This guide synthesizes foundational patterns, industry best practices, and hard-won production lessons to help you design agents that actually work.

The core architectural decision isn't which fancy framework to adopt—it's understanding when you need an agent at all. Agents trade latency and cost for capability. They excel at open-ended problems where the solution path can't be predetermined, but they're overkill for most tasks. Start with direct LLM calls, add tools, then add loops, and only introduce multi-agent coordination when simpler approaches demonstrably fail.

---

## Core reasoning patterns define how agents think

Every agent architecture builds on a small set of foundational reasoning patterns. Understanding these deeply matters more than any framework choice because they determine how your agent approaches problems.

**ReAct (Reasoning + Acting)** is the most influential pattern, introduced by Yao et al. in 2022. It synergizes reasoning and acting through an interleaved Thought-Action-Observation loop. The agent generates a reasoning trace ("I need to find the population of Paris"), executes an action (search), observes the result, then reasons again. This grounding in external sources reduces hallucination compared to pure reasoning—ReAct outperforms Chain-of-Thought on fact verification by significant margins.

```
while not task_complete:
    thought = llm.generate("Given observations, what should I do next?")
    action = parse_action(thought)
    if action.type == "finish":
        return action.answer
    observation = execute_tool(action)
    context.append(thought, action, observation)
```

ReAct's limitation is sequential bottleneck—each observation must complete before the next thought. It also struggles with repetitive action loops when the model gets stuck trying similar unsuccessful approaches. Use ReAct when tasks require external information retrieval and you need interpretable decision traces.

**Chain-of-Thought (CoT)** prompting enables complex reasoning by generating intermediate steps before the final answer. Zero-shot CoT—simply appending "Let's think step by step"—can boost accuracy from **17.7% to 78.7%** on arithmetic tasks. Few-shot CoT includes exemplars demonstrating step-by-step reasoning. A critical 2024-2025 finding: strong models like Qwen2.5 often ignore few-shot exemplars entirely, achieving equal or better performance with zero-shot instructions alone.

**Tree-of-Thoughts (ToT)** extends CoT by exploring multiple reasoning paths simultaneously, enabling backtracking when paths fail. It requires four components: thought decomposition, candidate generation, state evaluation, and search (BFS or DFS). On the Game of 24 puzzle, GPT-4 with standard prompting achieves 7.3%, with CoT 4%, but with ToT **74%**. However, ToT costs 5-100x more tokens than CoT. Use it only when problems genuinely require exploration and backtracking—complex puzzles, creative generation with constraints, or planning problems where early mistakes are catastrophic.

**Plan-and-Execute** separates strategic planning from tactical execution in a two-phase architecture. A planning LLM generates the complete task breakdown upfront; execution can use smaller, cheaper models or tools. This enables parallelization and cost optimization—the LLMCompiler variant claims **3.6x speedup** through DAG-based parallel execution. The tradeoff: rigid plans adapt poorly to unexpected observations mid-execution.

| Pattern | Token Efficiency | Adaptability | Best For |
|---------|-----------------|--------------|----------|
| ReAct | Low (many LLM calls) | High (adapts each step) | Knowledge-intensive, interactive tasks |
| CoT | High (single call) | None | Math, logic, commonsense reasoning |
| ToT | Very Low (5-100x CoT) | High (backtracking) | Puzzles, creative tasks with constraints |
| Plan-and-Execute | Medium | Low (requires replanning) | Multi-step workflows, cost optimization |

---

## Memory architectures determine what agents can remember

LLMs are fundamentally stateless—every piece of information an agent "knows" must be explicitly present in its context window. Memory architecture decisions determine what information survives between turns, sessions, and tasks.

**Short-term memory** is the context window itself, functioning as working RAM. Modern models support 8k-200k tokens, but longer contexts increase cost quadratically and suffer from the "lost-in-the-middle" problem where models under-attend to central content. Practical strategies include sliding window (FIFO eviction of oldest messages) and recursive summarization. MemGPT implements a "memory pressure" warning at ~80% capacity, prompting the agent to save critical context before automatic flush and summarization at 100%.

**Long-term memory** requires external persistent storage, typically vector databases. Text is embedded into dense vectors, indexed for similarity search, and retrieved at query time. The retrieval process is critical—naive semantic similarity can return topically related but factually irrelevant content. Production systems combine multiple retrieval strategies:

- **Recency-based**: Exponential decay scoring (γ^hours_since_access where γ ≈ 0.995)
- **Relevance-based**: Cosine similarity between query and memory embeddings
- **Importance-based**: LLM-scored significance at storage time

The Stanford Generative Agents paper combined all three into a weighted formula, demonstrating more human-like memory behavior. Vector databases (Pinecone, Chroma, Qdrant, FAISS, pgvector) differ primarily in managed vs. self-hosted tradeoffs, not fundamental capabilities.

**Episodic memory** stores complete experiences—what the agent did, when, and what happened. The Reflexion framework demonstrated powerful learning: storing failed trajectories with verbal reflections ("Last time I failed because..."), then including these reflections on retry. This achieved **+34% improvement on ALFWorld** without any model fine-tuning.

**Semantic memory** stores facts and concepts independently of when they were learned. Knowledge graphs (entity-relationship-entity triples) excel at multi-hop reasoning and explicit relationships; vector stores handle fuzzy semantic matching. Microsoft's GraphRAG combines both—extracting entities and relationships via LLM, building community-structured graphs, then using hierarchical summaries for retrieval. This approach significantly outperforms naive RAG on "connecting the dots" queries across documents.

A crucial implementation consideration: Claude Code uses **grep and regex search over vector embeddings** for code navigation. Anthropic found that Claude's understanding of code structure enables sophisticated pattern crafting that outperforms semantic search for codebase exploration.

---

## Tool use patterns enable agents to act on the world

Tools transform LLMs from reasoning engines into agents that can take actions. The fundamental mechanism is function calling: the model generates structured output (typically JSON) specifying which tool to invoke and with what parameters, then your application executes the tool and returns results.

**Function definitions matter enormously**. Tool descriptions effectively serve as API documentation for a "smart but distractible junior developer." Clear, specific descriptions with explicit parameter constraints (enums, types, examples) dramatically improve tool selection accuracy. From Anthropic: "If a human engineer can't definitively say which tool should be used in a given situation, an AI agent can't be expected to do better."

**Large tool inventories create problems**. Each tool definition consumes tokens. Overlapping functionality confuses models. Research shows LLMs exhibit positional bias—favoring tools at the beginning or end of lists while overlooking middle entries. Strategies include:

- **Tool search**: A meta-tool that queries a registry of thousands of tools without loading all definitions into context
- **Dynamic loading**: Load only category-relevant tools based on task classification
- **Minimal viable toolset**: Ruthlessly curate the smallest set that covers requirements

**When to use tools versus pure reasoning** follows a simple heuristic: Can the LLM answer correctly without external help? Use tools for real-time data, precise calculations, external actions, or domain knowledge beyond training. Use pure reasoning for synthesis, creativity, and information within model knowledge. Unnecessary tool calls incur latency, cost, and context pollution.

**Tool composition** enables complex workflows through chaining. Sequential patterns connect outputs to inputs (search → retrieve → analyze). Higher-order tools—tools that spawn sub-agents using other tools—enable separation of concerns. Anthropic's multi-agent research system uses orchestrator agents that spawn specialized sub-agents, each returning condensed summaries (1,000-2,000 tokens) to the coordinator.

**Tool design principles for agents**:
1. Self-contained: Each tool performs one clear function
2. Clear purpose: Unambiguous about when to use
3. Minimal overlap: No functional redundancy
4. Error messages that help recovery: Specific, actionable, including suggestions
5. Idempotent where possible: Safe to retry on failure

---

## Multi-agent patterns coordinate specialized capabilities

Multi-agent architectures enable division of labor, but add significant complexity. The key insight from production: use the simplest coordination pattern that solves your problem.

**Orchestrator-Worker** is the most common production pattern. A central lead agent analyzes queries, develops strategies, and spawns specialized workers that operate with independent context windows. Workers write outputs to shared state; the orchestrator synthesizes results. This pattern excels at parallelizable research tasks but costs **15x more tokens** than single-agent approaches.

**Agent Handoffs** transfer control between specialized agents—like transferring a phone call while preserving conversation history. Implementation approaches include tool-based handoffs (the LLM calls a `transfer_to_specialist` tool) and command-based handoffs (explicit `goto` directives). State transfer mechanisms range from full conversation history to context summarization to lightweight references. Use handoffs for domain specialization (billing vs. technical support) and escalation patterns.

**Hierarchical Agents** create manager-subordinate relationships at multiple levels: strategy layer (high-level planning), planning layer (task coordination), execution layer (atomic tasks). CrewAI implements this with a manager LLM that dynamically creates and delegates sub-tasks. Use hierarchies for enterprise workflows with natural chains of command; avoid them for highly dynamic peer coordination needs.

**Debate/Critique patterns** orchestrate multiple agents to iteratively discuss and refine responses. Research shows Multi-Agent Debate often doesn't outperform single-agent Chain-of-Thought + Self-Consistency for strong models. Benefits appear primarily with weaker models or especially difficult problems. The simpler Evaluator-Optimizer pattern—one agent generates, another evaluates, loop until acceptance—works well when clear evaluation criteria exist.

**Swarm architectures** feature decentralized coordination without central control, with behavior emerging from local interactions. Current LLMs struggle with pure swarm constraints (limited local perception), making this an active research area rather than production-ready pattern. OpenAI's Swarm implementation uses explicit handoff tools between peer agents, eliminating the supervisor intermediary layer—fewer LLM calls, lower latency, reduced token usage.

---

## Control flow patterns determine execution structure

Control flow patterns range from simple loops to complex graph-based workflows. Choose based on your task's determinism and complexity.

**Agentic loops** are the fundamental building block: `while not done: think → act → observe`. Critical implementation details include termination conditions (goal achieved, max iterations, token budget, explicit stop signal) and infinite loop prevention. With 99% success per step, you achieve only **90.4% success over 10 steps**—compound probability is unforgiving. Implement stuck detection (same action repeated N times triggers forced alternative), iteration limits, and user prompting for escape.

**State machines** model agent behavior as discrete states with defined transitions. They excel for well-defined processes with known states (customer service flows, compliance workflows), providing explicit state tracking for debugging and auditability. They fail for open-ended problems with unpredictable state spaces.

**Graph-based workflows (DAGs)** represent tasks as nodes with dependency edges. Frameworks like LangGraph enable parallel execution of independent branches, conditional routing, and dynamic worker creation. DAGs suit multi-step processes with clear dependencies but struggle with iterative refinement requiring cycles.

**Parallel execution** runs multiple agent branches concurrently for independent subtasks. Implementation requires careful attention to shared state—use unique output keys or reducer functions to prevent race conditions. Aggregation strategies include concatenation, majority voting, LLM synthesis, or best-of selection. Parallel execution can **cut time by 90%** for truly independent tasks.

---

## Planning approaches handle task complexity

Planning determines how agents decompose and sequence work. The right approach depends on task predictability and error tolerance.

**Task decomposition** breaks complex tasks into manageable subtasks. Decomposition-first (plan-then-execute) creates the full subtask list before execution; interleaved decomposition adjusts dynamically based on results. External planner integration (translating to PDDL for classical planners) guarantees optimal plans but requires domain-specific definitions.

**Iterative refinement** produces initial outputs then progressively improves through draft-critique-revise cycles. Self-Refine (Madaan et al., 2023) demonstrated significant improvements using a single LLM for both generation and critique. Convergence criteria include quality thresholds, no-improvement detection, max iterations, or external validation. The risk: later iterations may regress quality.

**Self-reflection** enables agents to analyze their own reasoning and generate corrective feedback. The Reflexion framework stores failed trajectories with verbal reflections, achieving substantial improvements without model retraining. Critical finding: self-reflection significantly improves problem-solving (p < 0.001), with largest gains on analytical reasoning tasks. However, it hurts when the model is confidently wrong—it may reinforce errors through circular reasoning.

---

## Production agents reveal what actually works

Real production deployments provide the most valuable lessons, often contradicting theoretical expectations.

**Anthropic's "Building Effective Agents"** (December 2024) distinguishes workflows (predefined code paths) from agents (LLMs dynamically directing their own processes). Their key recommendation: "Find the simplest solution possible, and only increase complexity when needed. This might mean not building agentic systems at all." They advise against over-relying on frameworks ("extra layers of abstraction that obscure underlying prompts") and starting with complex multi-agent systems.

Anthropic identifies **five composable workflow patterns**: prompt chaining (sequential LLM calls), routing (classifying inputs to specialized handlers), parallelization (concurrent independent tasks), orchestrator-workers (dynamic delegation), and evaluator-optimizer (generate-evaluate loops). These patterns combine as building blocks rather than requiring monolithic architectures.

**Claude Code's architecture** deliberately chose simplicity: a single-threaded master loop, not multi-agent systems. The core feedback loop is `gather context → take action → verify work → repeat`. Tools include reading (file viewing, grep, glob), writing (file edit/create), and execution (sandboxed bash). Notably, Claude Code uses **grep/regex search over vector embeddings** because "Claude's inherent understanding of code structure enables sophisticated regex pattern crafting."

Safety comes through filesystem isolation (specific directories only) and network isolation (approved servers only). Development velocity: ~60-100 internal releases per day, ~5 PRs per engineer per day.

**Cursor's architecture** provides an "autonomy slider" from tab completion through inline edits to full agentic mode. Their Composer model uses mixture-of-experts trained via reinforcement learning on real codebases. Key innovation: running the same task through multiple models simultaneously (GPT-5, Claude Sonnet, Composer) for comparison, with per-agent undo functionality.

**Devin** (Cognition Labs) operates in a sandboxed workspace with shell, editor, and browser. The workflow is `propose plan → execute autonomously → collaborate mid-flight → deliver`. The core loop: run tests → read error logs → attempt fixes → repeat until tests pass. Devin produces **~25% of Cognition's own pull requests** and achieved 13.86% on SWE-bench versus 1.96% baseline.

---

## Error handling separates production from prototype

Robust error handling distinguishes production agents from demos. Agent failures cascade—**73% of task failures stem from a single root error propagating downstream**.

**Retry strategies** must distinguish transient from permanent failures. Exponential backoff with jitter prevents thundering herds on recovery. Retry with rephrasing—maintaining multiple prompt templates for the same task—handles semantic failures. Always set maximum retry limits (typically 2-5 attempts).

**Fallback mechanisms** implement graceful degradation through tiered escalation:

| Level | Trigger | Action |
|-------|---------|--------|
| 1 | Low confidence | Alternative model |
| 2 | System unavailable | Backup agent |
| 3 | Complex query | Human transfer |
| 4 | System failure | Emergency protocols |

Model-level fallback chains (GPT-4 → GPT-4 Turbo → GPT-3.5 → Claude) should be configurable in databases for updates without code deployments.

**Error classification** spans modules: memory errors (hallucination, retrieval failure), reflection errors (incorrect self-assessment), planning errors (goal misalignment, infeasible plans), action errors (tool failures, format violations), and system errors (timeouts, dependencies). The AgentDebug framework traces backward through failure trajectories to identify root causes with **87% accuracy**, enabling up to 26% relative improvement through corrective feedback.

**Human-in-the-loop patterns** require explicit design: confidence thresholds for automation (>0.85 auto-execute, <0.5 escalate), approval workflows for high-impact actions, and capturing human corrections as training data. Implementations using durable state management (LangGraph persistence, Cloudflare Durable Objects) support long review periods.

**Recovery strategies** include checkpoint-and-resume (save state after each successful step), state rollback (revert to last checkpoint on failure), and alternative path exploration (trigger replanning when primary approach fails). The STRATUS system from IBM/UIUC achieved **150%+ improvement** on cloud engineering benchmarks using undo-and-retry mechanisms.

---

## Observability makes agents debuggable

Without comprehensive observability, agent debugging is nearly impossible. Production systems require tracing, metrics, and structured logging from day one.

**Tracing** captures the complete execution flow using spans for individual operations (LLM calls, retrievals, tool executions). Each span should record inputs/outputs, performance metrics (duration, tokens, cost), model configuration, context identifiers, and errors. OpenTelemetry-based tracing has emerged as the standard approach, with platforms like Datadog, Langfuse, LangSmith, and Arize providing visualization.

**Key metrics** include task success rates (binary completion, partial success scoring), efficiency metrics (steps, tokens, latency, API calls), and quality metrics (answer relevancy, faithfulness, hallucination detection). LLM-as-judge evaluation—using another LLM to assess output quality—scales better than human evaluation for iteration.

**Benchmark datasets** enable systematic evaluation: AgentBench (8 environments including OS, databases, web), GAIA (multi-step reasoning), WebArena (web-based tasks), ToolBench (16,000+ real APIs), and ToolEmu (safety evaluation with high-stakes tools).

**Testing strategies** span unit testing (individual components with golden datasets), integration testing (tool handoffs, retrieval pipelines), and end-to-end testing (complete user scenarios). CI/CD integration runs evaluations on every PR, tracking metric trends to detect gradual degradation. Use failure injection (tool failures, rate limits, invalid outputs) as part of the test suite.

---

## Prompt engineering for agents requires structure

Agent prompts differ from single-turn prompts in their emphasis on tool use, state management, and behavioral constraints.

**System prompt design** establishes persona, capabilities, constraints, and instruction hierarchy. Structure prompts into distinct sections using XML tags or Markdown headers: background information, instructions, tool guidance, output format. The "right altitude" principle: balance between too-specific (brittle, breaks on edge cases) and too-vague (insufficient constraint).

**Few-shot examples** demonstrate correct tool usage patterns—when to invoke, how to format parameters, how to interpret results. Research shows few-shot examples provide **2-10x improvement** in tool calling accuracy, especially for complex parameter generation. Dynamic example injection—retrieving semantically similar examples from a vector store—keeps prompts manageable while maintaining relevance.

**Structured output** ensures parseable responses. Native API support (OpenAI Structured Outputs, Anthropic tool use) guarantees schema adherence. Constrained decoding (Outlines, Microsoft Guidance) modifies token logits to only allow valid outputs. Pydantic-based validation catches and recovers from format errors.

**Instruction hierarchy** resolves conflicts: safety instructions take highest priority, followed by system-level constraints, user instructions, then default behaviors. Explicit hierarchical prompting prevents user instructions from overriding safety guidelines.

---

## Implementation recommendations for Golang agents

Building agents from scratch in Go requires translating these patterns into idiomatic implementations. Key architectural decisions:

**Core agent loop**: Implement as a state machine with explicit states (planning, executing, evaluating, complete) and guarded transitions. Go's select statements handle timeout conditions naturally. Use context.Context for cancellation and deadline propagation throughout the agent lifecycle.

**Memory management**: Short-term memory as a slice of messages with a token counting function; implement sliding window or summarization when approaching limits. Long-term memory through vector database clients (Qdrant and Milvus have Go SDKs). Consider a Memory interface that abstracts storage backends.

**Tool execution**: Define tools as interface implementations with standardized Execute methods. Use reflection or code generation for schema extraction from Go struct tags. Handle tool errors as values, not panics—return structured error types that help the agent recover.

**Concurrency**: Go's goroutines and channels map naturally to parallel agent execution. Use sync.WaitGroup for fan-out/fan-in patterns; errgroup for parallel tasks with error propagation. Be explicit about shared state—prefer passing state through channels over sharing mutable structures.

**Observability**: Use OpenTelemetry Go SDK for distributed tracing. Wrap every LLM call and tool execution in spans. Implement structured logging (zerolog, zap) with consistent field names across the agent lifecycle.

**Testing**: Table-driven tests for deterministic components; golden file tests for prompt templates. Mock LLM responses for unit tests; use recorded traces for replay testing. Integration tests against real LLM APIs with cost budgets.

The patterns in this guide provide the conceptual foundation. The implementation requires translating concepts into Go idioms while maintaining the essential architectural properties: explicit state management, comprehensive error handling, and observability from the ground up.

---

## Conclusion

The agent architecture landscape has matured significantly through 2024-2025, with clear patterns emerging from production deployments. The most important lessons are counterintuitive:

**Simplicity wins.** Claude Code's single-threaded loop outperforms elaborate multi-agent hierarchies. Start with direct LLM calls, add tools incrementally, introduce loops only when needed, and reserve multi-agent coordination for genuinely parallelizable problems.

**Tools matter more than reasoning patterns.** Giving agents access to file systems, terminals, and search—with well-designed interfaces—accounts for more capability than sophisticated reasoning architectures.

**Memory is undervalued.** Episodic memory with reflection (Reflexion pattern) enables learning without fine-tuning. Hybrid retrieval strategies combining recency, relevance, and importance outperform naive semantic search.

**Error handling is architecture.** With 73% of failures stemming from single root errors cascading downstream, robust recovery strategies aren't features—they're fundamental design requirements.

The path forward is clear: master the foundational patterns (ReAct, CoT, Plan-and-Execute), implement comprehensive observability from day one, design tools for agent consumption, and resist the temptation to add complexity before simpler approaches have demonstrably failed. The agents that work in production are architecturally boring—and that's exactly the point.