"use strict";

// Keep every lesson local so the shared file has no runtime dependencies.
const data = lessonData;
const byId = (id) => document.getElementById(id);
const all = (selector) => [...document.querySelectorAll(selector)];
const escapeHtml = (text) =>
  String(text).replace(
    /[&<>"']/g,
    (char) =>
      ({
        "&": "&amp;",
        "<": "&lt;",
        ">": "&gt;",
        '"': "&quot;",
        "'": "&#39;",
      })[char],
  );
const pages = ["start", "contracts", "documents", "ranking", "loop", "code", "operation", "check"];
const storageKey = "gent-policy-search-lessons-v1";
let complete = new Set();
let currentPage = "start";
let storageAvailable = true;

try {
  const saved = JSON.parse(localStorage.getItem(storageKey) || "[]");
  if (!Array.isArray(saved)) throw new Error("Invalid saved progress");
  complete = new Set(saved.filter((id) => pages.includes(id)));
} catch (error) {
  storageAvailable = false;
  byId("save-state").textContent = `Progress is session-only: ${error.message}`;
}

function saveProgress() {
  if (!storageAvailable) return;
  try {
    localStorage.setItem(storageKey, JSON.stringify([...complete]));
  } catch (error) {
    storageAvailable = false;
    byId("save-state").textContent = `Progress is session-only: ${error.message}`;
  }
}

function showProgress() {
  byId("progress").value = complete.size;
  byId("progress-text").textContent = `${complete.size} / 8`;
  all("nav [data-page]").forEach((link) => {
    const done = complete.has(link.dataset.page);
    link.querySelector("b").textContent = done ? "✓" : "";
    link.querySelector("b").setAttribute("aria-label", done ? "Complete" : "");
  });
  byId("complete-lesson").textContent = complete.has(currentPage)
    ? "Completed ✓"
    : "Mark as complete";
  byId("complete-lesson").setAttribute("aria-pressed", String(complete.has(currentPage)));
}

function showPage(moveFocus = false) {
  const target = location.hash.slice(1);
  currentPage = pages.includes(target) ? target : "start";
  all(".lesson").forEach((section) => {
    section.hidden = section.id !== currentPage;
  });
  all("nav [data-page]").forEach((link) => {
    if (link.dataset.page === currentPage) link.setAttribute("aria-current", "page");
    else link.removeAttribute("aria-current");
  });
  const index = pages.indexOf(currentPage);
  byId("previous-lesson").disabled = index === 0;
  byId("next-lesson").disabled = index === pages.length - 1;
  byId("next-lesson").textContent = index === pages.length - 1 ? "End of lessons" : "Next lesson →";
  showProgress();
  if (moveFocus) {
    byId("main").focus({ preventScroll: true });
    window.scrollTo({ top: 0, behavior: "instant" });
  }
}

byId("previous-lesson").onclick = () => {
  location.hash = pages[pages.indexOf(currentPage) - 1];
};
byId("next-lesson").onclick = () => {
  location.hash = pages[pages.indexOf(currentPage) + 1];
};
byId("complete-lesson").onclick = () => {
  if (complete.has(currentPage)) complete.delete(currentPage);
  else complete.add(currentPage);
  saveProgress();
  showProgress();
};
byId("reset-progress").onclick = () => {
  complete.clear();
  saveProgress();
  showProgress();
};
window.addEventListener("hashchange", () => showPage(true));
byId("print").onclick = () => window.print();

const mapItems = {
  runner: [
    "Runner owns the run",
    "It publishes lifecycle events, calls Loop.Next, checks cancellation, " +
      "and commits the final result. It does not choose a policy.",
    "Loop",
  ],
  loop: [
    "ReAct connects model decisions to tools",
    "It builds messages, sends native definitions, " +
      "executes calls, and checks final content through Output.",
    "Loop",
  ],
  model: [
    "Model is the provider boundary",
    "It receives Gent messages and tool definitions. " +
      "Its stream supplies content, native calls, and usage to the managed execution context.",
    "Model",
  ],
  registry: [
    "Registry handles the tool contract",
    "It validates arguments, converts typed input, " +
      "runs hooks, and preserves tool results. Register the policy tool before runs start.",
    "ToolChain",
  ],
  policy: [
    "PolicySearchTool is a normal typed tool",
    "It receives a query and returns Markdown. " +
      "Its constructor builds the BM25 index, vector index, and 40/60 fuser for you.",
    "Tool",
  ],
  index: [
    "FusedIndex combines two result lists",
    "BleveIndex finds text matches. FlatIndex finds " +
      "the best vector chunk for each policy. WeightedLinearFuser merges scores by policy ID.",
    "Fuser",
  ],
};

function selectMap(id) {
  const [title, text, name] = mapItems[id];
  all("[data-node]").forEach((button) => {
    button.setAttribute("aria-pressed", String(button.dataset.node === id));
  });
  byId("map-detail").innerHTML = `<h3>${title}</h3><p>${text}</p>
    <button class="text-button" data-inspect="${name}">Open ${name} contract →</button>`;
}
all("[data-node]").forEach((button) => {
  button.onclick = () => selectMap(button.dataset.node);
});
byId("map-detail").onclick = (event) => {
  const button = event.target.closest("[data-inspect]");
  if (!button) return;
  byId("contract-search").value = "";
  byId("contract-group").value = "All";
  selectedContract = button.dataset.inspect;
  renderContracts();
  location.hash = "contracts";
};

let selectedContract = "Loop";
function renderContracts() {
  const query = byId("contract-search").value.trim().toLowerCase();
  const group = byId("contract-group").value;
  const results = data.contracts.filter((item) => {
    return (
      (group === "All" || item.group === group) &&
      `${item.name} ${item.summary} ${item.advice}`.toLowerCase().includes(query)
    );
  });
  byId("contract-count").textContent = `${results.length} interfaces`;
  if (!results.some((item) => item.name === selectedContract)) selectedContract = results[0]?.name;
  byId("contract-list").innerHTML = results
    .map(
      (item) => `<button data-contract="${item.name}"
    aria-pressed="${item.name === selectedContract}"><span>${item.name}</span>
    <small>${escapeHtml(item.use)}</small></button>`,
    )
    .join("");
  if (!results.length) {
    byId("contract-detail").innerHTML =
      "<h2>No matching interface</h2><p>Use a shorter query or select All.</p>";
    return;
  }
  const item = results.find((entry) => entry.name === selectedContract);
  byId("contract-detail").innerHTML = `<span class="tag">${item.group.toUpperCase()}</span>
    <h2>${item.name}</h2><p>${escapeHtml(item.summary)}</p>
    <pre><code>${escapeHtml(item.signature)}</code></pre>
    <p class="contract-use"><strong>${escapeHtml(item.use)}.</strong> ${escapeHtml(item.advice)}</p>
    <div class="tiny">Source: ${item.path}:${item.line} · exact declaration</div>`;
}
byId("contract-search").oninput = renderContracts;
byId("contract-group").onchange = renderContracts;
byId("contract-list").onclick = (event) => {
  const button = event.target.closest("[data-contract]");
  if (!button) return;
  selectedContract = button.dataset.contract;
  renderContracts();
  byId("contract-list").querySelector(`[data-contract="${selectedContract}"]`).focus();
};

const policyFields = ["id", "description", "body", "keywords", "queries"];
const initialPolicy = Object.fromEntries(
  policyFields.map((name) => [name, byId(`policy-${name}`).value]),
);
function renderPolicy() {
  const value = Object.fromEntries(
    policyFields.map((name) => [name, byId(`policy-${name}`).value]),
  );
  const keywords = value.keywords
    .split(",")
    .map((word) => word.trim())
    .filter(Boolean);
  const queries = value.queries
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean);
  byId("bm25-document").textContent = JSON.stringify(
    {
      id: value.id,
      keywords: keywords.join(" "),
      fullcontent: value.body,
    },
    null,
    2,
  );
  byId("vector-document").textContent = `# ${value.id}

${value.body}`;
  byId("vector-queries").textContent = queries.length
    ? `# ${value.id}

${queries.join(String.fromCharCode(10))}`
    : "No synthetic-query chunk.";
  byId("policy-warning").textContent = value.id.trim()
    ? "Preview only. Use a unique ID and keep the rule text complete."
    : "Add an ID so each result can refer to one policy.";
}
policyFields.forEach((name) => {
  byId(`policy-${name}`).oninput = renderPolicy;
});
byId("reset-policy").onclick = () => {
  policyFields.forEach((name) => {
    byId(`policy-${name}`).value = initialPolicy[name];
  });
  renderPolicy();
};

