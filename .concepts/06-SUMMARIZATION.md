# How frontier AI agents compress their context windows

**The most effective agent frameworks use structured, section-based summarization prompts that explicitly enumerate what to preserve — and the simpler approach of observation masking often outperforms LLM-generated summaries.** This research covers the actual prompt text and templates from 12 major agent frameworks, academic findings from 2023–2025, and practical design recommendations for building a new summarization system. The core tension in every system is the same: summaries lose information, but unbounded context degrades performance. The best systems mitigate this through structured preservation requirements, hybrid strategies, and recovery mechanisms that let agents search their full history when summaries prove insufficient.

---

## Claude Code's compaction prompt is the gold standard for structured summarization

Claude Code's compaction system, extracted from its minified JS source by multiple independent reverse-engineering efforts, represents the most detailed production summarization prompt publicly available. It triggers at **~95% context capacity** (newer versions at ~75–80%) and uses a **7-section structured output** wrapped in XML tags.

**The actual prompt (client-side, ~1,121 tokens):**

```
Your task is to create a detailed summary of the conversation so far, paying close
attention to the user's explicit requests and your previous actions. This summary
should be thorough in capturing technical details, code patterns, and architectural
decisions that would be essential for continuing development work without losing context.

Before providing your final summary, wrap your analysis in <analysis> tags to organize
your thoughts and ensure you've covered all necessary points. In your analysis process:

1. Chronologically analyze each message and section of the conversation. For each
   section thoroughly identify:
   - The user's explicit requests and intents
   - Your approach to addressing the user's requests
   - Key decisions, technical concepts and code patterns
   - Specific details like:
     - file names
     - full code snippets
     - function signatures
     - file edits
   - Errors that you ran into and how you fixed them
   - Pay special attention to specific user feedback that you received, especially
     if the user told you to do something differently.

2. Double-check for technical accuracy and completeness, addressing each required
   element thoroughly.

Your summary should include the following sections:

Primary Request and Intent: Capture all of the user's explicit requests and intents
in detail

Key Technical Concepts: List all important technical concepts, technologies, and
frameworks discussed.

Files and Code Sections: Enumerate specific files and code sections examined, modified,
or created. Pay special attention to the most recent messages and include full code
snippets where applicable and include a summary of why this file read or edit was done.

Problem Solving: [Description of solved problems and ongoing troubleshooting]

Pending Tasks: [List of remaining tasks]

Current Work: Describe in detail precisely what was being worked on immediately before
this summary request, paying special attention to the most recent messages from both
user and assistant. Include file names and code snippets where applicable.

Optional Next Step: List the next step that you will take that is related to the most
recent work you were doing.
IMPORTANT: ensure that this step is DIRECTLY in line with the user's most recent
explicit requests, and the task you were working on immediately before this summary
request. If your last task was concluded, then only list next steps if they are
explicitly in line with the users request. Do not start on tangential requests or
really old requests that were already completed without confirming with the user first.
If there is a next step, include direct quotes from the most recent conversation
showing exactly what task you were working on and where you left off. This should be
verbatim to ensure there's no drift in task interpretation.
```

The output structure requires an `<analysis>` block (chain-of-thought reasoning), followed by a `<summary>` block with 7 numbered sections. When users add custom instructions via `/compact focus on API changes` or through `CLAUDE.md`, those instructions append to the base prompt.

**Anthropic's server-side compaction API** (beta `compact-2026-01-12`) uses a simpler default prompt:

```
You have written a partial transcript for the initial task above. Please write a
summary of the transcript. The purpose of this summary is to provide continuity so
you can continue to make progress towards solving the task in a future context, where
the raw history above may not be accessible and will be replaced with this summary.
Write down anything that would be helpful, including the state, next steps, learnings
etc. You must wrap your summary in a <summary></summary> block.
```

