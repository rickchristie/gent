# Gent reliability and resource review

Review date: 12 October 2026. Source revision: `f3b798a`.

All 12 confirmed findings are fixed in the working tree. The fixes preserve the intended model,
search, parsing, history, and recovery contracts. Failed and completed operations now release their
resources, and concurrent operations have defined ordering. No feature was removed.

The required constraints are in [AGENTS.md](../../AGENTS.md), in priority order: reliability,
CPU and memory use, then performance. Normal regression tests now cover the failures. The tagged
[probes](probes) preserve the original checks; their findings and source line numbers below describe
revision `f3b798a`, before these fixes.

## Fixes and regression coverage

| Finding | Root cause fix |
| --- | --- |
| R1 | Hold native lifetime locks through use; close once after active calls return. |
| R2 | Register each agent's sections once; protect shared built-in formats with locks. |
| R3 | Read entry IDs, records, and revision from one snapshot before expiration. |
| R4 | Close an error stream and await accepted model completion before the final checkpoint. |
| R5 | Lock each fused update across all subindexes; exclude reads during updates. |
| R6 | Release execution, child, and restored deadline contexts on every exit path. |
| R7 | Keep unsubscribe ownership after close; allow graceful drain to become discard. |
| R8 | Discover XML tags with supported expressions and quote literal section names. |
| R9 | Check cancellation, cancel native runs, and publish regex indexes under a lock. |
| R10 | Clear consumed queue slots and release empty backing storage. |
| R11 | Append private event indexes after acceptance; copy only public snapshots. |
| R12 | Reuse one CPU session, apply thread limits, release model bytes, stop idle spinning. |

[Root regression tests](../../reliability_test.go) and
[lifetime tests](../../reliability_lifetime_test.go) cover shared agents, model error completion,
concurrent expiration, fused Add/Add, Add/Remove, Add/Swap, Swap/Add and Search/Add ordering,
execution and child cleanup, restore failure, export ownership, and retained queue references.
The existing save failure, retry, replay, checkpoint, and output tests remain in place.

[Native tests](../../search/embedder_lifetime_test.go) cover cancellation before work and while a
permit is occupied, repeated and concurrent inference, repeated close, and close during native use.
The close test starts a separate process, including in race builds. The checked-in quantized model
checks exact vectors without a model download. Existing cached-model ranking tests still run.
[Trace tests](../../trace/subscription_lifetime_test.go) cover empty and unread bounded or unbounded
queues with both close orders and concurrent unsubscribe.

The test setup no longer downloads models before `m.Run`. Missing model files or a missing native
runtime can skip the applicable checks; model, tokenizer, and inference errors now fail once those
prerequisites are present. Download setup has a 30-minute deadline per file and publishes a complete
temporary file atomically. Partial downloads cannot remove another caller's complete file.
[Download tests](../../common/download_test.go) exercise incomplete responses and canceled reads.
JSON and YAML prompt tests now compare complete strings. XML has a bounded fuzz target.

### Follow-up: cancel fused-index lock waits

On 16 October, review found that the R5 update lock could delay cancellation. A search, add, or swap
used an ordinary blocking lock before checking its context. Long embedding work in another update
could therefore keep an already canceled request waiting, or prevent a waiting deadline from ending
the call. An early context check alone could not fix cancellation after the wait had started.

Fused indexes now use a weighted semaphore from `golang.org/x/sync`, at the version already selected
by the module graph. Each search acquires one permit; an update acquires all permits. Thus searches
can overlap, and updates still exclude all other operations across every subindex. The wait queue
prevents readers from bypassing a queued update. Cancellation removes the waiter and keeps no permit
or detached worker. `Remove` has no context parameter and retains its blocking contract.

[Cancellation tests](../../search/fused_index_cancellation_test.go) hold a FlatIndex in embedding
work and cover cancellation before acquisition and while waiting, expired and active deadlines,
queued `Search`, `Add`, and `Swap`, concurrent readers, and lock reuse after cancellation. They also
check that a canceled writer stops blocking other readers while an earlier reader remains active.
Both original failure cases failed before this fix. The existing operation ordering tests remain.
All 32 cancellation and deadline cases passed 10 race-detector runs. The existing ordering tests
also passed 10 runs. On 16 October, the full native, CGO-disabled, and race suites passed with
`-count=1 -timeout 300s`; `go vet ./...` and the tagged review probes also passed.

