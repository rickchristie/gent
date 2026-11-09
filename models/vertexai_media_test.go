package models

import (
	"context"
	"encoding/json"
	"image/color"
	"net/http"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestVertexAIImageControlsAndCredentialIsolation(t *testing.T) {
	image := imageFixture(t, 1, color.RGBA{B: 255, A: 255})
	var methods []string
	var authorizations []string
	var body map[string]any
	client := &http.Client{Transport: responseTransport(func(r *http.Request) (*http.Response, error) {
		methods = append(methods, r.Method)
		authorizations = append(authorizations, r.Header.Get("Authorization"))
		if r.Method == http.MethodGet {
			return fixtureHTTP(string(image.Data), "image/jpeg"), nil
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return nil, err
		}
		return fixtureHTTP(imageResponse("vertexai", []gent.BinaryContent{image}, true, false),
			"application/json"), nil
	})}
	config := map[string]any{"aspectRatio": "1:1"}
	request := gent.NewModelCallRequest([]*gent.ModelMessage{{Role: gent.ChatMessageTypeHuman,
		Parts: []gent.ContentPart{gent.ImageURLPart("https://input.test/image.jpg")}}})
	request.Options.Metadata["gemini"] = map[string]any{"imageConfig": config}
	request.Options.ResponseModalities = []string{"image"}
	request.Options.ImageMIMETypes = []string{"image/jpeg"}
	stream, err := fixtureVertex(&Config{HTTPClient: client, Model: "image-model"}).
		GenerateContentStream(context.Background(), request)
	if !assert.NoError(t, err) {
		return
	}
	defer stream.Close()
	for range stream.Chunks() {
	}
	response, err := stream.Response()
	if !assert.NoError(t, err) || !assert.Len(t, response.Choices, 1) {
		return
	}
	assert.Equal(t, []gent.ContentPart{image}, response.Choices[0].CandidateContent)
	assert.Equal(t, []string{"GET", "POST", "GET"}, methods)
	assert.Equal(t, []string{"", "Bearer fixture-key", ""}, authorizations)
	assert.Equal(t, map[string]any{"aspectRatio": "1:1"}, config)
	assert.Equal(t, map[string]any{"aspectRatio": "1:1",
		"imageOutputOptions": map[string]any{"mimeType": "image/jpeg"}},
		body["generationConfig"].(map[string]any)["imageConfig"])
}

func TestVertexAIInvalidImageConfig(t *testing.T) {
	request := vertexTestRequest()
	request.Options.Metadata["gemini"] = map[string]any{"imageConfig": "invalid"}
	request.Options.ResponseModalities = []string{"image"}
	stream, err := fixtureVertex(&Config{Model: "image-model"}).GenerateContentStream(
		context.Background(), request)
	assert.Nil(t, stream)
	assert.EqualError(t, err, "Vertex AI imageConfig must be an object")
}

func TestVertexAIHTTPFailureDoesNotRepeatPaidCall(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: responseTransport(func(*http.Request) (*http.Response, error) {
		calls++
		response := fixtureHTTP(`{"error":{"code":429,"message":"quota exceeded"}}`, "application/json")
		response.StatusCode = http.StatusTooManyRequests
		return response, nil
	})}
	stream, err := fixtureVertex(&Config{Model: "model", HTTPClient: client}).GenerateContentStream(
		context.Background(), vertexTestRequest())
	if !assert.NoError(t, err) {
		return
	}
	defer stream.Close()
	for range stream.Chunks() {
	}
	response, err := stream.Response()
	assert.Nil(t, response)
	assert.EqualError(t, err,
		`model HTTP 429: {"error":{"code":429,"message":"quota exceeded"}}`)
	assert.Equal(t, 1, calls)
}
