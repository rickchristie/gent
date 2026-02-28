package toolchain

import (
	"context"
	"fmt"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockIndexableTool implements both IndexableTool and Tool for
// testing purposes.
type mockIndexableTool struct {
	name             string
	description      string
	domain           string
	categories       []string
	keywords         []string
	syntheticQueries []string
}

func (t *mockIndexableTool) Name() string               { return t.name }
func (t *mockIndexableTool) Description() string         { return t.description }
func (t *mockIndexableTool) Domain() string              { return t.domain }
func (t *mockIndexableTool) Categories() []string        { return t.categories }
func (t *mockIndexableTool) Keywords() []string          { return t.keywords }
func (t *mockIndexableTool) SyntheticQueries() []string  { return t.syntheticQueries }
func (t *mockIndexableTool) Policy() string              { return "" }
func (t *mockIndexableTool) ParameterSchema() map[string]any {
	return nil
}

func (t *mockIndexableTool) Call(
	_ context.Context,
	input map[string]any,
) (*gent.ToolResult[string], error) {
	return &gent.ToolResult[string]{Text: "ok"}, nil
}

func TestRegex_Id(t *testing.T) {
	engine := NewRegexSearchEngine()
	assert.Equal(t, "regex", engine.Id())
}

func TestRegex_SearchGuidance(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		engine := NewRegexSearchEngine()
		guidance := engine.SearchGuidance()
		assert.NotEmpty(t, guidance)
		assert.Contains(t, guidance, "regex")
		assert.Contains(
			t, guidance, "order.*status",
		)
	})

	t.Run("custom", func(t *testing.T) {
		engine := NewRegexSearchEngine().
			WithSearchGuidance("custom regex tips")
		assert.Equal(
			t, "custom regex tips",
			engine.SearchGuidance(),
		)
	})
}

func TestRegex_IndexAll(t *testing.T) {
	type input struct {
		tools []gent.IndexableTool
	}

	type expected struct {
		err       error
		toolCount int
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
						description: "Search for items",
						domain:      "General",
					},
				},
			},
			expected: expected{
				err:       nil,
				toolCount: 1,
			},
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
			expected: expected{
				err:       nil,
				toolCount: 2,
			},
		},
		{
			name: "empty list",
			input: input{
				tools: []gent.IndexableTool{},
			},
			expected: expected{
				err:       nil,
				toolCount: 0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewRegexSearchEngine()
			err := engine.IndexAll(tt.input.tools)

			if tt.expected.err != nil {
				assert.ErrorIs(t, err, tt.expected.err)
			} else {
				require.NoError(t, err)
				assert.Len(t, engine.tools, tt.expected.toolCount)
			}
		})
	}
}

func TestRegex_IndexAll_Reindex(t *testing.T) {
	engine := NewRegexSearchEngine()

	firstTools := []gent.IndexableTool{
		&mockIndexableTool{name: "tool_a", domain: "A"},
	}
	err := engine.IndexAll(firstTools)
	require.NoError(t, err)
	assert.Len(t, engine.tools, 1)

	secondTools := []gent.IndexableTool{
		&mockIndexableTool{name: "tool_b", domain: "B"},
		&mockIndexableTool{name: "tool_c", domain: "C"},
	}
	err = engine.IndexAll(secondTools)
	require.NoError(t, err)
	assert.Len(t, engine.tools, 2)

	results, err := engine.Search(context.Background(), "tool_a")
	require.NoError(t, err)
	assert.Empty(t, results)

	results, err = engine.Search(context.Background(), "tool_b")
	require.NoError(t, err)
	assert.Equal(t, []string{"tool_b"}, results)
}

func TestRegex_Search(t *testing.T) {
	tools := []gent.IndexableTool{
		&mockIndexableTool{
			name:             "lookup_order",
			description:      "Look up order details by ID",
			domain:           "Orders",
			categories:       []string{"lookup", "orders"},
			keywords:         []string{"order", "tracking", "shipment"},
			syntheticQueries: []string{"find order status"},
		},
		&mockIndexableTool{
			name:             "send_email",
			description:      "Send an email to a customer",
			domain:           "Communication",
			categories:       []string{"email", "notification"},
			keywords:         []string{"email", "message", "send"},
			syntheticQueries: []string{"email the customer"},
		},
		&mockIndexableTool{
			name:             "check_order_status",
			description:      "Check the current status of an order",
			domain:           "Orders",
			categories:       []string{"lookup", "status"},
			keywords:         []string{"order", "status", "check"},
			syntheticQueries: []string{"what is my order status"},
		},
	}

	type input struct {
		query string
	}

	type expected struct {
		names []string
		err   error
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name:  "simple pattern matches name",
			input: input{query: "lookup_order"},
			expected: expected{
				names: []string{"lookup_order"},
				err:   nil,
			},
		},
		{
			name:  "dot-star regex across fields",
			input: input{query: "order.*status"},
			expected: expected{
				names: []string{
					"check_order_status",
					"lookup_order",
				},
				err: nil,
			},
		},
		{
			name:  "match in keywords",
			input: input{query: "shipment"},
			expected: expected{
				names: []string{"lookup_order"},
				err:   nil,
			},
		},
		{
			name:  "match in synthetic queries",
			input: input{query: "email the customer"},
			expected: expected{
				names: []string{"send_email"},
				err:   nil,
			},
		},
		{
			name:  "match in description",
			input: input{query: "Send an email"},
			expected: expected{
				names: []string{"send_email"},
				err:   nil,
			},
		},
		{
			name:  "match in domain",
			input: input{query: "Communication"},
			expected: expected{
				names: []string{"send_email"},
				err:   nil,
			},
		},
		{
			name:  "match in categories",
			input: input{query: "notification"},
			expected: expected{
				names: []string{"send_email"},
				err:   nil,
			},
		},
		{
			name:  "case insensitive",
			input: input{query: "ORDER"},
			expected: expected{
				// lookup_order: name(1)+desc(1)+domain(1)+
				// cats:orders(1)+kw:order(1)+synth(1)=6
				// check_order_status: name(1)+desc(1)+
				// domain(1)+kw:order(1)+synth(1)=5
				names: []string{
					"lookup_order",
					"check_order_status",
				},
				err: nil,
			},
		},
		{
			name:  "no matches returns empty slice",
			input: input{query: "nonexistent_xyz"},
			expected: expected{
				names: []string{},
				err:   nil,
			},
		},
		{
			name:  "invalid regex returns error",
			input: input{query: "[invalid"},
			expected: expected{
				names: nil,
				err:   fmt.Errorf("invalid regex"),
			},
		},
		{
			name:  "ranking by match count",
			input: input{query: "order"},
			expected: expected{
				// lookup_order: name(1)+desc(1)+domain(1)+
				// cats:orders(1)+kw:order(1)+synth(1)=6
				// check_order_status: name(1)+desc(1)+
				// domain(1)+kw:order(1)+synth(1)=5
				names: []string{
					"lookup_order",
					"check_order_status",
				},
				err: nil,
			},
		},
		{
			name:  "alternation regex",
			input: input{query: "email|shipment"},
			expected: expected{
				names: []string{
					"send_email",
					"lookup_order",
				},
				err: nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewRegexSearchEngine()
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
			if len(tt.expected.names) == 0 {
				assert.Empty(t, results)
			} else {
				assert.Equal(t, tt.expected.names, results)
			}
		})
	}
}
