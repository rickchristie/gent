package react

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/format"
	"github.com/rickchristie/gent/termination"
	"github.com/rickchristie/gent/toolchain"
	"github.com/tmc/langchaingo/llms"
)

// LoopData implements gent.LoopData for the ReAct agent loop.
type LoopData struct {
	originalInput    []gent.ContentPart
	iterationHistory []*gent.Iteration
	iterations       []*gent.Iteration
}

// NewLoopData creates a new LoopData with the given input.
func NewLoopData(input ...gent.ContentPart) *LoopData {
	return &LoopData{
		originalInput:    input,
		iterationHistory: make([]*gent.Iteration, 0),
		iterations:       make([]*gent.Iteration, 0),
	}
}

// GetOriginalInput returns the original input provided by the user.
func (d *LoopData) GetOriginalInput() []gent.ContentPart {
	return d.originalInput
}

// GetIterationHistory returns all Iteration recorded, including compacted ones.
func (d *LoopData) GetIterationHistory() []*gent.Iteration {
	return d.iterationHistory
}

// AddIterationHistory adds a new Iteration to the full history.
func (d *LoopData) AddIterationHistory(iter *gent.Iteration) {
	d.iterationHistory = append(d.iterationHistory, iter)
}

// GetIterations returns all Iteration that will be used in next iteration.
func (d *LoopData) GetIterations() []*gent.Iteration {
	return d.iterations
}

// SetIterations sets the iterations to be used in next iteration.
func (d *LoopData) SetIterations(iterations []*gent.Iteration) {
	d.iterations = iterations
}

// Compile-time check that LoopData implements gent.LoopData.
var _ gent.LoopData = (*LoopData)(nil)

// ----------------------------------------------------------------------------
// simpleSection - for thinking section
// ----------------------------------------------------------------------------

// simpleSection is a simple TextOutputSection implementation for sections like thinking.
type simpleSection struct {
	name   string
	prompt string
}

// Name returns the section identifier.
func (s *simpleSection) Name() string { return s.name }

// Prompt returns the instructions for this section.
func (s *simpleSection) Prompt() string { return s.prompt }

// ParseSection returns the content as-is.
func (s *simpleSection) ParseSection(content string) (any, error) {
	return content, nil
}

// ----------------------------------------------------------------------------
// Agent - ReAct AgentLoop Implementation
// ----------------------------------------------------------------------------

// Agent implements the ReAct (Reasoning and Acting) agent loop.
// Flow: Think -> Act -> Observe -> Repeat until termination.
//
// The prompt construction follows this structure:
//   - System message: ReAct explanation + output format + user's additional context
//   - User message: Original input/task
//   - Previous iterations: Assistant responses + observations (tool results)
//
// Templates can be customized via WithSystemTemplate() for full control over prompting.
type Agent struct {
	behaviorAndContext string
	criticalRules      string
	systemTemplate     *template.Template
	model             gent.Model
	format            gent.TextOutputFormat
	toolChain         gent.ToolChain
	termination       gent.Termination
	thinkingSection   gent.TextOutputSection
	timeProvider      gent.TimeProvider
	observationPrefix string
	errorPrefix       string
	useStreaming      bool
}

// NewAgent creates a new Agent with the given model and default settings.
// Defaults:
//   - Format: format.NewXML()
//   - ToolChain: toolchain.NewYAML()
//   - Termination: termination.NewText()
//   - TimeProvider: gent.NewDefaultTimeProvider()
//   - SystemTemplate: DefaultReActSystemTemplate
func NewAgent(model gent.Model) *Agent {
	return &Agent{
		model:             model,
		format:            format.NewXML(),
		toolChain:         toolchain.NewYAML(),
		termination:       termination.NewText(),
		timeProvider:      gent.NewDefaultTimeProvider(),
		systemTemplate:    DefaultReActSystemTemplate,
		observationPrefix: "Observation:\n",
		errorPrefix:       "Error:\n",
	}
}

// WithBehaviorAndContext sets behavior instructions and context to include in the system prompt.
// This is appended to the default ReAct instructions, not a replacement.
// Use WithSystemTemplate() to completely replace the system prompt template.
func (r *Agent) WithBehaviorAndContext(prompt string) *Agent {
	r.behaviorAndContext = prompt
	return r
}

