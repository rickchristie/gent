package output_test

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/output"
	"github.com/stretchr/testify/assert"
)

func imageFile(t *testing.T, mime string) gent.BinaryContent {
	t.Helper()
	value := image.NewRGBA(image.Rect(0, 0, 3, 2))
	var buffer bytes.Buffer
	var err error
	if mime == "image/png" {
		err = png.Encode(&buffer, value)
	} else {
		err = jpeg.Encode(&buffer, value, nil)
	}
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	return gent.BinaryContent{MIMEType: mime, Data: buffer.Bytes()}
}

func TestImageOutputValidation(t *testing.T) {
	first := imageFile(t, "image/png")
	second := imageFile(t, "image/jpeg")
	type input struct {
		out   *output.ImageOutput
		parts []gent.ContentPart
	}
	type expected struct {
		result    *gent.OutputResult[[]gent.BinaryContent]
		calls     int
		errorText string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	rejected := func(message string) expected {
		return expected{result: &gent.OutputResult[[]gent.BinaryContent]{Steering: text(message)}}
	}
	cases := []testCase{
		{"all exact boundaries", input{output.NewImageOutput().
			SetMaxFileSizeBytes(int64(len(first.Data))).SetMaxWidth(3).SetMaxHeight(2).
			SetMaxDecodedPixels(6).SetAllowedMIMETypes("image/png"), []gent.ContentPart{first}},
			expected{calls: 1, result: &gent.OutputResult[[]gent.BinaryContent]{Accepted: true,
				Value: []gent.BinaryContent{first}, Content: []gent.ContentPart{first}}}},
		{"all images in order", input{output.NewImageOutput(), []gent.ContentPart{&first, second}},
			expected{calls: 1, result: &gent.OutputResult[[]gent.BinaryContent]{Accepted: true,
				Value: []gent.BinaryContent{first, second}, Content: []gent.ContentPart{first, second}}}},
		{"width", input{output.NewImageOutput().SetMaxWidth(2), []gent.ContentPart{first}},
			rejected("Image 1 is 3 pixels wide. The maximum is 2. Return an image no wider than 2 pixels.")},
		{"height", input{output.NewImageOutput().SetMaxHeight(1), []gent.ContentPart{first}},
			rejected(
				"Image 1 is 2 pixels high. The maximum is 1. Return an image no higher than 1 pixels.")},
		{"file size", input{output.NewImageOutput().SetMaxFileSizeBytes(int64(len(first.Data) - 1)),
			[]gent.ContentPart{first}}, rejected(fmt.Sprintf(
			"Image 1 is %d bytes. The maximum is %d. Return a smaller image file.",
			len(first.Data), len(first.Data)-1))},
		{"decoded size", input{output.NewImageOutput().SetMaxDecodedPixels(5),
			[]gent.ContentPart{first}},
			rejected("Image 1 exceeds the decoder limit of 5 pixels. Return a smaller image.")},
		{"declared type", input{output.NewImageOutput(), []gent.ContentPart{
			gent.BinaryContent{MIMEType: "image/jpeg", Data: first.Data}}},
			rejected("Image 1 declares image/jpeg but contains image/png. Return matching image data.")},
		{"allowed type applies to whole batch", input{
			output.NewImageOutput().SetAllowedMIMETypes("image/png"), []gent.ContentPart{first, second}},
			rejected("Image 2 is image/jpeg. Return one of these types: image/png.")},
		{"invalid bytes", input{output.NewImageOutput(), []gent.ContentPart{
			gent.BinaryContent{MIMEType: "image/png", Data: []byte("not an image")}}},
			rejected("Image 1 is not a valid PNG or JPEG file. Return a complete image.")},
		{"valid header truncated body", input{output.NewImageOutput(), []gent.ContentPart{
			gent.BinaryContent{MIMEType: "image/png", Data: first.Data[:len(first.Data)-12]}}},
			rejected("Image 1 is not a valid PNG or JPEG file. Return a complete image.")},
		{"extra text", input{output.NewImageOutput(), []gent.ContentPart{first,
			gent.TextContent{Text: "answer text"}}},
			rejected("Answer part 2 is not image bytes. Return only complete image files.")},
		{"empty", input{out: output.NewImageOutput()},
			rejected("No images were returned. Return at least one image.")},
		{"invalid configuration", input{out: output.NewImageOutput().SetMaxWidth(-1)},
			expected{errorText: "output: image limits must not be negative"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			ctx := execution(t)
			calls := 0
			tt.input.out.SetCustomValidator("domain", func(
				_ *gent.ExecutionContext, value []gent.BinaryContent,
			) (*gent.OutputValidationResult, error) {
				calls++
				assert.Equal(t, tt.expected.result.Value, value)
				return &gent.OutputValidationResult{Accepted: true}, nil
			})
			result, err := tt.input.out.Process(ctx, tt.input.parts)
			assert.Equal(t, tt.expected.result, result)
			assert.Equal(t, tt.expected.calls, calls)
			if tt.expected.errorText != "" {
				assert.EqualError(t, err, tt.expected.errorText)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestImageOutputOwnershipAndConcurrentUse(t *testing.T) {
	value := imageFile(t, "image/png")
	allow := []string{"image/png"}
	var calls atomic.Int64
	out := output.NewImageOutput().SetAllowedMIMETypes(allow...).SetCustomValidator(
		"domain", func(_ *gent.ExecutionContext, images []gent.BinaryContent) (
			*gent.OutputValidationResult, error,
		) {
			calls.Add(1)
			assert.Equal(t, []gent.BinaryContent{value}, images)
			return &gent.OutputValidationResult{Accepted: true}, nil
		})
	allow[0] = "image/jpeg"
	var workers sync.WaitGroup
	for range 4 {
		ctx := execution(t)
		workers.Add(1)
		go func() {
			defer workers.Done()
			request := gent.NewModelCallRequest(nil)
			assert.NoError(t, out.Configure(&request))
			assert.Equal(t, []string{"image/png"}, request.Options.ImageMIMETypes)
			request.Options.ImageMIMETypes[0] = "image/jpeg"
			result, err := out.Process(ctx, []gent.ContentPart{value})
			if !assert.NoError(t, err) || !assert.True(t, result.Accepted) {
				return
			}
			assert.Equal(t, []gent.BinaryContent{value}, result.Value)
			result.Value[0].Data[0] = 99
			assert.Equal(t, []gent.ContentPart{value}, result.Content)
			result.Content[0].(gent.BinaryContent).Data[0] = 88
		}()
	}
	workers.Wait()
	assert.Equal(t, int64(4), calls.Load())
}
