package models

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"image"
	"strings"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents/react"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/output"
	"github.com/stretchr/testify/assert"
)

// These tests use the public service or the configured runtime, with no HTTP fixtures. Keep
// prompts small and run providers concurrently. Each provider has its own time and loop limits.
func TestLiveProviders(t *testing.T) {
	for _, provider := range liveProviders() {
		t.Run(provider.name, func(t *testing.T) {
			config := liveConfig(t, provider, false)
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			model := newLiveModel(t, provider, &config)
			t.Run("TextStream", func(t *testing.T) { checkLiveText(t, ctx, model) })
			t.Run("NativeTools", func(t *testing.T) { checkLiveTools(t, ctx, model) })
			checkLiveOutputs(t, ctx, model, provider.prefix != "DEEPSEEK")
		})
	}
}

type liveTextCheck interface {
	assert.TestingT
	Helper()
}

func checkLiveText(t liveTextCheck, ctx context.Context, model gent.Model) {
	t.Helper()
	type expected struct {
		status  gent.ModelStatus
		text    string
		preview string
	}
	request := gent.NewModelCallRequest([]*gent.ModelMessage{
		gent.TextParts(gent.ChatMessageTypeSystem, "Follow the user's output instructions exactly."),
		gent.TextParts(gent.ChatMessageTypeHuman, "Reply with exactly gent-live-ok and nothing else."),
	}, gent.WithMaxTokens(2048))
	stream, err := model.GenerateContentStream(ctx, request)
	if !assert.NoError(t, err) {
		return
	}
	defer stream.Close()
	actual := expected{}
	var preview strings.Builder
	for chunk := range stream.Chunks() {
		assert.NoError(t, chunk.Err)
		assert.Empty(t, chunk.Content, "provider output is provisional until validation")
		preview.WriteString(gent.MetaText(chunk.MetaContent, gent.MetaContentIntermediary))
	}
	actual.preview = preview.String()
	response, err := stream.Response()
	if !assert.NoError(t, err) || !assert.Len(t, response.Choices, 1) {
		return
	}
	actual.status = response.Choices[0].Completion.Status
	out := output.NewTextOutput()
	execCtx := liveExecutionContext(ctx, "text", "Return the requested text.", "gent-live-ok")
	defer execCtx.Release()
	processed, err := out.Process(execCtx, response.Choices[0].CandidateContent)
	if !assert.NoError(t, err) || !assert.True(t, processed.Accepted) {
		return
	}
	actual.text = processed.Value
	assert.Equal(t, expected{gent.ModelStatusAnswer, "gent-live-ok", "gent-live-ok"}, actual)
	if assert.NotNil(t, response.Info) {
		assert.Greater(t, response.Info.InputTokens, 0)
		assert.Greater(t, response.Info.OutputTokens, 0)
		assert.Equal(t, response.Info.InputTokens+response.Info.OutputTokens, response.Info.TotalTokens)
	}
}

func liveExecutionContext(ctx context.Context, name, system, prompt string) *gent.ExecutionContext {
	execCtx := gent.NewExecutionContext(ctx, "live "+name,
		gent.NewBasicLoopData(&gent.Task{Messages: []*gent.ModelMessage{
			gent.TextParts(gent.ChatMessageTypeSystem, system),
			gent.TextParts(gent.ChatMessageTypeHuman, prompt),
		}}))
	execCtx.SetLimits([]gent.Limit{{Type: gent.LimitExactKey, Key: gent.SCIterations, MaxValue: 4}})
	return execCtx
}

