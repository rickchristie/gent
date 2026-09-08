//go:build cgo

package policy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickchristie/gent/common"
	"github.com/rickchristie/gent/search"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testEmbedder(t *testing.T) search.Embedder {
	t.Helper()
	cfg := common.ConfigsForModel("multilingual-e5-small")[0]

	if !common.ModelDownloaded(&cfg.Model) {
		t.Skip("multilingual-e5-small model not downloaded")
	}

	dir, err := common.ModelDir(cfg.Model.Name)
	if err != nil {
		t.Skipf("cannot determine model dir: %v", err)
	}

	embedder, err := search.NewOnnxEmbedder(cfg, search.OnnxOptions{
		ModelPath:      filepath.Join(dir, cfg.Model.ModelFile),
		TokenizerPath:  filepath.Join(dir, "tokenizer.json"),
		NumThreads:     2,
		MaxConcurrency: 2,
	})
	if err != nil {
		t.Skipf("ONNX embedder not available (run gent setup onnx): %v", err)
	}
	return embedder
}

// expectedOutput constructs the exact string that PolicySearchTool.Call returns for the
// given policy IDs in order. This is used for exact assert.Equal matching — the full policy
// content is included, not just IDs. If policy content changes, this helper picks up the
// change automatically without duplicating text in test expectations.
func expectedOutput(policies []*Policy, ids ...string) string {
	var sb strings.Builder
	for i, id := range ids {
		if i > 0 {
			sb.WriteString("\n\n---\n\n")
		}
		for _, p := range policies {
			if p.Id == id {
				fmt.Fprintf(&sb, "# %s\n\n%s", p.Id, p.FullContent)
				break
			}
		}
	}
	return sb.String()
}

// ============================================================================
// Airline policy quality tests
// ============================================================================

