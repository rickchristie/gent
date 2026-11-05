# Tool API

This file records the approved target public API. It is not an implementation. Locked decisions
are recorded in [req-tool.md](req-tool.md). The examples use the target API and cannot run against
the current implementation.

Locked decisions remove tool-result deduplication, retain compaction, omit StructPart, and return
recovery decisions to the model. Executed replaces Responded. ModelToolCallResponse carries prepared
parts. ToolCallRecord distinguishes execution, recovery, and error outcomes.
The user also approved the main ToolBuilder interface, schema delivery, and no local validation of
tool result data against OutputSchema. Later approvals select the parts construction pattern,
recovery evidence fields, pointer-based model preparation, SharedLocalSearch, and search results.
The dated decisions in req-tool.md take precedence over older review notes.
The 2026-11-04 approvals use ToolCallRecord and ToolExecutionResponse. ToolCallRecord and
ToolRecoveryResponse have a nested Execution field. Batches use []*ToolCallRecord directly;
the ToolChainResult wrapper is removed. Prepared safe error text is an ordinary content part.
ModelToolCallRequest replaces ModelToolCall. ModelToolCallResponse replaces ToolCallResponse.
ContentResponse becomes ModelCallResponse. All three renames are approved.
The prepared-parts layout, record responsibilities, and consistent save rules are approved.
Keep separate enums. Kind, ExecutionStatus, and RecoveryStatus on the model reply are approved.
The main ownership requirement permits copying only when a feature requires independent values.
PTC includes accepted tool results automatically. Byte parts require tool.forward in the same
script; otherwise Gent sends omission guidance. No saved-part reference or retrieval API is added.
The 2026-11-05 approvals settle content helpers, steering configuration, record normalization, and
JavaScript values. JavaScript uses ModelToolCallResponse, Uint8Array bytes, and JavaScript number by
default. A per-execution PTC option selects bigint for captured integer data.

The public API review is complete. Unchanged framework types, such as ExecutionContext, Stream,
and SaveBatch, keep their existing interfaces unless a change is stated below. Code blocks show
declarations, not method bodies. Imports use context and github.com/rickchristie/gent/schema.

Records, prepared model replies, and recovery requests use private storage with the constructors
and getters below. Getters return read-only views, not defensive clones. The view remains valid
while its owner is retained. Callers must not change nested maps, slices, pointers, or byte buffers.
Tool-author responses and request authoring structs keep public fields for construction. After
submission, the documented ownership rule applies. ModelCallResponse keeps its approved fields.

ModelToolCallResponse connects prepared content to the model's tool call. Gent creates this reply;
the tool author returns ToolExecutionResponse or ToolRecoveryResponse. The adapter encodes the
reply for its provider; it does not send the Go wrapper as JSON content.

| Type | Produced by | Purpose |
| --- | --- | --- |
| ModelToolCallRequest | Model adapter | Tool call requested by the model. |
| ToolExecutionResponse | Tool.Call or Gent checks | Execution status, parts, and instructions. |
| ToolRecoveryResponse | Tool.Recover | Evidence about an interrupted call. |
| ModelToolCallResponse | Gent | Correlated reply for the loop and model adapter. |
| ToolCallRecord | Gent | Execution record with the reply and private diagnostic evidence. |

The model response retains the original call identity, safe prepared parts, and derived status
metadata. The adapter uses them to select its native tool-result fields. A tool author does not set
provider IDs or roles. A loop author does not assemble provider-specific payloads. The current API
has ToolCallResponse with ToolCallID, Name, and a Content string. The approved design replaces that
string with ordered parts. Full accepted responses and private evidence remain in ToolCallRecord.

For nil result with non-nil error, there is no ToolExecutionResponse. Gent makes a safe
ModelToolCallResponse with the original call ID and selected error text in Parts. The original error
stays in ToolCallRecord. Section 6 shows this division and its consistent save requirements.

## 1. Tool author: result and status

```go
type ToolStatus string

const (
    ToolExecuted         ToolStatus = "executed"
    ToolBlocked          ToolStatus = "blocked"
    ToolInvalidArguments ToolStatus = "invalid_arguments"
)

type ToolExecutionResponse struct {
    Status       ToolStatus
    Parts        []ContentPart
    Instructions string
}
```

| Status | Meaning |
| --- | --- |
| Executed | The tool executed and returned a response. Its data describes the domain outcome. |
| Blocked | A required condition prevents the action. Instructions explain how to proceed. |
| InvalidArguments | Supplied input was rejected. Safe guidance explains how to correct it. |

The shared ToolStatus enum includes InvalidArguments for both tool results and search results.
Gent produces this result before Tool.Call for failed input checks. A search engine can also return
it for query syntax that passes the JSON Schema. There is no Failed or Unknown status. Empty status
is invalid and uses safe error handling. Instructions alone can be a valid result body.
The name Execution refers to the requested path. It does not prove that Tool.Call ran. Gent can
produce Blocked or InvalidArguments before invocation; Status makes that distinction explicit.

A declined payment can be Executed with a domain status of Declined. An approval requirement can
be Blocked with a request reference in Instructions. Neither needs a special Go error type.

The user approved Parts only for Executed. Blocked and InvalidArguments have no model-facing Parts.
InvalidArguments uses the common correction message, complete input schema, and Instructions.
For Blocked, Gent sends the configured blocked sentence from SteeringMessages, then the tool's
Instructions. For example,
"The requested action was blocked." comes from Gent's configuration. The approval request reference
and next step come from the tool author. Do not send Gent wrapper fields or enum values as content.

For nil error, Gent sends every nonempty Instructions value with the result. For non-nil error,
Gent excludes the returned result from model input, saves both returned values for diagnostics, and
sends the tool's UnrecoverableErrorInstructions or the default. The tool error alone does not end
the run. Default text must not claim absence of side effects or that retry is safe.
Checkpoint failures, cancellation, and run limits retain their separate contracts.

The tool author has two ordinary return paths:

```go
// A handled outcome includes its model guidance and returns nil error.
return &ToolExecutionResponse{
    Status:       ToolExecuted,
    Parts:        parts,
    Instructions: "Check the order status before shipment.",
}, nil
```

```go
// An unexpected operation failure can return no result.
if err != nil {
    return nil, err
}
```

The second path has no ToolExecutionResponse.Status. Gent records the diagnostic error and prepares
safe model content from UnrecoverableErrorInstructions or the default. The author need not construct
a result only to accompany the error. Returning both a non-nil result and a non-nil error violates
the Tool.Call contract. It is not a supported partial-result path. Document this rule on Tool.Call.
Gent excludes the entire result and its instructions from model input if this occurs. It saves both
returned values for troubleshooting and uses safe guidance. This fallback does not make the return
pair valid. Restore must preserve the distinction between diagnostics and an accepted result.

Section 6 keeps the explicit call-error outcome in ToolCallRecord and safe text in the prepared
reply. This approved design does not create a ToolExecutionResponse or broaden Executed.

Nil result with nil error and an invalid result tag use the same failure path as a non-nil error.
Gent records the contract defect and retains rejected values for diagnostics. An inconsistent
Recover report uses the separate recovery-error path. This handling is now approved.

This draft does not add Code or Message fields. Domain data belongs in Parts. Guidance belongs in
Instructions. Status describes the call; it does not authorize a retry or enforce approval.

## 2. Content author: parts and a builder

Use the same parts in tool results, recovery evidence, ordinary messages, and the initial Task.
ContentPart remains a Gent-defined interface. Constructors show which content kinds are supported.
Each constructor and builder method must have full public documentation in the implementation.
Cover capture, ownership, MIME behavior, provider limits, failures, and recovery where they apply.

