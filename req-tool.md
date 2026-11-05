# Tool results and recovery

This file records the design discussion. "Lock this in" means update this file with the decision,
reason, and limits. Implement the complete change in one later pass. Do not treat this file as a
claim that the code already meets these requirements.

The current code was checked at `c19c05a`. Dated evidence below retains its original revision.
Only explicit approval makes a topic a decision. A request for thoughts, a suggested name, or
discussion of a flow is not approval. Later dated approvals take precedence over earlier proposals.
The requirements use the approved response names. Recorded source evidence retains the old names.

## Existing principle from AGENTS.md

### Explicit results

- Use result structs and typed enums for expected conditions that the caller must handle.
- Do not encode expected states in Go error types or special error constructors.
- Use a non-nil Go error for an unexpected operation failure without an accepted result. At a tool
  boundary, this does not itself end the agent run. AGENTS.md now states this scope explicitly.

## Locked decisions

### Main requirement: lightweight ownership and copying

- Do not copy or clone only for defensive safety against caller misuse. Gent must stay lightweight.
- Keep framework-owned state private and expose getters. Document ownership, permitted changes,
  and how long a caller can use a returned view. Do not clone each getter result for protection.
- A getter that returns a slice, map, pointer, or byte buffer can still expose mutable data. State
  that the view is read-only when changes are not permitted; private fields alone do not enforce it.
- Use documented ownership transfer where Gent retains submitted values. A caller must follow that
  contract. Do not add copies only because the caller could break it.
- Copy only when a feature needs independent values, such as an editable request that must preserve
  history, an independent mutable worker value, or a stable checkpoint snapshot.
- A stable encoded snapshot can meet the storage need without another deep clone. Reuse owned,
  read-only payloads where possible. Do not copy large media at each internal step.
- Copy only the data that the feature must keep independent. A required independent mutable byte
  buffer must not share its backing array; a read-only payload can be shared under its contract.
- Review and replace ownership contracts before removing existing copies. Preserve supported hook
  edits, historical evidence, recovery, and concurrency. Do not remove all clones in one edit.
- Document the feature that requires each retained copy. Caller mutation after ownership transfer
  or callback return is a contract violation, not a reason for automatic defensive cloning.

This requirement supersedes earlier blanket instructions to clone at each ownership boundary.
It does not remove the need for stable saved evidence or isolation between permitted mutations.
The API draft now specifies constructors and getters. Internal storage remains an implementation
detail; it must meet the ownership and saved-state contracts.

### Error text and instructions

- Never insert a raw Go error into model input. This applies to direct results, JavaScript output,
  recovery reports, and any history or diagnostics later used as model input.
- Keep the original error available to the caller and saved diagnostics. Do not swallow it.
- For a non-nil Go error, ignore the returned tool result and its `Instructions` for model input.
  Preserve both returned values as diagnostic evidence, as specified below.
- For a nil Go error and a valid result, include all nonempty `Instructions` in the model-facing
  result. Apply this rule to every status, including blocked actions and domain failures.
- Preserve instructions with their call through storage, restoration, and result formatting.
- Apply the same rule to direct calls, search calls, and calls inside `run_code`.

Raw errors can contain service responses, request data, or internal details. Explicit text separates
diagnostics from information intended for the model. Instructions must not disappear because a
wrapper selects a different output path.

Tool authors do not need to return a full result with a non-nil error. A handled outcome returns
the result with nil error. An unexpected operation failure can return nil result with the error.
Returning both a non-nil result and a non-nil error violates the Tool.Call contract. This is not a
supported partial-result path. State this rule clearly in the public method documentation.
If a tool violates this rule, Gent excludes the entire result and its instructions from model input.
It still saves both returned values for troubleshooting. This fallback does not make the return
pair valid. The safe guidance comes from UnrecoverableErrorInstructions or a default, not from the
rejected result.

### Diagnostic storage

- Save the original Go error evidence in the state or events supplied to SaveBatch.
- If a tool returns a result and a non-nil error, save both with the same call identity. Preserve
  the rejected result's parts, status, and instructions for troubleshooting.
- Keep this diagnostic record separate from the response selected for model input. Saving a
  rejected result does not make it an accepted response after restore.
- Never use raw diagnostic errors or rejected results as input to the main model, compaction
  model, or model-written JavaScript. A debug record must not become an alternate prompt source.
- Keep checkpoint and storage failures visible through the existing persistence error contract.
  Do not claim that evidence was saved when SaveBatch was not accepted.

The existing SavedError format preserves error text, causes, and recognized error codes. It does
not preserve every private Go field or recreate every concrete error type. An application codec can
preserve such details when required. This limit does not require a custom codec for ordinary errors.
The current tool-call path drops a returned result when an error is present; that path must change.

### Blocked content and wrapper metadata

- ToolExecutionResponse.Parts is valid only for Executed. A Blocked result supplies guidance in
  Instructions.
- For Blocked with nil error, send the configured blocked sentence followed by Instructions.
  Do not send Parts. Put an approval reference or other required context in Instructions.
- Keep Gent wrapper fields and enum values out of model content. Use authored content and plain
  steering sentences. Provider-required call identity still belongs in the native API request.

For example, "The requested action was blocked." is Gent's configurable sentence. "Approval is
required. Ask the user to approve request A123 before requesting execution again." is the tool
author's Instructions. The second sentence is appended; it does not replace the first.

### No Unknown in normal tool results

- Do not add `Unknown` to the normal tool-result status enum.
- Keep uncertainty about an interrupted operation in `ToolRecoveryResponse`, such as `Unresolved`.
- Do not turn a recovery report into a normal tool result only to fit a provider response field.

The provider's tool-result field can carry either observation. Its name does not require both kinds
of observation to share a Gent result type or status enum.

### Executed names a returned call

- Use `Executed` instead of `Responded` for a tool that executed and returned a response.
- This status does not claim business success. Data and instructions describe the domain outcome.
- Keep interrupted-call uncertainty in the separate recovery response.

The shipment approval selects Executed and Blocked. The later input-validation approval adds
InvalidArguments to the shared ToolStatus enum, including search results. There is no Failed or
Unknown status. Non-nil errors use a separate safe response path.

### No StructPart or exact output-type restoration

- Do not add StructPart. DataPart preserves portable tool output for storage and model input.
- Do not promise restoration of the original named Go output type or its methods.
- A custom hook that needs that output type after restore needs a separate typed-state contract.
- Keep the existing type responsibilities for tool input and accepted Output[T] values.

The user accepted this limit. The tool can process its original value before it returns parts.
Removing DeduplicateSummary removes the built-in reason discussed for a typed output hook.

### Recovery returns control to the model

- Use only Completed, NotStarted, and Unresolved as recovery states.
- Do not add RetryImmediately. Remove RetryAllowed from the later implementation.
- Restore saved results and collect recovery reports before the next model request.
- Do not execute a pending call only to fill its result slot.
- A NotStarted result does not direct Gent to call the tool. The model decides the next action.
- Do not resume an old tool batch or restart an interrupted script as an implicit recovery action.

The user rejected the immediate-retry exception, including for read tools. Time and domain state
can change during an interruption. A recovered result can also be old or no longer valid.

### One public tool response

Use the existing response type for a prepared reply correlated with the model's tool call:

```go
type ModelToolCallResponse struct {
    // Private state. Constructed with ToolCallRecordBuilder.
}

func (r *ModelToolCallResponse) ToolCallID() string
func (r *ModelToolCallResponse) Name() string
func (r *ModelToolCallResponse) Kind() ToolResponseKind
func (r *ModelToolCallResponse) ExecutionStatus() (status ToolStatus, present bool)
func (r *ModelToolCallResponse) RecoveryStatus() (status ToolRecoveryStatus, present bool)
func (r *ModelToolCallResponse) Parts() []ContentPart
```

The getters expose read-only views under the ownership requirement above. A status getter returns
the valid status and true when present, or the zero value and false when absent. Absence is not a
new execution or recovery state. Section 6 of the API draft specifies the record builder and the
request-specific WithParts method. The builder constructs the record and historical reply together.

The prepared-parts approval removed full Execution and Recovery branches from this wrapper. The
later metadata approval adds Kind, ExecutionStatus, and RecoveryStatus. ToolCallRecord retains the
accepted responses and diagnostics. Data and media stay as parts; preparation does not flatten them
into text. Go errors produce safe text parts without a fabricated execution response or a separate
ErrorInstructions field.

Gent owns provider encoding and call correlation. Do not add a public ToolObservation type. Keep
raw diagnostics outside this model-facing response. The dated approval below records the accepted
record consistency rules, ownership requirements, and response metadata. Keep the three enum types;
do not consolidate them. Provider encoding can use the metadata without sending it as content.

### Public API documentation

- Document Tool, each Tool method, and the public result and recovery types at their declarations.
- Document each content constructor and builder method. Include purpose, arguments, return values,
  ownership, errors, defaults, and recovery behavior when they apply.
- Explain status meaning without implying domain success or automatic retry.
- Explain schema scope, inference limits, native delivery, and description fallback.
- Put URL lifetime and byte-preservation rules beside ImageURLPart and byte constructors.
- Include practical examples. Explain why a caller would select each interface or constructor.

The user requires ample API documentation in the implementation. Short signatures in a review
draft do not satisfy this requirement. Use clear STE text; omit filler and repeated comments.

### One tool builder and one optional output schema

The user approved ToolBuilder[I] in section 3 of [the tool API](req-tool-api.md).
Keep one DefineTool entry point. Do not add DefineDataTool or an output type parameter.

The approved builder surface includes:

- Description, Parameters, ParameterSchema, Strict, and Policy.
- Domain, Categories, Keywords, and SyntheticQueries.
- Recovery and UnrecoverableErrorInstructions.
- OutputSchema and OutputSchemaFrom.
- Build and MustBuild, with the previously proposed return types.

```go
func (b *ToolBuilder[I]) OutputSchema(value map[string]any) *ToolBuilder[I]
func (b *ToolBuilder[I]) OutputSchemaFrom(example any) *ToolBuilder[I]
```

Both setters update the same optional schema value. OutputSchema accepts a map, including one made
with Gent's schema builder. OutputSchemaFrom uses the Go type, not example field values. Build
reports inference and schema-definition errors. No output-type registry is needed for inference.
This approval concerns the tool-definition builder. Content-part construction is a separate surface.

### Output-schema delivery and purpose

- Keep OutputSchema as structured metadata through Gent's common tool definition.
- The adapter sends a native field when the endpoint and response contract support it.
- Otherwise, put the schema in description text.
- OutputSchema gives the model more information when Gent supplies available functions.
- Do not validate returned tool data against OutputSchema locally.
- Do not reject, repair, or retry a tool result because its data differs from that schema.

The schema is descriptive. Build can still check the schema definition and report inference errors;
that is separate from checking runtime results against it. Part encoding, ownership, and provider
capability checks still apply. The schema does not change the content-parts result interface.

Do not assume native support from an OpenAI-compatible API label or weaken the schema silently.
Do not claim that a native schema declaration establishes the validity of a client-supplied result.

### Per-tool unrecoverable-error instructions

- Add `UnrecoverableErrorInstructions() string` to the Tool interface.
- Empty text selects a safe framework default. Each tool can supply its own authored text.
- Do not pass the raw Go error into this method.
- Expected failure-specific guidance belongs in `ToolExecutionResponse.Instructions`.
- For Tool.Call errors, choose the first nonempty text in this order: the tool's
  UnrecoverableErrorInstructions, the execution's tool-error SteeringMessages field, Gent's default.

This method supplies safe guidance when a tool returns a non-nil Go error. It is separate from the
rejected result's `Instructions`. The exact default text is not locked. The default must not claim
that the action had no effects or that retry is safe. An external action can complete before its
response is lost. A tool error alone does not stop the run.

### ImageURL delivery without provider knowledge

A caller can add an ImageURL part to a message or tool response. Gent's model caller wrapper selects
URL or byte delivery from the selected model and provider API capabilities. The caller does not need
to know the provider's transport rules or write a separate tool for each provider.

This is an ease-of-use goal for message and tool authors. It includes initial messages in `Task`.
The wrapper must handle the provider-specific preparation behind the common content interface.
The URL rules and steering configuration are locked below. Later approvals select the parts builder,
its construction-error behavior, and the content helper names and signatures in the API draft.

### ImageURL ownership and load failure

- `ImageURLPart` selects a remote reference. The caller owns content changes and URL expiry.
- Document this tradeoff beside `ImageURLPart`. Cover mutable URLs, signed URL expiry, and use
  after interruption or retry. Make this guidance easy to find.
- Callers who need saved bytes must use a byte-backed image or file part. Helper names are approved.
- Do not download a URL only to preserve its content for later requests. A download for provider
  compatibility does not create a promise to preserve the remote image.
- Replace an observed URL load failure with safe text and the URL. The model decides what to do
  next, including whether to request the tool again. Gent does not repeat the tool itself.

These rules replace the earlier proposal to capture URL content for exact replay by default. A URL
and saved bytes have different storage guarantees. The helper documentation must make this clear.

### SteeringMessages

- Rename SteeringErrorMessages to SteeringMessages. Some guidance describes a blocked action or
  recovery evidence, not an error.
