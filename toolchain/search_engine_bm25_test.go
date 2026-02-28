package toolchain

import (
	"context"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBM25_Id(t *testing.T) {
	engine := NewBM25SearchEngine()
	assert.Equal(t, "bm25", engine.Id())
}

func TestBM25_SearchGuidance(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		engine := NewBM25SearchEngine()
		guidance := engine.SearchGuidance()
		assert.NotEmpty(t, guidance)
		assert.Contains(
			t, guidance, "natural language",
		)
	})

	t.Run("custom", func(t *testing.T) {
		engine := NewBM25SearchEngine().
			WithSearchGuidance("custom guidance")
		assert.Equal(
			t, "custom guidance",
			engine.SearchGuidance(),
		)
	})
}

func TestBM25_IndexAll(t *testing.T) {
	type input struct {
		tools []gent.IndexableTool
	}

	type expected struct {
		err error
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "single tool",
			input: input{
				tools: []gent.IndexableTool{
					&mockIndexableTool{
						name:        "search",
						description: "Search items",
						domain:      "General",
					},
				},
			},
			expected: expected{err: nil},
		},
		{
			name: "multiple tools",
			input: input{
				tools: []gent.IndexableTool{
					&mockIndexableTool{
						name:   "search",
						domain: "General",
					},
					&mockIndexableTool{
						name:   "lookup",
						domain: "Orders",
					},
				},
			},
			expected: expected{err: nil},
		},
		{
			name: "empty list",
			input: input{
				tools: []gent.IndexableTool{},
			},
			expected: expected{err: nil},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewBM25SearchEngine()
			err := engine.IndexAll(tt.input.tools)

			if tt.expected.err != nil {
				assert.ErrorIs(t, err, tt.expected.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestBM25_IndexAll_Reindex(t *testing.T) {
	engine := NewBM25SearchEngine()

	firstTools := []gent.IndexableTool{
		&mockIndexableTool{
			name:        "tool_alpha",
			description: "Alpha tool for testing",
			domain:      "Testing",
		},
	}
	err := engine.IndexAll(firstTools)
	require.NoError(t, err)

	results, err := engine.Search(
		context.Background(), "alpha",
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"tool_alpha"}, results)

	secondTools := []gent.IndexableTool{
		&mockIndexableTool{
			name:        "tool_beta",
			description: "Beta tool for testing",
			domain:      "Testing",
		},
		&mockIndexableTool{
			name:        "tool_gamma",
			description: "Gamma tool for testing",
			domain:      "Testing",
		},
	}
	err = engine.IndexAll(secondTools)
	require.NoError(t, err)

	// Old tool should not be found
	results, err = engine.Search(
		context.Background(), "alpha",
	)
	require.NoError(t, err)
	assert.Empty(t, results)

	// New tool should be found
	results, err = engine.Search(
		context.Background(), "beta",
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"tool_beta"}, results)
}

func TestBM25_Search(t *testing.T) {
	tools := []gent.IndexableTool{
		&mockIndexableTool{
			name:        "lookup_order",
			description: "Look up order details by ID",
			domain:      "Orders",
			categories:  []string{"lookup", "orders"},
			keywords: []string{
				"order", "tracking", "shipment",
			},
			syntheticQueries: []string{
				"find order status",
			},
		},
		&mockIndexableTool{
			name:        "send_email",
			description: "Send an email to a customer",
			domain:      "Communication",
			categories:  []string{"email", "notification"},
			keywords:    []string{"email", "message", "send"},
			syntheticQueries: []string{
				"email the customer",
			},
		},
		&mockIndexableTool{
			name:        "check_inventory",
			description: "Check product inventory levels",
			domain:      "Inventory",
			categories:  []string{"lookup", "inventory"},
			keywords: []string{
				"stock", "product", "warehouse",
			},
			syntheticQueries: []string{
				"how much stock is left",
			},
		},
	}

	type input struct {
		query string
	}

	type expected struct {
		containsNames []string
		isEmpty       bool
		err           error
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name:  "match by name",
			input: input{query: "lookup order"},
			expected: expected{
				containsNames: []string{"lookup_order"},
			},
		},
		{
			name:  "match by description",
			input: input{query: "email customer"},
			expected: expected{
				containsNames: []string{"send_email"},
			},
		},
		{
			name:  "match by keywords",
			input: input{query: "tracking shipment"},
			expected: expected{
				containsNames: []string{"lookup_order"},
			},
		},
		{
			name:  "match by categories",
			input: input{query: "notification"},
			expected: expected{
				containsNames: []string{"send_email"},
			},
		},
		{
			name:  "match by domain",
			input: input{query: "inventory"},
			expected: expected{
				containsNames: []string{
					"check_inventory",
				},
			},
		},
		{
			name:  "match by synthetic queries",
			input: input{query: "stock left"},
			expected: expected{
				containsNames: []string{
					"check_inventory",
				},
			},
		},
		{
			name:  "no matches returns empty slice",
			input: input{query: "xyznonexistent"},
			expected: expected{
				isEmpty: true,
			},
		},
		{
			name:  "multi-word query",
			input: input{query: "order tracking shipment"},
			expected: expected{
				containsNames: []string{"lookup_order"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewBM25SearchEngine()
			err := engine.IndexAll(tools)
			require.NoError(t, err)

			results, err := engine.Search(
				context.Background(), tt.input.query,
			)

			if tt.expected.err != nil {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)

			if tt.expected.isEmpty {
				assert.Empty(t, results)
				return
			}

			for _, name := range tt.expected.containsNames {
				assert.Contains(t, results, name)
			}
		})
	}
}

func TestBM25_Search_NotInitialized(t *testing.T) {
	engine := NewBM25SearchEngine()
	results, err := engine.Search(
		context.Background(), "test",
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")
	assert.Nil(t, results)
}
