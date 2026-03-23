package toolchain

import (
	"context"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/search"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testIndexableTool is a minimal IndexableTool for testing.
type testIndexableTool struct {
	name             string
	description      string
	domain           string
	categories       []string
	keywords         []string
	syntheticQueries []string
}

func (t *testIndexableTool) Name() string              { return t.name }
func (t *testIndexableTool) Description() string       { return t.description }
func (t *testIndexableTool) Domain() string            { return t.domain }
func (t *testIndexableTool) Categories() []string      { return t.categories }
func (t *testIndexableTool) Keywords() []string        { return t.keywords }
func (t *testIndexableTool) SyntheticQueries() []string { return t.syntheticQueries }

func TestToolBleveAdapter_Convert(t *testing.T) {
	adapter := &ToolBleveAdapter{}
	tool := &testIndexableTool{
		name:             "get_billing_ledger",
		description:      "Retrieve billing ledger entries",
		domain:           "Billing",
		categories:       []string{"lookup", "billing"},
		keywords:         []string{"billing", "payment", "invoice"},
		syntheticQueries: []string{"check payment status", "look up invoices"},
	}

	result, err := adapter.Convert(tool)
	require.NoError(t, err)

	m, ok := result.(map[string]string)
	require.True(t, ok)
	assert.Equal(t, "get_billing_ledger", m["name"])
	assert.Equal(t, "get_billing_ledger", m["name_analyzed"])
	assert.Equal(t, "Billing", m["domain"])
	assert.Equal(t, "lookup billing", m["categories"])
	assert.Equal(t, "billing payment invoice", m["keywords"])
	assert.Equal(t, "Retrieve billing ledger entries", m["description"])
	assert.Equal(t, "check payment status look up invoices", m["synthetic_queries"])
}

func TestToolChunkAdapter_Convert(t *testing.T) {
	adapter := &ToolChunkAdapter{}
	tool := &testIndexableTool{
		name:             "get_billing_ledger",
		description:      "Retrieve billing ledger entries",
		domain:           "Billing",
		categories:       []string{"lookup", "billing"},
		keywords:         []string{"billing", "payment"},
		syntheticQueries: []string{"check payments", "look up invoices"},
	}

	tc := search.TokenCounterFunc(func(s string) int { return len(s) / 4 })
	chunks, err := adapter.Chunks(tool, tc, 512)
	require.NoError(t, err)
	assert.Equal(t, []search.Chunk{
		{
			Text: `# get_billing_ledger

Retrieve billing ledger entries

- Domain: Billing
- Categories: lookup, billing
- Keywords: billing, payment
- Example queries: check payments; look up invoices`,
		},
	}, chunks)
}

func TestIndexToolSearcher_WithBleveIndex(t *testing.T) {
	bleveIdx, err := search.NewBleveIndex(&ToolBleveAdapter{},
		search.WithTheoreticalMaxConfidenceThreshold(0),
	)
	require.NoError(t, err)
	defer bleveIdx.Close()

	engine := NewIndexToolSearcher("bm25", bleveIdx)

	tools := []gent.IndexableTool{
		&testIndexableTool{
			name: "get_billing_ledger", description: "Retrieve billing ledger entries",
			domain: "Billing", categories: []string{"lookup"},
			keywords: []string{"billing", "payment", "invoice", "ledger"},
			syntheticQueries: []string{"check payment status"},
		},
		&testIndexableTool{
			name: "send_notification", description: "Send notification via email or SMS",
			domain: "Communication", categories: []string{"send"},
			keywords: []string{"email", "sms", "notification", "message"},
			syntheticQueries: []string{"notify customer"},
		},
		&testIndexableTool{
			name: "cancel_reservation", description: "Cancel an existing reservation",
			domain: "Reservations", categories: []string{"mutation"},
			keywords: []string{"cancel", "reservation", "booking"},
			syntheticQueries: []string{"cancel a booking"},
		},
	}

	require.NoError(t, engine.IndexAll(tools))

	ctx := context.Background()

	// BM25 keyword match: "billing payment" should find the billing tool first.
	results, err := engine.Search(ctx, "billing payment")
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(results), 1)
	assert.Equal(t, "get_billing_ledger", results[0])

	// Exact tool name match should rank that tool first.
	results, err = engine.Search(ctx, "cancel_reservation")
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(results), 1)
	assert.Equal(t, "cancel_reservation", results[0])
}

func TestIndexToolSearcher_IndexAllReplacesExisting(t *testing.T) {
	bleveIdx, err := search.NewBleveIndex(&ToolBleveAdapter{},
		search.WithTheoreticalMaxConfidenceThreshold(0),
	)
	require.NoError(t, err)
	defer bleveIdx.Close()

	engine := NewIndexToolSearcher("bm25", bleveIdx)

	oldTools := []gent.IndexableTool{
		&testIndexableTool{
			name: "old_tool", description: "Old tool description",
			keywords: []string{"old", "deprecated"},
		},
	}
	require.NoError(t, engine.IndexAll(oldTools))

	newTools := []gent.IndexableTool{
		&testIndexableTool{
			name: "new_tool", description: "New tool description",
			keywords: []string{"new", "current"},
		},
	}
	require.NoError(t, engine.IndexAll(newTools))

	ctx := context.Background()

	// Old tool should be gone.
	results, err := engine.Search(ctx, "old deprecated")
	require.NoError(t, err)
	assert.Empty(t, results)

	// New tool should be found.
	results, err = engine.Search(ctx, "new current")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "new_tool", results[0])
}

func TestIndexToolSearcher_IdAndGuidance(t *testing.T) {
	bleveIdx, err := search.NewBleveIndex(&ToolBleveAdapter{},
		search.WithTheoreticalMaxConfidenceThreshold(0),
	)
	require.NoError(t, err)
	defer bleveIdx.Close()

	engine := NewIndexToolSearcher("hybrid", bleveIdx).
		WithSearchGuidance("Custom guidance")

	assert.Equal(t, "hybrid", engine.Id())
	assert.Equal(t, "Custom guidance", engine.SearchGuidance())
}
