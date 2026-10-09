package executor_test

import (
	"context"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/executor"
	"github.com/stretchr/testify/assert"
	"github.com/tmc/langchaingo/llms"
)

func TestRunnerContinuesAcrossConstructorChange(t *testing.T) {
	type constructor func(*resumeLoop, executor.Config) *executor.Runner
	type input struct {
		original constructor
		fresh    constructor
	}
	type expected struct {
		result     *gent.ExecutionResult
		configured int
		calls      int
		step       int
		err        error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	old := func(loop *resumeLoop, cfg executor.Config) *executor.Runner {
		return executor.New[*gent.BasicLoopData](loop, cfg)
	}
	current := func(loop *resumeLoop, cfg executor.Config) *executor.Runner {
		return executor.NewRunner(loop, cfg)
	}
	want := expected{result: &gent.ExecutionResult{TerminationReason: gent.TerminationSuccess,
		Output: []gent.ContentPart{llms.TextContent{Text: "saved task:2"}}},
		configured: 1, calls: 1, step: 2}
	cases := []testCase{
		{"generic executor to runner", input{old, current}, want},
		{"runner to generic executor", input{current, old}, want},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			worker, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := &resumeRecorder{view: gent.RunView{SchemaVersion: gent.RunSchemaVersion,
				RunId: "runner"}, stop: gent.PhaseIterationCompleted, cancel: cancel}
			ctx := gent.NewExecutionContext(worker, "root",
				gent.NewBasicLoopData(&gent.Task{Text: "saved task"}),
				gent.WithRunId("runner"), gent.WithRecorder(store))
			cfg := executor.Config{RecoveryVersion: "runner/v1"}
			_, err := test.input.original(&resumeLoop{}, cfg).Execute(ctx)
			if !assert.ErrorIs(t, err, context.Canceled) {
				return
			}
			fresh := &resumeLoop{}
			result, err := test.input.fresh(fresh, cfg).Resume(
				context.Background(), store.view, gent.RestoreBindings{})
			assert.Equal(t, test.expected,
				expected{result, fresh.configured, fresh.calls, fresh.step, err})
		})
	}
}
