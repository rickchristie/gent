package gent_test

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents/react"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/trace"
	"github.com/stretchr/testify/assert"
)

type savedReplyModel struct {
	responses    []string
	calls        int
	responseOnly bool
}

func (m *savedReplyModel) GenerateContentStream(
	_ context.Context, _ gent.ModelCallRequest,
) (gent.Stream, error) {
	if m.calls >= len(m.responses) {
		return nil, fmt.Errorf("unexpected model call %d", m.calls+1)
	}
	response := &gent.ContentResponse{
		Choices: []*gent.ContentChoice{
			{
				Completion:       gent.ModelCompletion{Status: gent.ModelStatusAnswer},
				CandidateContent: []gent.ContentPart{gent.TextContent{Text: m.responses[m.calls]}},
			},
		},
	}
	m.calls++
	if m.responseOnly {
		stream := gent.NewStreamWithDuration()
		stream.Complete(response, nil)
		return stream, nil
	}
	return gent.NewCompletedStream(response, nil), nil
}

type failedDecisionRecorder struct {
	view    gent.RunView
	codecs  *gent.CodecRegistry
	failure error
}

func (r *failedDecisionRecorder) Save(_ context.Context, batch *gent.SaveBatch) error {
	if batch.Checkpoint != nil && batch.Checkpoint.Phase == gent.PhaseDecisionReady {
		return r.failure
	}
	next, err := gent.ApplyBatch(r.view, batch, r.codecs)
	if err != nil {
		return err
	}
	r.view = next
	return nil
}

func TestRunResumeBeforeDecisionCheckpoint(t *testing.T) {
	type input struct{ responseOnly bool }
	type expected struct {
		result *gent.ExecutionResult
		calls  int
		err    error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	want := expected{result: &gent.ExecutionResult{TerminationReason: gent.TerminationSuccess,
		Output: []gent.ContentPart{gent.TextContent{Text: "original"}}}}
	cases := []testCase{
		{"saved chunks", input{}, want},
		{"saved response without chunks", input{true}, want},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			codecs := gent.NewCodecRegistry()
			failure := errors.New("decision checkpoint unavailable")
			store := &failedDecisionRecorder{view: gent.RunView{
				SchemaVersion: gent.RunSchemaVersion, RunId: "saved-response",
			}, codecs: codecs, failure: failure}
			ctx := gent.NewExecutionContext(context.Background(), "review",
				gent.NewBasicLoopData(gent.NewTask(gent.TextContent{
					Text: "review",
				})), gent.WithRunId("saved-response"),
				gent.WithCodecs(codecs), gent.WithRecorder(store))
			original := &savedReplyModel{responses: []string{"original"},
				responseOnly: test.input.responseOnly}
			agent := react.NewAgent(original).WithRecoveryVersion("review/v1")
			_, _, err := executor.NewRunner(agent, executor.DefaultConfig()).Execute(ctx)
			if !assert.ErrorIs(t, err, failure) {
				return
			}
			if !assert.Equal(t, gent.PhaseIterationReady, store.view.Checkpoint.Phase) {
				return
			}
			fresh := &savedReplyModel{responses: []string{"replacement"},
				responseOnly: test.input.responseOnly}
			freshAgent := react.NewAgent(fresh).WithRecoveryVersion("review/v1")
			_, result, err := executor.NewRunner(freshAgent, executor.DefaultConfig()).Resume(
				context.Background(), store.view, gent.RestoreBindings{})
			assert.Equal(t, test.expected, expected{result, fresh.calls, err})
		})
	}
}

func TestRunCustomSettings(t *testing.T) {
	type input struct {
		configure func(*react.Agent[string]) *react.Agent[string]
	}
	type expected struct {
		output []gent.ContentPart
		calls  int
		err    error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	want := expected{output: []gent.ContentPart{gent.TextContent{Text: "ok"}}, calls: 1}
	cases := []testCase{
		{name: "default", input: input{func(a *react.Agent[string]) *react.Agent[string] { return a }},
			expected: want},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model := &savedReplyModel{responses: []string{"ok"}}
			agent := tc.input.configure(react.NewAgent(model))
			ctx := gent.NewExecutionContext(context.Background(), "review",
				gent.NewBasicLoopData(gent.NewTask(gent.TextContent{Text: "review"})))
			_, result, err := executor.NewRunner(agent, executor.DefaultConfig()).Execute(ctx)
			got := expected{calls: model.calls, err: err}
			if result != nil {
				got.output = result.Output
			}
			assert.Equal(t, tc.expected, got)
		})
	}
}

