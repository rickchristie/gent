package toolchain

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestNativeNestedTemporalArguments(t *testing.T) {
	type schedule struct {
		Delays map[string]*time.Duration `json:"delays"`
		At     [1]time.Time              `json:"at"`
	}
	type scheduleInput struct {
		Groups []schedule `json:"groups"`
	}
	type input struct{ shared bool }
	type expected struct {
		status gent.ToolStatus
		value  scheduleInput
		calls  int
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	delay := 90 * time.Minute
	value := scheduleInput{Groups: []schedule{{
		Delays: map[string]*time.Duration{"dispatch": &delay, "optional": nil},
		At:     [1]time.Time{time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)},
	}}}
	cases := []testCase{
		{"direct", input{}, expected{gent.ToolExecuted, value, 1}},
		{"shared", input{shared: true}, expected{gent.ToolExecuted, value, 1}},
	}
	const arguments = `{"groups":[{
"delays":{"dispatch":"1h30m","optional":null},
"at":["2026-01-02T03:04:05Z"]
}]}`
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var received scheduleInput
			calls := 0
			tool := gent.DefineTool("schedule", func(_ context.Context, request scheduleInput) (
				*gent.ToolExecutionResponse, error,
			) {
				received = request
				calls++
				return &gent.ToolExecutionResponse{Status: gent.ToolExecuted}, nil
			}).MustBuild()
			ctx := newExecCtx()
			defer ctx.Release()
			var chain gent.ToolChain = New().RegisterTool(tool)
			call := nativeCall("schedule", arguments)
			if test.input.shared {
				shared := NewSearch(SearchHintSimpleList).RegisterEngine(NewRegexToolSearcher())
				shared.RegisterTool(tool)
				assert.NoError(t, shared.Initialize())
				chain = shared
				wrapped, err := json.Marshal(map[string]any{
					"tool": "schedule", "args": json.RawMessage(arguments),
				})
				assert.NoError(t, err)
				call = nativeCall("execute_tool", string(wrapped))
			}
			record := executeNativeTestCall(t, chain, ctx, call)
			assert.Equal(t, test.expected, expected{record.Execution().Status, received, calls})
			if record.Input() != nil && calls == 1 {
				restored, err := ctx.Codecs().Decode(*record.Input())
				assert.NoError(t, err)
				assert.Equal(t, test.expected.value, restored)
			}
		})
	}
}
