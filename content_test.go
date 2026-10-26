package gent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContentChoiceState(t *testing.T) {
	type input struct {
		reason string
		tools  bool
	}
	type expected struct{ state string }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"custom adapter", input{}, expected{ModelAnswer}},
		{"ordinary answer", input{reason: "stop"}, expected{ModelAnswer}},
		{"Anthropic answer", input{reason: "end_turn"}, expected{ModelAnswer}},
		{"refusal", input{reason: "refusal"}, expected{ModelAnswer}},
		{"tool request", input{"tool_calls", true}, expected{ModelTools}},
		{"Anthropic tools", input{"tool_use", true}, expected{ModelTools}},
		{"empty tool request", input{reason: "tool_calls"}, expected{ModelFailed}},
		{"token limit with tools", input{"length", true}, expected{ModelIncomplete}},
		{"output limit", input{reason: "max_output_tokens"}, expected{ModelIncomplete}},
		{"context limit", input{reason: "model_context_window_exceeded"}, expected{ModelIncomplete}},
		{"continuation", input{reason: "pause_turn"}, expected{ModelContinue}},
		{"filtered", input{reason: "content_filter"}, expected{ModelFailed}},
		{"unknown", input{reason: "new_reason"}, expected{ModelFailed}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			choice := &ContentChoice{StopReason: test.input.reason}
			if test.input.tools {
				choice.ToolCalls = []ModelToolCall{{ID: "lookup", Type: "function",
					FunctionCall: &FunctionCall{Name: "lookup", Arguments: "{}"}}}
			}
			assert.Equal(t, test.expected, expected{choice.State()})
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
	meta := &MetaContent{Type: "progress", Text: "Checking.", MessageId: "message"}
	assert.NoError(t, ctx.EmitChunk(StreamChunk{MetaContent: []*MetaContent{meta}}))
	meta.Text = "producer changed entry"
	one := <-first
	one.MetaContent[0].Text = "subscriber changed entry"
	two := <-second
	want := []*MetaContent{{Type: "progress", Text: "Checking.", MessageId: "message"}}
	assert.Equal(t, want, two.MetaContent)
	view, err := ctx.Export()
	assert.NoError(t, err)
	value, err := ctx.DecodeEvent(view.Activity[0])
	assert.NoError(t, err)
	assert.Equal(t, two, value.(StreamChunk))
}

func TestStreamOwnsMetadataAndResponse(t *testing.T) {
	meta := []*MetaContent{
		{Type: MetaContentReasoning, Text: "Check.", MessageId: "reason"},
		{Type: "progress", Text: "Half done.", MessageId: "progress"},
		{Type: MetaContentIntermediary, Text: "Answer.", MessageId: "answer"},
	}
	response := &ContentResponse{Choices: []*ContentChoice{{CandidateContent: "Answer.",
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
	meta[0].Text = "producer changed delta"
	response.Choices[0].MetaContent[0].Text = "producer changed response"
	response.Choices[0].CandidateSource.MessageIds[0] = "changed"
	response.Info.RawGenerationInfo["usage"].(map[string]any)["tokens"] = 999
	response.Choices[0].Continuation.Items[0][0] = '!'
	chunks := []StreamChunk{}
	for chunk := range stream.Chunks() {
		chunks = append(chunks, chunk)
	}
	assert.Equal(t, []StreamChunk{{MetaContent: want.Choices[0].MetaContent}}, chunks)
	chunks[0].MetaContent[0].Text = "subscriber changed delta"
	assert.Equal(t, want.Choices[0].MetaContent, stream.AccumulatedMetaContent())
	first, err := stream.Response()
	assert.NoError(t, err)
	assert.Equal(t, want, first)
	first.Choices[0].MetaContent[0].Text = "reader changed response"
	first.Choices[0].CandidateSource.MessageIds[0] = "changed"
	first.Info.RawGenerationInfo["usage"].(map[string]any)["tokens"] = 999
	first.Choices[0].Continuation.Items[0][0] = '!'
	second, err := stream.Response()
	assert.NoError(t, err)
	assert.Equal(t, want, second)
}
