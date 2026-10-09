package trace

import "github.com/rickchristie/gent"

// RestoreSequencer rebuilds display state from accepted records without restoring execution.
func RestoreSequencer(saved gent.RunView, cfg Config) (*Sequencer, error) {
	registry := cfg.Codecs
	if registry == nil {
		registry = gent.NewCodecRegistry()
	}
	if err := gent.ValidateBundle(saved, registry); err != nil {
		return nil, err
	}
	seq, err := NewSequencer(saved.RunId, cfg)
	if err != nil {
		return nil, err
	}
	if err := seq.ObserveCommitted(saved.Activity, registry); err != nil {
		return nil, err
	}
	return seq, nil
}

// ObserveCommitted sees only accepted records. The same projection handles live and restored
// activity, so neither callback order nor current executor state can change the displayed past.
func (s *Sequencer) ObserveCommitted(
	events []*gent.RecordedEvent, registry *gent.CodecRegistry,
) error {
	s.observeMu.Lock()
	defer s.observeMu.Unlock()
	for _, recorded := range events {
		event, err := s.project(recorded, registry)
		if err != nil {
			return err
		}
		s.record(event)
	}
	return nil
}

func (s *Sequencer) applyAttempt(event *Event) {
	if event.Type != "attempt_started" && event.Type != "attempt_interrupted" {
		return
	}
	interrupted := func(attempt string) bool {
		if event.Type == "attempt_started" {
			return attempt != event.AttemptId
		}
		return attempt == event.AttemptId
	}
	// A child interruption must not mark a concurrently running parent or sibling interrupted.
	withinContext := func(id string) bool {
		for id != "" {
			if id == event.ContextId {
				return true
			}
			parent := ""
			for _, saved := range s.snapshot.Contexts {
				if saved.Id == id {
					parent = saved.ParentId
					break
				}
			}
			id = parent
		}
		return false
	}
	resumeIteration := false
	for _, iteration := range s.snapshot.Iterations {
		if iteration.ContextId == event.ContextId && iteration.Iteration == event.Iteration &&
			(iteration.Status == StepStatusRunning || iteration.Status == StepStatusInterrupted) {
			resumeIteration = true
		}
	}
	for _, call := range s.snapshot.ModelCalls {
		if call.Status == StepStatusRunning && interrupted(call.AttemptId) &&
			withinContext(call.ContextId) {
			call.Status = StepStatusInterrupted
		}
	}
	for _, call := range s.snapshot.ToolCalls {
		if call.Status == StepStatusRunning && interrupted(call.AttemptId) &&
			withinContext(call.ContextId) {
			call.Status = StepStatusInterrupted
		}
	}
	for _, iteration := range s.snapshot.Iterations {
		if iteration.Status == StepStatusRunning && interrupted(iteration.AttemptId) &&
			withinContext(iteration.ContextId) {
			iteration.Status = StepStatusInterrupted
		}
	}
	if event.Type == "attempt_started" && resumeIteration {
		s.applyIterationStarted(event)
	}
}
