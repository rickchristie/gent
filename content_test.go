package gent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContentChoiceCompletion(t *testing.T) {
	type input struct{ choice *ContentChoice }
	type expected struct{ err string }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	tool := []ModelToolCall{{ID: "lookup", Type: "function",
		FunctionCall: &FunctionCall{Name: "lookup", Arguments: "{}"}}}
	cases := []testCase{
		{
			"answer",
			input{&ContentChoice{Completion: ModelCompletion{Status: ModelStatusAnswer}}},
			expected{},
		},
		{"unknown", input{&ContentChoice{}}, expected{`gent: unknown model completion ""`}},
		{"missing calls",
			input{
				&ContentChoice{Completion: ModelCompletion{Status: ModelStatusToolCalls}},
			},
			expected{"gent: tool completion has no calls"}},
		{"answer with calls", input{&ContentChoice{ToolCalls: tool,
			Completion: ModelCompletion{Status: ModelStatusAnswer}}},
			expected{"gent: answer completion contains tool calls"}},
		{"tools", input{&ContentChoice{ToolCalls: tool,
			Completion: ModelCompletion{Status: ModelStatusToolCalls}}}, expected{}},
		{"incomplete calls", input{&ContentChoice{ToolCalls: tool,
			Completion: ModelCompletion{Status: ModelStatusIncomplete}}}, expected{}},
		{"refused calls retained", input{&ContentChoice{ToolCalls: tool,
			Completion: ModelCompletion{Status: ModelStatusRefused, Refusal: &ModelRefusal{}}}}, expected{}},
		{"refusal missing",
			input{
				&ContentChoice{Completion: ModelCompletion{Status: ModelStatusRefused}},
			},
			expected{`gent: completion "refused" has inconsistent refusal data`}},
		{"refusal on answer", input{&ContentChoice{Completion: ModelCompletion{
			Status: ModelStatusAnswer, Refusal: &ModelRefusal{}}}},
			expected{`gent: completion "answer" has inconsistent refusal data`}},
		{"missing continuation",
			input{
				&ContentChoice{Completion: ModelCompletion{Status: ModelStatusContinue}},
			},
			expected{"gent: continuation has no provider data"}},
		{"continuation", input{&ContentChoice{Completion: ModelCompletion{Status: ModelStatusContinue},
			Continuation: &ProviderContinuation{Provider: "fixture"}}}, expected{}},
		{
			"failed",
			input{&ContentChoice{Completion: ModelCompletion{Status: ModelStatusFailed}}},
			expected{},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			actual := expected{}
			if err := tt.input.choice.ValidateCompletion(); err != nil {
				actual.err = err.Error()
			}
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestMetadataSubscribersOwnTheirChunks(t *testing.T) {
	ctx := NewExecutionContext(context.Background(), "root", nil)
	defer ctx.Release()
	first, stopFirst := ctx.SubscribeAll()
	defer stopFirst()
	second, stopSecond := ctx.SubscribeAll()
	defer stopSecond()
	meta := &MetaContent{
		Type:      "progress",
		Content:   []ContentPart{TextContent{Text: "Checking."}},
		MessageId: "message",
	}
	assert.NoError(t, ctx.EmitChunk(StreamChunk{MetaContent: []*MetaContent{meta}}))
	meta.Content = []ContentPart{TextContent{Text: "producer changed entry"}}
	one := <-first
	one.MetaContent[0].Content = []ContentPart{TextContent{Text: "subscriber changed entry"}}
	two := <-second
	want := []*MetaContent{
		{Type: "progress", Content: []ContentPart{TextContent{Text: "Checking."}}, MessageId: "message"},
	}
	assert.Equal(t, want, two.MetaContent)
	assert.NoError(t, ctx.FlushProgress())
	view, err := ctx.Export()
	assert.NoError(t, err)
	value, err := ctx.DecodeEvent(view.Activity[0])
	assert.NoError(t, err)
	assert.Equal(t, two, value.(StreamChunk))
}

func TestStreamOwnsMetadataAndResponse(t *testing.T) {
	meta := []*MetaContent{
		{
			Type:      MetaContentReasoning,
			Content:   []ContentPart{TextContent{Text: "Check."}},
			MessageId: "reason",
		},
		{
			Type:      "progress",
			Content:   []ContentPart{TextContent{Text: "Half done."}},
			MessageId: "progress",
		},
		{
			Type:      MetaContentIntermediary,
			Content:   []ContentPart{TextContent{Text: "Answer."}},
			MessageId: "answer",
		},
	}
	response := &ContentResponse{Choices: []*ContentChoice{{Completion: ModelCompletion{
		Status: ModelStatusAnswer,
	},
		CandidateContent: []ContentPart{
			TextContent{Text: "Answer."},
		},
		MetaContent: CloneMetaContent(meta), CandidateSource: &ContentSource{
			MessageIds: []string{
				"answer",
			},
		},
		Continuation: &ProviderContinuation{Provider: "fixture",
			Items: []json.RawMessage{json.RawMessage(`{"type":"message"}`)}}}},
		Info: &GenerationInfo{RawGenerationInfo: map[string]any{"usage": map[string]any{"tokens": 2}}}}
	want, err := cloneOwned(NewCodecRegistry(), response)
	assert.NoError(t, err)
	stream := NewStreamWithDuration()
	stream.Send(StreamChunk{MetaContent: meta})
	stream.Complete(response, nil)
	meta[0].Content = []ContentPart{TextContent{Text: "producer changed delta"}}
	response.Choices[0].MetaContent[0].Content = []ContentPart{
		TextContent{Text: "producer changed response"},
	}
	response.Choices[0].CandidateSource.MessageIds[0] = "changed"
	response.Info.RawGenerationInfo["usage"].(map[string]any)["tokens"] = 999
	response.Choices[0].Continuation.Items[0][0] = '!'
	chunks := []StreamChunk{}
	for chunk := range stream.Chunks() {
		chunks = append(chunks, chunk)
	}
	assert.Equal(t, []StreamChunk{{MetaContent: want.Choices[0].MetaContent}}, chunks)
	chunks[0].MetaContent[0].Content = []ContentPart{TextContent{Text: "subscriber changed delta"}}
	assert.Equal(t, want.Choices[0].MetaContent, stream.AccumulatedMetaContent())
	first, err := stream.Response()
	assert.NoError(t, err)
	assert.Equal(t, want, first)
	first.Choices[0].MetaContent[0].Content = []ContentPart{
		TextContent{Text: "reader changed response"},
	}
	first.Choices[0].CandidateSource.MessageIds[0] = "changed"
	first.Info.RawGenerationInfo["usage"].(map[string]any)["tokens"] = 999
	first.Choices[0].Continuation.Items[0][0] = '!'
	second, err := stream.Response()
	assert.NoError(t, err)
	assert.Equal(t, want, second)
}
