package models

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents/react"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/output"
	"github.com/stretchr/testify/assert"
)

func imageFixture(t *testing.T, width int, fill color.RGBA) gent.BinaryContent {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, 1))
	for x := range width {
		img.Set(x, 0, fill)
	}
	var buffer bytes.Buffer
	if !assert.NoError(t, jpeg.Encode(&buffer, img, nil)) {
		t.FailNow()
	}
	return gent.BinaryContent{MIMEType: "image/jpeg", Data: buffer.Bytes()}
}

func imageResponse(provider string, images []gent.BinaryContent, reference, extraText bool) string {
	var parts []any
	if extraText {
		switch provider {
		case "openai", "xai":
			parts = append(parts, map[string]any{"type": "message", "role": "assistant",
				"content": []any{map[string]any{"type": "output_text", "text": "Here are the images."}}})
		case "gemini":
			parts = append(parts, map[string]any{"text": "Here are the images."})
		}
	}
	for index, part := range images {
		encoded := base64.StdEncoding.EncodeToString(part.Data)
		uri := "data:" + part.MIMEType + ";base64," + encoded
		if reference {
			uri = []string{"https://images.test/one", "https://images.test/two"}[index]
		}
		switch provider {
		case "openai", "xai":
			parts = append(parts, map[string]any{"type": "image_generation_call", "status": "completed",
				"output_format": "jpeg", "result": encoded})
		case "gemini":
			part := map[string]any{"inlineData": map[string]any{"mimeType": part.MIMEType,
				"data": encoded}}
			if reference {
				part = map[string]any{"fileData": map[string]any{"mimeType": "image/jpeg", "fileUri": uri}}
			}
			parts = append(parts, part)
		case "openrouter":
			parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{
				"url": uri}})
		}
	}
	var value any
	switch provider {
	case "openai", "xai":
		value = map[string]any{"type": "response.completed", "response": map[string]any{
			"status": "completed", "output": parts}}
	case "gemini":
		value = map[string]any{"candidates": []any{map[string]any{"finishReason": "STOP",
			"content": map[string]any{"role": "model", "parts": parts}}}}
	case "openrouter":
		message := map[string]any{"role": "assistant", "images": parts}
		if extraText {
			message["content"] = "Here are the images."
		}
		value = map[string]any{"choices": []any{map[string]any{"message": message,
			"finish_reason": "stop"}}}
	}
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	if provider == "openai" || provider == "xai" {
		return "data: " + string(data) + `

`
	}
	return string(data)
}

