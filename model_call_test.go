package gent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

type requestOwnershipHook struct{ change func(*ModelCallRequest) }

func (h *requestOwnershipHook) MaxRecursion() int { return 10 }
func (h *requestOwnershipHook) Dispatch(_ *ExecutionContext, event Event) {
	if before, ok := event.(*BeforeModelCallEvent); ok {
		h.change(&before.Request)
	}
}

type requestOwnershipModel struct{ request ModelCallRequest }

func (m *requestOwnershipModel) GenerateContentStream(_ context.Context, request ModelCallRequest) (
	Stream, error,
) {
	m.request = request
	return NewCompletedStream(&ContentResponse{}, nil), nil
}

func TestManagedRequestHooksOwnNestedValues(t *testing.T) {
	type expected struct {
		messages []*ModelMessage
		options  CallOptions
		response *ContentResponse
		err      error
	}
	makeRequest := func(text, tool, mode string) ModelCallRequest {
		return NewModelCallRequest([]*ModelMessage{TextParts(ChatMessageTypeHuman, text)},
			WithOptions(&CallOptions{
				StopWords: []string{text},
				Tools: []ToolDefinition{{Type: "function", Function: &FunctionDefinition{
					Name: tool, Parameters: map[string]any{"required": []string{text}},
				}}},
				ToolChoice: &ToolChoice{Type: "function", Function: &FunctionReference{Name: tool}},
				Metadata: map[string]any{
					"thinking_config": &ThinkingConfig{Mode: ThinkingMode(mode)},
					"responses":       map[string]any{"reasoning": map[string]any{"effort": mode}},
				},
			}))
	}
	request := makeRequest("original", "lookup", "high")
	changed := makeRequest("changed", "read", "low")
	want := expected{changed.Messages, changed.Options, &ContentResponse{}, nil}
	ctx := NewExecutionContext(context.Background(), "fixture", nil)
	ctx.SetEventPublisher(&requestOwnershipHook{change: func(request *ModelCallRequest) {
		request.Messages[0].Parts[0] = TextContent{Text: "changed"}
		o := &request.Options
		o.StopWords[0] = "changed"
		o.Tools[0].Function.Name = "read"
		o.Tools[0].Function.Parameters.(map[string]any)["required"].([]string)[0] = "changed"
		o.ToolChoice.(*ToolChoice).Function.Name = "read"
		o.Metadata["thinking_config"].(*ThinkingConfig).Mode = "low"
		o.Metadata["responses"].(map[string]any)["reasoning"].(map[string]any)["effort"] = "low"
	}})
	model := &requestOwnershipModel{}
	stream, err := ctx.GenerateContentStream(model, ModelCall{}, request)
	if !assert.NoError(t, err) {
		return
	}
	defer stream.Close()
	for range stream.Chunks() {
	}
	response, err := stream.Response()
	assert.Equal(t, want, expected{model.request.Messages, model.request.Options, response, err})
	assert.Equal(t, makeRequest("original", "lookup", "high"), request)
}

func TestRequestHooksRetainOutputControls(t *testing.T) {
	type input struct{ change func(*ModelCallRequest) }
	type expected struct {
		err    string
		called bool
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	failure := "gent: model request hook changed required output controls"
	cases := []testCase{
		{"sampling", input{func(r *ModelCallRequest) { r.Options.MaxTokens = 100 }},
			expected{called: true}},
		{"schema removed", input{func(r *ModelCallRequest) { r.Options.ResponseSchema = nil }},
			expected{err: failure}},
		{"nested schema weakened", input{func(r *ModelCallRequest) {
			r.Options.ResponseSchema["properties"].(map[string]any)["name"] = map[string]any{}
		}}, expected{err: failure}},
		{"modality changed", input{func(r *ModelCallRequest) {
			r.Options.ResponseModalities[0] = "image"
		}}, expected{err: failure}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			ctx := NewExecutionContext(context.Background(), "root", nil)
			defer ctx.Release()
			ctx.SetEventPublisher(&requestOwnershipHook{change: tt.input.change})
			request := NewModelCallRequest([]*ModelMessage{TextParts(ChatMessageTypeHuman, "Name.")})
			request.Options.ResponseSchema = map[string]any{"type": "object",
				"properties": map[string]any{"name": map[string]any{"type": "string"}}}
			request.Options.ResponseMIMEType = "application/json"
			request.Options.ResponseModalities = []string{"text"}
			model := &requestOwnershipModel{}
			stream, err := ctx.GenerateContentStream(model, ModelCall{}, request)
			actual := expected{called: model.request.Messages != nil}
			if err != nil {
				actual.err = err.Error()
			}
			assert.Equal(t, tt.expected, actual)
			if stream != nil {
				_, err := stream.Response()
				assert.NoError(t, err)
				stream.Close()
			}
		})
	}
}