const scoreData = {
  intent: [
    ["return-window", 1, 0.92],
    ["order-cancel", 7, 0.48],
    ["damaged-delivery", 0, 0.58],
    ["warranty", 0, 0.3],
  ],
  keyword: [
    ["return-window", 8, 0.85],
    ["order-cancel", 1, 0.35],
    ["damaged-delivery", 2, 0.6],
    ["warranty", 0.4, 0.28],
  ],
  id: [
    ["return-window", 10, 0.73],
    ["order-cancel", 0, 0.2],
    ["damaged-delivery", 0, 0.29],
    ["warranty", 0, 0.19],
  ],
};
let selectedPolicy = "return-window";
function renderRanking() {
  const weight = Number(byId("bm25-weight").value) / 100;
  const topK = Number(byId("top-k").value);
  const rows = scoreData[byId("query-preset").value]
    .map(([id, raw, semantic]) => {
      const normalized = Math.min(raw / 10, 1);
      const bm = weight * normalized;
      const vector = (1 - weight) * semantic;
      return { id, raw, semantic, normalized, bm, vector, score: bm + vector };
    })
    .sort((left, right) => right.score - left.score || left.id.localeCompare(right.id))
    .slice(0, topK);
  if (!rows.some((row) => row.id === selectedPolicy)) selectedPolicy = rows[0].id;
  const weights = `${Math.round(weight * 100)}% BM25 / ${Math.round((1 - weight) * 100)}% vector`;
  byId("weight-label").textContent = weights;
  byId("bm25-weight").setAttribute("aria-valuetext", weights);
  byId("formula").innerHTML =
    `score = <span class="bm-text">${weight.toFixed(2)} × BM25 normalized</span>
    + <span class="vector-text">${(1 - weight).toFixed(2)} × cosine</span>`;
  byId("ranking-rows").innerHTML = rows
    .map(
      (row, index) => `<tr
    class="${row.id === selectedPolicy ? "selected" : ""}"><td><button data-policy="${row.id}"
    aria-pressed="${row.id === selectedPolicy}">${index + 1}. ${row.id}</button></td>
    <td>${row.raw ? row.raw.toFixed(2) : "—"}</td><td>${row.normalized.toFixed(2)}</td>
    <td>${row.semantic.toFixed(2)}</td><td><div class="score-bar" role="img"
    aria-label="BM25 ${row.bm.toFixed(3)} plus vector ${row.vector.toFixed(3)}">
    <span class="score-bm" style="width:${row.bm * 100}%"></span>
    <span class="score-vector" style="width:${row.vector * 100}%"></span></div></td>
    <td><strong>${row.score.toFixed(3)}</strong></td></tr>`,
    )
    .join("");
  const row = rows.find((item) => item.id === selectedPolicy);
  const winner = row.bm > row.vector ? "BM25" : "vector";
  let snippet = `The ${winner} source has the larger contribution and supplies the fused snippet.`;
  if (row.bm === row.vector) {
    snippet =
      row.bm === 0
        ? "Neither source has a positive contribution, so the fuser leaves this snippet empty."
        : "The contributions tie. The Go fuser does not define which snippet wins this tie.";
  }
  byId("rank-detail").innerHTML = `<h3>${row.id}: ${row.score.toFixed(3)}</h3>
    <p><code>${weight.toFixed(2)} × (${row.raw.toFixed(2)} / 10) +
    ${(1 - weight).toFixed(2)} × ${row.semantic.toFixed(2)} = ${row.score.toFixed(3)}</code></p>
    <p>${
      row.raw
        ? "This policy appears in both result lists."
        : "This policy is absent from the BM25 list, so that contribution is zero."
    }
    ${snippet}
    Full result mode still returns the complete policy body.</p>`;
}
byId("query-preset").onchange = renderRanking;
byId("top-k").onchange = renderRanking;
byId("bm25-weight").oninput = renderRanking;
byId("ranking-rows").onclick = (event) => {
  const button = event.target.closest("[data-policy]");
  if (!button) return;
  selectedPolicy = button.dataset.policy;
  renderRanking();
  byId("ranking-rows").querySelector(`[data-policy="${selectedPolicy}"]`).focus();
};
byId("reset-ranking").onclick = () => {
  byId("bm25-weight").value = "40";
  byId("top-k").value = "3";
  byId("query-preset").value = "intent";
  selectedPolicy = "return-window";
  renderRanking();
};