```go
// ContentPart is implemented by Gent's supported content and native call types.
// Tool and recovery Parts accept content only, not nested calls or responses.
type ContentPart interface{ isContentPart() }

func TextPart(text string) ContentPart
func DataPart(value any) (ContentPart, error)
func ImageURLPart(url string) ContentPart
func BytesPart(mimeType string, data []byte) ContentPart

func ImagePart(data []byte) (ContentPart, error)
func PNGPart(data []byte) ContentPart
func JPEGPart(data []byte) ContentPart
func GIFPart(data []byte) ContentPart
func WebPPart(data []byte) ContentPart
func PDFPart(data []byte) ContentPart
func AudioPart(mimeType string, data []byte) ContentPart
func VideoPart(mimeType string, data []byte) ContentPart

func NewParts() *PartsBuilder

type PartsBuilder struct { /* private state */ }

func (b *PartsBuilder) Text(value string) *PartsBuilder
func (b *PartsBuilder) Data(value any) *PartsBuilder
func (b *PartsBuilder) ImageURL(url string) *PartsBuilder
func (b *PartsBuilder) Bytes(mimeType string, data []byte) *PartsBuilder
func (b *PartsBuilder) Image(data []byte) *PartsBuilder
func (b *PartsBuilder) PNG(data []byte) *PartsBuilder
func (b *PartsBuilder) JPEG(data []byte) *PartsBuilder
func (b *PartsBuilder) GIF(data []byte) *PartsBuilder
func (b *PartsBuilder) WebP(data []byte) *PartsBuilder
func (b *PartsBuilder) PDF(data []byte) *PartsBuilder
func (b *PartsBuilder) Audio(mimeType string, data []byte) *PartsBuilder
func (b *PartsBuilder) Video(mimeType string, data []byte) *PartsBuilder
func (b *PartsBuilder) Build() ([]ContentPart, error)
```

Data captures owned, portable values when it is added. Build returns any capture error. The caller
does not call json.Marshal or register the original output type for replay. Unsupported Go values,
such as channels and live resources, cause a build error. An any parameter cannot make all values
portable.

Portable capture serves storage and recovery. It does not require a second clone of the original
Go object. Document each helper's ownership contract. For retained byte buffers, use documented
ownership transfer or sharing where possible; do not copy only to guard against caller misuse.

The user approved collecting construction errors and returning one error from Build, with no
partial parts on failure. Do not panic or silently remove invalid data. Provider media limits are
handled later through SteeringMessages, not as builder failures. The Data/PDF/Build authoring
pattern and the remaining helper names and signatures above are approved.

DataPart preserves field and number meaning in Gent's saved form. It does not promise to restore
the original named Go type or its methods. Gent chooses the model representation at the model
boundary. TextPart lets a caller choose the exact text instead.

Image detects the image MIME type. Named byte helpers supply a MIME type; they do not convert the
file or promise model support. Builders retain order and follow the main ownership requirement.
Tool-result parts cannot contain nested tool calls or change message roles. Gent validates this
restriction.

The builder collects conversion errors so the usual tool body needs only one check:

```go
func (s *Service) FetchOrder(
    ctx context.Context, input FetchOrderInput,
) (*gent.ToolExecutionResponse, error) {
    order, pdf, err := s.fetchOrder(ctx, input.ID)
    if err != nil {
        return nil, err
    }

    parts, err := gent.NewParts().Data(order).PDF(pdf).Build()
    if err != nil {
        return nil, err
    }
    return &gent.ToolExecutionResponse{
        Status:       gent.ToolExecuted,
        Parts:        parts,
        Instructions: "Check the order status before you request shipment.",
    }, nil
}
```

The example assumes fetchOrder returns a Go error only for unexpected operation failure. Expected
states, such as a missing order, must return an explicit domain result or a ToolExecutionResponse
status.

## 3. Tool author: the interface and function builder

The user approved the ToolBuilder interface below. Document Tool, each method, and each public
result type in the implementation. Explain when a method runs, what it returns, and what Gent does.
The brief signatures below do not replace the required Go documentation.

```go
type Tool[I any] interface {
    IndexableTool

    // Policy supplies tool-use guidance. It is not an approval callback.
    Policy() string

    // Strict requests the provider's strict argument mode where supported.
    // Gent still validates arguments locally before Call.
    Strict() bool

    // ParameterSchema describes the input accepted by Call.
    ParameterSchema() map[string]any

    // OutputSchema supplies optional domain-data guidance for the model.
    // Gent does not validate returned parts against this schema.
    OutputSchema() map[string]any

    // UnrecoverableErrorInstructions overrides safe guidance for a Call error.
    // Empty text selects the execution setting, then the Gent default.
    UnrecoverableErrorInstructions() string

    // Call receives validated input. Expected outcomes return a result and nil error.
    // Unexpected operation failures return nil and an error. The error stays private.
    // Returning both values is invalid; Gent saves both but uses only safe guidance.
    Call(context.Context, I) (*ToolExecutionResponse, error)

    // Recover checks an unfinished call with its saved effective input.
    // It must be safe to repeat and must not repeat the original action.
    // Its report goes to the model; it does not authorize automatic execution.
    Recover(context.Context, *ToolRecoveryRequest[I]) (*ToolRecoveryResponse, error)
}

type IndexableTool interface {
    Name() string
    Description() string
    Domain() string
    Categories() []string
    Keywords() []string
    SyntheticQueries() []string
}

func DefineTool[I any](
    name string, call func(context.Context, I) (*ToolExecutionResponse, error),
) *ToolBuilder[I]

type ToolBuilder[I any] struct { /* private state */ }

func (b *ToolBuilder[I]) Description(value string) *ToolBuilder[I]
func (b *ToolBuilder[I]) Parameters(fields ...*schema.FieldBuilder) *ToolBuilder[I]
func (b *ToolBuilder[I]) ParameterSchema(value map[string]any) *ToolBuilder[I]
func (b *ToolBuilder[I]) OutputSchema(value map[string]any) *ToolBuilder[I]
func (b *ToolBuilder[I]) OutputSchemaFrom(example any) *ToolBuilder[I]
func (b *ToolBuilder[I]) Strict(value bool) *ToolBuilder[I]
func (b *ToolBuilder[I]) Policy(value string) *ToolBuilder[I]
func (b *ToolBuilder[I]) Domain(value string) *ToolBuilder[I]
func (b *ToolBuilder[I]) Categories(values ...string) *ToolBuilder[I]
func (b *ToolBuilder[I]) Keywords(values ...string) *ToolBuilder[I]
func (b *ToolBuilder[I]) SyntheticQueries(values ...string) *ToolBuilder[I]
func (b *ToolBuilder[I]) Recovery(handler ToolRecoveryFunc[I]) *ToolBuilder[I]
func (b *ToolBuilder[I]) UnrecoverableErrorInstructions(value string) *ToolBuilder[I]
func (b *ToolBuilder[I]) Build() (Tool[I], error)
func (b *ToolBuilder[I]) MustBuild() Tool[I]
```

Input schema inference stays. There is no TextOutput type parameter, DeduplicateSummary method, or
generic output value stored beside Parts. Empty unrecoverable-error instructions select the default.
If the builder has no recovery handler, an unfinished call uses the approved missing-handler
recovery-error path. Gent must not infer NotStarted or repeat Call.

```go
tool := gent.DefineTool("fetch_order", service.FetchOrder).
    Description("Get an order and its PDF receipt.").
    OutputSchemaFrom(Order{}).
    Recovery(service.RecoverOrder).
    UnrecoverableErrorInstructions("The order lookup failed. Decide how to continue.").
    MustBuild()
```

The approved design uses one builder and one optional output schema. There is no DefineDataTool.
The caller selects direct schema input or inference from a Go type:

