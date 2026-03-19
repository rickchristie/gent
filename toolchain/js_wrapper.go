package toolchain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/format"
	"github.com/rickchristie/gent/schema"
	"github.com/rickchristie/gent/toolchain/jsruntime"
)

// JsToolChainWrapper wraps any ToolChain to add
// programmatic tool calling (PTC) via JavaScript.
//
// The LLM can output either:
//   - <direct_call> — passes through to the wrapped
//     ToolChain unchanged
//   - <code> — executes JS via Sobek, where tool.call()
//     routes back through the wrapped ToolChain
//
// All existing stats, events, schema validation, and
// limits work unchanged for tool calls made from code.
type JsToolChainWrapper struct {
	wrapped      gent.ToolChain
	codeTimeout  time.Duration
	codeGuidance string
	innerFormat  gent.TextFormat
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

	// Build default inner format for sub-section parsing
	w.innerFormat = buildDefaultInnerFormat()

	return w
}

// WithCodeTimeout sets the timeout for JS execution.
func (w *JsToolChainWrapper) WithCodeTimeout(
	d time.Duration,
) *JsToolChainWrapper {
	w.codeTimeout = d
	return w
}

// WithCodeGuidance sets custom guidance for the code
// section.
func (w *JsToolChainWrapper) WithCodeGuidance(
	guidance string,
) *JsToolChainWrapper {
	w.codeGuidance = guidance
	return w
}

// WithInnerFormat sets a custom TextFormat for parsing
// sub-sections (direct_call vs code).
func (w *JsToolChainWrapper) WithInnerFormat(
	f gent.TextFormat,
) *JsToolChainWrapper {
	w.innerFormat = f
	return w
}

// Name returns the wrapped ToolChain's section name.
func (w *JsToolChainWrapper) Name() string {
	return w.wrapped.Name()
}

// GetToolSchema delegates to the wrapped ToolChain.
func (w *JsToolChainWrapper) GetToolSchema(
	name string,
) *schema.Schema {
	return w.wrapped.GetToolSchema(name)
}

// RegisterTool delegates to the wrapped ToolChain.
func (w *JsToolChainWrapper) RegisterTool(
	tool any,
) gent.ToolChain {
	w.wrapped.RegisterTool(tool)
	return w
}

// AvailableToolsPrompt delegates to the wrapped
// ToolChain. JS environment details (tool.call,
// console.log, etc.) are covered in Guidance().
func (w *JsToolChainWrapper) AvailableToolsPrompt() string {
	return w.wrapped.AvailableToolsPrompt()
}

// Guidance returns combined guidance for both modes.
func (w *JsToolChainWrapper) Guidance() string {
	var sb strings.Builder
	sb.WriteString(
		"You can call tools in two ways:\n\n",
	)
	sb.WriteString(
		"1. Direct call — for simple single or " +
			"parallel tool calls:\n",
	)
	sb.WriteString("<direct_call>\n")
	sb.WriteString(w.wrapped.Guidance())
	sb.WriteString("\n</direct_call>\n\n")

	sb.WriteString(
		"2. Programmatic — for multi-step " +
			"orchestration with logic:\n",
	)
	sb.WriteString("<code>\n")
	if w.codeGuidance != "" {
		sb.WriteString(w.codeGuidance)
	} else {
		sb.WriteString(defaultCodeGuidance())
	}
	sb.WriteString("\n</code>\n\n")

	sb.WriteString(
		"Choose direct_call for simple operations. " +
			"Choose code when you need to chain " +
			"results, apply conditions, or loop.",
	)
	return sb.String()
}

// ParseSection detects sub-section mode and returns
// either parsed tool calls (for direct_call) or the
// code string (for code).
func (w *JsToolChainWrapper) ParseSection(
	execCtx *gent.ExecutionContext,
	content string,
) (any, error) {
	// Parse sub-sections using inner format (nil execCtx
	// to avoid stats pollution from inner format parsing)
	sections, err := w.innerFormat.Parse(nil, content)
	if err == nil {
		// Check for direct_call first (preferred)
		if dc, ok := sections["direct_call"]; ok &&
			len(dc) > 0 {
			return w.wrapped.ParseSection(
				execCtx, dc[0],
			)
		}

		// Check for code
		if code, ok := sections["code"]; ok &&
			len(code) > 0 {
			return code[0], nil
		}
	}

	// Fallback: try wrapped ParseSection directly
	// (graceful degradation when LLM omits sub-section
	// tags)
	return w.wrapped.ParseSection(execCtx, content)
}

