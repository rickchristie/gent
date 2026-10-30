package gent

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBatchDigestPreservesEvidence(t *testing.T) {
	type input struct {
		change func(*SaveBatch)
	}
	type expected struct {
		sameDigest bool
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"same evidence", input{func(*SaveBatch) {}}, expected{true}},
		{"batch ID excluded", input{func(b *SaveBatch) { b.BatchId = "assigned" }}, expected{true}},
		{"numeric type", input{func(b *SaveBatch) {
			b.Events[0].Change.AppendedEntries[0].Metadata["fixture:count"] = uint64(7)
		}}, expected{false}},
		{"history text", input{func(b *SaveBatch) {
			b.Events[0].Change.AppendedEntries[0].Messages[0].Parts[0] = TextContent{Text: "changed"}
		}}, expected{false}},
		{"payload type", input{func(b *SaveBatch) { b.Events[1].Payload.Type = "/uint64" }},
			expected{false}},
		{"payload value", input{func(b *SaveBatch) {
			b.Events[1].Payload.Data = json.RawMessage(`8`)
		}}, expected{false}},
		{"runtime", input{func(b *SaveBatch) { b.Runtime.Allocation++ }}, expected{false}},
		{"checkpoint", input{func(b *SaveBatch) { b.Checkpoint.Phase = PhaseTerminal }},
			expected{false}},
		{"event order", input{func(b *SaveBatch) {
			b.Events[0], b.Events[1] = b.Events[1], b.Events[0]
		}}, expected{false}},
	}
	registry := NewCodecRegistry()
	batch := &SaveBatch{SchemaVersion: RunSchemaVersion, RunId: "fixture",
		Events: []*RecordedEvent{
			{EventNumber: 1, RunId: "fixture", Type: "state_change",
				Ts: time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC),
				Change: &HistoryChange{AppendedEntries: []*HistoryEntry{{
					Messages: []*MessageContent{{Role: ChatMessageTypeHuman,
						Parts: []ContentPart{TextContent{Text: "original"}}}},
					Metadata: map[RecordMetadataKey]any{"fixture:count": int64(7)},
				}}}},
			{EventNumber: 2, RunId: "fixture", Type: "custom",
				Payload: &EncodedValue{Type: "/int64", Version: 1, Data: json.RawMessage(`7`)}},
		}, Checkpoint: &Checkpoint{Phase: PhaseIterationReady},
	}
	original, err := registry.Clone(batch)
	assert.NoError(t, err)
	digest, err := BatchDigest(batch, registry)
	assert.NoError(t, err)
	assert.Equal(t, original, batch)
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			owned, err := registry.Clone(batch)
			if !assert.NoError(t, err) {
				return
			}
			candidate := owned.(*SaveBatch)
			tt.input.change(candidate)
			before, err := registry.Clone(candidate)
			assert.NoError(t, err)
			actual, err := BatchDigest(candidate, registry)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected.sameDigest, digest == actual)
			assert.Equal(t, before, candidate)
		})
	}
}
