package toolchain

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rickchristie/gent"
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
	sectionName string
}

// NewJSON creates a new JSON toolchain with default section name "action".
func NewJSON() *JSON {
	return &JSON{
		tools:       make([]any, 0),
		toolMap:     make(map[string]any),
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
		meta, err := gent.GetToolMeta(tool)
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
func (c *JSON) RegisterTool(tool any) gent.ToolChain {
	meta, err := gent.GetToolMeta(tool)
	if err != nil {
		// Invalid tool, silently ignore (could log in the future)
		return c
	}
	c.tools = append(c.tools, tool)
	c.toolMap[meta.Name()] = tool
	return c
}

// Execute parses tool calls from content and executes them.
func (c *JSON) Execute(ctx context.Context, content string) (*gent.ToolChainResult, error) {
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
			continue
		}

		output, err := gent.CallToolReflect(ctx, tool, call.Args)
		if err != nil {
			result.Errors[i] = err
		} else {
			result.Results[i] = output
		}
	}

	return result, nil
}
