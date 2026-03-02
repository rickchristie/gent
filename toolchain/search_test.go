package toolchain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/format"
	"github.com/rickchristie/gent/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// searchTestFormat returns a TextFormat for use in tests.
func searchTestFormat() gent.TextFormat {
	return format.NewXML()
}

// indexableToolFunc wraps a ToolFunc with IndexableTool
// metadata. It implements both Tool[I,O] and IndexableTool.
type indexableToolFunc struct {
	*gent.ToolFunc[map[string]any, string]
	domain           string
	categories       []string
	keywords         []string
	syntheticQueries []string
}

func (t *indexableToolFunc) Domain() string {
	return t.domain
}
func (t *indexableToolFunc) Categories() []string {
	return t.categories
}
func (t *indexableToolFunc) Keywords() []string {
	return t.keywords
}
func (t *indexableToolFunc) SyntheticQueries() []string {
	return t.syntheticQueries
}

// newIndexableTool creates an indexable tool for testing.
func newIndexableTool(
	name, description, domain string,
	categories, keywords []string,
	fn func(ctx context.Context, args map[string]any) (string, error),
) *indexableToolFunc {
	return &indexableToolFunc{
		ToolFunc: gent.NewToolFunc(
			name, description, nil, fn,
		),
		domain:     domain,
		categories: categories,
		keywords:   keywords,
	}
}

// newIndexableToolWithSchema creates an indexable tool with
// a parameter schema for testing.
func newIndexableToolWithSchema(
	name, description, domain string,
	categories, keywords []string,
	s map[string]any,
	fn func(ctx context.Context, args map[string]any) (string, error),
) *indexableToolFunc {
	return &indexableToolFunc{
		ToolFunc: gent.NewToolFunc(
			name, description, s, fn,
		),
		domain:     domain,
		categories: categories,
		keywords:   keywords,
	}
}

// mockSearchEngine is a mock search engine for testing.
type mockSearchEngine struct {
	id          string
	guidance    string
	indexErr    error
	searchFn    func(ctx context.Context, query string) ([]string, error)
	indexCalled bool
}

func (e *mockSearchEngine) Id() string {
	return e.id
}
func (e *mockSearchEngine) SearchGuidance() string {
	return e.guidance
}
func (e *mockSearchEngine) IndexAll(
	_ []gent.IndexableTool,
) error {
	e.indexCalled = true
	return e.indexErr
}
func (e *mockSearchEngine) Search(
	ctx context.Context,
	query string,
) ([]string, error) {
	if e.searchFn != nil {
		return e.searchFn(ctx, query)
	}
	return nil, nil
}

// newExecCtx creates an ExecutionContext for testing.
func newExecCtx() *gent.ExecutionContext {
	return gent.NewExecutionContext(
		context.Background(), "test", nil,
	)
}

// setupSearchJSON creates a fully initialized SearchJSON
// with the given tools and engines.
func setupSearchJSON(
	tools []*indexableToolFunc,
	engines []gent.SearchEngine,
) *SearchJSON {
	tc := NewSearchJSON(SearchHintDomainCategories)
	for _, eng := range engines {
		tc.RegisterEngine(eng)
	}
	for _, tool := range tools {
		tc.RegisterTool(tool)
	}
	if err := tc.Initialize(); err != nil {
		panic(fmt.Sprintf("Initialize failed: %v", err))
	}
	return tc
}

// -------------------------------------------------------
// Constructor / Config Tests
// -------------------------------------------------------

func TestSearchJSON_Name(t *testing.T) {
	type input struct {
		customName string
	}

	type expected struct {
		name string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name:     "default name",
			input:    input{customName: ""},
			expected: expected{name: "action"},
		},
		{
			name:     "custom name",
			input:    input{customName: "tools"},
			expected: expected{name: "tools"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewSearchJSON(SearchHintDomainCategories)
			if tt.input.customName != "" {
				tc.WithSectionName(tt.input.customName)
			}
			assert.Equal(t, tt.expected.name, tc.Name())
		})
	}
}

func TestSearchJSON_Guidance(t *testing.T) {
	tc := NewSearchJSON(SearchHintDomainCategories)
	guidance := tc.Guidance()

	expectedGuidance := `Call tools using JSON format:
{"tool": "tool_name", "args": {...}}

For multiple parallel calls, use an array:
[{"tool": "tool1", "args": {...}}, ` +
		`{"tool": "tool2", "args": {...}}]`

	assert.Equal(t, expectedGuidance, guidance)
}

func TestSearchJSON_Config(t *testing.T) {
	type expected struct {
		pageSize         int
		noResultsMessage string
	}

	tests := []struct {
		name     string
		setup    func() *SearchJSON
		expected expected
	}{
		{
			name: "default page size",
			setup: func() *SearchJSON {
				return NewSearchJSON(SearchHintDomainCategories)
			},
			expected: expected{
				pageSize: 3,
				noResultsMessage: "No tools found " +
					"matching your query. " +
					"Try different keywords " +
					"or a broader search.",
			},
		},
		{
			name: "custom page size",
			setup: func() *SearchJSON {
				return NewSearchJSON(SearchHintDomainCategories).WithPageSize(5)
			},
			expected: expected{
				pageSize:         5,
				noResultsMessage: "No tools found matching your query. Try different keywords or a broader search.",
			},
		},
		{
			name: "custom no-results message",
			setup: func() *SearchJSON {
				return NewSearchJSON(SearchHintDomainCategories).
					WithNoResultsMessage("Nothing found")
			},
			expected: expected{
				pageSize:         3,
				noResultsMessage: "Nothing found",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := tt.setup()
			assert.Equal(
				t, tt.expected.pageSize, tc.pageSize,
			)
			assert.Equal(
				t,
				tt.expected.noResultsMessage,
				tc.noResultsMessage,
			)
		})
	}
}

// -------------------------------------------------------
// RegisterTool Tests
// -------------------------------------------------------

func TestSearchJSON_RegisterTool(t *testing.T) {
	t.Run("valid indexable tool succeeds", func(t *testing.T) {
		tc := NewSearchJSON(SearchHintDomainCategories)
		tool := newIndexableTool(
			"test", "Test tool", "General",
			nil, nil,
			func(_ context.Context, _ map[string]any) (string, error) {
				return "ok", nil
			},
		)
		result := tc.RegisterTool(tool)
		assert.NotNil(t, result)
		assert.Len(t, tc.tools, 1)
		assert.Len(t, tc.indexableTools, 1)
	})

	t.Run("panics on non-IndexableTool", func(t *testing.T) {
		tc := NewSearchJSON(SearchHintDomainCategories)
		tool := gent.NewToolFunc(
			"test", "Test tool", nil,
			func(_ context.Context, _ map[string]any) (string, error) {
				return "ok", nil
			},
		)
		assert.Panics(t, func() {
			tc.RegisterTool(tool)
		})
	})

	t.Run("panics on invalid type", func(t *testing.T) {
		tc := NewSearchJSON(SearchHintDomainCategories)
		assert.Panics(t, func() {
			tc.RegisterTool("not a tool")
		})
	})

	t.Run("panics on duplicate name", func(t *testing.T) {
		tc := NewSearchJSON(SearchHintDomainCategories)
		tool1 := newIndexableTool(
			"test", "Tool 1", "General",
			nil, nil,
			func(_ context.Context, _ map[string]any) (string, error) {
				return "ok", nil
			},
		)
		tool2 := newIndexableTool(
			"test", "Tool 2", "General",
			nil, nil,
			func(_ context.Context, _ map[string]any) (string, error) {
				return "ok", nil
			},
		)
		tc.RegisterTool(tool1)
		assert.Panics(t, func() {
			tc.RegisterTool(tool2)
		})
	})

	t.Run("method chaining works", func(t *testing.T) {
		tc := NewSearchJSON(SearchHintDomainCategories)
		tool1 := newIndexableTool(
			"tool1", "Tool 1", "A",
			nil, nil,
			func(_ context.Context, _ map[string]any) (string, error) {
				return "ok", nil
			},
		)
		tool2 := newIndexableTool(
			"tool2", "Tool 2", "B",
			nil, nil,
			func(_ context.Context, _ map[string]any) (string, error) {
				return "ok", nil
			},
		)
		result := tc.RegisterTool(tool1).RegisterTool(tool2)
		assert.NotNil(t, result)
		// SearchJSON is under the ToolChain interface,
		// cast back to check
		stc := result.(*SearchJSON)
		assert.Len(t, stc.tools, 2)
	})

	t.Run("multiple tools registered correctly",
		func(t *testing.T) {
			tc := NewSearchJSON(SearchHintDomainCategories)
			for i := range 5 {
				name := fmt.Sprintf("tool_%d", i)
				tool := newIndexableTool(
					name, "desc", "Domain",
					nil, nil,
					func(_ context.Context, _ map[string]any) (string, error) {
						return "ok", nil
					},
				)
				tc.RegisterTool(tool)
			}
			assert.Len(t, tc.tools, 5)
			assert.Len(t, tc.indexableTools, 5)
			assert.Len(t, tc.toolMap, 5)
		},
	)
}