### Follow-up: cancel native embedding waits during close

On 22 October, review found another wait that could delay cancellation. With spare inference
capacity, an embedding call could acquire a permit and then wait on the lifetime read lock behind
`Close`. An earlier active call kept `Close` waiting. Cancellation could not stop the read-lock
wait, so the later call returned only after the earlier call finished. The independent deadline
reproducer failed in all three race-detector runs; its stack showed the blocked read lock.

The inference limit now uses the existing weighted semaphore. `Close` acquires all inference
permits before it takes the lifetime write lock. Embeddings acquire one permit with their context
before they take the read lock. Thus an embedding that owns a permit cannot meet a pending lifetime
writer; later embeddings wait on a semaphore that supports cancellation. Canceled waiters leave
no permit or detached worker. The lifetime lock still protects token counting, and the closed
state still prevents repeated native cleanup. The inference limit and output values are unchanged.

The [native regression tests](../../search/embedder_lifetime_test.go) now cover cancellation and
deadlines with two and four permits. They hold one or three active calls, leave spare capacity,
and start four concurrent close calls. The queued embedding must return its context error while
the active calls remain held. All close calls must then finish, which also checks that canceled
waiters return their permits. Complete vectors, no extra inference, repeated close, and use after
close are checked. All four original cancellation cases failed before the fix.

The independent reproducer passed all three race-detector runs after the fix. The final four
cancellation cases and the existing native lifetime tests passed 10 race-detector runs. On
22 October, the full native, CGO-disabled, and race suites passed with `-count=1 -timeout 300s`;
`go vet ./...` and the tagged review probes also passed.

## Measurements after the fixes

The same streaming benchmark, host, and chunk sizes as the original review produced these results:

| Chunks | Elapsed time | Total Go allocation per operation |
| --- | --- | --- |
| 1,000 | 98–105 ms | 38.8–38.9 MB |
| 2,000 | 202–208 ms | 77.6 MB |
| 4,000 | 379–400 ms | 154.9–155.0 MB |
| 8,000 | 743–770 ms | 309.4 MB |

Allocation now grows approximately linearly. At 8,000 chunks it fell from about 2.78 GB to 309 MB;
elapsed time fell from about 2.2 seconds to 0.76 seconds. These are total allocations, not peak use.
The per-chunk recording, ownership, and checkpoint contracts remain enabled.

The small quantized fixture now takes 5.5–7.0 microseconds per complete embedding, compared with
about 1.009 milliseconds in the original probe. On cached `multilingual-e5-small`, the final check
took 2.34–2.68 ms per serial query and 1.41–1.61 ms per query with concurrent callers and a limit of
two active calls. The latter is elapsed time divided by query count, not individual request latency.
The benchmark compares complete vectors with the first result. It excludes model loading, and
these host-specific results do not estimate production-model speedup.

Native inference shares one session and thread pool with separate tensors and run options per call.
This follows the upstream [concurrent Run contract][ort-run]. The default loader owns the copied
model bytes; Gent no longer retains a second copy in Go. Idle threads stop spinning when the last
active run returns, using the upstream [session options][ort-options].

A Linux resource probe used `multilingual-e5-small`, two native threads, four concurrent calls,
and six create/use/close cycles with 200 queries each. Live Go heap during use fell from about
119 MB to 0.5 MB after removing the model-byte copy. Native live allocations were about 400 MB
during use and 8.5–9.2 MB after close. One native pool thread exited on each close. A 500 ms idle
sample used 0.058–0.079 ms of CPU after the spin change, compared with 10.6–13.0 ms before it.

Process RSS must not be read as live allocation. The Linux allocator retained freed pages after
close. The diagnostic `malloc_trim` call reduced RSS from roughly 343–399 MB to 54–79 MB, while
live-allocation counters stayed the same. Gent does not change the host allocator or call this
process-wide operation. Applications should reuse an embedder while its model is needed.

[ort-run]: https://github.com/microsoft/onnxruntime/discussions/10107
[ort-options]: https://onnxruntime.ai/docs/performance/tune-performance/threading.html

## Scope and verification

The source review covered execution and child contexts, events, stats and limits, history and
recovery, streams, model adapters, ReAct, compaction, tool chains, JavaScript execution, formats,
sections, termination, schema handling, search, native embedding, tracing, and test support.
The test inventory covered all 112 existing test files. A review and passing suites cannot prove
that all possible schedules or resource failures are safe.

