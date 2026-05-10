package trace

import (
	"fmt"
	"time"

	"github.com/rickchristie/gent"
)

var _ gent.BeforeExecutionSubscriber = (*Sequencer)(nil)
var _ gent.AfterExecutionSubscriber = (*Sequencer)(nil)
var _ gent.BeforeIterationSubscriber = (*Sequencer)(nil)
var _ gent.AfterIterationSubscriber = (*Sequencer)(nil)
var _ gent.BeforeSystemPromptSubscriber = (*Sequencer)(nil)
var _ gent.BeforeModelCallSubscriber = (*Sequencer)(nil)
var _ gent.AfterModelCallSubscriber = (*Sequencer)(nil)
var _ gent.BeforeToolCallSubscriber = (*Sequencer)(nil)
var _ gent.AfterToolCallSubscriber = (*Sequencer)(nil)
var _ gent.ParseErrorSubscriber = (*Sequencer)(nil)
var _ gent.ValidatorCalledSubscriber = (*Sequencer)(nil)
var _ gent.ValidatorResultSubscriber = (*Sequencer)(nil)
var _ gent.ErrorSubscriber = (*Sequencer)(nil)
var _ gent.LimitExceededSubscriber = (*Sequencer)(nil)
var _ gent.CompactionSubscriber = (*Sequencer)(nil)
var _ gent.CommonEventSubscriber = (*Sequencer)(nil)
var _ gent.CommonDiffEventSubscriber = (*Sequencer)(nil)

func (s *Sequencer) OnBeforeExecution(execCtx *gent.ExecutionContext, event *gent.BeforeExecutionEvent) {
	s.ObserveStreams(execCtx)
	s.record(normalizedEvent{
		event: s.eventFromBase(event.BaseEvent, EventTypeContextStarted, nil, nil),
		apply: (*Sequencer).applyContextStarted,
	})
	if !isRootLifecycleEvent(event.BaseEvent) {
		return
	}
	// The snapshot run lifecycle belongs to the root context. Nested executors still record
	// context lifecycle, but must not reset run timestamps while the parent is running.
	s.record(normalizedEvent{
		event: s.eventFromBase(event.BaseEvent, EventTypeExecutionStarted, nil, nil),
		apply: (*Sequencer).applyExecutionStarted,
	})
}

func (s *Sequencer) OnAfterExecution(execCtx *gent.ExecutionContext, event *gent.AfterExecutionEvent) {
	redactedErr := redactError(s.cfg.redactor, event.Error)
	status := runStatus(event.TerminationReason, redactedErr)
	result := &RunResult{TerminationReason: event.TerminationReason, Error: redactedErr}
	if s.cfg.IncludeRunOutput {
		result.Output = jsonSafeValue(s.cfg.redactor.RedactRunOutput(execCtx.FinalResult()))
	}
	payload := map[string]any{
		"terminationReason": event.TerminationReason,
		"status":            status,
		"result":            result,
	}
	if isRootLifecycleEvent(event.BaseEvent) {
		s.record(normalizedEvent{
			event: s.eventFromBase(event.BaseEvent, EventTypeExecutionFinished, payload, redactedErr),
			apply: (*Sequencer).applyExecutionFinished,
		})
	}
	s.record(normalizedEvent{
		event: s.eventFromBase(event.BaseEvent, EventTypeContextFinished, payload, redactedErr),
		apply: (*Sequencer).applyContextFinished,
	})
}

func (s *Sequencer) OnBeforeIteration(_ *gent.ExecutionContext, event *gent.BeforeIterationEvent) {
	s.record(normalizedEvent{
		event: s.eventFromBase(event.BaseEvent, EventTypeIterationStarted, nil, nil),
		apply: (*Sequencer).applyIterationStarted,
	})
}

func (s *Sequencer) OnAfterIteration(_ *gent.ExecutionContext, event *gent.AfterIterationEvent) {
	payload := map[string]any{"durationMs": event.Duration.Milliseconds()}
	if event.Result != nil {
		payload["action"] = event.Result.Action
		if len(event.Result.Result) > 0 {
			payload["result"] = jsonSafeValue(event.Result.Result)
		}
	}
	s.record(normalizedEvent{
		event: s.eventFromBase(event.BaseEvent, EventTypeIterationFinished, payload, nil),
		apply: (*Sequencer).applyIterationFinished,
	})
}

func (s *Sequencer) OnBeforeSystemPrompt(_ *gent.ExecutionContext, event *gent.BeforeSystemPromptEvent) {
	payload := map[string]any{"sectionCount": len(event.Sections), "sectionNames": sectionNames(event.Sections)}
	if s.cfg.IncludeSystemPrompt {
		payload["sections"] = s.cfg.redactor.RedactSystemPrompt(event.Sections)
	}
	s.record(normalizedEvent{event: s.eventFromBase(event.BaseEvent, EventTypeSystemPrompt, payload, nil)})
}

