package toolchain

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/schema"
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

// Prompt returns instructions for how to write tool calls in this section.
func (c *JSON) Prompt() string {
	var sb strings.Builder
	sb.WriteString("Call tools using JSON format:\n")
	sb.WriteString(`{"tool": "tool_name", "args": {...}}`)
	sb.WriteString("\n\nFor multiple parallel calls, use an array:\n")
	sb.WriteString(`[{"tool": "tool1", "args": {...}}, {"tool": "tool2", "args": {...}}]`)
	sb.WriteString("\n\nAvailable tools:\n")

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
func (c *JSON) ParseSection(content string) (any, error) {
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
func (c *JSON) Execute(
	ctx context.Context,
	execCtx *gent.ExecutionContext,
	content string,
) (*gent.ToolChainResult, error) {
	parsed, err := c.ParseSection(content)
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

		// Fire BeforeToolCall hook (may modify args or abort)
		beforeEvent := &gent.BeforeToolCallEvent{
			ToolName: call.Name,
			Args:     call.Args,
		}
		if execCtx != nil {
			if hookErr := execCtx.FireBeforeToolCall(ctx, beforeEvent); hookErr != nil {
				// Hook aborted the tool call
				result.Errors[i] = hookErr

				// Fire AfterToolCall with the abort error
				execCtx.FireAfterToolCall(ctx, gent.AfterToolCallEvent{
					ToolName: call.Name,
					Args:     beforeEvent.Args,
					Error:    hookErr,
				})

				// Trace the aborted call
				execCtx.Trace(gent.ToolCallTrace{
					ToolName: call.Name,
					Input:    beforeEvent.Args,
					Error:    hookErr,
				})
				continue
			}
			// Use potentially modified args
			call.Args = beforeEvent.Args
		}

		// Validate args against schema
		if compiledSchema, hasSchema := c.schemaMap[call.Name]; hasSchema {
			if validationErr := compiledSchema.Validate(call.Args); validationErr != nil {
				result.Errors[i] = validationErr

				if execCtx != nil {
					// Fire AfterToolCall with validation error
					execCtx.FireAfterToolCall(ctx, gent.AfterToolCallEvent{
						ToolName: call.Name,
						Args:     call.Args,
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

		startTime := time.Now()
		output, err := CallToolReflect(ctx, tool, call.Args)
		duration := time.Since(startTime)

		if err != nil {
			result.Errors[i] = err
		} else {
			result.Results[i] = output
		}

		// Fire AfterToolCall hook
		var outputVal any
		if output != nil {
			outputVal = output.Output
		}
		if execCtx != nil {
			execCtx.FireAfterToolCall(ctx, gent.AfterToolCallEvent{
				ToolName: call.Name,
				Args:     call.Args,
				Output:   outputVal,
				Duration: duration,
				Error:    err,
			})

			// Automatic tracing
			execCtx.Trace(gent.ToolCallTrace{
				ToolName: call.Name,
				Input:    call.Args,
				Output:   outputVal,
				Duration: duration,
				Error:    err,
			})
		}
	}

	return result, nil
}

// Compile-time check that JSON implements gent.ToolChain.
var _ gent.ToolChain = (*JSON)(nil)