```go
// OutputSchema accepts a schema map, including one made with Gent's schema builder.
func (b *ToolBuilder[I]) OutputSchema(value map[string]any) *ToolBuilder[I]

// OutputSchemaFrom infers a schema from the Go type. It does not inspect example field values.
func (b *ToolBuilder[I]) OutputSchemaFrom(example any) *ToolBuilder[I]
```

Suggested setter rules: each replaces the same schema setting; the last call wins. Nil in the direct
setter omits it. Untyped nil in OutputSchemaFrom is an error because it has no type. A typed nil
pointer still supplies a type, including its nullability. Build reports inference and schema errors.
Inference must match DataPart's model representation, including field names and optional values.
Custom encodings and types that cannot be described safely need an explicit schema.

Keep the schema structured until the adapter prepares the request. This locked delivery rule needs
a field on the existing common function definition. The field name below follows Tool.OutputSchema:

```go
type ToolDefinition struct {
    Type     string
    Function *FunctionDefinition
}

type FunctionDefinition struct {
    Name         string
    Description  string
    Parameters   any
    OutputSchema map[string]any
    Strict       bool
}
```

Use the native output-schema field when the endpoint, schema features, and result contract support
it. Otherwise put the complete schema in description text. Do not append it to the description in
ToolChain before the adapter can make that choice. The schema is optional and does not add an output
generic parameter or change the parts API. It gives the model information about available functions.

Do not validate runtime tool result data against OutputSchema. A mismatch must not cause Gent to
reject, repair, or retry the result. Build can check the schema definition and report
inference errors. Part encoding and provider compatibility retain their separate checks.

The author supplies a schema and returns data that agrees with it. Gent need not infer which of
several DataParts matches that schema or check the author's promise. This scope describes domain
data, not the Go result wrapper.

Instructions is authored model guidance and must be delivered under the locked rule. Gent wrapper
metadata stays out of model content. Blocked uses its configured sentence and Instructions, without
Parts. Recovery facts use plain steering text under the approved rendering rules.

Exact provider rendering is implementation work. Gent must preserve authored payload and guidance.
It must not claim a native shape that its own encoding cannot supply. Use description fallback;
do not introduce a schema covering every framework state merely because those states exist in Go.

See the dated provider evidence and source links in [req-tool.md](req-tool.md).

## 4. Tool author: recovery

```go
type ToolRecoveryStatus string

const (
    ToolRecoveryCompleted  ToolRecoveryStatus = "completed"
    ToolRecoveryNotStarted ToolRecoveryStatus = "not_started"
    ToolRecoveryUnresolved ToolRecoveryStatus = "unresolved"
)

// ToolRecoveryRequest provides the saved effective input for an interrupted call.
// Its getters provide read-only values. Recover must not change the saved evidence.
type ToolRecoveryRequest[I any] struct { /* private state */ }

// NewToolRecoveryRequest transfers ownership of args and input to the request.
// Gent calls this after restoring input; tool authors receive the completed request.
func NewToolRecoveryRequest[I any](
    identity ToolIdentity,
    name string,
    args map[string]any,
    input I,
) *ToolRecoveryRequest[I]

func (r *ToolRecoveryRequest[I]) Identity() ToolIdentity
func (r *ToolRecoveryRequest[I]) Name() string
func (r *ToolRecoveryRequest[I]) Args() map[string]any
func (r *ToolRecoveryRequest[I]) Input() I

type ToolRecoveryResponse struct {
    Status       ToolRecoveryStatus
    Execution    *ToolExecutionResponse
    Parts        []ContentPart
    Instructions string
}

type ToolRecoveryFunc[I any] func(
    context.Context, *ToolRecoveryRequest[I],
) (*ToolRecoveryResponse, error)

type ToolIdentity struct {
    RunId      string
    ContextId  string
    ToolCallId string
    AttemptId  string
}

func ToolIdentityFromContext(ctx context.Context) (ToolIdentity, bool)
```

Gent restores the original typed input from the registered Tool[I]. This is separate from preserving
an output's original Go type. Tool authors do not decode EncodedValue or create provider call IDs.
Save the effective input after input hooks. Restore it for Recover without running those hooks
again. Keep the original parsed model arguments separate. These responsibilities are approved.

| Recovery status | Evidence and next step |
| --- | --- |
| Completed | Execution is required. Gent returns the recovered response and report. |
| NotStarted | Execution is absent. Gent reports no execution; the model decides. |
| Unresolved | Execution is absent. Gent reports uncertainty and steering; the model decides. |

Parts carry recovery evidence, such as an external operation reference. They do not replace
Execution. Completed means the original result is available. It does not establish business success
or current validity. Gent keeps Execution.Instructions and recovery Instructions as separate
evidence and sends both. A completed recovery can return an original Blocked or Executed result. The
recovered response, optional recovery Parts, and Instructions are approved. The latest naming
decision calls the nested response Execution. Its presence is required only for Completed. Exact
safe sentence wording and rendering order can follow the approved constraints.

Gent's plain recovery sentences belong in the per-execution SteeringMessages configuration.
Completed uses its recovered result's rendering, followed by the recovery note,
evidence, and Instructions. NotStarted and Unresolved use their configured sentence, followed by
evidence and Instructions. Field names are approved; default wording follows the stated constraints.

The three recovery states and model decision are locked. RetryImmediately was rejected. No state
causes Gent to execute a pending call, resume the old batch, or restart an interrupted script.

A recovery handler must be safe to repeat. Do not report an active external job as NotStarted.
It can be Unresolved with an operation reference and instructions to check that job. There is no
separate InProgress state in the selected three-state set.

If Recover returns a Go error, retain the error and any returned report for diagnostics. Exclude the
report from model input and use the dedicated recovery-error path and steering message. This is
approved. Do not fabricate an accepted recovery report or claim the original action failed because
its recovery check failed. Section 6 supplies the approved RecoveryError kind and safe prepared
parts; there is no separate ErrorInstructions field on the model reply.

An accepted saved result, error response, or recovery observation is reused. Do not invoke Recover
for a saved Tool.Call error. If the old worker's error was never saved, the new worker has no such
evidence and must recover the unfinished call. Recovery does not execute or retry that call.

## 5. Application author: initial messages and steering settings

Task and ModelMessage keep their common content interface:

```go
type Task struct {
    Messages []*ModelMessage
}

func NewTask(parts ...ContentPart) *Task

type ModelMessage struct {
    Role         ChatMessageType
    Parts        []ContentPart
    Continuation *ProviderContinuation
}

type SteeringMessages struct {
    ToolBlocked        string
    ToolUnavailable    string
    InvalidArguments   string
    ToolError          string
    RecoveryError      string
    RecoveryCompleted  string
    RecoveryNotStarted string
    RecoveryUnresolved string
    ImageURLLoadFailed  string
    UnsupportedMedia   string
    PTCBytesOmitted    string
}

func WithSteeringMessages(value SteeringMessages) ContextOption
```

```go
task := gent.NewTask(
    gent.TextPart("Inspect the damage in this image."),
    gent.ImageURLPart(imageURL),
)
execCtx := gent.NewExecutionContext(
    ctx, "inspect_damage", gent.NewBasicLoopData(task),
    gent.WithSteeringMessages(gent.SteeringMessages{
        ImageURLLoadFailed: "Ask for a new image link before you continue.",
        UnsupportedMedia:  "Explain the media limit and ask for a supported format.",
    }),
)
```

Empty fields select defaults. Gent includes the affected URL or media type as safe context and uses
the configured text as guidance. FULL STOP has no special execution behavior. These settings are
saved for resume. A tool's UnrecoverableErrorInstructions remains separate from media fallbacks.

