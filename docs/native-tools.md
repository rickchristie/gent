# Native tools

Gent uses native tool calls throughout. `react.NewAgent` sends tool definitions through
`gent.CallOptions.Tools`, executes structured calls, and validates plain final assistant content.
There is no text action parser, fallback mode, or separate native agent constructor.

## Why this contract

The provider documentation checked on 24 October 2026 supports this choice:

| Provider | Documented interface |
| --- | --- |
| [OpenAI][openai] | Function definitions, call IDs, arguments, and tool results. |
| [Grok][grok] | Native function calls, including Responses API calls. |
| [Anthropic][anthropic] | Tool definitions and `tool_use` / `tool_result` blocks. |
| [DeepSeek][deepseek] | Structured tool calls in thinking and non-thinking modes. |
| [Qwen][qwen] | Function calling through supported serving APIs and model templates. |

[openai]: https://developers.openai.com/api/docs/guides/function-calling
[grok]: https://docs.x.ai/developers/tools/function-calling
[anthropic]: https://platform.claude.com/docs/en/agents-and-tools/tool-use/overview
[deepseek]: https://api-docs.deepseek.com/guides/tool_calls/
[qwen]: https://qwen.readthedocs.io/en/latest/framework/function_call.html

These interfaces support a native-only application contract. They do not prove that every model,
server, or template supports every tool feature. Open-weight models can still emit special text
that their serving layer parses. Gent requires the serving layer to expose structured calls.
Provider support also does not imply identical schemas, reasoning fields, or replay rules.

## Tool definitions

Use the builder for function tools. A bound method can keep service dependencies on its receiver.
Use a full `gent.Tool[I, O]` implementation when the tool itself needs fields or a custom `Call`
result with media and instructions.

```go
tool, err := gent.DefineTool("lookup_order", service.LookupOrder).
    Description("Get the current order status.").
    Parameters(schema.Field("order_id").Description("The order ID.").MinLength(1)).
    Policy("Use the order ID supplied by the user.").
    Domain("Orders").Categories("lookup").Keywords("order", "status").
    SyntheticQueries("Where is my order?").
    Recovery(service.RecoverOrder).
    Build()
if err != nil {
    return err
}
chain.RegisterTool(tool)
```

`DefineTool` infers generic input and output types from the handler. `Build` returns a tool and an
error; `MustBuild` panics on invalid fixed configuration. Configure before registration. Built
schemas and metadata are copied, including values returned by getters. Builder changes cannot
change an existing tool. The handler and recovery function must protect any shared mutable state.

The builder uses `json` names and Go types. All input fields are required by default, including
fields with `omitempty`. Add `.Optional()` to a field to permit absence. Pointer fields permit
null, but remain required unless marked optional. Slices and maps can also be null. No default
values are inserted. Struct schemas reject unknown properties. A dotted field path such as
`address.city` selects a nested object field. Fields support descriptions, enums, formats, numeric
bounds, string lengths and patterns, and array lengths. Formats are checked by local validation.
Enums accept named Go constants and check their JSON values against the field schema. Schema
copies use `json.Number` to keep numeric constraints exact, including large integers.

`ParameterSchema(raw)` replaces input inference with a full JSON Schema and cannot be mixed with
field settings. Use it for recursive types, embedded fields, JSON `string` tags, or custom JSON
encodings. Map and interface inputs also need an explicit parameter schema. Nil omits schema
constraints; use an inferred `struct{}` input for a tool that must receive no arguments. `OutputSchema(raw)` replaces output inference; nil omits the output description. Output
schemas describe the text result; Gent does not validate returned results against them.

`Strict(true)` passes the strict flag to the provider. Provider support and accepted schema
keywords can differ. Gent validates arguments against the supplied schema before it converts them
or calls the tool. The builder compiles both schemas to catch invalid definitions during setup. The registry
keeps a compiled input validator for calls; no schema inference or compilation occurs per call.

