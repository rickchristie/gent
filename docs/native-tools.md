# Native tool calls

## Review after the history refactor

Reviewed on 9 October 2026 at `ad01bf7`. The working tree was clean at the start of this work.
The provider research is dated 8 October 2026. This review updates the Gent findings.

The history refactor changed the implementation plan:

- `gent.Loop` removes the need for a generic agent type. Models receive a resolved request.
- The normal ReAct stream path now returns the complete response, including native tool calls.
  The earlier report that the normal path lost these calls is no longer correct.
- `StreamAccumulator.ResponseWithInfo` builds a text-only response. ReAct uses it after an early
  repetition or size stop. A partial response must not execute tools.
- Requests, responses, tool results, and loop decisions now have saved history and checkpoints.
- `ToolPlan` records the next call and its result. Native calls must use the same recovery path.
- JSON, YAML, and search chains encoded a saved call as JSON and then parsed it again. Both text
  and native calls now use shared execution of parsed calls. YAML normalizes its argument values
  before it saves the plan, so nested dates still reach schema checks as JSON strings.
- Result summaries are captured at execution time. Native messages must keep that evidence,
  provider call IDs, instructions, and media when duplicate results are shortened.
- An interrupted JavaScript program cannot safely restart without saved program state and
  known external effects. That recovery limit remains in place.

## What changed

Use `react.NewNativeAgent(model)` for native tool calls. `react.NewAgent(model)` keeps its
existing behavior. Both return `*react.Agent` and use the same executor, history, tool plans,
statistics, limits, stream controls, and termination validators.

| Decision | Current implementation |
| --- | --- |
| Replace required thought text | Native prompts do not request a thought transcript. |
| Replace response section parsing | Native mode reads `ToolCalls` and final `Content`. |
| Replace the tool catalog in the system prompt | Native definitions come from the tool registry. |
| Replace code envelopes | Native `run_code({code: ...})` calls the local JavaScript runtime. |
| Keep execution controls | Typed inputs, schema checks, hooks, limits, and errors share one path. |
| Keep local search | Search selects definitions for the next request from active history. |
| Keep local PTC | Sobek, the tool bridge, logs, media, timeouts, and tool statistics remain. |
| Keep history and recovery | Native and text calls use saved `ToolPlan` decisions. |
| Keep answer checks | Text/JSON termination and business validators still run. |

No methods were added to the required `Model` or `ToolChain` contracts. The optional
`gent.NativeToolChain` interface exposes definitions, prepares calls, and executes the next
saved call. JSON, YAML, SearchJSON, and JsToolChainWrapper implement it. A custom chain must
implement this interface to use `NewNativeAgent`. The native agent reports unsupported chains;
it does not silently switch to a text protocol.

Text formats still render prompts and tool results in native mode. Their response parsers and
`WithThinking` sections are not used. Existing text callers keep their formats, sections, and
custom prompts. These interfaces remain in use, so removing them would break those callers.

## Direct calls

```go
agent := react.NewNativeAgent(model).
    RegisterTool(lookupOrder).
    WithBehaviorAndContext("You are a customer support agent.")
```

A request has `Options.Tools` with the active definitions. The model response has
`ContentChoice.ToolCalls`. Gent decodes each argument object, checks the registered schema,
converts it to the tool's Go input, runs hooks, and executes it through the saved plan.
The next request has the assistant calls and one `ToolCallResponse` for each call.

Compaction includes native call IDs, names, arguments, and result text in the summary request.
This keeps tool evidence available when a turn has no assistant text. Summaries select new active
history; the original structured calls and results remain in saved history. Images and other
media keep the existing compaction rule: pin the entry if the next model needs that media.

Provider IDs pair messages on the wire. Gent IDs identify durable tool effects. They stay
separate. Missing provider IDs receive stable replacements in the saved decision. Duplicate
IDs and malformed argument objects cause retry feedback before any call in that group runs.

