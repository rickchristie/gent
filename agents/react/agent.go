package react

import (
	"errors"
	"fmt"
	"strings"

	"github.com/rickchristie/gent"
	agentutil "github.com/rickchristie/gent/agents"
	"github.com/rickchristie/gent/format"
	"github.com/rickchristie/gent/section"
	"github.com/rickchristie/gent/termination"
	"github.com/rickchristie/gent/toolchain"
	"github.com/tmc/langchaingo/llms"
)

// ----------------------------------------------------------------------------
// Agent - ReAct AgentLoop Implementation
// ----------------------------------------------------------------------------

// Agent implements the ReAct (Reasoning and Acting) agent loop.
// Flow: Think -> Act -> Observe -> Repeat until termination.
//
// The message construction follows this structure:
//  1. System prompt(s) - from SystemPromptBuilder (default: behavior, re_act, critical_rules, tools, output_format)
//  2. Task message (user) - formatted task text + media
//  3. Scratchpad messages - interleaved AI responses and observations from previous iterations
//  4. BEGIN!/CONTINUE! (user) - signals start or continuation of the loop
//
// System prompts can be customized via WithSystemPromptBuilder() for full control over prompting.
type Agent struct {
	behaviorAndContext  string
	criticalRules       string
	systemPromptBuilder SystemPromptBuilder
	model               gent.Model
	format              gent.TextFormat
	toolChain           gent.ToolChain
	termination         gent.Termination
	thinkingSection     gent.TextSection
	timeProvider        gent.TimeProvider
	callOptions            []llms.CallOption
	repetitionConfig       agentutil.RepetitionConfig
	maxResponseChars       int
	responseTooLongMessage string
}

// NewAgent creates a new Agent with the given model and default settings.
// Defaults:
//   - Format: format.NewXML()
//   - ToolChain: toolchain.NewYAML()
//   - Termination: termination.NewText("answer")
//   - TimeProvider: gent.NewDefaultTimeProvider()
//   - SystemPromptBuilder: DefaultSystemPromptBuilder
func NewAgent(model gent.Model) *Agent {
	return &Agent{
		model:                  model,
		format:                 format.NewXML(),
		toolChain:              toolchain.NewYAML(),
		termination:            termination.NewText("answer"),
		timeProvider:           gent.NewDefaultTimeProvider(),
		systemPromptBuilder:    DefaultSystemPromptBuilder,
		repetitionConfig:       agentutil.DefaultRepetitionConfig(),
		maxResponseChars:       agentutil.DefaultMaxResponseChars,
		responseTooLongMessage: agentutil.DefaultResponseTooLongMessage,
	}
}

// WithBehaviorAndContext sets behavior instructions and context to include in the system prompt.
// This is passed to the SystemPromptBuilder and formatted as a "behavior" section.
// Use WithSystemPromptBuilder() to completely replace how the system prompt is built.
func (r *Agent) WithBehaviorAndContext(prompt string) *Agent {
	r.behaviorAndContext = prompt
	return r
}

// WithCriticalRules sets critical rules to include in the system prompt.
// Critical rules are placed in a separate "critical_rules" section.
func (r *Agent) WithCriticalRules(rules string) *Agent {
	r.criticalRules = rules
	return r
}

