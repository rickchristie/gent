package toolchain

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/schema"
	"github.com/tmc/langchaingo/llms"
)

// JSON expects tool calls in JSON format.
//
// Single tool call:
//
//	{"tool": "search", "args": {"query": "weather"}}
//
// Multiple tool calls (parallel):
//
//	[
//	  {"tool": "search", "args": {"query": "weather"}},
//	  {"tool": "calendar", "args": {"date": "today"}}
//	]
type JSON struct {
	tools       []any
	toolMap     map[string]any
	schemaMap   map[string]*schema.Schema // compiled schemas for validation
	sectionName string
}

// NewJSON creates a new JSON toolchain with default section name "action".
func NewJSON() *JSON {
	return &JSON{
		tools:       make([]any, 0),
		toolMap:     make(map[string]any),
		schemaMap:   make(map[string]*schema.Schema),
		sectionName: "action",
	}
}

// WithSectionName sets the section name for this tool chain.
func (c *JSON) WithSectionName(name string) *JSON {
	c.sectionName = name
	return c
}

// Name returns the section identifier.
func (c *JSON) Name() string {
	return c.sectionName
}

// Guidance returns format instructions for how to call tools using JSON.
func (c *JSON) Guidance() string {
	var sb strings.Builder
	sb.WriteString("Call tools using JSON format:\n")
	sb.WriteString(`{"tool": "tool_name", "args": {...}}`)
	sb.WriteString("\n\nFor multiple parallel calls, use an array:\n")
	sb.WriteString(`[{"tool": "tool1", "args": {...}}, {"tool": "tool2", "args": {...}}]`)
	return sb.String()
}

// AvailableToolsPrompt returns the tool catalog with parameter schemas for each registered tool.
func (c *JSON) AvailableToolsPrompt() string {
	var sb strings.Builder
	sb.WriteString("Available tools:\n")

	for _, tool := range c.tools {
		meta, err := GetToolMeta(tool)
		if err != nil {
			continue
		}
		fmt.Fprintf(&sb, "\n- %s: %s\n", meta.Name(), meta.Description())
		if schema := meta.Schema(); schema != nil {
			schemaJSON, err := json.MarshalIndent(schema, "  ", "  ")
			if err == nil {
				sb.WriteString("  Parameters: ")
				sb.Write(schemaJSON)
				sb.WriteString("\n")
			}
		}
	}

	return sb.String()
}

// ParseSection parses the raw text content and returns []*gent.ToolCall.
func (c *JSON) ParseSection(execCtx *gent.ExecutionContext, content string) (any, error) {
	result, err := c.doParse(content)
	if err != nil {
		// Trace parse error (auto-updates stats)
		if execCtx != nil {
			execCtx.Trace(gent.ParseErrorTrace{
				ErrorType:  "toolchain",
				RawContent: content,
				Error:      err,
			})
		}
		return nil, err
	}

	// Successful parse - reset consecutive error counter
	if execCtx != nil {
		execCtx.Stats().ResetCounter(gent.KeyToolchainParseErrorConsecutive)
	}

	return result, nil
}

// doParse performs the actual parsing logic.
func (c *JSON) doParse(content string) ([]*gent.ToolCall, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return []*gent.ToolCall{}, nil
	}

	var calls []*gent.ToolCall

	// Try parsing as array first
	if strings.HasPrefix(content, "[") {
		var rawCalls []struct {
			Tool string         `json:"tool"`
			Args map[string]any `json:"args"`
		}
		if err := json.Unmarshal([]byte(content), &rawCalls); err != nil {
			return nil, fmt.Errorf("%w: %v", gent.ErrInvalidJSON, err)
		}
		for _, rc := range rawCalls {
			if rc.Tool == "" {
				return nil, gent.ErrMissingToolName
			}
			calls = append(calls, &gent.ToolCall{Name: rc.Tool, Args: rc.Args})
		}
	} else {
		// Try parsing as single object
		var rawCall struct {
			Tool string         `json:"tool"`
			Args map[string]any `json:"args"`
		}
		if err := json.Unmarshal([]byte(content), &rawCall); err != nil {
			return nil, fmt.Errorf("%w: %v", gent.ErrInvalidJSON, err)
		}
		if rawCall.Tool == "" {
			return nil, gent.ErrMissingToolName
		}
		calls = append(calls, &gent.ToolCall{Name: rawCall.Tool, Args: rawCall.Args})
	}

	return calls, nil
}

// RegisterTool adds a tool to the chain. The tool must implement Tool[I, O].
// The tool's schema is compiled for validation when arguments are provided.
func (c *JSON) RegisterTool(tool any) gent.ToolChain {
	meta, err := GetToolMeta(tool)
	if err != nil {
		// Invalid tool, silently ignore (could log in the future)
		return c
	}
	c.tools = append(c.tools, tool)
	c.toolMap[meta.Name()] = tool

	// Compile schema for validation
	if rawSchema := meta.Schema(); rawSchema != nil {
		compiled, err := schema.Compile(rawSchema)
		if err == nil && compiled != nil {
			c.schemaMap[meta.Name()] = compiled
		}
	}

	return c
}