SteeringMessages, its per-execution scope, and the fields above are approved. They cover blocked
actions, tool errors, recovery states, media problems, and PTC byte omission. Some are not errors.
Call-error precedence is approved: per-tool UnrecoverableErrorInstructions, execution ToolError,
then the built-in default. RecoveryError is the separate recovery-failure message. Use the execution
setting, then the built-in recovery default. Do not add a separate per-tool recovery-error method or
reuse a Tool.Call-specific message for failure of its recovery check.

PTCBytesOmitted is the approved field name for the PTC omission notice. The notice says
that byte content is not included automatically and requires tool.forward within the same script.
It must not promise access to a previous script's values. This is distinct from UnsupportedMedia:
forwarded content still uses the adapter's normal capability checks and fallback guidance.

Unavailable-tool guidance is also approved: provide a default and a per-execution SteeringMessages
setting for a new call whose target is not available. Recovery-error guidance also covers a missing
handler or a saved input that Gent cannot restore before invoking Recover. Neither case authorizes
automatic execution. A recovery setup failure must not produce NotStarted.

For direct adapter calls, add the same SteeringMessages field to existing CallOptions. Managed
calls use the saved execution setting; direct calls use the request setting. This approved rule
avoids two competing settings on a managed call. All paths use the same content preparation.

The approved CallOptions changes are these fields; the other model controls stay as they are:

```go
Tools            []*ToolDefinition
SteeringMessages SteeringMessages
```

The matching option helper uses the pointer-based definition slice:

```go
func WithTools(tools []*ToolDefinition) CallOption
```

SteeringMessages values are plain authored text. Gent adds safe context, such as
the affected tool name, URL, or expected input schema. There is no raw-error template variable.
An override replaces the default sentence, not the required correction details or authored
Instructions. This change does not add a separate per-tool recovery-error override.

ImageURL remains a remote reference. Document mutable content, expiry, and retry beside the helper.
The wrapper uses the URL if supported; otherwise it loads bytes. A recognized load failure becomes
safe text and the URL. Unsupported byte content becomes safe text. Unrelated provider errors keep
their own model-call error path. Gent does not repeat the source tool to repair a content failure.

## 6. Loop author: one common tool-response message

The approved model reply contains call identity, prepared parts, and Kind, ExecutionStatus, and
RecoveryStatus metadata. ToolCallRecord retains accepted responses and private diagnostics. Keep
the three enum types. The following declarations supply construction and read access. Status
getters return a value and a presence flag; callers do not inspect a sentinel enum value.

```go
// ToolResponseKind identifies the accepted outcome path in records and model reply metadata.
type ToolResponseKind string

const (
    ToolResponseExecution     ToolResponseKind = "execution"
    ToolResponseRecovery      ToolResponseKind = "recovery"
    ToolResponseCallError     ToolResponseKind = "call_error"
    ToolResponseRecoveryError ToolResponseKind = "recovery_error"
)

// ModelToolCallResponse associates prepared content with the original model tool call.
// Gent derives the metadata. Adapters use it and encode the parts for their provider.
// Parts contain no raw Go error, rejected return value, or nested tool call or response.
type ModelToolCallResponse struct { /* private state */ }

func (r *ModelToolCallResponse) ToolCallID() string
func (r *ModelToolCallResponse) Name() string
func (r *ModelToolCallResponse) Kind() ToolResponseKind

// ExecutionStatus is present for execution and for Completed recovery.
// If absent, it returns the zero value and false. The zero value is not a valid status.
func (r *ModelToolCallResponse) ExecutionStatus() (status ToolStatus, present bool)

// RecoveryStatus is present only for an accepted recovery report.
// If absent, it returns the zero value and false. The zero value is not a valid status.
func (r *ModelToolCallResponse) RecoveryStatus() (status ToolRecoveryStatus, present bool)

// Parts returns ordered, read-only content with safe guidance already included once.
func (r *ModelToolCallResponse) Parts() []ContentPart

// WithParts creates a request-specific reply with the same identity and metadata.
// On success it owns parts. It rejects nested calls and responses without changing r.
// This supports permitted request-hook edits while preserving saved history.
func (r *ModelToolCallResponse) WithParts(parts ...ContentPart) (*ModelToolCallResponse, error)

func (*ModelToolCallResponse) isContentPart()

// ToolCallRecord is an execution record. Only Response can enter model input.
// Diagnostic snapshots remain readable without the original tool or Go types.
type ToolCallRecord struct { /* private state */ }

// ToolCallID and AttemptID identify Gent's execution, not the provider's call.
func (r *ToolCallRecord) ToolCallID() string
func (r *ToolCallRecord) AttemptID() string

// Name identifies the selected target, which can differ from Response().Name().
func (r *ToolCallRecord) Name() string
func (r *ToolCallRecord) Kind() ToolResponseKind

// Execution is non-nil only for ToolResponseExecution. Its value is read-only.
func (r *ToolCallRecord) Execution() *ToolExecutionResponse

// Recovery is non-nil only for ToolResponseRecovery. Its value is read-only.
func (r *ToolCallRecord) Recovery() *ToolRecoveryResponse

// Response is always present on a built record, including error outcomes.
func (r *ToolCallRecord) Response() *ModelToolCallResponse
func (r *ToolCallRecord) DiscoveredTools() []string

// RawArguments retains supplied JSON, including malformed input.
func (r *ToolCallRecord) RawArguments() string

// Args is nil if parsing failed. Input is nil before typed conversion.
func (r *ToolCallRecord) Args() map[string]any
func (r *ToolCallRecord) Input() *EncodedValue

// Returned values include rejected returns. Nil means that no value was returned.
func (r *ToolCallRecord) ReturnedResult() *EncodedValue
func (r *ToolCallRecord) ReturnedRecovery() *EncodedValue

// Error returns private diagnostic evidence, or nil when none was recorded.
func (r *ToolCallRecord) Error() *EncodedValue

// ModelMessage creates a tool-role message that refers to the read-only prepared reply.
func (r *ToolCallRecord) ModelMessage() *ModelMessage
```

Gent and custom tool chains construct records through one builder. A tool author still returns
ToolExecutionResponse or ToolRecoveryResponse and does not use this builder.

```go
// ToolCallRecordBuilder joins the accepted outcome, diagnostics, and prepared reply.
// It does not invoke a tool, render provider payloads, save state, or update statistics.
type ToolCallRecordBuilder struct { /* private state */ }

// These IDs identify Gent's execution. ModelReply supplies the separate model call identity.
func NewToolCallRecordBuilder(
    toolCallID string, attemptID string, targetName string,
) *ToolCallRecordBuilder

func (b *ToolCallRecordBuilder) Arguments(
    raw string, parsed map[string]any,
) *ToolCallRecordBuilder
func (b *ToolCallRecordBuilder) Input(value *EncodedValue) *ToolCallRecordBuilder
func (b *ToolCallRecordBuilder) DiscoveredTools(names ...string) *ToolCallRecordBuilder

// Select exactly one outcome. Error paths have no accepted execution or recovery response.
func (b *ToolCallRecordBuilder) Execution(value *ToolExecutionResponse) *ToolCallRecordBuilder
func (b *ToolCallRecordBuilder) Recovery(value *ToolRecoveryResponse) *ToolCallRecordBuilder
func (b *ToolCallRecordBuilder) CallError() *ToolCallRecordBuilder
func (b *ToolCallRecordBuilder) RecoveryError() *ToolCallRecordBuilder

// ModelReply supplies safe, prepared content. Build derives its metadata from the outcome.
func (b *ToolCallRecordBuilder) ModelReply(
    toolCallID string, name string, parts ...ContentPart,
) *ToolCallRecordBuilder

// These optional snapshots preserve original returns and errors outside model content.
func (b *ToolCallRecordBuilder) ReturnedResult(value *EncodedValue) *ToolCallRecordBuilder
func (b *ToolCallRecordBuilder) ReturnedRecovery(value *EncodedValue) *ToolCallRecordBuilder
func (b *ToolCallRecordBuilder) Error(value *EncodedValue) *ToolCallRecordBuilder

// Build checks the contract and creates the record and model reply together.
// It returns no record on error and performs no local OutputSchema validation.
func (b *ToolCallRecordBuilder) Build() (*ToolCallRecord, error)
```

