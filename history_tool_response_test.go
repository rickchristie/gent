package gent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents/react"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/toolchain"
	"github.com/stretchr/testify/assert"
)

// This recorder accepts a recovery decision but loses its acknowledgement. The next worker
// must use the saved decision, even if the first worker never received confirmation.
type recoveryDecisionStore struct {
	view   gent.RunView
	codecs *gent.CodecRegistry
}

func (s *recoveryDecisionStore) Save(_ context.Context, batch *gent.SaveBatch) error {
	next, err := gent.ApplyBatch(s.view, batch, s.codecs)
	if err != nil {
		return err
	}
	s.view = next
	if batch.Checkpoint == nil || batch.Checkpoint.Phase != gent.PhaseToolReady {
		return nil
	}
	for _, state := range batch.Checkpoint.Contexts[0].State {
		value, err := s.codecs.Decode(state)
		if err != nil {
			return err
		}
		plan, ok := value.(*gent.ToolPlan)
		if ok && plan.ReadyCall != nil && plan.ReadyCall.Recovery != nil {
			return errRecoveryAck
		}
	}
	return nil
}

func TestToolRecoveryDecisionSurvivesLostAcknowledgement(t *testing.T) {
	type input struct{ result gent.ToolRecoveryResult }
	type expected struct {
		calls int
		text  string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"completed", input{gent.ToolRecoveryCompleted}, expected{1, `Recovered policy.

Recovery guidance:
Use the checked policy.`}},
		{"retry", input{gent.ToolRecoveryRetryAllowed}, expected{2, `<policy>
P10: exact original text
</policy>

Recovery guidance:
Use the checked policy.`}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			saved, _, _ := saveInterruptedRun(t, gent.PhaseToolReady, "")
			var calls []gent.ToolIdentity
			recoveries := 0
			agent := newRecoveryAgent(&recoveryModel{answer: true}, &calls,
				func(context.Context, *gent.ToolRecoveryRequest) (*gent.ToolRecoveryResponse, error) {
					recoveries++
					response := &gent.ToolRecoveryResponse{Result: test.input.result,
						SteeringMessage: "Use the checked policy."}
					if response.Result == gent.ToolRecoveryCompleted {
						response.Output = &gent.ToolCallResult{Text: "Recovered policy.",
							Output: "Recovered policy.", SummaryCaptured: true}
					}
					return response, nil
				})
			bindings := recoveryBindings(t, agent)
			store := &recoveryDecisionStore{view: saved, codecs: bindings.Codecs}
			bindings.Recorder = store
			first, _, err := resumeRecovery(agent, saved, bindings)
			if first != nil {
				defer first.Release()
			}
			if !assert.ErrorIs(t, err, errRecoveryAck) {
				return
			}
			assert.Equal(t, 1, recoveries)
			assert.Empty(t, calls)
			freshAgent := newRecoveryAgent(&recoveryModel{answer: true}, &calls,
				func(context.Context, *gent.ToolRecoveryRequest) (*gent.ToolRecoveryResponse, error) {
					recoveries++
					return nil, errors.New("saved recovery must take precedence")
				})
			freshBindings := recoveryBindings(t, freshAgent)
			// Cross the same serialization boundary as a replacement process.
			data, err := store.codecs.Marshal(store.view)
			assert.NoError(t, err)
			var restored gent.RunView
			assert.NoError(t, freshBindings.Codecs.Unmarshal(data, &restored))
			fresh, _, err := resumeRecovery(freshAgent, restored, freshBindings)
			if !assert.NoError(t, err) {
				return
			}
			defer fresh.Release()
			view, err := fresh.Export()
			assert.NoError(t, err)
			var outputs []*gent.ToolCallResult
			for _, event := range view.Activity {
				if event.Type != "tool_call_finished" {
					continue
				}
				value, err := fresh.DecodeEvent(event)
				if !assert.NoError(t, err) {
					return
				}
				outputs = append(outputs, value.(*gent.AfterToolCallEvent).Output.(*gent.ToolCallResult))
			}
			if !assert.Len(t, outputs, 2) {
				return
			}
			assert.Equal(t, test.expected, expected{len(calls), outputs[0].Text})
			assert.Equal(t, "Use the checked policy.", outputs[0].Instructions)
			assert.Equal(t, "", outputs[1].Instructions)
			assert.Equal(t, 1, recoveries)
		})
	}
}