| Check | Result |
| --- | --- |
| `go test ./... -count=1 -timeout 300s` | Passed |
| `CGO_ENABLED=0 go test ./... -count=1 -timeout 300s` | Passed |
| `go test -race ./... -count=1 -timeout 300s` | Passed |
| `go vet ./...` | Passed |
| Portable statement coverage on the reviewed revision | Passed |
| Original review probes at `f3b798a` | Reproduced the historical failures below |
| Original probes with all fixes and `-race` | Passed |

The native runtime and model files were available. Live provider and integration checks gated by
credentials or smoke-test settings were not enabled. The portable suite initially could not open
local HTTP test listeners in the sandbox; it passed when those listeners were allowed. The race
suite also ran with local listener access. The final race suite includes shared-agent and
isolated native-close regression tests. XML fuzzing completed 448,584 generated cases in 10 seconds
without a panic.

## Original reliability findings

### R1 P1 Native embedder close can free an active tokenizer

Source: [search/embedder_onnx.go](../../search/embedder_onnx.go), lines 175–198.

`Close` frees the tokenizer without waiting for `TokenCount` or embedding calls. Those calls use
the same native pointer without a lock. The declared mutex is unused. This violates the concurrent
use contract in [search/embedder.go](../../search/embedder.go).

`TestReviewEmbedderCloseDuringTokenCount` produced a Go race report between tokenizer encoding and
close, a Rust panic, and `SIGSEGV` in the native encoding call. This can stop the whole process.
The test used the small checked-in ONNX fixture and four token-count workers during shutdown.

Make close wait for active native users. Prevent new calls from using freed state, and define the
behavior after close. Preserve concurrent reads where the native library permits them. Test close
during token counting, embedding, and repeated close in an isolated process.

### R2 P1 First concurrent runs of one ReAct agent race on its format

Sources: [agents/react/checkpoint.go](../../agents/react/checkpoint.go), lines 74–78;
[agents/react/agent.go](../../agents/react/agent.go), lines 231–235;
[format/xml.go](../../format/xml.go), lines 127–134;
[format/markdown.go](../../format/markdown.go), lines 81–88.

Execution configuration and loop execution register sections on the shared format. Registration
changes a map and slice without synchronization. Other runs can register or describe sections at
the same time, even when the caller completed all configuration before starting the runs.

`TestReviewSharedAgent` starts eight separate execution contexts through one configured runner.
The race detector reported conflicting accesses for both XML and Markdown. These accesses can
also cause map panics or incorrect section registration.

Prepare an immutable section contract before concurrent use, or use separate format state for
each run. Do not serialize whole model calls to protect this short configuration step. Add a
shared-instance test to the normal race suite.

### R3 P1 Concurrent history changes can panic in ExpireRecords

Source: [context_history.go](../../context_history.go), lines 282–299.

`ExpireRecords` reads the draft and scratchpad in separate snapshots. An append between the reads
can make the scratchpad longer than `draft.Active`. The loop then indexes outside `draft.Active`.
Changes that keep the same length can instead pair a record with the wrong entry ID. Revision
checks occur after this pairing and cannot prevent the panic.

`TestReviewExpireRecordsDuringAppend` used only public `AppendRecord` and `ExpireRecords` calls.
It reproduced an index-out-of-range panic while both operations ran concurrently.

Read entries, active IDs, and their revision in one snapshot, then apply the update with that
revision. Test concurrent append, replacement, expiration, and reorder operations. A valid
revision conflict may return an error; it must not cause a panic or expire the wrong entry.

### R4 P1 A model error chunk can finish execution before model completion

Sources: [agents/react/agent.go](../../agents/react/agent.go), lines 1004–1006;
[model_call.go](../../model_call.go), lines 116–135.

ReAct returns immediately on `chunk.Err`. It does not close the managed stream and wait for
`Response`, as its repetition and size-limit paths do. The managed producer sends that chunk
before it publishes and accepts the final model event. The executor can thus commit a terminal
checkpoint while model completion is still running. A later save cannot enter a terminal run.
Final model evidence or usage can be lost, and a provider that is still active is not stopped by
this return path.

`TestReviewModelErrorWaitsForCompletion` blocks the after-model hook. The executor returns the
provider error while that hook is still blocked. This confirms the completion order failure
without relying on provider latency.

