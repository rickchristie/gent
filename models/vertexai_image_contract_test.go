package models

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents/react"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/output"
	"github.com/stretchr/testify/assert"
)

func TestVertexAIImageContractOverridesMetadata(t *testing.T) {
	type input struct {
		allowed  []string
		metadata string
	}
	type expected struct {
		image    gent.BinaryContent
		requests int
		rejected int64
		reason   gent.TerminationReason
		err      string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	blue := color.RGBA{B: 255, A: 255}
	pixels := image.NewRGBA(image.Rect(0, 0, 1, 1))
	pixels.Set(0, 0, blue)
	var data bytes.Buffer
	if !assert.NoError(t, png.Encode(&data, pixels)) {
		return
	}
	pngImage := gent.BinaryContent{MIMEType: "image/png", Data: data.Bytes()}
	jpegImage := imageFixture(t, 1, blue)
	cases := []testCase{
		{"PNG overrides JPEG", input{[]string{"image/png"}, "image/jpeg"},
			expected{image: pngImage, requests: 1, reason: gent.TerminationSuccess}},
		{"JPEG overrides PNG", input{[]string{"image/jpeg"}, "image/png"},
			expected{image: jpegImage, requests: 1, reason: gent.TerminationSuccess}},
		{"both formats allow JPEG", input{[]string{"image/png", "image/jpeg"}, "image/png"},
			expected{image: jpegImage, requests: 1, reason: gent.TerminationSuccess}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var requests []map[string]any
			client := &http.Client{Transport: responseTransport(func(r *http.Request) (
				*http.Response, error,
			) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					return nil, err
				}
				config := body["generationConfig"].(map[string]any)["imageConfig"].(map[string]any)
				requests = append(requests, config)
				format := config["imageOutputOptions"].(map[string]any)["mimeType"].(string)
				// The fixture follows the request format, so a conflict causes real output rejection.
				part := pngImage
				if format == "image/jpeg" {
					part = jpegImage
				}
				return fixtureHTTP(imageResponse("vertexai", []gent.BinaryContent{part}, false, false),
					"application/json"), nil
			})}
			metadata := map[string]any{"imageConfig": map[string]any{"aspectRatio": "1:1",
				"imageOutputOptions": map[string]any{"mimeType": test.input.metadata,
					"compressionQuality": float64(80)}}}
			out := output.NewImageOutput().SetAllowedMIMETypes(test.input.allowed...)
			agent := react.NewWithOutput(fixtureVertex(&Config{Model: "image-model", HTTPClient: client}),
				out).WithCallOptions(func(options *gent.CallOptions) {
				options.Metadata["gemini"] = metadata
			})
			ctx := gent.NewExecutionContext(context.Background(), "image contract", gent.NewBasicLoopData(
				gent.NewTask(gent.TextPart("Return only an image of a blue square."))))
			defer ctx.Release()
			ctx.SetLimits([]gent.Limit{{Type: gent.LimitExactKey, Key: gent.SCIterations, MaxValue: 2}})
			value, result, err := executor.Run(ctx, agent, executor.DefaultConfig())
			actual := expected{requests: len(requests),
				rejected: ctx.Stats().GetCounter(gent.SCOutputLocalRejectedTotal)}
			if err != nil {
				actual.err = err.Error()
			}
			if len(value) == 1 {
				actual.image = value[0]
			}
			if result != nil {
				actual.reason = result.TerminationReason
			}
			assert.Equal(t, test.expected, actual)
			assert.Equal(t, []gent.BinaryContent{test.expected.image}, value)
			assert.Equal(t, &gent.ExecutionResult{TerminationReason: test.expected.reason,
				Output: []gent.ContentPart{test.expected.image}}, result)
			assert.Equal(t, []map[string]any{{"aspectRatio": "1:1", "imageOutputOptions": map[string]any{
				"mimeType": test.expected.image.MIMEType, "compressionQuality": float64(80)}}}, requests)
			assert.Equal(t, map[string]any{"imageConfig": map[string]any{"aspectRatio": "1:1",
				"imageOutputOptions": map[string]any{"mimeType": test.input.metadata,
					"compressionQuality": float64(80)}}}, metadata)
		})
	}
}
