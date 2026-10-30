package gent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"
)

// RunSchemaVersion identifies the saved format used by SaveBatch, RunView, and Checkpoint.
// Batch application and recovery require this version; it is separate from custom codec versions.
const RunSchemaVersion = 6

// RecordedEvent is the lossless numbered event. Payload uses the registry, while Change is a
// structured history transition. Display projections may redact data without altering this record.
type RecordedEvent struct {
	// EventNumber is the event's position in its run, starting at 1. All contexts share this
	// sequence. Events in a SaveBatch are consecutive, and an exact retry keeps their numbers.
	// Gent assigns it before live delivery. It becomes accepted only after Save succeeds.
	// Recovery discards unsaved preview numbers and continues after the saved cursor.
	EventNumber uint64 `json:"eventNumber"`

	// RunId identifies the logical run and remains the same when a replacement worker resumes it.
	// It must match the SaveBatch and RunView that contain this event.
	RunId string `json:"runId"`

	// Ts is the event timestamp. Gent supplies one when the producer leaves it unset.
	// EventNumber defines order; timestamps can be equal or arrive out of time order.
	Ts time.Time `json:"ts"`

	// Type identifies the record kind, such as model_stream_chunk, tool_call_finished, or
	// state_change. A checkpoint-only save can use a synthetic checkpoint event. The presence
	// of SaveBatch.Checkpoint, rather than this type string, identifies a new control checkpoint.
	Type string `json:"type"`

	// EventName is the name set by Gent for built-in events, or by the application for a common
	// or custom event. Type selects the record kind; EventName preserves the specific event name.
	EventName string `json:"eventName,omitempty"`

	// ContextId identifies the execution context that owns the event. It can be empty for a
	// synthetic checkpoint marker that has no individual context as its source.
	ContextId string `json:"contextId"`

	// ParentContextId identifies the source context's parent when supplied. It is empty for the
	// root context and for records without parent context information.
	ParentContextId string `json:"parentContextId,omitempty"`

	// Iteration is the source context's iteration when the event was produced. Zero is used
	// before the first iteration and for records without an iteration.
	Iteration int `json:"iteration"`

	// AttemptId identifies the worker attempt associated with this evidence. A replacement
	// worker gets a new attempt ID; recovered tool evidence can retain the interrupted ID.
	// Synthetic checkpoint markers can leave this empty.
	AttemptId string `json:"attemptId"`

	// ModelCallId links model request, chunk, and response records for one logical model call.
	// It is empty when the event has no model call identity.
	ModelCallId string `json:"modelCallId,omitempty"`

	// ToolCallId links records for one logical tool call across execution attempts. Tools can
	// use this stable identity to check external effects during recovery. It is empty otherwise.
	ToolCallId string `json:"toolCallId,omitempty"`

	// Payload holds the frozen typed event value. Decode it with CodecRegistry.DecodeEvent so
	// concrete content and metadata types survive storage. It can be nil for structural records.
	Payload *EncodedValue `json:"payload,omitempty"`

	// Change holds a structured history transition for a state_change record. ApplyBatch
	// applies it to RunView.History in event order. It is nil for other record kinds.
	Change *HistoryChange `json:"change,omitempty"`
}

// RuntimeState preserves accepted accounting and identity allocation for the whole run.
// It can advance while the control checkpoint stays unchanged. Recovery uses these saved totals
// so replay does not add usage twice or allocate an identity that saved progress already used.
type RuntimeState struct {
	// Contexts contains the full accounting snapshot for known contexts, including completed
	// children. In a SaveBatch, nil keeps the previously accepted context snapshots.
	Contexts []ContextRuntime `json:"contexts"`

	// Allocation is the last allocated number shared by history entry and child context IDs.
	// A new identity increments it before use. It must not decrease when a batch is applied.
	Allocation uint64 `json:"allocation"`
}