func (s *Sequencer) OnBeforeModelCall(_ *gent.ExecutionContext, event *gent.BeforeModelCallEvent) {
	payload := map[string]any{
		"model":         event.Model,
		"provider":      event.Provider,
		"streamId":      event.StreamId,
		"streamTopicId": event.StreamTopicId,
	}
	if s.cfg.IncludeModelRequests {
		payload["request"] = jsonSafeValue(s.cfg.redactor.RedactModelRequest(event.Request))
	}
	traceEvent := s.eventFromBase(event.BaseEvent, EventTypeModelCallStarted, payload, nil)
	traceEvent.ModelCallId = event.ModelCallId
	traceEvent.StreamId = event.StreamId
	traceEvent.StreamTopicId = event.StreamTopicId
	s.record(normalizedEvent{event: traceEvent, apply: (*Sequencer).applyModelCallStarted})
}

func (s *Sequencer) OnAfterModelCall(_ *gent.ExecutionContext, event *gent.AfterModelCallEvent) {
	redactedErr := redactError(s.cfg.redactor, event.Error)
	usage := modelUsage(event.Response, event.InputTokens, event.OutputTokens)
	payload := map[string]any{
		"model":         event.Model,
		"provider":      event.Provider,
		"streamId":      event.StreamId,
		"streamTopicId": event.StreamTopicId,
		"durationMs":    event.Duration.Milliseconds(),
		"usage":         usage,
	}
	if s.cfg.IncludeModelResponses {
		payload["response"] = jsonSafeValue(s.cfg.redactor.RedactModelResponse(event.Response))
	}
	traceEvent := s.eventFromBase(event.BaseEvent, EventTypeModelCallFinished, payload, redactedErr)
	traceEvent.ModelCallId = event.ModelCallId
	traceEvent.StreamId = event.StreamId
	traceEvent.StreamTopicId = event.StreamTopicId
	s.record(normalizedEvent{event: traceEvent, apply: (*Sequencer).applyModelCallFinished})
}

func (s *Sequencer) OnBeforeToolCall(_ *gent.ExecutionContext, event *gent.BeforeToolCallEvent) {
	payload := map[string]any{"name": event.ToolName}
	if s.cfg.IncludeToolArgs {
		payload["args"] = jsonSafeValue(s.cfg.redactor.RedactToolArgs(event.Args))
	}
	traceEvent := s.eventFromBase(event.BaseEvent, EventTypeToolCallStarted, payload, nil)
	traceEvent.ToolCallId = event.ToolCallId
	s.record(normalizedEvent{event: traceEvent, apply: (*Sequencer).applyToolCallStarted})
}

func (s *Sequencer) OnAfterToolCall(_ *gent.ExecutionContext, event *gent.AfterToolCallEvent) {
	redactedErr := redactError(s.cfg.redactor, event.Error)
	payload := map[string]any{"name": event.ToolName, "durationMs": event.Duration.Milliseconds()}
	if s.cfg.IncludeToolOutput {
		payload["output"] = jsonSafeValue(s.cfg.redactor.RedactToolOutput(event.Output))
	}
	traceEvent := s.eventFromBase(event.BaseEvent, EventTypeToolCallFinished, payload, redactedErr)
	traceEvent.ToolCallId = event.ToolCallId
	s.record(normalizedEvent{event: traceEvent, apply: (*Sequencer).applyToolCallFinished})
}

func (s *Sequencer) OnParseError(_ *gent.ExecutionContext, event *gent.ParseErrorEvent) {
	redactedErr := redactError(s.cfg.redactor, event.Error)
	payload := map[string]any{"errorType": event.ErrorType}
	s.record(normalizedEvent{
		event: s.eventFromBase(event.BaseEvent, EventTypeParseError, payload, redactedErr),
		apply: func(seq *Sequencer, e *Event) {
			seq.snapshot.Stats.ParseErrorCount++
			seq.appendError(e.Error)
		},
	})
}

func (s *Sequencer) OnValidatorCalled(_ *gent.ExecutionContext, event *gent.ValidatorCalledEvent) {
	payload := map[string]any{"validatorName": event.ValidatorName}
	s.record(normalizedEvent{event: s.eventFromBase(event.BaseEvent, EventTypeValidatorCalled, payload, nil)})
}

func (s *Sequencer) OnValidatorResult(_ *gent.ExecutionContext, event *gent.ValidatorResultEvent) {
	payload := map[string]any{
		"validatorName": event.ValidatorName,
		"accepted":      event.Accepted,
		"feedbackCount": len(event.Feedback),
	}
	s.record(normalizedEvent{
		event: s.eventFromBase(event.BaseEvent, EventTypeValidatorResult, payload, nil),
		apply: func(seq *Sequencer, _ *Event) {
			if !event.Accepted {
				seq.snapshot.Stats.ValidatorRejectionCount++
			}
		},
	})
}