// -------------------------------------------------------
// Initialize Tests
// -------------------------------------------------------

func TestSearchJSON_Initialize(t *testing.T) {
	t.Run("success with one engine", func(t *testing.T) {
		tc := NewSearchJSON(SearchHintDomainCategories)
		engine := &mockSearchEngine{
			id:       "mock",
			guidance: "mock guidance",
		}
		tc.RegisterEngine(engine)
		tool := newIndexableTool(
			"test", "Test", "General",
			[]string{"cat1"}, nil,
			func(_ context.Context, _ map[string]any) (string, error) {
				return "ok", nil
			},
		)
		tc.RegisterTool(tool)

		err := tc.Initialize()
		require.NoError(t, err)
		assert.True(t, tc.initialized)
		assert.True(t, engine.indexCalled)
		assert.NotEmpty(t, tc.searchToolPrompt)
	})

	t.Run("success with multiple engines",
		func(t *testing.T) {
			tc := NewSearchJSON(SearchHintDomainCategories)
			eng1 := &mockSearchEngine{
				id: "eng1", guidance: "g1",
			}
			eng2 := &mockSearchEngine{
				id: "eng2", guidance: "g2",
			}
			tc.RegisterEngine(eng1)
			tc.RegisterEngine(eng2)
			tool := newIndexableTool(
				"test", "Test", "General",
				nil, nil,
				func(_ context.Context, _ map[string]any) (string, error) {
					return "ok", nil
				},
			)
			tc.RegisterTool(tool)

			err := tc.Initialize()
			require.NoError(t, err)
			assert.True(t, eng1.indexCalled)
			assert.True(t, eng2.indexCalled)
		},
	)

	t.Run("error when no engines", func(t *testing.T) {
		tc := NewSearchJSON(SearchHintDomainCategories)
		tool := newIndexableTool(
			"test", "Test", "General",
			nil, nil,
			func(_ context.Context, _ map[string]any) (string, error) {
				return "ok", nil
			},
		)
		tc.RegisterTool(tool)

		err := tc.Initialize()
		assert.Error(t, err)
		assert.Contains(
			t, err.Error(), "at least one search engine",
		)
	})

	t.Run("error when engine IndexAll fails",
		func(t *testing.T) {
			tc := NewSearchJSON(SearchHintDomainCategories)
			engine := &mockSearchEngine{
				id:       "failing",
				guidance: "g",
				indexErr: errors.New("index failed"),
			}
			tc.RegisterEngine(engine)
			tool := newIndexableTool(
				"test", "Test", "General",
				nil, nil,
				func(_ context.Context, _ map[string]any) (string, error) {
					return "ok", nil
				},
			)
			tc.RegisterTool(tool)

			err := tc.Initialize()
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "index failed")
		},
	)

	t.Run("re-initialize updates state", func(t *testing.T) {
		tc := NewSearchJSON(SearchHintDomainCategories)
		engine := &mockSearchEngine{
			id:       "mock",
			guidance: "guide",
		}
		tc.RegisterEngine(engine)
		tool := newIndexableTool(
			"test", "Test", "General",
			nil, nil,
			func(_ context.Context, _ map[string]any) (string, error) {
				return "ok", nil
			},
		)
		tc.RegisterTool(tool)

		err := tc.Initialize()
		require.NoError(t, err)
		prompt1 := tc.searchToolPrompt

		err = tc.Initialize()
		require.NoError(t, err)
		prompt2 := tc.searchToolPrompt

		assert.Equal(t, prompt1, prompt2)
		assert.True(t, tc.initialized)
	})

	t.Run("domain summary with categories and counts",
		func(t *testing.T) {
			tc := NewSearchJSON(SearchHintDomainCategories)
			engine := &mockSearchEngine{
				id: "mock", guidance: "g",
			}
			tc.RegisterEngine(engine)

			for i := range 3 {
				tool := newIndexableTool(
					fmt.Sprintf("order_%d", i),
					"Order tool",
					"Orders",
					[]string{"lookup", "mutation"},
					nil,
					func(_ context.Context, _ map[string]any) (string, error) {
						return "ok", nil
					},
				)
				tc.RegisterTool(tool)
			}
			tool := newIndexableTool(
				"email", "Email tool",
				"Communication",
				[]string{"email", "notification"},
				nil,
				func(_ context.Context, _ map[string]any) (string, error) {
					return "ok", nil
				},
			)
			tc.RegisterTool(tool)

			err := tc.Initialize()
			require.NoError(t, err)

			prompt := tc.AvailableToolsPrompt()
			assert.Contains(
				t, prompt,
				"Orders (lookup, mutation) - 3 tools",
			)
			assert.Contains(
				t, prompt,
				"Communication (email, notification) "+
					"- 1 tools",
			)
		},
	)
}

// -------------------------------------------------------
// AvailableToolsPrompt Tests
// -------------------------------------------------------

func TestSearchJSON_AvailableToolsPrompt(t *testing.T) {
	tools := []*indexableToolFunc{
		newIndexableTool(
			"tool1", "First tool", "DomainA",
			[]string{"cat1"}, nil,
			func(_ context.Context, _ map[string]any) (string, error) {
				return "ok", nil
			},
		),
		newIndexableTool(
			"tool2", "Second tool", "DomainA",
			[]string{"cat2"}, nil,
			func(_ context.Context, _ map[string]any) (string, error) {
				return "ok", nil
			},
		),
		newIndexableTool(
			"tool3", "Third tool", "DomainB",
			[]string{"cat3"}, nil,
			func(_ context.Context, _ map[string]any) (string, error) {
				return "ok", nil
			},
		),
	}

	eng1 := &mockSearchEngine{
		id: "bm25", guidance: "natural language",
	}
	eng2 := &mockSearchEngine{
		id: "regex", guidance: "regex patterns",
	}

	tc := setupSearchJSON(
		tools, []gent.SearchEngine{eng1, eng2},
	)
	prompt := tc.AvailableToolsPrompt()

	t.Run("shows only search tool", func(t *testing.T) {
		assert.Contains(t, prompt, "tool_registry_search")
		// Should NOT contain individual tools
		assert.NotContains(t, prompt, "\n- tool1:")
		assert.NotContains(t, prompt, "\n- tool2:")
		assert.NotContains(t, prompt, "\n- tool3:")
	})

	t.Run("includes domain summary", func(t *testing.T) {
		assert.Contains(t, prompt, "DomainA")
		assert.Contains(t, prompt, "DomainB")
	})

	t.Run("includes engine IDs in query_type enum",
		func(t *testing.T) {
			assert.Contains(t, prompt, `"bm25"`)
			assert.Contains(t, prompt, `"regex"`)
		},
	)

	t.Run("includes per-engine search guidance",
		func(t *testing.T) {
			assert.Contains(
				t, prompt, "bm25: natural language",
			)
			assert.Contains(
				t, prompt, "regex: regex patterns",
			)
		},
	)

	t.Run("includes correct total tool count",
		func(t *testing.T) {
			assert.Contains(t, prompt, "3 tools across")
		},
	)

	t.Run("schema has required properties",
		func(t *testing.T) {
			assert.Contains(t, prompt, `"query"`)
			assert.Contains(t, prompt, `"query_type"`)
			assert.Contains(t, prompt, `"page"`)
		},
	)
}

