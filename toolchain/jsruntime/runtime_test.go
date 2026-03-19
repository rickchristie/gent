package jsruntime

import (
	"context"
	"testing"
	"time"

	"github.com/grafana/sobek"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRuntimeExecute(t *testing.T) {
	type input struct {
		source  string
		setup   func(r *Runtime)
		config  Config
		ctxFunc func() (context.Context, context.CancelFunc)
	}

	type expected struct {
		consoleLog []string
		errMsg     string // empty = no error
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "simple expression no error",
			input: input{
				source: "var x = 1 + 2;",
				config: DefaultConfig(),
			},
			expected: expected{
				consoleLog: []string{},
			},
		},
		{
			name: "console.log single call",
			input: input{
				source: `console.log("hello");`,
				config: DefaultConfig(),
			},
			expected: expected{
				consoleLog: []string{"hello"},
			},
		},
		{
			name: "console.log multiple calls",
			input: input{
				source: `console.log("a"); console.log("b");`,
				config: DefaultConfig(),
			},
			expected: expected{
				consoleLog: []string{"a", "b"},
			},
		},
		{
			name: "console.log with multiple args",
			input: input{
				source: `console.log("x", 42, true);`,
				config: DefaultConfig(),
			},
			expected: expected{
				consoleLog: []string{"x 42 true"},
			},
		},
		{
			name: "registered Go function called from JS",
			input: input{
				source: `var r = add(3, 4); console.log(r);`,
				config: DefaultConfig(),
				setup: func(r *Runtime) {
					r.RegisterFunc(
						"add",
						func(
							call sobek.FunctionCall,
						) sobek.Value {
							a := call.Arguments[0].
								ToInteger()
							b := call.Arguments[1].
								ToInteger()
							return r.VM().ToValue(a + b)
						},
					)
				},
			},
			expected: expected{
				consoleLog: []string{"7"},
			},
		},
		{
			name: "RegisterObject with methods",
			input: input{
				source: `var r = math.double(5); console.log(r);`,
				config: DefaultConfig(),
				setup: func(r *Runtime) {
					r.RegisterObject(
						"math",
						map[string]func(
							sobek.FunctionCall,
						) sobek.Value{
							"double": func(
								call sobek.FunctionCall,
							) sobek.Value {
								v := call.Arguments[0].
									ToInteger()
								return r.VM().ToValue(
									v * 2,
								)
							},
						},
					)
				},
			},
			expected: expected{
				consoleLog: []string{"10"},
			},
		},
		{
			name: "timeout on infinite loop",
			input: input{
				source: "while(true) {}",
				config: Config{
					Timeout:      50 * time.Millisecond,
					MaxCallStack: 1024,
				},
			},
			expected: expected{
				errMsg: `execution interrupted: timeout

1 | while(true) {}
    ^ timeout
`,
			},
		},
		{
			name: "context cancellation interrupts",
			input: input{
				source: "while(true) {}",
				config: Config{
					Timeout:      5 * time.Second,
					MaxCallStack: 1024,
				},
				ctxFunc: func() (
					context.Context,
					context.CancelFunc,
				) {
					ctx, cancel := context.WithCancel(
						context.Background(),
					)
					go func() {
						time.Sleep(
							50 * time.Millisecond,
						)
						cancel()
					}()
					return ctx, cancel
				},
			},
			expected: expected{
				errMsg: `execution interrupted: cancelled

1 | while(true) {}
    ^ cancelled
`,
			},
		},
		{
			name: "syntax error returns formatted error",
			input: input{
				source: "var x = @;",
				config: DefaultConfig(),
			},
			expected: expected{
				errMsg: `SyntaxError: SyntaxError: (anonymous): Line 1:9 Unexpected token ILLEGAL (and 2 more errors)
`,
			},
		},
		{
			name: "runtime ReferenceError formatted",
			input: input{
				source: "undefinedVar;",
				config: DefaultConfig(),
			},
			expected: expected{
				errMsg: `ReferenceError: undefinedVar is not defined

1 | undefinedVar;
    ^ ReferenceError: undefinedVar is not defined
`,
			},
		},
		{
			name: "stack overflow returns error",
			input: input{
				source: "(function f() { f(); })();",
				config: Config{
					Timeout:      5 * time.Second,
					MaxCallStack: 64,
				},
			},
			expected: expected{
				errMsg: " at f (<eval>:1:18(7))",
			},
		},
		{
			name: "empty source string",
			input: input{
				source: "",
				config: DefaultConfig(),
			},
			expected: expected{
				consoleLog: []string{},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := New(tc.input.config)

			if tc.input.setup != nil {
				tc.input.setup(r)
			}

			var ctx context.Context
			if tc.input.ctxFunc != nil {
				var cancel context.CancelFunc
				ctx, cancel = tc.input.ctxFunc()
				defer cancel()
			} else {
				ctx = context.Background()
			}

			result, err := r.Execute(
				ctx, tc.input.source,
			)

			if tc.expected.errMsg != "" {
				require.Error(t, err)
				assert.Equal(
					t, tc.expected.errMsg,
					err.Error(),
				)
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				assert.Equal(
					t, tc.expected.consoleLog,
					result.ConsoleLog,
				)
			}
		})
	}
}