Builder methods take ownership of retained values when called. Do not change them through other
references. A builder is used for one record. Build checks the outcome selection, accepted response
status, recovery field pairing, supplied reply identity, and absence of nested call parts. A failed
Build is a construction defect, not a tool outcome. It returns no partial record. Private diagnostic
snapshots may contain rejected values; accepted response fields may not.

ModelReply accepts content that Gent's common preparation path has already made safe. It does not
append Instructions again, inspect domain success, or turn diagnostic errors into text. The builder
derives Kind, ExecutionStatus, and RecoveryStatus from the selected outcome. There are no separate
metadata setters or independent constructor for the historical ModelToolCallResponse. This keeps
the record and its derived metadata consistent without another public parameter struct.

WithParts is for a permitted request transformation. It keeps identity and metadata, validates the
replacement content kinds, and returns a new wrapper. The saved record retains its original reply.
The caller owns the replacement content until a successful call transfers it. Unchanged read-only
payloads can be shared. Copy only data that the hook needs to change independently. Record the
effective model request through the existing request-save path.

| Record kind | Execution() | Recovery() | Response().Parts() |
| --- | --- | --- | --- |
| Execution | Required | Absent | Prepared execution content and guidance. |
| Recovery | Absent | Required | Prepared recovery content and guidance. |
| CallError | Absent | Absent | Selected safe call-error text. |
| RecoveryError | Absent | Absent | Selected safe recovery-error text. |

Gent checks these record pairings before preparing and saving a reply. This is framework contract
checking, not validation against OutputSchema. Error outcomes have no ToolStatus. Neither error
kind claims that the original action had no effects, permits retry, or ends the run by itself.
Do not infer Kind solely from diagnostic Error: InvalidArguments can retain a private parser or
validation error while Execution remains the accepted response.

Gent derives the model reply's three metadata fields from that accepted record. The following
presence rules retain the meaning of the author responses:

| Kind | ExecutionStatus | RecoveryStatus |
| --- | --- | --- |
| Execution | Execution.Status. | Absent. |
| Recovery with Completed | Recovery.Execution.Status. | Completed. |
| Recovery with NotStarted or Unresolved | Absent. | Recovery.Status. |
| CallError or RecoveryError | Absent. | Absent. |

Completed recovery can contain an original Blocked or InvalidArguments response. An absent status
does not mean Unknown or prove that no action occurred. Metadata is not model content. Keep it
consistent with the accepted record when preparing and saving the reply; do not parse Parts or
diagnostic Error to reconstruct it. An absent getter returns ("", false); a present getter returns
the valid enum value and true. Empty status in an author response remains a contract defect.

ToolCallRecord.ToolCallID() is Gent's execution identity. For a native call, Response().ToolCallID()
is the exact provider call ID. ToolCallRecord.Name() identifies the target; Response().Name()
identifies the requested function. With shared execution these names can differ. Gent preserves
the mapping. Loop authors must not substitute the target name or a new ID in the provider reply.

Inner JavaScript calls have Gent-generated correlation IDs. They are saved separately and are not
sent as independent native replies to calls that the model never requested. The outer run_code
reply uses the original provider ID.

EncodedValue is Gent's existing portable snapshot type. Optional snapshot getters above
distinguish an absent return value from an encoded value. Saved diagnostic input does not require
decoding into its original Go type just to load completed history. Live hooks can still receive the
typed input and Go error. This changes the saved record, not the required diagnostic detail.

For a non-nil error, save Error and any returned result or report. Never place a rejected value in
the record's accepted Execution or Recovery. Invalid returns use the corresponding error kind and
retain the contract defect in Error. Save failures retain the run's separate persistence contract.

Tool.Call returns nil, err for this failure. After capturing the error, the common path uses the
selected safe text to build a record. For example, with identity, arguments, and input already set:

```go
record, err := builder.
    CallError().
    Error(savedError).
    ModelReply(originalCall.ID, originalCall.FunctionCall.Name, TextPart(selectedSafeInstructions)).
    Build()
if err != nil {
    return nil, err
}
return record, nil
```

There is no ToolExecutionResponse in this path. The selected safe text comes from the approved
precedence: per-tool UnrecoverableErrorInstructions, execution ToolError, then the built-in default.
It is saved so that replay does not need the tool registration or private error. RecoveryError uses
the execution's separate recovery-error message, then the built-in recovery default.

For a newly requested unavailable tool, use a CallError record with ToolUnavailable text in the
prepared reply. This placement is approved.
Here CallError means Gent could not supply a valid call result; it does not assert that Call ran.
The record kind alone does not determine invocation counters. A missing recovery binding instead
uses RecoveryError. Safe guidance and no automatic retry are locked.

The current cases include Executed, Blocked, InvalidArguments, recovery, call error, and
recovery-check error. Gent may produce Blocked for denied policy and InvalidArguments for input
rejection before Tool.Call. Invalid return values use the error path for the operation that returned
them. An in-progress call is a lifecycle state. Cancellation, run limits, and persistence failures
retain their approved run controls.

ModelMessage wraps Response in a common tool-role message. The delivery path is equivalent to:

```go
message := &ModelMessage{
    Role:  ChatMessageTypeTool,
    Parts: []ContentPart{callRecord.Response()},
}
```

The adapter maps this part to its native tool-result item and call ID. Any required media movement
to another provider message happens below this interface. Do not send the Go structure as JSON.
Raw diagnostics, rejected results, and framework field names stay out of model content.

| Reply | Model-facing content |
| --- | --- |
| Executed | Parts, then Instructions. |
| Blocked | Configured blocked sentence, then Instructions; no Parts. |
| InvalidArguments | Configured sentence, correction details, full schema, and Instructions. |
| Completed recovery | Recovered result, recovery note, evidence Parts, and recovery Instructions. |
| NotStarted recovery | Configured sentence, evidence Parts, and recovery Instructions. |
| Unresolved recovery | Configured sentence, evidence Parts, and recovery Instructions. |
| CallError or RecoveryError | Selected safe steering as an ordinary text part. |

For input rejection, store Gent's safe correction details and full schema in the accepted
Execution.Instructions together with any authored instructions. This storage choice is approved.
The original return stays in ReturnedResult. Gent supplies safe field paths and constraints without
copying raw error strings. This keeps the reply usable without the old tool registration
and needs no additional public validation-result type. Each item of guidance is included once.

The selected execution steering settings are saved with the run. One preparation path selects safe
parts and appends guidance before the model response is built. Compaction reads those safe parts;
it does not assemble instructions again or read private diagnostics. Provider-specific transport,
media delivery, and data encoding stay in the adapter. OutputSchema remains separate definition
metadata and keeps its native-field or description fallback rules.

The earlier three-field wrapper omitted information needed for Anthropic's optional is_error flag.
The later metadata approval removes that information limit. Adapters can use typed metadata to
select native request behavior where supported. Verify exact mappings during implementation. Do not
infer flags from guidance or domain-data success values. Preserve safe text. Recovery uncertainty
does not prove failure or authorize retry.

The accepted record and prepared reply have one construction and acceptance path:

1. Capture original returns for diagnostics.
2. Check the return contract and select the accepted outcome.
3. Build safe reply parts and derive status metadata from the accepted outcome once.
4. Preserve the outcome, metadata, and reply together as stable owned evidence.
5. Save both with execution evidence and statistics before dependent work starts.

