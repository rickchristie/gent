package gent

import (
	"testing"
)

func setTestRecords(t testing.TB, ctx *ExecutionContext, records []*HistoryEntry) {
	t.Helper()
	draft := ctx.ContextDraft()
	var entries []*HistoryEntry
	var active []EntryId
	for _, record := range records {
		entry, err := ctx.NewHistoryEntry(record)
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry)
		active = append(active, entry.Id)
	}
	if err := ctx.UpdateHistory(
		HistoryUpdate{
			ExpectedDraftRevision: draft.Revision,
			AppendedEntries:       entries,
			Active:                active,
			Reason:                ContextReplace,
		},
	); err != nil {
		t.Fatal(err)
	}
}

// These prompt/expiry tests compare complete message content. Durable identity and
// ownership are tested separately; strip execution-assigned fields from these owned snapshots.
func recordContent(entry *HistoryEntry) *HistoryEntry {
	return &HistoryEntry{Messages: entry.Messages, Origin: entry.Origin,
		ExpireAfterIteration: entry.ExpireAfterIteration, Metadata: entry.Metadata}
}
