package gent_test

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents/react"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/termination"
	"github.com/rickchristie/gent/trace"
	"github.com/stretchr/testify/assert"
)

type contentReply struct {
	choice *gent.ContentChoice
	deltas []string
	err    error
}

type contentModel struct {
	replies  []contentReply
	requests []gent.ModelCallRequest
	complete func()
}

func (model *contentModel) GenerateContentStream(_ context.Context, request gent.ModelCallRequest) (
	gent.Stream, error,
) {
	index := len(model.requests)
	model.requests = append(model.requests, request)
	if index >= len(model.replies) {
		return nil, errors.New("unexpected model request")
	}
	reply := model.replies[index]
	stream := gent.NewStreamWithDuration()
	for _, delta := range reply.deltas {
		stream.SendMeta(&gent.MetaContent{Type: gent.MetaContentIntermediary, Text: delta})
	}
	if model.complete != nil {
		model.complete()
	}
	stream.Complete(&gent.ContentResponse{Choices: []*gent.ContentChoice{reply.choice},
		Info: &gent.GenerationInfo{InputTokens: 2, OutputTokens: 4, TotalTokens: 6}}, reply.err)
	return stream, nil
}

func TestExecutionContent_FailuresKeepOnlyPreviews(t *testing.T) {
	type input struct {
		stopReason string
		provider   bool
		cancel     bool
		reject     bool
		tokenLimit bool
	}
	type expected struct {
		reason gent.TerminationReason
		err    string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"provider failure", input{provider: true}, expected{gent.TerminationError,
			"AgentLoop.Next (iteration 1): model call failed: provider failed"}},
		{"cancel", input{cancel: true}, expected{gent.TerminationContextCanceled, "context canceled"}},
		{"truncated", input{stopReason: "length"}, expected{gent.TerminationLimitExceeded,
			"limit exceeded: $self:gent:iterations >= 1"}},
		{"rejected", input{reject: true}, expected{gent.TerminationLimitExceeded,
			"limit exceeded: $self:gent:iterations >= 1"}},
		{"token limit", input{tokenLimit: true}, expected{gent.TerminationLimitExceeded,
			"limit exceeded: gent:output_tokens > 3"}},
		{"unknown completion", input{stopReason: "new_stop"}, expected{gent.TerminationError,
			`AgentLoop.Next (iteration 1): model stopped without a usable response: "new_stop"`}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx := gent.NewExecutionContext(context.Background(), "root",
				gent.NewBasicLoopData(&gent.Task{Text: "Get the status."}))
			choice := &gent.ContentChoice{CandidateContent: `{"status":"pending"}`,
				StopReason: test.input.stopReason}
			reply := contentReply{choice: choice, deltas: []string{choice.CandidateContent}}
			if test.input.provider {
				reply.err = errors.New("provider failed")
			}
			model := &contentModel{replies: []contentReply{reply}}
			if test.input.cancel {
				model.complete = func() { ctx.CancelRun(context.Canceled) }
			}
			agent := react.NewAgent(model)
			if test.input.reject {
				term := termination.NewJSON[contentAnswer]("answer")
				term.SetValidator(&contentValidator{})
				agent.WithTermination(term)
			}
			if test.input.reject || test.input.stopReason == "length" {
				ctx.SetLimits([]gent.Limit{{Type: gent.LimitExactKey,
					Key: gent.SCIterations.Self(), MaxValue: 1}})
			}
			if test.input.tokenLimit {
				ctx.SetLimits([]gent.Limit{{Type: gent.LimitExactKey,
					Key: gent.SCOutputTokens, MaxValue: 3}})
			}
			chunks, unsubscribe := ctx.SubscribeAll()
			defer unsubscribe()
			result, err := executor.NewRunner(agent, executor.DefaultConfig()).Execute(ctx)
			if !assert.Error(t, err) || !assert.NotNil(t, result) {
				return
			}
			assert.Equal(t, test.expected, expected{result.TerminationReason, err.Error()})
			var live []gent.StreamChunk
			for chunk := range chunks {
				assert.Equal(t, "", chunk.Content)
				assert.Equal(t, "", chunk.MessageId)
				live = append(live, chunk)
			}
			bundle, err := ctx.Export()
			assert.NoError(t, err)
			assert.Equal(t, live, contentChunks(t, bundle.Activity, ctx.Codecs()))
			assert.Equal(t, gent.PhaseTerminal, bundle.Checkpoint.Phase)
		})
	}
}