// Execute detects the mode(s) and routes accordingly.
// Supports direct_call only, code only, or both in the
// same action. When both are present, results are wrapped
// in <direct_call> and <code_execution> sections.
func (w *JsToolChainWrapper) Execute(
	execCtx *gent.ExecutionContext,
	content string,
	textFormat gent.TextFormat,
) (*gent.ToolChainResult, error) {
	if textFormat == nil {
		panic("textFormat must not be nil")
	}

	// Detect modes via inner format
	sections, err := w.innerFormat.Parse(nil, content)

	var dcContent string
	var codeContent string
	var hasCode bool

	if err == nil {
		if dc, ok := sections["direct_call"]; ok &&
			len(dc) > 0 {
			dcContent = dc[0]
		}
		if code, ok := sections["code"]; ok {
			hasCode = true
			if len(code) > 0 {
				codeContent = code[0]
			}
		}
	}

	// Check for <code> tags even if parse failed
	if !hasCode && hasCodeTags(content) {
		hasCode = true
	}

	hasDC := dcContent != ""
	dualMode := hasDC && hasCode

	// Single mode: direct_call only
	if hasDC && !hasCode {
		return w.executeDirectCall(
			execCtx, dcContent, textFormat,
		)
	}

	// Single mode: code only
	if hasCode && !hasDC {
		return w.executeSingleCode(
			execCtx, codeContent, textFormat,
		)
	}

	// Dual mode: both direct_call and code
	if dualMode {
		return w.executeDualMode(
			execCtx, dcContent, codeContent,
			textFormat,
		)
	}

	// Fallback: pass through to wrapped ToolChain
	return w.wrapped.Execute(
		execCtx, content, textFormat,
	)
}

// executeDirectCall delegates to the wrapped ToolChain.
func (w *JsToolChainWrapper) executeDirectCall(
	execCtx *gent.ExecutionContext,
	content string,
	textFormat gent.TextFormat,
) (*gent.ToolChainResult, error) {
	return w.wrapped.Execute(
		execCtx, content, textFormat,
	)
}

// executeSingleCode runs code and returns the result
// without any wrapper section.
func (w *JsToolChainWrapper) executeSingleCode(
	execCtx *gent.ExecutionContext,
	code string,
	textFormat gent.TextFormat,
) (*gent.ToolChainResult, error) {
	if code == "" {
		return &gent.ToolChainResult{
			Text: "Code executed successfully.",
			Raw:  &gent.RawToolChainResult{},
		}, nil
	}
	return w.executeCode(
		execCtx, code, textFormat,
	)
}

// executeDualMode runs both direct_call and code, then
// combines results with <direct_call> and
// <code_execution> wrapper sections.
func (w *JsToolChainWrapper) executeDualMode(
	execCtx *gent.ExecutionContext,
	dcContent, codeContent string,
	textFormat gent.TextFormat,
) (*gent.ToolChainResult, error) {
	// Run direct_call
	dcResult, dcErr := w.executeDirectCall(
		execCtx, dcContent, textFormat,
	)

	// Run code (even if direct_call failed)
	var codeResult *gent.ToolChainResult
	var codeErr error
	if codeContent == "" {
		codeResult = &gent.ToolChainResult{
			Text: "Code executed successfully.",
			Raw:  &gent.RawToolChainResult{},
		}
	} else {
		codeResult, codeErr = w.executeCode(
			execCtx, codeContent, textFormat,
		)
	}

	// If both errored, return the first Go-level error
	if dcErr != nil && codeErr != nil {
		return nil, dcErr
	}
	if dcErr != nil {
		return nil, dcErr
	}
	if codeErr != nil {
		return nil, codeErr
	}

	// Wrap results in sections
	var outerSections []gent.FormattedSection
	if dcResult != nil && dcResult.Text != "" {
		outerSections = append(
			outerSections, gent.FormattedSection{
				Name:    "direct_call",
				Content: dcResult.Text,
			},
		)
	}
	if codeResult != nil && codeResult.Text != "" {
		outerSections = append(
			outerSections, gent.FormattedSection{
				Name:    "code_execution",
				Content: codeResult.Text,
			},
		)
	}

	// Merge raw results
	merged := mergeRaw(dcResult, codeResult)

	// Merge media
	var media []gent.ContentPart
	if dcResult != nil {
		media = append(media, dcResult.Media...)
	}
	if codeResult != nil {
		media = append(media, codeResult.Media...)
	}

	text := textFormat.FormatSections(outerSections)
	if text == "" {
		text = "Code executed successfully."
	}

	return &gent.ToolChainResult{
		Text:  text,
		Raw:   merged,
		Media: media,
	}, nil
}

// mergeRaw combines RawToolChainResults from two results.
func mergeRaw(
	a, b *gent.ToolChainResult,
) *gent.RawToolChainResult {
	merged := &gent.RawToolChainResult{}
	for _, r := range []*gent.ToolChainResult{a, b} {
		if r != nil && r.Raw != nil {
			merged.Calls = append(
				merged.Calls, r.Raw.Calls...,
			)
			merged.Results = append(
				merged.Results, r.Raw.Results...,
			)
			merged.Errors = append(
				merged.Errors, r.Raw.Errors...,
			)
		}
	}
	return merged
}