Search metadata, policy, recovery, strict mode, output schema, and deduplication are all methods
on `gent.Tool`. Empty search metadata and policy are valid. `DeduplicateSummary(handler)` is off by
default. Use it only when the same arguments have the same result; a current-status lookup usually
needs fresh output. Search pinning, pagination, JavaScript timeouts, and whether to print output
schemas remain chain settings.

## Models and saved provider state

```go
model := models.NewOpenAI(apiKey, modelName)
// Or: model := models.NewXAI(apiKey, modelName)
agent := react.NewAgent(model).WithRecoveryVersion("support/v1")
```

Both constructors use the streaming Responses API. Gent saves the exact ordered output items in
`ProviderContinuation`, including opaque reasoning data. The next request replays these items with
the matching function outputs. Requests use `store: false`; saved runs do not depend on a provider
retaining the original response. Both adapters request encrypted reasoning content.
Grok's [stateless history guidance][grok-history] also requires replaying these opaque items.

[grok-history]: https://docs.x.ai/developers/model-capabilities/text/generate-text

`models.NewResponses` accepts an endpoint and optional HTTP client. Its provider name identifies
compatible continuation data. Replay through a different provider returns an error. Unsupported
content and options also return errors; the adapter must not silently discard required data.
Temperature and top-p use optional pointers, so zero differs from omission.
Each managed call copies its request before mutable hooks run. A hook can change sampling values,
tool definitions, and messages without changing settings used by another run.

Responses metadata merges `thinking_config` with `responses.reasoning`. Fields such as reasoning
effort and summary can be set together. An explicit field in `responses.reasoning` overrides the
same field from `thinking_config`. The final choice exposes provider-visible summaries through
`MetaContent` entries with type `reasoning`. Opaque and encrypted items remain unchanged in
`ProviderContinuation`.

Gent owns the Anthropic, Gemini, DeepSeek, local runtime, and OpenRouter adapters as well.
Each preserves its native continuation fields, including signed thinking or reasoning fields.
See [provider protocols and supported features](models.md). Unsupported inputs and native controls
return errors. A schema that cannot use native enforcement still receives complete local validation.

## Reasoning, commentary, and answers

Gent separates provider candidates from accepted execution output:

| Value | Meaning |
| --- | --- |
| `ContentChoice.CandidateContent` | Ordered content parts that the agent must check. |
| `MetaContent{Type: "intermediary"}` | Ordinary text whose role is not yet certain. |
| `MetaContent{Type: "reasoning"}` | Visible reasoning or a visible reasoning summary. |
| `MetaContent{Type: "commentary"}` | An assistant update with an explicit provider label. |
| `StreamChunk.Content` | The complete accepted answer, published once at terminal acceptance. |

`MetaContent` is an ordered slice of pointers. Its `Type` is a string. Constants cover the three
known types; unknown provider labels remain metadata. `Content` contains ordered text or media.
`MessageId` identifies its message. Encrypted or signed continuation data must not be used as display text.

