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
	type expected struct {
		isCode     bool
		isToolCall bool
	}

	tests := []struct {
		name     string
		input    string
		expected expected
	}{
		{
			name: "direct_call delegates to wrapped",
			input: `<direct_call>
{"tool": "lookup_customer", "args": {"id": "C1"}}
</direct_call>`,
			expected: expected{
				isToolCall: true,
			},
		},
		{
			name: "code returns string",
			input: `<code>
var x = tool.call({tool: "lookup_customer", args: {id: "C1"}});
console.log(JSON.stringify(x));
</code>`,
			expected: expected{
				isCode: true,
			},
		},
		{
			name: "neither — fallback to wrapped",
			input: `{"tool": "lookup_customer", "args": {"id": "C1"}}`,
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
				execCtx, tc.input,
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
	type expected struct {
		text string
	}

	tests := []struct {
		name     string
		input    string
		expected expected
	}{
		{
			name: "single tool call passes through",
			input: `<direct_call>
{"tool": "lookup_customer", "args": {"id": "C001"}}
</direct_call>`,
			expected: expected{
				text: `<lookup_customer>
"{\"id\":\"C001\",\"name\":\"Alice\"}"
</lookup_customer>`,
			},
		},
		{
			name: "parallel tool calls pass through",
			input: `<direct_call>
[{"tool": "lookup_customer", "args": {"id": "C001"}},
 {"tool": "get_orders", "args": {"customer_id": "C001"}}]
</direct_call>`,
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
			input: `{"tool": "lookup_customer", "args": {"id": "C002"}}`,
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
				execCtx, tc.input, tf,
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
	type expected struct {
		text    string
		hasErr  bool
		notText string
	}

	tests := []struct {
		name     string
		input    string
		expected expected
	}{
		{
			name: "simple tool.call with console.log",
			input: `<code>
var c = tool.call(
  {tool: "lookup_customer", args: {id: "C001"}}
);
console.log(JSON.stringify(c.output));
</code>`,
			expected: expected{
				text: `{"id":"C001","name":"Alice"}`,
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
			expected: expected{
				text: `{"customer":{"id":"C001","name":"Alice"},"orders":[{"order_id":"O1"}]}`,
			},
		},
		{
			name: "no console.log uses tool output",
			input: `<code>
var c = tool.call(
  {tool: "lookup_customer", args: {id: "C001"}}
);
</code>`,
			expected: expected{
				text: `<lookup_customer>
"{\"id\":\"C001\",\"name\":\"Alice\"}"
</lookup_customer>`,
			},
		},
		{
			name: "JS syntax error",
			input: `<code>
var x = @;
</code>`,
			expected: expected{
				text: `<code_error>
SyntaxError: SyntaxError: (anonymous): Line 1:9 Unexpected token ILLEGAL (and 2 more errors)

</code_error>`,
				hasErr: true,
			},
		},
		{
			name: "JS runtime error (ReferenceError)",
			input: `<code>
undefinedVar;
</code>`,
			expected: expected{
				text: `<code_error>
ReferenceError: undefinedVar is not defined

1 | undefinedVar;
    ^ ReferenceError: undefinedVar is not defined

</code_error>`,
				hasErr: true,
			},
		},
		{
			name: "code that calls no tools",
			input: `<code>
console.log("hello from JS");
</code>`,
			expected: expected{
				text: "hello from JS",
			},
		},
		{
			name: "multiple console.log calls",
			input: `<code>
console.log("line1");
console.log("line2");
</code>`,
			expected: expected{
				text: "line1\nline2",
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
			assert.Equal(
				t, tc.expected.text, result.Text,
			)
			if tc.expected.notText != "" {
				assert.NotContains(
					t, result.Text,
					tc.expected.notText,
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
			// Should return error in result, not err
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

// -------------------------------------------------------
// H. Pre-validation
// -------------------------------------------------------

func TestJsWrapper_PreValidation(t *testing.T) {
	t.Run(
		"invalid literal args caught by "+
			"pre-validation",
		func(t *testing.T) {
			w := setupJsWrapper()
			execCtx := newExecCtx()
			tf := jsTestFormat()

			// lookup_customer requires "id" field
			content := `<code>
var r = tool.call(
  {tool: "lookup_customer", args: {}}
);
console.log(r.output);
</code>`
			result, err := w.Execute(
				execCtx, content, tf,
			)
			require.NoError(t, err)
			require.NotNil(t, result)

			// Pre-validation should catch the
			// missing "id" field
			assert.Contains(
				t, result.Text,
				"schema pre-validation error",
			)
			assert.Contains(
				t, result.Text, "id",
			)

			// No tools should have been executed
			assert.Empty(t, result.Raw.Calls)

			// Error stats incremented
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
		"valid literal args pass pre-validation",
		func(t *testing.T) {
			w := setupJsWrapper()
			execCtx := newExecCtx()
			tf := jsTestFormat()

			content := `<code>
var r = tool.call(
  {tool: "lookup_customer", args: {id: "C001"}}
);
console.log(JSON.stringify(r.output));
</code>`
			result, err := w.Execute(
				execCtx, content, tf,
			)
			require.NoError(t, err)
			require.NotNil(t, result)

			// Should pass pre-validation and
			// execute successfully
			assert.Contains(
				t, result.Text, "Alice",
			)
			assert.NotContains(
				t, result.Text,
				"schema pre-validation error",
			)
		},
	)

	t.Run(
		"dynamic args skip pre-validation",
		func(t *testing.T) {
			w := setupJsWrapper()
			execCtx := newExecCtx()
			tf := jsTestFormat()

			content := `<code>
var myId = "C001";
var r = tool.call(
  {tool: "lookup_customer",
   args: {id: myId}}
);
console.log(JSON.stringify(r.output));
</code>`
			result, err := w.Execute(
				execCtx, content, tf,
			)
			require.NoError(t, err)
			require.NotNil(t, result)

			// Dynamic args are skipped — tool
			// executes normally
			assert.Contains(
				t, result.Text, "Alice",
			)
		},
	)

	t.Run(
		"multiple invalid calls all reported",
		func(t *testing.T) {
			w := setupJsWrapper()
			execCtx := newExecCtx()
			tf := jsTestFormat()

			content := `<code>
var r1 = tool.call(
  {tool: "lookup_customer", args: {}}
);
var r2 = tool.call(
  {tool: "get_orders", args: {}}
);
</code>`
			result, err := w.Execute(
				execCtx, content, tf,
			)
			require.NoError(t, err)
			require.NotNil(t, result)

			// Both errors reported
			assert.Contains(
				t, result.Text,
				"2 schema pre-validation error",
			)
			assert.Contains(
				t, result.Text,
				"lookup_customer",
			)
			assert.Contains(
				t, result.Text,
				"get_orders",
			)
		},
	)

	t.Run(
		"pre-validation error resets on success",
		func(t *testing.T) {
			w := setupJsWrapper()
			execCtx := newExecCtx()
			tf := jsTestFormat()

			// First: pre-validation error
			errContent := `<code>
var r = tool.call(
  {tool: "lookup_customer", args: {}}
);
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
		},
	)

	t.Run(
		"mixed invalid and dynamic calls",
		func(t *testing.T) {
			w := setupJsWrapper()
			execCtx := newExecCtx()
			tf := jsTestFormat()

			// One invalid literal call, one dynamic
			content := `<code>
var r1 = tool.call(
  {tool: "lookup_customer", args: {}}
);
var myArgs = {customer_id: "C001"};
var r2 = tool.call(
  {tool: "get_orders", args: myArgs}
);
</code>`
			result, err := w.Execute(
				execCtx, content, tf,
			)
			require.NoError(t, err)
			require.NotNil(t, result)

			// Only the invalid literal call caught
			assert.Contains(
				t, result.Text,
				"1 schema pre-validation error",
			)
			assert.Contains(
				t, result.Text,
				"lookup_customer",
			)
		},
	)
}

// -------------------------------------------------------
// I. Unknown tool error propagation
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

	t.Logf("Text: %q", result.Text)
	for i, e := range result.Raw.Errors {
		if e != nil {
			t.Logf("Raw.Errors[%d]: %v", i, e)
		}
	}

	// The error propagated from wrapped SearchJSON
	// should be ErrUnknownTool
	require.Len(t, result.Raw.Errors, 1)
	assert.ErrorIs(
		t, result.Raw.Errors[0],
		gent.ErrUnknownTool,
	)

	// The r.error seen by JS is the raw error
	// string, which console.log outputs as Text.
	assert.Equal(
		t,
		"unknown tool: nonexistent_tool",
		result.Text,
	)
}
