package gent_test

import (
	"context"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents/react"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/toolchain"
	"github.com/stretchr/testify/assert"
)

type nativeRecoveryModel struct {
	requests []gent.ModelCallRequest
	answer   bool
	code     bool
}

func (model *nativeRecoveryModel) GenerateContentStream(_ context.Context,
	request gent.ModelCallRequest) (gent.Stream, error) {
	model.requests = append(model.requests, request)
	choice := &gent.ContentChoice{CandidateContent: "Policy P10 permits it."}
	if !model.answer && len(model.requests) == 1 {
		choice.CandidateContent = "Read the policy."
		for _, id := range []string{"provider-a", "provider-b"} {
			choice.ToolCalls = append(choice.ToolCalls, gent.ModelToolCall{ID: id, Type: "function",
				FunctionCall: &gent.FunctionCall{Name: "policy", Arguments: `{"id":"P10"}`}})
		}
		if model.code {
			choice.ToolCalls = []gent.ModelToolCall{{ID: "provider-code", Type: "function",
				FunctionCall: &gent.FunctionCall{Name: "run_code",
					Arguments: `{"code":"tool.call({tool:'policy',args:{id:'P10'}});"}`}}}
		}
	}
	return gent.NewCompletedStream(&gent.ContentResponse{Choices: []*gent.ContentChoice{choice},
		Info: &gent.GenerationInfo{InputTokens: 12, OutputTokens: 8, TotalTokens: 20}}, nil), nil
}

func newNativeRecoveryAgent(model *nativeRecoveryModel, calls *[]gent.ToolIdentity) *react.Agent {
	chain := toolchain.New().RegisterTool(gent.DefineTool("policy",
		func(ctx context.Context, input recoveryInput) (string, error) {
			identity, _ := gent.ToolIdentityFromContext(ctx)
			*calls = append(*calls, identity)
			return input.Id + ": exact original text", nil
		}).Description("Read the policy").ParameterSchema(nil).MustBuild())
	if model.code {
		chain = toolchain.NewJsToolChainWrapper(chain)
	}
	return react.NewAgent(model).WithToolChain(chain).
		WithRecoveryVersion("native-recovery/v1")
}

