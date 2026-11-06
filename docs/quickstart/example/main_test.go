package main

import (
	"context"
	"errors"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

type scriptedModel struct {
	answer   string
	orderID  string
	requests []*gent.ModelCallRequest
}

func (m *scriptedModel) PrepareRequest(ctx *gent.ExecutionContext, request *gent.ModelCallRequest,
	chain gent.ToolChain,
) (*gent.ModelCallRequest, error) {
	return gent.PrepareModelRequest(ctx, request, chain)
}

func (m *scriptedModel) GenerateContentStream(ctx context.Context, request *gent.ModelCallRequest) (
	gent.Stream, error,
) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	first := len(m.requests)%2 == 0
	m.requests = append(m.requests, request)
	choice := &gent.ContentChoice{Completion: gent.ModelCompletion{Status: gent.ModelStatusAnswer},
		CandidateContent: []gent.ContentPart{gent.TextPart(m.answer)}}
	if first {
		choice = &gent.ContentChoice{Completion: gent.ModelCompletion{Status: gent.ModelStatusToolCalls},
			ToolCalls: []*gent.ModelToolCallRequest{{ID: "shipment-call", Type: "function",
				FunctionCall: &gent.FunctionCall{Name: "create_shipment",
					Arguments: `{"order_id":"` + m.orderID + `"}`}}}}
	}
	response := &gent.ModelCallResponse{Choices: []*gent.ContentChoice{choice}}
	return gent.NewCompletedStream(response, nil), nil
}

func TestShipmentQuickStart(t *testing.T) {
	type input struct{ order string }
	type expected struct {
		status gent.ToolStatus
		parts  []gent.ContentPart
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	created, err := gent.NewParts().Data(&Shipment{ID: "shipment-1001", OrderID: "order-1001",
		Status: ShipmentCreated}).PDF([]byte(sampleLabelPDF)).
		Text("Use the shipment status to determine whether it was created.").Build()
	assert.NoError(t, err)
	declined, err := gent.NewParts().Data(&Shipment{OrderID: "missing", Status: ShipmentDeclined}).
		Text("Use the shipment status to determine whether it was created.").Build()
	assert.NoError(t, err)
	cases := []testCase{
		{"created with PDF", input{"order-1001"}, expected{gent.ToolExecuted, created}},
		{"domain decline", input{"missing"}, expected{gent.ToolExecuted, declined}},
		{"approval", input{"approval-required"}, expected{gent.ToolBlocked, []gent.ContentPart{
			gent.TextPart("The requested action was blocked."),
			gent.TextPart("Obtain approval for request approval-1002 before requesting shipment.")}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			model := &scriptedModel{orderID: test.input.order, answer: "Reviewed the shipment response."}
			runner := newRunner(model, newDemoShipping())
			for run := 0; run < 2; run++ {
				answer, err := ask(context.Background(), runner, "Create the shipment and check the label.")
				assert.NoError(t, err)
				assert.Equal(t, model.answer, answer)
				requests := model.requests[run*2:]
				if !assert.Len(t, requests, 2) {
					return
				}
				assert.Len(t, requests[0].Messages, 2)
				definition := requests[0].Options.Tools[0].Function
				assert.Equal(t, "create_shipment", definition.Name)
				assert.Equal(t, NewShipmentTool(newDemoShipping()).OutputSchema(), definition.OutputSchema)
				reply := requests[1].Messages[3].Parts[0].(*gent.ModelToolCallResponse)
				status, present := reply.ExecutionStatus()
				assert.True(t, present)
				assert.Equal(t, test.expected.status, status)
				assert.Equal(t, test.expected.parts, reply.Parts())
				assert.Equal(t, "shipment-call", reply.ToolCallID())
			}
		})
	}
}

func TestShipmentRecovery(t *testing.T) {
	type input struct{ setup string }
	type expected struct {
		status   gent.ToolRecoveryStatus
		evidence []gent.ContentPart
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	evidence, err := gent.NewParts().Data(&RecoveryEvidence{OperationID: "call-1",
		State: "not_confirmed"}).Build()
	assert.NoError(t, err)
	cases := []testCase{
		{"saved response", input{"create"}, expected{gent.ToolRecoveryCompleted, nil}},
		{"positive cancellation evidence", input{"cancel"}, expected{gent.ToolRecoveryNotStarted, nil}},
		{"missing record is uncertain", input{}, expected{gent.ToolRecoveryUnresolved, evidence}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			service := newDemoShipping()
			in := &ShipmentInput{OrderID: "order-1001"}
			switch test.input.setup {
			case "create":
				_, err := service.Create(context.Background(), "call-1", in)
				assert.NoError(t, err)
			case "cancel":
				assert.NoError(t, service.CancelPlanned("call-1"))
			}
			response, err := NewShipmentTool(service).Recover(context.Background(),
				gent.NewToolRecoveryRequest(gent.ToolIdentity{ToolCallId: "call-1"}, "create_shipment",
					map[string]any{"order_id": "order-1001"}, in))
			assert.NoError(t, err)
			assert.Equal(t, test.expected.status, response.Status)
			assert.Equal(t, test.expected.evidence, response.Parts)
			if test.expected.status == gent.ToolRecoveryCompleted {
				assert.Equal(t, gent.PDFPart([]byte(sampleLabelPDF)), response.Execution.Parts[1])
			} else {
				assert.Nil(t, response.Execution)
			}
		})
	}
}

type unavailableShipping struct{ ShippingService }

func (*unavailableShipping) Create(context.Context, string, *ShipmentInput) (*ShipmentReply,
	error) {
	return nil, errors.New("private database connection string")
}
func TestShipmentErrorPrivacyAndCancellation(t *testing.T) {
	model := &scriptedModel{orderID: "order-1001", answer: "Check the existing shipment first."}
	runner := newRunner(model, &unavailableShipping{})
	_, err := ask(context.Background(), runner, "Ship this order.")
	assert.NoError(t, err)
	reply := model.requests[1].Messages[3].Parts[0].(*gent.ModelToolCallResponse)
	assert.Equal(t, gent.ToolResponseCallError, reply.Kind())
	assert.Equal(t, []gent.ContentPart{gent.TextPart("The shipment outcome could not be confirmed. " +
		"Check existing shipments before requesting another.")}, reply.Parts())
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	answer, err := ask(cancelled, runner, "Ship this order.")
	assert.Equal(t, "", answer)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Len(t, model.requests, 2)
}
