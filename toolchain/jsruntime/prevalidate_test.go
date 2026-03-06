package jsruntime

import (
	"testing"

	"github.com/rickchristie/gent/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindToolCalls(t *testing.T) {
	type expected struct {
		sites []ToolCallSite
		isErr bool
	}

	tests := []struct {
		name     string
		input    string
		expected expected
	}{
		{
			name: "single tool.call with literal object",
			input: `var r = tool.call({
  tool: "search",
  args: { query: "hello", limit: 10 }
});`,
			expected: expected{
				sites: []ToolCallSite{
					{
						ToolName: "search",
						Args: map[string]any{
							"query": "hello",
							"limit": int64(10),
						},
						Line:      1,
						Column:    19,
						IsDynamic: false,
					},
				},
			},
		},
		{
			name: "tool.call with variable arg",
			input: `var opts = {tool: "x", args: {}};
var r = tool.call(opts);`,
			expected: expected{
				sites: []ToolCallSite{
					{
						IsDynamic: true,
						Line:      2,
						Column:    19,
					},
				},
			},
		},
		{
			name: "tool.call with mixed literal/" +
				"dynamic args",
			input: `var r = tool.call({
  tool: "create",
  args: { name: "test", id: someVar }
});`,
			expected: expected{
				sites: []ToolCallSite{
					{
						ToolName:  "create",
						IsDynamic: true,
						Line:      1,
						Column:    19,
					},
				},
			},
		},
		{
			name: "parallelCall with array of literals",
			input: `var r = tool.parallelCall([
  { tool: "a", args: { x: 1 } },
  { tool: "b", args: { y: "two" } }
]);`,
			expected: expected{
				sites: []ToolCallSite{
					{
						ToolName: "a",
						Args: map[string]any{
							"x": int64(1),
						},
						Line:   2,
						Column: 3,
					},
					{
						ToolName: "b",
						Args: map[string]any{
							"y": "two",
						},
						Line:   3,
						Column: 3,
					},
				},
			},
		},
		{
			name: "parallelCall with variable array",
			input: `var arr = [];
tool.parallelCall(arr);`,
			expected: expected{
				sites: []ToolCallSite{
					{
						IsDynamic: true,
						Line:      2,
						Column:    19,
					},
				},
			},
		},
		{
			name: "multiple tool.call in sequence",
			input: `var a = tool.call({
  tool: "first", args: { a: 1 }
});
var b = tool.call({
  tool: "second", args: { b: 2 }
});`,
			expected: expected{
				sites: []ToolCallSite{
					{
						ToolName: "first",
						Args: map[string]any{
							"a": int64(1),
						},
						Line:   1,
						Column: 19,
					},
					{
						ToolName: "second",
						Args: map[string]any{
							"b": int64(2),
						},
						Line:   4,
						Column: 19,
					},
				},
			},
		},
		{
			name: "tool.call inside if block",
			input: `if (true) {
  tool.call({ tool: "inner", args: {} });
}`,
			expected: expected{
				sites: []ToolCallSite{
					{
						ToolName: "inner",
						Args:     map[string]any{},
						Line:     2,
						Column:   13,
					},
				},
			},
		},
		{
			name: "no tool calls",
			input: `var x = 1 + 2;
console.log(x);`,
			expected: expected{
				sites: nil,
			},
		},
		{
			name:  "syntax error returns parse error",
			input: `var x = @;`,
			expected: expected{
				isErr: true,
			},
		},
		{
			name: "string literal with escaped quotes",
			input: `tool.call({
  tool: "search",
  args: { query: "hello \"world\"" }
});`,
			expected: expected{
				sites: []ToolCallSite{
					{
						ToolName: "search",
						Args: map[string]any{
							"query": `hello "world"`,
						},
						Line:   1,
						Column: 11,
					},
				},
			},
		},
		{
			name: "number literals int and float",
			input: `tool.call({
  tool: "calc",
  args: { count: 42, ratio: 3.14 }
});`,
			expected: expected{
				sites: []ToolCallSite{
					{
						ToolName: "calc",
						Args: map[string]any{
							"count": int64(42),
							"ratio": 3.14,
						},
						Line:   1,
						Column: 11,
					},
				},
			},
		},
		{
			name: "boolean and null literals",
			input: `tool.call({
  tool: "flags",
  args: {
    active: true,
    deleted: false,
    note: null
  }
});`,
			expected: expected{
				sites: []ToolCallSite{
					{
						ToolName: "flags",
						Args: map[string]any{
							"active":  true,
							"deleted": false,
							"note":    nil,
						},
						Line:   1,
						Column: 11,
					},
				},
			},
		},
		{
			name: "object literal in args extracted " +
				"recursively",
			input: `tool.call({
  tool: "create",
  args: {
    details: { name: "Alice", age: 30 }
  }
});`,
			expected: expected{
				sites: []ToolCallSite{
					{
						ToolName: "create",
						Args: map[string]any{
							"details": map[string]any{
								"name": "Alice",
								"age":  int64(30),
							},
						},
						Line:   1,
						Column: 11,
					},
				},
			},
		},
		{
			name: "array literal in args extracted",
			input: `tool.call({
  tool: "batch",
  args: { ids: ["a", "b", "c"] }
});`,
			expected: expected{
				sites: []ToolCallSite{
					{
						ToolName: "batch",
						Args: map[string]any{
							"ids": []any{
								"a", "b", "c",
							},
						},
						Line:   1,
						Column: 11,
					},
				},
			},
		},
		{
			name: "tool name from variable is dynamic",
			input: `var name = "search";
tool.call({ tool: name, args: {} });`,
			expected: expected{
				sites: []ToolCallSite{
					{
						IsDynamic: true,
						Line:      2,
						Column:    11,
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sites, err := FindToolCalls(tc.input)
			if tc.expected.isErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(
				t, tc.expected.sites, sites,
			)
		})
	}
}

func TestPreValidate(t *testing.T) {
	searchSchema := schema.MustCompile(
		schema.Object(
			map[string]*schema.Property{
				"query": schema.String(
					"Search query",
				),
				"limit": schema.Integer(
					"Max results",
				),
			},
			"query",
		),
	)

	getSchema := schema.MustCompile(
		schema.Object(
			map[string]*schema.Property{
				"id": schema.String("Item ID"),
			},
			"id",
		),
	)

	lookupFn := func(name string) *schema.Schema {
		switch name {
		case "search":
			return searchSchema
		case "get":
			return getSchema
		default:
			return nil
		}
	}

	type expectedError struct {
		toolName     string
		errorMessage string
	}

	type input struct {
		source   string
		schemaFn SchemaLookupFn
	}

	type expected struct {
		errors []expectedError
		isErr  bool
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "single call missing required field",
			input: input{
				source: `tool.call({
  tool: "search",
  args: { limit: 5 }
});`,
				schemaFn: lookupFn,
			},
			expected: expected{
				errors: []expectedError{
					{
						toolName: "search",
						errorMessage: `Invalid args for tool 'search'.
Errors:
  - missing property 'query'
Expected fields:
  - 'limit' (integer): Max results
  - 'query' (required, string): Search query
`,
					},
				},
			},
		},
		{
			name: "multiple calls with different " +
				"missing fields",
			input: input{
				source: `tool.call({
  tool: "search",
  args: { limit: 5 }
});
tool.call({
  tool: "get",
  args: {}
});`,
				schemaFn: lookupFn,
			},
			expected: expected{
				errors: []expectedError{
					{
						toolName: "search",
						errorMessage: `Invalid args for tool 'search'.
Errors:
  - missing property 'query'
Expected fields:
  - 'limit' (integer): Max results
  - 'query' (required, string): Search query
`,
					},
					{
						toolName: "get",
						errorMessage: `Invalid args for tool 'get'.
Errors:
  - missing property 'id'
Expected fields:
  - 'id' (required, string): Item ID
`,
					},
				},
			},
		},
		{
			name: "call with valid args — no error",
			input: input{
				source: `tool.call({
  tool: "search",
  args: { query: "hello", limit: 10 }
});`,
				schemaFn: lookupFn,
			},
			expected: expected{},
		},
		{
			name: "dynamic args object skipped",
			input: input{
				source: `var a = { query: someVar };
tool.call({ tool: "search", args: a });`,
				schemaFn: lookupFn,
			},
			expected: expected{},
		},
		{
			name: "variable as entire argument skipped",
			input: input{
				source: `var req = {
  tool: "search", args: {}
};
tool.call(req);`,
				schemaFn: lookupFn,
			},
			expected: expected{},
		},
		{
			name: "mixed static invalid and dynamic",
			input: input{
				source: `tool.call({
  tool: "search",
  args: { limit: 5 }
});
var opts = {
  tool: "get", args: { id: "1" }
};
tool.call(opts);`,
				schemaFn: lookupFn,
			},
			expected: expected{
				errors: []expectedError{
					{
						toolName: "search",
						errorMessage: `Invalid args for tool 'search'.
Errors:
  - missing property 'query'
Expected fields:
  - 'limit' (integer): Max results
  - 'query' (required, string): Search query
`,
					},
				},
			},
		},
		{
			name: "missing args field entirely",
			input: input{
				source:   `tool.call({ tool: "search" });`,
				schemaFn: lookupFn,
			},
			expected: expected{
				errors: []expectedError{
					{
						toolName: "search",
						errorMessage: `Invalid args for tool 'search'.
Errors:
  - args is null or missing, expected object with required properties: query
Expected fields:
  - 'limit' (integer): Max results
  - 'query' (required, string): Search query
`,
					},
				},
			},
		},
		{
			name: "nil schemaFn returns empty",
			input: input{
				source: `tool.call({
  tool: "search", args: {}
});`,
				schemaFn: nil,
			},
			expected: expected{},
		},
		{
			name: "tool with no schema is skipped",
			input: input{
				source: `tool.call({
  tool: "unknown",
  args: {}
});`,
				schemaFn: lookupFn,
			},
			expected: expected{},
		},
		{
			name: "empty source — no errors",
			input: input{
				source:   ``,
				schemaFn: lookupFn,
			},
			expected: expected{},
		},
		{
			name: "all calls valid — empty errors",
			input: input{
				source: `tool.call({
  tool: "search",
  args: { query: "hello" }
});
tool.call({
  tool: "get",
  args: { id: "123" }
});`,
				schemaFn: lookupFn,
			},
			expected: expected{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			errs, err := PreValidate(
				tc.input.source,
				tc.input.schemaFn,
			)
			if tc.expected.isErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Len(
				t, errs, len(tc.expected.errors),
			)

			for i, exp := range tc.expected.errors {
				actual := errs[i]
				assert.Equal(
					t, exp.toolName,
					actual.Site.ToolName,
					"error %d: tool name", i,
				)
				assert.Equal(
					t, exp.errorMessage,
					actual.ErrorMessage,
					"error %d: message", i,
				)
			}
		})
	}
}

func TestFormatPreValidationErrors(t *testing.T) {
	type input struct {
		source string
		errors []PreValidationError
	}

	tests := []struct {
		name     string
		input    input
		expected string
	}{
		{
			name: "single error with realistic " +
				"FormatForLLM message",
			input: input{
				source: `var id = "C001";
tool.call({ tool: "search", args: { limit: 5 } });
console.log("done");`,
				errors: []PreValidationError{
					{
						Site: ToolCallSite{
							ToolName: "search",
							Line:     2,
							Column:   1,
						},
						ErrorMessage: `Invalid args for tool 'search'.
Errors:
  - missing property 'query'
Expected fields:
  - 'limit' (integer): Max results
  - 'query' (required, string): Search query
`,
					},
				},
			},
			expected: `1 schema pre-validation error(s):

--- Error 1: tool.call() at line 2 ---

2 | tool.call({ tool: "search", args: { limit: 5 } });
    ^
3 | console.log("done");

Invalid args for tool 'search'.
Errors:
  - missing property 'query'
Expected fields:
  - 'limit' (integer): Max results
  - 'query' (required, string): Search query

IMPORTANT: Use EXACT argument names and types from the tool schema.
Fix ALL errors above before re-submitting your code.
`,
		},
		{
			name: "multiple errors each with " +
				"realistic messages",
			input: input{
				source: `var id = "C001";
tool.call({ tool: "search", args: { limit: 5 } });
var x = 1;
var y = 2;
tool.call({ tool: "get", args: {} });
console.log("done");`,
				errors: []PreValidationError{
					{
						Site: ToolCallSite{
							ToolName: "search",
							Line:     2,
							Column:   1,
						},
						ErrorMessage: `Invalid args for tool 'search'.
Errors:
  - missing property 'query'
Expected fields:
  - 'limit' (integer): Max results
  - 'query' (required, string): Search query
`,
					},
					{
						Site: ToolCallSite{
							ToolName: "get",
							Line:     5,
							Column:   1,
						},
						ErrorMessage: `Invalid args for tool 'get'.
Errors:
  - missing property 'id'
Expected fields:
  - 'id' (required, string): Item ID
`,
					},
				},
			},
			expected: `2 schema pre-validation error(s):

--- Error 1: tool.call() at line 2 ---

2 | tool.call({ tool: "search", args: { limit: 5 } });
    ^
3 | var x = 1;
4 | var y = 2;

Invalid args for tool 'search'.
Errors:
  - missing property 'query'
Expected fields:
  - 'limit' (integer): Max results
  - 'query' (required, string): Search query

--- Error 2: tool.call() at line 5 ---

5 | tool.call({ tool: "get", args: {} });
    ^
6 | console.log("done");

Invalid args for tool 'get'.
Errors:
  - missing property 'id'
Expected fields:
  - 'id' (required, string): Item ID

IMPORTANT: Use EXACT argument names and types from the tool schema.
Fix ALL errors above before re-submitting your code.
`,
		},
		{
			name: "error at first line of source",
			input: input{
				source: `tool.call({ tool: "search", args: {} });`,
				errors: []PreValidationError{
					{
						Site: ToolCallSite{
							ToolName: "search",
							Line:     1,
							Column:   1,
						},
						ErrorMessage: `Invalid args for tool 'search'.
Errors:
  - missing property 'query'
Expected fields:
  - 'limit' (integer): Max results
  - 'query' (required, string): Search query
`,
					},
				},
			},
			expected: `1 schema pre-validation error(s):

--- Error 1: tool.call() at line 1 ---

1 | tool.call({ tool: "search", args: {} });
    ^

Invalid args for tool 'search'.
Errors:
  - missing property 'query'
Expected fields:
  - 'limit' (integer): Max results
  - 'query' (required, string): Search query

IMPORTANT: Use EXACT argument names and types from the tool schema.
Fix ALL errors above before re-submitting your code.
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := FormatPreValidationErrors(
				tc.input.source,
				tc.input.errors,
			)
			assert.Equal(t, tc.expected, result)
		})
	}
}
