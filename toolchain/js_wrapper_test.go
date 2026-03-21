package toolchain

import (
	"context"
	"strings"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/format"
	"github.com/rickchristie/gent/toolchain/jsruntime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupJsWrapper creates a JsToolChainWrapper wrapping
// a SearchJSON with test tools.
func setupJsWrapper() *JsToolChainWrapper {
	tools := []*indexableToolFunc{
		newIndexableToolWithSchema(
			"lookup_customer",
			"Look up customer by ID",
			"customers",
			[]string{"lookup"},
			[]string{"customer", "lookup"},
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{
						"type": "string",
					},
				},
				"required": []any{"id"},
			},
			func(
				ctx context.Context,
				args map[string]any,
			) (string, error) {
				id, _ := args["id"].(string)
				return `{"id":"` + id +
					`","name":"Alice"}`, nil
			},
		),
		newIndexableToolWithSchema(
			"get_orders",
			"Get orders for a customer",
			"orders",
			[]string{"orders"},
			[]string{"order", "customer"},
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"customer_id": map[string]any{
						"type": "string",
					},
				},
				"required": []any{"customer_id"},
			},
			func(
				ctx context.Context,
				args map[string]any,
			) (string, error) {
				return `[{"order_id":"O1"}]`, nil
			},
		),
		newIndexableToolWithSchema(
			"create_order",
			"Create an order with line items",
			"orders",
			[]string{"create"},
			[]string{"order", "create"},
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"customer_id": map[string]any{
						"type": "string",
					},
					"items": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"name": map[string]any{
									"type": "string",
								},
								"qty": map[string]any{
									"type": "integer",
								},
							},
							"required": []any{
								"name", "qty",
							},
						},
					},
				},
				"required": []any{
					"customer_id", "items",
				},
			},
			func(
				ctx context.Context,
				args map[string]any,
			) (string, error) {
				return `{"status":"created"}`, nil
			},
		),
		newIndexableToolWithSchema(
			"update_address",
			"Update customer address",
			"customers",
			[]string{"update"},
			[]string{"address", "update"},
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{
						"type": "string",
					},
					"address": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"street": map[string]any{
								"type": "string",
							},
							"city": map[string]any{
								"type": "string",
							},
						},
						"required": []any{
							"street", "city",
						},
					},
				},
				"required": []any{"id", "address"},
			},
			func(
				ctx context.Context,
				args map[string]any,
			) (string, error) {
				return `{"status":"updated"}`, nil
			},
		),
		newIndexableToolWithSchema(
			"update_stock",
			"Update stock quantities",
			"inventory",
			[]string{"stock"},
			[]string{"stock", "update"},
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{
						"type": "string",
					},
					"quantities": map[string]any{
						"type": "object",
						"additionalProperties": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"amount": map[string]any{
									"type": "integer",
								},
								"unit": map[string]any{
									"type": "string",
								},
							},
							"required": []any{
								"amount", "unit",
							},
						},
					},
				},
				"required": []any{"id", "quantities"},
			},
			func(
				ctx context.Context,
				args map[string]any,
			) (string, error) {
				return `{"status":"updated"}`, nil
			},
		),
		newIndexableToolWithSchema(
			"update_geo",
			"Update geo coordinates",
			"customers",
			[]string{"geo"},
			[]string{"geo", "update"},
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{
						"type": "string",
					},
					"address": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"street": map[string]any{
								"type": "string",
							},
							"geo": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"lat": map[string]any{
										"type": "number",
									},
									"lng": map[string]any{
										"type": "number",
									},
								},
								"required": []any{
									"lat", "lng",
								},
							},
						},
						"required": []any{
							"street", "geo",
						},
					},
				},
				"required": []any{"id", "address"},
			},
			func(
				ctx context.Context,
				args map[string]any,
			) (string, error) {
				return `{"status":"updated"}`, nil
			},
		),
		newIndexableToolWithSchema(
			"create_shipment",
			"Create a shipment with orders",
			"shipping",
			[]string{"shipment"},
			[]string{"shipment", "create"},
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{
						"type": "string",
					},
					"orders": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"order_id": map[string]any{
									"type": "string",
								},
								"items": map[string]any{
									"type": "array",
									"items": map[string]any{
										"type": "object",
										"properties": map[string]any{
											"name": map[string]any{
												"type": "string",
											},
											"qty": map[string]any{
												"type": "integer",
											},
										},
										"required": []any{
											"name", "qty",
										},
									},
								},
							},
							"required": []any{
								"order_id", "items",
							},
						},
					},
				},
				"required": []any{"id", "orders"},
			},
			func(
				ctx context.Context,
				args map[string]any,
			) (string, error) {
				return `{"status":"created"}`, nil
			},
		),
		newIndexableToolWithSchema(
			"update_regions",
			"Update region zones",
			"geography",
			[]string{"regions"},
			[]string{"region", "update"},
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{
						"type": "string",
					},
					"regions": map[string]any{
						"type": "object",
						"additionalProperties": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"zones": map[string]any{
									"type": "object",
									"additionalProperties": map[string]any{
										"type": "object",
										"properties": map[string]any{
											"code": map[string]any{
												"type": "string",
											},
											"population": map[string]any{
												"type": "integer",
											},
										},
										"required": []any{
											"code",
											"population",
										},
									},
								},
							},
							"required": []any{"zones"},
						},
					},
				},
				"required": []any{"id", "regions"},
			},
			func(
				ctx context.Context,
				args map[string]any,
			) (string, error) {
				return `{"status":"updated"}`, nil
			},
		),
		newIndexableToolWithSchema(
			"update_products",
			"Update product attributes",
			"products",
			[]string{"products"},
			[]string{"product", "update"},
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{
						"type": "string",
					},
					"items": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"name": map[string]any{
									"type": "string",
								},
								"attributes": map[string]any{
									"type": "object",
									"additionalProperties": map[string]any{
										"type": "object",
										"properties": map[string]any{
											"value": map[string]any{
												"type": "string",
											},
											"unit": map[string]any{
												"type": "string",
											},
										},
										"required": []any{
											"value", "unit",
										},
									},
								},
							},
							"required": []any{
								"name", "attributes",
							},
						},
					},
				},
				"required": []any{"id", "items"},
			},
			func(
				ctx context.Context,
				args map[string]any,
			) (string, error) {
				return `{"status":"updated"}`, nil
			},
		),
		newIndexableToolWithSchema(
			"update_catalog",
			"Update product catalog",
			"catalog",
			[]string{"catalog"},
			[]string{"catalog", "update"},
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{
						"type": "string",
					},
					"categories": map[string]any{
						"type": "object",
						"additionalProperties": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"products": map[string]any{
									"type": "array",
									"items": map[string]any{
										"type": "object",
										"properties": map[string]any{
											"name": map[string]any{
												"type": "string",
											},
											"price": map[string]any{
												"type": "number",
											},
										},
										"required": []any{
											"name", "price",
										},
									},
								},
							},
							"required": []any{
								"products",
							},
						},
					},
				},
				"required": []any{"id", "categories"},
			},
			func(
				ctx context.Context,
				args map[string]any,
			) (string, error) {
				return `{"status":"updated"}`, nil
			},
		),
		newIndexableTool(
			"fail_tool",
			"Always fails",
			"testing",
			[]string{"test"},
			[]string{"fail"},
			func(
				ctx context.Context,
				args map[string]any,
			) (string, error) {
				return "", assert.AnError
			},
		),
	}

	engines := []gent.ToolSearchEngine{
		&mockToolSearchEngine{
			id:       "keyword",
			guidance: "Search by keyword",
			searchFn: func(
				ctx context.Context, query string,
			) ([]string, error) {
				var results []string
				for _, t := range tools {
					if strings.Contains(
						strings.ToLower(t.Name()),
						strings.ToLower(query),
					) {
						results = append(
							results, t.Name(),
						)
					}
				}
				return results, nil
			},
		},
	}

	searchTC := setupSearchJSON(tools, engines)
	return NewJsToolChainWrapper(searchTC)
}

func jsTestFormat() gent.TextFormat {
	return format.NewXML()
}

// -------------------------------------------------------
// A. Name / Guidance / AvailableToolsPrompt
// -------------------------------------------------------

func TestJsWrapper_Name(t *testing.T) {
	w := setupJsWrapper()
	assert.Equal(t, "action", w.Name())
}

func TestJsWrapper_Guidance(t *testing.T) {
	w := setupJsWrapper()
	guidance := w.Guidance()

	// Contains both modes
	assert.Contains(t, guidance, "<direct_call>")
	assert.Contains(t, guidance, "</direct_call>")
	assert.Contains(t, guidance, "<code>")
	assert.Contains(t, guidance, "</code>")

	// Contains wrapped ToolChain's guidance inside
	// direct_call
	assert.Contains(
		t, guidance,
		`{"tool": "tool_name", "args": {...}}`,
	)

	// Contains recommendation text
	assert.Contains(
		t, guidance, "Choose direct_call",
	)
}

func TestJsWrapper_Guidance_Custom(t *testing.T) {
	w := setupJsWrapper().WithCodeGuidance(
		"Custom JS guidance here",
	)
	guidance := w.Guidance()
	assert.Contains(
		t, guidance, "Custom JS guidance here",
	)
}

func TestJsWrapper_AvailableToolsPrompt(t *testing.T) {
	w := setupJsWrapper()
	prompt := w.AvailableToolsPrompt()

	// Contains wrapped prompt content
	assert.Contains(t, prompt, "tool_registry_search")

	// JS environment details are in Guidance(), not here
	assert.NotContains(t, prompt, "JavaScript Environment")
}

