package output_test

import (
	"context"
	"errors"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/output"
	"github.com/stretchr/testify/assert"
)

func execution(t *testing.T) *gent.ExecutionContext {
	t.Helper()
	ctx := gent.NewExecutionContext(context.Background(), "output", gent.NewBasicLoopData(nil))
	t.Cleanup(ctx.Release)
	return ctx
}

func text(value string) []gent.ContentPart {
	return []gent.ContentPart{gent.TextContent{Text: value}}
}

func TestTextOutput(t *testing.T) {
	type input struct {
		parts     []gent.ContentPart
		validator *gent.OutputValidationResult
		failure   error
		custom    bool
	}
	type expected struct {
		result    *gent.OutputResult[string]
		calls     int
		errorText string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"literal tags", input{parts: text("<answer>Keep all text.</answer>")}, expected{
			result: &gent.OutputResult[string]{Accepted: true, Value: "<answer>Keep all text.</answer>",
				Content: text("<answer>Keep all text.</answer>")}}},
		{"ordered parts", input{parts: []gent.ContentPart{
			&gent.TextContent{Text: "first "}, gent.TextContent{Text: "second"},
		}}, expected{result: &gent.OutputResult[string]{Accepted: true, Value: "first second",
			Content: []gent.ContentPart{gent.TextContent{Text: "first "},
				gent.TextContent{Text: "second"}}}}},
		{"empty text", input{parts: text("")}, expected{result: &gent.OutputResult[string]{
			Accepted: true, Content: text("")}}},
		{"missing parts", input{}, expected{result: &gent.OutputResult[string]{
			Steering: text("The answer must contain text only. Return a text answer.")}}},
		{"binary rejection skips custom check", input{parts: []gent.ContentPart{
			gent.BinaryContent{MIMEType: "image/png", Data: []byte{1}},
		}, custom: true}, expected{result: &gent.OutputResult[string]{
			Steering: text("The answer must contain text only. Return a text answer.")}}},
		{"custom acceptance", input{parts: text("yes"), custom: true,
			validator: &gent.OutputValidationResult{Accepted: true}}, expected{calls: 1,
			result: &gent.OutputResult[string]{Accepted: true, Value: "yes", Content: text("yes")}}},
		{"custom rejection", input{parts: text("yes"), custom: true,
			validator: &gent.OutputValidationResult{Steering: text("Supply the order number.")}},
			expected{calls: 1, result: &gent.OutputResult[string]{
				Steering: text("Supply the order number.")}}},
		{"custom operation error", input{parts: text("yes"), custom: true,
			failure: errors.New("database unavailable")}, expected{
			calls: 1, errorText: "database unavailable"}},
		{"missing validation result", input{parts: text("yes"), custom: true}, expected{
			calls: 1, errorText: "output: custom validator returned no result"}},
		{"empty steering", input{parts: text("yes"), custom: true,
			validator: &gent.OutputValidationResult{Steering: text(" ")}}, expected{
			calls: 1, errorText: "output: rejected validation requires useful steering"}},
		{"accepted steering", input{parts: text("yes"), custom: true,
			validator: &gent.OutputValidationResult{Accepted: true, Steering: text("Change it.")}},
			expected{calls: 1, errorText: "output: accepted validation must not contain steering"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			ctx := execution(t)
			out := output.NewTextOutput()
			calls := 0
			if tt.input.custom {
				out.SetCustomValidator("domain", func(
					_ *gent.ExecutionContext, value string,
				) (*gent.OutputValidationResult, error) {
					calls++
					assert.Equal(t, "yes", value)
					return tt.input.validator, tt.input.failure
				})
			}
			result, err := out.Process(ctx, tt.input.parts)
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

func TestTextOutputConfigurationAndCancellation(t *testing.T) {
	out := output.NewTextOutput()
	request := gent.NewModelCallRequest([]*gent.ModelMessage{
		gent.TextParts(gent.ChatMessageTypeSystem, "Follow the business rules."),
		gent.TextParts(gent.ChatMessageTypeHuman, "Complete the order."),
	})
	original := request
	expected := original
	expected.Options.ResponseMIMEType = "text/plain"
	expected.Options.ResponseModalities = []string{"text"}
	assert.NoError(t, out.Configure(&request))
	assert.Equal(t, expected, request)
	ctx := execution(t)
	ctx.CancelRun(context.Canceled)
	result, err := out.Process(ctx, text("candidate"))
	assert.Nil(t, result)
	assert.ErrorIs(t, err, context.Canceled)
}
