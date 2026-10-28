package toolchain

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/rickchristie/gent"
)

const nativeCodeTool = "run_code"

// RecoveryConfig covers native program settings that are not part of the wrapped tool catalog.
func (w *JsToolChainWrapper) RecoveryConfig() any {
	return map[string]any{"codeTimeout": w.codeTimeout, "codeGuidance": w.codeGuidance,
		"directCallGuidance": w.directCallGuidance, "directCallDisabled": w.directCallDisabled,
		"wrapped": w.wrapped.RecoveryConfig()}
}

// NativeTools exposes the same local JavaScript runtime through a function call. Code-only mode
// still includes the available tool schemas in its description so code can use exact arguments.
func (w *JsToolChainWrapper) NativeTools(
	ctx *gent.ExecutionContext,
) ([]gent.ToolDefinition, error) {
	chain := w.wrapped
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
	tools = append(tools, gent.ToolDefinition{Type: "function", Function: &gent.FunctionDefinition{
		Name: nativeCodeTool, Description: description,
		Parameters: map[string]any{"type": "object", "properties": map[string]any{
			"code": map[string]any{"type": "string"},
		}, "required": []string{"code"}, "additionalProperties": false},
	}})
	return tools, nil
}

// RegisterCodecs also covers tool outputs saved inside a program result.
func (w *JsToolChainWrapper) RegisterCodecs(registry *gent.CodecRegistry) error {
	if err := registry.RegisterValueType(reflect.TypeOf(programState{})); err != nil {
		return err
	}
	if err := registry.RegisterValueType(reflect.TypeOf(parallelProgramCalls{})); err != nil {
		return err
	}
	return w.wrapped.RegisterCodecs(registry)
}

func (w *JsToolChainWrapper) PrepareCalls(
	ctx *gent.ExecutionContext, calls []gent.ModelToolCall,
) (*gent.ToolPlan, error) {
	return prepareNativeCalls(ctx, w, calls, func(call *parsedCall) error {
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
		chain := w.wrapped
		return chain.ExecuteNext(ctx, plan, format)
	}
	if plan.Abandoned {
		return completePlannedTool(ctx, plan, skippedTool(plan))
	}
	program, found, err := loadProgram(ctx)
	if err != nil {
		return nil, err
	}
	if found {
		return w.recoverProgram(ctx, plan, program, format)
	}
	program = &programState{AttemptId: ctx.AttemptId(), CallId: call.Id,
		Script: call.Args["code"].(string)}
	if err := ctx.SetCheckpointState("gent:program", program); err != nil {
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
	ctx.ClearCheckpointState("gent:program", "gent:program_calls")
	return completePlannedTool(ctx, plan, result)
}

var _ gent.ToolChain = (*JsToolChainWrapper)(nil)