// -------------------------------------------------------
// B. ParseSection — sub-section detection
// -------------------------------------------------------

func TestJsWrapper_ParseSection(t *testing.T) {
	type input struct {
		content string
	}

	type expected struct {
		isCode     bool
		isToolCall bool
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "direct_call delegates to wrapped",
			input: input{
				content: `<direct_call>
{"tool": "lookup_customer", "args": {"id": "C1"}}
</direct_call>`,
			},
			expected: expected{
				isToolCall: true,
			},
		},
		{
			name: "code returns string",
			input: input{
				content: `<code>
var x = tool.call({tool: "lookup_customer", args: {id: "C1"}});
console.log(JSON.stringify(x));
</code>`,
			},
			expected: expected{
				isCode: true,
			},
		},
		{
			name: "neither — fallback to wrapped",
			input: input{
				content: `{"tool": "lookup_customer", "args": {"id": "C1"}}`,
			},
			expected: expected{
				isToolCall: true,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := setupJsWrapper()
			execCtx := newExecCtx()
			result, err := w.ParseSection(
				execCtx, tc.input.content,
			)
			require.NoError(t, err)

			if tc.expected.isCode {
				_, ok := result.(string)
				assert.True(
					t, ok,
					"expected string for code section",
				)
			}
			if tc.expected.isToolCall {
				_, ok := result.([]*gent.ToolCall)
				assert.True(
					t, ok,
					"expected []*ToolCall for "+
						"direct_call",
				)
			}
		})
	}
}

// -------------------------------------------------------
// C. Execute — direct_call passthrough
// -------------------------------------------------------

func TestJsWrapper_Execute_DirectCall(t *testing.T) {
	type input struct {
		content string
	}

	type expected struct {
		text string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "single tool call passes through",
			input: input{
				content: `<direct_call>
{"tool": "lookup_customer", "args": {"id": "C001"}}
</direct_call>`,
			},
			expected: expected{
				text: `<lookup_customer>
"{\"id\":\"C001\",\"name\":\"Alice\"}"
</lookup_customer>`,
			},
		},
		{
			name: "parallel tool calls pass through",
			input: input{
				content: `<direct_call>
[{"tool": "lookup_customer", "args": {"id": "C001"}},
 {"tool": "get_orders", "args": {"customer_id": "C001"}}]
</direct_call>`,
			},
			expected: expected{
				text: `<lookup_customer>
"{\"id\":\"C001\",\"name\":\"Alice\"}"
</lookup_customer>
<get_orders>
"[{\"order_id\":\"O1\"}]"
</get_orders>`,
			},
		},
		{
			name: "fallback without tags",
			input: input{
				content: `{"tool": "lookup_customer", "args": {"id": "C002"}}`,
			},
			expected: expected{
				text: `<lookup_customer>
"{\"id\":\"C002\",\"name\":\"Alice\"}"
</lookup_customer>`,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := setupJsWrapper()
			execCtx := newExecCtx()
			tf := jsTestFormat()

			result, err := w.Execute(
				execCtx, tc.input.content, tf,
			)
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(
				t, tc.expected.text, result.Text,
			)
		})
	}
}

func TestJsWrapper_Execute_DirectCall_Stats(t *testing.T) {
	w := setupJsWrapper()
	execCtx := newExecCtx()
	tf := jsTestFormat()

	content := `<direct_call>
{"tool": "lookup_customer", "args": {"id": "C001"}}
</direct_call>`

	_, err := w.Execute(execCtx, content, tf)
	require.NoError(t, err)

	// Tool call stats flow through wrapped ToolChain
	assert.Equal(
		t, int64(1),
		execCtx.Stats().GetCounter(gent.SCToolCalls),
	)
	assert.Equal(
		t, int64(1),
		execCtx.Stats().GetCounter(
			gent.SCToolCallsFor+"lookup_customer",
		),
	)

	// Code execution stats should NOT be incremented
	assert.Equal(
		t, int64(0),
		execCtx.Stats().GetCounter(
			gent.SCCodeExecutions,
		),
	)
}

// -------------------------------------------------------
// D. Execute — code execution
// -------------------------------------------------------

func TestJsWrapper_Execute_Code(t *testing.T) {
	type input struct {
		content string
	}

	type expected struct {
		text string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "simple tool.call with console.log",
			input: input{
				content: `<code>
var c = tool.call(
  {tool: "lookup_customer", args: {id: "C001"}}
);
console.log(c.output.id + ":" + c.output.name);
</code>`,
			},
			expected: expected{
				text: "<tool_call_log>\n" +
					`[1] lookup_customer({"id":"C001"})` +
					` -> {"id":"C001","name":"Alice"}` +
					"\n</tool_call_log>\n" +
					"<output>\n" +
					"C001:Alice" +
					"\n</output>",
			},
		},
		{
			name: "chained calls",
			input: input{
				content: `<code>
var c = tool.call(
  {tool: "lookup_customer", args: {id: "C001"}}
);
var o = tool.call(
  {tool: "get_orders",
   args: {customer_id: c.output.id}}
);
console.log(
  "customer=" + c.output.id +
  " orders=" + o.output[0].order_id
);
</code>`,
			},
			expected: expected{
				text: "<tool_call_log>\n" +
					`[1] lookup_customer({"id":"C001"})` +
					` -> {"id":"C001","name":"Alice"}` +
					"\n" +
					`[2] get_orders({"customer_id":"C001"})` +
					` -> [{"order_id":"O1"}]` +
					"\n</tool_call_log>\n" +
					"<output>\n" +
					"customer=C001 orders=O1" +
					"\n</output>",
			},
		},
		{
			name: "no console.log uses tool output",
			input: input{
				content: `<code>
var c = tool.call(
  {tool: "lookup_customer", args: {id: "C001"}}
);
</code>`,
			},
			expected: expected{
				text: `<tool_call_log>
[1] lookup_customer({"id":"C001"}) -> {"id":"C001","name":"Alice"}
</tool_call_log>
<output>
Code executed successfully.
</output>`,
			},
		},
		{
			name: "JS syntax error",
			input: input{
				content: `<code>
var x = @;
</code>`,
			},
			expected: expected{
				text: `<output>
SyntaxError: SyntaxError: (anonymous): Line 1:9 Unexpected token ILLEGAL (and 2 more errors)

</output>`,
			},
		},
		{
			name: "JS runtime error (ReferenceError)",
			input: input{
				content: `<code>
undefinedVar;
</code>`,
			},
			expected: expected{
				text: `<output>
ReferenceError: undefinedVar is not defined

1 | undefinedVar;
    ^ ReferenceError: undefinedVar is not defined

</output>`,
			},
		},
		{
			name: "code that calls no tools",
			input: input{
				content: `<code>
console.log("hello from JS");
</code>`,
			},
			expected: expected{
				text: "<output>\n" +
					"hello from JS" +
					"\n</output>",
			},
		},
		{
			name: "multiple console.log calls",
			input: input{
				content: `<code>
console.log("line1");
console.log("line2");
</code>`,
			},
			expected: expected{
				text: "<output>\n" +
					"line1\nline2" +
					"\n</output>",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := setupJsWrapper()
			execCtx := newExecCtx()
			tf := jsTestFormat()

			result, err := w.Execute(
				execCtx, tc.input.content, tf,
			)

			// Code execution should never return
			// a Go error — errors are in the result
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(
				t, tc.expected.text,
				result.Text,
			)
		})
	}
}

// -------------------------------------------------------
// E. Execute — stats tracking
// -------------------------------------------------------

