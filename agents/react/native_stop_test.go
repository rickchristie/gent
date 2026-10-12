package react

import (
	"context"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
	"github.com/tmc/langchaingo/llms"
)

func TestNativeAgent_ProviderStopReasons(t *testing.T) {
	type input struct {
		reason string
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
	retry := &gent.AgentLoopResult{Action: gent.LAContinue,
		NextPrompt: "The response was incomplete. Use a shorter answer or fewer tool calls."}
	answer := &gent.AgentLoopResult{Action: gent.LATerminate,
		Result: []gent.ContentPart{llms.TextContent{Text: "Done."}}}
	toolResult := &gent.AgentLoopResult{Action: gent.LAContinue, NextPrompt: `<lookup>
record
</lookup>`}
	cases := []testCase{
		{"OpenAI tool limit", input{"length", true}, expected{retry, 0, nil}},
		{"OpenAI answer limit", input{"length", false}, expected{retry, 0, nil}},
		{"Anthropic tool limit", input{"max_tokens", true}, expected{retry, 0, nil}},
		{"Anthropic answer limit", input{"max_tokens", false}, expected{retry, 0, nil}},
		{"Anthropic tool context limit", input{"model_context_window_exceeded", true},
			expected{retry, 0, nil}},
		{"Anthropic answer context limit", input{"model_context_window_exceeded", false},
			expected{retry, 0, nil}},
		{"Google API tool limit", input{"MAX_TOKENS", true}, expected{retry, 0, nil}},
		{"Google API answer limit", input{"MAX_TOKENS", false}, expected{retry, 0, nil}},
		{"Google SDK tool limit", input{"FinishReasonMaxTokens", true}, expected{retry, 0, nil}},
		{"Google SDK answer limit", input{"FinishReasonMaxTokens", false}, expected{retry, 0, nil}},
		{"OpenAI complete answer", input{"stop", false}, expected{answer, 0, nil}},
		{"Anthropic complete answer", input{"end_turn", false}, expected{answer, 0, nil}},
		{"Anthropic complete tools", input{"tool_use", true}, expected{toolResult, 1, nil}},
		{"Google SDK complete tools", input{"FinishReasonStop", true},
			expected{toolResult, 1, nil}},
		{"Google SDK complete answer", input{"FinishReasonStop", false}, expected{answer, 0, nil}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			choice := &gent.ContentChoice{Content: "Done.", StopReason: test.input.reason}
			if test.input.tools {
				choice.ToolCalls = []llms.ToolCall{nativeTestCall("call-a", "lookup", "{}")}
			}
			model := newMockModel(nativeTestResponse(choice))
			calls := 0
			agent := NewNativeAgent(model).RegisterTool(gent.NewToolFunc("lookup", "Read", nil,
				func(context.Context, map[string]any) (string, error) {
					calls++
					return "record", nil
				}))
			ctx := newTestExecCtx(gent.NewBasicLoopData(&gent.Task{Text: "Read."}))
			result, err := agent.Next(ctx)
			assert.Equal(t, test.expected, expected{result, calls, err})
			assert.Equal(t, int64(test.expected.calls), ctx.Stats().GetCounter(gent.SCToolCalls))
		})
	}
}
