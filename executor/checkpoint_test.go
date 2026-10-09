package executor_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/executor"
	"github.com/stretchr/testify/assert"
	"github.com/tmc/langchaingo/llms"
)

type resumeLoop struct {
	configured int
	calls      int
	step       int
	task       string
}

func (loop *resumeLoop) ConfigureExecution(ctx *gent.ExecutionContext) error {
	loop.configured++
	loop.step = 0
	loop.task = ctx.Data().GetTask().Text
	return nil
}

func (loop *resumeLoop) Next(ctx *gent.ExecutionContext) (*gent.AgentLoopResult, error) {
	loop.calls++
	loop.step++
	if loop.step == 1 {
		return &gent.AgentLoopResult{Action: gent.LAContinue}, nil
	}
	text := fmt.Sprintf("%s:%d", loop.task, loop.step)
	return &gent.AgentLoopResult{Action: gent.LATerminate,
		Result: []gent.ContentPart{llms.TextContent{Text: text}}}, nil
}

func (*resumeLoop) CheckpointType() string { return "test/resume-loop" }
func (*resumeLoop) CheckpointVersion() int { return 1 }
func (loop *resumeLoop) SaveCheckpointState() (gent.EncodedValue, error) {
	return gent.NewCodecRegistry().Encode(loop.step)
}
func (loop *resumeLoop) RestoreCheckpointState(saved gent.EncodedValue) error {
	value, err := gent.NewCodecRegistry().Decode(saved)
	if err != nil {
		return err
	}
	step, ok := value.(int)
	if !ok {
		return fmt.Errorf("invalid loop step %T", value)
	}
	loop.step = step
	return nil
}

type resumeRecorder struct {
	view   gent.RunView
	stop   gent.ExecutionPhase
	cancel context.CancelFunc
}

func (recorder *resumeRecorder) Save(_ context.Context, batch *gent.SaveBatch) error {
	view, err := gent.ApplyBatch(recorder.view, batch)
	if err != nil {
		return err
	}
	recorder.view = view
	if batch.Checkpoint != nil && batch.Checkpoint.Phase == recorder.stop {
		recorder.cancel()
	}
	return nil
}

func TestExecutorResume_ConfiguresBeforeRestoringState(t *testing.T) {
	type input struct {
		phase         gent.ExecutionPhase
		version       string
		manualRestore bool
	}
	type expected struct {
		configured int
		calls      int
		step       int
		result     *gent.ExecutionResult
		err        error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	result := &gent.ExecutionResult{TerminationReason: gent.TerminationSuccess,
		Output: []gent.ContentPart{llms.TextContent{Text: "original task:2"}}}
	cases := []testCase{
		{"continue custom state", input{gent.PhaseIterationCompleted, "v1", false},
			expected{1, 1, 2, result, nil}},
		{"reject changed configuration", input{gent.PhaseIterationCompleted, "v2", false},
			expected{1, 0, 0, nil, gent.ErrIncompatibleCheckpoint}},
		{"reuse terminal result without configuration", input{gent.PhaseTerminal, "v2", false},
			expected{0, 0, 0, result, nil}},
		{"continue manually restored context", input{gent.PhaseIterationCompleted, "v1", true},
			expected{1, 1, 2, result, nil}},
		{"continue rejects changed executor", input{gent.PhaseIterationCompleted, "v2", true},
			expected{1, 0, 1, nil, gent.ErrIncompatibleCheckpoint}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			worker, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := &resumeRecorder{view: gent.RunView{SchemaVersion: gent.RunSchemaVersion,
				RunId: "resume-test"}, stop: test.input.phase, cancel: cancel}
			original := gent.NewExecutionContext(worker, "root",
				gent.NewBasicLoopData(&gent.Task{Text: "original task"}),
				gent.WithRunId("resume-test"), gent.WithRecorder(store))
			_, err := executor.New[gent.LoopData](&resumeLoop{},
				executor.Config{RecoveryVersion: "v1"}).Execute(original)
			if test.input.phase == gent.PhaseTerminal {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, context.Canceled)
			}
			registry := gent.NewCodecRegistry()
			encoded, err := registry.Marshal(store.view)
			if !assert.NoError(t, err) {
				return
			}
			var saved gent.RunView
			if !assert.NoError(t, registry.Unmarshal(encoded, &saved)) {
				return
			}
			fresh := &resumeLoop{}
			exec := executor.New[gent.LoopData](fresh,
				executor.Config{RecoveryVersion: test.input.version})
			var actual *gent.ExecutionResult
			if test.input.manualRestore {
				// Advanced callers prepare the actual context and bind fresh state components.
				// Continue must validate this executor without repeating that initialization.
				bindings := gent.RestoreBindings{Components: map[string]gent.CheckpointState{
					"loop": fresh}, Configure: func(ctx *gent.ExecutionContext) error {
					if err := fresh.ConfigureExecution(ctx); err != nil {
						return err
					}
					return ctx.SetCompatibility(saved.Checkpoint.Compatibility)
				}}
				restored, restoreErr := gent.RestoreExecutionContext(context.Background(), saved, bindings)
				if !assert.NoError(t, restoreErr) {
					return
				}
				actual, err = exec.Continue(restored)
			} else {
				actual, err = exec.Resume(context.Background(), saved, gent.RestoreBindings{})
			}
			assert.ErrorIs(t, err, test.expected.err)
			assert.Equal(t, test.expected.result, actual)
			assert.Equal(t, test.expected.configured, fresh.configured)
			assert.Equal(t, test.expected.calls, fresh.calls)
			assert.Equal(t, test.expected.step, fresh.step)
		})
	}
}