- Use this per-execution configuration for Gent's model-facing sentences. Include blocked actions,
  tool errors, recovery states, URL load failures, and unsupported media.
- Let the caller supply authored messages. The default states what happened and leaves the next
  action to the model. Field names are approved; choose default wording under these rules.
- Convey the configured text to the model. Do not interpret its words as execution commands.
- `FULL STOP` is only an example of caller-authored text. It has no special execution meaning.

The name identifies the recipient and purpose of these messages. This setting does not add a new
termination control. The caller can select text that fits the task on each execution.

### Remove tool-result deduplication

- Remove tool-result deduplication in the later implementation pass. Do not only disable it.
- Remove `DeduplicateSummary` from Tool, ToolChain, builders, wrappers, and reflection checks.
- Remove saved summary fields, result hashes, and prompt replacement code used only by this feature.
- Remove or update its tests, examples, and documentation. Do not retain an unused extension hook.
- Keep compaction interfaces, strategies, triggers, and checkpoint behavior intact.
- Keep call IDs, recovery, operation duplicate prevention, and stream replay handling intact.
  Those mechanisms do not deduplicate tool-result content in model input.

The user wants a smaller tool interface while the best result-reduction policy remains unsettled.
The research found different equality rules and prompt-cache tradeoffs. Revisit this feature only
after separate research defines its benefit, policy, and effect on saved evidence.

Compaction and result deduplication both reduce model context, but they are separate mechanisms.
The current compaction package does not call `DeduplicateSummary`. Removing deduplication can
increase model input size and make compaction run sooner. Keep the compaction feature and its
behavior; do not promise unchanged token use or trigger frequency. Deduplication removal does not
require a new compaction algorithm. If the separate parts proposal changes stored content types,
adapt content readers without changing compaction policy or its storage contracts.

## Tool-error scope clarification

The user clarified that an unrecoverable tool error does not mean the whole run must end. The safe
message is intended for the LLM so it can decide how to continue. The earlier claim that every such
error stops the run was an incorrect interpretation and must not guide implementation.

AGENTS.md now states this tool-boundary distinction. Run-level checkpoint failures, cancellation,
and limits keep their own contracts.
Do not use a default tool-error message that falsely states the run has stopped.

## Approved API and implementation scope

The public API review is complete. Provider encoding details follow the approved contracts during
implementation. Content helpers, steering configuration, and JavaScript values are approved.
The recovery states, absence of automatic retry, and ModelToolCallResponse shape are locked above.
Concrete getters, construction interfaces, and absent-status access are specified in the tool API.

The approved public API is in [req-tool-api.md](req-tool-api.md). The completed review makes the
requirements ready for implementation. This documentation task does not itself start implementation.

### Provider boundary

- Tool authors return Gent result and recovery types. They do not select provider message roles,
  native result fields, or native call IDs.
- Custom agent loops use one Gent message contract for all supported adapters.
- Adapters own provider encoding and native call correlation.
- The loop supplies the preceding assistant call and its provider continuation, then the matching
  tool response. A generic tool role alone is not sufficient to identify a call.
- Media support can differ by provider. Report unsupported content explicitly; do not silently
  remove it or require each custom loop to reproduce ReAct's provider work.

## Five-call recovery example

Before interruption, the model requested five calls. Three results were saved, the fourth call was
in progress, and the fifth had not started. A replacement worker restores the run.

| Call | Known evidence | Observation for the next model request |
| --- | --- | --- |
| 1-3 | Saved results | Original results with their original times |
| 4 | Outcome cannot be confirmed | Recovery state and tool-specific steering |
| 5 | Execution did not start | Recovery state that states the call was not started |

Gent sends one correlated response for each original call. The response can contain a normal result
or a recovery report. A provider's tool-result field carries either kind of observation. It does not
require a claim that the tool executed.

After these observations, the model decides whether to inspect current state, retry, change the
plan, ask the user, or stop. Gent must not execute call 5 merely because execution did not start.

"Planned" and "started" must remain distinct. The current native path stores all planned calls but
marks only the active call ready. When saved evidence proves that call 5 never reached dispatch,
Gent can produce its recovery report without a remote recovery query. If all calls are marked ready
before dispatch, that marker alone cannot prove which calls started.

## Proposed recovery limits and open details

These are recommendations and questions for discussion, not locked requirements.

- A recovery check must be safe to repeat. It inspects the earlier operation and returns facts and
  steering. It must not repeat the original operation as a hidden side effect.
- A missing result does not prove that an external job stopped. The old worker or remote service
  may still complete the operation. Worker ownership and tool-side duplicate prevention still apply.
- A new model decision does not bypass tool policy or approval. Instructions guide the model;
  deterministic checks still enforce execution constraints.
- Keep original result times and report when recovery occurred. Domain expiry belongs in the tool
  data. The model can then request a fresh check without rewriting the old result as current truth.
- A confirmed result can still be old. Recovery must distinguish retrieving an old result from
  checking current domain state. This also applies to saved recovery reports after another crash.
- A new provider call ID is not a business duplicate-prevention key. Preserve or expose the original
  operation reference so a new model call can check or repeat the same operation safely.
- Retain the complete interrupted batch and its steering until the model receives them. Compaction
  must preserve the evidence needed for the first decision after recovery.
- Keep the accepted typed responses in ToolCallRecord. Provider adapters receive prepared parts,
  call identity, and derived status metadata. Do not infer status from steering text.

## Proposed API details

These details need further agreement before implementation.

### Normal status

The approved shared ToolStatus values are Executed, Blocked, and InvalidArguments. Domain outcomes
remain in data and Instructions. InvalidArguments identifies input that must be corrected.

- Executed means a tool executed and returned a response. A domain Declined value can use it.
- Blocked means a required condition prevented the requested action.
- InvalidArguments means supplied input was rejected. Gent handles schema and conversion checks;
  a search engine can also use this status for invalid query syntax with safe Instructions.
- A status must not imply that a partially completed operation had no side effects.
- A non-nil Go error discards the tool's returned result but still needs a safe model response.
  The earlier Executed proposal referred to a Gent-created fallback after Tool.Call returned nil
  and an error. It did not require the tool author to return a result and an error together.
  The prepared-parts approval puts safe steering in ModelToolCallResponse.Parts(). Do not broaden
  Executed or require a valid domain result when the tool could not produce one.

Here, discarding a result means excluding it from model input. The locked diagnostic-storage rule
requires saving it with the error. Do not discard the evidence itself.

Do not add shared `Code` and `Message` fields without a caller need. Typed output can contain domain
details, and `Instructions` can supply the next step. InvalidArguments uses safe guidance to correct
the input.

### Recovery states

Completed, NotStarted, and Unresolved are locked. No recovery state directs immediate execution.
An active job can be Unresolved with an operation reference and steering. Do not invent NotStarted
when the remote operation may still be active.

A recovered-result state means evidence is available. It does not mean business success or that the
result is still valid. An unresolved state does not authorize repetition.

### Error-message convenience and default text

The method, empty-text fallback, and builder setting are locked above. Function tools need no
custom wrapper to configure this text.

Proposed default: "An internal error occurred in this tool call. Decide how to continue."
This wording is not locked. It does not claim that the operation had no side effects.

The user approved Tool.Call error precedence: nonempty UnrecoverableErrorInstructions, then the
execution's SteeringMessages tool-error text, then Gent's built-in default. A recovery-check error
has a distinct path and message; do not silently reuse text that describes failure of Tool.Call.

### Call outcomes and invalid returns

The user's latest list distinguishes an Executed response, a Blocked response, a recovery response,
and a non-nil Go error. A non-nil result beside that error is diagnostic evidence only. This list
describes observed call outcomes, not every stage of an in-progress execution.

Recommended handling for the remaining edges:

- Gent can produce Blocked before Tool.Call for denied policy. InvalidArguments is approved for
  input rejection and replaces the earlier Blocked recommendation for that case.
- Nil result with nil error, an invalid status, and inconsistent recovery fields use the same
  failure path as a non-nil error. This handling is now approved. Preserve rejected diagnostics.
- A pending or interrupted call is a lifecycle state. Use the existing recovery contract to learn
  its outcome. Do not add Unknown to ToolExecutionResponse.
- Cancellation, run limits, and checkpoint failures remain run controls, not another tool status.
- An error from Recover has its own approved failure path and steering message. Retain any
  rejected report for diagnostics. Do not claim that the original action failed.

The prepared-parts approval settles the safe model reply and the record's field responsibilities.
Keep one response contract for custom loops and adapters. Raw errors remain in private diagnostics.
Keep the three enum types. The API draft supplies their constant spellings.

### Recovery text

Use SteeringMessages for the plain recovery sentences. The API draft specifies this rendering order:

- Completed: render the recovered ToolExecutionResponse, then the recovery note, evidence, and
  Instructions.
- NotStarted: render its configured sentence, then recovery evidence and Instructions.
- Unresolved: render its configured sentence, then recovery evidence and Instructions.

The recovery states and steering fields are approved. Exact default wording can follow the approved
constraints during implementation. A recovered Blocked result uses its blocked rendering and has no
model-facing Parts. Recovery evidence is a separate report field.

### Schema authorship and model-facing content

The author is responsible for making returned data match the supplied schema, including avoiding
ambiguous multiple DataParts. This fits the locked descriptive-only contract. Gent need not infer or
verify that correspondence. OutputSchema describes domain data, not the Go ToolExecutionResponse or
ModelToolCallResponse structs.

Gent's wrapper metadata stays out of model content. The locked instructions rule remains: nonempty
ToolExecutionResponse.Instructions reaches the model when the result is accepted with nil error.
Recovery steering and safe error guidance also reach the model. Blocked uses the locked sentence and
Instructions rule above; its Parts are excluded.

Recommended presentation:

- Do not serialize the whole Go response struct or private execution metadata into model content.
- Render parts when the result permits them and include authored instructions. Keep diagnostics
  private, including a result that arrived with a Go error.
- Convey facts needed for the next decision, such as a blocked action or unresolved recovery.
  The literal Go field names and enum strings need not be the model-facing format.
- Keep OutputSchema separate from that steering. It describes the author's domain data.
- Do not create a schema for all possible framework states only because those states exist in Go.

The exact content rendering follows the approved contracts during implementation. Instructions must
not disappear to satisfy a schema, and Gent must not advertise a native shape that its own format
cannot represent. Use the locked description fallback when a native mapping is not applicable.

### Output-schema evidence checked on 2026-11-02

At 95bc765, nativeDefinitions appends OutputSchema as Returns text in the function description.
This is enabled by default. FunctionDefinition has no output-schema field. The OpenAI and Gemini
adapters send parameter schemas but no native tool-output schema.
An isolated request-capture probe checked both full declarations. It made no live provider call.

Current [OpenAI documentation][openai-tool-schema] describes output_schema for structured function
output. Its [Responses reference][openai-responses] also includes that field on function tools.
Current [Gemini FunctionDeclaration][gemini-tool-schema] supports responseJsonSchema or response.
These are tool-return schemas, separate from structured final-answer output controls.

This corrects any claim that those providers lack a native tool-output schema. It does not establish
support for every model, proxy, schema keyword, or content-part arrangement.

[openai-tool-schema]:
  https://developers.openai.com/api/docs/guides/tools-programmatic-tool-calling
[openai-responses]:
  https://developers.openai.com/api/reference/typescript/resources/responses/methods/create
[gemini-tool-schema]: https://ai.google.dev/api/generate-content#FunctionDeclaration

### Media and typed output

Keep tool results independent of conversation messages. A tool result has one call identity. A
`[]ModelMessage` result would let a tool select roles, create turns, and manage call ordering.
Those responsibilities belong to the framework.

The approved design uses ordered content parts without a separate generic Go output value. DataPart
captures ordinary structs for storage and model input. The tool can process its original Go value
before it returns. Restrict result content to data and media; nested calls are not ordinary media.

An earlier proposal kept a generic `Value` beside parts. The current draft removes that duplication.
It does not promise exact Go output-type restoration after resume. Typed tool inputs, accepted run
output, and application checkpoint state keep their own type and codec contracts. The later
shipment approval and the decision to omit StructPart settle this responsibility split.

A normalized native response should support these parts. Each adapter owns its mapping or fallback.

### Task and shared content preparation

`Task.Messages` already contains ordered `ContentPart` values. `NewTask(parts...)` creates one user
message. A caller can use `Task.Messages` for a system message and a user message with image parts.
No separate image field is needed on `Task` to meet the locked ImageURL goal.

Proposed placement: prepare content at the model caller boundary for both initial messages and later
tool responses. Keep provider roles, call correlation, and delivery choices out of tool code and the
generic loop. The same part must have the same preparation rules in either source.

The saved Task is the caller's original prompt. A prepared provider request is a separate record.
Keep resolved media and fallback evidence with their source parts without rewriting the saved Task.

### Media fallback and execution settings

Apply the locked ImageURL and SteeringMessages rules above:

- If the provider cannot accept a byte type, replace that part with text that explains the limit.
- Keep the other parts, tool instructions, and call identity. Do not repeat a tool to repair media.
- Keep original content and diagnostics for recovery and troubleshooting. Do not send raw errors.
- Handle identifiable load failures from Gent's loader or the provider. Do not label unrelated API
  failures as URL load failures.