func TestJsWrapper_Execute_Code_Stats(t *testing.T) {
	t.Run(
		"code execution increments SCCodeExecutions",
		func(t *testing.T) {
			w := setupJsWrapper()
			execCtx := newExecCtx()
			tf := jsTestFormat()

			content := `<code>
console.log("ok");
</code>`
			_, err := w.Execute(execCtx, content, tf)
			require.NoError(t, err)

			assert.Equal(
				t, int64(1),
				execCtx.Stats().GetCounter(
					gent.SCCodeExecutions,
				),
			)
			assert.Equal(
				t, int64(0),
				execCtx.Stats().GetCounter(
					gent.SCCodeExecutionsError,
				),
			)
			assert.Equal(
				t, 0.0,
				execCtx.Stats().GetGauge(
					gent.SGCodeExecutionsErrorConsecutive,
				),
			)
		},
	)

	t.Run(
		"code error increments error counters",
		func(t *testing.T) {
			w := setupJsWrapper()
			execCtx := newExecCtx()
			tf := jsTestFormat()

			content := `<code>
undefinedVar;
</code>`
			_, err := w.Execute(execCtx, content, tf)
			require.NoError(t, err) // no Go error

			assert.Equal(
				t, int64(1),
				execCtx.Stats().GetCounter(
					gent.SCCodeExecutions,
				),
			)
			assert.Equal(
				t, int64(1),
				execCtx.Stats().GetCounter(
					gent.SCCodeExecutionsError,
				),
			)
			assert.Equal(
				t, 1.0,
				execCtx.Stats().GetGauge(
					gent.SGCodeExecutionsErrorConsecutive,
				),
			)
		},
	)

	t.Run(
		"consecutive gauge resets on success",
		func(t *testing.T) {
			w := setupJsWrapper()
			execCtx := newExecCtx()
			tf := jsTestFormat()

			// First: error
			errContent := `<code>
undefinedVar;
</code>`
			_, err := w.Execute(
				execCtx, errContent, tf,
			)
			require.NoError(t, err)
			assert.Equal(
				t, 1.0,
				execCtx.Stats().GetGauge(
					gent.SGCodeExecutionsErrorConsecutive,
				),
			)

			// Second: success
			okContent := `<code>
console.log("ok");
</code>`
			_, err = w.Execute(
				execCtx, okContent, tf,
			)
			require.NoError(t, err)
			assert.Equal(
				t, 0.0,
				execCtx.Stats().GetGauge(
					gent.SGCodeExecutionsErrorConsecutive,
				),
			)

			// Counters still accumulate
			assert.Equal(
				t, int64(2),
				execCtx.Stats().GetCounter(
					gent.SCCodeExecutions,
				),
			)
			assert.Equal(
				t, int64(1),
				execCtx.Stats().GetCounter(
					gent.SCCodeExecutionsError,
				),
			)
		},
	)

	t.Run(
		"tool calls from code flow through wrapped "+
			"ToolChain stats",
		func(t *testing.T) {
			w := setupJsWrapper()
			execCtx := newExecCtx()
			tf := jsTestFormat()

			content := `<code>
tool.call(
  {tool: "lookup_customer", args: {id: "C001"}}
);
tool.call(
  {tool: "get_orders",
   args: {customer_id: "C001"}}
);
</code>`
			_, err := w.Execute(execCtx, content, tf)
			require.NoError(t, err)

			// Tool call stats from wrapped ToolChain
			assert.Equal(
				t, int64(2),
				execCtx.Stats().GetCounter(
					gent.SCToolCalls,
				),
			)
			assert.Equal(
				t, int64(1),
				execCtx.Stats().GetCounter(
					gent.SCToolCallsFor+
						"lookup_customer",
				),
			)
			assert.Equal(
				t, int64(1),
				execCtx.Stats().GetCounter(
					gent.SCToolCallsFor+"get_orders",
				),
			)

			// Plus code execution counter
			assert.Equal(
				t, int64(1),
				execCtx.Stats().GetCounter(
					gent.SCCodeExecutions,
				),
			)
		},
	)

	t.Run(
		"direct call does NOT count code execution",
		func(t *testing.T) {
			w := setupJsWrapper()
			execCtx := newExecCtx()
			tf := jsTestFormat()

			content := `<direct_call>
{"tool": "lookup_customer", "args": {"id": "C001"}}
</direct_call>`
			_, err := w.Execute(execCtx, content, tf)
			require.NoError(t, err)

			assert.Equal(
				t, int64(0),
				execCtx.Stats().GetCounter(
					gent.SCCodeExecutions,
				),
			)
			assert.Equal(
				t, int64(1),
				execCtx.Stats().GetCounter(
					gent.SCToolCalls,
				),
			)
		},
	)
}

// -------------------------------------------------------
// F. Execute — edge cases
// -------------------------------------------------------

func TestJsWrapper_Execute_EdgeCases(t *testing.T) {
	t.Run("empty code block", func(t *testing.T) {
		w := setupJsWrapper()
		execCtx := newExecCtx()
		tf := jsTestFormat()

		result, err := w.Execute(
			execCtx, "<code>\n</code>", tf,
		)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(
			t, "Code executed successfully.",
			result.Text,
		)
	})

	t.Run(
		"malformed tool.call returns error result",
		func(t *testing.T) {
			w := setupJsWrapper()
			execCtx := newExecCtx()
			tf := jsTestFormat()

			content := `<code>
var r = tool.call({args: {}});
if (r.error) {
  console.log("got error");
} else {
  console.log("unexpected success");
}
</code>`
			result, err := w.Execute(
				execCtx, content, tf,
			)
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(
				t,
				"<output>\ngot error\n</output>",
				result.Text,
			)
		},
	)

	t.Run(
		"context cancellation during code",
		func(t *testing.T) {
			ctx, cancel := context.WithCancel(
				context.Background(),
			)
			cancel() // Cancel immediately

			w := setupJsWrapper()
			execCtx := gent.NewExecutionContext(
				ctx, "test", nil,
			)
			tf := jsTestFormat()

			content := `<code>
while(true) {}
</code>`
			result, err := w.Execute(
				execCtx, content, tf,
			)
			// Should return error in result, not err
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(
				t,
				`<output>
execution interrupted: cancelled

1 | while(true) {}
    ^ cancelled

</output>`,
				result.Text,
			)
		},
	)

	t.Run(
		"raw results collected from code path",
		func(t *testing.T) {
			w := setupJsWrapper()
			execCtx := newExecCtx()
			tf := jsTestFormat()

			content := `<code>
var c = tool.call(
  {tool: "lookup_customer", args: {id: "C001"}}
);
console.log(JSON.stringify(c.output));
</code>`
			result, err := w.Execute(
				execCtx, content, tf,
			)
			require.NoError(t, err)
			require.NotNil(t, result.Raw)
			assert.NotEmpty(t, result.Raw.Calls)
			assert.Equal(
				t, "lookup_customer",
				result.Raw.Calls[0].Name,
			)
		},
	)

	t.Run(
		"tool error within code is catchable",
		func(t *testing.T) {
			w := setupJsWrapper()
			execCtx := newExecCtx()
			tf := jsTestFormat()

			content := `<code>
var r = tool.call(
  {tool: "fail_tool", args: {}}
);
if (r.error) {
  console.log("tool error: " + r.error);
} else {
  console.log("unexpected success");
}
</code>`
			result, err := w.Execute(
				execCtx, content, tf,
			)
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(
				t,
				"<tool_call_log>\n"+
					"[1] fail_tool({}) -> error: "+
					"assert.AnError general "+
					"error for testing\n"+
					"</tool_call_log>\n"+
					"<output>\n"+
					"tool error: assert.AnError "+
					"general error for testing\n"+
					"</output>",
				result.Text,
			)
		},
	)
}

// -------------------------------------------------------
// G. Raw result JSON sanity check
// -------------------------------------------------------

func TestJsWrapper_Execute_Code_RawResults(t *testing.T) {
	w := setupJsWrapper()
	execCtx := newExecCtx()
	tf := jsTestFormat()

	content := `<code>
var c = tool.call(
  {tool: "lookup_customer", args: {id: "C001"}}
);
var o = tool.call(
  {tool: "get_orders",
   args: {customer_id: "C001"}}
);
console.log("done");
</code>`
	result, err := w.Execute(execCtx, content, tf)
	require.NoError(t, err)
	require.NotNil(t, result.Raw)

	// Should have 2 tool calls in raw
	assert.Len(t, result.Raw.Calls, 2)
	assert.Equal(
		t, "lookup_customer",
		result.Raw.Calls[0].Name,
	)
	assert.Equal(
		t, "get_orders", result.Raw.Calls[1].Name,
	)

	// Results should be non-nil
	assert.Len(t, result.Raw.Results, 2)

	// Verify first result contains customer data
	firstOutput, ok := result.Raw.Results[0].
		Output.(string)
	require.True(t, ok)
	assert.Equal(
		t,
		`{"id":"C001","name":"Alice"}`,
		firstOutput,
	)
}

// -------------------------------------------------------
// H. Pre-validation
// -------------------------------------------------------