On every early stream exit, close the stream and wait for producer completion before finalizing
the run. Preserve the provider error and report any recording error. Test final event acceptance,
usage, cancellation, and checkpoint order for an error chunk followed by delayed completion.

### R5 P2 Concurrent fused-index updates can leave different documents

Source: [search/fused_index.go](../../search/fused_index.go), lines 55–88.

Each update walks the subindexes without synchronization across the whole operation. Two
successful replacements of the same ID can leave one subindex with the old document and another
with the new one. Thread safety inside each subindex does not make the combined operation safe.
The documented partial-error case does not cover this case: both calls return success.

`TestReviewFusedIndexConcurrentReplacement` uses two synchronized subindexes and channels to
control the order. It leaves `old` in one subindex and `new` in the other after both calls finish.

Serialize conflicting updates across subindexes. Make search observe a consistent generation if
the combined index retains its atomic replacement contract. Test Add/Add, Add/Remove, Swap/Add,
and reads during replacement. Keep the documented recovery behavior for partial errors.

### R6 P2 Completed execution contexts remain active

Sources: [executor/executor.go](../../executor/executor.go), lines 157 and 243–257;
[context.go](../../context.go), lines 208–215, 1489–1515, and 1550–1560.

Normal execution exit closes subscriptions but does not cancel its Go context. `SetTermination`
only stores result fields. `CompleteChild` also leaves its child context active. This conflicts
with the documented cancellation on execution termination.

`TestReviewExecutionReleasesContext` confirmed that both success and loop failure leave
`Context().Err()` nil. Work waiting for that context to end can survive the execution. Under a
long-lived cancellable parent, its uncanceled child contexts remain registered until the parent
ends. This is a lifetime leak; it does not by itself prove that all execution history is retained.

Release execution and child resources after final evidence is accepted, on every exit path.
Keep resource cleanup separate from explicit run cancellation so successful results and recovery
semantics remain correct. Test repeated success, failure, setup failure, and child completion.

### R7 P2 Trace unsubscribe cannot discard a subscription after close

Sources: [trace/subscription.go](../../trace/subscription.go), lines 67–74;
[trace/sequencer.go](../../trace/sequencer.go), lines 141–147 and 308–325.

`Sequencer.Close` removes subscribers from its map and asks their queues to drain. A later
unsubscribe looks only in that map, so it cannot discard the queue. The subscriber also uses
one `sync.Once` for graceful close and discard; retaining the pointer alone will not fix it.
If the consumer stops reading, the drain worker stays blocked with queued events.

`TestReviewTraceUnsubscribeAfterClose` queued 17 events, closed the sequencer, and unsubscribed.
One buffer worker remained and the channel still returned an event. The probe drains the channel
after the checks so that the probe itself does not leave the worker behind.

Let unsubscribe retain ownership of its subscription and permit discard after graceful close.
Test both operation orders with unread data, empty queues, and concurrent close/unsubscribe.

### R8 P2 The XML parser panics when no sections are registered

Source: [format/xml.go](../../format/xml.go), lines 228–232.

The default parsing path calls `regexp.MustCompile` with a backreference, `\1`, which Go regular
expressions do not support. `format.NewXML().Parse(nil, "<answer>done</answer>")` panics.
`TestReviewXMLWithoutRegisteredSections` reproduced it. This is a deterministic fault exposed by
the test review, rather than a scheduling fault.

Use a supported tag-pairing implementation. Add no-section, malformed-input, and nested-tag cases,
then fuzz the parser to check that model output cannot cause a panic.

### R9 P2 Search and embedding can ignore cancellation

Sources: [search/bleve_index.go](../../search/bleve_index.go), lines 138–159;
[search/embedder_onnx.go](../../search/embedder_onnx.go), lines 188–200 and 279–290.

Bleve search calls `Search` instead of the available `SearchInContext`. The native embedder selects
between its semaphore and cancellation; when both are ready, it can take the semaphore. It then
does not check cancellation before tokenization or inference, and the inference call does not
receive cancellation.

`TestReviewBleveSearchChecksCancellation` got a successful response with an already canceled
context. In `TestReviewEmbedderChecksCanceledContext`, 13 of 32 already canceled calls still ran
inference successfully. The exact count depends on scheduling. Canceled work can therefore keep
CPU and native resources busy after its caller stops waiting.

