# Live streaming and checkpoint saves

Status: checkpoint behavior is implemented. Interface naming review remains postponed.
This file records decisions from the recovery discussion started on 2026-10-29. Open choices are
listed separately so that implementation does not treat a suggestion as an approved requirement.

## Discussion rules and API decisions

Record each decision and its reason in this file as the review continues. Keep open proposals
separate from agreed requirements. On 2026-10-30, the user requested immediate implementation of the
agreed checkpoint behavior and postponed the interface field-name review. This implementation keeps
`Run`, `EventNumber`, `RunSchemaVersion`, and the string `RecordedEvent.Type`. The naming and typed
constant proposals below remain recorded for a later review; they are not part of this change.
The current API reference below describes the implemented source.

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
Recovery discards unsaved preview events and continues after the saved position, as agreed below.

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

### EventName: say whether Gent or the application supplies it

Use the names `Gent` and `the application` directly in comments. Do not use "original publisher"
as an alias for either. A reader should not need to infer who supplies a field.

Gent sets `EventName` for its framework events, using the constants in `event_names.go`.
For `PublishCommonEvent`, Gent copies the first argument; the application selects that value when
it calls the method. An application-defined event struct supplies the name in its `BaseEvent`.
Thus Gent determines its own event names, but not all names accepted by the public API.

Planned comment for `RecordedEvent.EventName`:

```go
// EventName is the name set by Gent for a framework event, or the name supplied by the
// application for an application event. It is empty for records without an event name.
```

Keep the field a string while application names are supported. Examples from the current API are:

| Source | `Type` | `EventName` |
| --- | --- | --- |
| Before model call | `model_call_started` | `gent:model_call:before` |
| After tool call | `tool_call_finished` | `gent:tool_call:after` |
| History change | `state_change` | `gent:state_change` |
| Gent creates a child context | `common` | `gent:child:spawn` |
| Application cache event | `common` | `myapp:cache_hit` |
| Model stream chunk | `model_stream_chunk` | Empty |

For the application example, call
`ctx.PublishCommonEvent("myapp:cache_hit", "Cache lookup succeeded", cacheData)`.
The description and application data belong to the encoded `CommonEvent` payload. The name lets
consumers distinguish this event from another `common` event such as `myapp:cache_miss`.
`EventName` can be empty for records that have no event name. It does not set event order,
select a payload codec, or mark a recovery checkpoint.

## Purpose and context

Gent must deliver live model output without a database transaction for each stream chunk. Recovery
must still have a reliable saved state before execution starts its next dependent external action.
The required change separates live delivery from the blocking persistence boundary.

Before this change, `recordChunk` in `context_recording.go` called `FlushProgress` for each chunk.
Consumers received the chunk only after `Recorder.Save` succeeded. This added a transaction and its
delay for every chunk. The quickstart store also rewrites the full `RunView` on each save. The new
path freezes and publishes each chunk, retains it, and saves groups at a boundary or threshold.

A save does not represent one model turn or one agent iteration. It accepts the next ordered group
of events and can move the resume point within an iteration. A `SaveBatch` is an update to saved
state. The accumulated `RunView` contains the history, activity, runtime data, and latest checkpoint
needed for recovery. A checkpoint by itself is not the full saved run.

The final agreement retains individual stream chunks in saved history. It groups those chunks into
fewer writes. An earlier suggestion to omit chunks from storage was not the selected design.
Grouping writes reduces transaction count; it does not reduce the total event data to store.

## Agreed delivery and save sequence

The numbers below identify ordered event groups, called batches in the discussion. They do not
identify turns, and they do not require a complete recovery snapshot in each live batch.

| Execution point | Live delivery | Blocking save | Next permitted work |
| --- | --- | --- | --- |
| Before model call | `1` | `[1]` | Call the model after commit |
| Model output | `2`, then `3`, then `4` | None | Continue receiving output |
| Complete model response | `5` | `[2, 3, 4, 5]` | Continue after commit |
| Before tool call | `6` | `[6]` | Call the tool after commit |
| Tool result | Next result batch | Pending batches and result | Continue after commit |

Live delivery publishes one batch at a time in order. A save accepts all pending batches through a
defined boundary in one transaction. Chunks already delivered live remain available for that save.
The complete response is saved with its chunks, usage, tool requests, and continuation data. Saving
only concatenated display text would not preserve the complete provider response.

The table describes the required order, not a fixed number of saves. Existing initial, iteration,
compaction, and terminal boundaries must remain valid for recovery. Consecutive local state changes
can share a save when doing so preserves the required external-call boundaries. For example, the
complete response and prepared tool plan can be included in the same save.

## Live delivery contract

Live events describe observed progress. Delivery does not mean that the event is already stored.
Do not save each chunk separately. Save at a normal execution boundary or when the pending-save
threshold is reached. A slow or disconnected display consumer must not stop the agent, delay its
next action, or cause unlimited queue growth.

Send-and-forget means that execution does not wait for a consumer acknowledgment or a socket write.
It still requires ordered publication, owned data, bounded queues, and resource cleanup. Do not
start an unbounded number of goroutines to hide blocking consumer calls.

Live delivery and durable acceptance have separate meanings. Existing observers that promise
committed records must keep that promise. Mutable execution hooks are also separate: hooks that
prepare a model request or tool input must finish before that effective input is frozen and saved.
This change does not turn those hooks into asynchronous display callbacks.

A complete model response can appear as a preview before its save completes. It must not be
reported as a durable final execution result at that point. Preserve the terminal rule: publish
accepted execution success only after the final outcome has been saved.