Tool calls take priority over text in the same response. Final assistant content goes to the
configured termination handler. A JSON handler still parses and validates the answer and can
reject it with feedback. This is application validation, not provider schema enforcement.

Tool definitions come from the registered chain. `WithCallOptions` still configures model
options, but its `Tools` list is replaced by the active registry. Request hooks can modify the
resolved request. Configure chains before execution, as with the existing text path.

## Local search, step by step

```go
chain := toolchain.NewSearchJSON(toolchain.SearchHintDomainCategories).
    RegisterEngine(toolchain.NewBM25ToolSearcher())
chain.RegisterTool(lookupOrder) // Must also implement gent.IndexableTool.
if err := chain.Initialize(); err != nil {
    return err
}
agent := react.NewNativeAgent(model).WithToolChain(chain)
```

1. The first request contains `tool_registry_search` and any pinned tools. It includes a short
   registry hint and the search engine guidance. It does not include all business schemas.
2. The model calls the search function. Gent runs the registered local search engine.
3. The result records `DiscoveredTools`. In `Search` mode, these are the tools on that page.
4. Gent adds those definitions to `Options.Tools` in the next model request.
5. The model can now call a discovered tool with its native argument schema.

`SearchGet` returns brief search results first. Only a successful `get_tool_schema` call makes
that tool's definition available. Pagination, multiple engines, policies, output schemas, and
pinned tools retain their current behavior.

Discovery is derived from active history, including search calls inside JavaScript. Separate
runs do not share mutable discovery state. When compaction or expiry removes the discovery
entry from active history, the definition leaves later requests. The model can search again.
Search controls prompt size; it is not an authorization boundary. Registered tool execution
and application access checks remain responsible for authorization.

## Local programmatic tool calling

```go
chain := toolchain.NewJsToolChainWrapper(toolchain.NewJSON().RegisterTool(lookupOrder))
agent := react.NewNativeAgent(model).WithToolChain(chain)
```

The model calls the native `run_code` function with a `code` string. For example:

```javascript
(function() {
  const order = tool.call({tool: "lookup_order", args: {order_id: "ORD-123"}});
  if (order.error) return;
  console.log(order.output.status);
})();
```

Gent runs the code locally. It does not enable a provider-hosted runtime or a provider PTC
service. The inner calls use the existing tool bridge, schema checks, hooks, limits, and
recovery evidence. Model inference and your server resources still have their normal costs.

The default mode exposes direct tools and `run_code`. `WithDirectCallDisabled()` exposes only
`run_code` and places the currently available tool definitions in its description. It rejects
direct native calls. A SearchJSON chain can be wrapped in the same way.

`run_code` is reserved in a native JavaScript wrapper. Existing JavaScript options, custom code
instructions, console output, complete call logs, and media remain available. The program
increments code statistics; inner business/search calls increment tool statistics. The
wrapper does not add an extra business-tool count for `run_code`.

A completed program result can be recovered without repeating its effects. An interruption
inside a program still returns `ErrUnsupportedRecovery` when a worker tries to restore it.
Direct calls keep normal per-tool recovery. Use `WithRecoveryVersion` as described in
[history and recovery](history.md). Changing between text and native mode changes compatibility.

## Adapter repairs and limits

