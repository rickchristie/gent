package toolchain

import (
	"fmt"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/schema"
)

// Registry binds typed tools to native calls. Configure it before sharing it between runs.
type Registry struct {
	tools             []any
	toolMap           map[string]any
	schemaMap         map[string]*schema.Schema // compiled schemas
	recovery          map[string]gent.RecoveryMessageProvider
	printOutputSchema bool
}

// New creates a registry with input and output schemas in its native definitions.
func New() *Registry {
	return &Registry{
		tools:             make([]any, 0),
		toolMap:           make(map[string]any),
		schemaMap:         make(map[string]*schema.Schema),
		printOutputSchema: true,
		recovery:          make(map[string]gent.RecoveryMessageProvider),
	}
}

// WithOutputSchema enables printing output schemas alongside input
// schemas in the tool catalog. When enabled, each tool's return type
// is shown under a "Returns:" section.
func (c *Registry) WithOutputSchema(enabled bool) *Registry {
	c.printOutputSchema = enabled
	return c
}

// RegisterTool adds a tool to the chain. The tool must implement Tool[I, O].
// The tool's schema is compiled for validation when arguments are provided.
// Panics when the tool is nil, invalid, or duplicates an existing tool name.
func (c *Registry) RegisterTool(tool any, options ...gent.ToolOption) gent.ToolChain {
	meta, err := GetToolMeta(tool)
	if err != nil {
		panic(fmt.Sprintf("Registry.RegisterTool: invalid tool type: %v", err))
	}
	if _, exists := c.toolMap[meta.Name()]; exists {
		panic(fmt.Sprintf("Registry.RegisterTool: duplicate tool name %q", meta.Name()))
	}
	config := gent.ToolOptions{}
	for _, option := range options {
		option(&config)
	}
	c.recovery[meta.Name()] = config.RecoveryMessage
	c.tools = append(c.tools, tool)
	c.toolMap[meta.Name()] = tool

	// Compile schema for validation
	if rawSchema := meta.Schema(); rawSchema != nil {
		compiled, err := schema.Compile(rawSchema)
		if err != nil {
			panic(fmt.Errorf("register tool %q schema: %w", meta.Name(), err))
		}
		if compiled != nil {
			c.schemaMap[meta.Name()] = compiled
		}
	}

	return c
}

func (c *Registry) executeParsed(
	execCtx *gent.ExecutionContext, calls []*parsedCall, textFormat gent.TextFormat,
) (*gent.ToolChainResult, error) {
	if textFormat == nil {
		panic("textFormat must not be nil")
	}
	ctx := execCtx.Context()
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
				"Error: unknown tool %q. Use a function from the current native tool definitions.",
				call.Name,
			))
			publishFailedToolAttempt(execCtx, call, toolErr, results[i])
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
				publishFailedToolAttempt(execCtx, call, validationErr, results[i])
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
			publishFailedToolAttempt(execCtx, call, transformErr, results[i])
			continue
		}
		results[i].Input = typedInput

		// Publish BeforeToolCall event (may modify args)
		inputToUse := typedInput
		var toolCallId string
		var toolCallSource string

		beforeEvent := execCtx.PublishBeforeToolCall(call.Name, typedInput,
			gent.WithToolCallId(call.Id))
		inputToUse = beforeEvent.Args
		toolCallId = beforeEvent.ToolCallId
		toolCallSource = beforeEvent.Source

		results[i].Input = inputToUse
		results[i].ToolCallId = toolCallId

		if err := execCtx.RecordingError(); err != nil {
			return nil, err
		}
		if err := execCtx.Context().Err(); err != nil {
			return nil, err
		}
		ctx = execCtx.ToolContext(toolCallId)
		results[i].AttemptId = execCtx.AttemptId()

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

			execCtx.Stats().ResetGauge(gent.SGToolCallsErrorConsecutive)
			execCtx.Stats().ResetGauge(
				gent.SGToolCallsErrorConsecutiveFor + gent.StatKey(call.Name),
			)

			results[i].Output = output.Text
			results[i].Instructions = output.Instructions

			// Format output. String outputs are passed through as-is
			// (no JSON wrapping) because they may contain Markdown,
			// prompts, or other structured text that should not be
			// quoted. Non-string outputs are JSON-marshalled.
			outputText, marshalErr := formatToolOutputJSON(output.Text)
			if marshalErr != nil {
				results[i].Error = fmt.Errorf("format tool output: %w", marshalErr)
				err = results[i].Error
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

		// Save the exact rendered result and dedup summary while this tool version is bound.
		results[i].DeduplicationSummary = c.DeduplicateSummary(results[i])
		results[i].SummaryCaptured = true

		execCtx.PublishAfterToolCall(
			call.Name, inputToUse, results[i], duration, err,
			gent.WithToolCallId(toolCallId),
			gent.WithToolCallSource(toolCallSource),
		)

	}

	return &gent.ToolChainResult{Results: results}, nil
}

// DeduplicateSummary returns an abbreviated observation text
// for a tool call whose output duplicates an earlier call.
func (c *Registry) DeduplicateSummary(
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
func (c *Registry) GetToolSchema(
	name string,
) *schema.Schema {
	return c.schemaMap[name]
}

// Compile-time check that Registry implements gent.ToolChain.
var _ gent.ToolChain = (*Registry)(nil)
