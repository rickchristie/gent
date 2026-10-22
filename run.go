package gent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"
)

const RunSchemaVersion = 3

// RecordedEvent is the lossless numbered event. Payload uses the registry, while Change is a
// structured history transition. Display projections may redact data without altering this record.
type RecordedEvent struct {
	EventNumber     uint64         `json:"eventNumber"`
	RunId           string         `json:"runId"`
	Ts              time.Time      `json:"ts"`
	Type            string         `json:"type"`
	EventName       string         `json:"eventName,omitempty"`
	ContextId       string         `json:"contextId"`
	ParentContextId string         `json:"parentContextId,omitempty"`
	Iteration       int            `json:"iteration"`
	AttemptId       string         `json:"attemptId"`
	ModelCallId     string         `json:"modelCallId,omitempty"`
	ToolCallId      string         `json:"toolCallId,omitempty"`
	Payload         *EncodedValue  `json:"payload,omitempty"`
	Change          *HistoryChange `json:"change,omitempty"`
}

type RuntimeState struct {
	Contexts   []ContextRuntime `json:"contexts"`
	Allocation uint64           `json:"allocation"`
}

type ContextRuntime struct {
	ContextId     string             `json:"contextId"`
	Iteration     int                `json:"iteration"`
	Counters      map[string]int64   `json:"counters"`
	Gauges        map[string]float64 `json:"gauges"`
	NextModelCall uint64             `json:"nextModelCall"`
	NextToolCall  uint64             `json:"nextToolCall"`
	ExceededLimit *Limit             `json:"exceededLimit,omitempty"`
}

// SaveBatch must be stored atomically. Adapters enforce PreviousEventNumber and accept the same
// BatchId/digest only once. The application also checks its worker lease in that transaction.
// Use CodecRegistry.Marshal for portable JSON that preserves concrete content and metadata types.
type SaveBatch struct {
	SchemaVersion       int              `json:"schemaVersion"`
	RunId               string           `json:"runId"`
	BatchId             string           `json:"batchId"`
	PreviousEventNumber uint64           `json:"previousEventNumber"`
	Events              []*RecordedEvent `json:"events"`
	Checkpoint          *Checkpoint      `json:"checkpoint,omitempty"`
	Runtime             RuntimeState     `json:"runtime"`
}

type Recorder interface {
	Save(context.Context, *SaveBatch) error
}

// RunView is a complete committed view, including progress beyond its resumable checkpoint.
// The cursor and contents always advance together through ApplyBatch.
type RunView struct {
	SchemaVersion          int               `json:"schemaVersion"`
	RunId                  string            `json:"runId"`
	LastAppliedEventNumber uint64            `json:"lastAppliedEventNumber"`
	History                HistorySnapshot   `json:"history"`
	Activity               []*RecordedEvent  `json:"activity"`
	Checkpoint             *Checkpoint       `json:"checkpoint,omitempty"`
	Runtime                RuntimeState      `json:"runtime"`
	Batches                map[string]string `json:"batches"`
}

var (
	ErrEventGap           = errors.New("gent: event sequence gap")
	ErrWrongRun           = errors.New("gent: wrong run identity")
	ErrHistoryConflict    = errors.New("gent: history revision conflict")
	ErrPersistenceStopped = errors.New("gent: recording stopped after a save failure")
)

// PersistenceError retains the exact uncertain batch. Do not issue new external work until the
// application reconciles this batch with storage or restores the last accepted run.
type PersistenceError struct {
	Batch *SaveBatch
	Cause error
}

func (e *PersistenceError) Error() string {
	return fmt.Sprintf("gent: save batch %s: %v", e.Batch.BatchId, e.Cause)
}

func (e *PersistenceError) Unwrap() error { return e.Cause }

// Export returns an owned snapshot of accepted state. Recorder.Save may call it while a candidate
// is being saved: application I/O never holds the run's reader lock.
func (ctx *ExecutionContext) Export() (RunView, error) {
	ctx.run.mu.RLock()
	view := ctx.run.view
	// Only these indexes grow in place. Payloads and other state are immutable after acceptance.
	view.Activity = slices.Clone(view.Activity)
	view.Batches = maps.Clone(view.Batches)
	ctx.run.mu.RUnlock()
	return cloneOwned(ctx.run.codecs, view)
}