function step(owner, title, description, iteration, tools, codeLabel, code) {
  return { owner, title, description, iteration, tools, codeLabel, code };
}
const startStep = step(
  "RUNNER + CONTEXT",
  "Start with a task",
  "Create BasicLoopData and a fresh " +
    "ExecutionContext. The runner publishes BeforeExecution and commits the initial checkpoint.",
  0,
  0,
  "APPLICATION",
  `data := gent.NewBasicLoopData(&gent.Task{Messages: []*gent.ModelMessage{
    gent.TextParts(gent.ChatMessageTypeSystem, "Use policy evidence. Give a short answer."),
    gent.TextParts(gent.ChatMessageTypeHuman, "Can I return an unused item after 10 days?"),
}})
execCtx := gent.NewExecutionContext(ctx, "policy-help", data)
answer, result, err := runner.Execute(execCtx)`,
);
const requestStep = step(
  "REACT → MODEL",
  "Send messages and native tools",
  "The runner starts " +
    "iteration 1. ReAct builds the system prompt, task, and active history. NativeTools supplies " +
    "the available definitions to the managed model call.",
  1,
  0,
  "KEY CONTRACTS",
  `ToolChain.NativeTools(execCtx)
ExecutionContext.GenerateContentStream(...)
Model.GenerateContentStream(ctx, request)

Tool name: search_policy
Required argument: query (string)`,
);
const callStep = step(
  "MODEL → REACT",
  "The model requests a policy search",
  "The response has a " +
    "native call. ReAct saves the decision. It does not accept answer text in this response.",
  1,
  0,
  "MODEL TOOL CALL / ILLUSTRATIVE",
  `{
  "id": "call-policy-1",
  "type": "function",
  "function": {
    "name": "search_policy",
    "arguments": "{\"query\":\"return refund unused\"}"
  }
}`,
);
const toolStep = step(
  "REGISTRY → POLICY TOOL",
  "Validate, plan, execute",
  "PrepareCalls saves " +
    "the plan. ExecuteNext checks JSON, runs hooks, and calls the policy tool. FusedIndex " +
    "queries both sources and combines their results.",
  1,
  1,
  "POLICY SEARCH",
  `PolicySearchInput{Query: "return refund unused"}

BleveIndex: up to 20 candidates
FlatIndex: up to 20 candidates
WeightedLinearFuser: 0.4 BM25 + 0.6 semantic
PolicySearchTool: up to 3 results`,
);
const historyStep = step(
  "REACT → HISTORY",
  "Save the observation",
  "The native call and its " +
    "tool response enter append-only history and the active selection. ReAct returns LAContinue. " +
    "The model can read the result in the next iteration.",
  1,
  1,
  "FULL POLICY RESULT / EXCERPT",
  `# return-window

## Returns

An unused item can be returned within 30 days of delivery.
The customer must supply the order ID.

Loop action: LAContinue`,
);
const answerText =
  "Policy return-window allows an unused item to be returned within 30 days of " +
  "delivery. At 10 days, it is within that window. Please supply the order ID.";
