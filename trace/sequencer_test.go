package trace

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/events"
	"github.com/rickchristie/gent/executor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
)

type traceTestLoop struct {
	result       []gent.ContentPart
	err          error
	beforeReturn func(*gent.ExecutionContext)
}

func (l *traceTestLoop) Next(execCtx *gent.ExecutionContext) (*gent.AgentLoopResult, error) {
	if l.beforeReturn != nil {
		l.beforeReturn(execCtx)
	}
	if l.err != nil {
		return nil, l.err
	}
	return &gent.AgentLoopResult{Action: gent.LATerminate, Result: l.result}, nil
}

func TestSequencer_EventNumbersReplayAndJSONSafety(t *testing.T) {
	type expected struct {
		lastEventNumber uint64
		recentCount     int
		replayCount     int
		stitchStatus    StitchStatus
	}

	seq := NewSequencer("run", Config{MaxRecentEvents: 3})
	seq.record(normalizedEvent{event: &Event{Type: EventTypeCommon, Ts: time.Now()}})
	seq.record(normalizedEvent{event: &Event{Type: EventTypeCommon, Ts: time.Now()}})
	seq.record(normalizedEvent{event: &Event{Type: EventTypeCommon, Ts: time.Now()}})
	seq.record(normalizedEvent{event: &Event{Type: EventTypeCommon, Ts: time.Now()}})

	snapshot := seq.Snapshot()
	replay, err := seq.EventsAfter(2)
	require.NoError(t, err)
	assertJSONSafe(t, snapshot)
	assertJSONSafe(t, replay)
	assertEventNumbersContiguous(t, snapshot.RecentEvents)
	assert.Equal(t, expected{
		lastEventNumber: 4,
		recentCount:     3,
		replayCount:     2,
		stitchStatus:    StitchStatusOK,
	}, expected{
		lastEventNumber: snapshot.LastEventNumber,
		recentCount:     len(snapshot.RecentEvents),
		replayCount:     len(replay),
		stitchStatus:    ValidateStitch(2, replay).Status,
	})

	_, err = seq.EventsAfter(0)
	assert.ErrorIs(t, err, ErrReplayUnavailable)
}

func TestSequencer_SnapshotModelToolAndStreamCorrelation(t *testing.T) {
	seq := NewSequencer("run", Config{
		IncludeChunkText:      true,
		IncludeModelRequests:  true,
		IncludeModelResponses: true,
		IncludeToolArgs:       true,
		IncludeToolOutput:     true,
	})
	registry := events.NewRegistry().Subscribe(seq)
	execCtx := gent.NewExecutionContext(context.Background(), "main", nil)
	execCtx.SetEventPublisher(registry)
	execCtx.PublishBeforeExecution()
	execCtx.IncrementIteration()
	execCtx.PublishBeforeIteration()

	request := gent.ModelCallRequest{
		Messages: []llms.MessageContent{llms.TextParts(llms.ChatMessageTypeHuman, "hello")},
		Options:  llms.CallOptions{Temperature: 0.2, MaxTokens: 123},
	}
	beforeModel := execCtx.PublishBeforeModelCall(
		"gpt", request,
		gent.WithModelStream("stream", "topic"),
		gent.WithModelProvider("openai"),
	)
	seq.ConsumeChunk(gent.StreamChunk{
		Content:       "hel",
		ModelCallId:   beforeModel.ModelCallId,
		StreamId:      "stream",
		StreamTopicId: "topic",
		Source:        beforeModel.Source,
		Iteration:     beforeModel.Iteration,
		Depth:         beforeModel.Depth,
		ContextId:     beforeModel.ContextId,
	})
	seq.ConsumeChunk(gent.StreamChunk{
		Content:       "lo",
		ModelCallId:   beforeModel.ModelCallId,
		StreamId:      "stream",
		StreamTopicId: "topic",
		Source:        beforeModel.Source,
		Iteration:     beforeModel.Iteration,
		Depth:         beforeModel.Depth,
		ContextId:     beforeModel.ContextId,
	})
	response := &gent.ContentResponse{Info: &gent.GenerationInfo{
		InputTokens: 3, OutputTokens: 4, CachedInputTokens: 1, ReasoningTokens: 2, TotalTokens: 7,
	}}
	execCtx.PublishAfterModelCall(
		"gpt", request, response, 10*time.Millisecond, nil,
		gent.WithModelCallId(beforeModel.ModelCallId),
		gent.WithModelStream("stream", "topic"),
		gent.WithModelCallSource(beforeModel.Source),
		gent.WithModelProvider("openai"),
	)

	beforeTool := execCtx.PublishBeforeToolCall("search", map[string]any{"query": "gent"})
	execCtx.PublishAfterToolCall(
		"search", beforeTool.Args, "result", 5*time.Millisecond, nil,
		gent.WithToolCallId(beforeTool.ToolCallId),
		gent.WithToolCallSource(beforeTool.Source),
	)
	execCtx.PublishAfterIteration(&gent.AgentLoopResult{Action: gent.LATerminate}, time.Millisecond)
	execCtx.SetTermination(gent.TerminationSuccess, nil, nil)
	execCtx.PublishAfterExecution(gent.TerminationSuccess, nil)

	snapshot := seq.Snapshot()
	require.Len(t, snapshot.ModelCalls, 1)
	require.Len(t, snapshot.ToolCalls, 1)
	assertJSONSafe(t, snapshot)
	assert.Equal(t, "hello", snapshot.ModelCalls[0].Stream.Content)
	assert.Equal(t, "openai", snapshot.ModelCalls[0].Provider)
	assert.Equal(t, &ModelUsage{
		InputTokens:       3,
		OutputTokens:      4,
		ReasoningTokens:   2,
		CachedInputTokens: 1,
		TotalTokens:       7,
	}, snapshot.ModelCalls[0].Usage)
	assert.Equal(t, "search", snapshot.ToolCalls[0].Name)
	assert.Equal(t, RunStatusSucceeded, snapshot.Status)
}