## Save implementor contract

Put these requirements in the public `Recorder.Save` documentation and storage examples. A save is
a blocking persistence operation, not a notification that an application can handle later.

1. Store the full supplied group atomically. All events, history changes, runtime data, cursor
   changes, and the supplied checkpoint belong to the same transaction.
2. Return `nil` only after the transaction has committed. Returning after adding work to an
   asynchronous queue does not satisfy this contract.
3. Do not commit each included batch in a separate transaction. A failed request must not leave
   only part of its group accepted.
4. Validate the run identity, expected previous saved position, event order, and payload identity.
   Reject gaps, conflicting duplicates, and writes from a stale worker.
5. Check the application's worker lease or fencing token in the same transaction as the write.
   Local Go locks cannot protect a database from another worker process.
6. Accept an exact retry of an already committed request without applying its effects twice. Keep
   the same request identity and contents when retrying after a lost acknowledgment.
7. Treat a timeout or lost acknowledgment as an uncertain result. It does not prove that storage
   rejected the write. Reconcile the exact request with storage before resuming execution.
8. Keep client delivery failures separate from storage results. A failed socket write after a
   database commit must not turn that committed transaction into a reported storage rejection.
9. Preserve concrete values with the codec registry. Plain JSON decoding must not erase message
   part types, provider continuation items, tool inputs, or custom state.

Gent must wait for save success before crossing the corresponding execution boundary. In
particular, `tool_ready` must commit before the tool is called, and a tool result must commit before
the next dependent step. A failed save stops further execution on that path. Making `Save`
asynchronous would break this ordering and is outside the agreed design.

This ordering does not imply that all already-running concurrent operations can be stopped in
place. Every path must satisfy its own required save boundary before starting an external action.
The implementation must define one consistent ordering for events from root and child contexts.

## One checkpoint for the end of a save

Each save group has at most one new control checkpoint. When present, it describes the state after
the last event included in that group. A live chunk does not need its own runtime or checkpoint
snapshot. Do not multiply full snapshots by the number of chunks.

This is already the shape of `SaveBatch` in `run.go`: an event slice, one `RuntimeState`, and one
optional `Checkpoint`, with run identity, a batch identity, and the previous saved cursor. The
existing `Recorder.Save(context.Context, *SaveBatch) error` signature already accepts multiple
events in one request.

The discussion described saving a slice of live batches. Preserve that grouping behavior. Decide
whether the existing event slice is sufficient or explicit live batch boundaries need a separate
type during implementation. Do not add a slice of complete checkpoint snapshots merely to match
the word "batches". The final public type names and exact slice shape are not yet fixed.

The quickstart `Writer.Save` in `docs/quickstart/server/store.go` already provides the required
transaction structure:

- `db.Update` encloses the entire write.
- The writer checks the run and worker fence.
- `gent.ApplyBatch` validates and applies the candidate to the previous saved view.
- The codec registry encodes the resulting view, which is written in that same transaction.
- The method returns the transaction result.

Thus the sample already saves one final view and checkpoint for the whole request. The primary
change is when Gent calls this method and how live delivery proceeds before that call. `ApplyBatch`
does not itself supply database atomicity or worker ownership checks; the writer supplies those.

Keep the distinction between `FlushProgress` and `CommitCheckpoint`: progress can be stored without
claiming a new resumable control state. A partial model stream cannot become a completed-response
checkpoint just because some of its events were stored.

The optional `SaveBatch.Checkpoint` is the marker for a new recovery checkpoint. `nil` means that
the save keeps the previous checkpoint. A non-nil value contains the new state, and its
`EventNumber` identifies the final event covered by that state. A separate `IsCheckpoint` flag on
every event would duplicate this information. The current synthetic event with type `checkpoint`
is used when there are no pending events; that event type is not the general checkpoint marker.

Keep these positions separate:

- `RunView.LastAppliedEventNumber` is the latest event accepted by storage.
- `RunView.Checkpoint.EventNumber` is the latest saved control boundary.
- The live position can be ahead of both while events wait for a save.

Recovery loads the complete latest saved view and restores control from its checkpoint. Saved
events after that checkpoint still supply evidence. Reuse a complete saved model response or tool
result when available. Partial chunks alone cannot resume an incomplete provider call. New events
continue after the latest saved event number, not after the earlier checkpoint number.

## Ownership, ordering, and concurrent saves

Freeze each event before live publication so its data cannot change before persistence. Assign a
stable order to events within the active execution. Do not let consumers or recorder callbacks
change the framework's retained values through shared mutable data.

At save time, capture a fixed ordered prefix of pending batches and its matching runtime state and
checkpoint. Events produced after that boundary belong to a later save. Do not save an earlier
event prefix with a checkpoint that depends on later events.

Serialize acceptance and cursor updates. Advance the saved position only after the recorder
confirms success. Keep a failed or uncertain request intact for reconciliation. Release pending
references only after success, and do not remove events queued after the captured boundary.

Saving an event must not publish it to the live stream a second time. Committed observers can be
notified of acceptance through their separate contract. Tests must cover concurrent root and child
events, callbacks, cancellation, and worker replacement.

## Recovery and the frontend contract

The application owns the API that reconstructs saved display state for its frontend. Gent supplies
the saved run and recovery primitives; it does not need to define an application-specific frontend
recovery API.

The agreed recovery model discards the previous worker's live stream. The application retrieves the
latest full saved `RunView`, reconstructs the display, and restarts frontend consumption from that
saved state. The previous unsaved preview does not need to survive.

