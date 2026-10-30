package gent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewTaskPreservesParts(t *testing.T) {
	type input struct{ parts []ContentPart }
	type expected struct{ task *Task }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"empty", input{}, expected{&Task{Messages: []*ModelMessage{{Role: ChatMessageTypeHuman}}}}},
		{"ordered media", input{[]ContentPart{TextContent{Text: "Read both."},
			&BinaryContent{MIMEType: "application/pdf", Data: []byte{0, 1, 255}},
			ImageURLContent{URL: "https://example.org/image.png"}}},
			expected{&Task{Messages: []*ModelMessage{{Role: ChatMessageTypeHuman,
				Parts: []ContentPart{TextContent{Text: "Read both."},
					&BinaryContent{MIMEType: "application/pdf", Data: []byte{0, 1, 255}},
					ImageURLContent{URL: "https://example.org/image.png"}}}}}}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected.task, NewTask(tt.input.parts...))
		})
	}
}

func TestTaskPointersAndMediaRemainOwnedAcrossRequests(t *testing.T) {
	makeTask := func() *Task {
		return &Task{Messages: []*ModelMessage{
			TextParts(ChatMessageTypeSystem, "Use the attached evidence."),
			{Role: ChatMessageTypeHuman, Parts: []ContentPart{
				&TextContent{Text: "Read the PDF."},
				&BinaryContent{MIMEType: "application/pdf", Data: []byte{0, 7, 255}},
				BinaryContent{MIMEType: "image/png", Data: []byte{1, 2, 3}},
			}},
			{Role: ChatMessageTypeAI, Continuation: &ProviderContinuation{Provider: "fixture",
				Items: []json.RawMessage{json.RawMessage(`{"signature":"original"}`)}}},
		}}
	}
	task := makeTask()
	ctx := NewExecutionContext(context.Background(), "root", NewBasicLoopData(task))
	defer ctx.Release()
	task.Messages[0].Role = ChatMessageTypeHuman
	task.Messages[1].Parts[1].(*BinaryContent).Data[0] = 99
	task.Messages[2].Continuation.Items[0][0] = '!'
	ctx.SetEventPublisher(&requestOwnershipHook{change: func(request *ModelCallRequest) {
		request.Messages[0].Parts[0] = TextContent{Text: "Request only."}
		request.Messages[1].Parts[1].(*BinaryContent).Data[0] = 88
		request.Messages[1].Parts[2].(BinaryContent).Data[0] = 77
		request.Messages[2].Continuation.Items[0][14] = 'c'
	}})
	owned, err := ctx.Task()
	assert.NoError(t, err)
	assert.Equal(t, makeTask(), owned)
	model := &requestOwnershipModel{}
	stream, err := ctx.GenerateContentStream(model, ModelCall{}, NewModelCallRequest(owned.Messages))
	if !assert.NoError(t, err) {
		return
	}
	defer stream.Close()
	_, err = stream.Response()
	assert.NoError(t, err)
	assert.Equal(t, makeTask(), owned)
	actual, err := ctx.Task()
	assert.NoError(t, err)
	assert.Equal(t, makeTask(), actual)
	ctx.SetTermination(TerminationSuccess, nil, nil)
	assert.NoError(t, ctx.CommitCheckpoint(PhaseTerminal))
	view, err := ctx.Export()
	assert.NoError(t, err)
	restored, err := RestoreExecutionContext(context.Background(), view, RestoreBindings{})
	if !assert.NoError(t, err) {
		return
	}
	defer restored.Release()
	actual, err = restored.Task()
	assert.NoError(t, err)
	assert.Equal(t, makeTask(), actual)
}

func TestBinaryStreamAndTerminalContentRestore(t *testing.T) {
	parts := []ContentPart{TextContent{Text: "Generated files."},
		&BinaryContent{MIMEType: "image/png", Data: []byte{0, 1, 255}},
		BinaryContent{MIMEType: "application/pdf", Data: []byte{3, 4, 5}},
		TextContent{Text: "Done."}}
	want := CloneContentParts(parts)
	ctx := NewExecutionContext(context.Background(), "root", nil)
	defer ctx.Release()
	chunks, unsubscribe := ctx.SubscribeAll()
	defer unsubscribe()
	assert.NoError(t, ctx.EmitChunk(StreamChunk{MetaContent: []*MetaContent{
		{Type: MetaContentIntermediary, Content: parts}}}))
	parts[1].(*BinaryContent).Data[0] = 99
	preview := <-chunks
	assert.Equal(t, want, preview.MetaContent[0].Content)
	preview.MetaContent[0].Content[1].(*BinaryContent).Data[0] = 88
	ctx.SetTermination(TerminationSuccess, CloneContentParts(want), nil)
	assert.NoError(t, ctx.CommitCheckpoint(PhaseTerminal))
	accepted := <-chunks
	assert.Equal(t, want, accepted.Content)
	accepted.Content[1].(*BinaryContent).Data[0] = 77
	view, err := ctx.Export()
	assert.NoError(t, err)
	assert.NoError(t, ValidateBundle(view))
	var savedPreview, savedAccepted []ContentPart
	for _, event := range view.Activity {
		value, err := ctx.DecodeEvent(event)
		assert.NoError(t, err)
		if chunk, ok := value.(StreamChunk); ok {
			if event.Type == "execution_content" {
				savedAccepted = chunk.Content
			}
			if event.Type == "model_stream_chunk" {
				savedPreview = chunk.MetaContent[0].Content
			}
		}
	}
	assert.Equal(t, want, savedPreview)
	assert.Equal(t, want, savedAccepted)
	restored, err := RestoreExecutionContext(context.Background(), view, RestoreBindings{})
	if !assert.NoError(t, err) {
		return
	}
	defer restored.Release()
	assert.Equal(t, want, restored.Result().Output)
}
