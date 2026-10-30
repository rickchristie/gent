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

// Use the same builder as application tools. Nil functions in catalog tests are read stubs.
type indexableToolFunc = gent.Tool[map[string]any, string]

func newIndexableTool(
	name, description, domain string, categories, keywords []string,
	fn func(context.Context, map[string]any) (string, error),
) indexableToolFunc {
	return newIndexableToolWithSchema(name, description, domain, categories, keywords, nil, fn)
}

func newIndexableToolWithSchema(
	name, description, domain string, categories, keywords []string, raw map[string]any,
	fn func(context.Context, map[string]any) (string, error),
) indexableToolFunc {
	if fn == nil {
		fn = func(context.Context, map[string]any) (string, error) { return "", nil }
	}
	return gent.DefineTool(name, fn).Description(description).ParameterSchema(raw).
		Domain(domain).Categories(categories...).Keywords(keywords...).MustBuild()
}

// mockToolSearcher is a mock search engine for testing.
type mockToolSearcher struct {
	id          string
	guidance    string
	indexErr    error
	searchFn    func(ctx context.Context, query string) ([]string, error)
	indexCalled bool
}

func (e *mockToolSearcher) Id() string {
	return e.id
}
func (e *mockToolSearcher) SearchGuidance() string {
	return e.guidance
}
func (e *mockToolSearcher) IndexAll(
	_ []gent.IndexableTool,
) error {
	e.indexCalled = true
	return e.indexErr
}
func (e *mockToolSearcher) Search(
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

// setupSearchChain creates a fully initialized SearchChain
// with the given tools and engines.
func setupSearchChain(
	tools []indexableToolFunc,
	engines []gent.ToolSearcher,
) *SearchChain {
	tc := NewSearch(SearchHintDomainCategories)
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

func TestSearchChain_Config(t *testing.T) {
	type expected struct {
		pageSize         int
		noResultsMessage string
	}

	tests := []struct {
		name     string
		setup    func() *SearchChain
		expected expected
	}{
		{
			name: "default page size",
			setup: func() *SearchChain {
				return NewSearch(SearchHintDomainCategories)
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
			setup: func() *SearchChain {
				return NewSearch(SearchHintDomainCategories).WithPageSize(5)
			},
			expected: expected{
				pageSize: 5,
				noResultsMessage: "No tools found matching your query. " +
					"Try different keywords or a broader search.",
			},
		},
		{
			name: "custom no-results message",
			setup: func() *SearchChain {
				return NewSearch(SearchHintDomainCategories).
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

func TestSearchChain_RegisterTool(t *testing.T) {
	t.Run("valid indexable tool succeeds", func(t *testing.T) {
		tc := NewSearch(SearchHintDomainCategories)
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

	t.Run("accepts builder with empty search metadata", func(t *testing.T) {
		tc := NewSearch(SearchHintDomainCategories)
		tool := gent.DefineTool("test", func(_ context.Context, _ map[string]any) (string, error) {
			return "ok", nil
		}).Description("Test tool").ParameterSchema(nil).MustBuild()
		assert.NotPanics(t, func() {
			tc.RegisterTool(tool)
		})
	})

	t.Run("panics on invalid type", func(t *testing.T) {
		tc := NewSearch(SearchHintDomainCategories)
		assert.Panics(t, func() {
			tc.RegisterTool("not a tool")
		})
	})

	t.Run("panics on duplicate name", func(t *testing.T) {
		tc := NewSearch(SearchHintDomainCategories)
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
		tc := NewSearch(SearchHintDomainCategories)
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
		// SearchChain is under the ToolChain interface,
		// cast back to check
		stc := result.(*SearchChain)
		assert.Len(t, stc.tools, 2)
	})

	t.Run("multiple tools registered correctly",
		func(t *testing.T) {
			tc := NewSearch(SearchHintDomainCategories)
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

func TestSearchChain_Initialize(t *testing.T) {
	t.Run("success with one engine", func(t *testing.T) {
		tc := NewSearch(SearchHintDomainCategories)
		engine := &mockToolSearcher{
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
		definitions, err := tc.NativeTools(newExecCtx())
		assert.NoError(t, err)
		assert.Len(t, definitions, 1)
	})

	t.Run("success with multiple engines",
		func(t *testing.T) {
			tc := NewSearch(SearchHintDomainCategories)
			eng1 := &mockToolSearcher{
				id: "eng1", guidance: "g1",
			}
			eng2 := &mockToolSearcher{
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
		tc := NewSearch(SearchHintDomainCategories)
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
			tc := NewSearch(SearchHintDomainCategories)
			engine := &mockToolSearcher{
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
		tc := NewSearch(SearchHintDomainCategories)
		engine := &mockToolSearcher{
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
		prompt1, err := tc.NativeTools(newExecCtx())
		require.NoError(t, err)

		err = tc.Initialize()
		require.NoError(t, err)
		prompt2, err := tc.NativeTools(newExecCtx())
		require.NoError(t, err)

		assert.Equal(t, prompt1, prompt2)
		assert.True(t, tc.initialized)
	})

	t.Run("domain summary with categories and counts",
		func(t *testing.T) {
			tc := NewSearch(SearchHintDomainCategories)
			engine := &mockToolSearcher{
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

			definitions, err := tc.NativeTools(newExecCtx())
			require.NoError(t, err)
			prompt := definitions[0].Function.Description
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
// NativeTools Tests
// -------------------------------------------------------

// -------------------------------------------------------
// Pin Tests
// -------------------------------------------------------

func TestSearchChain_Pin_InvalidTool(t *testing.T) {
	eng := &mockToolSearcher{
		id:       "bm25",
		guidance: "natural language",
	}

	tc := NewSearch(SearchHintDomainCategories)
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

func TestSearchChain_Pin_ToolStillSearchable(
	t *testing.T,
) {
	okFn := func(
		_ context.Context,
		_ map[string]any,
	) (string, error) {
		return "ok", nil
	}

	eng := &mockToolSearcher{
		id:       "bm25",
		guidance: "natural language",
		searchFn: func(
			_ context.Context, _ string,
		) ([]string, error) {
			return []string{"pinned_tool"}, nil
		},
	}

	tc := NewSearch(SearchHintDomainCategories)
	tc.RegisterEngine(eng)
	tc.RegisterTool(newIndexableTool(
		"pinned_tool", "A pinned tool", "D",
		nil, nil, okFn,
	))
	tc.Pin("pinned_tool")

	err := tc.Initialize()
	require.NoError(t, err)

	// Pinned tool should still be found via search
	result, err := executeTestCalls(tc,
		newExecCtx(),
		`{"tool":"tool_registry_search",`+
			`"args":{"query":"pinned",`+
			`"query_type":"bm25"}}`,
		searchTestFormat(),
	)
	require.NoError(t, err)
	assert.Contains(
		t, collectText(result), "pinned_tool",
	)
}

// -------------------------------------------------------
// Native tool call argument tests
// -------------------------------------------------------

// -------------------------------------------------------
// Execute — Search Tool Tests
// -------------------------------------------------------

func TestSearchChain_Execute_Search(t *testing.T) {
	okFn := func(_ context.Context, _ map[string]any) (string, error) {
		return "ok", nil
	}

	tools := []indexableToolFunc{
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

	mockEng := &mockToolSearcher{
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

	tc := setupSearchChain(
		tools, []gent.ToolSearcher{mockEng},
	)

	t.Run("search returns results with full definitions",
		func(t *testing.T) {
			content := `{"tool": "tool_registry_search", ` +
				`"args": {"query": "order", ` +
				`"query_type": "mock"}}`
			result, err := executeTestCalls(tc,
				nil, content, searchTestFormat(),
			)
			require.NoError(t, err)
			assert.Contains(
				t, collectText(result), "order_lookup",
			)
			assert.Contains(
				t, collectText(result),
				"Look up order details",
			)
		},
	)

	t.Run("pagination page 1",
		func(t *testing.T) {
			content := `{"tool": "tool_registry_search", ` +
				`"args": {"query": "all", ` +
				`"query_type": "mock"}}`
			result, err := executeTestCalls(tc,
				nil, content, searchTestFormat(),
			)
			require.NoError(t, err)
			// Default page size is 3, so page 1 shows 3
			assert.Contains(
				t, collectText(result), "order_lookup",
			)
			assert.Contains(
				t, collectText(result), "send_email",
			)
			assert.Contains(
				t, collectText(result), "check_inventory",
			)
			assert.NotContains(
				t, collectText(result), "billing_summary",
			)
			assert.Contains(
				t, collectText(result), "page 1 of 2",
			)
			assert.Contains(
				t, collectText(result), "4 total results",
			)
		},
	)

	t.Run("pagination page 2",
		func(t *testing.T) {
			content := `{"tool": "tool_registry_search", ` +
				`"args": {"query": "all", ` +
				`"query_type": "mock", "page": 2}}`
			result, err := executeTestCalls(tc,
				nil, content, searchTestFormat(),
			)
			require.NoError(t, err)
			assert.Contains(
				t, collectText(result), "billing_summary",
			)
			assert.NotContains(
				t, collectText(result), "order_lookup",
			)
			assert.Contains(
				t, collectText(result), "page 2 of 2",
			)
		},
	)

	t.Run("page beyond results returns no-results",
		func(t *testing.T) {
			content := `{"tool": "tool_registry_search", ` +
				`"args": {"query": "order", ` +
				`"query_type": "mock", "page": 99}}`
			result, err := executeTestCalls(tc,
				nil, content, searchTestFormat(),
			)
			require.NoError(t, err)
			assert.Contains(
				t, collectText(result), "No tools found",
			)
		},
	)

	t.Run("zero results returns no-results message",
		func(t *testing.T) {
			content := `{"tool": "tool_registry_search", ` +
				`"args": {"query": "zzzzz", ` +
				`"query_type": "mock"}}`
			result, err := executeTestCalls(tc,
				nil, content, searchTestFormat(),
			)
			require.NoError(t, err)
			assert.Contains(
				t, collectText(result), "No tools found",
			)
		},
	)

	t.Run("invalid query_type returns error",
		func(t *testing.T) {
			content := `{"tool": "tool_registry_search", ` +
				`"args": {"query": "test", ` +
				`"query_type": "unknown_engine"}}`
			result, err := executeTestCalls(tc,
				nil, content, searchTestFormat(),
			)
			// Schema validation should catch this
			require.NoError(t, err)
			assert.Contains(t, collectText(result), "Error")
		},
	)

	t.Run("engine returns error surfaced to LLM",
		func(t *testing.T) {
			errEng := &mockToolSearcher{
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
			tcErr := NewSearch(SearchHintDomainCategories).
				RegisterEngine(errEng)
			tool := newIndexableTool(
				"t1", "d", "D", nil, nil, okFn,
			)
			tcErr.RegisterTool(tool)
			require.NoError(t, tcErr.Initialize())

			content := `{"tool": "tool_registry_search", ` +
				`"args": {"query": "test", ` +
				`"query_type": "err_eng"}}`
			result, err := executeTestCalls(tcErr,
				nil, content, searchTestFormat(),
			)
			require.NoError(t, err)
			assert.Contains(
				t, collectText(result), "search service down",
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
			result, err := executeTestCalls(tc,
				nil, content, searchTestFormat(),
			)
			require.NoError(t, err)
			assert.Contains(
				t, collectText(result), "order_lookup",
			)
			assert.Contains(
				t, collectText(result), "send_email",
			)
		},
	)
}

// -------------------------------------------------------
// Execute — Regular Tool Tests
// -------------------------------------------------------

func TestSearchChain_Execute_RegularTool(t *testing.T) {
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
			eng := &mockToolSearcher{
				id: "m", guidance: "g",
			}
			tc := setupSearchChain(
				[]indexableToolFunc{tool},
				[]gent.ToolSearcher{eng},
			)

			content := `{"tool": "greet", ` +
				`"args": {"name": "Alice"}}`
			result, err := executeTestCalls(tc,
				nil, content, searchTestFormat(),
			)
			require.NoError(t, err)
			assert.Contains(
				t, collectText(result), "Hello, Alice!",
			)
			assert.Equal(
				t, "greet", result.Results[0].Name,
			)
		},
	)

	t.Run("unknown tool returns error", func(t *testing.T) {
		eng := &mockToolSearcher{
			id: "m", guidance: "g",
		}
		tool := newIndexableTool(
			"existing", "d", "D", nil, nil,
			func(_ context.Context, _ map[string]any) (string, error) {
				return "ok", nil
			},
		)
		tc := setupSearchChain(
			[]indexableToolFunc{tool},
			[]gent.ToolSearcher{eng},
		)

		content := `{"tool": "nonexistent", "args": {}}`
		result, err := executeTestCalls(tc,
			nil, content, searchTestFormat(),
		)
		require.NoError(t, err)
		assert.ErrorIs(
			t, result.Results[0].Error, gent.ErrUnknownTool,
		)
		assert.Equal(t, `<nonexistent>
Error: unknown tool "nonexistent". Use a function from the current native tool definitions.
</nonexistent>`, collectText(result))
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
		eng := &mockToolSearcher{
			id: "m", guidance: "g",
		}
		tc := setupSearchChain(
			[]indexableToolFunc{tool},
			[]gent.ToolSearcher{eng},
		)

		content := `{"tool": "strict_tool", "args": {}}`
		result, err := executeTestCalls(tc,
			nil, content, searchTestFormat(),
		)
		require.NoError(t, err)
		assert.Error(t, result.Results[0].Error)
		assert.Contains(
			t, collectText(result), "Error",
		)
	})

	t.Run("tool Call returns error", func(t *testing.T) {
		tool := newIndexableTool(
			"failing", "Fails", "D", nil, nil,
			func(_ context.Context, _ map[string]any) (string, error) {
				return "", errors.New("tool broke")
			},
		)
		eng := &mockToolSearcher{
			id: "m", guidance: "g",
		}
		tc := setupSearchChain(
			[]indexableToolFunc{tool},
			[]gent.ToolSearcher{eng},
		)

		content := `{"tool": "failing", "args": {}}`
		result, err := executeTestCalls(tc,
			nil, content, searchTestFormat(),
		)
		require.NoError(t, err)
		assert.Error(t, result.Results[0].Error)
		assert.Contains(t, collectText(result), "tool broke")
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
			eng := &mockToolSearcher{
				id: "m", guidance: "g",
			}
			tc := NewSearch(SearchHintDomainCategories).RegisterEngine(eng)
			tc.RegisterTool(instructionTool)
			require.NoError(t, tc.Initialize())

			content := `{"tool": "with_inst", "args": {}}`
			result, err := executeTestCalls(tc,
				nil, content, searchTestFormat(),
			)
			require.NoError(t, err)
			assert.Contains(
				t, collectText(result), "instructions",
			)
			assert.Contains(
				t, collectText(result), "Follow these steps",
			)
		},
	)
}

// instructionToolImpl is a tool that returns instructions.
type instructionToolImpl struct {
	indexableToolFunc
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

func TestSearchChain_Execute_Mixed(t *testing.T) {
	okFn := func(_ context.Context, args map[string]any) (string, error) {
		return "regular_result", nil
	}

	tools := []indexableToolFunc{
		newIndexableTool(
			"regular_tool", "A regular tool",
			"General", nil, nil, okFn,
		),
		newIndexableTool(
			"another_tool", "Another tool",
			"General", nil, nil, okFn,
		),
	}

	eng := &mockToolSearcher{
		id:       "mock",
		guidance: "g",
		searchFn: func(
			_ context.Context, _ string,
		) ([]string, error) {
			return []string{"regular_tool"}, nil
		},
	}

	tc := setupSearchChain(
		tools, []gent.ToolSearcher{eng},
	)

	t.Run("search + regular tool in same array",
		func(t *testing.T) {
			content := `[` +
				`{"tool": "tool_registry_search", ` +
				`"args": {"query": "test", ` +
				`"query_type": "mock"}},` +
				`{"tool": "regular_tool", "args": {}}]`
			result, err := executeTestCalls(tc,
				nil, content, searchTestFormat(),
			)
			require.NoError(t, err)
			assert.Len(t, result.Results, 2)
			// Search result
			assert.Contains(
				t, collectText(result), "regular_tool",
			)
			// Regular tool result
			assert.Contains(
				t, collectText(result), "regular_result",
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
			result, err := executeTestCalls(tc,
				nil, content, searchTestFormat(),
			)
			require.NoError(t, err)
			assert.Len(t, result.Results, 3)
		},
	)
}

// -------------------------------------------------------
// Execute — Events & Stats Tests
// -------------------------------------------------------

func TestSearchChain_Execute_Stats(t *testing.T) {
	okFn := func(_ context.Context, _ map[string]any) (string, error) {
		return "ok", nil
	}

	t.Run("search call increments tool call stats",
		func(t *testing.T) {
			eng := &mockToolSearcher{
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
			tc := setupSearchChain(
				[]indexableToolFunc{tool},
				[]gent.ToolSearcher{eng},
			)

			execCtx := newExecCtx()
			content := `{"tool": "tool_registry_search", ` +
				`"args": {"query": "x", ` +
				`"query_type": "mock"}}`
			_, err := executeTestCalls(tc,
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
			eng := &mockToolSearcher{
				id: "m", guidance: "g",
			}
			tool := newIndexableTool(
				"my_tool", "d", "D", nil, nil, okFn,
			)
			tc := setupSearchChain(
				[]indexableToolFunc{tool},
				[]gent.ToolSearcher{eng},
			)

			execCtx := newExecCtx()
			content := `{"tool": "my_tool", "args": {}}`
			_, err := executeTestCalls(tc,
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

	t.Run("unknown regular tool call increments tool call stats",
		func(t *testing.T) {
			eng := &mockToolSearcher{
				id: "m", guidance: "g",
			}
			tc := setupSearchChain(
				[]indexableToolFunc{},
				[]gent.ToolSearcher{eng},
			)

			execCtx := newExecCtx()
			content := `{"tool": "missing_tool", "args": {}}`
			_, err := executeTestCalls(tc,
				execCtx, content, searchTestFormat(),
			)
			require.NoError(t, err)

			assert.Equal(
				t, int64(1),
				execCtx.Stats().GetCounter(gent.SCToolCalls),
			)
			assert.Equal(
				t, int64(1),
				execCtx.Stats().GetCounter(
					gent.SCToolCallsFor+"missing_tool",
				),
			)
			assert.Equal(
				t, int64(1),
				execCtx.Stats().GetCounter(
					gent.SCToolCallsErrorTotal,
				),
			)
			assert.Equal(
				t, int64(1),
				execCtx.Stats().GetCounter(
					gent.SCToolCallsErrorFor+"missing_tool",
				),
			)
		},
	)

	t.Run("search error increments error stats",
		func(t *testing.T) {
			eng := &mockToolSearcher{
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
			tc := setupSearchChain(
				[]indexableToolFunc{tool},
				[]gent.ToolSearcher{eng},
			)

			execCtx := newExecCtx()
			content := `{"tool": "tool_registry_search", ` +
				`"args": {"query": "x", ` +
				`"query_type": "mock"}}`
			_, err := executeTestCalls(tc,
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
			eng := &mockToolSearcher{
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
			tc := setupSearchChain(
				[]indexableToolFunc{tool},
				[]gent.ToolSearcher{eng},
			)

			execCtx := newExecCtx()

			// First: error
			content := `{"tool": "tool_registry_search", ` +
				`"args": {"query": "x", ` +
				`"query_type": "mock"}}`
			_, _ = executeTestCalls(tc,
				execCtx, content, searchTestFormat(),
			)
			assert.Equal(
				t, float64(1),
				execCtx.Stats().GetGauge(
					gent.SGToolCallsErrorConsecutive,
				),
			)

			// Second: success
			_, err := executeTestCalls(tc,
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
			eng := &mockToolSearcher{
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
			tc := setupSearchChain(
				[]indexableToolFunc{failTool, okTool},
				[]gent.ToolSearcher{eng},
			)

			execCtx := newExecCtx()

			// First: error
			content := `{"tool": "fail_tool", "args": {}}`
			_, _ = executeTestCalls(tc,
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
			_, err := executeTestCalls(tc,
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

func TestSearchChain_Execute_InvalidAttemptsCountAgainstLimits(t *testing.T) {
	type input struct {
		content  string
		setup    func() *SearchChain
		limitKey gent.StatKey
	}

	type expected struct {
		exceededLimit gent.Limit
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "unknown regular tool trips per-tool call limit",
			input: input{
				content: `{"tool": "missing_tool", "args": {}}`,
				setup: func() *SearchChain {
					return setupSearchChain(
						[]indexableToolFunc{},
						[]gent.ToolSearcher{&mockToolSearcher{id: "mock", guidance: "g"}},
					)
				},
				limitKey: gent.SCToolCallsFor + "missing_tool",
			},
			expected: expected{exceededLimit: gent.Limit{
				Type:     gent.LimitExactKey,
				Key:      gent.SCToolCallsFor + "missing_tool",
				MaxValue: 0,
			}},
		},
		{
			name: "search schema validation failure trips search tool limit",
			input: input{
				content: `{
  "tool": "tool_registry_search",
  "args": {"query": "x", "query_type": "missing_engine"}
}`,
				setup: func() *SearchChain {
					return setupSearchChain(
						[]indexableToolFunc{},
						[]gent.ToolSearcher{&mockToolSearcher{id: "mock", guidance: "g"}},
					)
				},
				limitKey: gent.SCToolCallsFor + searchToolName,
			},
			expected: expected{exceededLimit: gent.Limit{
				Type:     gent.LimitExactKey,
				Key:      gent.SCToolCallsFor + searchToolName,
				MaxValue: 0,
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := tt.input.setup()
			execCtx := newExecCtx()
			execCtx.SetLimits([]gent.Limit{tt.expected.exceededLimit})

			_, err := executeTestCalls(tc, execCtx, tt.input.content, searchTestFormat())

			require.NoError(t, err)
			require.NotNil(t, execCtx.ExceededLimit())
			assert.Equal(t, tt.expected.exceededLimit, *execCtx.ExceededLimit())
			assert.Equal(t, int64(1), execCtx.Stats().GetCounter(gent.SCToolCalls))
			assert.Equal(t, int64(1), execCtx.Stats().GetCounter(tt.input.limitKey))
			assert.Error(t, execCtx.Context().Err())
		})
	}
}

// -------------------------------------------------------
// Execute — Pagination Details Tests
// -------------------------------------------------------

func TestSearchChain_Execute_Pagination(t *testing.T) {
	okFn := func(_ context.Context, _ map[string]any) (string, error) {
		return "ok", nil
	}

	// Create 7 tools for pagination testing
	var tools []indexableToolFunc
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

	eng := &mockToolSearcher{
		id:       "mock",
		guidance: "g",
		searchFn: func(
			_ context.Context, _ string,
		) ([]string, error) {
			return allNames, nil
		},
	}

	tc := setupSearchChain(
		tools, []gent.ToolSearcher{eng},
	)

	t.Run("page 1 shows first 3 tools + pagination info",
		func(t *testing.T) {
			content := `{"tool": "tool_registry_search", ` +
				`"args": {"query": "x", ` +
				`"query_type": "mock"}}`
			result, err := executeTestCalls(tc,
				nil, content, searchTestFormat(),
			)
			require.NoError(t, err)
			assert.Contains(t, collectText(result), "tool_0")
			assert.Contains(t, collectText(result), "tool_1")
			assert.Contains(t, collectText(result), "tool_2")
			assert.NotContains(t, collectText(result), "tool_3")
			assert.Contains(
				t, collectText(result),
				"Showing page 1 of 3 (7 total results)",
			)
		},
	)

	t.Run("page 2 shows next 3 tools", func(t *testing.T) {
		content := `{"tool": "tool_registry_search", ` +
			`"args": {"query": "x", ` +
			`"query_type": "mock", "page": 2}}`
		result, err := executeTestCalls(tc,
			nil, content, searchTestFormat(),
		)
		require.NoError(t, err)
		assert.Contains(t, collectText(result), "tool_3")
		assert.Contains(t, collectText(result), "tool_4")
		assert.Contains(t, collectText(result), "tool_5")
		assert.NotContains(t, collectText(result), "tool_6")
		assert.Contains(
			t, collectText(result),
			"Showing page 2 of 3 (7 total results)",
		)
	})

	t.Run("last page shows remaining tools (partial page)",
		func(t *testing.T) {
			content := `{"tool": "tool_registry_search", ` +
				`"args": {"query": "x", ` +
				`"query_type": "mock", "page": 3}}`
			result, err := executeTestCalls(tc,
				nil, content, searchTestFormat(),
			)
			require.NoError(t, err)
			assert.Contains(t, collectText(result), "tool_6")
			assert.NotContains(t, collectText(result), "tool_5")
			assert.Contains(
				t, collectText(result),
				"Showing page 3 of 3 (7 total results)",
			)
		},
	)

	t.Run("page 0 treated as page 1", func(t *testing.T) {
		content := `{"tool": "tool_registry_search", ` +
			`"args": {"query": "x", ` +
			`"query_type": "mock", "page": 0}}`
		result, err := executeTestCalls(tc,
			nil, content, searchTestFormat(),
		)
		require.NoError(t, err)
		assert.Contains(t, collectText(result), "tool_0")
		assert.Contains(
			t, collectText(result), "page 1 of 3",
		)
	})

	t.Run("page beyond total shows no-results",
		func(t *testing.T) {
			content := `{"tool": "tool_registry_search", ` +
				`"args": {"query": "x", ` +
				`"query_type": "mock", "page": 99}}`
			result, err := executeTestCalls(tc,
				nil, content, searchTestFormat(),
			)
			require.NoError(t, err)
			assert.Contains(
				t, collectText(result), "No tools found",
			)
		},
	)
}

// -------------------------------------------------------
// Execute — Search Dedup Tests
// -------------------------------------------------------

func TestSearchChain_Execute_SearchDedup(t *testing.T) {
	okFn := func(
		_ context.Context, _ map[string]any,
	) (string, error) {
		return "ok", nil
	}

	tools := []indexableToolFunc{
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
				dedupTools:  []string{"tool_b"},
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
					"tool_b", // deduped in q2
					"tool_a", // deduped in q3
					"tool_b", // deduped in q3
					"tool_c", // deduped in q3
				},
				noDescTools: []string{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eng := &mockToolSearcher{
				id:       "mock",
				guidance: "mock search",
				searchFn: tt.input.searchFn,
			}

			tc := setupSearchChain(
				tools, []gent.ToolSearcher{eng},
			)
			tc.WithPageSize(10) // avoid pagination

			result, err := executeTestCalls(tc,
				nil, tt.input.content,
				searchTestFormat(),
			)
			require.NoError(t, err)

			// Verify full definitions appear
			for _, name := range tt.expected.fullDefTools {
				assert.Contains(
					t, collectText(result), name,
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
					t, collectText(result),
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
					collectText(result), desc,
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
					t, collectText(result), desc,
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

func TestSearchChain_ConcurrentAccess(t *testing.T) {
	okFn := func(_ context.Context, _ map[string]any) (string, error) {
		return "ok", nil
	}

	tools := []indexableToolFunc{
		newIndexableTool(
			"tool_a", "Tool A", "D",
			nil, nil, okFn,
		),
		newIndexableTool(
			"tool_b", "Tool B", "D",
			nil, nil, okFn,
		),
	}

	eng := &mockToolSearcher{
		id:       "mock",
		guidance: "g",
		searchFn: func(
			_ context.Context, _ string,
		) ([]string, error) {
			return []string{"tool_a"}, nil
		},
	}

	tc := setupSearchChain(
		tools, []gent.ToolSearcher{eng},
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
					_, errs[idx] = executeTestCalls(tc,
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
		"concurrent Initialize + NativeTools",
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
					_, _ = tc.NativeTools(newExecCtx())
				}()
			}

			wg.Wait()
		},
	)
}
