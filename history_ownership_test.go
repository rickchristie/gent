package gent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type ownershipMetadata struct {
	Id     uint64
	Values []string
}

type ownershipRecorder struct {
	registry *CodecRegistry
	batch    *SaveBatch
	bytes    []byte
}

func (recorder *ownershipRecorder) Save(_ context.Context, batch *SaveBatch) error {
	data, err := recorder.registry.Marshal(batch)
	if err != nil {
		return err
	}
	recorder.batch, recorder.bytes = batch, data
	return nil
}

type ownershipObserver struct{ events []*RecordedEvent }

func (observer *ownershipObserver) ObserveCommitted(
	events []*RecordedEvent, _ *CodecRegistry,
) error {
	observer.events = events
	return nil
}

func TestHistoryEntryOwnershipAndTypedReplay(t *testing.T) {
	type input struct{ source string }
	type expected struct {
		message  string
		metadata *ownershipMetadata
		err      error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	want := expected{"original policy", &ownershipMetadata{1<<63 + 19, []string{"original"}}, nil}
	cases := []testCase{
		{"caller input", input{"caller"}, want},
		{"prepared proposal", input{"proposal"}, want},
		{"working context read", input{"scratchpad"}, want},
		{"history read", input{"history"}, want},
		{"recorder input", input{"recorder"}, want},
		{"observer input", input{"observer"}, want},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			registry := NewCodecRegistry()
			assert.NoError(t, registry.RegisterType("test/ownership", 1, ownershipMetadata{}))
			recorder := &ownershipRecorder{registry: registry}
			observer := &ownershipObserver{}
			ctx := NewExecutionContext(context.Background(), "root", nil, WithRunId("ownership"),
				WithCodecs(registry), WithRecorder(recorder), WithObserver(observer))
			caller := &HistoryEntry{Messages: []*MessageContent{{Role: ChatMessageTypeAI,
				Parts: []ContentPart{TextContent{Text: "original policy"}}}},
				Metadata: map[RecordMetadataKey]any{"custom": &ownershipMetadata{
					Id: 1<<63 + 19, Values: []string{"original"}}}}
			started := time.Now()
			proposal, err := ctx.NewHistoryEntry(caller)
			if !assert.NoError(t, err) {
				return
			}
			assert.NoError(t, ctx.UpdateHistory(HistoryUpdate{AppendedEntries: []*HistoryEntry{proposal},
				Active: []EntryId{proposal.Id}, Reason: ContextAppend}))
			if !assert.NoError(t, ctx.CommitCheckpoint(PhaseInitial)) {
				return
			}
			before, err := ctx.Export()
			if !assert.NoError(t, err) {
				return
			}
			entry := before.History.Entries[0]
			assert.GreaterOrEqual(t, entry.CreatedAt, started)
			assert.Equal(t, &HistoryEntry{Id: proposal.Id, Order: 1, RunId: ctx.RunId(),
				ContextId: ctx.ContextId(), AttemptId: ctx.AttemptId(), Kind: HistoryMessages,
				CreatedAt: entry.CreatedAt, Messages: []*MessageContent{{Role: ChatMessageTypeAI,
					Parts: []ContentPart{TextContent{Text: test.expected.message}}}},
				Metadata: map[RecordMetadataKey]any{"custom": test.expected.metadata}}, entry)
			var target *HistoryEntry
			switch test.input.source {
			case "caller":
				target = caller
			case "proposal":
				target = proposal
			case "scratchpad":
				entries, err := ctx.Scratchpad()
				if !assert.NoError(t, err) {
					return
				}
				target = entries[0]
			case "history":
				target, _ = ctx.History().Entry(proposal.Id)
			case "recorder":
				target = recorder.batch.Events[0].Change.AppendedEntries[0]
			case "observer":
				target = observer.events[0].Change.AppendedEntries[0]
			}
			target.Messages[0].Parts[0] = TextContent{Text: "changed"}
			target.Metadata["custom"].(*ownershipMetadata).Values[0] = "changed"
			after, err := ctx.Export()
			assert.ErrorIs(t, err, test.expected.err)
			assert.Equal(t, before, after)
			// A replacement reader has only stored JSON and fresh type registrations.
			fresh := NewCodecRegistry()
			assert.NoError(t, fresh.RegisterType("test/ownership", 1, ownershipMetadata{}))
			var batch *SaveBatch
			if !assert.NoError(t, fresh.Unmarshal(recorder.bytes, &batch)) {
				return
			}
			replayed, err := ApplyBatch(RunView{SchemaVersion: RunSchemaVersion, RunId: ctx.RunId()},
				batch, fresh)
			assert.ErrorIs(t, err, test.expected.err)
			assert.Equal(t, before, replayed)
			data, err := registry.Marshal(before)
			if !assert.NoError(t, err) {
				return
			}
			var loaded RunView
			assert.NoError(t, fresh.Unmarshal(data, &loaded))
			assert.Equal(t, before, loaded)
		})
	}
}

func TestHistoryRejectsNilPointers(t *testing.T) {
	type input struct{ kind string }
	type expected struct{ errorText string }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"constructor", input{"constructor"}, expected{"gent: nil history entry"}},
		{"update", input{"update"}, expected{"gent: nil history entry"}},
		{"message", input{"message"}, expected{"gent: nil history message"}},
		{"batch", input{"batch"}, expected{"gent: nil save batch"}},
		{"event", input{"event"}, expected{"gent: nil recorded event"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx := NewExecutionContext(context.Background(), "root", nil, WithRunId("nil-test"))
			before, err := ctx.Export()
			if !assert.NoError(t, err) {
				return
			}
			switch test.input.kind {
			case "constructor":
				_, err = ctx.NewHistoryEntry(nil)
			case "update":
				err = ctx.UpdateHistory(HistoryUpdate{AppendedEntries: []*HistoryEntry{nil}})
			case "message":
				entry, makeErr := ctx.NewHistoryEntry(&HistoryEntry{Messages: []*MessageContent{nil}})
				if !assert.NoError(t, makeErr) {
					return
				}
				err = ctx.UpdateHistory(HistoryUpdate{AppendedEntries: []*HistoryEntry{entry},
					Active: []EntryId{entry.Id}})
			case "batch":
				_, err = ApplyBatch(before, nil)
			case "event":
				batch := &SaveBatch{SchemaVersion: RunSchemaVersion, RunId: before.RunId,
					Events: []*RecordedEvent{nil}}
				batch.BatchId, err = BatchDigest(batch)
				if !assert.NoError(t, err) {
					return
				}
				_, err = ApplyBatch(before, batch)
			}
			assert.EqualError(t, err, test.expected.errorText)
			after, err := ctx.Export()
			assert.NoError(t, err)
			assert.Equal(t, before, after)
		})
	}
}
