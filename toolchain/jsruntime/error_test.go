package jsruntime

import (
	"errors"
	"testing"
	"time"

	"github.com/grafana/sobek"
	"github.com/stretchr/testify/assert"
)

// runAndGetError executes JS source and returns the error.
func runAndGetError(source string) error {
	vm := sobek.New()
	_, err := vm.RunString(source)
	return err
}

// runAndGetInterruptError runs an infinite loop and
// interrupts it after a short delay.
func runAndGetInterruptError(source string) error {
	vm := sobek.New()
	time.AfterFunc(10*time.Millisecond, func() {
		vm.Interrupt("timeout")
	})
	_, err := vm.RunString(source)
	return err
}

func TestFormatError(t *testing.T) {
	type input struct {
		source string
		err    error
	}

	type expected struct {
		output string
	}

	multiLineSrc := `var a = 1;
var b = 2;
var c = @;
var d = 4;`

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "nil error returns empty string",
			input: input{
				source: "var x = 1;",
				err:    nil,
			},
			expected: expected{
				output: "",
			},
		},
		{
			name: "non-sobek error returns Error()",
			input: input{
				source: "var x = 1;",
				err:    errors.New("generic failure"),
			},
			expected: expected{
				output: "generic failure",
			},
		},
		{
			name: "syntax error at line 1",
			input: input{
				source: "var x = @;",
				err:    runAndGetError("var x = @;"),
			},
			expected: expected{
				output: `SyntaxError: SyntaxError: (anonymous): Line 1:9 Unexpected token ILLEGAL (and 2 more errors)
`,
			},
		},
		{
			name: "syntax error in middle of code",
			input: input{
				source: multiLineSrc,
				err:    runAndGetError(multiLineSrc),
			},
			expected: expected{
				output: `SyntaxError: SyntaxError: (anonymous): Line 3:9 Unexpected token ILLEGAL (and 2 more errors)
`,
			},
		},
		{
			name: "runtime ReferenceError",
			input: input{
				source: "var x = undefinedVar;",
				err: runAndGetError(
					"var x = undefinedVar;",
				),
			},
			expected: expected{
				output: `ReferenceError: undefinedVar is not defined

1 | var x = undefinedVar;
            ^ ReferenceError: undefinedVar is not defined
`,
			},
		},
		{
			name: "runtime TypeError on property access",
			input: input{
				source: "var a = null;\nvar b = a.name;",
				err: runAndGetError(
					"var a = null;\nvar b = a.name;",
				),
			},
			expected: expected{
				output: `TypeError: Cannot read property 'name' of undefined

1 | var a = null;
2 | var b = a.name;
              ^ TypeError: Cannot read property 'name' of undefined
`,
			},
		},
		{
			name: "custom throw new Error",
			input: input{
				source: "throw new Error('custom msg');",
				err: runAndGetError(
					"throw new Error('custom msg');",
				),
			},
			expected: expected{
				output: `Error: custom msg

1 | throw new Error('custom msg');
          ^ Error: custom msg
`,
			},
		},
		{
			name: "timeout on infinite loop",
			input: input{
				source: "while(true) {}",
				err: runAndGetInterruptError(
					"while(true) {}",
				),
			},
			expected: expected{
				output: `execution interrupted: timeout

1 | while(true) {}
    ^ timeout
`,
			},
		},
		{
			name: "single-line source with error",
			input: input{
				source: "x",
				err:    runAndGetError("x"),
			},
			expected: expected{
				output: `ReferenceError: x is not defined

1 | x
    ^ ReferenceError: x is not defined
`,
			},
		},
		{
			name: "empty source string with error",
			input: input{
				source: "",
				err:    errors.New("some error"),
			},
			expected: expected{
				output: "some error",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := FormatError(
				tc.input.source, tc.input.err,
			)
			assert.Equal(
				t, tc.expected.output, result,
			)
		})
	}
}

func TestExtractSourceContext(t *testing.T) {
	type input struct {
		source  string
		line    int
		col     int
		message string
	}

	type expected struct {
		output string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "error at line 3 of 5 line source",
			input: input{
				source: `line1
line2
line3
line4
line5`,
				line:    3,
				col:     1,
				message: "error here",
			},
			expected: expected{
				output: `1 | line1
2 | line2
3 | line3
    ^ error here
4 | line4
5 | line5
`,
			},
		},
		{
			name: "error at line 1",
			input: input{
				source: `first
second
third`,
				line:    1,
				col:     3,
				message: "oops",
			},
			expected: expected{
				output: `1 | first
      ^ oops
2 | second
3 | third
`,
			},
		},
		{
			name: "error at last line",
			input: input{
				source: "a\nb\nc",
				line:    3,
				col:     2,
				message: "end error",
			},
			expected: expected{
				output: `1 | a
2 | b
3 | c
     ^ end error
`,
			},
		},
		{
			name: "empty source returns empty",
			input: input{
				source:  "",
				line:    1,
				col:     1,
				message: "x",
			},
			expected: expected{output: ""},
		},
		{
			name: "line zero returns empty",
			input: input{
				source:  "hello",
				line:    0,
				col:     1,
				message: "x",
			},
			expected: expected{output: ""},
		},
		{
			name: "line beyond source returns empty",
			input: input{
				source:  "hello",
				line:    5,
				col:     1,
				message: "x",
			},
			expected: expected{output: ""},
		},
		{
			name: "col at 1 - caret at start",
			input: input{
				source:  "bad",
				line:    1,
				col:     1,
				message: "here",
			},
			expected: expected{
				output: `1 | bad
    ^ here
`,
			},
		},
		{
			name: "col zero means no caret",
			input: input{
				source:  "ok",
				line:    1,
				col:     0,
				message: "msg",
			},
			expected: expected{
				output: "1 | ok\n",
			},
		},
		{
			name: "single-line source",
			input: input{
				source:  "only line",
				line:    1,
				col:     5,
				message: "err",
			},
			expected: expected{
				output: `1 | only line
        ^ err
`,
			},
		},
		{
			name: "double-digit line numbers align",
			input: input{
				source: `1
2
3
4
5
6
7
8
9
10
11
12`,
				line:    10,
				col:     1,
				message: "x",
			},
			expected: expected{
				output: ` 8 | 8
 9 | 9
10 | 10
     ^ x
11 | 11
12 | 12
`,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := extractSourceContext(
				tc.input.source,
				tc.input.line,
				tc.input.col,
				tc.input.message,
				2, 2,
			)
			assert.Equal(
				t, tc.expected.output, result,
			)
		})
	}
}
