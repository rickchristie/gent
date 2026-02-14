# GitHub Copilot Pro+ / GitHub Models Integration Plan

## Context

GitHub Copilot Pro+ subscribers get access to premium models (GPT-4.1, Claude, Gemini, Llama,
etc.) through the **GitHub Models API** at `https://models.github.ai/inference`.

**Goal**: Enable Gent users to use their GitHub Copilot Pro+ subscription to access models.

## Research Findings

### LangChainGo Support
- LangChainGo v0.1.14 has **no dedicated GitHub Models provider**
- Provider list: `anthropic`, `bedrock`, `cloudflare`, `cohere`, `ernie`, `googleai`,
  `huggingface`, `llamafile`, `local`, `maritaca`, `mistral`, `ollama`, `openai`, `watsonx`
- Python has an unofficial `langchain-github-copilot` package, but nothing for Go

### GitHub Models API Compatibility
The GitHub Models API is **OpenAI-compatible**:
- **Endpoint**: `POST https://models.github.ai/inference/chat/completions`
- **Auth**: `Authorization: Bearer <GITHUB_PAT>` (same as OpenAI Bearer format)
- **Request format**: Same as OpenAI (`model`, `messages`, `temperature`, `tools`, etc.)
- **Response format**: Same as OpenAI (`choices`, `usage` with `prompt_tokens`, etc.)
- **Model names**: `publisher/model_name` format (e.g., `openai/gpt-4.1`)

### LangChainGo OpenAI URL Construction
From `openaiclient.go`:
```go
func (c *Client) buildURL(suffix string, model string) string {
    return fmt.Sprintf("%s%s", c.baseURL, suffix)  // for non-Azure
}
```
Called with suffix `/chat/completions`, so:
- `baseURL("https://models.github.ai/inference") + "/chat/completions"` = correct endpoint

### Token Normalization
GitHub Models returns OpenAI-format usage (`PromptTokens`, `CompletionTokens`, `TotalTokens`),
so Gent's existing `extractInputTokens`/`extractOutputTokens` in `models/lcg.go` will work.

## Conclusion: It Already Works (Almost)

Using LangChainGo's OpenAI provider with `WithBaseURL` should work:

```go
llm, err := openai.New(
    openai.WithBaseURL("https://models.github.ai/inference"),
    openai.WithToken(os.Getenv("GITHUB_TOKEN")),
    openai.WithModel("openai/gpt-4.1"),
)
model := models.NewLCGWrapper(llm).WithModelName("gpt-4.1")
```

### Potential Issues
1. **Extra headers**: GitHub docs mention `Accept: application/vnd.github+json` and
   `X-GitHub-Api-Version: 2022-11-28`. LangChainGo's OpenAI client doesn't send these.
   May or may not be required for the inference endpoint.
2. **`max_completion_tokens` vs `max_tokens`**: Some GitHub-hosted models may only support one.
   LangChainGo has `WithLegacyMaxTokensField()` as a fallback.

## Proposed Plan

### Option A: Convenience Helper + Integration Test (Recommended)

Add a thin convenience layer in `models/` that makes GitHub Models easy to use and verify.

**Step 1: Add `models/github.go`** — Convenience constructor
```go
// NewGitHubModel creates a Model backed by GitHub Models API.
// Requires a GitHub PAT with models:read scope.
//
// Example:
//   model, err := models.NewGitHubModel("openai/gpt-4.1", os.Getenv("GITHUB_TOKEN"))
func NewGitHubModel(model, token string, opts ...openai.Option) (*LCGWrapper, error)
```

This would:
- Set `WithBaseURL("https://models.github.ai/inference")`
- Set `WithToken(token)`
- Set `WithModel(model)`
- Optionally inject a custom HTTP client RoundTripper for GitHub-required headers
  (if testing reveals they're needed)
- Return `NewLCGWrapper(llm).WithModelName(model)`

**Step 2: Add `models/github_test.go`** — Integration test
- Test with `GENT_TEST_GITHUB_TOKEN` env var (skip if not set)
- Test basic generation with a GitHub-hosted model (e.g., `openai/gpt-4o-mini`)
- Verify token counts are populated correctly
- Verify streaming works

**Step 3: Verify header requirements**
- Test whether the API works without `Accept` and `X-GitHub-Api-Version` headers
- If headers are required, add a custom `http.RoundTripper` wrapper that injects them

### Option B: Documentation Only

Just document how to use `openai.WithBaseURL` in existing code. No new Go code.
Pros: Zero maintenance burden. Cons: Users must discover the pattern themselves, no
verified integration.

### Option C: Full Provider

Write a dedicated `models/github/` provider that implements `gent.Model` directly
(bypassing LangChainGo). Overkill given the OpenAI compatibility.

## Recommendation

**Option A** — small, useful, and verified. The convenience function makes it discoverable
and the integration test ensures it actually works. If headers end up being needed, the
custom RoundTripper is a clean solution that doesn't require upstream LangChainGo changes.

## Available Models (GitHub Models, as of Feb 2026)

| Publisher | Models |
|-----------|--------|
| OpenAI | gpt-4.1, gpt-4.1-mini, gpt-4o, gpt-4o-mini, o1, o3-mini, o4-mini |
| Anthropic | claude-sonnet-4-5-20250929 |
| Google | gemini-2.0-flash |
| Meta | llama-4-scout, llama-4-maverick |
| DeepSeek | deepseek-r1 |
| Microsoft | phi-4, mai-ds-r1 |
