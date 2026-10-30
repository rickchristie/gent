package gent_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents/react"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/output"
	"github.com/stretchr/testify/assert"
)

type refusalRecorder struct {
	view        gent.RunView
	failure     error
	failedBatch string
}

func (r *refusalRecorder) Save(_ context.Context, batch *gent.SaveBatch) error {
	if batch.Checkpoint != nil && batch.Checkpoint.Phase == gent.PhaseTerminal && r.failure != nil {
		r.failedBatch = batch.BatchId
		return r.failure
	}
	if r.view.RunId == "" {
		r.view = gent.RunView{RunId: batch.RunId, SchemaVersion: gent.RunSchemaVersion}
	}
	view, err := gent.ApplyBatch(r.view, batch)
	if err != nil {
		return err
	}
	r.view = view
	return nil
}

func TestNativeRefusalSaveRestoreAndFailures(t *testing.T) {
	type input struct {
		refusal       *gent.ModelRefusal
		calls         bool
		providerError error
		saveError     error
	}
	type expected struct {
		result *gent.ExecutionResult
		err    string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	blocked := &gent.ModelRefusal{Code: "provider-code", Content: []gent.ContentPart{
		gent.TextContent{Text: "The provider declined this request."}}}
	cases := []testCase{
		{"no explanation", input{refusal: &gent.ModelRefusal{}}, expected{result: &gent.ExecutionResult{
			TerminationReason: gent.TerminationRefused, Refusal: &gent.ModelRefusal{}}}},
		{"partial calls", input{refusal: blocked, calls: true}, expected{result: &gent.ExecutionResult{
			TerminationReason: gent.TerminationRefused, Refusal: blocked}}},
		{"producer failure", input{refusal: blocked, providerError: errors.New("stream failed")},
			expected{err: "Loop.Next (iteration 1): model call failed: stream failed"}},
		{"save failure", input{refusal: blocked, saveError: errors.New("save failed")},
			expected{err: "save failed"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			choice := &gent.ContentChoice{Completion: gent.ModelCompletion{Status: gent.ModelStatusRefused,
				Refusal: tt.input.refusal}, CandidateContent: []gent.ContentPart{
				gent.TextContent{Text: "Provisional fragment."}}}
			if tt.input.calls {
				choice.ToolCalls = []gent.ModelToolCall{{ID: "call", Type: "function",
					FunctionCall: &gent.FunctionCall{Name: "effect", Arguments: "{}"}}}
			}
			model := &contentModel{replies: []contentReply{{choice: choice,
				deltas: []string{"Provisional fragment."}, err: tt.input.providerError}}}
			validationCalls := 0
			out := output.NewTextOutput().SetCustomValidator("refusal-check", func(
				_ *gent.ExecutionContext, value string,
			) (*gent.OutputValidationResult, error) {
				validationCalls++
				return &gent.OutputValidationResult{Accepted: true}, nil
			})
			toolCalls := 0
			build := func(model gent.Model) *react.Agent[string] {
				return react.NewWithOutput(model, out).WithRecoveryVersion("refusal-v1").
					RegisterTool(gent.DefineTool("effect", func(context.Context, map[string]any) (string, error) {
						toolCalls++
						return "done", nil
					}).Description("Perform an operation").ParameterSchema(nil).MustBuild())
			}
			store := &refusalRecorder{failure: tt.input.saveError}
			ctx := gent.NewExecutionContext(context.Background(), "root",
				gent.NewBasicLoopData(gent.NewTask(gent.TextContent{Text: "Complete the task."})),
				gent.WithRecorder(store))
			chunks, stop := ctx.SubscribeAll()
			defer stop()
			_, result, err := executor.NewRunner(build(model), executor.DefaultConfig()).Execute(ctx)
			assert.Equal(t, 1, len(model.requests))
			assert.Equal(t, 0, validationCalls)
			assert.Equal(t, 0, toolCalls)
			assert.Equal(t, int64(0), ctx.Stats().GetCounter(gent.SCOutputRejectedTotal))
			for chunk := range chunks {
				assert.Equal(t, []gent.ContentPart(nil), chunk.Content)
			}
			if tt.expected.err != "" {
				if tt.input.saveError != nil {
					assert.ErrorIs(t, err, tt.input.saveError)
					assert.EqualError(t, err, fmt.Sprintf("gent: save batch %s: save failed", store.failedBatch))
				} else {
					assert.EqualError(t, err, tt.expected.err)
				}
				if tt.input.providerError != nil {
					assert.Equal(t, &gent.ExecutionResult{TerminationReason: gent.TerminationError,
						Error: err}, result)
				} else {
					assert.Nil(t, result)
				}
				return
			}
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, tt.expected.result, result)
			assert.NoError(t, gent.ValidateBundle(store.view))
			assert.NotNil(t, store.view.Checkpoint.Outcome.Refusal)
			fresh := &contentModel{}
			_, restored, err := executor.NewRunner(build(fresh), executor.DefaultConfig()).Resume(
				context.Background(), store.view, gent.RestoreBindings{})
			assert.NoError(t, err)
			assert.Equal(t, tt.expected.result, restored)
			assert.Equal(t, 0, len(fresh.requests))
			assert.Equal(t, 0, validationCalls)
			assert.Equal(t, 0, toolCalls)
		})
	}
}