Preserve the execution's fallback settings on resume. Record the part and reason
behind each substitution. A load failure does not mean the tool's domain action failed.

### DataPart and explicit content builder

The approved shipment pattern uses NewParts().Data(...).PDF(...).Build(). Gent owns capture,
storage, and provider rendering. Build reports construction errors and returns no partial parts.
`TextPart` remains available when the caller wants control of the text. An explicit builder exposes
supported kinds, including PDF, PNG, and ImageURL. The API draft's helper signatures are approved.

Reflection can capture field values and type information into a Gent format. Freeze owned values
before accepting the result. Preserve number types, null values, and field meaning through restore.
The checkpoint format is separate from model input. DataPart must not become an ordinary text part
only because its storage format uses JSON.

An `any` parameter does not make all values portable. Functions, channels, and live resources need
clear rejection or a separate supported contract. A named Go type also needs a binding on the new
worker if a typed hook needs that exact type. Saved fields and a type name alone do not supply
its compiled methods. Gent can restore neutral structured data without that exact concrete type.

The locked output-type limit requires portable values without exact Go output-type restoration.
There is no StructPart or standard typed hook after restore. Keep number and field meaning without
promising compiled methods or arbitrary Go objects. This does not remove existing codec needs for
typed inputs, accepted run output, or custom application state.

### Remaining interface choices explained

Later approvals settle some of the earlier review items:

- Approved parts builder: Data(any) can receive unsupported Go values, such as channels or cycles.
  Image(bytes) can fail to detect a supported image format. Collect construction errors and return
  them from Build with no partial result. Provider media limits are handled later by steering;
  they are not builder errors or OutputSchema validation.
- Approved search result: the later side discussion selects ToolSearchResult with Status, Names,
  and Instructions. Use that contract and SharedLocalSearch; do not reopen the search-result shape.
  The later approval retains query, query_type, and optional page. Shared execution takes one tool
  name and its args, then uses the existing result contract.
- Approved JavaScript result: expose ModelToolCallResponse with its common kind, optional status
  metadata, and ordered parts. Instructions and safe errors are already text parts. Raw errors and
  rejected results stay outside the script. PTC includes results automatically, uses same-script
  byte forwarding, and adds no saved-part references. Bytes use Uint8Array. Integer mode is saved
  per execution: number by default, with an explicit bigint option.
- Approved counter rules: fresh Executed and Blocked responses reset consecutive tool-error gauges.
  InvalidArguments uses separate tracking and leaves tool-error totals and gauges unchanged.
  Count recovery-check errors separately without changing the tool-error gauge. Restore accepted
  totals and gauges unchanged; do not count saved observations again. A declined domain response
  does not become a Gent error.

The counter rules preserve a mechanical distinction between outcomes. Gent must not inspect domain
data to infer success. Configured run limits can still end a run after repeated errors; one tool
error does not itself require termination.

## Current implementation gaps

These are observations about `95bc765`, not accepted requirements for the new design.

| Area | Current behavior | Source |
| --- | --- | --- |
| Recovery | `RetryAllowed` permits execution before another model request | `tool_plan.go` |
| Native batch | Recovery can continue calls that never started | `toolchain/checkpoint.go` |
| Recovery types | Converts unresolved recovery to `Outcome: "unknown"` | `tool_plan.go` |
| JavaScript | A permitted inner retry can execute during recovery | `toolchain/js_recovery.go` |
| Error text | Exposes raw errors | `toolchain/registry.go`, `toolchain/js_wrapper.go` |
| Instructions | Drops inner-call instructions | `toolchain/js_wrapper.go` |
| Tool payload | `ToolResult` has typed `Text`, `Media`, and `Instructions` | `tool.go` |
| Model payload | `ToolCallResponse.Content` is a string | `model_types.go` |
| Media | ReAct sends media in a later user message | `agents/react/native.go` |
| Initial prompt | Task already accepts ordered content parts | `agent.go` |
| Image URL | Gemini rejects URL parts and requires bytes | `models/gemini_messages.go` |
| Recovery output | Tool authors return a normalized result with rendered text | `checkpoint.go` |

Text tool-role messages already use one adapter contract across all ten supported providers. The
provider transport tests check this. The media work inside ReAct prevents full confirmation of the
same convenience for custom loops with media results.

The direct result path already includes instructions. Tool authors must still format the normalized
output returned by the current recovery interface.

The current `ToolResult.Text` field accepts typed data. Gent keeps strings as text.
It converts other values to JSON. Authors do not need to convert structs for normal tool results.
`gent.BinaryPart(mimeType, data)` makes a content part from bytes. Adapters encode the bytes.
Accepted media types still depend on the provider and model.

The current codec registers declared tool types and their nested types. Concrete values hidden in
interface fields need explicit registration today. The proposed automatic DataPart path is not yet
implemented. See `history_codec_types.go` and `toolchain/checkpoint.go`.

`Tool.DeduplicateSummary(I, O) string` is a typed tool-author hook. A nonempty summary permits older
calls with the same name and arguments to be abbreviated in later model requests. An empty string
disables this. It does not prevent tool execution or prove that two outputs are equal.
The normal tool path saves the summary with the result. Restore reuses that saved text without
calling the hook again. Unrecorded custom-loop results still have a renderer fallback.
The earlier discussion used this hook too broadly as an example of work repeated during recovery.

Gent already serializes BinaryContent, including its MIME type and owned bytes. Ordinary byte slices
use a versioned base64 value in portable state. Gent restores bytes without a custom media encoder.
The application's recorder stores batches durably. A custom loop must save its continuation data in
Gent history or checkpoint state; local variables are not saved.
See `docs/history.md`, `history_codec_bytes_test.go`, and `task_content_test.go`.

## Candidate checks after design approval

Checks for proposed behavior apply only if that behavior is approved.

- Check the five-call case: three saved results, one unresolved call, one call not started.
- Check that recovery produces all observations before any model-selected new operation.
- Check that no recovery state executes a pending call or restarts a JavaScript program.
- Check interruption before dispatch, after an external effect, and before result storage.
- Check repeated takeover, lost save acknowledgement, and external work still in progress.
- Check old results, expired domain data, and saved recovery reports after a second interruption.
- Check exact model requests for raw-error exclusion, default text, and each tool's override.
- Check instructions for every expected status, empty data, media, recovery, and JavaScript paths.
- Check all provider mappings with a custom loop, including call IDs and provider continuation.
- Check ToolCallRecord's kind and accepted Execution or Recovery field. Error outcomes have neither
  accepted response. Check their safe text in ModelToolCallResponse.Parts().
- Check complete API documentation for each tool method, result type, and content constructor.
- Check approved builder methods, schema inference, and native or description delivery.
- Check that OutputSchema does not cause local validation or rejection of tool result data.
- Check nil result with a Go error and valid result with nil error as separate tool-author paths.
- Check safe fallback when a tool returns a result and a Go error together.
- Check SaveBatch retains both returned values while model input excludes the rejected result.
- Check error text, causes, and codes survive storage and restoration without a custom error type.
- Check that saved diagnostics cannot enter main-model, compaction, or JavaScript input.
- Check Blocked omits Parts and appends Instructions after the configured blocked sentence.
- Check per-execution SteeringMessages for tool, recovery, and media paths.
- Check ordered media, unsupported media, storage, and compaction with the new result content.
- Check that repeated calls keep their full selected results until an existing context policy acts.
- Check removal of every tool-result deduplication hook and field. Keep other duplicate safeguards.
- Check ImageURL delivery from Task, ordinary messages, and tool responses through the same wrapper.
- Check direct URL and byte delivery against the selected endpoint and model capabilities.
- If approved, check expired URLs, changed remote content, and stored media after interruption.
- If approved, check execution fallback text, saved original content, and no repeated tool action.
- If approved, restore DataPart in a fresh process without the original output type.
- Run the full native, CGO-disabled, and race suites with the 300-second timeout.

## Tool search decisions confirmed on 2026-11-02

These decisions come from the side discussion. They update the model and search interfaces in the
earlier API draft. This section records requirements only; it does not authorize implementation.
Only the items under the locked subsections below are approved. The review items remain proposals.

### Locked: three tool search flows

Use ToolSearchFlow for this setting. It describes the provider interaction and the search location.

| Value | Search location and provider interaction |
| --- | --- |
| ProviderSearch | The provider runs search through its native discovery flow. |
| ProviderLocalSearch | Gent runs search locally and uses the provider's native discovery flow. |
| SharedLocalSearch | Gent runs local search and uses shared search/call functions. |

ProviderSearch covers supported providers such as Anthropic and OpenAI. Initial support for
ProviderLocalSearch is limited to OpenAI. This is Gent's selected support scope, not a claim that
no other provider can support client search. SharedLocalSearch uses ordinary native function calls.
Registry continues to expose direct native tools. SearchChain supplies tool discovery.

This approval replaces the earlier two-value proposal. It does not select the exact default among
the three values or the behavior of an explicitly selected flow on an unsupported endpoint.

### Locked: pointer-based model and search interfaces

Keep ToolChain as the execution interface. SearchableToolChain embeds it and exposes discovery.
The model adapter depends on these interfaces, not on SearchChain or a particular search engine.
Do not rename ToolChain to ToolCatalog or add a ToolCatalogKind selector for search capability.

Use pointers by default for public request, result, and definition structs. A required pointer is
not optional. Apply the main ownership requirement at hook, storage, and call boundaries. Their
existence alone does not require a copy.

The approved model interface is:

```go
type Model interface {
    PrepareRequest(
        ctx *ExecutionContext,
        request *ModelCallRequest,
        chain ToolChain,
    ) (*ModelCallRequest, error)

    GenerateContentStream(
        context.Context,
        *ModelCallRequest,
    ) (Stream, error)
}
```

PrepareRequest resolves definitions and the selected search flow. It does not call the model or
execute domain tools. The managed model-call boundary invokes preparation. Custom loops use the
same boundary without provider-specific branches. Runtime bindings must not become saved objects.

The approved search interface and definition are:

```go
type SearchableToolChain interface {
    ToolChain

    SearchDefinition(*ExecutionContext) (*ToolSearchDefinition, error)
    Search(*ExecutionContext, *ToolSearchRequest) (*ToolSearchResult, error)
}

type ToolSearchDefinition struct {
    Tools       []*ToolDefinition
    PinnedNames []string
    LocalSearch *ToolDefinition
}
```

SearchDefinition returns a consistent view of definitions and pinned names for the execution.
LocalSearch describes local query arguments and guidance. The search engine remains an internal
choice of the chain. Later approvals select ToolSearchRequest inputs and ToolSearchResult fields.
Use those approved shapes; they are no longer open review items.

Document the types and methods fully during implementation, including ownership and error behavior.

### Locked: search diagnostics use existing model-call events

Do not add a new event category solely for tool-search troubleshooting. Add this field to the
existing BeforeModelCallEvent for local searches used by the request:

```go
// ToolSearches contains diagnostics for searches used by this request.
// Gent does not send these diagnostics to the model.
ToolSearches []*ToolSearchTrace
```

ToolSearchTrace carries query, selected names, duration, flow, and private error details. Its exact
field layout can follow the existing event conventions during implementation. These diagnostics are
not an additional model message.
Preserve the actual search reply and provider call identity in the normal conversation data.

It is acceptable to lose local search diagnostics if the process stops before the next model-call
event is saved. No separate search-start event or live search-start signal is required by this rule.
Provider-hosted search occurs during the call. Keep its results in the existing after-call evidence.

Use the existing progress-save path. A control checkpoint is not required for each search.
The existing save barrier before a provider request still applies.

### Locked: repeat unsaved local catalog searches

A process can stop during a catalog lookup or after it returns but before its result is saved.
Gent can repeat that unsaved lookup. Do not add a separate search recovery state machine for it.
Define local catalog search as a repeatable read. Keep any search replies already accepted into the
conversation as historical evidence.

This permission applies to internal tool discovery. It does not change the locked rule that
Tool.Recover returns control to the model, or permit replay of an interrupted domain tool or script.

### Locked: the runtime catalog is separate from recorded model evidence

Build the available tool catalog and search index from the current application when an execution
starts or continues. Do not save an additional catalog object or index for use as a runtime binding.
Do not restore the executable catalog from old model-call records.

Model requests and responses retain their actual tool definitions. If a provider receives the full
catalog in a model request, the request record may contain that full catalog. This is acceptable.
Do not omit those definitions from model-call capture solely because the catalog is large.
This clarification replaces the earlier proposal for reduced request capture.

| Data | Treatment |
| --- | --- |
| Runtime catalog and index | Use current bindings; no separate saved catalog or index. |
| Definitions in model calls | Record the definitions sent or received, including a full list. |
| Search query and diagnostics | Save with the relevant model-call evidence. |
| Accepted search replies | Keep the historical reply and required provider items. |
| Tool IDs, arguments, results | Keep existing call and recovery evidence. |
| Application/catalog fingerprint | Optional diagnostics; not an automatic catalog-change barrier. |

