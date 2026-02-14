package executor_test

import (
	"context"
	"errors"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/internal/tt"
	"github.com/stretchr/testify/assert"
)

// ----------------------------------------------------------------
// Executor Compaction Integration Tests
//
// These tests verify the executor's compaction machinery:
//   - Trigger/strategy wiring
//   - Event publishing and stat updates
//   - Error handling
//   - Ordering relative to BeforeIteration
// ----------------------------------------------------------------

func TestCompaction_ExecutorIntegration(t *testing.T) {
	type input struct {
		trigger     *tt.MockCompactionTrigger
		strategy    *tt.MockCompactionStrategy
		terminateAt int
	}

	type expected struct {
		terminationReason gent.TerminationReason
		hasError          bool
		compactions       int64
		strategyCallCount int
		triggerNotified   int
		scratchpadLen     int
		events            []gent.Event
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "no compaction configured runs normally",
			input: input{
				trigger:     nil,
				strategy:    nil,
				terminateAt: 2,
			},
			expected: expected{
				terminationReason: gent.TerminationSuccess,
				hasError:          false,
				compactions:       0,
				strategyCallCount: 0,
				triggerNotified:   0,
				scratchpadLen:     2,
				events: []gent.Event{
					tt.BeforeExec(0, 0),
					tt.BeforeIter(0, 1),
					tt.AfterIter(0, 1,
						tt.ContinueWithPrompt(
							mockObservation,
						)),
					tt.BeforeIter(0, 2),
					tt.AfterIter(0, 2,
						tt.Terminate("done")),
					tt.AfterExec(0, 2,
						gent.TerminationSuccess),
				},
			},
		},
		{
			name: "trigger never fires runs normally",
			input: input{
				trigger: tt.NewMockCompactionTrigger().
					WithShouldCompact(false),
				strategy: tt.NewMockCompactionStrategy(),
				// trigger checked once: before iter 2
				terminateAt: 2,
			},
			expected: expected{
				terminationReason: gent.TerminationSuccess,
				hasError:          false,
				compactions:       0,
				strategyCallCount: 0,
				triggerNotified:   0,
				scratchpadLen:     2,
				events: []gent.Event{
					tt.BeforeExec(0, 0),
					tt.BeforeIter(0, 1),
					tt.AfterIter(0, 1,
						tt.ContinueWithPrompt(
							mockObservation,
						)),
					tt.BeforeIter(0, 2),
					tt.AfterIter(0, 2,
						tt.Terminate("done")),
					tt.AfterExec(0, 2,
						gent.TerminationSuccess),
				},
			},
		},
		{
			name: "trigger fires once and strategy " +
				"succeeds",
			input: input{
				trigger: tt.NewMockCompactionTrigger().
					WithShouldCompact(true, false),
				strategy: tt.NewMockCompactionStrategy().
					WithCompactFunc(
						func(
							ctx *gent.ExecutionContext,
						) error {
							sp := ctx.Data().GetScratchPad()
							if len(sp) > 1 {
								ctx.Data().SetScratchPad(
									sp[len(sp)-1:],
								)
							}
							return nil
						},
					),
				terminateAt: 3,
			},
			expected: expected{
				terminationReason: gent.TerminationSuccess,
				hasError:          false,
				compactions:       1,
				strategyCallCount: 1,
				triggerNotified:   1,
				scratchpadLen:     3,
				events: []gent.Event{
					tt.BeforeExec(0, 0),
					tt.BeforeIter(0, 1),
					tt.AfterIter(0, 1,
						tt.ContinueWithPrompt(
							mockObservation,
						)),
					// Compaction fires before iter 2
					tt.Compaction(0, 1, 1, 1),
					tt.BeforeIter(0, 2),
					tt.AfterIter(0, 2,
						tt.ContinueWithPrompt(
							mockObservation,
						)),
					tt.BeforeIter(0, 3),
					tt.AfterIter(0, 3,
						tt.Terminate("done")),
					tt.AfterExec(0, 3,
						gent.TerminationSuccess),
				},
			},
		},
		{
			name: "trigger fires multiple times",
			input: input{
				trigger: tt.NewMockCompactionTrigger().
					WithShouldCompact(
						true, true, false,
					),
				strategy: tt.NewMockCompactionStrategy().
					WithCompactFunc(
						func(
							ctx *gent.ExecutionContext,
						) error {
							sp := ctx.Data().GetScratchPad()
							if len(sp) > 1 {
								ctx.Data().SetScratchPad(
									sp[len(sp)-1:],
								)
							}
							return nil
						},
					),
				terminateAt: 4,
			},
			expected: expected{
				terminationReason: gent.TerminationSuccess,
				hasError:          false,
				compactions:       2,
				strategyCallCount: 2,
				triggerNotified:   2,
				scratchpadLen:     3,
				events: []gent.Event{
					tt.BeforeExec(0, 0),
					tt.BeforeIter(0, 1),
					tt.AfterIter(0, 1,
						tt.ContinueWithPrompt(
							mockObservation,
						)),
					// Compaction before iter 2
					tt.Compaction(0, 1, 1, 1),
					tt.BeforeIter(0, 2),
					tt.AfterIter(0, 2,
						tt.ContinueWithPrompt(
							mockObservation,
						)),
					// Compaction before iter 3
					tt.Compaction(0, 2, 2, 1),
					tt.BeforeIter(0, 3),
					tt.AfterIter(0, 3,
						tt.ContinueWithPrompt(
							mockObservation,
						)),
					tt.BeforeIter(0, 4),
					tt.AfterIter(0, 4,
						tt.Terminate("done")),
					tt.AfterExec(0, 4,
						gent.TerminationSuccess),
				},
			},
		},
		{
			name: "strategy error terminates with " +
				"compaction_failed",
			input: input{
				trigger: tt.NewMockCompactionTrigger().
					WithShouldCompact(true),
				strategy: tt.NewMockCompactionStrategy().
					WithError(errors.New(
						"summarization model failed",
					)),
				terminateAt: 5,
			},
			expected: expected{
				terminationReason: gent.TerminationCompactionFailed,
				hasError:          true,
				compactions:       0,
				strategyCallCount: 1,
				triggerNotified:   0,
				scratchpadLen:     1,
				events: []gent.Event{
					tt.BeforeExec(0, 0),
					tt.BeforeIter(0, 1),
					tt.AfterIter(0, 1,
						tt.ContinueWithPrompt(
							mockObservation,
						)),
					// No CompactionEvent (strategy failed)
					tt.AfterExec(0, 1,
						gent.TerminationCompactionFailed),
				},
			},
		},
		{
			name: "strategy error on second trigger",
			input: input{
				trigger: tt.NewMockCompactionTrigger().
					WithShouldCompact(true, true),
				strategy: func() *tt.MockCompactionStrategy {
					callCount := 0
					return tt.NewMockCompactionStrategy().
						WithCompactFunc(
							func(
								ctx *gent.ExecutionContext,
							) error {
								callCount++
								if callCount == 1 {
									return nil
								}
								return errors.New(
									"second call fails",
								)
							},
						)
				}(),
				terminateAt: 5,
			},
			expected: expected{
				terminationReason: gent.TerminationCompactionFailed,
				hasError:          true,
				compactions:       1,
				strategyCallCount: 2,
				triggerNotified:   1,
				scratchpadLen:     2,
				events: []gent.Event{
					tt.BeforeExec(0, 0),
					tt.BeforeIter(0, 1),
					tt.AfterIter(0, 1,
						tt.ContinueWithPrompt(
							mockObservation,
						)),
					// First compaction succeeds
					tt.Compaction(0, 1, 1, 1),
					tt.BeforeIter(0, 2),
					tt.AfterIter(0, 2,
						tt.ContinueWithPrompt(
							mockObservation,
						)),
					// Second compaction fails
					tt.AfterExec(0, 2,
						gent.TerminationCompactionFailed),
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := gent.NewBasicLoopData(
				&gent.Task{Text: "test"},
			)

			loop := &scratchpadTrackingLoop{
				terminateAt: tc.input.terminateAt,
			}

			exec := executor.New[*gent.BasicLoopData](
				loop,
				executor.DefaultConfig(),
			)

			execCtx := gent.NewExecutionContext(
				context.Background(), "test", data,
			)
			execCtx.SetLimits(nil)

			if tc.input.trigger != nil &&
				tc.input.strategy != nil {
				execCtx.SetCompaction(
					tc.input.trigger,
					tc.input.strategy,
				)
			}

			exec.Execute(execCtx)

			result := execCtx.Result()
			assert.Equal(t,
				tc.expected.terminationReason,
				result.TerminationReason,
			)

			if tc.expected.hasError {
				assert.NotNil(t, result.Error)
			} else {
				assert.Nil(t, result.Error)
			}

			assert.Equal(t,
				tc.expected.compactions,
				execCtx.Stats().GetCounter(
					gent.SCCompactions,
				),
			)

			if tc.input.strategy != nil {
				assert.Equal(t,
					tc.expected.strategyCallCount,
					tc.input.strategy.CallCount(),
				)
			}

			if tc.input.trigger != nil {
				assert.Equal(t,
					tc.expected.triggerNotified,
					tc.input.trigger.NotifiedCount(),
				)
			}

			assert.Equal(t,
				tc.expected.scratchpadLen,
				len(data.GetScratchPad()),
			)

			lifecycleEvents := collectLifecycleAndCompaction(
				execCtx,
			)
			tt.AssertEventsEqual(
				t, tc.expected.events, lifecycleEvents,
			)
		})
	}
}

