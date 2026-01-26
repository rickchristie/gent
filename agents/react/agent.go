package react

import (
	"fmt"
	"strings"
	"text/template"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/format"
	"github.com/rickchristie/gent/section"
	"github.com/rickchristie/gent/termination"
	"github.com/rickchristie/gent/toolchain"
	"github.com/tmc/langchaingo/llms"
)

// LoopData implements gent.LoopData for the ReAct agent loop.
type LoopData struct {
	task             *gent.Task
	iterationHistory []*gent.Iteration
	scratchpad       []*gent.Iteration
}

// NewLoopData creates a new LoopData with the given task.
func NewLoopData(task *gent.Task) *LoopData {
	return &LoopData{
		task:             task,
		iterationHistory: make([]*gent.Iteration, 0),
		scratchpad:       make([]*gent.Iteration, 0),
	}
}

// GetTask returns the original input provided by the user.
func (d *LoopData) GetTask() *gent.Task {
	return d.task
}

// GetIterationHistory returns all Iteration recorded, including compacted ones.
func (d *LoopData) GetIterationHistory() []*gent.Iteration {
	return d.iterationHistory
}

// AddIterationHistory adds a new Iteration to the full history.
func (d *LoopData) AddIterationHistory(iter *gent.Iteration) {
	d.iterationHistory = append(d.iterationHistory, iter)
}

// GetScratchPad returns all Iteration that will be used in next iteration.
func (d *LoopData) GetScratchPad() []*gent.Iteration {
	return d.scratchpad
}

// SetScratchPad sets the iterations to be used in next iteration.
func (d *LoopData) SetScratchPad(iterations []*gent.Iteration) {
	d.scratchpad = iterations
}

// Compile-time check that LoopData implements gent.LoopData.
var _ gent.LoopData = (*LoopData)(nil)

// ----------------------------------------------------------------------------
// Agent - ReAct AgentLoop Implementation
// ----------------------------------------------------------------------------

// Agent implements the ReAct (Reasoning and Acting) agent loop.
// Flow: Think -> Act -> Observe -> Repeat until termination.
//
// The prompt construction follows this structure:
//   - System message: ReAct explanation + output format + user's additional context
//   - User message: Task and scratchpad (previous iterations)
//
// Templates can be customized via WithSystemTemplate() for full control over prompting.
type Agent struct {
	behaviorAndContext string
	criticalRules      string
	systemTemplate     *template.Template
	model              gent.Model
	format             gent.TextFormat
	toolChain          gent.ToolChain
	termination        gent.Termination
	thinkingSection    gent.TextSection
	timeProvider       gent.TimeProvider
	useStreaming       bool
}