The Responses adapter uses the assistant message `phase` from `response.output_item.added` for text
deltas. Both unlabelled text and `final_answer` stream as `intermediary`: a provider cannot know
whether an application validator will accept the answer. Unknown phases remain metadata and do not
become answer candidates. Only the last assistant message can supply a candidate. Earlier messages
remain previews. Raw items retain their phases for provider replay.
See [OpenAI's phase contract][openai-phase].

[openai-phase]: https://developers.openai.com/api/docs/guides/reasoning#phase-parameter

A provider completion ends one request. ReAct checks the stop reason, executes native calls, and
passes a complete candidate with no tool calls to `Output[T].Process`. Truncated responses
retry. A provider continuation such as `pause_turn` requires lossless continuation data and another
request. Unknown or failed completion states return an error. Commentary alone continues the loop.

Anthropic's wire-level `content` is an array of typed blocks. Its adapter maps visible thinking to
reasoning metadata and ordinary text to intermediary metadata. Literal tags remain ordinary text.
Signed thinking blocks are saved and replayed in their original order. Gemini retains thought
signatures with their parts; Chat endpoints retain the assistant's reasoning and native call fields.

### Frontend and storage contract

Use `ExecutionContext.SubscribeAll()` for accepted live chunks, or `RecordedEventObserver` for
accepted events. The same stream carries provisional metadata and the accepted answer. The provider
`Stream` carries provisional chunks and a candidate response; it never accepts an execution answer.

1. Append each metadata delta to the preview identified by its `MessageId` and type. Keep entry
order.
2. On `execution_content`, replace the final answer with `Content`. This is a complete ordered content snapshot.
   Hide or replace previews whose IDs occur in `SourceMessageIds`. Other updates can remain visible.
3. Identify an answer by its `ContextId` and final `MessageId`. Child contexts have separate
answers.
4. Deduplicate delivery by `(RunId, EventNumber)`. On reconnect, decode saved events with
   `CodecRegistry.DecodeEvent`, which fills these cursor fields from the accepted envelope.

For example, a model can stream ` { "status" : "done" } ` as intermediary text. If the JSON
output processor accepts it, the final event contains `{"status":"done"}`. The final snapshot
uses the actual output result, so it can differ from the preview. A frontend must replace it,
not append it. This can send text twice over the display transport, with no extra model call,
model tokens, or history message.

Each managed provider request scopes local message IDs to a new physical call. A retry gets new
IDs, including when it uses the same logical call ID in the same worker. Saved replay keeps the
original IDs. IDs are opaque; do not parse their format. Adapters with multiple messages must give
them distinct local IDs and set `CandidateSource.MessageIds` for the selected candidate. A single
message adapter can omit both. A custom loop can return `AgentLoopResult.ContentSource`; it is
optional for output that does not come from a model. A custom caller of `EmitChunk` must supply
its own message IDs. It can emit metadata but cannot publish `Content` through that method.

The executor saves the final event with the successful terminal checkpoint before any recorded
observer or stream subscriber receives it. A child saves its final event with its runtime evidence;
that save does not move the parent's control checkpoint. Failed saves do not publish the final
answer. A lost acknowledgment is reconciled with `RetrySave`, then restored from saved evidence.
Restoring a terminal run returns its saved result without another model request or another final
event. Reconnect replays the existing event. Delivery can repeat; the logical answer does not.

Failed, canceled, limited, truncated, or rejected attempts do not publish final `Content`. A custom
successful result with no text still has a final event and `MessageId`, with empty `Content`.
Opaque state, tools, and media retain their structured representations.

Trace snapshots retain provisional entries in `ModelStream.MetaContent` and the accepted content in
`Context.Content`. `MaxMetaContentBytes` bounds total retained metadata per model call;
`MaxContentBytes` bounds retained content per context. `IncludeChunkText` controls metadata, and
`IncludeRunOutput` controls final content. The chunk redactor applies to both; the run-output redactor
also applies to final content. An oversized media part is omitted from a bounded preview as a whole;
saved history retains the complete bytes.

`StreamAccumulator` keeps metadata separate from `Content`; use one accumulator per context.
Stream helpers copy responses and metadata. Provider metadata uses built-in wire types such as
maps, slices, scalars, time values, and raw JSON. Convert SDK objects to these types before completion.
Provider callback options such as `WithStreamingFunc` expose raw intermediary deltas before Gent's
save barrier. Use accepted events or context subscriptions for database and WebSocket delivery.

## ToolChain contract

`ToolChain` requires native definitions, preparation, step execution, recovery guidance, codec
registration, and compatibility configuration. All built-in chains implement the same contract:

- `toolchain.New()` creates a `Registry` of typed tools.
- `toolchain.NewSearch(...)` creates a `SearchChain` with dynamic definitions.
- `toolchain.NewJsToolChainWrapper(chain)` adds local JavaScript execution.

Preparation decodes native JSON arguments and saves distinct logical call IDs. Execution validates
schemas, converts typed inputs, runs before-tool hooks, saves effective input before an effect, and
saves results before the next step. Events, limits, summaries, instructions, and media use this
path.
Provider call IDs pair API messages. Gent call IDs identify operations across worker attempts.

Custom chains can embed the registry and override selection:

```go
type SelectedTools struct {
    *toolchain.Registry
    Select func(*gent.ExecutionContext, []gent.ToolDefinition) ([]gent.ToolDefinition, error)
}

func (c *SelectedTools) NativeTools(ctx *gent.ExecutionContext) ([]gent.ToolDefinition, error) {
    tools, err := c.Registry.NativeTools(ctx)
    if err != nil {
        return nil, err
    }
    return c.Select(ctx, tools)
}
```

This retains shared execution, hooks, and recovery. A selection callback changes visibility;
application access checks must still control permission to execute. Include declarative settings
in `RecoveryConfig`, identify callback behavior with the agent's `WithRecoveryVersion`, and save
mutable run state through `ExecutionContext.SetCheckpointState`. Configure chains before concurrent
use. Register each chain's types before decoding saved runs.

## Search

```go
chain := toolchain.NewSearch(toolchain.SearchHintDomainCategories).
    RegisterEngine(toolchain.NewRegexToolSearcher())
chain.RegisterTool(lookupOrder) // Search metadata is part of gent.Tool.
chain.Pin("lookup_order")      // Optional.
if err := chain.Initialize(); err != nil {
    return err
}
agent := react.NewAgent(model).WithToolChain(chain)
```

The first request contains search tools and pinned tools. In `Search` mode, a successful search
records the full definitions it returned as `DiscoveredTools`. The next request includes those
definitions. `SearchGet` returns brief search results and exposes a separate `get_tool_schema` call;
only a successful schema lookup adds that definition.

Discovery comes from active history, including results inside JavaScript. Saved runs retain it.
Expiry and compaction can remove discovery from the active context, after which the model can search
again. Runs do not share mutable discovery state. Search controls context size, not access rights.

## Programmatic tool calling

```go
chain := toolchain.NewJsToolChainWrapper(toolchain.New().RegisterTool(lookupOrder))
agent := react.NewAgent(model).WithToolChain(chain)
```

The native `run_code` tool accepts `{ "code": "..." }`. Its description teaches `tool.call`,
`tool.parallelCall`, error checks, and return values. Direct mode exposes the business definitions
beside `run_code`. `WithDirectCallDisabled()` exposes only `run_code` and includes the currently
available function definitions in its description. Input schemas and output schemas tell the model
which fields to pass and which returned fields to read.

```javascript
(function() {
  const order = tool.call({tool: "lookup_order", args: {order_id: "ORD-123"}});
  if (order.error) return;
  tool.call({tool: "read_shipment", args: {shipment_id: order.output.shipment_id}});
})();
```

The runtime runs on the application's server. It does not use a provider-hosted code service.
Inner calls use the same native preparation and execution path. Search inside a program updates
later tool definitions. Each program increments code statistics; its inner calls increment tool
statistics. `run_code` does not add an extra business-tool count.

`tool.call` waits for one call. With `Registry` and `SearchChain`, `tool.parallelCall` starts one
goroutine per call and waits for all calls. Results and the tool log keep the input order, even when
calls finish in a different order. A tool error is returned in that call's `error` field; other
calls can finish. Use parallel calls only for independent work. Tools, search engines, hooks,
summary callbacks, and custom text formats used here must be safe for concurrent use. Tools must
check their Go context so cancellation can stop them. The wrapper waits for started calls before
it returns, including after cancellation or a checkpoint failure. The JavaScript VM stays on one
goroutine.

Each concurrent call has its own saved plan, logical ID, effective input, and result. Parallel
searches keep separate lists of returned schemas so each search result is complete. A worker
restart keeps completed results, reports uncertain calls, and marks calls that did not start.
It does not restart the script. Custom chains must implement `gent.ParallelToolChain` and return
true from `SupportsParallelCalls` to accept batches with more than one call. The wrapper returns
an error before tool effects if this support is absent or disabled. The wrapper calls `ExecuteNext`
on the supplied chain, so an override on a type that embeds `Registry` or `SearchChain` still runs.
An override that requires sequential use must return false from `SupportsParallelCalls`.

If a parallel call panics, the wrapper interrupts the worker context and waits for the other calls
to stop. It then propagates the first panic to the caller. A panic during a sibling's cleanup does
not replace that value. The last accepted checkpoint remains available for recovery.

## Tool recovery

Each tool implements `Recover(context.Context, *gent.ToolRecoveryRequest)` and returns
`(*gent.ToolRecoveryResponse, error)`. The builder accepts that function through `.Recovery(...)`.
Without a handler, it returns `ToolRecoveryUnresolved`. Recovery runs only for an interrupted
operation with no saved result. Ordinary continuation and known results do not invoke it.

```go
builder.Recovery(func(ctx context.Context, request *gent.ToolRecoveryRequest) (
    *gent.ToolRecoveryResponse, error,
) {
    return &gent.ToolRecoveryResponse{
        Result: gent.ToolRecoveryUnresolved,
        SteeringMessage: "Check the booking service with the saved call ID before booking again.",
    }, nil
})
```

`ToolRecoveryResponse` contains the decision, optional `SteeringMessage`, and optional `Output`:

| Result | Output | Action |
| --- | --- | --- |
| `ToolRecoveryCompleted` | Required `*gent.ToolCallResult` | Reuse the recovered result. |
| `ToolRecoveryRetryAllowed` | Must be nil | Retry with the saved effective input. |
| `ToolRecoveryUnresolved` | Must be nil | Report an unknown outcome without repeating the call. |

A recovery error stops the run and leaves the checkpoint available for another recovery attempt.
Invalid decisions and missing completed outputs also fail. Gent supplies the original name, call
ID, attempt ID, arguments, and effective input on a recovered result. The tool supplies its output,
formatted text, media, and any instructions. A steering message never authorizes a retry.

Gent saves each decision before it uses it. The decision and message survive a lost save
acknowledgement. A retry gets a new attempt ID, but keeps its logical call ID and effective input;
its mutation hook does not run again. Steering accompanies the resulting observation, including
when a retry fails. Checks must be safe to repeat if the process stops before the decision is saved.
There is no global recovery handler or separate static recovery message setting.

An unresolved operation produces an `unknown` result with standard facts and any steering. Later
unstarted calls in that batch receive `not_started` results. The model can make a new plan. A known
tool error remains distinct from an unknown outcome.

An interrupted program never restarts its script or restores a JavaScript stack. Gent recovers
inner calls with each tool's handler, including a safe retry when permitted. It returns saved and
recovered partial results through the original `run_code` call ID. Operations that were not started
remain unstarted. Accepted results take precedence over handlers and are not repeated.

Set ReAct's `WithRecoveryVersion` for your application behavior and change it when a handler or its
configuration changes. Function behavior cannot be captured in a configuration hash. Tool schemas,
strict flags, and descriptions remain part of the automatic compatibility checks.

The application recorder must check its worker lease in each atomic save. A rejected save stops an
old worker before it can start another call. An already running external effect can still finish.
Gent supplies evidence and hooks; the application owns duplicate-effect policy and reconciliation.
See [history and recovery](history.md) for storage and compatibility rules.

## Tests

Unit and fixture tests cover typed calls, provider IDs, output schemas, hooks, limits, dynamic
search, dependent JavaScript calls, saved provider items, cancellation, and fresh-worker recovery.
Recovery cases include interruption before an effect, an effect with a lost result, partial program
completion, lost recovery acknowledgments, and worker takeover.

`TestNativeLive` runs actual OpenAI and Grok calls. Each provider has a code-only case and a search
then JavaScript case. These assert actual dependent tool inputs, code execution, and changing
native definitions. Saved inner results must show both business calls inside the same `run_code`
result. An initial script error without business effects can be corrected within the iteration
limit. A no-op program followed by direct business calls does not pass this check.
Set `GENT_TEST_OPENAI_KEY` / `GENT_TEST_XAI_KEY` or `OPENAI_API_KEY` /
`XAI_API_KEY`. Optional `GENT_TEST_OPENAI_MODEL` / `GENT_TEST_XAI_MODEL` select models. Cases skip
when credentials are absent. A fixture pass is not a live-provider pass.