No new frontend attempt ID is required for this model. The earlier suggestion to add one addressed
a different case: a client that keeps its old live cursor and merges a replacement stream into the
old preview. For example, the client can see `1, 2, 3, 4` while only `1` is stored. A replacement
worker can then produce new events numbered `2, 3, 4`. The old cursor would incorrectly discard
them as duplicates, or the display could mix output from the two executions.

A full frontend reset avoids that case. Document the application's responsibilities:

1. Close or invalidate the previous stream when its worker stops or recovery starts.
2. Discard queued frames and pending snapshot responses from the old connection.
3. Replace the old preview and cursor with the reconstructed saved state. The saved cursor can be
   lower than the last cursor seen live; this is expected after loss of unsaved progress.
4. Subscribe and load state with a defined ordering so new events cannot fall between those steps.
   Buffer events from the current connection when necessary, then apply them to that snapshot.
5. Detect a missing event range and reload state through the application's API. Do not merge a
   replacement stream with an unsaved suffix from the previous worker.

The application's lifecycle must ensure that the old worker no longer supplies the active stream.
If an application permits overlapping workers or keeps old streams, it must solve that additional
coordination problem. The existing storage fence remains necessary for rejecting stale writes.

This decision concerns frontend identity only. Existing internal `AttemptId` values distinguish
tool attempts and support recovery decisions. Do not remove those fields or change stable logical
tool call IDs as part of this delivery change.

Track live progress and saved progress separately inside Gent. A live cursor can advance while the
saved cursor remains unchanged. `Export` and recovery input must describe accepted saved state.
Display code must not treat the highest observed live number as proof of persistence.

The old quickstart refresh path rejected a snapshot behind the current display cursor. The new
path compares against its saved cursor, retains the current unsaved suffix across refreshes, and
resets that suffix on reconnect. It rejects stale callbacks and reads from the old connection.
No mandatory frontend attempt ID was added.

## Accepted interruption trade-offs

A worker can stop after sending stream chunks and before saving them. Losing those chunks and the
corresponding partial preview is acceptable. Recovery retries an incomplete model call; it does not
resume the provider from an arbitrary token position.

A complete saved response must be reused on recovery without another provider call or another
charge to the recorded token counters for that saved response. Provider charges for work whose
usage was never received or stored cannot be reconstructed from missing chunks.

Completed saved tool results must also be reused. A stop after an external tool action but before
its result save remains uncertain. Keep `Tool.Recover` and its existing decision contract. Blocking
saves preserve execution order; they do not create an exactly-once guarantee for external effects.

## Configurable client delivery limits

Bounded client delivery is agreed. Make queue capacity and delivery time limits configurable. Cover
both event count and byte size because one event can be much larger than another. Define cleanup
for disconnect, unsubscribe, completion, and cancellation so repeated runs do not retain queues or
goroutines.

Apply socket delivery timeouts in the client delivery worker. The agent must not wait through a
timeout for every slow client. Report a delivery failure through the delivery path without treating
it as a failed checkpoint or silently losing the reason for disconnection.

Recommended default: close a slow subscription and let the application reload state. Arbitrary
loss of text deltas can leave an incorrect display. If a configurable drop policy is offered, it
must expose the gap so the client can reload. Exact defaults, configuration names, and supported
overflow policies remain implementation choices to document and test.

## Agreed early progress saves

There are two different queues. A client queue holds events that the client has not received. The
pending-save queue holds events that have not been accepted by storage. A fast client can drain its
queue while Gent still retains every chunk for the save after model completion.

A long response can make the pending-save group large. Client queue settings alone cannot control
that growth. The agreed policy is a separate configurable threshold for pending event count or
byte size. Reaching the threshold triggers one grouped progress save. It does not make every chunk
a separate save request. The implementation choices for limits, byte accounting, and an oversized
event are recorded below.

The early save blocks until commit and supplies no new control checkpoint. Release the saved
prefix from the pending list only after success. Events added after that prefix remain pending.
A failed or uncertain save uses the same stop-and-reconcile contract as any other save.

For example, with a threshold of three pending chunks:

1. Save batch `1` before the model call, with the recovery checkpoint at `1`.
2. Receive chunks `2`, `3`, and `4`. Publish each live and retain it for persistence.
3. At the threshold, call `Save([2, 3, 4])` with no new checkpoint. Wait for commit.
4. The saved position becomes `4`; the recovery checkpoint remains at `1`. Clear the saved prefix
   from the pending list. The current provider call is still incomplete.
5. Receive chunks `5` and `6`, then the complete response `7`.
6. Save `[5, 6, 7]` with the matching completed-response control state. Continue after commit.

While an early save blocks stream processing, the already-started provider request can still
produce data. Bound any upstream queue used during that wait. A blocking save does not imply that
Gent can pause an external provider or stop other already-running work in place.

If the process stops after step 4, recovery retains saved events through `4`, restores control at
`1`, and retries the incomplete model call. The earlier chunks are saved evidence, not a provider
continuation token. Normal responses that stay below the threshold still save their chunks at
completion.

This decision replaces the earlier open choice. Stopping the model at the threshold or writing
temporary files are not the selected policy.

The goal is to control pending data and save request size. The current `RunView` also keeps accepted
activity in memory. An earlier save clears pending references but retains those events in the full
run view. The model response and subscription queues can also retain content. An early save alone
therefore cannot promise a bound on total memory per run. A policy for retaining saved history in
memory remains separate work; do not silently remove saved history or claim that this save policy
solves total memory retention.

## Implemented policy and delivery choices