func TestProvidersImageCorrectionAndOfflineRestore(t *testing.T) {
	type input struct {
		provider  string
		reference bool
	}
	type expected struct {
		requests, downloads, validations int
		localRejected                    int64
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"OpenAI", input{"openai", false}, expected{2, 0, 1, 1}},
		{"xAI", input{"xai", false}, expected{2, 0, 1, 1}},
		{"Gemini inline", input{"gemini", false}, expected{2, 0, 1, 1}},
		{"Gemini reference", input{"gemini", true}, expected{2, 4, 1, 1}},
		{"OpenRouter inline", input{"openrouter", false}, expected{2, 0, 1, 1}},
		{"OpenRouter reference", input{"openrouter", true}, expected{2, 4, 1, 1}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			images := []gent.BinaryContent{imageFixture(t, 1, color.RGBA{R: 255, A: 255}),
				imageFixture(t, 2, color.RGBA{B: 255, A: 255})}
			var requests, downloads, validations int
			var bodies []map[string]any
			client := &http.Client{Transport: responseTransport(func(request *http.Request) (
				*http.Response, error,
			) {
				if request.URL.Host == "images.test" {
					downloads++
					assert.Empty(t, request.Header.Get("Authorization"))
					assert.Empty(t, request.Header.Get("X-Goog-Api-Key"))
					part := images[0]
					if request.URL.Path == "/two" {
						part = images[1]
					}
					return &http.Response{StatusCode: 200,
						Header: http.Header{"Content-Type": {part.MIMEType}},
						Body:   io.NopCloser(bytes.NewReader(part.Data))}, nil
				}
				if request.Method == http.MethodGet {
					return fixtureHTTP(routerFixture, "application/json"), nil
				}
				requests++
				if requests > 2 {
					return nil, errors.New("unexpected image retry")
				}
				var body map[string]any
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					return nil, err
				}
				bodies = append(bodies, body)
				mime := "application/json"
				if test.input.provider == "openai" || test.input.provider == "xai" {
					mime = "text/event-stream"
				}
				return fixtureHTTP(imageResponse(test.input.provider, images, test.input.reference,
					requests == 1), mime), nil
			})}
			newOutput := func() *output.ImageOutput {
				return output.NewImageOutput().SetAllowedMIMETypes("image/jpeg").
					SetMaxWidth(2).SetMaxHeight(1).
					SetMaxFileSizeBytes(int64(max(len(images[0].Data), len(images[1].Data)))).
					SetCustomValidator("image-rules/v1", func(_ *gent.ExecutionContext,
						value []gent.BinaryContent,
					) (*gent.OutputValidationResult, error) {
						validations++
						assert.Equal(t, images, value)
						return &gent.OutputValidationResult{Accepted: true}, nil
					})
			}
			ctx := gent.NewExecutionContext(context.Background(), "images", gent.NewBasicLoopData(
				gent.NewTask(gent.TextContent{Text: "Return only two JPEG images."})))
			runner := executor.NewRunner(react.NewWithOutput(fixtureModel(test.input.provider, client),
				newOutput()), executor.DefaultConfig())
			value, result, err := runner.Execute(ctx)
			if !assert.NoError(t, err) {
				return
			}
			delivery := []gent.ContentPart{images[0], images[1]}
			assert.Equal(t, images, value)
			assert.Equal(t, &gent.ExecutionResult{TerminationReason: gent.TerminationSuccess,
				Output: delivery}, result)
			assert.Equal(t, test.expected, expected{requests, downloads, validations,
				ctx.Stats().GetCounter(gent.SCOutputLocalRejectedTotal)})
			assertImageRequest(t, test.input.provider, bodies[0])
			saved, err := ctx.Export()
			if !assert.NoError(t, err) {
				return
			}
			wire, err := ctx.Codecs().Marshal(saved)
			if !assert.NoError(t, err) {
				return
			}
			var bundle gent.RunView
			if !assert.NoError(t, ctx.Codecs().Unmarshal(wire, &bundle)) {
				return
			}
			value[0].Data[0] = 0
			result.Output[0] = gent.TextContent{Text: "changed"}
			// Both the model endpoint and the image URL are unavailable after the terminal save.
			client = &http.Client{Transport: responseTransport(func(*http.Request) (*http.Response, error) {
				t.Error("terminal restore made an HTTP request")
				return nil, errors.New("reference expired")
			})}
			runner = executor.NewRunner(react.NewWithOutput(fixtureModel(test.input.provider, client),
				newOutput()), executor.DefaultConfig())
			for range 2 {
				restored, metadata, err := runner.Resume(context.Background(), bundle, gent.RestoreBindings{})
				assert.NoError(t, err)
				assert.Equal(t, images, restored)
				assert.Equal(t, &gent.ExecutionResult{TerminationReason: gent.TerminationSuccess,
					Output: delivery}, metadata)
				if len(restored) > 0 {
					restored[0].Data[0] = 0
				}
			}
			assert.Equal(t, 1, validations, "restore must not run custom validation")
		})
	}
}

func assertImageRequest(t *testing.T, provider string, body map[string]any) {
	t.Helper()
	switch provider {
	case "openai", "xai":
		image := map[string]any{"type": "image_generation"}
		var choice any = "required"
		if provider == "openai" {
			image["output_format"] = "jpeg"
			choice = map[string]any{"type": "image_generation"}
		}
		assert.Equal(t, []any{image}, body["tools"])
		assert.Equal(t, choice, body["tool_choice"])
	case "gemini":
		config := body["generationConfig"].(map[string]any)
		assert.Equal(t, []any{"IMAGE"}, config["responseModalities"])
		assert.Equal(t, map[string]any{"image": map[string]any{"delivery": "INLINE",
			"mimeType": "IMAGE_JPEG"}}, config["responseFormat"])
	case "openrouter":
		assert.Equal(t, []any{"image"}, body["modalities"])
	}
}
