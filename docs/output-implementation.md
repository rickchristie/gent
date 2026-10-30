# Output migration implementation record

Date: 2026-10-30, Asia/Jakarta. Scope: typed outputs, native model adapters, and task ownership.
Implementation and verification are complete. This record separates fixtures from live API checks.

## Architecture and resolved details

The root package owns `Output[T]`, `OutputResult[T]`, `OutputCustomValidator[T]`, `Task`, and the
generic model content types. The `output` package supplies the four agreed built-ins. `Loop[T]`,
ReAct's `Agent[T]`, and `executor.Runner[T]` carry `T` through execution and terminal restoration.
`Execute`, `Continue`, `Run`, and `Resume` return `(T, *gent.ExecutionResult, error)`.

`Configure` supplies output controls before each request. It cannot change messages. Request hooks
receive owned messages, and the managed call checks that they retain required output controls.
The adapters select native controls only when their supported schema profile preserves the schema.
Local validation always runs before custom validation. Fallback never changes optional fields into
required nullable fields or removes local constraints.

`Process` returns an explicit acceptance or steering result. It has no separate `Format` method.
Operation errors stop execution. The typed value and delivery content are saved before accepted
content is published. Terminal restoration decodes those saved values without another model call,
download, `Process` call, or custom validation callback. Output type, codec version, schema, and
declared application configuration participate in restore compatibility checks.

The caller supplies `Task.Messages []*ModelMessage`. `NewTask` creates one user message. The
documented standard task has a system message and a user message. No output instructions are
inserted automatically. `BeforeModelCallSubscriber` is the retained dynamic request hook.
`ContentPart` remains an interface, and the content slice remains `[]ContentPart`. Built-in value
parts and concrete pointer parts work through provider and codec boundaries.

The task is frozen separately from compactable history. A rejected candidate and its steering form
one required history group through the next main model request, including custom compaction and
resume. The original native completion and continuation remain evidence. Incomplete responses keep
their evidence but their partial call groups leave the next request; recovery guidance remains.
Normal run limits govern retries. Native refusal is a separate saved terminal outcome.

The compact byte codec stores one base64 payload with type information. Custom codecs retain
precedence. Direct owned clones avoid encoding and decoding ordinary values when that copy has the
same semantics as the portable codec. Current saved runs use `RunSchemaVersion = 6`; stream chunks
use codec version 3. There is no compatibility alias, old-checkpoint migration, public buffer type,
or new blob store.

The remaining `TextFormat` interface only renders named tool-result sections. It cannot register,
locate, or parse an answer. `Termination`, `AnswerValidator`, `TextSection`, old prompt setters and
hooks, their parser statistics, LangChainGo adapters, and GitHub Models support are removed.
The unused `AgentLoopResult.NextPrompt` field is also removed. Structured history is the source
for later requests, including binary feedback. `AgentLoopResult` is a non-generic iteration result;
its saved encoded value lets the typed runner restore `T`.

See [Output usage](output.md), [adapter contracts](models.md), and [history ownership](history.md).

## Behavior coverage

Tests use complete values, actual codecs and checkpoints, and HTTP wire fixtures. Provider request
tests compare full request bodies rather than assuming that compatible endpoints have the same
behavior.

| Behavior | Main tests |
| --- | --- |
| Completion, refusal, incomplete output | [States][states], [refusal][refusal] |
| Typed values, execution, restore | [Execution][typed], [JSON][json] |
| Binary content and offline restore | [Content][content], [images][images] |
| Ten native adapters | [Requests][requests], [capabilities][capabilities] |
| Task roles and ownership | [Task][content], [hooks][hooks], [history][history] |
| Rejection, correction, operation errors | [Correction][correction], [limits][limits] |
| Concurrent use, cancellation, cleanup | [Streams][lifetime], [discovery][routing] |
| Removed APIs and migrated callers | Compilation, symbol audit, browser source comparisons |
| Custom compaction retains images | [Image policies][compaction] |
| Built-in types and image bounds | [Text][text], [JSON][json], [images][image-checks] |
| JSON names, omissions, roots, nulls | [Inference][inference], [contracts][contracts] |
| Codecs and ownership | [Bytes][bytes], [clones][clones], [digests][digests] |