func TestJsWrapper_PreValidation(t *testing.T) {
	type input struct {
		content string
	}

	type expected struct {
		text       string
		emptyCalls bool
		codeExec   int64
		codeErr    int64
		errGauge   float64
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "invalid literal args caught",
			input: input{
				content: `<code>
var r = tool.call(
  {tool: "lookup_customer", args: {}}
);
console.log(r.output);
</code>`,
			},
			expected: expected{
				text: `<output>
1 schema pre-validation error(s):

--- Error 1: tool.call() at line 2 ---

2 |   {tool: "lookup_customer", args: {}}
      ^
3 | );
4 | console.log(r.output);

Invalid args for tool 'lookup_customer'.
Errors:
  - missing property 'id'
Expected fields:
  - 'args.id' (required, string)
Example:
  tool.call({
    tool: "lookup_customer",
    args: {
      "id": "..."
    }
  });

IMPORTANT: Use EXACT argument names and types from the tool schema.
Fix ALL errors above before re-submitting your code.

</output>`,
				emptyCalls: true,
				codeExec:   1,
				codeErr:    1,
				errGauge:   1.0,
			},
		},
		{
			name: "valid literal args pass",
			input: input{
				content: `<code>
var r = tool.call(
  {tool: "lookup_customer", args: {id: "C001"}}
);
console.log(r.output.id + ":" + r.output.name);
</code>`,
			},
			expected: expected{
				text: "<tool_call_log>\n" +
					`[1] lookup_customer({"id":"C001"})` +
					` -> {"id":"C001","name":"Alice"}` +
					"\n</tool_call_log>\n" +
					"<output>\n" +
					"C001:Alice" +
					"\n</output>",
				codeExec: 1,
			},
		},
		{
			name: "dynamic args skip pre-validation",
			input: input{
				content: `<code>
var myId = "C001";
var r = tool.call(
  {tool: "lookup_customer",
   args: {id: myId}}
);
console.log(r.output.id + ":" + r.output.name);
</code>`,
			},
			expected: expected{
				text: "<tool_call_log>\n" +
					`[1] lookup_customer({"id":"C001"})` +
					` -> {"id":"C001","name":"Alice"}` +
					"\n</tool_call_log>\n" +
					"<output>\n" +
					"C001:Alice" +
					"\n</output>",
				codeExec: 1,
			},
		},
		{
			name: "multiple invalid calls all reported",
			input: input{
				content: `<code>
var r1 = tool.call(
  {tool: "lookup_customer", args: {}}
);
var r2 = tool.call(
  {tool: "get_orders", args: {}}
);
</code>`,
			},
			expected: expected{
				text: `<output>
2 schema pre-validation error(s):

--- Error 1: tool.call() at line 2 ---

2 |   {tool: "lookup_customer", args: {}}
      ^
3 | );
4 | var r2 = tool.call(

Invalid args for tool 'lookup_customer'.
Errors:
  - missing property 'id'
Expected fields:
  - 'args.id' (required, string)
Example:
  tool.call({
    tool: "lookup_customer",
    args: {
      "id": "..."
    }
  });

--- Error 2: tool.call() at line 5 ---

5 |   {tool: "get_orders", args: {}}
      ^
6 | );

Invalid args for tool 'get_orders'.
Errors:
  - missing property 'customer_id'
Expected fields:
  - 'args.customer_id' (required, string)
Example:
  tool.call({
    tool: "get_orders",
    args: {
      "customer_id": "..."
    }
  });

IMPORTANT: Use EXACT argument names and types from the tool schema.
Fix ALL errors above before re-submitting your code.

</output>`,
				emptyCalls: true,
				codeExec:   1,
				codeErr:    1,
				errGauge:   1.0,
			},
		},
		{
			name: "nested object missing sub-field",
			input: input{
				content: `<code>
var r = tool.call(
  {tool: "update_address", args: {
    id: "1",
    address: { street: "123 Main St" }
  }}
);
console.log(r.output);
</code>`,
			},
			expected: expected{
				text: `<output>
1 schema pre-validation error(s):

--- Error 1: tool.call() at line 2 ---

2 |   {tool: "update_address", args: {
      ^
3 |     id: "1",
4 |     address: { street: "123 Main St" }

Invalid args for tool 'update_address'.
Errors:
  - missing property 'city' for 'args.address'
Expected fields:
  - 'args.address' (required, object)
  - 'args.id' (required, string)
Example:
  tool.call({
    tool: "update_address",
    args: {
      "address": {
        "city": "...",
        "street": "..."
      },
      "id": "..."
    }
  });

IMPORTANT: Use EXACT argument names and types from the tool schema.
Fix ALL errors above before re-submitting your code.

</output>`,
				emptyCalls: true,
				codeExec:   1,
				codeErr:    1,
				errGauge:   1.0,
			},
		},
		{
			name: "array of objects missing " +
				"item field",
			input: input{
				content: `<code>
var r = tool.call(
  {tool: "create_order", args: {
    customer_id: "C1",
    items: [{ name: "Widget" }]
  }}
);
console.log(r.output);
</code>`,
			},
			expected: expected{
				text: `<output>
1 schema pre-validation error(s):

--- Error 1: tool.call() at line 2 ---

2 |   {tool: "create_order", args: {
      ^
3 |     customer_id: "C1",
4 |     items: [{ name: "Widget" }]

Invalid args for tool 'create_order'.
Errors:
  - missing property 'qty' for 'args.items[]'
Expected fields:
  - 'args.customer_id' (required, string)
  - 'args.items' (required, array of object)
Example:
  tool.call({
    tool: "create_order",
    args: {
      "customer_id": "...",
      "items": [
        {
          "name": "...",
          "qty": 0
        }
      ]
    }
  });

IMPORTANT: Use EXACT argument names and types from the tool schema.
Fix ALL errors above before re-submitting your code.

</output>`,
				emptyCalls: true,
				codeExec:   1,
				codeErr:    1,
				errGauge:   1.0,
			},
		},
		{
			name: "map of objects missing " +
				"value field",
			input: input{
				content: `<code>
var r = tool.call(
  {tool: "update_stock", args: {
    id: "S1",
    quantities: { apples: { amount: 5 } }
  }}
);
console.log(r.output);
</code>`,
			},
			expected: expected{
				text: `<output>
1 schema pre-validation error(s):

--- Error 1: tool.call() at line 2 ---

2 |   {tool: "update_stock", args: {
      ^
3 |     id: "S1",
4 |     quantities: { apples: { amount: 5 } }

Invalid args for tool 'update_stock'.
Errors:
  - missing property 'unit' for 'args.quantities.apples'
Expected fields:
  - 'args.id' (required, string)
  - 'args.quantities' (required, object)
Example:
  tool.call({
    tool: "update_stock",
    args: {
      "id": "...",
      "quantities": {
        "<key>": {
          "amount": 0,
          "unit": "..."
        }
      }
    }
  });

IMPORTANT: Use EXACT argument names and types from the tool schema.
Fix ALL errors above before re-submitting your code.

</output>`,
				emptyCalls: true,
				codeExec:   1,
				codeErr:    1,
				errGauge:   1.0,
			},
		},
		{
			name: "mixed invalid literal and dynamic",
			input: input{
				content: `<code>
var r1 = tool.call(
  {tool: "lookup_customer", args: {}}
);
var myArgs = {customer_id: "C001"};
var r2 = tool.call(
  {tool: "get_orders", args: myArgs}
);
</code>`,
			},
			expected: expected{
				text: `<output>
1 schema pre-validation error(s):

--- Error 1: tool.call() at line 2 ---

2 |   {tool: "lookup_customer", args: {}}
      ^
3 | );
4 | var myArgs = {customer_id: "C001"};

Invalid args for tool 'lookup_customer'.
Errors:
  - missing property 'id'
Expected fields:
  - 'args.id' (required, string)
Example:
  tool.call({
    tool: "lookup_customer",
    args: {
      "id": "..."
    }
  });

IMPORTANT: Use EXACT argument names and types from the tool schema.
Fix ALL errors above before re-submitting your code.

</output>`,
				emptyCalls: true,
				codeExec:   1,
				codeErr:    1,
				errGauge:   1.0,
			},
		},
		{
			name: "object contains object " +
				"missing deep field",
			input: input{
				content: `<code>
var r = tool.call(
  {tool: "update_geo", args: {
    id: "1",
    address: {
      street: "Main St",
      geo: { lat: 1.0 }
    }
  }}
);
console.log(r.output);
</code>`,
			},
			expected: expected{
				text: `<output>
1 schema pre-validation error(s):

--- Error 1: tool.call() at line 2 ---

2 |   {tool: "update_geo", args: {
      ^
3 |     id: "1",
4 |     address: {

Invalid args for tool 'update_geo'.
Errors:
  - missing property 'lng' for 'args.address.geo'
Expected fields:
  - 'args.address' (required, object)
  - 'args.id' (required, string)
Example:
  tool.call({
    tool: "update_geo",
    args: {
      "address": {
        "geo": {
          "lat": 0,
          "lng": 0
        },
        "street": "..."
      },
      "id": "..."
    }
  });

IMPORTANT: Use EXACT argument names and types from the tool schema.
Fix ALL errors above before re-submitting your code.

</output>`,
				emptyCalls: true,
				codeExec:   1,
				codeErr:    1,
				errGauge:   1.0,
			},
		},
		{
			name: "array of object contains " +
				"array of object",
			input: input{
				content: `<code>
var r = tool.call(
  {tool: "create_shipment", args: {
    id: "S1",
    orders: [{
      order_id: "O1",
      items: [{ name: "Widget" }]
    }]
  }}
);
console.log(r.output);
</code>`,
			},
			expected: expected{
				text: `<output>
1 schema pre-validation error(s):

--- Error 1: tool.call() at line 2 ---

2 |   {tool: "create_shipment", args: {
      ^
3 |     id: "S1",
4 |     orders: [{

Invalid args for tool 'create_shipment'.
Errors:
  - missing property 'qty' for 'args.orders[].items[]'
Expected fields:
  - 'args.id' (required, string)
  - 'args.orders' (required, array of object)
Example:
  tool.call({
    tool: "create_shipment",
    args: {
      "id": "...",
      "orders": [
        {
          "items": [
            {
              "name": "...",
              "qty": 0
            }
          ],
          "order_id": "..."
        }
      ]
    }
  });

IMPORTANT: Use EXACT argument names and types from the tool schema.
Fix ALL errors above before re-submitting your code.

</output>`,
				emptyCalls: true,
				codeExec:   1,
				codeErr:    1,
				errGauge:   1.0,
			},
		},
		{
			name: "map of object contains " +
				"map of object",
			input: input{
				content: `<code>
var r = tool.call(
  {tool: "update_regions", args: {
    id: "R1",
    regions: {
      us: { zones: {
        west: { population: 1000 }
      }}
    }
  }}
);
console.log(r.output);
</code>`,
			},
			expected: expected{
				text: `<output>
1 schema pre-validation error(s):

--- Error 1: tool.call() at line 2 ---

2 |   {tool: "update_regions", args: {
      ^
3 |     id: "R1",
4 |     regions: {

Invalid args for tool 'update_regions'.
Errors:
  - missing property 'code' for 'args.regions.us.zones.west'
Expected fields:
  - 'args.id' (required, string)
  - 'args.regions' (required, object)
Example:
  tool.call({
    tool: "update_regions",
    args: {
      "id": "...",
      "regions": {
        "<key>": {
          "zones": {
            "<key>": {
              "code": "...",
              "population": 0
            }
          }
        }
      }
    }
  });

IMPORTANT: Use EXACT argument names and types from the tool schema.
Fix ALL errors above before re-submitting your code.

</output>`,
				emptyCalls: true,
				codeExec:   1,
				codeErr:    1,
				errGauge:   1.0,
			},
		},
		{
			name: "array of object contains " +
				"map of object",
			input: input{
				content: `<code>
var r = tool.call(
  {tool: "update_products", args: {
    id: "P1",
    items: [{
      name: "Widget",
      attributes: {
        weight: { value: "5" }
      }
    }]
  }}
);
console.log(r.output);
</code>`,
			},
			expected: expected{
				text: `<output>
1 schema pre-validation error(s):

--- Error 1: tool.call() at line 2 ---

2 |   {tool: "update_products", args: {
      ^
3 |     id: "P1",
4 |     items: [{

Invalid args for tool 'update_products'.
Errors:
  - missing property 'unit' for 'args.items[].attributes.weight'
Expected fields:
  - 'args.id' (required, string)
  - 'args.items' (required, array of object)
Example:
  tool.call({
    tool: "update_products",
    args: {
      "id": "...",
      "items": [
        {
          "attributes": {
            "<key>": {
              "unit": "...",
              "value": "..."
            }
          },
          "name": "..."
        }
      ]
    }
  });

IMPORTANT: Use EXACT argument names and types from the tool schema.
Fix ALL errors above before re-submitting your code.

</output>`,
				emptyCalls: true,
				codeExec:   1,
				codeErr:    1,
				errGauge:   1.0,
			},
		},
		{
			name: "map of object contains " +
				"array of object",
			input: input{
				content: `<code>
var r = tool.call(
  {tool: "update_catalog", args: {
    id: "C1",
    categories: {
      electronics: {
        products: [{ name: "Phone" }]
      }
    }
  }}
);
console.log(r.output);
</code>`,
			},
			expected: expected{
				text: `<output>
1 schema pre-validation error(s):

--- Error 1: tool.call() at line 2 ---

2 |   {tool: "update_catalog", args: {
      ^
3 |     id: "C1",
4 |     categories: {

Invalid args for tool 'update_catalog'.
Errors:
  - missing property 'price' for 'args.categories.electronics.products[]'
Expected fields:
  - 'args.categories' (required, object)
  - 'args.id' (required, string)
Example:
  tool.call({
    tool: "update_catalog",
    args: {
      "categories": {
        "<key>": {
          "products": [
            {
              "name": "...",
              "price": 0
            }
          ]
        }
      },
      "id": "..."
    }
  });

IMPORTANT: Use EXACT argument names and types from the tool schema.
Fix ALL errors above before re-submitting your code.

</output>`,
				emptyCalls: true,
				codeExec:   1,
				codeErr:    1,
				errGauge:   1.0,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := setupJsWrapper()
			execCtx := newExecCtx()
			tf := jsTestFormat()

			result, err := w.Execute(
				execCtx, tc.input.content, tf,
			)
			require.NoError(t, err)
			require.NotNil(t, result)

			assert.Equal(
				t, tc.expected.text,
				result.Text,
			)

			if tc.expected.emptyCalls {
				assert.Empty(t, result.Raw.Calls)
			}

			assert.Equal(
				t, tc.expected.codeExec,
				execCtx.Stats().GetCounter(
					gent.SCCodeExecutions,
				),
			)
			assert.Equal(
				t, tc.expected.codeErr,
				execCtx.Stats().GetCounter(
					gent.SCCodeExecutionsError,
				),
			)
			assert.Equal(
				t, tc.expected.errGauge,
				execCtx.Stats().GetGauge(
					gent.SGCodeExecutionsErrorConsecutive,
				),
			)
		})
	}
}

