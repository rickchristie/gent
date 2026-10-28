package react

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/termination"
	"github.com/rickchristie/gent/toolchain"
	"github.com/stretchr/testify/assert"
)

type limitModel struct {
	kind  string
	calls int
}

func (*limitModel) ModelName() string { return "limit-model" }
func (m *limitModel) GenerateContentStream(
	_ context.Context, _ gent.ModelCallRequest,
) (gent.Stream, error) {
	m.calls++
	name, args := "work", "{}"
	if m.kind == "parse" {
		args = "{"
	}
	if m.kind == "code" {
		name, args = "run_code", `{"code":"throw new Error('failed');"}`
	}
	choice := &gent.ContentChoice{ToolCalls: []gent.ModelToolCall{{ID: fmt.Sprint(m.calls),
		Type: "function", FunctionCall: &gent.FunctionCall{Name: name, Arguments: args}}}}
	if m.kind == "answer" {
		choice = &gent.ContentChoice{CandidateContent: "invalid JSON"}
	}
	return gent.NewCompletedStream(&gent.ContentResponse{Choices: []*gent.ContentChoice{choice},
		Info: &gent.GenerationInfo{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}}, nil), nil
}

// Exercise limits through real native plans and the JavaScript bridge. Format and section
// parsers have independent tests because native agents do not parse a text action envelope.
func TestAgent_NativeLimits(t *testing.T) {
	type input struct {
		key       gent.StatKey
		kind      string
		increment float64
		gauge     bool
	}
	type expected struct {
		reason    gent.TerminationReason
		iteration int
		value     float64
		err       string
	}
	type testCase struct {
		name  string
		input input
	}
	cases := []testCase{
		{"iterations", input{gent.SCIterations.Self(), "tool", 1, false}},
		{"input", input{gent.SCInputTokens, "tool", 10, false}},
		{"output", input{gent.SCOutputTokens, "tool", 5, false}},
		{"total", input{gent.SCTotalTokens, "tool", 15, false}},
		{"model input", input{gent.SCInputTokensFor + "limit-model", "tool", 10, false}},
		{"model output", input{gent.SCOutputTokensFor + "limit-model", "tool", 5, false}},
		{"model total", input{gent.SCTotalTokensFor + "limit-model", "tool", 15, false}},
		{"tools", input{gent.SCToolCalls, "tool", 1, false}},
		{"named tools", input{gent.SCToolCallsFor + "work", "tool", 1, false}},
		{"tool errors", input{gent.SCToolCallsErrorTotal, "error", 1, false}},
		{"named tool errors", input{gent.SCToolCallsErrorFor + "work", "error", 1, false}},
		{"consecutive tool errors", input{gent.SGToolCallsErrorConsecutive, "error", 1, true}},
		{"consecutive named errors",
			input{gent.SGToolCallsErrorConsecutiveFor + "work", "error", 1, true}},
		{"argument parse errors", input{gent.SCToolchainParseErrorTotal, "parse", 1, false}},
		{"consecutive argument errors", input{gent.SGToolchainParseErrorConsecutive, "parse", 1, true}},
		{"answer parse errors", input{gent.SCTerminationParseErrorTotal, "answer", 1, false}},
		{"consecutive answer errors", input{gent.SGTerminationParseErrorConsecutive, "answer", 1, true}},
		{"code executions", input{gent.SCCodeExecutions, "code", 1, false}},
		{"code errors", input{gent.SCCodeExecutionsError, "code", 1, false}},
		{"consecutive code errors", input{gent.SGCodeExecutionsErrorConsecutive, "code", 1, true}},
	}
	for _, test := range cases {
		for _, stop := range []int{1, 3} {
			t.Run(fmt.Sprintf("%s/iteration %d", test.name, stop), func(t *testing.T) {
				model := &limitModel{kind: test.input.kind}
				chain := toolchain.New().RegisterTool(gent.DefineTool("work",
					func(context.Context, map[string]any) (string, error) {
						if test.input.kind == "error" {
							return "", errors.New("failed")
						}
						return "done", nil
					}).Description("Do work").ParameterSchema(nil).MustBuild())
				if test.input.kind == "code" {
					chain = toolchain.NewJsToolChainWrapper(chain)
				}
				agent := NewAgent(model).WithToolChain(chain)
				if test.input.kind == "answer" {
					agent.WithTermination(termination.NewJSON[map[string]string]("answer"))
				}
				threshold := float64(stop-1) * test.input.increment
				if test.input.key == gent.SCIterations.Self() {
					threshold = float64(stop)
				}
				limit := gent.Limit{Type: gent.LimitExactKey, Key: test.input.key, MaxValue: threshold}
				ctx := gent.NewExecutionContext(context.Background(), "limits",
					gent.NewBasicLoopData(&gent.Task{Text: "Continue to use the tool."}))
				ctx.SetLimits([]gent.Limit{limit})
				result, err := executor.New[gent.LoopData](agent, executor.DefaultConfig()).Execute(ctx)
				if !assert.Error(t, err) {
					return
				}
				value := float64(ctx.Stats().GetCounter(test.input.key))
				if test.input.gauge {
					value = ctx.Stats().GetGauge(test.input.key)
				}
				operator := ">"
				if test.input.key == gent.SCIterations.Self() {
					operator = ">="
				}
				want := expected{gent.TerminationLimitExceeded, stop, float64(stop) * test.input.increment,
					fmt.Sprintf("limit exceeded: %s %s %v", test.input.key, operator, threshold)}
				assert.Equal(t, want, expected{result.TerminationReason, ctx.Iteration(), value, err.Error()})
				assert.Equal(t, &limit, result.ExceededLimit)
			})
		}
	}
}

