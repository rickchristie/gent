package react

import (
	"errors"
	"fmt"
	"strings"

	"github.com/rickchristie/gent"
	agentutil "github.com/rickchristie/gent/agents"
	"github.com/rickchristie/gent/toolchain"
	"github.com/tmc/langchaingo/llms"
)

// NewNativeAgent uses native tool calls and plain final assistant content.
// It shares execution, history, limits, and validation with NewAgent. The text constructor keeps
// its existing defaults. Native mode uses TextFormat only to render prompts and tool results.
func NewNativeAgent(model gent.Model) *Agent {
	agent := NewAgent(model)
	agent.nativeToolCalls = true
	agent.toolChain = toolchain.NewJSON()
	agent.systemPromptBuilder = NativeSystemPromptBuilder
	return agent
}

func (r *Agent) nativeChain() (gent.NativeToolChain, error) {
	chain, ok := r.toolChain.(gent.NativeToolChain)
	if !ok {
		return nil, fmt.Errorf("tool chain %T does not support native calls", r.toolChain)
	}
	return chain, nil
}

// NativeSystemPromptBuilder leaves call syntax to the model API. It does not require a thought
// transcript or output sections. Custom builders and prompt hooks use the existing contract.
func NativeSystemPromptBuilder(ctx SystemPromptContext) []gent.FormattedSection {
	var sections []gent.FormattedSection
	if ctx.BehaviorAndContext != "" {
		sections = append(sections, gent.FormattedSection{
			Name: "behavior", Content: ctx.BehaviorAndContext,
		})
	}
	sections = append(sections, gent.FormattedSection{
		Name: "instructions",
		Content: `Use the available tools to get facts and complete the task.
Check tool errors before you continue. Do not invent tool results.
Give the final answer when you have enough information.`,
	})
	if ctx.CriticalRules != "" {
		sections = append(sections, gent.FormattedSection{
			Name: "critical_rules", Content: ctx.CriticalRules,
		})
	}
	if ctx.OutputPrompt != "" {
		sections = append(sections, gent.FormattedSection{
			Name: "answer_requirements", Content: ctx.OutputPrompt,
		})
	}
	return sections
}

func (r *Agent) buildNativeMessages(ctx *gent.ExecutionContext) ([]llms.MessageContent, error) {
	sections := r.systemPromptBuilder(SystemPromptContext{
		Format: r.format, BehaviorAndContext: r.behaviorAndContext,
		CriticalRules: r.criticalRules, OutputPrompt: r.termination.Guidance(), Time: r.timeProvider,
	})
	event := ctx.PublishBeforeSystemPrompt(sections)
	messages := []llms.MessageContent{llms.TextParts(llms.ChatMessageTypeSystem,
		r.format.FormatSections(event.Sections))}
	task, err := ctx.Task()
	if err != nil {
		return nil, err
	}
	if task == nil || (task.Text == "" && len(task.Media) == 0) {
		panic("task must have either text or media content")
	}
	messages = append(messages, llms.MessageContent{
		Role: llms.ChatMessageTypeHuman, Parts: agentutil.ToLLMParts(task.AsContentParts()),
	})
	records, err := ctx.Scratchpad()
	if err != nil {
		return nil, err
	}
	history, _ := agentutil.ScratchpadToMessages(records, ctx.Iteration(), r.toolChain, r.format)
	messages = append(messages, nativeHistoryMessages(history)...)
	return messages, ctx.RecordingError()
}

// The current provider adapters accept one ToolCallResponse per tool message. Keep media in
// history with its result for deduplication, then send it after the complete tool response group.
func nativeHistoryMessages(history []llms.MessageContent) []llms.MessageContent {
	var messages []llms.MessageContent
	var media []llms.ContentPart
	flushMedia := func() {
		if len(media) > 0 {
			messages = append(messages, llms.MessageContent{
				Role: llms.ChatMessageTypeHuman, Parts: media,
			})
			media = nil
		}
	}
	for _, message := range history {
		if message.Role != llms.ChatMessageTypeTool {
			flushMedia()
			messages = append(messages, message)
			continue
		}
		if len(message.Parts) > 1 {
			if call, ok := message.Parts[0].(llms.ToolCallResponse); ok {
				media = append(media, llms.TextContent{Text: fmt.Sprintf(
					"Media from %s (call %s):", call.Name, call.ToolCallID)})
				media = append(media, message.Parts[1:]...)
				message.Parts = message.Parts[:1]
			}
		}
		messages = append(messages, message)
	}
	flushMedia()
	return messages
}

