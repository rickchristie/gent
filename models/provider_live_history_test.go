package models

import (
	"context"
	"crypto/rand"
	"fmt"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestLiveProviderHistory(t *testing.T) {
	for _, provider := range liveProviders() {
		t.Run(provider.name, func(t *testing.T) {
			config := liveConfig(t, provider, false)
			t.Parallel()
			model := newLiveModel(t, provider, &config)
			t.Run("AssistantHistory", func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				checkLiveAssistantHistory(t, ctx, model)
			})
			t.Run("RepeatedCompaction", func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				secret := make([]byte, 12)
				if _, err := rand.Read(secret); !assert.NoError(t, err) {
					return
				}
				checkProviderCompaction(t, ctx, model, fmt.Sprintf("record-%x", secret))
			})
		})
	}
}

func checkLiveAssistantHistory(t *testing.T, ctx context.Context, model gent.Model) {
	t.Helper()
	request := gent.NewModelCallRequest([]*gent.ModelMessage{
		gent.TextParts(gent.ChatMessageTypeSystem, "Answer with the saved area name only."),
		gent.TextParts(gent.ChatMessageTypeHuman, "Keep the area from the following summary."),
		gent.TextParts(gent.ChatMessageTypeAI, "SUMMARY: the customer asked about a room in Tebet."),
		gent.TextParts(gent.ChatMessageTypeHuman, "Which area did I ask about?"),
	}, gent.WithMaxTokens(2048))
	stream, err := model.GenerateContentStream(ctx, request)
	if !assert.NoError(t, err) {
		return
	}
	defer stream.Close()
	for chunk := range stream.Chunks() {
		assert.NoError(t, chunk.Err)
	}
	response, err := stream.Response()
	if !assert.NoError(t, err) || !assert.Len(t, response.Choices, 1) {
		return
	}
	assert.Equal(t, gent.ModelStatusAnswer, response.Choices[0].Completion.Status)
	assert.Equal(t, "Tebet", gent.ContentText(response.Choices[0].CandidateContent))
}
