package models

import (
	"context"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
)

type closeTestModel struct {
	started            chan context.Context
	messages           chan []llms.MessageContent
	release            chan struct{}
	done               chan error
	waitForCancel      bool
	streamAfterRelease bool
	response           *llms.ContentResponse
}

func newCloseTestModel() *closeTestModel {
	return &closeTestModel{
		started:  make(chan context.Context, 1),
		messages: make(chan []llms.MessageContent, 1),
		release:  make(chan struct{}),
		done:     make(chan error, 1),
	}
}

func (m *closeTestModel) GenerateContent(
	ctx context.Context,
	messages []llms.MessageContent,
	options ...llms.CallOption,
) (*llms.ContentResponse, error) {
	var opts llms.CallOptions
	for _, opt := range options {
		opt(&opts)
	}
	m.messages <- messages

	if opts.StreamingFunc != nil {
		if err := opts.StreamingFunc(ctx, []byte("first")); err != nil {
			m.done <- err
			return nil, err
		}
	}
	m.started <- ctx

	if m.waitForCancel {
		<-ctx.Done()
		err := ctx.Err()
		m.done <- err
		return nil, err
	}

	<-m.release
	if m.streamAfterRelease && opts.StreamingFunc != nil {
		if err := opts.StreamingFunc(ctx, []byte("late")); err != nil {
			m.done <- err
			return nil, err
		}
	}

	response := m.response
	if response == nil {
		response = &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: "done"}}}
	}
	m.done <- nil
	return response, nil
}

func (m *closeTestModel) Call(
	ctx context.Context,
	prompt string,
	options ...llms.CallOption,
) (string, error) {
	return "", nil
}

type modelRequestInjectionSubscriber struct {
	afterRequest chan gent.ModelCallRequest
}

type requestMessageSummary struct {
	role llms.ChatMessageType
	text string
}

func (s *modelRequestInjectionSubscriber) OnBeforeModelCall(
	_ *gent.ExecutionContext,
	event *gent.BeforeModelCallEvent,
) {
	event.Request.Messages = append(event.Request.Messages, llms.TextParts(
		llms.ChatMessageTypeSystem,
		"injected context",
	))
}

func (s *modelRequestInjectionSubscriber) OnAfterModelCall(
	_ *gent.ExecutionContext,
	event *gent.AfterModelCallEvent,
) {
	s.afterRequest <- event.Request
}

func TestLCGWrapperGenerateContentStream_BeforeModelCallUsesTypedRequestInjection(t *testing.T) {
	type expected struct {
		messages    []requestMessageSummary
		temperature float64
	}

	model := newCloseTestModel()
	wrapper := NewLCGWrapper(model).WithModelName("typed-request-test")
	subscriber := &modelRequestInjectionSubscriber{
		afterRequest: make(chan gent.ModelCallRequest, 1),
	}
	registry := events.NewRegistry().Subscribe(subscriber)
	execCtx := gent.NewExecutionContext(context.Background(), "test", nil)
	execCtx.SetEventPublisher(registry)

	stream, err := wrapper.GenerateContentStream(
		execCtx,
		"stream",
		"topic",
		[]llms.MessageContent{llms.TextParts(llms.ChatMessageTypeHuman, "hello")},
		llms.WithTemperature(0.2),
	)
	require.NoError(t, err)
	receiveCallContext(t, model.started)
	modelMessages := receiveCallMessages(t, model.messages)

	close(model.release)
	response, err := stream.Response()
	require.NoError(t, err)
	require.NotNil(t, response)
	afterRequest := receiveModelCallRequest(t, subscriber.afterRequest)

	assert.Equal(t, expected{
		messages: []requestMessageSummary{
			{role: llms.ChatMessageTypeHuman, text: "hello"},
			{role: llms.ChatMessageTypeSystem, text: "injected context"},
		},
		temperature: 0.2,
	}, expected{
		messages:    summarizeRequestMessages(modelMessages),
		temperature: afterRequest.Options.Temperature,
	})
	assert.Equal(
		t,
		summarizeRequestMessages(modelMessages),
		summarizeRequestMessages(afterRequest.Messages),
	)
}

