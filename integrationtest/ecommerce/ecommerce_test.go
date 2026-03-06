// This file contains a single integration test for the
// e-commerce scenario, using the most complex configuration:
// SearchJSON toolchain with summarization compaction.
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
package ecommerce

import (
	"context"
	"os"
	"testing"

	"github.com/rickchristie/gent/integrationtest/testutil"
)

// TestDoubleChargeSearchSummarization tests the double-charge
// investigation scenario with SearchJSON toolchain and
// summarization compaction (trigger=5, keep=1).
//
// This is the most complex configuration, combining tool
// discovery via search with context compaction via
// summarization.
//
// Scenario: Customer Alex Rivera notices a duplicate $79.99
// charge for a "Mighty Mouse" purchase. The agent must
// investigate the billing issue, discover tools via search,
// and manage context through summarization compaction.
func TestDoubleChargeSearchSummarization(t *testing.T) {
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

	if err := RunDoubleChargeScenarioSearch(
		ctx, os.Stdout, config,
	); err != nil {
		t.Fatalf(
			"Double charge Search+Summarization "+
				"failed: %v", err,
		)
	}
}

// TestDoubleChargeSearchSummarizationPTC tests the
// double-charge scenario with SearchJSON toolchain
// wrapped in JsToolChainWrapper (programmatic tool
// calling) and summarization compaction.
func TestDoubleChargeSearchSummarizationPTC(
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

	if err := RunDoubleChargeScenarioSearch(
		ctx, os.Stdout, config,
	); err != nil {
		t.Fatalf(
			"Double charge Search+Summarization"+
				"+PTC failed: %v", err,
		)
	}
}