func TestSearchJSON_AvailableToolsPrompt_SimpleList(
	t *testing.T,
) {
	tools := []*indexableToolFunc{
		newIndexableTool(
			"search_customer", "Search customer",
			"Customer",
			[]string{"lookup"}, nil,
			func(
				_ context.Context,
				_ map[string]any,
			) (string, error) {
				return "ok", nil
			},
		),
		newIndexableTool(
			"get_policy", "Get policy",
			"Policy",
			[]string{"lookup"}, nil,
			func(
				_ context.Context,
				_ map[string]any,
			) (string, error) {
				return "ok", nil
			},
		),
		newIndexableTool(
			"send_email", "Send email",
			"Communication",
			[]string{"email"}, nil,
			func(
				_ context.Context,
				_ map[string]any,
			) (string, error) {
				return "ok", nil
			},
		),
	}

	eng := &mockSearchEngine{
		id: "bm25", guidance: "natural language",
	}

	tc := NewSearchJSON(SearchHintSimpleList)
	tc.RegisterEngine(eng)
	for _, tool := range tools {
		tc.RegisterTool(tool)
	}
	err := tc.Initialize()
	require.NoError(t, err)

	prompt := tc.AvailableToolsPrompt()

	t.Run("lists all tool names",
		func(t *testing.T) {
			assert.Contains(
				t, prompt, "  - search_customer\n",
			)
			assert.Contains(
				t, prompt, "  - get_policy\n",
			)
			assert.Contains(
				t, prompt, "  - send_email\n",
			)
		},
	)

	t.Run("does not contain domain summary",
		func(t *testing.T) {
			assert.NotContains(
				t, prompt, "Domains:",
			)
			assert.NotContains(
				t, prompt, "Customer (lookup)",
			)
		},
	)

	t.Run("includes correct total tool count",
		func(t *testing.T) {
			assert.Contains(
				t, prompt, "3 tools:",
			)
		},
	)

	t.Run("includes search tool name",
		func(t *testing.T) {
			assert.Contains(
				t, prompt, "tool_registry_search",
			)
		},
	)

	t.Run("includes engine guidance",
		func(t *testing.T) {
			assert.Contains(
				t, prompt,
				"bm25: natural language",
			)
		},
	)
}

// -------------------------------------------------------
// Pin Tests
// -------------------------------------------------------

func TestSearchJSON_Pin_DomainCategories(
	t *testing.T,
) {
	okFn := func(
		_ context.Context,
		_ map[string]any,
	) (string, error) {
		return "ok", nil
	}
	tools := []*indexableToolFunc{
		newIndexableToolWithSchema(
			"search_policy",
			"Search company policies",
			"Policy",
			[]string{"lookup", "policy"},
			[]string{"policy", "guidance"},
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"keyword": map[string]any{
						"type": "string",
					},
				},
				"required": []string{"keyword"},
			},
			okFn,
		),
		newIndexableTool(
			"lookup_customer",
			"Lookup customer by email",
			"Customer",
			[]string{"lookup", "customer"},
			nil, okFn,
		),
		newIndexableTool(
			"get_order", "Get order details",
			"Orders",
			[]string{"lookup", "orders"},
			nil, okFn,
		),
	}

	eng := &mockSearchEngine{
		id:       "bm25",
		guidance: "natural language",
	}

	tc := NewSearchJSON(SearchHintDomainCategories)
	tc.RegisterEngine(eng)
	for _, tool := range tools {
		tc.RegisterTool(tool)
	}
	tc.Pin("search_policy")
	err := tc.Initialize()
	require.NoError(t, err)

	prompt := tc.AvailableToolsPrompt()

	t.Run("shows pinned tool definition",
		func(t *testing.T) {
			assert.Contains(
				t, prompt,
				"- search_policy: "+
					"Search company policies\n",
			)
		},
	)

	t.Run("shows pinned tool schema",
		func(t *testing.T) {
			assert.Contains(
				t, prompt, `"keyword"`,
			)
		},
	)

	t.Run("does not show unpinned tools",
		func(t *testing.T) {
			assert.NotContains(
				t, prompt,
				"- lookup_customer:",
			)
			assert.NotContains(
				t, prompt,
				"- get_order:",
			)
		},
	)

	t.Run("changes guidance text when pinned",
		func(t *testing.T) {
			assert.Contains(
				t, prompt,
				"Some tools are pinned below.",
			)
			assert.Contains(
				t, prompt,
				"Use tool_registry_search "+
					"to discover more.",
			)
			assert.NotContains(
				t, prompt,
				"Use tool_registry_search "+
					"for tool discovery.",
			)
		},
	)

	t.Run("still shows domain summary",
		func(t *testing.T) {
			assert.Contains(t, prompt, "Policy")
			assert.Contains(t, prompt, "Customer")
			assert.Contains(t, prompt, "Orders")
		},
	)

	t.Run("includes total tool count",
		func(t *testing.T) {
			assert.Contains(
				t, prompt, "3 tools across",
			)
		},
	)
}

func TestSearchJSON_Pin_SimpleList(t *testing.T) {
	okFn := func(
		_ context.Context,
		_ map[string]any,
	) (string, error) {
		return "ok", nil
	}
	tools := []*indexableToolFunc{
		newIndexableToolWithSchema(
			"search_policy",
			"Search company policies",
			"Policy",
			[]string{"lookup"},
			nil,
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"keyword": map[string]any{
						"type": "string",
					},
				},
				"required": []string{"keyword"},
			},
			okFn,
		),
		newIndexableTool(
			"lookup_customer",
			"Lookup customer",
			"Customer",
			[]string{"lookup"}, nil, okFn,
		),
	}

	eng := &mockSearchEngine{
		id:       "bm25",
		guidance: "natural language",
	}

	tc := NewSearchJSON(SearchHintSimpleList)
	tc.RegisterEngine(eng)
	for _, tool := range tools {
		tc.RegisterTool(tool)
	}
	tc.Pin("search_policy")
	err := tc.Initialize()
	require.NoError(t, err)

	prompt := tc.AvailableToolsPrompt()

	t.Run("shows pinned tool definition",
		func(t *testing.T) {
			assert.Contains(
				t, prompt,
				"- search_policy: "+
					"Search company policies\n",
			)
		},
	)

	t.Run("changes guidance text when pinned",
		func(t *testing.T) {
			assert.Contains(
				t, prompt,
				"Some tools are pinned below.",
			)
			assert.Contains(
				t, prompt,
				"Use tool_registry_search "+
					"to get other tool details.",
			)
			assert.NotContains(
				t, prompt,
				"Use tool_registry_search "+
					"to get tool details "+
					"before calling.",
			)
		},
	)

	t.Run("still lists all tool names",
		func(t *testing.T) {
			assert.Contains(
				t, prompt, "  - search_policy\n",
			)
			assert.Contains(
				t, prompt,
				"  - lookup_customer\n",
			)
		},
	)
}

