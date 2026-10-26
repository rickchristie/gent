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
		messages []ModelMessage
		options  CallOptions
		response *ContentResponse
		err      error
	}
	makeRequest := func(text, tool, mode string) ModelCallRequest {
		return NewModelCallRequest([]ModelMessage{TextParts(ChatMessageTypeHuman, text)},
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
