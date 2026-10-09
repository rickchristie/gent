package compaction

import (
	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
	"testing"
)

func testShouldCompact(
	t *testing.T,
	trigger gent.CompactionTrigger,
	ctx *gent.ExecutionContext,
) bool {
	t.Helper()
	result, err := trigger.ShouldCompact(ctx)
	assert.NoError(t, err)
	return result
}

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

// These prompt/expiry tests compare complete message content. Durable identity and
// ownership are tested separately; strip execution-assigned fields from these owned snapshots.
func recordContent(entry *gent.HistoryEntry) *gent.HistoryEntry {
	return &gent.HistoryEntry{Messages: entry.Messages, Origin: entry.Origin,
		ExpireAfterIteration: entry.ExpireAfterIteration, Metadata: entry.Metadata}
}

func applyTestCompaction(ctx *gent.ExecutionContext, strategy gent.CompactionStrategy) error {
	input, err := ctx.CompactionInput()
	if err != nil {
		return err
	}
	proposal, err := strategy.Compact(ctx, input)
	if err != nil {
		return err
	}
	proposal.Reason = gent.ContextCompact
	return ctx.UpdateHistory(proposal)
}