This applies to progress saves and completion checkpoints. An accepted completed outcome must not
lack its prepared reply. After acceptance, the typed outcome governs program logic and the prepared
reply preserves historical model content. Reuse both after resume; do not independently rebuild
either from newer settings or tool registrations. Record later model-request transformations
separately. Save failures retain their existing run contract.

Gent's framework codecs must save and restore the private fields explicitly. Default serialization
of exported fields is not sufficient for these types. On restore, check the saved outcome and
metadata pairing, then reuse the saved prepared parts. Do not run live preparation, select new
steering text, or require the original tool registration to construct a completed record.

Do not clone ModelToolCallResponse or its parts merely for defensive safety. Keep framework-owned
state private, expose getters, and document ownership and permitted changes. Only features that
need independent values justify copies. Where a feature permits nested mutation, copy the data
that must stay independent; a shallow outer copy does not isolate a mutable byte buffer or map.
Share read-only payloads under their documented contract. Stable encoded evidence can avoid an
additional deep clone. Review ownership contracts before removing existing copies, and preserve
supported hook edits, history, recovery, and concurrency.

The loop retains the original assistant tool call and provider continuation before this response.
A tool-role message cannot repair missing call history. Gent preserves original record times and
recovery time in history; domain expiry remains tool data. Restoring a result does not make it new.

The side discussion approved this pointer-based Model interface:

```go
type Model interface {
    PrepareRequest(
        ctx *ExecutionContext,
        request *ModelCallRequest,
        chain ToolChain,
    ) (*ModelCallRequest, error)

    GenerateContentStream(context.Context, *ModelCallRequest) (Stream, error)
}

// ModelCallResponse is the complete provider response, not an accepted final answer.
// This rename preserves the existing fields and stream behavior.
type ModelCallResponse struct {
    Choices []*ContentChoice
    Info    *GenerationInfo
}

type Stream interface {
    Chunks() <-chan StreamChunk
    Response() (*ModelCallResponse, error)
    Close()
}
```

A custom loop can append callRecord.ModelMessage() to its selected messages and call the managed
model boundary. That boundary receives the tool chain and invokes PrepareRequest. Preparation
resolves definitions and search configuration; it does not call the model or execute domain tools.
Keep hooks, feature-required isolation, recording, and the save barrier there. Do not save bindings.
A custom loop still saves progress through the existing history and checkpoint API.

The ToolChain surface would be:

```go
type ToolChain interface {
    RegisterTool(any) ToolChain
    NativeTools(*ExecutionContext) ([]*ToolDefinition, error)
    PrepareCalls(*ExecutionContext, []*ModelToolCallRequest) (*ToolPlan, error)
    ExecuteNext(*ExecutionContext, *ToolPlan, TextFormat) (*ToolCallRecord, error)
    GetToolSchema(string) *schema.Schema
    Recover(context.Context, *ToolRecoveryRequest[EncodedValue]) (*ToolRecoveryResponse, error)
    RegisterCodecs(*CodecRegistry) error
    RecoveryConfig() any
}

type ParallelToolChain interface {
    ToolChain
    SupportsParallelCalls() bool
}
```

ToolCallRecord and removal of ToolChainResult are approved. Batch functions return []*ToolCallRecord
directly. Collectors, arguments, and saved metadata use the slice too. Preserve input order and
partial records. ExecuteNext still returns one record; this does not change batch execution rules.

For example, the existing jsruntime batch callback uses the slice directly:

```go
type ToolCallFn func(calls []*gent.ModelToolCallRequest) ([]*gent.ToolCallRecord, error)
```

The chain boundary uses saved input and binds it to I before the typed recovery handler runs.
RegisterCodecs still serves typed inputs and framework state. ExecuteNext keeps a tool error in the
diagnostic record beside a safe response; its own Go error signals a chain or run operation
that could not complete. TextFormat still formats framework text; it does not select provider roles.

Use pointers for public request, result, and definition structs by default, as approved in the side
discussion. Required pointers are not optional. Hook and save boundaries follow the main ownership
requirement; they do not require copies solely because they are boundaries. The record and metadata
responsibilities above are approved. Keep the separate enum types.

The JavaScript return value is a view of ModelToolCallResponse. This replaces the earlier separate
ToolExecutionResponse view and result/error variants. Reuse the common response fields, kind values,
status presence rules, and prepared parts. Do not add a JavaScript-specific status or error branch.

The following TypeScript notation describes runtime values; scripts still use ordinary JavaScript.
The tool object is supplied by Gent inside run_code. These are script methods, not additional Go
Tool methods or model-visible tools. ContentPart below is the JavaScript view of allowed content.

```typescript
type ContentPart =
    | { kind: "text"; text: string }
    | { kind: "data"; data: unknown }
    | { kind: "image_url"; url: string }
    | { kind: "bytes"; mimeType: string; data: Uint8Array };

type ModelToolCallResponse = {
    toolCallId: string;
    name: string;
    kind: "execution" | "recovery" | "call_error" | "recovery_error";
    executionStatus?: "executed" | "blocked" | "invalid_arguments";
    recoveryStatus?: "completed" | "not_started" | "unresolved";
    parts: ContentPart[];
};

type ToolRequest = {
    tool: string;
    args: Record<string, unknown>;
};

declare const tool: {
    call(request: ToolRequest): ModelToolCallResponse;
    parallelCall(requests: ToolRequest[]): ModelToolCallResponse[];
    // Select byte parts for this script's reply. No access to earlier script values.
    forward(parts: ContentPart[]): void;
};
```

The properties map to the common Go getters: toolCallId maps to ToolCallID(), name to Name(), kind
to Kind(), and parts to Parts(). Optional status properties map to the corresponding presence-aware
getters. Omit an absent status property; reading it returns undefined. Do not use an empty string,
null, or an additional Unknown state. Derive the view from the accepted common response once.

An execution reply has kind execution and its executionStatus. Call errors have kind call_error,
no executionStatus or recoveryStatus, and safe text in parts. There is no separate instructions
property: accepted instructions and safe guidance are already text parts. Private diagnostic errors
and rejected returns remain outside JavaScript. parallelCall returns these same response objects in
request order. Lifecycle failures retain their existing run controls.

Inner calls use their saved Gent correlation IDs. They do not acquire native provider calls merely
because their response has the common shape. The model receives the outer run_code reply under its
original provider ID. The shared recovery fields retain their existing meaning; this does not add
recovery calls to scripts or restart an interrupted script.
Automatic PTC output renders prepared parts and source call context. Keep the wrapper fields out
of model content; they remain available to scripts and native provider adapters.

The JavaScript view includes the inner response's accepted byte parts before the outer PTC delivery
filter runs. Otherwise a script could not select those parts with tool.forward. Only the outer model
reply replaces unselected bytes with omission guidance. The saved inner response stays intact.

Text remains a string even if it looks like JSON. Data exposes ordinary objects, arrays, strings,
booleans, null, and numeric values under the execution's PTC integer mode. Gent performs conversion;
the author does not parse JSON or restore the original named Go output type.

All byte formats use Uint8Array in a bytes part with their MIME type. PDF and image helpers need no
separate JavaScript variants. Returned views are read-only under the ownership contract; do not
copy solely for defensive safety. This does not add a saved-part reference API.

The per-execution PTC integer setting has this Go interface:

```go
// PTCIntegerMode selects how captured integer data appears in JavaScript.
// It does not change byte arrays, floating-point values, or saved source data.
type PTCIntegerMode uint8

const (
    PTCIntegerNumber PTCIntegerMode = iota
    PTCIntegerBigInt
)

func WithPTCIntegerMode(mode PTCIntegerMode) ContextOption
func (ctx *ExecutionContext) PTCIntegerMode() PTCIntegerMode
```