New searches use the current catalog. Historical definitions do not register executable tools or
override the current application. The original task, model selection, and other saved execution data
keep their existing continuation contracts. Catalog rebinding is not permission to replace them.

### Locked: unavailable tools, changed schemas, and catalog compatibility

These requirements are approved. The earlier review label is superseded by the later decisions.

- Add a configurable SteeringMessages entry for a tool that was not found or is no longer available.
  Return safe model guidance and let the model choose another tool or search again.
- On input-validation failure, send safe guidance with the complete current parameter schema.
  Follow the locked InvalidArguments rules below. Keep raw Go errors in diagnostics.
- Exclude catalog definitions and discovery metadata from the continuation compatibility hash.
  Catalog changes alone must not reject continuation. Keep checks for incompatible saved state
  and execution configuration. This is an implementation requirement, not an open design choice.

The intended correction path uses the new schema in Registry's next model request. With search,
the model can request the current definition and decide whether to try again. Gent must not execute
a missing tool or bypass its current input validation.

The following implementation notes do not reopen these decisions:

- A provider can reject a missing historical tool reference before Gent receives another tool call.
  Anthropic documents an HTTP 400. Tool-result steering alone cannot repair this request.
- Strict native calling uses the loaded schema. If an old schema requires order_id and the current
  schema requires id, new steering text does not replace the native schema. A supported schema
  refresh is needed before the model can call with the new arguments.
- Schema validation cannot detect every change in meaning. If amount remains an integer but its
  unit changes, old arguments can still pass. Authors must retain meaning or use a new contract.
- The current codec decodes saved named Go structs before tool validation. Changed or missing fields
  and unavailable registered types can prevent restore. Removing a catalog hash does not fix this.
  Apply the locked decoder rules and the per-call recovery-error handling specified below.
- An old action that may already have run still needs the existing recovery treatment. A missing
  current handler does not prove that the earlier action had no effects.
- RecoveryConfig currently includes execution settings as well as tool definitions. Limit any hash
  change to its approved scope; do not silently remove all state and execution compatibility checks.

These limits do not add an approved migration API, automatic retry, catalog-rewrite policy, or
provider-reference repair method. Decide those details separately. Preserve original saved evidence
when preparing any corrected provider request.

The code was inspected at c19c05a. Current relevant locations are toolchain/registry.go,
toolchain/recovery.go, toolchain/js_native.go, agents/react/checkpoint.go, and
history_codec_decode.go.
Official references for the provider constraints:

- [OpenAI strict function calling][tool-search-openai-functions]
- [OpenAI tool search][tool-search-openai-guide]
- [Anthropic tool search and missing-reference errors][anthropic-search]

[tool-search-openai-functions]: https://developers.openai.com/api/docs/guides/function-calling
[tool-search-openai-guide]: https://developers.openai.com/api/docs/guides/tools-tool-search
[anthropic-search]: https://platform.claude.com/docs/en/agents-and-tools/tool-use/tool-search-tool

## Tool discovery decisions confirmed on 2026-11-03

This section records the two explicit approvals from the side discussion. It adds requirements,
not implementation. It does not approve changes to the saved-value decoder or removal of type
registration. Those questions remain under discussion.

### Locked: deprecated tools remain available for execution and recovery

Provide deprecated registration on both Registry and SearchChain:

```go
func (c *Registry) RegisterDeprecatedTool(tool any) *Registry

func (c *SearchChain) RegisterDeprecatedTool(tool any) *SearchChain
```

- Exclude deprecated tools from search indexes and advertised native definitions, including pins.
- Retain each tool's name, argument schema, execution handler, and recovery handler.
- Use the normal registration, input conversion, codec registration, and validation paths.
  Deprecation changes discovery. It does not remove the execution or recovery binding.
- SearchChain accepts a deprecated tool by name through its shared execution function. Registry
  retains name-based dispatch when a call to that tool reaches the chain.
- SearchDefinition.Tools contains discoverable tools. The execution registry can also contain
  deprecated tools. Do not expose those extra bindings as discovery candidates.
- Registration must not execute a tool, start recovery, or authorize an automatic retry.

A deprecated tool is hidden from discovery but remains callable. A call that names it is not proof
of a retry or an earlier execution. Do not add an old-run-only restriction to this contract.
This lets an application keep old handlers available while new searches use its current tools.

### Locked: retain model preparation and explicit search interfaces

Keep the approved pointer-based Model.PrepareRequest and SearchableToolChain interfaces above.
Do not replace them with the smaller Model interface proposed as an alternative in the discussion.

The managed model-call boundary receives the tool chain and invokes Model.PrepareRequest. A custom
loop does not copy tool definitions, manage pinned lists, or inspect concrete chain implementations.
The adapter depends on the interfaces, not on SearchChain or its search engine.

Use one shared preparation implementation for SharedLocalSearch. Provider adapters can delegate
to it. Do not duplicate the same preparation rules in each adapter.
Preparation resolves definitions. Requested searches execute through the managed tool-call path.
Keep request hooks, feature-required ownership isolation, recording, and the save barrier there.
Runtime tool-chain bindings must not become saved request objects.

These interfaces remain explicit extension points even when only one search flow is in scope.
The small-interface alternative can offer the same loop convenience, but it is not the chosen API.

### Locked: application authors own compatibility of business meaning

Gent must permit catalog changes without requiring an agent version change for each edit.
Application authors must preserve business meaning and handle older application data. The contract
is like changing backend code that must still work with its saved database records. Gent cannot
infer that an integer changed from dollars to cents when the schema still accepts an integer.

Give clear documentation, preferably beside the public types and methods. Explain:

- The supported continuation behavior when tools and schemas change.
- Zero values for added fields when the decoder permits those additions.
- The effect of renamed fields and the limits of automatic type restoration.
- Changes in units or meaning that schema validation cannot detect.
- Recovery duties for calls that may already have produced an external effect.
- The difference between a deprecated callable tool and a tool that is no longer available.

This responsibility boundary does not claim that every code or state change is compatible.
Document the decoder behavior that is actually supported. A request for clear documentation does
not itself approve the proposed field-compatibility changes.

## Recovery and saved-value decisions confirmed on 2026-11-03

This section records the latest explicit approvals. It replaces the earlier discussion limits for
completed-call type retention and the default struct decoder. These are requirements for the later
implementation. They do not claim that the current code supports the new behavior.

### Locked: typed recovery uses the saved effective input

- Keep Completed, NotStarted, and Unresolved as the only recovery states.
- Save the effective input at tool_ready, after input hooks have run.
- Find the registered tool by its saved name. Restore the saved input as I before calling the
  typed Recover handler with ToolRecoveryRequest[I].
- Tool authors do not decode EncodedValue themselves. Do not rerun input hooks during restoration.
- Keep the original parsed model arguments separate from the saved effective input.
- Return the recovery report to the model. Restoring input or reporting NotStarted must not call
  the tool again, resume the remaining batch, or restart an interrupted script.
- Reuse an accepted saved result or recovery report instead of invoking recovery again for that
  same completed observation.

Keep compatible old type bindings for unfinished typed recovery. When a deprecated tool and its
replacement coexist, give them distinct tool names. A saved name must select one recovery handler.
Compatible changes can retain the existing tool name. Deprecation alone cannot repair a removed
input type or an incompatible custom codec.

The recovery flow and typed input are approved. The later shipment approval also selects optional
recovery Parts and the complete tool-author example. Those items are no longer open review items.

### Locked: completed tool records do not require the original Go types

Ordinary continuation must not require an old tool's concrete input or output types merely to read
its completed response. Keep type registration where Gent must construct an actual typed value.

| Use after restoration | Original tool input type required? |
| --- | --- |
| Invoke typed Recover for an unfinished call | Yes. |
| Reuse a saved result or recovery report | No. |
| Send a completed tool response to the model | No. |
| Read ordinary historical diagnostics | No. |
| Explicitly reconstruct an old value for application code | Yes. |

Keep diagnostic values as encoded snapshots with their data and type information. Ordinary reads
must not instantiate the old Go objects. EncodedValue can carry such snapshots without decoding
their payloads. Keep the original saved evidence; do not remove diagnostics to avoid type decoding.

Apply this rule to saved events, tool-plan results, and history metadata. Clearing ReadyCall alone
is insufficient. A nested diagnostic field must not force typed reconstruction when the caller only
needs the accepted response. Preserve the separation between diagnostics and model-facing content.

Live hooks can still receive typed input. After restoration, ordinary continuation and diagnostics
use the saved representation. Exact typed reconstruction is an explicit operation that requires
the applicable registered type and codec. Actual typed application state keeps its own contract.

This narrows how long old tool registrations must remain available. Completed tool records alone
do not require keeping a deprecated tool or its types. Keep them while unfinished recovery or
explicit typed application use still needs them. The DataPart contract still does not promise the
original named output type or its methods.

### Locked: default struct decoding permits added and removed fields

Apply these rules when restoring the same registered struct with Gent's default field decoder:

| Change in the current struct | Required behavior |
| --- | --- |
| Add a field absent from saved data | Set its Go zero value. |
| Remove a field present in saved data | Skip it when constructing the current value. |
| Change an existing field's declared type | Return a decoding error. |
| Rename a field | Treat it as removal and addition; do not transfer its value. |
| Add or remove fields in a nested struct | Apply these rules recursively. |

- Added fields do not receive constructor values or JSON Schema defaults.
- Match fields by their saved Go field names. Do not infer a rename from a value or JSON tag.
- Preserve original saved records when skipping removed fields. Do not decode an ignored field
  merely to reconstruct a value that the current struct no longer contains.
- Do not silently convert numeric types, strings, pointers, or container types.
- A missing registered type remains an error when actual typed reconstruction is required.
- Custom codecs retain their own decoding contract. These rules do not rewrite their formats.

Preserve enough declared field-type information to detect a type change. Comparing only a decoded
value's assignability is insufficient: a saved string could otherwise enter a new any field.
Apply nested field rules without rejecting a struct only because an allowed field was added or
removed. Keep the distinction between a declared interface field and the concrete value it held.

These changes permit structural evolution. Application authors remain responsible for business
meaning, required invariants, and safe use of zero values after continuation.

### Required checks for the later implementation

- Restore completed results and diagnostics in a fresh process without the old tool input types.
- Keep saved parts, instructions, errors, and rejected-result diagnostics intact across restore.
- Require the correct type binding for unfinished typed recovery and explicit typed state.
- Verify that recovery receives the saved effective input and does not rerun input hooks or calls.
- Verify added, removed, renamed, nested, and changed-type fields with exact restored values.
- Cover declared-type changes that ordinary Go assignability would accept, including string to any.
- Verify that ignored removed fields do not require their old nested type registrations.
- Keep original evidence unchanged and preserve the independent custom-codec contract.

## Tool authoring and quick-start approval confirmed on 2026-11-03

The user approved the complete shipment example and its explanation. The requirements below are
locked. Implement them in the later refactor; this approval is not a request to implement code now.
This section replaces earlier statements that the shown recovery Parts field and authoring pattern
still need review.

### Locked: recovery evidence and complete tool authoring

ToolRecoveryResponse has these distinct fields:

| Field | Purpose |
| --- | --- |
| Execution | The recovered original ToolExecutionResponse; required only for Completed. |
| Parts | Optional structured data or media obtained during the recovery check. |
| Instructions | Optional authored guidance for the next model decision. |

Recovery Parts can describe an external operation that is still processing even when the original
result is unavailable. The model receives that evidence and the authored instructions. A text-only
handler can leave Parts nil and use Instructions. Do not require evidence merely to return a state.
Keep normal result parts separate from this recovery evidence. Recovery Parts do not establish that
the original operation completed or supply a substitute Execution value for Completed.

Approve the shipment example's tool-author interface: Tool[I], typed Call and Recover, the complete
ToolBuilder configuration, and the NewParts().Data(...).PDF(...).Build() pattern. Build returns
parts and an error; the example returns nil tool result when construction fails. Data captures a
portable value, and PDF supplies a byte-backed content part. The author does not serialize JSON,
construct provider messages, decode saved input, or perform checkpoint plumbing.

Use Executed and Blocked for normal tool responses. Business outcomes stay in the domain payload.
A declined shipment is Executed. A required approval is Blocked and puts the approval reference in
Instructions, with no model-facing Parts. An unexpected Go error uses nil result and the error;
Gent sends the configured safe guidance and keeps the raw error private. The later prepared-parts
and metadata approvals settle the framework shape for that safe reply.

OutputSchemaFrom(Shipment{}) describes domain data. It does not describe the PDF, recovery evidence,
or Gent wrapper fields. It does not enable local result validation. Policy is model guidance; the
application service must enforce approval before dispatch.

### Locked: update the tool creation quick start with the shipment example

Update the tool-creation lesson in docs/quickstart/basic_steps.py and its tested source in
docs/quickstart/example/. Use the approved shipment example below as the complete authoring example.
Keep the simple path easy to follow, then explain approval, media, errors, and recovery using the
same tool. Do not add the example's ShippingService or domain types to Gent's public API.