func TestSearchJSON_Pin_NoPins(t *testing.T) {
	okFn := func(
		_ context.Context,
		_ map[string]any,
	) (string, error) {
		return "ok", nil
	}

	eng := &mockSearchEngine{
		id:       "bm25",
		guidance: "natural language",
	}

	t.Run("domain categories without pins",
		func(t *testing.T) {
			tc := NewSearchJSON(
				SearchHintDomainCategories,
			)
			tc.RegisterEngine(eng)
			tc.RegisterTool(newIndexableTool(
				"tool1", "Tool 1", "D",
				nil, nil, okFn,
			))
			err := tc.Initialize()
			require.NoError(t, err)

			prompt := tc.AvailableToolsPrompt()
			assert.Contains(
				t, prompt,
				"Use tool_registry_search "+
					"for tool discovery.",
			)
			assert.NotContains(
				t, prompt,
				"pinned below",
			)
		},
	)

	t.Run("simple list without pins",
		func(t *testing.T) {
			tc := NewSearchJSON(SearchHintSimpleList)
			tc.RegisterEngine(eng)
			tc.RegisterTool(newIndexableTool(
				"tool1", "Tool 1", "D",
				nil, nil, okFn,
			))
			err := tc.Initialize()
			require.NoError(t, err)

			prompt := tc.AvailableToolsPrompt()
			assert.Contains(
				t, prompt,
				"Use tool_registry_search "+
					"to get tool details "+
					"before calling.",
			)
			assert.NotContains(
				t, prompt,
				"pinned below",
			)
		},
	)
}

func TestSearchJSON_Pin_InvalidTool(t *testing.T) {
	eng := &mockSearchEngine{
		id:       "bm25",
		guidance: "natural language",
	}

	tc := NewSearchJSON(SearchHintDomainCategories)
	tc.RegisterEngine(eng)
	tc.RegisterTool(newIndexableTool(
		"tool1", "Tool 1", "D", nil, nil,
		func(
			_ context.Context,
			_ map[string]any,
		) (string, error) {
			return "ok", nil
		},
	))
	tc.Pin("nonexistent_tool")

	err := tc.Initialize()
	assert.Error(t, err)
	assert.Contains(
		t, err.Error(),
		"pinned tool \"nonexistent_tool\" "+
			"is not registered",
	)
}

func TestSearchJSON_Pin_MultiplePins(t *testing.T) {
	okFn := func(
		_ context.Context,
		_ map[string]any,
	) (string, error) {
		return "ok", nil
	}

	eng := &mockSearchEngine{
		id:       "bm25",
		guidance: "natural language",
	}

	tc := NewSearchJSON(SearchHintDomainCategories)
	tc.RegisterEngine(eng)
	tc.RegisterTool(newIndexableTool(
		"tool_a", "Tool A", "D",
		nil, nil, okFn,
	))
	tc.RegisterTool(newIndexableTool(
		"tool_b", "Tool B", "D",
		nil, nil, okFn,
	))
	tc.RegisterTool(newIndexableTool(
		"tool_c", "Tool C", "D",
		nil, nil, okFn,
	))
	tc.Pin("tool_a")
	tc.Pin("tool_c")

	err := tc.Initialize()
	require.NoError(t, err)

	prompt := tc.AvailableToolsPrompt()

	t.Run("shows both pinned tools",
		func(t *testing.T) {
			assert.Contains(
				t, prompt, "- tool_a: Tool A\n",
			)
			assert.Contains(
				t, prompt, "- tool_c: Tool C\n",
			)
		},
	)

	t.Run("does not show unpinned tool",
		func(t *testing.T) {
			assert.NotContains(
				t, prompt, "- tool_b:",
			)
		},
	)
}

func TestSearchJSON_Pin_ToolStillSearchable(
	t *testing.T,
) {
	okFn := func(
		_ context.Context,
		_ map[string]any,
	) (string, error) {
		return "ok", nil
	}

	eng := &mockSearchEngine{
		id:       "bm25",
		guidance: "natural language",
		searchFn: func(
			_ context.Context, _ string,
		) ([]string, error) {
			return []string{"pinned_tool"}, nil
		},
	}

	tc := NewSearchJSON(SearchHintDomainCategories)
	tc.RegisterEngine(eng)
	tc.RegisterTool(newIndexableTool(
		"pinned_tool", "A pinned tool", "D",
		nil, nil, okFn,
	))
	tc.Pin("pinned_tool")

	err := tc.Initialize()
	require.NoError(t, err)

	// Pinned tool should still be found via search
	result, err := tc.Execute(
		newExecCtx(),
		`{"tool":"tool_registry_search",`+
			`"args":{"query":"pinned",`+
			`"query_type":"bm25"}}`,
		searchTestFormat(),
	)
	require.NoError(t, err)
	assert.Contains(
		t, result.Text, "pinned_tool",
	)
}

// -------------------------------------------------------
// ParseSection Tests
// -------------------------------------------------------

func TestSearchJSON_ParseSection(t *testing.T) {
	type input struct {
		content string
	}

	type expected struct {
		calls []*gent.ToolCall
		err   error
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "valid single JSON call",
			input: input{
				content: `{"tool": "search", ` +
					`"args": {"query": "test"}}`,
			},
			expected: expected{
				calls: []*gent.ToolCall{
					{
						Name: "search",
						Args: map[string]any{
							"query": "test",
						},
					},
				},
			},
		},
		{
			name: "valid array of calls",
			input: input{
				content: `[` +
					`{"tool": "a", "args": {}},` +
					`{"tool": "b", "args": {}}]`,
			},
			expected: expected{
				calls: []*gent.ToolCall{
					{Name: "a", Args: map[string]any{}},
					{Name: "b", Args: map[string]any{}},
				},
			},
		},
		{
			name: "invalid JSON",
			input: input{
				content: `{invalid}`,
			},
			expected: expected{
				err: gent.ErrInvalidJSON,
			},
		},
		{
			name: "missing tool name",
			input: input{
				content: `{"args": {"q": "test"}}`,
			},
			expected: expected{
				err: gent.ErrMissingToolName,
			},
		},
		{
			name: "empty content returns empty slice",
			input: input{
				content: "",
			},
			expected: expected{
				calls: []*gent.ToolCall{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewSearchJSON(SearchHintDomainCategories)
			result, err := tc.ParseSection(
				nil, tt.input.content,
			)

			if tt.expected.err != nil {
				assert.ErrorIs(t, err, tt.expected.err)
				return
			}

			require.NoError(t, err)
			calls := result.([]*gent.ToolCall)
			assert.Len(
				t, calls, len(tt.expected.calls),
			)
			for i, exp := range tt.expected.calls {
				assert.Equal(t, exp.Name, calls[i].Name)
				assert.Equal(t, exp.Args, calls[i].Args)
			}
		})
	}
}

func TestSearchJSON_ParseSection_Events(t *testing.T) {
	t.Run("parse error publishes event",
		func(t *testing.T) {
			tc := NewSearchJSON(SearchHintDomainCategories)
			execCtx := newExecCtx()

			_, err := tc.ParseSection(
				execCtx, "{invalid}",
			)
			assert.Error(t, err)

			counter := execCtx.Stats().GetCounter(
				gent.SCToolchainParseErrorTotal,
			)
			assert.Equal(t, int64(1), counter)

			gauge := execCtx.Stats().GetGauge(
				gent.SGToolchainParseErrorConsecutive,
			)
			assert.Equal(t, float64(1), gauge)
		},
	)

	t.Run("successful parse resets consecutive gauge",
		func(t *testing.T) {
			tc := NewSearchJSON(SearchHintDomainCategories)
			execCtx := newExecCtx()

			// First: cause an error
			_, _ = tc.ParseSection(
				execCtx, "{invalid}",
			)
			gauge := execCtx.Stats().GetGauge(
				gent.SGToolchainParseErrorConsecutive,
			)
			assert.Equal(t, float64(1), gauge)

			// Then: successful parse
			_, err := tc.ParseSection(
				execCtx,
				`{"tool": "x", "args": {}}`,
			)
			require.NoError(t, err)

			gauge = execCtx.Stats().GetGauge(
				gent.SGToolchainParseErrorConsecutive,
			)
			assert.Equal(t, float64(0), gauge)
		},
	)
}

// -------------------------------------------------------
// Execute — Search Tool Tests
// -------------------------------------------------------

