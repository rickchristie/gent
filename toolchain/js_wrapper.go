package toolchain

import (
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

// RegisterTool delegates to the wrapped ToolChain.
func (w *JsToolChainWrapper) RegisterTool(tool any) gent.ToolChain {
	w.wrapped.RegisterTool(tool)
	return w
}

// executeCode runs one script. Tool responses are collected automatically; console output
// contains only values the script explicitly computed or selected.
func (w *JsToolChainWrapper) executeCode(ctx *gent.ExecutionContext, code string,
	textFormat gent.TextFormat,
) (*gent.ToolExecutionResponse, error) {
	if ctx.PTCIntegerMode() != gent.PTCIntegerNumber && ctx.PTCIntegerMode() != gent.PTCIntegerBigInt {
		return nil, fmt.Errorf("invalid PTC integer mode %d", ctx.PTCIntegerMode())
	}
	ctx.Stats().IncrCounter(gent.SCCodeExecutions, 1)
	preErrors, parseErr := jsruntime.PreValidate(code, w.wrapped.GetToolSchema)
	if parseErr == nil && len(preErrors) > 0 {
		ctx.Stats().IncrCounter(gent.SCCodeExecutionsError, 1)
		ctx.Stats().IncrGauge(gent.SGCodeExecutionsErrorConsecutive, 1)
		return &gent.ToolExecutionResponse{Status: gent.ToolInvalidArguments,
			Instructions: jsruntime.FormatPreValidationErrors(code, preErrors)}, nil
	}
	rt := jsruntime.New(jsruntime.Config{Timeout: w.codeTimeout, MaxCallStack: 1024})
	var callErr error
	bridge := jsruntime.RegisterToolBridge(rt, func(calls []*gent.ModelToolCallRequest) (
		[]*gent.ToolCallRecord, error,
	) {
		records, err := w.executeProgramCalls(ctx, calls, textFormat)
		if err != nil {
			callErr = err
		}
		return records, err
	}, ctx.PTCIntegerMode())
	result, err := rt.Execute(ctx.Context(), code)
	if callErr != nil {
		return nil, callErr
	}
	if ctx.Context().Err() != nil {
		return nil, ctx.Context().Err()
	}
	parts := bridge.Parts(ctx.SteeringMessages())
	message := "Code executed successfully."
	if err != nil {
		ctx.Stats().IncrCounter(gent.SCCodeExecutionsError, 1)
		ctx.Stats().IncrGauge(gent.SGCodeExecutionsErrorConsecutive, 1)
		// This is a script syntax/runtime error. Tool Go errors never enter the VM.
		message = err.Error()
	} else {
		ctx.Stats().ResetGauge(gent.SGCodeExecutionsErrorConsecutive)
		if len(result.ConsoleLog) > 0 {
			message = strings.Join(result.ConsoleLog, "\n")
		}
	}
	parts = append(parts, gent.TextPart(textFormat.FormatSections([]gent.FormattedSection{
		{Name: "output", Content: message},
	})))
	return &gent.ToolExecutionResponse{Status: gent.ToolExecuted, Parts: parts}, nil
}

// Keep the direct return explicit. A Promise chain can execute a tool before it fails, so a
// model retry can repeat a side effect.
func defaultCodeGuidance() string {
	return `Use tool.call({tool: "name", args: {...}}) to call one tool.
Tools are not global JavaScript functions. Calls are synchronous and return reply objects directly.
Assign the reply to a variable, check it, then make the next call.
Do not use async, await, .then, or Promise.all with tool calls.
Use tool.parallelCall([{tool: "name", args: {...}}, ...]) for independent calls.
Replies use toolCallId, name, kind, parts, and applicable executionStatus/recoveryStatus.
kind is "execution", "recovery", "call_error", or "recovery_error".
executionStatus is "executed", "blocked", or "invalid_arguments" when present.
recoveryStatus is "completed", "not_started", or "unresolved" when present.
For a normal response, check reply.kind === "execution" and reply.executionStatus === "executed".
An executed response can contain a domain failure. Check the tool's domain data too.
Read structured data from reply.parts.find(part => part.kind === "data").data.
Read text from reply.parts.filter(part => part.kind === "text").map(part => part.text).join("").
Use the tool's declared result format and check that the needed part exists before reading it.
Tool results and instructions appear automatically in output. Do not console.log them.
Byte parts are NOT included automatically. To include them in this script's model reply, use:
tool.forward(reply.parts.filter(part => part.kind === "bytes"));
Byte data is a read-only Uint8Array. Image URLs and data parts remain automatic.
There are no saved part references. Forward bytes in the script that receives them.
Use console.log only for values you computed or customized.
Use exact argument names and types from the tool schema.
Wrap early returns in (function() { ... })();.`
}

var _ gent.ToolChain = (*JsToolChainWrapper)(nil)
