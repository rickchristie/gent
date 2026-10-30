# Native model adapters

Gent owns all model requests, stream decoding, completion mapping, and continuation data. Select an
adapter for the serving API, not only the model weights. A model served by vLLM or SGLang uses that
runtime's adapter. Unsupported input content and options return errors. Gent does not convert an
unsupported file into text or silently remove an attachment.

```go
openai := models.NewOpenAI(openAIKey, modelName)
xai := models.NewXAI(xAIKey, modelName)
claude := models.NewAnthropic(&models.Config{APIKey: anthropicKey, Model: modelName})
gemini := models.NewGemini(&models.Config{APIKey: geminiKey, Model: modelName})
local := models.NewVLLM(&models.Config{
    BaseURL: "http://localhost:8000/v1",
    Model: localModelName,
})
```

`Config` also accepts an `HTTPClient`. Constructors for DeepSeek, Ollama, OpenRouter, llama.cpp,
and SGLang take the same config. Use `NewResponses(&ResponsesConfig{...})` to change an OpenAI or
xAI endpoint or client. Keep its `Provider` identity stable: saved continuation from another
provider is rejected. Credentials and clients are supplied again on restore, not saved in history.

## Protocols and output controls

| Constructor | Native endpoint | Answer schema control | Image output |
| --- | --- | --- | --- |
| `NewOpenAI` | `/v1/responses` | `text.format` with strict JSON schema | Native image tool |
| `NewXAI` | `/v1/responses` | `text.format` with strict JSON schema | Native image tool, JPEG |
| `NewAnthropic` | `/v1/messages` | `output_config.format` | Unsupported |
| `NewGemini` | `:streamGenerateContent` | `generationConfig.responseFormat.text` | Image parts |
| `NewDeepSeek` | `/v1/chat/completions` | JSON object mode; full schema stays local | Unsupported |
| `NewVLLM` | `/v1/chat/completions` | JSON schema response format | Unsupported |
| `NewSGLang` | `/v1/chat/completions` | JSON schema response format | Unsupported |
| `NewLlamaCpp` | `/v1/chat/completions` | Top-level `response_format.schema` | Unsupported |
| `NewOllama` | `/api/chat` | `format` schema | Unsupported |
| `NewOpenRouter` | `/api/v1/chat/completions` | Schema-capable endpoint routing | Model-dependent |

The table describes these adapters' feature set. A server, deployed model, tool parser, grammar
backend, account, or API version can impose further limits. A server error is returned with its HTTP
status and response body; it is not treated as a validation rejection. The adapters do not infer
capability from an “OpenAI-compatible” label. They do not retry server failures with weaker
settings.

Schema support differs across APIs. Gent checks supported keywords and structure before it sends a
native answer schema. If an exact mapping is unavailable, it omits native schema enforcement and
uses JSON object mode only when appropriate for that root. The complete original schema still runs
locally. `GenerationInfo.RawGenerationInfo["output_schema"]` records `native`, `profile`, and any
fallback `reason`. No native schema claim bypasses local or custom validation.

The profiles reflect these documented contracts:

- [OpenAI][openai-schema] requires a closed object root and all properties to be required. Optional
  fields therefore use local schema validation; Gent does not rewrite them as required nullable
  fields.
- [Anthropic][anthropic-schema] accepts optional fields but has a smaller constraint set.
  Unsupported numeric or string-length constraints stay local. References inside `allOf` also stay
  local. The adapter checks the combined limits for optional fields, unions, and strict tools. A
  separate `thinking` configuration and output effort can accompany the schema.
- [Gemini][gemini-schema] supports object, array, and scalar schemas. Unsupported keywords stay
  local instead of being sent as if enforced. Its native REST mapping uses the current per-modality
  response format fields.
- [xAI][xai-schema] has different union, object-default, and bound rules. Unsupported or best-effort
  constraints do not qualify as strict enforcement in Gent.
