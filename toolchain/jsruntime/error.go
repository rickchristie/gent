package jsruntime

import (
	"fmt"
	"strings"

	"github.com/grafana/sobek"
)

// FormatError converts a Sobek error + original source
// into an LLM-friendly error message with code context.
func FormatError(source string, err error) string {
	if err == nil {
		return ""
	}

	switch e := err.(type) {
	case *sobek.CompilerSyntaxError:
		return formatSyntaxError(source, e)
	case *sobek.Exception:
		return formatException(source, e)
	case *sobek.InterruptedError:
		return formatInterruptedError(source, e)
	default:
		return err.Error()
	}
}

// formatSyntaxError formats a compiler syntax error with
// source context around the error location.
func formatSyntaxError(
	source string, err *sobek.CompilerSyntaxError,
) string {
	var sb strings.Builder
	sb.WriteString("SyntaxError: ")
	sb.WriteString(err.Message)
	sb.WriteString("\n")

	if err.File != nil {
		pos := err.File.Position(err.Offset)
		ctx := extractSourceContext(
			source, pos.Line, pos.Column,
			err.Message, 2, 2,
		)
		if ctx != "" {
			sb.WriteString("\n")
			sb.WriteString(ctx)
		}
	}

	return sb.String()
}

// formatException formats a runtime exception with source
// context from the first stack frame.
func formatException(
	source string, err *sobek.Exception,
) string {
	var sb strings.Builder
	sb.WriteString(err.Value().String())
	sb.WriteString("\n")

	frames := err.Stack()
	if len(frames) > 0 {
		pos := frames[0].Position()
		if pos.Line > 0 {
			ctx := extractSourceContext(
				source, pos.Line, pos.Column,
				err.Value().String(), 2, 2,
			)
			if ctx != "" {
				sb.WriteString("\n")
				sb.WriteString(ctx)
			}
		}
	}

	return sb.String()
}

// formatInterruptedError formats a timeout/cancellation
// message with source context if available.
func formatInterruptedError(
	source string, err *sobek.InterruptedError,
) string {
	val := fmt.Sprintf("%v", err.Value())
	var sb strings.Builder
	sb.WriteString("execution interrupted: ")
	sb.WriteString(val)
	sb.WriteString("\n")

	frames := err.Stack()
	if len(frames) > 0 {
		pos := frames[0].Position()
		if pos.Line > 0 {
			ctx := extractSourceContext(
				source, pos.Line, pos.Column,
				val, 2, 2,
			)
			if ctx != "" {
				sb.WriteString("\n")
				sb.WriteString(ctx)
			}
		}
	}

	return sb.String()
}

// extractSourceContext builds a code snippet around
// line:col showing 2 lines before/after with a caret
// pointing at the error.
func extractSourceContext(
	source string,
	line, col int,
	message string,
	before, after int,
) string {
	if source == "" || line <= 0 {
		return ""
	}

	lines := strings.Split(source, "\n")
	if line > len(lines) {
		return ""
	}

	var sb strings.Builder

	startLine := max(line-before, 1)
	endLine := min(line+after, len(lines))

	// Calculate width needed for line numbers
	width := len(fmt.Sprintf("%d", endLine))

	for i := startLine; i <= endLine; i++ {
		prefix := fmt.Sprintf(
			"%*d | ", width, i,
		)
		sb.WriteString(prefix)
		sb.WriteString(lines[i-1])
		sb.WriteString("\n")

		// Add caret on error line
		if i == line && col > 0 {
			padding := strings.Repeat(" ", len(prefix))
			caret := strings.Repeat(" ", col-1) + "^"
			sb.WriteString(padding)
			sb.WriteString(caret)
			if message != "" {
				sb.WriteString(" ")
				sb.WriteString(message)
			}
			sb.WriteString("\n")
		}
	}

	return sb.String()
}