function answerStep(iteration, tools) {
  return step(
    "MODEL → OUTPUT",
    "Check final assistant content",
    "The next response has no " +
      "tool calls and native completion marks an answer. TextOutput " +
      "checks all parts and runs the optional custom validator before acceptance.",
    iteration,
    tools,
    "PLAIN FINAL CONTENT",
    answerText,
  );
}
function finishStep(iteration, tools) {
  return step(
    "REACT → RUNNER",
    "Return the accepted answer",
    "ReAct returns LATerminate. " +
      "The runner saves the terminal result and releases resources. The caller checks " +
      "the error and TerminationReason before using the typed answer.",
    iteration,
    tools,
    "EXECUTION RESULT",
    `TerminationReason: TerminationSuccess
Output: []ContentPart{TextContent{Text: answer}}

Loop action: LATerminate`,
  );
}
function runSteps(path) {
  const base = [startStep, requestStep, callStep, toolStep, historyStep];
  if (path === "snippet") {
    base[4] = step(
      "REACT → HISTORY",
      "Save IDs and snippets",
      "Snippet mode returns policy IDs " +
        "and snippets. Register get_policy with the same documents to read the full rule.",
      1,
      1,
      "SNIPPET RESULT / ILLUSTRATIVE",
      `id: return-window
An unused item can be returned within 30 days of delivery.`,
    );
    return [
      ...base,
      step(
        "MODEL → GET_POLICY",
        "Read the full policy",
        "In iteration 2 the model " +
          "requests get_policy. The registry checks policy_id and saves the result. A snippet " +
          "can omit a condition, so the prompt asks the model to read this before answering.",
        2,
        2,
        "SECOND TOOL CALL",
        `get_policy({"policy_id":"return-window"})

Result: full policy Markdown
Loop action: LAContinue`,
      ),
      answerStep(3, 2),
      finishStep(3, 2),
    ];
  }
  if (path === "reject") {
    return [
      ...base,
      step(
        "TERMINATION → VALIDATOR",
        "Reject an incomplete answer",
        "An optional " +
          "validator rejects an answer with no policy ID. ReAct saves feedback and continues. " +
          "The answer rejection counter increases.",
        2,
        1,
        "VALIDATOR RESULT / ILLUSTRATIVE",
        `ValidationResult{
    Accepted: false,
    Feedback: []FormattedSection{
        {Name: "error", Content: "Include the policy ID."},
    },
}

Loop action: LAContinue`,
      ),
      answerStep(3, 1),
      finishStep(3, 1),
    ];
  }
  if (path === "error") {
    return [
      startStep,
      requestStep,
      step(
        "MODEL → POLICY TOOL",
        "The query is empty",
        "The query " +
          "field is a string, so schema validation passes. The policy tool rejects the empty " +
          "string. The registry records an error observation.",
        1,
        1,
        "TOOL CALL AND ERROR",
        `search_policy({"query":""})

Tool error: query is required
Loop action: LAContinue`,
      ),
      step(
        "MODEL → REGISTRY",
        "Retry with useful search text",
        "The next " +
          "model call uses the error to choose a query. Success resets the consecutive " +
          "tool-error gauges. Errors still remain in history and total error counts.",
        2,
        2,
        "RETRY",
        `search_policy({"query":"return refund unused"})

Result: full policy Markdown
Loop action: LAContinue`,
      ),
      answerStep(3, 2),
      finishStep(3, 2),
    ];
  }
  if (path === "limit") {
    return [
      ...base,
      step(
        "RUNNER → LIMIT",
        "Stop the unfinished run",
        "For this path the " +
          "SCIterations.Self() maximum is 1. Iteration 1 finished with LAContinue, so the runner " +
          "stops before a second model call. Retrieved text is not a final answer.",
        1,
        1,
        "EXECUTION RESULT",
        `TerminationReason: TerminationLimitExceeded
Output: nil
ExceededLimit.Key: SCIterations.Self()
ExceededLimit.MaxValue: 1`,
      ),
    ];
  }
  return [...base, answerStep(2, 1), finishStep(2, 1)];
}
let position = 0;
function renderRun() {
  const steps = runSteps(byId("run-path").value);
  position = Math.min(position, steps.length - 1);
  const current = steps[position];
  byId("run-owner").textContent = current.owner;
  byId("run-title").textContent = current.title;
  byId("run-description").textContent = current.description;
  byId("run-iteration").textContent = current.iteration;
  byId("run-tools").textContent = current.tools;
  byId("run-code-label").textContent = current.codeLabel;
  byId("run-code").textContent = current.code;
  byId("run-position").textContent = `${position + 1} / ${steps.length}`;
  byId("run-step").max = steps.length - 1;
  byId("run-step").value = position;
  byId("run-step").setAttribute("aria-valuetext", `${position + 1}: ${current.title}`);
  byId("run-back").disabled = position === 0;
  byId("run-next").disabled = position === steps.length - 1;
  byId("run-timeline").innerHTML = steps
    .map(
      (item, index) => `<li><button data-step="${index}"
    aria-label="Step ${index + 1}: ${item.title}" ${index === position ? 'aria-current="step"' : ""}
    class="${index < position ? "passed" : ""}">
    ${String(index + 1).padStart(2, "0")}</button></li>`,
    )
    .join("");
}
byId("run-next").onclick = () => {
  position++;
  renderRun();
};
byId("run-back").onclick = () => {
  position--;
  renderRun();
};
byId("run-reset").onclick = () => {
  position = 0;
  renderRun();
};
byId("run-path").onchange = () => {
  position = 0;
  renderRun();
};
byId("run-step").oninput = (event) => {
  position = Number(event.target.value);
  renderRun();
};
byId("run-timeline").onclick = (event) => {
  const button = event.target.closest("[data-step]");
  if (!button) return;
  position = Number(button.dataset.step);
  renderRun();
  byId("run-timeline").querySelector(`[data-step="${position}"]`).focus();
};

