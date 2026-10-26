package toolchain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rickchristie/gent"

	"github.com/rickchristie/gent/schema"
	"github.com/rickchristie/gent/toolchain/jsruntime"
)

// JsToolChainWrapper adds native programmatic tool calls. Inner calls use the wrapped registry's
// validation, events, limits, and recovery. Each script gets an isolated JavaScript runtime.
type JsToolChainWrapper struct {
	wrapped            gent.ToolChain
	codeTimeout        time.Duration
	codeGuidance       string
	directCallGuidance string
	directCallDisabled bool
	recoveryMessage    gent.RecoveryMessageProvider
}

// NewJsToolChainWrapper creates a wrapper around the
// given ToolChain.
func NewJsToolChainWrapper(
	wrapped gent.ToolChain,
) *JsToolChainWrapper {
	w := &JsToolChainWrapper{
		wrapped:     wrapped,
		codeTimeout: 30 * time.Second,
	}

	return w
}

// WithCodeTimeout sets the timeout for JS execution.
func (w *JsToolChainWrapper) WithCodeTimeout(
	d time.Duration,
) *JsToolChainWrapper {
	w.codeTimeout = d
	return w
}

// WithCodeGuidance sets instructions in the native run_code definition.
func (w *JsToolChainWrapper) WithCodeGuidance(
	guidance string,
) *JsToolChainWrapper {
	w.codeGuidance = guidance
	return w
}

// WithDirectCallGuidance adds instructions to each direct function definition.
func (w *JsToolChainWrapper) WithDirectCallGuidance(
	guidance string,
) *JsToolChainWrapper {
	w.directCallGuidance = guidance
	return w
}

// WithDirectCallDisabled exposes only run_code and puts the inner catalog in its description.
// Direct native business calls are rejected.
func (w *JsToolChainWrapper) WithDirectCallDisabled() *JsToolChainWrapper {
	w.directCallDisabled = true
	return w
}

// GetToolSchema delegates to the wrapped ToolChain.
func (w *JsToolChainWrapper) GetToolSchema(
	name string,
) *schema.Schema {
	return w.wrapped.GetToolSchema(name)
}

// DeduplicateSummary delegates to the wrapped ToolChain.
func (w *JsToolChainWrapper) DeduplicateSummary(
	result *gent.ToolCallResult,
) string {
	return w.wrapped.DeduplicateSummary(result)
}

// RegisterTool delegates to the wrapped ToolChain.
func (w *JsToolChainWrapper) RegisterTool(
	tool any, options ...gent.ToolOption,
) gent.ToolChain {
	w.wrapped.RegisterTool(tool, options...)
	return w
}

// executeSingleCode runs code and returns the result
// without any wrapper section.
func (w *JsToolChainWrapper) executeSingleCode(
	execCtx *gent.ExecutionContext,
	code string,
	textFormat gent.TextFormat,
) (*gent.ToolChainResult, error) {
	if code == "" {
		return &gent.ToolChainResult{}, nil
	}
	return w.executeCode(
		execCtx, code, textFormat,
	)
}

