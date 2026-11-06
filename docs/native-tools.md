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

Use `gent.Tool[I]` or `gent.DefineTool`. The handler returns `*ToolExecutionResponse` and an
`error`. Gent owns input validation, typed conversion, saved state, and provider message mapping.
A bound method can keep service dependencies on its receiver.

```go
tool, err := gent.DefineTool("lookup_order", service.LookupOrder).
    Description("Get the current order status.").
    Parameters(schema.Field("order_id").Description("The order ID.").MinLength(1)).
    OutputSchemaFrom(Order{}).
    Policy("Use the order ID supplied by the user.").
    Domain("Orders").Categories("lookup").Keywords("order", "status").
    SyntheticQueries("Where is my order?").
    UnrecoverableErrorInstructions("The lookup response is unavailable. Decide how to continue.").
    Recovery(service.RecoverOrder).
    Build()
```

`Build` checks fixed configuration. `MustBuild` panics on a setup error. Configure before sharing
between runs. Retained maps, slices, and byte buffers transfer ownership; getters return read-only
views. Call and recovery handlers must protect their own shared mutable state.

Input inference uses Go types and `json` field names. All fields are required by default, including
`omitempty` fields. Use `.Optional()` to permit absence. Pointer fields permit null but remain
required. No defaults are inserted. Struct schemas reject unknown properties. Dotted paths such as
`address.city` select nested fields. Field builders support descriptions, enums, formats, bounds,
string patterns, and array lengths. Local validation checks formats.

`ParameterSchema(raw)` supplies a complete schema instead of inference. It cannot be mixed with
`Parameters`. Use it for maps, interfaces, embedded fields, custom JSON, or recursive types.
Nil permits any JSON object; use `struct{}` for a tool with no arguments. `Strict(true)` asks the
provider to enforce supported input constraints. Gent still checks the actual arguments and the
effective input after mutable hooks before invoking the tool. Invalid input produces safe correction
text with field paths and the complete expected schema.

`OutputSchema(raw)` and `OutputSchemaFrom(example)` set the same optional domain-data schema. The
last setter wins. Inference uses the example's type, not its field values. This schema describes
output to the model; it does not cause local result validation. The author is responsible for
matching returned data to the declaration. Adapters use a native declaration when applicable and
otherwise include the schema in description text. It does not describe framework status fields.

### Execution responses and content

| Return | Content sent to the model |
| --- | --- |
| `ToolExecuted`, nil error | Ordered Parts, then Instructions. Domain data defines success. |
| `ToolBlocked`, nil error | Configured blocked sentence, then Instructions. Parts are omitted. |
| `ToolInvalidArguments`, nil error | Correction details, complete input schema, and Instructions. |
| Nil response, non-nil error | Safe authored guidance. Raw error stays in private diagnostics. |
| Non-nil response and error | The response is rejected. Both returns are saved for diagnostics. |

Returning both values is a contract violation, not a partial-result feature. Nil/nil and invalid
response contracts use the safe error path. A tool error ends the operation, not the agent run.
Cancellation, persistence failures, and configured limits keep their separate run-level behavior.
An error cannot prove that an external action had no effects.

Use `Instructions` for required model guidance. For Blocked, include approval references there;
parts from a blocked response do not reach the model. Gent includes accepted instructions once,
even when JavaScript uses only one data field. Error guidance uses the tool's
`UnrecoverableErrorInstructions`, then execution `SteeringMessages.ToolError`, then Gent's default.

```go
parts, err := gent.NewParts().Data(order).PDF(labelPDF).Build()
if err != nil {
    return nil, err
}
return &gent.ToolExecutionResponse{
    Status: gent.ToolExecuted,
    Parts: parts,
    Instructions: "Check the label before shipment.",
}, nil
```

`DataPart(any)` captures portable structured data, including exact integer meaning and JSON names.
It does not require authors to serialize structs or restore their original output Go type.
Custom JSON encoders own their encoded shape and numeric tokens. Gent does not infer field or
element types inside that representation. Tool input validation also respects this boundary:
it validates the custom representation against the explicit schema, including after input hooks.
`TextPart` keeps exact text. `BytesPart`, `ImagePart`, `PNGPart`, `JPEGPart`, `GIFPart`, `WebPPart`,
`PDFPart`, `AudioPart`, and `VideoPart` make media explicit. `ImagePart` detects its image format;
the named helpers do not convert or validate file contents. The builder reports construction errors
from `Build` and returns no partial list. Provider compatibility is checked later.

