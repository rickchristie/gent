package gent_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents/react"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/output"
	"github.com/rickchristie/gent/schema"
	"github.com/stretchr/testify/assert"
)

type typedAnswer struct {
	ID     uint64   `json:"id"`
	Labels []string `json:"labels"`
}

type observedOutput[T any] struct {
	gent.Output[T]
	calls *atomic.Int64
}

func (out observedOutput[T]) Instructions() string { panic("instructions belong to the caller") }
func (out observedOutput[T]) Process(ctx *gent.ExecutionContext, content []gent.ContentPart) (
	*gent.OutputResult[T], error,
) {
	out.calls.Add(1)
	return out.Output.Process(ctx, content)
}
func (out observedOutput[T]) RecoveryConfig() any {
	return out.Output.(gent.RecoveryConfig).RecoveryConfig()
}

func typedJSONOutput[T any](
	t *testing.T, rule string, calls, validations *atomic.Int64,
) gent.Output[T] {
	t.Helper()
	out, err := output.NewJsonOutput[T](schema.Field("id").Description("An exact record ID."))
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	out.SetCustomValidator(rule, func(*gent.ExecutionContext, T) (
		*gent.OutputValidationResult, error,
	) {
		validations.Add(1)
		return &gent.OutputValidationResult{Accepted: true}, nil
	})
	return observedOutput[T]{Output: out, calls: calls}
}

func TestTypedOutputReturnsAndRestores(t *testing.T) {
	t.Run("value", func(t *testing.T) {
		checkTypedOutput(t, typedAnswer{ID: 9007199254740993, Labels: []string{"saved"}},
			func(value typedAnswer) { value.Labels[0] = "caller changed" })
	})
	t.Run("pointer", func(t *testing.T) {
		checkTypedOutput(t, &typedAnswer{ID: 9007199254740993, Labels: []string{"saved"}},
			func(value *typedAnswer) { value.ID = 0; value.Labels[0] = "caller changed" })
	})
}

func checkTypedOutput[T any](t *testing.T, expectedValue T, mutate func(T)) {
	t.Helper()
	const candidate = ` { "id":9007199254740993, "labels": ["saved"] } `
	const delivery = `{"id":9007199254740993,"labels":["saved"]}`
	var processed, validated atomic.Int64
	model := &contentModel{replies: []contentReply{{choice: &gent.ContentChoice{
		Completion:       gent.ModelCompletion{Status: gent.ModelStatusAnswer},
		CandidateContent: []gent.ContentPart{gent.TextContent{Text: candidate}},
	}}}}
	task := &gent.Task{Messages: []*gent.ModelMessage{
		gent.TextParts(gent.ChatMessageTypeSystem, "Return the record as JSON."),
		gent.TextParts(gent.ChatMessageTypeHuman, "Read the record."),
	}}
	ctx := gent.NewExecutionContext(context.Background(), "typed", gent.NewBasicLoopData(task))
	out := typedJSONOutput[T](t, "rules/v1", &processed, &validated)
	agent := react.NewWithOutput(model, out).WithRecoveryVersion("app/v1")
	value, result, err := executor.Run(ctx, agent, executor.DefaultConfig())
	if !assert.NoError(t, err) {
		return
	}
	assert.Equal(t, expectedValue, value)
	expectedResult := &gent.ExecutionResult{TerminationReason: gent.TerminationSuccess,
		Output: []gent.ContentPart{gent.TextContent{Text: delivery}}}
	assert.Equal(t, expectedResult, result)
	assert.Equal(t, int64(1), processed.Load())
	assert.Equal(t, int64(1), validated.Load())
	assert.Equal(t, task.Messages, model.requests[0].Messages)
	saved, err := ctx.Export()
	if !assert.NoError(t, err) {
		return
	}
	wire, err := ctx.Codecs().Marshal(saved)
	if !assert.NoError(t, err) {
		return
	}
	var bundle gent.RunView
	if !assert.NoError(t, ctx.Codecs().Unmarshal(wire, &bundle)) {
		return
	}
	mutate(value)
	result.Output[0] = gent.TextContent{Text: "caller changed"}

	type input struct{ rule, application string }
	type expected struct{ err error }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"same contract", input{"rules/v1", "app/v1"}, expected{}},
		{"changed validation", input{"rules/v2", "app/v1"}, expected{gent.ErrIncompatibleCheckpoint}},
		{"changed application", input{"rules/v1", "app/v2"}, expected{gent.ErrIncompatibleCheckpoint}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var processed, validated atomic.Int64
			fresh := &contentModel{}
			out := typedJSONOutput[T](t, test.input.rule, &processed, &validated)
			loop := react.NewWithOutput(fresh, out).WithRecoveryVersion(test.input.application)
			restored, metadata, err := executor.NewRunner(loop, executor.DefaultConfig()).Resume(
				context.Background(), bundle, gent.RestoreBindings{})
			assert.ErrorIs(t, err, test.expected.err)
			if test.expected.err == nil {
				assert.Equal(t, expectedValue, restored)
				assert.Equal(t, expectedResult, metadata)
				mutate(restored)
				again, _, err := executor.NewRunner(loop, executor.DefaultConfig()).Resume(
					context.Background(), bundle, gent.RestoreBindings{})
				assert.NoError(t, err)
				assert.Equal(t, expectedValue, again)
			} else {
				var zero T
				assert.Equal(t, zero, restored)
				assert.Nil(t, metadata)
			}
			assert.Equal(t, int64(0), processed.Load())
			assert.Equal(t, int64(0), validated.Load())
			assert.Empty(t, fresh.requests)
		})
	}
	_, metadata, err := executor.NewRunner(react.NewAgent(&contentModel{}).
		WithRecoveryVersion("app/v1"), executor.DefaultConfig()).Resume(
		context.Background(), bundle, gent.RestoreBindings{})
	assert.ErrorIs(t, err, gent.ErrIncompatibleCheckpoint)
	assert.Nil(t, metadata)
}