byId("copy-code").onclick = async () => {
  try {
    if (!navigator.clipboard?.writeText)
      throw new Error("Clipboard is unavailable on this file URL");
    await navigator.clipboard.writeText(data.example);
    byId("copy-status").textContent = "Go source copied.";
  } catch (error) {
    const selection = window.getSelection();
    const range = document.createRange();
    range.selectNodeContents(byId("go-example"));
    selection.removeAllRanges();
    selection.addRange(range);
    byId("copy-status").textContent =
      `${error.message}. Source selected; copy it or use Download main.go.`;
  }
};
byId("download-code").onclick = () => {
  const url = URL.createObjectURL(new Blob([data.example], { type: "text/plain;charset=utf-8" }));
  const link = document.createElement("a");
  link.href = url;
  link.download = "main.go";
  link.click();
  // Delay cleanup until the browser has started the local download.
  setTimeout(() => URL.revokeObjectURL(url), 1000);
};

const modes = {
  full: [
    "Full content is the default",
    "Returns the policy ID as a heading and the complete body. " +
      "Use it when the policies are short. The next model request can read the whole rule.",
    "policy.SearchResultFull",
    `# return-window

## Returns

An unused item can be returned within 30 days of delivery.
Supply the order ID. The refund uses the original payment method.`,
  ],
  snippet: [
    "Snippets reduce the first result size",
    "Returns an ID and the snippet selected by " +
      "the search index. Register get_policy and read the full text before using the rule.",
    "policy.SearchResultSnippet",
    `id: return-window
An unused item can be returned within 30 days of delivery.

Then call: get_policy({"policy_id":"return-window"})`,
  ],
  title: [
    "Title mode includes the description",
    "The implementation returns the ID and " +
      "Description. It does not return the rule body. Use get_policy to retrieve that body.",
    "policy.SearchResultTitle",
    `id: return-window
description: Return unused items within 30 days of delivery.

Then call: get_policy({"policy_id":"return-window"})`,
  ],
};
function renderMode(mode) {
  const [title, description, code, result] = modes[mode];
  byId("mode-title").textContent = title;
  byId("mode-description").textContent = description;
  byId("mode-code").textContent = `.WithResultMode(${code})`;
  byId("mode-result").textContent = result;
  all("[data-mode]").forEach((button) => {
    button.setAttribute("aria-pressed", String(button.dataset.mode === mode));
  });
}
all("[data-mode]").forEach((button) => {
  button.onclick = () => renderMode(button.dataset.mode);
});

