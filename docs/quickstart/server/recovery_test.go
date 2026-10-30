package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/trace"
	"github.com/stretchr/testify/assert"
)

type stopWriter struct {
	writer *Writer
	phase  gent.ExecutionPhase
	cancel context.CancelFunc
}

func (s *stopWriter) Save(ctx context.Context, batch *gent.SaveBatch) error {
	if err := s.writer.Save(ctx, batch); err != nil {
		return err
	}
	if batch.Checkpoint != nil && batch.Checkpoint.Phase == s.phase {
		s.cancel()
	}
	return nil
}

type countedModel struct {
	DemoModel
	requests int
}

func (m *countedModel) GenerateContentStream(ctx context.Context, request gent.ModelCallRequest) (
	gent.Stream, error,
) {
	m.requests++
	return m.DemoModel.GenerateContentStream(ctx, request)
}

func TestRecoveryFromReopenedDatabase(t *testing.T) {
	type input struct{ stop gent.ExecutionPhase }
	type expected struct {
		firstCalls, resumedCalls int
		answer                   []gent.ContentPart
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"uncertain read can retry", input{gent.PhaseToolReady},
			expected{1, 1, []gent.ContentPart{
				gent.TextContent{Text: "Order order-1001 has shipped."}}}},
		{"completed read is reused", input{gent.PhaseToolCompleted},
			expected{1, 1, []gent.ContentPart{
				gent.TextContent{Text: "Order order-1001 has shipped."}}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "runs.db")
			codecs := gent.NewCodecRegistry()
			first := &countedModel{}
			agent := newAgent(first)
			assert.NoError(t, agent.RegisterCodecs(codecs))
			store, err := openStore(path, codecs)
			if !assert.NoError(t, err) {
				return
			}
			writer, err := store.claim("recover")
			assert.NoError(t, err)
			worker, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx := gent.NewExecutionContext(worker, "order-helper",
				gent.NewBasicLoopData(gent.NewTask(gent.TextContent{Text: "Check order-1001"})),
				gent.WithRunId("recover"), gent.WithCodecs(codecs),
				gent.WithRecorder(&stopWriter{writer, test.input.stop, cancel}))
			_, _, err = executor.NewRunner(agent, executor.DefaultConfig()).Execute(ctx)
			assert.ErrorIs(t, err, context.Canceled)
			before, err := store.load("recover")
			assert.NoError(t, err)
			assert.Equal(t, test.input.stop, before.Checkpoint.Phase)
			assert.Equal(t, test.expected.firstCalls, first.requests)
			assert.NoError(t, store.db.Close())

			// Reopen with a fresh codec registry, model, agent, recorder, and trace projection.
			freshStore := testStore(t, path)
			saved, err := freshStore.load("recover")
			assert.NoError(t, err)
			assert.Equal(t, before, saved)
			freshModel := &countedModel{}
			freshRunner := executor.NewRunner(newAgent(freshModel), executor.DefaultConfig())
			replacement, err := freshStore.claim("recover")
			assert.NoError(t, err)
			seq, err := trace.RestoreSequencer(saved, displayConfig(freshStore.codecs))
			assert.NoError(t, err)
			defer seq.Close()
			_, result, err := freshRunner.Resume(context.Background(), saved, gent.RestoreBindings{
				Recorder: replacement, Codecs: freshStore.codecs,
				Observers: []gent.RecordedEventObserver{seq},
			})
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, test.expected.answer, result.Output)
			assert.Equal(t, test.expected.resumedCalls, freshModel.requests)
			terminal, err := freshStore.load("recover")
			assert.NoError(t, err)
			assert.Equal(t, gent.PhaseTerminal, terminal.Checkpoint.Phase)
			assert.Greater(t, terminal.LastAppliedEventNumber, saved.LastAppliedEventNumber)
			assert.NoError(t, gent.ValidateBundle(terminal, freshStore.codecs))
			lastModel := &countedModel{}
			lastRunner := executor.NewRunner(newAgent(lastModel), executor.DefaultConfig())
			_, result, err = lastRunner.Resume(context.Background(), terminal,
				gent.RestoreBindings{Codecs: freshStore.codecs})
			assert.NoError(t, err)
			assert.Equal(t, test.expected.answer, result.Output)
			assert.Equal(t, 0, lastModel.requests)
		})
	}
}