func checkLiveTools(t *testing.T, ctx context.Context, model gent.Model) {
	t.Helper()
	type input struct {
		ID string `json:"id"`
	}
	type expected struct {
		calls       []input
		value, text string
		termination gent.TerminationReason
	}
	// Only the real tool result contains this value. A guessed answer cannot pass the test.
	secret := make([]byte, 12)
	if _, err := rand.Read(secret); !assert.NoError(t, err) {
		return
	}
	record := fmt.Sprintf("record-%x", secret)
	var calls []input
	tool := gent.DefineTool("read_live_record", func(_ context.Context, value input) (
		*gent.ToolExecutionResponse, error,
	) {
		calls = append(calls, value)
		return capturedToolResponse(record, nil)
	}).Description("Read the record for the supplied id.").MustBuild()
	out := output.NewTextOutput().SetCustomValidator("live-tool-result",
		func(_ *gent.ExecutionContext, value string) (*gent.OutputValidationResult, error) {
			if value != record {
				// Do not put the random record in steering. The model must use the real tool result,
				// including when its first answer fails to copy that result correctly.
				return &gent.OutputValidationResult{Steering: []gent.ContentPart{gent.TextPart(
					"Read the saved tool result again. Return its exact text without other text.")}}, nil
			}
			return &gent.OutputValidationResult{Accepted: true}, nil
		})
	agent := react.NewWithOutput(model, out).RegisterTool(tool).
		WithCallOptions(gent.WithMaxTokens(2048))
	execCtx := liveExecutionContext(ctx, "native tools", "Use the tools to read records.",
		`Call read_live_record exactly once with id "test". Return only the record text it supplies,
without quotes, JSON, or formatting.`)
	value, result, err := executor.NewRunner(agent, executor.DefaultConfig()).Execute(execCtx)
	if !assert.NoError(t, err) {
		return
	}
	// Providers can split text into several content parts. Validate every part and the full text;
	// a fixed part count would test transport chunk boundaries instead of the accepted output.
	// The runner has closed execCtx, so this independent check needs its own context.
	checkCtx := liveExecutionContext(ctx, "record output", "Read the record text.", "")
	defer checkCtx.Release()
	processed, err := output.NewTextOutput().Process(checkCtx, result.Output)
	if !assert.NoError(t, err) || !assert.True(t, processed.Accepted) {
		return
	}
	assert.Equal(t, result.Output, processed.Content)
	want := expected{[]input{{ID: "test"}}, record, record, gent.TerminationSuccess}
	assert.Equal(t, want, expected{calls, value, processed.Value, result.TerminationReason})
	// Check the full tool result presented on each follow-up call. This distinguishes a lost
	// result from a model answer that does not use the result.
	for _, event := range execCtx.Events() {
		before, ok := event.(*gent.BeforeModelCallEvent)
		if !ok || before.Iteration == 1 {
			continue
		}
		var replies [][]gent.ContentPart
		for _, message := range before.Request.Messages {
			for _, part := range message.Parts {
				if reply, ok := part.(*gent.ModelToolCallResponse); ok {
					replies = append(replies, reply.Parts())
				}
			}
		}
		assert.Equal(t, [][]gent.ContentPart{{gent.TextPart(record)}}, replies)
	}
}

type liveOutputValue struct {
	Value int `json:"value"`
}

func checkLiveOutputs(t *testing.T, ctx context.Context, model gent.Model, native bool) {
	t.Helper()
	type input struct {
		build func(gent.OutputCustomValidator[liveOutputValue]) (gent.Output[liveOutputValue], error)
	}
	type expected struct {
		value       liveOutputValue
		validations []liveOutputValue
		native      []bool
		rejected    int64
		result      *gent.ExecutionResult
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	want := expected{liveOutputValue{2}, []liveOutputValue{{1}, {2}}, []bool{native, native}, 1,
		&gent.ExecutionResult{TerminationReason: gent.TerminationSuccess,
			Output: []gent.ContentPart{gent.TextContent{Text: `{"value":2}`}}}}
	cases := []testCase{
		{"JsonOutput", input{func(validator gent.OutputCustomValidator[liveOutputValue]) (
			gent.Output[liveOutputValue], error,
		) {
			out, err := output.NewJsonOutput[liveOutputValue]()
			if err != nil {
				return nil, err
			}
			return out.SetCustomValidator("live-correction", validator), nil
		}}, want},
		{"SchemaOutput", input{func(validator gent.OutputCustomValidator[liveOutputValue]) (
			gent.Output[liveOutputValue], error,
		) {
			out, err := output.NewSchemaOutput[liveOutputValue](providerOutputSchema())
			if err != nil {
				return nil, err
			}
			return out.SetCustomValidator("live-correction", validator), nil
		}}, want},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			actual := expected{}
			out, err := test.input.build(func(_ *gent.ExecutionContext, value liveOutputValue) (
				*gent.OutputValidationResult, error,
			) {
				actual.validations = append(actual.validations, value)
				if len(actual.validations) == 1 || value.Value != 2 {
					return &gent.OutputValidationResult{Steering: []gent.ContentPart{gent.TextContent{
						Text: `The required value is now 2. Return {"value":2}.`,
					}}}, nil
				}
				return &gent.OutputValidationResult{Accepted: true}, nil
			})
			if !assert.NoError(t, err) {
				return
			}
			execCtx := liveExecutionContext(ctx, test.name, out.Instructions(), `Return {"value":1}.`)
			agent := react.NewWithOutput(model, out).WithCallOptions(gent.WithMaxTokens(2048))
			actual.value, actual.result, err = executor.NewRunner(agent,
				executor.DefaultConfig()).Execute(execCtx)
			if !assert.NoError(t, err) {
				return
			}
			for _, event := range execCtx.Events() {
				after, ok := event.(*gent.AfterModelCallEvent)
				if !ok {
					continue
				}
				fields, ok := after.Response.Info.RawGenerationInfo["output_schema"].(map[string]any)
				if !assert.True(t, ok, "schema enforcement decision is required") {
					return
				}
				enforced, ok := fields["native"].(bool)
				if !assert.True(t, ok, "native enforcement decision is required") {
					return
				}
				actual.native = append(actual.native, enforced)
			}
			actual.rejected = execCtx.Stats().GetCounter(gent.SCOutputCustomRejectedTotal)
			assert.Equal(t, test.expected, actual)
		})
	}
}

