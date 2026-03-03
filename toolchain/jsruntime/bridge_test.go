package jsruntime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/rickchristie/gent"
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
			RegisterToolBridge(rt, callFn)

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