// executeCode runs JavaScript code via Sobek, routing
// tool.call() back through the wrapped ToolChain.
func (w *JsToolChainWrapper) executeCode(
	execCtx *gent.ExecutionContext,
	code string,
	textFormat gent.TextFormat,
) (*gent.ToolChainResult, error) {
	// Increment code execution counter
	if execCtx != nil {
		execCtx.Stats().IncrCounter(
			gent.SCCodeExecutions, 1,
		)
	}

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

	// Create ToolCallFn that routes through wrapped
	// ToolChain
	callFn := jsruntime.MakeToolCallFn(
		func(jsonContent string) (
			*gent.ToolChainResult, error,
		) {
			return w.wrapped.Execute(
				execCtx, jsonContent, textFormat,
			)
		},
		collector,
	)

	// Register tool bridge with source and schema
	// lookup for runtime error enhancement
	jsruntime.RegisterToolBridge(
		rt, callFn, code, schemaFn,
	)

	// Execute the code
	ctx := execCtx.Context()
	result, err := rt.Execute(ctx, code)

	if err != nil {
		// Code execution failed
		if execCtx != nil {
			execCtx.Stats().IncrCounter(
				gent.SCCodeExecutionsError, 1,
			)
			execCtx.Stats().IncrGauge(
				gent.SGCodeExecutionsErrorConsecutive,
				1,
			)
		}

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
		return &gent.ToolChainResult{
			Text: textFormat.FormatSections(
				sections,
			),
			Raw:   collector.BuildRaw(),
			Media: collector.AllMedia,
		}, nil
	}

	// Success — reset consecutive error gauge
	if execCtx != nil {
		execCtx.Stats().ResetGauge(
			gent.SGCodeExecutionsErrorConsecutive,
		)
	}

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

	return &gent.ToolChainResult{
		Text: textFormat.FormatSections(sections),
		Raw:  collector.BuildRaw(),
		Media: collector.AllMedia,
	}, nil
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
	if execCtx != nil {
		execCtx.Stats().IncrCounter(
			gent.SCCodeExecutionsError, 1,
		)
		execCtx.Stats().IncrGauge(
			gent.SGCodeExecutionsErrorConsecutive, 1,
		)
	}

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
		Text: errorText,
		Raw:  &gent.RawToolChainResult{},
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
	if entry.Call != nil {
		name = entry.Call.Name
	}
	sb.WriteString(prefix)
	sb.WriteString(" ")
	sb.WriteString(name)

	// Append args
	if entry.Call != nil && entry.Call.Args != nil {
		sb.WriteString("(")
		sb.WriteString(formatOutput(entry.Call.Args))
		sb.WriteString(")")
	}

	if entry.Error != nil {
		enhanced := enhanceLogError(
			entry, schemaFn,
		)
		if enhanced != "" {
			sb.WriteString(" -> error:\n")
			sb.WriteString(enhanced)
		} else {
			sb.WriteString(" -> error: ")
			sb.WriteString(entry.Error.Error())
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
	if schemaFn == nil || entry.Call == nil {
		return ""
	}
	var ve *schema.ValidationError
	if !errors.As(entry.Error, &ve) {
		return ""
	}
	sch := schemaFn(entry.Call.Name)
	if sch == nil {
		return ""
	}
	msg := sch.FormatForLLM(
		entry.Call.Name, entry.Call.Args,
	)
	if msg == "" {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(msg)
	jsruntime.WriteExampleCall(
		&sb, entry.Call.Name, sch,
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

// hasCodeTags returns true if content contains <code>
// and </code> tags, even if the content between them is
// empty.
func hasCodeTags(content string) bool {
	return strings.Contains(content, "<code>") &&
		strings.Contains(content, "</code>")
}

// simpleSection is a lightweight TextSection for inner
// format registration.
type simpleSection struct {
	name string
}

func (s *simpleSection) Name() string {
	return s.name
}

func (s *simpleSection) Guidance() string {
	return ""
}

func (s *simpleSection) ParseSection(
	_ *gent.ExecutionContext, content string,
) (any, error) {
	return content, nil
}

// buildDefaultInnerFormat creates an XML format with
// direct_call and code sections registered.
func buildDefaultInnerFormat() gent.TextFormat {
	f := format.NewXML()
	f.RegisterSection(&simpleSection{name: "direct_call"})
	f.RegisterSection(&simpleSection{name: "code"})
	return f
}

// defaultCodeGuidance returns the default guidance text
// for the code section.
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
