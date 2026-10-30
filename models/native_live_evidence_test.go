package models

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents/react"
	"github.com/rickchristie/gent/executor"
	"github.com/stretchr/testify/assert"
)

type liveEvidenceModel struct{ choices []*gent.ContentChoice }

func (m *liveEvidenceModel) GenerateContentStream(context.Context, gent.ModelCallRequest) (
	gent.Stream, error,
) {
	if len(m.choices) == 0 {
		return nil, fmt.Errorf("fixture has no remaining model response")
	}
	choice := m.choices[0]
	m.choices = m.choices[1:]
	response := &gent.ContentResponse{Choices: []*gent.ContentChoice{choice}}
	return gent.NewCompletedStream(response, nil), nil
}

func TestNativeLiveProgramEvidence(t *testing.T) {
	type input struct {
		programs []string
		direct   bool
	}
	type expected struct {
		calls                []string
		programs             [][]liveProgramCall
		output               []gent.ContentPart
		reason               gent.TerminationReason
		codeRuns, codeErrors int64
		accepted             bool
		err                  error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	const program = `(function() {
const first = tool.call({tool:"lookup",args:{id:"start"}});
if (first.error) throw new Error(first.error);
const second = tool.call({tool:"read",args:{id:first.output.next_id}});
if (second.error) throw new Error(second.error);
})();`
	calls := []string{"lookup:start", "read:record-9"}
	output := []gent.ContentPart{gent.TextContent{Text: "record-finished"}}
	lookup := liveProgramCall{"lookup", &responseInput{ID: "start"},
		&responseOutput{NextID: "record-9"}, nil}
	read := liveProgramCall{"read", &responseInput{ID: "record-9"}, "record-finished", nil}
	programs := [][]liveProgramCall{{lookup, read}}
	cases := []testCase{
		{"valid program", input{[]string{program}, false},
			expected{calls, programs, output, gent.TerminationSuccess, 1, 0, true, nil}},
		{"correct undefined function", input{[]string{`lookup({id:"start"});`, program}, false},
			expected{calls, programs, output, gent.TerminationSuccess, 2, 1, true, nil}},
		{"correct top-level return", input{[]string{`return;`, program}, false},
			expected{calls, programs, output, gent.TerminationSuccess, 2, 1, true, nil}},
		{"reject no-op program then direct calls", input{[]string{`console.log("no tools");`}, true},
			expected{calls, nil, output, gent.TerminationSuccess, 1, 0, false, nil}},
		{"reject direct calls only", input{nil, true},
			expected{calls, nil, output, gent.TerminationSuccess, 0, 0, false, nil}},
		{"reject calls in separate programs", input{[]string{
			`tool.call({tool:"lookup",args:{id:"start"}});`,
			`tool.call({tool:"read",args:{id:"record-9"}});`,
		}, false}, expected{calls, [][]liveProgramCall{{lookup}, {read}}, output,
			gent.TerminationSuccess, 2, 0, false, nil}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			model := &liveEvidenceModel{}
			addCall := func(name string, args map[string]any) {
				data, err := json.Marshal(args)
				if !assert.NoError(t, err) {
					t.FailNow()
				}
				model.choices = append(model.choices, &gent.ContentChoice{Completion: gent.ModelCompletion{
					Status: gent.ModelStatusToolCalls,
				},
					ToolCalls: []gent.ModelToolCall{{ID: fmt.Sprintf("call-%d", len(model.choices)),
						Type: "function", FunctionCall: &gent.FunctionCall{Name: name, Arguments: string(data)}}},
				})
			}
			addCall("tool_registry_search", map[string]any{"query": "lookup|read", "query_type": "regex"})
			for _, code := range test.input.programs {
				addCall("run_code", map[string]any{"code": code})
			}
			if test.input.direct {
				addCall("lookup", map[string]any{"id": "start"})
				addCall("read", map[string]any{"id": "record-9"})
			}
			model.choices = append(model.choices, &gent.ContentChoice{
				Completion:       gent.ModelCompletion{Status: gent.ModelStatusAnswer},
				CandidateContent: []gent.ContentPart{gent.TextContent{Text: "record-finished"}},
			})
			actual := expected{}
			chain, err := liveToolChain(&actual.calls, true)
			if !assert.NoError(t, err) {
				return
			}
			loop := react.NewAgent(model).WithToolChain(chain).WithRecoveryVersion("live-evidence/v1")
			ctx := gent.NewExecutionContext(context.Background(), "live evidence",
				gent.NewBasicLoopData(gent.NewTask(gent.TextContent{
					Text: "Search, then use code to read the record.",
				})))
			ctx.SetLimits([]gent.Limit{{Type: gent.LimitExactKey, Key: gent.SCIterations, MaxValue: 6}})
			_, result, err := executor.NewRunner(loop, executor.DefaultConfig()).Execute(ctx)
			if !assert.NoError(t, err) {
				return
			}
			actual.output, actual.reason = result.Output, result.TerminationReason
			actual.programs, actual.err = liveProgramCalls(ctx)
			actual.codeRuns = ctx.Stats().GetCounter(gent.SCCodeExecutions)
			actual.codeErrors = ctx.Stats().GetCounter(gent.SCCodeExecutionsError)
			actual.accepted = reflect.DeepEqual(programs, actual.programs)
			assert.Equal(t, test.expected, actual)
		})
	}
}