The implementation keeps `Save(context.Context, *SaveBatch) error`. Its `Events` slice already
represents a group; a second slice of `SaveBatch` objects would repeat runtime and checkpoint state.
Live delivery sends one owned `RecordedEvent` at a time. Event numbers are assigned before delivery,
under the shared execution lock, and never changed when that event is saved or retried.

```go
type CheckpointPolicy struct {
    MaxEvents   int
    MaxBytes    int
    SaveTimeout time.Duration
}

func WithCheckpointPolicy(CheckpointPolicy) ContextOption
```

- Pending-save defaults: 256 events, 1 MiB of encoded events, and a 30-second Save timeout.
  Either size threshold triggers a blocking progress save. The recorder must honor the Save
  context deadline; Gent waits for its result. Zero fields select defaults; negative
  fields stop execution with a configuration error. `RestoreBindings.CheckpointPolicy` supplies
  the replacement worker's policy; policy is not persisted as control state.
- Pending bytes use `CodecRegistry.Marshal(RecordedEvent)`, including the frozen payload and event
  envelope. Runtime and checkpoint size are separate. A single oversized event is saved whole.
  These are flush thresholds, not hard bounds: concurrent producers can add events during a save.
  Each producer waits at a reached threshold before producing its next event. Built-in provider
  queues are bounded while waiting; custom adapters must also bound their upstream queues.
- Subscriber defaults: 4,096 queued or in-flight events, 8 MiB of encoded data, and 30 seconds per
  delivery. Event queues use the codec encoding; stream and trace queues use JSON, including saved
  error text. These are data-size limits, not exact heap measurements. Zero selects defaults;
  negative values return an error. One oversized delivery closes that subscription.
- Overflow or timeout discards that subscriber's pending data and reports `ErrDeliveryOverflow`
  or `ErrDeliveryTimeout`. It does not stop execution or reject a checkpoint. Socket writers need
  their own write deadlines after receiving an event. No callback runs in the producer path.
- The terminal lifecycle event is frozen after its hooks, then numbered and queued inside the
  terminal save with the accepted answer. A concurrent progress save cannot publish success before
  the outcome commits. Subsequent live events and merged chunks keep their sequence order
  behind that save. Each subscriber owns its error data as well as its text metadata.
- Export and committed observers see saved data only. The live cursor can be ahead of Export.
  A grouped save does not deliver its model chunks a second time. Exact retry preserves event
  numbers, payloads, timestamps, the runtime snapshot, and the request digest.
- Ownership copies reuse frozen payload encoding and copy its bytes directly. They do not encode
  and decode that envelope again for each subscriber, save, or Export. History values still use
  the codec registry because their content and metadata can contain application types.
- The quickstart uses `trace.Sequencer.Follow` for live projection. Its socket queue is 256 events,
  2 MiB, and 5 seconds. It subscribes before loading state and sends any retained unsaved prefix.
  The client retains this connection's unsaved suffix across periodic saved-state refreshes. On
  reconnect it resets preview and cursor from saved state, and rejects old callbacks and fetches.
  Its local buffer remains limited to 1,024 events and 2 MiB; no frontend attempt token was added.

The full `RunView.Activity`, completed model response, and convenience response stream can retain
content. Pending flush limits and client queue limits do not cap total execution memory.

### Integration with typed output and native adapters

The 2026-10-30 main update introduced schema version 6, typed `Loop[T]` and `Runner[T]`, native
provider adapters, and ordered text and binary content parts. The checkpoint implementation keeps
these changes. Grouped saves preserve complete native responses, completion decisions, refusal
data, and binary parts. A progress save still cannot make an incomplete model call resumable.

Terminal saves contain the accepted typed value and its delivery content together. A failed or
blocked save prevents typed success and final content delivery. Terminal recovery decodes the
saved value without calling the provider, output processor, or custom validator again. The runner
still checks the current output contract before returning that value. Queue byte limits include
encoded binary data; they are not estimates of decoded image memory.

The current API reference below includes these upstream changes. The field-name review stays
deferred. The earlier `system_prompt`, `validator_called`, and `validator_result` categories were
removed by the native-output update; `output_validation` records output decisions.

## Implementation checks

Use real execution paths and public APIs. Tests must prove behavior with delayed and failed saves,
not only inspect a new configuration or helper. Include these cases:

- Many model chunks reach live consumers before model completion without per-chunk save calls.
- With no threshold reached, the completion save contains all chunks and the complete response.
- With an early save, the completion save contains the remaining chunks and complete response.
- One save contains one matching final runtime state and at most one new control checkpoint.
- A blocked model-request save prevents the provider call. A blocked `tool_ready` save prevents
  the tool call. A blocked result save prevents the next dependent step.
- Failed saves stop execution; exact retries after lost acknowledgments do not duplicate data.
- Storage rejects stale workers, cursor gaps, and conflicting requests without partial acceptance.
- Live events are not delivered twice when their grouped save completes.
- Mutating consumer or recorder data cannot change retained events or recovery state.
- Concurrent child events cannot move a checkpoint past data absent from its save request.
- A stop during streaming restores only saved state and retries the incomplete model call.
- A saved complete response and saved tool results are reused without repeated external calls.
- Frontend recovery resets an unsaved higher cursor and ignores frames from the old connection.
- A slow client cannot block a fast client, execution, or checkpoint persistence.
- Event and byte limits, delivery timeouts, unsubscribe, and cancellation release resources.
- A pending threshold causes one grouped progress save and blocks until it commits.
- An early progress save advances saved events without replacing the control checkpoint.
- A failed early save retains the exact request and prevents further dependent work.
- Saved chunks are not delivered live again; events beyond the saved prefix remain pending.
- Recovery after an early save retries the incomplete call and preserves later saved event numbers.
- Tests distinguish pending references from accepted activity when measuring memory retention.