func TestConsoleLog(t *testing.T) {
	type input struct {
		source string
	}

	type expected struct {
		consoleLog []string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		// Primitives
		{
			name:  "string",
			input: input{source: `console.log("hello");`},
			expected: expected{
				consoleLog: []string{"hello"},
			},
		},
		{
			name:  "integer",
			input: input{source: `console.log(42);`},
			expected: expected{
				consoleLog: []string{"42"},
			},
		},
		{
			name:  "float",
			input: input{source: `console.log(3.14);`},
			expected: expected{
				consoleLog: []string{"3.14"},
			},
		},
		{
			name:  "boolean true",
			input: input{source: `console.log(true);`},
			expected: expected{
				consoleLog: []string{"true"},
			},
		},
		{
			name:  "boolean false",
			input: input{source: `console.log(false);`},
			expected: expected{
				consoleLog: []string{"false"},
			},
		},
		{
			name:  "null",
			input: input{source: `console.log(null);`},
			expected: expected{
				consoleLog: []string{"null"},
			},
		},
		{
			name: "undefined",
			input: input{
				source: `console.log(undefined);`,
			},
			expected: expected{
				consoleLog: []string{"undefined"},
			},
		},
		{
			name:  "empty string",
			input: input{source: `console.log("");`},
			expected: expected{
				consoleLog: []string{""},
			},
		},

		// Objects — auto-stringified as JSON
		{
			name: "simple object",
			input: input{
				source: `console.log({a: 1, b: "two"});`,
			},
			expected: expected{
				consoleLog: []string{
					`{"a":1,"b":"two"}`,
				},
			},
		},
		{
			name: "nested object",
			input: input{
				source: `console.log(` +
					`{user: {name: "Alice", ` +
					`age: 30}});`,
			},
			expected: expected{
				consoleLog: []string{
					`{"user":{"name":"Alice",` +
						`"age":30}}`,
				},
			},
		},
		{
			name: "empty object",
			input: input{
				source: `console.log({});`,
			},
			expected: expected{
				consoleLog: []string{`{}`},
			},
		},

		// Arrays — auto-stringified as JSON
		{
			name: "array of numbers",
			input: input{
				source: `console.log([1, 2, 3]);`,
			},
			expected: expected{
				consoleLog: []string{`[1,2,3]`},
			},
		},
		{
			name: "array of strings",
			input: input{
				source: `console.log(["a", "b"]);`,
			},
			expected: expected{
				consoleLog: []string{
					`["a","b"]`,
				},
			},
		},
		{
			name: "array of objects",
			input: input{
				source: `console.log(` +
					`[{id: 1}, {id: 2}]);`,
			},
			expected: expected{
				consoleLog: []string{
					`[{"id":1},{"id":2}]`,
				},
			},
		},
		{
			name: "empty array",
			input: input{
				source: `console.log([]);`,
			},
			expected: expected{
				consoleLog: []string{`[]`},
			},
		},
		{
			name: "nested array",
			input: input{
				source: `console.log([[1, 2], [3]]);`,
			},
			expected: expected{
				consoleLog: []string{`[[1,2],[3]]`},
			},
		},

		// Mixed arguments
		{
			name: "string and object",
			input: input{
				source: `console.log(` +
					`"result:", {id: "C001"});`,
			},
			expected: expected{
				consoleLog: []string{
					`result: {"id":"C001"}`,
				},
			},
		},
		{
			name: "string number and array",
			input: input{
				source: `console.log(` +
					`"items:", 3, [1, 2, 3]);`,
			},
			expected: expected{
				consoleLog: []string{
					`items: 3 [1,2,3]`,
				},
			},
		},
		{
			name: "label with nested object",
			input: input{
				source: `console.log("filtered:",` +
					` {users: [{name: "Alice"}]});`,
			},
			expected: expected{
				consoleLog: []string{
					`filtered: {"users":` +
						`[{"name":"Alice"}]}`,
				},
			},
		},

		// Multiple console.log calls
		{
			name: "multiple calls mixed types",
			input: input{
				source: `console.log("start");
console.log({status: "ok"});
console.log([1, 2]);
console.log("end");`,
			},
			expected: expected{
				consoleLog: []string{
					"start",
					`{"status":"ok"}`,
					`[1,2]`,
					"end",
				},
			},
		},

		// Edge cases
		{
			name: "object with null value",
			input: input{
				source: `console.log({a: null});`,
			},
			expected: expected{
				consoleLog: []string{`{"a":null}`},
			},
		},
		{
			name: "no arguments",
			input: input{
				source: `console.log();`,
			},
			expected: expected{
				consoleLog: []string{""},
			},
		},
		{
			name: "computed value from expression",
			input: input{
				source: `var x = {a: 1};
var y = {b: 2};
console.log("merged:", {a: x.a, b: y.b});`,
			},
			expected: expected{
				consoleLog: []string{
					`merged: {"a":1,"b":2}`,
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := New(DefaultConfig())
			result, err := r.Execute(
				context.Background(),
				tc.input.source,
			)
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(
				t, tc.expected.consoleLog,
				result.ConsoleLog,
			)
		})
	}
}
