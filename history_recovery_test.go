package gent_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents/react"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/trace"
	"github.com/stretchr/testify/assert"
)

type recoveryInput struct {
	Id string `json:"id"`
}

type recoveryModel struct {
	requests []gent.ModelCallRequest
	answer   bool
}

func (m *recoveryModel) GenerateContentStream(
	_ context.Context, request gent.ModelCallRequest,
) (gent.Stream, error) {
	m.requests = append(m.requests, request)
	choice := &gent.ContentChoice{
		Completion:       gent.ModelCompletion{Status: gent.ModelStatusAnswer},
		CandidateContent: []gent.ContentPart{gent.TextContent{Text: "Policy P10 permits it."}},
	}
	if !m.answer && len(m.requests) == 1 {
		choice.CandidateContent = []gent.ContentPart{gent.TextContent{Text: "Read the policy."}}
		for _, id := range []string{"policy-a", "policy-b"} {
			choice.Completion.Status = gent.ModelStatusToolCalls
			choice.ToolCalls = append(choice.ToolCalls, gent.ModelToolCall{ID: id, Type: "function",
				FunctionCall: &gent.FunctionCall{Name: "policy", Arguments: `{"id":"P10"}`}})
		}
	}
	return gent.NewCompletedStream(&gent.ContentResponse{
		Choices: []*gent.ContentChoice{choice},
		Info:    &gent.GenerationInfo{InputTokens: 12, OutputTokens: 8, TotalTokens: 20},
	}, nil), nil
}

func newRecoveryAgent(model *recoveryModel, calls *[]gent.ToolIdentity,
	recovery ...gent.ToolRecoveryFunc,
) *react.Agent[string] {
	var handler gent.ToolRecoveryFunc
	if len(recovery) != 0 {
		handler = recovery[0]
	}
	tool := gent.DefineTool("policy", func(ctx context.Context, input recoveryInput) (string, error) {
		identity, ok := gent.ToolIdentityFromContext(ctx)
		if !ok {
			return "", errors.New("missing logical tool identity")
		}
		*calls = append(*calls, identity)
		return input.Id + ": exact original text", nil
	}).Description("Read the original policy").ParameterSchema(nil).Recovery(handler).MustBuild()
	return react.NewAgent(model).RegisterTool(tool).WithRecoveryVersion("recovery-fixture/v1")
}

type recoveryStore struct {
	mu      sync.Mutex
	view    gent.RunView
	cancel  context.CancelFunc
	phase   gent.ExecutionPhase
	event   string
	stopped bool
	after   int
	hits    int
	codecs  *gent.CodecRegistry
}

func (s *recoveryStore) Save(_ context.Context, batch *gent.SaveBatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next, err := gent.ApplyBatch(s.view, batch, s.codecs)
	if err != nil {
		return err
	}
	s.view = next
	stop := batch.Checkpoint != nil && batch.Checkpoint.Phase == s.phase
	for _, event := range batch.Events {
		stop = stop || event.Type == s.event
	}
	if stop {
		s.hits++
	}
	if stop && s.hits >= s.after && !s.stopped && s.cancel != nil {
		s.stopped = true
		s.cancel()
	}
	return nil
}

func recoveryBindings(t *testing.T, agent *react.Agent[string]) gent.RestoreBindings {
	t.Helper()
	codecs := gent.NewCodecRegistry()
	if !assert.NoError(t, agent.RegisterCodecs(codecs)) {
		t.FailNow()
	}
	return gent.RestoreBindings{Codecs: codecs}
}

func resumeRecovery(agent *react.Agent[string], saved gent.RunView, bindings gent.RestoreBindings) (
	*gent.ExecutionContext, *gent.ExecutionResult, error,
) {
	var restored *gent.ExecutionContext
	prepare := bindings.Configure
	bindings.Configure = func(ctx *gent.ExecutionContext) error {
		restored = ctx
		if prepare != nil {
			return prepare(ctx)
		}
		return nil
	}
	_, result, err := executor.NewRunner(agent, executor.DefaultConfig()).Resume(
		context.Background(), saved, bindings)
	return restored, result, err
}