func (s *Sequencer) OnError(_ *gent.ExecutionContext, event *gent.ErrorEvent) {
	redactedErr := redactError(s.cfg.redactor, event.Error)
	s.record(normalizedEvent{
		event: s.eventFromBase(event.BaseEvent, EventTypeError, nil, redactedErr),
		apply: func(seq *Sequencer, e *Event) {
			seq.snapshot.Stats.ErrorCount++
			seq.appendError(e.Error)
		},
	})
}

func (s *Sequencer) OnLimitExceeded(_ *gent.ExecutionContext, event *gent.LimitExceededEvent) {
	limitErr := fmt.Errorf("limit exceeded: %s = %v", event.MatchedKey, event.CurrentValue)
	redactedErr := redactError(s.cfg.redactor, limitErr)
	payload := map[string]any{
		"limit":        event.Limit,
		"currentValue": event.CurrentValue,
		"matchedKey":   event.MatchedKey,
	}
	s.record(normalizedEvent{
		event: s.eventFromBase(event.BaseEvent, EventTypeLimitExceeded, payload, redactedErr),
		apply: func(seq *Sequencer, e *Event) {
			seq.snapshot.Status = RunStatusLimitExceeded
			seq.snapshot.Stats.LimitExceededCount++
			seq.appendError(e.Error)
		},
	})
}

func (s *Sequencer) OnCompaction(_ *gent.ExecutionContext, event *gent.CompactionEvent) {
	payload := map[string]any{
		"scratchpadLengthBefore": event.ScratchpadLengthBefore,
		"scratchpadLengthAfter":  event.ScratchpadLengthAfter,
		"durationMs":             event.Duration.Milliseconds(),
	}
	s.record(normalizedEvent{
		event: s.eventFromBase(event.BaseEvent, EventTypeCompaction, payload, nil),
		apply: func(seq *Sequencer, _ *Event) { seq.snapshot.Stats.CompactionCount++ },
	})
}

func (s *Sequencer) OnCommonEvent(_ *gent.ExecutionContext, event *gent.CommonEvent) {
	if event.EventName == gent.EventNameChildSpawn {
		s.record(s.childContextEvent(event, EventTypeContextStarted, (*Sequencer).applyContextStarted))
		return
	}
	if event.EventName == gent.EventNameChildComplete {
		s.record(s.childContextEvent(event, EventTypeContextFinished, (*Sequencer).applyContextFinished))
		return
	}
	payload := map[string]any{"description": event.Description}
	if s.cfg.IncludeCommonPayload {
		payload["data"] = jsonSafeValue(s.cfg.redactor.RedactCommonPayload(event.EventName, event.Data))
	}
	s.record(normalizedEvent{event: s.eventFromBase(event.BaseEvent, EventTypeCommon, payload, nil)})
}

func (s *Sequencer) OnCommonDiffEvent(_ *gent.ExecutionContext, event *gent.CommonDiffEvent) {
	payload := map[string]any{}
	if s.cfg.IncludeDiffPayload {
		payload["data"] = jsonSafeValue(
			s.cfg.redactor.RedactDiffPayload(event.EventName, event.Before, event.After, event.Diff),
		)
	}
	s.record(normalizedEvent{event: s.eventFromBase(event.BaseEvent, EventTypeCommonDiff, payload, nil)})
}

func (s *Sequencer) eventFromBase(
	base gent.BaseEvent,
	eventType EventType,
	payload any,
	err *Error,
) *Event {
	return &Event{
		Ts:              base.Timestamp,
		Type:            eventType,
		EventName:       base.EventName,
		Iteration:       base.Iteration,
		Depth:           base.Depth,
		Source:          base.Source,
		ContextId:       base.ContextId,
		ParentContextId: base.ParentContextId,
		Payload:         payload,
		Error:           err,
	}
}

func isRootLifecycleEvent(base gent.BaseEvent) bool {
	return base.ParentContextId == "" && base.Depth == 0
}

func (s *Sequencer) childContextEvent(
	event *gent.CommonEvent,
	eventType EventType,
	apply func(*Sequencer, *Event),
) normalizedEvent {
	data, _ := event.Data.(map[string]any)
	childId, _ := data["child_context_id"].(string)
	parentId, _ := data["parent_context_id"].(string)
	childName, _ := data["child_name"].(string)
	childSource, _ := data["child_source"].(string)
	childDepth := intFromAny(data["child_depth"])
	payload := map[string]any{
		"name":              childName,
		"parentContextId":   parentId,
		"source":            childSource,
		"depth":             childDepth,
		"terminationReason": data["termination_reason"],
	}
	if duration, ok := data["duration"].(time.Duration); ok {
		payload["durationMs"] = duration.Milliseconds()
	}
	traceEvent := s.eventFromBase(event.BaseEvent, eventType, payload, nil)
	traceEvent.ContextId = childId
	traceEvent.ParentContextId = parentId
	traceEvent.Source = childSource
	traceEvent.Depth = childDepth
	return normalizedEvent{event: traceEvent, apply: apply}
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
