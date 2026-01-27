package toolchain

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/schema"
	"gopkg.in/yaml.v3"
)

// YAML implements [gent.ToolChain] for parsing YAML-formatted tool calls.
//
// YAML is the recommended toolchain for most use cases. It's more forgiving than JSON
// (no quotes required for strings, multiline strings are easier), and uses schema-aware
// parsing to preserve string types when the schema expects them.
//
// # Creating and Configuring
//
//	// Create with default "action" section name
//	tc := toolchain.NewYAML()
//
//	// Or customize the section name
//	tc := toolchain.NewYAML().WithSectionName("tool_call")
//
// # Registering Tools
//
//	// Register tools using method chaining
//	tc := toolchain.NewYAML().
//	    RegisterTool(searchTool).
//	    RegisterTool(calendarTool).
//	    RegisterTool(emailTool)
//
// # Expected Model Output Format
//
// Single tool call:
//
//	tool: search
//	args:
//	  query: weather in tokyo
//
// Multiple parallel tool calls (use YAML array):
//
//	- tool: search
//	  args:
//	    query: weather
//	- tool: calendar
//	  args:
//	    date: today
//
// # Schema-Aware Parsing
//
// YAML toolchain uses the tool's JSON Schema to guide type parsing. When a field is
// declared as "string" in the schema, the raw YAML value is preserved as a string
// even if YAML would normally parse it as another type.
//
// This prevents issues like "123" being parsed as an integer when you need a string:
//
//	schema.Object(map[string]*schema.Property{
//	    "phone": schema.String("Phone number"),  // "555-1234" stays as string
//	})
//
// # Using with Agent
//
//	agent := react.NewAgent(model).
//	    WithToolChain(toolchain.NewYAML().
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
type YAML struct {
	tools        []any
	toolMap      map[string]any
	schemaMap    map[string]*schema.Schema // compiled schemas for validation
	rawSchemaMap map[string]map[string]any // raw schemas for type-aware parsing
	sectionName  string
}

// NewYAML creates a new YAML toolchain with default section name "action".
func NewYAML() *YAML {
	return &YAML{
		tools:        make([]any, 0),
		toolMap:      make(map[string]any),
		schemaMap:    make(map[string]*schema.Schema),
		rawSchemaMap: make(map[string]map[string]any),
		sectionName:  "action",
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

// Guidance returns format instructions for how to call tools using YAML.
func (c *YAML) Guidance() string {
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
	sb.WriteString("\nFor strings with special characters (colons, quotes) or multiple lines, ")
	sb.WriteString("use double quotes:\n")
	sb.WriteString("- tool: send_email\n")
	sb.WriteString("  args:\n")
	sb.WriteString("    subject: \"Unsubscribe Confirmation: Newsletter\"\n")
	sb.WriteString("    body: \"You have been unsubscribed.\\n\\nYou will no longer receive emails.\"")
	return sb.String()
}

// AvailableToolsPrompt returns the tool catalog with parameter schemas for each registered tool.
func (c *YAML) AvailableToolsPrompt() string {
	var sb strings.Builder
	sb.WriteString("Available tools:\n")

	for _, tool := range c.tools {
		meta, err := GetToolMeta(tool)
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

// ParseSection parses the raw text content and returns []*gent.ToolCall.
// It uses schema-aware parsing to preserve string types where the schema expects strings.
func (c *YAML) ParseSection(execCtx *gent.ExecutionContext, content string) (any, error) {
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
func (c *YAML) doParse(content string) ([]*gent.ToolCall, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return []*gent.ToolCall{}, nil
	}

	// Parse into yaml.Node to preserve raw values
	var rootNode yaml.Node
	if err := yaml.Unmarshal([]byte(content), &rootNode); err != nil {
		return nil, fmt.Errorf("%w: %v", gent.ErrInvalidYAML, err)
	}

	// Root node is a document node, get its content
	if rootNode.Kind != yaml.DocumentNode || len(rootNode.Content) == 0 {
		return nil, fmt.Errorf("%w: unexpected YAML structure", gent.ErrInvalidYAML)
	}
	contentNode := rootNode.Content[0]

	var calls []*gent.ToolCall

	switch contentNode.Kind {
	case yaml.SequenceNode:
		// Array of tool calls
		for _, itemNode := range contentNode.Content {
			call, err := c.parseToolCallNode(itemNode)
			if err != nil {
				return nil, err
			}
			calls = append(calls, call)
		}
	case yaml.MappingNode:
		// Single tool call
		call, err := c.parseToolCallNode(contentNode)
		if err != nil {
			return nil, err
		}
		calls = append(calls, call)
	default:
		return nil, fmt.Errorf("%w: expected mapping or sequence", gent.ErrInvalidYAML)
	}

	return calls, nil
}

// parseToolCallNode parses a single tool call from a yaml.Node.
func (c *YAML) parseToolCallNode(node *yaml.Node) (*gent.ToolCall, error) {
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%w: tool call must be a mapping", gent.ErrInvalidYAML)
	}

	var toolName string
	var argsNode *yaml.Node

	// Extract tool name and args node
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]

		switch keyNode.Value {
		case "tool":
			toolName = valueNode.Value
		case "args":
			argsNode = valueNode
		}
	}

	if toolName == "" {
		return nil, gent.ErrMissingToolName
	}

	// Get the schema for this tool to guide parsing
	var propTypes map[string]string
	if rawSchema, ok := c.rawSchemaMap[toolName]; ok {
		propTypes = extractPropertyTypes(rawSchema)
	}

	// Parse args with schema awareness
	var args map[string]any
	if argsNode != nil {
		args = c.decodeArgsNode(argsNode, propTypes)
	}

	return &gent.ToolCall{Name: toolName, Args: args}, nil
}

// extractPropertyTypes extracts a map of property name -> type from a raw schema.
func extractPropertyTypes(rawSchema map[string]any) map[string]string {
	result := make(map[string]string)
	props, ok := rawSchema["properties"].(map[string]any)
	if !ok {
		return result
	}
	for name, propDef := range props {
		if propMap, ok := propDef.(map[string]any); ok {
			if typ, ok := propMap["type"].(string); ok {
				result[name] = typ
			}
		}
	}
	return result
}

// decodeArgsNode decodes args from a yaml.Node using schema type hints.
func (c *YAML) decodeArgsNode(node *yaml.Node, propTypes map[string]string) map[string]any {
	if node.Kind != yaml.MappingNode {
		return nil
	}

	result := make(map[string]any)
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]
		key := keyNode.Value

		// Check if schema expects a string for this property
		expectedType := propTypes[key]
		result[key] = c.decodeValueNode(valueNode, expectedType)
	}
	return result
}