func TestSequencer_RunLifecycleIgnoresChildExecutionEvents(t *testing.T) {
	type expectedAfterChild struct {
		status                  RunStatus
		startedTs               time.Time
		completed               bool
		result                  *RunResult
		executionStartedEvents  int
		executionFinishedEvents int
	}
	type expectedChildContext struct {
		name      string
		parentId  string
		depth     int
		status    StepStatus
		completed bool
	}
	type expectedFinal struct {
		status                  RunStatus
		startedTs               time.Time
		completed               bool
		result                  *RunResult
		executionStartedEvents  int
		executionFinishedEvents int
	}

	seq := NewSequencer("run", Config{
		IncludeRunOutput: true,
		Redactor: RedactorFuncs{
			RunOutput: func(output []gent.ContentPart) any {
				if len(output) == 0 {
					return ""
				}
				text, _ := output[0].(llms.TextContent)
				return text.Text
			},
		},
	})
	registry := events.NewRegistry().Subscribe(seq)
	childExec := executor.New[*gent.BasicLoopData](
		&traceTestLoop{result: []gent.ContentPart{llms.TextContent{Text: "child result"}}},
		executor.Config{Events: registry},
	)

	var rootStartedTs time.Time
	var afterChild *Snapshot
	parentLoop := &traceTestLoop{
		result: []gent.ContentPart{llms.TextContent{Text: "root result"}},
		beforeReturn: func(execCtx *gent.ExecutionContext) {
			rootStartedTs = seq.Snapshot().StartedTs
			childData := gent.NewBasicLoopData(&gent.Task{Text: "child task"})
			childCtx := execCtx.SpawnChild("child", childData)
			childExec.Execute(childCtx)
			afterChild = seq.Snapshot()
			execCtx.CompleteChild(childCtx)
		},
	}
	rootExec := executor.New[*gent.BasicLoopData](parentLoop, executor.Config{Events: registry})
	rootData := gent.NewBasicLoopData(&gent.Task{Text: "root task"})
	rootCtx := gent.NewExecutionContext(context.Background(), "root", rootData)

	rootExec.Execute(rootCtx)

	require.NotNil(t, afterChild)
	assert.Equal(t, expectedAfterChild{
		status:                  RunStatusRunning,
		startedTs:               rootStartedTs,
		completed:               false,
		result:                  nil,
		executionStartedEvents:  1,
		executionFinishedEvents: 0,
	}, expectedAfterChild{
		status:                  afterChild.Status,
		startedTs:               afterChild.StartedTs,
		completed:               afterChild.CompletedTs != nil,
		result:                  afterChild.Result,
		executionStartedEvents:  countTraceEventType(afterChild.RecentEvents, EventTypeExecutionStarted),
		executionFinishedEvents: countTraceEventType(afterChild.RecentEvents, EventTypeExecutionFinished),
	})
	childContext := onlyChildTraceContext(t, afterChild)
	assert.Equal(t, expectedChildContext{
		name:      "child",
		parentId:  rootCtx.ContextId(),
		depth:     1,
		status:    StepStatusSucceeded,
		completed: true,
	}, expectedChildContext{
		name:      childContext.Name,
		parentId:  childContext.ParentId,
		depth:     childContext.Depth,
		status:    childContext.Status,
		completed: childContext.CompletedTs != nil,
	})

	snapshot := seq.Snapshot()
	require.NotNil(t, snapshot.CompletedTs)
	assert.GreaterOrEqual(t, snapshot.CompletedTs.UnixNano(), rootStartedTs.UnixNano())
	assert.Equal(t, expectedFinal{
		status:    RunStatusSucceeded,
		startedTs: rootStartedTs,
		completed: true,
		result: &RunResult{
			TerminationReason: gent.TerminationSuccess,
			Output:            "root result",
		},
		executionStartedEvents:  1,
		executionFinishedEvents: 1,
	}, expectedFinal{
		status:                  snapshot.Status,
		startedTs:               snapshot.StartedTs,
		completed:               snapshot.CompletedTs != nil,
		result:                  snapshot.Result,
		executionStartedEvents:  countTraceEventType(snapshot.RecentEvents, EventTypeExecutionStarted),
		executionFinishedEvents: countTraceEventType(snapshot.RecentEvents, EventTypeExecutionFinished),
	})
}

