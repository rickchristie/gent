package toolchain

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rickchristie/gent"
	"github.com/tmc/langchaingo/llms"
)

const nativeCodeTool = "run_code"

// RecoveryConfig covers native program settings that are not part of the wrapped tool catalog.
func (w *JsToolChainWrapper) RecoveryConfig() any {
	return map[string]any{"codeTimeout": w.codeTimeout, "codeGuidance": w.codeGuidance,
		"directCallGuidance": w.directCallGuidance, "directCallDisabled": w.directCallDisabled}
}

// NativeTools exposes the same local JavaScript runtime through a function call. Code-only mode
// still includes the available tool schemas in its description so code can use exact arguments.
func (w *JsToolChainWrapper) NativeTools(ctx *gent.ExecutionContext) ([]llms.Tool, error) {
	chain, err := w.nativeChain()
	if err != nil {
		return nil, err
	}
	tools, err := chain.NativeTools(ctx)
	if err != nil {
		return nil, err
	}
	var catalog strings.Builder
	for _, tool := range tools {
		if tool.Function.Name == nativeCodeTool {
			return nil, fmt.Errorf("tool name %q is reserved for JavaScript execution", nativeCodeTool)
		}
		if w.directCallDisabled {
			encoded, err := json.Marshal(tool.Function)
			if err != nil {
				return nil, err
			}
			catalog.Write(encoded)
			catalog.WriteByte('\n')
		}
		if w.directCallGuidance != "" {
			tool.Function.Description += "\n\n" + w.directCallGuidance
		}
	}
	guidance := w.codeGuidance
	if guidance == "" {
		guidance = defaultCodeGuidance()
	}
	description := "Run JavaScript on the application server.\n" +
		"Use tool.call or tool.parallelCall for registered tools.\n\n" + guidance
	if w.directCallDisabled {
		description += "\n\nAvailable tools inside code:\n" + catalog.String()
		tools = nil
	} else {
		description += "\n\nThe other function definitions also describe the tools available in code."
	}
	tools = append(tools, llms.Tool{Type: "function", Function: &llms.FunctionDefinition{
		Name: nativeCodeTool, Description: description,
		Parameters: map[string]any{"type": "object", "properties": map[string]any{
			"code": map[string]any{"type": "string"},
		}, "required": []string{"code"}, "additionalProperties": false},
	}})
	return tools, nil
}

func (w *JsToolChainWrapper) nativeChain() (gent.NativeToolChain, error) {
	chain, ok := w.wrapped.(gent.NativeToolChain)
	if !ok {
		return nil, fmt.Errorf("tool chain %T does not support native calls", w.wrapped)
	}
	return chain, nil
}

// RegisterCodecs also covers tool outputs saved inside a program result.
func (w *JsToolChainWrapper) RegisterCodecs(registry *gent.CodecRegistry) error {
	if chain, ok := w.wrapped.(interface {
		RegisterCodecs(*gent.CodecRegistry) error
	}); ok {
		return chain.RegisterCodecs(registry)
	}
	return nil
}

func (w *JsToolChainWrapper) PrepareCalls(
	ctx *gent.ExecutionContext, calls []llms.ToolCall,
) (*gent.ToolPlan, error) {
	return prepareNativeCalls(ctx, w, calls, func(call *gent.ToolCall) error {
		return w.checkNativeCall(call.Name, call.Args)
	})
}

func (w *JsToolChainWrapper) checkNativeCall(name string, args map[string]any) error {
	if name != nativeCodeTool {
		if w.directCallDisabled {
			return fmt.Errorf("call %q through %s", name, nativeCodeTool)
		}
		return nil
	}
	if _, ok := args["code"].(string); !ok || len(args) != 1 {
		return fmt.Errorf("%s requires one string argument named code", nativeCodeTool)
	}
	return nil
}

func (w *JsToolChainWrapper) ExecuteNext(
	ctx *gent.ExecutionContext, plan *gent.ToolPlan, format gent.TextFormat,
) (*gent.ToolCallResult, error) {
	if err := checkToolPlan(ctx, plan); err != nil {
		return nil, err
	}
	call := plan.Calls[plan.NextCall]
	if err := w.checkNativeCall(call.Name, call.Args); err != nil {
		return nil, err
	}
	if call.Name != nativeCodeTool {
		chain, err := w.nativeChain()
		if err != nil {
			return nil, err
		}
		return chain.ExecuteNext(ctx, plan, format)
	}
	// A program can complete several external effects before it returns. Its JavaScript stack is
	// not saved, so recovery must refuse to restart it while this marker is present.
	if err := ctx.SetCheckpointState("gent:toolchain_active", "JavaScript toolchain"); err != nil {
		return nil, err
	}
	if err := ctx.SetToolPlan(plan); err != nil {
		return nil, err
	}
	if err := ctx.CommitCheckpoint(gent.PhaseDecisionReady); err != nil {
		return nil, err
	}
	output, err := w.executeSingleCode(ctx, call.Args["code"].(string), format)
	if recordErr := ctx.RecordingError(); recordErr != nil {
		return nil, recordErr
	}
	if err != nil {
		return nil, err
	}
	if ctx.IsWorkerInterrupted() {
		return nil, ctx.Context().Err()
	}
	result := &gent.ToolCallResult{Name: call.Name, Args: call.Args,
		ToolCallId: call.Id, AttemptId: ctx.AttemptId(), Output: output,
		Text: collectText(output), Media: collectMedia(output), SummaryCaptured: true}
	// Save the outer result and clear the marker in the same checkpoint. Inner calls use the
	// existing tool bridge, validation, limits, and per-tool evidence. They keep their own IDs.
	ctx.ClearCheckpointState("gent:toolchain_active")
	return completePlannedTool(ctx, plan, result)
}

var _ gent.NativeToolChain = (*JsToolChainWrapper)(nil)
