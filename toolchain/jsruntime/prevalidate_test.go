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

	addressSchema := schema.MustCompile(map[string]any{
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
				"required": []any{"street", "city"},
			},
		},
		"required": []any{"id", "address"},
	})

	orderSchema := schema.MustCompile(map[string]any{
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
					"required": []any{"name", "qty"},
				},
			},
		},
		"required": []any{"customer_id", "items"},
	})

	stockSchema := schema.MustCompile(map[string]any{
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
					"required": []any{"amount", "unit"},
				},
			},
		},
		"required": []any{"id", "quantities"},
	})

	geoSchema := schema.MustCompile(map[string]any{
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
				"required": []any{"street", "geo"},
			},
		},
		"required": []any{"id", "address"},
	})

	shipmentSchema := schema.MustCompile(map[string]any{
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

	regionsSchema := schema.MustCompile(map[string]any{
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

	productsSchema := schema.MustCompile(map[string]any{
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

	catalogSchema := schema.MustCompile(map[string]any{
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

	lookupFn := func(name string) *schema.Schema {
		switch name {
		case "search":
			return searchSchema
		case "get":
			return getSchema
		case "update_address":
			return addressSchema
		case "create_order":
			return orderSchema
		case "update_stock":
			return stockSchema
		case "update_geo":
			return geoSchema
		case "create_shipment":
			return shipmentSchema
		case "update_regions":
			return regionsSchema
		case "update_products":
			return productsSchema
		case "update_catalog":
			return catalogSchema
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
  - 'args.limit' (integer): Max results
  - 'args.query' (required, string): Search query
Example:
  tool.call({
    tool: "search",
    args: {
      "limit": 0,
      "query": "..."
    }
  });
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
  - 'args.limit' (integer): Max results
  - 'args.query' (required, string): Search query
Example:
  tool.call({
    tool: "search",
    args: {
      "limit": 0,
      "query": "..."
    }
  });
`,
					},
					{
						toolName: "get",
						errorMessage: `Invalid args for tool 'get'.
Errors:
  - missing property 'id'
Expected fields:
  - 'args.id' (required, string): Item ID
Example:
  tool.call({
    tool: "get",
    args: {
      "id": "..."
    }
  });
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
  - 'args.limit' (integer): Max results
  - 'args.query' (required, string): Search query
Example:
  tool.call({
    tool: "search",
    args: {
      "limit": 0,
      "query": "..."
    }
  });
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
  - 'args.limit' (integer): Max results
  - 'args.query' (required, string): Search query
Example:
  tool.call({
    tool: "search",
    args: {
      "limit": 0,
      "query": "..."
    }
  });
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
		{
			name: "nested object missing sub-field",
			input: input{
				source: `tool.call({
  tool: "update_address",
  args: {
    id: "1",
    address: { street: "123 Main St" }
  }
});`,
				schemaFn: lookupFn,
			},
			expected: expected{
				errors: []expectedError{
					{
						toolName: "update_address",
						errorMessage: `Invalid args for tool 'update_address'.
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
			},
		},
		{
			name: "array of objects missing item field",
			input: input{
				source: `tool.call({
  tool: "create_order",
  args: {
    customer_id: "C1",
    items: [{ name: "Widget" }]
  }
});`,
				schemaFn: lookupFn,
			},
			expected: expected{
				errors: []expectedError{
					{
						toolName: "create_order",
						errorMessage: `Invalid args for tool 'create_order'.
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
			},
		},
		{
			name: "map of objects missing value field",
			input: input{
				source: `tool.call({
  tool: "update_stock",
  args: {
    id: "S1",
    quantities: { apples: { amount: 5 } }
  }
});`,
				schemaFn: lookupFn,
			},
			expected: expected{
				errors: []expectedError{
					{
						toolName: "update_stock",
						errorMessage: `Invalid args for tool 'update_stock'.
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
			},
		},
		{
			name: "object contains object missing " +
				"deep field",
			input: input{
				source: `tool.call({
  tool: "update_geo",
  args: {
    id: "1",
    address: {
      street: "Main St",
      geo: { lat: 1.0 }
    }
  }
});`,
				schemaFn: lookupFn,
			},
			expected: expected{
				errors: []expectedError{
					{
						toolName: "update_geo",
						errorMessage: `Invalid args for tool 'update_geo'.
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
			},
		},
		{
			name: "array of object contains array " +
				"of object",
			input: input{
				source: `tool.call({
  tool: "create_shipment",
  args: {
    id: "S1",
    orders: [
      {
        order_id: "O1",
        items: [{ name: "Widget" }]
      }
    ]
  }
});`,
				schemaFn: lookupFn,
			},
			expected: expected{
				errors: []expectedError{
					{
						toolName: "create_shipment",
						errorMessage: `Invalid args for tool 'create_shipment'.
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
			},
		},
		{
			name: "map of object contains map " +
				"of object",
			input: input{
				source: `tool.call({
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
});`,
				schemaFn: lookupFn,
			},
			expected: expected{
				errors: []expectedError{
					{
						toolName: "update_regions",
						errorMessage: `Invalid args for tool 'update_regions'.
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
			},
		},
		{
			name: "array of object contains map " +
				"of object",
			input: input{
				source: `tool.call({
  tool: "update_products",
  args: {
    id: "P1",
    items: [
      {
        name: "Widget",
        attributes: {
          weight: { value: "5" }
        }
      }
    ]
  }
});`,
				schemaFn: lookupFn,
			},
			expected: expected{
				errors: []expectedError{
					{
						toolName: "update_products",
						errorMessage: `Invalid args for tool 'update_products'.
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
			},
		},
		{
			name: "map of object contains array " +
				"of object",
			input: input{
				source: `tool.call({
  tool: "update_catalog",
  args: {
    id: "C1",
    categories: {
      electronics: {
        products: [{ name: "Phone" }]
      }
    }
  }
});`,
				schemaFn: lookupFn,
			},
			expected: expected{
				errors: []expectedError{
					{
						toolName: "update_catalog",
						errorMessage: `Invalid args for tool 'update_catalog'.
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
			},
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
  - 'args.limit' (integer): Max results
  - 'args.query' (required, string): Search query
Example:
  tool.call({
    tool: "search",
    args: {
      "limit": 0,
      "query": "..."
    }
  });
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
  - 'args.limit' (integer): Max results
  - 'args.query' (required, string): Search query
Example:
  tool.call({
    tool: "search",
    args: {
      "limit": 0,
      "query": "..."
    }
  });

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
  - 'args.limit' (integer): Max results
  - 'args.query' (required, string): Search query
Example:
  tool.call({
    tool: "search",
    args: {
      "limit": 0,
      "query": "..."
    }
  });
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
  - 'args.id' (required, string): Item ID
Example:
  tool.call({
    tool: "get",
    args: {
      "id": "..."
    }
  });
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
  - 'args.limit' (integer): Max results
  - 'args.query' (required, string): Search query
Example:
  tool.call({
    tool: "search",
    args: {
      "limit": 0,
      "query": "..."
    }
  });

--- Error 2: tool.call() at line 5 ---

5 | tool.call({ tool: "get", args: {} });
    ^
6 | console.log("done");

Invalid args for tool 'get'.
Errors:
  - missing property 'id'
Expected fields:
  - 'args.id' (required, string): Item ID
Example:
  tool.call({
    tool: "get",
    args: {
      "id": "..."
    }
  });

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
  - 'args.limit' (integer): Max results
  - 'args.query' (required, string): Search query
Example:
  tool.call({
    tool: "search",
    args: {
      "limit": 0,
      "query": "..."
    }
  });
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
  - 'args.limit' (integer): Max results
  - 'args.query' (required, string): Search query
Example:
  tool.call({
    tool: "search",
    args: {
      "limit": 0,
      "query": "..."
    }
  });

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
