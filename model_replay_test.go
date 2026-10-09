package gent_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func readModelCall(
	t *testing.T, ctx *gent.ExecutionContext, model gent.Model, call gent.ModelCall,
) (
	*gent.ContentResponse, []gent.StreamChunk,
) {
	t.Helper()
	stream, err := ctx.GenerateContentStream(model, call, gent.NewModelCallRequest(nil))
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	defer stream.Close()
	var chunks []gent.StreamChunk
	for chunk := range stream.Chunks() {
		chunks = append(chunks, chunk)
	}
	response, err := stream.Response()
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	return response, chunks
}

func TestModelReplayCallOrder(t *testing.T) {
	type input struct {
		saved           []gent.ModelCall
		checkpointAfter int
		calls           []gent.ModelCall
		nextIteration   bool
	}
	type expected struct {
		answers       []string
		ids           []string
		providerCalls int
		err           error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	firstId, secondId := "call-order:root:model:1", "call-order:root:model:2"
	thirdId, fourthId := "call-order:root:model:3", "call-order:root:model:4"
	cases := []testCase{
		{"explicit IDs can replay in a different order", input{
			saved: []gent.ModelCall{{Id: "first"}, {Id: "second"}},
			calls: []gent.ModelCall{{Id: "second"}, {Id: "first"}},
		}, expected{[]string{"saved 2", "saved 1"}, []string{"second", "first"}, 0, nil}},
		{"stream labels do not identify the call", input{
			saved: []gent.ModelCall{{Id: "first", StreamId: "old"}, {Id: "second"}},
			calls: []gent.ModelCall{{Id: "first", StreamId: "changed"}},
		}, expected{[]string{"saved 1"}, []string{"first"}, 0, nil}},
		{"automatic IDs replay once before new calls", input{
			saved: []gent.ModelCall{{}, {}}, calls: []gent.ModelCall{{}, {}, {}, {}},
		}, expected{[]string{"saved 1", "saved 2", "new 1", "new 2"},
			[]string{firstId, secondId, thirdId, fourthId}, 2, nil}},
		{"automatic IDs start after the checkpoint", input{
			saved: []gent.ModelCall{{}, {}}, checkpointAfter: 1, calls: []gent.ModelCall{{}, {}},
		}, expected{[]string{"saved 2", "new 1"}, []string{secondId, thirdId}, 1, nil}},
		{"a new iteration skips unused saved IDs", input{
			saved: []gent.ModelCall{{}, {}}, calls: []gent.ModelCall{{}}, nextIteration: true,
		}, expected{[]string{"new 1"}, []string{thirdId}, 1, nil}},
		{"explicit and automatic IDs stay separate", input{
			saved: []gent.ModelCall{{Id: "named"}, {}, {}},
			calls: []gent.ModelCall{{Id: "named"}, {}, {}, {Id: "new"}},
		}, expected{[]string{"saved 1", "saved 2", "saved 3", "new 1"},
			[]string{"named", firstId, secondId, "new"}, 1, nil}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx := gent.NewExecutionContext(context.Background(), "root", nil,
				gent.WithRunId("call-order"))
			ctx.IncrementIteration()
			if !assert.NoError(t, ctx.CommitCheckpoint(gent.PhaseIterationReady)) {
				return
			}
			model := &savedReplyModel{}
			for i := range test.input.saved {
				model.responses = append(model.responses, fmt.Sprintf("saved %d", i+1))
			}
			for i, call := range test.input.saved {
				readModelCall(t, ctx, model, call)
				if i+1 == test.input.checkpointAfter {
					if !assert.NoError(t, ctx.CommitCheckpoint(gent.PhaseIterationReady)) {
						return
					}
				}
			}
			bundle, err := ctx.Export()
			if !assert.NoError(t, err) {
				return
			}
			restored, err := gent.RestoreExecutionContext(context.Background(), bundle,
				gent.RestoreBindings{})
			if !assert.NoError(t, err) {
				return
			}
			fresh := &savedReplyModel{responses: []string{"new 1", "new 2"}}
			if test.input.nextIteration {
				restored.IncrementIteration()
			}
			var actual expected
			for _, call := range test.input.calls {
				response, chunks := readModelCall(t, restored, fresh, call)
				if !assert.Len(t, chunks, 1) {
					return
				}
				actual.answers = append(actual.answers, response.Choices[0].Content)
				actual.ids = append(actual.ids, chunks[0].ModelCallId)
			}
			actual.providerCalls = fresh.calls
			view, err := restored.Export()
			if !assert.NoError(t, err) {
				return
			}
			actual.err = gent.ValidateBundle(view)
			assert.Equal(t, test.expected, actual)
			if test.expected.providerCalls == 0 {
				assert.Equal(t, bundle, view)
			}
		})
	}
}

type savedChunkModel struct {
	response *gent.ContentResponse
	chunks   []gent.StreamChunk
	err      error
}

