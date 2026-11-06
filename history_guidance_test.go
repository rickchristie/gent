package gent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents/react"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/toolchain"
	"github.com/stretchr/testify/assert"
)

type bookingInput struct {
	RequestID string `json:"request_id"`
}

type guidanceModel struct {
	program, finish bool
	script          string
	requests        []*gent.ModelCallRequest
}

const interruptedScript = `tool.call({tool:"lookup",args:{request_id:"request-7"}});
tool.call({tool:"book",args:{request_id:"request-7"}});
tool.call({tool:"later",args:{request_id:"request-7"}});`

func (m *guidanceModel) GenerateContentStream(
	_ context.Context, request *gent.ModelCallRequest,
) (gent.Stream, error) {
	m.requests = append(m.requests, request)
	choice := &gent.ContentChoice{
		Completion: gent.ModelCompletion{Status: gent.ModelStatusAnswer},
		CandidateContent: []gent.ContentPart{
			gent.TextContent{Text: "Check the booking before another attempt."},
		},
	}
	if !m.finish {
		choice.CandidateContent = nil
		for _, name := range []string{"lookup", "book", "later"} {
			choice.Completion.Status = gent.ModelStatusToolCalls
			choice.ToolCalls = append(choice.ToolCalls, &gent.ModelToolCallRequest{ID: "provider-" + name,
				Type: "function", FunctionCall: &gent.FunctionCall{
					Name: name, Arguments: `{"request_id":"request-7"}`}})
		}
		if m.program {
			script := m.script
			if script == "" {
				script = interruptedScript
			}
			args, err := json.Marshal(map[string]string{"code": script})
			if err != nil {
				return nil, err
			}
			choice.Completion.Status = gent.ModelStatusToolCalls
			choice.ToolCalls = []*gent.ModelToolCallRequest{{ID: "provider-program", Type: "function",
				FunctionCall: &gent.FunctionCall{Name: "run_code", Arguments: string(args)}}}
		}
	}
	response := &gent.ModelCallResponse{Choices: []*gent.ContentChoice{choice}}
	return gent.NewCompletedStream(response, nil), nil
}

var errRecoveryAck = errors.New("recovery acknowledgement lost")

type guidanceStore struct {
	view            gent.RunView
	codecs          *gent.CodecRegistry
	cancel          context.CancelFunc
	afterEffect     bool
	loseRecoveryAck bool
}

func (s *guidanceStore) Save(_ context.Context, batch *gent.SaveBatch) error {
	if s.afterEffect {
		for _, event := range batch.Events {
			if event.Type != "tool_call_finished" {
				continue
			}
			value, err := s.codecs.Decode(*event.Payload)
			if err != nil {
				return err
			}
			if value.(*gent.AfterToolCallEvent).ToolName == "book" {
				return errors.New("result save failed")
			}
		}
	}
	next, err := gent.ApplyBatch(s.view, batch, s.codecs)
	if err != nil {
		return err
	}
	s.view = next
	if s.loseRecoveryAck {
		for _, event := range batch.Events {
			if event.Type != "tool_call_finished" {
				continue
			}
			value, err := s.codecs.Decode(*event.Payload)
			if err != nil {
				return err
			}
			result := value.(*gent.AfterToolCallEvent).Output.(*gent.ToolCallRecord)
			if result.Kind() == gent.ToolResponseRecovery {
				return errRecoveryAck
			}
		}
	}
	if s.cancel != nil && !s.afterEffect && batch.Checkpoint != nil &&
		batch.Checkpoint.Phase == gent.PhaseToolReady {
		for _, value := range batch.Checkpoint.Contexts[0].State {
			decoded, err := s.codecs.Decode(value)
			if err != nil {
				return err
			}
			plan, ok := decoded.(*gent.ToolPlan)
			if ok && plan.ReadyCall != nil && plan.ReadyCall.Name == "book" {
				s.cancel()
			}
		}
	}
	return nil
}

