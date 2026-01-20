package airline

import (
	"context"
	"os"
	"testing"
)

// TestRescheduleScenario tests the ReAct agent loop handling a flight reschedule request.
// This is an integration test that uses a real model (Grok) but mocked tools.
//
// Scenario: Customer John Smith (C001) wants to reschedule his flight AA100 to a later time
// on the same day because his meeting ran late.
func TestRescheduleScenario(t *testing.T) {
	if os.Getenv("GENT_TEST_XAI_KEY") == "" {
		t.Skip("GENT_TEST_XAI_KEY not set, skipping integration test")
	}

	ctx := context.Background()
	config := TestConfig()

	if err := RunRescheduleScenario(ctx, os.Stdout, config); err != nil {
		t.Fatalf("Reschedule scenario failed: %v", err)
	}
}
