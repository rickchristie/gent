package toolchain

import (
	"context"
	"fmt"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestToolPlanKeepsCapturedSummary(t *testing.T) {
	type input struct {
		chain func() gent.ToolChain
		empty bool
	}
	type expected struct {
		toolCalls    int
		summaryCalls int
		summary      string
		captured     bool
		err          error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	chains := map[string]func() gent.ToolChain{
		"Registry": func() gent.ToolChain { return New() },
		"SearchChain": func() gent.ToolChain {
			return NewSearch(SearchHintSimpleList).RegisterEngine(NewRegexToolSearcher())
		},
	}
	var cases []testCase
	for name, chain := range chains {
		cases = append(cases,
			testCase{name + "/summary", input{chain, false}, expected{1, 1, "summary 1", true, nil}},
			testCase{name + "/empty", input{chain, true}, expected{1, 1, "", true, nil}})
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			toolCalls, summaryCalls := 0, 0
			tool := newIndexableTool("lookup", "Read the answer", "Answer", nil, nil,
				func(context.Context, map[string]any) (string, error) {
					toolCalls++
					return "answer", nil
				})
			tool.WithDeduplicateSummary(func(map[string]any, string) string {
				summaryCalls++
				if test.input.empty {
					return ""
				}
				return fmt.Sprintf("summary %d", summaryCalls)
			})
			chain := test.input.chain()
			chain.RegisterTool(tool)
			if search, ok := chain.(*SearchChain); ok {
				if !assert.NoError(t, search.Initialize()) {
					return
				}
			}
			ctx := newExecCtx()
			result, err := executeTestCalls(chain, ctx, `[{"tool":"lookup","args":{}}]`, searchTestFormat())
			if !assert.NoError(t, err) || !assert.Len(t, result.Results, 1) {
				return
			}
			call := result.Results[0]
			assert.Equal(t, test.expected, expected{toolCalls, summaryCalls,
				call.DeduplicationSummary, call.SummaryCaptured, err})
			bundle, err := ctx.Export()
			if !assert.NoError(t, err) {
				return
			}
			var recorded []*gent.ToolCallResult
			for _, event := range bundle.Activity {
				if event.Type != "tool_call_finished" {
					continue
				}
				decoded, err := ctx.DecodeEvent(event)
				if !assert.NoError(t, err) {
					return
				}
				recorded = append(recorded,
					decoded.(*gent.AfterToolCallEvent).Output.(*gent.ToolCallResult))
			}
			assert.Equal(t, result.Results, recorded)
			// Restore from bytes so the comparison also checks the saved empty-summary marker.
			data, err := ctx.Codecs().Marshal(bundle)
			if !assert.NoError(t, err) {
				return
			}
			var saved gent.RunView
			if !assert.NoError(t, ctx.Codecs().Unmarshal(data, &saved)) {
				return
			}
			fresh, err := gent.RestoreExecutionContext(context.Background(), saved,
				gent.RestoreBindings{Codecs: ctx.Codecs()})
			if !assert.NoError(t, err) {
				return
			}
			plan, ok, err := fresh.SavedToolPlan()
			if !assert.NoError(t, err) || !assert.True(t, ok) {
				return
			}
			assert.Equal(t, result.Results, plan.Results)
			assert.Equal(t, test.expected.toolCalls, toolCalls)
			assert.Equal(t, test.expected.summaryCalls, summaryCalls)
		})
	}
}