// Execute parses tool calls from content and executes them.
// When execCtx is provided, each tool call is automatically traced.
// If execCtx is nil, tools are executed without tracing using context.Background().
func (c *JSON) Execute(
	execCtx *gent.ExecutionContext,
	content string,
) (*gent.ToolChainResult, error) {
	var ctx context.Context
	if execCtx != nil {
		ctx = execCtx.Context()
	} else {
		ctx = context.Background()
	}
	// ParseSection handles tracing of parse errors
	parsed, err := c.ParseSection(execCtx, content)
	if err != nil {
		return nil, err
	}

	calls := parsed.([]*gent.ToolCall)
	result := &gent.ToolChainResult{
		Calls:   calls,
		Results: make([]*gent.ToolCallResult, len(calls)),
		Errors:  make([]error, len(calls)),
	}

	for i, call := range calls {
		tool, ok := c.toolMap[call.Name]
		if !ok {
			result.Errors[i] = fmt.Errorf("%w: %s", gent.ErrUnknownTool, call.Name)
			// Trace the failed call if execCtx is provided
			if execCtx != nil {
				execCtx.Trace(gent.ToolCallTrace{
					ToolName: call.Name,
					Input:    call.Args,
					Error:    result.Errors[i],
				})
			}
			continue
		}

		// Validate args against schema before transformation
		if compiledSchema, hasSchema := c.schemaMap[call.Name]; hasSchema {
			if validationErr := compiledSchema.Validate(call.Args); validationErr != nil {
				result.Errors[i] = validationErr

				if execCtx != nil {
					// Fire AfterToolCall with validation error (Args is nil since we
					// couldn't transform)
					execCtx.FireAfterToolCall(ctx, gent.AfterToolCallEvent{
						ToolName: call.Name,
						Args:     nil,
						Error:    validationErr,
					})

					// Trace the validation failure
					execCtx.Trace(gent.ToolCallTrace{
						ToolName: call.Name,
						Input:    call.Args,
						Error:    validationErr,
					})
				}
				continue
			}
		}

		// Transform raw args to typed input
		typedInput, transformErr := TransformArgsReflect(tool, call.Args)
		if transformErr != nil {
			result.Errors[i] = transformErr
			if execCtx != nil {
				execCtx.FireAfterToolCall(ctx, gent.AfterToolCallEvent{
					ToolName: call.Name,
					Args:     nil,
					Error:    transformErr,
				})
				execCtx.Trace(gent.ToolCallTrace{
					ToolName: call.Name,
					Input:    call.Args,
					Error:    transformErr,
				})
			}
			continue
		}

		// Fire BeforeToolCall hook with typed input (may modify args)
		beforeEvent := &gent.BeforeToolCallEvent{
			ToolName: call.Name,
			Args:     typedInput,
		}
		if execCtx != nil {
			execCtx.FireBeforeToolCall(ctx, beforeEvent)
		}

		// Use potentially modified typed input
		inputToUse := beforeEvent.Args

		startTime := time.Now()
		output, err := CallToolWithTypedInputReflect(ctx, tool, inputToUse)
		duration := time.Since(startTime)

		if err != nil {
			result.Errors[i] = err
		} else {
			// Format output as JSON (toolchain owns formatting responsibility)
			result.Results[i] = c.formatOutput(output)
		}

		// Fire AfterToolCall hook with typed input
		var outputVal any
		if output != nil {
			outputVal = output.Output
		}
		if execCtx != nil {
			execCtx.FireAfterToolCall(ctx, gent.AfterToolCallEvent{
				ToolName: call.Name,
				Args:     inputToUse,
				Output:   outputVal,
				Duration: duration,
				Error:    err,
			})

			// Automatic tracing
			execCtx.Trace(gent.ToolCallTrace{
				ToolName: call.Name,
				Input:    inputToUse,
				Output:   outputVal,
				Duration: duration,
				Error:    err,
			})
		}
	}

	return result, nil
}

// formatOutput formats the tool output as JSON for LLM consumption.
func (c *JSON) formatOutput(output *gent.ToolCallResult) *gent.ToolCallResult {
	if output == nil {
		return nil
	}

	data, err := json.Marshal(output.Output)
	if err != nil {
		return &gent.ToolCallResult{
			Name:   output.Name,
			Output: output.Output,
			Result: []gent.ContentPart{llms.TextContent{Text: "error: failed to marshal output"}},
		}
	}

	return &gent.ToolCallResult{
		Name:   output.Name,
		Output: output.Output,
		Result: []gent.ContentPart{llms.TextContent{Text: string(data)}},
	}
}

// Compile-time check that JSON implements gent.ToolChain.
var _ gent.ToolChain = (*JSON)(nil)
