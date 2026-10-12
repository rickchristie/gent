package react

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

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
//  1. System prompt(s) - from SystemPromptBuilder (behavior, rules, tools, and output format)
//  2. Task message (user) - formatted task text + media
//  3. Scratchpad messages - interleaved AI responses and observations from previous iterations
//  4. BEGIN!/CONTINUE! (user) - signals start or continuation of the loop
//
// System prompts can be customized via WithSystemPromptBuilder() for full control over prompting.
// NewNativeAgent uses API tool calls and plain final content with the same execution loop.
type Agent struct {
	behaviorAndContext     string
	criticalRules          string
	systemPromptBuilder    SystemPromptBuilder
	model                  gent.Model
	format                 gent.TextFormat
	toolChain              gent.ToolChain
	termination            gent.Termination
	thinkingSection        gent.TextSection
	timeProvider           gent.TimeProvider
	callOptions            []llms.CallOption
	userSampling           llms.CallOptions
	repetitionConfig       agentutil.RepetitionConfig
	maxResponseChars       int
	responseTooLongMessage string
	recoveryVersion        string
	nativeToolCalls        bool
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

// WithFormat sets the text output format. Native agents use it to render prompts and tool results;
// they do not parse the assistant response with this format.
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

// WithClock sets the clock used by all standard time helpers in prompt templates.
// Use WithTimeProvider to also change how dates and times are formatted.
func (r *Agent) WithClock(now func() time.Time) *Agent {
	return r.WithTimeProvider(gent.TimeProviderFunc(now))
}

// TimeProvider returns the current time provider.
func (r *Agent) TimeProvider() gent.TimeProvider {
	return r.timeProvider
}

// WithThinking enables the thinking section with the given guidance for NewAgent.
// NewNativeAgent does not request or parse thought sections. Model reasoning controls remain
// provider options, separate from visible text that asks the model to explain its work.
func (r *Agent) WithThinking(guidance string) *Agent {
	r.thinkingSection = section.NewText("thinking").
		WithGuidance(guidance)
	return r
}

// WithThinkingSection sets a custom thinking section for NewAgent.
// Native mode ignores this section.
func (r *Agent) WithThinkingSection(s gent.TextSection) *Agent {
	r.thinkingSection = s
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

// WithCallOptions resolves the supplied functions once into reusable configuration. The saved
// compatibility manifest and every request therefore use the same option values.
func (r *Agent) WithCallOptions(options ...llms.CallOption) *Agent {
	resolved := gent.NewModelCallRequest(nil).Options
	// NaN distinguishes an omitted sampling parameter from an explicit zero without evaluating
	// an option function twice. Replace the sentinels before using or encoding the configuration.
	resolved.Temperature, resolved.TopP = math.NaN(), math.NaN()
	for _, option := range options {
		option(&resolved)
	}
	r.userSampling = llms.CallOptions{}
	defaults := gent.DefaultSamplingParams(r.model)
	if math.IsNaN(resolved.Temperature) {
		resolved.Temperature, _ = defaults.Temperature.EffectiveValue()
	} else {
		r.userSampling.Temperature = resolved.Temperature
	}
	if math.IsNaN(resolved.TopP) {
		resolved.TopP, _ = defaults.TopP.EffectiveValue()
	} else {
		r.userSampling.TopP = resolved.TopP
	}
	r.callOptions = []llms.CallOption{llms.WithOptions(resolved)}
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
func (r *Agent) next(execCtx *gent.ExecutionContext) (*gent.AgentLoopResult, error) {
	data := execCtx.Data()
	frame, err := r.loadFrame(execCtx)
	if err != nil {
		return nil, err
	}

	outputPrompt, toolsPrompt := "", ""
	if !r.nativeToolCalls {
		// Text agents keep their configured section contract.
		for _, section := range r.buildOutputSections() {
			r.format.RegisterSection(section)
		}
		outputPrompt = r.format.DescribeStructure()
		toolsPrompt = r.toolChain.AvailableToolsPrompt()
	}

	if frame.Response == nil {
		messages, err := r.buildMessages(execCtx, data, outputPrompt, toolsPrompt)
		if err != nil {
			return nil, err
		}
		streamId := fmt.Sprintf("iter-%d", execCtx.Iteration())
		response, repetition, err := r.callModel(execCtx, streamId, "llm-response", messages)
		if err != nil {
			return nil, fmt.Errorf("model call failed: %w", err)
		}
		frame.Response, frame.Repetition = response, repetition
		if err := saveFrame(execCtx, frame); err != nil {
			return nil, err
		}
	}
	response, repResult := frame.Response, frame.Repetition

	// Handle repetition or max-size truncation.
	if repResult != nil {
		if errors.Is(repResult.Err, agentutil.ErrMaxResponseSize) {
			return r.handleResponseTooLong(execCtx, data, response)
		}
		return r.handleRepetition(execCtx, data, response, repResult)
	}
	if r.nativeToolCalls {
		return r.nextNative(execCtx, frame)
	}

	// Extract response content
	responseContent := ""
	if len(response.Choices) > 0 {
		responseContent = response.Choices[0].Content
	}

	// Parse complete response to identify all available sections
	// The format handles tracing of parse errors and resetting consecutive counter
	parsed, parseErr := frame.Sections, frame.ParseError
	if !frame.Parsed {
		parsed, parseErr = r.format.Parse(execCtx, responseContent)

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

		frame.Sections, frame.ParseError, frame.Parsed = parsed, parseErr, true
		if err := saveFrame(execCtx, frame); err != nil {
			return nil, err
		}
	}

	// Check for action (tool calls) section first - actions take priority over termination
	// This ensures tools are executed even if the model also outputs an answer
	actionContents, hasActions := parsed[r.toolChain.Name()]
	if hasActions && len(actionContents) > 0 {
		// Execute tool calls (automatically traced via execCtx)
		tcResult, observation, err := r.executeToolCalls(
			execCtx, frame, actionContents,
		)
		if err != nil {
			return nil, err
		}

		// Group the AI response and tool observations in one context record.
		iter := r.buildToolCallIteration(
			responseContent, tcResult,
		)
		if err := execCtx.AppendRecord(iter, true); err != nil {
			return nil, err
		}

		// Add to scratchpad for next call

		// Check for violations: action + answer, or extra sections after action
		_, hasAnswer := parsed[r.termination.Name()]
		if hasAnswer && len(parsed[r.termination.Name()]) > 0 {
			reminder := &gent.HistoryEntry{
				Origin: gent.RecordSystemInjected,
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
			if err := execCtx.AppendRecord(reminder, true); err != nil {
				return nil, err
			}
		} else if r.hasExtraSections(parsed) {
			reminder := &gent.HistoryEntry{
				Origin: gent.RecordSystemInjected,
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
			if err := execCtx.AppendRecord(reminder, true); err != nil {
				return nil, err
			}
		}

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
			if result.Error != nil {
				return nil, result.Error
			}
			switch result.Status {
			case gent.TerminationAnswerAccepted:
				execCtx.Stats().ResetGauge(gent.SGRepetitionLoopConsecutive)
				iter := r.buildIteration(responseContent, "")
				if err := execCtx.AppendRecord(iter, false); err != nil {
					return nil, err
				}
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
				if err := execCtx.AppendRecord(iter, true); err != nil {
					return nil, err
				}

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
			if err := execCtx.AppendRecord(iter, true); err != nil {
				return nil, err
			}

			return &gent.AgentLoopResult{
				Action:     gent.LAContinue,
				NextPrompt: observation,
			}, nil
		}
	}

	// Handle parse error or no recognized sections — the model's response
	// could not be processed. Two context records are added within this one loop iteration:
	//
	// 1. Expired record: the raw response + error message, logged for
	//    debugging but never sent back to the model (prevents hallucinated
	//    content from polluting the scratchpad).
	//
	// 2. System error record: a stern reminder that the model must follow
	//    the output format described in the system prompt. This record
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

	// 1. Expired record with raw response (for debugging only)
	expiredIter := r.buildIteration(responseContent, errorMsg)
	expiredIter.ExpireAfterIteration = max(execCtx.Iteration(), 1)
	if err := execCtx.AppendRecord(expiredIter, true); err != nil {
		return nil, err
	}

	// 2. System error reminder (persists in scratchpad)
	systemError := r.format.FormatSections([]gent.FormattedSection{
		{Name: "system_error", Content: "You MUST follow the output format " +
			"described in the system prompt. Every response MUST contain " +
			"properly formatted sections. Do NOT fabricate tool outputs " +
			"or observations — only use the sections defined in your " +
			"instructions."},
	})
	errorIter := &gent.HistoryEntry{
		Origin: gent.RecordSystemInjected,
		Messages: []*gent.MessageContent{
			{
				Role:  llms.ChatMessageTypeHuman,
				Parts: []gent.ContentPart{llms.TextContent{Text: systemError}},
			},
		},
	}
	if err := execCtx.AppendRecord(errorIter, true); err != nil {
		return nil, err
	}

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

	// Expired record with truncated response (debugging only).
	expiredIter := r.buildIteration(responseContent, "Response exceeded max length")
	expiredIter.ExpireAfterIteration = max(execCtx.Iteration(), 1)
	if err := execCtx.AppendRecord(expiredIter, true); err != nil {
		return nil, err
	}

	// Conciseness reminder (persists in scratchpad).
	reminder := r.format.FormatSections([]gent.FormattedSection{
		{Name: "system_error", Content: r.responseTooLongMessage},
	})
	reminderIter := &gent.HistoryEntry{
		Origin: gent.RecordSystemInjected,
		Messages: []*gent.MessageContent{{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []gent.ContentPart{llms.TextContent{Text: reminder}},
		}},
	}
	if err := execCtx.AppendRecord(reminderIter, true); err != nil {
		return nil, err
	}

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

	// 1. Expired record with the truncated response (debugging only).
	expiredIter := r.buildIteration(responseContent, "Repetition loop detected")
	expiredIter.ExpireAfterIteration = max(execCtx.Iteration(), 1)
	if err := execCtx.AppendRecord(expiredIter, true); err != nil {
		return nil, err
	}

	// 2. Recovery reminder (persists in scratchpad).
	reminder := r.format.FormatSections([]gent.FormattedSection{
		{Name: "system_error", Content: reminderText},
	})
	reminderIter := &gent.HistoryEntry{
		Origin: gent.RecordSystemInjected,
		Messages: []*gent.MessageContent{{
			Role:  llms.ChatMessageTypeHuman,
			Parts: []gent.ContentPart{llms.TextContent{Text: reminder}},
		}},
	}
	if err := execCtx.AppendRecord(reminderIter, true); err != nil {
		return nil, err
	}

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
func (r *Agent) buildOutputSections() []gent.TextSection {
	var sections []gent.TextSection

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
) ([]llms.MessageContent, error) {
	if r.nativeToolCalls {
		return r.buildNativeMessages(execCtx)
	}
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
	task, err := execCtx.Task()
	if err != nil {
		return nil, err
	}
	messages = append(messages, r.buildTaskMessage(gent.NewBasicLoopData(task)))

	// 3. Scratchpad messages with deduplication
	records, err := execCtx.Scratchpad()
	if err != nil {
		return nil, err
	}
	scratchpadMsgs, includedIterations := agentutil.ScratchpadToMessages(
		records,
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

	return messages, execCtx.RecordingError()
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
	execCtx *gent.ExecutionContext, frame *iterationFrame,
	contents []string,
) (*gent.ToolChainResult, string, error) {
	allResults := frame.ToolResults
	errorSections := frame.ToolErrors

	for frame.ToolContent < len(contents) {
		result, err := r.executeRecoverableToolContent(execCtx, frame, contents[frame.ToolContent])
		if recordErr := execCtx.RecordingError(); recordErr != nil {
			return nil, "", recordErr
		}
		if execCtx.IsWorkerInterrupted() {
			return nil, "", execCtx.Context().Err()
		}
		if errors.Is(err, gent.ErrUncertainTool) || errors.Is(err, gent.ErrUnsupportedRecovery) {
			return nil, "", err
		}
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
			frame.ToolErrors = errorSections
			frame.ToolContent++
			frame.ToolPlanActive = false
			if err := saveFrame(execCtx, frame); err != nil {
				return nil, "", err
			}
			continue
		}
		allResults = append(allResults, result.Results...)
		frame.ToolResults = allResults
		frame.ToolContent++
		frame.ToolPlanActive = false
		if err := saveFrame(execCtx, frame); err != nil {
			return nil, "", err
		}
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
		return tcResult, "", nil
	}
	observation := r.format.FormatSections(
		[]gent.FormattedSection{
			{
				Name:    "observation",
				Content: strings.Join(sections, "\n"),
			},
		},
	)
	return tcResult, observation, nil
}

// buildIteration groups a response and observation in one HistoryEntry.
// The response is stored as AI role, and observation as Human role.
// Note: We use Human role for observations because the text-based ReAct pattern
// doesn't use native tool calling APIs. The observation is a user message containing
// tool output in text form.
func (r *Agent) buildIteration(response, observation string) *gent.HistoryEntry {
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

	return &gent.HistoryEntry{
		Messages: messages,
	}
}

// buildToolCallIteration creates a HistoryEntry from an AI
// response and a ToolChainResult. The AI response is stored
// as the first message. The observation (from ToHistoryEntry)
// carries tool chain metadata for deduplication.
func (r *Agent) buildToolCallIteration(
	response string,
	tcResult *gent.ToolChainResult,
) *gent.HistoryEntry {
	aiMsg := &gent.MessageContent{
		Role: llms.ChatMessageTypeAI,
		Parts: []gent.ContentPart{
			llms.TextContent{Text: response},
		},
	}

	obsIter := tcResult.ToHistoryEntry(r.format)
	messages := append(
		[]*gent.MessageContent{aiMsg},
		obsIter.Messages...,
	)
	return &gent.HistoryEntry{Messages: messages}
}

// callModel calls the model via streaming and returns the complete response.
// Also returns a non-nil RepetitionResult if a loop was detected.
func (r *Agent) callModel(
	execCtx *gent.ExecutionContext, streamId string,
	streamTopicId string, messages []llms.MessageContent,
) (*gent.ContentResponse, *agentutil.RepetitionResult, error) {
	opts := r.effectiveCallOptions()
	if r.nativeToolCalls {
		chain, err := r.nativeChain()
		if err != nil {
			return nil, nil, err
		}
		tools, err := chain.NativeTools(execCtx)
		if err != nil {
			return nil, nil, err
		}
		// The registry owns the active catalog. Other caller options keep their current behavior.
		opts = append(opts, llms.WithTools(tools))
	}
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
	userResolved := r.userSampling
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
	stream, err := execCtx.GenerateContentStream(
		r.model,
		gent.ModelCall{StreamId: streamId, StreamTopicId: streamTopicId},
		gent.NewModelCallRequest(messages, opts...),
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
	// The final response can include content or choice data that is absent from chunks.
	// Use the accumulator only when repetition or size limits stop the stream early.
	// Check final-only content too. Some adapters stream no text when they return tool calls.
	if len(streamResponse.Choices) > 0 && streamResponse.Choices[0] != nil {
		choice := streamResponse.Choices[0]
		content := choice.Content
		for _, call := range choice.ToolCalls {
			if call.FunctionCall != nil {
				content += call.FunctionCall.Arguments
			}
		}
		finalDetector := agentutil.NewRepetitionDetector(r.repetitionConfig)
		if repetition := finalDetector.Feed(content); repetition != nil {
			return streamResponse, repetition, nil
		}
		if r.maxResponseChars > 0 && len(content) > r.maxResponseChars {
			if repetition := finalDetector.CheckAccumulated(); repetition != nil {
				return streamResponse, repetition, nil
			}
			return streamResponse, &agentutil.RepetitionResult{Err: agentutil.ErrMaxResponseSize}, nil
		}
	}
	return streamResponse, nil, nil
}

// Compile-time check that Agent implements gent.Loop.
var _ gent.Loop = (*Agent)(nil)