// accept runs under flushMu. Only this path allocates event numbers and advances accepted state.
func (run *executionRun) accept(ctx context.Context, input *SaveBatch) (*SaveBatch, error) {
	if run.failed != nil {
		batch, err := cloneOwned(run.codecs, run.failed.Batch)
		if err != nil {
			return nil, err
		}
		return nil, &PersistenceError{Batch: batch,
			Cause: errors.Join(ErrPersistenceStopped, run.failed.Cause)}
	}
	run.mu.RLock()
	view := run.view
	run.mu.RUnlock()
	// Pending payloads are already frozen by their input boundary. Copy only the envelopes we
	// number here; serializing every payload again would make each streamed token pay that cost.
	events := input.Events
	if len(events) == 0 {
		events = []*RecordedEvent{{Type: "checkpoint"}}
	}
	batch := &SaveBatch{SchemaVersion: RunSchemaVersion, RunId: view.RunId,
		PreviousEventNumber: view.LastAppliedEventNumber, Events: make([]*RecordedEvent, len(events)),
		Runtime: input.Runtime}
	for i, pending := range events {
		if pending == nil {
			return nil, errors.New("gent: nil recorded event")
		}
		event := *pending
		if event.RunId != "" && event.RunId != view.RunId {
			return nil, ErrWrongRun
		}
		event.RunId = view.RunId
		event.EventNumber = view.LastAppliedEventNumber + uint64(i) + 1
		if event.Ts.IsZero() {
			event.Ts = time.Now()
		}
		batch.Events[i] = &event
	}
	if input.Checkpoint != nil {
		checkpoint := *input.Checkpoint
		checkpoint.EventNumber = batch.Events[len(batch.Events)-1].EventNumber
		batch.Checkpoint = &checkpoint
	}
	var err error
	batch.BatchId, err = BatchDigest(batch, run.codecs)
	if err != nil {
		return nil, err
	}
	return run.save(ctx, view, batch)
}

func (run *executionRun) save(
	ctx context.Context, view RunView, batch *SaveBatch,
) (*SaveBatch, error) {
	next, err := prepareBatch(view, batch, batch.BatchId)
	if err != nil {
		return nil, err
	}
	if run.recorder != nil {
		owned, err := cloneOwned(run.codecs, batch)
		if err != nil {
			return nil, err
		}
		if err := run.recorder.Save(ctx, owned); err != nil {
			run.failed = &PersistenceError{Batch: batch, Cause: err}
			owned, copyErr := cloneOwned(run.codecs, batch)
			if copyErr != nil {
				return nil, errors.Join(err, copyErr)
			}
			return nil, &PersistenceError{Batch: owned, Cause: err}
		}
	}
	run.mu.Lock()
	if next.LastAppliedEventNumber != view.LastAppliedEventNumber {
		// Commit only after Save succeeds. Export copies these indexes under the same lock.
		next.Activity = append(view.Activity, batch.Events...)
		if next.Batches == nil {
			next.Batches = make(map[string]string)
		}
		next.Batches[batch.BatchId] = batch.BatchId
	}
	run.view = next
	run.mu.Unlock()
	run.failed = nil
	return batch, nil
}

// RetrySave sends exactly the failed batch again. A timeout does not establish whether Save
// committed. Retry only reconciles storage; resume from the resulting export on a fresh context.
func (ctx *ExecutionContext) RetrySave(worker context.Context) (RunView, error) {
	run := ctx.run
	run.flushMu.Lock()
	if run.failed == nil {
		run.flushMu.Unlock()
		return RunView{}, errors.New("gent: no failed batch to retry")
	}
	run.mu.RLock()
	view := run.view
	run.mu.RUnlock()
	batch, err := run.save(worker, view, run.failed.Batch)
	if err == nil {
		run.mu.Lock()
		run.notifications = append(run.notifications, committedNotification{ctx, batch.Events})
		run.mu.Unlock()
	}
	run.flushMu.Unlock()
	if err != nil {
		return RunView{}, err
	}
	if err := ctx.notifyCommitted(); err != nil {
		return RunView{}, err
	}
	return ctx.Export()
}

// BatchDigest includes typed values, so distinct numeric or custom content types cannot share
// an identity merely because ordinary JSON would render them alike.
func BatchDigest(batch *SaveBatch, registries ...*CodecRegistry) (string, error) {
	if batch == nil {
		return "", errors.New("gent: nil save batch")
	}
	copy := *batch
	copy.BatchId = ""
	data, err := registryOrDefault(registries).Marshal(&copy)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func registryOrDefault(registries []*CodecRegistry) *CodecRegistry {
	if len(registries) > 0 && registries[0] != nil {
		return registries[0]
	}
	return NewCodecRegistry()
}