func TestJsWrapper_PreValidation_ConsecutiveReset(
	t *testing.T,
) {
	w := setupJsWrapper()
	execCtx := newExecCtx()
	tf := jsTestFormat()

	// First: pre-validation error
	errContent := `<code>
var r = tool.call(
  {tool: "lookup_customer", args: {}}
);
</code>`
	_, err := w.Execute(execCtx, errContent, tf)
	require.NoError(t, err)
	assert.Equal(
		t, 1.0,
		execCtx.Stats().GetGauge(
			gent.SGCodeExecutionsErrorConsecutive,
		),
	)

	// Second: success
	okContent := `<code>
console.log("ok");
</code>`
	_, err = w.Execute(execCtx, okContent, tf)
	require.NoError(t, err)
	assert.Equal(
		t, 0.0,
		execCtx.Stats().GetGauge(
			gent.SGCodeExecutionsErrorConsecutive,
		),
	)
}

// -------------------------------------------------------
// I. Bridge-path errors (dynamic args bypass
//    pre-validation, errors caught at runtime)
// -------------------------------------------------------

// enhancedSchemaLog builds the enhanced log entry for
// the tool_call_log section when a schema validation
// error occurs. prefix is e.g. "[1] lookup_customer({})".
// Uses FormatForLLM + WriteExampleCall to produce the
// same output as writeLogEntry in js_wrapper.go.
func enhancedSchemaLog(
	t *testing.T,
	w *JsToolChainWrapper,
	prefix string,
	toolName string,
	args map[string]any,
) string {
	t.Helper()
	sch := w.GetToolSchema(toolName)
	require.NotNil(t, sch)
	msg := sch.FormatForLLM(toolName, args)
	require.NotEmpty(t, msg)
	var sb strings.Builder
	sb.WriteString(prefix)
	sb.WriteString(" -> error:\n")
	sb.WriteString(msg)
	jsruntime.WriteExampleCall(
		&sb, toolName, sch,
	)
	return strings.TrimRight(
		sb.String(), "\n",
	)
}