func TestNativeRecoveryGuidance(t *testing.T) {
	type input struct{ program, afterEffect, loseRecoveryAck, search bool }
	type expected struct {
		effects map[string]int
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"batch before effect", input{}, expected{map[string]int{"lookup": 1}}},
		{"batch lost result", input{afterEffect: true},
			expected{map[string]int{"lookup": 1, "book": 1}}},
		{"program before effect", input{program: true}, expected{map[string]int{"lookup": 1}}},
		{"program lost result", input{program: true, afterEffect: true},
			expected{map[string]int{"lookup": 1, "book": 1}}},
		{"batch recovery acknowledgement lost", input{afterEffect: true, loseRecoveryAck: true},
			expected{map[string]int{"lookup": 1, "book": 1}}},
		{"program recovery acknowledgement lost",
			input{program: true, afterEffect: true, loseRecoveryAck: true},
			expected{map[string]int{"lookup": 1, "book": 1}}},
		{"search batch lost result", input{search: true, afterEffect: true},
			expected{map[string]int{"lookup": 1, "book": 1}}},
		{"search program lost result", input{search: true, program: true, afterEffect: true},
			expected{map[string]int{"lookup": 1, "book": 1}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			effects := map[string]int{}
			build := func(model *guidanceModel) *react.Agent[string] {
				var chain gent.ToolChain = toolchain.New()
				if test.input.search {
					chain = toolchain.NewSearch(toolchain.SearchHintSimpleList).
						RegisterEngine(toolchain.NewRegexToolSearcher())
				}
				messages := map[string]string{
					"lookup": "Look up the saved request ID again.",
					"book":   "Check the saved request ID with the booking service before booking again.",
					"later":  "Check whether the later operation completed.",
				}
				for _, name := range []string{"lookup", "book", "later"} {
					tool := gent.DefineTool(name, func(_ context.Context, input *bookingInput) (
						*gent.ToolExecutionResponse, error,
					) {
						effects[name]++
						return capturedToolResponse(name+":"+input.RequestID, nil)
					}).Description("Booking operation").ParameterSchema(nil).
						Recovery(func(context.Context, *gent.ToolRecoveryRequest[*bookingInput]) (
							*gent.ToolRecoveryResponse, error,
						) {
							return &gent.ToolRecoveryResponse{Status: gent.ToolRecoveryUnresolved,
								Instructions: messages[name]}, nil
						}).MustBuild()
					chain.RegisterTool(&recoverySearchTool{tool})
				}
				if search, ok := chain.(*toolchain.SearchChain); ok {
					search.Pin("lookup").Pin("book").Pin("later")
					if !assert.NoError(t, search.Initialize()) {
						t.FailNow()
					}
				}
				var selected gent.ToolChain = chain
				if test.input.program {
					selected = toolchain.NewJsToolChainWrapper(chain)
				}
				return react.NewAgent(model).WithToolChain(selected).WithRecoveryVersion("guidance/v1")
			}
			worker, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := &guidanceStore{codecs: gent.NewCodecRegistry(), cancel: cancel,
				afterEffect: test.input.afterEffect,
				view:        gent.RunView{SchemaVersion: gent.RunSchemaVersion, RunId: "guidance"}}
			model := &guidanceModel{program: test.input.program}
			data := gent.NewBasicLoopData(gent.NewTask(gent.TextContent{Text: "Book once."}))
			ctx := gent.NewExecutionContext(worker, "booking", data,
				gent.WithRunId("guidance"), gent.WithRecorder(store), gent.WithCodecs(store.codecs))
			defer ctx.Release()
			_, _, err := executor.NewRunner(build(model), executor.DefaultConfig()).Execute(ctx)
			assert.Error(t, err)
			assert.Equal(t, test.expected.effects, effects)
			freshModel := &guidanceModel{program: test.input.program, finish: true}
			freshAgent := build(freshModel)
			bindings := recoveryBindings(t, freshAgent)
			savedData, err := store.codecs.Marshal(store.view)
			if !assert.NoError(t, err) {
				return
			}
			var saved gent.RunView
			if !assert.NoError(t, bindings.Codecs.Unmarshal(savedData, &saved)) {
				return
			}
			if test.input.loseRecoveryAck {
				recoveryStore := &guidanceStore{view: saved, codecs: bindings.Codecs,
					loseRecoveryAck: true}
				bindings.Recorder = recoveryStore
				_, _, err := resumeRecovery(freshAgent, saved, bindings)
				if !assert.ErrorIs(t, err, errRecoveryAck) {
					return
				}
				assert.Empty(t, freshModel.requests)
				saved = recoveryStore.view
				freshModel = &guidanceModel{program: test.input.program, finish: true}
				freshAgent = build(freshModel)
				bindings = recoveryBindings(t, freshAgent)
			}
			restored, result, err := resumeRecovery(freshAgent, saved, bindings)
			if !assert.NoError(t, err) {
				return
			}
			defer restored.Release()
			assert.Equal(t, gent.TerminationSuccess, result.TerminationReason)
			assert.Equal(t, test.expected, expected{effects})
			var outputs []*gent.ModelToolCallResponse
			for _, message := range freshModel.requests[0].Messages {
				for _, part := range message.Parts {
					if output, ok := part.(*gent.ModelToolCallResponse); ok {
						outputs = append(outputs, output)
					}
				}
			}
			bookNumber := 2
			if test.input.program {
				bookNumber = 3
			}
			bookID := fmt.Sprintf("%s:tool:%d", ctx.ContextId(), bookNumber)
			// Recovery keeps the native call IDs and provides plain guidance without wrapper fields.
			var replies []*gent.ModelToolCallResponse
			for _, message := range freshModel.requests[0].Messages {
				for _, part := range message.Parts {
					if reply, ok := part.(*gent.ModelToolCallResponse); ok {
						replies = append(replies, reply)
					}
				}
			}
			if test.input.program {
				if assert.Len(t, replies, 1) {
					assert.Equal(t, "provider-program", replies[0].ToolCallID())
					assert.Equal(t, gent.ToolResponseRecovery, replies[0].Kind())
				}
			} else if assert.Len(t, replies, 3) {
				assert.Equal(t, []string{"provider-lookup", "provider-book", "provider-later"},
					[]string{replies[0].ToolCallID(), replies[1].ToolCallID(), replies[2].ToolCallID()})
				assert.Equal(t, gent.ToolResponseExecution, replies[0].Kind())
				assert.Equal(t, gent.ToolResponseRecovery, replies[1].Kind())
				status, _ := replies[2].RecoveryStatus()
				assert.Equal(t, gent.ToolRecoveryNotStarted, status)
			}

			view, err := restored.Export()
			assert.NoError(t, err)
			assert.NoError(t, gent.ValidateBundle(view, restored.Codecs()))
			var unknown []*gent.ToolCallRecord
			for _, event := range view.Activity {
				if event.Type != "tool_call_finished" {
					continue
				}
				value, err := restored.DecodeEvent(event)
				if !assert.NoError(t, err) {
					return
				}
				call := value.(*gent.AfterToolCallEvent).Output.(*gent.ToolCallRecord)
				if call.Kind() == gent.ToolResponseRecovery && call.Name() == "book" {
					unknown = append(unknown, call)
				}
			}
			if assert.Len(t, unknown, 1) {
				record := unknown[0]
				assert.Equal(t, "book", record.Name())
				assert.Equal(t, bookID, record.ToolCallID())
				assert.Equal(t, gent.ToolRecoveryUnresolved, record.Recovery().Status)
				assert.Equal(t, "Check the saved request ID with the booking service before booking again.",
					record.Recovery().Instructions)
			}
		})
	}
}

