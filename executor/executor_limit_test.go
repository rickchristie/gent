package executor_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/internal/tt"
	"github.com/stretchr/testify/assert"
)

// -----------------------------------------------------------------------------
// Mock Infrastructure
// -----------------------------------------------------------------------------

// errContextCanceled is returned when context is canceled during model call.
var errContextCanceled = errors.New("context canceled during model call")

// simulateModelCall simulates a real model call with proper event publishing.
// It publishes BeforeModelCall, checks for context cancellation, then publishes AfterModelCall.
// Returns error if context is canceled before AfterModelCall can be published.
func simulateModelCall(
	execCtx *gent.ExecutionContext,
	model string,
	inputTokens, outputTokens int,
) error {
	// Publish BeforeModelCall first (like real model calls do)
	execCtx.PublishBeforeModelCall(model, nil)

	// Check if context is already canceled (limit exceeded during BeforeModelCall or earlier)
	select {
	case <-execCtx.Context().Done():
		// Context canceled, don't publish AfterModelCall
		return errContextCanceled
	default:
		// Continue with model call
	}

	// Simulate successful model response
	response := &gent.ContentResponse{
		Info: &gent.GenerationInfo{
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
		},
	}
	execCtx.PublishAfterModelCall(model, nil, response, 0, nil)
	return nil
}

// mockLoopData implements gent.LoopData for testing.
type mockLoopData struct {
	task             *gent.Task
	iterationHistory []*gent.Iteration
	scratchpad       []*gent.Iteration
}

func newMockLoopData() *mockLoopData {
	return &mockLoopData{
		task:             &gent.Task{Text: "test task"},
		iterationHistory: make([]*gent.Iteration, 0),
		scratchpad:       make([]*gent.Iteration, 0),
	}
}

func (d *mockLoopData) GetTask() *gent.Task {
	return d.task
}

func (d *mockLoopData) GetIterationHistory() []*gent.Iteration {
	return d.iterationHistory
}

func (d *mockLoopData) AddIterationHistory(iter *gent.Iteration) {
	d.iterationHistory = append(d.iterationHistory, iter)
}

func (d *mockLoopData) GetScratchPad() []*gent.Iteration {
	return d.scratchpad
}

func (d *mockLoopData) SetScratchPad(iters []*gent.Iteration) {
	d.scratchpad = iters
}

func (d *mockLoopData) SetExecutionContext(ctx *gent.ExecutionContext) {}

// mockAgentLoop implements gent.AgentLoop[*mockLoopData] for testing.
type mockAgentLoop struct {
	mu           sync.Mutex
	nextFn       func(execCtx *gent.ExecutionContext) (*gent.AgentLoopResult, error)
	calls        int
	terminateAt  int // terminate after this many calls (0 = never auto-terminate)
	inputTokens  int // tokens to trace per iteration
	outputTokens int
}

func (m *mockAgentLoop) Next(execCtx *gent.ExecutionContext) (*gent.AgentLoopResult, error) {
	m.mu.Lock()
	m.calls++
	currentCall := m.calls
	nextFn := m.nextFn
	terminateAt := m.terminateAt
	inputTokens := m.inputTokens
	outputTokens := m.outputTokens
	m.mu.Unlock()

	// Simulate model call to update stats via event
	if inputTokens > 0 || outputTokens > 0 {
		if err := simulateModelCall(execCtx, "test-model", inputTokens, outputTokens); err != nil {
			return nil, err
		}
	}

	// Custom function takes precedence
	if nextFn != nil {
		return nextFn(execCtx)
	}

	// Auto-terminate behavior
	if terminateAt > 0 && currentCall >= terminateAt {
		return tt.Terminate("done"), nil
	}

	return tt.Continue(), nil
}

func (m *mockAgentLoop) GetCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// -----------------------------------------------------------------------------
// Single AgentLoop Tests - Iteration Limit
// -----------------------------------------------------------------------------

func TestLimits_IterationLimit_Exceeded(t *testing.T) {
	// Note: Limits use > comparison, so if MaxValue=5, then 6 iterations run
	// before the limit is exceeded (because 6 > 5). The loop detects the exceeded
	// limit at the start of iteration N+1 when iteration counter exceeds MaxValue.
	type input struct {
		maxIterations float64
	}

	type expected struct {
		calls         int // MaxValue + 1 because limit triggers when counter > MaxValue
		terminate     gent.TerminationReason
		exceededLimit gent.Limit
		events        []gent.Event
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name:  "limit at 5 iterations terminates after 6",
			input: input{maxIterations: 5},
			expected: expected{
				calls:     6,
				terminate: gent.TerminationLimitExceeded,
				exceededLimit: gent.Limit{
					Type:     gent.LimitExactKey,
					Key:      gent.KeyIterations,
					MaxValue: 5,
				},
				events: []gent.Event{
					tt.BeforeExec(0, 0),
					tt.BeforeIter(0, 1),
					tt.AfterIter(0, 1, tt.Continue()),
					tt.BeforeIter(0, 2),
					tt.AfterIter(0, 2, tt.Continue()),
					tt.BeforeIter(0, 3),
					tt.AfterIter(0, 3, tt.Continue()),
					tt.BeforeIter(0, 4),
					tt.AfterIter(0, 4, tt.Continue()),
					tt.BeforeIter(0, 5),
					tt.AfterIter(0, 5, tt.Continue()),
					tt.BeforeIter(0, 6),
					tt.LimitExceeded(0, 6,
						tt.ExactLimit(gent.KeyIterations, 5),
						6, gent.KeyIterations),
					tt.AfterIter(0, 6, tt.Continue()),
					tt.AfterExec(0, 6, gent.TerminationLimitExceeded),
				},
			},
		},
		{
			name:  "limit at 1 iteration terminates after 2",
			input: input{maxIterations: 1},
			expected: expected{
				calls:     2,
				terminate: gent.TerminationLimitExceeded,
				exceededLimit: gent.Limit{
					Type:     gent.LimitExactKey,
					Key:      gent.KeyIterations,
					MaxValue: 1,
				},
				events: []gent.Event{
					tt.BeforeExec(0, 0),
					tt.BeforeIter(0, 1),
					tt.AfterIter(0, 1, tt.Continue()),
					tt.BeforeIter(0, 2),
					tt.LimitExceeded(0, 2,
						tt.ExactLimit(gent.KeyIterations, 1),
						2, gent.KeyIterations),
					tt.AfterIter(0, 2, tt.Continue()),
					tt.AfterExec(0, 2, gent.TerminationLimitExceeded),
				},
			},
		},
		{
			name:  "limit at 10 iterations terminates after 11",
			input: input{maxIterations: 10},
			expected: expected{
				calls:     11,
				terminate: gent.TerminationLimitExceeded,
				exceededLimit: gent.Limit{
					Type:     gent.LimitExactKey,
					Key:      gent.KeyIterations,
					MaxValue: 10,
				},
				events: []gent.Event{
					tt.BeforeExec(0, 0),
					tt.BeforeIter(0, 1),
					tt.AfterIter(0, 1, tt.Continue()),
					tt.BeforeIter(0, 2),
					tt.AfterIter(0, 2, tt.Continue()),
					tt.BeforeIter(0, 3),
					tt.AfterIter(0, 3, tt.Continue()),
					tt.BeforeIter(0, 4),
					tt.AfterIter(0, 4, tt.Continue()),
					tt.BeforeIter(0, 5),
					tt.AfterIter(0, 5, tt.Continue()),
					tt.BeforeIter(0, 6),
					tt.AfterIter(0, 6, tt.Continue()),
					tt.BeforeIter(0, 7),
					tt.AfterIter(0, 7, tt.Continue()),
					tt.BeforeIter(0, 8),
					tt.AfterIter(0, 8, tt.Continue()),
					tt.BeforeIter(0, 9),
					tt.AfterIter(0, 9, tt.Continue()),
					tt.BeforeIter(0, 10),
					tt.AfterIter(0, 10, tt.Continue()),
					tt.BeforeIter(0, 11),
					tt.LimitExceeded(0, 11,
						tt.ExactLimit(gent.KeyIterations, 10),
						11, gent.KeyIterations),
					tt.AfterIter(0, 11, tt.Continue()),
					tt.AfterExec(0, 11, gent.TerminationLimitExceeded),
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			loop := &mockAgentLoop{} // Never auto-terminates
			exec := executor.New[*mockLoopData](loop, executor.DefaultConfig())

			ctx := context.Background()
			data := newMockLoopData()
			execCtx := gent.NewExecutionContext(ctx, "test", data)
			execCtx.SetLimits([]gent.Limit{
				{Type: gent.LimitExactKey, Key: gent.KeyIterations, MaxValue: tc.input.maxIterations},
			})

			exec.Execute(execCtx)

			result := execCtx.Result()
			assert.NotNil(t, result)
			assert.Equal(t, tc.expected.terminate, result.TerminationReason)
			assert.NotNil(t, result.ExceededLimit)
			assert.Equal(t, tc.expected.exceededLimit, *result.ExceededLimit)
			assert.Equal(t, tc.expected.calls, loop.GetCalls())
			tt.AssertEventsEqual(t, tc.expected.events, tt.CollectLifecycleEvents(execCtx))
		})
	}
}