func saveInterruptedRun(
	t *testing.T, phase gent.ExecutionPhase, event string,
) (gent.RunView, []gent.ModelCallRequest, []gent.ToolIdentity) {
	t.Helper()
	worker, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &recoveryStore{
		codecs: gent.NewCodecRegistry(),
		view:   gent.RunView{SchemaVersion: gent.RunSchemaVersion, RunId: "recovery"},
		cancel: cancel,
		phase:  phase,
		event:  event,
	}
	model := &recoveryModel{}
	var calls []gent.ToolIdentity
	agent := newRecoveryAgent(model, &calls)
	ctx := gent.NewExecutionContext(worker, "root", gent.NewBasicLoopData(
		gent.NewTask(gent.TextContent{
			Text: "Read policy P10 twice, then answer.",
		})), gent.WithRunId("recovery"),
		gent.WithRecorder(store), gent.WithCodecs(store.codecs))
	ctx.SetLimits(nil)
	_, _, err := executor.NewRunner(agent, executor.DefaultConfig()).Execute(ctx)
	if !assert.ErrorIs(t, err, context.Canceled) {
		t.FailNow()
	}
	bundle, err := ctx.Export()
	if !assert.NoError(t, err) || !assert.NoError(t, gent.ValidateBundle(bundle, ctx.Codecs())) {
		t.FailNow()
	}
	assert.NotEqual(t, gent.PhaseTerminal, bundle.Checkpoint.Phase)
	return bundle, model.requests, calls
}

func TestHistoryRecovery_Boundaries(t *testing.T) {
	type input struct {
		phase gent.ExecutionPhase
		event string
	}
	type expected struct {
		modelsBefore int
		modelsAfter  int
		toolsBefore  int
		toolsAfter   int
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"initial", input{phase: gent.PhaseInitial}, expected{0, 2, 0, 2}},
		{"iteration ready", input{phase: gent.PhaseIterationReady}, expected{0, 2, 0, 2}},
		{"saved response", input{event: "model_call_finished"}, expected{1, 1, 0, 2}},
		{"decision ready", input{phase: gent.PhaseDecisionReady}, expected{1, 1, 0, 2}},
		{"first tool completed", input{phase: gent.PhaseToolCompleted}, expected{1, 1, 1, 1}},
		{"iteration completed", input{phase: gent.PhaseIterationCompleted}, expected{1, 1, 2, 0}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			bundle, requests, earlierCalls := saveInterruptedRun(
				t,
				test.input.phase,
				test.input.event,
			)
			// JSON is the boundary: continuation receives no runtime pointers from the producer.
			model := &recoveryModel{answer: len(requests) > 0}
			var calls []gent.ToolIdentity
			agent := newRecoveryAgent(model, &calls)
			bindings := recoveryBindings(t, agent)
			data, err := bindings.Codecs.Marshal(bundle)
			assert.NoError(t, err)
			var saved gent.RunView
			assert.NoError(t, bindings.Codecs.Unmarshal(data, &saved))
			ctx, result, err := resumeRecovery(agent, saved, bindings)
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, []gent.ContentPart{gent.TextContent{Text: "Policy P10 permits it."}},
				result.Output)
			assert.Equal(t, test.expected, expected{len(requests), len(model.requests),
				len(earlierCalls), len(calls)})
			assert.Equal(t, int64(40), ctx.Stats().GetCounter(gent.SCTotalTokens))
			assert.Equal(t, int64(2), ctx.Stats().GetCounter(gent.SCToolCalls))
			allCalls := append(earlierCalls, calls...)
			if assert.Len(t, allCalls, 2) {
				assert.NotEqual(t, allCalls[0].ToolCallId, allCalls[1].ToolCallId)
			}
			terminal, err := ctx.Export()
			assert.NoError(t, err)
			assert.NoError(t, gent.ValidateBundle(terminal, ctx.Codecs()))
			assert.Greater(t, terminal.LastAppliedEventNumber, saved.LastAppliedEventNumber)
			assert.Equal(t, saved.RunId, terminal.RunId)
			// A terminal bundle needs no executable bindings, including model/tool implementations.
			finished, err := gent.RestoreExecutionContext(context.Background(), terminal,
				gent.RestoreBindings{Codecs: bindings.Codecs})
			if assert.NoError(t, err) {
				_, out, err := executor.NewRunner(agent, executor.DefaultConfig()).Continue(
					finished,
				)
				assert.NoError(t, err)
				assert.Equal(t, result.Output, out.Output)
			}
		})
	}
}

type recoveryFile struct {
	Bundle   json.RawMessage
	Requests []gent.EncodedValue
}

