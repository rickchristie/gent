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
	"github.com/rickchristie/gent/toolchain"
)

type OrderInput struct {
	OrderID string `json:"order_id"`
}

type OrderStatus struct {
	OrderID string `json:"order_id"`
	Status  string `json:"status"`
}

const agentPrompt = `Help the user check an order.
Use lookup_order for order facts. Do not invent a status.`

func newOrderTool() gent.Tool[OrderInput, OrderStatus] {
	return gent.DefineTool("lookup_order", lookupOrder).
		Description("Get the current status of an order by its ID.").
		Parameters(schema.Field("order_id").
			Description("The order ID, for example order-1001.").MinLength(1)).
		MustBuild()
}

func lookupOrder(ctx context.Context, input OrderInput) (OrderStatus, error) {
	if err := ctx.Err(); err != nil {
		return OrderStatus{}, err
	}
	// Use a fixed record so this example needs no database. Replace it with your API.
	if input.OrderID != "order-1001" {
		return OrderStatus{}, fmt.Errorf("order not found: %s", input.OrderID)
	}
	return OrderStatus{OrderID: input.OrderID, Status: "shipped"}, nil
}

// Build the tool registry once, before you use the runner for questions.
func newRunner(model gent.Model) *executor.Runner[string] {
	chain := toolchain.New()
	chain.RegisterTool(newOrderTool())

	loop := react.NewAgent(model).
		WithToolChain(chain)
	return executor.NewRunner(loop, executor.DefaultConfig())
}

func ask(ctx context.Context, runner *executor.Runner[string], question string) (string, error) {
	// Each question gets its own history, counters, and cancellation state.
	data := gent.NewBasicLoopData(&gent.Task{Messages: []*gent.ModelMessage{
		gent.TextParts(gent.ChatMessageTypeSystem, agentPrompt),
		gent.TextParts(gent.ChatMessageTypeHuman, question),
	}})
	execCtx := gent.NewExecutionContext(ctx, "order-helper", data)
	limits := gent.DefaultLimits()
	for i := range limits {
		if limits[i].Key == gent.SCIterations.Self() {
			limits[i].MaxValue = 6
		}
	}
	execCtx.SetLimits(limits)

	answer, result, err := runner.Execute(execCtx)
	if err != nil {
		return "", err
	}
	if result.TerminationReason != gent.TerminationSuccess {
		return "", fmt.Errorf("run ended with %s", result.TerminationReason)
	}
	return answer, nil
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
