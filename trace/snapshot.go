package trace

import (
	"fmt"
	"unicode/utf8"

	"github.com/rickchristie/gent"
)

func (s *Sequencer) applyContextStarted(event *Event) {
	ctx := s.contextById[event.ContextId]
	if ctx == nil {
		ctx = &Context{
			Id:                 event.ContextId,
			ParentId:           event.ParentContextId,
			Source:             event.Source,
			Depth:              event.Depth,
			StartedTs:          event.Ts,
			StartedEventNumber: event.EventNumber,
			Status:             StepStatusRunning,
		}
		s.contextById[event.ContextId] = ctx
		s.snapshot.Contexts = append(s.snapshot.Contexts, ctx)
		s.snapshot.Stats.ContextCount++
	}
	payload, _ := event.Payload.(map[string]any)
	if name, ok := payload["name"].(string); ok {
		ctx.Name = name
	}
	ctx.LastUpdatedTs = event.Ts
	ctx.LastEventNumber = event.EventNumber
	if s.snapshot.Status == RunStatusPending {
		s.snapshot.Status = RunStatusRunning
	}
}

func (s *Sequencer) applyContextFinished(event *Event) {
	ctx := s.contextById[event.ContextId]
	if ctx == nil {
		s.applyContextStarted(event)
		ctx = s.contextById[event.ContextId]
	}
	payload, _ := event.Payload.(map[string]any)
	ctx.Status = contextStatusFromPayload(payload, event.Error)
	ctx.LastUpdatedTs = event.Ts
	ctx.LastEventNumber = event.EventNumber
	ctx.CompletedEventNumber = event.EventNumber
	ctx.CompletedTs = &event.Ts
	if durationMs, ok := payload["durationMs"].(int64); ok {
		ctx.DurationMs = durationMs
	}
	if event.Error != nil {
		ctx.Error = cloneError(event.Error)
		return
	}
	// Child completion is a synthetic lifecycle event and may not carry the child executor error.
	// Keep the executor's earlier terminal error unless this finish event is an actual success.
	if ctx.Status == StepStatusSucceeded {
		ctx.Error = nil
	}
}

func (s *Sequencer) applyExecutionStarted(event *Event) {
	s.snapshot.Status = RunStatusRunning
	s.snapshot.StartedTs = event.Ts
}

func (s *Sequencer) applyExecutionFinished(event *Event) {
	payload, _ := event.Payload.(map[string]any)
	status, _ := payload["status"].(RunStatus)
	if status == "" {
		if event.Error != nil {
			status = RunStatusFailed
		} else {
			status = RunStatusSucceeded
		}
	}
	s.snapshot.Status = status
	s.snapshot.CompletedTs = &event.Ts
	s.snapshot.Stats.DurationMs = event.Ts.Sub(s.snapshot.StartedTs).Milliseconds()
	if result, ok := payload["result"].(*RunResult); ok {
		s.snapshot.Result = cloneRunResult(result)
	}
	if event.Error != nil {
		s.appendError(event.Error)
	}
}

func (s *Sequencer) applyIterationStarted(event *Event) {
	key := iterationKey(event.ContextId, event.Iteration)
	iteration := &Iteration{
		Iteration:          event.Iteration,
		Depth:              event.Depth,
		Source:             event.Source,
		ContextId:          event.ContextId,
		ParentContextId:    event.ParentContextId,
		Status:             StepStatusRunning,
		StartedTs:          event.Ts,
		LastUpdatedTs:      event.Ts,
		StartedEventNumber: event.EventNumber,
		LastEventNumber:    event.EventNumber,
	}
	s.iterationByKey[key] = iteration
	s.snapshot.Iterations = append(s.snapshot.Iterations, iteration)
	s.snapshot.Stats.IterationCount++
}

func (s *Sequencer) applyIterationFinished(event *Event) {
	iteration := s.iterationByKey[iterationKey(event.ContextId, event.Iteration)]
	if iteration == nil {
		return
	}
	iteration.Status = statusFromError(event.Error)
	iteration.LastUpdatedTs = event.Ts
	iteration.CompletedTs = &event.Ts
	iteration.DurationMs = durationMsFromPayload(event.Payload)
	iteration.LastEventNumber = event.EventNumber
	iteration.CompletedEventNumber = event.EventNumber
	iteration.Error = cloneError(event.Error)
}

func (s *Sequencer) applyModelCallStarted(event *Event) {
	if event.ModelCallId == "" {
		return
	}
	payload, _ := event.Payload.(map[string]any)
	call := &ModelCall{
		Id:                 event.ModelCallId,
		Model:              stringFromPayload(payload, "model"),
		Provider:           stringFromPayload(payload, "provider"),
		StreamId:           event.StreamId,
		StreamTopicId:      event.StreamTopicId,
		Source:             event.Source,
		Iteration:          event.Iteration,
		Depth:              event.Depth,
		ContextId:          event.ContextId,
		ParentContextId:    event.ParentContextId,
		Status:             StepStatusRunning,
		StartedTs:          event.Ts,
		LastUpdatedTs:      event.Ts,
		StartedEventNumber: event.EventNumber,
		LastEventNumber:    event.EventNumber,
	}
	if request, ok := payload["request"]; ok {
		call.Request = request
	}
	s.modelById[event.ModelCallId] = call
	if event.StreamId != "" {
		s.modelIdsByStream[event.StreamId] = append(
			s.modelIdsByStream[event.StreamId], event.ModelCallId,
		)
	}
	s.snapshot.ModelCalls = append(s.snapshot.ModelCalls, call)
	s.snapshot.Stats.ModelCallCount++
	s.attachModelToIteration(event.ContextId, event.Iteration, event.ModelCallId)
}