Keep the example runnable after the refactor. Supply an application service fixture for the guide
and tests, including a valid sample PDF. The service checks approval before dispatch and associates
the operation and its response with the stable call ID. Inspect returns CheckNotStarted only with
evidence; a missing record alone does not establish that execution never started.

Explain these points next to the relevant code:

- The builder supplies all Tool and IndexableTool metadata and schema methods.
- Gent validates input, converts it to ShipmentInput, and saves the effective input before Call.
- The application owns the shipment operation and recovery evidence.
- Parts preserve data and media; the adapter handles provider delivery and configured fallbacks.
- A domain decline does not become a Go error or change Executed into a business-success claim.
- A Go error excludes any returned result from model input and uses safe instructions.
- Completed returns the original response; it does not claim that old shipment data is current.
- NotStarted and Unresolved return control to the model without automatic execution.
- Unresolved can include operation evidence in recovery Parts.
- A restored recovery input is already typed. Tool authors do not decode saved values.

Update related tool-author examples in docs/native-tools.md so the two guides use the same contract.
Update source annotations, extracted snippets, example tests, and embedded project downloads.
Rebuild the standalone HTML from source; do not edit generated HTML directly.
Follow docs/quickstart/README.md for source extraction, offline browser checks, and example checks.
Run the required full Go suites after implementation, including the separate modules where changed.

### Approved shipment source for the later quick-start update

This source targets the approved future API. It is not expected to compile against the current
two-parameter Tool API. Package/import setup is included so the example can be checked as Go source.
The implementation must add the service fixture and executable guide entry point.

```go
package shipping

import (
    "context"
    "fmt"

    "github.com/rickchristie/gent"
)

type ShipmentInput struct {
    OrderID string `json:"order_id"`
}

type ShipmentStatus string

const (
    ShipmentNeedsApproval ShipmentStatus = "needs_approval"
    ShipmentCreated       ShipmentStatus = "created"
    ShipmentDeclined      ShipmentStatus = "declined"
)

type Shipment struct {
    ID      string         `json:"id,omitempty"`
    OrderID string         `json:"order_id"`
    Status  ShipmentStatus `json:"status"`
}

type ShipmentReply struct {
    Shipment          *Shipment
    LabelPDF          []byte
    ApprovalRequestID string
}

type CheckStatus string

const (
    CheckCompleted  CheckStatus = "completed"
    CheckNotStarted CheckStatus = "not_started"
    CheckUnresolved CheckStatus = "unresolved"
)

type ShipmentCheck struct {
    Status        CheckStatus
    Reply         *ShipmentReply
    OperationID   string
    ProviderState string
}

type RecoveryEvidence struct {
    OperationID string `json:"operation_id"`
    State       string `json:"state"`
}

type ShippingService interface {
    Create(
        context.Context,
        string, // Stable tool call ID.
        *ShipmentInput,
    ) (*ShipmentReply, error)

    Inspect(
        context.Context,
        string, // Original tool call ID.
        string, // Original order ID.
    ) (*ShipmentCheck, error)
}

type ShipmentTool struct {
    service ShippingService
}

func NewShipmentTool(service ShippingService) gent.Tool[*ShipmentInput] {
    tool := &ShipmentTool{service: service}

    return gent.DefineTool("create_shipment", tool.Call).
        Description("Create a shipment for an approved order and return its label.").
        Domain("shipping").
        Categories("orders", "fulfillment").
        Keywords("ship", "dispatch", "shipping label").
        SyntheticQueries(
            "Ship this order.",
            "Create a shipping label for an approved order.",
        ).
        Policy("Require approval before dispatching a shipment.").
        Strict(true).
        ParameterSchema(map[string]any{
            "type": "object",
            "properties": map[string]any{
                "order_id": map[string]any{
                    "type":        "string",
                    "description": "The order to ship.",
                    "minLength":   1,
                },
            },
            "required":             []string{"order_id"},
            "additionalProperties": false,
        }).
        OutputSchemaFrom(Shipment{}).
        UnrecoverableErrorInstructions(
            "The shipment outcome could not be confirmed. " +
                "Check existing shipments before requesting another.",
        ).
        Recovery(tool.Recover).
        MustBuild()
}

func (t *ShipmentTool) Call(
    ctx context.Context,
    input *ShipmentInput,
) (*gent.ToolExecutionResponse, error) {
    identity, ok := gent.ToolIdentityFromContext(ctx)
    if !ok {
        return nil, fmt.Errorf("missing tool call identity")
    }

    reply, err := t.service.Create(ctx, identity.ToolCallId, input)
    if err != nil {
        return nil, err
    }

    return t.result(reply)
}

func (t *ShipmentTool) result(
    reply *ShipmentReply,
) (*gent.ToolExecutionResponse, error) {
    switch reply.Shipment.Status {
    case ShipmentNeedsApproval:
        return &gent.ToolExecutionResponse{
            Status: gent.ToolBlocked,
            Instructions: fmt.Sprintf(
                "Obtain approval for request %s before requesting shipment.",
                reply.ApprovalRequestID,
            ),
        }, nil

    case ShipmentCreated, ShipmentDeclined:
        // Both are domain responses. The payload states the outcome.

    default:
        return nil, fmt.Errorf(
            "unexpected shipment status %q",
            reply.Shipment.Status,
        )
    }

    builder := gent.NewParts().Data(reply.Shipment)
    if len(reply.LabelPDF) > 0 {
        builder.PDF(reply.LabelPDF)
    }

    parts, err := builder.Build()
    if err != nil {
        return nil, err
    }

    return &gent.ToolExecutionResponse{
        Status:       gent.ToolExecuted,
        Parts:        parts,
        Instructions: "Use the shipment status to determine whether it was created.",
    }, nil
}

func (t *ShipmentTool) Recover(
    ctx context.Context,
    request *gent.ToolRecoveryRequest[*ShipmentInput],
) (*gent.ToolRecoveryResponse, error) {
    check, err := t.service.Inspect(
        ctx,
        request.Identity().ToolCallId,
        request.Input().OrderID,
    )
    if err != nil {
        return nil, err
    }

    switch check.Status {
    case CheckCompleted:
        output, err := t.result(check.Reply)
        if err != nil {
            return nil, err
        }

        return &gent.ToolRecoveryResponse{
            Status: gent.ToolRecoveryCompleted,
            Execution: output,
            Instructions: "This is the recorded response. " +
                "Check the current shipment state before changing the order.",
        }, nil

    case CheckNotStarted:
        return &gent.ToolRecoveryResponse{
            Status: gent.ToolRecoveryNotStarted,
            Instructions: "Review the current order and approval " +
                "before choosing the next action.",
        }, nil

    case CheckUnresolved:
        parts, err := gent.NewParts().
            Data(&RecoveryEvidence{
                OperationID: check.OperationID,
                State:       check.ProviderState,
            }).
            Build()
        if err != nil {
            return nil, err
        }

        return &gent.ToolRecoveryResponse{
            Status: gent.ToolRecoveryUnresolved,
            Parts:  parts,
            Instructions: "Check this operation before requesting another shipment.",
        }, nil

    default:
        return nil, fmt.Errorf(
            "unexpected shipment recovery status %q",
            check.Status,
        )
    }
}
```

### Current search scope from the later simplification

The later instruction selects SharedLocalSearch as the only search flow for this implementation.
It supersedes the earlier three-flow implementation scope. ProviderSearch and ProviderLocalSearch
are deferred; do not implement provider-hosted discovery as part of this refactor.

Registry advertises direct native tools. SearchChain uses shared search and execution functions,
plus pinned tools. Discovered tools are described in search results; do not promote them into new
target-specific native definitions. Retain Model.PrepareRequest and SearchableToolChain as approved.

### Implementation readiness

The later JavaScript approval completes the public API review. Scripts receive ModelToolCallResponse
with prepared parts and status metadata. Uint8Array represents bytes. The default integer mode uses
number; a per-execution option selects bigint. Automatic result inclusion, same-script forwarding,
and no saved-part references remain approved. Do not reopen these decisions.

The later shared-call approval settles ToolSearchRequest inputs and shared execution arguments.
The prepared reply, separate enums, and derived metadata are approved. The tool API supplies
construction and getter interfaces, including status presence. The 2026-11-05 approvals settle the
remaining content helpers, steering settings, record choices, and JavaScript values.

The later counter approval settles resets for fresh responses, unchanged tool-error gauges for
InvalidArguments, separate recovery-error accounting, and restoration without counting again.

The requirements are ready for the full refactor. Preserve the approved tool-author example,
recovery states, decoder rules, deprecated registration, and shared search direction. Exact safe
sentence wording and internal storage layout follow the approved constraints during implementation.

The requested documentation completion reconciles the earlier open-item lists and API draft with
the dated approvals. AGENTS.md now states the locked tool-boundary scope: a tool operation error
does not itself require ending the agent run. This documentation work does not authorize Go changes.

## Search engine result contract confirmed on 2026-11-03

The user approved ToolSearchResult and the change to ToolSearcher.Search. This section supersedes
earlier notes that these result fields and this return type remain proposals. It records the
contract for later implementation; it does not request implementation now.

### Locked: explicit search results use the shared tool status

Use this result and search-engine interface:

```go
type ToolSearchResult struct {
    Status       ToolStatus
    Names        []string
    Instructions string
}

type ToolSearcher interface {
    // Id identifies the search engine.
    Id() string

    // SearchGuidance explains how to write queries for this engine.
    SearchGuidance() string

    // IndexAll builds or updates the search index.
    IndexAll(tools []IndexableTool) error

    // Search returns all matching tool names, ranked by relevance.
    // SearchChain handles pagination.
    Search(ctx context.Context, query string) (*ToolSearchResult, error)
}
```

ToolSearchResult.Status uses the same ToolStatus type as ToolExecutionResponse.Status. Do not add a
separate search-status enum. ToolExecuted means the search ran; an empty Names slice means no
matches. ToolBlocked means the query could not proceed, such as an invalid query. Instructions
supplies authored guidance to the model, including how to correct the query.

Names is execution data, not only diagnostic data. SearchChain uses these names to obtain matching
tool definitions and include them in the search reply. This does not promote discovered tools into
new native definitions under SharedLocalSearch.

Unexpected search-backend failures return a Go error. Keep the raw error in private diagnostics
and send safe steering to the model. Remove the current ToolSearcher documentation that permits
raw Search errors to become model guidance. IndexAll retains its setup-error contract.

ToolSearcher is the search-engine interface used inside SearchChain. SearchableToolChain remains
the approved chain-level interface used for model integration. Its Search method uses the same
ToolSearchResult. This approval does not change the selected search flow or the approved method
signatures. ToolSearchRequest fields and the shared execution arguments are not selected here.

## Tool error and restore decisions confirmed on 2026-11-03

This section records the approvals from the error and counter review. The later input-validation
section approves and extends the InvalidArguments proposal. Neither section authorizes
implementation. The search, recovery, decoder, and shipment approvals remain in effect except where
the later input-validation decision replaces the invalid-query status.

### Locked: call-error text precedence

For Tool.Call errors, use the first nonempty setting in this order:

1. The tool's UnrecoverableErrorInstructions.
2. The execution's SteeringMessages entry for tool-call errors.
3. Gent's built-in default.

An explicit tool override takes priority over the task's template. Preserve the selected safe text
with its response. Keep the original Go error and any rejected result in diagnostic storage.

### Locked: invalid returns use the error path

Treat nil result with nil error, an invalid result tag, and inconsistent recovery fields as
operation failures. Use the same safe response path as a non-nil error from that operation.
Gent records a diagnostic error for the contract defect and preserves the returned evidence.
Do not fabricate Executed, publish rejected Instructions, or add a normal contract-defect status.

An invalid Call result uses call-error handling. An invalid Recover report uses the distinct
recovery-check error handling below. Domain output is still not validated against OutputSchema.

### Locked: recovery-check errors have separate steering

A non-nil error from Recover has its own safe response path and SteeringMessages entry. State that
the recovery check failed. It does not establish whether the original operation ran or completed.
Preserve both the error and any rejected report for troubleshooting. Do not fabricate a successful
recovery report or treat this error as permission to repeat the original action.

RecoveryError is the approved field name. Use the execution setting, then the built-in default.
Do not add a separate per-tool recovery-error method. Do not apply a Tool.Call-specific override
when it could misstate which operation failed.

### Locked: interrupted execution remains a lifecycle condition

Keep cancellation, storage failures, and run limits under their existing run contracts. They are not
normal ToolExecutionResponse statuses. Use the recovery contract for an unfinished call. Do not add
Unknown or automatic retry because execution was interrupted.

### Locked: parts construction reports one error

The parts builder collects construction errors and reports them from Build. Build returns no
partial parts on error. This includes unsupported Data values and failure to detect an image format.
Do not silently remove invalid content or require authors to handle an error after every addition.

Provider capability checks happen later. An unsupported provider media type uses the agreed
SteeringMessages fallback. It is not a construction error or local output-schema validation.
The complete shipment example already approves the Data/PDF/Build authoring pattern.

### Locked: JavaScript exposes an ordered parts array

Use a JavaScript array for the result's parts. Preserve order and expose data as ordinary
JavaScript values through explicit part access. Do not retain the single output-value assumption.
Gent preserves Instructions independently of which values the script reads. Raw Go errors and
rejected results stay outside the script.