func (r *Agent) nextNative(
	ctx *gent.ExecutionContext, frame *iterationFrame,
) (*gent.AgentLoopResult, error) {
	if len(frame.Response.Choices) == 0 || frame.Response.Choices[0] == nil {
		return r.nativeRetry(ctx, "", "The model returned no response. Please try again.")
	}
	choice := frame.Response.Choices[0]
	if nativeResponseTruncated(choice.StopReason) {
		return r.nativeRetry(ctx, choice.Content,
			"The response was incomplete. Use a shorter answer or fewer tool calls.")
	}
	if len(choice.ToolCalls) > 0 {
		return r.nextNativeTools(ctx, frame, choice)
	}
	if choice.FuncCall != nil {
		return nil, errors.New("native agents require ToolCalls, not the legacy FuncCall field")
	}
	return r.nextNativeAnswer(ctx, choice.Content)
}

// Adapters retain provider stop reasons. Check both Google API and SDK forms, and the
// Anthropic context limit, before tool execution or answer validation can accept partial data.
func nativeResponseTruncated(reason string) bool {
	switch reason {
	case "length", "max_tokens", "model_context_window_exceeded", "MAX_TOKENS",
		"FinishReasonMaxTokens":
		return true
	default:
		return false
	}
}

func (r *Agent) nextNativeTools(
	ctx *gent.ExecutionContext, frame *iterationFrame, choice *gent.ContentChoice,
) (*gent.AgentLoopResult, error) {
	chain, err := r.nativeChain()
	if err != nil {
		return nil, err
	}
	if !frame.ToolPlanActive {
		if err := normalizeCallIDs(ctx, choice.ToolCalls); err != nil {
			ctx.PublishParseError(gent.ParseErrorTypeToolchain, choice.Content, err)
			return r.nativeRetry(ctx, choice.Content, err.Error())
		}
	}
	result, err := r.executeToolPlan(ctx, frame, func() (*gent.ToolPlan, error) {
		return chain.PrepareCalls(ctx, choice.ToolCalls)
	}, chain.ExecuteNext)
	if recordErr := ctx.RecordingError(); recordErr != nil {
		return nil, recordErr
	}
	if ctx.Context().Err() != nil {
		return nil, ctx.Context().Err()
	}
	if errors.Is(err, gent.ErrUncertainTool) || errors.Is(err, gent.ErrUnsupportedRecovery) {
		return nil, err
	}
	if err != nil {
		if frame.ToolPlanActive {
			// A failed saved plan can contain effects. Keep it for explicit recovery.
			return nil, err
		}
		return r.nativeRetry(ctx, choice.Content, err.Error())
	}
	entry := nativeToolEntry(choice, result)
	if err := ctx.AppendRecord(entry, true); err != nil {
		return nil, err
	}
	ctx.Stats().ResetGauge(gent.SGRepetitionLoopConsecutive)
	var text []string
	for _, call := range result.Results {
		text = append(text, call.Text)
	}
	return &gent.AgentLoopResult{Action: gent.LAContinue, NextPrompt: strings.Join(text, "\n")}, nil
}

func normalizeCallIDs(ctx *gent.ExecutionContext, calls []llms.ToolCall) error {
	seen := map[string]bool{}
	for i := range calls {
		if calls[i].ID == "" {
			calls[i].ID = "native-" + ctx.NewToolCallId()
		}
		if seen[calls[i].ID] {
			return fmt.Errorf("duplicate model tool call ID %q", calls[i].ID)
		}
		seen[calls[i].ID] = true
		if calls[i].Type == "" {
			calls[i].Type = "function"
		}
	}
	return nil
}

