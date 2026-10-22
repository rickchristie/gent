# Durable history and continuation

Gent keeps an append-only history, an ordered working context, and a checkpoint for continuation.
Use a recorder when execution must survive a worker restart. The executable [history
example](../examples/history/main.go) demonstrates transactions, writer fencing, missed updates,
fresh bindings, terminal restoration, and uncertain tool recovery without a database.

## Three different views of a run

`HistoryEntry` holds messages, origin, expiry, metadata, and execution identity directly. Message
and summary entries supply working context; compaction entries describe a decision. One execution
iteration can add several entries, and an accepted final answer can add history without entering
the next working context. Iteration counters still count loop iterations.

`HistorySnapshot` retains every `HistoryEntry`, including original observations, summaries, and
compaction decisions. Entries have stable run/context/attempt identities. Replacing a record creates
a new entry with `Replaces`; it never edits the earlier entry. Compaction markers identify all input
IDs, the resulting ordered selection, summary IDs, model call IDs, and any failure.

Working context is an ordered list of entry IDs. `ContextAt(contextID, revision)` reconstructs it at
any accepted history revision. Reorder, removal, expiry, pin changes, and same-length replacements
are explicit transitions. Full tool results remain in history when later requests use abbreviated
observations. Each abbreviation is captured with the original result, so a changed tool cannot
rewrite old evidence.

`RunView` contains the entire accepted history and activity sequence, the
latest checkpoint, absolute accounting, and allocation high-water marks. Its cursor includes saved
partial attempts beyond the checkpoint. A checkpoint identifies where control can continue; its
cursor can be older than the full view's cursor.

## Configure persistence and observation

```go
codecs := gent.NewCodecRegistry()
seq, err := trace.NewSequencer(runID, trace.Config{
    Codecs: codecs,
    IncludeModelRequests: true,
    IncludeModelResponses: true,
})
if err != nil {
    return err
}
ctx := gent.NewExecutionContext(worker, "refund", gent.NewBasicLoopData(task),
    gent.WithRunId(runID), gent.WithCodecs(codecs),
    gent.WithRecorder(recorder), gent.WithObserver(seq))
agent := react.NewAgent(model).WithRecoveryVersion("refund-agent/v1")
exec := executor.NewRunner(agent, executor.DefaultConfig())
result, err := exec.Execute(ctx)
```

Handle the returned error even if `result` is nil. A persistence failure may prevent a terminal
checkpoint. Constructors with a task preserve its original value; unknown task codecs surface via
`RecordingError()` and `Execute` before any external call. Register custom content codecs before
constructing that context.

`WithRecorder` installs the application storage boundary. `WithObserver` installs an accepted-event
observer, including a sequencer. Use the same codec registry in the recorder when storing custom
content. The execution context owns numbering and acceptance.
No recorder means in-memory acceptance only. Mutable execution hooks still use the event registry.

`trace.Sequencer` projects accepted canonical numbers. `gent.RecordedEvent` holds the lossless
activity record. `trace.Event` is its JSON-safe display projection; it has the
same event number, but can be redacted and truncated. Keeping these types separate prevents display
settings from becoming a restoration format. Live observation and restored traces use the same
direct projection. Replay never needs a live execution context or dispatches a mutation hook.
Call correlation uses the recorded logical call ID and attempt ID. Matching a stream or tool name
cannot establish which call produced an event; events with missing or unknown call IDs remain visible
without updating another call's summary.

## Edit context through owned values

```go
if err := ctx.AppendRecord(record, true); err != nil {
    return err
}
if err := ctx.CommitCheckpoint(gent.PhaseIterationCompleted); err != nil {
    return err
}
history := ctx.History()
records, err := ctx.Scratchpad()
```

`Task`, `Scratchpad`, `History`, `Export`, and decoded values are owned copies. History and working
context APIs use `*HistoryEntry`; entries returned by a snapshot belong to that snapshot. Copying
at boundaries, including nested metadata and message parts, supplies isolation. Pointer syntax does
not enforce it. `NewHistoryEntry(entry)` copies the proposed content and assigns identity.
For a custom edit, read `ContextDraft()`, prepare replacement entries, then call `UpdateHistory`
with its draft revision, appended entries, complete resulting `Active` order, and a concrete reason.
A stale draft returns `ErrHistoryConflict`. `CustomState` provides named, typed state replacements
in the same transition.

