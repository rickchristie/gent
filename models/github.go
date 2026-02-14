package models

import (
	"fmt"
	"net/http"

	"github.com/tmc/langchaingo/llms/openai"
)

const (
	// GitHubModelsBaseURL is the base URL for the GitHub Models API.
	// The OpenAI-compatible chat completions endpoint is at
	// {baseURL}/chat/completions.
	GitHubModelsBaseURL = "https://models.github.ai/inference"
)

// githubHeaderTransport wraps an http.RoundTripper and injects
// GitHub-specific headers into every request.
type githubHeaderTransport struct {
	base http.RoundTripper
}

func (t *githubHeaderTransport) Do(
	req *http.Request,
) (*http.Response, error) {
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	return t.base.RoundTrip(req)
}

// NewGitHubCopilotModel creates a Model backed by the GitHub
// Models API. This lets you use models available through a GitHub
// Copilot Pro+ subscription (or the free tier with lower rate
// limits).
//
// The token must be a GitHub Personal Access Token (fine-grained)
// with the models:read permission under Account permissions.
//
// Model names use the publisher/model format, for example:
//
//	"openai/gpt-4.1"
//	"openai/gpt-4o-mini"
//	"meta/llama-4-scout"
//
// Additional openai.Option values can be passed to customise the
// underlying LangChainGo OpenAI client (e.g. WithHTTPClient).
//
// Example:
//
//	model, err := models.NewGitHubCopilotModel(
//	    "openai/gpt-4.1",
//	    os.Getenv("GITHUB_TOKEN"),
//	)
func NewGitHubCopilotModel(
	model string,
	token string,
	opts ...openai.Option,
) (*LCGWrapper, error) {
	if token == "" {
		return nil, fmt.Errorf(
			"github token is required: "+
				"create a fine-grained PAT with models:read "+
				"at https://github.com/settings/personal-access-tokens/new",
		)
	}

	// Base options: point at GitHub Models API.
	baseOpts := []openai.Option{
		openai.WithBaseURL(GitHubModelsBaseURL),
		openai.WithToken(token),
		openai.WithModel(model),
		openai.WithHTTPClient(&githubHeaderTransport{
			base: http.DefaultTransport,
		}),
	}

	// Caller options come after so they can override defaults
	// (e.g. a custom HTTP client).
	allOpts := append(baseOpts, opts...)

	llm, err := openai.New(allOpts...)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to create GitHub Models client: %w", err,
		)
	}

	return NewLCGWrapper(llm).WithModelName(model), nil
}
