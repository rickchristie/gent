package airline

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/hooks"
	"github.com/rickchristie/gent/integrationtest/loggers"
	"github.com/rickchristie/gent/models"
	"github.com/rickchristie/gent/toolchain"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
)

// TestRescheduleScenario tests the ReAct agent loop handling a flight reschedule request.
// This is an integration test that uses a real model (Grok) but mocked tools.
//
// Scenario: Customer John Smith (C001) wants to reschedule his flight AA100 to a later time
// on the same day because his meeting ran late.
func TestRescheduleScenario(t *testing.T) {
	apiKey := os.Getenv("GENT_TEST_XAI_KEY")
	if apiKey == "" {
		t.Skip("GENT_TEST_XAI_KEY not set, skipping integration test")
	}

	ctx := context.Background()

	// Create the model (Grok 4.1 fast via xAI API)
	llm, err := openai.New(
		openai.WithToken(apiKey),
		openai.WithBaseURL("https://api.x.ai/v1"),
		openai.WithModel("grok-4-1-fast"),
	)
	if err != nil {
		t.Fatalf("failed to create xAI LLM: %v", err)
	}
	model := models.NewLCGWrapper(llm).WithModelName("grok-4-1-fast")

	// Create toolchain and register all airline tools
	tc := toolchain.NewYAML()
	RegisterAllTools(tc)

	// Create the ReAct loop with airline customer service context
	loop := agents.NewReactLoop(model).
		WithToolChain(tc).
		WithSystemPrompt(`## Task Description

You are a helpful airline customer service agent for SkyWings Airlines.
Your role is to assist customers with their flight bookings, including checking flight information,
rescheduling flights, and answering policy questions.

Always be polite and professional. When rescheduling, make sure to:
1. Verify the customer's identity and booking
2. Check the airline's change policy
3. Search for available alternative flights
4. Inform the customer of any fees before making changes
5. Confirm the change and provide updated booking details`).
		WithThinking("Think step by step about how to help the customer.")

	// Create the loop data with customer request
	customerRequest := `Hi, I'm John Smith and my email is john.smith@email.com.
I have a flight booked for tomorrow (flight AA100 from JFK to LAX) but my meeting is running late.
Can you help me reschedule to a later flight on the same day? I'd prefer an evening flight if possible.`

	data := agents.NewReactLoopData(llms.TextContent{Text: customerRequest})

	// Create hook registry with logger
	hookRegistry := hooks.NewRegistry()
	hookRegistry.Register(loggers.NewLoggerHook())

	// Create executor with hooks and reasonable limits
	exec := executor.New[*agents.ReactLoopData](loop, executor.Config{MaxIterations: 15}).
		WithHooks(hookRegistry)

	// Print test header
	fmt.Println(strings.Repeat("=", 80))
	fmt.Println("AIRLINE RESCHEDULE SCENARIO - INTEGRATION TEST")
	fmt.Println(strings.Repeat("=", 80))
	fmt.Println()

	// Execute the agent loop
	result := exec.Execute(ctx, data)

	// Print final summary
	fmt.Println()
	fmt.Println(strings.Repeat("=", 80))
	fmt.Println("FINAL SUMMARY")
	fmt.Println(strings.Repeat("=", 80))
	fmt.Println()

	execCtx := result.Context

	// Print final result
	if result.Error != nil {
		fmt.Printf("Error: %v\n", result.Error)
	} else {
		fmt.Println("Final Response to Customer:")
		fmt.Println(strings.Repeat("-", 40))
		for _, part := range result.Result {
			if tc, ok := part.(llms.TextContent); ok {
				fmt.Println(tc.Text)
			}
		}
		fmt.Println(strings.Repeat("-", 40))
	}
	fmt.Println()

	// Print full iteration history for debugging
	fmt.Println(strings.Repeat("=", 80))
	fmt.Println("FULL ITERATION HISTORY")
	fmt.Println(strings.Repeat("=", 80))

	for i, iter := range data.GetIterationHistory() {
		fmt.Printf("\n--- Iteration %d ---\n", i+1)
		for _, msg := range iter.Messages {
			fmt.Printf("[%s]\n", msg.Role)
			for _, part := range msg.Parts {
				if tc, ok := part.(llms.TextContent); ok {
					// Truncate very long responses for readability
					text := tc.Text
					if len(text) > 3000 {
						text = text[:3000] + "\n... (truncated)"
					}
					fmt.Println(text)
				}
			}
			fmt.Println()
		}
	}

	// Print all trace events
	fmt.Println(strings.Repeat("=", 80))
	fmt.Println("ALL TRACE EVENTS")
	fmt.Println(strings.Repeat("=", 80))

	for i, event := range execCtx.Events() {
		fmt.Printf("\n[%d] ", i+1)
		switch e := event.(type) {
		case gent.IterationStartTrace:
			fmt.Printf("IterationStart: iteration=%d\n", e.Iteration)
		case gent.IterationEndTrace:
			fmt.Printf("IterationEnd: iteration=%d, action=%s, duration=%s\n",
				e.Iteration, e.Action, e.Duration)
		case gent.ModelCallTrace:
			fmt.Printf("ModelCall: model=%s, input=%d, output=%d, duration=%s\n",
				e.Model, e.InputTokens, e.OutputTokens, e.Duration)
		case gent.ToolCallTrace:
			outputJSON, _ := json.Marshal(e.Output)
			outputStr := string(outputJSON)
			if len(outputStr) > 200 {
				outputStr = outputStr[:200] + "..."
			}
			fmt.Printf("ToolCall: tool=%s, duration=%s\n", e.ToolName, e.Duration)
			fmt.Printf("          input=%v\n", e.Input)
			fmt.Printf("          output=%s\n", outputStr)
			if e.Error != nil {
				fmt.Printf("          error=%v\n", e.Error)
			}
		default:
			fmt.Printf("Unknown event type: %T\n", event)
		}
	}

	fmt.Println()
	fmt.Println(strings.Repeat("=", 80))
	fmt.Println("TEST COMPLETE")
	fmt.Println(strings.Repeat("=", 80))
}
