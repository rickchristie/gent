package gent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type metadataCommonSubscriber struct {
	events []*CommonEvent
}

func (s *metadataCommonSubscriber) OnCommonEvent(_ *ExecutionContext, event *CommonEvent) {
	s.events = append(s.events, event)
}

type metadataPublisher struct {
	subscriber *metadataCommonSubscriber
}

func (p metadataPublisher) Dispatch(execCtx *ExecutionContext, event Event) {
	if commonEvent, ok := event.(*CommonEvent); ok {
		p.subscriber.OnCommonEvent(execCtx, commonEvent)
	}
}

func (metadataPublisher) MaxRecursion() int { return 10 }

func TestExecutionContext_TraceMetadata_ContextIdentity(t *testing.T) {
	type expected struct {
		rootParentContextId string
		childParentMatches  bool
		childrenDistinct    bool
	}

	execCtx := NewExecutionContext(context.Background(), "main", nil)
	execCtx.IncrementIteration()
	childA := execCtx.SpawnChild("worker", nil)
	childB := execCtx.SpawnChild("worker", nil)

	expectedOutput := expected{
		rootParentContextId: "",
		childParentMatches:  true,
		childrenDistinct:    true,
	}
	actual := expected{
		rootParentContextId: execCtx.ParentContextId(),
		childParentMatches: childA.ParentContextId() == execCtx.ContextId() &&
			childB.ParentContextId() == execCtx.ContextId(),
		childrenDistinct: childA.ContextId() != childB.ContextId(),
	}

	require.NotEmpty(t, execCtx.ContextId())
	require.NotEmpty(t, childA.ContextId())
	require.NotEmpty(t, childB.ContextId())
	assert.Equal(t, expectedOutput, actual)
}

func TestExecutionContext_TraceMetadata_BaseEventAndChunkMetadata(t *testing.T) {
	type expected struct {
		eventSource          string
		eventIteration       int
		eventDepth           int
		eventContextId       string
		eventParentContextId string
		chunkSource          string
		chunkIteration       int
		chunkDepth           int
		chunkContextId       string
		chunkParentContextId string
	}

	execCtx := NewExecutionContext(context.Background(), "main", nil)
	execCtx.IncrementIteration()
	eventEarliestTs := time.Now()
	event := execCtx.PublishBeforeIteration()
	chunkEarliestTs := time.Now()
	chunks, unsubscribe := execCtx.SubscribeAll()
	defer unsubscribe()
	execCtx.EmitChunk(StreamChunk{Content: "hello", StreamId: "stream"})
	chunk := receiveTraceMetadataChunk(t, chunks)

	assert.GreaterOrEqual(t, event.Timestamp.UnixNano(), eventEarliestTs.UnixNano())
	assert.GreaterOrEqual(t, chunk.Timestamp.UnixNano(), chunkEarliestTs.UnixNano())
	assert.Equal(t, expected{
		eventSource:          "main/1",
		eventIteration:       1,
		eventDepth:           0,
		eventContextId:       execCtx.ContextId(),
		eventParentContextId: "",
		chunkSource:          "main/1",
		chunkIteration:       1,
		chunkDepth:           0,
		chunkContextId:       execCtx.ContextId(),
		chunkParentContextId: "",
	}, expected{
		eventSource:          event.Source,
		eventIteration:       event.Iteration,
		eventDepth:           event.Depth,
		eventContextId:       event.ContextId,
		eventParentContextId: event.ParentContextId,
		chunkSource:          chunk.Source,
		chunkIteration:       chunk.Iteration,
		chunkDepth:           chunk.Depth,
		chunkContextId:       chunk.ContextId,
		chunkParentContextId: chunk.ParentContextId,
	})
}

func TestExecutionContext_TraceMetadata_ModelAndToolCorrelation(t *testing.T) {
	type expected struct {
		modelCallId      string
		provider         string
		streamId         string
		streamTopicId    string
		afterModelSource string
		toolCallId       string
		afterToolSource  string
	}

	execCtx := NewExecutionContext(context.Background(), "main", nil)
	execCtx.IncrementIteration()
	beforeModel := execCtx.PublishBeforeModelCall(
		"gpt", ModelCallRequest{},
		WithModelStream("stream", "topic"),
		WithModelProvider("openai"),
	)
	afterModel := execCtx.PublishAfterModelCallForIteration(
		beforeModel.Iteration,
		"gpt", ModelCallRequest{}, nil, time.Millisecond, nil,
		WithModelCallId(beforeModel.ModelCallId),
		WithModelStream(beforeModel.StreamId, beforeModel.StreamTopicId),
		WithModelCallSource(beforeModel.Source),
		WithModelProvider(beforeModel.Provider),
	)
	beforeTool := execCtx.PublishBeforeToolCall("search", map[string]any{"query": "x"})
	afterTool := execCtx.PublishAfterToolCall(
		"search", beforeTool.Args, "ok", time.Millisecond, nil,
		WithToolCallId(beforeTool.ToolCallId),
		WithToolCallSource(beforeTool.Source),
	)

	require.NotEmpty(t, beforeModel.ModelCallId)
	require.NotEmpty(t, beforeTool.ToolCallId)
	assert.Equal(t, expected{
		modelCallId:      beforeModel.ModelCallId,
		provider:         "openai",
		streamId:         "stream",
		streamTopicId:    "topic",
		afterModelSource: beforeModel.Source,
		toolCallId:       beforeTool.ToolCallId,
		afterToolSource:  beforeTool.Source,
	}, expected{
		modelCallId:      afterModel.ModelCallId,
		provider:         afterModel.Provider,
		streamId:         afterModel.StreamId,
		streamTopicId:    afterModel.StreamTopicId,
		afterModelSource: afterModel.Source,
		toolCallId:       afterTool.ToolCallId,
		afterToolSource:  afterTool.Source,
	})
}

func TestExecutionContext_TraceMetadata_ChildLifecycleEventsDispatch(t *testing.T) {
	type expected struct {
		eventNames []string
		childIds   []string
		parentIds  []string
	}

	subscriber := &metadataCommonSubscriber{}
	execCtx := NewExecutionContext(context.Background(), "main", nil)
	execCtx.SetEventPublisher(metadataPublisher{subscriber: subscriber})
	execCtx.IncrementIteration()
	child := execCtx.SpawnChild("worker", nil)
	execCtx.CompleteChild(child)

	require.Len(t, subscriber.events, 2)
	actual := expected{}
	for _, event := range subscriber.events {
		data, ok := event.Data.(map[string]any)
		require.True(t, ok)
		actual.eventNames = append(actual.eventNames, event.EventName)
		actual.childIds = append(actual.childIds, data["child_context_id"].(string))
		actual.parentIds = append(actual.parentIds, data["parent_context_id"].(string))
	}
	assert.Equal(t, expected{
		eventNames: []string{EventNameChildSpawn, EventNameChildComplete},
		childIds:   []string{child.ContextId(), child.ContextId()},
		parentIds:  []string{execCtx.ContextId(), execCtx.ContextId()},
	}, actual)
}

func receiveTraceMetadataChunk(t *testing.T, ch <-chan StreamChunk) StreamChunk {
	t.Helper()
	select {
	case chunk := <-ch:
		return chunk
	case <-time.After(time.Second):
		t.Fatal("expected stream chunk")
		return StreamChunk{}
	}
}