func TestNativeHistory_Recovery(t *testing.T) {
	type input struct {
		phase gent.ExecutionPhase
		event string
	}
	type expected struct {
		beforeModels, afterModels, beforeTools, afterTools int
		failure                                            error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"initial", input{phase: gent.PhaseInitial}, expected{0, 2, 0, 2, nil}},
		{"saved response", input{event: "model_call_finished"}, expected{1, 1, 0, 2, nil}},
		{"decision", input{phase: gent.PhaseDecisionReady}, expected{1, 1, 0, 2, nil}},
		{"uncertain call", input{phase: gent.PhaseToolReady},
			expected{1, 1, 0, 0, nil}},
		{"first result", input{phase: gent.PhaseToolCompleted}, expected{1, 1, 1, 1, nil}},
		{"saved iteration", input{phase: gent.PhaseIterationCompleted}, expected{1, 1, 2, 0, nil}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			worker, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := &recoveryStore{codecs: gent.NewCodecRegistry(), cancel: cancel,
				view:  gent.RunView{SchemaVersion: gent.RunSchemaVersion, RunId: "native"},
				phase: test.input.phase, event: test.input.event}
			model := &nativeRecoveryModel{}
			var before []gent.ToolIdentity
			agent := newNativeRecoveryAgent(model, &before)
			ctx := gent.NewExecutionContext(worker, "root", gent.NewBasicLoopData(
				&gent.Task{Text: "Read the policy twice."}), gent.WithRunId("native"),
				gent.WithRecorder(store), gent.WithCodecs(store.codecs))
			_, err := executor.New[gent.LoopData](agent, executor.DefaultConfig()).Execute(ctx)
			assert.ErrorIs(t, err, context.Canceled)
			bundle, err := ctx.Export()
			assert.NoError(t, err)
			assert.NoError(t, gent.ValidateBundle(bundle, ctx.Codecs()))
			nextModel := &nativeRecoveryModel{answer: len(model.requests) > 0}
			var after []gent.ToolIdentity
			nextAgent := newNativeRecoveryAgent(nextModel, &after)
			bindings := recoveryBindings(t, nextAgent)
			data, err := bindings.Codecs.Marshal(bundle)
			assert.NoError(t, err)
			var saved gent.RunView
			assert.NoError(t, bindings.Codecs.Unmarshal(data, &saved))
			restored, result, err := resumeRecovery(nextAgent, saved, bindings)
			if test.expected.failure != nil {
				assert.ErrorIs(t, err, test.expected.failure)
			} else if assert.NoError(t, err) {
				assert.Equal(t, []gent.ContentPart{gent.TextContent{Text: "Policy P10 permits it."}},
					result.Output)
				assert.Equal(t, int64(40), restored.Stats().GetCounter(gent.SCTotalTokens))
				expectedCalls := int64(2)
				if test.name == "uncertain call" {
					expectedCalls = 1
				}
				assert.Equal(t, expectedCalls, restored.Stats().GetCounter(gent.SCToolCalls))
				last := nextModel.requests[len(nextModel.requests)-1]
				assert.Equal(t, gent.ModelMessage{Role: gent.ChatMessageTypeAI,
					Parts: []gent.ContentPart{gent.TextContent{Text: "Read the policy."},
						gent.ModelToolCall{ID: "provider-a", Type: "function", FunctionCall: &gent.FunctionCall{
							Name: "policy", Arguments: `{"id":"P10"}`,
						}},
						gent.ModelToolCall{ID: "provider-b", Type: "function", FunctionCall: &gent.FunctionCall{
							Name: "policy", Arguments: `{"id":"P10"}`,
						}}}},
					last.Messages[2])
			}
			assert.Equal(t, test.expected, expected{len(model.requests), len(nextModel.requests),
				len(before), len(after), test.expected.failure})
		})
	}
}

func TestNativeHistory_JavaScriptRecoveryBoundary(t *testing.T) {
	type input struct{ completedCheckpoints int }
	type expected struct {
		failure           error
		resumedModelCalls int
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"inside program", input{1}, expected{nil, 1}},
		{"complete program", input{2}, expected{nil, 1}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			worker, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := &recoveryStore{codecs: gent.NewCodecRegistry(), cancel: cancel,
				view:  gent.RunView{SchemaVersion: gent.RunSchemaVersion, RunId: "native-code"},
				phase: gent.PhaseToolCompleted, after: test.input.completedCheckpoints}
			model := &nativeRecoveryModel{code: true}
			var effects []gent.ToolIdentity
			agent := newNativeRecoveryAgent(model, &effects)
			ctx := gent.NewExecutionContext(worker, "root", gent.NewBasicLoopData(
				&gent.Task{Text: "Read the policy."}), gent.WithRunId("native-code"),
				gent.WithRecorder(store), gent.WithCodecs(store.codecs))
			_, err := executor.New[gent.LoopData](agent, executor.DefaultConfig()).Execute(ctx)
			assert.ErrorIs(t, err, context.Canceled)
			bundle, err := ctx.Export()
			assert.NoError(t, err)
			nextModel := &nativeRecoveryModel{code: true, answer: true}
			var repeated []gent.ToolIdentity
			nextAgent := newNativeRecoveryAgent(nextModel, &repeated)
			_, result, err := resumeRecovery(nextAgent, bundle, recoveryBindings(t, nextAgent))
			if test.expected.failure != nil {
				assert.ErrorIs(t, err, test.expected.failure)
			} else if assert.NoError(t, err) {
				assert.Equal(t, []gent.ContentPart{gent.TextContent{Text: "Policy P10 permits it."}},
					result.Output)
			}
			assert.Len(t, effects, 1)
			assert.Equal(t, []gent.ToolIdentity(nil), repeated)
			assert.Equal(t, test.expected.resumedModelCalls, len(nextModel.requests))
		})
	}
}