func TestHistoryRecovery_FreshProcess(t *testing.T) {
	mode := os.Getenv("GENT_HISTORY_PROCESS")
	path := os.Getenv("GENT_HISTORY_BUNDLE")
	if mode == "produce" {
		bundle, requests, calls := saveInterruptedRun(t, gent.PhaseToolCompleted, "")
		assert.Len(t, calls, 1)
		agent := newRecoveryAgent(&recoveryModel{}, new([]gent.ToolIdentity))
		registry := recoveryBindings(t, agent).Codecs
		encoded, err := registry.Marshal(bundle)
		assert.NoError(t, err)
		file := recoveryFile{Bundle: encoded}
		for _, request := range requests {
			value, err := gent.NewCodecRegistry().Encode(request)
			assert.NoError(t, err)
			file.Requests = append(file.Requests, value)
		}
		data, err := json.Marshal(file)
		assert.NoError(t, err)
		assert.NoError(t, os.WriteFile(path, data, 0600))
		return
	}
	if mode == "continue" {
		data, err := os.ReadFile(path)
		assert.NoError(t, err)
		var file recoveryFile
		assert.NoError(t, json.Unmarshal(data, &file))
		model := &recoveryModel{answer: true}
		var calls []gent.ToolIdentity
		agent := newRecoveryAgent(model, &calls)
		bindings := recoveryBindings(t, agent)
		var bundle gent.RunView
		assert.NoError(t, bindings.Codecs.Unmarshal(file.Bundle, &bundle))
		seq, err := trace.RestoreSequencer(bundle, trace.Config{Codecs: bindings.Codecs})
		assert.NoError(t, err)
		bindings.Observers = []gent.RecordedEventObserver{seq}
		ctx, result, err := resumeRecovery(agent, bundle, bindings)
		if !assert.NoError(t, err) {
			return
		}
		assert.Equal(
			t,
			[]gent.ContentPart{gent.TextContent{Text: "Policy P10 permits it."}},
			result.Output,
		)
		assert.Len(t, calls, 1)
		assert.Len(t, model.requests, 1)
		assert.Equal(t, int64(40), ctx.Stats().GetCounter(gent.SCTotalTokens))
		// A fresh uninterrupted binding must send exactly the same next model messages and options.
		baselineModel := &recoveryModel{}
		var baselineCalls []gent.ToolIdentity
		baselineAgent := newRecoveryAgent(baselineModel, &baselineCalls)
		baseline := gent.NewExecutionContext(context.Background(), "root", gent.NewBasicLoopData(
			gent.NewTask(gent.TextContent{Text: "Read policy P10 twice, then answer."})))
		baseline.SetLimits(nil)
		_, _, err = executor.NewRunner(baselineAgent, executor.DefaultConfig()).Execute(
			baseline,
		)
		assert.NoError(t, err)
		assert.Equal(t, baselineModel.requests[1].Messages, model.requests[0].Messages)
		assert.Equal(t, baselineModel.requests[1].Options, model.requests[0].Options)
		assert.Equal(t, seq.Snapshot().LastEventNumber, uint64(len(seq.Snapshot().RecentEvents)))
		return
	}
	path = filepath.Join(t.TempDir(), "run.json")
	for _, mode := range []string{"produce", "continue"} {
		command := exec.Command(os.Args[0], "-test.run=^TestHistoryRecovery_FreshProcess$")
		command.Env = append(
			os.Environ(),
			"GENT_HISTORY_PROCESS="+mode,
			"GENT_HISTORY_BUNDLE="+path,
		)
		log, err := os.CreateTemp("/tmp", "gent-history-process-"+mode+"-*.log")
		if !assert.NoError(t, err) {
			return
		}
		command.Stdout, command.Stderr = log, log
		err = command.Run()
		assert.NoError(t, log.Close())
		if !assert.NoError(t, err, "subprocess output: %s", log.Name()) {
			return
		}
	}
}

func recoveryChoice() *gent.ContentChoice {
	return &gent.ContentChoice{Completion: gent.ModelCompletion{
		Status: gent.ModelStatusToolCalls,
	},
		ToolCalls: []gent.ModelToolCall{
			{ID: "policy-a", Type: "function",
				FunctionCall: &gent.FunctionCall{Name: "policy", Arguments: `{"id":"P10"}`}},
			{ID: "policy-b", Type: "function",
				FunctionCall: &gent.FunctionCall{Name: "policy", Arguments: `{"id":"P10"}`}},
		}}
}
