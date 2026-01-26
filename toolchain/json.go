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
// The textFormat parameter is used to format the results - it must not be nil.
//
// When execCtx is provided, each tool call is automatically traced.
// If execCtx is nil, tools are executed without tracing using context.Background().
//
// Panics if textFormat is nil.
func (c *JSON) Execute(
	execCtx *gent.ExecutionContext,
	content string,
	textFormat gent.TextFormat,
) (*gent.ToolChainResult, error) {
	if textFormat == nil {
		panic("textFormat must not be nil")
	}

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
	raw := &gent.RawToolChainResult{
		Calls:   calls,
		Results: make([]*gent.RawToolCallResult, len(calls)),
		Errors:  make([]error, len(calls)),
	}

	// Collect formatted sections and media
	var sections []gent.FormattedSection
	var allMedia []gent.ContentPart

	for i, call := range calls {
		tool, ok := c.toolMap[call.Name]
		if !ok {
			raw.Errors[i] = fmt.Errorf("%w: %s", gent.ErrUnknownTool, call.Name)
			// Add error as a section
			sections = append(sections, gent.FormattedSection{
				Name:    call.Name,
				Content: fmt.Sprintf("Error: %v", raw.Errors[i]),
			})
			// Trace the failed call if execCtx is provided
			if execCtx != nil {
				execCtx.Trace(gent.ToolCallTrace{
					ToolName: call.Name,
					Input:    call.Args,
					Error:    raw.Errors[i],
				})
			}
			continue
		}

		// Validate args against schema before transformation
		if compiledSchema, hasSchema := c.schemaMap[call.Name]; hasSchema {
			if validationErr := compiledSchema.Validate(call.Args); validationErr != nil {
				raw.Errors[i] = validationErr
				sections = append(sections, gent.FormattedSection{
					Name:    call.Name,
					Content: fmt.Sprintf("Error: %v", validationErr),
				})

				if execCtx != nil {
					// Fire AfterToolCall with validation error
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
			raw.Errors[i] = transformErr
			sections = append(sections, gent.FormattedSection{
				Name:    call.Name,
				Content: fmt.Sprintf("Error: %v", transformErr),
			})
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
			raw.Errors[i] = err
			sections = append(sections, gent.FormattedSection{
				Name:    call.Name,
				Content: fmt.Sprintf("Error: %v", err),
			})
		} else {
			// Store raw result
			raw.Results[i] = &gent.RawToolCallResult{
				Name:   output.Name,
				Output: output.Text,
			}

			// Format output as JSON
			jsonData, marshalErr := json.Marshal(output.Text)
			if marshalErr != nil {
				sections = append(sections, gent.FormattedSection{
					Name:    call.Name,
					Content: "error: failed to marshal output",
				})
			} else {
				// If instructions present, create nested sections as children
				if output.Instructions != "" {
					sections = append(sections, gent.FormattedSection{
						Name: call.Name,
						Children: []gent.FormattedSection{
							{Name: "result", Content: string(jsonData)},
							{Name: "instructions", Content: output.Instructions},
						},
					})
				} else {
					sections = append(sections, gent.FormattedSection{
						Name:    call.Name,
						Content: string(jsonData),
					})
				}
			}

			// Collect media from tool result
			if len(output.Media) > 0 {
				allMedia = append(allMedia, output.Media...)
			}
		}

		// Fire AfterToolCall hook with typed input
		var outputVal any
		if output != nil {
			outputVal = output.Text
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

	// Build formatted text using TextFormat
	return &gent.ToolChainResult{
		Text:  textFormat.FormatSections(sections),
		Media: allMedia,
		Raw:   raw,
	}, nil
}

// Compile-time check that JSON implements gent.ToolChain.
var _ gent.ToolChain = (*JSON)(nil)