func TestLCGWrapperGenerateContentStream_CloseCancelsModelContext(t *testing.T) {
	model := newCloseTestModel()
	model.waitForCancel = true
	wrapper := NewLCGWrapper(model).WithModelName("close-test")
	execCtx := gent.NewExecutionContext(context.Background(), "test", nil)

	stream, err := wrapper.GenerateContentStream(
		execCtx,
		"stream",
		"topic",
		[]llms.MessageContent{llms.TextParts(llms.ChatMessageTypeHuman, "hello")},
	)
	require.NoError(t, err)

	callCtx := receiveCallContext(t, model.started)
	assert.NoError(t, callCtx.Err())

	stream.Close()

	select {
	case <-callCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("expected stream close to cancel the model context")
	}

	select {
	case err := <-model.done:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("expected model call to return after cancellation")
	}
}

func TestLCGWrapperGenerateContentStream_NormalCompletionCancelsModelContext(t *testing.T) {
	model := newCloseTestModel()
	wrapper := NewLCGWrapper(model).WithModelName("normal-completion-test")
	execCtx := gent.NewExecutionContext(context.Background(), "test", nil)

	stream, err := wrapper.GenerateContentStream(
		execCtx,
		"stream",
		"topic",
		[]llms.MessageContent{llms.TextParts(llms.ChatMessageTypeHuman, "hello")},
	)
	require.NoError(t, err)

	callCtx := receiveCallContext(t, model.started)
	assert.NoError(t, callCtx.Err())

	close(model.release)
	response, err := stream.Response()
	require.NoError(t, err)
	require.NotNil(t, response)
	require.Len(t, response.Choices, 1)
	assert.Equal(t, "done", response.Choices[0].Content)

	select {
	case err := <-model.done:
		assert.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("expected model call to complete")
	}

	require.Eventually(t, func() bool {
		return callCtx.Err() != nil
	}, time.Second, 10*time.Millisecond)
	assert.ErrorIs(t, callCtx.Err(), context.Canceled)
}

func TestLCGWrapperGenerateContentStream_NilExecutionContextSkipsTracing(t *testing.T) {
	model := newCloseTestModel()
	wrapper := NewLCGWrapper(model).WithModelName("nil-context-test")

	stream, err := wrapper.GenerateContentStream(
		nil,
		"stream",
		"topic",
		[]llms.MessageContent{llms.TextParts(llms.ChatMessageTypeHuman, "hello")},
	)
	require.NoError(t, err)
	receiveCallContext(t, model.started)

	close(model.release)
	response, err := stream.Response()
	require.NoError(t, err)
	require.NotNil(t, response)
	require.Len(t, response.Choices, 1)
	assert.Equal(t, "done", response.Choices[0].Content)

	select {
	case err := <-model.done:
		assert.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("expected model call to complete")
	}
}

