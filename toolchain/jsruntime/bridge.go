package jsruntime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/grafana/sobek"
	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/schema"
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
//
// When source and schemaFn are provided, schema validation
// errors are enhanced with code context and field
// descriptions.
func RegisterToolBridge(
	rt *Runtime,
	callFn ToolCallFn,
	source string,
	schemaFn SchemaLookupFn,
) {
	vm := rt.VM()
	rt.RegisterObject("tool", map[string]func(
		sobek.FunctionCall,
	) sobek.Value{
		"call": func(
			call sobek.FunctionCall,
		) sobek.Value {
			return toolCall(
				vm, callFn, call,
				source, schemaFn,
			)
		},
		"parallelCall": func(
			call sobek.FunctionCall,
		) sobek.Value {
			return toolParallelCall(
				vm, callFn, call,
				source, schemaFn,
			)
		},
	})
}

// toolCall implements tool.call({tool, args}).
func toolCall(
	vm *sobek.Runtime,
	callFn ToolCallFn,
	call sobek.FunctionCall,
	source string,
	schemaFn SchemaLookupFn,
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
		return buildMissingToolError(
			vm, source,
		)
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
		source, schemaFn, req,
	)
}

// toolParallelCall implements
// tool.parallelCall([{tool, args}, ...]).
func toolParallelCall(
	vm *sobek.Runtime,
	callFn ToolCallFn,
	call sobek.FunctionCall,
	source string,
	schemaFn SchemaLookupFn,
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
		source, schemaFn,
	)
}

