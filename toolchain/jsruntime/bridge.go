package jsruntime

import (
	"encoding/json"
	"fmt"

	"github.com/grafana/sobek"
	"github.com/rickchristie/gent"
)

// ToolCallFn executes a tool call given JSON content.
// Returns the ToolChainResult from the wrapped ToolChain.
// The JSON content is in the format the wrapped ToolChain
// expects (e.g., {"tool":"x","args":{...}} for single,
// or [{...},{...}] for parallel).
type ToolCallFn func(content string) (
	*gent.ToolChainResult, error,
)

// RegisterToolBridge registers tool.call() and
// tool.parallelCall() on the given Runtime.
//
// tool.call({tool: "name", args: {...}})
//
//	→ returns {name, output} or {name, error}
//
// tool.parallelCall([{tool, args}, ...])
//
//	→ returns [{name, output|error}, ...]
func RegisterToolBridge(
	rt *Runtime,
	callFn ToolCallFn,
) {
	vm := rt.VM()
	rt.RegisterObject("tool", map[string]func(
		sobek.FunctionCall,
	) sobek.Value{
		"call": func(
			call sobek.FunctionCall,
		) sobek.Value {
			return toolCall(vm, callFn, call)
		},
		"parallelCall": func(
			call sobek.FunctionCall,
		) sobek.Value {
			return toolParallelCall(vm, callFn, call)
		},
	})
}

// toolCall implements tool.call({tool, args}).
func toolCall(
	vm *sobek.Runtime,
	callFn ToolCallFn,
	call sobek.FunctionCall,
) sobek.Value {
	if len(call.Arguments) < 1 {
		panic(vm.NewTypeError(
			"tool.call requires 1 argument",
		))
	}

	// Export JS object to Go map
	var req map[string]any
	if err := vm.ExportTo(
		call.Arguments[0], &req,
	); err != nil {
		panic(vm.NewTypeError(
			"tool.call argument must be an object: %v",
			err,
		))
	}

	toolName, _ := req["tool"].(string)
	if toolName == "" {
		panic(vm.NewTypeError(
			"tool.call: 'tool' field is required",
		))
	}

	// Marshal to JSON for the wrapped ToolChain
	jsonBytes, err := json.Marshal(req)
	if err != nil {
		panic(vm.NewTypeError(
			"tool.call: failed to marshal: %v", err,
		))
	}

	result, execErr := callFn(string(jsonBytes))
	return buildSingleResult(
		vm, toolName, result, execErr,
	)
}

// toolParallelCall implements
// tool.parallelCall([{tool, args}, ...]).
func toolParallelCall(
	vm *sobek.Runtime,
	callFn ToolCallFn,
	call sobek.FunctionCall,
) sobek.Value {
	if len(call.Arguments) < 1 {
		panic(vm.NewTypeError(
			"tool.parallelCall requires 1 argument",
		))
	}

	// Export JS array to Go slice
	var reqs []map[string]any
	if err := vm.ExportTo(
		call.Arguments[0], &reqs,
	); err != nil {
		panic(vm.NewTypeError(
			"tool.parallelCall argument must be "+
				"an array: %v", err,
		))
	}

	if len(reqs) == 0 {
		return vm.ToValue([]any{})
	}

	// Marshal to JSON array for the wrapped ToolChain
	jsonBytes, err := json.Marshal(reqs)
	if err != nil {
		panic(vm.NewTypeError(
			"tool.parallelCall: failed to marshal: %v",
			err,
		))
	}

	result, execErr := callFn(string(jsonBytes))
	return buildParallelResults(
		vm, reqs, result, execErr,
	)
}

// buildSingleResult converts a ToolChainResult into a JS
// value: {name, output} or {name, error}.
func buildSingleResult(
	vm *sobek.Runtime,
	toolName string,
	result *gent.ToolChainResult,
	execErr error,
) sobek.Value {
	jsResult := make(map[string]any)
	jsResult["name"] = toolName

	if execErr != nil {
		jsResult["error"] = execErr.Error()
		return vm.ToValue(jsResult)
	}

	if result == nil || result.Raw == nil {
		jsResult["output"] = nil
		return vm.ToValue(jsResult)
	}

	// For a single call, use first result/error
	raw := result.Raw
	if len(raw.Errors) > 0 && raw.Errors[0] != nil {
		jsResult["error"] = raw.Errors[0].Error()
	} else if len(raw.Results) > 0 &&
		raw.Results[0] != nil {
		jsResult["output"] = parseOutputJSON(
			raw.Results[0].Output,
		)
	} else {
		jsResult["output"] = nil
	}

	return vm.ToValue(jsResult)
}