func nativeToolEntry(
	choice *gent.ContentChoice, result *gent.ToolChainResult,
) *gent.HistoryEntry {
	ai := &gent.MessageContent{Role: llms.ChatMessageTypeAI}
	if choice.Content != "" {
		ai.Parts = append(ai.Parts, llms.TextContent{Text: choice.Content})
	}
	for _, call := range choice.ToolCalls {
		ai.Parts = append(ai.Parts, call)
	}
	entry := &gent.HistoryEntry{Messages: []*gent.MessageContent{ai}}
	for i, call := range choice.ToolCalls {
		output := result.Results[i]
		message := &gent.MessageContent{Role: llms.ChatMessageTypeTool,
			Parts: []gent.ContentPart{llms.ToolCallResponse{ToolCallID: call.ID,
				Name: call.FunctionCall.Name, Content: output.Text}}}
		message.Parts = append(message.Parts, output.Media...)
		metadata := &gent.ToolChainResult{Results: []*gent.ToolCallResult{output}}
		if nested, ok := output.Output.(*gent.ToolChainResult); ok {
			metadata = nested
		}
		message.SetMetadata(gent.MMKToolChainResult, metadata)
		entry.Messages = append(entry.Messages, message)
	}
	return entry
}

func (r *Agent) nextNativeAnswer(
	ctx *gent.ExecutionContext, content string,
) (*gent.AgentLoopResult, error) {
	if _, err := r.termination.ParseSection(ctx, content); err != nil {
		return r.nativeFeedback(ctx, content, fmt.Sprintf("Invalid answer: %v", err))
	}
	result := r.termination.ShouldTerminate(ctx, content)
	if result.Error != nil {
		return nil, result.Error
	}
	switch result.Status {
	case gent.TerminationAnswerAccepted:
		ctx.Stats().ResetGauge(gent.SGRepetitionLoopConsecutive)
		if err := ctx.AppendRecord(r.buildIteration(content, ""), false); err != nil {
			return nil, err
		}
		return &gent.AgentLoopResult{Action: gent.LATerminate, Result: result.Content}, nil
	case gent.TerminationAnswerRejected:
		var feedback []string
		for _, part := range result.Content {
			if text, ok := part.(llms.TextContent); ok {
				feedback = append(feedback, text.Text)
			}
		}
		message := strings.TrimSpace(strings.Join(feedback, "\n"))
		if message == "" {
			message = "Answer validation failed. Please try again."
		}
		return r.nativeFeedback(ctx, content, message)
	default:
		return r.nativeFeedback(ctx, content, "Use a tool or give a final answer.")
	}
}

func (r *Agent) nativeFeedback(
	ctx *gent.ExecutionContext, content, feedback string,
) (*gent.AgentLoopResult, error) {
	if err := ctx.AppendRecord(r.buildIteration(content, feedback), true); err != nil {
		return nil, err
	}
	return &gent.AgentLoopResult{Action: gent.LAContinue, NextPrompt: feedback}, nil
}

func (r *Agent) nativeRetry(
	ctx *gent.ExecutionContext, content, feedback string,
) (*gent.AgentLoopResult, error) {
	// Keep the rejected response for inspection. Do not send incomplete call groups to the model.
	entry := r.buildIteration(content, "")
	entry.ExpireAfterIteration = max(ctx.Iteration(), 1)
	if err := ctx.AppendRecord(entry, true); err != nil {
		return nil, err
	}
	reminder := &gent.HistoryEntry{Origin: gent.RecordSystemInjected,
		Messages: []*gent.MessageContent{{Role: llms.ChatMessageTypeHuman,
			Parts: []gent.ContentPart{llms.TextContent{Text: feedback}}}}}
	if err := ctx.AppendRecord(reminder, true); err != nil {
		return nil, err
	}
	return &gent.AgentLoopResult{Action: gent.LAContinue, NextPrompt: feedback}, nil
}