The installed LangChainGo v0.1.14 Anthropic adapter exposes content blocks as separate choices
and reads only the first part of an assistant message. `LCGWrapper` now merges those response
blocks and splits outgoing assistant parts for that concrete adapter. The Messages API combines
[consecutive messages with the same role](https://platform.claude.com/docs/en/api/messages/create).
This keeps text plus multiple tool calls in one logical Gent turn. Other adapters retain their
choice semantics. A custom wrapper around an Anthropic client must preserve these semantics too.

Current OpenAI and Anthropic adapters do not accept media beside a tool response part. Gent keeps
media with its saved result, then sends a separate user message after the complete tool-result
group. Each media group names its source tool and provider call ID. Duplicate-result processing
retains the IDs and removes media only when that result is abbreviated.

The response-size and repetition checks also inspect completed content and tool arguments that
were absent from display chunks. A size stop, repetition stop, or incomplete native response
cannot execute its tool calls. Search now also checks cancellation after its before-tool hook,
so a cancelled call cannot start an external effect.

The native agent also rejects incomplete final answers. It handles `length`, `max_tokens`,
`model_context_window_exceeded`, `MAX_TOKENS`, and the Google SDK form `FinishReasonMaxTokens`
before it executes tools or validates an answer. The context-window stop reason is incomplete
even when its text or tool arguments can be parsed.

The model adapter must return complete native calls in `ToolCalls`. The old `FuncCall` field
alone is not the native contract. This work does not certify every model or serving template.
Use the same task and tool tests when changing providers or local models.

## Deferred: Strengthen

This change does not add strict provider output schemas, reasoning-effort controls, a capability
registry, tool-argument stream events, or replay of opaque provider state. Reasoning text that
the current adapter supplies remains in stream events and recorded model responses. It is not
inserted into the conversation as a required thought transcript.

Signed thinking blocks and other opaque continuation items need a richer adapter contract.
Do not assume that enabling an extended-thinking provider option makes native multi-turn tool
use correct with the current common message types. That support remains part of Strengthen.

## Verification

The tests keep the existing text suite and add native call/result checks, JSON answer validation,
malformed-call retries, limits, final-only response checks, media, result deduplication, search
selection and expiry, local JavaScript, and saved-plan recovery. HTTP fixtures test full requests
and streamed responses with the real OpenAI-compatible and Anthropic adapters. They run against
local test servers and do not use paid model APIs.

The full native, CGO-disabled, and race suites passed with `-count=1 -timeout 300s`.
`go vet ./...` and `git diff --check` also passed. No existing test was removed.
The native tests cover parsing, limits, recovery, compaction evidence, nested YAML dates, and
provider stop reasons. The stop-reason tests include the real Anthropic stream decoder and both
tool calls and final answers.

Portable statement coverage, with the same package set and coverage options:

| Scope | Before | After |
| --- | ---: | ---: |
| Entire repository | 66.1% | 67.3% |
| ReAct | 80.6% | 85.4% |
| Shared agent utilities | 95.7% | 96.8% |
| Tool chains | 84.5% | 85.0% |
| Model adapters | 21.1% | 27.9% |

The repository's `gent setup onnx` command installed ONNX Runtime 1.24.4 and tokenizers 1.26.0
to `~/.gent/lib/`. It also added the library paths to `.bashrc`. The `gent model download`
command installed all ten model files and their tokenizers to `~/.gent/models/` (893 MiB).
These files support all eleven registered model configurations.

The native and race runs now pass all eleven model configurations, the quantized matrix
multiplication regression test, and the search and policy integration tests. No ONNX test skips
because of a missing runtime or model. Two `query_vs_document_prefixes` subtests still skip for
`all-MiniLM-L6-v2` and `bge-micro-v2`, because those models have no prefixes. Twenty live-provider
tests still skip because their credentials or opt-in settings are absent.

Native and race runs use `CGO_ENABLED=1`, `CGO_LDFLAGS=-L/home/ricky/.gent/lib`, and `-p 1`.
Running one test package at a time limits memory use while loading large ONNX models. `GOCACHE`
points to `/tmp/gent-review-gocache` because the normal cache is not writable in this sandbox.
Local HTTP test servers require permission to bind a socket. Test output uses `-json` to record
individual passes and skips as well as the package results.

Test setup checks the runtime before downloading model files. A stack dump confirmed that the
old setup could wait in a download before `testing.M.Run`, outside the Go test timeout. The
partial model from that interrupted check was moved to `/tmp`, so it cannot be used as a valid
cache entry. The later setup downloaded the complete model before the ONNX checks ran.

The interactive analysis passed 432 cell-detail checks, filters, CSV/JSON exports, and desktop
and mobile layouts. The browser reported no console errors or external network requests.