Draft revisions are local concurrency tokens; public history revisions advance upon acceptance.
Pending edits affect the next request under construction, while `History()` exposes accepted state.
Use `FlushProgress` to accept evidence without moving the control checkpoint. Only commit a control
phase when your loop can actually resume there. A custom loop must save its continuation state.

`StateChangeEventSubscriber.OnStateChangeEvent` receives an accepted `HistoryChange`: previous/new
revision, appended entries, selections, custom replacements, and reason. These callbacks run after
acceptance, outside commit locks, and may publish more events.

## Recorder transactions and retries

Implement `Recorder.Save(context.Context, *gent.SaveBatch) error`. A batch contains its schema/run
ID, stable digest ID, previous cursor, consecutively numbered events, accounting, and optional
checkpoint. The application transaction must:

1. Check the writer lease or fencing token and expected previous cursor.
2. Accept an identical batch ID idempotently; reject conflicting or stale writes.
3. Store all events, history effects, accounting, and checkpoint together.
4. Commit before returning success or publishing an update to another service.

Use `codecs.Marshal(batch)` and `codecs.Unmarshal(savedJSON, &batch)` for portable storage. Plain
JSON decoding cannot restore concrete message parts or values hidden in metadata interfaces.
The same helpers encode/decode a complete `RunView`. `EncodedValue` keeps its tagged payload intact
instead of recursively expanding already encoded JSON bytes.

`gent.ApplyBatch(view, batch, codecs)` reconstructs this transaction's materialized view. The codec
argument may be omitted for built-in types. It checks numbering,
run identity, revisions, entry references, and checkpoint consistency. It does not execute hooks,
tools, stats publishers, or limits. A database still supplies atomicity and fencing across workers.
The execution context serializes local acceptance; the database supplies the distributed writer lease.

Gent assigns numbers and freezes the candidate before `Save`. Readers and accepted subscribers
continue seeing the old cursor while saving. A successful acknowledgment promotes the whole batch.
Internal commits share already frozen payloads and calculate the digest once. Ownership copies are
made at application boundaries, avoiding repeated serialization for each streamed token.
The private event and batch indexes grow only after acceptance. `Export` copies those indexes under
the run lock before copying payloads, so a caller can read or change its snapshot during recording.
`PersistenceError` retains the exact failed or uncertain batch and stops further execution. No next
model or tool effect starts after that failure. `ctx.RetrySave(worker)` retries the same batch and identity;
do not manufacture a new batch ID after a lost acknowledgment. After recovery, load the store's
accepted run and resume on a fresh execution context. Retrying a save does not clear the failed
execution's cancellation or restart its model/tool work.

Publication failure after a storage commit does not mean the transaction failed. Handle delivery
separately, using a transactional outbox or the full-view reload protocol. Do not report a committed
transaction as rejected just because a browser or message bus is unavailable.

Already observed evidence can be saved after worker cancellation. Saves use an independent context
with a 30-second timeout; the application must still reject a stale writer. This is why cancellation
alone cannot replace storage fencing.

Executors release their execution context on every return path, after final evidence is accepted
when possible. Release cancels context-bound work and closes stream subscriptions for draining.
`CompleteChild` also releases child work. Direct users of `NewExecutionContext` must defer
`ctx.Release()`. Release is safe to repeat and does not change a saved outcome or mark a successful
execution as interrupted. Explicit application cancellation still uses `CancelRun`.

## Exact model requests and tool evidence

Provider adapters implement:

```go
GenerateContentStream(context.Context, gent.ModelCallRequest) (gent.Stream, error)
```

Invoke them through `ctx.GenerateContentStream(model, call, request)`. The framework runs mutable
before-hooks, freezes the effective messages and resolved options, accepts that request, then calls
the provider. The request records the source selection/revision as well as the actual provider
input. Hooks can change options; the adapter receives those final values. `NewModelCallRequest`
evaluates option functions once. ReAct's `WithCallOptions` resolves its functions once when
configuring the agent and reuses those values for compatibility and requests.

Adapters emit chunks through the returned stream; they must not publish duplicate Gent events. The
managed boundary accepts chunks before delivering them live and records the final response, partial
output, usage, and error. `Stream.Close` requests cancellation. `Response` waits until the producer
finishes. The built-in LangChainGo adapter uses a bounded queue and cancellation-aware backpressure;
custom adapters can use `NewBoundedStreamWithDuration` too. Its producer must run in a goroutine and
call `Complete` after it actually stops, even on cancellation.

