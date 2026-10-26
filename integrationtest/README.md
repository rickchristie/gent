# Integration test CLI

Run `go run ./integrationtest/cli` to select a scenario, toolchain, and model.

The model menu has one current general-purpose flagship from each supported frontier provider.
Choose the strongest model with public API access. Exclude older generations, small variants,
specialized models, and models that need restricted access. This keeps the menu short and makes
provider comparisons easier. Public model constants remain available for other callers.

Model IDs checked on 2026-10-26:

| Provider | Model | API key environment variable |
| --- | --- | --- |
| xAI | `grok-4.7` (default) | `GENT_TEST_XAI_KEY` |
| OpenAI | `gpt-6-astra` | `GENT_TEST_OPENAI_KEY` |
| Google | `gemini-4-argon` | `GENT_TEST_GEMINI_KEY` |
| Anthropic | `claude-fable-5-1` | `GENT_TEST_ANTHROPIC_KEY` |

Use the provider model pages to review future changes:

- [Grok 4.7](https://docs.x.ai/developers/models/grok-4.7)
- [OpenAI model guidance](https://developers.openai.com/api/docs/guides/latest-model)
- [Gemini 4 Argon](https://ai.google.dev/gemini-api/docs/models/gemini-4-argon)
- [Claude Fable 5.1](https://platform.claude.com/docs/en/models/fable-5-1/overview)

Update `testutil.AvailableModels`, `testutil.DefaultModelName`, and the menu tests together.
Check request settings as well as model names. A model change can make old settings invalid.

Grok 4.7 uses temperature 0.7 and top-p 0.95, as specified in the
[xAI guide](https://docs.x.ai/developers/model-capabilities/text/best-practices).
GPT-6 Astra uses the Responses API for tool calls and omits temperature and top-p.
Claude Fable 5.1 uses its default adaptive thinking and rejects custom sampling values.
The current Anthropic SDK always sends temperature and pins API version `2023-06-01`.
The existing SDK adapter sends the accepted default value of 1 when Gent forbids custom values.
Check the [Claude sampling rules](https://platform.claude.com/docs/en/build-with-claude/thinking)
before changing that API version: `2026-10-21` and later require these fields to be absent.
Gemini 4 Argon supports the existing
[OpenAI-compatible endpoint](https://ai.google.dev/gemini-api/docs/openai).
It ignores sampling values.

The Anthropic client still cannot preserve signed thinking blocks across tool calls. Claude Fable
5.1 always uses thinking, so its complete tool loop needs a lossless Anthropic adapter. Updating the
menu and request defaults does not remove this adapter limit. The local request tests below check
request construction, not live reasoning continuation.

The local request tests check model IDs, endpoints, credentials, and complete request bodies.
They do not make paid API calls. Live scenario tests need provider credentials and the ONNX setup
described by `gent setup onnx` and `gent model download multilingual-e5-small`.