// buildSingleResult converts a ToolChainResult into a JS
// value: {name, output} or {name, error}.
func buildSingleResult(
	vm *sobek.Runtime,
	toolName string,
	result *gent.ToolChainResult,
	execErr error,
	source string,
	schemaFn SchemaLookupFn,
	req map[string]any,
) sobek.Value {
	jsResult := make(map[string]any)
	jsResult["name"] = toolName

	if execErr != nil {
		jsResult["error"] = execErr.Error()
		return vm.ToValue(jsResult)
	}

	if result == nil || len(result.Results) == 0 {
		jsResult["output"] = nil
		return vm.ToValue(jsResult)
	}

	// For a single call, use first result
	first := result.Results[0]
	if first != nil && first.Error != nil {
		jsResult["error"] = enhanceSchemaError(
			vm, first.Error, toolName,
			source, schemaFn, req,
		)
	} else if first != nil {
		jsResult["output"] = normalizeOutput(
			first.Output,
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
	source string,
	schemaFn SchemaLookupFn,
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

	if result == nil || len(result.Results) == 0 {
		return vm.ToValue([]any{})
	}

	results := make([]any, len(result.Results))
	for i, r := range result.Results {
		entry := map[string]any{
			"name": r.Name,
		}
		if r.Error != nil {
			req := map[string]any{}
			if i < len(reqs) {
				req = reqs[i]
			}
			entry["error"] = enhanceSchemaError(
				vm, r.Error, r.Name,
				source, schemaFn, req,
			)
		} else if r.Output != nil {
			entry["output"] = normalizeOutput(
				r.Output,
			)
		} else {
			entry["output"] = nil
		}
		results[i] = entry
	}

	return vm.ToValue(results)
}

// writeCallContext writes the "tool.call() error at
// line N:" header and source context snippet to sb.
func writeCallContext(
	sb *strings.Builder,
	vm *sobek.Runtime,
	source string,
	annotation string,
) {
	if source == "" {
		return
	}
	frames := vm.CaptureCallStack(10, nil)
	for _, frame := range frames {
		pos := frame.Position()
		if pos.Line > 0 {
			fmt.Fprintf(sb,
				"tool.call() error at "+
					"line %d:\n\n",
				pos.Line,
			)
			ctx := extractSourceContext(
				source, pos.Line,
				pos.Column,
				annotation,
				2, 2,
			)
			if ctx != "" {
				sb.WriteString(ctx)
				sb.WriteString("\n")
			}
			break
		}
	}
}

// enhanceSchemaError checks if err is a schema validation
// error and, if so, replaces the raw error with an
// LLM-friendly message including code context and field
// descriptions. Falls back to err.Error() for non-schema
// errors or when schemaFn is nil.
func enhanceSchemaError(
	vm *sobek.Runtime,
	err error,
	toolName string,
	source string,
	schemaFn SchemaLookupFn,
	req map[string]any,
) string {
	var valErr *schema.ValidationError
	if !errors.As(err, &valErr) {
		return err.Error()
	}
	if schemaFn == nil {
		return err.Error()
	}
	sch := schemaFn(toolName)
	if sch == nil {
		return err.Error()
	}

	args, _ := req["args"].(map[string]any)
	llmMsg := sch.FormatForLLM(toolName, args)
	if llmMsg == "" {
		return err.Error()
	}

	var sb strings.Builder
	writeCallContext(
		&sb, vm, source, "schema validation error",
	)
	sb.WriteString(llmMsg)
	WriteExampleCall(&sb, toolName, sch)
	return sb.String()
}

// WriteExampleCall appends a tool.call() example to sb
// using the schema's ExampleObject. Writes nothing if the
// schema has no properties.
func WriteExampleCall(
	sb *strings.Builder,
	toolName string,
	sch *schema.Schema,
) {
	example := sch.ExampleObject()
	if example == nil {
		return
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("    ", "  ")
	if err := enc.Encode(example); err != nil {
		return
	}
	argsJSON := bytes.TrimRight(
		buf.Bytes(), "\n",
	)
	sb.WriteString("Example:\n")
	sb.WriteString("  tool.call({\n")
	sb.WriteString("    tool: \"")
	sb.WriteString(toolName)
	sb.WriteString("\",\n")
	sb.WriteString("    args: ")
	sb.Write(argsJSON)
	sb.WriteString("\n  });\n")
}

// buildMissingToolError returns a JS error result with
// code context when tool.call() is missing the required
// 'tool' field.
func buildMissingToolError(
	vm *sobek.Runtime,
	source string,
) sobek.Value {
	var sb strings.Builder
	writeCallContext(
		&sb, vm, source, "missing required field",
	)
	sb.WriteString(
		"Invalid tool.call() request.\n" +
			"Errors:\n" +
			"  - missing required 'tool' field\n" +
			"Expected format:\n" +
			"  tool.call({tool: \"tool_name\"," +
			" args: {...}})\n" +
			"\nIMPORTANT: Use EXACT argument " +
			"names and types from the tool " +
			"schema.\n" +
			"Fix ALL errors above before " +
			"re-submitting your code.",
	)
	jsResult := map[string]any{
		"name":  "",
		"error": sb.String(),
	}
	return vm.ToValue(jsResult)
}

// normalizeOutput converts a tool output to a JS-friendly
// value. If the output is a string that looks like JSON, it
// parses it into a map/slice. If the output is a Go struct
// or other typed value, it round-trips through JSON to
// respect json tags (e.g., json:"snake_case") so the JS
// code sees the same field names as the schema.
func normalizeOutput(output any) any {
	if output == nil {
		return nil
	}

	str, ok := output.(string)
	if ok {
		// Try to parse as JSON object or array
		var parsed any
		if err := json.Unmarshal(
			[]byte(str), &parsed,
		); err != nil {
			return str
		}
		return parsed
	}

	// Non-string output (Go struct, pointer, etc.):
	// round-trip through JSON so json tags are respected.
	data, err := json.Marshal(output)
	if err != nil {
		return output
	}
	var normalized any
	if err := json.Unmarshal(data, &normalized); err != nil {
		return output
	}
	return normalized
}

// ToolCallEntry represents a single tool call with its
// result (which includes Name, Args, Output, and Error).
type ToolCallEntry struct {
	Result *gent.ToolCallResult
}

// ToolCallGroup represents one or more tool calls from a
// single tool.call() or tool.parallelCall() invocation.
// Sequential calls produce groups with one entry; parallel
// calls produce groups with multiple entries.
type ToolCallGroup struct {
	Entries []ToolCallEntry
}

// CollectedResults accumulates ToolChainResults from
// multiple tool calls within a code execution. It merges
// all results so the wrapper can build a single
// ToolChainResult.
type CollectedResults struct {
	AllResults []*gent.ToolCallResult
	Groups     []ToolCallGroup
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
	c.AllResults = append(
		c.AllResults, result.Results...,
	)

	// Build group for sequential/parallel tracking
	group := ToolCallGroup{}
	for _, r := range result.Results {
		group.Entries = append(
			group.Entries,
			ToolCallEntry{Result: r},
		)
	}
	if len(group.Entries) > 0 {
		c.Groups = append(c.Groups, group)
	}
}

// HasSchemaErrors returns true if any collected error
// is a schema.ValidationError.
func (c *CollectedResults) HasSchemaErrors() bool {
	for _, r := range c.AllResults {
		if r == nil || r.Error == nil {
			continue
		}
		var valErr *schema.ValidationError
		if errors.As(r.Error, &valErr) {
			return true
		}
	}
	return false
}

// BuildResult returns the merged ToolChainResult.
func (c *CollectedResults) BuildResult() *gent.ToolChainResult {
	return &gent.ToolChainResult{
		Results: c.AllResults,
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
