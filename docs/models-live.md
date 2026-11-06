# Live provider tests

Every model adapter has tests that send requests to the real provider or a running local server.
They use no HTTP fixtures. A configured key enables that provider in normal `go test` runs, so
these runs can incur API charges. Keep the fixture tests as well: they cover failure paths that a
live model cannot produce reliably on demand.

Copy the required entries from [`.env.example`](../.env.example) into your existing `.env` file.
Tests read exported variables. They do not load `.env` automatically. In Bash:

```sh
set -a
source .env
set +a
go test ./models -run '^Test(Live|NativeLive$)' -count=1 -timeout 300s
```

The file is shell input, so load only a file you trust. `-count=1` prevents cached test success.
Missing providers are skipped during normal runs; partial configuration fails. For a complete
matrix, use:

```sh
GENT_LIVE_REQUIRE_ALL=1 go test ./models \
  -run '^Test(Live|NativeLive$)' -count=1 -timeout 300s
```

This mode fails when any of the ten providers or four image models is not configured. It cannot
report a complete pass by skipping providers. To check only the text, tools, and JSON matrix,
select `'^TestLiveProviders$'` instead. To run one provider, select
`'^TestLiveProviders$/^xAI$'`, for example.

To test only configured cloud providers while a local server is unavailable, select their names.
This does not change `.env` or turn partial configuration into a pass:

```sh
go test ./models -run '^Test(Live|NativeLive$)/^(OpenAI|Anthropic|Gemini|xAI)$' \
  -count=1 -timeout 300s
```

## Accounts and billing

These are separate API accounts. Add API credit or enable API billing in each console. An
OpenRouter key tests OpenRouter; it does not replace the keys for direct provider adapter tests.

| Provider | Key variable | Create a key | Fund API use |
| --- | --- | --- | --- |
| OpenAI | `GENT_TEST_OPENAI_KEY` | [API keys][openai-keys] | [Billing][openai-billing] |
| Anthropic | `GENT_TEST_ANTHROPIC_KEY` | [API keys][claude-keys] | [Buy credits][claude-billing] |
| Gemini | `GENT_TEST_GEMINI_KEY` | [AI Studio][gemini-keys] | [Set up billing][gemini-billing] |
| xAI | `GENT_TEST_XAI_KEY` | [Console][xai-console] | [Billing instructions][xai-billing] |
| DeepSeek | `GENT_TEST_DEEPSEEK_KEY` | [Platform][deepseek] | Top up in the same console |
| OpenRouter | `GENT_TEST_OPENROUTER_KEY` | [API keys][router-keys] | [Credits][router-billing] |

In the xAI console, select Billing, then API spend management. In AI Studio, select Set up billing
for the project that owns the API key and link an active Google Cloud billing account. Complete
the Prepay setup and add credits when the console requires it. See the linked billing pages for
account-specific payment terms.

## Models and endpoints

For each cloud provider, `GENT_TEST_<PROVIDER>_URL` is an optional API base URL override. Leave it
empty to test the official service. `GENT_TEST_<PROVIDER>_MODEL` selects the text model.

| Provider prefix | Text model when `_MODEL` is empty | Image model variable |
| --- | --- | --- |
| `OPENAI` | `gpt-5.4-mini` | `GENT_TEST_OPENAI_IMAGE_MODEL` |
| `ANTHROPIC` | `claude-sonnet-4-6` | Not supported |
| `GEMINI` | `gemini-2.5-flash` | `GENT_TEST_GEMINI_IMAGE_MODEL` |
| `XAI` | `grok-4.3` | `GENT_TEST_XAI_IMAGE_MODEL` |
| `DEEPSEEK` | `deepseek-flash` | Not supported |
| `OPENROUTER` | Required; no default | `GENT_TEST_OPENROUTER_IMAGE_MODEL` |

These are test defaults, not a claim that every account has access to each model. Use an available
model that supports tools and the provider's native JSON controls. OpenRouter must have an eligible
schema-capable route. DeepSeek's JSON object mode is tested with local schema validation.

Image variables select the model passed to the adapter. OpenAI and xAI need a Responses model that
can invoke the native `image_generation` tool, not a model for a separate image endpoint. Gemini and
OpenRouter need models with native image output. Images run only when `_IMAGE_MODEL` is set, unless
require-all mode is active, in which case each image model is required. The test requests JPEG.

## Local runtime servers

These adapters test the real runtime. They have no provider credit balance. You supply the compute,
model files, and server. Configure URL and MODEL for each runtime; KEY is optional authentication
that you set on the server. All three names start with `GENT_TEST_`.

