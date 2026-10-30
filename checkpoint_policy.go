package gent

import (
	"encoding/json"
	"errors"
	"time"
)

// CheckpointPolicy controls grouped progress saves independently of live subscriber queues.
// Zero fields use defaults. Limits count pending encoded event data, not total run memory.
type CheckpointPolicy struct {
	// MaxEvents triggers a blocking progress save at this pending count. Default: 256.
	MaxEvents int
	// MaxBytes triggers a progress save at this encoded byte count. Default: 1 MiB.
	// One oversized event is saved whole; it is never split or silently discarded.
	MaxBytes int
	// SaveTimeout sets the Save context deadline independently of worker cancellation.
	// Default: 30 seconds. The recorder must honor its context; Gent waits for its result.
	SaveTimeout time.Duration
}

// WithCheckpointPolicy sets the pending progress thresholds. It does not remove explicit model,
// tool, or terminal save boundaries. Invalid limits stop execution before any external action.
func WithCheckpointPolicy(policy CheckpointPolicy) ContextOption {
	return func(options *contextOptions) { options.checkpointPolicy = policy }
}

func checkpointPolicy(policy CheckpointPolicy) (CheckpointPolicy, error) {
	if policy.MaxEvents < 0 || policy.MaxBytes < 0 || policy.SaveTimeout < 0 {
		return policy, errors.New("gent: checkpoint limits must not be negative")
	}
	if policy.MaxEvents == 0 {
		policy.MaxEvents = 256
	}
	if policy.MaxBytes == 0 {
		policy.MaxBytes = 1024 * 1024
	}
	if policy.SaveTimeout == 0 {
		policy.SaveTimeout = 30 * time.Second
	}
	return policy, nil
}

// queueEvent runs with run.mu held. Assign the live position once, before either delivery or
// saving. All contexts use this lock, so frozen events cannot overtake one another.
func (run *executionRun) queueEvent(event *RecordedEvent) error {
	if run.err != nil {
		return run.err
	}
	event.RunId = run.id
	event.EventNumber = run.nextEventNumber + 1
	if event.Ts.IsZero() {
		event.Ts = time.Now()
	}
	data, err := run.codecs.Marshal(event)
	if err != nil {
		return err
	}
	run.nextEventNumber = event.EventNumber
	run.pending = append(run.pending, event)
	run.pendingSizes = append(run.pendingSizes, len(data))
	run.pendingBytes += len(data)
	if event.Type == "execution_finished" || event.Type == "execution_content" {
		run.deferLive = true
	}
	if !run.deferLive {
		run.publishLive(event, len(data))
	}
	return nil
}

// publishLive runs under run.mu. It only copies frozen data and queues it; application consumers
// run on their own goroutines and cannot delay a save or another subscriber.
func (run *executionRun) publishLive(event *RecordedEvent, size int) {
	if event.EventNumber <= run.liveEventNumber {
		return
	}
	run.liveEventNumber = event.EventNumber
	for sub := range run.liveSubscribers {
		owned, err := cloneRecordedEvent(run.codecs, event)
		if err != nil {
			sub.queue.Fail(err)
			delete(run.liveSubscribers, sub)
			continue
		}
		if !sub.queue.Send(owned, size) {
			delete(run.liveSubscribers, sub)
		}
	}
	if delayed, ok := run.delayedChunks[event.EventNumber]; ok {
		delayed.owner.emitStreamChunk(delayed.chunk)
		delete(run.delayedChunks, event.EventNumber)
	}
}

func (ctx *ExecutionContext) flushPendingLimit() error {
	run := ctx.run
	run.mu.RLock()
	flush := !run.deferLive && (len(run.pending) >= run.policy.MaxEvents ||
		run.pendingBytes >= run.policy.MaxBytes)
	run.mu.RUnlock()
	if flush {
		return ctx.FlushProgress()
	}
	return ctx.RecordingError()
}

func chunkSize(chunk StreamChunk) int {
	// Count the saved error text, not the often empty JSON object for an application error.
	// Content parts include binary data, which JSON counts in its base64 form.
	chunk.Err = SaveError(chunk.Err)
	data, err := json.Marshal(chunk)
	if err != nil {
		panic(err)
	}
	return len(data)
}
