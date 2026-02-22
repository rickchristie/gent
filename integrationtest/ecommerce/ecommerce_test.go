package ecommerce

import (
	"context"
	"os"
	"testing"

	"github.com/rickchristie/gent/integrationtest/testutil"
)

// TestDoubleChargeScenarioYAML tests the double-charge
// investigation scenario without compaction.
func TestDoubleChargeScenarioYAML(t *testing.T) {
	if os.Getenv("GENT_TEST_XAI_KEY") == "" {
		t.Skip(
			"GENT_TEST_XAI_KEY not set, " +
				"skipping integration test",
		)
	}

	ctx := context.Background()
	config := testutil.DefaultTestConfig()

	if err := RunDoubleChargeScenario(
		ctx, os.Stdout, config,
	); err != nil {
		t.Fatalf(
			"Double charge scenario failed: %v", err,
		)
	}
}

// TestDoubleChargeScenarioJSON tests the double-charge
// investigation scenario with JSON toolchain.
func TestDoubleChargeScenarioJSON(t *testing.T) {
	if os.Getenv("GENT_TEST_XAI_KEY") == "" {
		t.Skip(
			"GENT_TEST_XAI_KEY not set, " +
				"skipping integration test",
		)
	}

	ctx := context.Background()
	config := testutil.DefaultTestConfigJSON()

	if err := RunDoubleChargeScenario(
		ctx, os.Stdout, config,
	); err != nil {
		t.Fatalf(
			"Double charge scenario (JSON) failed: %v",
			err,
		)
	}
}

// TestDoubleChargeSlidingWindow tests the double-charge
// scenario with sliding window compaction (trigger=5, window=3).
func TestDoubleChargeSlidingWindow(t *testing.T) {
	if os.Getenv("GENT_TEST_XAI_KEY") == "" {
		t.Skip(
			"GENT_TEST_XAI_KEY not set, " +
				"skipping integration test",
		)
	}

	ctx := context.Background()
	config := testutil.DefaultTestConfig()
	config.Compaction = testutil.CompactionConfig{
		Type:              testutil.CompactionSlidingWindow,
		TriggerIterations: 5,
		WindowSize:        3,
	}

	if err := RunDoubleChargeScenario(
		ctx, os.Stdout, config,
	); err != nil {
		t.Fatalf(
			"Double charge sliding window failed: %v",
			err,
		)
	}
}

// TestDoubleChargeSummarization tests the double-charge
// scenario with summarization compaction (trigger=5, keep=1).
func TestDoubleChargeSummarization(t *testing.T) {
	if os.Getenv("GENT_TEST_XAI_KEY") == "" {
		t.Skip(
			"GENT_TEST_XAI_KEY not set, " +
				"skipping integration test",
		)
	}

	ctx := context.Background()
	config := testutil.DefaultTestConfig()
	config.Compaction = testutil.CompactionConfig{
		Type:              testutil.CompactionSummarization,
		TriggerIterations: 5,
		KeepRecent:        1,
	}

	if err := RunDoubleChargeScenario(
		ctx, os.Stdout, config,
	); err != nil {
		t.Fatalf(
			"Double charge summarization failed: %v",
			err,
		)
	}
}