func TestLimits_IterationLimit_NotExceeded(t *testing.T) {
	type input struct {
		maxIterations float64
		terminateAt   int
	}

	type expected struct {
		calls     int
		terminate gent.TerminationReason
		events    []gent.Event
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "terminate before limit",
			input: input{
				maxIterations: 10,
				terminateAt:   3,
			},
			expected: expected{
				calls:     3,
				terminate: gent.TerminationSuccess,
				events: []gent.Event{
					tt.BeforeExec(0, 0),
					tt.BeforeIter(0, 1),
					tt.AfterIter(0, 1, tt.Continue()),
					tt.BeforeIter(0, 2),
					tt.AfterIter(0, 2, tt.Continue()),
					tt.BeforeIter(0, 3),
					tt.AfterIter(0, 3, tt.Terminate("done")),
					tt.AfterExec(0, 3, gent.TerminationSuccess),
				},
			},
		},
		{
			name: "terminate at exactly limit value does not exceed",
			input: input{
				maxIterations: 5,
				terminateAt:   5,
			},
			expected: expected{
				calls:     5,
				terminate: gent.TerminationSuccess,
				events: []gent.Event{
					tt.BeforeExec(0, 0),
					tt.BeforeIter(0, 1),
					tt.AfterIter(0, 1, tt.Continue()),
					tt.BeforeIter(0, 2),
					tt.AfterIter(0, 2, tt.Continue()),
					tt.BeforeIter(0, 3),
					tt.AfterIter(0, 3, tt.Continue()),
					tt.BeforeIter(0, 4),
					tt.AfterIter(0, 4, tt.Continue()),
					tt.BeforeIter(0, 5),
					tt.AfterIter(0, 5, tt.Terminate("done")),
					tt.AfterExec(0, 5, gent.TerminationSuccess),
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			loop := &mockAgentLoop{terminateAt: tc.input.terminateAt}
			exec := executor.New[*mockLoopData](loop, executor.DefaultConfig())

			ctx := context.Background()
			data := newMockLoopData()
			execCtx := gent.NewExecutionContext(ctx, "test", data)
			execCtx.SetLimits([]gent.Limit{
				{Type: gent.LimitExactKey, Key: gent.KeyIterations, MaxValue: tc.input.maxIterations},
			})

			exec.Execute(execCtx)

			result := execCtx.Result()
			assert.NotNil(t, result)
			assert.Equal(t, tc.expected.terminate, result.TerminationReason)
			assert.Nil(t, result.ExceededLimit)
			assert.Equal(t, tc.expected.calls, loop.GetCalls())
			tt.AssertEventsEqual(t, tc.expected.events, tt.CollectLifecycleEvents(execCtx))
		})
	}
}

// -----------------------------------------------------------------------------
// Single AgentLoop Tests - Token Limit
// -----------------------------------------------------------------------------