// WithSystemPromptBuilder sets a custom function to build the system prompt.
// Use this for full control over the system prompt structure.
// See DefaultSystemPromptBuilder for the expected behavior.
func (r *Agent) WithSystemPromptBuilder(builder SystemPromptBuilder) *Agent {
	r.systemPromptBuilder = builder
	return r
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

// WithStreaming is a no-op retained for backward compatibility.
// All model calls now use streaming unconditionally.
//
// Deprecated: Streaming is always enabled. This method will be removed.
func (r *Agent) WithStreaming(_ bool) *Agent {
	return r
}

// WithRepetitionConfig sets the streaming repetition detection
// configuration. See [agentutil.RepetitionConfig] for available options.
// Default: [agentutil.DefaultRepetitionConfig] (enabled, block size 400,
// threshold 3).
func (r *Agent) WithRepetitionConfig(
	cfg agentutil.RepetitionConfig,
) *Agent {
	r.repetitionConfig = cfg
	return r
}

// WithMaxResponseChars sets the maximum characters allowed in a single model response.
// When exceeded, the stream is cancelled and the accumulated content is inspected for
// repetition. If repetition is detected, the loop recovery path runs. If not, a "too long"
// reminder is injected instead. Default: [agentutil.DefaultMaxResponseChars] (16000).
// Set to 0 to disable.
func (r *Agent) WithMaxResponseChars(n int) *Agent {
	r.maxResponseChars = n
	return r
}

// WithResponseTooLongMessage sets the message injected when a response exceeds
// MaxResponseChars but is NOT a repetition loop.
// Default: [agentutil.DefaultResponseTooLongMessage].
func (r *Agent) WithResponseTooLongMessage(msg string) *Agent {
	r.responseTooLongMessage = msg
	return r
}

// WithCallOptions sets LLM call options (e.g., temperature, top-p, max tokens) that are
// forwarded to the model on every GenerateContent call.
func (r *Agent) WithCallOptions(options ...llms.CallOption) *Agent {
	r.callOptions = options
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
	messages := r.buildMessages(execCtx, data, outputPrompt, toolsPrompt)

	// Generate stream ID based on iteration for unique identification
	streamId := fmt.Sprintf("iter-%d", execCtx.Iteration())
	streamTopicId := "llm-response"

	// Call model with streaming + repetition detection.
	response, repResult, err := r.callModel(execCtx, streamId, streamTopicId, messages)
	if err != nil {
		return nil, fmt.Errorf("model call failed: %w", err)
	}

	// Handle repetition or max-size truncation.
	if repResult != nil {
		if errors.Is(repResult.Err, agentutil.ErrMaxResponseSize) {
			return r.handleResponseTooLong(execCtx, data, response)
		}
		return r.handleRepetition(execCtx, data, response, repResult)
	}

	// Extract response content
	responseContent := ""
	if len(response.Choices) > 0 {
		responseContent = response.Choices[0].Content
	}

	// Parse complete response to identify all available sections
	// The format handles tracing of parse errors and resetting consecutive counter
	parsed, parseErr := r.format.Parse(execCtx, responseContent)

	// Process thinking section if configured and present
	// This validates structured thinking output and tracks section parse errors.
	// Section parse errors don't stop the current iteration, but the executor
	// will terminate if section parse error limits are exceeded.
	if r.thinkingSection != nil {
		if thinkingContents, ok := parsed[r.thinkingSection.Name()]; ok {
			for _, content := range thinkingContents {
				// ParseSection handles stats tracking:
				// - On error: publishes ParseErrorEvent, increments total/consecutive counters
				// - On success: resets consecutive counter
				_, _ = r.thinkingSection.ParseSection(execCtx, content)
			}
		}
	}

	// Check for action (tool calls) section first - actions take priority over termination
	// This ensures tools are executed even if the model also outputs an answer
	actionContents, hasActions := parsed[r.toolChain.Name()]
	if hasActions && len(actionContents) > 0 {
		// Execute tool calls (automatically traced via execCtx)
		tcResult, observation := r.executeToolCalls(
			execCtx, actionContents,
		)

		// Build iteration: AI response + observation from tool results
		iter := r.buildToolCallIteration(
			responseContent, tcResult,
		)
		data.AddIterationHistory(iter)

		// Add to scratchpad for next call
		scratchpad := data.GetScratchPad()
		scratchpad = append(scratchpad, iter)

		// Check for violations: action + answer, or extra sections after action
		_, hasAnswer := parsed[r.termination.Name()]
		if hasAnswer && len(parsed[r.termination.Name()]) > 0 {
			reminder := &gent.Iteration{
				Origin: gent.IterationSystemInjected,
				Messages: []*gent.MessageContent{{
					Role: llms.ChatMessageTypeHuman,
					Parts: []gent.ContentPart{llms.TextContent{
						Text: r.format.FormatSections([]gent.FormattedSection{{
							Name: "system_reminder",
							Content: "ERROR! You can only choose between either " +
								"Action or Answer in one turn, your Answer " +
								"will be ignored!",
						}}),
					}},
				}},
			}
			data.AddIterationHistory(reminder)
			scratchpad = append(scratchpad, reminder)
		} else if r.hasExtraSections(parsed) {
			reminder := &gent.Iteration{
				Origin: gent.IterationSystemInjected,
				Messages: []*gent.MessageContent{{
					Role: llms.ChatMessageTypeHuman,
					Parts: []gent.ContentPart{llms.TextContent{
						Text: r.format.FormatSections([]gent.FormattedSection{{
							Name: "system_reminder",
							Content: "ERROR! Any string or characters after " +
								"action section will be ignored!",
						}}),
					}},
				}},
			}
			data.AddIterationHistory(reminder)
			scratchpad = append(scratchpad, reminder)
		}

		data.SetScratchPad(scratchpad)

		// Successful tool execution resets the repetition loop gauge.
		execCtx.Stats().ResetGauge(gent.SGRepetitionLoopConsecutive)

		return &gent.AgentLoopResult{
			Action:     gent.LAContinue,
			NextPrompt: observation,
		}, nil
	}

	// No actions present - check for termination
	if terminationContents, ok := parsed[r.termination.Name()]; ok && len(terminationContents) > 0 {
		var terminationParseErrors []string

		for _, content := range terminationContents {
			// First validate by calling ParseSection (traces errors for stats)
			_, termParseErr := r.termination.ParseSection(execCtx, content)
			if termParseErr != nil {
				terminationParseErrors = append(terminationParseErrors,
					fmt.Sprintf("Termination parse error: %v\nContent: %s", termParseErr, content))
				continue
			}

			// ParseSection succeeded, check if we should terminate
			result := r.termination.ShouldTerminate(execCtx, content)
			switch result.Status {
			case gent.TerminationAnswerAccepted:
				execCtx.Stats().ResetGauge(gent.SGRepetitionLoopConsecutive)
				iter := r.buildIteration(responseContent, "")
				data.AddIterationHistory(iter)
				return &gent.AgentLoopResult{
					Action: gent.LATerminate,
					Result: result.Content,
				}, nil

			case gent.TerminationAnswerRejected:
				// Build observation from rejection feedback
				var feedbackText string
				for _, part := range result.Content {
					if tc, ok := part.(llms.TextContent); ok {
						feedbackText += tc.Text + "\n"
					}
				}
				if feedbackText == "" {
					feedbackText = "Answer validation failed. Please try again."
				}

				observation := r.format.FormatSections([]gent.FormattedSection{
					{Name: "observation", Content: strings.TrimSpace(feedbackText)},
				})

				iter := r.buildIteration(responseContent, observation)
				data.AddIterationHistory(iter)

				scratchpad := data.GetScratchPad()
				scratchpad = append(scratchpad, iter)
				data.SetScratchPad(scratchpad)

				return &gent.AgentLoopResult{
					Action:     gent.LAContinue,
					NextPrompt: observation,
				}, nil

			case gent.TerminationContinue:
				// Continue checking other termination contents
				continue
			}
		}

		// If we had termination parse errors but no successful termination, feed back errors
		if len(terminationParseErrors) > 0 {
			errorContent := strings.Join(terminationParseErrors, "\n\n") +
				"\n\nPlease try again with proper formatting."
			observation := r.format.FormatSections([]gent.FormattedSection{
				{Name: "observation", Content: errorContent},
			})

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
	}

	// Handle parse error or no recognized sections — the model's response
	// could not be processed. Two iterations are added:
	//
	// 1. Expired iteration: the raw response + error message, logged for
	//    debugging but never sent back to the model (prevents hallucinated
	//    content from polluting the scratchpad).
	//
	// 2. System error iteration: a stern reminder that the model must follow
	//    the output format described in the system prompt. This iteration
	//    persists in the scratchpad so the model sees it on retry.
	//
	// Stats are already updated by format.Parse() which publishes
	// ParseErrorEvent before returning the error. Consecutive error limits
	// will terminate execution if this keeps happening.
	var errorMsg string
	if parseErr != nil {
		errorMsg = fmt.Sprintf("Format parse error: %v", parseErr)
	} else {
		errorMsg = "No recognized action or answer sections in response."
	}

	// 1. Expired iteration with raw response (for debugging only)
	expiredIter := r.buildIteration(responseContent, errorMsg)
	expiredIter.ExpireAfterIteration = max(execCtx.Iteration(), 1)
	data.AddIterationHistory(expiredIter)

	// 2. System error reminder (persists in scratchpad)
	systemError := r.format.FormatSections([]gent.FormattedSection{
		{Name: "system_error", Content: "You MUST follow the output format " +
			"described in the system prompt. Every response MUST contain " +
			"properly formatted sections. Do NOT fabricate tool outputs " +
			"or observations — only use the sections defined in your " +
			"instructions."},
	})
	errorIter := &gent.Iteration{
		Origin: gent.IterationSystemInjected,
		Messages: []*gent.MessageContent{
			{
				Role:  llms.ChatMessageTypeHuman,
				Parts: []gent.ContentPart{llms.TextContent{Text: systemError}},
			},
		},
	}
	data.AddIterationHistory(errorIter)

	scratchpad := data.GetScratchPad()
	scratchpad = append(scratchpad, expiredIter)
	scratchpad = append(scratchpad, errorIter)
	data.SetScratchPad(scratchpad)

	return &gent.AgentLoopResult{
		Action:     gent.LAContinue,
		NextPrompt: systemError,
	}, nil
}

// handleResponseTooLong processes a response that exceeded MaxResponseChars but does NOT
// contain repetition. The truncated response is expired, and a conciseness reminder is
// injected. No loop stats are incremented.
func (r *Agent) handleResponseTooLong(
	execCtx *gent.ExecutionContext, data gent.LoopData,
	response *gent.ContentResponse,
) (*gent.AgentLoopResult, error) {
	responseContent := ""
	if response != nil && len(response.Choices) > 0 {
		responseContent = response.Choices[0].Content
	}

	// Expired iteration with truncated response (debugging only).
	expiredIter := r.buildIteration(responseContent, "Response exceeded max length")
	expiredIter.ExpireAfterIteration = max(execCtx.Iteration(), 1)
	data.AddIterationHistory(expiredIter)

	// Conciseness reminder (persists in scratchpad).
	reminder := r.format.FormatSections([]gent.FormattedSection{
		{Name: "system_error", Content: r.responseTooLongMessage},
	})
	reminderIter := &gent.Iteration{
		Origin: gent.IterationSystemInjected,
		Messages: []*gent.MessageContent{{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []gent.ContentPart{llms.TextContent{Text: reminder}},
		}},
	}
	data.AddIterationHistory(reminderIter)

	scratchpad := data.GetScratchPad()
	scratchpad = append(scratchpad, expiredIter, reminderIter)
	data.SetScratchPad(scratchpad)

	return &gent.AgentLoopResult{Action: gent.LAContinue, NextPrompt: reminder}, nil
}

// handleRepetition processes a detected repetition loop. For RepetitionRecover, it
// increments stats, expires the poisoned response, and injects a recovery reminder.
// For RepetitionTerminate, it returns a fatal error.
func (r *Agent) handleRepetition(
	execCtx *gent.ExecutionContext, data gent.LoopData,
	response *gent.ContentResponse, rep *agentutil.RepetitionResult,
) (*gent.AgentLoopResult, error) {
	// Update stats.
	execCtx.Stats().IncrCounter(gent.SCRepetitionLoopTotal, 1)
	execCtx.Stats().IncrGauge(gent.SGRepetitionLoopConsecutive, 1)

	if r.repetitionConfig.Action == agentutil.RepetitionTerminate {
		return nil, fmt.Errorf("model call failed: %w", rep.Err)
	}

	// Recover: build the reminder message.
	cfg := r.repetitionConfig
	filtered := ""
	if cfg.Filter != nil && rep.RepeatedBlock != "" {
		filtered = cfg.Filter(rep.RepeatedBlock, cfg.PoisonKeywords)
	}

	count := execCtx.Stats().GetGauge(gent.SGRepetitionLoopConsecutive)
	var reminderText string
	if filtered != "" {
		reminderText = cfg.RecoverMessage
		reminderText = strings.Replace(reminderText, "{block}", filtered, 1)
		reminderText = strings.Replace(
			reminderText, "{count}",
			fmt.Sprintf("%d", int(count)), 1,
		)
	} else {
		reminderText = cfg.RecoverPoisonedMessage
	}

	// Extract raw response content for the expired iteration.
	responseContent := ""
	if response != nil && len(response.Choices) > 0 {
		responseContent = response.Choices[0].Content
	}

	// 1. Expired iteration with the truncated response (debugging only).
	expiredIter := r.buildIteration(responseContent, "Repetition loop detected")
	expiredIter.ExpireAfterIteration = max(execCtx.Iteration(), 1)
	data.AddIterationHistory(expiredIter)

	// 2. Recovery reminder (persists in scratchpad).
	reminder := r.format.FormatSections([]gent.FormattedSection{
		{Name: "system_error", Content: reminderText},
	})
	reminderIter := &gent.Iteration{
		Origin: gent.IterationSystemInjected,
		Messages: []*gent.MessageContent{{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []gent.ContentPart{llms.TextContent{Text: reminder}},
		}},
	}
	data.AddIterationHistory(reminderIter)

	scratchpad := data.GetScratchPad()
	scratchpad = append(scratchpad, expiredIter, reminderIter)
	data.SetScratchPad(scratchpad)

	return &gent.AgentLoopResult{Action: gent.LAContinue, NextPrompt: reminder}, nil
}

// hasExtraSections returns true if the parsed output contains sections beyond
// the expected action and optional thinking sections. This detects cases where
// the model appends extra content after the action section.
func (r *Agent) hasExtraSections(parsed map[string][]string) bool {
	for name, contents := range parsed {
		if len(contents) == 0 {
			continue
		}
		if name == r.toolChain.Name() {
			continue
		}
		if r.thinkingSection != nil && name == r.thinkingSection.Name() {
			continue
		}
		return true
	}
	return false
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

// buildMessages constructs the message list for the model call.
// Message structure:
//  1. System prompt (from SystemPromptBuilder + BeforeSystemPromptEvent) - x1
//  2. Task (role: user) - x1, text + media parts, panics if both empty
//  3. Scratchpad (N messages interleaved: role: AI, then role: human)
//  4. BEGIN!/CONTINUE! (role: user) - x1
func (r *Agent) buildMessages(
	execCtx *gent.ExecutionContext,
	data gent.LoopData,
	outputPrompt string,
	toolsPrompt string,
) []llms.MessageContent {
	var messages []llms.MessageContent

	// 1. System prompt: build sections, publish event, format
	ctx := SystemPromptContext{
		Format:             r.format,
		BehaviorAndContext: r.behaviorAndContext,
		CriticalRules:      r.criticalRules,
		OutputPrompt:       outputPrompt,
		ToolsPrompt:        toolsPrompt,
		Time:               r.timeProvider,
	}
	sections := r.systemPromptBuilder(ctx)

	// Publish BeforeSystemPromptEvent — subscribers may modify sections
	event := execCtx.PublishBeforeSystemPrompt(sections)
	sections = event.Sections

	systemContent := r.format.FormatSections(sections)
	messages = append(messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeSystem,
		Parts: []llms.ContentPart{llms.TextContent{Text: systemContent}},
	})

	// 2. Task message (role: user) - text + media parts
	messages = append(messages, r.buildTaskMessage(data))

	// 3. Scratchpad messages with deduplication
	scratchpadMsgs, includedIterations := agentutil.ScratchpadToMessages(
		data.GetScratchPad(),
		execCtx.Iteration(),
		r.toolChain,
		r.format,
	)
	messages = append(messages, scratchpadMsgs...)

	// 4. BEGIN!/CONTINUE! message (role: user)
	continueText := "BEGIN!"
	if includedIterations > 0 {
		continueText = "CONTINUE!"
	}
	messages = append(messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextContent{Text: continueText}},
	})

	return messages
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
		formattedText := r.format.FormatSections([]gent.FormattedSection{
			{Name: "task", Content: task.Text},
		})
		parts = append(parts, llms.TextContent{Text: formattedText})
	}

	// Add media parts
	parts = append(parts, agentutil.ToLLMParts(task.Media)...)

	return llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: parts,
	}
}