func TestPolicySearch_Quality_Airline(t *testing.T) {
	if os.Getenv("GENT_SKIP_ONNX") != "" {
		t.Skip("GENT_SKIP_ONNX set")
	}
	embedder := testEmbedder(t)
	defer embedder.Close()

	policies := airlinePolicies()
	tool, err := NewPolicySearchTool(context.Background(), embedder, policies)
	require.NoError(t, err)

	ctx := context.Background()

	// --- Semantic queries (BM25 is noise, semantic drives ranking) ---

	t.Run("exact ID search", func(t *testing.T) {
		result, err := tool.Call(ctx, PolicySearchInput{Query: "cancellation-refund"})
		require.NoError(t, err)
		assert.Equal(t,
			expectedOutput(policies,
				"cancellation-refund", "delay-compensation", "24-hour-cancellation"),
			result.Text)
	})

	t.Run("keyword cancel refund", func(t *testing.T) {
		result, err := tool.Call(ctx, PolicySearchInput{Query: "cancel refund ticket"})
		require.NoError(t, err)
		assert.Equal(t,
			expectedOutput(policies,
				"cancellation-refund", "delay-compensation", "24-hour-cancellation"),
			result.Text)
	})

	t.Run("semantic leave early", func(t *testing.T) {
		result, err := tool.Call(ctx, PolicySearchInput{
			Query: "customer wants to leave the trip early and go home",
		})
		require.NoError(t, err)
		assert.Equal(t,
			expectedOutput(policies,
				"flight-change-rebooking", "pet-travel", "24-hour-cancellation"),
			result.Text)
	})

	t.Run("baggage query", func(t *testing.T) {
		result, err := tool.Call(ctx, PolicySearchInput{
			Query: "how many bags can I check and what does it cost",
		})
		require.NoError(t, err)
		assert.Equal(t,
			expectedOutput(policies,
				"baggage-allowance", "apis-travel-documentation", "flight-change-rebooking"),
			result.Text)
	})

	t.Run("child flying alone", func(t *testing.T) {
		result, err := tool.Call(ctx, PolicySearchInput{
			Query: "my 10 year old needs to fly alone to visit grandparents",
		})
		require.NoError(t, err)
		assert.Equal(t,
			expectedOutput(policies,
				"unaccompanied-minor", "involuntary-rebooking", "pet-travel"),
			result.Text)
	})

	t.Run("pet travel", func(t *testing.T) {
		result, err := tool.Call(ctx, PolicySearchInput{
			Query: "bringing a dog on the plane in a carrier",
		})
		require.NoError(t, err)
		assert.Equal(t,
			expectedOutput(policies,
				"pet-travel", "baggage-allowance", "eu261-passenger-rights"),
			result.Text)
	})

	t.Run("overbooked flight", func(t *testing.T) {
		result, err := tool.Call(ctx, PolicySearchInput{
			Query: "flight is overbooked and they want to bump passengers",
		})
		require.NoError(t, err)
		assert.Equal(t,
			expectedOutput(policies,
				"involuntary-rebooking", "eu261-passenger-rights", "flight-change-rebooking"),
			result.Text)
	})

	t.Run("just booked want to cancel", func(t *testing.T) {
		// Both cancellation policies answer this query. The next results are near-tied
		// change/delay policies whose order depends on quantized inference kernels.
		// Check the full relevant response without prescribing an unrelated third match.
		tool.WithTopK(2)
		defer tool.WithTopK(3)
		result, err := tool.Call(ctx, PolicySearchInput{
			Query: "I just bought the ticket an hour ago, can I get my money back",
		})
		require.NoError(t, err)
		assert.Equal(t,
			expectedOutput(policies,
				"24-hour-cancellation", "cancellation-refund"),
			result.Text)
	})

	t.Run("broad frequent flyer changing flights", func(t *testing.T) {
		result, err := tool.Call(ctx, PolicySearchInput{
			Query: "frequent flyer benefits when changing flights",
		})
		require.NoError(t, err)
		assert.Equal(t,
			expectedOutput(policies,
				"frequent-flyer-benefits", "flight-change-rebooking", "same-day-change"),
			result.Text)
	})

	// --- BM25-critical queries (BM25 should strongly contribute) ---

	t.Run("identifier EU261", func(t *testing.T) {
		// Regulation code — embedding model can't encode an identifier.
		result, err := tool.Call(ctx, PolicySearchInput{Query: "EC261/2004"})
		require.NoError(t, err)
		assert.Equal(t,
			expectedOutput(policies,
				"eu261-passenger-rights", "unaccompanied-minor", "24-hour-cancellation"),
			result.Text)
	})

	t.Run("acronym APIS", func(t *testing.T) {
		// Rare aviation acronym the model likely never saw.
		result, err := tool.Call(ctx, PolicySearchInput{Query: "APIS requirements"})
		require.NoError(t, err)
		assert.Equal(t,
			expectedOutput(policies,
				"apis-travel-documentation", "unaccompanied-minor", "eu261-passenger-rights"),
			result.Text)
	})

	t.Run("short query WiFi", func(t *testing.T) {
		// Single token — embedding is low-information, BM25 matches exactly.
		result, err := tool.Call(ctx, PolicySearchInput{Query: "WiFi"})
		require.NoError(t, err)
		assert.Equal(t,
			expectedOutput(policies,
				"wifi-inflight-services", "baggage-allowance", "delay-compensation"),
			result.Text)
	})

	t.Run("short query ESTA", func(t *testing.T) {
		// 4-char travel acronym — BM25 finds it in apis-travel-documentation.
		result, err := tool.Call(ctx, PolicySearchInput{Query: "ESTA"})
		require.NoError(t, err)
		assert.Equal(t,
			expectedOutput(policies,
				"apis-travel-documentation", "baggage-allowance", "eu261-passenger-rights"),
			result.Text)
	})

	t.Run("numeric age discrimination", func(t *testing.T) {
		// "under 5" — BM25 matches "5" in unaccompanied-minor policy.
		result, err := tool.Call(ctx, PolicySearchInput{
			Query: "children under 5 years old",
		})
		require.NoError(t, err)
		assert.Equal(t,
			expectedOutput(policies,
				"unaccompanied-minor", "frequent-flyer-benefits", "pet-travel"),
			result.Text)
	})
}

// ============================================================================
// Ecommerce policy quality tests
// ============================================================================

