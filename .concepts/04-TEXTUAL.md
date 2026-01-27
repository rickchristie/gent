# Textual representations of non-linear structures for LLMs

The core insight from examining Tree-of-Thoughts, Graph-of-Thoughts, and major agent frameworks is counterintuitive: **LLMs rarely see explicit graph or tree serializations**. Instead, these systems manage structure programmatically and present only linearized "views" to the model—current state, relevant thoughts, or specific frontier nodes. The choice of encoding method can boost performance by **4.8% to 61.8%**, making serialization format one of the highest-leverage optimizations in agent architectures.

## Tree-of-Thoughts uses state concatenation, not tree serialization

The original Princeton-NLP implementation of Tree-of-Thoughts reveals a fundamental pattern: the tree exists only in code, while the LLM receives plain text strings joined by newlines. When generating thoughts for the Game of 24 problem, the model sees this format:

```
Input: 2 8 8 14
Possible next steps:
2 + 8 = 10 (left: 8 10 14)
8 / 2 = 4 (left: 4 8 14)
14 + 2 = 16 (left: 8 8 16)
Input: {remaining_numbers}
Possible next steps:
```

The "tree position" is communicated implicitly through accumulated state—if a node at depth 3 has thoughts "A → B → C", the prompt contains `"A\nB\nC"`. The LLM has no awareness of sibling branches or the broader tree topology. Evaluation uses separate prompts where candidates appear as numbered choices:

```
Given an instruction and several choices, decide which choice is most promising.
Choice 1: [plan text]
Choice 2: [plan text]
Choice 3: [plan text]
The best choice is {s}
```

LangChain's experimental ToT implementation uses **Jinja2 templates** with a similar approach:

```jinja2
PROBLEM: {{problem_description}}
{% if thoughts %}
THOUGHTS
{% for thought in thoughts %}
{{ thought }}
{% endfor %}
{% endif %}
Generate exactly {{n}} possible next thoughts as a JSON list of strings.
```

The tree data structure (`TreeNode` with `state`, `thought`, `value`, `children`) lives entirely in Python, with BFS/DFS traversal controlled programmatically. Modern implementations using LangGraph request structured output via Pydantic schemas (`llm.with_structured_output(GuessEquations)`) rather than parsing free text.

## Graph-of-Thoughts serializes only active frontiers and merge inputs

Besta et al.'s Graph-of-Thoughts extends beyond trees by enabling **aggregation** (merging multiple thoughts) and **refinement** (self-loops). The Graph Reasoning State (GRS) maintains all thoughts, scores, and dependencies—but critically, only relevant portions are serialized into prompts.

For generation operations, prompts use XML-style delimiters with JSON output specifications:

```
<Instruction> Split the following list of 64 numbers into 4 lists of 16 
numbers each... Only output the final 4 lists in the following format:
{{
  "List 1": [3, 4, 3, 5, ...],
  "List 2": [2, 9, 2, 4, ...],
  ...
}}
</Instruction>
<Example>
Input: [3, 1, 9, 3, 7, ...]
Output: {{ "List 1": [...], ... }}
</Example>
Input: {input}
```

The key differentiator—**aggregation**—includes multiple parent thoughts as numbered inputs:

```
<Instruction> Merge the following 2 sorted lists into one sorted list 
using a merge sort style approach.
</Instruction>
<Approach>
1. Compare the first element of both lists.
2. Append the smaller element...
</Approach>
Merge these lists:
1: {input_list1}
2: {input_list2}
Merged list:
```

Edges are implicit: the merge prompt contains content from both parent nodes, but no explicit edge notation. The controller outputs graph structure as JSON for debugging:

```json
{
  "thoughts": [{"id": 1, "state": {...}, "score": 0.95}],
  "edges": [[0, 1], [1, 3]],
  "total_tokens": 12345
}
```

This demonstrates a key principle: **graphs are for orchestration, not for LLM consumption**. The LLM sees linearized views; the graph topology guides which views to present.

## LLMCompiler represents DAGs with variable references

For parallel task execution, LLMCompiler uses a remarkably concise format—numbered tasks with `$N` dependency notation:

```
1. search("What is Microsoft's market cap?")
2. search("What is Apple's market cap?")  
3. calculate($1, $2)  # Depends on outputs of tasks 1 and 2
4. join($3)
<END_OF_PLAN>
```

The prompt instructs the LLM to maximize parallelism while respecting dependencies:

```
Each action MUST have a unique ID, which is strictly increasing.
Inputs can be constants or outputs from preceding actions—use $id format.
Ensure the plan maximizes parallelizability.
```

Internally, this parses to explicit dependency sets:

```python
{"idx": 3, "tool": calculate, "args": ("$1", "$2"), "dependencies": {1, 2}}
```

Alternative representations found in agent-patterns documentation use JSON with explicit `depends_on` arrays, while YAML-based workflow definitions use fork/join blocks for parallel execution:

```yaml
workflow:
  - id: fetch_data
    parallel:
      - task: fetch_sales_q1
      - task: fetch_sales_q2
    join: aggregate_results
```

## Framework state serialization is for persistence, not prompting

**LangGraph** uses `JsonPlusSerializer` (msgpack by default) for checkpointing:

```python
checkpoint = {
    "ts": "2024-05-04T06:32:42.235444+00:00",
    "id": "1ef4f797-8335-6428-8001-8a1503f9b875",
    "channel_values": {"messages": [...], "state": {...}},
    "channel_versions": {"messages": "3"},
    "versions_seen": {"chatbot": {"messages": "2"}}
}
```