func TestSearchJSON_Execute_Search(t *testing.T) {
	okFn := func(_ context.Context, _ map[string]any) (string, error) {
		return "ok", nil
	}

	tools := []*indexableToolFunc{
		newIndexableToolWithSchema(
			"order_lookup",
			"Look up order details",
			"Orders",
			[]string{"lookup"},
			[]string{"order"},
			schema.Object(map[string]*schema.Property{
				"id": schema.String("Order ID"),
			}, "id"),
			okFn,
		),
		newIndexableTool(
			"send_email",
			"Send email to customer",
			"Communication",
			[]string{"email"},
			[]string{"email", "send"},
			okFn,
		),
		newIndexableTool(
			"check_inventory",
			"Check inventory levels",
			"Inventory",
			[]string{"lookup"},
			[]string{"stock", "inventory"},
			okFn,
		),
		newIndexableTool(
			"billing_summary",
			"Get billing summary",
			"Billing",
			[]string{"billing"},
			[]string{"billing", "invoice"},
			okFn,
		),
	}

	mockEng := &mockSearchEngine{
		id:       "mock",
		guidance: "mock search",
		searchFn: func(
			_ context.Context, query string,
		) ([]string, error) {
			// Return tools matching by simple contain
			var results []string
			for _, tool := range tools {
				if strings.Contains(
					tool.Name(), query,
				) || strings.Contains(
					query, "all",
				) {
					results = append(
						results, tool.Name(),
					)
				}
			}
			return results, nil
		},
	}

	tc := setupSearchJSON(
		tools, []gent.SearchEngine{mockEng},
	)

	t.Run("search returns results with full definitions",
		func(t *testing.T) {
			content := `{"tool": "tool_registry_search", ` +
				`"args": {"query": "order", ` +
				`"query_type": "mock"}}`
			result, err := tc.Execute(
				nil, content, searchTestFormat(),
			)
			require.NoError(t, err)
			assert.Contains(
				t, result.Text, "order_lookup",
			)
			assert.Contains(
				t, result.Text,
				"Look up order details",
			)
		},
	)

	t.Run("pagination page 1",
		func(t *testing.T) {
			content := `{"tool": "tool_registry_search", ` +
				`"args": {"query": "all", ` +
				`"query_type": "mock"}}`
			result, err := tc.Execute(
				nil, content, searchTestFormat(),
			)
			require.NoError(t, err)
			// Default page size is 3, so page 1 shows 3
			assert.Contains(
				t, result.Text, "order_lookup",
			)
			assert.Contains(
				t, result.Text, "send_email",
			)
			assert.Contains(
				t, result.Text, "check_inventory",
			)
			assert.NotContains(
				t, result.Text, "billing_summary",
			)
			assert.Contains(
				t, result.Text, "page 1 of 2",
			)
			assert.Contains(
				t, result.Text, "4 total results",
			)
		},
	)

	t.Run("pagination page 2",
		func(t *testing.T) {
			content := `{"tool": "tool_registry_search", ` +
				`"args": {"query": "all", ` +
				`"query_type": "mock", "page": 2}}`
			result, err := tc.Execute(
				nil, content, searchTestFormat(),
			)
			require.NoError(t, err)
			assert.Contains(
				t, result.Text, "billing_summary",
			)
			assert.NotContains(
				t, result.Text, "order_lookup",
			)
			assert.Contains(
				t, result.Text, "page 2 of 2",
			)
		},
	)

	t.Run("page beyond results returns no-results",
		func(t *testing.T) {
			content := `{"tool": "tool_registry_search", ` +
				`"args": {"query": "order", ` +
				`"query_type": "mock", "page": 99}}`
			result, err := tc.Execute(
				nil, content, searchTestFormat(),
			)
			require.NoError(t, err)
			assert.Contains(
				t, result.Text, "No tools found",
			)
		},
	)

	t.Run("zero results returns no-results message",
		func(t *testing.T) {
			content := `{"tool": "tool_registry_search", ` +
				`"args": {"query": "zzzzz", ` +
				`"query_type": "mock"}}`
			result, err := tc.Execute(
				nil, content, searchTestFormat(),
			)
			require.NoError(t, err)
			assert.Contains(
				t, result.Text, "No tools found",
			)
		},
	)

	t.Run("invalid query_type returns error",
		func(t *testing.T) {
			content := `{"tool": "tool_registry_search", ` +
				`"args": {"query": "test", ` +
				`"query_type": "unknown_engine"}}`
			result, err := tc.Execute(
				nil, content, searchTestFormat(),
			)
			// Schema validation should catch this
			require.NoError(t, err)
			assert.Contains(t, result.Text, "Error")
		},
	)

	t.Run("engine returns error surfaced to LLM",
		func(t *testing.T) {
			errEng := &mockSearchEngine{
				id:       "err_eng",
				guidance: "err",
				searchFn: func(
					_ context.Context, _ string,
				) ([]string, error) {
					return nil, errors.New(
						"search service down",
					)
				},
			}
			tcErr := NewSearchJSON(SearchHintDomainCategories).
				RegisterEngine(errEng)
			tool := newIndexableTool(
				"t1", "d", "D", nil, nil, okFn,
			)
			tcErr.RegisterTool(tool)
			require.NoError(t, tcErr.Initialize())

			content := `{"tool": "tool_registry_search", ` +
				`"args": {"query": "test", ` +
				`"query_type": "err_eng"}}`
			result, err := tcErr.Execute(
				nil, content, searchTestFormat(),
			)
			require.NoError(t, err)
			assert.Contains(
				t, result.Text, "search service down",
			)
		},
	)

	t.Run("multiple parallel searches",
		func(t *testing.T) {
			content := `[` +
				`{"tool": "tool_registry_search", ` +
				`"args": {"query": "order", ` +
				`"query_type": "mock"}},` +
				`{"tool": "tool_registry_search", ` +
				`"args": {"query": "email", ` +
				`"query_type": "mock"}}]`
			result, err := tc.Execute(
				nil, content, searchTestFormat(),
			)
			require.NoError(t, err)
			assert.Contains(
				t, result.Text, "order_lookup",
			)
			assert.Contains(
				t, result.Text, "send_email",
			)
		},
	)
}

// -------------------------------------------------------
// Execute — Regular Tool Tests
// -------------------------------------------------------