// buildParallelResults converts a ToolChainResult from
// parallel calls into a JS array of results.
func buildParallelResults(
	vm *sobek.Runtime,
	reqs []map[string]any,
	result *gent.ToolChainResult,
	execErr error,
) sobek.Value {
	if execErr != nil {
		// Return error for all calls
		results := make([]any, len(reqs))
		for i, req := range reqs {
			name, _ := req["tool"].(string)
			results[i] = map[string]any{
				"name":  name,
				"error": execErr.Error(),
			}
		}
		return vm.ToValue(results)
	}

	if result == nil || result.Raw == nil {
		return vm.ToValue([]any{})
	}

	raw := result.Raw
	results := make([]any, len(raw.Calls))
	for i, call := range raw.Calls {
		entry := map[string]any{
			"name": call.Name,
		}
		if i < len(raw.Errors) && raw.Errors[i] != nil {
			entry["error"] = raw.Errors[i].Error()
		} else if i < len(raw.Results) &&
			raw.Results[i] != nil {
			entry["output"] = parseOutputJSON(
				raw.Results[i].Output,
			)
		} else {
			entry["output"] = nil
		}
		results[i] = entry
	}

	return vm.ToValue(results)
}

// parseOutputJSON attempts to parse a tool output as JSON.
// If the output is a string that looks like JSON, it
// parses it so the JS code gets a proper object. Otherwise
// returns the value as-is.
func parseOutputJSON(output any) any {
	str, ok := output.(string)
	if !ok {
		return output
	}

	// Try to parse as JSON object or array
	var parsed any
	if err := json.Unmarshal(
		[]byte(str), &parsed,
	); err != nil {
		return str
	}
	return parsed
}

// CollectedResults accumulates ToolChainResults from
// multiple tool calls within a code execution. It merges
// all results so the wrapper can build a single
// ToolChainResult.
type CollectedResults struct {
	AllCalls   []*gent.ToolCall
	AllResults []*gent.RawToolCallResult
	AllErrors  []error
	AllMedia   []gent.ContentPart
	TextParts  []string
}

// NewCollectedResults creates an empty collector.
func NewCollectedResults() *CollectedResults {
	return &CollectedResults{}
}

// Add merges a ToolChainResult into the collector.
func (c *CollectedResults) Add(
	result *gent.ToolChainResult,
) {
	if result == nil {
		return
	}
	if result.Raw != nil {
		c.AllCalls = append(
			c.AllCalls, result.Raw.Calls...,
		)
		c.AllResults = append(
			c.AllResults, result.Raw.Results...,
		)
		c.AllErrors = append(
			c.AllErrors, result.Raw.Errors...,
		)
	}
	c.AllMedia = append(c.AllMedia, result.Media...)
	if result.Text != "" {
		c.TextParts = append(
			c.TextParts, result.Text,
		)
	}
}

// BuildRaw returns the merged RawToolChainResult.
func (c *CollectedResults) BuildRaw() *gent.RawToolChainResult {
	return &gent.RawToolChainResult{
		Calls:   c.AllCalls,
		Results: c.AllResults,
		Errors:  c.AllErrors,
	}
}

// MakeToolCallFn creates a ToolCallFn that routes through
// the provided callback and collects results. This is
// used by JsToolChainWrapper to wire up the bridge.
func MakeToolCallFn(
	executeFn func(content string) (
		*gent.ToolChainResult, error,
	),
	collector *CollectedResults,
) ToolCallFn {
	return func(content string) (
		*gent.ToolChainResult, error,
	) {
		result, err := executeFn(content)
		if err != nil {
			return nil, fmt.Errorf(
				"tool execution error: %w", err,
			)
		}
		collector.Add(result)
		return result, nil
	}
}
