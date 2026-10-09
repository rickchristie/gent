package trace

import (
	"fmt"
	"time"

	"github.com/rickchristie/gent"
)

// project decodes one accepted payload directly into its display representation. Hook objects
// are never dispatched or rewritten during replay; the accepted envelope owns identity/order.
func (s *Sequencer) project(
	recorded *gent.RecordedEvent, registry *gent.CodecRegistry,
) (*Event, error) {
	if recorded == nil {
		return nil, fmt.Errorf("trace: nil recorded event")
	}
	if recorded.RunId != s.runId {
		return nil, gent.ErrWrongRun
	}
	value, err := registry.DecodeEvent(recorded)
	if err != nil {
		return nil, err
	}
	event := &Event{EventNumber: recorded.EventNumber, RunId: recorded.RunId, Ts: recorded.Ts,
		Type: EventType(recorded.Type), EventName: recorded.EventName, ContextId: recorded.ContextId,
		ParentContextId: recorded.ParentContextId, Iteration: recorded.Iteration,
		AttemptId: recorded.AttemptId, ModelCallId: recorded.ModelCallId, ToolCallId: recorded.ToolCallId}
	if withBase, ok := value.(interface{ EventBase() *gent.BaseEvent }); ok {
		base := withBase.EventBase()
		event.Depth, event.Source = base.Depth, base.Source
	}
	payload := map[string]any{}
	event.Payload = payload
	switch saved := value.(type) {
	case *gent.BeforeExecutionEvent:
		event.Type = EventTypeContextStarted
		if event.ParentContextId == "" && event.Depth == 0 {
			event.Type = EventTypeExecutionStarted
		}
		payload["name"] = saved.Name
	case *gent.AfterExecutionEvent:
		event.Type = EventTypeContextFinished
		if event.ParentContextId == "" && event.Depth == 0 {
			event.Type = EventTypeExecutionFinished
		}
		event.Error = redactError(s.cfg.redactor, saved.Error)
		result := &RunResult{TerminationReason: saved.TerminationReason, Error: event.Error}
		if s.cfg.IncludeRunOutput {
			result.Output = jsonSafeValue(s.cfg.redactor.RedactRunOutput(saved.Output))
		}
		payload["terminationReason"], payload["result"] = saved.TerminationReason, result
		payload["status"] = runStatus(saved.TerminationReason, event.Error)
		payload["durationMs"] = saved.Duration.Milliseconds()
	case *gent.BeforeIterationEvent:
		event.Payload = nil
	case *gent.AfterIterationEvent:
		payload["durationMs"] = saved.Duration.Milliseconds()
		event.Error = redactError(s.cfg.redactor, saved.Error)
		if saved.Result != nil {
			payload["action"] = saved.Result.Action
			if s.cfg.IncludeRunOutput && len(saved.Result.Result) > 0 {
				payload["result"] = jsonSafeValue(s.cfg.redactor.RedactRunOutput(saved.Result.Result))
			}
		}
	case *gent.BeforeSystemPromptEvent:
		payload["sectionCount"] = len(saved.Sections)
		payload["sectionNames"] = sectionNames(saved.Sections)
		if s.cfg.IncludeSystemPrompt {
			payload["sections"] = s.cfg.redactor.RedactSystemPrompt(saved.Sections)
		}
	case *gent.BeforeModelCallEvent:
		event.StreamId, event.StreamTopicId = saved.StreamId, saved.StreamTopicId
		payload["model"], payload["provider"] = saved.Model, saved.Provider
		payload["streamId"], payload["streamTopicId"] = saved.StreamId, saved.StreamTopicId
		if s.cfg.IncludeModelRequests {
			payload["request"] = jsonSafeValue(s.cfg.redactor.RedactModelRequest(saved.Request))
		}
	case *gent.AfterModelCallEvent:
		event.StreamId, event.StreamTopicId = saved.StreamId, saved.StreamTopicId
		event.Error = redactError(s.cfg.redactor, saved.Error)
		payload["model"], payload["provider"] = saved.Model, saved.Provider
		payload["streamId"], payload["streamTopicId"] = saved.StreamId, saved.StreamTopicId
		payload["durationMs"] = saved.Duration.Milliseconds()
		payload["usage"] = modelUsage(saved.Response, saved.InputTokens, saved.OutputTokens)
		if s.cfg.IncludeModelResponses {
			payload["response"] = jsonSafeValue(s.cfg.redactor.RedactModelResponse(saved.Response))
		}
	case *gent.BeforeToolCallEvent:
		payload["name"] = saved.ToolName
		if s.cfg.IncludeToolArgs {
			payload["args"] = jsonSafeValue(s.cfg.redactor.RedactToolArgs(saved.Args))
		}
	case *gent.AfterToolCallEvent:
		event.Error = redactError(s.cfg.redactor, saved.Error)
		payload["name"], payload["durationMs"] = saved.ToolName, saved.Duration.Milliseconds()
		if s.cfg.IncludeToolOutput {
			payload["output"] = jsonSafeValue(s.cfg.redactor.RedactToolOutput(saved.Output))
		}
	case *gent.ParseErrorEvent:
		event.Error = redactError(s.cfg.redactor, saved.Error)
		payload["errorType"] = saved.ErrorType
	case *gent.ValidatorCalledEvent:
		payload["validatorName"] = saved.ValidatorName
	case *gent.ValidatorResultEvent:
		payload["validatorName"], payload["accepted"] = saved.ValidatorName, saved.Accepted
		payload["feedbackCount"] = len(saved.Feedback)
	case *gent.ErrorEvent:
		event.Error, event.Payload = redactError(s.cfg.redactor, saved.Error), nil
	case *gent.LimitExceededEvent:
		err := fmt.Errorf("limit exceeded: %s = %v", saved.MatchedKey, saved.CurrentValue)
		event.Error = redactError(s.cfg.redactor, err)
		payload["limit"], payload["currentValue"] = saved.Limit, saved.CurrentValue
		payload["matchedKey"] = saved.MatchedKey
	case *gent.CompactionEvent:
		payload["scratchpadLengthBefore"] = saved.ScratchpadLengthBefore
		payload["scratchpadLengthAfter"] = saved.ScratchpadLengthAfter
		payload["durationMs"] = saved.Duration.Milliseconds()
	case *gent.CommonEvent:
		payload["description"] = saved.Description
		if s.cfg.IncludeCommonPayload {
			payload["data"] = jsonSafeValue(s.cfg.redactor.RedactCommonPayload(saved.EventName, saved.Data))
		}
		if saved.EventName != gent.EventNameChildSpawn && saved.EventName != gent.EventNameChildComplete {
			break
		}
		data, _ := saved.Data.(map[string]any)
		event.ContextId, _ = data["child_context_id"].(string)
		event.ParentContextId, _ = data["parent_context_id"].(string)
		event.Source, _ = data["child_source"].(string)
		event.Depth = intFromAny(data["child_depth"])
		childError, _ := data["error"].(error)
		event.Error = redactError(s.cfg.redactor, childError)
		event.Type = EventTypeContextStarted
		if saved.EventName == gent.EventNameChildComplete {
			event.Type = EventTypeContextFinished
		}
		payload = map[string]any{"name": data["child_name"], "parentContextId": event.ParentContextId,
			"source": event.Source, "depth": event.Depth, "terminationReason": data["termination_reason"]}
		if duration, ok := data["duration"].(time.Duration); ok {
			payload["durationMs"] = duration.Milliseconds()
		}
		event.Payload = payload
	case gent.HistoryChange:
		if s.cfg.IncludeStateChanges {
			payload["data"] = jsonSafeValue(s.cfg.redactor.RedactStateChange(recorded.EventName, saved))
		}
	case gent.StreamChunk:
		chunk := s.cfg.redactor.RedactChunk(saved)
		event.Depth, event.Source = chunk.Depth, chunk.Source
		event.StreamId, event.StreamTopicId = chunk.StreamId, chunk.StreamTopicId
		event.Error = redactError(s.cfg.redactor, chunk.Err)
		payload["streamId"], payload["streamTopicId"] = chunk.StreamId, chunk.StreamTopicId
		if s.cfg.IncludeChunkText {
			if chunk.Content != "" {
				payload["content"] = chunk.Content
			}
			if chunk.ReasoningContent != "" {
				payload["reasoningContent"] = chunk.ReasoningContent
			}
		}
	default:
		event.Payload = nil
	}
	return event, nil
}

