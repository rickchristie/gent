package toolchain

import (
	"context"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestSearchChainPlanContinuesWithSavedResultsAndSeenTools(t *testing.T) {
	type input struct {
		mode    SearchType
		content string
	}
	type expected struct {
		remainingSearches int
		seen              map[string]bool
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"paging and repeated full definitions", input{Search, `[
  {"tool":"tool_registry_search", "args":{"query":"policy", "query_type":"mock", "page":1}},
  {"tool":"tool_registry_search", "args":{"query":"policy", "query_type":"mock", "page":2}},
  {"tool":"tool_registry_search", "args":{"query":"policy", "query_type":"mock", "page":1}}
]`}, expected{2, map[string]bool{"policy_a": true, "policy_b": true}}},
		{"brief discovery and schema lookup", input{SearchGet, `[
  {"tool":"tool_registry_search", "args":{"query":"policy", "query_type":"mock"}},
  {"tool":"get_tool_schema", "args":{"tool_name":"policy_a"}},
  {"tool":"tool_registry_search", "args":{"query":"policy", "query_type":"mock"}}
]`}, expected{1, map[string]bool{"policy_a": true}}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			newChain := func(searches *int) *SearchChain {
				chain := NewSearch(SearchHintSimpleList).WithSearchType(tt.input.mode)
				chain.WithPageSize(1).RegisterEngine(&mockToolSearcher{
					id: "mock", guidance: "Find policies",
					searchFn: func(context.Context, string) ([]string, error) {
						*searches++
						return []string{"policy_a", "policy_b"}, nil
					},
				})
				for _, name := range []string{"policy_a", "policy_b"} {
					chain.RegisterTool(newIndexableTool(name, "Return the saved policy", "Policy",
						nil, nil, func(context.Context, map[string]any) (string, error) {
							return "P10 permits the refund.", nil
						}))
				}
				if !assert.NoError(t, chain.Initialize()) {
					t.FailNow()
				}
				return chain
			}
			searches := 0
			chain := newChain(&searches)
			ctx := newExecCtx()
			plan, err := chain.PrepareCalls(ctx, testCalls(tt.input.content))
			if !assert.NoError(t, err) {
				return
			}
			first, err := chain.ExecuteNext(ctx, plan, searchTestFormat())
			assert.NoError(t, err)
			assert.Equal(t, 1, searches)
			bundle, err := ctx.Export()
			assert.NoError(t, err)
			bytes, err := ctx.Codecs().Marshal(bundle)
			assert.NoError(t, err)
			var saved gent.RunView
			assert.NoError(t, ctx.Codecs().Unmarshal(bytes, &saved))
			for plan.NextCall < len(plan.Calls) {
				_, err := chain.ExecuteNext(ctx, plan, searchTestFormat())
				if !assert.NoError(t, err) {
					return
				}
			}
			freshSearches := 0
			freshChain := newChain(&freshSearches)
			codecs := gent.NewCodecRegistry()
			assert.NoError(t, freshChain.RegisterCodecs(codecs))
			fresh, err := gent.RestoreExecutionContext(context.Background(), saved,
				gent.RestoreBindings{Codecs: codecs})
			if !assert.NoError(t, err) {
				return
			}
			resumed, ok, err := fresh.SavedToolPlan()
			assert.NoError(t, err)
			if !assert.True(t, ok) {
				return
			}
			assert.Equal(t, []*gent.ToolCallResult{first}, resumed.Results)
			for resumed.NextCall < len(resumed.Calls) {
				_, err := freshChain.ExecuteNext(fresh, resumed, searchTestFormat())
				if !assert.NoError(t, err) {
					return
				}
			}
			assert.Equal(t, tt.expected.remainingSearches, freshSearches)
			for i, result := range plan.Results {
				want := *result
				if i > 0 {
					want.AttemptId = fresh.AttemptId()
				}
				assert.Equal(t, &want, resumed.Results[i])
			}
			seen, ok, err := fresh.CheckpointState("gent:search_seen")
			assert.NoError(t, err)
			assert.True(t, ok)
			assert.Equal(t, tt.expected.seen, seen)
			assert.Equal(t, ctx.Stats().Counters(), fresh.Stats().Counters())
		})
	}
}