// NewAgent creates a new Agent with the given model and default settings.
// Defaults:
//   - Format: format.NewXML()
//   - ToolChain: toolchain.NewYAML()
//   - Termination: termination.NewText("answer")
//   - TimeProvider: gent.NewDefaultTimeProvider()
//   - SystemTemplate: DefaultReActSystemTemplate
func NewAgent(model gent.Model) *Agent {
	return &Agent{
		model:          model,
		format:         format.NewXML(),
		toolChain:      toolchain.NewYAML(),
		termination:    termination.NewText("answer"),
		timeProvider:   gent.NewDefaultTimeProvider(),
		systemTemplate: DefaultReActSystemTemplate,
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
func (r *Agent) WithFormat(f gent.TextFormat) *Agent {
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

// WithThinking enables the thinking section with the given guidance.
func (r *Agent) WithThinking(guidance string) *Agent {
	r.thinkingSection = section.NewText("thinking").
		WithGuidance(guidance)
	return r
}

// WithThinkingSection sets a custom thinking section.
func (r *Agent) WithThinkingSection(s gent.TextSection) *Agent {
	r.thinkingSection = s
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
//
// The method follows a specific order of operations:
//  1. Build prompts and call the model
//  2. Parse the complete response to identify all sections
//  3. Check for action (tool calls) section - if present, execute tools and continue the loop
//  4. Check for termination (answer) section - only terminate if no actions were present
//
// This order ensures that tool calls are always executed before termination. If the model
// outputs both an action and an answer in the same response, the action takes priority.
// This prevents premature termination when tools might fail or produce unexpected results.
func (r *Agent) Next(execCtx *gent.ExecutionContext) (*gent.AgentLoopResult, error) {
	data := execCtx.Data()

	// Register output sections and generate prompts
	for _, section := range r.buildOutputSections() {
		r.format.RegisterSection(section)
	}
	outputPrompt := r.format.DescribeStructure()
	toolsPrompt := r.toolChain.AvailableToolsPrompt()

	// Build messages for model call
	messages := r.buildMessages(data, outputPrompt, toolsPrompt)

	// Generate stream ID based on iteration for unique identification
	streamId := fmt.Sprintf("iter-%d", execCtx.Iteration())
	streamTopicId := "llm-response"

	// Call model - use streaming if enabled and model supports it
	response, err := r.callModel(execCtx, streamId, streamTopicId, messages)
	if err != nil {
		return nil, fmt.Errorf("model call failed: %w", err)
	}

	// Extract response content
	responseContent := ""
	if len(response.Choices) > 0 {
		responseContent = response.Choices[0].Content
	}

	// Parse complete response to identify all available sections
	// The format handles tracing of parse errors and resetting consecutive counter
	parsed, parseErr := r.format.Parse(execCtx, responseContent)

	// Check for action (tool calls) section first - actions take priority over termination
	// This ensures tools are executed even if the model also outputs an answer
	actionContents, hasActions := parsed[r.toolChain.Name()]
	if hasActions && len(actionContents) > 0 {
		// Execute tool calls (automatically traced via execCtx)
		observation := r.executeToolCalls(execCtx, actionContents)

		// Build iteration and update data
		iter := r.buildIteration(responseContent, observation)
		data.AddIterationHistory(iter)

		// Add to scratchpad for next call
		scratchpad := data.GetScratchPad()
		scratchpad = append(scratchpad, iter)
		data.SetScratchPad(scratchpad)

		return &gent.AgentLoopResult{
			Action:     gent.LAContinue,
			NextPrompt: observation,
		}, nil
	}

	// No actions present - check for termination
	if terminationContents, ok := parsed[r.termination.Name()]; ok && len(terminationContents) > 0 {
		for _, content := range terminationContents {
			if result := r.termination.ShouldTerminate(content); len(result) > 0 {
				// Add final iteration to history
				iter := r.buildIteration(responseContent, "")
				data.AddIterationHistory(iter)
				return &gent.AgentLoopResult{
					Action: gent.LATerminate,
					Result: result,
				}, nil
			}
		}
	}

	// Handle parse error - feed back to agent as observation to allow recovery
	if parseErr != nil {
		observation := fmt.Sprintf(`<observation>
Format parse error: %v

Your response could not be parsed. Please ensure your response follows the expected format.

Your raw response was:
%s

Please try again with proper formatting.
</observation>`, parseErr, responseContent)

		// Build iteration with parse error feedback
		iter := r.buildIteration(responseContent, observation)
		data.AddIterationHistory(iter)

		scratchpad := data.GetScratchPad()
		scratchpad = append(scratchpad, iter)
		data.SetScratchPad(scratchpad)

		return &gent.AgentLoopResult{
			Action:     gent.LAContinue,
			NextPrompt: observation,
		}, nil
	}

	// No actions and no valid termination - continue loop with empty observation
	// This handles edge cases where the model didn't output a properly formatted response
	iter := r.buildIteration(responseContent, "")
	data.AddIterationHistory(iter)

	scratchpad := data.GetScratchPad()
	scratchpad = append(scratchpad, iter)
	data.SetScratchPad(scratchpad)

	return &gent.AgentLoopResult{
		Action:     gent.LAContinue,
		NextPrompt: "",
	}, nil
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
// Message structure:
//  1. System prompt (role: system) - x1
//  2. Task (role: user) - x1, text + media parts, panics if both empty
//  3. Scratchpad (N messages interleaved: role: AI, then role: tool)
//  4. BEGIN!/CONTINUE! (role: user) - x1
func (r *Agent) buildMessages(
	data gent.LoopData,
	outputPrompt string,
	toolsPrompt string,
) []llms.MessageContent {
	var messages []llms.MessageContent

	// 1. System prompt
	messages = append(messages, r.buildSystemMessage(outputPrompt, toolsPrompt))

	// 2. Task message (role: user) - text + media parts
	messages = append(messages, r.buildTaskMessage(data))

	// 3. Scratchpad messages (interleaved AI and tool messages)
	scratchpad := data.GetScratchPad()
	for _, iter := range scratchpad {
		for _, msg := range iter.Messages {
			messages = append(messages, llms.MessageContent{
				Role:  msg.Role,
				Parts: toLLMParts(msg.Parts),
			})
		}
	}

	// 4. BEGIN!/CONTINUE! message (role: user)
	continueText := "BEGIN!"
	if len(scratchpad) > 0 {
		continueText = "CONTINUE!"
	}
	messages = append(messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextContent{Text: continueText}},
	})

	return messages
}

// buildSystemMessage constructs the system message using the template.
func (r *Agent) buildSystemMessage(outputPrompt string, toolsPrompt string) llms.MessageContent {
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

	return llms.MessageContent{
		Role:  llms.ChatMessageTypeSystem,
		Parts: []llms.ContentPart{llms.TextContent{Text: systemContent}},
	}
}

// buildTaskMessage constructs the task message with text and media parts.
// Panics if task is nil or has both empty text and no media.
func (r *Agent) buildTaskMessage(data gent.LoopData) llms.MessageContent {
	task := data.GetTask()
	if task == nil || (task.Text == "" && len(task.Media) == 0) {
		panic("task must have either text or media content")
	}

	var parts []llms.ContentPart

	// Add formatted task text if present
	if task.Text != "" {
		formattedText := r.format.FormatSection("task", task.Text)
		parts = append(parts, llms.TextContent{Text: formattedText})
	}

	// Add media parts
	parts = append(parts, toLLMParts(task.Media)...)

	return llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: parts,
	}
}

// toLLMParts converts gent.ContentPart slice to llms.ContentPart slice.
func toLLMParts(parts []gent.ContentPart) []llms.ContentPart {
	result := make([]llms.ContentPart, len(parts))
	for i, p := range parts {
		result[i] = p
	}
	return result
}

// executeToolCalls executes tool calls from the parsed action contents.
// The result.Text contains formatted sections from the ToolChain. This method
// collects all sections and wraps them in a single observation section.
func (r *Agent) executeToolCalls(
	execCtx *gent.ExecutionContext,
	contents []string,
) string {
	var allSections []string

	for _, content := range contents {
		result, err := r.toolChain.Execute(execCtx, content, r.format)
		if err != nil {
			// Format error using the text format
			errorText := r.format.FormatSection("error", fmt.Sprintf("Error: %v", err))
			allSections = append(allSections, errorText)
			continue
		}

		if result.Text != "" {
			allSections = append(allSections, result.Text)
		}

		// TODO: Handle result.Media for multimodal support
		// For now, media is not included in the observation text
	}

	if len(allSections) == 0 {
		return ""
	}

	// Wrap all sections in a single observation
	return r.format.FormatSection("observation", strings.Join(allSections, "\n"))
}

// buildIteration creates an Iteration from response and observation.
// The response is stored as AI role, and observation as Human role.
// Note: We use Human role for observations because the text-based ReAct pattern
// doesn't use native tool calling APIs. The observation is a user message containing
// tool output in text form.
func (r *Agent) buildIteration(response, observation string) *gent.Iteration {
	var messages []gent.MessageContent

	// Assistant message (response)
	assistantMsg := gent.MessageContent{
		Role:  llms.ChatMessageTypeAI,
		Parts: []gent.ContentPart{llms.TextContent{Text: response}},
	}
	messages = append(messages, assistantMsg)

	// Observation message (Human role) - only if there's an observation
	if observation != "" {
		observationMsg := gent.MessageContent{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []gent.ContentPart{llms.TextContent{Text: observation}},
		}
		messages = append(messages, observationMsg)
	}

	return &gent.Iteration{
		Messages: messages,
	}
}

// callModel calls the model, using streaming if enabled and supported.
func (r *Agent) callModel(
	execCtx *gent.ExecutionContext,
	streamId string,
	streamTopicId string,
	messages []llms.MessageContent,
) (*gent.ContentResponse, error) {
	// Check if streaming is enabled and model supports it
	if r.useStreaming {
		if streamingModel, ok := r.model.(gent.StreamingModel); ok {
			return r.callModelStreaming(execCtx, streamingModel, streamId, streamTopicId, messages)
		}
	}

	// Fall back to non-streaming
	return r.model.GenerateContent(execCtx, streamId, streamTopicId, messages)
}

// callModelStreaming calls the model with streaming and accumulates the response.
func (r *Agent) callModelStreaming(
	execCtx *gent.ExecutionContext,
	model gent.StreamingModel,
	streamId string,
	streamTopicId string,
	messages []llms.MessageContent,
) (*gent.ContentResponse, error) {
	stream, err := model.GenerateContentStream(execCtx, streamId, streamTopicId, messages)
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