The final JavaScript approval uses ModelToolCallResponse, with common kind and optional execution
and recovery status metadata. Parts is an ordered array with prepared instructions and safe error
guidance. Uint8Array represents byte content. Do not require scripts to parse steering text to
determine an outcome. PTC integer mode is number by default, with a per-execution bigint option.

### Locked: restore accounting without counting saved calls again

Restore counters and gauges from the latest accepted runtime snapshot, including saved progress
after the last control checkpoint. Restoring the same call does not increment or reset them.

| Step | Total tool errors | Consecutive tool errors |
| --- | --- | --- |
| Three errors accepted into saved state | 3 | 3 |
| Restore that saved state | 3 | 3 |
| Reuse those saved error responses | 3 | 3 |
| One new tool call returns an error | 4 | 4 |

The last row assumes the run limits permit another call and no intervening event resets the gauge.
Keep an already exceeded limit effective after restoration. Do not reset a run's budget on takeover.
The later counter approval settles resets for fresh nil-error results and input rejection.

A saved Tool.Call error is a completed observation. Send its safe response to the model; do not
call Tool.Recover or Tool.Call to replace it. The model decides any later action. A tool may still
have produced external effects, so the model can request an inspection through available tools.

If the error existed only in the old worker's memory and was never accepted into saved state, the
new worker cannot assume that outcome. That unfinished saved call still needs normal recovery.
The same distinction applies to results accepted after the control checkpoint in a progress save.

### Locked: InvalidArguments for input rejection

The later input-validation approval adds InvalidArguments to the shared ToolStatus enum. Gent
validates and converts input before invoking Tool.Call, then sends correction guidance to the model
itself when validation fails. Search results can also return InvalidArguments for invalid queries.
The full-schema message and shared-core requirements are specified in the later section.

Approved scope:

- Cover malformed argument JSON, parameter-schema rejection, and failed conversion to the input
  type when caused by the supplied arguments. Defective framework or tool configuration uses its
  own error path.
- Do not call the tool or silently repair its arguments. The model decides the corrected request.
- Render safe field-specific details and the complete current expected input schema with a
  configurable sentence. Do not send a raw validator or decoder error string.
- Preserve the guarantee if an input hook changes arguments before execution. Validate the final
  effective input as needed; do not claim that checking only the original request covers changes.
- Keep domain checks in the tool. Schema validation cannot prove that an order exists or that an
  approval is still valid.
- Under SharedLocalSearch, validate the selected target tool's schema. The shared execution
  function's outer schema is not sufficient for the target's arguments.

The later approval replaces Blocked with InvalidArguments for invalid search query syntax, even
when the query string passes the parameter schema. Search authors supply safe Instructions for
checks that Gent cannot infer from the schema. Backend failures retain the safe error path.

### Rebase and side-discussion review

The checkout moved from 95bc765 to c19c05a. The new commit keeps pending-save thresholds effective
during deferred terminal delivery and corrects subscriber count/byte capacity after receipt. It
does not implement the tool-result redesign or change the error accounting contract above.

There is no requirements.md in the inspected checkouts. The new tool requirements are appended to
req-tool.md. The current decisions include SharedLocalSearch only, pointer-based Model preparation,
explicit SearchableToolChain, approved ToolSearchResult, deprecated callable tools, typed saved
recovery input, portable completed diagnostics, and the default struct field-evolution rules.

The API draft must reflect those approvals. Do not treat the earlier value-based Model interface,
proposed search-result fields, or optional recovery Parts as current open decisions.

The later shared-call approval retains query, query_type, and optional page for search. Shared
execution selects one target with tool and args and reuses the common result contract. Carry each
target's input and output schemas with its discovered definition. Do not advertise one target's
OutputSchema as the result contract of a shared function that can execute many targets.

### Required checks for these decisions

- Check exact error-message precedence for a tool override, execution setting, and default.
- Check malformed Call results use call-error handling and malformed Recover reports use recovery
  error handling. Keep rejected evidence in SaveBatch and out of every model path.
- Check parts construction returns one error and no partial parts; keep provider fallbacks separate.
- Check JavaScript part order, data access, media forwarding, and automatic Instructions retention.
- Save three errors, restore in a new context, and verify both counters and gauges remain three.
- Accept an error in a progress save after tool_ready, then fail the completion checkpoint. Restore
  the latest accepted counts and reuse that error without calling Recover or executing the tool.
- Verify that only errors with no accepted saved outcome require recovery of an unfinished call.
- Check InvalidArguments across direct, pinned, and shared execution paths, including input
  hooks, safe correction details, and rejection before any tool side effect.

## Input-validation decisions confirmed on 2026-11-03

This section records the user's follow-up approval. It replaces earlier notes that input-error
details, full-schema rendering, or the InvalidArguments enum remain proposals. It also replaces the
earlier invalid-query example that used Blocked. These are requirements for the later refactor.

### Locked: shared InvalidArguments status

Add ToolInvalidArguments to ToolStatus. ToolExecutionResponse.Status and ToolSearchResult.Status use
this same enum. Do not add a separate status type for search. Do not use Go error types for expected
input rejection.

Gent returns InvalidArguments for malformed argument JSON, parameter-schema rejection, and
argument-dependent input conversion failure. Do not invoke Tool.Call for those rejected arguments.
A search engine can return InvalidArguments with nil Go error and authored Instructions when its
query is invalid, including query syntax that passes the JSON Schema. Gent cannot infer each search
engine's syntax rules. Blocked remains available for other conditions that prevent the action.

A check that detects an invalid query can itself run. InvalidArguments describes rejected input;
it does not claim that no validation code ran. An unexpected backend or configuration failure uses
the safe Go-error path. Do not infer an InvalidArguments response from raw error text.

### Locked: clear correction details and the full input schema

Each input rejection must identify the selected tool, clearly locate the invalid input, explain
the expected value or constraint, and include the complete current input schema. Print the actual
parameter schema, not only a list of fields or an example call. Keep nested definitions and schema
constraints. For malformed JSON, identify the parse location when available; do not invent a field
path before the input can be parsed.

Keep raw validator, decoder, and parser errors in diagnostics. Build model guidance from safe
validation details. Include any authored Instructions and the execution's configured steering text.
The model decides how to correct the request. Gent does not repair or repeat the call itself.

For shared execution, validate the selected target tool's arguments against its current schema.
Report that target's complete schema when its arguments fail. The shared function's outer schema
is not a substitute. A malformed outer request still uses its own available contract; do not invent
a target schema when no target can be selected.

For an invalid search expression, include safe engine-authored guidance as well as the search
function's complete input schema. The schema may only describe a string and cannot explain the
engine's query grammar by itself.

### Locked: one reusable validation and message core

PTC and shared execution must use the same reusable core for input validation and model-facing
correction details. Apply that core to direct and pinned tool calls too, so the same target input
gets the same field details and full schema through each entry point. Keep raw errors out of all
model paths. PTC can add script line context and an example call around the common message.

Current PTC code is the starting point: schema.Schema.FormatForLLM reports validation details and
expected fields. The pre-validation and runtime bridge paths add source context and an example
call. This does not yet print the full schema. Preserve the useful context while adding the full
schema and the safe error boundary. Existing fallback paths that return raw error strings must not
be copied into the new model-facing contract.

Gent must make schema declaration and validation easy for tool authors. Keep the approved schema
builder, explicit schema, and type-inference options. Validate the effective input after any hook
that changes it. Tools remain responsible for domain facts such as order existence and approval
expiry; a schema cannot establish those facts. OutputSchema remains descriptive only.

### Required checks for input validation

- Compare complete correction messages for missing fields, wrong types, nested fields, array items,
  rejected constraints, malformed JSON, and argument-dependent conversion failures.
- Verify that full schema output retains nested definitions and constraints that a field summary
  or example cannot express. Examples do not replace the schema.
- Check PTC pre-validation, PTC runtime calls, direct calls, pinned tools, and shared execution use
  the common field details and schema. Verify PTC keeps its additional source context.
- Verify shared execution uses the selected target's schema and does not call the target on invalid
  arguments. Check effective arguments after input hooks too.
- Check search returns InvalidArguments and safe authored Instructions for invalid query syntax,
  Executed with empty Names for no matches, and the safe error path for a backend failure.
- Verify every input-rejection message retains the full schema and required guidance under custom
  SteeringMessages, without raw Go error strings or Gent wrapper fields.

## Catalog compatibility and unavailable-tool decisions confirmed on 2026-11-03

This section corrects the stale review status and records the user's explicit approvals. It defines
requirements for the later refactor. It does not authorize implementation now.

### Locked: catalog changes do not reject continuation

Exclude catalog definitions and discovery metadata from the continuation compatibility check.
This includes tool definitions, pinned selection, and metadata used for tool discovery. Adding,
removing, or changing tools or their schemas must not by itself produce a compatibility rejection.

Use the current application's catalog when execution continues. Keep actual model requests,
responses, and earlier call evidence as recorded. Keep checks for incompatible saved run state
and execution configuration. Do not remove every compatibility check or require an agent version
change only because the catalog changed.

### Locked: unavailable tools receive safe steering

When a new call names a tool that is not available in the current execution registry, return safe
model guidance. Provide a default message and a per-execution SteeringMessages setting for this
condition. The model decides whether to search again, select another tool, or take another action.

Do not execute a substitute tool or repeat the call automatically. Keep lookup diagnostics private.
Apply this behavior to direct calls, shared execution, and calls inside PTC. A deprecated tool that
is still registered remains callable under the approved deprecation rules.

### Locked: recovery setup failures use the recovery-error response

Use the dedicated safe recovery-error response when Gent cannot invoke Recover for an unfinished
call because its handler is unavailable or its saved effective input cannot be restored. This
includes a missing input type or codec binding and a failure to decode the input under its contract.

Handle the failure at that call's recovery boundary. Do not reject the whole run merely because
that call's binding or input type is missing. Preserve the call identity, encoded input, and error
evidence for SaveBatch. Send safe recovery guidance through the dedicated SteeringMessages entry;
do not send raw lookup or decoder errors to the model.

State that recovery could not be completed. Do not claim that the original action did not run or
had no effects. Do not report NotStarted or fabricate another recovery result. Do not invoke Call,
rerun input hooks, or call Recover with fabricated input. The model decides the next action.

The default decoder's approved field-evolution rules still apply. Errors restoring core run state,
storage failures, cancellation, and run limits retain their existing contracts. This per-call
fallback does not make incompatible application state valid.

Completed responses remain readable without the old tool or type registration. Reuse accepted
saved responses under the existing replay rules; missing bindings do not turn them into unfinished
calls or cause another recovery attempt.

### Required checks for catalog changes and missing bindings

- Resume after adding, removing, or changing tool definitions and discovery metadata. Use the
  current catalog without rejecting the run solely because its catalog hash would differ.
- Verify that incompatible saved state and execution configuration retain their existing checks.
- Request an unavailable tool through each call path. Check safe configurable guidance and no
  substitute execution, automatic retry, or raw diagnostic text.
- Resume an unfinished call with a missing handler, missing input binding, or input decode failure.
  Check the dedicated recovery-error response, saved original evidence, and no Call or Recover.
- Verify that these failures never produce NotStarted or claim absence of earlier effects.
- Resume completed records without their original tools or types. Reuse their saved responses.

## JavaScript, input counters, and shared-call decisions confirmed on 2026-11-03

This section records the user's approvals. The catalog compatibility and unavailable-tool section
above settles those questions. The later prepared-parts approval replaces the safe-error field
proposal below. These decisions do not authorize the implementation pass.

### Superseded: a JavaScript equivalent of ToolExecutionResponse

The earlier approval selected an explicit object with status, ordered parts, and instructions.
The final JavaScript approval replaces that object with a view of ModelToolCallResponse. Reuse its
kind, optional statuses, and safe prepared parts. Do not retain a separate instructions field or
JavaScript-specific result/error variants.

The original goals still apply: scripts inspect typed status metadata and ordinary data values,
and Gent retains required guidance independently of script access. Automatic results, same-script
byte forwarding, and no saved-part references remain approved. Raw Go errors and rejected values
remain outside JavaScript. The common metadata is not rendered as wrapper JSON for the model.

### Locked: track InvalidArguments separately

Record InvalidArguments in separate input-rejection statistics. Do not add it to the tool-error
counter or increment the consecutive tool-error gauge. An expected input rejection is distinct
from an operation error. It still consumes the applicable ordinary call and run limits.

Preserve the approved restore rules for these statistics too: restore accepted values without
adding an old rejection again. New stat names and the detailed input-rejection limit interface can
follow the existing stats conventions during implementation.

The later counter approval requires leaving the tool-error gauge unchanged on InvalidArguments.
Do not reset or increment it. A separate consecutive input-rejection gauge and its reset rules
are not selected by this approval.

### Locked: shared search and execution arguments

Retain query, query_type, and optional page for search. Use the existing pagination defaults.
ToolSearchRequest carries these inputs. SearchChain keeps engine selection and pagination; the
approved ToolSearcher and ToolSearchResult responsibilities stay the same.