// ContextRuntime is one context's accounting at a save boundary. Counters and gauges are absolute
// values, not changes to replay. The context's control state is stored in ContextCheckpoint.
type ContextRuntime struct {
	// ContextId identifies the context whose accounting this snapshot contains.
	ContextId string `json:"contextId"`

	// Iteration is the context's iteration at this save. A progress save can contain accounting
	// newer than the iteration state in the last control checkpoint.
	Iteration int `json:"iteration"`

	// Counters contains all accumulated counter totals, keyed by the string form of StatKey.
	// Totals cannot decrease, and a later snapshot must retain previously saved counter keys.
	Counters map[string]int64 `json:"counters"`

	// Gauges contains the current local gauge values, keyed by the string form of StatKey.
	// Values can increase, decrease, or reset; they are restored without publishing stats events.
	Gauges map[string]float64 `json:"gauges"`

	// NextModelCall stores the last allocated automatic model-call number. A new automatic ID
	// increments this value before use. Zero means no automatic model-call ID has been allocated.
	NextModelCall uint64 `json:"nextModelCall"`

	// NextToolCall stores the last allocated tool-call number. A new ID increments this value
	// before use. Preserving it prevents new work from reusing a saved logical call identity.
	NextToolCall uint64 `json:"nextToolCall"`

	// ExceededLimit is the limit that stopped this context, or nil when no limit was exceeded.
	// Recovery preserves that stop condition instead of granting the run a new usage allowance.
	ExceededLimit *Limit `json:"exceededLimit,omitempty"`
}

// SaveBatch is one persistence request for a consecutive group of events, accepted runtime, and
// at most one new control checkpoint. It can cover part of an iteration or several local steps.
//
// Sequence numbers belong to RecordedEvent.EventNumber. For example, a request with
// PreviousEventNumber = 1 and event numbers [2, 3, 4, 5] advances the saved position to 5.
// Its first event is PreviousEventNumber + 1; its last event supplies the new saved position.
// There is no separate save-request sequence number. BatchId identifies the request for retries.
//
// SaveBatch must be stored atomically. Adapters enforce PreviousEventNumber and accept the same
// BatchId/digest only once. The application also checks its worker lease in that transaction.
// Use CodecRegistry.Marshal for portable JSON that preserves concrete content and metadata types.
type SaveBatch struct {
	// SchemaVersion identifies this request's saved format and must equal RunSchemaVersion.
	SchemaVersion int `json:"schemaVersion"`

	// RunId identifies the run to update. The view, all events, and any supplied checkpoint
	// must have this same identity.
	RunId string `json:"runId"`

	// BatchId is the BatchDigest of this exact request, including its typed values. It is a
	// content identity, not an ordinal. Keep it and all request data unchanged for an exact retry.
	BatchId string `json:"batchId"`

	// PreviousEventNumber is the saved cursor expected before this request. It must match
	// RunView.LastAppliedEventNumber for a new batch; zero means this is the first batch.
	// An already accepted exact duplicate is recognized before this cursor check.
	PreviousEventNumber uint64 `json:"previousEventNumber"`

	// Events contains a nonempty, ordered sequence with no gaps or nil entries. The first number
	// is PreviousEventNumber + 1. Store the full sequence in the same transaction as Runtime
	// and Checkpoint; applying only part of it would invalidate recovery state.
	Events []*RecordedEvent `json:"events"`

	// Checkpoint optionally replaces the resumable control state. Its EventNumber must equal
	// the last event number in Events, and its Revision must match the resulting history.
	// Nil keeps the previous checkpoint while accepting progress. Saved partial model chunks
	// therefore need not claim that the incomplete provider call can be resumed.
	Checkpoint *Checkpoint `json:"checkpoint,omitempty"`

	// Runtime contains accounting and allocation state at the end of this event group. It can
	// advance even when Checkpoint is nil; recovery must preserve that accepted progress.
	Runtime RuntimeState `json:"runtime"`
}