func TestJsWrapper_BridgeSchemaError(t *testing.T) {
	// Use shared wrapper to get enhanced schema errors
	// via enhancedSchemaLog, avoiding machine-specific
	// file paths from the jsonschema library.
	w := setupJsWrapper()

	// bridgeText builds the expected result.Text for
	// bridge schema errors. logEntry is the enhanced
	// error for tool_call_log; outputBody is the
	// enhanced error from console.log(r.error).
	bridgeText := func(
		logEntry string, outputBody string,
	) string {
		return "<tool_call_log>\n" +
			logEntry +
			"\n</tool_call_log>\n" +
			"<output>\n" +
			outputBody +
			"\n</output>"
	}

	type input struct {
		content string
	}

	type expected struct {
		textFn   func() string
		codeExec int64
		codeErr  int64
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "dynamic args missing " +
				"required field",
			input: input{
				content: `<code>
var badArgs = {};
var r = tool.call(
  {tool: "lookup_customer", args: badArgs}
);
console.log(r.error);
</code>`,
			},
			expected: expected{
				textFn: func() string {
					logEntry := enhancedSchemaLog(
						t, w,
						"[1] lookup_customer({})",
						"lookup_customer",
						map[string]any{},
					)
					return bridgeText(
						logEntry,
						`tool.call() error at line 2:

1 | var badArgs = {};
2 | var r = tool.call(
                     ^ schema validation error
3 |   {tool: "lookup_customer", args: badArgs}
4 | );

Invalid args for tool 'lookup_customer'.
Errors:
  - missing property 'id'
Expected fields:
  - 'args.id' (required, string)
Example:
  tool.call({
    tool: "lookup_customer",
    args: {
      "id": "..."
    }
  });
`,
					)
				},
				codeExec: 1,
			},
		},
		{
			name: "missing tool field in req",
			input: input{
				content: `<code>
var req = {args: {id: "C001"}};
var r = tool.call(req);
console.log(r.error);
</code>`,
			},
			expected: expected{
				textFn: func() string {
					return `<output>
tool.call() error at line 2:

1 | var req = {args: {id: "C001"}};
2 | var r = tool.call(req);
                     ^ missing required field
3 | console.log(r.error);

Invalid tool.call() request.
Errors:
  - missing required 'tool' field
Expected format:
  tool.call({tool: "tool_name", args: {...}})

IMPORTANT: Use EXACT argument names and types from the tool schema.
Fix ALL errors above before re-submitting your code.
</output>`
				},
				codeExec: 1,
			},
		},
		{
			name: "missing args field in req",
			input: input{
				content: `<code>
var req = {tool: "lookup_customer"};
var r = tool.call(req);
console.log(r.error);
</code>`,
			},
			expected: expected{
				textFn: func() string {
					logEntry := enhancedSchemaLog(
						t, w,
						"[1] lookup_customer",
						"lookup_customer",
						nil,
					)
					return bridgeText(
						logEntry,
						`tool.call() error at line 2:

1 | var req = {tool: "lookup_customer"};
2 | var r = tool.call(req);
                     ^ schema validation error
3 | console.log(r.error);

Invalid args for tool 'lookup_customer'.
Errors:
  - args is null or missing, expected object with required properties: id
Expected fields:
  - 'args.id' (required, string)
Example:
  tool.call({
    tool: "lookup_customer",
    args: {
      "id": "..."
    }
  });
`,
					)
				},
				codeExec: 1,
			},
		},
		{
			name: "wrong field name in args",
			input: input{
				content: `<code>
var req = {
  tool: "lookup_customer",
  args: {name: "Alice"}
};
var r = tool.call(req);
console.log(r.error);
</code>`,
			},
			expected: expected{
				textFn: func() string {
					logEntry := enhancedSchemaLog(
						t, w,
						`[1] lookup_customer`+
							`({"name":"Alice"})`,
						"lookup_customer",
						map[string]any{
							"name": "Alice",
						},
					)
					return bridgeText(
						logEntry,
						`tool.call() error at line 5:

3 |   args: {name: "Alice"}
4 | };
5 | var r = tool.call(req);
                     ^ schema validation error
6 | console.log(r.error);

Invalid args for tool 'lookup_customer'.
Errors:
  - missing property 'id'
Expected fields:
  - 'args.id' (required, string)
Example:
  tool.call({
    tool: "lookup_customer",
    args: {
      "id": "..."
    }
  });
`,
					)
				},
				codeExec: 1,
			},
		},
		{
			name: "deep field error in array item",
			input: input{
				content: `<code>
var req = {
  tool: "create_order",
  args: {
    customer_id: "C001",
    items: [{name: "Widget"}]
  }
};
var r = tool.call(req);
console.log(r.error);
</code>`,
			},
			expected: expected{
				textFn: func() string {
					logEntry := enhancedSchemaLog(
						t, w,
						`[1] create_order(`+
							`{"customer_id":`+
							`"C001","items":`+
							`[{"name":"Widget"}]})`,
						"create_order",
						map[string]any{
							"customer_id": "C001",
							"items": []any{
								map[string]any{
									"name": "Widget",
								},
							},
						},
					)
					return bridgeText(
						logEntry,
						`tool.call() error at line 8:

6 |   }
7 | };
8 | var r = tool.call(req);
                     ^ schema validation error
9 | console.log(r.error);

Invalid args for tool 'create_order'.
Errors:
  - missing property 'qty' for 'args.items[]'
Expected fields:
  - 'args.customer_id' (required, string)
  - 'args.items' (required, array of object)
Example:
  tool.call({
    tool: "create_order",
    args: {
      "customer_id": "...",
      "items": [
        {
          "name": "...",
          "qty": 0
        }
      ]
    }
  });
`,
					)
				},
				codeExec: 1,
			},
		},
		{
			name: "nested object bridge error",
			input: input{
				content: `<code>
var req = {
  tool: "update_address",
  args: {
    id: "1",
    address: { street: "123 Main St" }
  }
};
var r = tool.call(req);
console.log(r.error);
</code>`,
			},
			expected: expected{
				textFn: func() string {
					logEntry := enhancedSchemaLog(
						t, w,
						`[1] update_address(`+
							`{"address":`+
							`{"street":"123 Main St"},`+
							`"id":"1"})`,
						"update_address",
						map[string]any{
							"id": "1",
							"address": map[string]any{
								"street": "123 Main St",
							},
						},
					)
					return bridgeText(
						logEntry,
						`tool.call() error at line 8:

6 |   }
7 | };
8 | var r = tool.call(req);
                     ^ schema validation error
9 | console.log(r.error);

Invalid args for tool 'update_address'.
Errors:
  - missing property 'city' for 'args.address'
Expected fields:
  - 'args.address' (required, object)
  - 'args.id' (required, string)
Example:
  tool.call({
    tool: "update_address",
    args: {
      "address": {
        "city": "...",
        "street": "..."
      },
      "id": "..."
    }
  });
`,
					)
				},
				codeExec: 1,
			},
		},
		{
			name: "map of objects bridge error",
			input: input{
				content: `<code>
var req = {
  tool: "update_stock",
  args: {
    id: "S1",
    quantities: {
      apples: { amount: 5 }
    }
  }
};
var r = tool.call(req);
console.log(r.error);
</code>`,
			},
			expected: expected{
				textFn: func() string {
					logEntry := enhancedSchemaLog(
						t, w,
						`[1] update_stock(`+
							`{"id":"S1",`+
							`"quantities":`+
							`{"apples":`+
							`{"amount":5}}})`,
						"update_stock",
						map[string]any{
							"id": "S1",
							"quantities": map[string]any{
								"apples": map[string]any{
									"amount": 5,
								},
							},
						},
					)
					return bridgeText(
						logEntry,
						`tool.call() error at line 10:

 8 |   }
 9 | };
10 | var r = tool.call(req);
                      ^ schema validation error
11 | console.log(r.error);

Invalid args for tool 'update_stock'.
Errors:
  - missing property 'unit' for 'args.quantities.apples'
Expected fields:
  - 'args.id' (required, string)
  - 'args.quantities' (required, object)
Example:
  tool.call({
    tool: "update_stock",
    args: {
      "id": "...",
      "quantities": {
        "<key>": {
          "amount": 0,
          "unit": "..."
        }
      }
    }
  });
`,
					)
				},
				codeExec: 1,
			},
		},
		{
			name: "object contains object " +
				"bridge error",
			input: input{
				content: `<code>
var req = {
  tool: "update_geo",
  args: {
    id: "1",
    address: {
      street: "Main St",
      geo: { lat: 1.0 }
    }
  }
};
var r = tool.call(req);
console.log(r.error);
</code>`,
			},
			expected: expected{
				textFn: func() string {
					logEntry := enhancedSchemaLog(
						t, w,
						`[1] update_geo(`+
							`{"address":`+
							`{"geo":{"lat":1},`+
							`"street":"Main St"},`+
							`"id":"1"})`,
						"update_geo",
						map[string]any{
							"id": "1",
							"address": map[string]any{
								"street": "Main St",
								"geo": map[string]any{
									"lat": 1.0,
								},
							},
						},
					)
					return bridgeText(
						logEntry,
						`tool.call() error at line 11:

 9 |   }
10 | };
11 | var r = tool.call(req);
                      ^ schema validation error
12 | console.log(r.error);

Invalid args for tool 'update_geo'.
Errors:
  - missing property 'lng' for 'args.address.geo'
Expected fields:
  - 'args.address' (required, object)
  - 'args.id' (required, string)
Example:
  tool.call({
    tool: "update_geo",
    args: {
      "address": {
        "geo": {
          "lat": 0,
          "lng": 0
        },
        "street": "..."
      },
      "id": "..."
    }
  });
`,
					)
				},
				codeExec: 1,
			},
		},
		{
			name: "array of object contains " +
				"array of object bridge error",
			input: input{
				content: `<code>
var req = {
  tool: "create_shipment",
  args: {
    id: "S1",
    orders: [{
      order_id: "O1",
      items: [{ name: "Widget" }]
    }]
  }
};
var r = tool.call(req);
console.log(r.error);
</code>`,
			},
			expected: expected{
				textFn: func() string {
					logEntry := enhancedSchemaLog(
						t, w,
						`[1] create_shipment(`+
							`{"id":"S1",`+
							`"orders":[{"items":`+
							`[{"name":"Widget"}],`+
							`"order_id":"O1"}]})`,
						"create_shipment",
						map[string]any{
							"id": "S1",
							"orders": []any{
								map[string]any{
									"order_id": "O1",
									"items": []any{
										map[string]any{
											"name": "Widget",
										},
									},
								},
							},
						},
					)
					return bridgeText(
						logEntry,
						`tool.call() error at line 11:

 9 |   }
10 | };
11 | var r = tool.call(req);
                      ^ schema validation error
12 | console.log(r.error);

Invalid args for tool 'create_shipment'.
Errors:
  - missing property 'qty' for 'args.orders[].items[]'
Expected fields:
  - 'args.id' (required, string)
  - 'args.orders' (required, array of object)
Example:
  tool.call({
    tool: "create_shipment",
    args: {
      "id": "...",
      "orders": [
        {
          "items": [
            {
              "name": "...",
              "qty": 0
            }
          ],
          "order_id": "..."
        }
      ]
    }
  });
`,
					)
				},
				codeExec: 1,
			},
		},
		{
			name: "map of object contains " +
				"map of object bridge error",
			input: input{
				content: `<code>
var req = {
  tool: "update_regions",
  args: {
    id: "R1",
    regions: {
      us: {
        zones: {
          west: { population: 1000 }
        }
      }
    }
  }
};
var r = tool.call(req);
console.log(r.error);
</code>`,
			},
			expected: expected{
				textFn: func() string {
					logEntry := enhancedSchemaLog(
						t, w,
						`[1] update_regions(`+
							`{"id":"R1",`+
							`"regions":{"us":`+
							`{"zones":{"west":`+
							`{"population":`+
							`1000}}}}})`,
						"update_regions",
						map[string]any{
							"id": "R1",
							"regions": map[string]any{
								"us": map[string]any{
									"zones": map[string]any{
										"west": map[string]any{
											"population": 1000,
										},
									},
								},
							},
						},
					)
					return bridgeText(
						logEntry,
						`tool.call() error at line 14:

12 |   }
13 | };
14 | var r = tool.call(req);
                      ^ schema validation error
15 | console.log(r.error);

Invalid args for tool 'update_regions'.
Errors:
  - missing property 'code' for 'args.regions.us.zones.west'
Expected fields:
  - 'args.id' (required, string)
  - 'args.regions' (required, object)
Example:
  tool.call({
    tool: "update_regions",
    args: {
      "id": "...",
      "regions": {
        "<key>": {
          "zones": {
            "<key>": {
              "code": "...",
              "population": 0
            }
          }
        }
      }
    }
  });
`,
					)
				},
				codeExec: 1,
			},
		},
		{
			name: "array of object contains " +
				"map of object bridge error",
			input: input{
				content: `<code>
var req = {
  tool: "update_products",
  args: {
    id: "P1",
    items: [{
      name: "Widget",
      attributes: {
        weight: { value: "5" }
      }
    }]
  }
};
var r = tool.call(req);
console.log(r.error);
</code>`,
			},
			expected: expected{
				textFn: func() string {
					logEntry := enhancedSchemaLog(
						t, w,
						`[1] update_products(`+
							`{"id":"P1",`+
							`"items":[{"attributes":`+
							`{"weight":{"value":"5"}},`+
							`"name":"Widget"}]})`,
						"update_products",
						map[string]any{
							"id": "P1",
							"items": []any{
								map[string]any{
									"name": "Widget",
									"attributes": map[string]any{
										"weight": map[string]any{
											"value": "5",
										},
									},
								},
							},
						},
					)
					return bridgeText(
						logEntry,
						`tool.call() error at line 13:

11 |   }
12 | };
13 | var r = tool.call(req);
                      ^ schema validation error
14 | console.log(r.error);

Invalid args for tool 'update_products'.
Errors:
  - missing property 'unit' for 'args.items[].attributes.weight'
Expected fields:
  - 'args.id' (required, string)
  - 'args.items' (required, array of object)
Example:
  tool.call({
    tool: "update_products",
    args: {
      "id": "...",
      "items": [
        {
          "attributes": {
            "<key>": {
              "unit": "...",
              "value": "..."
            }
          },
          "name": "..."
        }
      ]
    }
  });
`,
					)
				},
				codeExec: 1,
			},
		},
		{
			name: "map of object contains " +
				"array of object bridge error",
			input: input{
				content: `<code>
var req = {
  tool: "update_catalog",
  args: {
    id: "C1",
    categories: {
      electronics: {
        products: [{ name: "Phone" }]
      }
    }
  }
};
var r = tool.call(req);
console.log(r.error);
</code>`,
			},
			expected: expected{
				textFn: func() string {
					logEntry := enhancedSchemaLog(
						t, w,
						`[1] update_catalog(`+
							`{"categories":`+
							`{"electronics":`+
							`{"products":`+
							`[{"name":"Phone"}]}},`+
							`"id":"C1"})`,
						"update_catalog",
						map[string]any{
							"id": "C1",
							"categories": map[string]any{
								"electronics": map[string]any{
									"products": []any{
										map[string]any{
											"name": "Phone",
										},
									},
								},
							},
						},
					)
					return bridgeText(
						logEntry,
						`tool.call() error at line 12:

10 |   }
11 | };
12 | var r = tool.call(req);
                      ^ schema validation error
13 | console.log(r.error);

Invalid args for tool 'update_catalog'.
Errors:
  - missing property 'price' for 'args.categories.electronics.products[]'
Expected fields:
  - 'args.categories' (required, object)
  - 'args.id' (required, string)
Example:
  tool.call({
    tool: "update_catalog",
    args: {
      "categories": {
        "<key>": {
          "products": [
            {
              "name": "...",
              "price": 0
            }
          ]
        }
      },
      "id": "..."
    }
  });
`,
					)
				},
				codeExec: 1,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			execCtx := newExecCtx()
			tf := jsTestFormat()

			result, err := w.Execute(
				execCtx, tc.input.content, tf,
			)
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(
				t, tc.expected.textFn(),
				result.Text,
			)
			assert.Equal(
				t, tc.expected.codeExec,
				execCtx.Stats().GetCounter(
					gent.SCCodeExecutions,
				),
			)
			assert.Equal(
				t, tc.expected.codeErr,
				execCtx.Stats().GetCounter(
					gent.SCCodeExecutionsError,
				),
			)
		})
	}
}