Only model, tool, and system-prompt before-hooks mutate effective input. Other lifecycle observers
receive copies. The low-level typed `Publish...` helpers queue evidence and return event values;
managed execution checks `RecordingError` and crosses explicit acceptance barriers. Custom callers
must check `FlushProgress` before external work. Generic `Publish` returns an error directly.

YAML, JSON, and SearchJSON implement `RecoverableToolChain` with `Prepare` and `ExecuteNext`. Plans
save logical call identities, positions, raw arguments, effective typed input, completed results,
and the next cursor. SearchJSON also saves its seen-tools state. Two identical calls in two list
positions have different logical IDs. `ToolCallResult` retains IDs, raw/typed input, typed output,
formatted text, media, instructions, errors, and the deduplication summary captured by that tool.

## Continue on a fresh worker

Create fresh model/tool/agent/configuration objects. Register their declared types before decoding
saved JSON; this requires type definitions, not live provider calls. Resume prepares the actual
restored context and checks configuration automatically:

```go
codecs := gent.NewCodecRegistry()
if err := freshAgent.RegisterCodecs(codecs); err != nil {
    return err
}
var saved gent.RunView
if err := codecs.Unmarshal(savedJSON, &saved); err != nil {
    return err
}
seq, err := trace.RestoreSequencer(saved, trace.Config{Codecs: codecs})
if err != nil {
    return err
}
result, err := freshExecutor.Resume(worker, saved, gent.RestoreBindings{
    Recorder: replacementWriter,
    Observers: []gent.RecordedEventObserver{seq},
    Codecs: codecs,
    ToolRecovery: recovery,
    // Supply these together if the saved run used compaction.
    CompactionTrigger: trigger,
    CompactionStrategy: strategy,
})
```

Restoration validates the full bundle, codec versions, manifest, state, and supported boundary
before external work. The old run/context/logical IDs survive; a new attempt ID and higher event
numbers identify the replacement worker. Original limits, absolute deadlines, counters, gauges,
allocation, and compaction baselines are restored. Known usage newer than the control checkpoint
remains charged once. Counters are imported directly, so replay does not check limits or increment
stats again.

Supported root ReAct boundaries are initial, iteration-ready, saved decision, tool-ready, completed
tool, completed iteration, and completed compaction. A saved successful model response is reused if
the worker stopped before its decision checkpoint. A partial or failed model attempt remains visible
and a retry uses a separate attempt. Completed tool steps are reused in order. Completed compaction
is skipped once at takeover; its baseline, selected records, and summary commit together.

Model recovery matches `ModelCall.Id`, not stream labels. A saved successful call returns its
original chunks in order and its complete response, with no new provider call or usage charge.
The final `Stream.Response()` can contain choice data that does not appear in the chunks.
When `Id` is empty, automatic IDs replay in call order after the saved checkpoint. Keep that call
order stable during recovery. Supply explicit, unique IDs for concurrent calls or code that can
change the call order. New calls use IDs above the saved allocation counter.

A canceled worker leaves an interrupted attempt and the last recovery checkpoint. `CancelRun`
expresses intentional terminal cancellation. An absolute deadline cannot be extended by restarting.
A terminal run returns its saved output/error without rebinding a provider, tool, or agent. Codecs
for stored custom types are still required when decoding its history. A terminal run accepts no new
work. Application code must not replace a terminal failure with an empty success.

### Uncertain tools

Saving tool-ready happens before calling the tool. The process can die after an external write but
before its result is saved. Gent cannot determine whether that action occurred from a checkpoint.
The default is `ErrUncertainTool`, leaving the run resumable.

`ToolRecovery` receives the stable logical ID, previous attempt, arguments, and effective input. It
must return one of `ToolRecoveryCompleted` with the full result, `ToolRecoveryRetryAllowed`, or
`ToolRecoveryUnresolved`. A durable saved result takes precedence over that adapter. An authorized
retry uses the same logical ID, a new attempt, and the original effective input without rerunning
its mutation hook. Tools can read `ToolIdentityFromContext` and pass that ID to an external
idempotency mechanism. Gent makes no exactly-once guarantee about external side effects.

Tool summaries are captured once with the completed result. Recovery keeps that value, including
an empty summary, so it cannot change when the callback or application state changes.

### Compatibility and custom state

