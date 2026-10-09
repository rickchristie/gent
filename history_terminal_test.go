package gent_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/executor"
	"github.com/stretchr/testify/assert"
)

type terminalTestLoop struct {
	calls int
	err   error
}

func (l *terminalTestLoop) Next(*gent.ExecutionContext) (*gent.AgentLoopResult, error) {
	l.calls++
	return nil, l.err
}

func TestHistoryRecovery_TerminalFailuresAndCancellation(t *testing.T) {
	lookupErr := fmt.Errorf("lookup failed: %w", gent.ErrHistoryConflict)
	stopErr := errors.New("operator requested stop")
	type input struct {
		loopErr error
		cancel  bool
		cause   error
	}
	type expected struct {
		calls  int
		reason gent.TerminationReason
		err    error
		is     error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"failure", input{loopErr: lookupErr}, expected{
			calls: 1, reason: gent.TerminationError,
			err: fmt.Errorf("AgentLoop.Next (iteration 1): %w", lookupErr),
			is:  gent.ErrHistoryConflict,
		}},
		{"intentional cancellation", input{cancel: true}, expected{
			reason: gent.TerminationContextCanceled, err: context.Canceled, is: context.Canceled,
		}},
		{"intentional cancellation with cause", input{cancel: true, cause: stopErr}, expected{
			reason: gent.TerminationContextCanceled,
			err:    errors.Join(context.Canceled, stopErr), is: context.Canceled,
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			started := time.Now()
			ctx := gent.NewExecutionContext(context.Background(), "root", nil)
			loop := &terminalTestLoop{err: tt.input.loopErr}
			if tt.input.cancel {
				ctx.CancelRun(tt.input.cause)
			}
			exec := executor.New[gent.LoopData](loop, executor.DefaultConfig())
			result, err := exec.Execute(ctx)
			assert.EqualError(t, err, tt.expected.err.Error())
			assert.ErrorIs(t, err, tt.expected.is)
			assert.Equal(t, tt.expected.calls, loop.calls)
			bundle, err := ctx.Export()
			assert.NoError(t, err)
			assert.Equal(t, gent.PhaseTerminal, bundle.Checkpoint.Phase)
			assert.GreaterOrEqual(t, bundle.Checkpoint.Outcome.CompletedAt, started)
			fresh, err := gent.RestoreExecutionContext(context.Background(), bundle,
				gent.RestoreBindings{})
			if !assert.NoError(t, err) {
				return
			}
			// No loop, provider, or compatibility manifest is needed to return a terminal outcome.
			freshExec := executor.New[gent.LoopData](nil, executor.DefaultConfig())
			restored, err := freshExec.Continue(fresh)
			assert.EqualError(t, err, tt.expected.err.Error())
			assert.ErrorIs(t, err, tt.expected.is)
			assert.Equal(t, &gent.ExecutionResult{
				TerminationReason: tt.expected.reason, Error: gent.SaveError(tt.expected.err),
			}, restored)
			assert.Equal(t, result.Output, restored.Output)
			unchanged, err := fresh.Export()
			assert.NoError(t, err)
			assert.Equal(t, bundle, unchanged)
		})
	}
}
