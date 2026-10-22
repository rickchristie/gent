package gent

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
)

// ApplyBatch reconstructs accepted state without running hooks, models, tools, or live stats.
// A rejected batch leaves its inputs untouched. Supply the registry for custom content types;
// codecs must be deterministic local serialization functions. Exact duplicate batches are safe.
func ApplyBatch(view RunView, batch *SaveBatch, registries ...*CodecRegistry) (RunView, error) {
	registry := registryOrDefault(registries)
	owned, err := cloneOwned(registry, batch)
	if err != nil {
		return RunView{}, err
	}
	digest, err := BatchDigest(owned, registry)
	if err != nil {
		return RunView{}, err
	}
	next, err := applyBatch(view, owned, digest)
	if err != nil {
		return RunView{}, err
	}
	return cloneOwned(registry, next)
}

// applyBatch preserves the public replay contract: a rejected batch changes neither input.
func applyBatch(view RunView, batch *SaveBatch, digest string) (RunView, error) {
	next, err := prepareBatch(view, batch, digest)
	if err != nil || next.LastAppliedEventNumber == view.LastAppliedEventNumber {
		return next, err
	}
	next.Activity = append(append([]*RecordedEvent(nil), view.Activity...), batch.Events...)
	next.Batches = make(map[string]string, len(view.Batches)+1)
	for id, digest := range view.Batches {
		next.Batches[id] = digest
	}
	next.Batches[batch.BatchId] = digest
	return next, nil
}

// prepareBatch validates a candidate without copying the growing event and batch indexes.
// All changed history, runtime, and checkpoint values remain separate from the accepted view.
func prepareBatch(view RunView, batch *SaveBatch, digest string) (RunView, error) {
	if batch == nil {
		return RunView{}, errors.New("gent: nil save batch")
	}
	if batch.SchemaVersion != RunSchemaVersion || view.SchemaVersion != RunSchemaVersion {
		return RunView{}, errors.New("gent: unsupported run schema version")
	}
	if batch.RunId != view.RunId || batch.RunId == "" {
		return RunView{}, ErrWrongRun
	}
	if batch.BatchId == "" || batch.BatchId != digest {
		return RunView{}, errors.New("gent: batch digest mismatch")
	}
	if old, ok := view.Batches[batch.BatchId]; ok {
		if old != digest {
			return RunView{}, errors.New("gent: conflicting duplicate batch")
		}
		return view, nil
	}
	if view.Checkpoint != nil && view.Checkpoint.Phase == PhaseTerminal {
		return RunView{}, errors.New("gent: terminal run cannot accept new work")
	}
	if batch.PreviousEventNumber != view.LastAppliedEventNumber || len(batch.Events) == 0 {
		return RunView{}, ErrEventGap
	}
	next := view
	for _, event := range batch.Events {
		if event == nil {
			return RunView{}, errors.New("gent: nil recorded event")
		}
		if event.RunId != view.RunId {
			return RunView{}, ErrWrongRun
		}
		if event.EventNumber != next.LastAppliedEventNumber+1 {
			return RunView{}, ErrEventGap
		}
		if event.Type == "" || event.Ts.IsZero() {
			return RunView{}, errors.New("gent: event has no type or timestamp")
		}
		if event.Change != nil {
			if event.Type != "state_change" {
				return RunView{}, errors.New("gent: history change has wrong event type")
			}
			if err := applyHistory(&next.History, *event.Change, next.RunId); err != nil {
				return RunView{}, err
			}
		}
		next.LastAppliedEventNumber = event.EventNumber
	}
	if batch.Runtime.Allocation < next.Runtime.Allocation {
		return RunView{}, errors.New("gent: allocation high-water mark moved backwards")
	}
	if batch.Runtime.Contexts != nil {
		if err := validateRuntime(next.Runtime, batch.Runtime); err != nil {
			return RunView{}, err
		}
		next.Runtime = cloneWire(batch.Runtime)
	} else {
		next.Runtime.Allocation = batch.Runtime.Allocation
	}
	if batch.Checkpoint != nil {
		if err := validateCheckpoint(*batch.Checkpoint, next); err != nil {
			return RunView{}, err
		}
		next.Checkpoint = cloneWire(batch.Checkpoint)
	}
	return next, nil
}

