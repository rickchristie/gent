package models

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"image"
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
			model := newLiveModel(provider, &config)
			t.Run("TextStream", func(t *testing.T) { checkLiveText(t, ctx, model) })
			t.Run("NativeTools", func(t *testing.T) { checkLiveTools(t, ctx, model) })
			checkLiveOutputs(t, ctx, model, provider.prefix != "DEEPSEEK")
		})
	}
}

func checkLiveText(t *testing.T, ctx context.Context, model gent.Model) {
	t.Helper()
	type expected struct {
		status   gent.ModelStatus
		text     string
		streamed bool
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
	for range stream.Chunks() {
		actual.streamed = true
	}
	response, err := stream.Response()
	if !assert.NoError(t, err) || !assert.Len(t, response.Choices, 1) {
		return
	}
	actual.status = response.Choices[0].Completion.Status
	out := output.NewTextOutput()
	execCtx := liveExecutionContext(ctx, "text", "Return the requested text.", "gent-live-ok")
	processed, err := out.Process(execCtx, response.Choices[0].CandidateContent)
	if !assert.NoError(t, err) || !assert.True(t, processed.Accepted) {
		return
	}
	actual.text = processed.Value
	assert.Equal(t, expected{gent.ModelStatusAnswer, "gent-live-ok", true}, actual)
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
		calls  []input
		value  string
		result *gent.ExecutionResult
	}
	// Only the real tool result contains this value. A guessed answer cannot pass the test.
	secret := make([]byte, 12)
	if _, err := rand.Read(secret); !assert.NoError(t, err) {
		return
	}
	record := fmt.Sprintf("record-%x", secret)
	var calls []input
	tool := gent.DefineTool("read_live_record", func(_ context.Context, value input) (string, error) {
		calls = append(calls, value)
		return record, nil
	}).Description("Read the record for the supplied id.").MustBuild()
	agent := react.NewAgent(model).RegisterTool(tool).WithCallOptions(gent.WithMaxTokens(2048))
	execCtx := liveExecutionContext(ctx, "native tools", "Use the tools to read records.",
		`Call read_live_record exactly once with id "test". Then return only its complete output.`)
	value, result, err := executor.NewRunner(agent, executor.DefaultConfig()).Execute(execCtx)
	if !assert.NoError(t, err) {
		return
	}
	want := expected{[]input{{ID: "test"}}, record, &gent.ExecutionResult{
		TerminationReason: gent.TerminationSuccess,
		Output:            []gent.ContentPart{gent.TextContent{Text: record}},
	}}
	assert.Equal(t, want, expected{calls, value, result})
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
			execCtx := liveExecutionContext(ctx, "image", out.Instructions(),
				"Generate one 512 by 512 image of a solid blue square. Return only the JPEG image.")
			agent := react.NewWithOutput(newLiveModel(provider, &config), out).
				WithCallOptions(gent.WithMaxTokens(2048))
			values, result, err := executor.NewRunner(agent, executor.DefaultConfig()).Execute(execCtx)
			if !assert.NoError(t, err) || !assert.Len(t, values, 1) {
				return
			}
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
