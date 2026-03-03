package toolchain

import (
	"strings"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/format"
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

// RegisterTool delegates to the wrapped ToolChain.
func (w *JsToolChainWrapper) RegisterTool(
	tool any,
) gent.ToolChain {
	w.wrapped.RegisterTool(tool)
	return w
}

// AvailableToolsPrompt returns the wrapped ToolChain's
// prompt plus a JS environment description.
func (w *JsToolChainWrapper) AvailableToolsPrompt() string {
	var sb strings.Builder
	sb.WriteString(w.wrapped.AvailableToolsPrompt())
	sb.WriteString("\n\n")
	sb.WriteString(
		"JavaScript Environment:\n" +
			"When using <code> mode, the following " +
			"functions are available:\n" +
			"- tool.call({tool, args}) — call a single" +
			" tool, returns {name, output} or " +
			"{name, error}\n" +
			"- tool.parallelCall([{tool, args}, ...]) " +
			"— call multiple tools, returns array of " +
			"results\n" +
			"- console.log(...) — output results " +
			"(only console.log output is returned)\n",
	)
	return sb.String()
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

// Execute detects the mode and routes accordingly.
func (w *JsToolChainWrapper) Execute(
	execCtx *gent.ExecutionContext,
	content string,
	textFormat gent.TextFormat,
) (*gent.ToolChainResult, error) {
	if textFormat == nil {
		panic("textFormat must not be nil")
	}

	// Detect mode via inner format
	sections, err := w.innerFormat.Parse(nil, content)

	// Direct call path
	if err == nil {
		if dc, ok := sections["direct_call"]; ok &&
			len(dc) > 0 {
			return w.wrapped.Execute(
				execCtx, dc[0], textFormat,
			)
		}
	}

	// Code path
	if err == nil {
		if code, ok := sections["code"]; ok &&
			len(code) > 0 {
			return w.executeCode(
				execCtx, code[0], textFormat,
			)
		}

		// Empty code block (tags present but no
		// content) — return noop result
		if _, ok := sections["code"]; ok {
			return &gent.ToolChainResult{
				Text: "Code executed successfully.",
				Raw:  &gent.RawToolChainResult{},
			}, nil
		}
	}

	// Check if content has <code> tags even if parse
	// failed (e.g. empty content between tags)
	if hasCodeTags(content) {
		return &gent.ToolChainResult{
			Text: "Code executed successfully.",
			Raw:  &gent.RawToolChainResult{},
		}, nil
	}

	// Fallback: pass through to wrapped ToolChain
	return w.wrapped.Execute(
		execCtx, content, textFormat,
	)
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

	// Register tool bridge
	jsruntime.RegisterToolBridge(rt, callFn)

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
				gent.SGCodeExecutionsErrorConsecutive, 1,
			)
		}

		// Return error as observation text
		errorText := textFormat.FormatSections(
			[]gent.FormattedSection{
				{
					Name:    "code_error",
					Content: err.Error(),
				},
			},
		)
		return &gent.ToolChainResult{
			Text:  errorText,
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

	// Build result text from console.log output
	var text string
	if len(result.ConsoleLog) > 0 {
		text = strings.Join(result.ConsoleLog, "\n")
	} else if len(collector.TextParts) > 0 {
		text = strings.Join(collector.TextParts, "\n")
	} else {
		text = "Code executed successfully."
	}

	return &gent.ToolChainResult{
		Text:  text,
		Raw:   collector.BuildRaw(),
		Media: collector.AllMedia,
	}, nil
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
	return `// Sequential calls (use result of first ` +
		`in second):
const customer = tool.call(
  {tool: "lookup_customer", args: {id: "C001"}}
);
const orders = tool.call(
  {tool: "get_orders",
   args: {customer_id: customer.output.id}}
);

// Parallel calls:
const results = tool.parallelCall([
  {tool: "tool1", args: {}},
  {tool: "tool2", args: {}},
]);

// Output results (only console.log output is returned):
console.log(JSON.stringify({customer, orders}));`
}

// Compile-time check that JsToolChainWrapper implements
// gent.ToolChain.
var _ gent.ToolChain = (*JsToolChainWrapper)(nil)
