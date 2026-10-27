package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents/react"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/models"
	"github.com/rickchristie/gent/schema"
	"github.com/rickchristie/gent/termination"
	"github.com/rickchristie/gent/toolchain"
)

type OrderInput struct {
	OrderID string `json:"order_id"`
}

type OrderStatus struct {
	OrderID string `json:"order_id"`
	Status  string `json:"status"`
}

func newOrderTool() *gent.ToolFunc[OrderInput, OrderStatus] {
	return gent.NewToolFunc(
		"lookup_order",
		"Get the current status of an order by its ID.",
		schema.Object(map[string]*schema.Property{
			"order_id": schema.String("The order ID, for example order-1001."),
		}, "order_id"),
		func(ctx context.Context, input OrderInput) (OrderStatus, error) {
			if err := ctx.Err(); err != nil {
				return OrderStatus{}, err
			}
			// Use a fixed record so this example needs no database. Replace it with your API.
			if input.OrderID != "order-1001" {
				return OrderStatus{}, fmt.Errorf("order not found: %s", input.OrderID)
			}
			return OrderStatus{OrderID: input.OrderID, Status: "shipped"}, nil
		},
	)
}

// Build the tool registry once, before you use the runner for questions.
func newRunner(model gent.Model) *executor.Runner {
	chain := toolchain.New()
	chain.RegisterTool(newOrderTool())

	term := termination.NewText("answer").WithGuidance(
		"Give a short answer with the order ID and its status.",
	)
	loop := react.NewAgent(model).
		WithToolChain(chain).
		WithTermination(term).
		WithBehaviorAndContext("Help the user check an order.").
		WithCriticalRules("Use lookup_order for order facts. Do not invent a status.")

	return executor.NewRunner(loop, executor.DefaultConfig())
}

func ask(ctx context.Context, runner *executor.Runner, question string) (string, error) {
	// Each question gets its own history, counters, and cancellation state.
	data := gent.NewBasicLoopData(&gent.Task{Text: question})
	execCtx := gent.NewExecutionContext(ctx, "order-helper", data)
	limits := gent.DefaultLimits()
	for i := range limits {
		if limits[i].Key == gent.SCIterations.Self() {
			limits[i].MaxValue = 6
		}
	}
	execCtx.SetLimits(limits)

	result, err := runner.Execute(execCtx)
	if err != nil {
		return "", err
	}
	if result.TerminationReason != gent.TerminationSuccess {
		return "", fmt.Errorf("run ended with %s", result.TerminationReason)
	}
	if len(result.Output) != 1 {
		return "", fmt.Errorf("expected one text answer, got %d parts", len(result.Output))
	}
	answer, ok := result.Output[0].(gent.TextContent)
	if !ok {
		return "", fmt.Errorf("expected text, got %T", result.Output[0])
	}
	return answer.Text, nil
}

func run() error {
	key, modelName := os.Getenv("OPENAI_API_KEY"), os.Getenv("GENT_MODEL")
	if key == "" || modelName == "" {
		return fmt.Errorf("set OPENAI_API_KEY and GENT_MODEL first")
	}
	model := models.NewOpenAI(key, modelName)
	runner := newRunner(model)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	answer, err := ask(ctx, runner, "What is the status of order-1001?")
	if err != nil {
		return err
	}
	fmt.Println(answer)
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