// Image models are separate because text-only models cannot exercise image generation. Use a
// Responses model with the image tool for OpenAI/xAI, and an image model for Gemini/OpenRouter.
func TestLiveImages(t *testing.T) {
	for _, provider := range liveProviders() {
		if !provider.images {
			continue
		}
		t.Run(provider.name, func(t *testing.T) {
			config := liveConfig(t, provider, true)
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			out := output.NewImageOutput().SetAllowedMIMETypes("image/jpeg").
				SetMaxFileSizeBytes(8 << 20).SetMaxWidth(2048).SetMaxHeight(2048)
			validations := 0
			out.SetCustomValidator("live-image-correction", func(_ *gent.ExecutionContext,
				_ []gent.BinaryContent,
			) (*gent.OutputValidationResult, error) {
				validations++
				if validations == 1 {
					return &gent.OutputValidationResult{Steering: []gent.ContentPart{gent.TextPart(
						"Generate the blue square again. Return only the JPEG image, with no text.")}}, nil
				}
				return &gent.OutputValidationResult{Accepted: true}, nil
			})
			execCtx := liveExecutionContext(ctx, "image", out.Instructions(),
				`Generate one 512 by 512 image of a solid blue square. Return only the JPEG image.
The image_generation tool output is the complete answer. Do not send any assistant text,
including a caption, a link, a description, or a completion message.`)
			defer logLiveImageFailure(t, execCtx)
			agent := react.NewWithOutput(newLiveModel(t, provider, &config), out).
				WithCallOptions(gent.WithMaxTokens(2048))
			values, result, err := executor.NewRunner(agent, executor.DefaultConfig()).Execute(execCtx)
			if !assert.NoError(t, err) || !assert.Len(t, values, 1) {
				return
			}
			assert.Equal(t, 2, validations)
			assert.Equal(t, int64(1), execCtx.Stats().GetCounter(gent.SCOutputCustomRejectedTotal))
			assert.Equal(t, &gent.ExecutionResult{TerminationReason: gent.TerminationSuccess,
				Output: []gent.ContentPart{values[0]}}, result)
			assert.Equal(t, "image/jpeg", values[0].MIMEType)
			assert.Greater(t, len(values[0].Data), 0)
			assert.LessOrEqual(t, len(values[0].Data), 8<<20)
			decoded, format, err := image.DecodeConfig(bytes.NewReader(values[0].Data))
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, "jpeg", format)
			assert.Greater(t, decoded.Width, 0)
			assert.Greater(t, decoded.Height, 0)
			assert.LessOrEqual(t, decoded.Width, 2048)
			assert.LessOrEqual(t, decoded.Height, 2048)
		})
	}
}

// Report validation and part sizes so a live failure can be diagnosed without image bytes,
// provider continuation data, or credentials in the test log.
func logLiveImageFailure(t *testing.T, ctx *gent.ExecutionContext) {
	t.Helper()
	if !t.Failed() {
		return
	}
	for _, event := range ctx.Events() {
		switch event := event.(type) {
		case *gent.OutputValidationEvent:
			t.Logf("image validation: iteration=%d stage=%s accepted=%t steering=%q error=%v",
				event.Iteration, event.Stage, event.Accepted, gent.ContentText(event.Steering), event.Error)
		case *gent.AfterModelCallEvent:
			if event.Error != nil {
				t.Logf("image call: iteration=%d error=%v", event.Iteration, event.Error)
				continue
			}
			for _, choice := range event.Response.Choices {
				var parts []string
				for _, part := range choice.CandidateContent {
					switch part := part.(type) {
					case gent.BinaryContent:
						parts = append(parts, fmt.Sprintf("%s: %d bytes", part.MIMEType, len(part.Data)))
					case gent.TextContent:
						parts = append(parts, fmt.Sprintf("text: %d bytes", len(part.Text)))
						t.Logf("image text: %q", part.Text)
					default:
						parts = append(parts, fmt.Sprintf("%T", part))
					}
				}
				t.Logf("image completion: iteration=%d status=%s reason=%q parts=%v",
					event.Iteration, choice.Completion.Status, choice.Completion.ProviderReason, parts)
			}
		}
	}
}
