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
	requests        []gent.ModelCallRequest
}

const interruptedScript = `tool.call({tool:"lookup",args:{request_id:"request-7"}});
tool.call({tool:"book",args:{request_id:"request-7"}});
tool.call({tool:"later",args:{request_id:"request-7"}});`

func (m *guidanceModel) GenerateContentStream(
	_ context.Context, request gent.ModelCallRequest,
) (gent.Stream, error) {
	m.requests = append(m.requests, request)
	choice := &gent.ContentChoice{CandidateContent: "Check the booking before another attempt."}
	if !m.finish {
		choice.CandidateContent = ""
		for _, name := range []string{"lookup", "book", "later"} {
			choice.ToolCalls = append(choice.ToolCalls, gent.ModelToolCall{ID: "provider-" + name,
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
			choice.ToolCalls = []gent.ModelToolCall{{ID: "provider-program", Type: "function",
				FunctionCall: &gent.FunctionCall{Name: "run_code", Arguments: string(args)}}}
		}
	}
	response := &gent.ContentResponse{Choices: []*gent.ContentChoice{choice}}
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
			result := value.(*gent.AfterToolCallEvent).Output.(*gent.ToolCallResult)
			if result.Outcome == "unknown" {
				return errRecoveryAck
			}
		}
	}
	if !s.afterEffect && batch.Checkpoint != nil && batch.Checkpoint.Phase == gent.PhaseToolReady {
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
	type input struct{ program, afterEffect, loseRecoveryAck bool }
	type expected struct {
		effects                       map[string]int
		toolMessages, programMessages int
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"batch before effect", input{}, expected{map[string]int{"lookup": 1}, 1, 0}},
		{"batch lost result", input{afterEffect: true},
			expected{map[string]int{"lookup": 1, "book": 1}, 1, 0}},
		{"program before effect", input{program: true}, expected{map[string]int{"lookup": 1}, 1, 1}},
		{"program lost result", input{program: true, afterEffect: true},
			expected{map[string]int{"lookup": 1, "book": 1}, 1, 1}},
		{"batch recovery acknowledgement lost", input{afterEffect: true, loseRecoveryAck: true},
			expected{map[string]int{"lookup": 1, "book": 1}, 1, 0}},
		{"program recovery acknowledgement lost",
			input{program: true, afterEffect: true, loseRecoveryAck: true},
			expected{map[string]int{"lookup": 1, "book": 1}, 1, 1}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			effects := map[string]int{}
			var toolInfo, programInfo []*gent.RecoveryInfo
			build := func(model *guidanceModel) *react.Agent {
				chain := toolchain.New()
				for _, name := range []string{"lookup", "book", "later"} {
					chain.RegisterTool(gent.NewToolFunc(name, "Booking operation", nil,
						func(_ context.Context, input *bookingInput) (string, error) {
							effects[name]++
							return name + ":" + input.RequestID, nil
						}), gent.WithRecoveryMessage(func(info *gent.RecoveryInfo) string {
						toolInfo = append(toolInfo, info)
						return "Check request-7 with the booking service before booking again."
					}))
				}
				var selected gent.ToolChain = chain
				if test.input.program {
					selected = toolchain.NewJsToolChainWrapper(chain).WithRecoveryMessage(
						func(info *gent.RecoveryInfo) string {
							programInfo = append(programInfo, info)
							return "Keep completed lookup results. Resolve the booking before the later operation."
						})
				}
				return react.NewAgent(model).WithToolChain(selected).WithRecoveryVersion("guidance/v1")
			}
			worker, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := &guidanceStore{codecs: gent.NewCodecRegistry(), cancel: cancel,
				afterEffect: test.input.afterEffect,
				view:        gent.RunView{SchemaVersion: gent.RunSchemaVersion, RunId: "guidance"}}
			model := &guidanceModel{program: test.input.program}
			data := gent.NewBasicLoopData(&gent.Task{Text: "Book once."})
			ctx := gent.NewExecutionContext(worker, "booking", data,
				gent.WithRunId("guidance"), gent.WithRecorder(store), gent.WithCodecs(store.codecs))
			_, err := executor.New[gent.LoopData](build(model), executor.DefaultConfig()).Execute(ctx)
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
			assert.Equal(t, gent.TerminationSuccess, result.TerminationReason)
			assert.Equal(t, test.expected, expected{effects, len(toolInfo), len(programInfo)})
			if !assert.Len(t, toolInfo, 1) {
				return
			}
			info := toolInfo[0]
			assert.Equal(t, "book", info.Name)
			assert.Equal(t, &bookingInput{RequestID: "request-7"}, info.Input)
			assert.Equal(t, map[string]any{"request_id": "request-7"}, info.Args)
			if test.input.program {
				assert.Equal(t, interruptedScript, programInfo[0].Script)
				assert.Len(t, programInfo[0].Completed, 1)
				assert.Equal(t, "lookup", programInfo[0].Completed[0].Name)
				assert.Len(t, programInfo[0].Uncertain, 1)
			}
			var outputs []gent.ToolCallResponse
			for _, message := range freshModel.requests[0].Messages {
				for _, part := range message.Parts {
					if output, ok := part.(gent.ToolCallResponse); ok {
						outputs = append(outputs, output)
					}
				}
			}
			if test.input.program {
				assert.Equal(t, "provider-program", outputs[0].ToolCallID)
			} else {
				assert.Equal(t, gent.ToolCallResponse{ToolCallID: "provider-book", Name: "book",
					Content: fmt.Sprintf(`Tool book (call %s):
%s

Recovery guidance:
Check request-7 with the booking service before booking again.`,
						info.ToolCallId, gent.DefaultRecoveryMessage)}, outputs[1])
				assert.Equal(t, gent.ToolCallResponse{ToolCallID: "provider-later", Name: "later",
					Content: "This call did not start. Review the interrupted operation before making a new plan.",
				}, outputs[2])
			}
			view, err := restored.Export()
			assert.NoError(t, err)
			assert.NoError(t, gent.ValidateBundle(view, restored.Codecs()))
		})
	}
}

type recoverySearchTool struct {
	*gent.ToolFunc[*bookingInput, string]
}

func (*recoverySearchTool) Domain() string             { return "bookings" }
func (*recoverySearchTool) Categories() []string       { return []string{"bookings"} }
func (*recoverySearchTool) Keywords() []string         { return []string{"book"} }
func (*recoverySearchTool) SyntheticQueries() []string { return []string{"book a flight"} }

func TestInterruptedProgramKeepsSearchDiscovery(t *testing.T) {
	effects := 0
	build := func(model *guidanceModel) *react.Agent {
		chain := toolchain.NewSearch(toolchain.SearchHintSimpleList).
			RegisterEngine(toolchain.NewRegexToolSearcher())
		chain.RegisterTool(&recoverySearchTool{gent.NewToolFunc("book", "Book a flight", nil,
			func(context.Context, *bookingInput) (string, error) { effects++; return "booked", nil })})
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
		gent.NewBasicLoopData(&gent.Task{Text: "Book a flight."}), gent.WithRunId("search-program"),
		gent.WithRecorder(store), gent.WithCodecs(store.codecs))
	_, err := executor.New[gent.LoopData](build(model), executor.DefaultConfig()).Execute(ctx)
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
	assert.Equal(t, []string{"tool_registry_search", "book", "run_code"}, names)
}
