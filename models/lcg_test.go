package models

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"testing"

	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
)

func TestHelloGrok(t *testing.T) {
	apiKey := os.Getenv("GENT_TEST_XAI_KEY")
	if apiKey == "" {
		t.Fatal("GENT_TEST_XAI_KEY not set")
	}

	ctx := context.Background()

	llm, err := openai.New(
		openai.WithToken(apiKey),
		openai.WithBaseURL("https://api.x.ai/v1"),
		openai.WithModel("grok-4-1-fast-reasoning"),
	)
	if err != nil {
		t.Fatalf("failed to create xAI LLM: %v", err)
	}

	model := NewLCGWrapper(llm)

	response, err := model.GenerateContent(ctx, nil, []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeHuman, "Hello Grok! Nice to meet you!"),
	})
	if err != nil {
		t.Fatalf("failed to generate response: %v", err)
	}

	responseJSON, _ := json.MarshalIndent(response, "", "  ")
	log.Printf("Response:\n%s", responseJSON)

	if len(response.Choices) == 0 || response.Choices[0].Content == "" {
		t.Error("expected non-empty response")
	}
}