func (s *Sequencer) applyModelCallFinished(event *Event) {
	payload, _ := event.Payload.(map[string]any)
	usage := modelUsageFromPayload(payload)
	s.addUsage(usage)
	s.snapshot.Stats.ModelDurationMs += durationMsFromPayload(event.Payload)

	call := s.modelById[event.ModelCallId]
	if call == nil {
		call = s.unambiguousModelCall(event)
	}
	if call == nil {
		if event.Error != nil {
			s.appendError(event.Error)
		}
		return
	}
	call.Status = statusFromError(event.Error)
	call.LastUpdatedTs = event.Ts
	call.CompletedTs = &event.Ts
	call.DurationMs = durationMsFromPayload(event.Payload)
	call.LastEventNumber = event.EventNumber
	call.CompletedEventNumber = event.EventNumber
	call.Usage = usage
	if response, ok := payload["response"]; ok {
		call.Response = response
	}
	call.Error = cloneError(event.Error)
	if event.Error != nil {
		s.appendError(event.Error)
	}
}

func (s *Sequencer) applyModelChunk(event *Event) {
	call := s.modelById[event.ModelCallId]
	if call == nil {
		call = s.unambiguousModelCall(event)
	}
	if call == nil {
		if event.Error != nil {
			s.appendError(event.Error)
		}
		return
	}
	if call.Stream == nil {
		call.Stream = &ModelStream{}
	}
	call.Stream.ChunkCount++
	call.Stream.LastChunkEventNumber = event.EventNumber
	call.LastUpdatedTs = event.Ts
	call.LastEventNumber = event.EventNumber
	payload, _ := event.Payload.(map[string]any)
	if content, ok := payload["content"].(string); ok {
		call.Stream.Content, call.Stream.ContentTruncated = appendLimitedUTF8(
			call.Stream.Content, content, s.cfg.maxModelContentBytes,
		)
	}
	if reasoning, ok := payload["reasoningContent"].(string); ok {
		call.Stream.ReasoningContent, call.Stream.ReasoningContentTruncated = appendLimitedUTF8(
			call.Stream.ReasoningContent, reasoning, s.cfg.maxReasoningContentBytes,
		)
	}
	if event.Error != nil {
		call.Error = cloneError(event.Error)
		s.appendError(event.Error)
	}
}

func (s *Sequencer) applyToolCallStarted(event *Event) {
	if event.ToolCallId == "" {
		return
	}
	payload, _ := event.Payload.(map[string]any)
	call := &ToolCall{
		Id:                 event.ToolCallId,
		Name:               stringFromPayload(payload, "name"),
		Source:             event.Source,
		Iteration:          event.Iteration,
		Depth:              event.Depth,
		ContextId:          event.ContextId,
		ParentContextId:    event.ParentContextId,
		Status:             StepStatusRunning,
		StartedTs:          event.Ts,
		LastUpdatedTs:      event.Ts,
		StartedEventNumber: event.EventNumber,
		LastEventNumber:    event.EventNumber,
	}
	if args, ok := payload["args"]; ok {
		call.Args = args
	}
	s.toolById[event.ToolCallId] = call
	s.snapshot.ToolCalls = append(s.snapshot.ToolCalls, call)
	s.snapshot.Stats.ToolCallCount++
	s.attachToolToIteration(event.ContextId, event.Iteration, event.ToolCallId)
}

func (s *Sequencer) applyToolCallFinished(event *Event) {
	s.snapshot.Stats.ToolDurationMs += durationMsFromPayload(event.Payload)
	call := s.toolById[event.ToolCallId]
	if call == nil {
		call = s.unambiguousToolCall(event)
	}
	if call == nil {
		if event.Error != nil {
			s.appendError(event.Error)
		}
		return
	}
	payload, _ := event.Payload.(map[string]any)
	call.Status = statusFromError(event.Error)
	call.LastUpdatedTs = event.Ts
	call.CompletedTs = &event.Ts
	call.DurationMs = durationMsFromPayload(event.Payload)
	call.LastEventNumber = event.EventNumber
	call.CompletedEventNumber = event.EventNumber
	if output, ok := payload["output"]; ok {
		call.Output = output
	}
	call.Error = cloneError(event.Error)
	if event.Error != nil {
		s.appendError(event.Error)
	}
}

func (s *Sequencer) appendError(err *Error) {
	if err == nil {
		return
	}
	s.snapshot.Errors = append(s.snapshot.Errors, cloneError(err))
}