- [vLLM](https://docs.vllm.ai/en/latest/features/structured_outputs/) and [SGLang][sglang-schema]
  use the XGrammar profile for native structured output. Deploy a compatible backend; configure
  runtime-specific options through that adapter's metadata namespace.
- [llama.cpp](https://github.com/ggml-org/llama.cpp/blob/master/grammars/README.md) and local
  [Ollama][ollama-schema] use the supported grammar subset. Native limitations such as unrestricted
  object keys, non-integer numeric bounds, or nested references cause local fallback. Ollama's cloud
  service has a different feature set from the local `/api/chat` runtime this adapter targets.
- [DeepSeek](https://api-docs.deepseek.com/api/create-chat-completion/) uses JSON object mode for
  object output. Gent preserves `reasoning_content` with native calls for later requests.

OpenRouter checks the model's endpoint catalog and restricts `provider.only` to eligible endpoints
when it enables a schema. It retains caller `only`, `ignore`, and other route preferences and sets
`require_parameters: true`. If the allowed routes cannot enforce the schema, it reports local
fallback rather than silently changing the route. Catalog errors stop the request. Discovery is
shared across concurrent requests, cached for one minute, and bounded to 64 model entries.
See [OpenRouter structured output](https://openrouter.ai/docs/guides/features/structured-outputs).

## Input and prompt mapping

All adapters accept the standard leading system message and user text. Anthropic combines leading
system text blocks in its `system` field; Gemini uses `systemInstruction`. These adapters reject a
system message inserted after conversation content because moving it would change its meaning.
Other adapters retain the native role sequence.

| Input | OpenAI | xAI | Anthropic | Gemini | Chat runtimes / DeepSeek | Ollama | OpenRouter |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Image bytes | Yes | Yes | Yes | Yes | Yes | Yes | Model-dependent |
| Image URL | Yes | Yes | Yes | Use typed bytes | Yes | Unsupported | Model-dependent |
| PDF bytes | Yes | Unsupported | Yes | Yes | Unsupported | Unsupported | Model-dependent |
| Audio/video bytes | No | No | No | Supported MIME types | No | No | No |

The image tests cover ordered bytes and pointer parts. Actual image support also depends on the
selected model. Gemini inline input requires a supported MIME type. `ImageURLContent` does not
carry one, so that adapter rejects it rather than fetch the URL and guess. Ollama carries one text
string and an ordered image array; it joins text parts in their original order. Other protocols
retain interleaved text and image blocks. No input URL is downloaded by Gent.

## Continuation and completion

Responses requests use `store: false` and request encrypted reasoning data. All ordered response
items remain in `ProviderContinuation` and are replayed with native tool results. Anthropic retains
signed thinking blocks. Gemini retains thought signatures on their original parts. Chat runtimes
retain raw assistant fields, tool IDs, and reasoning data. Ollama correlates tool results by name;
Gent's local call IDs do not replace its native fields.

OpenRouter reasoning deltas are assembled into ordered summary and signed text blocks before
replay. Block type, ID, index, and format determine whether adjacent fragments can be joined.
Encrypted items remain separate because an index can be shared across different block types.
See [OpenRouter reasoning details][openrouter-reasoning].

Provider completion becomes one of `answer`, `tool_calls`, `continue`, `incomplete`, `refused`, or
`failed`. Only a complete answer candidate can enter `Output.Process`. A call or JSON value that
looks complete remains incomplete when the native stop reason says so. An unknown status returns
an error. Ollama has no native refusal field; refusal-like text is ordinary candidate text.

A validated refusal ends the run even when retained text or partial tool arguments exceed a size
or repetition guard. The loop saves the refused evidence and does not ask for a correction or run
its calls. Closing a stream for a guard can produce an expected cancellation; transport, recording,
and cleanup errors still end the run and remain visible to the caller.

Gemini token continuation stores the exact original request contents. The next request sends those
contents with the token. Its native maximum output token setting is cumulative across continuation
requests. With an explicit ceiling, Gent uses the ordinary incomplete-response retry instead of
raising or dropping that ceiling. With no explicit ceiling, it resumes the native token.
See the [Gemini request contract](https://ai.google.dev/api/generate-content).

Stream chunks are provisional metadata. Cancellation requests stop the producer; `Response()` waits
until the HTTP body is closed. Consumer callbacks can return an operation error. Errors from body
cleanup are retained. A stream that ends without its required terminal event cannot produce an
accepted answer. Bounded queues apply backpressure. Consume `Chunks()` before awaiting `Response()`
for a streaming call, or cancel it with `Close()` when the consumer stops.

## Media results

OpenAI and xAI use the native `image_generation` tool. Gemini decodes inline images and supported
file
references. OpenRouter accepts base64 image data URLs and HTTP(S) image references. References
become owned bytes before validation. Gent does not send model authorization headers to the image
host. Configure an HTTP client whose transport also respects that boundary.

Image output keeps every candidate part, including unexpected text. `ImageOutput` decides whether
the complete batch meets its contract. Native MIME controls are requested where available; hard
byte, pixel, and dimension limits always run locally. These adapters currently expose text and image
output, not general audio generation. Custom loops can still supply other `BinaryContent` types to
custom output processors.

## Verification

Deterministic HTTP fixtures cover all ten adapters: request fields, streaming, native tool replay,
local and custom correction, cancellation, unsupported content, and incomplete output. Tests also
cover native refusal where exposed, signed continuation, OpenRouter routing, image validation, and
terminal restore after output URLs become unavailable. The local fixture servers run in the provider
suite; no credentials are needed.

Live tests cover all ten adapters with real streaming, native tools, typed JSON, and validation
correction. Four adapters have image generation tests. Configure the variables in `.env.example`;
configured providers run in ordinary test runs. See [live test setup](models-live.md) for billing,
model selection, and the mode that requires the complete matrix. Fixture success alone does not
establish that a live deployment, model version, or account accepted a request.

[openai-schema]: https://developers.openai.com/api/docs/guides/structured-outputs
[anthropic-schema]: https://platform.claude.com/docs/en/build-with-claude/structured-outputs
[gemini-schema]: https://ai.google.dev/gemini-api/docs/generate-content/structured-output
[xai-schema]: https://docs.x.ai/developers/model-capabilities/text/structured-outputs
[sglang-schema]:
  https://github.com/sgl-project/sglang/blob/main/docs/docs/advanced_features/structured_outputs.mdx
[ollama-schema]: https://docs.ollama.com/capabilities/structured-outputs
[openrouter-reasoning]: https://openrouter.ai/docs/guides/best-practices/reasoning-tokens
