package airline

import (
	"context"
	"os"
	"testing"

	"github.com/rickchristie/gent/integrationtest/testutil"
)

// TestRescheduleScenarioYAML tests the ReAct agent loop handling
// a flight reschedule request.
//
// Scenario: Customer John Smith (C001) wants to reschedule his
// flight AA100 to a later time on the same day because his
// meeting ran late.
func TestRescheduleScenarioYAML(t *testing.T) {
	if os.Getenv("GENT_TEST_XAI_KEY") == "" {
		t.Skip(
			"GENT_TEST_XAI_KEY not set, " +
				"skipping integration test",
		)
	}

	ctx := context.Background()
	config := testutil.DefaultTestConfig()

	if err := RunRescheduleScenario(
		ctx, os.Stdout, config,
	); err != nil {
		t.Fatalf("Reschedule scenario failed: %v", err)
	}
}

// TestRescheduleScenarioJSON tests the ReAct agent loop handling
// a flight reschedule request using the JSON toolchain.
//
// Scenario: Customer John Smith (C001) wants to reschedule his
// flight AA100 to a later time on the same day because his
// meeting ran late.
func TestRescheduleScenarioJSON(t *testing.T) {
	if os.Getenv("GENT_TEST_XAI_KEY") == "" {
		t.Skip(
			"GENT_TEST_XAI_KEY not set, " +
				"skipping integration test",
		)
	}

	ctx := context.Background()
	config := testutil.DefaultTestConfigJSON()

	if err := RunRescheduleScenario(
		ctx, os.Stdout, config,
	); err != nil {
		t.Fatalf(
			"Reschedule scenario (JSON) failed: %v", err,
		)
	}
}
