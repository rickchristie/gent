// This file contains a single integration test for the airline
// scenario, using the most complex configuration: SearchJSON
// toolchain with summarization compaction.
//
// For other variations (YAML/JSON toolchain, sliding window
// compaction, SimpleList hint type, interactive chat), use the
// integration CLI instead:
//
//	go run ./integrationtest/cli
//
// The CLI provides a menu-driven interface to select scenarios,
// toolchains, compaction strategies, and even an interactive
// chat mode with real-time streaming output.
package airline

import (
	"context"
	"os"
	"testing"

	"github.com/rickchristie/gent/integrationtest/testutil"
)

// TestRescheduleSearchSummarization tests the ReAct agent
// loop handling a flight reschedule request using the
// SearchJSON toolchain with summarization compaction.
//
// This is the most complex configuration, combining tool
// discovery via search with context compaction via
// summarization (trigger=5, keep=1).
//
// Scenario: Customer John Smith (C001) wants to reschedule
// his flight AA100 to a later time on the same day because
// his meeting ran late.
func TestRescheduleSearchSummarization(t *testing.T) {
	if os.Getenv("GENT_TEST_XAI_KEY") == "" {
		t.Skip(
			"GENT_TEST_XAI_KEY not set, " +
				"skipping integration test",
		)
	}

	ctx := context.Background()
	config := testutil.DefaultTestConfig()
	config.ToolChain = testutil.ToolChainSearch
	config.Compaction = testutil.CompactionConfig{
		Type:              testutil.CompactionSummarization,
		TriggerIterations: 5,
		KeepRecent:        1,
	}

	if err := RunRescheduleScenarioSearch(
		ctx, os.Stdout, config,
	); err != nil {
		t.Fatalf(
			"Reschedule Search+Summarization "+
				"failed: %v", err,
		)
	}
}

// TestRescheduleSearchSummarizationPTC tests the
// flight reschedule scenario with SearchJSON toolchain
// wrapped in JsToolChainWrapper (programmatic tool
// calling) and summarization compaction.
func TestRescheduleSearchSummarizationPTC(
	t *testing.T,
) {
	if os.Getenv("GENT_TEST_XAI_KEY") == "" {
		t.Skip(
			"GENT_TEST_XAI_KEY not set, " +
				"skipping integration test",
		)
	}

	ctx := context.Background()
	config := testutil.DefaultTestConfig()
	config.ToolChain = testutil.ToolChainSearch
	config.WrapPTC = true
	config.Compaction = testutil.CompactionConfig{
		Type:              testutil.CompactionSummarization,
		TriggerIterations: 5,
		KeepRecent:        1,
	}

	if err := RunRescheduleScenarioSearch(
		ctx, os.Stdout, config,
	); err != nil {
		t.Fatalf(
			"Reschedule Search+Summarization+PTC "+
				"failed: %v", err,
		)
	}
}