func TestSequencer_ChildCompletePreservesTerminalContextStatus(t *testing.T) {
	type input struct {
		loop                *traceTestLoop
		limits              []gent.Limit
		cancelBeforeExecute bool
	}
	type expected struct {
		terminationReason gent.TerminationReason
		status            StepStatus
		completed         bool
		errorMessage      string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}

	testCases := []testCase{
		{
			name: "error",
			input: input{
				loop: &traceTestLoop{err: errors.New("child boom")},
			},
			expected: expected{
				terminationReason: gent.TerminationError,
				status:            StepStatusFailed,
				completed:         true,
				errorMessage:      "AgentLoop.Next (iteration 1): child boom",
			},
		},
		{
			name: "limit exceeded",
			input: input{
				loop: &traceTestLoop{
					result: []gent.ContentPart{llms.TextContent{Text: "child result"}},
				},
				limits: []gent.Limit{{
					Type:     gent.LimitExactKey,
					Key:      gent.SCIterations.Self(),
					MaxValue: 1,
				}},
			},
			expected: expected{
				terminationReason: gent.TerminationLimitExceeded,
				status:            StepStatusFailed,
				completed:         true,
				errorMessage:      "limit exceeded: $self:gent:iterations >= 1",
			},
		},
		{
			name: "context canceled",
			input: input{
				loop:                &traceTestLoop{},
				cancelBeforeExecute: true,
			},
			expected: expected{
				terminationReason: gent.TerminationContextCanceled,
				status:            StepStatusCanceled,
				completed:         true,
				errorMessage:      "context canceled",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			seq := NewSequencer("run", Config{})
			registry := events.NewRegistry().Subscribe(seq)
			goCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
			rootCtx := gent.NewExecutionContext(
				goCtx,
				"root",
				gent.NewBasicLoopData(&gent.Task{Text: "root task"}),
			)
			rootCtx.SetEventPublisher(registry)
			childCtx := rootCtx.SpawnChild(
				"child",
				gent.NewBasicLoopData(&gent.Task{Text: "child task"}),
			)
			if tc.input.limits != nil {
				childCtx.SetLimits(tc.input.limits)
			}
			if tc.input.cancelBeforeExecute {
				cancel()
			}

			childExec := executor.New[*gent.BasicLoopData](
				tc.input.loop,
				executor.Config{Events: registry},
			)
			childExec.Execute(childCtx)
			rootCtx.CompleteChild(childCtx)

			snapshot := seq.Snapshot()
			childContext := onlyChildTraceContext(t, snapshot)
			actual := expected{
				terminationReason: childCtx.TerminationReason(),
				status:            childContext.Status,
				completed:         childContext.CompletedTs != nil,
				errorMessage:      traceErrorMessage(childContext.Error),
			}
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestSequencer_RedactionMissingIdsAndUTF8Truncation(t *testing.T) {
	seq := NewSequencer("run", Config{
		IncludeChunkText:      true,
		MaxModelContentBytes:  5,
		IncludeModelRequests:  true,
		IncludeModelResponses: true,
		IncludeToolOutput:     true,
		Redactor: RedactorFuncs{
			ModelRequest: func(any) any { return func() {} },
			Error: func(error) *Error {
				return &Error{Message: "redacted"}
			},
		},
	})
	base := gent.BaseEvent{
		Timestamp: time.Now(), EventName: gent.EventNameModelCallBefore,
		Iteration: 1, Source: "main/1", ContextId: "ctx:1",
	}
	seq.OnBeforeModelCall(nil, &gent.BeforeModelCallEvent{
		BaseEvent: base,
		Model:     "gpt",
		Request: gent.ModelCallRequest{
			Messages: []llms.MessageContent{llms.TextParts(llms.ChatMessageTypeHuman, "secret")},
		},
		ModelCallId: "model-1",
		StreamId:    "stream",
	})
	seq.ConsumeChunk(gent.StreamChunk{
		Content:     "abcédef",
		ModelCallId: "model-1",
		StreamId:    "stream",
		Source:      "main/1",
		ContextId:   "ctx:1",
		Iteration:   1,
	})
	seq.OnAfterModelCall(nil, &gent.AfterModelCallEvent{
		BaseEvent: base,
		Model:     "gpt",
		Duration:  time.Millisecond,
		Error:     errors.New("raw secret"),
		// Missing ModelCallId is intentionally uncorrelated.
	})

	snapshot := seq.Snapshot()
	require.Len(t, snapshot.ModelCalls, 1)
	require.Len(t, snapshot.Errors, 1)
	assertJSONSafe(t, snapshot)
	assert.Equal(t, "abcé", snapshot.ModelCalls[0].Stream.Content)
	assert.True(t, snapshot.ModelCalls[0].Stream.ContentTruncated)
	assert.Equal(t, StepStatusRunning, snapshot.ModelCalls[0].Status)
	assert.Equal(t, "redacted", snapshot.Errors[0].Message)
	_, ok := snapshot.ModelCalls[0].Request.(map[string]any)
	assert.True(t, ok, "unmarshalable redactor output should become PayloadCaptureError map")
}

func TestSequencer_BoundedSubscriptionOverflow(t *testing.T) {
	type expected struct {
		received []uint64
		overflow OverflowInfo
	}

	seq := NewSequencer("run", Config{})
	overflowCh := make(chan OverflowInfo, 1)
	eventsCh, unsubscribe := seq.SubscribeWithConfig(SubscribeConfig{
		BufferSize:     1,
		OverflowPolicy: OverflowDropOldest,
		OnOverflow: func(info OverflowInfo) {
			overflowCh <- info
		},
	})
	defer unsubscribe()
	seq.record(normalizedEvent{event: &Event{Type: EventTypeCommon, Ts: time.Now()}})
	seq.record(normalizedEvent{event: &Event{Type: EventTypeCommon, Ts: time.Now()}})

	first := receiveTraceEvent(t, eventsCh)
	overflow := receiveOverflow(t, overflowCh)
	assert.Equal(t, expected{
		received: []uint64{2},
		overflow: OverflowInfo{
			Policy:             OverflowDropOldest,
			DroppedEventNumber: 1,
			NewEventNumber:     2,
		},
	}, expected{received: []uint64{first.EventNumber}, overflow: overflow})
}

func TestSequencer_ConcurrentRecordingIsMonotonic(t *testing.T) {
	seq := NewSequencer("run", Config{})
	const workers = 50
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			seq.ConsumeChunk(gent.StreamChunk{Content: "x", StreamId: "s"})
		}(i)
	}
	close(start)
	wg.Wait()

	snapshot := seq.Snapshot()
	require.Len(t, snapshot.RecentEvents, workers)
	assertEventNumbersContiguous(t, snapshot.RecentEvents)
	assert.Equal(t, uint64(workers), snapshot.LastEventNumber)
}

func assertJSONSafe(t *testing.T, value any) {
	t.Helper()
	_, err := json.Marshal(value)
	require.NoError(t, err)
}

func assertEventNumbersContiguous(t *testing.T, events []*Event) {
	t.Helper()
	for i := 1; i < len(events); i++ {
		assert.Equal(t, events[i-1].EventNumber+1, events[i].EventNumber)
	}
}

func countTraceEventType(events []*Event, eventType EventType) int {
	count := 0
	for _, event := range events {
		if event.Type == eventType {
			count++
		}
	}
	return count
}

func onlyChildTraceContext(t *testing.T, snapshot *Snapshot) *Context {
	t.Helper()
	children := make([]*Context, 0)
	for _, ctx := range snapshot.Contexts {
		if ctx.ParentId != "" {
			children = append(children, ctx)
		}
	}
	require.Len(t, children, 1)
	return children[0]
}

func traceErrorMessage(err *Error) string {
	if err == nil {
		return ""
	}
	return err.Message
}

func receiveTraceEvent(t *testing.T, ch <-chan *Event) *Event {
	t.Helper()
	select {
	case event := <-ch:
		return event
	case <-time.After(time.Second):
		t.Fatal("expected trace event")
		return nil
	}
}

func receiveOverflow(t *testing.T, ch <-chan OverflowInfo) OverflowInfo {
	t.Helper()
	select {
	case info := <-ch:
		return info
	case <-time.After(time.Second):
		t.Fatal("expected overflow info")
		return OverflowInfo{}
	}
}