func TestLCGWrapperGenerateContentStream_LateCompletionKeepsCallIteration(t *testing.T) {
	model := newCloseTestModel()
	model.streamAfterRelease = true
	model.response = &llms.ContentResponse{Choices: []*llms.ContentChoice{{
		Content: "done",
		GenerationInfo: map[string]any{
			"PromptTokens":     3,
			"CompletionTokens": 5,
		},
	}}}
	wrapper := NewLCGWrapper(model).WithModelName("close-test")
	execCtx := gent.NewExecutionContext(context.Background(), "test", nil)
	execCtx.IncrementIteration()
	execCtx.PublishBeforeIteration()
	chunks, unsubscribe := execCtx.SubscribeToStream("stream")
	defer unsubscribe()

	expectedChunkEarliestTs := time.Now()
	stream, err := wrapper.GenerateContentStream(
		execCtx,
		"stream",
		"topic",
		[]llms.MessageContent{llms.TextParts(llms.ChatMessageTypeHuman, "hello")},
	)
	require.NoError(t, err)
	receiveCallContext(t, model.started)

	streamChunk := receiveStreamChunk(t, stream.Chunks())
	assert.Equal(t, gent.StreamChunk{Content: "first"}, streamChunk)
	emittedChunk := receiveStreamChunk(t, chunks)
	beforeEvents := beforeModelCallEvents(execCtx)
	require.Len(t, beforeEvents, 1)
	assert.GreaterOrEqual(t, emittedChunk.Timestamp.UnixNano(), expectedChunkEarliestTs.UnixNano())
	assert.Equal(t, gent.StreamChunk{
		Content:       "first",
		Timestamp:     emittedChunk.Timestamp,
		Iteration:     1,
		Depth:         0,
		Source:        "test/1",
		ContextId:     execCtx.ContextId(),
		ModelCallId:   beforeEvents[0].ModelCallId,
		StreamId:      "stream",
		StreamTopicId: "topic",
	}, emittedChunk)

	stream.Close()
	response, err := stream.Response()
	assert.NoError(t, err)
	assert.Nil(t, response)

	execCtx.IncrementIteration()
	execCtx.PublishBeforeIteration()
	close(model.release)
	require.Eventually(t, func() bool {
		return len(afterModelCallEvents(execCtx)) == 1
	}, time.Second, 10*time.Millisecond)

	afterEvents := afterModelCallEvents(execCtx)
	require.Len(t, afterEvents, 1)
	assert.Equal(t, 1, afterEvents[0].Iteration)
	assert.Equal(t, int64(5), execCtx.Stats().GetCounter(gent.SCOutputTokens))
	assert.Equal(t, 0.0, execCtx.Stats().GetGauge(gent.SGOutputTokensLastIteration))

	select {
	case chunk := <-chunks:
		assert.Equal(t, gent.StreamChunk{}, chunk)
		t.Fatal("expected late stream chunks to be suppressed after close")
	case <-time.After(50 * time.Millisecond):
	}
}

func receiveCallContext(t *testing.T, ch <-chan context.Context) context.Context {
	t.Helper()
	select {
	case ctx := <-ch:
		return ctx
	case <-time.After(time.Second):
		t.Fatal("expected model call to start")
		return nil
	}
}

func receiveCallMessages(t *testing.T, ch <-chan []llms.MessageContent) []llms.MessageContent {
	t.Helper()
	select {
	case messages := <-ch:
		return messages
	case <-time.After(time.Second):
		t.Fatal("expected model call messages")
		return nil
	}
}

func receiveModelCallRequest(
	t *testing.T,
	ch <-chan gent.ModelCallRequest,
) gent.ModelCallRequest {
	t.Helper()
	select {
	case request := <-ch:
		return request
	case <-time.After(time.Second):
		t.Fatal("expected after model call request")
		return gent.ModelCallRequest{}
	}
}

func summarizeRequestMessages(messages []llms.MessageContent) []requestMessageSummary {
	result := make([]requestMessageSummary, 0)
	for _, message := range messages {
		for _, part := range message.Parts {
			text, ok := part.(llms.TextContent)
			if !ok {
				continue
			}
			result = append(result, requestMessageSummary{role: message.Role, text: text.Text})
		}
	}
	return result
}

func receiveStreamChunk(t *testing.T, ch <-chan gent.StreamChunk) gent.StreamChunk {
	t.Helper()
	select {
	case chunk := <-ch:
		return chunk
	case <-time.After(time.Second):
		t.Fatal("expected stream chunk")
		return gent.StreamChunk{}
	}
}

func beforeModelCallEvents(execCtx *gent.ExecutionContext) []*gent.BeforeModelCallEvent {
	events := execCtx.Events()
	result := make([]*gent.BeforeModelCallEvent, 0)
	for _, event := range events {
		if beforeEvent, ok := event.(*gent.BeforeModelCallEvent); ok {
			result = append(result, beforeEvent)
		}
	}
	return result
}

func afterModelCallEvents(execCtx *gent.ExecutionContext) []*gent.AfterModelCallEvent {
	events := execCtx.Events()
	result := make([]*gent.AfterModelCallEvent, 0)
	for _, event := range events {
		if afterEvent, ok := event.(*gent.AfterModelCallEvent); ok {
			result = append(result, afterEvent)
		}
	}
	return result
}
