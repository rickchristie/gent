package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents/react"
	"github.com/rickchristie/gent/schema"
)

type OrderInput struct {
	OrderID string `json:"order_id"`
}

func newAgent(model gent.Model) *react.Agent {
	tool := gent.NewToolFunc("lookup_order", "Read an order status.",
		schema.Object(map[string]*schema.Property{
			"order_id": schema.String("The order ID, such as order-1001."),
		}, "order_id"), func(ctx context.Context, input OrderInput) (string, error) {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			if input.OrderID != "order-1001" {
				return "", fmt.Errorf("order not found: %s", input.OrderID)
			}
			return "Order order-1001 has shipped.", nil
		})
	return react.NewAgent(model).RegisterTool(tool).
		WithRecoveryVersion("order-streaming/v1").
		WithBehaviorAndContext("Help the user check an order.").
		WithCriticalRules("Use lookup_order for order facts. Do not invent a status.").
		WithCallOptions(gent.WithMaxTokens(1024))
}

// The demo provider makes the complete example usable without credentials or API calls.
type DemoModel struct{ Delay time.Duration }

func (m *DemoModel) GenerateContentStream(
	parent context.Context, request gent.ModelCallRequest,
) (gent.Stream, error) {
	for _, message := range request.Messages {
		if message.Role == gent.ChatMessageTypeTool {
			return m.answer(parent)
		}
	}
	return gent.NewCompletedStream(&gent.ContentResponse{
		Choices: []*gent.ContentChoice{{ToolCalls: []gent.ModelToolCall{{
			ID: "order-read", Type: "function", FunctionCall: &gent.FunctionCall{
				Name: "lookup_order", Arguments: `{"order_id":"order-1001"}`,
			},
		}}}},
	}, nil), nil
}

func (m *DemoModel) answer(parent context.Context) (gent.Stream, error) {
	ctx, cancel := context.WithCancel(parent)
	stream, err := gent.NewBoundedStreamWithDuration(ctx, 16, cancel)
	if err != nil {
		cancel()
		return nil, err
	}
	go func() {
		defer cancel()
		answer := "Order order-1001 has shipped."
		for _, word := range strings.Split(answer, " ") {
			timer := time.NewTimer(m.Delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				stream.Complete(nil, ctx.Err())
				return
			case <-timer.C:
			}
			if !stream.SendMeta(&gent.MetaContent{
				Type: gent.MetaContentIntermediary, MessageId: "answer", Text: word + " ",
			}) {
				stream.Complete(nil, context.Canceled)
				return
			}
		}
		stream.Complete(&gent.ContentResponse{Choices: []*gent.ContentChoice{{
			CandidateContent: answer,
			// The accepted answer must name the same local message as its preview chunks.
			CandidateSource: &gent.ContentSource{MessageIds: []string{"answer"}},
		}}}, nil)
	}()
	return stream, nil
}

// Only the named read-only tool can be retried without an external receipt.
func recoverTool(_ context.Context, request *gent.ToolRecoveryRequest) (
	*gent.ToolRecoveryResult, error,
) {
	status := gent.ToolRecoveryUnresolved
	if request.Name == "lookup_order" {
		status = gent.ToolRecoveryRetryAllowed
	}
	return &gent.ToolRecoveryResult{Status: status}, nil
}