type contentLoop func(*gent.ExecutionContext) (*gent.AgentLoopResult, error)

func (loop contentLoop) Next(ctx *gent.ExecutionContext) (*gent.AgentLoopResult, error) {
	return loop(ctx)
}

func TestExecutionContent_ChildRunsHaveSeparateAnswers(t *testing.T) {
	ctx := gent.NewExecutionContext(context.Background(), "root", gent.NewBasicLoopData(nil))
	chunks, unsubscribe := ctx.SubscribeAll()
	defer unsubscribe()
	children := []*gent.ExecutionContext{}
	loop := contentLoop(func(ctx *gent.ExecutionContext) (*gent.AgentLoopResult, error) {
		var pending sync.WaitGroup
		failures := make(chan error, 2)
		for range 2 {
			child := ctx.SpawnChild("child", gent.NewBasicLoopData(&gent.Task{Text: "Answer."}))
			children = append(children, child)
			pending.Add(1)
			go func() {
				defer pending.Done()
				model := &contentModel{replies: []contentReply{{
					choice: &gent.ContentChoice{CandidateContent: "Child answer."},
					deltas: []string{"Child answer."}}}}
				_, err := executor.NewRunner(react.NewAgent(model), executor.DefaultConfig()).Execute(child)
				failures <- errors.Join(err, ctx.CompleteChild(child))
			}()
		}
		pending.Wait()
		close(failures)
		for err := range failures {
			if err != nil {
				return nil, err
			}
		}
		return &gent.AgentLoopResult{Action: gent.LATerminate,
			Result: []gent.ContentPart{gent.TextContent{Text: "Parent answer."}}}, nil
	})
	_, err := executor.NewRunner(loop, executor.DefaultConfig()).Execute(ctx)
	assert.NoError(t, err)
	finals := map[string]gent.StreamChunk{}
	previews := map[string]string{}
	for chunk := range chunks {
		for _, meta := range chunk.MetaContent {
			previews[chunk.ContextId] = meta.MessageId
		}
		if chunk.MessageId != "" {
			_, exists := finals[chunk.ContextId]
			assert.False(t, exists)
			finals[chunk.ContextId] = chunk
		}
	}
	assert.Len(t, finals, 3)
	assert.Equal(t, "Parent answer.", finals[ctx.ContextId()].Content)
	assert.Nil(t, finals[ctx.ContextId()].SourceMessageIds)
	assert.NotEqual(t, previews[children[0].ContextId()], previews[children[1].ContextId()])
	for _, child := range children {
		final := finals[child.ContextId()]
		assert.Equal(t, ctx.ContextId(), final.ParentContextId)
		assert.Equal(t, "Child answer.", final.Content)
		assert.Equal(t, child.ContextId()+":answer", final.MessageId)
		assert.Equal(t, []string{previews[child.ContextId()]}, final.SourceMessageIds)
	}
	view, err := ctx.Export()
	assert.NoError(t, err)
	assert.NoError(t, gent.ValidateBundle(view, ctx.Codecs()))
}

func TestContentIdentity_NewRequestsDoNotReusePreviewIDs(t *testing.T) {
	ctx := gent.NewExecutionContext(context.Background(), "root", nil)
	defer ctx.Release()
	child := ctx.SpawnChild("child", nil)
	defer child.Release()
	model := &contentModel{replies: []contentReply{
		{choice: &gent.ContentChoice{CandidateContent: "Partial."},
			deltas: []string{"Partial."}, err: context.Canceled},
		{choice: &gent.ContentChoice{CandidateContent: "Retry."}, deltas: []string{"Retry."}},
		{choice: &gent.ContentChoice{CandidateContent: "Child."}, deltas: []string{"Child."}},
	}}
	seen := map[string]bool{}
	for index, owner := range []*gent.ExecutionContext{ctx, ctx, child} {
		stream, err := owner.GenerateContentStream(model,
			gent.ModelCall{Id: "answer"}, gent.NewModelCallRequest(nil))
		if !assert.NoError(t, err) {
			return
		}
		var ids []string
		for chunk := range stream.Chunks() {
			for _, item := range chunk.MetaContent {
				ids = append(ids, item.MessageId)
			}
		}
		response, err := stream.Response()
		if index == 0 {
			assert.ErrorIs(t, err, context.Canceled)
		} else {
			assert.NoError(t, err)
		}
		if !assert.Len(t, ids, 1) {
			return
		}
		assert.Regexp(t, "^"+regexp.QuoteMeta(owner.ContextId())+
			":message:[a-f0-9]{32}:message:0$", ids[0])
		assert.False(t, seen[ids[0]], "a new provider request must have a new preview ID")
		seen[ids[0]] = true
		assert.Equal(t, &gent.ContentSource{ModelCallId: "answer", MessageIds: ids},
			response.Choices[0].CandidateSource)
	}
}

