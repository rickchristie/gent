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

func TestRecoveryRejectsChangedHandlerVersion(t *testing.T) {
	type input struct{ search, program bool }
	type expected struct {
		result        *gent.ExecutionResult
		err           error
		toolCalls     int
		modelRequests int
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"registry", input{}, expected{err: gent.ErrIncompatibleCheckpoint}},
		{"search", input{search: true}, expected{err: gent.ErrIncompatibleCheckpoint}},
		{"program", input{program: true}, expected{err: gent.ErrIncompatibleCheckpoint}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			toolCalls := 0
			build := func(model *guidanceModel, version string) *react.Agent[string] {
				tool := &recoverySearchTool{gent.DefineTool("book",
					func(context.Context, *bookingInput) (string, error) {
						toolCalls++
						return "booked", nil
					}).Description("Book a flight").ParameterSchema(nil).
					Recovery(func(context.Context, *gent.ToolRecoveryRequest) (*gent.ToolRecoveryResponse, error) {
						return &gent.ToolRecoveryResponse{Result: gent.ToolRecoveryUnresolved,
							SteeringMessage: "Check the booking service."}, nil
					}).MustBuild()}
				var chain gent.ToolChain = toolchain.New().RegisterTool(tool)
				if test.input.search {
					search := toolchain.NewSearch(toolchain.SearchHintSimpleList).
						RegisterEngine(toolchain.NewRegexToolSearcher()).Pin("book")
					search.RegisterTool(tool)
					if !assert.NoError(t, search.Initialize()) {
						t.FailNow()
					}
					chain = search
				}
				if test.input.program {
					chain = toolchain.NewJsToolChainWrapper(chain)
				}
				return react.NewAgent(model).WithToolChain(chain).WithRecoveryVersion(version)
			}
			worker, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := &recoveryStore{codecs: gent.NewCodecRegistry(), cancel: cancel,
				phase: gent.PhaseInitial,
				view:  gent.RunView{SchemaVersion: gent.RunSchemaVersion, RunId: "guidance-config"}}
			model := &guidanceModel{finish: true}
			agent := build(model, "guidance/v1")
			ctx := gent.NewExecutionContext(worker, "booking", gent.NewBasicLoopData(nil),
				gent.WithRunId("guidance-config"), gent.WithRecorder(store), gent.WithCodecs(store.codecs))
			defer ctx.Release()
			_, _, err := executor.NewRunner(agent, executor.DefaultConfig()).Execute(ctx)
			if !assert.ErrorIs(t, err, context.Canceled) {
				return
			}
			assert.Equal(t, []gent.ModelCallRequest(nil), model.requests)
			freshModel := &guidanceModel{finish: true}
			freshAgent := build(freshModel, "guidance/v2")
			bindings := recoveryBindings(t, freshAgent)
			data, err := store.codecs.Marshal(store.view)
			if !assert.NoError(t, err) {
				return
			}
			var saved gent.RunView
			if !assert.NoError(t, bindings.Codecs.Unmarshal(data, &saved)) {
				return
			}
			fresh, result, err := resumeRecovery(freshAgent, saved, bindings)
			if fresh != nil {
				defer fresh.Release()
			}
			assert.Equal(t, test.expected, expected{result, err, toolCalls, len(freshModel.requests)})
		})
	}
}