func (s *Sequencer) addUsage(usage *ModelUsage) {
	if usage == nil {
		return
	}
	s.snapshot.Stats.InputTokens += usage.InputTokens
	s.snapshot.Stats.OutputTokens += usage.OutputTokens
	s.snapshot.Stats.ReasoningTokens += usage.ReasoningTokens
	s.snapshot.Stats.CachedInputTokens += usage.CachedInputTokens
	s.snapshot.Stats.TotalTokens += usage.TotalTokens
}

func (s *Sequencer) attachModelToIteration(contextId string, iterationNumber int, id string) {
	iteration := s.iterationByKey[iterationKey(contextId, iterationNumber)]
	if iteration == nil {
		return
	}
	iteration.ModelCallIds = append(iteration.ModelCallIds, id)
}

func (s *Sequencer) attachToolToIteration(contextId string, iterationNumber int, id string) {
	iteration := s.iterationByKey[iterationKey(contextId, iterationNumber)]
	if iteration == nil {
		return
	}
	iteration.ToolCallIds = append(iteration.ToolCallIds, id)
}

func (s *Sequencer) unambiguousModelCall(event *Event) *ModelCall {
	if event.StreamId == "" {
		return nil
	}
	// Missing IDs are legacy/partial telemetry. Only stitch them when one active call is provably
	// associated with the stream; otherwise keep the finish/chunk event uncorrelated.
	ids := s.modelIdsByStream[event.StreamId]
	var result *ModelCall
	for _, id := range ids {
		call := s.modelById[id]
		if call == nil || call.Status != StepStatusRunning {
			continue
		}
		if result != nil {
			return nil
		}
		result = call
	}
	return result
}

func (s *Sequencer) unambiguousToolCall(event *Event) *ToolCall {
	payload, _ := event.Payload.(map[string]any)
	name := stringFromPayload(payload, "name")
	var result *ToolCall
	for _, call := range s.toolById {
		if call.Name != name || call.ContextId != event.ContextId || call.Iteration != event.Iteration {
			continue
		}
		if call.Status != StepStatusRunning {
			continue
		}
		if result != nil {
			return nil
		}
		result = call
	}
	return result
}

func iterationKey(contextId string, iteration int) string {
	return fmt.Sprintf("%s:%d", contextId, iteration)
}

func statusFromError(err *Error) StepStatus {
	if err != nil {
		return StepStatusFailed
	}
	return StepStatusSucceeded
}

func contextStatusFromPayload(payload map[string]any, err *Error) StepStatus {
	if status, ok := payload["status"].(RunStatus); ok {
		return stepStatusFromRunStatus(status, err)
	}
	if reason, ok := payload["terminationReason"].(gent.TerminationReason); ok {
		return stepStatusFromTerminationReason(reason, err)
	}
	if reason, ok := payload["terminationReason"].(string); ok {
		return stepStatusFromTerminationReason(gent.TerminationReason(reason), err)
	}
	return statusFromError(err)
}

func stepStatusFromRunStatus(status RunStatus, err *Error) StepStatus {
	switch status {
	case RunStatusSucceeded:
		return StepStatusSucceeded
	case RunStatusCanceled:
		return StepStatusCanceled
	case RunStatusFailed, RunStatusLimitExceeded:
		return StepStatusFailed
	default:
		return statusFromError(err)
	}
}

func stepStatusFromTerminationReason(reason gent.TerminationReason, err *Error) StepStatus {
	switch reason {
	case gent.TerminationSuccess:
		return StepStatusSucceeded
	case gent.TerminationContextCanceled:
		return StepStatusCanceled
	case gent.TerminationError, gent.TerminationCompactionFailed, gent.TerminationLimitExceeded:
		return StepStatusFailed
	default:
		return statusFromError(err)
	}
}

func durationMsFromPayload(payload any) int64 {
	data, _ := payload.(map[string]any)
	switch value := data["durationMs"].(type) {
	case int64:
		return value
	case int:
		return int64(value)
	case float64:
		return int64(value)
	default:
		return 0
	}
}

func stringFromPayload(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

func modelUsageFromPayload(payload map[string]any) *ModelUsage {
	usage, ok := payload["usage"].(*ModelUsage)
	if ok {
		return usage
	}
	return nil
}

func appendLimitedUTF8(current string, chunk string, max int) (string, bool) {
	if max == NoContentLimit {
		return current + chunk, false
	}
	combined := current + chunk
	if len(combined) <= max {
		return combined, false
	}
	if max <= 0 {
		return "", true
	}
	for max > 0 && !utf8.ValidString(combined[:max]) {
		max--
	}
	return combined[:max], true
}

func runStatus(reason gent.TerminationReason, err *Error) RunStatus {
	switch reason {
	case gent.TerminationSuccess:
		return RunStatusSucceeded
	case gent.TerminationContextCanceled:
		return RunStatusCanceled
	case gent.TerminationLimitExceeded:
		return RunStatusLimitExceeded
	case gent.TerminationError, gent.TerminationCompactionFailed:
		return RunStatusFailed
	default:
		if err != nil {
			return RunStatusFailed
		}
		return RunStatusSucceeded
	}
}