Run the full native, CGO-disabled, and race suites with a 300-second package timeout after the final
implementation changes. The quickstart server has its own Go module and requires a separate test
run. Update the client tests for reset, refresh, and gap behavior, then verify the live example.

## Current public interfaces for review

This section describes the implemented API. Signatures below omit JSON tags and method bodies.
A recorded event, a save request, a checkpoint, and a complete saved run have different roles.
The naming and typed-category proposals above are deferred. These declarations keep current names.

### Persistence boundary

```go
type Recorder interface {
    Save(context.Context, *SaveBatch) error
}

type SaveBatch struct {
    SchemaVersion       int
    RunId               string
    BatchId             string
    PreviousEventNumber uint64
    Events              []*RecordedEvent
    Checkpoint          *Checkpoint
    Runtime             RuntimeState
}
```

Gent calls the recorder with the next ordered group. The application owns the database transaction,
worker fence, and storage schema. `BatchId` identifies the exact request for duplicate detection;
it is separate from event numbers. `PreviousEventNumber` is the expected saved cursor before the
transaction. `Runtime` describes accepted accounting after this group, including progress beyond
the control checkpoint. The optional checkpoint replaces the previous one only when supplied.

There is no required `Load` method on `Recorder`. The application loads a complete `RunView` through
its own storage API, claims the worker lease, and supplies that view to recovery. `WithRecorder`
attaches the writer. Without a recorder, Gent retains accepted state locally but supplies no
database durability.

```go
func ApplyBatch(view RunView, batch *SaveBatch, registries ...*CodecRegistry) (RunView, error)
func BatchDigest(batch *SaveBatch, registries ...*CodecRegistry) (string, error)
func ValidateBundle(bundle RunView, registries ...*CodecRegistry) error
```

`ApplyBatch` validates ordering, identity, history transitions, runtime, and checkpoint state. It
returns a new accepted view without executing hooks, models, or tools. Exact duplicates are safe;
a rejected batch does not change its inputs. It does not acquire a database transaction or check
an application lease. `BatchDigest` covers the typed contents and excludes `BatchId` itself.
`ValidateBundle` checks the reconstructed saved run; it does not by itself supply executable
bindings or prove that the application can resume every saved phase.

### Recorded evidence and the saved run

```go
type RecordedEvent struct {
    EventNumber     uint64
    RunId           string
    Ts              time.Time
    Type            string
    EventName       string
    ContextId       string
    ParentContextId string
    Iteration       int
    AttemptId       string
    ModelCallId     string
    ToolCallId      string
    Payload         *EncodedValue
    Change          *HistoryChange
}

type RunView struct {
    SchemaVersion          int
    RunId                  string
    LastAppliedEventNumber uint64
    History                HistorySnapshot
    Activity               []*RecordedEvent
    Checkpoint             *Checkpoint
    Runtime                RuntimeState
    Batches                map[string]string
}
```

`RecordedEvent` is numbered evidence. `Payload` holds the typed event value; `Change` holds a
structured history change when applicable. Identity fields link evidence to its context and call.
`RunView.Activity` contains the accepted event sequence. `History` contains the accumulated entries
and selection changes. `Batches` records accepted request identities and digests for exact retries.

History uses `HistorySnapshot`, `HistoryChange`, `HistoryRevision`, and `EntryId` from `history.go`.
`UpdateHistory(HistoryUpdate) error` queues a change. It becomes accepted with its save, together
with the event cursor and checkpoint. A checkpoint's active entry IDs refer to this saved history;
the checkpoint does not contain a second complete copy of the history.

```go
type RuntimeState struct {
    Contexts   []ContextRuntime
    Allocation uint64
}

type ContextRuntime struct {
    ContextId     string
    Iteration     int
    Counters      map[string]int64
    Gauges        map[string]float64
    NextModelCall uint64
    NextToolCall  uint64
    ExceededLimit *Limit
}
```

Runtime values preserve accounting, limits, and call-ID allocation. `Allocation` preserves the
run's identity allocation position. On restore, the latest accepted runtime can be newer than the
runtime inside the checkpoint. Restore must preserve accepted accounting without adding saved
usage twice or reusing identifiers allocated after the earlier control checkpoint.

### Checkpoint contents and execution phases

```go
type Checkpoint struct {
    SchemaVersion int
    RunId         string
    Revision      HistoryRevision
    EventNumber   uint64
    Phase         ExecutionPhase
    Contexts      []ContextCheckpoint
    Compatibility CompatibilityManifest
    Outcome       *SavedOutcome
}

type ContextCheckpoint struct {
    ContextId          string
    ParentContextId    string
    Name               string
    Task               EncodedValue
    Active             []EntryId
    Iteration          int
    CompletedIteration int
    AttemptId          string
    Runtime            ContextRuntime
    Limits             []Limit
    StartedAt          time.Time
    Deadline           *time.Time
    State              map[string]EncodedValue
    Completed          bool
    Extensions         map[string]SavedCheckpointState
}

type SavedOutcome struct {
    Value       *EncodedValue
    Refusal     *EncodedValue
    Reason      TerminationReason
    Result      EncodedValue
    Error       *SavedError
    CompletedAt time.Time
}
```

