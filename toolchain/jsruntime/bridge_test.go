package jsruntime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockToolCallFn creates a ToolCallFn that records calls
// and returns preset results keyed by tool name.
func mockToolCallFn(
	results map[string]*gent.ToolChainResult,
	errs map[string]error,
	calls *[]string,
) ToolCallFn {
	return func(content string) (
		*gent.ToolChainResult, error,
	) {
		*calls = append(*calls, content)

		// Parse to get tool name(s)
		var single struct {
			Tool string `json:"tool"`
		}
		var arr []struct {
			Tool string `json:"tool"`
		}

		if err := json.Unmarshal(
			[]byte(content), &single,
		); err == nil && single.Tool != "" {
			if e, ok := errs[single.Tool]; ok {
				return nil, e
			}
			return results[single.Tool], nil
		}

		if err := json.Unmarshal(
			[]byte(content), &arr,
		); err == nil && len(arr) > 0 {
			// For parallel calls, merge results
			merged := &gent.ToolChainResult{
				Raw: &gent.RawToolChainResult{},
			}
			for _, a := range arr {
				if e, ok := errs[a.Tool]; ok {
					merged.Raw.Calls = append(
						merged.Raw.Calls,
						&gent.ToolCall{Name: a.Tool},
					)
					merged.Raw.Results = append(
						merged.Raw.Results, nil,
					)
					merged.Raw.Errors = append(
						merged.Raw.Errors, e,
					)
				} else if r, ok := results[a.Tool]; ok &&
					r.Raw != nil {
					merged.Raw.Calls = append(
						merged.Raw.Calls,
						r.Raw.Calls...,
					)
					merged.Raw.Results = append(
						merged.Raw.Results,
						r.Raw.Results...,
					)
					merged.Raw.Errors = append(
						merged.Raw.Errors,
						r.Raw.Errors...,
					)
				}
			}
			return merged, nil
		}

		return nil, errors.New("unknown format")
	}
}

// assertJSONEqual compares two strings as JSON if both
// are valid JSON, otherwise compares as plain strings.
func assertJSONEqual(
	t *testing.T, expected, actual string,
) {
	t.Helper()
	var exp, act any
	expErr := json.Unmarshal([]byte(expected), &exp)
	actErr := json.Unmarshal([]byte(actual), &act)
	if expErr == nil && actErr == nil {
		assert.Equal(t, exp, act)
		return
	}
	// Fall back to string comparison
	assert.Equal(t, expected, actual)
}