type recoverySearchTool struct {
	gent.Tool[*bookingInput]
}

func (*recoverySearchTool) Domain() string             { return "bookings" }
func (*recoverySearchTool) Categories() []string       { return []string{"bookings"} }
func (*recoverySearchTool) Keywords() []string         { return []string{"book"} }
func (*recoverySearchTool) SyntheticQueries() []string { return []string{"book a flight"} }

func TestInterruptedProgramKeepsSearchDiscovery(t *testing.T) {
	effects := 0
	build := func(model *guidanceModel) *react.Agent[string] {
		chain := toolchain.NewSearch(toolchain.SearchHintSimpleList).
			RegisterEngine(toolchain.NewRegexToolSearcher())
		chain.RegisterTool(&recoverySearchTool{gent.DefineTool("book",
			func(context.Context, *bookingInput) (*gent.ToolExecutionResponse, error) {
				effects++
				return capturedToolResponse("booked", nil)
			}).
			Description("Book a flight").ParameterSchema(nil).MustBuild()})
		if !assert.NoError(t, chain.Initialize()) {
			t.FailNow()
		}
		return react.NewAgent(model).WithToolChain(toolchain.NewJsToolChainWrapper(chain)).
			WithRecoveryVersion("search-program/v1")
	}
	worker, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &guidanceStore{codecs: gent.NewCodecRegistry(), cancel: cancel,
		view: gent.RunView{SchemaVersion: gent.RunSchemaVersion, RunId: "search-program"}}
	model := &guidanceModel{program: true, script: `tool.call({tool:"tool_registry_search",
args:{query_type:"regex",query:"book"}});
tool.call({tool:"book",args:{request_id:"request-7"}});`}
	ctx := gent.NewExecutionContext(worker, "search program",
		gent.NewBasicLoopData(gent.NewTask(gent.TextContent{
			Text: "Book a flight.",
		})), gent.WithRunId("search-program"),
		gent.WithRecorder(store), gent.WithCodecs(store.codecs))
	_, _, err := executor.NewRunner(build(model), executor.DefaultConfig()).Execute(ctx)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 0, effects)
	freshModel := &guidanceModel{finish: true}
	fresh := build(freshModel)
	bindings := recoveryBindings(t, fresh)
	encoded, err := store.codecs.Marshal(store.view)
	assert.NoError(t, err)
	var saved gent.RunView
	assert.NoError(t, bindings.Codecs.Unmarshal(encoded, &saved))
	_, result, err := resumeRecovery(fresh, saved, bindings)
	if !assert.NoError(t, err) {
		return
	}
	assert.Equal(t, gent.TerminationSuccess, result.TerminationReason)
	assert.Equal(t, 0, effects)
	var names []string
	for _, definition := range freshModel.requests[0].Options.Tools {
		names = append(names, definition.Function.Name)
	}
	assert.Equal(t, []string{"tool_registry_search", "execute_tool", "run_code"}, names)
}
func (m *guidanceModel) PrepareRequest(ctx *gent.ExecutionContext,
	request *gent.ModelCallRequest, chain gent.ToolChain,
) (*gent.ModelCallRequest, error) {
	return gent.PrepareModelRequest(ctx, request, chain)
}
