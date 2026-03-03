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
	tests := []struct {
		name  string
		input struct {
			source  string
			setup   func(r *Runtime)
			config  Config
			ctxFunc func() (context.Context, context.CancelFunc)
		}
		expected struct {
			consoleLog []string
			errContain string // empty = no error
		}
	}{
		{
			name: "simple expression no error",
			input: struct {
				source  string
				setup   func(r *Runtime)
				config  Config
				ctxFunc func() (context.Context, context.CancelFunc)
			}{
				source: "var x = 1 + 2;",
				config: DefaultConfig(),
			},
			expected: struct {
				consoleLog []string
				errContain string
			}{
				consoleLog: []string{},
			},
		},
		{
			name: "console.log single call",
			input: struct {
				source  string
				setup   func(r *Runtime)
				config  Config
				ctxFunc func() (context.Context, context.CancelFunc)
			}{
				source: `console.log("hello");`,
				config: DefaultConfig(),
			},
			expected: struct {
				consoleLog []string
				errContain string
			}{
				consoleLog: []string{"hello"},
			},
		},
		{
			name: "console.log multiple calls",
			input: struct {
				source  string
				setup   func(r *Runtime)
				config  Config
				ctxFunc func() (context.Context, context.CancelFunc)
			}{
				source: `console.log("a"); console.log("b");`,
				config: DefaultConfig(),
			},
			expected: struct {
				consoleLog []string
				errContain string
			}{
				consoleLog: []string{"a", "b"},
			},
		},
		{
			name: "console.log with multiple args",
			input: struct {
				source  string
				setup   func(r *Runtime)
				config  Config
				ctxFunc func() (context.Context, context.CancelFunc)
			}{
				source: `console.log("x", 42, true);`,
				config: DefaultConfig(),
			},
			expected: struct {
				consoleLog []string
				errContain string
			}{
				consoleLog: []string{"x 42 true"},
			},
		},
		{
			name: "registered Go function called from JS",
			input: struct {
				source  string
				setup   func(r *Runtime)
				config  Config
				ctxFunc func() (context.Context, context.CancelFunc)
			}{
				source: `var r = add(3, 4); console.log(r);`,
				config: DefaultConfig(),
				setup: func(r *Runtime) {
					r.RegisterFunc(
						"add",
						func(
							call sobek.FunctionCall,
						) sobek.Value {
							a := call.Arguments[0].ToInteger()
							b := call.Arguments[1].ToInteger()
							return r.VM().ToValue(a + b)
						},
					)
				},
			},
			expected: struct {
				consoleLog []string
				errContain string
			}{
				consoleLog: []string{"7"},
			},
		},
		{
			name: "RegisterObject with methods",
			input: struct {
				source  string
				setup   func(r *Runtime)
				config  Config
				ctxFunc func() (context.Context, context.CancelFunc)
			}{
				source: `var r = math.double(5);` +
					` console.log(r);`,
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
								return r.VM().ToValue(v * 2)
							},
						},
					)
				},
			},
			expected: struct {
				consoleLog []string
				errContain string
			}{
				consoleLog: []string{"10"},
			},
		},
		{
			name: "timeout on infinite loop",
			input: struct {
				source  string
				setup   func(r *Runtime)
				config  Config
				ctxFunc func() (context.Context, context.CancelFunc)
			}{
				source: "while(true) {}",
				config: Config{
					Timeout:      50 * time.Millisecond,
					MaxCallStack: 1024,
				},
			},
			expected: struct {
				consoleLog []string
				errContain string
			}{
				errContain: "execution interrupted: timeout",
			},
		},
		{
			name: "context cancellation interrupts",
			input: struct {
				source  string
				setup   func(r *Runtime)
				config  Config
				ctxFunc func() (context.Context, context.CancelFunc)
			}{
				source: "while(true) {}",
				config: Config{
					Timeout:      5 * time.Second,
					MaxCallStack: 1024,
				},
				ctxFunc: func() (
					context.Context, context.CancelFunc,
				) {
					ctx, cancel := context.WithCancel(
						context.Background(),
					)
					go func() {
						time.Sleep(50 * time.Millisecond)
						cancel()
					}()
					return ctx, cancel
				},
			},
			expected: struct {
				consoleLog []string
				errContain string
			}{
				errContain: "execution interrupted: cancelled",
			},
		},
		{
			name: "syntax error returns formatted error",
			input: struct {
				source  string
				setup   func(r *Runtime)
				config  Config
				ctxFunc func() (context.Context, context.CancelFunc)
			}{
				source: "var x = @;",
				config: DefaultConfig(),
			},
			expected: struct {
				consoleLog []string
				errContain string
			}{
				errContain: "SyntaxError",
			},
		},
		{
			name: "runtime ReferenceError formatted",
			input: struct {
				source  string
				setup   func(r *Runtime)
				config  Config
				ctxFunc func() (context.Context, context.CancelFunc)
			}{
				source: "undefinedVar;",
				config: DefaultConfig(),
			},
			expected: struct {
				consoleLog []string
				errContain string
			}{
				errContain: "ReferenceError",
			},
		},
		{
			name: "stack overflow returns error",
			input: struct {
				source  string
				setup   func(r *Runtime)
				config  Config
				ctxFunc func() (context.Context, context.CancelFunc)
			}{
				source: "(function f() { f(); })();",
				config: Config{
					Timeout:      5 * time.Second,
					MaxCallStack: 64,
				},
			},
			expected: struct {
				consoleLog []string
				errContain string
			}{
				errContain: "at f (<eval>",
			},
		},
		{
			name: "empty source string",
			input: struct {
				source  string
				setup   func(r *Runtime)
				config  Config
				ctxFunc func() (context.Context, context.CancelFunc)
			}{
				source: "",
				config: DefaultConfig(),
			},
			expected: struct {
				consoleLog []string
				errContain string
			}{
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

			if tc.expected.errContain != "" {
				require.Error(t, err)
				assert.Contains(
					t, err.Error(),
					tc.expected.errContain,
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
