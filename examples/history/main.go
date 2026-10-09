// Command history demonstrates application-owned transactions, writer fencing, lost publication,
// and continuation with fresh Go objects. The in-memory store is a teaching adapter, not durable
// storage. A real adapter performs the same checks and writes in a shared database transaction.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents/react"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/trace"
)

type memoryStore struct {
	mu      sync.Mutex
	view    gent.RunView
	lease   uint64
	updates chan *gent.SaveBatch
	codecs  *gent.CodecRegistry
}

func (s *memoryStore) load() (gent.RunView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.codecs.Marshal(s.view)
	if err != nil {
		return gent.RunView{}, err
	}
	var owned gent.RunView
	err = s.codecs.Unmarshal(data, &owned)
	return owned, err
}

func (s *memoryStore) nextLease() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lease++
	return s.lease
}

type writer struct {
	store   *memoryStore
	lease   uint64
	stop    gent.ExecutionPhase
	cancel  context.CancelFunc
	stopped bool
}

func (w *writer) Save(_ context.Context, batch *gent.SaveBatch) error {
	w.store.mu.Lock()
	if w.lease != w.store.lease {
		w.store.mu.Unlock()
		return errors.New("writer lease expired")
	}
	next, err := gent.ApplyBatch(w.store.view, batch, w.store.codecs)
	if err != nil {
		w.store.mu.Unlock()
		return err
	}
	w.store.view = next // The transaction commits history, activity, accounting, and checkpoint.
	w.store.mu.Unlock()
	// Publication happens after commit. This deliberately bounded channel can lose updates; a
	// browser detects gaps by cursor and periodically reloads the same full view for a lost finish.
	select {
	case w.store.updates <- batch:
	default:
		// A full reload is the chosen delivery mechanism when this demonstration queue is full.
	}
	if !w.stopped && batch.Checkpoint != nil && batch.Checkpoint.Phase == w.stop {
		w.stopped = true
		w.cancel()
	}
	return nil
}

type policyInput struct {
	Id string `json:"id"`
}

type demoModel struct{ answer bool }

func (m *demoModel) GenerateContentStream(
	context.Context,
	gent.ModelCallRequest,
) (gent.Stream, error) {
	content := `<action>
- tool: read_policy
  args:
    id: P10
</action>`
	if m.answer {
		content = "<answer>P10 permits the refund.</answer>"
	}
	m.answer = true
	return gent.NewCompletedStream(&gent.ContentResponse{
		Choices: []*gent.ContentChoice{{Content: content}},
		Info:    &gent.GenerationInfo{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
	}, nil), nil
}

func newAgent(answer bool, calls *int) *react.Agent {
	return react.NewAgent(&demoModel{answer: answer}).WithRecoveryVersion("history-example/v1").
		RegisterTool(gent.NewToolFunc("read_policy", "Read policy evidence", nil,
			func(ctx context.Context, input policyInput) (string, error) {
				if _, ok := gent.ToolIdentityFromContext(ctx); !ok {
					return "", errors.New("missing tool identity")
				}
				*calls++
				return input.Id + ": refunds are permitted within 30 days", nil
			}))
}

func run(out io.Writer) error {
	for _, stop := range []gent.ExecutionPhase{gent.PhaseToolCompleted, gent.PhaseToolReady} {
		if err := demonstrate(out, stop); err != nil {
			return err
		}
	}
	return nil
}

func demonstrate(out io.Writer, stop gent.ExecutionPhase) error {
	store := &memoryStore{
		view:    gent.RunView{SchemaVersion: gent.RunSchemaVersion, RunId: string(stop)},
		updates: make(chan *gent.SaveBatch, 1),
		codecs:  gent.NewCodecRegistry(),
	}
	worker, cancel := context.WithCancel(context.Background())
	defer cancel()
	firstWriter := &writer{store: store, lease: store.nextLease(), stop: stop, cancel: cancel}
	seq, err := trace.NewSequencer(string(stop), trace.Config{})
	if err != nil {
		return err
	}
	toolCalls := 0
	agent := newAgent(false, &toolCalls)
	ctx := gent.NewExecutionContext(worker, "refund", gent.NewBasicLoopData(
		&gent.Task{Text: "May I receive a refund under policy P10?"}), gent.WithRunId(string(stop)),
		gent.WithRecorder(firstWriter), gent.WithObserver(seq), gent.WithCodecs(store.codecs))
	_, err = executor.NewRunner(agent, executor.DefaultConfig()).Execute(ctx)
	if !errors.Is(err, context.Canceled) {
		return fmt.Errorf("expected worker interruption, received %v", err)
	}
	// A late observer loads one consistent stored cursor and everything covered by it. Buffered
	// updates at or below that cursor are duplicates; a gap requires another full load.
	bundle, err := store.load()
	if err != nil {
		return err
	}
	if err := gent.ValidateBundle(bundle, store.codecs); err != nil {
		return err
	}
	freshAgent := newAgent(true, &toolCalls)
	freshExecutor := executor.NewRunner(freshAgent, executor.DefaultConfig())
	bindings := gent.RestoreBindings{Codecs: store.codecs}
	for {
		newWriter := &writer{store: store, lease: store.nextLease()}
		restoredSeq, err := trace.RestoreSequencer(bundle, trace.Config{
			Codecs: bindings.Codecs})
		if err != nil {
			return err
		}
		bindings.Recorder = newWriter
		bindings.Observers = []gent.RecordedEventObserver{restoredSeq}
		_, err = freshExecutor.Resume(context.Background(), bundle, bindings)
		if !errors.Is(err, gent.ErrUncertainTool) {
			if err != nil {
				return err
			}
			break
		}
		if _, err := fmt.Fprintln(
			out,
			"Uncertain tool: continuation waits for a recovery decision.",
		); err != nil {
			return err
		}
		// This particular tool is a pure read, so retry is safe. A writing tool must consult its
		// durable idempotency receipt by request.ToolCallId and reconcile with the old writer.
		bindings.ToolRecovery = gent.ToolRecoveryFunc(
			func(context.Context, gent.ToolRecoveryRequest) (
				gent.ToolRecoveryResult, error,
			) {
				return gent.ToolRecoveryResult{Status: gent.ToolRecoveryRetryAllowed}, nil
			},
		)
		bundle, err = store.load()
		if err != nil {
			return err
		}
	}
	terminal, err := store.load() // Finds the lost completion even if no later update arrives.
	if err != nil {
		return err
	}
	finished, err := gent.RestoreExecutionContext(
		context.Background(),
		terminal,
		gent.RestoreBindings{Codecs: store.codecs},
	)
	if err != nil {
		return err
	}
	result, err := freshExecutor.Continue(finished)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "%s: %s; tool executions: %d; saved terminal result reused.\n",
		stop, result.Output[0], toolCalls)
	return err
}

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
