package ecommerce

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/policy"
	"github.com/rickchristie/gent/schema"
	"github.com/rickchristie/gent/search"
)

// -------------------------------------------------------------------------
// Data Types
// -------------------------------------------------------------------------

// Customer represents an e-commerce customer.
type Customer struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Phone string `json:"phone"`
}

// Order represents a customer order.
type Order struct {
	OrderID    string  `json:"order_id"`
	CustomerID string  `json:"customer_id"`
	Product    string  `json:"product_name"`
	Amount     float64 `json:"amount"`
	Status     string  `json:"status"`
	OrderDate  string  `json:"order_date"`
}

// OrderPage represents a paginated page of orders.
type OrderPage struct {
	Orders     []*Order `json:"orders"`
	NextCursor string   `json:"next_cursor"`
	HasMore    bool     `json:"has_more"`
}

// Payment represents a payment for an order.
type Payment struct {
	PaymentID   string  `json:"payment_id"`
	OrderID     string  `json:"order_id"`
	Amount      float64 `json:"amount"`
	Status      string  `json:"status"`
	GatewayTxID string  `json:"gateway_tx_id"`
	PaymentDate string  `json:"payment_date"`
}

// GatewayTx represents a payment gateway transaction.
type GatewayTx struct {
	TxID      string  `json:"tx_id"`
	Amount    float64 `json:"amount"`
	Status    string  `json:"status"`
	CardLast4 string  `json:"card_last4"`
}

// -------------------------------------------------------------------------
// Tool Result Types
// -------------------------------------------------------------------------

// OrderPaymentsResult is the result of looking up payments
// for an order.
type OrderPaymentsResult struct {
	Payments []*Payment `json:"payments"`
	Note     string     `json:"note"`
}

// GatewayCancelResult is the result of attempting to cancel
// a gateway transaction.
type GatewayCancelResult struct {
	Success bool   `json:"success"`
	Reason  string `json:"reason"`
}

// RefundResult is the result of attempting to process a refund.
type RefundResult struct {
	Success bool   `json:"success"`
	Reason  string `json:"reason"`
}

