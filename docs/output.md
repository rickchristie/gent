# Typed output and caller prompts

An output contract has three jobs: describe optional prompt guidance, configure native generation,
and process a complete candidate. The model adapter identifies tool calls, continuation, incomplete
responses, and refusal. The output processor does not decide where an answer starts or ends.

```go
type Output[T any] interface {
    Instructions() string
    Configure(request *ModelCallRequest) error
    Process(ctx *ExecutionContext, content []ContentPart) (*OutputResult[T], error)
}

type OutputResult[T any] struct {
    Accepted bool
    Value    T
    Content  []ContentPart
    Steering []ContentPart
}
```

`Configure` sets request options without changing messages. A custom implementation can request
native schema or modality controls through `CallOptions`. Adapters use an exact supported native
mapping, or retain local schema validation. `Instructions()` is independent of those controls.
Gent never appends that text to the prompt.

`Accepted: true` supplies the typed value and its matching delivery content, with no steering.
A rejection has `Accepted: false` and useful steering; its value and content are unused. `error`
means an operation failed and stops execution. A nil result or empty correction is an error in the
processor. There is no separate `Format` method: `Process` supplies both representations together.

## Starting messages and attachments

The caller owns system instructions and user input. Both use `*ModelMessage`. Use this pattern when
an application has trusted instructions and a separate task:

```go
task := &gent.Task{Messages: []*gent.ModelMessage{
    gent.TextParts(gent.ChatMessageTypeSystem,
        "Read the supplied report. Return the requested record. " + out.Instructions()),
    {Role: gent.ChatMessageTypeHuman, Parts: []gent.ContentPart{
        gent.TextContent{Text: "Extract the invoice total."},
        gent.BinaryContent{MIMEType: "application/pdf", Data: pdfBytes},
    }},
}}
```

For one user message, `gent.NewTask(parts...)` keeps the supplied part order. A content interface
can contain `TextContent` or `*TextContent`, and likewise for binary and URL parts. `[]ContentPart`
does not need pointers to interface variables. Message and byte slices are copied at execution,
request-hook, recording, and delivery ownership boundaries. Do not mutate an input concurrently
with its initial submission. Later caller edits cannot rewrite a saved task or result.

The original task remains outside history compaction. Each request includes that task plus selected
working history. Put earlier conversation in working history when it should be compacted.
`BeforeModelCallSubscriber` can change an owned request for dynamic context. It cannot change the
saved task or remove required output controls. Compose dates and other application text in the
caller; the agent does not expand templates or add a default system prompt.

An adapter maps supported files to its native wire fields. It returns an error for unsupported
input. They do not extract text, render pages, or drop an attachment. See [models](models.md).

## Built-ins

| Implementation | Direct Go result | Contract |
| --- | --- | --- |
| `TextOutput` | `string` | Join ordered text parts; reject non-text parts. |
| `ImageOutput` | `[]gent.BinaryContent` | Return all validated images in order. |
| `JsonOutput[T]` | `T` | Infer a schema, validate JSON, and decode into `T`. |
| `SchemaOutput[T]` | `T` | Apply an explicit shared schema and decode into `T`. |

`react.NewAgent(model)` selects `TextOutput`. Use `react.NewWithOutput(model, out)` for the other
contracts. Go infers `T` for `executor.NewRunner`; `Execute`, `Resume`, and `executor.Run` return
`(T, *gent.ExecutionResult, error)`. Check the operation error and then the terminal reason.
Only `TerminationSuccess` supplies an accepted value. A native refusal returns `TerminationRefused`
and `ExecutionResult.Refusal` with a nil operation error.

### JSON

```go
type Order struct {
    ID     int64   `json:"id"`
    State  string  `json:"state"`
    Note   *string `json:"note,omitempty"`
}

out, err := output.NewJsonOutput[*Order](
    schema.Field("state").Enum("pending", "shipped"),
)
if err != nil {
    return err
}
agent := react.NewWithOutput(model, out)
```

A root pointer selects the Go return type and allocation; it does not permit a null answer.
`omitempty` and `omitzero` make output fields optional. Other fields are required. `json:"-"` is
ignored. A nullable value is separate from an absent field. Nested pointers, slices, and maps retain
their inferred null rules. Unknown fields are rejected. Numeric validation uses exact JSON numbers;
integer conversion does not pass through `float64`.

For an explicit schema, use the same `schema.Object`, `schema.String`, and other builders as tools:

```go
raw := schema.Object(map[string]*schema.Property{
    "state": schema.String("Current state").Enum("pending", "shipped"),
}, "state")
raw["additionalProperties"] = false
out, err := output.NewSchemaOutput[map[string]any](raw)
```