```go
execCtx := gent.NewExecutionContext(
    ctx, "inspect_order", gent.NewBasicLoopData(task),
    gent.WithPTCIntegerMode(gent.PTCIntegerBigInt),
)
```

Omitting the option selects PTCIntegerNumber. Number mode uses JavaScript number for captured
integers, including values outside the safe integer range. Such values can lose precision in the
JavaScript view. Do not silently switch large integers to bigint or strings in this mode.

BigInt mode converts all captured integer data to bigint, including small integers. Floating-point
values remain number in both modes. Strings remain strings. Byte elements remain Uint8Array values;
the setting does not rewrite JavaScript literals, array lengths, or other language operations.

Store the resolved mode with the execution configuration and restore it on resume. Do not keep a
mutable global mode on a shared tool-chain wrapper. Concurrent executions can use different modes.
Reject unsupported mode values before script execution.

Keep saved content and automatic model output based on the original captured parts. Number-mode
rounding in a JavaScript view must not rewrite that source. Values computed, logged, or passed to
another tool by the script follow the selected JavaScript representation.

Gent must support bigint in tool arguments, automatic output, and console.log formatting without
script-authored serialization. Preserve exact integers when the script passes them to another
tool; check the target Go type's range. Do not first convert bigint arguments through float64.
A script's own JSON.stringify keeps its native behavior. Default PTC guidance must state the active
mode, number precision limits, and the bigint arithmetic and JSON rules when that mode is selected.

PTC includes each accepted inner tool response in the outer run_code reply automatically. Preserve
the existing behavior when the result becomes ordered content parts. Scripts do not need to log
or forward returned text, data, or required guidance. The accepted status and error rendering rules
still apply; rejected returns and private errors never become automatic output.

Byte parts are the explicit exception. Include a steering notice for each omitted byte part, with
its MIME type and source call context. tool.forward selects byte parts for inclusion in the same
script's outer reply. A selected part is delivered as content instead of its omission notice. Gent
still preserves required instructions, safe errors, validation details, and full expected schemas.
ImageURL parts retain the approved URL delivery rules; provider conversion does not change the
kind of the original part. This PTC rule does not restrict direct native tool-result delivery.

For example, order data is included automatically. The script also selects its PDF for delivery:

```javascript
const reply = tool.call({tool: "fetch_order", args: {id: "O123"}});
if (reply.kind === "execution" && reply.executionStatus === "executed") {
    const pdfs = reply.parts.filter(
        part => part.kind === "bytes" && part.mimeType === "application/pdf"
    );
    tool.forward(pdfs);
}
```

The next ordinary model request carries the order data, selected PDF parts, and required guidance.
tool.forward does not send another model request. console.log is for values computed or customized
by the script; it is not required to expose a tool response.

Forwarding is limited to parts available in that script. Do not add saved-part handles, references,
or a retrieval API for later scripts. If the model later needs omitted bytes, it can request a new
run_code call that calls the source tool and forwards the returned byte parts. This is a new model
decision and a fresh execution, not recovery or reuse of old JavaScript variables. Gent must not
repeat the old script or tool call automatically. Application authors supply instructions for when
their agents need binary results and when another call is appropriate.

The default PTC instructions must teach automatic result inclusion, the byte exception, same-script
forwarding, and use of console.log only for computed or customized values. The omission notice must
state the same scope. Save original byte content under the existing state contract even when it is
omitted from model input. Reuse an accepted outer reply on resume; do not rerun its script to supply
bytes or change the saved forwarding choice.

Recovery reports do not return to a restarted script: Gent does not restart an interrupted program.
They go to the model with saved partial evidence. Scripts must not parse steering text for status.
The JavaScript control fields above are separate from rendered model-facing tool replies.

## 7. Search extension author: approved search contracts

The side discussion approved the following result and search-engine interface. It replaces the
current contract that tells authors to put model guidance in Go error text:

```go
type ToolSearchResult struct {
    Status       ToolStatus
    Names        []string
    Instructions string
}

type ToolSearcher interface {
    Id() string
    SearchGuidance() string
    IndexAll([]IndexableTool) error
    Search(context.Context, string) (*ToolSearchResult, error)
}
```

No matches is Executed with an empty Names slice. An invalid query returns InvalidArguments and
authored instructions. Blocked remains available for other conditions that prevent the search.
Unexpected Go errors stay private and use the search tool's safe message.
IndexAll is a setup operation and keeps its existing error contract. The shared ToolStatus type and
these result fields are approved. Do not add another search-status enum.

For example, an invalid regex returns InvalidArguments with safe instructions to correct the query.
Gent adds the search function's complete input schema through the common correction core. The
schema alone may only describe a string, so engine-authored guidance must explain the syntax issue.
Do not use a raw Go parser error as model guidance. A search backend outage can return an error and
use configured safe text. Names remain typed so SearchChain need not parse model text to select
tool definitions. This change affects search extension authors, not ordinary tool authors.

The model-facing chain interface is also approved:

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

Only SharedLocalSearch is in scope. Registry advertises direct native tools. SearchChain uses
shared search and execution functions, plus pinned tools. Search replies describe discovered
tools; they do not promote those tools into new target-specific native definitions. ProviderSearch
and ProviderLocalSearch are deferred. Keep one shared preparation implementation across adapters.

The user approved retaining query, query_type, and optional page, with existing pagination defaults.
ToolSearchRequest carries those inputs. One possible Go spelling is:

```go
type ToolSearchRequest struct {
    Query     string `json:"query"`
    QueryType string `json:"query_type"`
    Page      int    `json:"page,omitempty"`
}
```

Shared execution accepts one target per invocation through tool and args:

```json
{"tool": "fetch_order", "args": {"id": "O123"}}
```

The Go request type spells out that approved wire contract:

```go
type ToolExecuteRequest struct {
    Tool string         `json:"tool"`
    Args map[string]any `json:"args"`
}
```

Use the same target validation, typed input, hooks, result handling, and recovery path as a direct
call. Reuse the common result contract. Gent owns provider call-ID correlation and internal target
identity. Validate the target's parameter schema, not only the shared function's outer arguments.
Keep each target's OutputSchema with its definition; one target cannot describe every shared call.

Local catalog searches are repeatable reads. Unsaved searches can run again without a separate
recovery state machine. This permission does not permit automatic retry of domain tools. Keep
search diagnostics in BeforeModelCallEvent.ToolSearches and use existing progress saves.

The following registration methods are approved in the toolchain package:

```go
func (c *Registry) RegisterDeprecatedTool(tool any) *Registry
func (c *SearchChain) RegisterDeprecatedTool(tool any) *SearchChain
```

Deprecated tools stay available by name for execution and recovery. Exclude them from discovery,
advertised definitions, and pins. They use normal input validation and binding; a call need not
belong to an old run. Rebuild the runtime catalog from current bindings when execution continues.

## 8. Compaction author: no feature change

Keep these existing interfaces and the existing trigger and strategy implementations:

```go
type CompactionTrigger interface {
    ShouldCompact(*ExecutionContext) (bool, error)
    NotifyCompacted(*ExecutionContext) error
}

type CompactionStrategy interface {
    Compact(*ExecutionContext, CompactionInput) (HistoryUpdate, error)
}

type CompactionInput struct {
    Revision uint64
    Entries  []*HistoryEntry
    Required []EntryId
}
```

Deduplication removal does not require a new compaction algorithm. Both features affect context
size, so removal can change when token-based triggers fire. Prepared ModelToolCallResponse.Parts()
requires the existing summary renderer to read the new shape. Preserve its policy, required groups,
original history, and atomic checkpoint behavior. This is content integration, not a new strategy.

## 9. Storage and typed processing