// collectText joins all non-empty Text fields from the
// given ToolChainResult's individual results.
func collectText(
	r *gent.ToolChainResult,
) string {
	if r == nil {
		return ""
	}
	var parts []string
	for _, result := range r.Results {
		if result.Text != "" {
			parts = append(parts, result.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// collectMedia gathers all Media from the given
// ToolChainResults' individual results.
func collectMedia(
	results ...*gent.ToolChainResult,
) []gent.ContentPart {
	var media []gent.ContentPart
	for _, r := range results {
		if r == nil {
			continue
		}
		for _, result := range r.Results {
			media = append(media, result.Media...)
		}
	}
	return media
}

// buildCodeResult constructs a ToolChainResult from a
// collector's results and the wrapper's formatted text.
// Individual results retain their Hash/Input/Output for
// deduplication but have their Text cleared to avoid
// duplication with the wrapper's combined text.
func buildCodeResult(
	collector *jsruntime.CollectedResults,
	text string,
) *gent.ToolChainResult {
	built := collector.BuildResult()

	// Collect media before clearing individual results.
	var media []gent.ContentPart
	for _, r := range built.Results {
		media = append(media, r.Media...)
	}

	// Clear Text/Media on individual results to avoid
	// duplication with the synthetic wrapper entry.
	for _, r := range built.Results {
		r.Text = ""
		r.Media = nil
	}

	built.Results = append(
		built.Results,
		&gent.ToolCallResult{
			Text:  text,
			Media: media,
		},
	)
	return built
}

// executeCode runs JavaScript code via Sobek, routing
// tool.call() back through the wrapped ToolChain.
func (w *JsToolChainWrapper) executeCode(
	execCtx *gent.ExecutionContext,
	code string,
	textFormat gent.TextFormat,
) (*gent.ToolChainResult, error) {
	// Increment code execution counter

	execCtx.Stats().IncrCounter(
		gent.SCCodeExecutions, 1,
	)

	// Use wrapped toolchain's schema lookup for
	// pre-validation and runtime error enhancement
	schemaFn := jsruntime.SchemaLookupFn(
		w.wrapped.GetToolSchema,
	)

	// Pre-validate: check all literal tool.call()
	// args against schemas before executing code
	preErrs, preErr := jsruntime.PreValidate(
		code, schemaFn,
	)
	if preErr == nil && len(preErrs) > 0 {
		return w.preValidationError(
			execCtx, textFormat,
			code, preErrs,
		)
	}

	// Create runtime with configured timeout
	config := jsruntime.Config{
		Timeout:      w.codeTimeout,
		MaxCallStack: 1024,
	}
	rt := jsruntime.New(config)

	// Create result collector
	collector := jsruntime.NewCollectedResults()

	// A failed checkpoint is not a normal tool error. Stop the VM before another bridge
	// call can replace its pending plan, even if the script catches or ignores errors.
	var callErr error
	callFn := jsruntime.MakeToolCallFn(
		func(calls []gent.ModelToolCall) (
			*gent.ToolChainResult, error,
		) {
			if callErr != nil {
				return nil, callErr
			}
			result, err := w.executeProgramCalls(execCtx, calls, textFormat)
			if err != nil {
				callErr = err
				rt.VM().Interrupt(err)
			}
			return result, err
		},
		collector,
	)

	// Register tool bridge with source and schema
	// lookup for runtime error enhancement
	jsruntime.RegisterToolBridge(
		rt, callFn, code, schemaFn,
	)

	// Execute the code under the run context.
	ctx := execCtx.Context()
	result, err := rt.Execute(ctx, code)
	if callErr != nil {
		return nil, callErr
	}

	if err != nil {
		// Code execution failed

		execCtx.Stats().IncrCounter(
			gent.SCCodeExecutionsError, 1,
		)
		execCtx.Stats().IncrGauge(
			gent.SGCodeExecutionsErrorConsecutive,
			1,
		)

		// Build sections: tool_call_log (if any calls
		// happened before the error) + output with
		// the error message.
		var sections []gent.FormattedSection
		logText := buildToolCallLog(collector.Groups, schemaFn)
		if logText != "" {
			sections = append(
				sections, gent.FormattedSection{
					Name:    "tool_call_log",
					Content: logText,
				},
			)
		}
		sections = append(
			sections, gent.FormattedSection{
				Name:    "output",
				Content: err.Error(),
			},
		)
		return buildCodeResult(
			collector,
			textFormat.FormatSections(sections),
		), nil
	}

	// Success — reset consecutive error gauge

	execCtx.Stats().ResetGauge(
		gent.SGCodeExecutionsErrorConsecutive,
	)

	// Build observation sections
	var sections []gent.FormattedSection

	// Tool call log — always include if there were
	// any tool calls, so the LLM sees what happened
	logText := buildToolCallLog(collector.Groups, schemaFn)
	if logText != "" {
		sections = append(
			sections, gent.FormattedSection{
				Name:    "tool_call_log",
				Content: logText,
			},
		)
	}

	// Script output — always present on success.
	// Shows console.log content or a confirmation.
	output := "Code executed successfully."
	if len(result.ConsoleLog) > 0 {
		output = strings.Join(
			result.ConsoleLog, "\n",
		)
	}
	sections = append(
		sections, gent.FormattedSection{
			Name:    "output",
			Content: output,
		},
	)

	return buildCodeResult(
		collector,
		textFormat.FormatSections(sections),
	), nil
}

// preValidationError builds the error result when
// pre-validation catches schema errors before code
// execution.
func (w *JsToolChainWrapper) preValidationError(
	execCtx *gent.ExecutionContext,
	textFormat gent.TextFormat,
	code string,
	preErrs []jsruntime.PreValidationError,
) (*gent.ToolChainResult, error) {

	execCtx.Stats().IncrCounter(
		gent.SCCodeExecutionsError, 1,
	)
	execCtx.Stats().IncrGauge(
		gent.SGCodeExecutionsErrorConsecutive, 1,
	)

	msg := jsruntime.FormatPreValidationErrors(
		code, preErrs,
	)
	errorText := textFormat.FormatSections(
		[]gent.FormattedSection{
			{
				Name:    "output",
				Content: msg,
			},
		},
	)
	return &gent.ToolChainResult{
		Results: []*gent.ToolCallResult{
			{Text: errorText},
		},
	}, nil
}

// buildToolCallLog formats all tool call groups into a
// human-readable log. Sequential calls get [1], [2], etc.
// Parallel calls get [2a], [2b], [2c], etc.
// schemaFn is used to produce enhanced error messages for
// schema validation errors.
func buildToolCallLog(
	groups []jsruntime.ToolCallGroup,
	schemaFn jsruntime.SchemaLookupFn,
) string {
	if len(groups) == 0 {
		return ""
	}

	var sb strings.Builder
	groupNum := 0
	for _, group := range groups {
		groupNum++
		if len(group.Entries) == 1 {
			writeLogEntry(
				&sb,
				fmt.Sprintf("[%d]", groupNum),
				group.Entries[0],
				schemaFn,
			)
		} else {
			for i, entry := range group.Entries {
				writeLogEntry(
					&sb,
					fmt.Sprintf(
						"[%d%c]",
						groupNum, 'a'+rune(i),
					),
					entry,
					schemaFn,
				)
			}
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

// writeLogEntry writes a single tool call log entry
// in the format: [N] name(args) -> output or error.
// For schema validation errors, uses FormatForLLM +
// WriteExampleCall to produce enhanced error messages.
func writeLogEntry(
	sb *strings.Builder,
	prefix string,
	entry jsruntime.ToolCallEntry,
	schemaFn jsruntime.SchemaLookupFn,
) {
	name := ""
	if entry.Result != nil {
		name = entry.Result.Name
	}
	sb.WriteString(prefix)
	sb.WriteString(" ")
	sb.WriteString(name)

	// Append args
	if entry.Result != nil && entry.Result.Args != nil {
		sb.WriteString("(")
		sb.WriteString(formatOutput(entry.Result.Args))
		sb.WriteString(")")
	}

	if entry.Result != nil && entry.Result.Error != nil {
		enhanced := enhanceLogError(
			entry, schemaFn,
		)
		if enhanced != "" {
			sb.WriteString(" -> error:\n")
			sb.WriteString(enhanced)
		} else {
			sb.WriteString(" -> error: ")
			sb.WriteString(entry.Result.Error.Error())
			sb.WriteString("\n")
		}
		return
	}

	if entry.Result != nil && entry.Result.Output != nil {
		sb.WriteString(" -> ")
		sb.WriteString(
			formatOutput(entry.Result.Output),
		)
	}
	sb.WriteString("\n")
}

// enhanceLogError returns an enhanced error message for
// schema validation errors using FormatForLLM +
// WriteExampleCall. Returns "" for non-schema errors.
func enhanceLogError(
	entry jsruntime.ToolCallEntry,
	schemaFn jsruntime.SchemaLookupFn,
) string {
	if schemaFn == nil || entry.Result == nil {
		return ""
	}
	var ve *schema.ValidationError
	if !errors.As(entry.Result.Error, &ve) {
		return ""
	}
	sch := schemaFn(entry.Result.Name)
	if sch == nil {
		return ""
	}
	msg := sch.FormatForLLM(
		entry.Result.Name, entry.Result.Args,
	)
	if msg == "" {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(msg)
	jsruntime.WriteExampleCall(
		&sb, entry.Result.Name, sch,
	)
	return sb.String()
}

// formatOutput converts a tool output to a JSON string
// for display in the tool call log. Handles Go structs
// (json.Marshal respects json tags), JSON strings
// (returned as-is), and plain strings (quoted).
func formatOutput(output any) string {
	if output == nil {
		return "null"
	}

	// String output: if it's valid JSON, return as-is.
	// Otherwise marshal to get a quoted string.
	if str, ok := output.(string); ok {
		if json.Valid([]byte(str)) {
			return str
		}
		data, _ := json.Marshal(str)
		return string(data)
	}

	// Go struct or other type — marshal respects json
	// tags, producing snake_case keys.
	data, err := json.Marshal(output)
	if err != nil {
		return fmt.Sprintf("%v", output)
	}
	return string(data)
}

// defaultCodeGuidance returns the default guidance text
// for the native run_code tool.
func defaultCodeGuidance() string {
	return `(function() {
// Sequential calls — check .error before using .output:
const result1 = tool.call(
  {tool: "tool1", args: {id: "C001"}}
);
// Errors automatically logged, return immediately.
if (result1.error) return;

const result2 = tool.call(
  {tool: "tool2", args: {id: result1.output.tag}}
);
if (result2.error) return;

// Parallel calls:
const results = tool.parallelCall([
  {tool: "tool3", args: {name: result2.output.name}},
  {tool: "tool4",
   args: {category: result2.output.category}},
]);
if (results[0].error || results[1].error) return;

// Tool call results appear automatically in output, ` +
		`do not console.log them.
// The above script will automatically print ` +
		`result.output of tool1, tool2, tool3, tool4.
// Only use console.log for values you computed ` +
		`or customized.
})();

// IMPORTANT: Use EXACT argument names and types ` +
		`from the tool schema.
// Do NOT guess, rename, or add extra fields. Only ` +
		`include properties defined in the schema.
// Always wrap code in (function() { ... })(); so ` +
		`return works for early exit on errors.`
}

// Compile-time check that JsToolChainWrapper implements
// gent.ToolChain.
var _ gent.ToolChain = (*JsToolChainWrapper)(nil)