`Revision` binds the checkpoint to the corresponding history revision. `Contexts` places the root
first and preserves its task, active history selection, loop position, limits, deadline, and custom
state. The deadline is absolute; a replacement worker does not receive a fresh time allowance.
`Outcome` is present for a terminal checkpoint and lets recovery return the saved result. `Value`
contains the accepted typed output; `Result` contains its ordered delivery content. `Refusal`
preserves a native refusal when the provider refuses the request.

`ExecutionPhase` is a string with these supported constants:

| Constant | Meaning at the saved boundary |
| --- | --- |
| `PhaseInitial` | Setup completed; the first iteration has not started. |
| `PhaseIterationReady` | The current iteration can enter the agent loop. |
| `PhaseDecisionReady` | Saved agent state can continue processing a model response or tool plan. |
| `PhaseToolReady` | Effective tool input is saved; the external outcome may be uncertain. |
| `PhaseToolCompleted` | The tool result and updated plan are saved. |
| `PhaseIterationCompleted` | The iteration completed; prepare the next iteration. |
| `PhaseCompactionCompleted` | Compaction completed; continue without doing it again. |
| `PhaseTerminal` | Return the saved outcome without calling the model or tools. |

These phases do not describe arbitrary paused Go stacks. A custom loop must also save the state
needed to interpret its phase. Current recovery rejects active child contexts and child runtime
newer than the root checkpoint when it cannot restore their execution. Completed children remain
part of saved history. Root checkpoints own the control boundary; child saves accept evidence
without replacing that root boundary.

### ExecutionContext operations

The following methods belong to `*ExecutionContext`:

```go
FlushProgress() error
CommitCheckpoint(phase ExecutionPhase) error
Export() (RunView, error)
RetrySave(worker context.Context) (RunView, error)
RecordingError() error
Phase() ExecutionPhase
RecordInterruption(cause error) error
IsWorkerInterrupted() bool
CancelRun(cause error)
```

`FlushProgress` saves pending evidence and runtime without moving the root control checkpoint.
`CommitCheckpoint` saves that evidence plus a matching control snapshot. Both use the same blocking
recorder. The executor and toolchain call them at required boundaries; applications need not call
them for every chunk. A custom loop uses them when it introduces its own resumable state.

`Export` returns an owned copy of accepted state. It does not flush pending events. `RetrySave`
repeats the exact uncertain request, returns the resulting view, and requires continuation on a
fresh restored context. It does not make the failed worker safe to continue. `RecordingError`
reports a recording failure that stops further execution on that path.

`RecordInterruption` saves interruption evidence while keeping the previous control checkpoint.
Canceling the worker context allows replacement; `CancelRun` expresses an application decision to
end the run. `IsWorkerInterrupted` distinguishes worker interruption from a run cancellation or a
limit/deadline termination. These methods do not make an unfinished model stream resumable.

### Custom state and compatibility

The simple state methods belong to `*ExecutionContext`:

```go
SetCheckpointState(name string, value any) error
CheckpointState(name string) (any, bool, error)
ClearCheckpointState(names ...string)
BindCheckpointState(name string, component CheckpointState) error
Compatibility() CompatibilityManifest
SetCompatibility(manifest CompatibilityManifest) error
```

`SetCheckpointState` encodes working state. It does not write to storage. `CheckpointState` decodes
an owned value and reports whether the key exists. Set and clear operations take effect in a saved
checkpoint only when a matching checkpoint commit succeeds.

```go
type CheckpointState interface {
    CheckpointType() string
    CheckpointVersion() int
    SaveCheckpointState() (EncodedValue, error)
    RestoreCheckpointState(EncodedValue) error
}

type SavedCheckpointState struct {
    Type    string
    Version int
    Value   EncodedValue
}

type RecoveryConfig interface {
    RecoveryConfig() any
}

type CompatibilityManifest struct {
    Version    int
    Components map[string]string
}
```

`CheckpointState` lets a loop, custom data object, or compaction component preserve mutable state.
Bind explicit components before execution. These save/restore methods only copy local state; they
must not publish events or call external services. Gent saves the encoded value through `Recorder`.
The component method named `SaveCheckpointState` is not a second persistence callback.

`RecoveryConfig` describes declarative configuration, not mutable execution state.
`RecoveryFingerprint(value any) (string, error)` produces a configuration fingerprint. The
manifest identifies compatible replacements. Functions, credentials, live clients, goroutines,
and locks are supplied by the replacement process; they are not serialized into checkpoints.

ReAct uses `WithRecoveryVersion(version string) *Agent[T]`. The executor also has
`Config.RecoveryVersion` for application behavior that a configuration fingerprint cannot detect.
Custom mutable state still needs `CheckpointState`; a version string does not preserve that state.
The current ReAct implementation rejects continuation without an explicit recovery version.

### Restore and resume

```go
type RestoreBindings struct {
    Recorder           Recorder
    CheckpointPolicy   CheckpointPolicy
    Observers          []RecordedEventObserver
    Codecs             *CodecRegistry
    Data               LoopData
    Compatibility      CompatibilityManifest
    Components         map[string]CheckpointState
    CompactionStrategy CompactionStrategy
    CompactionTrigger  CompactionTrigger
    Configure          func(*ExecutionContext) error
}

func RestoreExecutionContext(
    worker context.Context, bundle RunView, bindings RestoreBindings,
) (*ExecutionContext, error)
```

`RestoreBindings` supplies new process objects. `Data` holds custom loop data when needed;
`Components` binds named state owners. The compaction strategy and trigger must be supplied
together when rebinding them. `Configure` prepares the actual context before compatibility checks
and state restoration, including registration of application codecs.