func TestRunCustomSettingsRecovery(t *testing.T) {
	type input struct {
		configure func(*react.Agent[string]) *react.Agent[string]
		version   string
	}
	type expected struct {
		result   *gent.ExecutionResult
		recovery string
		calls    int
		err      error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	configurations := map[string]func(*react.Agent[string]) *react.Agent[string]{
		"output options": func(a *react.Agent[string]) *react.Agent[string] {
			return a.WithCallOptions(gent.WithMaxTokens(256))
		},
	}
	var cases []testCase
	for name, configure := range configurations {
		cases = append(cases,
			testCase{name + "/without_version", input{configure, ""}, expected{
				recovery: "unsupported", err: gent.ErrUnsupportedRecovery}},
			testCase{name + "/with_version", input{configure, "settings/v1"}, expected{
				recovery: "supported", result: &gent.ExecutionResult{
					TerminationReason: gent.TerminationSuccess,
					Output:            []gent.ContentPart{gent.TextContent{Text: "saved answer"}},
				}}})
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			failure := errors.New("decision checkpoint unavailable")
			store := &failedDecisionRecorder{view: gent.RunView{
				SchemaVersion: gent.RunSchemaVersion, RunId: "settings",
			}, codecs: gent.NewCodecRegistry(), failure: failure}
			ctx := gent.NewExecutionContext(context.Background(), "root",
				gent.NewBasicLoopData(gent.NewTask(gent.TextContent{Text: "Read the saved answer"})),
				gent.WithRunId("settings"), gent.WithCodecs(store.codecs), gent.WithRecorder(store))
			original := &savedReplyModel{responses: []string{"saved answer"}}
			agent := test.input.configure(react.NewAgent(original)).
				WithRecoveryVersion(test.input.version)
			_, _, err := executor.NewRunner(agent, executor.DefaultConfig()).Execute(ctx)
			if !assert.ErrorIs(t, err, failure) {
				return
			}
			assert.Equal(t, 1, original.calls)
			assert.Equal(t, test.expected.recovery,
				store.view.Checkpoint.Compatibility.Components["gent:recovery"])
			fresh := &savedReplyModel{}
			freshAgent := test.input.configure(react.NewAgent(fresh)).
				WithRecoveryVersion(test.input.version)
			_, result, err := executor.NewRunner(freshAgent, executor.DefaultConfig()).Resume(
				context.Background(), store.view, gent.RestoreBindings{})
			assert.ErrorIs(t, err, test.expected.err)
			assert.Equal(t, test.expected.result, result)
			assert.Equal(t, test.expected.calls, fresh.calls)
		})
	}
}

func TestModelReplayExplicitId(t *testing.T) {
	type input struct{ call gent.ModelCall }
	type expected struct {
		content       string
		providerCalls int
		err           error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{name: "replay_first_saved_call", input: input{gent.ModelCall{Id: "first"}},
			expected: expected{content: "first answer"}},
		{name: "new_logical_call", input: input{gent.ModelCall{Id: "third"}},
			expected: expected{content: "third answer", providerCalls: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := gent.NewExecutionContext(context.Background(), "review",
				gent.NewBasicLoopData(gent.NewTask(gent.TextContent{Text: "review"})))
			ctx.IncrementIteration()
			if !assert.NoError(t, ctx.CommitCheckpoint(gent.PhaseIterationReady)) {
				return
			}
			original := &savedReplyModel{responses: []string{"first answer", "second answer"}}
			for _, id := range []string{"first", "second"} {
				stream, err := ctx.GenerateContentStream(original, gent.ModelCall{Id: id},
					gent.NewModelCallRequest(nil))
				if !assert.NoError(t, err) {
					return
				}
				_, err = stream.Response()
				stream.Close()
				if !assert.NoError(t, err) {
					return
				}
			}
			saved, err := ctx.Export()
			if !assert.NoError(t, err) {
				return
			}
			restored, err := gent.RestoreExecutionContext(
				context.Background(), saved, gent.RestoreBindings{})
			if !assert.NoError(t, err) {
				return
			}
			fresh := &savedReplyModel{responses: []string{"third answer"}}
			stream, err := restored.GenerateContentStream(fresh, tc.input.call,
				gent.NewModelCallRequest(nil))
			if !assert.NoError(t, err) {
				return
			}
			response, err := stream.Response()
			stream.Close()
			got := expected{content: gent.ContentText(response.Choices[0].CandidateContent),
				providerCalls: fresh.calls, err: err}
			assert.Equal(t, tc.expected, got)
		})
	}
}

