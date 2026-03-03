package toolchain

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/format"
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

	engines := []gent.SearchEngine{
		&mockSearchEngine{
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

	// Contains JS environment description
	assert.Contains(t, prompt, "tool.call")
	assert.Contains(t, prompt, "tool.parallelCall")
	assert.Contains(t, prompt, "console.log")
}

// -------------------------------------------------------
// B. ParseSection — sub-section detection
// -------------------------------------------------------

func TestJsWrapper_ParseSection(t *testing.T) {
	tests := []struct {
		name  string
		input string
		expected struct {
			isCode     bool
			isToolCall bool
			errContain string
		}
	}{
		{
			name: "direct_call delegates to wrapped",
			input: `<direct_call>
{"tool": "lookup_customer", "args": {"id": "C1"}}
</direct_call>`,
			expected: struct {
				isCode     bool
				isToolCall bool
				errContain string
			}{
				isToolCall: true,
			},
		},
		{
			name: "code returns string",
			input: `<code>
var x = tool.call({tool: "lookup_customer", args: {id: "C1"}});
console.log(JSON.stringify(x));
</code>`,
			expected: struct {
				isCode     bool
				isToolCall bool
				errContain string
			}{
				isCode: true,
			},
		},
		{
			name: "neither — fallback to wrapped",
			input: `{"tool": "lookup_customer", ` +
				`"args": {"id": "C1"}}`,
			expected: struct {
				isCode     bool
				isToolCall bool
				errContain string
			}{
				isToolCall: true,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := setupJsWrapper()
			execCtx := newExecCtx()
			result, err := w.ParseSection(
				execCtx, tc.input,
			)

			if tc.expected.errContain != "" {
				require.Error(t, err)
				assert.Contains(
					t, err.Error(),
					tc.expected.errContain,
				)
				return
			}

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
	tests := []struct {
		name  string
		input string
		expected struct {
			textContains string
			errNil       bool
		}
	}{
		{
			name: "single tool call passes through",
			input: `<direct_call>
{"tool": "lookup_customer", "args": {"id": "C001"}}
</direct_call>`,
			expected: struct {
				textContains string
				errNil       bool
			}{
				textContains: "Alice",
				errNil:       true,
			},
		},
		{
			name: "parallel tool calls pass through",
			input: `<direct_call>
[{"tool": "lookup_customer", "args": {"id": "C001"}},
 {"tool": "get_orders", "args": {"customer_id": "C001"}}]
</direct_call>`,
			expected: struct {
				textContains string
				errNil       bool
			}{
				textContains: "Alice",
				errNil:       true,
			},
		},
		{
			name: "fallback without tags",
			input: `{"tool": "lookup_customer", ` +
				`"args": {"id": "C002"}}`,
			expected: struct {
				textContains string
				errNil       bool
			}{
				textContains: "Alice",
				errNil:       true,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := setupJsWrapper()
			execCtx := newExecCtx()
			tf := jsTestFormat()

			result, err := w.Execute(
				execCtx, tc.input, tf,
			)

			if tc.expected.errNil {
				require.NoError(t, err)
			}
			require.NotNil(t, result)
			assert.Contains(
				t, result.Text,
				tc.expected.textContains,
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
	tests := []struct {
		name  string
		input string
		expected struct {
			textContains   string
			textNotContain string
			hasError       bool
		}
	}{
		{
			name: "simple tool.call with console.log",
			input: `<code>
var c = tool.call(
  {tool: "lookup_customer", args: {id: "C001"}}
);
console.log(JSON.stringify(c.output));
</code>`,
			expected: struct {
				textContains   string
				textNotContain string
				hasError       bool
			}{
				textContains: "Alice",
			},
		},
		{
			name: "chained calls",
			input: `<code>
var c = tool.call(
  {tool: "lookup_customer", args: {id: "C001"}}
);
var o = tool.call(
  {tool: "get_orders",
   args: {customer_id: c.output.id}}
);
console.log(JSON.stringify({
  customer: c.output, orders: o.output
}));
</code>`,
			expected: struct {
				textContains   string
				textNotContain string
				hasError       bool
			}{
				textContains: "Alice",
			},
		},
		{
			name: "no console.log uses tool output",
			input: `<code>
var c = tool.call(
  {tool: "lookup_customer", args: {id: "C001"}}
);
</code>`,
			expected: struct {
				textContains   string
				textNotContain string
				hasError       bool
			}{
				textContains: "lookup_customer",
			},
		},
		{
			name: "JS syntax error",
			input: `<code>
var x = @;
</code>`,
			expected: struct {
				textContains   string
				textNotContain string
				hasError       bool
			}{
				textContains: "SyntaxError",
				hasError:     true,
			},
		},
		{
			name: "JS runtime error (ReferenceError)",
			input: `<code>
undefinedVar;
</code>`,
			expected: struct {
				textContains   string
				textNotContain string
				hasError       bool
			}{
				textContains: "ReferenceError",
				hasError:     true,
			},
		},
		{
			name: "code that calls no tools",
			input: `<code>
console.log("hello from JS");
</code>`,
			expected: struct {
				textContains   string
				textNotContain string
				hasError       bool
			}{
				textContains: "hello from JS",
			},
		},
		{
			name: "multiple console.log calls",
			input: `<code>
console.log("line1");
console.log("line2");
</code>`,
			expected: struct {
				textContains   string
				textNotContain string
				hasError       bool
			}{
				textContains: "line1",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := setupJsWrapper()
			execCtx := newExecCtx()
			tf := jsTestFormat()

			result, err := w.Execute(
				execCtx, tc.input, tf,
			)

			// Code execution should never return
			// a Go error — errors are in the result
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Contains(
				t, result.Text,
				tc.expected.textContains,
			)
			if tc.expected.textNotContain != "" {
				assert.NotContains(
					t, result.Text,
					tc.expected.textNotContain,
				)
			}
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

		content := `<code>
</code>`
		result, err := w.Execute(
			execCtx, content, tf,
		)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Contains(
			t, result.Text,
			"Code executed successfully",
		)
	})

	t.Run(
		"code with malformed tool.call request",
		func(t *testing.T) {
			w := setupJsWrapper()
			execCtx := newExecCtx()
			tf := jsTestFormat()

			content := `<code>
try {
  tool.call({args: {}});
} catch(e) {
  console.log("caught: " + e.message);
}
</code>`
			result, err := w.Execute(
				execCtx, content, tf,
			)
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Contains(
				t, result.Text, "caught:",
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
			// Should return error in result, not as err
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Contains(
				t, result.Text, "interrupted",
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

			// fail_tool always returns error. The
			// error should show up in the tool.call
			// result, not crash the JS code.
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
			// The error from fail_tool appears in
			// the result wrapping, not in JS throw.
			// But since execute returns an error for
			// fail_tool, it will be caught by our
			// MakeToolCallFn wrapper. Let me check
			// what actually happens...
			// The wrapped SearchJSON.Execute returns
			// a result with the error in Raw.Errors.
			// Our bridge sees this and returns
			// {name, error} to JS.
			assert.Contains(
				t, result.Text, "tool error:",
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
	firstOutput, _ := json.Marshal(
		result.Raw.Results[0].Output,
	)
	assert.Contains(
		t, string(firstOutput), "Alice",
	)
}
