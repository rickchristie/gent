package executor_test

import (
	"github.com/rickchristie/gent"
	"testing"
)

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
