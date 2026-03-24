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
			merged := &gent.ToolChainResult{}
			for _, a := range arr {
				if e, ok := errs[a.Tool]; ok {
					merged.Results = append(
						merged.Results,
						&gent.ToolCallResult{
							Name:  a.Tool,
							Error: e,
						},
					)
				} else if r, ok := results[a.Tool]; ok {
					merged.Results = append(
						merged.Results,
						r.Results...,
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
						Results: []*gent.ToolCallResult{
							{
								Name:   "lookup",
								Output: `{"name":"Alice"}`,
							},
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
						Results: []*gent.ToolCallResult{
							{Name: "a", Output: `"ra"`},
						},
					},
					"b": {
						Results: []*gent.ToolCallResult{
							{Name: "b", Output: `"rb"`},
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
			name: "tool.call missing tool field " +
				"returns error",
			input: struct {
				source  string
				results map[string]*gent.ToolChainResult
				errs    map[string]error
			}{
				source: `var r = tool.call({args: {}});` +
					`console.log(` +
					`r.error ? "has_error" ` +
					`: "no_error");`,
				results: map[string]*gent.ToolChainResult{},
				errs:    map[string]error{},
			},
			expected: struct {
				consoleLogJSON []string
				callCount      int
				errContain     string
			}{
				callCount:      0,
				consoleLogJSON: []string{"has_error"},
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
						Results: []*gent.ToolCallResult{
							{
								Name:   "noargs",
								Output: "ok",
							},
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
						Results: []*gent.ToolCallResult{
							{
								Name:   "get_id",
								Output: `"C001"`,
							},
						},
					},
					"get_name": {
						Results: []*gent.ToolCallResult{
							{
								Name:   "get_name",
								Output: `"Alice"`,
							},
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

	addressSch, err := schema.Compile(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{"type": "string"},
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
	})
	require.NoError(t, err)

	orderSch, err := schema.Compile(map[string]any{
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
	})
	require.NoError(t, err)

	stockSch, err := schema.Compile(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{"type": "string"},
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
	})
	require.NoError(t, err)

	geoSch, err := schema.Compile(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{"type": "string"},
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
	})
	require.NoError(t, err)

	shipmentSch, err := schema.Compile(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{"type": "string"},
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
	})
	require.NoError(t, err)

	regionsSch, err := schema.Compile(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{"type": "string"},
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
	})
	require.NoError(t, err)

	productsSch, err := schema.Compile(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{"type": "string"},
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
	})
	require.NoError(t, err)

	catalogSch, err := schema.Compile(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{"type": "string"},
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
					"required": []any{"products"},
				},
			},
		},
		"required": []any{"id", "categories"},
	})
	require.NoError(t, err)

	schemaFn := func(
		name string,
	) *schema.Schema {
		switch name {
		case "create_case":
			return caseSch
		case "lookup_customer":
			return lookupSch
		case "update_address":
			return addressSch
		case "create_order":
			return orderSch
		case "update_stock":
			return stockSch
		case "update_geo":
			return geoSch
		case "create_shipment":
			return shipmentSch
		case "update_regions":
			return regionsSch
		case "update_products":
			return productsSch
		case "update_catalog":
			return catalogSch
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

	valErrAddress := addressSch.Validate(map[string]any{
		"id": "1",
		"address": map[string]any{
			"street": "123 Main St",
		},
	})
	require.Error(t, valErrAddress)

	valErrOrder := orderSch.Validate(map[string]any{
		"customer_id": "C1",
		"items": []any{
			map[string]any{"name": "Widget"},
		},
	})
	require.Error(t, valErrOrder)

	valErrStock := stockSch.Validate(map[string]any{
		"id": "S1",
		"quantities": map[string]any{
			"apples": map[string]any{
				"amount": 5,
			},
		},
	})
	require.Error(t, valErrStock)

	valErrGeo := geoSch.Validate(map[string]any{
		"id": "1",
		"address": map[string]any{
			"street": "Main St",
			"geo":    map[string]any{"lat": 1.0},
		},
	})
	require.Error(t, valErrGeo)

	valErrShipment := shipmentSch.Validate(map[string]any{
		"id": "S1",
		"orders": []any{
			map[string]any{
				"order_id": "O1",
				"items": []any{
					map[string]any{"name": "Widget"},
				},
			},
		},
	})
	require.Error(t, valErrShipment)

	valErrRegions := regionsSch.Validate(map[string]any{
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
	})
	require.Error(t, valErrRegions)

	valErrProducts := productsSch.Validate(
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
	require.Error(t, valErrProducts)

	valErrCatalog := catalogSch.Validate(map[string]any{
		"id": "C1",
		"categories": map[string]any{
			"electronics": map[string]any{
				"products": []any{
					map[string]any{"name": "Phone"},
				},
			},
		},
	})
	require.Error(t, valErrCatalog)

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
						Results: []*gent.ToolCallResult{
							{
								Name:  "create_case",
								Error: valErr,
							},
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
  - 'args.details' (required, string): Description of the issue
  - 'args.order_id' (required, string): The order ID
Example:
  tool.call({
    tool: "create_case",
    args: {
      "details": "...",
      "order_id": "..."
    }
  });
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
						Results: []*gent.ToolCallResult{
							{
								Name:  "create_case",
								Error: valErrNilArgs,
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
  - 'args.details' (required, string): Description of the issue
  - 'args.order_id' (required, string): The order ID
Example:
  tool.call({
    tool: "create_case",
    args: {
      "details": "...",
      "order_id": "..."
    }
  });
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
						Results: []*gent.ToolCallResult{
							{
								Name:  "create_case",
								Error: valErr,
							},
						},
					},
					"lookup_customer": {
						Results: []*gent.ToolCallResult{
							{
								Name:  "lookup_customer",
								Error: valErrLookup,
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
  - 'args.details' (required, string): Description of the issue
  - 'args.order_id' (required, string): The order ID
Example:
  tool.call({
    tool: "create_case",
    args: {
      "details": "...",
      "order_id": "..."
    }
  });

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
  - 'args.id' (required, string): Customer ID
Example:
  tool.call({
    tool: "lookup_customer",
    args: {
      "id": "..."
    }
  });
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
						Results: []*gent.ToolCallResult{
							{
								Name:   "create_case",
								Output: `{"id":"C001"}`,
							},
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
						Results: []*gent.ToolCallResult{
							{
								Name:  "unknown_tool",
								Error: valErr,
							},
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
						Results: []*gent.ToolCallResult{
							{
								Name: "fail",
								Error: errors.New(
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
						Results: []*gent.ToolCallResult{
							{
								Name:  "create_case",
								Error: valErr,
							},
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
			name: "nested object missing sub-field",
			input: input{
				source: `var r = tool.call({
  tool: "update_address",
  args: {
    id: "1",
    address: { street: "123 Main St" }
  }
});
console.log(r.error);`,
				schemaFn: schemaFn,
				results: map[string]*gent.ToolChainResult{
					"update_address": {
						Results: []*gent.ToolCallResult{
							{
								Name:  "update_address",
								Error: valErrAddress,
							},
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
2 |   tool: "update_address",
3 |   args: {

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
			},
		},
		{
			name: "array of objects missing " +
				"item field",
			input: input{
				source: `var r = tool.call({
  tool: "create_order",
  args: {
    customer_id: "C1",
    items: [{ name: "Widget" }]
  }
});
console.log(r.error);`,
				schemaFn: schemaFn,
				results: map[string]*gent.ToolChainResult{
					"create_order": {
						Results: []*gent.ToolCallResult{
							{
								Name:  "create_order",
								Error: valErrOrder,
							},
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
2 |   tool: "create_order",
3 |   args: {

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
			},
		},
		{
			name: "map of objects missing " +
				"value field",
			input: input{
				source: `var r = tool.call({
  tool: "update_stock",
  args: {
    id: "S1",
    quantities: {
      apples: { amount: 5 }
    }
  }
});
console.log(r.error);`,
				schemaFn: schemaFn,
				results: map[string]*gent.ToolChainResult{
					"update_stock": {
						Results: []*gent.ToolCallResult{
							{
								Name:  "update_stock",
								Error: valErrStock,
							},
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
2 |   tool: "update_stock",
3 |   args: {

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
			},
		},
		{
			name: "object contains object " +
				"missing deep field",
			input: input{
				source: `var r = tool.call({
  tool: "update_geo",
  args: {
    id: "1",
    address: {
      street: "Main St",
      geo: { lat: 1.0 }
    }
  }
});
console.log(r.error);`,
				schemaFn: schemaFn,
				results: map[string]*gent.ToolChainResult{
					"update_geo": {
						Results: []*gent.ToolCallResult{
							{
								Name:  "update_geo",
								Error: valErrGeo,
							},
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
2 |   tool: "update_geo",
3 |   args: {

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
			},
		},
		{
			name: "array of object contains " +
				"array of object",
			input: input{
				source: `var r = tool.call({
  tool: "create_shipment",
  args: {
    id: "S1",
    orders: [{
      order_id: "O1",
      items: [{ name: "Widget" }]
    }]
  }
});
console.log(r.error);`,
				schemaFn: schemaFn,
				results: map[string]*gent.ToolChainResult{
					"create_shipment": {
						Results: []*gent.ToolCallResult{
							{
								Name:  "create_shipment",
								Error: valErrShipment,
							},
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
2 |   tool: "create_shipment",
3 |   args: {

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
			},
		},
		{
			name: "map of object contains " +
				"map of object",
			input: input{
				source: `var r = tool.call({
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
});
console.log(r.error);`,
				schemaFn: schemaFn,
				results: map[string]*gent.ToolChainResult{
					"update_regions": {
						Results: []*gent.ToolCallResult{
							{
								Name:  "update_regions",
								Error: valErrRegions,
							},
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
2 |   tool: "update_regions",
3 |   args: {

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
			},
		},
		{
			name: "array of object contains " +
				"map of object",
			input: input{
				source: `var r = tool.call({
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
});
console.log(r.error);`,
				schemaFn: schemaFn,
				results: map[string]*gent.ToolChainResult{
					"update_products": {
						Results: []*gent.ToolCallResult{
							{
								Name:  "update_products",
								Error: valErrProducts,
							},
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
2 |   tool: "update_products",
3 |   args: {

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
			},
		},
		{
			name: "map of object contains " +
				"array of object",
			input: input{
				source: `var r = tool.call({
  tool: "update_catalog",
  args: {
    id: "C1",
    categories: {
      electronics: {
        products: [{ name: "Phone" }]
      }
    }
  }
});
console.log(r.error);`,
				schemaFn: schemaFn,
				results: map[string]*gent.ToolChainResult{
					"update_catalog": {
						Results: []*gent.ToolCallResult{
							{
								Name:  "update_catalog",
								Error: valErrCatalog,
							},
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
2 |   tool: "update_catalog",
3 |   args: {

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

func TestNormalizeOutput(t *testing.T) {
	type taggedStruct struct {
		CaseID  string `json:"case_id"`
		OrderID string `json:"order_id"`
		Status  string `json:"status"`
	}

	type nestedStruct struct {
		ID      string        `json:"id"`
		Details *taggedStruct `json:"details"`
	}

	type input struct {
		output any
	}

	type expected struct {
		output any
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "string JSON parsed to map",
			input: input{
				output: `{"name":"Alice","age":30}`,
			},
			expected: expected{
				output: map[string]any{
					"name": "Alice",
					"age":  float64(30),
				},
			},
		},
		{
			name: "string JSON array parsed",
			input: input{
				output: `[1, 2, 3]`,
			},
			expected: expected{
				output: []any{
					float64(1),
					float64(2),
					float64(3),
				},
			},
		},
		{
			name: "plain string returned as-is",
			input: input{
				output: "not json",
			},
			expected: expected{
				output: "not json",
			},
		},
		{
			name: "nil returns nil",
			input: input{
				output: nil,
			},
			expected: expected{
				output: nil,
			},
		},
		{
			name: "struct with json tags " +
				"normalized to snake_case",
			input: input{
				output: &taggedStruct{
					CaseID:  "CASE-1",
					OrderID: "ORD-1",
					Status:  "open",
				},
			},
			expected: expected{
				output: map[string]any{
					"case_id":  "CASE-1",
					"order_id": "ORD-1",
					"status":   "open",
				},
			},
		},
		{
			name: "nested struct with json tags",
			input: input{
				output: &nestedStruct{
					ID: "N1",
					Details: &taggedStruct{
						CaseID:  "CASE-2",
						OrderID: "ORD-2",
						Status:  "closed",
					},
				},
			},
			expected: expected{
				output: map[string]any{
					"id": "N1",
					"details": map[string]any{
						"case_id":  "CASE-2",
						"order_id": "ORD-2",
						"status":   "closed",
					},
				},
			},
		},
		{
			name: "struct value (not pointer)" +
				" normalized",
			input: input{
				output: taggedStruct{
					CaseID:  "CASE-3",
					OrderID: "ORD-3",
					Status:  "open",
				},
			},
			expected: expected{
				output: map[string]any{
					"case_id":  "CASE-3",
					"order_id": "ORD-3",
					"status":   "open",
				},
			},
		},
		{
			name: "map passes through unchanged",
			input: input{
				output: map[string]any{
					"already": "a map",
				},
			},
			expected: expected{
				output: map[string]any{
					"already": "a map",
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := normalizeOutput(tc.input.output)
			assert.Equal(
				t, tc.expected.output, result,
			)
		})
	}
}

// TestToolBridge_StructOutput verifies that tool outputs
// with Go structs are normalized via json tags so the JS
// code sees snake_case field names matching the schema.
func TestToolBridge_StructOutput(t *testing.T) {
	type caseResult struct {
		CaseID  string `json:"case_id"`
		OrderID string `json:"order_id"`
		Status  string `json:"status"`
	}

	var calls []string
	callFn := mockToolCallFn(
		map[string]*gent.ToolChainResult{
			"create_case": {
				Results: []*gent.ToolCallResult{
					{
						Name: "create_case",
						Output: &caseResult{
							CaseID:  "CASE-1",
							OrderID: "ORD-1",
							Status:  "open",
						},
					},
				},
			},
		},
		map[string]error{},
		&calls,
	)

	rt := New(DefaultConfig())
	RegisterToolBridge(rt, callFn, "", nil)

	// Access output fields using snake_case names
	// (from json tags, not Go PascalCase)
	result, err := rt.Execute(
		context.Background(),
		`var r = tool.call(
  {tool: "create_case", args: {}}
);
console.log(r.output.case_id);
console.log(r.output.order_id);
console.log(r.output.status);`,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, len(calls))
	require.Len(t, result.ConsoleLog, 3)
	assert.Equal(t, "CASE-1", result.ConsoleLog[0])
	assert.Equal(t, "ORD-1", result.ConsoleLog[1])
	assert.Equal(t, "open", result.ConsoleLog[2])
}

func TestCollectedResults(t *testing.T) {
	c := NewCollectedResults()

	r1 := &gent.ToolChainResult{
		Results: []*gent.ToolCallResult{
			{Name: "t1", Output: "out1"},
		},
	}
	r2 := &gent.ToolChainResult{
		Results: []*gent.ToolCallResult{
			{Name: "t2", Error: errors.New("e")},
		},
	}

	c.Add(r1)
	c.Add(r2)
	c.Add(nil) // should not panic

	built := c.BuildResult()
	assert.Len(t, built.Results, 2)
	assert.Equal(t, "t1", built.Results[0].Name)
	assert.Equal(t, "t2", built.Results[1].Name)
	assert.Equal(t, "out1", built.Results[0].Output)
	assert.Error(t, built.Results[1].Error)
}