// scratchpadTrackingLoop implements AgentLoop that adds an
// iteration to the scratchpad on each call. This simulates
// real agent behavior where each iteration grows the
// scratchpad.
type scratchpadTrackingLoop struct {
	calls       int
	terminateAt int
}

func (l *scratchpadTrackingLoop) Next(
	execCtx *gent.ExecutionContext,
) (*gent.AgentLoopResult, error) {
	l.calls++

	// Add an iteration to scratchpad (like real agents do)
	iter := &gent.Iteration{
		Messages: []*gent.MessageContent{},
	}
	execCtx.Data().SetScratchPad(
		append(execCtx.Data().GetScratchPad(), iter),
	)

	if l.terminateAt > 0 && l.calls >= l.terminateAt {
		return tt.Terminate("done"), nil
	}
	return tt.ContinueWithPrompt(mockObservation), nil
}

// collectLifecycleAndCompaction collects lifecycle events
// plus CompactionEvents.
func collectLifecycleAndCompaction(
	execCtx *gent.ExecutionContext,
) []gent.Event {
	var result []gent.Event
	for _, event := range execCtx.Events() {
		if tt.IsLifecycleEvent(event) {
			result = append(result, event)
		}
	}
	return result
}

// ----------------------------------------------------------------
// Test: per-iteration gauges available during trigger check
//
// This verifies the critical placement invariant: compaction
// runs BEFORE BeforeIterationEvent, so per-iteration gauges
// from the previous iteration are still available for the
// trigger to inspect.
// ----------------------------------------------------------------