// Recorder is the application's persistence boundary. Attach it with WithRecorder when execution
// must survive worker replacement. Without a recorder, accepted state remains only in memory.
// Loading the stored RunView and assigning worker ownership belong to the application.
type Recorder interface {
	// Save blocks until the full batch has committed. Return nil only after durable acceptance;
	// adding the request to an asynchronous queue does not satisfy this contract.
	//
	// Store events, history changes, runtime, the optional checkpoint, cursor, and duplicate
	// receipt atomically. Check the expected cursor and the application's worker lease or fence
	// in that same transaction. ApplyBatch validates the data; the adapter supplies the transaction.
	// An exact retry must succeed without applying the same effects twice.
	//
	// Honor the supplied context deadline and return errors to stop further dependent execution.
	// A lost acknowledgment can return an error after commit; it does not prove that nothing
	// was stored. Keep socket delivery separate: its failure cannot undo a committed save.
	// The supplied batch is an owned copy. Persist the original request without changing its data.
	Save(context.Context, *SaveBatch) error
}

// RunView is a complete committed view, including progress beyond its resumable checkpoint.
// The cursor and contents always advance together through ApplyBatch.
// Pass the full view to recovery: the checkpoint refers to its history, and later saved events
// can supply completed model responses or tool results that must not execute again.
type RunView struct {
	// SchemaVersion identifies the saved format and must equal RunSchemaVersion.
	SchemaVersion int `json:"schemaVersion"`

	// RunId identifies the logical run across all of its worker attempts.
	RunId string `json:"runId"`

	// LastAppliedEventNumber is the final accepted event number, or zero for an empty view.
	// It equals the length of Activity and can be ahead of Checkpoint.EventNumber. A live
	// delivery cursor is separate and does not establish which events storage has accepted.
	LastAppliedEventNumber uint64 `json:"lastAppliedEventNumber"`

	// History contains accepted history entries and selection changes reconstructed from
	// Activity. Checkpoint contexts select entries from this history by ID and revision.
	History HistorySnapshot `json:"history"`

	// Activity contains every accepted event in order, numbered 1 through LastAppliedEventNumber.
	// It includes progress after the control checkpoint. The current view retains this full
	// sequence in memory even after a successful save clears the pending event list.
	Activity []*RecordedEvent `json:"activity"`

	// Checkpoint is the latest accepted control state, or nil before any checkpoint is saved.
	// Recovery needs a non-nil checkpoint. Progress-only saves leave this value unchanged.
	Checkpoint *Checkpoint `json:"checkpoint,omitempty"`

	// Runtime is the latest accepted accounting and allocation state. It can be newer than the
	// runtime inside Checkpoint and preserves usage and IDs from later saved progress.
	Runtime RuntimeState `json:"runtime"`

	// Batches maps accepted BatchId values to their digests. ApplyBatch uses it to recognize
	// exact retries even after the saved cursor advances. It does not contain batch snapshots.
	Batches map[string]string `json:"batches"`
}

var (
	// ErrEventGap means the expected saved cursor or consecutive event sequence does not match.
	// An empty SaveBatch.Events also fails this requirement.
	ErrEventGap = errors.New("gent: event sequence gap")

	// ErrWrongRun means a batch or event does not belong to the view's logical run.
	ErrWrongRun = errors.New("gent: wrong run identity")

	// ErrHistoryConflict means a history transition conflicts with accepted history state.
	ErrHistoryConflict = errors.New("gent: history revision conflict")

	// ErrPersistenceStopped means new acceptance was attempted while an earlier save was
	// unresolved. Reconcile that exact request before resuming execution on a fresh context.
	ErrPersistenceStopped = errors.New("gent: recording stopped after a save failure")
)

// PersistenceError retains the exact uncertain batch. Do not issue new external work until the
// application reconciles this batch with storage or restores the last accepted run.
type PersistenceError struct {
	// Batch is the exact failed or uncertain save request. Preserve its identity, event numbers,
	// and contents for reconciliation; a storage error does not prove that it was not committed.
	Batch *SaveBatch

	// Cause is the underlying failure. A blocked request also wraps ErrPersistenceStopped.
	// Unwrap exposes this error to errors.Is and errors.As.
	Cause error
}

// Error identifies the affected save request and includes its cause.
func (e *PersistenceError) Error() string {
	return fmt.Sprintf("gent: save batch %s: %v", e.Batch.BatchId, e.Cause)
}

