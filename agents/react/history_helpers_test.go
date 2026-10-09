package react

import (
	"github.com/rickchristie/gent"
	"testing"
)

func setTestRecords(t testing.TB, ctx *gent.ExecutionContext, records []*gent.HistoryEntry) {
	t.Helper()
	draft := ctx.ContextDraft()
	var entries []*gent.HistoryEntry
	var active []gent.EntryId
	for _, record := range records {
		entry, err := ctx.NewHistoryEntry(record)
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry)
		active = append(active, entry.Id)
	}
	if err := ctx.UpdateHistory(
		gent.HistoryUpdate{
			ExpectedDraftRevision: draft.Revision,
			AppendedEntries:       entries,
			Active:                active,
			Reason:                gent.ContextReplace,
		},
	); err != nil {
		t.Fatal(err)
	}
}
func testScratchpad(t testing.TB, ctx *gent.ExecutionContext) []*gent.HistoryEntry {
	t.Helper()
	records, err := ctx.Scratchpad()
	if err != nil {
		t.Fatal(err)
	}
	result := make([]*gent.HistoryEntry, len(records))
	for i := range records {
		result[i] = recordContent(records[i])
	}
	return result
}
func testHistoryRecords(t testing.TB, ctx *gent.ExecutionContext) []*gent.HistoryEntry {
	t.Helper()
	if err := ctx.FlushProgress(); err != nil {
		t.Fatal(err)
	}
	var records []*gent.HistoryEntry
	for _, entry := range ctx.History().Entries {
		if entry.Kind == gent.HistoryCompaction {
			continue
		}
		records = append(records, recordContent(entry))
	}
	return records
}

// These prompt/expiry tests compare complete message content. Durable identity and
// ownership are tested separately; strip execution-assigned fields from these owned snapshots.
func recordContent(entry *gent.HistoryEntry) *gent.HistoryEntry {
	return &gent.HistoryEntry{Messages: entry.Messages, Origin: entry.Origin,
		ExpireAfterIteration: entry.ExpireAfterIteration, Metadata: entry.Metadata}
}