func TestCompaction_PerIterationGaugesAvailable(
	t *testing.T,
) {
	type input struct {
		inputTokensPerIter  int
		outputTokensPerIter int
	}

	type expected struct {
		// The per-iteration gauge values captured by the
		// trigger during the ShouldCompact call before
		// iteration 2.
		inputTokens  float64
		outputTokens float64
		totalTokens  float64
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "gauges reflect previous iteration " +
				"token usage",
			input: input{
				inputTokensPerIter:  150,
				outputTokensPerIter: 50,
			},
			expected: expected{
				inputTokens:  150,
				outputTokens: 50,
				totalTokens:  200,
			},
		},
		{
			name: "gauges reflect different token " +
				"counts",
			input: input{
				inputTokensPerIter:  500,
				outputTokensPerIter: 100,
			},
			expected: expected{
				inputTokens:  500,
				outputTokens: 100,
				totalTokens:  600,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := gent.NewBasicLoopData(
				&gent.Task{Text: "test"},
			)

			loop := &modelCallingLoop{
				terminateAt:  2,
				inputTokens:  tc.input.inputTokensPerIter,
				outputTokens: tc.input.outputTokensPerIter,
			}

			// Custom trigger that captures gauge
			// values during ShouldCompact
			trigger := &gaugeCapturingTrigger{}

			strategy := tt.NewMockCompactionStrategy()

			exec := executor.New[*gent.BasicLoopData](
				loop,
				executor.DefaultConfig(),
			)

			execCtx := gent.NewExecutionContext(
				context.Background(), "test", data,
			)
			execCtx.SetLimits(nil)
			execCtx.SetCompaction(trigger, strategy)

			exec.Execute(execCtx)

			assert.Equal(t,
				gent.TerminationSuccess,
				execCtx.Result().TerminationReason,
			)

			// Verify trigger was called and captured
			// per-iteration gauges from iteration 1.
			assert.Equal(t, 1, trigger.callCount,
				"ShouldCompact should be called once "+
					"(before iteration 2)",
			)
			assert.Equal(t,
				tc.expected.inputTokens,
				trigger.capturedInputTokens,
				"SGInputTokensLastIteration should "+
					"reflect iteration 1 tokens",
			)
			assert.Equal(t,
				tc.expected.outputTokens,
				trigger.capturedOutputTokens,
				"SGOutputTokensLastIteration should "+
					"reflect iteration 1 tokens",
			)
			assert.Equal(t,
				tc.expected.totalTokens,
				trigger.capturedTotalTokens,
				"SGTotalTokensLastIteration should "+
					"reflect iteration 1 tokens",
			)
		})
	}
}