func TestExecutionContent_ProviderContinuationIsNotFinal(t *testing.T) {
	continuation := &gent.ProviderContinuation{Provider: "fixture", Items: []json.RawMessage{
		json.RawMessage(`{"type":"server_tool_use","id":"work"}`)}}
	model := &contentModel{replies: []contentReply{
		{choice: &gent.ContentChoice{CandidateContent: "Still working.",
			StopReason: "pause_turn", Continuation: continuation}, deltas: []string{"Still working."}},
		{choice: &gent.ContentChoice{CandidateContent: "Done."}, deltas: []string{"Done."}},
	}}
	ctx := gent.NewExecutionContext(context.Background(), "root",
		gent.NewBasicLoopData(&gent.Task{Text: "Finish the work."}))
	chunks, unsubscribe := ctx.SubscribeAll()
	defer unsubscribe()
	result, err := executor.NewRunner(react.NewAgent(model), executor.DefaultConfig()).Execute(ctx)
	assert.NoError(t, err)
	assert.Equal(t, &gent.ExecutionResult{TerminationReason: gent.TerminationSuccess,
		Output: []gent.ContentPart{gent.TextContent{Text: "Done."}}}, result)
	if !assert.Len(t, model.requests, 2) {
		return
	}
	assert.Equal(t, continuation, model.requests[1].Messages[2].Continuation)
	var contents []string
	for chunk := range chunks {
		if chunk.MessageId != "" {
			contents = append(contents, chunk.Content)
		}
	}
	assert.Equal(t, []string{"Done."}, contents)
}

type heldContentObserver struct {
	started chan struct{}
	release chan struct{}
}

func (observer *heldContentObserver) ObserveCommitted(
	events []*gent.RecordedEvent, _ *gent.CodecRegistry,
) error {
	for _, event := range events {
		if event.EventName == "hold" {
			close(observer.started)
			<-observer.release
		}
	}
	return nil
}

func TestExecutionContent_ObserverDelayDoesNotLoseFinalChunk(t *testing.T) {
	observer := &heldContentObserver{started: make(chan struct{}), release: make(chan struct{})}
	ctx := gent.NewExecutionContext(context.Background(), "root", nil, gent.WithObserver(observer))
	chunks, unsubscribe := ctx.SubscribeAll()
	defer unsubscribe()
	ctx.PublishCommonEvent("hold", "Hold one observer during concurrent completion.", nil)
	flushed := make(chan error, 1)
	go func() { flushed <- ctx.FlushProgress() }()
	<-observer.started
	loop := contentLoop(func(*gent.ExecutionContext) (*gent.AgentLoopResult, error) {
		return &gent.AgentLoopResult{Action: gent.LATerminate,
			Result: []gent.ContentPart{gent.TextContent{Text: "Done."}}}, nil
	})
	_, err := executor.NewRunner(loop, executor.DefaultConfig()).Execute(ctx)
	close(observer.release)
	assert.NoError(t, err)
	assert.NoError(t, <-flushed)
	var contents []string
	for chunk := range chunks {
		contents = append(contents, chunk.Content)
	}
	assert.Equal(t, []string{"Done."}, contents)
}

type contentAnswer struct {
	Status string `json:"status"`
}

type contentValidator struct{}

func (*contentValidator) Name() string { return "status" }
func (*contentValidator) Validate(_ *gent.ExecutionContext, answer any) *gent.ValidationResult {
	return &gent.ValidationResult{Accepted: answer.(contentAnswer).Status == "done",
		Feedback: []gent.FormattedSection{{Name: "error", Content: "Check the status."}}}
}

// The terminal save can pause, fail before acceptance, or lose its acknowledgment. This exercises
// the same public storage boundary that a database adapter uses, without changing run internals.
type contentStore struct {
	mu       sync.Mutex
	view     gent.RunView
	terminal *gent.SaveBatch
	reached  chan struct{}
	release  chan struct{}
	err      error
	accept   bool
	failed   bool
}

