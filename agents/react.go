package agents

import (
	"context"
	"fmt"
	"strings"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/format"
	"github.com/rickchristie/gent/termination"
	"github.com/rickchristie/gent/toolchain"
	"github.com/tmc/langchaingo/llms"
)

// ReactLoopData implements gent.LoopData for the ReAct agent loop.
type ReactLoopData struct {
	originalInput    []gent.ContentPart
	iterationHistory [][]*gent.IterationInfo
	iterations       [][]*gent.IterationInfo
}

// NewReactLoopData creates a new ReactLoopData with the given input.
func NewReactLoopData(input ...gent.ContentPart) *ReactLoopData {
	return &ReactLoopData{
		originalInput:    input,
		iterationHistory: make([][]*gent.IterationInfo, 0),
		iterations:       make([][]*gent.IterationInfo, 0),
	}
}

// GetOriginalInput returns the original input provided by the user.
func (d *ReactLoopData) GetOriginalInput() []gent.ContentPart {
	return d.originalInput
}

// GetIterationHistory returns all IterationInfo recorded, including compacted ones.
func (d *ReactLoopData) GetIterationHistory() [][]*gent.IterationInfo {
	return d.iterationHistory
}

// AddIterationHistory adds a new IterationInfo to the full history.
func (d *ReactLoopData) AddIterationHistory(info *gent.IterationInfo) {
	d.iterationHistory = append(d.iterationHistory, []*gent.IterationInfo{info})
}

// GetIterations returns all IterationInfo that will be used in next iteration.
func (d *ReactLoopData) GetIterations() [][]*gent.IterationInfo {
	return d.iterations
}

// SetIterations sets the iterations to be used in next iteration.
func (d *ReactLoopData) SetIterations(iterations [][]*gent.IterationInfo) {
	d.iterations = iterations
}

// Compile-time check that ReactLoopData implements gent.LoopData.
var _ gent.LoopData = (*ReactLoopData)(nil)

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
// ReactLoop - ReAct AgentLoop Implementation
// ----------------------------------------------------------------------------

// ReactLoop implements the ReAct (Reasoning and Acting) agent loop.
// Flow: Think -> Act -> Observe -> Repeat until termination.
type ReactLoop struct {
	systemPrompt      string
	model             gent.Model
	format            gent.TextOutputFormat
	toolChain         gent.ToolChain
	termination       gent.Termination
	thinkingSection   gent.TextOutputSection
	observationPrefix string
	errorPrefix       string
}

// NewReactLoop creates a new ReactLoop with the given model and default settings.
// Defaults:
//   - Format: format.NewXML()
//   - ToolChain: toolchain.NewYAML()
//   - Termination: termination.NewText()
func NewReactLoop(model gent.Model) *ReactLoop {
	return &ReactLoop{
		model:             model,
		format:            format.NewXML(),
		toolChain:         toolchain.NewYAML(),
		termination:       termination.NewText(),
		observationPrefix: "Tool results:\n",
		errorPrefix:       "Tool error:\n",
	}
}

// WithSystemPrompt sets the system prompt.
func (r *ReactLoop) WithSystemPrompt(prompt string) *ReactLoop {
	r.systemPrompt = prompt
	return r
}

// WithFormat sets the text output format.
func (r *ReactLoop) WithFormat(f gent.TextOutputFormat) *ReactLoop {
	r.format = f
	return r
}

// WithToolChain sets the tool chain.
func (r *ReactLoop) WithToolChain(tc gent.ToolChain) *ReactLoop {
	r.toolChain = tc
	return r
}

// WithTermination sets the termination handler.
func (r *ReactLoop) WithTermination(t gent.Termination) *ReactLoop {
	r.termination = t
	return r
}

// WithThinking enables the thinking section with the given prompt.
func (r *ReactLoop) WithThinking(prompt string) *ReactLoop {
	r.thinkingSection = &simpleSection{
		name:   "thinking",
		prompt: prompt,
	}
	return r
}

// WithThinkingSection sets a custom thinking section.
func (r *ReactLoop) WithThinkingSection(section gent.TextOutputSection) *ReactLoop {
	r.thinkingSection = section
	return r
}

// RegisterTool adds a tool to the tool chain.
func (r *ReactLoop) RegisterTool(tool any) *ReactLoop {
	r.toolChain.RegisterTool(tool)
	return r
}

// Iterate executes one iteration of the ReAct loop.
func (r *ReactLoop) Iterate(ctx context.Context, data gent.LoopData) *gent.AgentLoopResult {
	// Build output sections and generate output prompt
	sections := r.buildOutputSections()
	outputPrompt := r.format.Describe(sections)

	// Build messages for model call
	messages := r.buildMessages(data, outputPrompt)

	// Call model
	response, err := r.model.GenerateContent(ctx, messages)
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
				info := r.buildIterationInfo(responseContent, "")
				data.AddIterationHistory(info)
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
			info := r.buildIterationInfo(responseContent, "")
			data.AddIterationHistory(info)
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

	// Execute tool calls if action section is present
	actionName := r.toolChain.Name()
	observation := ""
	if actionContents, ok := parsed[actionName]; ok && len(actionContents) > 0 {
		observation = r.executeToolCalls(ctx, actionContents)
	}

	// Build iteration info and update data
	info := r.buildIterationInfo(responseContent, observation)
	data.AddIterationHistory(info)

	// Add to iterations for next call
	iterations := data.GetIterations()
	iterations = append(iterations, []*gent.IterationInfo{info})
	data.SetIterations(iterations)

	return &gent.AgentLoopResult{
		Action:     gent.LAContinue,
		NextPrompt: observation,
	}
}

// buildOutputSections constructs the list of output sections.
func (r *ReactLoop) buildOutputSections() []gent.TextOutputSection {
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

// buildMessages constructs the message list for the model call.
func (r *ReactLoop) buildMessages(data gent.LoopData, outputPrompt string) []llms.MessageContent {
	var messages []llms.MessageContent

	// System message: system prompt + output prompt
	systemContent := r.systemPrompt
	if outputPrompt != "" {
		if systemContent != "" {
			systemContent += "\n\n"
		}
		systemContent += outputPrompt
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
	for _, iterGroup := range data.GetIterations() {
		for _, iter := range iterGroup {
			for _, msgGroup := range iter.Messages {
				for _, msg := range msgGroup {
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
		}
	}

	return messages
}

// executeToolCalls executes tool calls from the parsed action contents.
func (r *ReactLoop) executeToolCalls(ctx context.Context, contents []string) string {
	var observations []string

	for _, content := range contents {
		result, err := r.toolChain.Execute(ctx, content)
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

// buildIterationInfo creates an IterationInfo from response and observation.
func (r *ReactLoop) buildIterationInfo(response, observation string) *gent.IterationInfo {
	var messages [][]gent.MessageContent

	// Assistant message (response)
	assistantMsg := gent.MessageContent{
		Role:  llms.ChatMessageTypeAI,
		Parts: []gent.ContentPart{llms.TextContent{Text: response}},
	}
	messages = append(messages, []gent.MessageContent{assistantMsg})

	// User message (observation) - only if there's an observation
	if observation != "" {
		userMsg := gent.MessageContent{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []gent.ContentPart{llms.TextContent{Text: observation}},
		}
		messages = append(messages, []gent.MessageContent{userMsg})
	}

	return &gent.IterationInfo{
		Messages: messages,
	}
}

// Compile-time check that ReactLoop implements gent.AgentLoop.
var _ gent.AgentLoop[*ReactLoopData] = (*ReactLoop)(nil)
