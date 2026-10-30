"""Build one offline lesson file from the current Gent source.

Read interface declarations from Go so the guide cannot keep old method signatures by accident.
The HTML has all styles, scripts, examples, and source references needed for sharing.
"""

import hashlib
import html
import json
import re
import subprocess
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent.parent.parent
OUTPUT = HERE.parent / "dist/gent-policy-search-lessons.html"

# Each entry explains when this interface matters in this specific application.
CONTRACTS = [
    ("Loop", "agent.go", "Run", "Use react.Agent",
     "Runs one model-and-tool iteration. The runner calls Next until the loop stops.",
     "Use react.NewAgent(model). You do not need to write a loop for policy retrieval."),
    ("LoopData", "agent.go", "Run", "Use BasicLoopData",
     "Supplies the initial Task. ExecutionContext owns history and the active selection.",
     "Use gent.NewBasicLoopData. Add custom data only if the application needs it."),
    ("Model", "model.go", "Run", "Supply an adapter",
     "Converts a Gent request into a provider call and returns a stream.",
     "Use a provider adapter that supports native tool calls. ReAct uses managed model calls."),
    ("Stream", "model.go", "Run", "Adapter boundary",
     "Supplies chunks, the final response, and cancellation.",
     "An adapter must finish its producer before the final response completes."),
    ("Tool", "tool.go", "Tools", "Use PolicySearchTool",
     "Defines typed input, schema, execution, and text output for one operation.",
     "PolicySearchTool implements Tool[PolicySearchInput, string]. Its result is Markdown text."),
    ("ToolChain", "toolchain.go", "Tools", "Use Registry",
     "Selects native definitions, prepares a saved plan, and executes one call at a time.",
     "Use toolchain.New(). Register the policy tool before you share the runner."),
    ("SearchIndex", "search/search.go", "Search", "Built by the policy tool",
     "Stores documents and returns ranked SearchResult values. Implementations must be concurrent.",
     "BleveIndex, FlatIndex, and FusedIndex implement SearchIndex[*policy.Policy]."),
    ("BleveAdapter", "search/bleve_adapter.go", "Search", "Use PolicyBleveAdapter",
     "Defines fields, document conversion, and the full-text query.",
     "Policy fields use boosts of 10 for ID, 3 for keywords, and 1 for body text."),
    ("BleveIDFProvider", "search/bleve_adapter.go", "Search", "Optional normalization metadata",
     "Supplies the text fields and analyzer used for the theoretical BM25 maximum.",
     "PolicyBleveAdapter supplies this. Without maximum metadata, raw scores pass through."),
    ("ChunkAdapter", "search/flat_adapter.go", "Search", "Use PolicyChunkAdapter",
     "Converts each policy into token-aware chunks before vector embedding.",
     "The policy adapter uses Markdown headings and adds a separate synthetic-query chunk."),
    ("TokenCounter", "search/embedder.go", "Search", "Part of Embedder",
     "Counts tokens with the tokenizer used by the embedding model.",
     "A character count is not a replacement for the model tokenizer in production chunking."),
    ("Embedder", "search/embedder.go", "Search", "Supply and own one",
     "Embeds queries and text. It also supplies token counts, dimensions, and resource cleanup.",
     "Keep it open across searches. Close it after all calls finish. Custom embedders work too."),
    ("Fuser", "search/fuser.go", "Search", "Use WeightedLinearFuser",
     "Combines named result lists into a single ranked list.",
     "The policy helper uses 0.4 BM25 plus 0.6 semantic. A missing source contributes zero."),
    ("Output", "output.go", "Answer", "Use output.TextOutput",
     "Processes ordered content and returns acceptance, a typed value, or correction steering.",
     "Native completion identifies the candidate. Local checks and custom validation decide acceptance."),
    ("TextFormat", "format.go", "Tools", "Tool result renderer",
     "Renders named sections in tool observations. It does not parse model answers.",
     "The caller supplies system instructions and user content in Task messages."),
    ("IndexableTool", "tool_search.go", "Tools", "Part of Tool",
     "Adds domain, categories, keywords, and example queries to a tool definition.",
     "Tool includes these methods. Registry requires them. Empty metadata is valid."),
    ("ToolSearcher", "tool_search.go", "Optional", "For tool discovery",
     "Searches registered tool definitions and returns ranked tool names.",
     "This is different from searching policy documents. Add SearchChain only when needed."),
    ("Recorder", "run.go", "Optional", "For durable runs",
     "Accepts a SaveBatch containing run evidence and checkpoints.",
     "Use the history storage contract for worker recovery. The demo uses the default store."),
]


def contract(item):
    name, path, group, use, summary, advice = item
    source = (ROOT / path).read_text()
    match = re.search(r"^type " + name + r"(?:\[.*?\])? interface \{.*?^}", source, re.M | re.S)
    if not match:
        raise ValueError(f"Interface not found: {path}: {name}")
    lines = [line for line in match[0].splitlines() if not line.lstrip().startswith("//")]
    signature = "\n".join(line for line in lines if line.strip())
    return dict(name=name, path=path, group=group, use=use, summary=summary, advice=advice,
                signature=signature, line=source[:match.start()].count("\n") + 1)


def script_data(value):
    """Keep embedded data readable and below the source line limit without runtime loading."""
    def wrap_string(match):
        parts = []
        part = ""
        for character in json.loads(match[0]):
            encoded = json.dumps(character, ensure_ascii=False)[1:-1].replace("<", "\\u003c")
            if len(part) + len(encoded) > 74:
                parts.append('"' + part + '"')
                part = ""
            part += encoded
        parts.append('"' + part + '"')
        return " +\n        ".join(parts)

    return re.sub(r'"(?:\\.|[^"\\])*"', wrap_string,
                  json.dumps(value, ensure_ascii=False, indent=2))


files = sorted({item[1] for item in CONTRACTS} | {
    "policy/policy.go", "policy/search.go", "search/normalize.go", "search/fused_index.go",
    "search/flat_index.go", "search/bleve_index.go", "agents/react/native.go",
    "agents/react/agent.go", "executor/executor.go", "limit.go", "search/README.md",
    "output/text.go", "toolchain/registry.go", "toolchain/native.go", "toolchain/output.go",
})
digest = hashlib.sha256()
for name in files:
    digest.update(name.encode())
    digest.update((ROOT / name).read_bytes())
data = {
    "contracts": [contract(item) for item in CONTRACTS],
    "revision": subprocess.check_output(["git", "rev-parse", "--short", "HEAD"], cwd=ROOT,
                                        text=True).strip(),
    "digest": digest.hexdigest()[:16],
    "sources": files,
    "example": (HERE / "example/main.go").read_text(),
}
template = (HERE / "template.html").read_text()
template = template.replace("@@CSS@@", (HERE / "lesson.css").read_text())
json_data = script_data(data)
template = template.replace("@@DATA@@", json_data)
template = template.replace("@@JS@@", (HERE / "lesson.js").read_text())
template = template.replace("@@EXAMPLE@@", html.escape(data["example"], quote=False))
if re.search(r"@@[A-Z]+@@", template):
    raise ValueError("Unfilled template slot")
OUTPUT.parent.mkdir(exist_ok=True)
OUTPUT.write_text(template)
print(f"Built {OUTPUT.relative_to(ROOT)} ({len(template.encode()):,} bytes)")
