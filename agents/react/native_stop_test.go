package react

import genttest "github.com/rickchristie/gent/internal/tt"

import (
	"context"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestNativeAgent_CompletionStates(t *testing.T) {
	type input struct {
		status gent.ModelStatus
		tools  bool
	}
	type expected struct {
		result *gent.AgentLoopResult
		calls  int
		err    error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	retry := &gent.AgentLoopResult{Action: gent.LAContinue}
	answer := &gent.AgentLoopResult{TerminationReason: gent.TerminationSuccess,
		Action: gent.LATerminate,
		Result: []gent.ContentPart{
			gent.TextContent{Text: "Done."},
		},
		Value: genttest.Value(gent.ContentText([]gent.ContentPart{
			gent.TextContent{Text: "Done."},
		}))}
	toolResult := &gent.AgentLoopResult{Action: gent.LAContinue}
	cases := []testCase{
		{"incomplete tools", input{gent.ModelStatusIncomplete, true}, expected{retry, 0, nil}},
		{"incomplete answer", input{gent.ModelStatusIncomplete, false}, expected{retry, 0, nil}},
		{"completed tools", input{gent.ModelStatusToolCalls, true}, expected{toolResult, 1, nil}},
		{"completed answer", input{gent.ModelStatusAnswer, false}, expected{answer, 0, nil}},
		{"refused with calls", input{gent.ModelStatusRefused, true}, expected{
			&gent.AgentLoopResult{Action: gent.LATerminate, TerminationReason: gent.TerminationRefused,
				Refusal: &gent.ModelRefusal{Code: "blocked"}}, 0, nil}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			choice := &gent.ContentChoice{
				CandidateContent: []gent.ContentPart{gent.TextContent{Text: "Done."}},
				Completion:       gent.ModelCompletion{Status: test.input.status},
			}
			if test.input.status == gent.ModelStatusRefused {
				choice.Completion.Refusal = &gent.ModelRefusal{Code: "blocked"}
			}
			if test.input.tools {
				choice.ToolCalls = []gent.ModelToolCall{nativeTestCall("call-a", "lookup", "{}")}
			}
			model := newMockModel(nativeTestResponse(choice))
			calls := 0
			agent := NewAgent(model).RegisterTool(gent.DefineTool("lookup",
				func(context.Context, map[string]any) (string, error) {
					calls++
					return "record", nil
				}).Description("Read").ParameterSchema(nil).MustBuild())
			ctx := newTestExecCtx(gent.NewBasicLoopData(gent.NewTask(gent.TextContent{Text: "Read."})))
			result, err := agent.Next(ctx)
			want := test.expected
			if want.result.TerminationReason == gent.TerminationSuccess {
				copy := *want.result
				copy.ContentSource = nativeTestSource(ctx, 1)
				want.result = &copy
			}
			assert.Equal(t, want, expected{result, calls, err})
			assert.Equal(t, int64(test.expected.calls), ctx.Stats().GetCounter(gent.SCToolCalls))
		})
	}
}