`Property.Nullable()` permits null for a property. `schema.Nullable(raw)` permits null for a whole
schema, including a root. The chosen Go type must represent it. Object, array, and scalar roots are
supported. For custom JSON methods, embedded fields, recursion, or `json:",string"`, supply an
explicit schema when inference rejects the type. Native grammar limits never cause Gent to turn an
optional property into a required nullable property or remove a constraint from the local schema.

Construction checks the Go type and the compiled schema together. Invalid map keys, unsupported
nested types, and incompatible declared field or element types return an error before any model
call. Explicit schemas use the JSON field names that Go encodes. References, unions, quoted fields,
embedded fields, and custom JSON or text methods are supported. A custom JSON method supplies its
own representation; Gent does not run that method during construction to guess its accepted values.
Setup checks do not prove that every schema constraint can be satisfied. Range errors, map key
syntax, and other candidate-specific failures still return steering during processing.

Validation first checks the JSON document, then checks lossless Go shape and conversion. The saved
delivery is encoded from `T` and validated again. This keeps custom JSON mappings from returning a
value whose display still shows the unprocessed candidate. JSON syntax and schema failures produce
steering. Failure to encode a processed Go value is an operation error.

### Custom validation

Built-ins always run local checks before the optional callback, including after native enforcement.
A local rejection skips the callback. Give the rules a stable name for checkpoint compatibility:

```go
out.SetCustomValidator("shipping-rules/v1", func(
    ctx *gent.ExecutionContext, order *Order,
) (*gent.OutputValidationResult, error) {
    if order.ID <= 0 {
        return &gent.OutputValidationResult{Steering: []gent.ContentPart{
            gent.TextContent{Text: "Use the positive order ID returned by the lookup tool."},
        }}, nil
    }
    return &gent.OutputValidationResult{Accepted: true}, nil
})
```

The callback type is `gent.OutputCustomValidator[T]`. It inspects the value; it must not mutate
referenced pointers, maps, slices, or image bytes. Mutation is detected as an operation error.
Use a custom `Output[T]` when processing must transform the value. Change the validator name when
its rules change. Configure output objects before sharing them across concurrent executions.

### Images

```go
out := output.NewImageOutput().
    SetMaxFileSizeBytes(8 << 20).
    SetMaxWidth(2048).
    SetMaxHeight(2048).
    SetAllowedMIMETypes("image/png", "image/jpeg")
agent := react.NewWithOutput(model, out)
```

The built-in validates complete PNG and JPEG data. It checks decoded type, declared MIME type,
file size, dimensions, and every image in a batch. Text or another unexpected part rejects the
candidate; no part is silently removed. The default decoder bound is 67,108,864 pixels per image.
`SetMaxDecodedPixels` changes that resource bound; zero disables it. This is separate from width
and height rules. The optional callback receives `[]gent.BinaryContent` only after all checks pass.

Native format controls are used where supported. Hard maximum byte and dimension limits still run
locally; a requested provider size is not the same as a hard maximum. Supported output URLs are
resolved before validation and saving. A failed download is an operation error. Saved results own
all bytes and do not need the URL again. The adapters bound individual decoded output images at
32 MiB and provider JSON bodies or streamed event data at 64 MiB.

## Correction, limits, and restore

A rejected candidate and its native continuation remain assistant evidence. Gent appends a user-role
steering message with framework provenance. The candidate and steering stay selected through the
next main model request, even if a custom compaction strategy attempts to remove them. Later
compaction can drop earlier attempts from working context while keeping their saved originals.

Retries use normal iteration, token, time, and error limits. `SCOutputRejectedTotal` counts one
rejection per processed candidate. Local and custom rejection counters are separate; the custom
prefix `SCOutputCustomRejectedBy` can set per-validator limits. `OutputValidationEvent.Stage`
distinguishes local checks, the custom callback, and the final processing decision.

An incomplete response remains evidence and receives shorter-answer guidance. Its partial native
calls are never executed. A native continuation uses the provider's continuation state. Refusal and
operation failure do not start output correction. Literal answer tags have no control meaning.

Acceptance saves the typed value, delivery parts, source IDs, and terminal checkpoint together.
Only then can subscribers receive `StreamChunk.Content`. Provider previews remain `MetaContent`.
A terminal restore decodes the saved value directly; it does not call the model, download media,
run `Process`, or run a validator. Output type, codec version, and declarative configuration must
match the saved contract. Register custom value codecs before execution and bind them on restore.

For a custom `Loop[T]`, expose `Output()`, call `Process` only after native completion checks, and
use `gent.AcceptOutput(ctx, result, source)` to build a successful loop result. The runner owns
terminal saving and publication. See [history and recovery](history.md) for storage contracts.
