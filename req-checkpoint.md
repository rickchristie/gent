# Pending checkpoint decisions

Checkpoint behavior is implemented. Its contracts are documented in [history and
storage](docs/history.md) and [live delivery](docs/native-tools.md). This file keeps only the
remaining decisions and the context needed to review them.

The discussion started on 2026-10-29. On 2026-10-30, the user postponed the interface field-name
review. The API still uses `Run`, `EventNumber`, `RunSchemaVersion`, and a string for
`RecordedEvent.Type`. The naming and typed-constant decisions below are not implemented. Open
proposals remain separate from agreed decisions.

## Deferred interface review

### Deferred sequence-name decision: use SequenceNumber

Use `SequenceNumber` instead of `EventNumber` or the longer `EventSequenceNumber`. The containing
type already identifies an event. Keep qualifiers where they identify a different position:

| Current field | Planned field |
| --- | --- |
| `RecordedEvent.EventNumber` | `RecordedEvent.SequenceNumber` |
| `SaveBatch.PreviousEventNumber` | `SaveBatch.PreviousSequenceNumber` |
| `RunView.LastAppliedEventNumber` | `RunView.LastAppliedSequenceNumber` |
| `Checkpoint.EventNumber` | `Checkpoint.SequenceNumber` |

Apply the same name to `BaseEvent`, `StreamChunk`, and trace events where they carry this same run
sequence. Use matching JSON field names, such as `sequenceNumber`, when implementation changes.

The name does not replace the contract: each run has one event sequence shared by all its contexts.
The first event is 1, each next event adds 1, and the saved sequence has no gaps. An exact save
retry keeps the same numbers. The saved position advances only after commit. An application that
subscribes to only some events can observe gaps in this shared sequence because of its filter.

`SaveBatch` contains a consecutive range, so it does not need a separate batch `SequenceNumber`.
For example, `PreviousSequenceNumber = 1` with events 2 through 5 produces a saved position of 5.
`BatchId` still identifies an exact save request for retries; it is not a sequence number.
The rename must preserve recovery from the saved position and discard unsaved preview events.

### Execution terminology and schema version: proposals remain open

The user prefers a clear noun to `Run` for this central concept. Recommend `Execution`, which
already appears in `ExecutionContext` and `ExecutionResult`. Define one execution as the complete
agent operation, including its iterations and child contexts. It keeps its identity across worker
replacement and recovery. A worker attempt is only one attempt to continue that execution.

| Term | Assessment |
| --- | --- |
| `Execution` | Recommended; describes one agent operation and fits existing Gent type names. |
| `Session` | Can suggest a conversation that contains several independent executions. |
| `Task` | Describes the requested work; Gent already uses `Task` for input data. |

If this term is selected, use `ExecutionId`, `ExecutionView`, and `ExecutionSchemaVersion` in place
of `RunId`, `RunView`, and `RunSchemaVersion`. The scope includes related public names and comments,
not just the version constant. These are proposals; the final term and rename scope remain open.

`RecorderSchemaVersion` was also proposed for the constant. The current `RunSchemaVersion`, with
value 6, identifies Gent's shared data format for `SaveBatch`, `RunView`, and `Checkpoint`. Batch
application and recovery check it even when an application does not attach a `Recorder`.
`trace.SchemaVersion` currently uses the same value.

This is a format compatibility version, not an event sequence or a run counter. A save does not
increment it. A change that makes the saved format incompatible requires a version change. It is
also separate from the application's database schema and the versions of custom payload codecs.
The current checks reject unsupported versions; the constant does not provide automatic migration.

Recommend `ExecutionSchemaVersion` if `Execution` is selected. The format applies to readers,
validation, and recovery as well as recording. `RecorderSchemaVersion` could suggest a particular
writer's format or database schema. This replaces the earlier recommendation to keep the word
`Run`; it does not approve a rename yet. Record the final decision before implementation.

### Deferred RecordedEvent.Type decision: use a named string type and typed constants

Use a named string type with exported typed constants for the supported record categories. This
makes the available values visible in source and editor completion while keeping readable JSON.
The proposed type name is `RecordedEventType`; exact constant names remain open. For example,
`type RecordedEventType string` and a typed constant for `"model_call_started"` express this design.

Gent already selects record categories in `recordedEventType` and the direct record producers.
Application event names do not require arbitrary values in `RecordedEvent.Type`:

- Framework events use fixed categories such as `model_call_started` and `tool_call_finished`.
- `CommonEvent` uses `Type = "common"`. It is Gent's generic event struct with a name, description,
  and data. `PublishCommonEvent` puts its name argument in `EventName`. Applications can call it,
  but Gent also calls it for child-context creation and completion. Thus `common` does not mean
  only application events, nor does it mean events used more frequently than other events.