func TestRunTraceOutputPolicy(t *testing.T) {
	type input struct{ config trace.Config }
	type expected struct {
		runOutput       any
		iterationOutput any
		err             error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{name: "excluded", input: input{trace.Config{}}, expected: expected{}},
		{name: "redacted", input: input{trace.Config{IncludeRunOutput: true,
			Redactor: trace.RedactorFuncs{RunOutput: func([]gent.ContentPart) any {
				return "REDACTED"
			}}}},
			expected: expected{runOutput: "REDACTED", iterationOutput: "REDACTED"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seq, err := trace.NewSequencer("review", tc.input.config)
			if !assert.NoError(t, err) {
				return
			}
			defer seq.Close()
			live, unsubscribe := seq.Subscribe()
			defer unsubscribe()
			ctx := gent.NewExecutionContext(context.Background(), "review",
				gent.NewBasicLoopData(gent.NewTask(gent.TextContent{Text: "review"})),
				gent.WithRunId("review"), gent.WithObserver(seq))
			model := &savedReplyModel{responses: []string{"PRIVATE ANSWER"}}
			runner := executor.NewRunner(react.NewAgent(model), executor.DefaultConfig())
			_, _, err = runner.Execute(ctx)
			snapshot := seq.Snapshot()
			if !assert.Len(t, snapshot.Iterations, 1) {
				return
			}
			var iterationOutput any
			for _, event := range snapshot.RecentEvents {
				if event.Type == trace.EventTypeIterationFinished {
					iterationOutput = event.Payload.(map[string]any)["result"]
				}
			}
			got := expected{snapshot.Result.Output, iterationOutput, err}
			assert.Equal(t, tc.expected, got)
			seq.Close()
			var events []*trace.Event
			for event := range live {
				events = append(events, event)
			}
			assert.Equal(t, snapshot.RecentEvents, events)
			recent, err := seq.EventsAfter(0)
			assert.NoError(t, err)
			assert.Equal(t, events, recent)
			bundle, err := ctx.Export()
			if !assert.NoError(t, err) {
				return
			}
			restored, err := trace.RestoreSequencer(bundle, tc.input.config)
			if !assert.NoError(t, err) {
				return
			}
			defer restored.Close()
			assert.Equal(t, snapshot, restored.Snapshot())
			recent, err = restored.EventsAfter(0)
			assert.NoError(t, err)
			assert.Equal(t, events, recent)
		})
	}
}

func TestRunConcurrentHistoryAndIteration(t *testing.T) {
	ctx := gent.NewExecutionContext(context.Background(), "review", gent.NewBasicLoopData(nil))
	var wg sync.WaitGroup
	wg.Add(2)
	errors := make(chan error, 1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10000; i++ {
			ctx.IncrementIteration()
			runtime.Gosched()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			if err := ctx.AppendRecord(&gent.HistoryEntry{}, false); err != nil {
				errors <- err
				return
			}
		}
	}()
	wg.Wait()
	close(errors)
	for err := range errors {
		assert.NoError(t, err)
	}
	assert.Equal(t, 10000, ctx.Iteration())
	assert.NoError(t, ctx.FlushProgress())
	view, err := ctx.Export()
	assert.NoError(t, err)
	assert.Len(t, view.History.Entries, 100)
}