// WithCriticalRules sets critical rules to include in the system prompt.
// Critical rules are placed in a separate section from behavior and context.
func (r *Agent) WithCriticalRules(rules string) *Agent {
	r.criticalRules = rules
	return r
}

// WithSystemTemplate sets a custom system prompt template.
// Use this for full control over the ReAct prompting.
// See DefaultReActSystemTemplate for the expected template structure.
func (r *Agent) WithSystemTemplate(tmpl *template.Template) *Agent {
	r.systemTemplate = tmpl
	return r
}

// WithSystemTemplateString sets a custom system prompt template from a string.
// The string is parsed as a Go text/template with access to SystemPromptData fields:
//   - {{.BehaviorAndContext}} - behavior instructions from WithBehaviorAndContext()
//   - {{.CriticalRules}} - critical rules from WithCriticalRules()
//   - {{.OutputPrompt}} - output format instructions (tools, termination, etc.)
//
// Example:
//
//	loop.WithSystemTemplateString(`You are a coding assistant.
//	{{if .BehaviorAndContext}}{{.BehaviorAndContext}}{{end}}
//	{{.OutputPrompt}}`)
//
// Returns error if the template string is invalid.
func (r *Agent) WithSystemTemplateString(tmplStr string) (*Agent, error) {
	tmpl, err := template.New("react_system").Parse(tmplStr)
	if err != nil {
		return r, fmt.Errorf("failed to parse template: %w", err)
	}
	r.systemTemplate = tmpl
	return r, nil
}

// WithFormat sets the text output format.
func (r *Agent) WithFormat(f gent.TextOutputFormat) *Agent {
	r.format = f
	return r
}

// WithToolChain sets the tool chain.
func (r *Agent) WithToolChain(tc gent.ToolChain) *Agent {
	r.toolChain = tc
	return r
}

// WithTermination sets the termination handler.
func (r *Agent) WithTermination(t gent.Termination) *Agent {
	r.termination = t
	return r
}

// WithTimeProvider sets the time provider.
// Use this to inject a mock time provider for testing.
func (r *Agent) WithTimeProvider(tp gent.TimeProvider) *Agent {
	r.timeProvider = tp
	return r
}

// TimeProvider returns the current time provider.
func (r *Agent) TimeProvider() gent.TimeProvider {
	return r.timeProvider
}

// WithThinking enables the thinking section with the given prompt.
func (r *Agent) WithThinking(prompt string) *Agent {
	r.thinkingSection = &simpleSection{
		name:   "thinking",
		prompt: prompt,
	}
	return r
}

// WithThinkingSection sets a custom thinking section.
func (r *Agent) WithThinkingSection(section gent.TextOutputSection) *Agent {
	r.thinkingSection = section
	return r
}

// WithStreaming enables streaming mode for model calls.
// When enabled and the model implements StreamingModel, responses are streamed
// token-by-token. This allows ExecutionContext subscribers to receive chunks
// in real-time via SubscribeAll() or SubscribeToTopic("llm-response").
//
// Default: false (uses non-streaming GenerateContent)
func (r *Agent) WithStreaming(enabled bool) *Agent {
	r.useStreaming = enabled
	return r
}

// RegisterTool adds a tool to the tool chain.
func (r *Agent) RegisterTool(tool any) *Agent {
	r.toolChain.RegisterTool(tool)
	return r
}