const questions = [
  {
    question: "Which piece must you write to add standard hybrid policy search?",
    correct: 1,
    options: [
      "A new Loop implementation",
      "Your policy documents and dependency setup",
      "A SearchChain that discovers every policy as a tool",
    ],
    why: "ReAct, Registry, and PolicySearchTool already implement the required behavior.",
  },
  {
    question: "Where does Policy.Description affect search?",
    correct: 2,
    options: [
      "It is the highest-boost BM25 field",
      "It is embedded as a separate vector chunk",
      "It is display text in title mode; the adapters do not index it",
    ],
    why: "BM25 uses ID, keywords, and body. Vectors use ID, body, and synthetic queries.",
  },
  {
    question: "A hit has no BM25 match and cosine 0.80. What is its default fused score?",
    correct: 0,
    options: ["0.48", "0.80", "0.00 because both sources must match"],
    why: "The missing source contributes zero: 0.4 × 0 + 0.6 × 0.80 = 0.48.",
  },
  {
    question: "The response has both a native tool call and final-answer text. What happens?",
    correct: 0,
    options: [
      "ReAct runs the tool calls first",
      "The runner accepts the answer immediately",
      "TextFormat must parse an XML action section",
    ],
    why: "Native calls take priority. Answer validation runs when there are no tool calls.",
  },
  {
    question: "How do you change the built-in 40/60 weights in this working copy?",
    correct: 1,
    options: [
      "Call PolicySearchTool.WithWeights",
      "Compose indexes and a fuser behind your own tool",
      "Change the loop iteration limit",
    ],
    why: "PolicySearchTool has no weight setter. SearchIndex and Fuser are the extension points.",
  },
];
byId("quiz-questions").innerHTML = questions
  .map(
    (item, index) => `<fieldset class="quiz-card">
  <legend>${String(index + 1).padStart(2, "0")} / ${escapeHtml(item.question)}</legend>
  ${item.options
    .map(
      (option, choice) => `<label><input type="radio" name="question-${index}"
    value="${choice}">${escapeHtml(option)}</label>`,
    )
    .join("")}
  <p class="quiz-feedback" id="feedback-${index}" aria-live="polite"></p></fieldset>`,
  )
  .join("");
