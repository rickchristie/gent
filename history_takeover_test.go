package gent_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents/react"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/toolchain"
	"github.com/stretchr/testify/assert"
)

var errExpiredLease = errors.New("worker lease expired")

type takeoverStore struct {
	mu     sync.Mutex
	lease  int
	view   gent.RunView
	codecs *gent.CodecRegistry
}

type takeoverRecorder struct {
	store *takeoverStore
	lease int
}

func (r *takeoverRecorder) Save(_ context.Context, batch *gent.SaveBatch) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	if r.lease != r.store.lease {
		return errExpiredLease
	}
	next, err := gent.ApplyBatch(r.store.view, batch, r.store.codecs)
	if err != nil {
		return err
	}
	r.store.view = next
	return nil
}

func (s *takeoverStore) takeover() (gent.RunView, *takeoverRecorder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lease++
	data, err := s.codecs.Marshal(s.view)
	if err != nil {
		return gent.RunView{}, nil, err
	}
	var saved gent.RunView
	if err := s.codecs.Unmarshal(data, &saved); err != nil {
		return saved, nil, err
	}
	return saved, &takeoverRecorder{store: s, lease: s.lease}, nil
}

func TestNativeTakeoverStopsOldWorker(t *testing.T) {
	type input struct{ program bool }
	type expected struct{ effects int64 }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"native batch", input{}, expected{1}},
		{"JavaScript", input{true}, expected{1}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			var effects atomic.Int64
			build := func(finish bool) *react.Agent[string] {
				chain := toolchain.New()
				for _, name := range []string{"lookup", "book", "later"} {
					chain.RegisterTool(gent.DefineTool(name,
						func(ctx context.Context, _ *bookingInput) (string, error) {
							effects.Add(1)
							if name == "lookup" {
								close(started)
								select {
								case <-release:
								case <-ctx.Done():
									return "", ctx.Err()
								}
							}
							return name, nil
						}).Description("Operation").ParameterSchema(nil).MustBuild())
				}
				var selected gent.ToolChain = chain
				if test.input.program {
					selected = toolchain.NewJsToolChainWrapper(chain)
				}
				return react.NewAgent(&guidanceModel{program: test.input.program, finish: finish}).
					WithToolChain(selected).WithRecoveryVersion("takeover/v1")
			}
			store := &takeoverStore{lease: 1, codecs: gent.NewCodecRegistry(),
				view: gent.RunView{SchemaVersion: gent.RunSchemaVersion, RunId: "takeover"}}
			worker, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			ctx := gent.NewExecutionContext(worker, "takeover",
				gent.NewBasicLoopData(gent.NewTask(gent.TextContent{Text: "Run the operations."})),
				gent.WithRunId("takeover"), gent.WithCodecs(store.codecs),
				gent.WithRecorder(&takeoverRecorder{store: store, lease: 1}))
			oldDone := make(chan error, 1)
			go func() {
				_, _, err := executor.NewRunner(build(false), executor.DefaultConfig()).Execute(ctx)
				oldDone <- err
			}()
			select {
			case <-started:
			case err := <-oldDone:
				t.Fatalf("old worker stopped before the tool: %v", err)
			case <-worker.Done():
				t.Fatal("old worker did not start the tool")
			}
			saved, recorder, err := store.takeover()
			if !assert.NoError(t, err) {
				return
			}
			fresh := build(true)
			bindings := recoveryBindings(t, fresh)
			bindings.Recorder = recorder
			_, result, err := resumeRecovery(fresh, saved, bindings)
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, gent.TerminationSuccess, result.TerminationReason)
			unblock()
			select {
			case err := <-oldDone:
				assert.ErrorIs(t, err, errExpiredLease)
			case <-worker.Done():
				t.Fatal("old worker did not stop after takeover")
			}
			assert.Equal(t, test.expected, expected{effects.Load()})
		})
	}
}