// -------------------------------------------------------
// J. Unknown tool error propagation
// -------------------------------------------------------

func TestJsWrapper_UnknownToolError(t *testing.T) {
	w := setupJsWrapper()
	tf := jsTestFormat()

	content := `<code>
var r = tool.call({
  tool: "nonexistent_tool",
  args: {}
});
console.log(r.error);
</code>`

	execCtx := newExecCtx()
	result, err := w.Execute(
		execCtx, content, tf,
	)
	require.NoError(t, err)
	require.NotNil(t, result)

	// The error propagated from wrapped SearchJSON
	// should be ErrUnknownTool
	require.Len(t, result.Raw.Errors, 1)
	assert.ErrorIs(
		t, result.Raw.Errors[0],
		gent.ErrUnknownTool,
	)

	// Result text has tool_call_log with the error
	// plus output with what console.log printed.
	assert.Equal(
		t,
		"<tool_call_log>\n"+
			"[1] nonexistent_tool({}) -> error: "+
			"unknown tool: nonexistent_tool\n"+
			"</tool_call_log>\n"+
			"<output>\n"+
			"unknown tool: nonexistent_tool\n"+
			"</output>",
		result.Text,
	)
}

// -------------------------------------------------------
// K. Tool call log observation split — comprehensive
//    tests for the <tool_call_log> + <output> format.
// -------------------------------------------------------

func TestJsWrapper_ToolCallLog(t *testing.T) {
	type input struct {
		content string
	}

	type expected struct {
		text string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "tool error then success — " +
				"both appear in log",
			input: input{
				content: `<code>
var r1 = tool.call(
  {tool: "fail_tool", args: {}}
);
var r2 = tool.call(
  {tool: "lookup_customer",
   args: {id: "C001"}}
);
console.log(
  "err=" + (r1.error ? "yes" : "no") +
  " ok=" + (r2.error ? "no" : "yes")
);
</code>`,
			},
			expected: expected{
				text: `<tool_call_log>
[1] fail_tool({}) -> error: assert.AnError general error for testing
[2] lookup_customer({"id":"C001"}) -> {"id":"C001","name":"Alice"}
</tool_call_log>
<output>
err=yes ok=yes
</output>`,
			},
		},
		{
			name: "success then error — " +
				"both appear in log",
			input: input{
				content: `<code>
var r1 = tool.call(
  {tool: "lookup_customer",
   args: {id: "C001"}}
);
var r2 = tool.call(
  {tool: "fail_tool", args: {}}
);
console.log(
  "ok=" + (r1.error ? "no" : "yes") +
  " err=" + (r2.error ? "yes" : "no")
);
</code>`,
			},
			expected: expected{
				text: `<tool_call_log>
[1] lookup_customer({"id":"C001"}) -> {"id":"C001","name":"Alice"}
[2] fail_tool({}) -> error: assert.AnError general error for testing
</tool_call_log>
<output>
ok=yes err=yes
</output>`,
			},
		},
		{
			name: "parallel calls all success",
			input: input{
				content: `<code>
var results = tool.parallelCall([
  {tool: "lookup_customer",
   args: {id: "C001"}},
  {tool: "get_orders",
   args: {customer_id: "C001"}}
]);
console.log("done");
</code>`,
			},
			expected: expected{
				text: `<tool_call_log>
[1a] lookup_customer({"id":"C001"}) -> {"id":"C001","name":"Alice"}
[1b] get_orders({"customer_id":"C001"}) -> [{"order_id":"O1"}]
</tool_call_log>
<output>
done
</output>`,
			},
		},
		{
			name: "parallel calls all failed",
			input: input{
				content: `<code>
var results = tool.parallelCall([
  {tool: "fail_tool", args: {}},
  {tool: "fail_tool", args: {}}
]);
console.log(
  "a=" + (results[0].error ? "err" : "ok") +
  " b=" + (results[1].error ? "err" : "ok")
);
</code>`,
			},
			expected: expected{
				text: `<tool_call_log>
[1a] fail_tool({}) -> error: assert.AnError general error for testing
[1b] fail_tool({}) -> error: assert.AnError general error for testing
</tool_call_log>
<output>
a=err b=err
</output>`,
			},
		},
		{
			name: "parallel calls partial " +
				"success and failure",
			input: input{
				content: `<code>
var results = tool.parallelCall([
  {tool: "lookup_customer",
   args: {id: "C001"}},
  {tool: "fail_tool", args: {}}
]);
console.log(
  "a=" + (results[0].error ? "err" : "ok") +
  " b=" + (results[1].error ? "err" : "ok")
);
</code>`,
			},
			expected: expected{
				text: `<tool_call_log>
[1a] lookup_customer({"id":"C001"}) -> {"id":"C001","name":"Alice"}
[1b] fail_tool({}) -> error: assert.AnError general error for testing
</tool_call_log>
<output>
a=ok b=err
</output>`,
			},
		},
		{
			name: "sequential then parallel — " +
				"mixed numbering",
			input: input{
				content: `<code>
var r = tool.call(
  {tool: "lookup_customer",
   args: {id: "C001"}}
);
var results = tool.parallelCall([
  {tool: "get_orders",
   args: {customer_id: "C001"}},
  {tool: "get_orders",
   args: {customer_id: "C001"}}
]);
console.log("done");
</code>`,
			},
			expected: expected{
				text: `<tool_call_log>
[1] lookup_customer({"id":"C001"}) -> {"id":"C001","name":"Alice"}
[2a] get_orders({"customer_id":"C001"}) -> [{"order_id":"O1"}]
[2b] get_orders({"customer_id":"C001"}) -> [{"order_id":"O1"}]
</tool_call_log>
<output>
done
</output>`,
			},
		},
		{
			name: "no tool calls no console.log",
			input: input{
				content: `<code>
var x = 1 + 2;
</code>`,
			},
			expected: expected{
				text: `<output>
Code executed successfully.
</output>`,
			},
		},
		{
			name: "tool calls but no console.log",
			input: input{
				content: `<code>
tool.call(
  {tool: "lookup_customer",
   args: {id: "C001"}}
);
</code>`,
			},
			expected: expected{
				text: `<tool_call_log>
[1] lookup_customer({"id":"C001"}) -> {"id":"C001","name":"Alice"}
</tool_call_log>
<output>
Code executed successfully.
</output>`,
			},
		},
		{
			name: "runtime error after successful " +
				"tool call — log + code_error",
			input: input{
				content: `<code>
var r = tool.call(
  {tool: "lookup_customer",
   args: {id: "C001"}}
);
undefinedVar;
</code>`,
			},
			expected: expected{
				text: `<tool_call_log>
[1] lookup_customer({"id":"C001"}) -> {"id":"C001","name":"Alice"}
</tool_call_log>
<output>
ReferenceError: undefinedVar is not defined

3 |    args: {id: "C001"}}
4 | );
5 | undefinedVar;
    ^ ReferenceError: undefinedVar is not defined

</output>`,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := setupJsWrapper()
			execCtx := newExecCtx()
			tf := jsTestFormat()

			result, err := w.Execute(
				execCtx, tc.input.content, tf,
			)
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(
				t, tc.expected.text,
				result.Text,
			)
		})
	}
}