func applyHistory(history *HistorySnapshot, change HistoryChange, runId string) error {
	if change.PreviousRevision != history.Revision || change.Revision != history.Revision+1 {
		return ErrHistoryConflict
	}
	entries := make(map[EntryId]*HistoryEntry, len(history.Entries)+len(change.AppendedEntries))
	for _, entry := range history.Entries {
		if entry == nil {
			return errors.New("gent: nil history entry")
		}
		entries[entry.Id] = entry
	}
	for i, entry := range change.AppendedEntries {
		if entry == nil {
			return errors.New("gent: nil history entry")
		}
		if entry.Id == "" || entry.RunId != runId || entry.ContextId == "" {
			return errors.New("gent: invalid history entry identity")
		}
		if entry.Order != uint64(len(history.Entries)+i+1) || entry.CreatedAt.IsZero() {
			return errors.New("gent: invalid history entry order or timestamp")
		}
		if _, exists := entries[entry.Id]; exists {
			return fmt.Errorf("gent: history entry %s already exists", entry.Id)
		}
		switch entry.Kind {
		case HistoryMessages, HistorySummary:
			if entry.Compaction != nil {
				return errors.New("gent: message entry cannot contain a compaction")
			}
			for _, message := range entry.Messages {
				if message == nil {
					return errors.New("gent: nil history message")
				}
			}
		case HistoryCompaction:
			if entry.Compaction == nil || len(entry.Messages) != 0 {
				return errors.New("gent: compaction entry requires a compaction only")
			}
		default:
			return fmt.Errorf("gent: unknown history entry kind %q", entry.Kind)
		}
		entries[entry.Id] = entry
	}
	for _, entry := range change.AppendedEntries {
		for _, replaced := range entry.Replaces {
			old, ok := entries[replaced]
			if !ok || old.ContextId != entry.ContextId || old.Order >= entry.Order {
				return errors.New("gent: invalid replacement reference")
			}
		}
		if entry.Compaction != nil {
			lists := [][]EntryId{entry.Compaction.InputIds, entry.Compaction.Active,
				entry.Compaction.SummaryIds}
			for _, ids := range lists {
				if err := validateSelection(entries, entry.ContextId, ids); err != nil {
					return err
				}
			}
			for _, id := range entry.Compaction.SummaryIds {
				if entries[id].Kind != HistorySummary {
					return errors.New("gent: compaction summary reference is not a summary")
				}
			}
		}
	}
	seenContexts := map[string]bool{}
	for _, selection := range change.Selections {
		if seenContexts[selection.ContextId] || selection.ContextId == "" {
			return errors.New("gent: duplicate or empty context selection")
		}
		seenContexts[selection.ContextId] = true
		if err := validateSelection(entries, selection.ContextId, selection.Active); err != nil {
			return err
		}
	}
	for _, state := range change.CustomState {
		if state.Name == "" || state.ContextId == "" || !json.Valid(state.Value.Data) {
			return errors.New("gent: invalid custom state replacement")
		}
	}
	history.Entries = append(append([]*HistoryEntry(nil), history.Entries...),
		change.AppendedEntries...)
	history.Changes = append(append([]HistoryChange(nil), history.Changes...), change)
	history.Revision = change.Revision
	return nil
}

func validateSelection(
	entries map[EntryId]*HistoryEntry, contextId string, active []EntryId,
) error {
	seen := map[EntryId]bool{}
	for _, id := range active {
		entry, ok := entries[id]
		if !ok || entry.ContextId != contextId || entry.Kind == HistoryCompaction {
			return fmt.Errorf("gent: invalid active entry %q in context %q", id, contextId)
		}
		if seen[id] {
			return fmt.Errorf("gent: duplicate active entry %q", id)
		}
		seen[id] = true
	}
	return nil
}

func validateRuntime(previous, next RuntimeState) error {
	seen := map[string]bool{}
	oldById := map[string]ContextRuntime{}
	for _, old := range previous.Contexts {
		oldById[old.ContextId] = old
	}
	for _, state := range next.Contexts {
		if state.ContextId == "" || seen[state.ContextId] {
			return errors.New("gent: invalid runtime context identity")
		}
		seen[state.ContextId] = true
		old := oldById[state.ContextId]
		if state.NextModelCall < old.NextModelCall || state.NextToolCall < old.NextToolCall {
			return errors.New("gent: call allocation moved backwards")
		}
		for key, value := range state.Counters {
			if value < 0 || value < oldById[state.ContextId].Counters[key] {
				return fmt.Errorf("gent: counter %q moved backwards", key)
			}
		}
		for key, value := range oldById[state.ContextId].Counters {
			if state.Counters[key] < value {
				return fmt.Errorf("gent: counter %q is missing", key)
			}
		}
	}
	for id := range oldById {
		if !seen[id] {
			return errors.New("gent: runtime context disappeared")
		}
	}
	return nil
}