func (store *contentStore) Save(_ context.Context, batch *gent.SaveBatch) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if batch.Checkpoint != nil && batch.Checkpoint.Phase == gent.PhaseTerminal && !store.failed {
		store.terminal = batch
		if store.reached != nil {
			close(store.reached)
			<-store.release
		}
		if store.err != nil {
			store.failed = true
			if !store.accept {
				saveErr := store.err
				store.err = nil
				return saveErr
			}
		}
	}
	view, err := gent.ApplyBatch(store.view, batch)
	if err != nil {
		return err
	}
	store.view = view
	if store.failed && store.err != nil {
		err, store.err = store.err, nil
		return err
	}
	return nil
}

func contentChunks(
	t *testing.T, events []*gent.RecordedEvent, codecs *gent.CodecRegistry,
) []gent.StreamChunk {
	t.Helper()
	var chunks []gent.StreamChunk
	for _, event := range events {
		if event.Type != "model_stream_chunk" && event.Type != "execution_content" {
			continue
		}
		value, err := codecs.DecodeEvent(event)
		if !assert.NoError(t, err) {
			return nil
		}
		chunks = append(chunks, value.(gent.StreamChunk))
	}
	return chunks
}

func TestExecutionContent_AcceptSaveAndRestore(t *testing.T) {
	type input struct{ fail, accept bool }
	type expected struct {
		finals int
		err    error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	saveErr := errors.New("terminal save failed")
	cases := []testCase{
		{"accepted", input{}, expected{1, nil}},
		{"failed save", input{fail: true}, expected{0, saveErr}},
		{"lost acknowledgment", input{true, true}, expected{0, saveErr}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			store := &contentStore{view: gent.RunView{
				SchemaVersion: gent.RunSchemaVersion, RunId: "content"},
				reached: make(chan struct{}), release: make(chan struct{}), accept: test.input.accept}
			if test.input.fail {
				store.err = saveErr
			}
			var release sync.Once
			t.Cleanup(func() { release.Do(func() { close(store.release) }) })
			seq, err := trace.NewSequencer("content", trace.Config{
				IncludeChunkText: true, IncludeRunOutput: true})
			if !assert.NoError(t, err) {
				return
			}
			defer seq.Close()
			model := &contentModel{replies: []contentReply{
				{choice: &gent.ContentChoice{CandidateContent: "Looking up.",
					ToolCalls: []gent.ModelToolCall{{ID: "lookup", Type: "function",
						FunctionCall: &gent.FunctionCall{Name: "lookup", Arguments: "{}"}}}},
					deltas: []string{"Looking up."}},
				{choice: &gent.ContentChoice{CandidateContent: `{"status":"pending"}`},
					deltas: []string{`{"status":"pending"}`}},
				{choice: &gent.ContentChoice{CandidateContent: ` { "status" : "done" } `},
					deltas: []string{` { "status" : `, `"done" } `}},
			}}
			term := termination.NewJSON[contentAnswer]("answer")
			term.SetValidator(&contentValidator{})
			toolCalls := 0
			agent := react.NewAgent(model).WithTermination(term).WithRecoveryVersion("content-v1").
				RegisterTool(gent.DefineTool("lookup", func(context.Context, map[string]any) (string, error) {
					toolCalls++
					return "done", nil
				}).Description("Read the status").ParameterSchema(nil).MustBuild())
			ctx := gent.NewExecutionContext(context.Background(), "root",
				gent.NewBasicLoopData(&gent.Task{Text: "Read the status."}),
				gent.WithRunId("content"), gent.WithRecorder(store), gent.WithObserver(seq))
			chunks, unsubscribe := ctx.SubscribeAll()
			defer unsubscribe()
			type outcome struct {
				result *gent.ExecutionResult
				err    error
			}
			done := make(chan outcome, 1)
			started := time.Now()
			go func() {
				result, err := executor.NewRunner(agent, executor.DefaultConfig()).Execute(ctx)
				done <- outcome{result, err}
			}()
			var live []gent.StreamChunk
			for range 4 {
				select {
				case chunk := <-chunks:
					live = append(live, chunk)
				case <-time.After(5 * time.Second):
					t.Fatal("missing provisional chunk")
				}
			}
			select {
			case <-store.reached:
			case <-time.After(5 * time.Second):
				t.Fatal("terminal save was not reached")
			}
			var texts []string
			for _, chunk := range live {
				assert.Equal(t, "", chunk.Content)
				texts = append(texts, gent.MetaText(chunk.MetaContent, gent.MetaContentIntermediary))
				assert.GreaterOrEqual(t, chunk.Timestamp.UnixNano(), started.UnixNano())
			}
			assert.Equal(t, []string{"Looking up.", `{"status":"pending"}`,
				` { "status" : `, `"done" } `}, texts)
			assert.Equal(t, "", seq.Snapshot().Contexts[0].Content)
			select {
			case chunk := <-chunks:
				t.Errorf("chunk arrived before terminal acceptance: %#v", chunk)
			default:
			}
			release.Do(func() { close(store.release) })
			finished := <-done
			assert.ErrorIs(t, finished.err, test.expected.err)
			finals := 0
			for chunk := range chunks {
				live = append(live, chunk)
				if chunk.MessageId != "" {
					finals++
				}
			}
			assert.Equal(t, test.expected.finals, finals)
			assert.Equal(t, 3, len(model.requests))
			assert.Equal(t, 1, toolCalls)
			assert.Equal(t, int64(18), ctx.Stats().GetCounter(gent.SCTotalTokens))
			assert.Equal(t, int64(1), ctx.Stats().GetCounter(gent.SCAnswerRejectedTotal))
			terminal := contentChunks(t, store.terminal.Events, ctx.Codecs())
			if !assert.Len(t, terminal, 1) {
				return
			}
			final := terminal[0]
			assert.Equal(t, gent.StreamChunk{Content: `{"status":"done"}`,
				MessageId:        ctx.ContextId() + ":answer",
				SourceMessageIds: []string{live[3].MetaContent[0].MessageId},
				RunId:            ctx.RunId(), EventNumber: final.EventNumber, Timestamp: final.Timestamp,
				Iteration: 3, Source: "root/3", ContextId: ctx.ContextId(),
				ModelCallId: ctx.ContextId() + ":model:3", StreamId: "iter-3",
				StreamTopicId: "llm-response"}, final)
			assert.GreaterOrEqual(t, final.Timestamp.UnixNano(), started.UnixNano())
			assert.Equal(t, gent.PhaseTerminal, store.terminal.Checkpoint.Phase)
			bundle, err := ctx.Export()
			assert.NoError(t, err)
			assert.Equal(t, live, contentChunks(t, bundle.Activity, ctx.Codecs()))
			if test.input.fail {
				bundle, err = ctx.RetrySave(context.Background())
				if !assert.NoError(t, err) {
					return
				}
			}
			var historyText []string
			for _, entry := range bundle.History.Entries {
				for _, message := range entry.Messages {
					if message.Role != gent.ChatMessageTypeAI {
						continue
					}
					for _, part := range message.Parts {
						if text, ok := part.(gent.TextContent); ok {
							historyText = append(historyText, text.Text)
						}
					}
				}
			}
			assert.Equal(t, []string{"Looking up.", `{"status":"pending"}`,
				` { "status" : "done" } `}, historyText)
			// Reconnect uses the accepted event cursor. Replaying the same evidence is idempotent.
			assert.NoError(t, seq.ObserveCommitted(bundle.Activity, ctx.Codecs()))
			assert.NoError(t, seq.ObserveCommitted(bundle.Activity, ctx.Codecs()))
			assert.Equal(t, final.Content, seq.Snapshot().Contexts[0].Content)
			assert.Equal(t, final.SourceMessageIds, seq.Snapshot().Contexts[0].SourceMessageIds)
			encoded, err := ctx.Codecs().Marshal(bundle)
			assert.NoError(t, err)
			var saved gent.RunView
			assert.NoError(t, ctx.Codecs().Unmarshal(encoded, &saved))
			assert.NoError(t, gent.ValidateBundle(saved, ctx.Codecs()))
			fresh := &contentModel{}
			result, err := executor.NewRunner(react.NewAgent(fresh), executor.DefaultConfig()).Resume(
				context.Background(), saved, gent.RestoreBindings{Codecs: ctx.Codecs()})
			assert.NoError(t, err)
			assert.Equal(t, &gent.ExecutionResult{TerminationReason: gent.TerminationSuccess,
				Output: []gent.ContentPart{gent.TextContent{Text: `{"status":"done"}`}}}, result)
			assert.Empty(t, fresh.requests)
			assert.Equal(t, append(live[:4:4], final), contentChunks(t, saved.Activity, ctx.Codecs()))
		})
	}
}
