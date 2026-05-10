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

// JSON implements [gent.ToolChain] for parsing JSON-formatted tool calls.
//
// Use JSON toolchain when working with models that are trained to output JSON,
// or when you need strict parsing without YAML's type inference. Note that JSON
// is more strict than YAML - all strings must be quoted, no multiline strings
// without escaping.
//
// For most use cases, [YAML] is recommended as it's more forgiving and works
// well with language models.
//
// # Creating and Configuring
//
//	// Create with default "action" section name
//	tc := toolchain.NewJSON()
//
//	// Or customize the section name
//	tc := toolchain.NewJSON().WithSectionName("tool_call")
//
// # Registering Tools
//
//	// Register tools using method chaining
//	tc := toolchain.NewJSON().
//	    RegisterTool(searchTool).
//	    RegisterTool(calendarTool).
//	    RegisterTool(emailTool)
//
// # Expected Model Output Format
//
// Single tool call:
//
//	{"tool": "search", "args": {"query": "weather"}}
//
// Multiple parallel tool calls (use JSON array):
//
//	[
//	  {"tool": "search", "args": {"query": "weather"}},
//	  {"tool": "calendar", "args": {"date": "today"}}
//	]
//
// # Using with Agent
//
//	agent := react.NewAgent(model).
//	    WithToolChain(toolchain.NewJSON().
//	        RegisterTool(searchTool).
//	        RegisterTool(calendarTool))
//
// # Integration with TextFormat
//
// The Execute method requires a TextFormat to format results. This is typically
// provided by the agent loop:
//
//	result, err := tc.Execute(execCtx, actionContent, textFormat)
//	// result.Text contains formatted observation to feed back to the model
type JSON struct {
	tools             []any
	toolMap           map[string]any
	schemaMap         map[string]*schema.Schema // compiled schemas
	sectionName       string
	printOutputSchema bool
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

// WithOutputSchema enables printing output schemas alongside input
// schemas in the tool catalog. When enabled, each tool's return type
// is shown under a "Returns:" section.
func (c *JSON) WithOutputSchema(enabled bool) *JSON {
	c.printOutputSchema = enabled
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
		if policy := meta.Policy(); policy != "" {
			sb.WriteString("  Policy: ")
			sb.WriteString(policy)
			sb.WriteString("\n")
		}
		if schema := meta.Schema(); schema != nil {
			schemaJSON, err := json.MarshalIndent(schema, "  ", "  ")
			if err == nil {
				sb.WriteString("  Parameters: ")
				sb.Write(schemaJSON)
				sb.WriteString("\n")
			}
		}
		if c.printOutputSchema {
			if os := meta.OutputSchema(); os != nil {
				outJSON, err := json.MarshalIndent(os, "  ", "  ")
				if err == nil {
					sb.WriteString("  Returns: ")
					sb.Write(outJSON)
					sb.WriteString("\n")
				}
			}
		}
	}

	return sb.String()
}

