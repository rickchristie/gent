package gent

import (
	"context"

	"github.com/tmc/langchaingo/llms"
)

// Generate sends a prompt to the LLM and returns the response.
func Generate(ctx context.Context, model llms.Model, prompt string) (string, error) {
	return llms.GenerateFromSinglePrompt(ctx, model, prompt)
}
