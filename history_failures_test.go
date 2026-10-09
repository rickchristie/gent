package gent_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/events"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/format"
	"github.com/rickchristie/gent/toolchain"
	"github.com/stretchr/testify/assert"
)

func TestHistoryRecovery_UncertainTool(t *testing.T) {
	type input struct{ decision gent.ToolRecoveryStatus }
	type expected struct {
		err        error
		toolCalls  int
		modelCalls int
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"no recovery adapter", input{}, expected{gent.ErrUncertainTool, 0, 0}},
		{"unresolved", input{gent.ToolRecoveryUnresolved}, expected{gent.ErrUncertainTool, 0, 0}},
		{"retry authorized", input{gent.ToolRecoveryRetryAllowed}, expected{nil, 2, 1}},
		{"completed externally", input{gent.ToolRecoveryCompleted}, expected{nil, 1, 1}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			bundle, _, priorCalls := saveInterruptedRun(t, gent.PhaseToolReady, "")
			assert.Empty(t, priorCalls)
			model := &recoveryModel{answer: true}
			var calls []gent.ToolIdentity
			agent := newRecoveryAgent(model, &calls)
			bindings := recoveryBindings(t, agent)
			var recovered gent.ToolRecoveryRequest
			if test.input.decision != "" {
				bindings.ToolRecovery = gent.ToolRecoveryFunc(func(_ context.Context,
					request gent.ToolRecoveryRequest,
				) (gent.ToolRecoveryResult, error) {
					recovered = request
					input, err := bindings.Codecs.Decode(request.Input)
					assert.NoError(t, err)
					assert.Equal(t, recoveryInput{Id: "P10"}, input)
					return gent.ToolRecoveryResult{Status: test.input.decision,
						Result: &gent.ToolCallResult{Name: "policy", Args: request.Args,
							Input: input, Output: "P10: exact original text", SummaryCaptured: true,
							Text: "<policy>P10: exact original text</policy>"}}, nil
				})
			}
			ctx, _, err := resumeRecovery(agent, bundle, bindings)
			assert.ErrorIs(t, err, test.expected.err)
			assert.Len(t, calls, test.expected.toolCalls)
			assert.Len(t, model.requests, test.expected.modelCalls)
			view, exportErr := ctx.Export()
			assert.NoError(t, exportErr)
			assert.NoError(t, gent.ValidateBundle(view, ctx.Codecs()))
			if test.expected.err != nil {
				assert.Equal(t, gent.PhaseToolReady, view.Checkpoint.Phase)
				assert.Nil(t, view.Checkpoint.Outcome)
			} else {
				assert.Equal(t, gent.PhaseTerminal, view.Checkpoint.Phase)
			}
			if test.input.decision == gent.ToolRecoveryRetryAllowed {
				assert.Equal(t, recovered.ToolCallId, calls[0].ToolCallId)
				assert.NotEqual(t, recovered.AttemptId, calls[0].AttemptId)
			}
		})
	}
}

type rejectRecorder struct {
	event string
	phase gent.ExecutionPhase
	err   error
}

func (r rejectRecorder) Save(_ context.Context, batch *gent.SaveBatch) error {
	if batch.Checkpoint != nil && batch.Checkpoint.Phase == r.phase {
		return r.err
	}
	for _, event := range batch.Events {
		if event.Type == r.event {
			return r.err
		}
	}
	return nil
}

