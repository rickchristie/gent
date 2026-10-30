package react

import (
	"errors"
	"fmt"

	"github.com/rickchristie/gent"
	agentutil "github.com/rickchristie/gent/agents"
)

func (r *Agent[T]) buildMessages(ctx *gent.ExecutionContext) ([]*gent.ModelMessage, error) {
	task, err := ctx.Task()
	if err != nil {
		return nil, err
	}
	if task == nil || len(task.Messages) == 0 {
		return nil, errors.New("task must contain messages")
	}
	messages := task.Messages
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
func nativeHistoryMessages(history []*gent.ModelMessage) []*gent.ModelMessage {
	var messages []*gent.ModelMessage
	var media []gent.ContentPart
	flushMedia := func() {
		if len(media) > 0 {
			messages = append(messages, &gent.ModelMessage{
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

func (r *Agent[T]) nextNative(
	ctx *gent.ExecutionContext, frame *iterationFrame,
) (*gent.AgentLoopResult, error) {
	if len(frame.Response.Choices) == 0 || frame.Response.Choices[0] == nil {
		return r.nativeRetry(ctx, nil, "The model returned no response. Please try again.")
	}
	choice := frame.Response.Choices[0]
	if err := choice.ValidateCompletion(); err != nil {
		return nil, err
	}
	if choice.Completion.Status == gent.ModelStatusRefused {
		if err := ctx.AppendRecord(r.answerEntry(choice, nil), false); err != nil {
			return nil, err
		}
		return &gent.AgentLoopResult{Action: gent.LATerminate,
			TerminationReason: gent.TerminationRefused, Refusal: choice.Completion.Refusal}, nil
	}
	if choice.Completion.Status == gent.ModelStatusFailed {
		return nil, fmt.Errorf(
			"model stopped without a usable response: %q", choice.Completion.ProviderReason)
	}
	// Provider completion is authoritative. A refusal or failed response must never turn
	// into a request to try again because its retained evidence is long or repetitive.
	if repetition := frame.Repetition; repetition != nil {
		if errors.Is(repetition.Err, agentutil.ErrMaxResponseSize) {
			return r.handleResponseTooLong(ctx, frame.Response)
		}
		return r.handleRepetition(ctx, frame.Response, repetition)
	}
	if choice.Completion.Status == gent.ModelStatusIncomplete {
		return r.nativeRetry(ctx, choice,
			"The response was incomplete. Use a shorter answer or fewer tool calls.")
	}
	if choice.Completion.Status == gent.ModelStatusContinue {
		if choice.Continuation == nil {
			return nil, fmt.Errorf(
				"model continuation %q requires a lossless adapter", choice.Completion.ProviderReason)
		}
		if err := ctx.AppendRecord(r.answerEntry(choice, nil), true); err != nil {
			return nil, err
		}
		return &gent.AgentLoopResult{Action: gent.LAContinue}, nil
	}
	if len(choice.ToolCalls) > 0 {
		return r.nextNativeTools(ctx, frame, choice)
	}

	return r.nextNativeAnswer(ctx, choice)
}

func (r *Agent[T]) nextNativeTools(
	ctx *gent.ExecutionContext, frame *iterationFrame, choice *gent.ContentChoice,
) (*gent.AgentLoopResult, error) {
	chain := r.toolChain
	if !frame.ToolPlanActive {
		if err := normalizeCallIDs(ctx, choice.ToolCalls); err != nil {
			ctx.PublishParseError(
				gent.ParseErrorTypeToolchain, gent.ContentText(choice.CandidateContent), err)
			return r.nativeRetry(ctx, choice, err.Error())
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
		return r.nativeRetry(ctx, choice, err.Error())
	}
	entry := nativeToolEntry(choice, result)
	if err := ctx.AppendRecord(entry, true); err != nil {
		return nil, err
	}
	ctx.Stats().ResetGauge(gent.SGRepetitionLoopConsecutive)
	return &gent.AgentLoopResult{Action: gent.LAContinue}, nil
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
	ai.Parts = gent.CloneContentParts(choice.CandidateContent)
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

func (r *Agent[T]) nextNativeAnswer(
	ctx *gent.ExecutionContext, choice *gent.ContentChoice,
) (*gent.AgentLoopResult, error) {
	result, err := r.output.Process(ctx, choice.CandidateContent)
	if err == nil {
		err = result.Validate()
	}
	if err != nil {
		ctx.PublishOutputValidation(gent.OutputValidationProcess, "", false, nil, err)
		return nil, err
	}
	if !result.Accepted {
		// Save the candidate and steering even if a stage limit has just canceled this run.
		next, err := r.nativeFeedback(ctx, choice, result.Steering)
		if err != nil {
			return nil, err
		}
		ctx.PublishOutputValidation(gent.OutputValidationProcess, "", false, result.Steering, nil)
		return next, nil
	}
	ctx.Stats().ResetGauge(gent.SGRepetitionLoopConsecutive)
	if err := ctx.AppendRecord(r.answerEntry(choice, nil), false); err != nil {
		return nil, err
	}
	accepted, err := gent.AcceptOutput(ctx, result, choice.CandidateSource)
	ctx.PublishOutputValidation(gent.OutputValidationProcess, "", err == nil, nil, err)
	return accepted, err
}

func (r *Agent[T]) nativeFeedback(
	ctx *gent.ExecutionContext, choice *gent.ContentChoice, feedback []gent.ContentPart,
) (*gent.AgentLoopResult, error) {
	if err := ctx.AppendRequiredRecord(r.answerEntry(choice, feedback)); err != nil {
		return nil, err
	}
	return &gent.AgentLoopResult{Action: gent.LAContinue}, nil
}

// Answer feedback can cause another request. Keep the provider items for that turn too.
func (r *Agent[T]) answerEntry(
	choice *gent.ContentChoice, feedback []gent.ContentPart,
) *gent.HistoryEntry {
	message := &gent.MessageContent{Role: gent.ChatMessageTypeAI,
		Parts: gent.CloneContentParts(choice.CandidateContent), Continuation: choice.Continuation}
	for _, call := range choice.ToolCalls {
		message.Parts = append(message.Parts, call)
	}
	entry := &gent.HistoryEntry{Messages: []*gent.MessageContent{message}}
	if len(feedback) > 0 {
		steering := &gent.MessageContent{Role: gent.ChatMessageTypeHuman,
			Parts: gent.CloneContentParts(feedback)}
		steering.SetMetadata("gent:output_steering", true)
		entry.Messages = append(entry.Messages, steering)
	}
	return entry
}

func (r *Agent[T]) nativeRetry(
	ctx *gent.ExecutionContext, choice *gent.ContentChoice, feedback string,
) (*gent.AgentLoopResult, error) {
	// Keep the rejected response for inspection. Do not send incomplete call groups to the model.
	entry := &gent.HistoryEntry{}
	if choice != nil {
		entry = r.answerEntry(choice, nil)
	}
	entry.ExpireAfterIteration = max(ctx.Iteration(), 1)
	if err := ctx.AppendRecord(entry, true); err != nil {
		return nil, err
	}
	reminder := &gent.HistoryEntry{Origin: gent.RecordSystemInjected,
		Messages: []*gent.MessageContent{{Role: gent.ChatMessageTypeHuman,
			Parts: []gent.ContentPart{gent.TextContent{Text: feedback}}}}}
	if err := ctx.AppendRequiredRecord(reminder); err != nil {
		return nil, err
	}
	return &gent.AgentLoopResult{Action: gent.LAContinue}, nil
}