`ImageURLPart` stores a remote reference. The adapter sends the URL when supported, or downloads
bytes when required. The caller owns URL lifetime and content changes, including signed URL expiry.
Use a byte part when saved state must preserve the original content. A compatibility download is
not a promise to snapshot a URL forever. An observed load failure produces safe text with the URL.
Unsupported media produces safe text with its MIME type. Original parts and replacement reasons
remain in private evidence. Configure fallback wording with per-execution `SteeringMessages`.

The same parts work in initial `Task` messages, execution replies, and recovery evidence. Gent's
codec saves byte content and captured data. Applications store the resulting `SaveBatch`; they do
not supply a media codec. There is no tool-result deduplication. Compaction remains independent.

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
native controls return errors. Media delivery uses the explicit steering fallback described above.
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
See [provider protocols and supported features](models.md). Unsupported native controls return errors; unsupported media uses safe steering. A schema that cannot use native enforcement still receives complete local validation.

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

Use `ExecutionContext.SubscribeAllWithConfig(DeliveryConfig)` for live chunks, or
`SubscribeEvents(DeliveryConfig)` for the full numbered feed. Provisional chunks can arrive before
their grouped save. `RecordedEventObserver` remains committed-only. The same chunk subscription
carries provisional metadata and, after the terminal save succeeds, the accepted answer. The
provider `Stream` carries provisional chunks and a candidate response; it never accepts an execution
answer.

1. Append each metadata delta to the preview identified by its `MessageId` and type. Keep entry
order.
2. On `execution_content`, replace the final answer with `Content`. This is a complete ordered content snapshot.
   Hide or replace previews whose IDs occur in `SourceMessageIds`. Other updates can remain visible.
3. Identify an answer by its `ContextId` and final `MessageId`. Child contexts have separate
answers.
4. Deduplicate delivery by `(RunId, EventNumber)` within the current connection. After recovery,
   discard the old preview and reset from saved state even at a lower cursor. Ignore callbacks
   from the old connection. Decode saved events with `CodecRegistry.DecodeEvent`, which fills
   these cursor fields from the saved envelope.