func TestHistoryRecorderFailure_PreventsSideEffects(t *testing.T) {
	type input struct {
		event string
		phase gent.ExecutionPhase
	}
	type expected struct{ models, tools int }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"initial checkpoint", input{phase: gent.PhaseInitial}, expected{0, 0}},
		{"model request", input{event: "model_call_started"}, expected{0, 0}},
		{"decision checkpoint", input{phase: gent.PhaseDecisionReady}, expected{1, 0}},
		{"tool readiness", input{phase: gent.PhaseToolReady}, expected{1, 0}},
		{"first tool result", input{phase: gent.PhaseToolCompleted}, expected{1, 1}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			saveErr := errors.New("writer lease expired")
			recorder := rejectRecorder{test.input.event, test.input.phase, saveErr}
			model := &recoveryModel{}
			var calls []gent.ToolIdentity
			agent := newRecoveryAgent(model, &calls)
			ctx := gent.NewExecutionContext(context.Background(), "root",
				gent.NewBasicLoopData(
					&gent.Task{Text: "policy question"},
				), gent.WithRunId("fenced"), gent.WithRecorder(recorder))
			_, err := executor.New[gent.LoopData](agent, executor.DefaultConfig()).Execute(ctx)
			assert.ErrorIs(t, err, saveErr)
			var failure *gent.PersistenceError
			if assert.ErrorAs(t, err, &failure) {
				view, err := ctx.Export()
				assert.NoError(t, err)
				assert.Equal(t, failure.Batch.PreviousEventNumber, view.LastAppliedEventNumber)
				assert.NoError(t, gent.ValidateBundle(view, ctx.Codecs()))
			}
			assert.Equal(t, test.expected, expected{len(model.requests), len(calls)})
		})
	}
}

type toolInputHook struct {
	value string
	calls int
}

func (h *toolInputHook) OnBeforeToolCall(
	_ *gent.ExecutionContext,
	event *gent.BeforeToolCallEvent,
) {
	h.calls++
	event.Args = recoveryInput{Id: h.value}
}

func TestToolRecovery_ReusesEffectiveInput(t *testing.T) {
	worker, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &recoveryStore{
		codecs: gent.NewCodecRegistry(),
		view:   gent.RunView{SchemaVersion: gent.RunSchemaVersion, RunId: "input"},
		cancel: cancel,
		phase:  gent.PhaseToolReady,
	}
	var got []recoveryInput
	newChain := func() *toolchain.YAML {
		chain := toolchain.NewYAML()
		chain.RegisterTool(gent.NewToolFunc("policy", "lookup", nil,
			func(_ context.Context, input recoveryInput) (string, error) {
				got = append(got, input)
				return fmt.Sprintf("policy %s", input.Id), nil
			}))
		return chain
	}
	ctx := gent.NewExecutionContext(worker, "root", nil, gent.WithRunId("input"),
		gent.WithRecorder(store), gent.WithCodecs(store.codecs))
	hook := &toolInputHook{value: "effective"}
	ctx.SetEventPublisher(events.NewRegistry().Subscribe(hook))
	chain := newChain()
	plan, err := chain.Prepare(ctx, `- tool: policy
  args:
    id: original`)
	assert.NoError(t, err)
	_, err = chain.ExecuteNext(ctx, plan, format.NewXML())
	assert.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, got)
	bundle, err := ctx.Export()
	assert.NoError(t, err)
	freshChain := newChain()
	codecs := gent.NewCodecRegistry()
	assert.NoError(t, freshChain.RegisterCodecs(codecs))
	fresh, err := gent.RestoreExecutionContext(context.Background(), bundle, gent.RestoreBindings{
		Codecs: codecs, ToolRecovery: gent.ToolRecoveryFunc(
			func(context.Context, gent.ToolRecoveryRequest) (
				gent.ToolRecoveryResult, error,
			) {
				return gent.ToolRecoveryResult{Status: gent.ToolRecoveryRetryAllowed}, nil
			},
		),
	})
	if !assert.NoError(t, err) {
		return
	}
	freshHook := &toolInputHook{value: "would change the saved input"}
	fresh.SetEventPublisher(events.NewRegistry().Subscribe(freshHook))
	savedPlan, ok, err := fresh.SavedToolPlan()
	assert.NoError(t, err)
	assert.True(t, ok)
	_, err = freshChain.ExecuteNext(fresh, savedPlan, format.NewXML())
	assert.NoError(t, err)
	assert.Equal(t, []recoveryInput{{Id: "effective"}}, got)
	assert.Equal(t, 1, hook.calls)
	assert.Equal(t, 0, freshHook.calls)
}