func TestToolRecoveryRejectsInvalidResponses(t *testing.T) {
	type input struct {
		response *gent.ToolRecoveryResponse
		err      error
	}
	type expected struct{ err string }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"missing response", input{}, expected{"gent: tool recovery returned no response"}},
		{"unknown decision", input{response: &gent.ToolRecoveryResponse{Result: "retry"}},
			expected{`gent: invalid tool recovery result "retry"`}},
		{"missing output", input{response: &gent.ToolRecoveryResponse{
			Result: gent.ToolRecoveryCompleted}}, expected{"gent: completed tool recovery has no output"}},
		{"unexpected output", input{response: &gent.ToolRecoveryResponse{
			Result: gent.ToolRecoveryUnresolved, Output: &gent.ToolCallResult{}}},
			expected{"gent: only completed tool recovery can return output"}},
		{"conflicting outcome", input{response: &gent.ToolRecoveryResponse{
			Result: gent.ToolRecoveryCompleted, Output: &gent.ToolCallResult{Outcome: "unknown"}}},
			expected{"gent: completed tool recovery cannot have an unknown or unstarted outcome"}},
		{"service error", input{err: errors.New("receipt service unavailable")},
			expected{"receipt service unavailable"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			saved, _, _ := saveInterruptedRun(t, gent.PhaseToolReady, "")
			var calls []gent.ToolIdentity
			agent := newRecoveryAgent(&recoveryModel{answer: true}, &calls,
				func(context.Context, *gent.ToolRecoveryRequest) (*gent.ToolRecoveryResponse, error) {
					return test.input.response, test.input.err
				})
			fresh, _, err := resumeRecovery(agent, saved, recoveryBindings(t, agent))
			if fresh != nil {
				defer fresh.Release()
			}
			assert.EqualError(t, err, "Loop.Next (iteration 1): "+test.expected.err)
			assert.Empty(t, calls)
		})
	}
}

func TestToolRecoveryRetryInsideProgram(t *testing.T) {
	type input struct{ program bool }
	type expected struct{ calls map[string]int }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"native batch continues", input{}, expected{map[string]int{"lookup": 1, "book": 1, "later": 1}}},
		{"program does not restart", input{true}, expected{map[string]int{"lookup": 1, "book": 1}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			calls := map[string]int{}
			recoveries := []string{}
			build := func(model *guidanceModel) *react.Agent[string] {
				var chain gent.ToolChain = toolchain.New()
				for _, name := range []string{"lookup", "book", "later"} {
					tool := gent.DefineTool(name,
						func(context.Context, *bookingInput) (string, error) {
							calls[name]++
							return name, nil
						}).Recovery(func(_ context.Context, request *gent.ToolRecoveryRequest) (
						*gent.ToolRecoveryResponse, error,
					) {
						recoveries = append(recoveries, request.Name)
						return &gent.ToolRecoveryResponse{Result: gent.ToolRecoveryRetryAllowed,
							SteeringMessage: "The saved request can be repeated safely."}, nil
					}).MustBuild()
					chain.RegisterTool(tool)
				}
				if test.input.program {
					chain = toolchain.NewJsToolChainWrapper(chain)
				}
				return react.NewAgent(model).WithToolChain(chain).WithRecoveryVersion("retry/v1")
			}
			worker, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := &guidanceStore{codecs: gent.NewCodecRegistry(), cancel: cancel,
				view: gent.RunView{SchemaVersion: gent.RunSchemaVersion, RunId: "retry"}}
			agent := build(&guidanceModel{program: test.input.program})
			data := gent.NewBasicLoopData(gent.NewTask(gent.TextContent{Text: "Check the booking."}))
			ctx := gent.NewExecutionContext(worker, "booking", data,
				gent.WithRunId("retry"), gent.WithCodecs(store.codecs), gent.WithRecorder(store))
			defer ctx.Release()
			_, _, err := executor.NewRunner(agent, executor.DefaultConfig()).Execute(ctx)
			if !assert.ErrorIs(t, err, context.Canceled) {
				return
			}
			freshAgent := build(&guidanceModel{finish: true})
			fresh, _, err := resumeRecovery(freshAgent, store.view, recoveryBindings(t, freshAgent))
			if !assert.NoError(t, err) {
				return
			}
			defer fresh.Release()
			assert.Equal(t, test.expected, expected{calls})
			assert.Equal(t, []string{"book"}, recoveries)
			view, err := fresh.Export()
			assert.NoError(t, err)
			var books []*gent.ToolCallResult
			for _, event := range view.Activity {
				if event.Type != "tool_call_finished" {
					continue
				}
				value, err := fresh.DecodeEvent(event)
				if !assert.NoError(t, err) {
					return
				}
				result := value.(*gent.AfterToolCallEvent).Output.(*gent.ToolCallResult)
				if result.Name == "book" {
					books = append(books, result)
				}
			}
			if !assert.Len(t, books, 1) {
				return
			}
			assert.Equal(t, "The saved request can be repeated safely.", books[0].Instructions)
			assert.Equal(t, `<book>
book
</book>

Recovery guidance:
The saved request can be repeated safely.`, books[0].Text)
			assert.Equal(t, "", books[0].Outcome)
		})
	}
}
