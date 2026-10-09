package gent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type savedCounter struct{ count int }

func (c *savedCounter) Add(n int)              { c.count += n }
func (c *savedCounter) Value() int             { return c.count }
func (c *savedCounter) CheckpointType() string { return "fixture/counter" }
func (c *savedCounter) CheckpointVersion() int { return 1 }
func (c *savedCounter) SaveCheckpointState() (EncodedValue, error) {
	return NewCodecRegistry().Encode(c.count)
}
func (c *savedCounter) RestoreCheckpointState(value EncodedValue) error {
	decoded, err := decodeAs[int](NewCodecRegistry(), value)
	if err != nil {
		return err
	}
	c.count = decoded
	return nil
}

func TestCheckpointExtensions(t *testing.T) {
	ctx := NewExecutionContext(context.Background(), "root", nil)
	counter := &savedCounter{}
	counter.Add(17)
	assert.NoError(t, ctx.BindCheckpointState("counter", counter))
	assert.NoError(t, ctx.CommitCheckpoint(PhaseIterationCompleted))
	counter.Add(100)
	bundle, err := ctx.Export()
	assert.NoError(t, err)
	_, err = RestoreExecutionContext(context.Background(), bundle, RestoreBindings{})
	assert.ErrorIs(t, err, ErrIncompatibleCheckpoint)
	freshCounter := &savedCounter{}
	fresh, err := RestoreExecutionContext(context.Background(), bundle, RestoreBindings{
		Components: map[string]CheckpointState{"counter": freshCounter},
	})
	if !assert.NoError(t, err) {
		return
	}
	assert.Equal(t, 17, freshCounter.Value())
	freshCounter.Add(3)
	assert.NoError(t, fresh.CommitCheckpoint(PhaseIterationCompleted))
	next, err := fresh.Export()
	assert.NoError(t, err)
	value, err := NewCodecRegistry().Decode(next.Checkpoint.Contexts[0].Extensions["counter"].Value)
	assert.NoError(t, err)
	assert.Equal(t, 20, value)
}

func TestRestoreUnsupportedBoundaries(t *testing.T) {
	type input struct{ setup func(*ExecutionContext) error }
	type expected struct{ err error }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"active child in checkpoint", input{func(ctx *ExecutionContext) error {
			ctx.SpawnChild("child", nil)
			return ctx.CommitCheckpoint(PhaseIterationReady)
		}}, expected{ErrUnsupportedRecovery}},
		{"child newer than checkpoint", input{func(ctx *ExecutionContext) error {
			if err := ctx.CommitCheckpoint(PhaseIterationReady); err != nil {
				return err
			}
			ctx.SpawnChild("child", nil)
			return ctx.FlushProgress()
		}}, expected{ErrUnsupportedRecovery}},
		{"active JavaScript frame", input{func(ctx *ExecutionContext) error {
			if err := ctx.SetCheckpointState("gent:toolchain_active", "JavaScript"); err != nil {
				return err
			}
			return ctx.CommitCheckpoint(PhaseDecisionReady)
		}}, expected{ErrUnsupportedRecovery}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx := NewExecutionContext(context.Background(), "root", nil)
			assert.NoError(t, test.input.setup(ctx))
			bundle, err := ctx.Export()
			assert.NoError(t, err)
			_, err = RestoreExecutionContext(context.Background(), bundle, RestoreBindings{})
			assert.ErrorIs(t, err, test.expected.err)
		})
	}
}

