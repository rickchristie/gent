package main

import (
	"context"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

type scriptedModel struct {
	answer   string
	orderID  string
	requests []gent.ModelCallRequest
}

func (m *scriptedModel) GenerateContentStream(
	ctx context.Context, request gent.ModelCallRequest,
) (gent.Stream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	index := len(m.requests)
	m.requests = append(m.requests, request)
	choice := &gent.ContentChoice{CandidateContent: m.answer}
	if index%2 == 0 {
		choice = &gent.ContentChoice{ToolCalls: []gent.ModelToolCall{{
			ID: "lookup-1", Type: "function", FunctionCall: &gent.FunctionCall{
				Name: "lookup_order", Arguments: `{"order_id":"` + m.orderID + `"}`,
			},
		}}}
	}
	return gent.NewCompletedStream(&gent.ContentResponse{
		Choices: []*gent.ContentChoice{choice},
	}, nil), nil
}

func TestQuickStart(t *testing.T) {
	type input struct {
		question string
	}
	type mocks struct {
		orderID string
		answer  string
	}
	type expected struct {
		answer string
		err    error
		result gent.ToolCallResponse
	}
	type testCase struct {
		name     string
		input    input
		mocks    mocks
		expected expected
	}
	cases := []testCase{
		{
			name:  "look up a known order",
			input: input{"What is the status of order-1001?"},
			mocks: mocks{"order-1001", "Order order-1001 has shipped."},
			expected: expected{
				answer: "Order order-1001 has shipped.",
				result: gent.ToolCallResponse{
					ToolCallID: "lookup-1", Name: "lookup_order",
					Content: `<lookup_order>
{"order_id":"order-1001","status":"shipped"}
</lookup_order>`,
				},
			},
		},
		{
			name:  "show a tool error to the model",
			input: input{"What is the status of missing-order?"},
			mocks: mocks{"missing-order", "Order missing-order was not found."},
			expected: expected{
				answer: "Order missing-order was not found.",
				result: gent.ToolCallResponse{
					ToolCallID: "lookup-1", Name: "lookup_order",
					Content: `<lookup_order>
Error: order not found: missing-order
</lookup_order>`,
				},
			},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			model := &scriptedModel{orderID: test.mocks.orderID, answer: test.mocks.answer}
			runner := newRunner(model)
			// A second question proves that the runner does not reuse the first run's history.
			for run := 0; run < 2; run++ {
				answer, err := ask(context.Background(), runner, test.input.question)
				assert.Equal(t, test.expected.answer, answer)
				assert.Equal(t, test.expected.err, err)
				requests := model.requests[run*2:]
				if !assert.Len(t, requests, 2) {
					return
				}
				assert.Len(t, requests[0].Messages, 2)
				assert.Equal(t, gent.TextParts(gent.ChatMessageTypeHuman, test.input.question),
					requests[0].Messages[1])
				if assert.Len(t, requests[0].Options.Tools, 1) {
					assert.Equal(t, "lookup_order", requests[0].Options.Tools[0].Function.Name)
				}
				last := requests[1].Messages[len(requests[1].Messages)-1]
				assert.Equal(t, gent.ModelMessage{Role: gent.ChatMessageTypeTool,
					Parts: []gent.ContentPart{test.expected.result}}, last)
			}
		})
	}
}

func TestQuickStartCancellation(t *testing.T) {
	model := &scriptedModel{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	answer, err := ask(ctx, newRunner(model), "What is the status of order-1001?")
	assert.Equal(t, "", answer)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, model.requests)
}