// executeToolCalls executes tool calls from the parsed
// action contents. Returns the ToolChainResult (for metadata
// storage on the iteration) and the formatted observation
// text (for NextPrompt).
func (r *Agent) executeToolCalls(
	execCtx *gent.ExecutionContext,
	contents []string,
) (*gent.ToolChainResult, string) {
	var allResults []*gent.ToolCallResult
	var errorSections []string

	for _, content := range contents {
		result, err := r.toolChain.Execute(
			execCtx, content, r.format,
		)
		if err != nil {
			errorText := r.format.FormatSections(
				[]gent.FormattedSection{
					{
						Name: "error",
						Content: fmt.Sprintf(
							"Error: %v", err,
						),
					},
				},
			)
			errorSections = append(
				errorSections, errorText,
			)
			continue
		}
		allResults = append(
			allResults, result.Results...,
		)
	}

	// Add error sections as synthetic ToolCallResults
	// so they appear in the observation text.
	for _, errText := range errorSections {
		allResults = append(allResults, &gent.ToolCallResult{
			Name:  "error",
			Error: fmt.Errorf("tool chain execution error"),
			Text:  errText,
		})
	}

	tcResult := &gent.ToolChainResult{Results: allResults}

	// Build observation text for NextPrompt
	var sections []string
	for _, tcr := range allResults {
		if tcr.Text != "" {
			sections = append(sections, tcr.Text)
		}
	}
	if len(sections) == 0 {
		return tcResult, ""
	}
	observation := r.format.FormatSections(
		[]gent.FormattedSection{
			{
				Name:    "observation",
				Content: strings.Join(sections, "\n"),
			},
		},
	)
	return tcResult, observation
}