func TestSearchJSON_Execute_RegularTool(t *testing.T) {
	t.Run("tool call succeeds with typed result",
		func(t *testing.T) {
			tool := newIndexableTool(
				"greet", "Greet user", "General",
				nil, nil,
				func(_ context.Context, args map[string]any) (string, error) {
					name := args["name"].(string)
					return fmt.Sprintf(
						"Hello, %s!", name,
					), nil
				},
			)
			eng := &mockSearchEngine{
				id: "m", guidance: "g",
			}
			tc := setupSearchJSON(
				[]*indexableToolFunc{tool},
				[]gent.SearchEngine{eng},
			)

			content := `{"tool": "greet", ` +
				`"args": {"name": "Alice"}}`
			result, err := tc.Execute(
				nil, content, searchTestFormat(),
			)
			require.NoError(t, err)
			assert.Contains(
				t, result.Text, "Hello, Alice!",
			)
			assert.Equal(
				t, "greet", result.Raw.Calls[0].Name,
			)
		},
	)

	t.Run("unknown tool returns error", func(t *testing.T) {
		eng := &mockSearchEngine{
			id: "m", guidance: "g",
		}
		tool := newIndexableTool(
			"existing", "d", "D", nil, nil,
			func(_ context.Context, _ map[string]any) (string, error) {
				return "ok", nil
			},
		)
		tc := setupSearchJSON(
			[]*indexableToolFunc{tool},
			[]gent.SearchEngine{eng},
		)

		content := `{"tool": "nonexistent", "args": {}}`
		result, err := tc.Execute(
			nil, content, searchTestFormat(),
		)
		require.NoError(t, err)
		assert.ErrorIs(
			t, result.Raw.Errors[0], gent.ErrUnknownTool,
		)
	})

	t.Run("schema validation fails", func(t *testing.T) {
		tool := newIndexableToolWithSchema(
			"strict_tool", "Strict", "D",
			nil, nil,
			schema.Object(
				map[string]*schema.Property{
					"required_field": schema.String(
						"Required",
					),
				},
				"required_field",
			),
			func(_ context.Context, _ map[string]any) (string, error) {
				return "ok", nil
			},
		)
		eng := &mockSearchEngine{
			id: "m", guidance: "g",
		}
		tc := setupSearchJSON(
			[]*indexableToolFunc{tool},
			[]gent.SearchEngine{eng},
		)

		content := `{"tool": "strict_tool", "args": {}}`
		result, err := tc.Execute(
			nil, content, searchTestFormat(),
		)
		require.NoError(t, err)
		assert.Error(t, result.Raw.Errors[0])
		assert.Contains(
			t, result.Text, "Error",
		)
	})

	t.Run("tool Call returns error", func(t *testing.T) {
		tool := newIndexableTool(
			"failing", "Fails", "D", nil, nil,
			func(_ context.Context, _ map[string]any) (string, error) {
				return "", errors.New("tool broke")
			},
		)
		eng := &mockSearchEngine{
			id: "m", guidance: "g",
		}
		tc := setupSearchJSON(
			[]*indexableToolFunc{tool},
			[]gent.SearchEngine{eng},
		)

		content := `{"tool": "failing", "args": {}}`
		result, err := tc.Execute(
			nil, content, searchTestFormat(),
		)
		require.NoError(t, err)
		assert.Error(t, result.Raw.Errors[0])
		assert.Contains(t, result.Text, "tool broke")
	})

	t.Run("tool with instructions creates nested sections",
		func(t *testing.T) {
			// We need a tool that returns instructions
			// Use a custom tool struct for this
			instructionTool := &instructionToolImpl{
				indexableToolFunc: newIndexableTool(
					"with_inst", "Has instructions",
					"D", nil, nil, nil,
				),
			}
			eng := &mockSearchEngine{
				id: "m", guidance: "g",
			}
			tc := NewSearchJSON(SearchHintDomainCategories).RegisterEngine(eng)
			tc.RegisterTool(instructionTool)
			require.NoError(t, tc.Initialize())

			content := `{"tool": "with_inst", "args": {}}`
			result, err := tc.Execute(
				nil, content, searchTestFormat(),
			)
			require.NoError(t, err)
			assert.Contains(
				t, result.Text, "instructions",
			)
			assert.Contains(
				t, result.Text, "Follow these steps",
			)
		},
	)
}

// instructionToolImpl is a tool that returns instructions.
type instructionToolImpl struct {
	*indexableToolFunc
}

func (t *instructionToolImpl) Call(
	_ context.Context,
	_ map[string]any,
) (*gent.ToolResult[string], error) {
	return &gent.ToolResult[string]{
		Text:         "tool output",
		Instructions: "Follow these steps",
	}, nil
}

// -------------------------------------------------------
// Execute — Mixed Parallel Calls Tests
// -------------------------------------------------------

func TestSearchJSON_Execute_Mixed(t *testing.T) {
	okFn := func(_ context.Context, args map[string]any) (string, error) {
		return "regular_result", nil
	}

	tools := []*indexableToolFunc{
		newIndexableTool(
			"regular_tool", "A regular tool",
			"General", nil, nil, okFn,
		),
		newIndexableTool(
			"another_tool", "Another tool",
			"General", nil, nil, okFn,
		),
	}

	eng := &mockSearchEngine{
		id:       "mock",
		guidance: "g",
		searchFn: func(
			_ context.Context, _ string,
		) ([]string, error) {
			return []string{"regular_tool"}, nil
		},
	}

	tc := setupSearchJSON(
		tools, []gent.SearchEngine{eng},
	)

	t.Run("search + regular tool in same array",
		func(t *testing.T) {
			content := `[` +
				`{"tool": "tool_registry_search", ` +
				`"args": {"query": "test", ` +
				`"query_type": "mock"}},` +
				`{"tool": "regular_tool", "args": {}}]`
			result, err := tc.Execute(
				nil, content, searchTestFormat(),
			)
			require.NoError(t, err)
			assert.Len(t, result.Raw.Calls, 2)
			// Search result
			assert.Contains(
				t, result.Text, "regular_tool",
			)
			// Regular tool result
			assert.Contains(
				t, result.Text, "regular_result",
			)
		},
	)

	t.Run("two searches + regular tool",
		func(t *testing.T) {
			content := `[` +
				`{"tool": "tool_registry_search", ` +
				`"args": {"query": "q1", ` +
				`"query_type": "mock"}},` +
				`{"tool": "tool_registry_search", ` +
				`"args": {"query": "q2", ` +
				`"query_type": "mock"}},` +
				`{"tool": "another_tool", "args": {}}]`
			result, err := tc.Execute(
				nil, content, searchTestFormat(),
			)
			require.NoError(t, err)
			assert.Len(t, result.Raw.Calls, 3)
		},
	)
}

// -------------------------------------------------------
// Execute — Events & Stats Tests
// -------------------------------------------------------