// Next executes one iteration of the ReAct loop.
func (r *Agent) Next(ctx context.Context, execCtx *gent.ExecutionContext) *gent.AgentLoopResult {
	data := execCtx.Data()

	// Build output sections and generate prompts
	sections := r.buildOutputSections()
	outputPrompt := r.format.DescribeStructure(sections)
	toolsPrompt := r.toolChain.Prompt()

	// Build messages for model call
	messages := r.buildMessages(data, outputPrompt, toolsPrompt)

	// Generate stream ID based on iteration for unique identification
	streamId := fmt.Sprintf("iter-%d", execCtx.Iteration())
	streamTopicId := "llm-response"

	// Call model - use streaming if enabled and model supports it
	response, err := r.callModel(ctx, execCtx, streamId, streamTopicId, messages)
	if err != nil {
		return &gent.AgentLoopResult{
			Action: gent.LATerminate,
			Result: []gent.ContentPart{llms.TextContent{Text: fmt.Sprintf("Model error: %v", err)}},
		}
	}

	// Extract response content
	responseContent := ""
	if len(response.Choices) > 0 {
		responseContent = response.Choices[0].Content
	}

	// Parse response
	parsed, parseErr := r.format.Parse(responseContent)

	// Check termination first
	terminationName := r.termination.Name()
	if terminationContents, ok := parsed[terminationName]; ok && len(terminationContents) > 0 {
		for _, content := range terminationContents {
			if result := r.termination.ShouldTerminate(content); len(result) > 0 {
				// Add final iteration to history
				iter := r.buildIteration(responseContent, "")
				data.AddIterationHistory(iter)
				return &gent.AgentLoopResult{
					Action: gent.LATerminate,
					Result: result,
				}
			}
		}
	}

	// Handle parse error after termination check
	if parseErr != nil {
		// Try treating raw content as termination
		if result := r.termination.ShouldTerminate(responseContent); len(result) > 0 {
			iter := r.buildIteration(responseContent, "")
			data.AddIterationHistory(iter)
			return &gent.AgentLoopResult{
				Action: gent.LATerminate,
				Result: result,
			}
		}
		return &gent.AgentLoopResult{
			Action: gent.LATerminate,
			Result: []gent.ContentPart{llms.TextContent{
				Text: fmt.Sprintf("Parse error: %v\nRaw response: %s", parseErr, responseContent),
			}},
		}
	}

	// Execute tool calls if action section is present (automatically traced via execCtx)
	actionName := r.toolChain.Name()
	observation := ""
	if actionContents, ok := parsed[actionName]; ok && len(actionContents) > 0 {
		observation = r.executeToolCalls(ctx, execCtx, actionContents)
	}

	// Build iteration and update data
	iter := r.buildIteration(responseContent, observation)
	data.AddIterationHistory(iter)

	// Add to iterations for next call
	iterations := data.GetIterations()
	iterations = append(iterations, iter)
	data.SetIterations(iterations)

	return &gent.AgentLoopResult{
		Action:     gent.LAContinue,
		NextPrompt: observation,
	}
}

// buildOutputSections constructs the list of output sections.
func (r *Agent) buildOutputSections() []gent.TextOutputSection {
	var sections []gent.TextOutputSection

	// Add thinking section if configured
	if r.thinkingSection != nil {
		sections = append(sections, r.thinkingSection)
	}

	// Add tool chain section
	sections = append(sections, r.toolChain)

	// Add termination section
	sections = append(sections, r.termination)

	return sections
}

// processTemplateString processes a string as a template.
// This allows users to use template variables like {{.Time.Today}} in their prompts.
func (r *Agent) processTemplateString(input string) string {
	if input == "" {
		return ""
	}

	// If the input doesn't contain template syntax, return as-is
	if !strings.Contains(input, "{{") {
		return input
	}

	// Parse and execute the input as a template
	tmpl, err := template.New("template_string").Parse(input)
	if err != nil {
		// If parsing fails, return the original string
		return input
	}

	// Execute with access to Time provider
	data := struct {
		Time gent.TimeProvider
	}{
		Time: r.timeProvider,
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		// If execution fails, return the original string
		return input
	}

	return buf.String()
}

// buildMessages constructs the message list for the model call.
func (r *Agent) buildMessages(
	data gent.LoopData,
	outputPrompt string,
	toolsPrompt string,
) []llms.MessageContent {
	var messages []llms.MessageContent

	// Process prompts as templates to expand variables like {{.Time.Today}}
	processedBehavior := r.processTemplateString(r.behaviorAndContext)
	processedRules := r.processTemplateString(r.criticalRules)

	// Build system message using template
	templateData := SystemPromptData{
		BehaviorAndContext: processedBehavior,
		CriticalRules:      processedRules,
		OutputPrompt:       outputPrompt,
		ToolsPrompt:        toolsPrompt,
		Time:               r.timeProvider,
	}

	systemContent, err := ExecuteTemplate(r.systemTemplate, templateData)
	if err != nil {
		// Fallback to basic prompt if template fails
		systemContent = outputPrompt
	}

	if systemContent != "" {
		messages = append(messages, llms.MessageContent{
			Role:  llms.ChatMessageTypeSystem,
			Parts: []llms.ContentPart{llms.TextContent{Text: systemContent}},
		})
	}

	// User message: original input
	originalInput := data.GetOriginalInput()
	if len(originalInput) > 0 {
		userParts := make([]llms.ContentPart, len(originalInput))
		for i, part := range originalInput {
			userParts[i] = part
		}
		messages = append(messages, llms.MessageContent{
			Role:  llms.ChatMessageTypeHuman,
			Parts: userParts,
		})
	}

	// Previous iterations
	for _, iter := range data.GetIterations() {
		for _, msg := range iter.Messages {
			parts := make([]llms.ContentPart, len(msg.Parts))
			for i, part := range msg.Parts {
				parts[i] = part
			}
			messages = append(messages, llms.MessageContent{
				Role:  msg.Role,
				Parts: parts,
			})
		}
	}

	return messages
}