// ParseSection parses the raw text content and returns []*gent.ToolCall.
func (c *JSON) ParseSection(execCtx *gent.ExecutionContext, content string) (any, error) {
	result, err := c.doParse(content)
	if err != nil {
		// Publish parse error event (auto-updates stats)
		if execCtx != nil {
			execCtx.PublishParseError(gent.ParseErrorTypeToolchain, content, err)
		}
		return nil, err
	}

	// Successful parse - reset consecutive error gauge
	if execCtx != nil {
		execCtx.Stats().ResetGauge(gent.SGToolchainParseErrorConsecutive)
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
// Panics when the tool is nil, invalid, or duplicates an existing tool name.
func (c *JSON) RegisterTool(tool any) gent.ToolChain {
	meta, err := GetToolMeta(tool)
	if err != nil {
		panic(fmt.Sprintf("JSON.RegisterTool: invalid tool type: %v", err))
	}
	if _, exists := c.toolMap[meta.Name()]; exists {
		panic(fmt.Sprintf("JSON.RegisterTool: duplicate tool name %q", meta.Name()))
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
	results := make([]*gent.ToolCallResult, len(calls))

	for i, call := range calls {
		results[i] = &gent.ToolCallResult{
			Name: call.Name,
			Args: call.Args,
			Hash: ToolCallResultHash(call.Name, call.Args),
		}

		tool, ok := c.toolMap[call.Name]
		if !ok {
			toolErr := fmt.Errorf("%w: %s", gent.ErrUnknownTool, call.Name)
			results[i].Error = toolErr
			results[i].Text = formatSectionText(textFormat, call.Name, fmt.Sprintf(
				"Error: unknown tool %q. "+
					"Review the available "+
					"tools section for valid "+
					"tool names.",
				call.Name,
			))
			publishFailedToolAttempt(execCtx, call.Name, call.Args, toolErr)
			continue
		}

		// Validate args against schema before transformation
		if compiledSchema, hasSchema := c.schemaMap[call.Name]; hasSchema {
			if validationErr := compiledSchema.Validate(call.Args); validationErr != nil {
				results[i].Error = validationErr
				results[i].Text = formatSectionText(
					textFormat, call.Name,
					fmt.Sprintf("Error: %v", validationErr),
				)
				publishFailedToolAttempt(execCtx, call.Name, call.Args, validationErr)
				continue
			}
		}

		// Transform raw args to typed input
		typedInput, transformErr := TransformArgsReflect(tool, call.Args)
		if transformErr != nil {
			results[i].Error = transformErr
			results[i].Text = formatSectionText(
				textFormat, call.Name,
				fmt.Sprintf("Error: %v", transformErr),
			)
			publishFailedToolAttempt(execCtx, call.Name, call.Args, transformErr)
			continue
		}
		results[i].Input = typedInput

		// Publish BeforeToolCall event (may modify args)
		inputToUse := typedInput
		var toolCallId string
		var toolCallSource string
		if execCtx != nil {
			beforeEvent := execCtx.PublishBeforeToolCall(call.Name, typedInput)
			inputToUse = beforeEvent.Args
			toolCallId = beforeEvent.ToolCallId
			toolCallSource = beforeEvent.Source
		}

		startTime := time.Now()
		output, err := CallToolWithTypedInputReflect(ctx, tool, inputToUse)
		duration := time.Since(startTime)

		if err != nil {
			results[i].Error = err
			results[i].Text = formatSectionText(
				textFormat, call.Name,
				fmt.Sprintf("Error: %v", err),
			)
		} else {
			// Successful tool call - reset consecutive error gauges
			if execCtx != nil {
				execCtx.Stats().ResetGauge(gent.SGToolCallsErrorConsecutive)
				execCtx.Stats().ResetGauge(
					gent.SGToolCallsErrorConsecutiveFor + gent.StatKey(call.Name),
				)
			}

			results[i].Output = output.Text

			// Format output. String outputs are passed through as-is
			// (no JSON wrapping) because they may contain Markdown,
			// prompts, or other structured text that should not be
			// quoted. Non-string outputs are JSON-marshalled.
			outputText, marshalErr := formatToolOutputJSON(output.Text)
			if marshalErr != nil {
				results[i].Text = formatSectionText(
					textFormat, call.Name,
					"error: failed to marshal output",
				)
			} else {
				if output.Instructions != "" {
					results[i].Text = textFormat.FormatSections(
						[]gent.FormattedSection{{
							Name: call.Name,
							Children: []gent.FormattedSection{
								{Name: "result", Content: outputText},
								{Name: "instructions", Content: output.Instructions},
							},
						}},
					)
				} else {
					results[i].Text = formatSectionText(
						textFormat, call.Name, outputText,
					)
				}
			}

			// Store media from tool result
			if len(output.Media) > 0 {
				results[i].Media = output.Media
			}
		}

		// Publish AfterToolCall event
		var outputVal any
		if output != nil {
			outputVal = output.Text
		}
		if execCtx != nil {
			execCtx.PublishAfterToolCall(
				call.Name, inputToUse, outputVal, duration, err,
				gent.WithToolCallId(toolCallId),
				gent.WithToolCallSource(toolCallSource),
			)
		}
	}

	return &gent.ToolChainResult{Results: results}, nil
}

// DeduplicateSummary returns an abbreviated observation text
// for a tool call whose output duplicates an earlier call.
func (c *JSON) DeduplicateSummary(
	result *gent.ToolCallResult,
) string {
	tool, ok := c.toolMap[result.Name]
	if !ok {
		return ""
	}
	return CallDeduplicateSummaryReflect(
		tool, result.Input, result.Output,
	)
}

// GetToolSchema returns the compiled schema for the
// named tool, or nil if not found.
func (c *JSON) GetToolSchema(
	name string,
) *schema.Schema {
	return c.schemaMap[name]
}

// Compile-time check that JSON implements gent.ToolChain.
var _ gent.ToolChain = (*JSON)(nil)