| Runtime | URL variable | Model variable | Optional key variable |
| --- | --- | --- | --- |
| [vLLM][vllm] | `VLLM_URL` | `VLLM_MODEL` | `VLLM_KEY` |
| [Ollama][ollama] | `OLLAMA_URL` | `OLLAMA_MODEL` | `OLLAMA_KEY` |
| [llama.cpp][llamacpp] | `LLAMACPP_URL` | `LLAMACPP_MODEL` | `LLAMACPP_KEY` |
| [SGLang][sglang] | `SGLANG_URL` | `SGLANG_MODEL` | `SGLANG_KEY` |

For vLLM, llama.cpp, and SGLang, include `/v1` in the base URL. For Ollama, supply the server root,
such as `http://localhost:11434`, without `/api` or `/v1`. Use the port your server actually listens
on. The old `GENT_TEST_OLLAMA_HOST` variable is not used.

Choose a tool-capable model, configure its tool parser and chat template, and enable a schema
backend compatible with the [adapter's schema profile](models.md). The Ollama adapter targets local
structured output; an Ollama cloud account does not replace this server test.

## What a pass establishes

`TestLiveProviders` runs four cases for each adapter:

1. `TextStream` requires the complete exact preview, checks every chunk error, forbids accepted
   `Content` in raw provider chunks, and checks the exact answer with `TextOutput` and token usage.
2. `NativeTools` requires a real native call and a following answer that uses its result. The
   result has a random value that is absent from the original prompt. A custom validator requires
   that exact value; correction steering never reveals it. Every follow-up request must retain
   the complete tool result, and the tool must run exactly once.
3. `JsonOutput` returns a Go struct, rejects the first value through custom validation, and checks
   the corrected value and native schema decision.
4. `SchemaOutput` runs the same correction using an explicit schema.

The other live tests check paths that a single successful answer does not cover:

- `TestLiveProviderHistory/AssistantHistory` reads application-made assistant text without native
  continuation.
- `TestLiveProviderHistory/RepeatedCompaction` completes two compactions with real summaries,
  preserves recent native tool groups, uses old and recent tool results, and restores the saved
  answer twice.
- `TestLiveProviderConcurrentCalls` uses one adapter for three concurrent conversations with
  distinct exact answers.
- `TestLiveProviderImageInput` reads owned PNG bytes with OpenAI, Anthropic, Gemini, and xAI.
- `TestLiveProviderToolJSON` uses default and explicit `auto` tool choice with `JsonOutput`. A
  random record is available only through the tool. The test rejects the first typed answer,
  requires the corrected value, and checks that the completed tool runs exactly once.
- `TestLiveProviderCancellation` requires five live chunks before cancellation, a cancellation
  completion cause, prompt producer cleanup, and closure of every HTTP body. An early answer or
  HTTP failure fails the check. The same check is used by the existing xAI streaming scenario.
- `TestLiveProviderUnfinishedResume` interrupts after a saved native tool result. A fresh adapter,
  agent, runner, and codec registry restore serialized state, make further real requests, reject
  and correct a typed output, and finish without repeating the completed tool.
- `TestNativeLive` runs two dependent calls in one program, with and without prior tool search,
  for every configured adapter.

The compaction task states that `prior_work_summary` is saved history and that its completed calls
must not be repeated. This is part of the caller's prompt contract. Without this instruction,
live Anthropic checks repeated a lookup that the summary already marked complete. A typed JSON
output contract enforces the final format. Native tool choice requires the two tool turns before
the answer where supported; Ollama has no tool-choice control. The test checks the exact two calls
and result; it does not increase the iteration limit to accept repeated work or premature answers.

Fixture tests cover the same compaction path for all ten adapters. They also check the complete
native encoding of assistant text and data, role-specific media steering, unused media on native
continuation, empty image text, and OpenAI image replay without server storage. Existing fixtures
cover truncated streams, HTTP errors, cancellation, cleanup, refusal, schema limits, and routing.
The unfinished-resume fixture matrix checks four accepted storage boundaries for every adapter:
saved model response, saved tool result, rejected output, and completed compaction. It compares
complete outgoing requests with an uninterrupted run, checks native IDs and signatures against
protocol fixtures, and verifies exact usage, tool calls, corrections, and final typed output.
Terminal restore remains a separate check. Negative tests also prove that the live stream and
cancellation checks reject invalid streams, early completion, and HTTP failures.

`TestLiveImages` tests `ImageOutput` with OpenAI, xAI, Gemini, and OpenRouter. It requires one JPEG,
rejects the first valid image through custom validation, and requires a corrected image. This
checks image history replay as well as initial generation.
Each image must fit in 8 MiB and 2048 pixels on either axis. Accepted bytes must match the saved
final content. Main calls have a 2048-token output limit; program calls use 3000. Summary calls use
the strategy's default request. Each main agent has a four-iteration limit; programs permit six.
Each provider's text, tools, and JSON group has a two-minute time limit. Each image case has the
same time limit. Providers run in parallel; one provider's text cases run in order.
Each history case has its own time limit. Concurrent and image-input calls permit one minute.

With no corrections beyond the required JSON rejections, the core suite uses seven requests per
provider. History, concurrent calls, image input, and program tests add requests. Each compaction
case needs five model calls: three main calls and two summaries. Image tests need at least two
generation requests and can cost more than the text cases. The separate xAI stream tests and
scenario tests also run when their keys are set and their names match the test selection.

An HTTP error, invalid key, low balance, incompatible model, failed tool use, validation failure,
or timeout fails the test. There is no mock fallback or retry that conceals a service error. Keep
live results separate from fixture results; unconfigured services remain unverified.

## Recorded live checks

### 2026-11-06 review follow-up

An independent review then passed all 67 selected live cases in one run, all seven separate live
streaming checks, both longer agent scenarios, and the full native, CGO-disabled, and normal
parallel race suites. The Go source was unchanged. The earlier intermittent timeout and extra
OpenAI probe mismatch below remain follow-up work; a later pass does not establish their cause.

All 67 selected live cases have passed with the race detector. The expanded run passed 66 cases;
the remaining xAI image case passed in a separate retry after a provider capacity error:

| Provider | Passed cases | Remaining check |
| --- | --- | --- |
| OpenAI | 17 | None |
| Anthropic | 16 | None; image generation is unsupported |
| Gemini | 17 | None |
| xAI | 17 | None; image generation passed in the separate retry |

The model selections and command below are unchanged; the selection now includes the new tool/JSON,
cancellation, and unfinished-resume cases. The combined run did not pass in one attempt. The xAI
image test first failed before a candidate was available, with a provider capacity message. Its
separate retry used the same model and assertions. No service tier was changed. The seven separate
xAI streaming cases and both longer airline and ecommerce scenarios also passed with the race
detector and real API calls.

The review found that the earlier text-stream check could accept an empty or failed preview, and
the earlier cancellation check could accept an HTTP failure. Those earlier passes did not prove
these contracts. Negative fixture tests now reject those cases. The new live cancellation checks
passed for all four cloud providers with every HTTP body closed before completion.

The review also found a normal Gemini 2.5 path missing from the earlier matrix: automatic tools
combined with JSON output and later correction. The adapter now selects supported native controls
without changing automatic tool choice or the local output contract. Gemini then exposed Markdown
fences in answers on this path. Shared JSON instructions and correction text now forbid those
fences explicitly; validation still rejects them. Default and explicit `auto` cases passed for all
four cloud providers.

All four live unfinished-tool resume cases passed. The 40 fixture cases also passed with the race
detector across all ten adapters and all four saved boundaries. Their complete requests match
uninterrupted runs, including native signatures, tool IDs, correction history, and compaction.

The reviewer's original automatic-tool/JSON probe passed for Gemini, Anthropic, and xAI. OpenAI
returned a wrong second record once. That probe accepts every candidate after its first rejection;
its final assertion found the mismatch. Three captured repeats with the same record sent the exact
tool result and received the exact expected answers. The source of the first mismatch is unproven.
The permanent tool/JSON tests require the exact record in their validator on every accepted answer.

The full native and CGO-disabled suites passed over `./...`. The full race suite passed with
packages run in sequence:

```sh
go test ./... -count=1 -timeout 300s
CGO_ENABLED=0 go test ./... -count=1 -timeout 300s
go test -race -p 1 ./... -count=1 -timeout 300s
```

The default parallel race command failed twice at the existing five-second limit in
`TestExecutionContext_EmitChunkRecordsWithoutListeners`. The second failure had no live tests
running beside it. An isolated run passed and recorded 2.50 seconds of CPU samples during a
2.51-second profile, with most samples in race instrumentation and runtime code. The results
suggest that load affects the test, but do not prove the cause. This timing failure remains open.
The serial package run kept all tests, assertions, timeouts, and race checks enabled. No data race
was reported. These full-suite runs used no API credentials; the live runs are separate.

### 2026-11-06 initial check

All 51 selected live cases passed with the race detector and real API calls:

| Provider | Text model | Image output model | Passed cases |
| --- | --- | --- | --- |
| OpenAI | `gpt-5.4-mini` | `gpt-5.4-mini` | 13 |
| Anthropic | `claude-sonnet-4-6` | Not supported | 12 |
| Gemini | `gemini-2.5-flash` | `gemini-3.1-flash-image` | 13 |
| xAI | `grok-4.3` | `grok-4.7` | 13 |

Each provider passed the four core cases, assistant history, repeated compaction, three concurrent
calls, image input, and both program cases. OpenAI, Gemini, and xAI also passed image generation
and correction. Seven separate xAI streaming cases passed with the race detector, including slow
consumers, no listener, cancellation, concurrent calls, reasoning, and usage reporting.
The existing airline and ecommerce scenarios also passed with xAI `grok-4.7` and the race detector.
They combine policy search, program tools, and summarization in longer agent tasks.

This command reproduced the 51-case result after loading `.env` as shown above. Image selections
were command overrides; the existing `.env` file was not changed.

```sh
GENT_TEST_OPENAI_IMAGE_MODEL=gpt-5.4-mini \
GENT_TEST_GEMINI_IMAGE_MODEL=gemini-3.1-flash-image \
GENT_TEST_XAI_IMAGE_MODEL=grok-4.7 \
go test -race ./models -run '^Test(Live|NativeLive$)/^(OpenAI|Anthropic|Gemini|xAI)$' \
  -count=1 -timeout 300s
```

Run the separate streaming and scenario checks with the loaded xAI key:

```sh
go test -race ./models -run '^TestStreaming' -count=1 -timeout 300s
go test -race ./integrationtest/airline ./integrationtest/ecommerce -count=1 -timeout 300s
```

The full native, CGO-disabled, and race suites also passed over `./...`, each with `-count=1` and
`-timeout 300s`. Those runs used no API credentials and included fixtures for all ten adapters.
An earlier race run overlapped the live matrix and exceeded the existing five-second limit for
recording 1000 chunks. The unchanged test then passed three isolated runs at 2.09–2.11 seconds,
and the full race suite passed without the separate live job. CPU contention is a possible cause;
these results do not prove it. The timeout and assertions were not changed.

The live checks found and verified fixes for these provider failures:

- OpenAI rejected compaction summaries encoded as assistant `input_text` with HTTP 400. Assistant
  text now uses `output_text`, including synthetic history without native continuation.
- OpenAI emitted an empty text block beside an image. It no longer creates an empty text answer
  part that causes `ImageOutput` to reject the image. Nonempty text still needs validation.
- OpenAI rejected saved image-generation IDs with HTTP 404 when `store:false` was used. Replay
  now supplies the saved image bytes as input media and retains the original continuation.
- Gemini rejected forced function calls combined with answer JSON controls. Forced tool turns
  now omit those controls, and answer turns apply them again.

Ollama live checks were not run because the configured URL was reported unavailable and no model
was set. DeepSeek, OpenRouter, vLLM, llama.cpp, and SGLang had no live configuration. Their fixture
checks passed, but this record does not establish live support for those services or models.

### 2026-10-30

On 2026-10-30, all four xAI core cases passed with `grok-4.3`, and image generation passed with
`grok-4.7`. Both checks also passed with the race detector. The image check found that xAI rejects
OpenAI's image-specific `tool_choice` object.
The xAI adapter now uses `required` when image generation is the only tool, and its request fixture
checks that mapping. The other providers had no configured live-test credentials or server settings
for that earlier check.

[openai-keys]: https://platform.openai.com/api-keys
[openai-billing]: https://platform.openai.com/settings/organization/billing/overview
[claude-keys]: https://platform.claude.com/settings/keys
[claude-billing]: https://platform.claude.com/settings/billing
[gemini-keys]: https://aistudio.google.com/api-keys
[gemini-billing]: https://ai.google.dev/gemini-api/docs/billing
[xai-console]: https://console.x.ai/
[xai-billing]: https://docs.x.ai/console/billing
[deepseek]: https://platform.deepseek.com/
[router-keys]: https://openrouter.ai/keys
[router-billing]: https://openrouter.ai/settings/credits
[vllm]: https://docs.vllm.ai/en/stable/serving/online_serving/openai_compatible_server/
[ollama]: https://docs.ollama.com/api/authentication
[llamacpp]: https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md
[sglang]: https://docs.sglang.ai/basic_usage/openai_api.html