// This strategy deliberately discards every entry. The core must preserve the pending correction
// even when a custom strategy does not know how the agent grouped its validation feedback.
type discardHistory struct{}

func (discardHistory) Compact(_ *gent.ExecutionContext, input gent.CompactionInput) (
	gent.HistoryUpdate, error,
) {
	return gent.HistoryUpdate{ExpectedDraftRevision: input.Revision}, nil
}
func (discardHistory) RecoveryConfig() any { return "discard/v1" }

type alwaysCompact struct{}

func (alwaysCompact) ShouldCompact(*gent.ExecutionContext) (bool, error) { return true, nil }
func (alwaysCompact) NotifyCompacted(*gent.ExecutionContext) error       { return nil }
func (alwaysCompact) RecoveryConfig() any                                { return "always/v1" }

type correctionStore struct {
	view   gent.RunView
	cancel context.CancelFunc
}

func (store *correctionStore) Save(_ context.Context, batch *gent.SaveBatch) error {
	if store.view.RunId == "" {
		store.view = gent.RunView{SchemaVersion: gent.RunSchemaVersion, RunId: batch.RunId}
	}
	view, err := gent.ApplyBatch(store.view, batch)
	if err != nil {
		return err
	}
	store.view = view
	if batch.Checkpoint != nil && batch.Checkpoint.Phase == gent.PhaseIterationCompleted &&
		batch.Checkpoint.Contexts[0].Iteration == 1 && store.cancel != nil {
		store.cancel()
		store.cancel = nil
	}
	return nil
}

func correctionOutput() *output.TextOutput {
	return output.NewTextOutput().SetCustomValidator("rules", func(
		_ *gent.ExecutionContext, value string,
	) (
		*gent.OutputValidationResult, error,
	) {
		if value == "ok" {
			return &gent.OutputValidationResult{Accepted: true}, nil
		}
		return &gent.OutputValidationResult{Steering: []gent.ContentPart{
			gent.TextContent{Text: "Replace " + value + " with ok."},
			gent.BinaryContent{MIMEType: "image/png", Data: []byte{1, 2, 3}},
		}}, nil
	})
}

func correctionReplies(values ...string) []contentReply {
	var replies []contentReply
	for _, value := range values {
		replies = append(replies, contentReply{choice: &gent.ContentChoice{
			Completion:       gent.ModelCompletion{Status: gent.ModelStatusAnswer},
			CandidateContent: []gent.ContentPart{gent.TextContent{Text: value}},
		}})
	}
	return replies
}

