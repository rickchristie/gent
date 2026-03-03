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
	tests := []struct {
		name     string
		input    struct {
			source string
			err    error
		}
		expected string
	}{
		{
			name: "nil error returns empty string",
			input: struct {
				source string
				err    error
			}{
				source: "var x = 1;",
				err:    nil,
			},
			expected: "",
		},
		{
			name: "non-sobek error returns Error()",
			input: struct {
				source string
				err    error
			}{
				source: "var x = 1;",
				err:    errors.New("generic failure"),
			},
			expected: "generic failure",
		},
		{
			name: "syntax error at line 1",
			input: struct {
				source string
				err    error
			}{
				source: "var x = @;",
				err:    runAndGetError("var x = @;"),
			},
		},
		{
			name: "syntax error in middle of code",
			input: struct {
				source string
				err    error
			}{
				source: "var a = 1;\nvar b = 2;\nvar c = @;\nvar d = 4;",
				err:    runAndGetError("var a = 1;\nvar b = 2;\nvar c = @;\nvar d = 4;"),
			},
		},
		{
			name: "runtime ReferenceError",
			input: struct {
				source string
				err    error
			}{
				source: "var x = undefinedVar;",
				err:    runAndGetError("var x = undefinedVar;"),
			},
		},
		{
			name: "runtime TypeError on property access",
			input: struct {
				source string
				err    error
			}{
				source: "var a = null;\nvar b = a.name;",
				err:    runAndGetError("var a = null;\nvar b = a.name;"),
			},
		},
		{
			name: "custom throw new Error",
			input: struct {
				source string
				err    error
			}{
				source: "throw new Error('custom msg');",
				err:    runAndGetError("throw new Error('custom msg');"),
			},
		},
		{
			name: "timeout on infinite loop",
			input: struct {
				source string
				err    error
			}{
				source: "while(true) {}",
				err:    runAndGetInterruptError("while(true) {}"),
			},
		},
		{
			name: "single-line source with error",
			input: struct {
				source string
				err    error
			}{
				source: "x",
				err:    runAndGetError("x"),
			},
		},
		{
			name: "empty source string with error",
			input: struct {
				source string
				err    error
			}{
				source: "",
				err:    errors.New("some error"),
			},
			expected: "some error",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := FormatError(
				tc.input.source, tc.input.err,
			)

			if tc.expected != "" {
				assert.Equal(t, tc.expected, result)
				return
			}

			// For sobek errors, verify structure
			if tc.input.err == nil {
				assert.Empty(t, result)
				return
			}

			assert.NotEmpty(t, result)

			// Syntax errors should contain source
			// context
			switch tc.input.err.(type) {
			case *sobek.CompilerSyntaxError:
				assert.Contains(t, result, "|")
				assert.Contains(t, result, "^")
			case *sobek.Exception:
				// Runtime errors should have the
				// error message
				assert.NotEmpty(t, result)
			case *sobek.InterruptedError:
				assert.Contains(
					t, result,
					"execution interrupted",
				)
			}
		})
	}
}

func TestExtractSourceContext(t *testing.T) {
	tests := []struct {
		name  string
		input struct {
			source  string
			line    int
			col     int
			message string
		}
		expected string
	}{
		{
			name: "error at line 3 of 5 line source",
			input: struct {
				source  string
				line    int
				col     int
				message string
			}{
				source:  "line1\nline2\nline3\nline4\nline5",
				line:    3,
				col:     1,
				message: "error here",
			},
			expected: "1 | line1\n" +
				"2 | line2\n" +
				"3 | line3\n" +
				"    ^ error here\n" +
				"4 | line4\n" +
				"5 | line5\n",
		},
		{
			name: "error at line 1",
			input: struct {
				source  string
				line    int
				col     int
				message string
			}{
				source:  "first\nsecond\nthird",
				line:    1,
				col:     3,
				message: "oops",
			},
			expected: "1 | first\n" +
				"      ^ oops\n" +
				"2 | second\n" +
				"3 | third\n",
		},
		{
			name: "error at last line",
			input: struct {
				source  string
				line    int
				col     int
				message string
			}{
				source:  "a\nb\nc",
				line:    3,
				col:     2,
				message: "end error",
			},
			expected: "1 | a\n" +
				"2 | b\n" +
				"3 | c\n" +
				"     ^ end error\n",
		},
		{
			name: "empty source returns empty",
			input: struct {
				source  string
				line    int
				col     int
				message string
			}{
				source:  "",
				line:    1,
				col:     1,
				message: "x",
			},
			expected: "",
		},
		{
			name: "line zero returns empty",
			input: struct {
				source  string
				line    int
				col     int
				message string
			}{
				source:  "hello",
				line:    0,
				col:     1,
				message: "x",
			},
			expected: "",
		},
		{
			name: "line beyond source returns empty",
			input: struct {
				source  string
				line    int
				col     int
				message string
			}{
				source:  "hello",
				line:    5,
				col:     1,
				message: "x",
			},
			expected: "",
		},
		{
			name: "col at 1 - caret at start",
			input: struct {
				source  string
				line    int
				col     int
				message string
			}{
				source:  "bad",
				line:    1,
				col:     1,
				message: "here",
			},
			expected: "1 | bad\n" +
				"    ^ here\n",
		},
		{
			name: "col zero means no caret",
			input: struct {
				source  string
				line    int
				col     int
				message string
			}{
				source:  "ok",
				line:    1,
				col:     0,
				message: "msg",
			},
			expected: "1 | ok\n",
		},
		{
			name: "single-line source",
			input: struct {
				source  string
				line    int
				col     int
				message string
			}{
				source:  "only line",
				line:    1,
				col:     5,
				message: "err",
			},
			expected: "1 | only line\n" +
				"        ^ err\n",
		},
		{
			name: "double-digit line numbers align",
			input: struct {
				source  string
				line    int
				col     int
				message string
			}{
				source: "1\n2\n3\n4\n5\n6\n" +
					"7\n8\n9\n10\n11\n12",
				line:    10,
				col:     1,
				message: "x",
			},
			expected: " 8 | 8\n" +
				" 9 | 9\n" +
				"10 | 10\n" +
				"     ^ x\n" +
				"11 | 11\n" +
				"12 | 12\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := extractSourceContext(
				tc.input.source,
				tc.input.line,
				tc.input.col,
				tc.input.message,
			)
			assert.Equal(t, tc.expected, result)
		})
	}
}
