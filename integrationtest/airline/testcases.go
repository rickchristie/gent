package airline

import (
	"context"
	"fmt"
	"io"

	"github.com/rickchristie/gent/integrationtest/testutil"
)

// RunRescheduleScenario runs the flight reschedule scenario.
func RunRescheduleScenario(
	ctx context.Context,
	w io.Writer,
	config testutil.TestConfig,
) error {
	fixture := NewAirlineFixture(nil)
	tp := fixture.TimeProvider()

	return testutil.RunScenario(ctx, w, config, testutil.ScenarioConfig{
		Name:        "airline-reschedule",
		HeaderTitle: "AIRLINE RESCHEDULE SCENARIO",
		CustomerRequest: `Hi, I'm John Smith and my email is ` +
			`john.smith@email.com.
I have a flight booked for tomorrow (flight AA100 from ` +
			`JFK to LAX) but my meeting is running late.
Can you help me reschedule to a later flight on the same ` +
			`day? I'd prefer an evening flight if possible.`,
		MaxIterations: 15,
		RegisterTools: fixture.RegisterAllTools,
		TimeProvider:  tp,
		SystemPrompt: fmt.Sprintf(`## Task Description

You are a helpful airline customer service agent for SkyWings `+
			`Airlines.
Your role is to assist customers with their flight bookings, `+
			`including checking flight information,
rescheduling flights, and answering policy questions.

Today is %s (%s).

Always be polite and professional. When rescheduling, make `+
			`sure to:
1. Verify the customer's identity and booking
2. Check the airline's change policy
3. Search for available alternative flights
4. Inform the customer of any fees before making changes
5. Confirm the change and provide updated booking details

SkyWings is an international airline. Reply with customer's `+
			`language.
`, tp.Today(), tp.Weekday()),
		CriticalRules: `DO NOT HALLUCINATE
- Every claim in your answer MUST come from tool outputs ` +
			`or user-provided information
- NEVER invent specific data (IDs, prices, times, ` +
			`availability)
- If information is missing, say so explicitly`,
		ThinkingPrompt: "Think step by step about how to " +
			"help the customer.",
	})
}

// GetAirlineTestCases returns all airline test cases for YAML.
func GetAirlineTestCases() []testutil.TestCase {
	return []testutil.TestCase{
		{
			Name: "John Smith — Reschedule (YAML)",
			Description: "John reschedules his morning " +
				"JFK→LAX flight to evening " +
				"(YAML toolchain)",
			Run: RunRescheduleScenario,
		},
	}
}

// GetAirlineTestCasesJSON returns all airline test cases for JSON.
func GetAirlineTestCasesJSON() []testutil.TestCase {
	return []testutil.TestCase{
		{
			Name: "John Smith — Reschedule (JSON)",
			Description: "John reschedules his morning " +
				"JFK→LAX flight to evening " +
				"(JSON toolchain)",
			Run: RunRescheduleScenario,
		},
	}
}

// NewAirlineInteractiveChat creates an interactive chat session
// for the airline domain.
func NewAirlineInteractiveChat(
	w io.Writer,
	config testutil.TestConfig,
) (*testutil.InteractiveChat, error) {
	fixture := NewAirlineFixture(nil)
	tp := fixture.TimeProvider()

	return testutil.NewInteractiveChat(w, config, testutil.ChatConfig{
		Name: "airline",
		SystemPrompt: fmt.Sprintf(`## Task Description

You are a helpful airline customer service agent for SkyWings `+
			`Airlines.
Your role is to assist customers with their flight bookings, `+
			`including checking flight information,
rescheduling flights, and answering policy questions.

Today is %s (%s).

Always be polite and professional. When rescheduling, make `+
			`sure to:
1. Verify the customer's identity and booking
2. Check the airline's change policy
3. Search for available alternative flights
4. Inform the customer of any fees before making changes
5. Confirm the change and provide updated booking details

SkyWings is an international airline. Reply with customer's `+
			`language.
`, tp.Today(), tp.Weekday()),
		CriticalRules: `DO NOT HALLUCINATE
- Every claim in your answer MUST come from tool outputs ` +
			`or user-provided information
- NEVER invent specific data (IDs, prices, times, ` +
			`availability)
- If information is missing, say so explicitly`,
		ThinkingPrompt: "Think step by step about how to " +
			"help the customer.",
		MaxIterations: 15,
		RegisterTools: fixture.RegisterAllTools,
		TimeProvider:  tp,
	})
}