func TestModelReplayRetriesFailedCall(t *testing.T) {
	type input struct {
		call        gent.ModelCall
		olderResult bool
	}
	type expected struct {
		content       string
		id            string
		providerCalls int
		err           error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"automatic ID", input{}, expected{"retry", "retry:root:model:1", 1, nil}},
		{"explicit ID ignores an older completed attempt", input{gent.ModelCall{Id: "answer"}, true},
			expected{"retry", "answer", 1, nil}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx := gent.NewExecutionContext(context.Background(), "root", nil, gent.WithRunId("retry"))
			ctx.IncrementIteration()
			if !assert.NoError(t, ctx.CommitCheckpoint(gent.PhaseIterationReady)) {
				return
			}
			if test.input.olderResult {
				readModelCall(t, ctx, &savedReplyModel{responses: []string{"old"}}, test.input.call)
			}
			failed := &savedChunkModel{chunks: []gent.StreamChunk{{Content: "partial"}},
				err: context.Canceled}
			stream, err := ctx.GenerateContentStream(failed, test.input.call,
				gent.NewModelCallRequest(nil))
			if !assert.NoError(t, err) {
				return
			}
			_, err = stream.Response()
			stream.Close()
			assert.ErrorIs(t, err, context.Canceled)
			bundle, err := ctx.Export()
			if !assert.NoError(t, err) {
				return
			}
			restored, err := gent.RestoreExecutionContext(context.Background(), bundle,
				gent.RestoreBindings{})
			if !assert.NoError(t, err) {
				return
			}
			fresh := &savedReplyModel{responses: []string{"retry"}}
			response, chunks := readModelCall(t, restored, fresh, test.input.call)
			if !assert.Len(t, chunks, 1) {
				return
			}
			view, err := restored.Export()
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, test.expected, expected{response.Choices[0].Content,
				chunks[0].ModelCallId, fresh.calls, gent.ValidateBundle(view)})
		})
	}
}

func (model *savedChunkModel) GenerateContentStream(context.Context, gent.ModelCallRequest) (
	gent.Stream, error,
) {
	stream := gent.NewStreamWithDuration()
	for _, chunk := range model.chunks {
		stream.Send(chunk)
	}
	stream.Complete(model.response, model.err)
	return stream, nil
}

func TestModelReplayKeepsStreamAndUsage(t *testing.T) {
	type input struct {
		chunks []gent.StreamChunk
		retry  bool
	}
	type expected struct {
		response      *gent.ContentResponse
		providerCalls int
		totalTokens   int64
		err           error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	response := &gent.ContentResponse{Choices: []*gent.ContentChoice{{Content: "hello",
		ReasoningContent: "reason", StopReason: "stop"}, {Content: "another choice"}},
		Info: &gent.GenerationInfo{InputTokens: 3, OutputTokens: 5, TotalTokens: 8}}
	stamp := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	chunks := []gent.StreamChunk{{ReasoningContent: "reason", Timestamp: stamp},
		{Content: "he", Timestamp: stamp}, {Content: "llo", Timestamp: stamp}}
	cases := []testCase{
		{"saved chunks", input{chunks: chunks}, expected{response, 0, 8, nil}},
		{"response without chunks", input{}, expected{response, 0, 8, nil}},
		{"only the last attempt is replayed", input{chunks: chunks, retry: true},
			expected{response, 0, 10, nil}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx := gent.NewExecutionContext(context.Background(), "root", nil)
			ctx.IncrementIteration()
			if !assert.NoError(t, ctx.CommitCheckpoint(gent.PhaseIterationReady)) {
				return
			}
			call := gent.ModelCall{Id: "answer", StreamId: "stream", StreamTopicId: "topic"}
			if test.input.retry {
				failed := &savedChunkModel{response: &gent.ContentResponse{
					Info: &gent.GenerationInfo{InputTokens: 1, OutputTokens: 1, TotalTokens: 2}},
					chunks: []gent.StreamChunk{{Content: "partial"}}, err: context.Canceled}
				stream, err := ctx.GenerateContentStream(failed, call, gent.NewModelCallRequest(nil))
				if !assert.NoError(t, err) {
					return
				}
				_, err = stream.Response()
				stream.Close()
				assert.ErrorIs(t, err, context.Canceled)
			}
			_, liveChunks := readModelCall(t, ctx,
				&savedChunkModel{response: response, chunks: test.input.chunks}, call)
			bundle, err := ctx.Export()
			if !assert.NoError(t, err) {
				return
			}
			restored, err := gent.RestoreExecutionContext(context.Background(), bundle,
				gent.RestoreBindings{})
			if !assert.NoError(t, err) {
				return
			}
			fresh := &savedReplyModel{}
			result, replayChunks := readModelCall(t, restored, fresh, call)
			assert.Equal(t, liveChunks, replayChunks)
			assert.Equal(t, ctx.Stats().Counters(), restored.Stats().Counters())
			view, err := restored.Export()
			assert.Equal(t, test.expected, expected{result, fresh.calls,
				restored.Stats().GetCounter(gent.SCTotalTokens), err})
			assert.Equal(t, bundle, view)
		})
	}
}