func TestPolicySearch_Quality_Ecommerce(t *testing.T) {
	if os.Getenv("GENT_SKIP_ONNX") != "" {
		t.Skip("GENT_SKIP_ONNX set")
	}
	embedder := testEmbedder(t)
	defer embedder.Close()

	policies := ecommercePolicies()
	tool, err := NewPolicySearchTool(context.Background(), embedder, policies)
	require.NoError(t, err)

	ctx := context.Background()

	// --- Semantic queries ---

	t.Run("return product", func(t *testing.T) {
		result, err := tool.Call(ctx, PolicySearchInput{
			Query: "customer wants to return a product they bought last week",
		})
		require.NoError(t, err)
		assert.Equal(t,
			expectedOutput(policies,
				"standard-return", "electronics-return-15day", "exchange"),
			result.Text)
	})

	t.Run("price match Amazon", func(t *testing.T) {
		result, err := tool.Call(ctx, PolicySearchInput{
			Query: "customer found a lower price on Amazon for the same item",
		})
		require.NoError(t, err)
		assert.Equal(t,
			expectedOutput(policies,
				"price-match", "exchange", "damaged-defective"),
			result.Text)
	})

	t.Run("damaged in shipping", func(t *testing.T) {
		result, err := tool.Call(ctx, PolicySearchInput{
			Query: "item arrived broken and damaged in shipping",
		})
		require.NoError(t, err)
		assert.Equal(t,
			expectedOutput(policies,
				"damaged-defective", "shipping-delivery", "exchange"),
			result.Text)
	})

	t.Run("gift card expiration", func(t *testing.T) {
		result, err := tool.Call(ctx, PolicySearchInput{
			Query: "does the gift card expire",
		})
		require.NoError(t, err)
		assert.Equal(t,
			expectedOutput(policies,
				"gift-card", "store-credit", "refund-processing"),
			result.Text)
	})

	t.Run("loyalty points", func(t *testing.T) {
		// Only loyalty-program answers how to earn/redeem points. Requiring unrelated
		// return or price-match policies also tests platform-dependent numeric noise.
		tool.WithTopK(1)
		defer tool.WithTopK(3)
		result, err := tool.Call(ctx, PolicySearchInput{
			Query: "how do I earn and redeem loyalty points",
		})
		require.NoError(t, err)
		assert.Equal(t, expectedOutput(policies, "loyalty-program"), result.Text)
	})

	// --- BM25-critical queries ---

	t.Run("acronym RMA", func(t *testing.T) {
		// Domain-specific acronym the embedding model won't encode.
		result, err := tool.Call(ctx, PolicySearchInput{Query: "RMA"})
		require.NoError(t, err)
		assert.Equal(t,
			expectedOutput(policies,
				"rma-returns-authorization", "standard-return", "store-credit"),
			result.Text)
	})

	t.Run("identifier 1099-K", func(t *testing.T) {
		// Tax form number — arbitrary identifier with no semantic meaning.
		result, err := tool.Call(ctx, PolicySearchInput{Query: "1099-K"})
		require.NoError(t, err)
		assert.Equal(t,
			expectedOutput(policies,
				"tax-reporting-1099k", "standard-return", "store-credit"),
			result.Text)
	})

	t.Run("numeric 15 day electronics", func(t *testing.T) {
		// Numeric discrimination — "15 day" vs standard-return's "30 day".
		result, err := tool.Call(ctx, PolicySearchInput{
			Query: "15 day return electronics",
		})
		require.NoError(t, err)
		assert.Equal(t,
			expectedOutput(policies,
				"electronics-return-15day", "standard-return", "rma-returns-authorization"),
			result.Text)
	})

	t.Run("semantic confusion return laptop", func(t *testing.T) {
		// Semantic confusion — standard-return and electronics-return-15day are
		// semantically similar. BM25 matching "laptop" in electronics helps.
		result, err := tool.Call(ctx, PolicySearchInput{
			Query: "return a laptop",
		})
		require.NoError(t, err)
		assert.Equal(t,
			expectedOutput(policies,
				"standard-return", "electronics-return-15day", "rma-returns-authorization"),
			result.Text)
	})
}