Additional provider checks cover [streaming and continuation][streams], [Responses phases][phases],
[media cleanup][downloads], and [schema limits][schema-limits]. [Large-file benchmarks][media-bench]
measure ordinary message copies separately from owned snapshots and saved restoration.

The all-provider tables cover OpenAI, Anthropic, Gemini, xAI, DeepSeek, vLLM, Ollama, OpenRouter,
llama.cpp, and SGLang. Image generation fixtures cover the adapters that expose image output.
Unsupported image output is checked as a capability error for the remaining adapters.

Additional checks cover native schema size limits, Claude's combined strict-tool/schema limits,
Claude's unsupported `allOf`/`$ref` combination, Gemini continuation with an explicit token ceiling,
and an OpenRouter discovery owner canceled while another request is waiting.

## Recording timing and producer cleanup

The original failure was reproduced with the unchanged 1,000-chunk workload and five-second
deadline. The failing race profile lasted 5.92 seconds. It showed substantial race instrumentation
and repeated JSON work: 28.12% of samples were in `racecall`, and 35.44% were in external runtime
code. JSON compaction and validation were visible costs. This established recording overhead under
race instrumentation; it did not establish a data race or one common cause for every earlier
takeover/expiry observation.

The changes reduce repeated serialization in owned snapshots, digest preparation, and stream
records. Type identity and evidence remain part of the digest. The byte codec avoids reflection
and metadata for every byte. The test now cancels its context and joins its producer during cleanup,
including when the deadline fails, so a failed test does not leave work running into later tests.

The test still records all 1,000 events, uses the five-second deadline, checks the saved cursor,
and calls `ValidateBundle`. It is not skipped, reduced, or given a longer deadline. The affected
chunk, takeover, and expiry checks also run together repeatedly under the race detector.

Before the optimization, the 1,000-chunk benchmark used approximately 306.6 ms, 63.2 MB, and 520,932
allocations per operation. The final run used 109.0 ms, 23.2 MB, and 220,037 allocations. These are
local measurements on an AMD Ryzen 7 5800HS with Go 1.24.10, not service-level guarantees.

The following commands reproduce the performance checks. The stream test uses three complete
1,000-chunk operations. The separate media run gives descriptor copies enough iterations to measure
them independently of timer overhead. Its byte fixture is a 4 MiB opaque payload; it measures
ownership and serialization, not PDF format validation.

```sh
go test . -run '^$' -bench 'Benchmark(RecordAnswerStream|CodecBinaryRoundTrip)$' \
  -benchtime=3x -benchmem
go test . -run '^$' -bench 'BenchmarkLargeMediaOwnership$' -benchtime=200ms -benchmem
```

| Operation | Time per operation | Allocated bytes | Allocations |
| --- | ---: | ---: | ---: |
| Record 1,000 provisional chunks | 109.0 ms | 23,165,040 | 220,037 |
| Encode/decode 64 KiB of binary data | 4.87 ms | 860,034 | 79 |
| Copy a message value with a 4 MiB attachment | 1.41 ns | 0 | 0 |
| Copy its message pointer | 1.04 ns | 0 | 0 |
| Decode an owned 4 MiB task snapshot | 467 ms | 48,987,992 | 250 |
| Save and restore the 4 MiB task | 832 ms | 116,159,656 | 405 |

The saved 4 MiB task occupies 5,593,966 encoded bytes, close to base64 expansion plus its envelopes.
Snapshot and serialization cost remains material. Pointer choice does not reduce that work. The
current task path decodes the frozen saved representation on each access, including custom codec
semantics. An optimization must preserve those semantics and independent mutable snapshots; it
must not expose the submitted task or share its byte slices across requests.

## Verification results

All required checks passed. The final root race run completed its root package in 65.372 seconds.
The recording and two related recovery checks passed five consecutive runs in 19.252 seconds.

