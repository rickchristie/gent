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
		h.change(before.Request)
	}
}

type requestOwnershipModel struct{ request *ModelCallRequest }

func (m *requestOwnershipModel) GenerateContentStream(_ context.Context,
	request *ModelCallRequest) (
	Stream, error,
) {
	m.request = request
	return NewCompletedStream(&ModelCallResponse{}, nil), nil
}

func TestManagedRequestHooksOwnNestedValues(t *testing.T) {
	type expected struct {
		messages []*ModelMessage
		options  CallOptions
		response *ModelCallResponse
		err      error
	}
	makeRequest := func(text, tool, mode string) *ModelCallRequest {
		return NewModelCallRequest([]*ModelMessage{TextParts(ChatMessageTypeHuman, text)},
			WithOptions(&CallOptions{
				StopWords: []string{text},
				Tools: []*ToolDefinition{{Type: "function", Function: &FunctionDefinition{
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
	changed.Options.SteeringMessages = DefaultSteeringMessages()
	want := expected{changed.Messages, changed.Options, &ModelCallResponse{}, nil}
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
			actual := expected{called: model.request != nil}
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

func TestRequestHookEditsToolMediaWithoutChangingSavedReply(t *testing.T) {
	makeRecord := func(text string, first byte) *ToolCallRecord {
		data, err := DataPart(map[string]any{"order": map[string]any{"id": text}})
		assert.NoError(t, err)
		parts := []ContentPart{data, PDFPart([]byte{first, 2, 3}), TextPart("Check the date.")}
		record, err := NewToolCallRecordBuilder("execution-1", "attempt-1", "receipt").
			Execution(&ToolExecutionResponse{Status: ToolExecuted, Parts: parts}).
			ModelReply("provider-1", "receipt", parts...).Build()
		assert.NoError(t, err)
		return record
	}
	original := makeRecord("O1", 1)
	want := makeRecord("O2", 9)
	ctx := NewExecutionContext(context.Background(), "tool-media", nil)
	defer ctx.Release()
	ctx.SetEventPublisher(&requestOwnershipHook{change: func(request *ModelCallRequest) {
		// The hook owns an editable request view. It can change nested content independently.
		reply := request.Messages[0].Parts[0].(*ModelToolCallResponse)
		reply.Parts()[0].(DataContent).Value().(map[string]any)["order"].(map[string]any)["id"] = "O2"
		reply.Parts()[1].(BinaryContent).Data[0] = 9
	}})
	model := &requestOwnershipModel{}
	request := NewModelCallRequest([]*ModelMessage{original.ModelMessage()})
	stream, err := ctx.GenerateContentStream(model, ModelCall{}, request)
	if !assert.NoError(t, err) {
		return
	}
	defer stream.Close()
	_, err = stream.Response()
	assert.NoError(t, err)
	assert.Equal(t, []*ModelMessage{want.ModelMessage()}, model.request.Messages)
	assert.Equal(t, makeRecord("O1", 1), original)
	assert.Equal(t, []*ModelMessage{original.ModelMessage()}, request.Messages)
}
func (m *requestOwnershipModel) PrepareRequest(ctx *ExecutionContext,
	request *ModelCallRequest, chain ToolChain,
) (*ModelCallRequest, error) {
	return PrepareModelRequest(ctx, request, chain)
}