func TestRestorePreservesCompletedChildrenAndAbsoluteDeadline(t *testing.T) {
	deadline := time.Now().Add(25 * time.Millisecond)
	worker, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	ctx := NewExecutionContext(worker, "root", nil)
	child := ctx.SpawnChild("completed", nil)
	child.Stats().IncrCounter(SCInputTokens, 7)
	child.SetTermination(TerminationSuccess, nil, nil)
	assert.NoError(t, ctx.CompleteChild(child))
	assert.NoError(t, ctx.CommitCheckpoint(PhaseIterationCompleted))
	bundle, err := ctx.Export()
	assert.NoError(t, err)
	<-worker.Done()
	fresh, err := RestoreExecutionContext(context.Background(), bundle, RestoreBindings{})
	if !assert.NoError(t, err) {
		return
	}
	assert.ErrorIs(t, fresh.Context().Err(), context.DeadlineExceeded)
	actualDeadline, ok := fresh.Context().Deadline()
	assert.True(t, ok)
	// Monotonic readings are process-local; the absolute wall deadline survives serialization.
	assert.Equal(t, deadline.Round(0), actualDeadline)
	assert.Equal(t, int64(7), fresh.Stats().GetCounter(SCInputTokens))
	assert.Equal(t, int64(0), fresh.Stats().GetCounter(SCInputTokens.Self()))
	assert.NoError(t, fresh.CommitCheckpoint(PhaseIterationCompleted))
	next, err := fresh.Export()
	assert.NoError(t, err)
	assert.Equal(t, bundle.Checkpoint.Contexts[1:], next.Checkpoint.Contexts[1:])
	assert.NoError(t, ValidateBundle(next))
}

func TestRestoreProgressSelectionUsesExplicitTransition(t *testing.T) {
	ctx := NewExecutionContext(context.Background(), "root", nil)
	assert.NoError(t, ctx.AppendRecord(&HistoryEntry{Origin: RecordOriginal}, true))
	assert.NoError(t, ctx.CommitCheckpoint(PhaseIterationReady))
	checkpointActive := ctx.ContextDraft().Active
	assert.NoError(t, ctx.AppendRecord(&HistoryEntry{Origin: RecordSystemInjected}, true))
	assert.NoError(t, ctx.FlushProgress())
	bundle, err := ctx.Export()
	assert.NoError(t, err)
	fresh, err := RestoreExecutionContext(context.Background(), bundle, RestoreBindings{})
	if !assert.NoError(t, err) {
		return
	}
	assert.Equal(t, checkpointActive, fresh.ContextDraft().Active)
	assert.NoError(t, fresh.CommitCheckpoint(PhaseIterationReady))
	next, err := fresh.Export()
	assert.NoError(t, err)
	assert.Equal(t, bundle.History.Entries, next.History.Entries)
	assert.Equal(t, bundle.History.Revision+1, next.History.Revision)
	assert.NoError(t, ValidateBundle(next))
}

func TestRestorePreservesRemainingLimits(t *testing.T) {
	type input struct {
		limit     Limit
		key       StatKey
		initial   int64
		child     int64
		competing StatKey
		gauge     bool
	}
	type expected struct {
		matchedKey StatKey
		finalValue float64
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"aggregate counter includes children", input{
			limit: Limit{Type: LimitExactKey, Key: SCInputTokens, MaxValue: 10},
			key:   SCInputTokens, initial: 5, child: 4,
		}, expected{SCInputTokens, 11}},
		{"self counter excludes children", input{
			limit: Limit{Type: LimitExactKey, Key: SCInputTokens.Self(), MaxValue: 10},
			key:   SCInputTokens, initial: 9, child: 4,
		}, expected{SCInputTokens.Self(), 11}},
		{"competing prefixed counters", input{
			limit: Limit{Type: LimitKeyPrefix, Key: SCToolCallsFor, MaxValue: 3},
			key:   SCToolCallsFor + "tool2", initial: 2, competing: SCToolCallsFor + "tool1",
		}, expected{SCToolCallsFor + "tool2", 4}},
		{"consecutive errors remain charged", input{
			limit: Limit{Type: LimitExactKey, Key: SGToolCallsErrorConsecutive, MaxValue: 3},
			key:   SGToolCallsErrorConsecutive, initial: 2, gauge: true,
		}, expected{SGToolCallsErrorConsecutive, 4}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			original := NewExecutionContext(context.Background(), "root", nil)
			original.SetLimits([]Limit{tt.input.limit})
			assert.NoError(t, original.AppendRecord(&HistoryEntry{Origin: RecordOriginal}, true))
			if tt.input.child > 0 {
				child := original.SpawnChild("completed child", nil)
				child.Stats().IncrCounter(tt.input.key, tt.input.child)
				assert.NoError(t, original.CompleteChild(child))
			}
			if tt.input.gauge {
				original.Stats().SetGauge(tt.input.key, float64(tt.input.initial))
			} else {
				original.Stats().IncrCounter(tt.input.key, tt.input.initial)
			}
			if tt.input.competing != "" {
				original.Stats().IncrCounter(tt.input.competing, 3)
			}
			assert.NoError(t, original.Context().Err())
			assert.NoError(t, original.CommitCheckpoint(PhaseIterationReady))
			bundle, err := original.Export()
			assert.NoError(t, err)
			restored, err := RestoreExecutionContext(
				context.Background(), bundle, RestoreBindings{},
			)
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, original.Stats().Counters(), restored.Stats().Counters())
			assert.Equal(t, original.Stats().Gauges(), restored.Stats().Gauges())
			for step := 1; step <= 2; step++ {
				for _, ctx := range []*ExecutionContext{original, restored} {
					if tt.input.gauge {
						ctx.Stats().IncrGauge(tt.input.key, 1)
					} else {
						ctx.Stats().IncrCounter(tt.input.key, 1)
					}
					if step == 1 {
						assert.NoError(t, ctx.Context().Err())
						assert.Nil(t, ctx.ExceededLimit())
					}
				}
				assert.Equal(t, original.Stats().Counters(), restored.Stats().Counters())
				assert.Equal(t, original.Stats().Gauges(), restored.Stats().Gauges())
			}
			assert.Equal(t, &tt.input.limit, restored.ExceededLimit())
			assert.ErrorIs(t, restored.Context().Err(), context.Canceled)
			var limits []*LimitExceededEvent
			for _, event := range restored.Events() {
				if limit, ok := event.(*LimitExceededEvent); ok {
					limits = append(limits, limit)
				}
			}
			if assert.Len(t, limits, 1) {
				assert.Equal(t, tt.expected.matchedKey, limits[0].MatchedKey)
				assert.Equal(t, tt.expected.finalValue, limits[0].CurrentValue)
			}
		})
	}
}