Shared execution accepts one target tool per invocation, using tool for its name and args for its
input object. For example:

```json
{"tool": "fetch_order", "args": {"id": "O123"}}
```

Use the same target schema validation, typed input, hooks, execution, result handling, and recovery
path as a direct call. Reuse the common result contract. Do not introduce a second result format.
Gent owns native call-ID correlation and the target's internal call identity.

### Superseded proposal: a separate safe error response field

Tool.Call can return nil with a non-nil error. Gent must still retain a correlated safe reply for
the model. There is no accepted ToolExecutionResponse or ToolRecoveryResponse in this case. Do not
fabricate Executed, Blocked, or a recovery state to fill the existing response fields.

The earlier proposal put CallError and RecoveryError kinds and an ErrorInstructions field in the
model wrapper. The prepared-parts approval replaces that layout. ToolCallRecord keeps the outcome
distinction, and ModelToolCallResponse.Parts() returns the safe text. Saving that text allows resume
without the original tool registration or access to private diagnostics.

The model receives safe content in its correlated tool reply, not Gent's kind or field names. The
original error and any rejected result stay private. This adds no Failed ToolStatus and does not
terminate the run. Keep the separate enums and use the constant spellings in the API draft.

### Required checks for these decisions

- Verify JavaScript sees the common response kind, optional statuses, and ordered parts. Check data
  access and automatic retention of prepared instructions, without raw errors or rejected values.
- Verify InvalidArguments increments separate statistics without changing tool-error totals or
  consecutive tool-error gauges. It must not reset or increment those gauges.
- Restore saved input-rejection counts without counting the same rejection again.
- Check search inputs and pagination, and single-target shared execution through the same target
  validation and recovery path as a direct call. Keep provider and internal call IDs distinct.

## Tool response and record naming confirmed on 2026-11-04

### Locked: rename ToolCallResult to ToolCallRecord

Use ToolCallRecord for the complete record returned by ToolChain.ExecuteNext. It records supplied
input and the observed outcome, with call identity, a safe model reply, and private diagnostics.
This name separates the execution record from the response that a tool author returns.

A record does not guarantee that execution started or that a valid output exists. Rejected input
can have no typed Input. A Go error can have no returned tool value. Preserve the available input,
return values, and error evidence under the existing storage rules. Do not fabricate missing data.

Apply the rename to the type, method signatures, collection element types, documentation, and
associated references in the implementation pass. No compatibility alias is required. Preserve
private diagnostics and the separate response selected for model input. This decision changes the
name. The later prepared-parts approval settles record responsibilities and removes error-response
fields from the model wrapper.

### Locked: use slices of call records

Remove ToolChainResult. Return []*ToolCallRecord directly where that type held the ordered records
from several calls. Update the corresponding arguments, collectors, plan data, and saved metadata
to use record slices. Do not add ToolChainCallRecords or another one-field wrapper in its place.

Preserve request order, call identity, partial records, and accepted diagnostic evidence. Removing
the wrapper does not remove batch execution, change concurrency, or authorize retry. ExecuteNext
still returns one *ToolCallRecord. No compatibility alias is required.

### Locked: response names and nested Execution fields

Within Gent tool calling, Response means information intended to form a model reply. Record means
the complete captured execution evidence. Gent prepares content and guidance; the adapter encodes
that content without sending Go wrapper fields or private diagnostics.

| Type | Responsibility |
| --- | --- |
| ToolExecutionResponse | Response from the normal execution path; replaces ToolResult. |
| ToolRecoveryResponse | Recovery report with an optional recovered execution response. |
| ModelToolCallResponse | Reply correlated with the original model tool call. |
| ToolCallRecord | Input, returned values, diagnostics, and the selected model reply. |

The original naming approval used Execution in ModelToolCallResponse and ToolRecoveryResponse.
The later prepared-parts approval moves the model wrapper's Execution and Recovery fields into
ToolCallRecord. ToolRecoveryResponse retains Execution. Tool.Call and its builder callbacks return
(*ToolExecutionResponse, error). Tool.Recover retains (*ToolRecoveryResponse, error).

Apply these names to the public declarations, examples, builder signatures, documentation, and
corresponding saved representations in the implementation pass. The later prepared-parts approval
settles the safe reply and diagnostic responsibilities. The later JavaScript approval selects the
common ModelToolCallResponse shape, byte representation, and per-execution integer mode.

### Locked: Execution names the path, not proof of invocation

ToolExecutionResponse does not prove that Tool.Call ran. Gent can create it with Blocked or
InvalidArguments before invocation. Its Status distinguishes those states from Executed.
Keep normal input validation and policy handling under their existing contracts.

Completed recovery requires Execution. NotStarted and Unresolved must leave Execution absent and
can supply recovery Parts and Instructions. Recovered execution data does not establish current
business validity or authorize another call. The model still decides what to do next.

### Superseded proposal: branches in Gent's model reply

For an unexpected Tool.Call failure, the valid return pair is nil, err. The tool author does not
construct an execution response or a second response for the model. A non-nil response with a
non-nil error remains a contract violation; Gent saves both only as diagnostic evidence.

Gent still owes the model a reply associated with the original call ID. The earlier proposal put
safe-error branches in ModelToolCallResponse. The later approval replaces those branches with
prepared Parts and retains typed outcomes in ToolCallRecord. No additional Tool.Call return value,
error type, tool method, or configuration setting is needed.

Save the selected safe text with the reply so restoration needs neither the old tool registration
nor private diagnostics. Recovery-check failures retain their separate safe message and record
outcome. Keep the separate enums and use the constant spellings in the API draft.

## Model tool-call naming confirmed on 2026-11-04

### Locked: pair the model tool-call request and response names

| Current name | Approved name |
| --- | --- |
| ModelToolCall | ModelToolCallRequest |
| ToolCallResponse | ModelToolCallResponse |

ModelToolCallRequest is the model's request for a tool call. ModelToolCallResponse is Gent's reply
to that request. Gent carries the reply in ModelMessage.Parts within the next ModelCallRequest.
The shared prefix identifies the exchange. Request and Response identify each part of the exchange.
The response is not the tool author's ToolExecutionResponse or the complete ToolCallRecord.

Apply both names to declarations, signatures, content parts, adapters, saved representations,
examples, and tests in the implementation pass. Preserve field meanings and original call IDs.
No compatibility alias is required. The current implementation evidence above retains its old names.

This naming approval did not select response fields. The later prepared-parts approval below
selects Parts and removes the earlier wrapper branches. The API draft supplies error-kind constants.

### Locked: rename ContentResponse to ModelCallResponse

Use ModelCallResponse for the complete response to ModelCallRequest. The current
ContentResponse holds choices and generation information. Each choice can contain candidate content,
tool-call requests, completion state, and provider continuation. The approved name describes this
full response and does not imply that Gent accepted a final answer.

Update Stream.Response and other consumers to use ModelCallResponse in the implementation pass.
Preserve fields, completion rules, and stream behavior. No compatibility alias is needed.
ModelCallRequest and ModelCallResponse describe a call to the model. ModelToolCallRequest and
ModelToolCallResponse describe the tool call that the model requests within that exchange.

## Prepared tool replies confirmed on 2026-11-04

The user approved the prepared-parts design, its native error flag limit, record responsibilities,
and atomic acceptance rules. These decisions replace the earlier model-wrapper branches. They
authorize documentation updates only. Go implementation and tests remain for the later pass.

### Locked: prepared content in ModelToolCallResponse

The initial prepared-parts approval selected ToolCallID, Name, and Parts. The later approval below
adds Kind, ExecutionStatus, and RecoveryStatus as metadata. Full Execution and Recovery branches and
ErrorInstructions remain absent. ToolExecutionResponse and ToolRecoveryResponse remain explicit
tool-author interfaces. ToolCallRecord retains the accepted responses and private evidence.

Prepare content before constructing the correlated reply:

- Executed supplies its accepted parts, then Instructions.
- Blocked supplies the configured blocked sentence and Instructions, without returned parts.
- InvalidArguments supplies safe correction details, the full expected schema, configured guidance,
  and Instructions.
- Recovery supplies the applicable guidance, recovery evidence, recovered content when present,
  and all accepted execution and recovery instructions.
- A Go error supplies the selected safe steering as an ordinary text part. A rejected return never
  supplies content or instructions for this reply.

Keep structured data and media as content parts. Select content once; provider adapters still own
native encoding, call-ID mapping, and media delivery. ModelMessage carries the response as a part
with role tool, and ModelCallRequest carries that message. Do not serialize the Go wrapper itself
as model content. Keep the original assistant call and required provider continuation.

A consumer can inspect the derived status metadata after the later approval below. Full accepted
responses and private evidence remain in ToolCallRecord. Prepared parts preserve the selected
wording after resume without restoring the earlier model-wrapper branches.

### Earlier decision: accept the native error flag limit

Anthropic supports an optional is_error field on tool results. The current Gent adapter does not
set it. The user accepted omitting this native flag with the three-field prepared-parts wrapper.
The model still receives the selected safe error text. Do not infer an error flag from that text.