Key design choices in Claude Code: **recency bias** (most recent messages get disproportionate attention), **verbatim quotes** from the latest conversation to prevent task drift, **explicit anti-drift guardrails** (don't start tangential work), and a **pre-compaction pruning step** that clears older tool outputs before triggering full summarization.

---

## Aider and Codex CLI take fundamentally different approaches

**Aider's prompt** is notably concise compared to Claude Code's — just **~150 words** — but contains several clever design decisions:

```
*Briefly* summarize this partial conversation about programming.
Include less detail about older parts and more detail about the most recent messages.
Start a new paragraph every time the topic changes!

This is only part of a longer conversation so *DO NOT* conclude the summary
with language like "Finally, ...". Because the conversation continues after
the summary.
The summary *MUST* include the function names, libraries, packages that are
being discussed.
The summary *MUST* include the filenames that are being referenced by the
assistant inside the ```...``` fenced code blocks!
The summaries *MUST NOT* include ```...``` fenced code blocks!

Phrase the summary with the USER in first person, telling the ASSISTANT about
the conversation. Write *as* the user.
The user should refer to the assistant as *you*.
Start the summary with "I asked you...".
```

Aider's implementation uses a **recursive binary-split strategy**: it finds the midpoint of the conversation by token count, summarizes the older half, keeps the newer half verbatim, and recurses if the result still exceeds the budget. It uses a **weak/cheap model** for summarization (e.g., GPT-3.5 or Claude Haiku) with the main model as fallback. The summary is injected as a user-role message prefixed with `"I spoke to you previously about a number of things.\n"`, followed by an assistant message saying `"Ok."` to maintain turn-taking. Default token budgets are small: **1,024 tokens** for models with <32K context, **2,048 tokens** for larger models.

**OpenAI Codex CLI** frames compaction as a "handoff" to another LLM instance:

```
You are performing a CONTEXT CHECKPOINT COMPACTION. Create a handoff summary for
another LLM that will resume the task.

Include:
- Current progress and key decisions made
- Important context, constraints, or user preferences
- What remains to be done (clear next steps)
- Any critical data, examples, or references needed to continue

Be concise, structured, and focused on helping the next LLM seamlessly continue
the work.
```

When the summary is injected into the new context, it's preceded by this framing:

```
Another language model started to solve this problem and produced a summary of its
thinking process. You also have access to the state of the tools that were used by
that language model. Use this to build on the work that has already been done and
avoid duplicating work. Here is the summary produced by the other language model,
use the information in this summary to assist with your own analysis:
```

Codex CLI preserves the **last ~20K tokens of recent user messages** verbatim alongside the summary, and auto-triggers based on model-specific token limits (e.g., 180K or 244K tokens).

---

## LangChain and LlamaIndex provide the simplest baseline prompts

**LangChain's `ConversationSummaryMemory`** uses a progressive summarization prompt with a single few-shot example:

```
Progressively summarize the lines of conversation provided, adding onto the
previous summary returning a new summary.

EXAMPLE
Current summary:
The human asks what the AI thinks of artificial intelligence. The AI thinks
artificial intelligence is a force for good.

New lines of conversation:
Human: Why do you think artificial intelligence is a force for good?
AI: Because artificial intelligence will help humans reach their full potential.

New summary:
The human asks what the AI thinks of artificial intelligence. The AI thinks
artificial intelligence is a force for good because it will help humans reach
their full potential.
END OF EXAMPLE

Current summary:
{summary}

New lines of conversation:
{new_lines}

New summary:
```

This prompt takes two template variables — `{summary}` (running summary) and `{new_lines}` (new messages) — and produces a third-person narrative. `ConversationSummaryBufferMemory` adds a `max_token_limit` parameter: messages within the limit stay verbatim, older ones get summarized. Both are now **deprecated** in favor of LangGraph patterns.

**LlamaIndex's `ChatSummaryMemoryBuffer`** uses an even simpler single-sentence prompt:

```
The following is a conversation between the user and assistant. Write a concise
summary about the contents of this conversation.
```

This is a **one-shot** summarizer — it summarizes everything that doesn't fit in the token window at once, without progressive refinement. The summary is stored as a `SystemMessage` at the beginning of the returned chat history. Also deprecated in favor of the newer `Memory` class.

**LangGraph's recommended pattern** (from official docs) is fully custom:

```python
if summary:
    summary_message = (
        f"This is a summary of the conversation to date: {summary}\n\n"
        "Extend the summary by taking into account the new messages above:"
    )
else:
    summary_message = "Create a summary of the conversation above:"
```

The newer **`langmem` library** provides a `SummarizationNode` with three separate prompts (initial summary, existing summary extension, and final combination with remaining messages), configurable `max_summary_tokens` (default 256), and tracking of `summarized_message_ids` to avoid re-summarizing.

---

## MemGPT and SWE-agent represent opposing philosophies

**MemGPT/Letta** uses an OS-inspired tiered memory hierarchy where the LLM manages its own memory via function calls. Its summarization prompt is deliberately minimal — a system message saying `"You are a helpful assistant. Keep your responses short and concise."` plus an injected assistant acknowledgment: `"Understood, I will respond with a summary of the message (and only the summary, nothing else) once I receive the conversation history. I'm ready."` The actual intelligence lives in the architecture, not the prompt. Summaries are packaged as system alerts:

```
Note: prior messages ({hidden_count} of {total_count} total messages) have been
hidden from view due to conversation memory constraints.
The following is a summary of the previous {summary_count} messages:
{summary_text}
```

MemGPT's key insight: **recursive summarization is inherently lossy** and "eventually leads to large holes in the memory of the system." Its advantage comes from combining summarization with **explicit retrieval tools** (`conversation_search`, `archival_memory_search`) that let the agent recover specific details from full history stored in a database. The agent receives a memory pressure warning at **75% token capacity**, triggers summarization at the limit, evicts ~75% of oldest messages (keeping the last 3), and stores evicted messages in searchable recall storage.

**SWE-agent takes the opposite approach** — no LLM summarization at all. It uses deterministic **observation masking**: older tool outputs are replaced with a fixed placeholder (`"Previous {N} lines elided for brevity"`), while the agent's **reasoning traces and actions are always preserved in full**. Only the last 5–10 observations stay visible.

The JetBrains/TUM "Complexity Trap" paper (NeurIPS 2025 DL4Code) found that observation masking **outperformed LLM summarization in 4 out of 5 experimental settings** — delivering equal or better solve rates at **~50% lower cost**. LLM summarization caused **trajectory elongation** (~15% more turns) because summaries "smooth over" failure signals, causing agents to persist on unproductive paths longer.

---

## AutoGen, Cursor, and Devin each solve the problem differently

**Microsoft AutoGen's** default summary prompt is terse: `"Summarize the takeaway from the conversation. Do not add any introductory phrases. If the intended request is NOT properly addressed, please point it out."` But AutoGen's real strength is its **TransformMessages pipeline**, which stacks multiple strategies: `MessageHistoryLimiter` (keep last N messages), `MessageTokenLimiter` (enforce per-message and total token budgets), and `TextMessageCompressor` (using Microsoft's LLMLingua for non-LLM prompt compression achieving **up to 20x compression** with minimal performance loss).

**Cursor** uses a smaller "flash" model for summarization and makes **history searchable as files** after compaction — the agent gets a reference to a history file it can search to recover details missing from the summary. This is a notable innovation that mitigates information loss. No actual prompt text has leaked.

**Devin (Cognition Labs)** uses **fine-tuned smaller models** specifically trained for compression rather than generic LLM prompting. Their blog reveals that relying on the model's own notes without their proprietary compaction systems caused "performance degradation and gaps in specific knowledge: the model didn't know what it didn't know." They also discovered that models exhibit **"context anxiety"** near context limits, proactively summarizing and taking shortcuts. Their mitigation: enable the 1M token beta but cap actual usage at 200K, so the model "thinks it has plenty of runway."

**OpenHands** (formerly OpenDevin) provides an `LLMSummarizingCondenser` that triggers when history exceeds `max_size` events, always keeps the first `keep_first` events (system prompts, initial user messages), and replaces dropped events with an LLM-generated summary wrapped in `<PREVIOUS SUMMARY>` XML tags. Their evaluation on SWE-bench Verified showed **54% solve rate** with condensation vs. 53% baseline — a slight improvement — while achieving **2x per-turn API cost reduction** and transforming cost scaling from quadratic to linear.

---

## What the academic literature says about optimal summarization

Five key papers from 2023–2025 provide empirical foundations:

- **"The Complexity Trap" (Lindenbauer et al., NeurIPS 2025)**: First systematic comparison of observation masking vs. LLM summarization. Found masking cheaper and equally effective. Recommends a **hybrid approach**: default to observation masking, trigger LLM summarization only occasionally after accumulating many turns.

- **MemGPT (Packer et al., 2023)**: Proved that recursive summarization alone is lossy; the key is combining summarization with retrieval tools for recovering lost details.

- **ReSum (Wu et al., 2025)**: Fine-tuned a 30B model specifically for goal-oriented summarization in web search agents. Critical finding: **do not ask summaries to list information gaps or action plans** — this traps agents in self-verification loops. Achieved +4.5% improvement over vanilla ReAct.

- **MEM1 (Zhou et al., NeurIPS 2025 Workshop)**: Trained via RL to maintain a compact "internal state" that unifies memory consolidation and reasoning. Achieved **3.5x performance improvement with 3.7x memory reduction** vs. larger models — the most aggressive approach, completely replacing history with a learned compressed state.

- **LLMLingua series (Microsoft Research, 2023–2024)**: Token-level prompt compression using small model perplexity scores. Achieves **up to 20x compression with only 1.5% performance loss**, running 3–6x faster than LLM-based summarization. Integrated into both LangChain and AutoGen.

---

## What the best systems preserve and what they discard

Across all frameworks, a consistent hierarchy emerges for what information to preserve:

**Always preserve** (consensus across Claude Code, Codex CLI, OpenHands, and Anthropic's engineering guide): the user's original goals and explicit requests, current progress and status, architectural and design decisions with rationale, unresolved bugs and error states, **exact file paths, variable names, and function signatures** (these compress poorly and are critical for coding agents), failing tests and their error messages, and key technical constraints or discovered limitations.

**Preserve with recency bias** (Aider, Claude Code): recent messages get more detail than older ones. Claude Code explicitly instructs to "pay special attention to the most recent messages." Aider says "include less detail about older parts and more detail about the most recent messages."

**Safe to discard** (OpenHands, SWE-agent): verbose raw tool outputs (raw file contents, long test logs), intermediate chain-of-thought reasoning (keep conclusions only), redundant information repeated across turns, and failed retry attempts (SWE-agent removes these entirely from history).

**Explicitly warned against**: starting tangential work not aligned with the user's latest request (Claude Code), listing information gaps or next-step plans in the summary itself (ReSum), concluding the summary as if the conversation is over (Aider), and including fenced code blocks in summaries (Aider — they waste tokens and the LLM should reference files instead).

---

## Design recommendations for a new summarization system

Based on this research, a Go-based agent framework should implement a **three-tier context management strategy**:

**Tier 1 — Tool output pruning (cheapest, trigger first).** Replace older tool call results with placeholders like `[output from {tool_name} omitted — {N} tokens]`. Anthropic calls this "low-hanging fruit." Once a tool result has been processed, the raw output rarely needs to remain in context. This alone can reclaim 40–60% of context in tool-heavy conversations.

**Tier 2 — Observation masking with a rolling window.** Keep the last 10 full turns (reasoning + actions + observations). For older turns, keep reasoning and actions but replace observations with one-line placeholders. This is the approach SWE-agent uses and which the JetBrains study found competitive with full LLM summarization at half the cost.

**Tier 3 — LLM summarization (most expensive, trigger last).** When tiers 1 and 2 are insufficient, generate a structured summary. Use a prompt that combines Claude Code's structured output format with Codex CLI's "handoff" framing:

```
You are compacting this conversation to create a continuation checkpoint. Another
instance will resume this work using only your summary plus recent messages.

Before writing your summary, analyze the conversation chronologically in <analysis>
tags, identifying: user requests, decisions made, errors encountered, and current
state.

Then provide your summary in <summary> tags with these sections:

1. GOAL: The user's primary request and intent (verbatim quotes for recent requests)
2. PROGRESS: What has been accomplished, with specific file paths and key code changes
3. CURRENT STATE: What was being worked on immediately before this checkpoint, with
   exact file names and relevant code snippets
4. ERRORS & FIXES: Problems encountered and how they were resolved
5. CONSTRAINTS: User preferences, technical requirements, and key design decisions
6. REMAINING WORK: Pending tasks, ordered by priority
7. NEXT STEP: The single most immediate action to take, with verbatim quotes from
   recent messages showing the exact task context

RULES:
- Preserve exact file paths, function names, variable names, and error messages
- Include more detail for recent work, less for older completed work
- Do NOT include raw code blocks unless they represent critical current state
- Do NOT plan beyond what the user has explicitly requested
- Do NOT conclude as if the conversation is over — work continues after this summary
```

**Additional architectural recommendations**: use a **cheaper/smaller model** for summarization (Aider, Cursor, and Devin all do this), implement **progressive summarization** where each compaction incorporates the previous summary rather than re-summarizing everything, keep the summary output as a **system message** or clearly labeled synthetic message (not mixed into conversation turns), provide a **recovery mechanism** (like Cursor's searchable history files or MemGPT's recall storage) so agents can retrieve details lost during compaction, and trigger compaction at **~80% context capacity** rather than 95% to leave headroom for the compaction process itself.

---

## Conclusion

The landscape of context summarization reveals a clear design spectrum. At one end, **observation masking** (SWE-agent) is simple, cheap, and empirically strong — it should be the default first strategy. At the other end, **fine-tuned compression models** (Devin) and **RL-trained memory consolidation** (MEM1) represent the frontier but require significant training investment. The practical sweet spot for a new framework is the **hybrid approach**: cheap deterministic pruning first, structured LLM summarization second, with a recovery mechanism as safety net. The single most impactful design decision is **what the prompt tells the model to preserve** — and the evidence strongly favors explicit, section-based structured output over free-form prose summaries. Claude Code's 7-section format with chain-of-thought analysis before the summary, combined with Codex CLI's "handoff" framing that tells the model another instance will continue the work, produces the highest-quality continuations.