The normal application entry point is `executor.Runner[T].Resume`:

```go
func (e *Runner[T]) Resume(
    worker context.Context, saved gent.RunView, bindings gent.RestoreBindings,
) (T, *gent.ExecutionResult, error)

func (e *Runner[T]) Continue(ctx *gent.ExecutionContext) (T, *gent.ExecutionResult, error)
```

`Resume` configures, restores, and continues the saved run with the runner's loop. `Continue` is the
lower-level entry point for an already restored context. Models and tool implementations are bound
through that loop. A terminal saved run returns its typed outcome without executing the loop or
processing the output again. The runner configures the current output contract and checks its
compatibility, so a different output type or validator cannot silently accept an old value.
Direct metadata-only `RestoreExecutionContext` can omit executable bindings for a terminal run;
it still needs codecs for the saved values. When `Configure` is supplied, compatibility is checked.

New contexts accept `WithRunId`, `WithRecorder`, `WithObserver`, `WithCodecs`,
`WithCheckpointPolicy`, and `WithCompatibility`, each returning `ContextOption`. The context also
exposes `RunId()`,
`AttemptId()`, and `Codecs()`. Recovery creates a new internal attempt identity while retaining
logical run and call identities. This does not add a frontend attempt-ID requirement.

### Encoding and saved errors

```go
type EncodedValue struct {
    Type    string
    Version int
    Data    json.RawMessage
}

type ValueCodec interface {
    Type() string
    Version() int
    Encode(any) (json.RawMessage, error)
    Decode(json.RawMessage) (any, error)
}
```

`NewCodecRegistry(...CodecOption) *CodecRegistry` creates the registry. It provides
`Encode(any) (EncodedValue, error)`, `Decode(EncodedValue) (any, error)`,
`Clone(any) (any, error)`, `Marshal(any) ([]byte, error)`, and `Unmarshal([]byte, any) error`.
Register application types with `RegisterType(string, int, any) error`,
`Register(any, ValueCodec) error`, or `RegisterValueType(reflect.Type) error` before use.
`DecodeEvent(*RecordedEvent) (any, error)` is available on the registry and execution context.

Use the registry for stored batches and run views so concrete content, input, and metadata types
survive recovery. Decoding does not load application code from saved data. Both processes need
compatible registered types. ReAct and toolchain `RegisterCodecs` methods register their types.

`SavedError` preserves `Message`, `Codes`, and nested `Causes`; `SaveError(error) *SavedError`
constructs it. `CodedError` extends `error` with `ErrorCode() string` for stable application error
classification. It does not preserve arbitrary private implementation fields automatically.

### Tool plans and uncertain external outcomes

```go
type ToolRecoveryFunc func(
    context.Context, *ToolRecoveryRequest,
) (*ToolRecoveryResponse, error)

type ToolRecoveryRequest struct {
    RunId      string
    ContextId  string
    ToolCallId string
    AttemptId  string
    Name       string
    Args       map[string]any
    Input      EncodedValue
}

type ToolRecoveryResponse struct {
    Result          ToolRecoveryResult
    SteeringMessage string
    Output          *ToolCallResult
}
```

Both `Tool` and `ToolChain` expose
`Recover(context.Context, *ToolRecoveryRequest) (*ToolRecoveryResponse, error)`.
`ToolBuilder[I, O].Recovery(ToolRecoveryFunc) *ToolBuilder[I, O]` attaches a handler. Without a
handler, the builder returns an unresolved decision. Recovery checks must be safe to repeat.

The result constants are `ToolRecoveryCompleted`, `ToolRecoveryRetryAllowed`, and
`ToolRecoveryUnresolved`. Completed requires output. The other results must omit output. An
unresolved result becomes an unknown tool observation and prevents the remaining plan from running.
A steering message supplies guidance; it does not authorize a retry. Gent saves the decision
before using it. Accepted result evidence takes precedence over another recovery call.

The plan structures hold the next step and the evidence needed to recover it:

| Type | Fields |
| --- | --- |
| `PlannedToolCall` | `Id`, `Name`, `Args` |
| `SavedToolCall` | `Id`, `Name`, `Args`, `Input`, `AttemptId`, `Recovery` |
| `ToolPlan` | `CheckpointKey`, `Abandoned`, `Version`, `Calls`, `NextCall` |
| Remaining `ToolPlan` fields | `ReadyCall`, `Results`, `State`, `RecoverySteering` |

`ToolChain.PrepareCalls(*ExecutionContext, []ModelToolCall) (*ToolPlan, error)` prepares the plan.
`ToolChain.ExecuteNext(*ExecutionContext, *ToolPlan, TextFormat) (*ToolCallResult, error)` executes
one step. The toolchain saves effective input before the call and the result/next position after it.
The context also supplies `SetToolPlan`, `SavedToolPlan`, `NewToolCallId`, `SelectToolCall`,
`BindToolCall`, and `RecoverTool`. `SetToolPlan` updates working state; it does not itself commit.
`RecoverTool` returns the result, whether recovery completed the step, and any error.

`ParallelToolChain` adds `SupportsParallelCalls() bool`. Concurrent steps need independent plans
and checkpoint keys; `BindToolCall` associates each logical call with its own plan until unbound.
`ToolIdentityFromContext(context.Context) (ToolIdentity, bool)` exposes `RunId`, `ContextId`,
`ToolCallId`, and `AttemptId`; `ExecutionContext.ToolContext(string) context.Context` supplies that
identity. Tools can use the stable logical call ID with an external idempotency API. A checkpoint
cannot eliminate the failure window between an external effect and its result save.