The later status-metadata approval removes that information limit. It permits adapters to select
native request behavior from typed metadata. Verify exact provider mappings during implementation.
Do not treat the earlier limit as a permanent prohibition. Prepared safe text is still required.
See [Anthropic's tool-result documentation][prepared-parts-anthropic-errors].

[prepared-parts-anthropic-errors]:
  https://platform.claude.com/docs/en/agents-and-tools/tool-use/handle-tool-calls

### Locked: retain the typed outcome in ToolCallRecord

The user accepted these field responsibilities. The API draft supplies enum constants and getters.

| Fields | Responsibility |
| --- | --- |
| Kind | Distinguish execution, recovery, call error, and recovery-check error. |
| Execution | Accepted normal ToolExecutionResponse, when present. |
| Recovery | Accepted ToolRecoveryResponse, including its recovered Execution. |
| Response | Prepared ModelToolCallResponse used for model input. |
| RawArguments, Args, Input | Supplied arguments and saved effective input. |
| ReturnedResult, ReturnedRecovery, Error | Private snapshots of returns and errors. |
| Existing identity and discovery fields | Execution identity, target name, and search evidence. |

Move the accepted Execution and Recovery fields from the earlier model wrapper into this record.
For a call or recovery error, both accepted fields are absent. The prepared reply still exists.
Raw diagnostic error presence alone does not determine the outcome: an InvalidArguments response
can retain a private parser or validation error.

Keep the framework execution identity separate from the provider call ID. The selected target name
can differ from the model-requested function name under shared execution. Inner JavaScript calls
remain separate records; only the outer model-requested call gets the native provider reply.

### Locked: accept the outcome and prepared reply together

1. Capture original returns for diagnostics.
2. Check the return contract and select the accepted outcome.
3. Build safe reply parts once from the outcome, selected steering, and required validation details.
4. Freeze the outcome and reply together as owned evidence.
5. Save both with the corresponding execution evidence and statistics before dependent work starts.

Apply this rule to progress saves as well as completion checkpoints. An accepted completed outcome
must not lack its prepared reply. Save failures keep the existing persistence contract.

After acceptance, the typed outcome is authoritative for program logic. The prepared reply is
authoritative for historical model content. Reuse both after resume without independently rewriting
either, selecting new steering, or repeating instruction assembly. Record later model-request
transformations separately. Completed replies do not require the old tool or its original Go types.

### Locked: required copies must cover the permitted mutation

The later main ownership requirement replaces blanket deep cloning. When a feature needs an
independent mutable value, copy the nested data that can change. Copying only an outer struct or
slice does not isolate nested maps or byte buffers. Read-only payloads can remain shared under
their ownership contract. This rule does not require a copy where no feature needs independence.

### Resolved: retain separate enums and add response metadata

Retain ToolStatus, ToolRecoveryStatus, and ToolResponseKind. Search continues to use ToolStatus.
The user selected Kind, ExecutionStatus, and RecoveryStatus on ModelToolCallResponse. The dated
approval below records their purpose and preserves the prepared-parts design.

## Lightweight ownership and response metadata confirmed on 2026-11-04

### Locked: feature-required copies only

The user approved the main ownership requirement near the start of this document. Gent must not
copy or clone solely for defensive safety. Use documentation, private framework state, getters,
and explicit ownership rules. Review those contracts before removing existing clones. Required
features must still preserve independent mutable views, stable history, and checkpoint evidence.

This is a design requirement for the later implementation. It does not authorize Go changes or
tests during this discussion. Do not replace the earlier broad clone rule with an equally broad
ban on copies that a feature requires.

### Locked: three metadata fields on ModelToolCallResponse

The user explicitly confirmed Kind, ExecutionStatus, and RecoveryStatus. Keep the three enum types;
do not replace them with a combined ToolCallRecordStatus. These fields are adapter metadata beside
ToolCallID, Name, and prepared Parts. They do not contain full execution or recovery responses.

Gent derives the metadata from the accepted outcome. Save it with the prepared parts and record in
the same acceptance operation. Reuse it after resume. Do not derive it from raw diagnostic errors,
rendered text, or domain-data success values. Do not expose the enum names as model content.

The following presence rules describe the existing accepted response fields:

| Kind | ExecutionStatus | RecoveryStatus |
| --- | --- | --- |
| Execution | Accepted execution status. | Absent. |
| Recovery with Completed | Recovered execution status. | Completed. |
| Recovery with NotStarted or Unresolved | Absent. | Accepted recovery status. |
| CallError or RecoveryError | Absent. | Absent. |

A completed recovery can retain Blocked or InvalidArguments as its recovered execution status.
Do not invent Executed when there is no accepted execution response. Absence of a metadata field
does not add Unknown or another status. The API draft now specifies (status, present) getters.
Absent metadata returns the zero value and false. Empty status in an author response is invalid.

Adapters can use this metadata to select supported native request behavior, including native error
signaling where applicable. Keep the safe prepared guidance. A domain failure inside Executed is
still domain data. Recovery uncertainty is not proof that the original action failed. Status
metadata must not cause automatic retry or bypass the existing lifecycle and run-limit contracts.

## Counter decisions confirmed on 2026-11-04

The user approved the counter and limit recommendation. These rules replace the earlier proposals
for fresh-response resets, InvalidArguments, and recovery-error accounting. This approval covers
counters only. The final JavaScript decision is below. This discussion does not start Go changes.

### Locked: fresh responses and recovery errors

| New observation | Tool-error totals | Consecutive tool-error gauges |
| --- | --- | --- |
| Executed with nil error | No change. | Reset. |
| Blocked with nil error | No change. | Reset. |
| InvalidArguments | No change; track input rejection separately. | No change. |
| Recovery-check error | No change; count recovery errors separately. | No change. |

Keep existing increments for a fresh tool operation error. Gent must not inspect domain data to
classify an Executed response as a tool error. Apply the reset and unchanged rules to the existing
context-wide and per-tool consecutive tool-error gauges. New recovery-error counter names follow
the existing stats conventions. This approval does not add a new consecutive input-rejection gauge.

### Locked: saved observations are not new events

Restore accepted counters and gauges exactly, including accepted progress after the latest control
checkpoint. Reusing a saved execution response, error reply, or recovery report must not increment
or reset statistics. Count a new recovery-check error once; replay does not count it again.

A recovered execution response is evidence of the earlier operation, not a fresh Tool.Call.
Do not apply fresh Executed or Blocked reset rules to its embedded response. Keep configured run
limits effective after resume. A tool error alone still does not require ending the run.

## Documentation completion on 2026-11-04

The user requested completion of the documentation tasks. The API draft now specifies private
storage, construction interfaces, and read-only getters for records, prepared replies, and recovery
requests. Optional status getters return (status, present). They add no enum state.

ToolCallRecordBuilder constructs the accepted record and prepared reply together. It derives the
reply metadata from the accepted outcome. It receives content from the common safe preparation
path; it does not expose independent metadata setters or prepare instructions a second time.
WithParts supports a permitted request-hook transformation without changing the saved reply.
Both APIs follow the ownership rule and require no copies solely for defensive safety.

Earlier open labels now refer to the later approvals where applicable. AGENTS.md states the
approved tool-error scope. JavaScript does not require unconditional byte copies. Later approvals
settle its value choices, tool.forward, and the other final review items. The API document is now
req-tool-api.md. No Go changes or test execution are part of this documentation task.

## PTC output and byte forwarding confirmed on 2026-11-04

The user approved same-script byte forwarding and rejected saved-part references. This decision
preserves automatic tool-result inclusion and accepts that the model can request another PTC call
when it later needs omitted bytes. It does not authorize Go implementation during this discussion.

### Locked: automatic results with an explicit byte exception

- Preserve automatic inclusion of each accepted inner tool response in the outer run_code reply.
  The move to ordered content parts must not require scripts to log ordinary tool results.
- Include accepted text, data, and required guidance automatically. Keep the existing approved
  status and error rendering rules. Do not expose raw errors or rejected tool returns.
- Replace byte parts in automatic model output with configurable steering that explains the
  omission and the need to call tool.forward within the same script. Identify the MIME type and
  source call so the model can understand what was omitted.
- tool.forward(parts) selects byte parts available in the current script for inclusion. Deliver
  selected content instead of an omission notice for that part. Preserve data and media as parts.
- Keep all required instructions, safe errors, correction details, and expected schemas automatic,
  even when the script does not use a result or forward any bytes.
- Keep ImageURL delivery under its existing contract. Provider download does not turn an original
  ImageURL part into a byte part for this PTC rule. Direct native tool results keep their own path.

PTCBytesOmitted is the approved SteeringMessages field name. The omission behavior is approved;
default wording follows the existing constraints during implementation.
Forwarding does not imply provider support. Forwarded bytes use the existing adapter capability
checks and safe fallback. Original bytes remain in saved evidence even when omitted from the
automatic model reply.

### Locked: same-script scope and no saved-part reference API

- Do not add public saved-part handles, references, or a retrieval API for later scripts.
- A later script cannot use the previous script's variables or byte parts through tool.forward.
- If the model needs omitted bytes, it can request a new run_code call that calls the source tool
  and forwards the returned byte parts. Gent does not make this retry decision.
- Count that request as new execution under ordinary limits. Do not treat it as restoration of the
  previous result, and do not automatically retry tools or restart an interrupted script.
- Application authors must state when binary results are needed and when another call is suitable.
  Gent's PTC instructions must teach the byte exception and forwarding in the same script.
- Update the existing instruction that tool results appear automatically. Keep console.log for
  computed or customized values; do not require it for returned tool results.
- Reuse the accepted outer reply after resume, with its saved content and omission notices. Do not
  repeat a tool or script to fill an omitted byte part.

This rejects the suggested cross-script reference feature. It does not remove Gent's existing
responsibility to serialize byte content for state storage and recovery. The final JavaScript
approval below selects the common response view, byte representation, and per-execution integers.

### Required implementation checks

- Cover sequential and parallel inner calls whose results are never logged. Check automatic
  inclusion of data, text, and required guidance in their established order.
- Cover mixed data and byte results, omitted bytes, and same-script forwarding. Verify the actual
  next model request, including supported media delivery and unsupported-media fallback.
- Check that default PTC instructions and omission notices explain the same forwarding scope.
- Restore accepted PTC replies and check identical content selection, saved bytes, and no replay.
- Keep interrupted-script recovery separate from a new PTC call explicitly requested by the model.

### Readiness after the PTC decision

The behavior requirements are defined, including PTC byte forwarding. The 2026-11-05 approval below
settles the remaining helper and steering signatures, unavailable-tool handling, and input-guidance
storage. The final JavaScript approval completes the API review in section 12 of req-tool-api.md.

Provider encoding, native metadata mapping, exact safe sentence wording, and private storage layout
can be resolved during implementation within the approved constraints. They do not require another
architecture decision. The API review is complete and the document is now req-tool-api.md.

## Remaining Go API choices confirmed on 2026-11-05

The user accepted every item in the final API review except JavaScript values. This records the
remaining Go API and record choices. It does not authorize Go implementation or tests in this turn.

### Locked: content helpers and steering configuration

- Approve the content constructors and PartsBuilder methods in section 2 of the API draft, including
  the named image, PDF, audio, and video helpers. Preserve their stated signatures and behavior.
- Approve the SteeringMessages fields in section 5, including PTCBytesOmitted, and the
  WithSteeringMessages option. Empty fields select the built-in defaults.
- Approve CallOptions.SteeringMessages for direct model calls. Managed calls use the saved execution
  setting; direct calls use the request setting. Do not create competing settings on managed calls.
- Recovery errors use the execution-level RecoveryError message, then the built-in recovery default.
  Do not add another per-tool recovery-error method or reuse the Tool.Call-specific override.
- Overrides replace default steering text, not required correction details, schemas, or authored
  Instructions. Preserve the approved per-tool call-error precedence.

### Locked: unavailable-tool and input-guidance records

- A newly requested unavailable tool produces a CallError record with ToolUnavailable guidance.
  That kind does not prove Tool.Call ran. A missing recovery binding uses RecoveryError.
- For InvalidArguments, save safe correction details and the complete expected schema in the
  accepted Execution.Instructions, together with authored instructions. Include each item once.
- Keep original returned values and raw parser, validation, and operation errors in private
  diagnostics. The accepted normalized instructions must not contain raw Go error strings.
- Retain saved prepared replies without requiring old tool registration or rebuilding guidance.

### Resolved by the final JavaScript approval

The user requested an explanation of JavaScript values before approving the remaining interface.
The approval below selects ModelToolCallResponse, Uint8Array, and number by default with a
per-execution bigint option. It replaces the earlier separate JavaScript result/error variants
and automatic bigint conversion for large integers.

## JavaScript response and integer mode confirmed on 2026-11-05

The user selected the common response interface, approved ordered parallel results and Uint8Array,
and selected JavaScript number as the default with an explicit bigint option for each execution.
These decisions complete the public API review. They do not start Go implementation or tests.

### Locked: JavaScript uses ModelToolCallResponse

- Return the JavaScript view of ModelToolCallResponse from tool.call. Do not define a separate
  ToolReply, result kind, status field, instructions field, or safe-error variant for scripts.
- Expose toolCallId, name, kind, executionStatus, recoveryStatus, and ordered parts. These values
  map to the common Go response getters and use the same enum values and status presence rules.
- Omit an absent status property so reading it returns undefined. Do not fabricate a status value.
- Accepted instructions and safe errors are text parts already prepared by Gent. Keep raw diagnostic
  errors and rejected values outside JavaScript. Do not add guidance again during script conversion.
- Use the saved inner response to construct the view. Preserve the difference between inner Gent
  correlation IDs and the outer model-requested run_code ID.
- Keep recovery metadata consistent with the common type. This does not resume a script or deliver
  recovery reports to a restarted script. Existing recovery still returns control to the model.
- parallelCall returns an array of the same response objects in request order. The adapter retains
  responsibility for provider encoding; do not render the wrapper metadata as model content.

The script receives accepted byte parts from the inner response before PTC applies automatic byte
omission to the outer reply. This permits tool.forward to select those bytes in the same script.
The outer reply includes selected bytes or omission guidance, and keeps all required instructions.
Do not alter the saved inner response or add a cross-script lookup or saved-part reference API.

### Locked: JavaScript content parts and byte values

- Keep the common content kinds: text, data, image_url, and bytes, with their documented fields.
- Text remains a string. Data exposes ordinary JavaScript objects, arrays, and scalar values.
  Do not parse TextPart as JSON or restore an original named Go output type.
- Represent byte data with Uint8Array. Keep MIME type in the part. PDF and image helpers use this
  same shape; do not add a separate JavaScript variant for each format.
- Keep returned values read-only under the ownership contract. Avoid defensive copies and preserve
  byte-backed content in saved state under the existing serialization contract.

### Locked: integer mode is configured for each execution

- Use PTCIntegerMode with PTCIntegerNumber and PTCIntegerBigInt. Number is the default.
- WithPTCIntegerMode selects the mode when creating ExecutionContext. PTCIntegerMode() reads it.
  Save the resolved mode with the execution configuration and restore it on resume.
- Number mode converts captured integer data to JavaScript number, even outside the safe integer
  range. Large values can lose precision in the JavaScript view. Document this behavior explicitly.
  Do not automatically select bigint or decimal strings for large integers in this mode.
- BigInt mode converts all captured integer data to bigint, including small integers. Floating-point
  values remain number. Strings and Uint8Array values keep their types. Do not rewrite JavaScript
  literals, array lengths, or other language operations.
- Keep the original captured values in saved parts and automatic model output. Rounding in the
  default JavaScript view must not rewrite that source. Script computations and new tool arguments
  use the values actually present in the script.
- Gent handles bigint in tool arguments, automatic output, and console.log formatting. Preserve
  exact integer arguments and check the target Go type's range without conversion through float64.
  A script's own JSON.stringify retains native behavior.
- Default PTC instructions must describe the active mode and its precision and arithmetic rules.
  Store this configuration per execution, not as mutable state on a shared tool-chain wrapper.

This mode replaces the proposal to switch large integers to bigint automatically. It changes only
the JavaScript view and script behavior; it does not change the DataPart persistence contract.

### Required implementation checks

- Compare each JavaScript reply with its accepted ModelToolCallResponse: identity, kind, optional
  statuses, part order, and prepared guidance must agree after the documented value conversion.
- Check execution, blocked actions, invalid arguments, call errors, and absent status properties.
  Preserve safe text and exclude all private diagnostics. Check parallel result order.
- Verify byte access before the outer omission filter and same-script tool.forward behavior.
- Check the default number mode and explicit bigint mode with small integers, values outside the
  safe integer range, floating-point values, strings, and byte parts. Preserve the saved originals.
- Pass bigint values between tools, including target-range rejection. Check automatic output and
  console.log formatting without changing native JSON.stringify behavior.
- Restore the configured mode and accepted replies without replay. Use one shared wrapper in
  concurrent executions with different modes and verify that the settings do not affect each other.

### Ready for full implementation

All required public API choices are approved. The API document has been renamed to req-tool-api.md,
as requested once the review was complete. No implementation-blocking design question remains.
Provider mapping, exact default sentences, private storage layout, and required tests follow the
approved contracts in the later implementation pass. This turn changes documentation only.