func TestToolBridge(t *testing.T) {
	tests := []struct {
		name  string
		input struct {
			source  string
			results map[string]*gent.ToolChainResult
			errs    map[string]error
		}
		expected struct {
			// consoleLogJSON entries are compared as
			// JSON (order-independent).
			consoleLogJSON []string
			callCount      int
			errContain     string
		}
	}{
		{
			name: "single tool.call with valid args",
			input: struct {
				source  string
				results map[string]*gent.ToolChainResult
				errs    map[string]error
			}{
				source: `var r = tool.call(` +
					`{tool: "lookup", args: ` +
					`{id: "C001"}});` +
					`console.log(JSON.stringify(r));`,
				results: map[string]*gent.ToolChainResult{
					"lookup": {
						Raw: &gent.RawToolChainResult{
							Calls: []*gent.ToolCall{
								{Name: "lookup"},
							},
							Results: []*gent.RawToolCallResult{
								{
									Name:   "lookup",
									Output: `{"name":"Alice"}`,
								},
							},
							Errors: []error{nil},
						},
					},
				},
				errs: map[string]error{},
			},
			expected: struct {
				consoleLogJSON []string
				callCount      int
				errContain     string
			}{
				callCount: 1,
				consoleLogJSON: []string{
					`{"name":"lookup","output":` +
						`{"name":"Alice"}}`,
				},
			},
		},
		{
			name: "tool.call when callFn returns error",
			input: struct {
				source  string
				results map[string]*gent.ToolChainResult
				errs    map[string]error
			}{
				source: `var r = tool.call(` +
					`{tool: "fail", args: {}});` +
					`console.log(JSON.stringify(r));`,
				results: map[string]*gent.ToolChainResult{},
				errs: map[string]error{
					"fail": errors.New("tool failed"),
				},
			},
			expected: struct {
				consoleLogJSON []string
				callCount      int
				errContain     string
			}{
				callCount: 1,
				consoleLogJSON: []string{
					`{"name":"fail",` +
						`"error":"tool failed"}`,
				},
			},
		},
		{
			name: "parallelCall with 2 tools",
			input: struct {
				source  string
				results map[string]*gent.ToolChainResult
				errs    map[string]error
			}{
				source: `var r = tool.parallelCall([` +
					`{tool: "a", args: {}},` +
					`{tool: "b", args: {}}]);` +
					`console.log(JSON.stringify(r));`,
				results: map[string]*gent.ToolChainResult{
					"a": {
						Raw: &gent.RawToolChainResult{
							Calls: []*gent.ToolCall{
								{Name: "a"},
							},
							Results: []*gent.RawToolCallResult{
								{Name: "a", Output: `"ra"`},
							},
							Errors: []error{nil},
						},
					},
					"b": {
						Raw: &gent.RawToolChainResult{
							Calls: []*gent.ToolCall{
								{Name: "b"},
							},
							Results: []*gent.RawToolCallResult{
								{Name: "b", Output: `"rb"`},
							},
							Errors: []error{nil},
						},
					},
				},
				errs: map[string]error{},
			},
			expected: struct {
				consoleLogJSON []string
				callCount      int
				errContain     string
			}{
				callCount: 1,
				consoleLogJSON: []string{
					`[{"name":"a","output":"ra"},` +
						`{"name":"b","output":"rb"}]`,
				},
			},
		},
		{
			name: "parallelCall with empty array",
			input: struct {
				source  string
				results map[string]*gent.ToolChainResult
				errs    map[string]error
			}{
				source: `var r = tool.parallelCall([]);` +
					`console.log(JSON.stringify(r));`,
				results: map[string]*gent.ToolChainResult{},
				errs:    map[string]error{},
			},
			expected: struct {
				consoleLogJSON []string
				callCount      int
				errContain     string
			}{
				callCount:      0,
				consoleLogJSON: []string{"[]"},
			},
		},
		{
			name: "tool.call with non-object throws",
			input: struct {
				source  string
				results map[string]*gent.ToolChainResult
				errs    map[string]error
			}{
				source: `tool.call("not an object");`,
				results: map[string]*gent.ToolChainResult{},
				errs:    map[string]error{},
			},
			expected: struct {
				consoleLogJSON []string
				callCount      int
				errContain     string
			}{
				errContain: "tool",
			},
		},
		{
			name: "tool.call missing tool field throws",
			input: struct {
				source  string
				results map[string]*gent.ToolChainResult
				errs    map[string]error
			}{
				source:  `tool.call({args: {}});`,
				results: map[string]*gent.ToolChainResult{},
				errs:    map[string]error{},
			},
			expected: struct {
				consoleLogJSON []string
				callCount      int
				errContain     string
			}{
				errContain: "'tool' field is required",
			},
		},
		{
			name: "tool.call with no args returns result",
			input: struct {
				source  string
				results map[string]*gent.ToolChainResult
				errs    map[string]error
			}{
				source: `tool.call({tool: "noargs"});`,
				results: map[string]*gent.ToolChainResult{
					"noargs": {
						Raw: &gent.RawToolChainResult{
							Calls: []*gent.ToolCall{
								{Name: "noargs"},
							},
							Results: []*gent.RawToolCallResult{
								{
									Name:   "noargs",
									Output: "ok",
								},
							},
							Errors: []error{nil},
						},
					},
				},
				errs: map[string]error{},
			},
			expected: struct {
				consoleLogJSON []string
				callCount      int
				errContain     string
			}{
				callCount: 1,
			},
		},
		{
			name: "chained calls use result of first",
			input: struct {
				source  string
				results map[string]*gent.ToolChainResult
				errs    map[string]error
			}{
				source: `var c = tool.call(` +
					`{tool: "get_id", args: {}});` +
					`var d = tool.call(` +
					`{tool: "get_name", ` +
					`args: {id: c.output}});` +
					`console.log(d.output);`,
				results: map[string]*gent.ToolChainResult{
					"get_id": {
						Raw: &gent.RawToolChainResult{
							Calls: []*gent.ToolCall{
								{Name: "get_id"},
							},
							Results: []*gent.RawToolCallResult{
								{
									Name:   "get_id",
									Output: `"C001"`,
								},
							},
							Errors: []error{nil},
						},
					},
					"get_name": {
						Raw: &gent.RawToolChainResult{
							Calls: []*gent.ToolCall{
								{Name: "get_name"},
							},
							Results: []*gent.RawToolCallResult{
								{
									Name:   "get_name",
									Output: `"Alice"`,
								},
							},
							Errors: []error{nil},
						},
					},
				},
				errs: map[string]error{},
			},
			expected: struct {
				consoleLogJSON []string
				callCount      int
				errContain     string
			}{
				callCount:      2,
				consoleLogJSON: []string{`Alice`},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			callFn := mockToolCallFn(
				tc.input.results,
				tc.input.errs,
				&calls,
			)

			rt := New(DefaultConfig())
			RegisterToolBridge(rt, callFn, "", nil)

			result, err := rt.Execute(
				context.Background(), tc.input.source,
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
			require.NotNil(t, result)
			assert.Equal(
				t, tc.expected.callCount, len(calls),
			)
			if tc.expected.consoleLogJSON != nil {
				require.Equal(
					t,
					len(tc.expected.consoleLogJSON),
					len(result.ConsoleLog),
					"console.log call count mismatch",
				)
				for i, exp := range tc.expected.consoleLogJSON {
					assertJSONEqual(
						t, exp, result.ConsoleLog[i],
					)
				}
			}
		})
	}
}

func TestToolBridge_SchemaErrors(t *testing.T) {
	// Build schemas for tools
	caseSch, err := schema.Compile(
		schema.Object(
			map[string]*schema.Property{
				"order_id": schema.String(
					"The order ID",
				),
				"details": schema.String(
					"Description of the issue",
				),
			}, "order_id", "details",
		),
	)
	require.NoError(t, err)

	lookupSch, err := schema.Compile(
		schema.Object(
			map[string]*schema.Property{
				"id": schema.String(
					"Customer ID",
				),
			}, "id",
		),
	)
	require.NoError(t, err)

	schemaFn := func(
		name string,
	) *schema.Schema {
		switch name {
		case "create_case":
			return caseSch
		case "lookup_customer":
			return lookupSch
		}
		return nil
	}

	// Validation errors for test cases
	valErr := caseSch.Validate(map[string]any{
		"order_id": "O1",
	})
	require.Error(t, valErr)

	valErrNilArgs := caseSch.Validate(nil)
	require.Error(t, valErrNilArgs)

	valErrLookup := lookupSch.Validate(
		map[string]any{},
	)
	require.Error(t, valErrLookup)

	type input struct {
		source   string
		schemaFn SchemaLookupFn
		results  map[string]*gent.ToolChainResult
		errs     map[string]error
	}

	type expected struct {
		log            string
		logNotContains []string
		callCount      int
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "schema error returns enhanced " +
				"format with field descriptions",
			input: input{
				source: `var r = tool.call({
  tool: "create_case",
  args: {order_id: "O1"}
});
console.log(r.error);`,
				schemaFn: schemaFn,
				results: map[string]*gent.ToolChainResult{
					"create_case": {
						Raw: &gent.RawToolChainResult{
							Calls: []*gent.ToolCall{
								{Name: "create_case"},
							},
							Results: []*gent.RawToolCallResult{
								nil,
							},
							Errors: []error{valErr},
						},
					},
				},
				errs: map[string]error{},
			},
			expected: expected{
				callCount: 1,
				log: `tool.call() error at line 1:

1 | var r = tool.call({
                     ^ schema validation error
2 |   tool: "create_case",
3 |   args: {order_id: "O1"}

Invalid args for tool 'create_case'.
Errors:
  - missing property 'details'
Expected fields:
  - 'details' (required, string): Description of the issue
  - 'order_id' (required, string): The order ID

IMPORTANT: Use EXACT argument names and types from the tool schema.
Fix ALL errors above before re-submitting your code.
`,
			},
		},
		{
			name: "missing args field entirely",
			input: input{
				source: `var r = tool.call({ tool: "create_case" });
console.log(r.error);`,
				schemaFn: schemaFn,
				results: map[string]*gent.ToolChainResult{
					"create_case": {
						Raw: &gent.RawToolChainResult{
							Calls: []*gent.ToolCall{
								{Name: "create_case"},
							},
							Results: []*gent.RawToolCallResult{
								nil,
							},
							Errors: []error{
								valErrNilArgs,
							},
						},
					},
				},
				errs: map[string]error{},
			},
			expected: expected{
				callCount: 1,
				log: `tool.call() error at line 1:

1 | var r = tool.call({ tool: "create_case" });
                     ^ schema validation error
2 | console.log(r.error);

Invalid args for tool 'create_case'.
Errors:
  - args is null or missing, expected object with required properties: order_id, details
Expected fields:
  - 'details' (required, string): Description of the issue
  - 'order_id' (required, string): The order ID

IMPORTANT: Use EXACT argument names and types from the tool schema.
Fix ALL errors above before re-submitting your code.
`,
			},
		},
		{
			name: "multiple calls with different " +
				"schema errors",
			input: input{
				source: `var r1 = tool.call({
  tool: "create_case",
  args: {order_id: "O1"}
});
console.log("err1: " + r1.error);
var r2 = tool.call({
  tool: "lookup_customer",
  args: {}
});
console.log("err2: " + r2.error);`,
				schemaFn: schemaFn,
				results: map[string]*gent.ToolChainResult{
					"create_case": {
						Raw: &gent.RawToolChainResult{
							Calls: []*gent.ToolCall{
								{Name: "create_case"},
							},
							Results: []*gent.RawToolCallResult{
								nil,
							},
							Errors: []error{
								valErr,
							},
						},
					},
					"lookup_customer": {
						Raw: &gent.RawToolChainResult{
							Calls: []*gent.ToolCall{
								{Name: "lookup_customer"},
							},
							Results: []*gent.RawToolCallResult{
								nil,
							},
							Errors: []error{
								valErrLookup,
							},
						},
					},
				},
				errs: map[string]error{},
			},
			expected: expected{
				callCount: 2,
				log: `err1: tool.call() error at line 1:

1 | var r1 = tool.call({
                      ^ schema validation error
2 |   tool: "create_case",
3 |   args: {order_id: "O1"}

Invalid args for tool 'create_case'.
Errors:
  - missing property 'details'
Expected fields:
  - 'details' (required, string): Description of the issue
  - 'order_id' (required, string): The order ID

IMPORTANT: Use EXACT argument names and types from the tool schema.
Fix ALL errors above before re-submitting your code.

err2: tool.call() error at line 6:

4 | });
5 | console.log("err1: " + r1.error);
6 | var r2 = tool.call({
                      ^ schema validation error
7 |   tool: "lookup_customer",
8 |   args: {}

Invalid args for tool 'lookup_customer'.
Errors:
  - missing property 'id'
Expected fields:
  - 'id' (required, string): Customer ID

IMPORTANT: Use EXACT argument names and types from the tool schema.
Fix ALL errors above before re-submitting your code.
`,
			},
		},
		{
			name: "valid args — no error",
			input: input{
				source: `var r = tool.call({
  tool: "create_case",
  args: {
    order_id: "O1",
    details: "broken"
  }
});
console.log(r.error || "no error");`,
				schemaFn: schemaFn,
				results: map[string]*gent.ToolChainResult{
					"create_case": {
						Raw: &gent.RawToolChainResult{
							Calls: []*gent.ToolCall{
								{Name: "create_case"},
							},
							Results: []*gent.RawToolCallResult{
								{
									Name:   "create_case",
									Output: `{"id":"C001"}`,
								},
							},
							Errors: []error{nil},
						},
					},
				},
				errs: map[string]error{},
			},
			expected: expected{
				callCount: 1,
				log:       "no error",
				logNotContains: []string{
					"Invalid args",
					"Expected fields:",
					"IMPORTANT:",
				},
			},
		},
		{
			name: "tool with no schema falls " +
				"back to raw error",
			input: input{
				// schemaFn returns nil for
				// "unknown_tool", so enhanceSchemaError
				// can't look up the schema and falls
				// back to raw valErr.Error().
				source: `var r = tool.call({
  tool: "unknown_tool", args: {}
});
console.log(r.error);`,
				schemaFn: schemaFn,
				results: map[string]*gent.ToolChainResult{
					"unknown_tool": {
						Raw: &gent.RawToolChainResult{
							Calls: []*gent.ToolCall{
								{Name: "unknown_tool"},
							},
							Results: []*gent.RawToolCallResult{
								nil,
							},
							Errors: []error{valErr},
						},
					},
				},
				errs: map[string]error{},
			},
			expected: expected{
				callCount: 1,
				log:       valErr.Error(),
				logNotContains: []string{
					"Expected fields:",
					"IMPORTANT:",
				},
			},
		},
		{
			name: "non-schema errors unchanged",
			input: input{
				source: `var r = tool.call({
  tool: "fail", args: {}
});
console.log(r.error);`,
				schemaFn: schemaFn,
				results: map[string]*gent.ToolChainResult{
					"fail": {
						Raw: &gent.RawToolChainResult{
							Calls: []*gent.ToolCall{
								{Name: "fail"},
							},
							Results: []*gent.RawToolCallResult{
								nil,
							},
							Errors: []error{
								errors.New(
									"tool failed",
								),
							},
						},
					},
				},
				errs: map[string]error{},
			},
			expected: expected{
				callCount: 1,
				log:       "tool failed",
				logNotContains: []string{
					"Expected fields:",
					"IMPORTANT:",
				},
			},
		},
		{
			name: "nil schemaFn falls back to " +
				"raw error",
			input: input{
				source:   "",
				schemaFn: nil,
				results: map[string]*gent.ToolChainResult{
					"create_case": {
						Raw: &gent.RawToolChainResult{
							Calls: []*gent.ToolCall{
								{Name: "create_case"},
							},
							Results: []*gent.RawToolCallResult{
								nil,
							},
							Errors: []error{valErr},
						},
					},
				},
				errs: map[string]error{},
			},
			expected: expected{
				callCount: 1,
				log:       valErr.Error(),
				logNotContains: []string{
					"Expected fields:",
					"IMPORTANT:",
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			callFn := mockToolCallFn(
				tc.input.results,
				tc.input.errs,
				&calls,
			)

			rt := New(DefaultConfig())
			RegisterToolBridge(
				rt, callFn,
				tc.input.source,
				tc.input.schemaFn,
			)

			// Use source as JS if present,
			// otherwise use a simple call
			jsCode := tc.input.source
			if jsCode == "" {
				jsCode = `var r = tool.call({
  tool: "create_case",
  args: {order_id: "O1"}
});
console.log(r.error || "no error");`
			}

			result, execErr := rt.Execute(
				context.Background(), jsCode,
			)

			require.NoError(t, execErr)
			require.NotNil(t, result)
			assert.Equal(
				t, tc.expected.callCount,
				len(calls),
			)

			require.NotEmpty(
				t, result.ConsoleLog,
				"expected console output",
			)
			log := strings.Join(
				result.ConsoleLog, "\n",
			)
			if tc.expected.log != "" {
				assert.Equal(
					t, tc.expected.log, log,
				)
			}
			for _, s := range tc.expected.logNotContains {
				assert.NotContains(t, log, s)
			}
		})
	}
}

func TestCollectedResults(t *testing.T) {
	c := NewCollectedResults()

	r1 := &gent.ToolChainResult{
		Text: "result1",
		Raw: &gent.RawToolChainResult{
			Calls:   []*gent.ToolCall{{Name: "t1"}},
			Results: []*gent.RawToolCallResult{{Name: "t1"}},
			Errors:  []error{nil},
		},
	}
	r2 := &gent.ToolChainResult{
		Text: "result2",
		Raw: &gent.RawToolChainResult{
			Calls:   []*gent.ToolCall{{Name: "t2"}},
			Results: []*gent.RawToolCallResult{{Name: "t2"}},
			Errors:  []error{errors.New("e")},
		},
	}

	c.Add(r1)
	c.Add(r2)
	c.Add(nil) // should not panic

	raw := c.BuildRaw()
	assert.Len(t, raw.Calls, 2)
	assert.Len(t, raw.Results, 2)
	assert.Len(t, raw.Errors, 2)
	assert.Equal(t, "t1", raw.Calls[0].Name)
	assert.Equal(t, "t2", raw.Calls[1].Name)
	assert.Equal(
		t,
		[]string{"result1", "result2"},
		c.TextParts,
	)
}