byId("quiz-form").onsubmit = (event) => {
  event.preventDefault();
  const values = new FormData(event.target);
  let score = 0;
  questions.forEach((item, index) => {
    const raw = values.get(`question-${index}`);
    const correct = raw !== null && Number(raw) === item.correct;
    if (correct) score++;
    const feedback = byId(`feedback-${index}`);
    feedback.classList.toggle("wrong", !correct);
    const status = raw === null ? "No answer selected." : correct ? "Correct." : "Try again.";
    feedback.textContent = `${status} ${item.why}`;
  });
  byId("quiz-result").textContent =
    score === questions.length
      ? "5 / 5 — You have the complete path."
      : `${score} / 5 — Read the feedback and try again.`;
};
byId("quiz-reset").onclick = () => {
  byId("quiz-form").reset();
  all(".quiz-feedback").forEach((element) => {
    element.textContent = "";
  });
  byId("quiz-result").textContent = "";
};
byId("source-version").textContent =
  `Checked against the local working copy on 26 October 2026. ` +
  `Base commit: ${data.revision}; source digest: ${data.digest}. Local changes are included. ` +
  `The digest identifies the source bytes used to build these lessons.`;
byId("source-files").innerHTML = data.sources
  .map((path) => `<li>${escapeHtml(path)}</li>`)
  .join("");

// Initialize hidden sections too so print output includes the current experiment states.
selectMap("policy");
renderContracts();
renderPolicy();
renderRanking();
renderRun();
renderMode("full");
document.documentElement.classList.add("js");
showPage();
