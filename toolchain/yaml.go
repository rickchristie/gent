package toolchain

import (
	"context"
	"fmt"
	"strings"

	"github.com/rickchristie/gent"
	"gopkg.in/yaml.v3"
)

// YAML expects tool calls in YAML format.
//
// Single tool call:
//
//	tool: search
//	args:
//	  query: weather in tokyo
//
// Multiple tool calls:
//
//	- tool: search
//	  args:
//	    query: weather
//	- tool: calendar
//	  args:
//	    date: today
type YAML struct {
	tools       []any
	toolMap     map[string]any
	sectionName string
}

// NewYAML creates a new YAML toolchain with default section name "action".
func NewYAML() *YAML {
	return &YAML{
		tools:       make([]any, 0),
		toolMap:     make(map[string]any),
		sectionName: "action",
	}
}

// WithSectionName sets the section name for this tool chain.
func (c *YAML) WithSectionName(name string) *YAML {
	c.sectionName = name
	return c
}

// Name returns the section identifier.
func (c *YAML) Name() string {
	return c.sectionName
}

// Prompt returns instructions for how to write tool calls in this section.
func (c *YAML) Prompt() string {
	var sb strings.Builder
	sb.WriteString("Call tools using YAML format:\n")
	sb.WriteString("tool: tool_name\n")
	sb.WriteString("args:\n")
	sb.WriteString("  param: value\n")
	sb.WriteString("\nFor multiple parallel calls, use a list:\n")
	sb.WriteString("- tool: tool1\n")
	sb.WriteString("  args:\n")
	sb.WriteString("    param: value\n")
	sb.WriteString("- tool: tool2\n")
	sb.WriteString("  args:\n")
	sb.WriteString("    param: value\n")
	sb.WriteString("\nAvailable tools:\n")

	for _, tool := range c.tools {
		meta, err := gent.GetToolMeta(tool)
		if err != nil {
			continue
		}
		fmt.Fprintf(&sb, "\n- %s: %s\n", meta.Name(), meta.Description())
		if schema := meta.Schema(); schema != nil {
			schemaYAML, err := yaml.Marshal(schema)
			if err == nil {
				sb.WriteString("  Parameters:\n")
				// Indent the YAML schema
				lines := strings.Split(string(schemaYAML), "\n")
				for _, line := range lines {
					if line != "" {
						sb.WriteString("    ")
						sb.WriteString(line)
						sb.WriteString("\n")
					}
				}
			}
		}
	}

	return sb.String()
}

// rawToolCall is used for YAML unmarshalling.
type rawToolCall struct {
	Tool string         `yaml:"tool"`
	Args map[string]any `yaml:"args"`
}

// ParseSection parses the raw text content and returns []*gent.ToolCall.
func (c *YAML) ParseSection(content string) (any, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return []*gent.ToolCall{}, nil
	}

	var calls []*gent.ToolCall

	// Try parsing as array first
	if strings.HasPrefix(content, "-") {
		var rawCalls []rawToolCall
		if err := yaml.Unmarshal([]byte(content), &rawCalls); err != nil {
			return nil, fmt.Errorf("%w: %v", gent.ErrInvalidYAML, err)
		}
		for _, rc := range rawCalls {
			if rc.Tool == "" {
				return nil, gent.ErrMissingToolName
			}
			calls = append(calls, &gent.ToolCall{Name: rc.Tool, Args: rc.Args})
		}
	} else {
		// Try parsing as single object
		var rawCall rawToolCall
		if err := yaml.Unmarshal([]byte(content), &rawCall); err != nil {
			return nil, fmt.Errorf("%w: %v", gent.ErrInvalidYAML, err)
		}
		if rawCall.Tool == "" {
			return nil, gent.ErrMissingToolName
		}
		calls = append(calls, &gent.ToolCall{Name: rawCall.Tool, Args: rawCall.Args})
	}

	return calls, nil
}

// RegisterTool adds a tool to the chain. The tool must implement Tool[I, O].
func (c *YAML) RegisterTool(tool any) gent.ToolChain {
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
func (c *YAML) Execute(ctx context.Context, content string) (*gent.ToolChainResult, error) {
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
