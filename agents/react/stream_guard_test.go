package react

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	agentutil "github.com/rickchristie/gent/agents"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/output"
	"github.com/stretchr/testify/assert"
)

type guardedModel struct {
	choice        *gent.ContentChoice
	streamed      bool
	completionErr error
	calls         int
}

const guardedText = "abcdabcdabcdabcd"

func (m *guardedModel) GenerateContentStream(ctx context.Context, _ gent.ModelCallRequest) (
	gent.Stream, error,
) {
	m.calls++
	if m.calls > 1 {
		return gent.NewCompletedStream(&gent.ContentResponse{Choices: []*gent.ContentChoice{{
			Completion:       gent.ModelCompletion{Status: gent.ModelStatusAnswer},
			CandidateContent: []gent.ContentPart{gent.TextContent{Text: "OK"}},
		}}}, nil), nil
	}
	stream := gent.NewStreamWithDuration()
	response := &gent.ContentResponse{Choices: []*gent.ContentChoice{m.choice}}
	if !m.streamed {
		stream.Complete(response, m.completionErr)
		return stream, nil
	}
	go func() {
		stream.SendMeta(&gent.MetaContent{Type: gent.MetaContentIntermediary,
			Content: []gent.ContentPart{gent.TextContent{Text: guardedText}}})
		// The guard must close the producer, then use its completion and cleanup result.
		<-ctx.Done()
		stream.Complete(response, m.completionErr)
	}()
	return stream, nil
}

func TestRefusalBeforeResponseGuards(t *testing.T) {
	type input struct{ streamed, tools, repetition bool }
	type expected struct {
		result *gent.ExecutionResult
		calls  int
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	refusal := &gent.ModelRefusal{Code: "content_filter", Content: []gent.ContentPart{
		gent.TextContent{Text: "The provider declined this request."}}}
	want := expected{&gent.ExecutionResult{TerminationReason: gent.TerminationRefused,
		Refusal: refusal}, 1}
	cases := []testCase{
		{"final text size", input{}, want},
		{"final calls size", input{tools: true}, want},
		{"stream text size", input{streamed: true}, want},
		{"stream calls size", input{streamed: true, tools: true}, want},
		{"final text repetition", input{repetition: true}, want},
		{"final calls repetition", input{tools: true, repetition: true}, want},
		{"stream text repetition", input{streamed: true, repetition: true}, want},
		{"stream calls repetition", input{streamed: true, tools: true, repetition: true}, want},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			choice := &gent.ContentChoice{Completion: gent.ModelCompletion{
				Status: gent.ModelStatusRefused, Refusal: refusal}}
			if test.input.tools {
				choice.ToolCalls = []gent.ModelToolCall{nativeTestCall("call", "effect", guardedText)}
			} else {
				choice.CandidateContent = []gent.ContentPart{gent.TextContent{Text: guardedText}}
			}
			model := &guardedModel{choice: choice, streamed: test.input.streamed}
			validations, toolCalls := 0, 0
			out := output.NewTextOutput().SetCustomValidator("check", func(
				*gent.ExecutionContext, string,
			) (*gent.OutputValidationResult, error) {
				validations++
				return &gent.OutputValidationResult{Accepted: true}, nil
			})
			loop := NewWithOutput(model, out).WithMaxResponseChars(3)
			config := agentutil.DefaultRepetitionConfig()
			config.Enabled, config.BlockSize = test.input.repetition, 4
			config.Action = agentutil.RepetitionTerminate
			loop.WithRepetitionConfig(config).RegisterTool(gent.DefineTool("effect",
				func(context.Context, map[string]any) (string, error) {
					toolCalls++
					return "done", nil
				}).Description("Perform an operation").ParameterSchema(nil).MustBuild())
			if test.input.repetition {
				loop.WithMaxResponseChars(0)
			}
			base, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			ctx := gent.NewExecutionContext(base, "guarded",
				gent.NewBasicLoopData(gent.NewTask(gent.TextContent{Text: "Read."})))
			defer ctx.Release()
			value, result, err := executor.NewRunner(loop, executor.Config{}).Execute(ctx)
			assert.NoError(t, err)
			assert.Equal(t, test.expected, expected{result, model.calls})
			assert.Equal(t, "", value)
			assert.Equal(t, 0, validations)
			assert.Equal(t, 0, toolCalls)
			assert.Equal(t, int64(0), ctx.Stats().GetCounter(gent.SCRepetitionLoopTotal))
			entries := ctx.History().Entries
			if assert.Len(t, entries, 1) {
				parts := gent.CloneContentParts(choice.CandidateContent)
				for _, call := range choice.ToolCalls {
					parts = append(parts, call)
				}
				assert.Equal(t, []*gent.MessageContent{{Role: gent.ChatMessageTypeAI, Parts: parts}},
					entries[0].Messages)
			}
		})
	}
}

func TestResponseGuardCompletionErrors(t *testing.T) {
	type input struct {
		completionErr error
		refusal       bool
	}
	type expected struct {
		err   string
		calls int
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cleanupErr := errors.New("cleanup failed")
	cases := []testCase{
		{"cancel only", input{completionErr: context.Canceled}, expected{calls: 2}},
		{"wrapped cancel only", input{completionErr: fmt.Errorf("read: %w", context.Canceled)},
			expected{calls: 2}},
		{"cleanup error", input{completionErr: cleanupErr, refusal: true},
			expected{"Loop.Next (iteration 1): model call failed: cleanup failed", 1}},
		{"joined cleanup error", input{completionErr: errors.Join(context.Canceled, cleanupErr),
			refusal: true}, expected{`Loop.Next (iteration 1): model call failed: context canceled
cleanup failed`, 1}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			choice := &gent.ContentChoice{Completion: gent.ModelCompletion{
				Status: gent.ModelStatusIncomplete}}
			if test.input.refusal {
				choice.Completion = gent.ModelCompletion{Status: gent.ModelStatusRefused,
					Refusal: &gent.ModelRefusal{Code: "blocked"}}
			}
			model := &guardedModel{choice: choice, streamed: true,
				completionErr: test.input.completionErr}
			loop := NewAgent(model).WithMaxResponseChars(3).
				WithRepetitionConfig(agentutil.RepetitionConfig{})
			base, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			ctx := gent.NewExecutionContext(base, "guarded",
				gent.NewBasicLoopData(gent.NewTask(gent.TextContent{Text: "Read."})))
			defer ctx.Release()
			value, result, err := executor.NewRunner(loop, executor.Config{}).Execute(ctx)
			got := expected{calls: model.calls}
			if err != nil {
				got.err = err.Error()
			}
			assert.Equal(t, test.expected, got)
			if test.expected.err == "" {
				assert.Equal(t, "OK", value)
				assert.Equal(t, &gent.ExecutionResult{TerminationReason: gent.TerminationSuccess,
					Output: []gent.ContentPart{gent.TextContent{Text: "OK"}}}, result)
				return
			}
			assert.Equal(t, "", value)
			assert.ErrorIs(t, err, cleanupErr)
			assert.Equal(t, &gent.ExecutionResult{TerminationReason: gent.TerminationError,
				Error: err}, result)
			assert.Equal(t, []*gent.HistoryEntry(nil), ctx.History().Entries)
		})
	}
}