func validateCheckpoint(checkpoint Checkpoint, view RunView) error {
	if checkpoint.SchemaVersion != RunSchemaVersion || checkpoint.RunId != view.RunId {
		return ErrIncompatibleCheckpoint
	}
	if checkpoint.EventNumber != view.LastAppliedEventNumber ||
		checkpoint.Revision != view.History.Revision {
		return errors.New("gent: checkpoint cursor or history revision mismatch")
	}
	if len(checkpoint.Contexts) == 0 {
		return errors.New("gent: checkpoint has no root context")
	}
	if checkpoint.Contexts[0].ParentContextId != "" {
		return errors.New("gent: first checkpoint context is not the root")
	}
	switch checkpoint.Phase {
	case PhaseInitial, PhaseIterationReady, PhaseDecisionReady, PhaseToolReady, PhaseToolCompleted,
		PhaseIterationCompleted, PhaseCompactionCompleted, PhaseTerminal:
	default:
		return ErrUnsupportedRecovery
	}
	if (checkpoint.Phase == PhaseTerminal) != (checkpoint.Outcome != nil) {
		return errors.New("gent: terminal checkpoint and outcome disagree")
	}
	seen := map[string]bool{}
	for i, state := range checkpoint.Contexts {
		if state.ContextId == "" || seen[state.ContextId] {
			return errors.New("gent: duplicate checkpoint context")
		}
		if i > 0 && !seen[state.ParentContextId] {
			return errors.New("gent: checkpoint parent must precede child")
		}
		seen[state.ContextId] = true
		active, err := view.History.ContextAt(state.ContextId, checkpoint.Revision)
		if err != nil {
			return err
		}
		if !equalIds(active, state.Active) {
			return errors.New("gent: checkpoint selection differs from history")
		}
	}
	return nil
}

func equalIds(a, b []EntryId) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ValidateBundle reconstructs history from the complete activity sequence. A bundle cannot claim
// a cursor for missing events or silently substitute a newer selection for an older checkpoint.
func ValidateBundle(bundle RunView, registries ...*CodecRegistry) error {
	if bundle.SchemaVersion != RunSchemaVersion || bundle.RunId == "" {
		return ErrIncompatibleCheckpoint
	}
	if _, err := registryOrDefault(registries).Encode(bundle); err != nil {
		return err
	}
	var history HistorySnapshot
	var checkpointRevision HistoryRevision
	for i, event := range bundle.Activity {
		if event == nil {
			return errors.New("gent: nil recorded event")
		}
		if event.RunId != bundle.RunId {
			return ErrWrongRun
		}
		if event.EventNumber != uint64(i+1) {
			return ErrEventGap
		}
		if event.Type == "" || event.Ts.IsZero() ||
			(event.Change != nil && event.Type != "state_change") {
			return errors.New("gent: malformed recorded event")
		}
		if event.Change != nil {
			if err := applyHistory(&history, *event.Change, bundle.RunId); err != nil {
				return err
			}
		}
		if bundle.Checkpoint != nil && event.EventNumber == bundle.Checkpoint.EventNumber {
			checkpointRevision = history.Revision
		}
	}
	if bundle.LastAppliedEventNumber != uint64(len(bundle.Activity)) {
		return ErrEventGap
	}
	if !reflect.DeepEqual(history, bundle.History) {
		return errors.New("gent: history does not match recorded transitions")
	}
	if bundle.Checkpoint != nil {
		if bundle.Checkpoint.EventNumber == 0 || bundle.Checkpoint.Revision != checkpointRevision {
			return errors.New("gent: checkpoint revision differs from its committed cursor")
		}
		checkpointView := bundle
		checkpointView.LastAppliedEventNumber = bundle.Checkpoint.EventNumber
		checkpointView.History.Revision = bundle.Checkpoint.Revision
		if bundle.Checkpoint.EventNumber > bundle.LastAppliedEventNumber ||
			bundle.Checkpoint.Revision > bundle.History.Revision {
			return errors.New("gent: checkpoint is ahead of committed activity")
		}
		if err := validateCheckpoint(*bundle.Checkpoint, checkpointView); err != nil {
			return err
		}
	}
	return validateRuntime(RuntimeState{}, bundle.Runtime)
}