func TestOutputCorrectionSurvivesCompactionAndRestore(t *testing.T) {
	type input struct{ restore bool }
	type expected struct {
		calls      int
		rejections int64
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"one worker", input{}, expected{3, 2}},
		{"replacement worker", input{true}, expected{3, 2}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			worker, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := &correctionStore{}
			if test.input.restore {
				store.cancel = cancel
			}
			task := &gent.Task{Messages: []*gent.ModelMessage{
				gent.TextParts(gent.ChatMessageTypeSystem, "Check all answers."),
				gent.TextParts(gent.ChatMessageTypeHuman, "Answer now."),
			}}
			model := &contentModel{replies: correctionReplies("bad", "wrong", "ok")}
			ctx := gent.NewExecutionContext(worker, "correction", gent.NewBasicLoopData(task),
				gent.WithRecorder(store))
			ctx.SetCompaction(alwaysCompact{}, discardHistory{})
			agent := react.NewWithOutput(model, correctionOutput()).WithRecoveryVersion("app/v1")
			answer, result, err := executor.NewRunner(agent, executor.DefaultConfig()).Execute(ctx)
			requests := model.requests
			if test.input.restore {
				if !assert.ErrorIs(t, err, context.Canceled) {
					return
				}
				assert.Equal(t, "", answer)
				assert.Equal(t, gent.PhaseIterationCompleted, store.view.Checkpoint.Phase)
				replacement := &contentModel{replies: correctionReplies("wrong", "ok")}
				agent := react.NewWithOutput(replacement, correctionOutput()).WithRecoveryVersion("app/v1")
				answer, result, err = executor.NewRunner(agent, executor.DefaultConfig()).Resume(
					context.Background(), store.view, gent.RestoreBindings{Recorder: store,
						CompactionStrategy: discardHistory{}, CompactionTrigger: alwaysCompact{}})
				requests = append(requests, replacement.requests...)
			}
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, "ok", answer)
			assert.Equal(t, &gent.ExecutionResult{TerminationReason: gent.TerminationSuccess,
				Output: []gent.ContentPart{gent.TextContent{Text: "ok"}}}, result)
			if !assert.Len(t, requests, test.expected.calls) {
				return
			}
			assert.Equal(t, task.Messages, requests[0].Messages)
			for i, candidate := range []string{"bad", "wrong"} {
				want := append([]*gent.ModelMessage(nil), task.Messages...)
				want = append(want, gent.TextParts(gent.ChatMessageTypeAI, candidate), &gent.ModelMessage{
					Role: gent.ChatMessageTypeHuman, Parts: []gent.ContentPart{
						gent.TextContent{Text: "Replace " + candidate + " with ok."},
						gent.BinaryContent{MIMEType: "image/png", Data: []byte{1, 2, 3}},
					}})
				assert.Equal(t, want, requests[i+1].Messages)
			}
			var candidates []string
			for _, entry := range store.view.History.Entries {
				for _, message := range entry.Messages {
					if message.Role == gent.ChatMessageTypeAI {
						candidates = append(candidates,
							gent.ContentText(message.Parts))
					}
				}
			}
			assert.Equal(t, []string{"bad", "wrong", "ok"}, candidates)
			fresh, err := gent.RestoreExecutionContext(
				context.Background(), store.view, gent.RestoreBindings{})
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, test.expected.rejections, fresh.Stats().GetCounter(gent.SCOutputRejectedTotal))
			assert.Equal(t, test.expected.rejections,
				fresh.Stats().GetCounter(gent.SCOutputCustomRejectedTotal))
			assert.Equal(t, test.expected.rejections,
				fresh.Stats().GetCounter(gent.SCOutputCustomRejectedBy+"rules"))
			assert.Equal(t, float64(0), fresh.Stats().GetGauge(gent.SGOutputRejectedConsecutive))
			assert.Equal(t, float64(0), fresh.Stats().GetGauge(gent.SGOutputCustomRejectedConsecutive))
			savedTask, err := fresh.Task()
			assert.NoError(t, err)
			assert.Equal(t, task, savedTask)
			snapshot, err := fresh.CompactionInput()
			assert.NoError(t, err)
			assert.Empty(t, snapshot.Required)
		})
	}
}

type decisionOutput struct {
	result *gent.OutputResult[string]
	err    error
}

func (decisionOutput) Instructions() string                   { return "" }
func (decisionOutput) Configure(*gent.ModelCallRequest) error { return nil }
func (out decisionOutput) Process(*gent.ExecutionContext, []gent.ContentPart) (
	*gent.OutputResult[string], error,
) {
	return out.result, out.err
}

func TestOutputInvalidDecisionsStopTheLoop(t *testing.T) {
	type input struct {
		result *gent.OutputResult[string]
		err    error
	}
	type expected struct{ message string }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"no result", input{}, expected{"gent: output returned no result"}},
		{"blank steering", input{result: &gent.OutputResult[string]{Steering: []gent.ContentPart{
			gent.TextContent{Text: " "}}}}, expected{"gent: rejected output requires useful steering"}},
		{"acceptance with steering", input{result: &gent.OutputResult[string]{Accepted: true,
			Value: "unsafe", Steering: []gent.ContentPart{gent.TextContent{Text: "retry"}}}},
			expected{"gent: accepted output contains steering"}},
		{"error ignores acceptance", input{result: &gent.OutputResult[string]{Accepted: true,
			Value: "unsafe"}, err: errors.New("processing failed")}, expected{"processing failed"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			model := &contentModel{replies: correctionReplies("candidate")}
			ctx := gent.NewExecutionContext(context.Background(), "invalid", gent.NewBasicLoopData(
				gent.NewTask(gent.TextContent{Text: "Answer."})))
			loop := react.NewWithOutput(model, decisionOutput{test.input.result, test.input.err})
			answer, result, err := executor.Run(ctx, loop, executor.DefaultConfig())
			assert.EqualError(t, err, "Loop.Next (iteration 1): "+test.expected.message)
			assert.Equal(t, "", answer)
			assert.Equal(t, gent.TerminationError, result.TerminationReason)
			assert.Empty(t, result.Output)
			assert.Len(t, model.requests, 1)
			assert.Equal(t, int64(0), ctx.Stats().GetCounter(gent.SCOutputRejectedTotal))
		})
	}
}