func TestSearchJSON_Execute_Stats(t *testing.T) {
	okFn := func(_ context.Context, _ map[string]any) (string, error) {
		return "ok", nil
	}

	t.Run("search call increments tool call stats",
		func(t *testing.T) {
			eng := &mockSearchEngine{
				id:       "mock",
				guidance: "g",
				searchFn: func(
					_ context.Context, _ string,
				) ([]string, error) {
					return []string{}, nil
				},
			}
			tool := newIndexableTool(
				"t", "d", "D", nil, nil, okFn,
			)
			tc := setupSearchJSON(
				[]*indexableToolFunc{tool},
				[]gent.SearchEngine{eng},
			)

			execCtx := newExecCtx()
			content := `{"tool": "tool_registry_search", ` +
				`"args": {"query": "x", ` +
				`"query_type": "mock"}}`
			_, err := tc.Execute(
				execCtx, content, searchTestFormat(),
			)
			require.NoError(t, err)

			assert.Equal(
				t, int64(1),
				execCtx.Stats().GetCounter(
					gent.SCToolCalls,
				),
			)
			assert.Equal(
				t, int64(1),
				execCtx.Stats().GetCounter(
					gent.SCToolCallsFor+
						"tool_registry_search",
				),
			)
		},
	)

	t.Run("regular tool call increments stats",
		func(t *testing.T) {
			eng := &mockSearchEngine{
				id: "m", guidance: "g",
			}
			tool := newIndexableTool(
				"my_tool", "d", "D", nil, nil, okFn,
			)
			tc := setupSearchJSON(
				[]*indexableToolFunc{tool},
				[]gent.SearchEngine{eng},
			)

			execCtx := newExecCtx()
			content := `{"tool": "my_tool", "args": {}}`
			_, err := tc.Execute(
				execCtx, content, searchTestFormat(),
			)
			require.NoError(t, err)

			assert.Equal(
				t, int64(1),
				execCtx.Stats().GetCounter(
					gent.SCToolCalls,
				),
			)
			assert.Equal(
				t, int64(1),
				execCtx.Stats().GetCounter(
					gent.SCToolCallsFor+"my_tool",
				),
			)
		},
	)

	t.Run("search error increments error stats",
		func(t *testing.T) {
			eng := &mockSearchEngine{
				id:       "mock",
				guidance: "g",
				searchFn: func(
					_ context.Context, _ string,
				) ([]string, error) {
					return nil, errors.New("fail")
				},
			}
			tool := newIndexableTool(
				"t", "d", "D", nil, nil, okFn,
			)
			tc := setupSearchJSON(
				[]*indexableToolFunc{tool},
				[]gent.SearchEngine{eng},
			)

			execCtx := newExecCtx()
			content := `{"tool": "tool_registry_search", ` +
				`"args": {"query": "x", ` +
				`"query_type": "mock"}}`
			_, err := tc.Execute(
				execCtx, content, searchTestFormat(),
			)
			require.NoError(t, err)

			assert.Equal(
				t, int64(1),
				execCtx.Stats().GetCounter(
					gent.SCToolCallsErrorTotal,
				),
			)
			assert.Equal(
				t, float64(1),
				execCtx.Stats().GetGauge(
					gent.SGToolCallsErrorConsecutive,
				),
			)
		},
	)

	t.Run("successful search resets consecutive gauges",
		func(t *testing.T) {
			callCount := 0
			eng := &mockSearchEngine{
				id:       "mock",
				guidance: "g",
				searchFn: func(
					_ context.Context, _ string,
				) ([]string, error) {
					callCount++
					if callCount == 1 {
						return nil, errors.New("fail")
					}
					return []string{}, nil
				},
			}
			tool := newIndexableTool(
				"t", "d", "D", nil, nil, okFn,
			)
			tc := setupSearchJSON(
				[]*indexableToolFunc{tool},
				[]gent.SearchEngine{eng},
			)

			execCtx := newExecCtx()

			// First: error
			content := `{"tool": "tool_registry_search", ` +
				`"args": {"query": "x", ` +
				`"query_type": "mock"}}`
			_, _ = tc.Execute(
				execCtx, content, searchTestFormat(),
			)
			assert.Equal(
				t, float64(1),
				execCtx.Stats().GetGauge(
					gent.SGToolCallsErrorConsecutive,
				),
			)

			// Second: success
			_, err := tc.Execute(
				execCtx, content, searchTestFormat(),
			)
			require.NoError(t, err)
			assert.Equal(
				t, float64(0),
				execCtx.Stats().GetGauge(
					gent.SGToolCallsErrorConsecutive,
				),
			)
		},
	)

	t.Run(
		"successful regular tool resets consecutive gauges",
		func(t *testing.T) {
			eng := &mockSearchEngine{
				id: "m", guidance: "g",
			}
			failTool := newIndexableTool(
				"fail_tool", "d", "D", nil, nil,
				func(_ context.Context, _ map[string]any) (string, error) {
					return "", errors.New("fail")
				},
			)
			okTool := newIndexableTool(
				"ok_tool", "d", "D", nil, nil, okFn,
			)
			tc := setupSearchJSON(
				[]*indexableToolFunc{failTool, okTool},
				[]gent.SearchEngine{eng},
			)

			execCtx := newExecCtx()

			// First: error
			content := `{"tool": "fail_tool", "args": {}}`
			_, _ = tc.Execute(
				execCtx, content, searchTestFormat(),
			)
			assert.Equal(
				t, float64(1),
				execCtx.Stats().GetGauge(
					gent.SGToolCallsErrorConsecutive,
				),
			)

			// Then: success
			content = `{"tool": "ok_tool", "args": {}}`
			_, err := tc.Execute(
				execCtx, content, searchTestFormat(),
			)
			require.NoError(t, err)
			assert.Equal(
				t, float64(0),
				execCtx.Stats().GetGauge(
					gent.SGToolCallsErrorConsecutive,
				),
			)
		},
	)
}

// -------------------------------------------------------
// Execute — Pagination Details Tests
// -------------------------------------------------------

func TestSearchJSON_Execute_Pagination(t *testing.T) {
	okFn := func(_ context.Context, _ map[string]any) (string, error) {
		return "ok", nil
	}

	// Create 7 tools for pagination testing
	var tools []*indexableToolFunc
	for i := range 7 {
		name := fmt.Sprintf("tool_%d", i)
		tools = append(tools, newIndexableTool(
			name, fmt.Sprintf("Tool %d desc", i),
			"Domain", nil, nil, okFn,
		))
	}

	allNames := make([]string, 7)
	for i := range 7 {
		allNames[i] = fmt.Sprintf("tool_%d", i)
	}

	eng := &mockSearchEngine{
		id:       "mock",
		guidance: "g",
		searchFn: func(
			_ context.Context, _ string,
		) ([]string, error) {
			return allNames, nil
		},
	}

	tc := setupSearchJSON(
		tools, []gent.SearchEngine{eng},
	)

	t.Run("page 1 shows first 3 tools + pagination info",
		func(t *testing.T) {
			content := `{"tool": "tool_registry_search", ` +
				`"args": {"query": "x", ` +
				`"query_type": "mock"}}`
			result, err := tc.Execute(
				nil, content, searchTestFormat(),
			)
			require.NoError(t, err)
			assert.Contains(t, result.Text, "tool_0")
			assert.Contains(t, result.Text, "tool_1")
			assert.Contains(t, result.Text, "tool_2")
			assert.NotContains(t, result.Text, "tool_3")
			assert.Contains(
				t, result.Text,
				"Showing page 1 of 3 (7 total results)",
			)
		},
	)

	t.Run("page 2 shows next 3 tools", func(t *testing.T) {
		content := `{"tool": "tool_registry_search", ` +
			`"args": {"query": "x", ` +
			`"query_type": "mock", "page": 2}}`
		result, err := tc.Execute(
			nil, content, searchTestFormat(),
		)
		require.NoError(t, err)
		assert.Contains(t, result.Text, "tool_3")
		assert.Contains(t, result.Text, "tool_4")
		assert.Contains(t, result.Text, "tool_5")
		assert.NotContains(t, result.Text, "tool_6")
		assert.Contains(
			t, result.Text,
			"Showing page 2 of 3 (7 total results)",
		)
	})

	t.Run("last page shows remaining tools (partial page)",
		func(t *testing.T) {
			content := `{"tool": "tool_registry_search", ` +
				`"args": {"query": "x", ` +
				`"query_type": "mock", "page": 3}}`
			result, err := tc.Execute(
				nil, content, searchTestFormat(),
			)
			require.NoError(t, err)
			assert.Contains(t, result.Text, "tool_6")
			assert.NotContains(t, result.Text, "tool_5")
			assert.Contains(
				t, result.Text,
				"Showing page 3 of 3 (7 total results)",
			)
		},
	)

	t.Run("page 0 treated as page 1", func(t *testing.T) {
		content := `{"tool": "tool_registry_search", ` +
			`"args": {"query": "x", ` +
			`"query_type": "mock", "page": 0}}`
		result, err := tc.Execute(
			nil, content, searchTestFormat(),
		)
		require.NoError(t, err)
		assert.Contains(t, result.Text, "tool_0")
		assert.Contains(
			t, result.Text, "page 1 of 3",
		)
	})

	t.Run("page beyond total shows no-results",
		func(t *testing.T) {
			content := `{"tool": "tool_registry_search", ` +
				`"args": {"query": "x", ` +
				`"query_type": "mock", "page": 99}}`
			result, err := tc.Execute(
				nil, content, searchTestFormat(),
			)
			require.NoError(t, err)
			assert.Contains(
				t, result.Text, "No tools found",
			)
		},
	)
}

// -------------------------------------------------------
// Execute — Search Dedup Tests
// -------------------------------------------------------