### Committed observers and live consumers

```go
type RecordedEventObserver interface {
    ObserveCommitted([]*RecordedEvent, *CodecRegistry) error
}
```

This observer receives accepted events after persistence. Its method is not the live delivery
callback and does not return a persistence acknowledgment. The current implementation propagates
observer errors after commit; such an error cannot undo the write. Keep display delivery failures
outside this path. Use `SubscribeEvents` or `trace.Sequencer.Follow` for live delivery.

`trace.RestoreSequencer` reconstructs display state without resuming execution. It accepts a
`gent.RunView` and `trace.Config`, and returns `(*trace.Sequencer, error)`. The application still
owns its frontend snapshot API.

```go
type DeliveryConfig struct {
    MaxEvents int
    MaxBytes  int
    Timeout   time.Duration
}

func (ctx *ExecutionContext) SubscribeEvents(DeliveryConfig) (*EventSubscription, error)
func (ctx *ExecutionContext) SubscribeAllWithConfig(DeliveryConfig) (*StreamSubscription, error)
func (ctx *ExecutionContext) SubscribeToStreamWithConfig(
    string, DeliveryConfig,
) (*StreamSubscription, error)
func (ctx *ExecutionContext) SubscribeToTopicWithConfig(
    string, DeliveryConfig,
) (*StreamSubscription, error)
```

`EventSubscription` exposes `Events() <-chan *RecordedEvent`. It includes all contexts and all
sequence positions. `StreamSubscription` exposes `Chunks() <-chan StreamChunk` for a filtered
chunk view, which can have gaps in the shared event sequence. Both expose `Err() error`,
`Done() <-chan struct{}`, and `Close()`. Values delivered to a consumer are owned copies.
`Close` discards queued values and waits for the delivery worker. Repeated close calls are safe.
Normal context release drains queues with the configured timeout. Cancellation closes delivery;
already observed evidence can still be saved after that closure.

The convenience methods `SubscribeAll`, `SubscribeToStream`, and `SubscribeToTopic` still return
`(<-chan StreamChunk, UnsubscribeFunc)` and use finite default limits. Use the `WithConfig` methods
to read delivery errors. `SubscribeAllDraining` is for internal consumers that keep consuming after
unsubscribe; its drain is also subject to the timeout.

`trace.Sequencer.Follow(ctx, config)` returns `(*trace.LiveFeed, error)`. Attach it before
execution.
For recovery, first rebuild the sequencer from the saved view, then attach the feed through
`RestoreBindings.Configure`. Do not also attach that sequencer as a committed observer.
`LiveFeed.Wait()` reports delivery or projection errors after the context releases the source.
`LiveFeed.Close()` discards pending delivery and waits. Display redactors must return promptly;
Gent cannot forcibly stop an application callback.

`trace.Sequencer.SubscribeWithLimits(config)` returns `(*trace.Subscription, error)`, with
`Events`, `Err`, `Done`, and `Close` methods. It uses the same configurable queue bounds.
`Subscribe()` uses those defaults. The older `SubscribeWithConfig` API retains count-based drop
policies; zero capacity now uses a finite default. Use `SubscribeWithLimits` for byte limits and
timeouts. Overflow callbacks use one worker and coalesce notifications; callbacks must return.

### Persistence failures and remaining API choices

`PersistenceError` contains the exact `Batch *SaveBatch` and `Cause error`. It unwraps the cause.
Relevant sentinel errors are `ErrEventGap`, `ErrWrongRun`, `ErrHistoryConflict`,
`ErrPersistenceStopped`, `ErrIncompatibleCheckpoint`, `ErrUnsupportedRecovery`,
`ErrUncertainTool`, and `ErrToolPlanComplete`. The last one signals an exhausted tool plan rather
than a failed save. Storage-specific fence errors belong to the application writer.

The implemented types cover atomic save groups, optional checkpoints, typed recovery, uncertain
tool outcomes, bounded live delivery, and grouped progress saves. Saved-history retention and a
hard total-memory bound require a separate design; early saves alone do not provide them.
Field names and event-category types remain deferred as requested.

## Source references and documentation updates

- [Save types and accepted state](run.go)
- [Event types and event name fields](events.go)
- [Framework event name constants](event_names.go)
- [Trace event categories](trace/types.go)
- [Checkpoint and recovery types](checkpoint.go)
- [Context options and working checkpoint state](context_history.go)
- [Custom state and compatibility](checkpoint_extensions.go)
- [Restore and resume entry point](executor/checkpoint.go)
- [Typed encoding](history_codec.go)
- [Saved error classification](history_error.go)
- [Batch application and validation](run_replay.go)
- [Chunk recording](context_recording.go)
- [Progress saves and control checkpoints](context_checkpoint.go)
- [Managed model calls](model_call.go)
- [Saved model replay](model_replay.go)
- [Tool readiness and recovery](tool_plan.go)
- [Completed tool steps](toolchain/checkpoint.go)
- [Quickstart writer](docs/quickstart/server/store.go)
- [Quickstart connection lifecycle](docs/quickstart/client/src/connection.ts)
- [Quickstart event reducer](docs/quickstart/client/src/state.ts)
- [History and storage contract](docs/history.md)
- [Frontend contract](docs/native-tools.md)

Update the public comments, history documentation, streaming documentation, trace documentation,
and quickstart examples when the implementation changes. Remove claims that every live chunk is
already durable. Preserve comments that explain still-valid ownership and recovery guarantees.