// CaseResult is the result of creating a support case.
type CaseResult struct {
	CaseID  string `json:"case_id"`
	OrderID string `json:"order_id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// CreditRequestResult is the result of creating a credit request.
type CreditRequestResult struct {
	RequestID string  `json:"request_id"`
	CaseID    string  `json:"case_id"`
	Amount    float64 `json:"amount"`
	Status    string  `json:"status"`
	Message   string  `json:"message"`
}

// -------------------------------------------------------------------------
// Tool Input Types
// -------------------------------------------------------------------------

type getCustomerInfoInput struct {
	Email string `json:"email"`
}

type getOrdersInput struct {
	CustomerID string `json:"customer_id"`
	Cursor     string `json:"cursor"`
}

type getOrderPaymentsInput struct {
	OrderID string `json:"order_id"`
}

type gatewayGetTxDetailInput struct {
	TxID string `json:"tx_id"`
}

type gatewayCancelTxInput struct {
	TxID string `json:"tx_id"`
}

type processRefundInput struct {
	PaymentID string `json:"payment_id"`
}

type createCaseInput struct {
	OrderID string `json:"order_id"`
	Details string `json:"details"`
}

type createCreditRequestInput struct {
	CaseID string  `json:"case_id"`
	Amount float64 `json:"amount"`
}

// -------------------------------------------------------------------------
// EcommerceFixture
// -------------------------------------------------------------------------

// EcommerceFixture provides a complete e-commerce mock environment.
type EcommerceFixture struct {
	timeProvider gent.TimeProvider
	policySearch *policy.PolicySearchTool
	policyGet    *policy.GetPolicyTool

	customers  map[string]*Customer
	orderPages map[string]*OrderPage // keyed by cursor
	payments   map[string]*OrderPaymentsResult
	gatewayTxs map[string]*GatewayTx
}

// NewEcommerceFixture creates a new EcommerceFixture.
// If tp is nil, uses gent.DefaultTimeProvider. The embedder is required for
// PolicySearchTool (hybrid BM25+semantic policy search).
func NewEcommerceFixture(
	tp gent.TimeProvider, embedder search.Embedder,
) *EcommerceFixture {
	if tp == nil {
		tp = gent.NewDefaultTimeProvider()
	}

	f := &EcommerceFixture{
		timeProvider: tp,
		customers:    make(map[string]*Customer),
		orderPages:   make(map[string]*OrderPage),
		payments:     make(map[string]*OrderPaymentsResult),
		gatewayTxs:   make(map[string]*GatewayTx),
	}
	f.initializeData()

	policies := ecommercePolicies()
	searchTool, err := policy.NewPolicySearchTool(
		context.Background(), embedder, policies,
	)
	if err != nil {
		panic("ecommerce: failed to create PolicySearchTool: " +
			err.Error())
	}
	f.policySearch = searchTool.
		WithName("search_policy").
		WithDescription(
			"Search policy by describing what you need " +
				"(e.g., 'double charge', 'refund process')").
		WithResultMode(policy.SearchResultTitle)
	f.policyGet = policy.NewGetPolicyTool(policies).
		WithName("get_policy").
		WithDescription("Get full policy content by ID")
	return f
}

// PolicySuggestionPrompt searches policies relevant to the conversation
// text and returns a formatted prompt section. Returns empty string if
// no policies match.
func (f *EcommerceFixture) PolicySuggestionPrompt(
	ctx context.Context, text string,
) string {
	suggestions, err := f.policySearch.SuggestPolicies(
		ctx, text, 3,
	)
	if err != nil || len(suggestions) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("Policies that might be relevant:\n")
	for _, s := range suggestions {
		fmt.Fprintf(&sb, "- id: %s - %s\n", s.Id, s.Description)
	}
	sb.WriteString(
		"Use get_policy tool for relevant policies. " +
			"Use search_policy tool to find relevant " +
			"policy if suggested policies are not relevant.")
	return sb.String()
}

// TimeProvider returns the fixture's time provider.
func (f *EcommerceFixture) TimeProvider() gent.TimeProvider {
	return f.timeProvider
}

func (f *EcommerceFixture) initializeData() {
	now := f.timeProvider.Now()
	today := time.Date(
		now.Year(), now.Month(), now.Day(),
		0, 0, 0, 0, time.UTC,
	)

	fmtDate := func(t time.Time) string {
		return t.Format("2006-01-02")
	}

	// Customer
	f.customers["C001"] = &Customer{
		ID:    "C001",
		Name:  "Alex Rivera",
		Email: "alex.rivera@email.com",
		Phone: "+1-555-0321",
	}

	// Orders — 9 total, 3 pages of 3
	page1 := &OrderPage{
		Orders: []*Order{
			{
				OrderID: "ORD-1001", CustomerID: "C001",
				Product: "USB-C Hub", Amount: 45.99,
				Status:    "processing",
				OrderDate: fmtDate(today),
			},
			{
				OrderID: "ORD-1002", CustomerID: "C001",
				Product: "Laptop Stand", Amount: 89.99,
				Status:    "shipped",
				OrderDate: fmtDate(today.AddDate(0, 0, -1)),
			},
			{
				OrderID: "ORD-1003", CustomerID: "C001",
				Product: "Mechanical Keyboard",
				Amount:  149.99, Status: "delivered",
				OrderDate: fmtDate(today.AddDate(0, 0, -2)),
			},
		},
		NextCursor: "cur_p2",
		HasMore:    true,
	}

	page2 := &OrderPage{
		Orders: []*Order{
			{
				OrderID: "ORD-1004", CustomerID: "C001",
				Product: "Monitor Light Bar", Amount: 39.99,
				Status:    "delivered",
				OrderDate: fmtDate(today.AddDate(0, 0, -3)),
			},
			{
				OrderID: "ORD-1005", CustomerID: "C001",
				Product: "Desk Mat XL", Amount: 29.99,
				Status:    "delivered",
				OrderDate: fmtDate(today.AddDate(0, 0, -5)),
			},
			{
				OrderID: "ORD-1006", CustomerID: "C001",
				Product: "Webcam HD Pro", Amount: 79.99,
				Status:    "delivered",
				OrderDate: fmtDate(today.AddDate(0, 0, -7)),
			},
		},
		NextCursor: "cur_p3",
		HasMore:    true,
	}

	page3 := &OrderPage{
		Orders: []*Order{
			{
				OrderID: "ORD-1007", CustomerID: "C001",
				Product: "Mighty Mouse Pro Wireless Mouse",
				Amount:  79.99, Status: "delivered",
				OrderDate: fmtDate(today.AddDate(0, 0, -10)),
			},
			{
				OrderID: "ORD-1008", CustomerID: "C001",
				Product: "Ergonomic Mouse Pad",
				Amount:  19.99, Status: "delivered",
				OrderDate: fmtDate(today.AddDate(0, 0, -14)),
			},
			{
				OrderID: "ORD-1009", CustomerID: "C001",
				Product: "Cable Management Kit",
				Amount:  12.99, Status: "delivered",
				OrderDate: fmtDate(today.AddDate(0, 0, -21)),
			},
		},
		NextCursor: "",
		HasMore:    false,
	}

	f.orderPages[""] = page1
	f.orderPages["cur_p2"] = page2
	f.orderPages["cur_p3"] = page3

	// Payments for ORD-1007
	tenDaysAgo := fmtDate(today.AddDate(0, 0, -10))
	f.payments["ORD-1007"] = &OrderPaymentsResult{
		Payments: []*Payment{
			{
				PaymentID:   "PAY-2001",
				OrderID:     "ORD-1007",
				Amount:      79.99,
				Status:      "failed",
				GatewayTxID: "GW-TX-5001",
				PaymentDate: tenDaysAgo,
			},
			{
				PaymentID:   "PAY-2002",
				OrderID:     "ORD-1007",
				Amount:      79.99,
				Status:      "successful",
				GatewayTxID: "GW-TX-5002",
				PaymentDate: tenDaysAgo,
			},
		},
		Note: "Our internal payment status may not " +
			"always reflect the latest state from " +
			"the payment gateway. For accurate, " +
			"real-time payment status, use the " +
			"gateway_get_tx_detail tool to verify " +
			"each transaction's current state with " +
			"the payment processor.",
	}

	// Gateway transactions
	f.gatewayTxs["GW-TX-5001"] = &GatewayTx{
		TxID: "GW-TX-5001", Amount: 79.99,
		Status: "SETTLED", CardLast4: "4242",
	}
	f.gatewayTxs["GW-TX-5002"] = &GatewayTx{
		TxID: "GW-TX-5002", Amount: 79.99,
		Status: "SETTLED", CardLast4: "4242",
	}

}

// -------------------------------------------------------------------------
// Tool Methods
// -------------------------------------------------------------------------

func (f *EcommerceFixture) getCustomerInfoTool() gent.Tool[
	getCustomerInfoInput, *Customer,
] {
	return gent.DefineTool("get_customer_info", func(
		ctx context.Context,
		input getCustomerInfoInput,
	) (*Customer, error) {
		for _, c := range f.customers {
			if c.Email == input.Email {
				return c, nil
			}
		}
		return nil, fmt.Errorf(
			"customer not found with email: %s",
			input.Email,
		)
	}).Description("Retrieve customer information by email address").
		ParameterSchema(schema.Object(map[string]*schema.Property{
			"email": schema.String(
				"The customer's email address",
			),
		}, "email")).Domain("Customer").Categories("lookup", "customer").Keywords(
		"customer", "profile", "email",
		"account",
	).SyntheticQueries(
		"find customer by email",
		"look up customer information",
	).MustBuild()
}

func (f *EcommerceFixture) getOrdersTool() gent.Tool[
	getOrdersInput, *OrderPage,
] {
	return gent.DefineTool("get_orders", func(
		ctx context.Context,
		input getOrdersInput,
	) (*OrderPage, error) {
		if input.CustomerID == "" {
			return nil, fmt.Errorf(
				"customer_id is required",
			)
		}
		if _, exists := f.customers[input.CustomerID]; !exists {
			return nil, fmt.Errorf(
				"customer not found: %s",
				input.CustomerID,
			)
		}
		page, exists := f.orderPages[input.Cursor]
		if !exists {
			return nil, fmt.Errorf(
				"invalid cursor: %s", input.Cursor,
			)
		}
		return page, nil
	}).Description("Retrieve customer orders with cursor-based "+
		"pagination. Pass an empty cursor for the "+
		"first page. Use the next_cursor from the "+
		"response to fetch subsequent pages.").ParameterSchema(schema.Object(map[string]*schema.Property{
		"customer_id": schema.String(
			"The customer's unique ID",
		),
		"cursor": schema.String(
			"Pagination cursor. Empty string for " +
				"the first page.",
		),
	}, "customer_id")).Domain("Orders").Categories("lookup", "orders").Keywords(
		"order", "purchase", "history",
		"cursor", "pagination",
	).SyntheticQueries(
		"get customer orders",
		"list order history",
		"look up purchases",
	).MustBuild()
}

func (f *EcommerceFixture) getOrderPaymentsTool() gent.Tool[
	getOrderPaymentsInput, *OrderPaymentsResult,
] {
	return gent.DefineTool("get_order_payments", func(
		ctx context.Context,
		input getOrderPaymentsInput,
	) (*OrderPaymentsResult, error) {
		if input.OrderID == "" {
			return nil, fmt.Errorf(
				"order_id is required",
			)
		}
		result, exists := f.payments[input.OrderID]
		if !exists {
			return nil, fmt.Errorf(
				"no payments found for order: %s",
				input.OrderID,
			)
		}
		return result, nil
	}).Description("Retrieve all payment records for a specific order").
		ParameterSchema(schema.Object(map[string]*schema.Property{
			"order_id": schema.String("The order ID"),
		}, "order_id")).Domain("Payments").Categories("lookup", "payment").Keywords(
		"payment", "charge", "transaction",
		"billing",
	).SyntheticQueries(
		"get payments for an order",
		"check payment status",
		"look up charges on order",
	).MustBuild()
}

func (f *EcommerceFixture) gatewayGetTxDetailTool() gent.Tool[
	gatewayGetTxDetailInput, *GatewayTx,
] {
	return gent.DefineTool("gateway_get_tx_detail", func(
		ctx context.Context,
		input gatewayGetTxDetailInput,
	) (*GatewayTx, error) {
		if input.TxID == "" {
			return nil, fmt.Errorf(
				"tx_id is required",
			)
		}
		tx, exists := f.gatewayTxs[input.TxID]
		if !exists {
			return nil, fmt.Errorf(
				"transaction not found: %s",
				input.TxID,
			)
		}
		return tx, nil
	}).Description("Get real-time transaction details from the "+
		"payment gateway. Use this to verify the "+
		"actual status of a payment transaction.").
		ParameterSchema(schema.Object(map[string]*schema.Property{
			"tx_id": schema.String(
				"The gateway transaction ID",
			),
		}, "tx_id")).Domain("Payments").Categories("lookup", "gateway").Keywords(
		"gateway", "transaction", "real-time",
		"verify", "status",
	).SyntheticQueries(
		"verify transaction with gateway",
		"check real-time payment status",
		"get gateway transaction details",
	).MustBuild()
}

func (f *EcommerceFixture) gatewayCancelTxTool() gent.Tool[
	gatewayCancelTxInput, *GatewayCancelResult,
] {
	return gent.DefineTool("gateway_cancel_tx", func(
		ctx context.Context,
		input gatewayCancelTxInput,
	) (*GatewayCancelResult, error) {
		if input.TxID == "" {
			return nil, fmt.Errorf(
				"tx_id is required",
			)
		}
		if _, exists := f.gatewayTxs[input.TxID]; !exists {
			return nil, fmt.Errorf(
				"transaction not found: %s",
				input.TxID,
			)
		}
		return &GatewayCancelResult{
			Success: false,
			Reason: "Transaction has already " +
				"settled and cannot be cancelled",
		}, nil
	}).Description("Attempt to cancel a payment gateway "+
		"transaction. Only works for transactions "+
		"that have not yet settled.").ParameterSchema(schema.Object(map[string]*schema.Property{
		"tx_id": schema.String(
			"The gateway transaction ID to cancel",
		),
	}, "tx_id")).Domain("Payments").Categories("mutation", "gateway").Keywords(
		"cancel", "void", "transaction",
		"gateway",
	).SyntheticQueries(
		"cancel a payment transaction",
		"void gateway transaction",
	).MustBuild()
}

func (f *EcommerceFixture) processRefundTool() gent.Tool[
	processRefundInput, *RefundResult,
] {
	return gent.DefineTool("process_refund", func(
		ctx context.Context,
		input processRefundInput,
	) (*RefundResult, error) {
		if input.PaymentID == "" {
			return nil, fmt.Errorf(
				"payment_id is required",
			)
		}
		if input.PaymentID == "PAY-2001" {
			return &RefundResult{
				Success: false,
				Reason: "Refund rejected by bank: " +
					"card ending in 4242 is no " +
					"longer active. Please use " +
					"an alternative resolution " +
					"method.",
			}, nil
		}
		return nil, fmt.Errorf(
			"payment not found: %s",
			input.PaymentID,
		)
	}).Description("Attempt to process a refund for a payment. "+
		"Refund goes back to the original payment "+
		"method.").ParameterSchema(schema.Object(map[string]*schema.Property{
		"payment_id": schema.String(
			"The payment ID to refund",
		),
	}, "payment_id")).Domain("Payments").Categories("mutation", "refund").
		Keywords(
			"refund", "return", "reimburse",
			"credit",
		).SyntheticQueries(
		"process a refund",
		"refund a payment",
		"return money to customer",
	).MustBuild()
}

func (f *EcommerceFixture) createCaseTool() gent.Tool[
	createCaseInput, *CaseResult,
] {
	return gent.DefineTool("create_case", func(
		ctx context.Context,
		input createCaseInput,
	) (*CaseResult, error) {
		if input.OrderID == "" || input.Details == "" {
			return nil, fmt.Errorf(
				"order_id and details are required",
			)
		}
		return &CaseResult{
			CaseID:  "CASE-3001",
			OrderID: input.OrderID,
			Status:  "open",
			Message: "Support case created " +
				"successfully. A billing " +
				"specialist will review " +
				"within 24 hours.",
		}, nil
	}).Description("Create a support case for issues that "+
		"cannot be resolved automatically").ParameterSchema(schema.Object(map[string]*schema.Property{
		"order_id": schema.String(
			"The order ID related to the case",
		),
		"details": schema.String(
			"Description of the issue and " +
				"resolution steps attempted",
		),
	}, "order_id", "details")).Domain("Support").Categories("mutation", "support").
		Keywords(
			"case", "ticket", "escalate",
			"support", "issue",
		).SyntheticQueries(
		"create a support case",
		"escalate issue to support",
		"open a ticket",
	).MustBuild()
}

func (f *EcommerceFixture) createCreditRequestTool() gent.Tool[
	createCreditRequestInput, *CreditRequestResult,
] {
	return gent.DefineTool("create_credit_request", func(
		ctx context.Context,
		input createCreditRequestInput,
	) (*CreditRequestResult, error) {
		if input.CaseID == "" {
			return nil, fmt.Errorf(
				"case_id is required",
			)
		}
		if input.Amount <= 0 {
			return nil, fmt.Errorf(
				"amount must be positive",
			)
		}
		return &CreditRequestResult{
			RequestID: "CR-4001",
			CaseID:    input.CaseID,
			Amount:    input.Amount,
			Status:    "approved",
			Message: fmt.Sprintf(
				"Store credit of $%.2f has been "+
					"approved and applied to "+
					"the customer's account. "+
					"The credit is available "+
					"immediately for use on "+
					"any future purchase.",
				input.Amount,
			),
		}, nil
	}).Description("Create a store credit request associated "+
		"with a support case. Credit is applied "+
		"immediately if approved.").ParameterSchema(schema.Object(map[string]*schema.Property{
		"case_id": schema.String(
			"The support case ID",
		),
		"amount": schema.Number(
			"The credit amount in dollars",
		),
	}, "case_id", "amount")).Domain("Support").Categories("mutation", "credit").
		Keywords(
			"credit", "store credit", "compensation",
			"account credit",
		).SyntheticQueries(
		"issue store credit",
		"create credit request",
		"compensate customer",
	).MustBuild()
}

// RegisterAllTools registers all e-commerce tools to a toolchain.
func (f *EcommerceFixture) RegisterAllTools(
	tc gent.ToolChain,
) {
	tc.RegisterTool(f.getCustomerInfoTool())
	tc.RegisterTool(f.getOrdersTool())
	tc.RegisterTool(f.getOrderPaymentsTool())
	tc.RegisterTool(f.gatewayGetTxDetailTool())
	tc.RegisterTool(f.policySearch)
	tc.RegisterTool(f.policyGet)
	tc.RegisterTool(f.gatewayCancelTxTool())
	tc.RegisterTool(f.processRefundTool())
	tc.RegisterTool(f.createCaseTool())
	tc.RegisterTool(f.createCreditRequestTool())
}

// RegisterAllToolsSearch registers all e-commerce tools
// with IndexableTool metadata for SearchChain toolchain.
func (f *EcommerceFixture) RegisterAllToolsSearch(
	tc gent.ToolChain,
) {
	tc.RegisterTool(f.getCustomerInfoTool())
	tc.RegisterTool(f.getOrdersTool())
	tc.RegisterTool(f.getOrderPaymentsTool())
	tc.RegisterTool(f.gatewayGetTxDetailTool())
	// PolicySearchTool implements IndexableTool — register directly.
	tc.RegisterTool(f.policySearch)
	tc.RegisterTool(f.policyGet)
	tc.RegisterTool(f.gatewayCancelTxTool())
	tc.RegisterTool(f.processRefundTool())
	tc.RegisterTool(f.createCaseTool())
	tc.RegisterTool(f.createCreditRequestTool())
}
