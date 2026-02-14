package models

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
)

const ghCopilotTestModel = GHCopilotGPT4oMini

func TestGitHubCopilotGenerate(t *testing.T) {
	token := os.Getenv("GENT_TEST_GITHUB_TOKEN")
	if token == "" {
		t.Skip("GENT_TEST_GITHUB_TOKEN not set")
	}

	ctx := context.Background()

	model, err := NewGitHubCopilotModel(ghCopilotTestModel, token)
	require.NoError(t, err, "failed to create GitHub Copilot model")

	execCtx := gent.NewExecutionContext(ctx, "gh-test", nil)
	response, err := model.GenerateContent(
		execCtx, "", "",
		[]llms.MessageContent{
			llms.TextParts(
				llms.ChatMessageTypeHuman,
				"Reply with exactly: Hello from GitHub Models",
			),
		},
	)
	require.NoError(t, err, "GenerateContent failed")

	responseJSON, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		log.Printf("Error marshaling response: %v", err)
	} else {
		log.Printf("Response:\n%s", responseJSON)
	}

	require.NotEmpty(t, response.Choices, "expected non-empty choices")
	assert.NotEmpty(
		t, response.Choices[0].Content,
		"expected non-empty response content",
	)

	// Verify token counts are populated (OpenAI-compatible format).
	require.NotNil(t, response.Info, "expected generation info")
	assert.Greater(
		t, response.Info.InputTokens, 0,
		"expected positive input tokens",
	)
	assert.Greater(
		t, response.Info.OutputTokens, 0,
		"expected positive output tokens",
	)
	assert.Greater(
		t, response.Info.TotalTokens, 0,
		"expected positive total tokens",
	)
}

func TestGitHubCopilotStreaming(t *testing.T) {
	token := os.Getenv("GENT_TEST_GITHUB_TOKEN")
	if token == "" {
		t.Skip("GENT_TEST_GITHUB_TOKEN not set")
	}

	ctx := context.Background()

	model, err := NewGitHubCopilotModel(ghCopilotTestModel, token)
	require.NoError(t, err, "failed to create GitHub Copilot model")

	execCtx := gent.NewExecutionContext(ctx, "gh-stream-test", nil)
	stream, err := model.GenerateContentStream(
		execCtx, "gh-stream", "test",
		[]llms.MessageContent{
			llms.TextParts(
				llms.ChatMessageTypeHuman,
				"Write a haiku about Go programming.",
			),
		},
	)
	require.NoError(t, err, "GenerateContentStream failed")

	var chunkCount int
	var totalContent string
	for chunk := range stream.Chunks() {
		require.NoError(t, chunk.Err, "stream chunk error")
		totalContent += chunk.Content
		if chunk.Content != "" {
			chunkCount++
		}
	}

	response, err := stream.Response()
	require.NoError(t, err, "stream Response() failed")

	log.Printf(
		"Streamed %d chunks, %d chars total",
		chunkCount, len(totalContent),
	)
	log.Printf("Content: %s", totalContent)

	assert.Greater(t, chunkCount, 0, "expected at least one chunk")
	assert.NotEmpty(t, totalContent, "expected non-empty streamed content")

	// Verify response info from streaming.
	require.NotNil(t, response.Info, "expected generation info")
	assert.Greater(
		t, response.Info.InputTokens, 0,
		"expected positive input tokens",
	)
	assert.Greater(
		t, response.Info.OutputTokens, 0,
		"expected positive output tokens",
	)
}

func TestGitHubCopilotMissingToken(t *testing.T) {
	_, err := NewGitHubCopilotModel(ghCopilotTestModel, "")
	require.Error(t, err, "expected error for empty token")
	assert.Contains(
		t, err.Error(), "github token is required",
		"expected descriptive error message",
	)
}