// -------------------------------------------------------
// L. Dual mode — direct_call + code in same action
// -------------------------------------------------------

func TestJsWrapper_DualMode(t *testing.T) {
	type input struct {
		content string
	}

	type expected struct {
		text          string
		codeExec      int64
		toolCallCount int64
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "both direct_call and code",
			input: input{
				content: `<direct_call>
{"tool": "lookup_customer", "args": {"id": "C001"}}
</direct_call>
<code>
(function() {
var r = tool.call(
  {tool: "get_orders",
   args: {customer_id: "C001"}}
);
if (r.error) return;
})();
</code>`,
			},
			expected: expected{
				text: `<direct_call>
<lookup_customer>
"{\"id\":\"C001\",\"name\":\"Alice\"}"
</lookup_customer>
</direct_call>
<code_execution>
<tool_call_log>
[1] get_orders({"customer_id":"C001"}) -> [{"order_id":"O1"}]
</tool_call_log>
<output>
Code executed successfully.
</output>
</code_execution>`,
				codeExec:      1,
				toolCallCount: 2,
			},
		},
		{
			name: "direct_call with code " +
				"console.log output",
			input: input{
				content: `<direct_call>
{"tool": "lookup_customer", "args": {"id": "C001"}}
</direct_call>
<code>
(function() {
var r = tool.call(
  {tool: "get_orders",
   args: {customer_id: "C001"}}
);
if (r.error) return;
console.log("orders: " + r.output.length);
})();
</code>`,
			},
			expected: expected{
				text: `<direct_call>
<lookup_customer>
"{\"id\":\"C001\",\"name\":\"Alice\"}"
</lookup_customer>
</direct_call>
<code_execution>
<tool_call_log>
[1] get_orders({"customer_id":"C001"}) -> [{"order_id":"O1"}]
</tool_call_log>
<output>
orders: 1
</output>
</code_execution>`,
				codeExec:      1,
				toolCallCount: 2,
			},
		},
		{
			name: "direct_call error does not " +
				"block code",
			input: input{
				content: `<direct_call>
{"tool": "nonexistent", "args": {}}
</direct_call>
<code>
(function() {
var r = tool.call(
  {tool: "lookup_customer",
   args: {id: "C001"}}
);
if (r.error) return;
console.log("found: " + r.output.name);
})();
</code>`,
			},
			expected: expected{
				codeExec:      1,
				toolCallCount: 1,
			},
		},
		{
			name: "code error does not block " +
				"direct_call result",
			input: input{
				content: `<direct_call>
{"tool": "lookup_customer", "args": {"id": "C001"}}
</direct_call>
<code>
undefinedVar;
</code>`,
			},
			expected: expected{
				text: `<direct_call>
<lookup_customer>
"{\"id\":\"C001\",\"name\":\"Alice\"}"
</lookup_customer>
</direct_call>
<code_execution>
<output>
ReferenceError: undefinedVar is not defined

1 | undefinedVar;
    ^ ReferenceError: undefinedVar is not defined

</output>
</code_execution>`,
				codeExec:      1,
				toolCallCount: 1,
			},
		},
		{
			name: "direct_call only — no wrapper",
			input: input{
				content: `<direct_call>
{"tool": "lookup_customer", "args": {"id": "C001"}}
</direct_call>`,
			},
			expected: expected{
				text: `<lookup_customer>
"{\"id\":\"C001\",\"name\":\"Alice\"}"
</lookup_customer>`,
				toolCallCount: 1,
			},
		},
		{
			name: "code only — no wrapper",
			input: input{
				content: `<code>
(function() {
var r = tool.call(
  {tool: "lookup_customer",
   args: {id: "C001"}}
);
if (r.error) return;
})();
</code>`,
			},
			expected: expected{
				text: `<tool_call_log>
[1] lookup_customer({"id":"C001"}) -> {"id":"C001","name":"Alice"}
</tool_call_log>
<output>
Code executed successfully.
</output>`,
				codeExec:      1,
				toolCallCount: 1,
			},
		},
		{
			name: "both with empty code block",
			input: input{
				content: `<direct_call>
{"tool": "lookup_customer", "args": {"id": "C001"}}
</direct_call>
<code>
</code>`,
			},
			expected: expected{
				text: `<direct_call>
<lookup_customer>
"{\"id\":\"C001\",\"name\":\"Alice\"}"
</lookup_customer>
</direct_call>
<code_execution>
Code executed successfully.
</code_execution>`,
				toolCallCount: 1,
			},
		},
		{
			name: "both with parallel direct_call",
			input: input{
				content: `<direct_call>
[{"tool": "lookup_customer", "args": {"id": "C001"}},
 {"tool": "get_orders", "args": {"customer_id": "C001"}}]
</direct_call>
<code>
console.log("computed: 42");
</code>`,
			},
			expected: expected{
				text: `<direct_call>
<lookup_customer>
"{\"id\":\"C001\",\"name\":\"Alice\"}"
</lookup_customer>
<get_orders>
"[{\"order_id\":\"O1\"}]"
</get_orders>
</direct_call>
<code_execution>
<output>
computed: 42
</output>
</code_execution>`,
				codeExec:      1,
				toolCallCount: 2,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := setupJsWrapper()
			execCtx := newExecCtx()
			tf := jsTestFormat()

			result, err := w.Execute(
				execCtx, tc.input.content, tf,
			)
			require.NoError(t, err)
			require.NotNil(t, result)

			if tc.expected.text != "" {
				assert.Equal(
					t, tc.expected.text,
					result.Text,
				)
			}

			if tc.expected.codeExec > 0 {
				assert.Equal(
					t, tc.expected.codeExec,
					execCtx.Stats().GetCounter(
						gent.SCCodeExecutions,
					),
				)
			}
			if tc.expected.toolCallCount > 0 {
				assert.Equal(
					t, tc.expected.toolCallCount,
					execCtx.Stats().GetCounter(
						gent.SCToolCalls,
					),
				)
			}
		})
	}
}

// -------------------------------------------------------
// M. Direct call disabled — code-only mode
// -------------------------------------------------------

func TestJsWrapper_DirectCallDisabled(t *testing.T) {
	setupCodeOnly := func() *JsToolChainWrapper {
		return setupJsWrapper().
			WithDirectCallDisabled()
	}

	t.Run("guidance shows code only", func(t *testing.T) {
		w := setupCodeOnly()
		guidance := w.Guidance()
		assert.Contains(t, guidance, "<code>")
		assert.Contains(
			t, guidance, "Call tools using JavaScript",
		)
		assert.NotContains(
			t, guidance, "direct_call",
		)
		assert.NotContains(
			t, guidance, "two ways",
		)
	})

	t.Run(
		"guidance with custom code guidance",
		func(t *testing.T) {
			w := setupCodeOnly().WithCodeGuidance(
				"Custom code instructions",
			)
			guidance := w.Guidance()
			assert.Contains(
				t, guidance,
				"Custom code instructions",
			)
			assert.NotContains(
				t, guidance, "direct_call",
			)
		},
	)

	t.Run(
		"direct_call tags ignored in Execute",
		func(t *testing.T) {
			w := setupCodeOnly()
			execCtx := newExecCtx()
			tf := jsTestFormat()

			content := `<direct_call>
{"tool": "lookup_customer", "args": {"id": "C001"}}
</direct_call>`
			result, err := w.Execute(
				execCtx, content, tf,
			)
			require.NoError(t, err)
			require.NotNil(t, result)

			// direct_call ignored, content treated as
			// code fallback (not valid JS, errors)
			assert.Contains(
				t, result.Text, "<output>",
			)
		},
	)

	t.Run(
		"code executes normally",
		func(t *testing.T) {
			w := setupCodeOnly()
			execCtx := newExecCtx()
			tf := jsTestFormat()

			content := `<code>
var r = tool.call(
  {tool: "lookup_customer",
   args: {id: "C001"}}
);
console.log(r.output.name);
</code>`
			result, err := w.Execute(
				execCtx, content, tf,
			)
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(
				t,
				`<tool_call_log>
[1] lookup_customer({"id":"C001"}) -> {"id":"C001","name":"Alice"}
</tool_call_log>
<output>
Alice
</output>`,
				result.Text,
			)
		},
	)

	t.Run(
		"raw content without code tags "+
			"treated as code",
		func(t *testing.T) {
			w := setupCodeOnly()
			execCtx := newExecCtx()
			tf := jsTestFormat()

			content := `console.log("hello");`
			result, err := w.Execute(
				execCtx, content, tf,
			)
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(
				t,
				`<output>
hello
</output>`,
				result.Text,
			)
		},
	)

	t.Run(
		"ParseSection skips direct_call",
		func(t *testing.T) {
			w := setupCodeOnly()
			execCtx := newExecCtx()

			content := `<direct_call>
{"tool": "lookup_customer", "args": {"id": "C001"}}
</direct_call>
<code>
console.log("from code");
</code>`
			parsed, err := w.ParseSection(
				execCtx, content,
			)
			require.NoError(t, err)

			// Should return the code string, not
			// parsed tool calls
			str, ok := parsed.(string)
			assert.True(t, ok, "expected string")
			assert.Contains(
				t, str, "console.log",
			)
		},
	)
}