// buildIteration creates an Iteration from response and observation.
// The response is stored as AI role, and observation as Human role.
// Note: We use Human role for observations because the text-based ReAct pattern
// doesn't use native tool calling APIs. The observation is a user message containing
// tool output in text form.
func (r *Agent) buildIteration(response, observation string) *gent.Iteration {
	var messages []*gent.MessageContent

	// Assistant message (response)
	messages = append(messages, &gent.MessageContent{
		Role:  llms.ChatMessageTypeAI,
		Parts: []gent.ContentPart{llms.TextContent{Text: response}},
	})

	// Observation message (Human role) - only if there's an observation
	if observation != "" {
		messages = append(messages, &gent.MessageContent{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []gent.ContentPart{llms.TextContent{Text: observation}},
		})
	}

	return &gent.Iteration{
		Messages: messages,
	}
}

// buildToolCallIteration creates an Iteration from an AI
// response and a ToolChainResult. The AI response is stored
// as the first message. The observation (from ToIteration)
// carries tool chain metadata for deduplication.
func (r *Agent) buildToolCallIteration(
	response string,
	tcResult *gent.ToolChainResult,
) *gent.Iteration {
	aiMsg := &gent.MessageContent{
		Role:  llms.ChatMessageTypeAI,
		Parts: []gent.ContentPart{
			llms.TextContent{Text: response},
		},
	}

	obsIter := tcResult.ToIteration(r.format)
	messages := append(
		[]*gent.MessageContent{aiMsg},
		obsIter.Messages...,
	)
	return &gent.Iteration{Messages: messages}
}