Live queues have configurable count, byte, and timeout limits. A slow subscriber is disconnected
without stopping execution. Reconnect and reload saved state after `ErrDeliveryOverflow` or
`ErrDeliveryTimeout`. Keep unsaved live events across periodic saved-state refreshes on the same
connection. See [grouped saves and live limits](history.md#grouped-saves-and-live-queue-limits).

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
maps, slices, scalars, time values, and raw JSON. Convert SDK objects to these types before
completion.
Provider callback options such as `WithStreamingFunc` expose raw intermediary deltas before Gent's
save barrier. Use `Recorder.Save` for persistence and bounded live subscriptions for WebSocket
delivery. Keep socket errors separate from storage results.

## ToolChain contract

`ToolChain` requires native definitions, preparation, step execution, recovery guidance, codec
registration, and compatibility configuration. All built-in chains implement the same contract:

- `toolchain.New()` creates a `Registry` of typed tools.
- `toolchain.NewSearch(...)` creates a `SearchChain` with dynamic definitions.
- `toolchain.NewJsToolChainWrapper(chain)` adds local JavaScript execution.

Preparation decodes native JSON arguments and saves distinct logical call IDs. Execution validates
schemas, converts typed inputs, runs before-tool hooks, saves effective input before an effect, and
saves the accepted outcome and prepared reply before the next step. Events, limits, instructions,
and media use this path.
Provider call IDs pair API messages. Gent call IDs identify operations across worker attempts.

Custom chains can embed the registry and override selection:

```go
type SelectedTools struct {
    *toolchain.Registry
    Select func(*gent.ExecutionContext, []*gent.ToolDefinition) ([]*gent.ToolDefinition, error)
}

func (c *SelectedTools) NativeTools(ctx *gent.ExecutionContext) ([]*gent.ToolDefinition, error) {
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

`SharedLocalSearch` exposes search, `execute_tool`, and pinned tools. A search returns complete
current definitions as content. It does not promote each result to a top-level provider function.
Use `execute_tool` with `{"tool":"lookup_order","args":{"order_id":"O1"}}`. The selected target uses
the same validation, hooks, records, and recovery as a direct call. The outer schema alone is not
enough; correction guidance includes the complete selected tool schema.

Search engines return `ToolSearchResult{Status, Names, Instructions}`. Empty names with Executed
means no matches. InvalidArguments supplies query correction guidance; Blocked supplies a condition
that prevents searching. A backend Go error stays private and produces safe model guidance.
Search traces retain query inputs, engine calls, rankings, and returned definitions for diagnostics.

The active process supplies the current catalog after resume. Recorded definitions remain historical
evidence. Catalog changes do not reject continuation. `RegisterDeprecatedTool` retains execution
and recovery bindings but excludes the tool from search and pinned definitions. A missing new-call
binding produces configured unavailable-tool guidance. A missing unfinished recovery binding uses
recovery-error guidance. An unsaved local catalog search may run again; business calls do not.
Search and visibility are not access controls. Tools must enforce their own business permissions.

## Programmatic tool calling

```go
chain := toolchain.NewJsToolChainWrapper(toolchain.New().RegisterTool(lookupOrder))
agent := react.NewAgent(model).WithToolChain(chain)
```

`run_code` accepts `{"code":"..."}`. Scripts use `tool.call` and `tool.parallelCall`; each returns
Gent's common model reply view. `parallelCall` returns an array in request order. The fields are
`toolCallId`, `name`, `kind`, `parts`, and applicable `executionStatus` and `recoveryStatus`.
Absent status fields are omitted. Safe errors are ordinary text parts, with the error kind in
metadata. Rejected returns and raw Go errors never enter the script.

```javascript
const reply = tool.call({tool: "lookup_order", args: {order_id: "O1"}});
if (reply.kind === "execution" && reply.executionStatus === "executed") {
    const data = reply.parts.find(part => part.kind === "data");
    // Check the tool's domain status before using its data.
    if (data && data.data.status === "ready") {
        tool.call({tool: "read_shipment", args: {shipment_id: data.data.shipment_id}});
    }
    tool.forward(reply.parts.filter(part => part.kind === "bytes"));
}
```

Text, data, URLs, and required instructions appear automatically in the outer reply. Do not log them.
Byte parts are read-only `Uint8Array` views with `mimeType`; their automatic output is an omission
notice. `tool.forward(parts)` selects original byte parts in the same script, replacing their notices.
It does not start a model call. There are no handles or references for a later script. The model
can request a fresh tool call if it later needs bytes. The application must explain when that is
appropriate, especially for tools with external effects. Direct tool calls include bytes normally.

`console.log` is for values computed by the script. Captured integers use JavaScript number by
default, including its precision limit. Set `gent.WithPTCIntegerMode(gent.PTCIntegerBigInt)` on the
execution context to use bigint for all captured integers. Floats stay number and byte elements
stay Uint8Array numbers. Gent preserves bigint in tool arguments and logs; native JSON.stringify
still rejects bigint. Saved data and automatic output use the original exact parts in either mode.
Steering and integer mode are saved per execution. Concurrent runs can share one wrapper.

Direct mode advertises the business functions beside `run_code`. `WithDirectCallDisabled` puts
their definitions in the code tool description and blocks direct business calls. Each script uses
an isolated VM. Parallel calls use independent saved plans and wait for all started workers, including
after cancellation or persistence failure. Use them only for independent work. Tools, hooks,
search engines, and custom formats must support concurrent calls. The VM remains on one goroutine.
A panic interrupts the worker context, waits for siblings, then propagates the first panic.

Custom chains must implement `gent.ParallelToolChain` and return true from `SupportsParallelCalls`
for parallel batches. Calls go through the supplied chain's `ExecuteNext`, including overrides on
an embedded registry. An interrupted script is not restarted. Accepted outer replies, including
byte selections, are reused unchanged after resume. A progress save can accept the outer record
before the control checkpoint closes the program. Resume checks that record first, clears the old
program state, and preserves the saved reply and counters. It does not recover completed inner calls.

## Tool recovery

Each tool implements `Recover(context.Context, *gent.ToolRecoveryRequest[I])`. The typed request
contains the original identity, arguments, and saved effective input. Before-call hooks and current
input-schema validation do not run again on this input. Recovery must be safe to repeat because a
worker can stop before its report is saved. It must inspect the action, not perform it again.

| Recovery status | Execution | Meaning sent to the model |
| --- | --- | --- |
| `ToolRecoveryCompleted` | Required `*ToolExecutionResponse` | Recovered original response; it may be stale. |
| `ToolRecoveryNotStarted` | Nil | Evidence establishes that the action did not start. |
| `ToolRecoveryUnresolved` | Nil | The original outcome cannot be confirmed. |

Every report can include evidence Parts and Instructions. Completed also includes the recovered
execution's content and instructions. No status causes automatic execution. The model decides
whether a fresh call is needed after reviewing all saved and recovered results. A call that never
reached its ready boundary receives NotStarted without executing it. A ready call uses its handler.

A missing handler, a failed typed-input reconstruction, a Go error, or an invalid report produces
separate recovery-error guidance. It does not claim that the original action failed. Completed
records and known call errors are reused without invoking recovery or requiring their old Go types.
Gent saves accepted outcomes, prepared replies, diagnostics, and statistics together. Resume uses
the saved wording and does not count old errors or successes again.

Fresh Executed and Blocked responses reset consecutive tool-error gauges. InvalidArguments has
separate counters and leaves those gauges unchanged. Recovery errors have separate total and
per-tool counters; other recovery reports do not reset call-error gauges. Limits can still end a
run. The model receives content, not serialized framework wrapper fields; adapters can use the
metadata for native fields such as Anthropic's `is_error`.

`ToolCallRecord` keeps Gent identity, target, arguments, effective input, typed outcome, original
returns, diagnostics, and `Response()`. The response is `*ModelToolCallResponse` with prepared Parts,
provider call ID, model-requested function name, and status metadata. Shared execution can have a
target name different from the model-requested function. `record.ModelMessage()` makes the tool-role
message. All adapters consume that common message through `ModelCallRequest.Messages`.

Set ReAct's `WithRecoveryVersion` for application behavior that cannot be checked structurally.
Catalog definitions are excluded from the compatibility hash. The application owns business meaning
and any upgrade rules for old input. Default decoding permits added and removed struct fields;
changed declared field types fail reconstruction. See [history and recovery](history.md).

The recorder must check its worker lease in each atomic save. A rejected save stops an old worker
before it can start another call; an external action already in progress can still complete.
Gent supplies evidence, not an exactly-once guarantee about external systems.

## Tests

Unit and fixture tests cover typed calls, provider IDs, output schemas, hooks, limits, dynamic
search, dependent JavaScript calls, saved provider items, cancellation, and fresh-worker recovery.
Recovery cases include interruption before an effect, an effect with a lost result, partial program
completion, lost recovery acknowledgments, and worker takeover.

`TestNativeLive` runs actual OpenAI and Grok calls. Each provider has a code-only case and a search
then JavaScript case. These assert actual dependent tool inputs, code execution, and shared
search definitions. Saved inner results must show both business calls inside the same `run_code`
result. An initial script error without business effects can be corrected within the iteration
limit. A no-op program followed by direct business calls does not pass this check.
Set `GENT_TEST_OPENAI_KEY` / `GENT_TEST_XAI_KEY` or `OPENAI_API_KEY` /
`XAI_API_KEY`. Optional `GENT_TEST_OPENAI_MODEL` / `GENT_TEST_XAI_MODEL` select models. Cases skip
when credentials are absent. A fixture pass is not a live-provider pass.

Diagnostic JSON includes private record fields and readable prepared parts. Do not send a serialized
ToolCallRecord to the model. Use its Response through a tool-role message. Use CodecRegistry for
saved state; diagnostic JSON is for trace display and does not retain every Go numeric type.