func TestRestoreRejectsInvalidCheckpointData(t *testing.T) {
	type input struct{ change func(*RunView) }
	type expected struct{ err string }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"unsupported schema", input{func(bundle *RunView) {
			bundle.SchemaVersion--
		}}, expected{ErrIncompatibleCheckpoint.Error()}},
		{"incompatible configuration", input{func(bundle *RunView) {
			bundle.Checkpoint.Compatibility.Components = map[string]string{"agent": "changed"}
		}}, expected{ErrIncompatibleCheckpoint.Error()}},
		{"unknown task codec", input{func(bundle *RunView) {
			bundle.Checkpoint.Contexts[0].Task.Type = "missing/task"
		}}, expected{`gent: unknown codec "missing/task"`}},
		{"unsupported task codec version", input{func(bundle *RunView) {
			bundle.Checkpoint.Contexts[0].Task.Version = 2
		}}, expected{"gent: unsupported pointer version 2"}},
		{"history revision does not match cursor", input{func(bundle *RunView) {
			bundle.Checkpoint.EventNumber = bundle.LastAppliedEventNumber
		}}, expected{"gent: checkpoint revision differs from its committed cursor"}},
		{"root references itself as parent", input{func(bundle *RunView) {
			bundle.Checkpoint.Contexts[0].ParentContextId = bundle.Checkpoint.Contexts[0].ContextId
		}}, expected{"gent: first checkpoint context is not the root"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			ctx := NewExecutionContext(context.Background(), "root", NewBasicLoopData(&Task{}))
			assert.NoError(t, ctx.AppendRecord(&HistoryEntry{Origin: RecordOriginal}, true))
			assert.NoError(t, ctx.CommitCheckpoint(PhaseIterationReady))
			assert.NoError(t, ctx.AppendRecord(&HistoryEntry{Origin: RecordSystemInjected}, true))
			assert.NoError(t, ctx.FlushProgress())
			bundle, err := ctx.Export()
			assert.NoError(t, err)
			tt.input.change(&bundle)
			restored, err := RestoreExecutionContext(
				context.Background(), bundle, RestoreBindings{},
			)
			assert.Nil(t, restored)
			assert.EqualError(t, err, tt.expected.err)
		})
	}
}