// Unwrap returns the underlying save failure or stopped-persistence error.
func (e *PersistenceError) Unwrap() error { return e.Cause }

// Export returns an owned snapshot of accepted state. Recorder.Save may call it while a candidate
// is being saved: application I/O never holds the run's reader lock.
// It does not call Save or include pending events and uncommitted working state. A caller can
// change the returned snapshot without changing the execution context's accepted view.
func (ctx *ExecutionContext) Export() (RunView, error) {
	ctx.run.mu.RLock()
	view := ctx.run.view
	// Only these indexes grow in place. Payloads and other state are immutable after acceptance.
	view.Activity = slices.Clone(view.Activity)
	view.Batches = maps.Clone(view.Batches)
	ctx.run.mu.RUnlock()
	registry := ctx.run.codecs
	var err error
	view.Activity, err = cloneRecordedEvents(registry, view.Activity)
	if err != nil {
		return RunView{}, err
	}
	view.History, err = cloneOwned(registry, view.History)
	if err != nil {
		return RunView{}, err
	}
	view.Runtime, err = cloneOwned(registry, view.Runtime)
	if err != nil {
		return RunView{}, err
	}
	view.Checkpoint, err = cloneOwned(registry, view.Checkpoint)
	return view, err
}

// accept runs under flushMu. Event numbers were fixed before live delivery; only this path
// advances accepted state after the recorder confirms the complete group.
func (run *executionRun) accept(ctx context.Context, input *SaveBatch) (*SaveBatch, error) {
	if run.failed != nil {
		batch, err := cloneSaveBatch(run.codecs, run.failed.Batch)
		if err != nil {
			return nil, err
		}
		return nil, &PersistenceError{Batch: batch,
			Cause: errors.Join(ErrPersistenceStopped, run.failed.Cause)}
	}
	run.mu.RLock()
	view := run.view
	run.mu.RUnlock()
	// Pending payloads and sequence numbers are frozen before live delivery. Copy their envelopes
	// without changing the identities already seen by subscribers.
	events := input.Events
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
		if event.EventNumber != view.LastAppliedEventNumber+uint64(i)+1 {
			return nil, ErrEventGap
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
		owned, err := cloneSaveBatch(run.codecs, batch)
		if err != nil {
			return nil, err
		}
		if err := run.recorder.Save(ctx, owned); err != nil {
			run.failed = &PersistenceError{Batch: batch, Cause: err}
			owned, copyErr := cloneSaveBatch(run.codecs, batch)
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
// The supplied worker context controls this retry. Success does not restart the failed context
// or clear its recording error. If there is no failed batch, RetrySave returns an error.
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
// It returns a SHA-256 hexadecimal digest of the request with BatchId cleared. Supply the run's
// codec registry for custom types; omitting it uses the default registry. Gent uses this digest
// as BatchId, and ApplyBatch recomputes it before accepting a new request or an exact duplicate.
func BatchDigest(batch *SaveBatch, registries ...*CodecRegistry) (string, error) {
	if batch == nil {
		return "", errors.New("gent: nil save batch")
	}
	copy := *batch
	copy.BatchId = ""
	// Batch envelopes, runtime, and checkpoints already contain only wire values. Re-encoding
	// every field through reflection adds work for each stream chunk. Only history changes
	// contain application values that still need the registry's type and version information.
	copy.Events = make([]*RecordedEvent, len(batch.Events))
	changes := make([]*EncodedValue, len(batch.Events))
	for i, event := range batch.Events {
		if event == nil {
			continue // ApplyBatch reports invalid events after checking the digest.
		}
		owned := *event
		if event.Change != nil {
			value, err := registryOrDefault(registries).Encode(event.Change)
			if err != nil {
				return "", err
			}
			changes[i] = &value
			owned.Change = nil
		}
		copy.Events[i] = &owned
	}
	type digestValue struct {
		Batch   SaveBatch       `json:"batch"`
		Changes []*EncodedValue `json:"changes"`
	}
	data, err := json.Marshal(digestValue{Batch: copy, Changes: changes})
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