// executeToolCalls executes tool calls from the parsed action contents.
func (r *Agent) executeToolCalls(
	ctx context.Context,
	execCtx *gent.ExecutionContext,
	contents []string,
) string {
	var observations []string

	for _, content := range contents {
		result, err := r.toolChain.Execute(ctx, execCtx, content)
		if err != nil {
			observations = append(observations,
				fmt.Sprintf("%s%v", r.errorPrefix, err))
			continue
		}

		// Process results and errors
		for i, toolResult := range result.Results {
			if result.Errors[i] != nil {
				observations = append(observations,
					fmt.Sprintf("%s%s: %v", r.errorPrefix, result.Calls[i].Name, result.Errors[i]))
				continue
			}
			if toolResult != nil {
				// Format tool result
				var resultText strings.Builder
				resultText.WriteString(r.observationPrefix)
				resultText.WriteString(fmt.Sprintf("[%s] ", toolResult.Name))
				for _, part := range toolResult.Result {
					if tc, ok := part.(llms.TextContent); ok {
						resultText.WriteString(tc.Text)
					}
				}
				observations = append(observations, resultText.String())
			}
		}
	}

	return strings.Join(observations, "\n\n")
}

// buildIteration creates an Iteration from response and observation.
func (r *Agent) buildIteration(response, observation string) *gent.Iteration {
	var messages []gent.MessageContent

	// Assistant message (response)
	assistantMsg := gent.MessageContent{
		Role:  llms.ChatMessageTypeAI,
		Parts: []gent.ContentPart{llms.TextContent{Text: response}},
	}
	messages = append(messages, assistantMsg)

	// User message (observation) - only if there's an observation
	if observation != "" {
		userMsg := gent.MessageContent{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []gent.ContentPart{llms.TextContent{Text: observation}},
		}
		messages = append(messages, userMsg)
	}

	return &gent.Iteration{
		Messages: messages,
	}
}

// callModel calls the model, using streaming if enabled and supported.
func (r *Agent) callModel(
	ctx context.Context,
	execCtx *gent.ExecutionContext,
	streamId string,
	streamTopicId string,
	messages []llms.MessageContent,
) (*gent.ContentResponse, error) {
	// Check if streaming is enabled and model supports it
	if r.useStreaming {
		if streamingModel, ok := r.model.(gent.StreamingModel); ok {
			return r.callModelStreaming(ctx, execCtx, streamingModel, streamId, streamTopicId, messages)
		}
	}

	// Fall back to non-streaming
	return r.model.GenerateContent(ctx, execCtx, streamId, streamTopicId, messages)
}

// callModelStreaming calls the model with streaming and accumulates the response.
func (r *Agent) callModelStreaming(
	ctx context.Context,
	execCtx *gent.ExecutionContext,
	model gent.StreamingModel,
	streamId string,
	streamTopicId string,
	messages []llms.MessageContent,
) (*gent.ContentResponse, error) {
	stream, err := model.GenerateContentStream(ctx, execCtx, streamId, streamTopicId, messages)
	if err != nil {
		return nil, err
	}

	// Accumulate chunks into response
	acc := gent.NewStreamAccumulator()
	for chunk := range stream.Chunks() {
		if chunk.Err != nil {
			return nil, chunk.Err
		}
		acc.Add(chunk)
	}

	// Get final response with token info from stream
	streamResponse, err := stream.Response()
	if err != nil {
		return nil, err
	}

	return acc.ResponseWithInfo(streamResponse), nil
}

// Compile-time check that Agent implements gent.AgentLoop.
var _ gent.AgentLoop[*LoopData] = (*Agent)(nil)