type validationLoop struct {
	checks []*output.TextOutput
	next   int
}

func (*validationLoop) Output() gent.Output[string] { return output.NewTextOutput() }
func (loop *validationLoop) Next(ctx *gent.ExecutionContext) (*gent.AgentLoopResult, error) {
	check := loop.checks[loop.next]
	loop.next++
	_, err := check.Process(ctx, []gent.ContentPart{gent.TextContent{Text: "candidate"}})
	if err != nil {
		return nil, err
	}
	return &gent.AgentLoopResult{
		Action: gent.LAContinue,
	}, nil
}

func TestOutputCustomValidationPrefixLimit(t *testing.T) {
	loop := &validationLoop{}
	for _, name := range []string{"alpha", "beta", "beta", "beta"} {
		loop.checks = append(loop.checks, output.NewTextOutput().SetCustomValidator(name,
			func(*gent.ExecutionContext, string) (*gent.OutputValidationResult, error) {
				return &gent.OutputValidationResult{Steering: []gent.ContentPart{
					gent.TextContent{Text: "Return an approved result."}}}, nil
			}))
	}
	ctx := gent.NewExecutionContext(context.Background(), "prefix", gent.NewBasicLoopData(nil))
	limit := gent.Limit{Type: gent.LimitKeyPrefix, Key: gent.SCOutputCustomRejectedBy, MaxValue: 2}
	ctx.SetLimits([]gent.Limit{limit})
	value, result, err := executor.Run(ctx, loop, executor.DefaultConfig())
	assert.EqualError(t, err, "limit exceeded: gent:output_custom_rejected_by: > 2")
	assert.Equal(t, "", value)
	assert.Equal(t, gent.TerminationLimitExceeded, result.TerminationReason)
	assert.Equal(t, &limit, result.ExceededLimit)
	assert.Equal(t, 4, ctx.Iteration())
	assert.Equal(t, int64(1), ctx.Stats().GetCounter(gent.SCOutputCustomRejectedBy+"alpha"))
	assert.Equal(t, int64(3), ctx.Stats().GetCounter(gent.SCOutputCustomRejectedBy+"beta"))
	var exceeded []gent.StatKey
	for _, event := range ctx.Events() {
		if event, ok := event.(*gent.LimitExceededEvent); ok {
			exceeded = append(exceeded, event.MatchedKey)
		}
	}
	assert.Equal(t, []gent.StatKey{gent.SCOutputCustomRejectedBy + "beta"}, exceeded)
}

func TestNullableTypedOutputTerminalRestore(t *testing.T) {
	out, err := output.NewSchemaOutput[*string](schema.Nullable(schema.String("A label").Schema()))
	if !assert.NoError(t, err) {
		return
	}
	model := &contentModel{replies: correctionReplies("null")}
	agent := react.NewWithOutput(model, out).WithRecoveryVersion("app/v1")
	ctx := gent.NewExecutionContext(context.Background(), "nullable", gent.NewBasicLoopData(
		gent.NewTask(gent.TextContent{Text: "Return a nullable label."})))
	value, result, err := executor.Run(ctx, agent, executor.DefaultConfig())
	if !assert.NoError(t, err) {
		return
	}
	assert.Nil(t, value)
	assert.Equal(t, &gent.ExecutionResult{TerminationReason: gent.TerminationSuccess,
		Output: []gent.ContentPart{gent.TextContent{Text: "null"}}}, result)
	saved, err := ctx.Export()
	if !assert.NoError(t, err) {
		return
	}
	fresh := &contentModel{}
	restored, metadata, err := executor.NewRunner(react.NewWithOutput(fresh, out).
		WithRecoveryVersion("app/v1"), executor.DefaultConfig()).Resume(
		context.Background(), saved, gent.RestoreBindings{})
	assert.NoError(t, err)
	assert.Nil(t, restored)
	assert.Equal(t, result, metadata)
	assert.Empty(t, fresh.requests)
}
