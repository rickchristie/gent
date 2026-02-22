package ecommerce

import (
	"context"
	"fmt"
	"io"

	"github.com/rickchristie/gent/integrationtest/testutil"
)

// RunDoubleChargeScenario runs the double-charge investigation
// scenario. Expected ~11 iterations with 2 compaction events
// at trigger=5.
func RunDoubleChargeScenario(
	ctx context.Context,
	w io.Writer,
	config testutil.TestConfig,
) error {
	fixture := NewEcommerceFixture(nil)
	tp := fixture.TimeProvider()

	return testutil.RunScenario(
		ctx, w, config,
		testutil.ScenarioConfig{
			Name:        "ecommerce-double-charge",
			HeaderTitle: "ECOMMERCE DOUBLE CHARGE SCENARIO",
			CustomerRequest: `Hi, I'm Alex Rivera and my ` +
				`email is alex.rivera@email.com.
I just noticed that I was charged twice for a ` +
				`"Mighty Mouse" purchase I made about a ` +
				`week ago. The charge shows up two times ` +
				`on my credit card statement, both for ` +
				`$79.99. Can you look into this and help ` +
				`me get the duplicate charge resolved?`,
			MaxIterations: 20,
			RegisterTools: fixture.RegisterAllTools,
			TimeProvider:  tp,
			SystemPrompt: fmt.Sprintf(
				`## Task Description

You are a helpful billing support agent for TechGear `+
					`Online Store.
Your role is to assist customers with billing `+
					`inquiries, investigate payment `+
					`issues, and resolve disputes `+
					`following company procedures.

Today is %s (%s).

Always be polite and professional. When handling `+
					`billing issues:
1. Verify the customer's identity
2. Look up the relevant order and payment records
3. Search company guidance policies for the `+
					`correct procedure
4. Follow the resolution steps in order
5. Provide clear updates to the customer at each step
6. If automated resolution fails, escalate `+
					`appropriately

Important: Our internal payment records may be `+
					`out of date. Always verify with the `+
					`payment gateway for real-time status.
`, tp.Today(), tp.Weekday()),
			CriticalRules: `DO NOT HALLUCINATE
- Every claim MUST come from tool outputs or ` +
				`user-provided information
- NEVER invent data (IDs, amounts, statuses)
- If information is missing, say so explicitly
- Follow the guidance policy steps IN ORDER — ` +
				`do not skip steps`,
			ThinkingPrompt: "Think step by step about " +
				"how to investigate and resolve " +
				"the customer's billing issue.",
		},
	)
}

// GetEcommerceTestCases returns all e-commerce test cases
// for YAML toolchain.
func GetEcommerceTestCases() []testutil.TestCase {
	return []testutil.TestCase{
		{
			Name: "Alex Rivera — Double Charge (YAML)",
			Description: "Alex investigates a double " +
				"charge on a mouse purchase " +
				"(YAML toolchain)",
			Run: RunDoubleChargeScenario,
		},
	}
}

// GetEcommerceTestCasesJSON returns all e-commerce test cases
// for JSON toolchain.
func GetEcommerceTestCasesJSON() []testutil.TestCase {
	return []testutil.TestCase{
		{
			Name: "Alex Rivera — Double Charge (JSON)",
			Description: "Alex investigates a double " +
				"charge on a mouse purchase " +
				"(JSON toolchain)",
			Run: RunDoubleChargeScenario,
		},
	}
}

// NewEcommerceInteractiveChat creates an interactive chat
// session for the e-commerce domain.
func NewEcommerceInteractiveChat(
	w io.Writer,
	config testutil.TestConfig,
) (*testutil.InteractiveChat, error) {
	fixture := NewEcommerceFixture(nil)
	tp := fixture.TimeProvider()

	return testutil.NewInteractiveChat(
		w, config,
		testutil.ChatConfig{
			Name: "ecommerce",
			SystemPrompt: fmt.Sprintf(
				`## Task Description

You are a helpful billing support agent for TechGear `+
					`Online Store.
Your role is to assist customers with billing `+
					`inquiries, investigate payment `+
					`issues, and resolve disputes `+
					`following company procedures.

Today is %s (%s).

Always be polite and professional. When handling `+
					`billing issues:
1. Verify the customer's identity
2. Look up the relevant order and payment records
3. Search company guidance policies for the `+
					`correct procedure
4. Follow the resolution steps in order
5. Provide clear updates to the customer at each step
6. If automated resolution fails, escalate `+
					`appropriately

Important: Our internal payment records may be `+
					`out of date. Always verify with the `+
					`payment gateway for real-time status.
`, tp.Today(), tp.Weekday()),
			CriticalRules: `DO NOT HALLUCINATE
- Every claim MUST come from tool outputs or ` +
				`user-provided information
- NEVER invent data (IDs, amounts, statuses)
- If information is missing, say so explicitly
- Follow the guidance policy steps IN ORDER — ` +
				`do not skip steps`,
			ThinkingPrompt: "Think step by step about " +
				"how to investigate and resolve " +
				"the customer's billing issue.",
			MaxIterations: 20,
			RegisterTools: fixture.RegisterAllTools,
			TimeProvider:  tp,
		},
	)
}