// decodeValueNode decodes a single value node, using expectedType to guide decoding.
func (c *YAML) decodeValueNode(node *yaml.Node, expectedType string) any {
	// If schema expects a string, use the raw value regardless of YAML's auto-detection
	if expectedType == "string" && node.Kind == yaml.ScalarNode {
		return node.Value
	}

	// Otherwise, let YAML decode naturally
	var value any
	if err := node.Decode(&value); err != nil {
		// Fallback to raw value on decode error
		return node.Value
	}

	// Handle nested structures
	switch v := value.(type) {
	case map[string]any:
		// For nested objects, we don't have nested schema info here,
		// so just return as-is (could be enhanced to support nested schemas)
		return v
	case []any:
		return v
	default:
		return value
	}
}

// RegisterTool adds a tool to the chain. The tool must implement Tool[I, O].
// The tool's schema is compiled for validation when arguments are provided.
func (c *YAML) RegisterTool(tool any) gent.ToolChain {
	meta, err := GetToolMeta(tool)
	if err != nil {
		// Invalid tool, silently ignore (could log in the future)
		return c
	}
	c.tools = append(c.tools, tool)
	c.toolMap[meta.Name()] = tool

	// Store raw schema for type-aware parsing and compile for validation
	if rawSchema := meta.Schema(); rawSchema != nil {
		c.rawSchemaMap[meta.Name()] = rawSchema
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
func (c *YAML) Execute(
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
					execCtx.FireAfterToolCall(ctx, &gent.AfterToolCallEvent{
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
				execCtx.FireAfterToolCall(ctx, &gent.AfterToolCallEvent{
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
			// Successful tool call - reset consecutive error counters
			if execCtx != nil {
				execCtx.Stats().ResetCounter(gent.KeyToolCallsErrorConsecutive)
				execCtx.Stats().ResetCounter(gent.KeyToolCallsErrorConsecutiveFor + call.Name)
			}

			// Store raw result
			raw.Results[i] = &gent.RawToolCallResult{
				Name:   output.Name,
				Output: output.Text,
			}

			// Format output as YAML
			yamlData, marshalErr := yaml.Marshal(output.Text)
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
							{Name: "result", Content: strings.TrimSpace(string(yamlData))},
							{Name: "instructions", Content: output.Instructions},
						},
					})
				} else {
					sections = append(sections, gent.FormattedSection{
						Name:    call.Name,
						Content: strings.TrimSpace(string(yamlData)),
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
			execCtx.FireAfterToolCall(ctx, &gent.AfterToolCallEvent{
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

// Compile-time check that YAML implements gent.ToolChain.
var _ gent.ToolChain = (*YAML)(nil)
