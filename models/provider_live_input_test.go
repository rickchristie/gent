package models

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

// Each call uses the same adapter but a different answer. A shared response or request buffer
// must not mix concurrent conversations. This also checks that an adapter can be used again.
func TestLiveProviderConcurrentCalls(t *testing.T) {
	type input struct{ text string }
	type expected struct {
		text   string
		status gent.ModelStatus
		err    error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"first", input{"gent-first-ok"}, expected{"gent-first-ok", gent.ModelStatusAnswer, nil}},
		{"second", input{"gent-second-ok"}, expected{"gent-second-ok", gent.ModelStatusAnswer, nil}},
		{"third", input{"gent-third-ok"}, expected{"gent-third-ok", gent.ModelStatusAnswer, nil}},
	}
	for _, provider := range liveProviders() {
		t.Run(provider.name, func(t *testing.T) {
			config := liveConfig(t, provider, false)
			t.Parallel()
			model := newLiveModel(t, provider, &config)
			for _, test := range cases {
				t.Run(test.name, func(t *testing.T) {
					t.Parallel()
					ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
					defer cancel()
					request := gent.NewModelCallRequest([]*gent.ModelMessage{
						gent.TextParts(gent.ChatMessageTypeHuman,
							"Reply with exactly "+test.input.text+" and nothing else."),
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
					choice := response.Choices[0]
					assert.Equal(t, test.expected, expected{
						gent.ContentText(choice.CandidateContent), choice.Completion.Status, err})
				})
			}
		})
	}
}

// These default cloud models support image input. Local and routed models need separate model
// selection; their media mapping is checked by the full provider fixture matrix.
func TestLiveProviderImageInput(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{R: 255, A: 255}), image.Point{}, draw.Src)
	var buffer bytes.Buffer
	if !assert.NoError(t, png.Encode(&buffer, img)) {
		return
	}
	for _, provider := range liveProviders() {
		if provider.prefix != "OPENAI" && provider.prefix != "ANTHROPIC" &&
			provider.prefix != "GEMINI" && provider.prefix != "VERTEXAI" && provider.prefix != "XAI" {
			continue
		}
		t.Run(provider.name, func(t *testing.T) {
			config := liveConfig(t, provider, false)
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			request := gent.NewModelCallRequest([]*gent.ModelMessage{
				{Role: gent.ChatMessageTypeHuman, Parts: []gent.ContentPart{
					gent.TextPart("What single color fills this image? Reply with one lowercase word."),
					gent.BytesPart("image/png", buffer.Bytes()),
				}},
			}, gent.WithMaxTokens(2048))
			stream, err := newLiveModel(t, provider, &config).GenerateContentStream(ctx, request)
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
			assert.Equal(t, "red", gent.ContentText(response.Choices[0].CandidateContent))
		})
	}
}