func TestAgent_NativePrefixLimit(t *testing.T) {
	type input struct {
		failures bool
		key      gent.StatKey
	}
	type expected struct {
		alpha, beta int64
		iterations  int
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"calls", input{false, gent.SCToolCallsFor}, expected{1, 3, 4}},
		{"errors", input{true, gent.SCToolCallsErrorFor}, expected{1, 3, 4}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var responses []*gent.ContentResponse
			for i, name := range []string{"alpha", "beta", "beta", "beta"} {
				choice := &gent.ContentChoice{ToolCalls: []gent.ModelToolCall{
					nativeTestCall(fmt.Sprint(i), name, "{}"),
				}}
				responses = append(responses, nativeTestResponse(choice))
			}
			model := newMockModel(responses...)
			agent := NewAgent(model)
			for _, name := range []string{"alpha", "beta"} {
				agent.RegisterTool(gent.DefineTool(name, func(context.Context, map[string]any) (string, error) {
					if test.input.failures {
						return "", errors.New("failed")
					}
					return "done", nil
				}).Description("Do work").ParameterSchema(nil).MustBuild())
			}
			limit := gent.Limit{Type: gent.LimitKeyPrefix, Key: test.input.key, MaxValue: 2}
			ctx := newTestExecCtx(gent.NewBasicLoopData(&gent.Task{Text: "Use both tools."}))
			ctx.SetLimits([]gent.Limit{limit})
			result, err := executor.New[gent.LoopData](agent, executor.DefaultConfig()).Execute(ctx)
			assert.EqualError(t, err, fmt.Sprintf("limit exceeded: %s > 2", test.input.key))
			assert.Equal(t, gent.TerminationLimitExceeded, result.TerminationReason)
			assert.Equal(t, &limit, result.ExceededLimit)
			assert.Equal(t, test.expected, expected{ctx.Stats().GetCounter(test.input.key + "alpha"),
				ctx.Stats().GetCounter(test.input.key + "beta"), ctx.Iteration()})
			var keys []gent.StatKey
			for _, event := range ctx.Events() {
				if exceeded, ok := event.(*gent.LimitExceededEvent); ok {
					keys = append(keys, exceeded.MatchedKey)
				}
			}
			assert.Equal(t, []gent.StatKey{test.input.key + "beta"}, keys)
		})
	}
}