// callModel calls the model via streaming and returns the complete response.
// Also returns a non-nil RepetitionResult if a loop was detected.
func (r *Agent) callModel(
	execCtx *gent.ExecutionContext, streamId string,
	streamTopicId string, messages []llms.MessageContent,
) (*gent.ContentResponse, *agentutil.RepetitionResult, error) {
	opts := r.effectiveCallOptions()
	r.warnRestrictiveSampling(execCtx, opts)
	return r.callModelStreaming(execCtx, streamId, streamTopicId, messages, opts)
}

// effectiveCallOptions returns the merged call options: model-appropriate defaults followed
// by user-provided options. User options are applied last and override defaults (langchaingo
// applies CallOptions sequentially).
func (r *Agent) effectiveCallOptions() []llms.CallOption {
	params := gent.DefaultSamplingParams(r.model)

	var opts []llms.CallOption
	if v, ok := params.Temperature.EffectiveValue(); ok {
		opts = append(opts, llms.WithTemperature(v))
	}
	if v, ok := params.TopP.EffectiveValue(); ok {
		opts = append(opts, llms.WithTopP(v))
	}

	opts = append(opts, r.callOptions...)
	return opts
}

// warnRestrictiveSampling publishes a warning event when the effective temperature and
// top-p are both set to restrictive values. Vendor guidance (Anthropic, OpenAI) recommends
// adjusting one parameter at a time.
func (r *Agent) warnRestrictiveSampling(
	execCtx *gent.ExecutionContext, opts []llms.CallOption,
) {
	if execCtx == nil {
		return
	}

	// Resolve effective values by applying all options.
	var resolved llms.CallOptions
	for _, opt := range opts {
		opt(&resolved)
	}

	// Both restrictive: temperature < 0.3 (but set) and top-p < 0.8 (but set).
	// Zero values mean unset.
	if resolved.Temperature > 0 && resolved.Temperature < 0.3 &&
		resolved.TopP > 0 && resolved.TopP < 0.8 {
		execCtx.PublishCommonEvent(
			"gent:sampling_params_restrictive",
			fmt.Sprintf(
				"temperature (%.2f) and top-p (%.2f) are both restrictive; "+
					"adjust one at a time",
				resolved.Temperature, resolved.TopP,
			),
			nil,
		)
	}

	// Warn if user sets forbidden params. Only check user-provided options
	// (r.callOptions), not the full merged set — the framework itself sends
	// temperature=1.0 for forbidden models as a langchaingo workaround.
	params := gent.DefaultSamplingParams(r.model)
	var userResolved llms.CallOptions
	for _, opt := range r.callOptions {
		opt(&userResolved)
	}
	if params.Temperature.Directive == gent.ParamForbidden &&
		userResolved.Temperature > 0 {
		execCtx.PublishCommonEvent(
			"gent:sampling_param_forbidden",
			fmt.Sprintf(
				"temperature (%.2f) is forbidden for this model "+
					"and may cause an API error",
				userResolved.Temperature,
			),
			nil,
		)
	}
	if params.TopP.Directive == gent.ParamForbidden &&
		userResolved.TopP > 0 {
		execCtx.PublishCommonEvent(
			"gent:sampling_param_forbidden",
			fmt.Sprintf(
				"top-p (%.2f) is forbidden for this model "+
					"and may cause an API error",
				userResolved.TopP,
			),
			nil,
		)
	}
}