Pass cancellation into Bleve. Check it before and after semaphore acquisition, and use native run
termination where supported. Test cancellation before work, while waiting for a permit, and during
work. Also check long FlatIndex scans and regex search, which currently do not poll cancellation.

## Original memory and performance findings

### R10 P2 Drained queues retain their previous payloads

Sources: [internal/buffer/unbounded.go](../../internal/buffer/unbounded.go), lines 96–100;
[stream.go](../../stream.go), lines 134–139.

Both queues remove their first item by reslicing without clearing the old slot. An empty slice
can still retain the backing array and references in that array while its owner remains reachable.

`TestReviewDrainedBufferReleasesValues` sent and drained 32 one-MiB byte slices, closed the buffer,
and forced garbage collection while keeping the buffer alive. About 33.5 MB remained allocated.
The test expected retained heap to fall below 2 MiB. The direct measurement covers the generic
buffer; the stream queue has the same reference-retention pattern.

Clear each removed slot and release or reset empty backing storage. Check both partial and full
drains. Keep queue capacity reuse bounded so a temporary burst does not set permanent memory use.

### R11 P2 Every stream chunk copies the growing accepted history

Sources: [context_recording.go](../../context_recording.go), lines 133–146;
[run_replay.go](../../run_replay.go), lines 58–59 and 101–105.

Every chunk flushes a batch. Applying that batch copies the complete activity slice and the map
of all previous batches. Total copying therefore grows quadratically with chunk count. This
cost applies even when the run has no external recorder or observer.

`BenchmarkReviewEmitChunk` records one-character chunks in a new context for each operation.
Three runs per size, with one operation per run, produced these ranges:

| Chunks | Elapsed time | Total Go allocation per operation |
| --- | --- | --- |
| 1,000 | 119–135 ms | 75.8–75.9 MB |
| 2,000 | 288–327 ms | 229.5 MB |
| 4,000 | 749–840 ms | 766.5–768.8 MB |
| 8,000 | 2.16–2.28 s | 2.77–2.78 GB |

These are total allocated bytes, not peak or retained heap. The host used Go 1.24.10 on Linux amd64,
an AMD Ryzen 9 7900, and `GOMAXPROCS=24`. No provider or network time is included.

Use append-only storage or shared immutable pages internally. Copy public snapshots when required
by ownership rules. Preserve acceptance before delivery and retry behavior; removing those checks
would trade reliability for speed. Keep a scaling benchmark to detect renewed quadratic growth.

### R12 P2 Native embedding rebuilds its session and ignores NumThreads

Source: [search/embedder_onnx.go](../../search/embedder_onnx.go), lines 63–65 and 261–290.

Every embedding call constructs session options, loads an ONNX session, runs it once, and destroys
it. Constructor warmup also destroys its session. Repeated calls therefore repeat session setup
and cannot reuse the warmed session. `NumThreads` gets a default value but is never stored or
applied to session options, so it does not control the requested CPU use.

`BenchmarkReviewOnnx` used the same small checked-in quantized graph in both cases. Gent embedding
took about 1.009 ms per call; `Run` on an already created session took 3.919 microseconds. This is
evidence of setup overhead on a small graph, not a production-model speedup estimate. The reused
case excludes tokenization and other Gent work. Go allocation counters also exclude native memory.

Keep a bounded set of reusable sessions, apply the thread settings, and coordinate their lifetime
with close. Preserve the quantization precision option. Benchmark a real model after the change,
including repeated calls, concurrent calls, native memory, and total process thread count.

## Original test review

The existing suite has 676 Test-prefixed functions, including package setup, across 112 test files.
It has no benchmark functions or fuzz targets, and no `t.Parallel` calls. Some tests do start
goroutines; serial top-level tests are not themselves a defect. The key gap is that the concurrent
operations and lifecycle boundaries above were not exercised together.

History, checkpoint, ownership, replay, and stats tests contain useful exact assertions. Preserve
those checks when fixing these issues. Portable statement coverage was 83.1% for the root package,
85.5% for ReAct, 77.1% for executor, 87.9% for format, 88.2% for search, and 81.9% for trace.
Search coverage excludes native code in this configuration. High statement coverage did not expose
the failing interleavings or retained references.

The original review called for these changes in priority order; the fixes above implement them:

1. Add the confirmed lifecycle and shared-instance failures to normal tests with their fixes. Use
   channels to control interleavings where possible. Assert complete results, errors, accepted
   events, and checkpoint state. Use isolated processes for tests that can crash native code.
