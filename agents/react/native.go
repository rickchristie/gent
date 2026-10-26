package react

import (
	"errors"
	"fmt"
	"strings"

	"github.com/rickchristie/gent"
	agentutil "github.com/rickchristie/gent/agents"
)

// DefaultSystemPromptBuilder leaves call syntax to the model API. It does not require a thought
// transcript or output sections. Custom builders and prompt hooks use the existing contract.
func DefaultSystemPromptBuilder(ctx *SystemPromptContext) []gent.FormattedSection {
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

func (r *Agent) buildMessages(ctx *gent.ExecutionContext) ([]gent.ModelMessage, error) {
	sections := r.systemPromptBuilder(&SystemPromptContext{
		Format: r.format, BehaviorAndContext: r.behaviorAndContext,
		CriticalRules: r.criticalRules, OutputPrompt: r.termination.Guidance(), Time: r.timeProvider,
	})
	event := ctx.PublishBeforeSystemPrompt(sections)
	messages := []gent.ModelMessage{gent.TextParts(gent.ChatMessageTypeSystem,
		r.format.FormatSections(event.Sections))}
	task, err := ctx.Task()
	if err != nil {
		return nil, err
	}
	if task == nil || (task.Text == "" && len(task.Media) == 0) {
		panic("task must have either text or media content")
	}
	messages = append(messages, gent.ModelMessage{
		Role: gent.ChatMessageTypeHuman, Parts: task.AsContentParts(),
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
func nativeHistoryMessages(history []gent.ModelMessage) []gent.ModelMessage {
	var messages []gent.ModelMessage
	var media []gent.ContentPart
	flushMedia := func() {
		if len(media) > 0 {
			messages = append(messages, gent.ModelMessage{
				Role: gent.ChatMessageTypeHuman, Parts: media,
			})
			media = nil
		}
	}
	for _, message := range history {
		if message.Role != gent.ChatMessageTypeTool {
			flushMedia()
			messages = append(messages, message)
			continue
		}
		if len(message.Parts) > 1 {
			if call, ok := message.Parts[0].(gent.ToolCallResponse); ok {
				media = append(media, gent.TextContent{Text: fmt.Sprintf(
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
	if choice.State() == gent.ModelIncomplete {
		return r.nativeRetry(ctx, choice.CandidateContent,
			"The response was incomplete. Use a shorter answer or fewer tool calls.")
	}
	if choice.State() == gent.ModelFailed {
		return nil, fmt.Errorf("model stopped without a usable response: %q", choice.StopReason)
	}
	if choice.State() == gent.ModelContinue {
		if choice.Continuation == nil {
			return nil, fmt.Errorf("model continuation %q requires a lossless adapter", choice.StopReason)
		}
		if err := ctx.AppendRecord(r.answerEntry(choice, ""), true); err != nil {
			return nil, err
		}
		return &gent.AgentLoopResult{Action: gent.LAContinue}, nil
	}
	if len(choice.ToolCalls) > 0 {
		return r.nextNativeTools(ctx, frame, choice)
	}

	return r.nextNativeAnswer(ctx, choice)
}

func (r *Agent) nextNativeTools(
	ctx *gent.ExecutionContext, frame *iterationFrame, choice *gent.ContentChoice,
) (*gent.AgentLoopResult, error) {
	chain := r.toolChain
	if !frame.ToolPlanActive {
		if err := normalizeCallIDs(ctx, choice.ToolCalls); err != nil {
			ctx.PublishParseError(gent.ParseErrorTypeToolchain, choice.CandidateContent, err)
			return r.nativeRetry(ctx, choice.CandidateContent, err.Error())
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
		return r.nativeRetry(ctx, choice.CandidateContent, err.Error())
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

func normalizeCallIDs(ctx *gent.ExecutionContext, calls []gent.ModelToolCall) error {
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
	ai := &gent.MessageContent{Role: gent.ChatMessageTypeAI, Continuation: choice.Continuation}
	if choice.CandidateContent != "" {
		ai.Parts = append(ai.Parts, gent.TextContent{Text: choice.CandidateContent})
	}
	for _, call := range choice.ToolCalls {
		ai.Parts = append(ai.Parts, call)
	}
	entry := &gent.HistoryEntry{Messages: []*gent.MessageContent{ai}}
	for i, call := range choice.ToolCalls {
		output := result.Results[i]
		message := &gent.MessageContent{Role: gent.ChatMessageTypeTool,
			Parts: []gent.ContentPart{gent.ToolCallResponse{ToolCallID: call.ID,
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
	ctx *gent.ExecutionContext, choice *gent.ContentChoice,
) (*gent.AgentLoopResult, error) {
	content := choice.CandidateContent
	if _, err := r.termination.ParseSection(ctx, content); err != nil {
		return r.nativeFeedback(ctx, choice, fmt.Sprintf("Invalid answer: %v", err))
	}
	result := r.termination.ShouldTerminate(ctx, content)
	if result.Error != nil {
		return nil, result.Error
	}
	switch result.Status {
	case gent.TerminationAnswerAccepted:
		ctx.Stats().ResetGauge(gent.SGRepetitionLoopConsecutive)
		if err := ctx.AppendRecord(r.answerEntry(choice, ""), false); err != nil {
			return nil, err
		}
		return &gent.AgentLoopResult{Action: gent.LATerminate, Result: result.Content,
			ContentSource: choice.CandidateSource}, nil
	case gent.TerminationAnswerRejected:
		var feedback []string
		for _, part := range result.Content {
			if text, ok := part.(gent.TextContent); ok {
				feedback = append(feedback, text.Text)
			}
		}
		message := strings.TrimSpace(strings.Join(feedback, "\n"))
		if message == "" {
			message = "Answer validation failed. Please try again."
		}
		return r.nativeFeedback(ctx, choice, message)
	default:
		return r.nativeFeedback(ctx, choice, "Use a tool or give a final answer.")
	}
}

func (r *Agent) nativeFeedback(
	ctx *gent.ExecutionContext, choice *gent.ContentChoice, feedback string,
) (*gent.AgentLoopResult, error) {
	if err := ctx.AppendRecord(r.answerEntry(choice, feedback), true); err != nil {
		return nil, err
	}
	return &gent.AgentLoopResult{Action: gent.LAContinue, NextPrompt: feedback}, nil
}

// Answer feedback can cause another request. Keep the provider items for that turn too.
func (r *Agent) answerEntry(choice *gent.ContentChoice, feedback string) *gent.HistoryEntry {
	entry := r.buildIteration(choice.CandidateContent, feedback)
	entry.Messages[0].Continuation = choice.Continuation
	return entry
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
		Messages: []*gent.MessageContent{{Role: gent.ChatMessageTypeHuman,
			Parts: []gent.ContentPart{gent.TextContent{Text: feedback}}}}}
	if err := ctx.AppendRecord(reminder, true); err != nil {
		return nil, err
	}
	return &gent.AgentLoopResult{Action: gent.LAContinue, NextPrompt: feedback}, nil
}