func (s *Sequencer) applyEvent(event *Event) {
	switch event.Type {
	case EventTypeExecutionStarted:
		s.applyContextStarted(event)
		s.applyExecutionStarted(event)
	case EventTypeExecutionFinished:
		s.applyContextFinished(event)
		s.applyExecutionFinished(event)
	case EventTypeContextStarted:
		s.applyContextStarted(event)
	case EventTypeContextFinished:
		s.applyContextFinished(event)
	case EventTypeIterationStarted:
		s.applyIterationStarted(event)
	case EventTypeIterationFinished:
		s.applyIterationFinished(event)
	case EventTypeModelCallStarted:
		s.applyModelCallStarted(event)
	case EventTypeModelCallFinished:
		s.applyModelCallFinished(event)
	case EventTypeModelStreamChunk:
		s.applyModelChunk(event)
	case EventTypeToolCallStarted:
		s.applyToolCallStarted(event)
	case EventTypeToolCallFinished:
		s.applyToolCallFinished(event)
	case EventTypeParseError:
		s.snapshot.Stats.ParseErrorCount++
		s.appendError(event.Error)
	case EventTypeValidatorResult:
		payload, _ := event.Payload.(map[string]any)
		if accepted, _ := payload["accepted"].(bool); !accepted {
			s.snapshot.Stats.ValidatorRejectionCount++
		}
	case EventTypeError:
		s.snapshot.Stats.ErrorCount++
		s.appendError(event.Error)
	case EventTypeLimitExceeded:
		s.snapshot.Status = RunStatusLimitExceeded
		s.snapshot.Stats.LimitExceededCount++
		s.appendError(event.Error)
	case EventTypeCompaction:
		s.snapshot.Stats.CompactionCount++
	default:
		s.applyAttempt(event)
	}
}

func modelUsage(response *gent.ContentResponse, inputTokens int, outputTokens int) *ModelUsage {
	usage := &ModelUsage{InputTokens: inputTokens, OutputTokens: outputTokens}
	if response != nil && response.Info != nil {
		usage.InputTokens = response.Info.InputTokens
		usage.OutputTokens = response.Info.OutputTokens
		usage.CachedInputTokens = response.Info.CachedInputTokens
		usage.ReasoningTokens = response.Info.ReasoningTokens
		usage.TotalTokens = response.Info.TotalTokens
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	return usage
}

func sectionNames(sections []gent.FormattedSection) []string {
	result := make([]string, 0, len(sections))
	for _, section := range sections {
		result = append(result, section.Name)
	}
	return result
}

func intFromAny(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}