| Check | Result |
| --- | --- |
| Root `go test ./... -count=1 -timeout 300s` | Pass |
| Root `CGO_ENABLED=0 go test ./... -count=1 -timeout 300s` | Pass |
| Root `go test -race ./... -count=1 -timeout 300s` | Pass |
| `go vet ./...` | Pass |
| All three full suites in `docs/quickstart/server` | Pass |
| Repeated chunk, takeover, and expiry race checks, `-count=5` | Pass |
| Client `npm test` and `npm run build` | Pass |
| Interface guide and five interactive lessons, Chromium | Pass |
| Quickstart and policy guide, Chromium | Pass |
| Real streaming server/client refresh and restart recovery, Chromium | Pass |
| Image/PDF snapshot display, mobile layout, browser errors | Pass |
| README example compilation, generated source comparisons, local documentation links | Pass |
| `git diff --check` and changed Go source line-length audit | Pass |

The commands used `GOCACHE=/tmp/gent-go-cache`. Browser checks used `/opt/google/chrome/chrome`.
The root suites include the lesson examples. The server has a separate Go module, so it was tested
separately. Browser checks compare generated snippets and downloads with disk source and fail on
console errors. Desktop and mobile screenshots were also inspected.

The repeated reliability command was:

```sh
gent_timing_tests='Test(ExecutionContext_EmitChunkRecordsWithoutListeners'
gent_timing_tests="$gent_timing_tests|NativeTakeoverStopsOldWorker"
gent_timing_tests="$gent_timing_tests|ReliabilityExpireRecordsDuringAppend)$"
go test -race . -run "$gent_timing_tests" -count=5 -timeout 300s
```

Local logs are in `/tmp/gent-output-final-{native,portable,race}.log`,
`/tmp/gent-output-t1-repeat.log`, `/tmp/gent-output-vet.log`, and
`/tmp/gent-server-{native,portable,race}.log`. Benchmark logs are
`/tmp/gent-output-final-bench.log` and `/tmp/gent-output-media-bench.log`.
Client logs are `/tmp/gent-client-{tests,build}.log`. The five browser logs are
`/tmp/gent-{interface,lessons,quick,policy,streaming}-browser-test.log`.
These temporary files are local verification evidence; the tests and this record remain in Git.

## Declared limits

Live API checks are opt-in and were not run for this migration. The adapter tests establish the
implemented request and response contracts with fixtures; they do not establish live account or
deployment capability. Model choice, server version, tool parser, grammar backend, and account
features can impose further limits. See [native adapters](models.md) for those boundaries.

`ImageOutput` initially decodes PNG and JPEG. Its default limit is 67,108,864 decoded pixels per
image; callers can set tighter byte and dimension limits. Generated image references have a 32 MiB
download bound, and provider JSON bodies and individual stream events have a 64 MiB bound.
Unsupported modalities and input types return errors. No input conversion or image resizing occurs.

Bytes remain owned and saved inline. This retains export and offline restoration and still costs
base64 space and ownership copies. The large-file benchmark measures that cost separately from a
message value copy or pointer assignment. A future external content store would need a separate
atomic-save, retention, and restoration contract.

[states]: ../models/provider_completion_test.go
[refusal]: ../execution_refusal_test.go
[typed]: ../execution_output_test.go
[json]: ../output/json_test.go
[content]: ../task_content_test.go
[images]: ../models/provider_image_test.go
[requests]: ../models/provider_request_test.go
[capabilities]: ../models/provider_capability_test.go
[hooks]: ../model_call_test.go
[history]: ../agents/react/native_test.go
[correction]: ../models/provider_output_test.go
[limits]: ../agents/react/agent_executor_limits_test.go
[lifetime]: ../models/provider_lifetime_test.go
[routing]: ../models/openrouter_test.go
[compaction]: ../executor/image_compaction_test.go
[text]: ../output/text_test.go
[image-checks]: ../output/image_test.go
[inference]: ../schema/infer_test.go
[contracts]: ../output/contract_test.go
[bytes]: ../history_codec_bytes_test.go
[clones]: ../history_codec_clone_test.go
[digests]: ../run_digest_test.go
[streams]: ../models/provider_stream_test.go
[phases]: ../models/responses_phases_test.go
[downloads]: ../models/output_media_test.go
[schema-limits]: ../models/native_schema_limits_test.go
[media-bench]: ../media_bench_test.go