**LlamaIndex** workflows use a JSON serializer that preserves type information:

```python
{"__is_pydantic": True, "value": {...}, "qualified_name": "mymodule.RunState"}
```

**AutoGen** serializes components with provider paths and versions:

```json
{
  "provider": "autogen_agentchat.agents.AssistantAgent",
  "component_type": "agent",
  "version": 1,
  "config": {"name": "assistant", "model_client": {...}}
}
```

These formats enable state persistence and component rehydration—they're not sent to LLMs for reasoning. The actual prompts these frameworks generate are typically conversation histories with system messages.

## Coding agents compress file trees using AST-derived summaries

Aider's repository map demonstrates sophisticated structural compression. Rather than raw file trees, it uses tree-sitter to extract hierarchical signatures:

```
aider/coders/base_coder.py:
⋮...
│class Coder:
│    abs_fnames = None
⋮...
│    @classmethod
│    def create(self, main_model, edit_format, ...
⋮...
│    def run(self, with_message=None):
⋮...
```

The `⋮...` markers indicate elided code, and a graph-ranking algorithm selects which signatures to include within token budgets (default 1k tokens). Cursor uses **vector embeddings** for codebase-wide semantic retrieval rather than serializing explicit dependency graphs.

## Research reveals encoding choice dramatically affects performance

The "Talk like a Graph" study (Google Research, ICLR 2024) compared graph encoding methods:

| Encoding Method | Example | Best Use Case |
|----------------|---------|---------------|
| **Incident** (node-centric) | "Node A connects to: B, C, D" | Most tasks; shortest context |
| Adjacency | "Edges: (A,B), (A,C), (A,D)" | When edge enumeration needed |
| Natural language | "A and B are friends" | Contextual understanding |

For finding connected nodes, incident encoding achieved **53.8% accuracy** versus adjacency's **19.8%**—a 34-point difference from encoding alone.

The NLGraph benchmark (NeurIPS 2023) found LLMs demonstrate preliminary graph reasoning (**37-58% above random** on simple tasks) but performance degrades sharply on complex problems. GraphArena (ICLR 2025) revealed severe hallucination issues: models produce structurally plausible but contextually wrong responses, with smaller models hallucinating up to **91.2%** of the time.

## Format restrictions hurt reasoning performance

Critical research from "Let Me Speak Freely?" (2024) demonstrates that **stricter format constraints degrade reasoning**:

| Constraint Level | Impact |
|-----------------|--------|
| JSON-mode (strictest) | Worst reasoning performance |
| Format-Restricting Instructions | Moderate degradation |
| NL-to-Format (two-step) | Minimal impact |
| Natural Language | Best reasoning |

For nested data, comparative studies found:
- **YAML**: Best overall (17.7% higher than XML)
- **Markdown**: Close second, 10% fewer tokens than YAML  
- **JSON**: Moderate; token-heavy
- **XML**: Worst performer, 80% more tokens than Markdown

However, for tabular data, **HTML with format explanations** achieved the highest accuracy (65.43%)—likely due to training data exposure.

## Positional bias compounds structural understanding challenges

The "Lost in the Middle" phenomenon (Liu et al., TACL 2024) shows a **U-shaped performance curve**: models perform best when relevant information appears at context **beginning or end**, with **30%+ degradation** for middle positions.

For graph structures specifically, "Lost-in-Distance" research found that performance degrades based on both textual position AND the **relative distance between related elements** in the serialized encoding. If two nodes that share an edge are serialized far apart in the text, the model struggles to recognize their connection.

LLMs are also effectively "spatially blind"—the ArtPrompt study found GPT-4 achieves only **25.19% accuracy** on ASCII art recognition due to sequential tokenization destroying 2D spatial relationships.

## Practical serialization recommendations

Based on this research synthesis, the following patterns emerge for representing non-linear structures:

- **For tree traversal**: Use plain text state concatenation, not explicit tree serialization. Manage tree topology in code; present current path to LLM.

- **For graph operations**: Serialize only relevant frontier nodes. For aggregation, include parent contents as numbered inputs. Use XML-style delimiters (`<Instruction>`, `<Example>`) for prompt structure.

- **For DAGs/parallel execution**: Use numbered task lists with `$N` variable references. The notation `calculate($1, $2)` is both human-readable and LLM-parseable.

- **For state machines**: Represent current state and valid transitions; the LLM doesn't need the full automaton structure.

- **For knowledge graphs in RAG**: Use incident encoding (node-centric connection listing) or entity-triplet format. Place critical triples at context boundaries.

- **Format selection**: Prefer YAML or Markdown for nested structures. Use two-step generation (reason in natural language, then format) for complex reasoning. Reserve strict JSON mode for classification or extraction tasks.

- **Position optimization**: Place critical structural information at the beginning or end of prompts. For multi-hop graph queries, order the serialization to minimize textual distance between related nodes.

## Conclusion

The central finding is architectural: **non-linear structures in agent systems are orchestration scaffolds, not LLM inputs**. Trees and graphs guide what context to present, but the LLM receives linearized views—current state, relevant thoughts, or task dependencies in `$N` notation. The sophistication lies in what the orchestration layer chooses to serialize and where it places that information in the context window.

Format choice remains high-leverage: incident encoding over adjacency for graphs, YAML over XML for nested data, and minimizing format constraints for reasoning tasks. The "lost in the middle" effect applies to structural representations, making positional engineering essential. Future architectures may benefit from explicit structural encodings or graph neural network integration, but current best practices involve strategic linearization rather than faithful graph serialization.