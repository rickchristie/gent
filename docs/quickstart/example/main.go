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
	"github.com/rickchristie/gent/toolchain"
)

type ShipmentInput struct {
	OrderID string `json:"order_id"`
}

type ShipmentStatus string

const (
	ShipmentNeedsApproval ShipmentStatus = "needs_approval"
	ShipmentCreated       ShipmentStatus = "created"
	ShipmentDeclined      ShipmentStatus = "declined"
)

type Shipment struct {
	ID      string         `json:"id,omitempty"`
	OrderID string         `json:"order_id"`
	Status  ShipmentStatus `json:"status"`
}

type ShipmentReply struct {
	Shipment          *Shipment
	LabelPDF          []byte
	ApprovalRequestID string
}

type CheckStatus string

const (
	CheckCompleted  CheckStatus = "completed"
	CheckNotStarted CheckStatus = "not_started"
	CheckUnresolved CheckStatus = "unresolved"
)

type ShipmentCheck struct {
	Status        CheckStatus
	Reply         *ShipmentReply
	OperationID   string
	ProviderState string
}

type RecoveryEvidence struct {
	OperationID string `json:"operation_id"`
	State       string `json:"state"`
}

type ShippingService interface {
	Create(
		context.Context,
		string, // Stable tool call ID.
		*ShipmentInput,
	) (*ShipmentReply, error)

	Inspect(
		context.Context,
		string, // Original tool call ID.
		string, // Original order ID.
	) (*ShipmentCheck, error)
}

type ShipmentTool struct {
	service ShippingService
}

func NewShipmentTool(service ShippingService) gent.Tool[*ShipmentInput] {
	tool := &ShipmentTool{service: service}

	return gent.DefineTool("create_shipment", tool.Call).
		Description("Create a shipment for an approved order and return its label.").
		Domain("shipping").
		Categories("orders", "fulfillment").
		Keywords("ship", "dispatch", "shipping label").
		SyntheticQueries(
			"Ship this order.",
			"Create a shipping label for an approved order.",
		).
		Policy("Require approval before dispatching a shipment.").
		Strict(true).
		ParameterSchema(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"order_id": map[string]any{
					"type":        "string",
					"description": "The order to ship.",
					"minLength":   1,
				},
			},
			"required":             []string{"order_id"},
			"additionalProperties": false,
		}).
		OutputSchemaFrom(Shipment{}).
		UnrecoverableErrorInstructions(
			"The shipment outcome could not be confirmed. " +
				"Check existing shipments before requesting another.",
		).
		Recovery(tool.Recover).
		MustBuild()
}

func (t *ShipmentTool) Call(
	ctx context.Context,
	input *ShipmentInput,
) (*gent.ToolExecutionResponse, error) {
	identity, ok := gent.ToolIdentityFromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("missing tool call identity")
	}

	reply, err := t.service.Create(ctx, identity.ToolCallId, input)
	if err != nil {
		return nil, err
	}

	return t.result(reply)
}

func (t *ShipmentTool) result(
	reply *ShipmentReply,
) (*gent.ToolExecutionResponse, error) {
	switch reply.Shipment.Status {
	case ShipmentNeedsApproval:
		return &gent.ToolExecutionResponse{
			Status: gent.ToolBlocked,
			Instructions: fmt.Sprintf(
				"Obtain approval for request %s before requesting shipment.",
				reply.ApprovalRequestID,
			),
		}, nil

	case ShipmentCreated, ShipmentDeclined:
		// Both are domain responses. The payload states the outcome.

	default:
		return nil, fmt.Errorf(
			"unexpected shipment status %q",
			reply.Shipment.Status,
		)
	}

	builder := gent.NewParts().Data(reply.Shipment)
	if len(reply.LabelPDF) > 0 {
		builder.PDF(reply.LabelPDF)
	}

	parts, err := builder.Build()
	if err != nil {
		return nil, err
	}

	return &gent.ToolExecutionResponse{
		Status:       gent.ToolExecuted,
		Parts:        parts,
		Instructions: "Use the shipment status to determine whether it was created.",
	}, nil
}

func (t *ShipmentTool) Recover(
	ctx context.Context,
	request *gent.ToolRecoveryRequest[*ShipmentInput],
) (*gent.ToolRecoveryResponse, error) {
	check, err := t.service.Inspect(
		ctx,
		request.Identity().ToolCallId,
		request.Input().OrderID,
	)
	if err != nil {
		return nil, err
	}

	switch check.Status {
	case CheckCompleted:
		output, err := t.result(check.Reply)
		if err != nil {
			return nil, err
		}

		return &gent.ToolRecoveryResponse{
			Status:    gent.ToolRecoveryCompleted,
			Execution: output,
			Instructions: "This is the recorded response. " +
				"Check the current shipment state before changing the order.",
		}, nil

	case CheckNotStarted:
		return &gent.ToolRecoveryResponse{
			Status: gent.ToolRecoveryNotStarted,
			Instructions: "Review the current order and approval " +
				"before choosing the next action.",
		}, nil

	case CheckUnresolved:
		parts, err := gent.NewParts().
			Data(&RecoveryEvidence{
				OperationID: check.OperationID,
				State:       check.ProviderState,
			}).
			Build()
		if err != nil {
			return nil, err
		}

		return &gent.ToolRecoveryResponse{
			Status:       gent.ToolRecoveryUnresolved,
			Parts:        parts,
			Instructions: "Check this operation before requesting another shipment.",
		}, nil

	default:
		return nil, fmt.Errorf(
			"unexpected shipment recovery status %q",
			check.Status,
		)
	}
}

const agentPrompt = `Help the user create shipments.
Use create_shipment for shipment facts. Never claim that a blocked action was executed.
Use the domain status to tell whether a shipment was created or declined.
Ask the user for approval when required. Check current state before repeating an uncertain action.`

// Build the tool registry once, before you use the runner for questions.
func newRunner(model gent.Model, service ShippingService) *executor.Runner[string] {
	chain := toolchain.New()
	chain.RegisterTool(NewShipmentTool(service))

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
	execCtx := gent.NewExecutionContext(ctx, "shipping-helper", data)
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
	runner := newRunner(model, newDemoShipping())

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	answer, err := ask(ctx, runner, "Create a shipment for order-1001 and check its label.")
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