Gent captures and restores supported part values, including bytes and MIME types. The application's
recorder remains responsible for durable storage. A new server uses its current tool catalog.
Exclude catalog definitions and discovery metadata from the continuation compatibility check.
Keep checks for incompatible saved run state and execution configuration. Local variables do not
become checkpoints automatically. These catalog rules are approved, not pending API proposals.

SaveBatch must retain diagnostic evidence separately from model content. If a tool returns both a
result and an error, save both with their call identity. Keep the rejected result's parts and
Instructions; do not treat them as an accepted response after restore. The default error codec
preserves the message, causes, and recognized codes. Exact private error fields or concrete error
types need an application codec when required. Ordinary error evidence needs no such codec.

The approved result path does not need the original output Go type. Any tool-specific processing
that needs that type happens before the tool returns its parts. Compaction and model preparation
operate on saved parts. There is no DeduplicateSummary callback after restore, or before it.

This does not make every Gent value untyped. Tool inputs, accepted Output[T], and custom application
state keep their type contracts. A custom hook that expects the original tool output type needs
separate typed state. This API does not supply it. Code that reads ToolCallResult.Output as Order
must change. A saved type name cannot recreate compiled methods on another server.

The side discussion extends this rule to completed inputs and diagnostics. Keep them as encoded
snapshots with data and type information. Reading a completed response must not reconstruct old Go
objects or require their old registrations. Apply this to events, plan results, and history
metadata. Typed recovery for an unfinished call still needs the input binding. Live hooks can
still receive typed input; exact reconstruction after restore must be explicit.

If a handler is unavailable or the saved effective input cannot be restored, use the dedicated
safe recovery-error response for that unfinished call. Keep its encoded evidence and diagnostic
error for SaveBatch. Do not let a missing call-specific binding reject the whole run before this
response can be produced. Do not fabricate NotStarted, rerun hooks, or invoke the original action.
The model decides what to do next. Core run-state restoration retains its separate error contract.

The default decoder may add or remove fields of the same registered struct. Added fields receive
Go zero values; removed fields are skipped without decoding their old values. Match saved Go field
names, do not infer renames, and reject changes to existing declared types. Apply the rules to
nested structs. Custom codecs keep their own contracts. Authors remain responsible for changes in
business meaning that a schema cannot detect. These rules are approved for later implementation.

## 10. Error counters and run limits

The user approved fresh-response resets, unchanged tool-error gauges for InvalidArguments,
separate recovery-error counters, and restoration without counting saved observations again.
Keep the existing increments for fresh tool operation errors.

| Outcome | Error count | Consecutive error gauge |
| --- | --- | --- |
| Non-nil tool error | Add one | Add one |
| InvalidArguments | Separate input-rejection counter; no tool-error increment | No change |
| Executed with nil error | No change | Reset |
| Blocked with nil error | No change | Reset |
| Recovery-check error | Separate recovery-error counter; no tool-error increment | No change |
| Restored result or recovery report | Do not count the old call again | No reset from replay |

InvalidArguments must not increment tool-error totals or reset or increment consecutive tool-error
gauges. Apply these rules to existing context-wide and per-tool gauges. Invalid arguments consume
applicable ordinary call and run limits. Gent does not need a non-nil Go error to record this state.
A separate consecutive input-rejection gauge is not selected by this approval.

The restore rule is approved: saved totals and gauges remain unchanged. Three saved errors restore
as three errors. Reusing their responses keeps three; one later fresh error makes four if limits
permit the call. Restore the latest accepted runtime, even when it is newer than the control
checkpoint. Keep limits effective. Do not reset gauges or re-publish old errors as new events.

Count a new recovery-check error once in separate recovery-error statistics; do not change the
tool-error gauge. Reusing its saved reply does not count it again. A Completed recovery carries an
earlier execution response; do not treat that embedded response as a fresh Executed or Blocked
event for gauge resets.

A saved Tool.Call error is a completed observation and does not invoke Tool.Recover. Send the safe
response to the model. If the old error was never accepted into saved state, recover the unfinished
call through the normal contract. This distinction concerns saved evidence, not domain success.

Executed can contain a declined payment or another domain failure. Gent must not inspect that data
to infer business success. Repeated blocked calls still consume ordinary run limits. This change
does not add a blocked-call counter. A configured error limit can end the run; one tool error alone
does not require termination.

## 11. Input rejection and shared correction messages

InvalidArguments is approved in the shared ToolStatus enum. Gent uses it for malformed argument
JSON, parameter-schema mismatch, and argument-dependent input conversion failure. Search engines
can also return it with nil error and safe Instructions for invalid query syntax. This replaces the
earlier Blocked example. Domain facts and search grammar remain the author's responsibility.

Gent identifies the selected tool, the invalid field or parse location, and the expected constraint.
It includes the complete current input schema, required correction details, authored Instructions,
and the configurable steering sentence. Field summaries and examples cannot replace that schema.
The InvalidArguments field name in SteeringMessages and the message requirements are approved.
Exact issue-record fields and template mechanics remain implementation details.

PTC and shared execution use one reusable core for input validation and correction messages. Use it
for direct and pinned calls too. Preserve PTC's source context and call examples around the common
message. Current FormatForLLM provides field details but not the full schema; extend that behavior
without retaining raw-error fallbacks. Shared execution uses the selected target's full schema,
not merely the shared function's outer schema. An invalid outer request uses the outer contract
when no target can be selected. An invalid search expression needs authored syntax guidance because
the schema may only describe a string.

Gent does not execute a target with rejected input or silently repair arguments. The model selects
the next request. If hooks change input, validate the effective input passed to Call. Keep schema
declaration easy through the approved builder and inference methods. Schema-valid input can still
name a missing order or lack current approval; tools retain those domain checks. OutputSchema does
not enable result validation.

## 12. Implementation readiness

The 2026-11-05 approvals settle the remaining public API choices. The requirements are ready for
the full implementation pass. This design approval does not itself start Go changes or tests.

| Area | Status |
| --- | --- |
| Unavailable tool | Approved: CallError with ToolUnavailable guidance; does not imply Call ran. |
| Input rejection | Approved: safe details and full schema in normalized Execution.Instructions. |
| Parts helpers | Approved: remaining media helper names and signatures in section 2. |
| Steering settings | Approved: field names in section 5, including PTCBytesOmitted. |
| Direct model calls | Approved: CallOptions.SteeringMessages and the stated setting precedence. |
| Recovery errors | Approved: execution-level override; no additional per-tool method. |
| JavaScript | Approved: common reply, Uint8Array, and per-execution integer mode. |

The recommendations are already concrete in the preceding sections. They do not require a new
recovery flow, reference system, or result type hierarchy. Exact provider encoding, native metadata
mapping, safe sentence wording, and private storage layout can be resolved during implementation
within the approved behavior. Verify each mapping and preserve all required content and guidance.

The counter behavior in section 10 is approved. Preserve saved counts and never recount a restored
response. New counter key names can follow the current stats naming conventions. Automatic PTC
results, explicit byte forwarding within the same script, omission guidance, and no saved-part
reference API are approved. JavaScript uses the common response shape and safe prepared parts.
Number mode is the default. BigInt mode is an explicit per-execution option for integer data.

The requested documentation completion supplies the constructors, record builder, getters, and
status-presence rules in sections 4 and 6. Those items no longer have placeholder field layouts.
The separate input-rejection tracking rule is approved; no extra consecutive-rejection gauge is
part of this change. AGENTS.md now matches the approved tool-error scope.

The main builder, parts construction error handling, search result fields, typed recovery, recovery
evidence, model preparation, diagnostic retention, and call-error message precedence are approved.
Prepared model reply parts, separate enums, derived status metadata, record responsibilities, and
consistent saves are approved. Copy only when a feature requires it; use private framework state,
getters, and documented ownership. Implement the shipment quick-start lesson and source checks in
the later refactor. Do not reopen settled decisions because an older section uses another shape.