- `Type = "custom"` is the fallback for an event struct that `recordedEventType` does not recognize.
  An application can define its own struct, embed `BaseEvent`, register its codec, and use
  `ExecutionContext.Publish`. The payload keeps that type. Gent copies `BaseEvent.EventName`
  when present. This is different from putting application data inside a `CommonEvent`.

For example, an application can report `myapp:cache_hit` with `PublishCommonEvent`, which records
`common`. If it defines and publishes a `CacheHitEvent` struct with the same name, Gent records
`custom`. The distinction comes from the Go event struct, not from the name or who calls Gent.
The current trace code displays the description and configured data for `CommonEvent`; it does
not provide a general display conversion for an unknown custom struct. Its saved payload remains
available through the codec registry.

These are explanations of the current categories, not a new requirement to retain both names.
Whether to combine or rename `common` and `custom` remains open. Preserve the required application
event behavior when reviewing that choice.

The current producer categories are:

| Group | Values |
| --- | --- |
| Execution | `execution_started`, `execution_finished`, `execution_content` |
| Iteration | `iteration_started`, `iteration_finished` |
| Model | `model_call_started`, `model_stream_chunk`, `model_call_finished` |
| Tool | `tool_call_started`, `tool_call_finished` |
| Validation | `parse_error`, `output_validation` |
| Failure and limits | `error`, `limit_exceeded` |
| Compaction and history | `compaction`, `state_change` |
| Generic and custom event structs | `common`, `custom` |
| Worker attempts | `attempt_started`, `attempt_interrupted` |
| Empty checkpoint save | `checkpoint` |

Current validation accepts other nonempty `Type` strings, except that a history change must use
`state_change`. Thus the current public API does not enforce a closed set, even though Gent's own
producers select known categories. A named Go string type alone will not enforce a closed set
either: callers and JSON decoding can still supply values outside the constants.

Open validation proposal: reject unknown categories in both `ApplyBatch` and `ValidateBundle`, and
use `common` or `custom` for application events. This would detect spelling errors and unsupported
categories. Decide this rule explicitly before implementation; adding typed constants alone must
not silently change acceptance of unknown values. `trace.EventType` already has typed constants,
but its display categories are not the complete set of recorded categories. Keep that distinction.

### What Type is used for

`RecordedEvent.Type` selects how Gent processes a saved record. It is not just display text:

| Consumer | Use of `Type` |
| --- | --- |
| Model recovery | Find saved model starts, complete responses, and chunks for replay. |
| Tool recovery | Find a saved tool result before considering another tool call. |
| History validation | Require `state_change` when a record carries a structured history change. |
| Stream delivery | Identify live model chunks and saved final execution content. |
| Trace processing | Update model, tool, context, iteration, and worker-attempt display state. |

Model and tool recovery first select records by category and call identity, then decode the
selected payloads. An application event named `gent:model_call:after` would still have category
`common` or `custom`; its name alone does not make it a saved model response.

Keep the three current meanings clear:

- `RecordedEvent.Type` is Gent's record category and selects processing rules.
- `RecordedEvent.EventName` is a name set by Gent or supplied by the application.
- `RecordedEvent.Payload.Type` is a codec identifier that selects how to decode the stored value.

The record category cannot always be derived from the payload's Go type: both `model_stream_chunk`
and `execution_content` can contain a `StreamChunk`. Some records have no encoded event payload.
Conversely, `EventName` is empty for some records that have a category.

Two separate category/name fields are not a fundamental recovery requirement. A future design
could use one identifier if it covers every record and defines how application names are handled.
For Gent's built-in events, `Type` and `EventName` often describe the same action in two forms.
Removing either field requires updating its consumers and preserving those distinctions; this
review has not approved that change. Typed constants remain the plan for the record category.

## Separate work: saved-history retention and total memory

Saved-history retention and a hard total-memory bound require a separate design. Early progress
saves release pending references, but `RunView.Activity` retains accepted events. Model responses
and subscription queues can also retain content. Pending-save thresholds and client queue limits
do not cap total execution memory.

No retention policy was selected in this discussion. Do not remove saved history or claim that
early saves solve total memory retention without a separate decision.

## Sources for the remaining review

- [Saved format version, event fields, save requests, and run views](run.go)
- [Checkpoint fields](checkpoint.go)
- [Base event fields](events.go)
- [Stream chunk fields](stream.go)
- [Framework event names](event_names.go)
- [Record category selection](context_recording.go)
- [Record validation](run_replay.go)
- [Trace event categories](trace/types.go)
- [Model recovery](model_replay.go)
- [Tool recovery](tool_plan.go)