Set ReAct's `WithRecoveryVersion` to identify application behavior: hooks, prompts, clocks, tool
implementations, validators, custom sections, and private provider defaults. Change it when their
meaning changes. Built-in configuration hashes also include prompt/format/tool declarations, model
identity/options, repetition settings, and compaction configuration.
`LCGWrapper.WithRecoveryVersion` can identify configuration hidden inside its injected client.
Credentials are always rebound.

Ordinary execution accepts custom clocks and prompt builders without a recovery version. The saved
configuration is then marked unsupported for recovery. A nonterminal restore requires the version
to have been set on the original agent. `WithClock` uses the standard prompt time formats;
`WithTimeProvider` also supports custom formats.

Custom loops need an executor `RecoveryVersion` and `CheckpointState`. Stateful custom data,
triggers, strategies, and other components implement that same explicit type/version/save/restore
contract. Bind named components before saving; provide fresh `RestoreBindings.Components` when
restoring. Custom data is provided through `RestoreBindings.Data`. A version identifies executable
behavior; it does not serialize hidden mutable fields. Save/restore methods only copy local state
and must not publish events or perform external work.

`RestoreBindings.Configure` supports advanced setup on the actual restored context. It binds
configuration and components before state restoration; it must not publish events or start external
work. `Runner.Resume` adds the executor/loop setup and performs it once, so setup cannot reset
already restored custom state. Custom executors can still use `RestoreExecutionContext` and
`Continue` explicitly with compatible bindings. That lower-level path must initialize the loop
during restoration; `Continue` checks executor compatibility without reinitializing saved state.

An active child or JavaScript frame is unsupported in this release: there is no saved Go/VM stack.
Gent rejects these continuations explicitly. Completed children remain archived in later
checkpoints, including their accounting and history. Ordinary execution and observation of children
and JavaScript continue to work. Unsupported custom loops can execute normally but cannot claim
resumable state.

`CodecRegistry` preserves dynamic types, large integers, named containers, bytes, media, cache
duration, and typed tool metadata. Built-in tool/JSON-answer declarations register their types.
Register values hidden inside `any` and custom content parts explicitly with a stable type name and
version, or supply a `ValueCodec` for private fields. Unknown types, cycles, callbacks, channels,
and unsupported versions fail explicitly. Codecs must be deterministic local serialization
functions, return fresh owned values, and keep their meaning stable by version.

Compaction strategies receive `CompactionInput{Revision, Entries}`. Each owned entry carries its
identity; `EntryIds()` supplies the ordered IDs when needed. Strategies return `HistoryUpdate`,
including optional `CompactionRecord` provenance such as generated summary IDs. The executor fills
the input selection, decision identity, model-call references, and error, then accepts the update
with trigger state and checkpoint. Removal-only and failed compactions remain recorded.

`SavedError` preserves text, causes, standard Gent/context classifications, and `CodedError`
application codes for `errors.Is`. It does not recreate arbitrary private error implementations. Use
a value codec when private error fields themselves are needed inside a typed saved value.

## Observers, cursors, and retention

Use `seq.Snapshot`, `EventsAfter`, and `Subscribe` for display. Use `ctx.Export()` or the
same consistent application-store read for lossless replay, late joins, recovery, and reconnects.
`RecordedEventObserver.ObserveCommitted(events, codecs)` receives owned `[]*RecordedEvent` values
after acceptance. The registry decodes payloads without consulting mutable execution state.
Local retention and redaction only affect display. Canonical runs retain the full unredacted
task, requests, outputs, and evidence; apply application access controls to those reads.
`IncludeRunOutput` and `RedactRunOutput` apply to iteration and final execution results.

A joining observer buffers updates, loads a full view through cursor C, discards buffered duplicates
through C, then applies contiguous later updates. Reload on a gap. Periodically reload even without
a gap so a lost terminal update is eventually discovered. A checkpoint at event 120 with saved
progress through 128 resumes numbering at 129. Reloading through 130 includes a missing event 129;
the next update to apply is 131. Slow or bounded display subscribers cannot alter durable ordering.

Permanent tests cover subprocess recovery, recorder failure boundaries, retries, exact resumed
requests, two summaries, immutable views, cursor/delta replay, lost completion, interrupted model
usage, tool reconciliation, SearchJSON paging/schema state, typed values, extension state, remaining
limits, terminal failure/cancellation, deadlines, concurrent observers, and unsupported boundaries.
