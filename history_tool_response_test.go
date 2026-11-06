package gent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

// Accept the recovery record, then lose the acknowledgement. A new worker must reuse that
// prepared reply even when the first worker did not receive confirmation of the save.
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
	for _, event := range batch.Events {
		if event.Type != "tool_call_finished" {
			continue
		}
		value, err := s.codecs.Decode(*event.Payload)
		if err != nil {
			return err
		}
		record := value.(*gent.AfterToolCallEvent).Output.(*gent.ToolCallRecord)
		if record.Kind() == gent.ToolResponseRecovery {
			return errRecoveryAck
		}
	}
	return nil
}

func TestToolRecoveryDecisionSurvivesLostAcknowledgement(t *testing.T) {
	saved, _, _ := saveInterruptedRun(t, gent.PhaseToolReady, "")
	var calls []gent.ToolIdentity
	recoveries := 0
	agent := newRecoveryAgent(&recoveryModel{answer: true}, &calls,
		func(context.Context, *gent.ToolRecoveryRequest[recoveryInput]) (
			*gent.ToolRecoveryResponse, error,
		) {
			recoveries++
			return &gent.ToolRecoveryResponse{Status: gent.ToolRecoveryCompleted,
				Execution: &gent.ToolExecutionResponse{Status: gent.ToolExecuted,
					Parts:        []gent.ContentPart{gent.TextPart("Recovered original policy.")},
					Instructions: "Original guidance."},
				Parts:        []gent.ContentPart{gent.TextPart("Verified external receipt.")},
				Instructions: "Check current policy before a new action."}, nil
		})
	bindings := recoveryBindings(t, agent)
	store := &recoveryDecisionStore{view: saved, codecs: bindings.Codecs}
	bindings.Recorder = store
	first, _, err := resumeRecovery(agent, saved, bindings)
	if first != nil {
		defer first.Release()
	}
	assert.ErrorIs(t, err, errRecoveryAck)
	assert.Equal(t, 1, recoveries)
	assert.Empty(t, calls)
	nextModel := &recoveryModel{answer: true}
	nextAgent := newRecoveryAgent(nextModel, &calls,
		func(context.Context, *gent.ToolRecoveryRequest[recoveryInput]) (
			*gent.ToolRecoveryResponse, error,
		) {
			recoveries++
			return nil, errors.New("must reuse saved report")
		})
	next, _, err := resumeRecovery(nextAgent, store.view, recoveryBindings(t, nextAgent))
	if !assert.NoError(t, err) {
		return
	}
	defer next.Release()
	assert.Equal(t, 1, recoveries)
	assert.Empty(t, calls)
	var replies []*gent.ModelToolCallResponse
	for _, message := range nextModel.requests[0].Messages {
		for _, part := range message.Parts {
			if reply, ok := part.(*gent.ModelToolCallResponse); ok {
				replies = append(replies, reply)
			}
		}
	}
	if !assert.Len(t, replies, 2) {
		return
	}
	assert.Equal(t, []gent.ContentPart{gent.TextPart("Recovered original policy."),
		gent.TextPart("Original guidance."),
		gent.TextPart(gent.DefaultSteeringMessages().RecoveryCompleted),
		gent.TextPart("Verified external receipt."),
		gent.TextPart("Check current policy before a new action.")}, replies[0].Parts())
	status, present := replies[1].RecoveryStatus()
	assert.True(t, present)
	assert.Equal(t, gent.ToolRecoveryNotStarted, status)
}

func TestToolRecoveryInvalidReturnsUseSafeGuidance(t *testing.T) {
	type input struct {
		response *gent.ToolRecoveryResponse
		err      error
	}
	type expected struct {
		kind  gent.ToolResponseKind
		parts []gent.ContentPart
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	want := expected{gent.ToolResponseRecoveryError,
		[]gent.ContentPart{gent.TextPart(gent.DefaultSteeringMessages().RecoveryError)}}
	cases := []testCase{
		{"nil nil", input{}, want},
		{"error", input{err: errors.New("private recovery details")}, want},
		{"invalid enum", input{response: &gent.ToolRecoveryResponse{Status: "retry"}}, want},
		{"missing execution", input{response: &gent.ToolRecoveryResponse{
			Status: gent.ToolRecoveryCompleted}}, want},
		{"extra execution", input{response: &gent.ToolRecoveryResponse{
			Status: gent.ToolRecoveryUnresolved, Execution: &gent.ToolExecutionResponse{
				Status: gent.ToolExecuted}}}, want},
		{"both returns", input{response: &gent.ToolRecoveryResponse{
			Status: gent.ToolRecoveryUnresolved, Instructions: "PRIVATE REJECTED GUIDANCE"},
			err: errors.New("private")}, want},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			saved, _, _ := saveInterruptedRun(t, gent.PhaseToolReady, "")
			var calls []gent.ToolIdentity
			model := &recoveryModel{answer: true}
			agent := newRecoveryAgent(model, &calls,
				func(context.Context, *gent.ToolRecoveryRequest[recoveryInput]) (
					*gent.ToolRecoveryResponse, error,
				) {
					return test.input.response, test.input.err
				})
			restored, _, err := resumeRecovery(agent, saved, recoveryBindings(t, agent))
			if !assert.NoError(t, err) {
				return
			}
			defer restored.Release()
			assert.Empty(t, calls)
			var got *gent.ModelToolCallResponse
			for _, message := range model.requests[0].Messages {
				for _, part := range message.Parts {
					if reply, ok := part.(*gent.ModelToolCallResponse); ok && reply.ToolCallID() == "policy-a" {
						got = reply
					}
				}
			}
			if !assert.NotNil(t, got) {
				return
			}
			assert.Equal(t, test.expected, expected{got.Kind(), got.Parts()})
		})
	}
}
