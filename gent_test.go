package gent

import (
	"context"
	"testing"

	"github.com/tmc/langchaingo/llms/openai"
)

func TestHelloWorld(t *testing.T) {
	ctx := context.Background()

	// Pass your API key programmatically
	apiKey := "your-api-key-here"

	llm, err := openai.New(openai.WithToken(apiKey))
	if err != nil {
		t.Fatalf("failed to create OpenAI LLM: %v", err)
	}

	// Generate a response using the LLM
	response, err := Generate(ctx, llm, "Say 'Hello, World!' and nothing else.")
	if err != nil {
		t.Fatalf("failed to generate response: %v", err)
	}

	t.Logf("LLM response: %s", response)

	if response == "" {
		t.Error("expected non-empty response")
	}
}
