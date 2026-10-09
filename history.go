package gent

import (
	"encoding/json"
	"fmt"
)

type EntryId string
type HistoryRevision uint64
type HistoryEntryKind string
type ContextChangeKind string

const (
	HistoryMessages   HistoryEntryKind  = "messages"
	HistorySummary    HistoryEntryKind  = "summary"
	HistoryCompaction HistoryEntryKind  = "compaction"
	ContextAppend     ContextChangeKind = "append"
	ContextReplace    ContextChangeKind = "replace"
	ContextReorder    ContextChangeKind = "reorder"
	ContextExpire     ContextChangeKind = "expire"
	ContextCompact    ContextChangeKind = "compact"
	ContextCustom     ContextChangeKind = "custom"
)

type CompactionRecord struct {
	Strategy     string      `json:"strategy"`
	InputIds     []EntryId   `json:"inputIds"`
	Active       []EntryId   `json:"active"`
	SummaryIds   []EntryId   `json:"summaryIds"`
	ModelCallIds []string    `json:"modelCallIds,omitempty"`
	Error        *SavedError `json:"error,omitempty"`
}

type ContextSelection struct {
	ContextId string    `json:"contextId"`
	Active    []EntryId `json:"active"`
}

// StateValue replaces one named application value. It is structured data, not a text diff.
type StateValue struct {
	ContextId string       `json:"contextId"`
	Name      string       `json:"name"`
	Value     EncodedValue `json:"value"`
}

type HistoryChange struct {
	PreviousRevision HistoryRevision    `json:"previousRevision"`
	Revision         HistoryRevision    `json:"revision"`
	AppendedEntries  []*HistoryEntry    `json:"appendedEntries"`
	Selections       []ContextSelection `json:"selections"`
	CustomState      []StateValue       `json:"customState,omitempty"`
	Reason           ContextChangeKind  `json:"reason"`
}

// HistoryUpdate proposes one context change. Compaction supplies its provenance here;
// the executor fills the decision identity and commits it with the trigger and checkpoint.
type HistoryUpdate struct {
	Compaction            *CompactionRecord
	ExpectedDraftRevision uint64
	AppendedEntries       []*HistoryEntry
	Active                []EntryId
	CustomState           []StateValue
	Reason                ContextChangeKind
}

type ContextDraft struct {
	Revision uint64
	Active   []EntryId
}

// HistorySnapshot includes every entry and selection transition; compaction never removes evidence.
type HistorySnapshot struct {
	Revision HistoryRevision `json:"revision"`
	Entries  []*HistoryEntry `json:"entries"`
	Changes  []HistoryChange `json:"changes"`
}

func (h HistorySnapshot) ContextAt(contextId string, revision HistoryRevision) ([]EntryId, error) {
	if revision > h.Revision {
		return nil, fmt.Errorf("gent: history revision %d is unavailable", revision)
	}
	var active []EntryId
	for _, change := range h.Changes {
		if change.Revision > revision {
			break
		}
		for _, selection := range change.Selections {
			if selection.ContextId == contextId {
				active = append([]EntryId(nil), selection.Active...)
			}
		}
	}
	return active, nil
}

// Entry returns a record within this caller-owned snapshot.
func (h HistorySnapshot) Entry(id EntryId) (*HistoryEntry, bool) {
	for _, entry := range h.Entries {
		if entry.Id == id {
			return entry, true
		}
	}
	return nil, false
}

func decodeAs[T any](registry *CodecRegistry, value EncodedValue) (T, error) {
	var zero T
	decoded, err := registry.Decode(value)
	if err != nil {
		return zero, err
	}
	if decoded == nil {
		return zero, nil
	}
	result, ok := decoded.(T)
	if !ok {
		return zero, fmt.Errorf("gent: decoded %T, expected %T", decoded, zero)
	}
	return result, nil
}

// cloneWire is used only on validated wire types with no arbitrary interface values. A failure
// here is an internal invariant violation, not an application serialization error to ignore.
func cloneWire[T any](value T) T {
	data, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("gent: invalid internal wire value: %v", err))
	}
	var result T
	if err := json.Unmarshal(data, &result); err != nil {
		panic(fmt.Sprintf("gent: invalid internal wire clone: %v", err))
	}
	return result
}
