package models

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
)

func TestHelloGrok(t *testing.T) {
	apiKey := os.Getenv("GENT_TEST_XAI_KEY")
	if apiKey == "" {
		t.Skip("GENT_TEST_XAI_KEY not set")
	}

	ctx := context.Background()

	llm, err := openai.New(
		openai.WithToken(apiKey),
		openai.WithBaseURL("https://api.x.ai/v1"),
		openai.WithModel("grok-4-1-fast-reasoning"),
	)
	require.NoError(t, err, "failed to create xAI LLM")

	model := NewLCGWrapper(llm)

	response, err := model.GenerateContent(ctx, nil, "", "", []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeHuman, "Hello Grok! Nice to meet you!"),
	})
	require.NoError(t, err, "failed to generate response")

	responseJSON, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		log.Printf("Error marshaling response: %v", err)
	} else {
		log.Printf("Response:\n%s", responseJSON)
	}

	assert.NotEmpty(t, response.Choices, "expected non-empty choices")
	assert.NotEmpty(t, response.Choices[0].Content, "expected non-empty response content")
}