2. Add repeated-run tests that verify worker exit, cancellation, queue release, and native close.
   Measure retained heap after collection with a justified tolerance. A passing race detector
   cannot establish leak freedom or correct operation ordering.
3. Make native test prerequisites explicit. [search/testmain_test.go](../../search/testmain_test.go)
   downloads all missing registered models through
   [search/testmodel_test.go](../../search/testmodel_test.go) before `m.Run`, outside the Go test
   timeout. [common/download.go](../../common/download.go) uses the default HTTP client without a
   request deadline. A small targeted test can thus depend on large downloads with no time bound.
   Keep normal checks offline with small fixtures; move large model checks to explicit integration
   setup with deadlines and verified cached files.
4. Separate missing prerequisites from actual native failures. `testEmbedderForConfig` skips on
   every constructor error, including model or implementation errors. Once prerequisites are
   present, those errors must fail the test. Report explicit prerequisite skips in native CI.
5. Add streaming and embedding benchmarks with increasing input sizes. Track total allocation,
   retained Go heap, native memory, and elapsed time separately. Add parser fuzz targets and
   cancellation cases for expensive work.
6. Replace partial string assertions where the complete output is known. For example,
   [section/json_test.go](../../section/json_test.go) and
   [section/yaml_test.go](../../section/yaml_test.go) check prompt fragments. These can miss
   duplicate or malformed output and conflict with AGENTS.md. The inventory found 196 Contains
   or NotContains assertion calls; many check collection membership and are not string violations.

## Repeat the checks

Run commands from the repository root. Normal regressions run without build tags. The original
review probes assert the required behavior and fail on the reviewed revision. The fused-index probe
now expects serialized updates. Run the heap probes alone for useful allocation measurements.

```sh
CGO_ENABLED=0 go test -tags gent_review ./docs/reliability-review/probes \
  -run 'TestReview(Model|Execution|Trace|XML|Bleve|Fused|Runtime|Expire)' \
  -count=1 -timeout 300s

go test -race -tags gent_review ./docs/reliability-review/probes \
  -run '^TestReviewSharedAgent$' -count=1 -timeout 300s

go test -tags gent_review ./docs/reliability-review/probes \
  -run '^TestReviewEmbedderChecksCanceledContext$' -count=1 -timeout 300s

CGO_ENABLED=0 go test -tags gent_review ./docs/reliability-review/probes \
  -run '^TestReviewDrainedBufferReleasesValues$' -count=1 -timeout 300s

CGO_ENABLED=0 go test -tags gent_review ./docs/reliability-review/probes \
  -run '^$' -bench '^BenchmarkReviewEmitChunk$' -benchtime=1x -count=3 -timeout 300s

go test -tags gent_review ./docs/reliability-review/probes \
  -run '^$' -bench '^BenchmarkReviewOnnx$' -benchtime=20x -count=1 -timeout 300s
```

The native checks require the installed ONNX and tokenizer libraries. They use the checked-in
quantized model fixture and do not download models. The close probe has an additional build tag
because it caused a native process crash. Run it in a separate process with core dumps disabled:

```sh
ulimit -c 0
go test -race -tags 'gent_review gent_review_native_crash' ./docs/reliability-review/probes \
  -run '^TestReviewEmbedderCloseDuringTokenCount$' -count=1 -timeout 300s
```

Run all three full suites from the verification table after changes. Targeted checks do not replace
them. The native close regression remains isolated so a future native fault produces a test failure.

Normal benchmark and fuzz commands:

```sh
CGO_ENABLED=0 go test . -run '^$' -bench '^BenchmarkReliabilityEmitChunk$' \
  -benchtime=1x -count=3 -timeout 300s
go test ./search -run '^$' -bench '^BenchmarkOnnxEmbedder' \
  -benchtime=20x -count=3 -timeout 300s
CGO_ENABLED=0 go test ./format -run '^$' -fuzz '^FuzzXMLParsing$' \
  -fuzztime=10s -parallel=4 -timeout 300s
```

On Linux with glibc and the cached model, repeat the process resource check with:

```sh
go run -tags gent_review_native_allocator ./docs/reliability-review/resources
```

This diagnostic reads native allocator counters and uses `malloc_trim` after close. The normal
library does not trim the process allocator. Compare live allocation with RSS before interpreting
a retained page as a leak.