// modelCallingLoop is an AgentLoop that publishes
// AfterModelCallEvent on each iteration to set per-iteration
// token gauges.
type modelCallingLoop struct {
	calls        int
	terminateAt  int
	inputTokens  int
	outputTokens int
}

func (l *modelCallingLoop) Next(
	execCtx *gent.ExecutionContext,
) (*gent.AgentLoopResult, error) {
	l.calls++

	// Publish model call event to set per-iteration gauges
	resp := &gent.ContentResponse{
		Choices: []*gent.ContentChoice{
			{Content: "response"},
		},
		Info: &gent.GenerationInfo{
			InputTokens:  l.inputTokens,
			OutputTokens: l.outputTokens,
		},
	}
	execCtx.PublishAfterModelCall(
		"test-model", nil, resp, 0, nil,
	)

	// Add iteration to scratchpad
	iter := &gent.Iteration{
		Messages: []*gent.MessageContent{},
	}
	execCtx.Data().SetScratchPad(
		append(execCtx.Data().GetScratchPad(), iter),
	)

	if l.terminateAt > 0 && l.calls >= l.terminateAt {
		return tt.Terminate("done"), nil
	}
	return tt.ContinueWithPrompt(mockObservation), nil
}

// gaugeCapturingTrigger captures per-iteration gauge values
// when ShouldCompact is called.
type gaugeCapturingTrigger struct {
	callCount            int
	capturedInputTokens  float64
	capturedOutputTokens float64
	capturedTotalTokens  float64
}

func (t *gaugeCapturingTrigger) ShouldCompact(
	execCtx *gent.ExecutionContext,
) bool {
	t.callCount++
	stats := execCtx.Stats()
	t.capturedInputTokens = stats.GetGauge(
		gent.SGInputTokensLastIteration,
	)
	t.capturedOutputTokens = stats.GetGauge(
		gent.SGOutputTokensLastIteration,
	)
	t.capturedTotalTokens = stats.GetGauge(
		gent.SGTotalTokensLastIteration,
	)
	return false // Don't actually compact
}

func (t *gaugeCapturingTrigger) NotifyCompacted(
	_ *gent.ExecutionContext,
) {
}
