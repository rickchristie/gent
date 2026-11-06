package react

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/toolchain"
	"github.com/stretchr/testify/assert"
)

func TestRecoveryErrorLimitsAfterWorkerReplacement(t *testing.T) {
	type input struct {
		key       gent.StatKey
		limitType gent.LimitType
		max       float64
	}
	type expected struct {
		checks, alpha, beta int64
		matched             gent.StatKey
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"first recovery error", input{gent.SCToolRecoveryErrorTotal, gent.LimitExactKey, 0},
			expected{1, 1, 0, gent.SCToolRecoveryErrorTotal}},
		{"third recovery error", input{gent.SCToolRecoveryErrorTotal, gent.LimitExactKey, 2},
			expected{3, 1, 2, gent.SCToolRecoveryErrorTotal}},
		{"first named error", input{gent.SCToolRecoveryErrorFor, gent.LimitKeyPrefix, 0},
			expected{1, 1, 0, gent.SCToolRecoveryErrorFor + "alpha"}},
		{"third named error", input{gent.SCToolRecoveryErrorFor, gent.LimitKeyPrefix, 2},
			expected{4, 1, 3, gent.SCToolRecoveryErrorFor + "beta"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			worker, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var starts, checks atomic.Int64
			build := func(model gent.Model) *Agent[string] {
				chain := toolchain.New()
				for _, name := range []string{"alpha", "beta"} {
					chain.RegisterTool(gent.DefineTool(name, func(ctx context.Context, _ struct{}) (
						*gent.ToolExecutionResponse, error,
					) {
						if starts.Add(1) == 4 {
							cancel()
						}
						<-ctx.Done()
						return nil, ctx.Err()
					}).Recovery(func(context.Context, *gent.ToolRecoveryRequest[struct{}]) (
						*gent.ToolRecoveryResponse, error,
					) {
						checks.Add(1)
						return nil, errors.New("private recovery backend failure")
					}).MustBuild())
				}
				return NewAgent(model).WithToolChain(toolchain.NewJsToolChainWrapper(chain)).
					WithRecoveryVersion("recovery-limits/v1")
			}
			first := newMockModel(nativeTestResponse(&gent.ContentChoice{
				Completion: gent.ModelCompletion{Status: gent.ModelStatusToolCalls},
				ToolCalls: []*gent.ModelToolCallRequest{nativeTestCall("batch", "run_code",
					`{"code": "tool.parallelCall([1,2,3,4].map(n => ({tool:n===1?'alpha':'beta',args:{}})));"}`)},
			}))
			ctx := gent.NewExecutionContext(worker, "limits",
				gent.NewBasicLoopData(gent.NewTask(gent.TextPart("Run the independent checks."))))
			defer ctx.Release()
			limit := gent.Limit{Type: test.input.limitType, Key: test.input.key, MaxValue: test.input.max}
			ctx.SetLimits([]gent.Limit{limit})
			_, _, err := executor.NewRunner(build(first), executor.DefaultConfig()).Execute(ctx)
			if !assert.ErrorIs(t, err, context.Canceled) {
				return
			}
			assert.Equal(t, int64(4), starts.Load())
			saved, err := ctx.Export()
			assert.NoError(t, err)
			var restored *gent.ExecutionContext
			_, result, err := executor.NewRunner(build(newMockModel()), executor.DefaultConfig()).Resume(
				context.Background(), saved, gent.RestoreBindings{
					Configure: func(ctx *gent.ExecutionContext) error { restored = ctx; return nil },
				})
			assert.EqualError(t, err,
				fmt.Sprintf("limit exceeded: %s > %v", test.input.key, test.input.max))
			if !assert.NotNil(t, result) || !assert.NotNil(t, restored) {
				return
			}
			assert.Equal(t, gent.TerminationLimitExceeded, result.TerminationReason)
			assert.Equal(t, &limit, result.ExceededLimit)
			var matched []gent.StatKey
			for _, event := range restored.Events() {
				if exceeded, ok := event.(*gent.LimitExceededEvent); ok {
					matched = append(matched, exceeded.MatchedKey)
				}
			}
			assert.Equal(t, []gent.StatKey{test.expected.matched}, matched)
			assert.Equal(t, test.expected.checks, checks.Load())
			assert.Equal(t, test.expected.alpha,
				restored.Stats().GetCounter(gent.SCToolRecoveryErrorFor+"alpha"))
			assert.Equal(t, test.expected.beta,
				restored.Stats().GetCounter(gent.SCToolRecoveryErrorFor+"beta"))
			assert.Equal(t, int64(0), restored.Stats().GetCounter(gent.SCToolCallsErrorTotal))
		})
	}
}
