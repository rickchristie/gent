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
go test ./models -run '^TestLive(Providers|Images)$' -count=1 -timeout 300s
```

The file is shell input, so load only a file you trust. `-count=1` prevents cached test success.
Missing providers are skipped during normal runs; partial configuration fails. For a complete
matrix, use:

```sh
GENT_LIVE_REQUIRE_ALL=1 go test ./models \
  -run '^TestLive(Providers|Images)$' -count=1 -timeout 300s
```

This mode fails when any of the ten providers or four image models is not configured. It cannot
report a complete pass by skipping providers. To check only the text, tools, and JSON matrix,
select `'^TestLiveProviders$'` instead. To run one provider, select
`'^TestLiveProviders$/^xAI$'`, for example.

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

1. `TextStream` consumes a real stream and checks an exact answer with `TextOutput`.
2. `NativeTools` requires a real native call and a following answer that uses its result. The
   result has a random value that is absent from the original prompt, so a guessed answer fails.
3. `JsonOutput` returns a Go struct, rejects the first value through custom validation, and checks
   the corrected value and native schema decision.
4. `SchemaOutput` runs the same correction using an explicit schema.

`TestLiveImages` tests `ImageOutput` with OpenAI, xAI, Gemini, and OpenRouter. It requires one JPEG.
Each image must fit in 8 MiB and 2048 pixels on either axis. Accepted bytes must match the saved
final content. Each model call has a 2048-token output limit. Each agent has a four-iteration limit.
Each provider's text, tools, and JSON group has a two-minute time limit. Each image case has the
same time limit. Providers run in parallel; one provider's text cases run in order.

With no corrections beyond the required JSON rejections, the core suite uses seven requests per
provider. Image tests make additional generation requests and can cost more than the text cases.
Old OpenAI/xAI program tests, xAI stream tests, and scenario tests also run when their keys are set
and their names match the test selection. The commands above select only the new matrix.

An HTTP error, invalid key, low balance, incompatible model, failed tool use, validation failure,
or timeout fails the test. There is no mock fallback or retry that conceals a service error. Keep
live results separate from fixture results; unconfigured services remain unverified.

## Recorded live check

On 2026-10-30, all four xAI core cases passed with `grok-4.3`, and image generation passed with
`grok-4.7`. Both checks also passed with the race detector. The image check found that xAI rejects
OpenAI's image-specific `tool_choice` object.
The xAI adapter now uses `required` when image generation is the only tool, and its request fixture
checks that mapping. The other providers had no configured live-test credentials or server settings.
Their live cases are present but remain unverified until those settings are supplied.

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