func TestSearchJSON_Execute_SearchDedup(t *testing.T) {
	okFn := func(
		_ context.Context, _ map[string]any,
	) (string, error) {
		return "ok", nil
	}

	tools := []*indexableToolFunc{
		newIndexableToolWithSchema(
			"tool_a",
			"Tool A description",
			"Domain",
			[]string{"cat"},
			[]string{"a"},
			schema.Object(map[string]*schema.Property{
				"x": schema.String("X param"),
			}, "x"),
			okFn,
		),
		newIndexableToolWithSchema(
			"tool_b",
			"Tool B description",
			"Domain",
			[]string{"cat"},
			[]string{"b"},
			schema.Object(map[string]*schema.Property{
				"y": schema.String("Y param"),
			}, "y"),
			okFn,
		),
		newIndexableToolWithSchema(
			"tool_c",
			"Tool C description",
			"Domain",
			[]string{"cat"},
			[]string{"c"},
			schema.Object(map[string]*schema.Property{
				"z": schema.String("Z param"),
			}, "z"),
			okFn,
		),
	}

	type input struct {
		content  string
		searchFn func(
			ctx context.Context, query string,
		) ([]string, error)
	}

	type expected struct {
		// Tools whose full definition must appear
		fullDefTools []string
		// Tools that must appear as dedup reference
		dedupTools []string
		// Tools whose description must NOT appear
		// (to confirm dedup replaced full def)
		noDescTools []string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "overlapping parallel searches " +
				"dedup shared tool",
			input: input{
				// query1 returns [tool_a, tool_b]
				// query2 returns [tool_b, tool_c]
				// tool_b should be deduped in query2
				searchFn: func(
					_ context.Context, query string,
				) ([]string, error) {
					switch query {
					case "q1":
						return []string{
							"tool_a", "tool_b",
						}, nil
					case "q2":
						return []string{
							"tool_b", "tool_c",
						}, nil
					}
					return nil, nil
				},
				content: `[` +
					`{"tool": "tool_registry_search",` +
					` "args": {"query": "q1",` +
					` "query_type": "mock"}},` +
					`{"tool": "tool_registry_search",` +
					` "args": {"query": "q2",` +
					` "query_type": "mock"}}]`,
			},
			expected: expected{
				fullDefTools: []string{
					"tool_a", "tool_b", "tool_c",
				},
				dedupTools: []string{"tool_b"},
				noDescTools: []string{},
			},
		},
		{
			name: "single search has no dedup",
			input: input{
				searchFn: func(
					_ context.Context, _ string,
				) ([]string, error) {
					return []string{
						"tool_a", "tool_b",
					}, nil
				},
				content: `{"tool": ` +
					`"tool_registry_search",` +
					` "args": {"query": "q1",` +
					` "query_type": "mock"}}`,
			},
			expected: expected{
				fullDefTools: []string{
					"tool_a", "tool_b",
				},
				dedupTools:  []string{},
				noDescTools: []string{},
			},
		},
		{
			name: "regular tool call does not " +
				"interfere with search dedup",
			input: input{
				// query returns [tool_a, tool_b]
				// then regular call to tool_c
				// then query returns [tool_a, tool_c]
				// tool_a deduped, tool_c full (not
				// affected by regular call)
				searchFn: func(
					_ context.Context, query string,
				) ([]string, error) {
					switch query {
					case "first":
						return []string{
							"tool_a", "tool_b",
						}, nil
					case "second":
						return []string{
							"tool_a", "tool_c",
						}, nil
					}
					return nil, nil
				},
				content: `[` +
					`{"tool": "tool_registry_search",` +
					` "args": {"query": "first",` +
					` "query_type": "mock"}},` +
					`{"tool": "tool_c", "args": ` +
					`{"z": "val"}},` +
					`{"tool": "tool_registry_search",` +
					` "args": {"query": "second",` +
					` "query_type": "mock"}}]`,
			},
			expected: expected{
				fullDefTools: []string{
					"tool_a", "tool_b", "tool_c",
				},
				dedupTools:  []string{"tool_a"},
				noDescTools: []string{},
			},
		},
		{
			name: "all tools overlapping across " +
				"three searches",
			input: input{
				searchFn: func(
					_ context.Context, query string,
				) ([]string, error) {
					switch query {
					case "q1":
						return []string{
							"tool_a", "tool_b",
						}, nil
					case "q2":
						return []string{
							"tool_b", "tool_c",
						}, nil
					case "q3":
						return []string{
							"tool_a", "tool_b",
							"tool_c",
						}, nil
					}
					return nil, nil
				},
				content: `[` +
					`{"tool": "tool_registry_search",` +
					` "args": {"query": "q1",` +
					` "query_type": "mock"}},` +
					`{"tool": "tool_registry_search",` +
					` "args": {"query": "q2",` +
					` "query_type": "mock"}},` +
					`{"tool": "tool_registry_search",` +
					` "args": {"query": "q3",` +
					` "query_type": "mock"}}]`,
			},
			expected: expected{
				fullDefTools: []string{
					"tool_a", "tool_b", "tool_c",
				},
				dedupTools: []string{
					"tool_b",  // deduped in q2
					"tool_a",  // deduped in q3
					"tool_b",  // deduped in q3
					"tool_c",  // deduped in q3
				},
				noDescTools: []string{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eng := &mockSearchEngine{
				id:       "mock",
				guidance: "mock search",
				searchFn: tt.input.searchFn,
			}

			tc := setupSearchJSON(
				tools, []gent.SearchEngine{eng},
			)
			tc.WithPageSize(10) // avoid pagination

			result, err := tc.Execute(
				nil, tt.input.content,
				searchTestFormat(),
			)
			require.NoError(t, err)

			// Verify full definitions appear
			for _, name := range tt.expected.fullDefTools {
				assert.Contains(
					t, result.Text, name,
					"expected tool %q in output",
					name,
				)
			}

			// Verify dedup references appear
			dedupRef := func(name string) string {
				return fmt.Sprintf(
					"%s: (see definition above)",
					name,
				)
			}
			for _, name := range tt.expected.dedupTools {
				assert.Contains(
					t, result.Text,
					dedupRef(name),
					"expected dedup ref for %q",
					name,
				)
			}

			// For deduped tools, count occurrences of
			// the full description to verify it only
			// appears once.
			descMap := map[string]string{
				"tool_a": "Tool A description",
				"tool_b": "Tool B description",
				"tool_c": "Tool C description",
			}
			for _, name := range tt.expected.dedupTools {
				desc := descMap[name]
				count := strings.Count(
					result.Text, desc,
				)
				assert.Equal(
					t, 1, count,
					"description %q for tool %q "+
						"should appear exactly "+
						"once, got %d",
					desc, name, count,
				)
			}

			// Verify noDescTools descriptions are absent
			for _, name := range tt.expected.noDescTools {
				desc := descMap[name]
				assert.NotContains(
					t, result.Text, desc,
					"description for %q should "+
						"not appear", name,
				)
			}
		})
	}
}

// -------------------------------------------------------
// Thread Safety Tests
// -------------------------------------------------------

func TestSearchJSON_ConcurrentAccess(t *testing.T) {
	okFn := func(_ context.Context, _ map[string]any) (string, error) {
		return "ok", nil
	}

	tools := []*indexableToolFunc{
		newIndexableTool(
			"tool_a", "Tool A", "D",
			nil, nil, okFn,
		),
		newIndexableTool(
			"tool_b", "Tool B", "D",
			nil, nil, okFn,
		),
	}

	eng := &mockSearchEngine{
		id:       "mock",
		guidance: "g",
		searchFn: func(
			_ context.Context, _ string,
		) ([]string, error) {
			return []string{"tool_a"}, nil
		},
	}

	tc := setupSearchJSON(
		tools, []gent.SearchEngine{eng},
	)

	t.Run("concurrent Execute calls succeed",
		func(t *testing.T) {
			var wg sync.WaitGroup
			errs := make([]error, 10)

			for i := range 10 {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					content := `{"tool": "tool_a", ` +
						`"args": {}}`
					_, errs[idx] = tc.Execute(
						nil, content, searchTestFormat(),
					)
				}(i)
			}

			wg.Wait()
			for _, err := range errs {
				assert.NoError(t, err)
			}
		},
	)

	t.Run(
		"concurrent Initialize + AvailableToolsPrompt",
		func(t *testing.T) {
			var wg sync.WaitGroup

			for range 5 {
				wg.Add(2)
				go func() {
					defer wg.Done()
					_ = tc.Initialize()
				}()
				go func() {
					defer wg.Done()
					_ = tc.AvailableToolsPrompt()
				}()
			}

			wg.Wait()
		},
	)
}
