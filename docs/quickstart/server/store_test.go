package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func testStore(t *testing.T, path string) *Store {
	t.Helper()
	codecs := gent.NewCodecRegistry()
	assert.NoError(t, newAgent(&DemoModel{}).RegisterCodecs(codecs))
	store, err := openStore(path, codecs)
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	t.Cleanup(func() { assert.NoError(t, store.db.Close()) })
	return store
}

func savedBatch(t *testing.T, number, previous uint64) *gent.SaveBatch {
	t.Helper()
	batch := &gent.SaveBatch{SchemaVersion: gent.RunSchemaVersion, RunId: "test",
		PreviousEventNumber: previous, Events: []*gent.RecordedEvent{{
			RunId: "test", EventNumber: number, Type: "checkpoint",
			Ts: time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC),
		}}}
	var err error
	batch.BatchId, err = gent.BatchDigest(batch)
	assert.NoError(t, err)
	return batch
}

func TestSaveTransaction(t *testing.T) {
	type input struct {
		number, previous uint64
		replaceWriter    bool
	}
	type expected struct{ err error }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"duplicate is idempotent", input{1, 0, false}, expected{}},
		{"gap is rejected", input{3, 1, false}, expected{gent.ErrEventGap}},
		{"stale cursor is rejected", input{2, 0, false}, expected{gent.ErrEventGap}},
		{"old worker is fenced", input{2, 1, true}, expected{errStaleWriter}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			store := testStore(t, filepath.Join(t.TempDir(), "runs.db"))
			writer, err := store.claim("test")
			assert.NoError(t, err)
			assert.NoError(t, writer.Save(context.Background(), savedBatch(t, 1, 0)))
			before, err := store.load("test")
			assert.NoError(t, err)
			if test.input.replaceWriter {
				_, err := store.claim("test")
				assert.NoError(t, err)
			}
			err = writer.Save(context.Background(),
				savedBatch(t, test.input.number, test.input.previous))
			assert.True(t, errors.Is(err, test.expected.err), "unexpected error: %v", err)
			after, err := store.load("test")
			assert.NoError(t, err)
			assert.Equal(t, before, after)
		})
	}
}