func TestLimits_TokenLimit_Exceeded(t *testing.T) {
	type input struct {
		inputTokens  int
		outputTokens int
	}

	type limits struct {
		maxInput  float64
		maxOutput float64
	}

	type expected struct {
		calls         int
		iteration     int
		exceededLimit gent.Limit
		reason        gent.TerminationReason
		events        []gent.Event
	}

	tests := []struct {
		name     string
		input    input
		limits   limits
		expected expected
	}{
		{
			name:   "input token limit exceeded",
			input:  input{inputTokens: 1000, outputTokens: 100},
			limits: limits{maxInput: 2500, maxOutput: 10000},
			expected: expected{
				calls:     3,
				iteration: 3,
				exceededLimit: gent.Limit{
					Type: gent.LimitExactKey, Key: gent.KeyInputTokens, MaxValue: 2500},
				reason: gent.TerminationLimitExceeded,
				events: []gent.Event{
					tt.BeforeExec(0, 0),
					tt.BeforeIter(0, 1),
					tt.BeforeModelCall(0, 1, "test-model"),
					tt.AfterModelCall(0, 1, "test-model", 1000, 100),
					tt.AfterIter(0, 1, tt.Continue()),
					tt.BeforeIter(0, 2),
					tt.BeforeModelCall(0, 2, "test-model"),
					tt.AfterModelCall(0, 2, "test-model", 1000, 100),
					tt.AfterIter(0, 2, tt.Continue()),
					tt.BeforeIter(0, 3),
					tt.BeforeModelCall(0, 3, "test-model"),
					tt.AfterModelCall(0, 3, "test-model", 1000, 100),
					tt.LimitExceeded(0, 3,
						tt.ExactLimit(gent.KeyInputTokens, 2500), 3000, gent.KeyInputTokens),
					tt.AfterIter(0, 3, tt.Continue()),
					tt.AfterExec(0, 3, gent.TerminationLimitExceeded),
				},
			},
		},
		{
			name:   "output token limit exceeded",
			input:  input{inputTokens: 100, outputTokens: 500},
			limits: limits{maxInput: 10000, maxOutput: 1200},
			expected: expected{
				calls:     3,
				iteration: 3,
				exceededLimit: gent.Limit{
					Type: gent.LimitExactKey, Key: gent.KeyOutputTokens, MaxValue: 1200},
				reason: gent.TerminationLimitExceeded,
				events: []gent.Event{
					tt.BeforeExec(0, 0),
					tt.BeforeIter(0, 1),
					tt.BeforeModelCall(0, 1, "test-model"),
					tt.AfterModelCall(0, 1, "test-model", 100, 500),
					tt.AfterIter(0, 1, tt.Continue()),
					tt.BeforeIter(0, 2),
					tt.BeforeModelCall(0, 2, "test-model"),
					tt.AfterModelCall(0, 2, "test-model", 100, 500),
					tt.AfterIter(0, 2, tt.Continue()),
					tt.BeforeIter(0, 3),
					tt.BeforeModelCall(0, 3, "test-model"),
					tt.AfterModelCall(0, 3, "test-model", 100, 500),
					tt.LimitExceeded(0, 3,
						tt.ExactLimit(gent.KeyOutputTokens, 1200), 1500, gent.KeyOutputTokens),
					tt.AfterIter(0, 3, tt.Continue()),
					tt.AfterExec(0, 3, gent.TerminationLimitExceeded),
				},
			},
		},
		{
			name:   "input tokens check comes first when both exceeded",
			input:  input{inputTokens: 1000, outputTokens: 1000},
			limits: limits{maxInput: 1500, maxOutput: 1500},
			expected: expected{
				calls:         2,
				iteration:     2,
				exceededLimit: tt.ExactLimit(gent.KeyInputTokens, 1500),
				reason:        gent.TerminationLimitExceeded,
				events: []gent.Event{
					tt.BeforeExec(0, 0),
					tt.BeforeIter(0, 1),
					tt.BeforeModelCall(0, 1, "test-model"),
					tt.AfterModelCall(0, 1, "test-model", 1000, 1000),
					tt.AfterIter(0, 1, tt.Continue()),
					tt.BeforeIter(0, 2),
					tt.BeforeModelCall(0, 2, "test-model"),
					tt.AfterModelCall(0, 2, "test-model", 1000, 1000),
					tt.LimitExceeded(0, 2,
						tt.ExactLimit(gent.KeyInputTokens, 1500), 2000, gent.KeyInputTokens),
					tt.AfterIter(0, 2, tt.Continue()),
					tt.AfterExec(0, 2, gent.TerminationLimitExceeded),
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			loop := &mockAgentLoop{
				inputTokens:  tc.input.inputTokens,
				outputTokens: tc.input.outputTokens,
			}
			exec := executor.New[*mockLoopData](loop, executor.DefaultConfig())

			ctx := context.Background()
			data := newMockLoopData()
			execCtx := gent.NewExecutionContext(ctx, "test", data)
			execCtx.SetLimits([]gent.Limit{
				{Type: gent.LimitExactKey, Key: gent.KeyInputTokens, MaxValue: tc.limits.maxInput},
				{Type: gent.LimitExactKey, Key: gent.KeyOutputTokens, MaxValue: tc.limits.maxOutput},
				{Type: gent.LimitExactKey, Key: gent.KeyIterations, MaxValue: 100},
			})

			exec.Execute(execCtx)

			// Verify termination
			result := execCtx.Result()
			assert.NotNil(t, result)
			assert.Equal(t, tc.expected.reason, result.TerminationReason)
			assert.NotNil(t, result.ExceededLimit)
			assert.Equal(t, tc.expected.exceededLimit, *result.ExceededLimit)

			// Verify iteration and call counts
			assert.Equal(t, tc.expected.calls, loop.GetCalls())
			assert.Equal(t, tc.expected.iteration, execCtx.Iteration())

			// Verify events
			tt.AssertEventsEqual(t, tc.expected.events, tt.CollectLifecycleEvents(execCtx))
		})
	}
}

// -----------------------------------------------------------------------------
// Single AgentLoop Tests - Prefix Limits
// -----------------------------------------------------------------------------

func TestLimits_PrefixLimit_Exceeded(t *testing.T) {
	loop := &mockAgentLoop{
		nextFn: func(execCtx *gent.ExecutionContext) (*gent.AgentLoopResult, error) {
			// Simulate model calls to different model names
			iteration := execCtx.Iteration()
			var err error
			if iteration%2 == 1 {
				err = simulateModelCall(execCtx, "model-a", 1000, 0)
			} else {
				err = simulateModelCall(execCtx, "model-b", 500, 0)
			}
			if err != nil {
				return nil, err
			}
			return tt.Continue(), nil
		},
	}
	exec := executor.New[*mockLoopData](loop, executor.DefaultConfig())

	ctx := context.Background()
	data := newMockLoopData()
	execCtx := gent.NewExecutionContext(ctx, "test", data)
	execCtx.SetLimits([]gent.Limit{
		// Limit any single model to 2500 tokens
		{Type: gent.LimitKeyPrefix, Key: gent.KeyInputTokensFor, MaxValue: 2500},
		{Type: gent.LimitExactKey, Key: gent.KeyIterations, MaxValue: 100},
	})

	exec.Execute(execCtx)

	// Verify termination
	result := execCtx.Result()
	assert.NotNil(t, result)
	assert.Equal(t, gent.TerminationLimitExceeded, result.TerminationReason)
	assert.NotNil(t, result.ExceededLimit)
	assert.Equal(t, gent.KeyInputTokensFor, result.ExceededLimit.Key)

	// model-a gets 1000 tokens on iterations 1, 3, 5 -> 3000 tokens > 2500
	// So should terminate after iteration 5
	stats := execCtx.Stats()
	assert.Equal(t, int64(3000), stats.GetCounter(gent.KeyInputTokensFor+"model-a"))

	// Verify iteration count
	// Iterations: 1(a:1000), 2(b:500), 3(a:2000), 4(b:1000), 5(a:3000 > 2500, limit!)
	assert.Equal(t, 5, execCtx.Iteration())

	// Verify events
	expectedEvents := []gent.Event{
		tt.BeforeExec(0, 0),
		tt.BeforeIter(0, 1),
		tt.BeforeModelCall(0, 1, "model-a"),
		tt.AfterModelCall(0, 1, "model-a", 1000, 0),
		tt.AfterIter(0, 1, tt.Continue()),
		tt.BeforeIter(0, 2),
		tt.BeforeModelCall(0, 2, "model-b"),
		tt.AfterModelCall(0, 2, "model-b", 500, 0),
		tt.AfterIter(0, 2, tt.Continue()),
		tt.BeforeIter(0, 3),
		tt.BeforeModelCall(0, 3, "model-a"),
		tt.AfterModelCall(0, 3, "model-a", 1000, 0),
		tt.AfterIter(0, 3, tt.Continue()),
		tt.BeforeIter(0, 4),
		tt.BeforeModelCall(0, 4, "model-b"),
		tt.AfterModelCall(0, 4, "model-b", 500, 0),
		tt.AfterIter(0, 4, tt.Continue()),
		tt.BeforeIter(0, 5),
		tt.BeforeModelCall(0, 5, "model-a"),
		tt.AfterModelCall(0, 5, "model-a", 1000, 0),
		tt.LimitExceeded(0, 5,
			tt.PrefixLimit(gent.KeyInputTokensFor, 2500), 3000, gent.KeyInputTokensFor+"model-a"),
		tt.AfterIter(0, 5, tt.Continue()),
		tt.AfterExec(0, 5, gent.TerminationLimitExceeded),
	}
	tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
}

// -----------------------------------------------------------------------------
// Parallel Children Tests
// -----------------------------------------------------------------------------

func TestLimits_ParallelChildren_AggregateToParent(t *testing.T) {
	// Parent spawns multiple children running in parallel
	// All children contribute to parent stats
	loop := &mockAgentLoop{
		nextFn: func(execCtx *gent.ExecutionContext) (*gent.AgentLoopResult, error) {
			var wg sync.WaitGroup
			numChildren := 5
			tokensPerChild := 500

			for i := range numChildren {
				wg.Add(1)
				childData := newMockLoopData()
				child := execCtx.SpawnChild("child", childData)
				go func(c *gent.ExecutionContext, tokens int, childNum int) {
					defer wg.Done()
					defer execCtx.CompleteChild(c)
					// Each child simulates a model call (ignoring error in goroutine)
					_ = simulateModelCall(c, "child-model", tokens, 0)
				}(child, tokensPerChild, i)
			}
			wg.Wait()

			return tt.Terminate("done"), nil
		},
	}
	exec := executor.New[*mockLoopData](loop, executor.DefaultConfig())

	ctx := context.Background()
	data := newMockLoopData()
	execCtx := gent.NewExecutionContext(ctx, "test", data)
	execCtx.SetLimits([]gent.Limit{
		{Type: gent.LimitExactKey, Key: gent.KeyInputTokens, MaxValue: 10000},
		{Type: gent.LimitExactKey, Key: gent.KeyIterations, MaxValue: 100},
	})

	exec.Execute(execCtx)

	// Verify termination
	result := execCtx.Result()
	assert.NotNil(t, result)
	assert.Equal(t, gent.TerminationSuccess, result.TerminationReason)

	// Verify stats: Parent should have aggregated 5 * 500 = 2500 tokens
	stats := execCtx.Stats()
	assert.Equal(t, int64(2500), stats.GetTotalInputTokens())

	// Verify iteration count
	assert.Equal(t, 1, execCtx.Iteration())

	// Verify parent events (deterministic order)
	expectedParentEvents := []gent.Event{
		tt.BeforeExec(0, 0),
		tt.BeforeIter(0, 1),
		tt.AfterIter(0, 1, tt.Terminate("done")),
		tt.AfterExec(0, 1, gent.TerminationSuccess),
	}
	tt.AssertEventsEqual(t, expectedParentEvents, tt.CollectLifecycleEvents(execCtx))

	// Verify total events including children
	// Child events may arrive in any order due to parallelism, so count by type
	allEvents := tt.CollectLifecycleEventsWithChildren(execCtx)
	eventTypeCounts := tt.CountEventTypes(allEvents)

	assert.Equal(t, 1, eventTypeCounts["BeforeExecutionEvent"])
	assert.Equal(t, 1, eventTypeCounts["BeforeIterationEvent"])
	assert.Equal(t, 1, eventTypeCounts["AfterIterationEvent"])
	assert.Equal(t, 1, eventTypeCounts["AfterExecutionEvent"])
	assert.Equal(t, 5, eventTypeCounts["BeforeModelCallEvent"]) // 5 children each call model
	assert.Equal(t, 5, eventTypeCounts["AfterModelCallEvent"])  // All 5 complete

	// Verify each child event has the correct model and tokens
	for _, e := range allEvents {
		if afterModel, ok := e.(*gent.AfterModelCallEvent); ok {
			assert.Equal(t, "child-model", afterModel.Model)
			assert.Equal(t, 500, afterModel.InputTokens)
			assert.Equal(t, 0, afterModel.OutputTokens)
		}
	}
}

func TestLimits_ParallelChildren_LimitExceededOnNextIteration(t *testing.T) {
	// Children running in parallel all trace tokens.
	// Limit check happens immediately when stats are updated (as child stats propagate to root).
	// So if limit is exceeded during iteration N, context is cancelled immediately during N.
	iterCount := 0
	loop := &mockAgentLoop{
		nextFn: func(execCtx *gent.ExecutionContext) (*gent.AgentLoopResult, error) {
			iterCount++
			var wg sync.WaitGroup

			// Each iteration spawns 5 children, each tracing 200 tokens = 1000 per iteration
			for i := range 5 {
				wg.Add(1)
				childData := newMockLoopData()
				child := execCtx.SpawnChild("child", childData)
				go func(c *gent.ExecutionContext, childNum int) {
					defer wg.Done()
					defer execCtx.CompleteChild(c)
					// Ignoring error - context cancellation stops further AfterModelCall
					_ = simulateModelCall(c, "child-model", 200, 0)
				}(child, i)
			}
			wg.Wait()

			return tt.Continue(), nil
		},
	}
	exec := executor.New[*mockLoopData](loop, executor.DefaultConfig())

	ctx := context.Background()
	data := newMockLoopData()
	execCtx := gent.NewExecutionContext(ctx, "test", data)
	execCtx.SetLimits([]gent.Limit{
		// Each iteration: 5 children * 200 = 1000 tokens
		// Iter 1: 1000 tokens (< 1500, OK)
		// Iter 2: During execution, when cumulative tokens exceed 1500, context cancelled immediately
		// Iter 3: ctx.Err() detected at top of loop → terminate
		{Type: gent.LimitExactKey, Key: gent.KeyInputTokens, MaxValue: 1500},
		{Type: gent.LimitExactKey, Key: gent.KeyIterations, MaxValue: 100},
	})

	exec.Execute(execCtx)

	// Verify termination
	result := execCtx.Result()
	assert.NotNil(t, result)
	assert.Equal(t, gent.TerminationLimitExceeded, result.TerminationReason)
	assert.NotNil(t, result.ExceededLimit)
	assert.Equal(t, gent.KeyInputTokens, result.ExceededLimit.Key)

	// Verify iteration counts
	// Iter 1, 2 call Next(); limit exceeded during iter 2, detected at iter 3 start
	assert.Equal(t, 2, iterCount)
	assert.Equal(t, 2, execCtx.Iteration())

	// Verify parent events - LimitExceededEvent is published on parent when children exceed limits
	// Since limit is exceeded during iteration 2 by children (parallel), order is:
	// BeforeExecution -> BeforeIteration(1) -> AfterIteration(1) ->
	// BeforeIteration(2) -> LimitExceeded -> AfterIteration(2) -> AfterExecution
	parentEvents := tt.CollectLifecycleEvents(execCtx)
	parentEventTypeCounts := tt.CountEventTypes(parentEvents)

	assert.Equal(t, 1, parentEventTypeCounts["BeforeExecutionEvent"])
	assert.Equal(t, 2, parentEventTypeCounts["BeforeIterationEvent"])
	assert.Equal(t, 2, parentEventTypeCounts["AfterIterationEvent"])
	assert.Equal(t, 1, parentEventTypeCounts["AfterExecutionEvent"])
	assert.Equal(t, 1, parentEventTypeCounts["LimitExceededEvent"])

	// Verify LimitExceededEvent on parent has correct data
	for _, e := range parentEvents {
		if limitEvent, ok := e.(*gent.LimitExceededEvent); ok {
			assert.Equal(t, 2, limitEvent.Iteration)
			assert.Equal(t, gent.KeyInputTokens, limitEvent.Limit.Key)
			assert.Equal(t, 1500.0, limitEvent.Limit.MaxValue)
			assert.Greater(t, limitEvent.CurrentValue, 1500.0)
			assert.Equal(t, gent.KeyInputTokens, limitEvent.MatchedKey)
		}
		if afterExec, ok := e.(*gent.AfterExecutionEvent); ok {
			assert.Equal(t, gent.TerminationLimitExceeded, afterExec.TerminationReason)
		}
	}

	// Verify total events including children
	// Child events may arrive in any order due to parallelism, so count by type
	allEvents := tt.CollectLifecycleEventsWithChildren(execCtx)
	eventTypeCounts := tt.CountEventTypes(allEvents)

	assert.Equal(t, 1, eventTypeCounts["BeforeExecutionEvent"])
	assert.Equal(t, 2, eventTypeCounts["BeforeIterationEvent"])
	assert.Equal(t, 2, eventTypeCounts["AfterIterationEvent"])
	assert.Equal(t, 1, eventTypeCounts["AfterExecutionEvent"])
	assert.Equal(t, 10, eventTypeCounts["BeforeModelCallEvent"]) // 5 children * 2 iterations
	// AfterModelCall: 5 from iter 1 + some from iter 2 (at least 3 to trigger limit)
	assert.GreaterOrEqual(t, eventTypeCounts["AfterModelCallEvent"], 8) // At least 5 + 3
	assert.LessOrEqual(t, eventTypeCounts["AfterModelCallEvent"], 10)   // At most all 10
	// LimitExceededEvent should be present exactly once (on parent)
	assert.Equal(t, 1, eventTypeCounts["LimitExceededEvent"])
}

// -----------------------------------------------------------------------------
// Serial Children Tests
// -----------------------------------------------------------------------------

func TestLimits_SerialChildren_CumulativeLimit(t *testing.T) {
	// Each iteration spawns a serial child that traces 500 tokens.
	// Limit check happens immediately when stats are updated.
	// So when iter 3 traces 1500 tokens (> 1200), the limit is detected immediately.
	callCount := 0
	loop := &mockAgentLoop{
		nextFn: func(execCtx *gent.ExecutionContext) (*gent.AgentLoopResult, error) {
			callCount++
			// Spawn a child that uses 500 tokens
			childData := newMockLoopData()
			child := execCtx.SpawnChild("serial-child", childData)
			err := simulateModelCall(child, "child-model", 500, 0)
			execCtx.CompleteChild(child)
			if err != nil {
				return nil, err
			}

			return tt.Continue(), nil
		},
	}
	exec := executor.New[*mockLoopData](loop, executor.DefaultConfig())

	ctx := context.Background()
	data := newMockLoopData()
	execCtx := gent.NewExecutionContext(ctx, "test", data)
	execCtx.SetLimits([]gent.Limit{
		// Iter 1: 500 tokens (< 1200, OK)
		// Iter 2: 1000 tokens (< 1200, OK)
		// Iter 3: 1500 tokens (> 1200, limit detected during AfterModelCall)
		// Iter 4: ctx.Err() detected at top of loop → terminate
		{Type: gent.LimitExactKey, Key: gent.KeyInputTokens, MaxValue: 1200},
		{Type: gent.LimitExactKey, Key: gent.KeyIterations, MaxValue: 100},
	})

	exec.Execute(execCtx)

	// Verify termination
	result := execCtx.Result()
	assert.NotNil(t, result)
	assert.Equal(t, gent.TerminationLimitExceeded, result.TerminationReason)
	assert.Equal(t, gent.KeyInputTokens, result.ExceededLimit.Key)

	// Iter 1: 500, Iter 2: 1000, Iter 3: 1500 > 1200 (detected during iter 3)
	// Iter 4 never starts since ctx.Err() is detected at top of loop
	assert.Equal(t, 3, callCount)
	assert.Equal(t, 3, execCtx.Iteration())

	// Verify parent events - LimitExceededEvent is published on parent during iteration 3
	// Order: BeforeExecution -> iter1 -> iter2 -> BeforeIter3 -> LimitExceeded -> AfterIter3 ->
	// AfterExecution
	expectedParentEvents := []gent.Event{
		tt.BeforeExec(0, 0),
		tt.BeforeIter(0, 1),
		tt.AfterIter(0, 1, tt.Continue()),
		tt.BeforeIter(0, 2),
		tt.AfterIter(0, 2, tt.Continue()),
		tt.BeforeIter(0, 3),
		tt.LimitExceeded(0, 3,
			tt.ExactLimit(gent.KeyInputTokens, 1200), 1500, gent.KeyInputTokens),
		tt.AfterIter(0, 3, tt.Continue()),
		tt.AfterExec(0, 3, gent.TerminationLimitExceeded),
	}
	tt.AssertEventsEqual(t, expectedParentEvents, tt.CollectLifecycleEvents(execCtx))

	// Verify total events including children (serial, so deterministic order)
	allEvents := tt.CollectLifecycleEventsWithChildren(execCtx)
	eventTypeCounts := tt.CountEventTypes(allEvents)

	assert.Equal(t, 1, eventTypeCounts["BeforeExecutionEvent"])
	assert.Equal(t, 3, eventTypeCounts["BeforeIterationEvent"])
	assert.Equal(t, 3, eventTypeCounts["AfterIterationEvent"])
	assert.Equal(t, 1, eventTypeCounts["AfterExecutionEvent"])
	assert.Equal(t, 3, eventTypeCounts["BeforeModelCallEvent"]) // 3 children, 1 model call each
	assert.Equal(t, 3, eventTypeCounts["AfterModelCallEvent"])  // All 3 complete, limit on 3rd
	assert.Equal(t, 1, eventTypeCounts["LimitExceededEvent"])
}

// -----------------------------------------------------------------------------
// Mixed Topology Tests
// -----------------------------------------------------------------------------

func TestLimits_MixedTopology_NestedParallelAndSerial(t *testing.T) {
	// Parent runs iterations, each spawns parallel children.
	// Each iteration: 3 children * 100 tokens = 300 tokens.
	// Limit check happens immediately when stats are updated.
	iterCount := 0
	loop := &mockAgentLoop{
		nextFn: func(execCtx *gent.ExecutionContext) (*gent.AgentLoopResult, error) {
			iterCount++
			var wg sync.WaitGroup

			// Each iteration spawns 3 parallel children
			for i := range 3 {
				wg.Add(1)
				childData := newMockLoopData()
				child := execCtx.SpawnChild("parallel-child", childData)
				go func(c *gent.ExecutionContext, childNum int) {
					defer wg.Done()
					defer execCtx.CompleteChild(c)
					// Ignoring error - context cancellation stops further AfterModelCall
					_ = simulateModelCall(c, "child-model", 100, 0)
				}(child, i)
			}
			wg.Wait()

			return tt.Continue(), nil
		},
	}
	exec := executor.New[*mockLoopData](loop, executor.DefaultConfig())

	ctx := context.Background()
	data := newMockLoopData()
	execCtx := gent.NewExecutionContext(ctx, "test", data)
	execCtx.SetLimits([]gent.Limit{
		// Each iteration: 3 children * 100 = 300 tokens
		// Iter 1: 300 (< 1000, OK)
		// Iter 2: 600 (< 1000, OK)
		// Iter 3: 900 (< 1000, OK)
		// Iter 4: During execution, when cumulative > 1000, limit detected immediately
		// Iter 5: ctx.Err() detected at top of loop → terminate
		{Type: gent.LimitExactKey, Key: gent.KeyInputTokens, MaxValue: 1000},
		{Type: gent.LimitExactKey, Key: gent.KeyIterations, MaxValue: 100},
	})

	exec.Execute(execCtx)

	// Verify termination
	result := execCtx.Result()
	assert.NotNil(t, result)
	assert.Equal(t, gent.TerminationLimitExceeded, result.TerminationReason)
	assert.Equal(t, gent.KeyInputTokens, result.ExceededLimit.Key)

	// Iter 1: 300, Iter 2: 600, Iter 3: 900, Iter 4: >1000 (detected during iter 4)
	// Iter 5 never starts since ctx.Err() is detected at top of loop
	assert.Equal(t, 4, iterCount)
	assert.Equal(t, 4, execCtx.Iteration())

	// Verify parent events - LimitExceededEvent is published on parent when children exceed limits
	// Since children run in parallel, exact position of LimitExceededEvent may vary within iter 4
	parentEvents := tt.CollectLifecycleEvents(execCtx)
	parentEventTypeCounts := tt.CountEventTypes(parentEvents)

	assert.Equal(t, 1, parentEventTypeCounts["BeforeExecutionEvent"])
	assert.Equal(t, 4, parentEventTypeCounts["BeforeIterationEvent"])
	assert.Equal(t, 4, parentEventTypeCounts["AfterIterationEvent"])
	assert.Equal(t, 1, parentEventTypeCounts["AfterExecutionEvent"])
	assert.Equal(t, 1, parentEventTypeCounts["LimitExceededEvent"])

	// Verify parent event sequence (iteration order is deterministic, LimitExceeded is in iter 4)
	for _, e := range parentEvents {
		if limitEvent, ok := e.(*gent.LimitExceededEvent); ok {
			assert.Equal(t, 4, limitEvent.Iteration)
			assert.Equal(t, gent.KeyInputTokens, limitEvent.Limit.Key)
			assert.Equal(t, 1000.0, limitEvent.Limit.MaxValue)
			assert.Greater(t, limitEvent.CurrentValue, 1000.0)
			assert.Equal(t, gent.KeyInputTokens, limitEvent.MatchedKey)
		}
		if afterExec, ok := e.(*gent.AfterExecutionEvent); ok {
			assert.Equal(t, gent.TerminationLimitExceeded, afterExec.TerminationReason)
		}
	}

	// Verify total events including children
	// Child events may arrive in any order due to parallelism, so count by type
	allEvents := tt.CollectLifecycleEventsWithChildren(execCtx)
	eventTypeCounts := tt.CountEventTypes(allEvents)

	assert.Equal(t, 1, eventTypeCounts["BeforeExecutionEvent"])
	assert.Equal(t, 4, eventTypeCounts["BeforeIterationEvent"])
	assert.Equal(t, 4, eventTypeCounts["AfterIterationEvent"])
	assert.Equal(t, 1, eventTypeCounts["AfterExecutionEvent"])
	assert.Equal(t, 12, eventTypeCounts["BeforeModelCallEvent"]) // 3 children * 4 iterations
	// AfterModelCall: iter 1-3 complete (9), iter 4 partial (at least 1 to exceed 1000)
	assert.GreaterOrEqual(t, eventTypeCounts["AfterModelCallEvent"], 10) // At least 9 + 1
	assert.LessOrEqual(t, eventTypeCounts["AfterModelCallEvent"], 12)    // At most all 12
	assert.Equal(t, 1, eventTypeCounts["LimitExceededEvent"])
}

// -----------------------------------------------------------------------------
// Edge Case Tests
// -----------------------------------------------------------------------------

func TestLimits_EdgeCase_NoLimits(t *testing.T) {
	loop := &mockAgentLoop{terminateAt: 5}
	exec := executor.New[*mockLoopData](loop, executor.DefaultConfig())

	ctx := context.Background()
	data := newMockLoopData()
	execCtx := gent.NewExecutionContext(ctx, "test", data)
	execCtx.SetLimits(nil) // Clear all limits

	exec.Execute(execCtx)

	result := execCtx.Result()
	assert.NotNil(t, result)
	assert.Equal(t, gent.TerminationSuccess, result.TerminationReason)
	assert.Nil(t, result.ExceededLimit)
	assert.Equal(t, 5, loop.GetCalls())
}

func TestLimits_EdgeCase_EmptyLimits(t *testing.T) {
	loop := &mockAgentLoop{terminateAt: 5}
	exec := executor.New[*mockLoopData](loop, executor.DefaultConfig())

	ctx := context.Background()
	data := newMockLoopData()
	execCtx := gent.NewExecutionContext(ctx, "test", data)
	execCtx.SetLimits([]gent.Limit{}) // Empty slice

	exec.Execute(execCtx)

	result := execCtx.Result()
	assert.NotNil(t, result)
	assert.Equal(t, gent.TerminationSuccess, result.TerminationReason)
	assert.Nil(t, result.ExceededLimit)
}

func TestLimits_EdgeCase_ZeroMaxValue(t *testing.T) {
	// A limit with MaxValue 0 should trigger immediately when stat > 0
	loop := &mockAgentLoop{inputTokens: 100}
	exec := executor.New[*mockLoopData](loop, executor.DefaultConfig())

	ctx := context.Background()
	data := newMockLoopData()
	execCtx := gent.NewExecutionContext(ctx, "test", data)
	execCtx.SetLimits([]gent.Limit{
		{Type: gent.LimitExactKey, Key: gent.KeyInputTokens, MaxValue: 0},
		{Type: gent.LimitExactKey, Key: gent.KeyIterations, MaxValue: 100},
	})

	exec.Execute(execCtx)

	// Verify termination
	result := execCtx.Result()
	assert.NotNil(t, result)
	assert.Equal(t, gent.TerminationLimitExceeded, result.TerminationReason)
	assert.Equal(t, gent.KeyInputTokens, result.ExceededLimit.Key)

	// Limit triggered on first iteration during AfterModelCall
	assert.Equal(t, 1, loop.GetCalls())
	assert.Equal(t, 1, execCtx.Iteration())

	// Verify events
	expectedEvents := []gent.Event{
		tt.BeforeExec(0, 0),
		tt.BeforeIter(0, 1),
		tt.BeforeModelCall(0, 1, "test-model"),
		tt.AfterModelCall(0, 1, "test-model", 100, 0),
		tt.LimitExceeded(0, 1,
			tt.ExactLimit(gent.KeyInputTokens, 0), 100, gent.KeyInputTokens),
		tt.AfterIter(0, 1, tt.Continue()),
		tt.AfterExec(0, 1, gent.TerminationLimitExceeded),
	}
	tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
}

func TestLimits_EdgeCase_ExactlyAtLimit(t *testing.T) {
	// When value equals MaxValue, should NOT be exceeded (needs to be > MaxValue)
	loop := &mockAgentLoop{inputTokens: 500, terminateAt: 2}
	exec := executor.New[*mockLoopData](loop, executor.DefaultConfig())

	ctx := context.Background()
	data := newMockLoopData()
	execCtx := gent.NewExecutionContext(ctx, "test", data)
	execCtx.SetLimits([]gent.Limit{
		// 2 iterations * 500 = 1000, limit at 1000 should NOT trigger
		{Type: gent.LimitExactKey, Key: gent.KeyInputTokens, MaxValue: 1000},
		{Type: gent.LimitExactKey, Key: gent.KeyIterations, MaxValue: 100},
	})

	exec.Execute(execCtx)

	result := execCtx.Result()
	assert.NotNil(t, result)
	assert.Equal(t, gent.TerminationSuccess, result.TerminationReason)
	assert.Nil(t, result.ExceededLimit)
	assert.Equal(t, int64(1000), execCtx.Stats().GetTotalInputTokens())

	// Verify iteration count
	assert.Equal(t, 2, execCtx.Iteration())

	// Verify events
	expectedEvents := []gent.Event{
		tt.BeforeExec(0, 0),
		tt.BeforeIter(0, 1),
		tt.BeforeModelCall(0, 1, "test-model"),
		tt.AfterModelCall(0, 1, "test-model", 500, 0),
		tt.AfterIter(0, 1, tt.Continue()),
		tt.BeforeIter(0, 2),
		tt.BeforeModelCall(0, 2, "test-model"),
		tt.AfterModelCall(0, 2, "test-model", 500, 0),
		tt.AfterIter(0, 2, tt.Terminate("done")),
		tt.AfterExec(0, 2, gent.TerminationSuccess),
	}
	tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
}

func TestLimits_EdgeCase_MultipleMatchingLimits(t *testing.T) {
	// Multiple limits would be exceeded - first one wins
	loop := &mockAgentLoop{
		inputTokens:  1000,
		outputTokens: 1000,
	}
	exec := executor.New[*mockLoopData](loop, executor.DefaultConfig())

	ctx := context.Background()
	data := newMockLoopData()
	execCtx := gent.NewExecutionContext(ctx, "test", data)
	execCtx.SetLimits([]gent.Limit{
		{Type: gent.LimitExactKey, Key: gent.KeyInputTokens, MaxValue: 500},  // Would exceed
		{Type: gent.LimitExactKey, Key: gent.KeyOutputTokens, MaxValue: 500}, // Would exceed
		{Type: gent.LimitExactKey, Key: gent.KeyIterations, MaxValue: 100},
	})

	exec.Execute(execCtx)

	result := execCtx.Result()
	assert.NotNil(t, result)
	assert.Equal(t, gent.TerminationLimitExceeded, result.TerminationReason)
	// First limit in the list should be the one reported
	assert.Equal(t, gent.KeyInputTokens, result.ExceededLimit.Key)

	// Verify iteration count
	// Both input (1000) and output (1000) tokens exceed their limits (500) on first iteration
	assert.Equal(t, 1, loop.GetCalls())
	assert.Equal(t, 1, execCtx.Iteration())

	// Verify events
	expectedEvents := []gent.Event{
		tt.BeforeExec(0, 0),
		tt.BeforeIter(0, 1),
		tt.BeforeModelCall(0, 1, "test-model"),
		tt.AfterModelCall(0, 1, "test-model", 1000, 1000),
		tt.LimitExceeded(0, 1,
			tt.ExactLimit(gent.KeyInputTokens, 500), 1000, gent.KeyInputTokens),
		tt.AfterIter(0, 1, tt.Continue()),
		tt.AfterExec(0, 1, gent.TerminationLimitExceeded),
	}
	tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
}

func TestLimits_EdgeCase_ContextAlreadyCancelled(t *testing.T) {
	loop := &mockAgentLoop{terminateAt: 5}
	exec := executor.New[*mockLoopData](loop, executor.DefaultConfig())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before execution starts

	data := newMockLoopData()
	execCtx := gent.NewExecutionContext(ctx, "test", data)
	execCtx.SetLimits([]gent.Limit{
		{Type: gent.LimitExactKey, Key: gent.KeyIterations, MaxValue: 100},
	})

	exec.Execute(execCtx)

	result := execCtx.Result()
	assert.NotNil(t, result)
	assert.Equal(t, gent.TerminationContextCanceled, result.TerminationReason)
	assert.Nil(t, result.ExceededLimit)
	assert.Equal(t, 0, loop.GetCalls()) // No iterations should have run

	// Verify events
	expectedEvents := []gent.Event{
		tt.BeforeExec(0, 0),
		tt.AfterExec(0, 0, gent.TerminationContextCanceled),
	}
	tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
}

// -----------------------------------------------------------------------------
// Consecutive Error Limit Tests
// -----------------------------------------------------------------------------

func TestLimits_ConsecutiveFormatParseErrors_Exceeded(t *testing.T) {
	errorCount := 0
	loop := &mockAgentLoop{
		nextFn: func(execCtx *gent.ExecutionContext) (*gent.AgentLoopResult, error) {
			errorCount++
			// Simulate format parse error
			execCtx.PublishParseError(gent.ParseErrorTypeFormat, "invalid content", nil)
			return tt.Continue(), nil
		},
	}
	exec := executor.New[*mockLoopData](loop, executor.DefaultConfig())

	ctx := context.Background()
	data := newMockLoopData()
	execCtx := gent.NewExecutionContext(ctx, "test", data)
	execCtx.SetLimits([]gent.Limit{
		{Type: gent.LimitExactKey, Key: gent.KeyFormatParseErrorConsecutive, MaxValue: 3},
		{Type: gent.LimitExactKey, Key: gent.KeyIterations, MaxValue: 100},
	})

	exec.Execute(execCtx)

	result := execCtx.Result()
	assert.NotNil(t, result)
	assert.Equal(t, gent.TerminationLimitExceeded, result.TerminationReason)
	assert.Equal(t, gent.KeyFormatParseErrorConsecutive, result.ExceededLimit.Key)
	assert.Equal(t, 4, errorCount) // 4 errors exceed the limit of 3

	// Verify events
	expectedEvents := []gent.Event{
		tt.BeforeExec(0, 0),
		tt.BeforeIter(0, 1),
		tt.ParseError(0, 1, gent.ParseErrorTypeFormat, "invalid content"),
		tt.AfterIter(0, 1, tt.Continue()),
		tt.BeforeIter(0, 2),
		tt.ParseError(0, 2, gent.ParseErrorTypeFormat, "invalid content"),
		tt.AfterIter(0, 2, tt.Continue()),
		tt.BeforeIter(0, 3),
		tt.ParseError(0, 3, gent.ParseErrorTypeFormat, "invalid content"),
		tt.AfterIter(0, 3, tt.Continue()),
		tt.BeforeIter(0, 4),
		tt.ParseError(0, 4, gent.ParseErrorTypeFormat, "invalid content"),
		tt.LimitExceeded(0, 4,
			tt.ExactLimit(gent.KeyFormatParseErrorConsecutive, 3), 4,
			gent.KeyFormatParseErrorConsecutive),
		tt.AfterIter(0, 4, tt.Continue()),
		tt.AfterExec(0, 4, gent.TerminationLimitExceeded),
	}
	tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
}

func TestLimits_ConsecutiveToolchainParseErrors_Exceeded(t *testing.T) {
	errorCount := 0
	loop := &mockAgentLoop{
		nextFn: func(execCtx *gent.ExecutionContext) (*gent.AgentLoopResult, error) {
			errorCount++
			// Simulate toolchain parse error
			execCtx.PublishParseError(gent.ParseErrorTypeToolchain, "invalid yaml", nil)
			return tt.Continue(), nil
		},
	}
	exec := executor.New[*mockLoopData](loop, executor.DefaultConfig())

	ctx := context.Background()
	data := newMockLoopData()
	execCtx := gent.NewExecutionContext(ctx, "test", data)
	execCtx.SetLimits([]gent.Limit{
		{Type: gent.LimitExactKey, Key: gent.KeyToolchainParseErrorConsecutive, MaxValue: 2},
		{Type: gent.LimitExactKey, Key: gent.KeyIterations, MaxValue: 100},
	})

	exec.Execute(execCtx)

	result := execCtx.Result()
	assert.NotNil(t, result)
	assert.Equal(t, gent.TerminationLimitExceeded, result.TerminationReason)
	assert.Equal(t, gent.KeyToolchainParseErrorConsecutive, result.ExceededLimit.Key)
	assert.Equal(t, 3, errorCount) // 3 errors exceed the limit of 2

	// Verify events
	expectedEvents := []gent.Event{
		tt.BeforeExec(0, 0),
		tt.BeforeIter(0, 1),
		tt.ParseError(0, 1, gent.ParseErrorTypeToolchain, "invalid yaml"),
		tt.AfterIter(0, 1, tt.Continue()),
		tt.BeforeIter(0, 2),
		tt.ParseError(0, 2, gent.ParseErrorTypeToolchain, "invalid yaml"),
		tt.AfterIter(0, 2, tt.Continue()),
		tt.BeforeIter(0, 3),
		tt.ParseError(0, 3, gent.ParseErrorTypeToolchain, "invalid yaml"),
		tt.LimitExceeded(0, 3,
			tt.ExactLimit(gent.KeyToolchainParseErrorConsecutive, 2), 3,
			gent.KeyToolchainParseErrorConsecutive),
		tt.AfterIter(0, 3, tt.Continue()),
		tt.AfterExec(0, 3, gent.TerminationLimitExceeded),
	}
	tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
}

func TestLimits_ConsecutiveErrors_ResetOnSuccess(t *testing.T) {
	// Alternating errors and successes should not trigger consecutive limit
	callCount := 0
	loop := &mockAgentLoop{
		nextFn: func(execCtx *gent.ExecutionContext) (*gent.AgentLoopResult, error) {
			callCount++
			if callCount%2 == 1 {
				// Odd iterations: parse error
				execCtx.PublishParseError(gent.ParseErrorTypeFormat, "invalid", nil)
			} else {
				// Even iterations: success (reset consecutive counter)
				execCtx.Stats().ResetCounter(gent.KeyFormatParseErrorConsecutive)
			}

			if callCount >= 10 {
				return tt.Terminate("done"), nil
			}
			return tt.Continue(), nil
		},
	}
	exec := executor.New[*mockLoopData](loop, executor.DefaultConfig())

	ctx := context.Background()
	data := newMockLoopData()
	execCtx := gent.NewExecutionContext(ctx, "test", data)
	execCtx.SetLimits([]gent.Limit{
		{Type: gent.LimitExactKey, Key: gent.KeyFormatParseErrorConsecutive, MaxValue: 3},
		{Type: gent.LimitExactKey, Key: gent.KeyIterations, MaxValue: 100},
	})

	exec.Execute(execCtx)

	result := execCtx.Result()
	assert.NotNil(t, result)
	assert.Equal(t, gent.TerminationSuccess, result.TerminationReason)
	assert.Nil(t, result.ExceededLimit)
	assert.Equal(t, 10, callCount)

	// Total errors should be 5 (iterations 1, 3, 5, 7, 9)
	stats := execCtx.Stats()
	assert.Equal(t, int64(5), stats.GetFormatParseErrorTotal())

	// Verify events - parse errors occur on odd iterations (1, 3, 5, 7, 9)
	expectedEvents := []gent.Event{
		tt.BeforeExec(0, 0),
		tt.BeforeIter(0, 1),
		tt.ParseError(0, 1, gent.ParseErrorTypeFormat, "invalid"),
		tt.AfterIter(0, 1, tt.Continue()),
		tt.BeforeIter(0, 2),
		tt.AfterIter(0, 2, tt.Continue()),
		tt.BeforeIter(0, 3),
		tt.ParseError(0, 3, gent.ParseErrorTypeFormat, "invalid"),
		tt.AfterIter(0, 3, tt.Continue()),
		tt.BeforeIter(0, 4),
		tt.AfterIter(0, 4, tt.Continue()),
		tt.BeforeIter(0, 5),
		tt.ParseError(0, 5, gent.ParseErrorTypeFormat, "invalid"),
		tt.AfterIter(0, 5, tt.Continue()),
		tt.BeforeIter(0, 6),
		tt.AfterIter(0, 6, tt.Continue()),
		tt.BeforeIter(0, 7),
		tt.ParseError(0, 7, gent.ParseErrorTypeFormat, "invalid"),
		tt.AfterIter(0, 7, tt.Continue()),
		tt.BeforeIter(0, 8),
		tt.AfterIter(0, 8, tt.Continue()),
		tt.BeforeIter(0, 9),
		tt.ParseError(0, 9, gent.ParseErrorTypeFormat, "invalid"),
		tt.AfterIter(0, 9, tt.Continue()),
		tt.BeforeIter(0, 10),
		tt.AfterIter(0, 10, tt.Terminate("done")),
		tt.AfterExec(0, 10, gent.TerminationSuccess),
	}
	tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
}

// -----------------------------------------------------------------------------
// Default Limits Tests
// -----------------------------------------------------------------------------

func TestLimits_DefaultLimits_Applied(t *testing.T) {
	// Verify that NewExecutionContext applies default limits
	ctx := context.Background()
	data := newMockLoopData()
	execCtx := gent.NewExecutionContext(ctx, "test", data)

	limits := execCtx.Limits()
	assert.NotEmpty(t, limits)

	// Should contain iteration limit
	found := false
	for _, limit := range limits {
		if limit.Key == gent.KeyIterations {
			found = true
			assert.Equal(t, float64(100), limit.MaxValue)
			break
		}
	}
	assert.True(t, found, "Default limits should include iteration limit")
}

func TestLimits_DefaultLimits_CanBeOverridden(t *testing.T) {
	ctx := context.Background()
	data := newMockLoopData()
	execCtx := gent.NewExecutionContext(ctx, "test", data)

	customLimits := []gent.Limit{
		{Type: gent.LimitExactKey, Key: "custom:limit", MaxValue: 42},
	}
	execCtx.SetLimits(customLimits)

	limits := execCtx.Limits()
	assert.Len(t, limits, 1)
	assert.Equal(t, "custom:limit", limits[0].Key)
	assert.Equal(t, float64(42), limits[0].MaxValue)
}

// -----------------------------------------------------------------------------
// Gauge-Specific Tests
// -----------------------------------------------------------------------------

func TestLimits_GaugeLimit_Exceeded(t *testing.T) {
	type input struct {
		gaugeKey       string
		gaugeIncrement float64
	}

	type mocks struct {
		maxValue float64
	}

	type expected struct {
		terminationReason gent.TerminationReason
		exceededKey       string
		callCount         int
		finalGaugeValue   float64
	}

	tests := []struct {
		name     string
		input    input
		mocks    mocks
		expected expected
	}{
		{
			name: "gauge limit exceeded after 3 increments",
			input: input{
				gaugeKey:       "test:custom_gauge",
				gaugeIncrement: 0.05,
			},
			mocks: mocks{
				maxValue: 0.12,
			},
			expected: expected{
				terminationReason: gent.TerminationLimitExceeded,
				exceededKey:       "test:custom_gauge",
				callCount:         3,
				finalGaugeValue:   0.15,
			},
		},
		{
			name: "gauge limit exceeded on first increment",
			input: input{
				gaugeKey:       "test:another_gauge",
				gaugeIncrement: 1.0,
			},
			mocks: mocks{
				maxValue: 0.5,
			},
			expected: expected{
				terminationReason: gent.TerminationLimitExceeded,
				exceededKey:       "test:another_gauge",
				callCount:         1,
				finalGaugeValue:   1.0,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			loop := &mockAgentLoop{
				nextFn: func(execCtx *gent.ExecutionContext) (*gent.AgentLoopResult, error) {
					execCtx.Stats().IncrGauge(tc.input.gaugeKey, tc.input.gaugeIncrement)
					return tt.Continue(), nil
				},
			}
			exec := executor.New[*mockLoopData](loop, executor.DefaultConfig())

			ctx := context.Background()
			data := newMockLoopData()
			execCtx := gent.NewExecutionContext(ctx, "test", data)
			execCtx.SetLimits([]gent.Limit{
				{Type: gent.LimitExactKey, Key: tc.input.gaugeKey, MaxValue: tc.mocks.maxValue},
				{Type: gent.LimitExactKey, Key: gent.KeyIterations, MaxValue: 100},
			})

			exec.Execute(execCtx)

			result := execCtx.Result()
			assert.NotNil(t, result)
			assert.Equal(t, tc.expected.terminationReason, result.TerminationReason)
			assert.NotNil(t, result.ExceededLimit)
			assert.Equal(t, tc.expected.exceededKey, result.ExceededLimit.Key)
			assert.Equal(t, tc.expected.callCount, loop.GetCalls())

			stats := execCtx.Stats()
			assert.InDelta(t, tc.expected.finalGaugeValue, stats.GetGauge(tc.input.gaugeKey), 0.001)
		})
	}
}

func TestLimits_GaugePrefixLimit_Exceeded(t *testing.T) {
	type input struct {
		gaugePrefix string
	}

	type mocks struct {
		maxValue float64
	}

	type expected struct {
		terminationReason gent.TerminationReason
		exceededKeyPrefix string
		expensiveValue    float64
		cheapValue        float64
	}

	tests := []struct {
		name     string
		input    input
		mocks    mocks
		expected expected
	}{
		{
			name: "prefix limit triggers on expensive gauge",
			input: input{
				gaugePrefix: "test:resource:",
			},
			mocks: mocks{
				maxValue: 0.25,
			},
			expected: expected{
				terminationReason: gent.TerminationLimitExceeded,
				exceededKeyPrefix: "test:resource:",
				expensiveValue:    0.30, // 3 iterations * 0.10
				cheapValue:        0.02, // 2 iterations * 0.01
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			callCount := 0
			loop := &mockAgentLoop{
				nextFn: func(execCtx *gent.ExecutionContext) (*gent.AgentLoopResult, error) {
					callCount++
					// Alternate between expensive and cheap resources
					if callCount%2 == 1 {
						execCtx.Stats().IncrGauge(tc.input.gaugePrefix+"expensive", 0.10)
					} else {
						execCtx.Stats().IncrGauge(tc.input.gaugePrefix+"cheap", 0.01)
					}
					return tt.Continue(), nil
				},
			}
			exec := executor.New[*mockLoopData](loop, executor.DefaultConfig())

			ctx := context.Background()
			data := newMockLoopData()
			execCtx := gent.NewExecutionContext(ctx, "test", data)
			execCtx.SetLimits([]gent.Limit{
				{Type: gent.LimitKeyPrefix, Key: tc.input.gaugePrefix, MaxValue: tc.mocks.maxValue},
				{Type: gent.LimitExactKey, Key: gent.KeyIterations, MaxValue: 100},
			})

			exec.Execute(execCtx)

			result := execCtx.Result()
			assert.NotNil(t, result)
			assert.Equal(t, tc.expected.terminationReason, result.TerminationReason)
			assert.Equal(t, tc.expected.exceededKeyPrefix, result.ExceededLimit.Key)

			// expensive: 0.10 per odd iteration -> iterations 1,3,5 = 0.30 > 0.25
			stats := execCtx.Stats()
			assert.InDelta(t, tc.expected.expensiveValue,
				stats.GetGauge(tc.input.gaugePrefix+"expensive"), 0.001)
			assert.InDelta(t, tc.expected.cheapValue,
				stats.GetGauge(tc.input.gaugePrefix+"cheap"), 0.001)
		})
	}
}

func TestLimits_GaugeExactKey_NotExceededUntilOverThreshold(t *testing.T) {
	type input struct {
		gaugeKey       string
		gaugeIncrement float64
		terminateAt    int
	}

	type mocks struct {
		maxValue float64
	}

	type expected struct {
		terminationReason gent.TerminationReason
		finalGaugeValue   float64
	}

	tests := []struct {
		name     string
		input    input
		mocks    mocks
		expected expected
	}{
		{
			name: "gauge at exactly max value does not exceed",
			input: input{
				gaugeKey:       "test:threshold_gauge",
				gaugeIncrement: 0.25,
				terminateAt:    4, // 4 * 0.25 = 1.0 exactly at limit
			},
			mocks: mocks{
				maxValue: 1.0,
			},
			expected: expected{
				terminationReason: gent.TerminationSuccess,
				finalGaugeValue:   1.0,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			callCount := 0
			loop := &mockAgentLoop{
				nextFn: func(execCtx *gent.ExecutionContext) (*gent.AgentLoopResult, error) {
					callCount++
					execCtx.Stats().IncrGauge(tc.input.gaugeKey, tc.input.gaugeIncrement)
					if callCount >= tc.input.terminateAt {
						return tt.Terminate("done"), nil
					}
					return tt.Continue(), nil
				},
			}
			exec := executor.New[*mockLoopData](loop, executor.DefaultConfig())

			ctx := context.Background()
			data := newMockLoopData()
			execCtx := gent.NewExecutionContext(ctx, "test", data)
			execCtx.SetLimits([]gent.Limit{
				{Type: gent.LimitExactKey, Key: tc.input.gaugeKey, MaxValue: tc.mocks.maxValue},
				{Type: gent.LimitExactKey, Key: gent.KeyIterations, MaxValue: 100},
			})

			exec.Execute(execCtx)

			result := execCtx.Result()
			assert.NotNil(t, result)
			assert.Equal(t, tc.expected.terminationReason, result.TerminationReason)
			assert.Nil(t, result.ExceededLimit)
			assert.InDelta(t, tc.expected.finalGaugeValue,
				execCtx.Stats().GetGauge(tc.input.gaugeKey), 0.001)
		})
	}
}

// -----------------------------------------------------------------------------
// Stats Aggregation Verification
// -----------------------------------------------------------------------------

func TestLimits_StatsAggregation_VerifyAccuracy(t *testing.T) {
	// Comprehensive test that verifies stats are accurately aggregated
	loop := &mockAgentLoop{
		nextFn: func(execCtx *gent.ExecutionContext) (*gent.AgentLoopResult, error) {
			// Parent simulates a model call
			if err := simulateModelCall(execCtx, "parent-model", 100, 50); err != nil {
				return nil, err
			}

			// Spawn child that also simulates a model call
			childData := newMockLoopData()
			child := execCtx.SpawnChild("child", childData)
			_ = simulateModelCall(child, "child-model", 200, 100) // Ignore error for child
			execCtx.CompleteChild(child)

			return tt.Terminate("done"), nil
		},
	}
	exec := executor.New[*mockLoopData](loop, executor.DefaultConfig())

	ctx := context.Background()
	data := newMockLoopData()
	execCtx := gent.NewExecutionContext(ctx, "test", data)

	exec.Execute(execCtx)

	stats := execCtx.Stats()

	// Parent stats should include both parent and child contributions
	assert.Equal(t, int64(300), stats.GetTotalInputTokens())  // 100 + 200
	assert.Equal(t, int64(150), stats.GetTotalOutputTokens()) // 50 + 100

	// Per-model stats
	assert.Equal(t, int64(100), stats.GetCounter(gent.KeyInputTokensFor+"parent-model"))
	assert.Equal(t, int64(200), stats.GetCounter(gent.KeyInputTokensFor+"child-model"))

	// Verify iteration count
	assert.Equal(t, 1, execCtx.Iteration())

	// Verify parent events (deterministic order)
	expectedParentEvents := []gent.Event{
		tt.BeforeExec(0, 0),
		tt.BeforeIter(0, 1),
		tt.BeforeModelCall(0, 1, "parent-model"),
		tt.AfterModelCall(0, 1, "parent-model", 100, 50),
		tt.AfterIter(0, 1, tt.Terminate("done")),
		tt.AfterExec(0, 1, gent.TerminationSuccess),
	}
	tt.AssertEventsEqual(t, expectedParentEvents, tt.CollectLifecycleEvents(execCtx))

	// Verify total events including children
	allEvents := tt.CollectLifecycleEventsWithChildren(execCtx)
	eventTypeCounts := tt.CountEventTypes(allEvents)

	assert.Equal(t, 1, eventTypeCounts["BeforeExecutionEvent"])
	assert.Equal(t, 1, eventTypeCounts["BeforeIterationEvent"]) // Only parent has iterations
	assert.Equal(t, 1, eventTypeCounts["AfterIterationEvent"])
	assert.Equal(t, 1, eventTypeCounts["AfterExecutionEvent"])
	assert.Equal(t, 2, eventTypeCounts["BeforeModelCallEvent"]) // Parent + child
	assert.Equal(t, 2, eventTypeCounts["AfterModelCallEvent"])  // Both complete

	// Verify child events have correct data
	childModelCallCount := 0
	for _, e := range allEvents {
		if afterModel, ok := e.(*gent.AfterModelCallEvent); ok && afterModel.Model == "child-model" {
			childModelCallCount++
			assert.Equal(t, 200, afterModel.InputTokens)
			assert.Equal(t, 100, afterModel.OutputTokens)
		}
	}
	assert.Equal(t, 1, childModelCallCount)
}

// -----------------------------------------------------------------------------
// LimitExceededEvent Tests
// -----------------------------------------------------------------------------

func TestLimits_LimitExceededEvent_PublishedOnIterationLimit(t *testing.T) {
	loop := &mockAgentLoop{} // Never auto-terminates
	exec := executor.New[*mockLoopData](loop, executor.DefaultConfig())

	ctx := context.Background()
	data := newMockLoopData()
	execCtx := gent.NewExecutionContext(ctx, "test", data)
	execCtx.SetLimits([]gent.Limit{
		{Type: gent.LimitExactKey, Key: gent.KeyIterations, MaxValue: 3},
	})

	exec.Execute(execCtx)

	// Verify termination
	result := execCtx.Result()
	assert.Equal(t, gent.TerminationLimitExceeded, result.TerminationReason)

	// Verify events
	expectedEvents := []gent.Event{
		tt.BeforeExec(0, 0),
		tt.BeforeIter(0, 1),
		tt.AfterIter(0, 1, tt.Continue()),
		tt.BeforeIter(0, 2),
		tt.AfterIter(0, 2, tt.Continue()),
		tt.BeforeIter(0, 3),
		tt.AfterIter(0, 3, tt.Continue()),
		tt.BeforeIter(0, 4),
		tt.LimitExceeded(0, 4, tt.ExactLimit(gent.KeyIterations, 3), 4, gent.KeyIterations),
		tt.AfterIter(0, 4, tt.Continue()),
		tt.AfterExec(0, 4, gent.TerminationLimitExceeded),
	}
	tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
}

func TestLimits_LimitExceededEvent_PublishedOnTokenLimit(t *testing.T) {
	loop := &mockAgentLoop{
		inputTokens:  500,
		outputTokens: 100,
	}
	exec := executor.New[*mockLoopData](loop, executor.DefaultConfig())

	ctx := context.Background()
	data := newMockLoopData()
	execCtx := gent.NewExecutionContext(ctx, "test", data)
	execCtx.SetLimits([]gent.Limit{
		{Type: gent.LimitExactKey, Key: gent.KeyInputTokens, MaxValue: 1000},
		{Type: gent.LimitExactKey, Key: gent.KeyIterations, MaxValue: 100},
	})

	exec.Execute(execCtx)

	// Verify termination
	result := execCtx.Result()
	assert.Equal(t, gent.TerminationLimitExceeded, result.TerminationReason)
	assert.Equal(t, gent.KeyInputTokens, result.ExceededLimit.Key)

	// Verify events
	expectedEvents := []gent.Event{
		tt.BeforeExec(0, 0),
		tt.BeforeIter(0, 1),
		tt.BeforeModelCall(0, 1, "test-model"),
		tt.AfterModelCall(0, 1, "test-model", 500, 100),
		tt.AfterIter(0, 1, tt.Continue()),
		tt.BeforeIter(0, 2),
		tt.BeforeModelCall(0, 2, "test-model"),
		tt.AfterModelCall(0, 2, "test-model", 500, 100),
		tt.AfterIter(0, 2, tt.Continue()),
		tt.BeforeIter(0, 3),
		tt.BeforeModelCall(0, 3, "test-model"),
		tt.AfterModelCall(0, 3, "test-model", 500, 100),
		tt.LimitExceeded(0, 3,
			tt.ExactLimit(gent.KeyInputTokens, 1000), 1500, gent.KeyInputTokens),
		tt.AfterIter(0, 3, tt.Continue()),
		tt.AfterExec(0, 3, gent.TerminationLimitExceeded),
	}
	tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
}

func TestLimits_LimitExceededEvent_PrefixLimit_ContainsMatchedKey(t *testing.T) {
	loop := &mockAgentLoop{
		nextFn: func(execCtx *gent.ExecutionContext) (*gent.AgentLoopResult, error) {
			// Alternate between two models
			iteration := execCtx.Iteration()
			var err error
			if iteration%2 == 1 {
				err = simulateModelCall(execCtx, "expensive-model", 1000, 0)
			} else {
				err = simulateModelCall(execCtx, "cheap-model", 100, 0)
			}
			if err != nil {
				return nil, err
			}
			return tt.Continue(), nil
		},
	}
	exec := executor.New[*mockLoopData](loop, executor.DefaultConfig())

	ctx := context.Background()
	data := newMockLoopData()
	execCtx := gent.NewExecutionContext(ctx, "test", data)
	execCtx.SetLimits([]gent.Limit{
		{Type: gent.LimitKeyPrefix, Key: gent.KeyInputTokensFor, MaxValue: 2500},
		{Type: gent.LimitExactKey, Key: gent.KeyIterations, MaxValue: 100},
	})

	exec.Execute(execCtx)

	// Verify events
	// expensive-model: 1000 on iterations 1, 3, 5 -> 3000 > 2500 (limit on iter 5)
	// cheap-model: 100 on iterations 2, 4 -> 200
	expectedEvents := []gent.Event{
		tt.BeforeExec(0, 0),
		tt.BeforeIter(0, 1),
		tt.BeforeModelCall(0, 1, "expensive-model"),
		tt.AfterModelCall(0, 1, "expensive-model", 1000, 0),
		tt.AfterIter(0, 1, tt.Continue()),
		tt.BeforeIter(0, 2),
		tt.BeforeModelCall(0, 2, "cheap-model"),
		tt.AfterModelCall(0, 2, "cheap-model", 100, 0),
		tt.AfterIter(0, 2, tt.Continue()),
		tt.BeforeIter(0, 3),
		tt.BeforeModelCall(0, 3, "expensive-model"),
		tt.AfterModelCall(0, 3, "expensive-model", 1000, 0),
		tt.AfterIter(0, 3, tt.Continue()),
		tt.BeforeIter(0, 4),
		tt.BeforeModelCall(0, 4, "cheap-model"),
		tt.AfterModelCall(0, 4, "cheap-model", 100, 0),
		tt.AfterIter(0, 4, tt.Continue()),
		tt.BeforeIter(0, 5),
		tt.BeforeModelCall(0, 5, "expensive-model"),
		tt.AfterModelCall(0, 5, "expensive-model", 1000, 0),
		tt.LimitExceeded(0, 5,
			tt.PrefixLimit(gent.KeyInputTokensFor, 2500), 3000,
			gent.KeyInputTokensFor+"expensive-model"),
		tt.AfterIter(0, 5, tt.Continue()),
		tt.AfterExec(0, 5, gent.TerminationLimitExceeded),
	}
	tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
}

func TestLimits_LimitExceededEvent_NotPublishedOnSuccess(t *testing.T) {
	loop := &mockAgentLoop{terminateAt: 3}
	exec := executor.New[*mockLoopData](loop, executor.DefaultConfig())

	ctx := context.Background()
	data := newMockLoopData()
	execCtx := gent.NewExecutionContext(ctx, "test", data)
	execCtx.SetLimits([]gent.Limit{
		{Type: gent.LimitExactKey, Key: gent.KeyIterations, MaxValue: 10},
	})

	exec.Execute(execCtx)

	// Verify success
	result := execCtx.Result()
	assert.Equal(t, gent.TerminationSuccess, result.TerminationReason)

	// Verify events - no LimitExceededEvent should be present
	expectedEvents := []gent.Event{
		tt.BeforeExec(0, 0),
		tt.BeforeIter(0, 1),
		tt.AfterIter(0, 1, tt.Continue()),
		tt.BeforeIter(0, 2),
		tt.AfterIter(0, 2, tt.Continue()),
		tt.BeforeIter(0, 3),
		tt.AfterIter(0, 3, tt.Terminate("done")),
		tt.AfterExec(0, 3, gent.TerminationSuccess),
	}
	tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
}