// callModelStreaming calls the model with streaming and applies repetition detection.
// Returns the response plus an optional RepetitionResult if a loop or max-size was hit.
func (r *Agent) callModelStreaming(
	execCtx *gent.ExecutionContext,
	streamId string, streamTopicId string,
	messages []llms.MessageContent, opts []llms.CallOption,
) (*gent.ContentResponse, *agentutil.RepetitionResult, error) {
	stream, err := r.model.GenerateContentStream(
		execCtx, streamId, streamTopicId, messages, opts...,
	)
	if err != nil {
		return nil, nil, err
	}

	acc := gent.NewStreamAccumulator()
	detector := agentutil.NewRepetitionDetector(r.repetitionConfig)
	totalChars := 0

	for chunk := range stream.Chunks() {
		if chunk.Err != nil {
			return nil, nil, chunk.Err
		}
		acc.Add(chunk)
		totalChars += len(chunk.Content)

		// Real-time repetition detection.
		if result := detector.Feed(chunk.Content); result != nil {
			stream.Close()
			streamResp, _ := stream.Response()
			return acc.ResponseWithInfo(streamResp), result, nil
		}

		// Max response size: truncate and inspect for repetition.
		if r.maxResponseChars > 0 && totalChars > r.maxResponseChars {
			stream.Close()
			streamResp, _ := stream.Response()
			resp := acc.ResponseWithInfo(streamResp)

			// Two-step check: is the truncated content a loop or just too long?
			if repResult := detector.CheckAccumulated(); repResult != nil {
				return resp, repResult, nil
			}
			return resp, &agentutil.RepetitionResult{Err: agentutil.ErrMaxResponseSize}, nil
		}
	}

	streamResponse, err := stream.Response()
	if err != nil {
		return nil, nil, err
	}
	return acc.ResponseWithInfo(streamResponse), nil, nil
}

// Compile-time check that Agent implements gent.AgentLoop.
var _ gent.AgentLoop[*gent.BasicLoopData] = (*Agent)(nil)
