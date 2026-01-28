package executor_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/executor"
	"github.com/stretchr/testify/assert"
	"github.com/tmc/langchaingo/llms"
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
		return &gent.AgentLoopResult{
			Action: gent.LATerminate,
			Result: []gent.ContentPart{llms.TextContent{Text: "done"}},
		}, nil
	}

	return &gent.AgentLoopResult{Action: gent.LAContinue}, nil
}

func (m *mockAgentLoop) GetCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// -----------------------------------------------------------------------------
// Event Counting Helpers
// -----------------------------------------------------------------------------

// eventCounts holds counts of different event types for test assertions.
type eventCounts struct {
	BeforeIteration int
	AfterIteration  int
	BeforeModelCall int
	AfterModelCall  int
}

// countEvents counts events by type from an ExecutionContext (not including children).
func countEvents(execCtx *gent.ExecutionContext) eventCounts {
	counts := eventCounts{}
	for _, event := range execCtx.Events() {
		switch event.(type) {
		case *gent.BeforeIterationEvent:
			counts.BeforeIteration++
		case *gent.AfterIterationEvent:
			counts.AfterIteration++
		case *gent.BeforeModelCallEvent:
			counts.BeforeModelCall++
		case *gent.AfterModelCallEvent:
			counts.AfterModelCall++
		}
	}
	return counts
}

// countEventsWithChildren counts events by type from an ExecutionContext and all its children.
func countEventsWithChildren(execCtx *gent.ExecutionContext) eventCounts {
	counts := countEvents(execCtx)
	for _, child := range execCtx.Children() {
		childCounts := countEventsWithChildren(child)
		counts.BeforeIteration += childCounts.BeforeIteration
		counts.AfterIteration += childCounts.AfterIteration
		counts.BeforeModelCall += childCounts.BeforeModelCall
		counts.AfterModelCall += childCounts.AfterModelCall
	}
	return counts
}

// -----------------------------------------------------------------------------
// Single AgentLoop Tests - Iteration Limit
// -----------------------------------------------------------------------------

func TestLimits_IterationLimit_Exceeded(t *testing.T) {
	// Note: Limits use > comparison, so if MaxValue=5, then 6 iterations run
	// before the limit is exceeded (because 6 > 5). The loop detects the exceeded
	// limit at the start of iteration N+1 when iteration counter exceeds MaxValue.
	tests := []struct {
		name              string
		maxIterations     float64
		expectedCalls     int // MaxValue + 1 because limit triggers when counter > MaxValue
		expectedTerminate gent.TerminationReason
	}{
		{
			name:              "limit at 5 iterations terminates after 6",
			maxIterations:     5,
			expectedCalls:     6,
			expectedTerminate: gent.TerminationLimitExceeded,
		},
		{
			name:              "limit at 1 iteration terminates after 2",
			maxIterations:     1,
			expectedCalls:     2,
			expectedTerminate: gent.TerminationLimitExceeded,
		},
		{
			name:              "limit at 10 iterations terminates after 11",
			maxIterations:     10,
			expectedCalls:     11,
			expectedTerminate: gent.TerminationLimitExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loop := &mockAgentLoop{} // Never auto-terminates
			exec := executor.New[*mockLoopData](loop, executor.DefaultConfig())

			ctx := context.Background()
			data := newMockLoopData()
			execCtx := gent.NewExecutionContext(ctx, "test", data)
			execCtx.SetLimits([]gent.Limit{
				{Type: gent.LimitExactKey, Key: gent.KeyIterations, MaxValue: tt.maxIterations},
			})

			exec.Execute(execCtx)

			result := execCtx.Result()
			assert.NotNil(t, result)
			assert.Equal(t, tt.expectedTerminate, result.TerminationReason)
			assert.NotNil(t, result.ExceededLimit)
			assert.Equal(t, gent.KeyIterations, result.ExceededLimit.Key)
			assert.Equal(t, tt.maxIterations, result.ExceededLimit.MaxValue)
			assert.Equal(t, tt.expectedCalls, loop.GetCalls())
		})
	}
}

func TestLimits_IterationLimit_NotExceeded(t *testing.T) {
	tests := []struct {
		name              string
		maxIterations     float64
		terminateAt       int
		expectedCalls     int
		expectedTerminate gent.TerminationReason
	}{
		{
			name:              "terminate before limit",
			maxIterations:     10,
			terminateAt:       3,
			expectedCalls:     3,
			expectedTerminate: gent.TerminationSuccess,
		},
		{
			name:              "terminate at exactly limit value does not exceed",
			maxIterations:     5,
			terminateAt:       5,
			expectedCalls:     5,
			expectedTerminate: gent.TerminationSuccess,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loop := &mockAgentLoop{terminateAt: tt.terminateAt}
			exec := executor.New[*mockLoopData](loop, executor.DefaultConfig())

			ctx := context.Background()
			data := newMockLoopData()
			execCtx := gent.NewExecutionContext(ctx, "test", data)
			execCtx.SetLimits([]gent.Limit{
				{Type: gent.LimitExactKey, Key: gent.KeyIterations, MaxValue: tt.maxIterations},
			})

			exec.Execute(execCtx)

			result := execCtx.Result()
			assert.NotNil(t, result)
			assert.Equal(t, tt.expectedTerminate, result.TerminationReason)
			assert.Nil(t, result.ExceededLimit)
			assert.Equal(t, tt.expectedCalls, loop.GetCalls())
		})
	}
}

// -----------------------------------------------------------------------------
// Single AgentLoop Tests - Token Limit
// -----------------------------------------------------------------------------

func TestLimits_TokenLimit_Exceeded(t *testing.T) {
	tests := []struct {
		name     string
		input    struct{ inputTokens, outputTokens int }
		limits   struct{ maxInput, maxOutput float64 }
		expected struct {
			calls           int
			iteration       int
			key             string
			reason          gent.TerminationReason
			beforeIteration int
			afterIteration  int
			beforeModelCall int
			afterModelCall  int
		}
	}{
		{
			name:   "input token limit exceeded",
			input:  struct{ inputTokens, outputTokens int }{1000, 100},
			limits: struct{ maxInput, maxOutput float64 }{2500, 10000},
			expected: struct {
				calls           int
				iteration       int
				key             string
				reason          gent.TerminationReason
				beforeIteration int
				afterIteration  int
				beforeModelCall int
				afterModelCall  int
			}{
				calls:           3,
				iteration:       3,
				key:             gent.KeyInputTokens,
				reason:          gent.TerminationLimitExceeded,
				beforeIteration: 3,
				afterIteration:  3,
				beforeModelCall: 3,
				afterModelCall:  3, // All 3 complete; limit exceeded on 3rd AfterModelCall
			},
		},
		{
			name:   "output token limit exceeded",
			input:  struct{ inputTokens, outputTokens int }{100, 500},
			limits: struct{ maxInput, maxOutput float64 }{10000, 1200},
			expected: struct {
				calls           int
				iteration       int
				key             string
				reason          gent.TerminationReason
				beforeIteration int
				afterIteration  int
				beforeModelCall int
				afterModelCall  int
			}{
				calls:           3,
				iteration:       3,
				key:             gent.KeyOutputTokens,
				reason:          gent.TerminationLimitExceeded,
				beforeIteration: 3,
				afterIteration:  3,
				beforeModelCall: 3,
				afterModelCall:  3, // All 3 complete; limit exceeded on 3rd AfterModelCall
			},
		},
		{
			name:   "input tokens check comes first when both exceeded",
			input:  struct{ inputTokens, outputTokens int }{1000, 1000},
			limits: struct{ maxInput, maxOutput float64 }{1500, 1500},
			expected: struct {
				calls           int
				iteration       int
				key             string
				reason          gent.TerminationReason
				beforeIteration int
				afterIteration  int
				beforeModelCall int
				afterModelCall  int
			}{
				calls:           2,
				iteration:       2,
				key:             gent.KeyInputTokens, // first limit in list
				reason:          gent.TerminationLimitExceeded,
				beforeIteration: 2,
				afterIteration:  2,
				beforeModelCall: 2,
				afterModelCall:  2, // Both complete; limit exceeded on 2nd AfterModelCall
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loop := &mockAgentLoop{
				inputTokens:  tt.input.inputTokens,
				outputTokens: tt.input.outputTokens,
			}
			exec := executor.New[*mockLoopData](loop, executor.DefaultConfig())

			ctx := context.Background()
			data := newMockLoopData()
			execCtx := gent.NewExecutionContext(ctx, "test", data)
			execCtx.SetLimits([]gent.Limit{
				{Type: gent.LimitExactKey, Key: gent.KeyInputTokens, MaxValue: tt.limits.maxInput},
				{Type: gent.LimitExactKey, Key: gent.KeyOutputTokens, MaxValue: tt.limits.maxOutput},
				{Type: gent.LimitExactKey, Key: gent.KeyIterations, MaxValue: 100},
			})

			exec.Execute(execCtx)

			// Verify termination
			result := execCtx.Result()
			assert.NotNil(t, result)
			assert.Equal(t, tt.expected.reason, result.TerminationReason)
			assert.NotNil(t, result.ExceededLimit)
			assert.Equal(t, tt.expected.key, result.ExceededLimit.Key)

			// Verify iteration and call counts
			assert.Equal(t, tt.expected.calls, loop.GetCalls())
			assert.Equal(t, tt.expected.iteration, execCtx.Iteration())

			// Verify event counts
			counts := countEvents(execCtx)
			assert.Equal(t, tt.expected.beforeIteration, counts.BeforeIteration)
			assert.Equal(t, tt.expected.afterIteration, counts.AfterIteration)
			assert.Equal(t, tt.expected.beforeModelCall, counts.BeforeModelCall)
			assert.Equal(t, tt.expected.afterModelCall, counts.AfterModelCall)
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
			return &gent.AgentLoopResult{Action: gent.LAContinue}, nil
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

	// Verify iteration and event counts
	// Iterations: 1(a:1000), 2(b:500), 3(a:2000), 4(b:1000), 5(a:3000 > 2500, limit!)
	assert.Equal(t, 5, execCtx.Iteration())
	counts := countEvents(execCtx)
	assert.Equal(t, 5, counts.BeforeIteration)
	assert.Equal(t, 5, counts.AfterIteration)
	assert.Equal(t, 5, counts.BeforeModelCall)
	assert.Equal(t, 5, counts.AfterModelCall) // All 5 complete; limit exceeded on 5th
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

			return &gent.AgentLoopResult{
				Action: gent.LATerminate,
				Result: []gent.ContentPart{llms.TextContent{Text: "done"}},
			}, nil
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

	// Verify parent event counts (1 iteration)
	parentCounts := countEvents(execCtx)
	assert.Equal(t, 1, parentCounts.BeforeIteration)
	assert.Equal(t, 1, parentCounts.AfterIteration)
	assert.Equal(t, 0, parentCounts.BeforeModelCall) // Parent doesn't call models
	assert.Equal(t, 0, parentCounts.AfterModelCall)

	// Verify child event counts (5 children, each with 1 model call)
	allCounts := countEventsWithChildren(execCtx)
	assert.Equal(t, 1, allCounts.BeforeIteration)  // Only parent has iteration events
	assert.Equal(t, 1, allCounts.AfterIteration)
	assert.Equal(t, 5, allCounts.BeforeModelCall)  // 5 children each call model
	assert.Equal(t, 5, allCounts.AfterModelCall)   // All 5 complete (no limit exceeded)
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

			return &gent.AgentLoopResult{Action: gent.LAContinue}, nil
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

	// Verify parent event counts (2 iterations)
	parentCounts := countEvents(execCtx)
	assert.Equal(t, 2, parentCounts.BeforeIteration)
	assert.Equal(t, 2, parentCounts.AfterIteration)

	// Verify child event counts
	// Iter 1: 5 children * (BeforeModelCall + AfterModelCall) = 10 events
	// Iter 2: 5 children start, but limit exceeded after some AfterModelCall events
	// The exact number depends on timing, but:
	// - All 10 BeforeModelCall should fire (5 per iteration)
	// - Iter 1: 5 AfterModelCall (1000 tokens)
	// - Iter 2: At least 3 AfterModelCall needed to exceed 1500 (1000 + 600 > 1500)
	allCounts := countEventsWithChildren(execCtx)
	assert.Equal(t, 2, allCounts.BeforeIteration)
	assert.Equal(t, 2, allCounts.AfterIteration)
	assert.Equal(t, 10, allCounts.BeforeModelCall) // 5 children * 2 iterations
	// AfterModelCall: 5 from iter 1 + some from iter 2 (at least 3 to trigger limit)
	assert.GreaterOrEqual(t, allCounts.AfterModelCall, 8) // At least 5 + 3
	assert.LessOrEqual(t, allCounts.AfterModelCall, 10)   // At most all 10
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

			return &gent.AgentLoopResult{Action: gent.LAContinue}, nil
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

	// Verify parent event counts
	parentCounts := countEvents(execCtx)
	assert.Equal(t, 3, parentCounts.BeforeIteration)
	assert.Equal(t, 3, parentCounts.AfterIteration)

	// Verify child event counts (3 children, each with 1 model call)
	allCounts := countEventsWithChildren(execCtx)
	assert.Equal(t, 3, allCounts.BeforeIteration)
	assert.Equal(t, 3, allCounts.AfterIteration)
	assert.Equal(t, 3, allCounts.BeforeModelCall)
	assert.Equal(t, 3, allCounts.AfterModelCall) // All 3 complete, limit on 3rd
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

			return &gent.AgentLoopResult{Action: gent.LAContinue}, nil
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

	// Verify parent event counts
	parentCounts := countEvents(execCtx)
	assert.Equal(t, 4, parentCounts.BeforeIteration)
	assert.Equal(t, 4, parentCounts.AfterIteration)

	// Verify child event counts
	// 4 iterations * 3 children = 12 total children
	allCounts := countEventsWithChildren(execCtx)
	assert.Equal(t, 4, allCounts.BeforeIteration)
	assert.Equal(t, 4, allCounts.AfterIteration)
	assert.Equal(t, 12, allCounts.BeforeModelCall) // 3 children * 4 iterations
	// AfterModelCall: iter 1-3 complete (9), iter 4 partial (at least 1 to exceed 1000)
	assert.GreaterOrEqual(t, allCounts.AfterModelCall, 10) // At least 9 + 1
	assert.LessOrEqual(t, allCounts.AfterModelCall, 12)    // At most all 12
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

	// Verify event counts
	counts := countEvents(execCtx)
	assert.Equal(t, 1, counts.BeforeIteration)
	assert.Equal(t, 1, counts.AfterIteration)
	assert.Equal(t, 1, counts.BeforeModelCall)
	assert.Equal(t, 1, counts.AfterModelCall) // Limit exceeded on this event
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

	// Verify iteration and event counts
	assert.Equal(t, 2, execCtx.Iteration())
	counts := countEvents(execCtx)
	assert.Equal(t, 2, counts.BeforeIteration)
	assert.Equal(t, 2, counts.AfterIteration)
	assert.Equal(t, 2, counts.BeforeModelCall)
	assert.Equal(t, 2, counts.AfterModelCall) // Both complete, no limit exceeded
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

	// Verify iteration and event counts
	// Both input (1000) and output (1000) tokens exceed their limits (500) on first iteration
	assert.Equal(t, 1, loop.GetCalls())
	assert.Equal(t, 1, execCtx.Iteration())
	counts := countEvents(execCtx)
	assert.Equal(t, 1, counts.BeforeIteration)
	assert.Equal(t, 1, counts.AfterIteration)
	assert.Equal(t, 1, counts.BeforeModelCall)
	assert.Equal(t, 1, counts.AfterModelCall) // Limit exceeded on this event
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
			return &gent.AgentLoopResult{Action: gent.LAContinue}, nil
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
}

func TestLimits_ConsecutiveToolchainParseErrors_Exceeded(t *testing.T) {
	errorCount := 0
	loop := &mockAgentLoop{
		nextFn: func(execCtx *gent.ExecutionContext) (*gent.AgentLoopResult, error) {
			errorCount++
			// Simulate toolchain parse error
			execCtx.PublishParseError(gent.ParseErrorTypeToolchain, "invalid yaml", nil)
			return &gent.AgentLoopResult{Action: gent.LAContinue}, nil
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
				return &gent.AgentLoopResult{
					Action: gent.LATerminate,
					Result: []gent.ContentPart{llms.TextContent{Text: "done"}},
				}, nil
			}
			return &gent.AgentLoopResult{Action: gent.LAContinue}, nil
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loop := &mockAgentLoop{
				nextFn: func(execCtx *gent.ExecutionContext) (*gent.AgentLoopResult, error) {
					execCtx.Stats().IncrGauge(tt.input.gaugeKey, tt.input.gaugeIncrement)
					return &gent.AgentLoopResult{Action: gent.LAContinue}, nil
				},
			}
			exec := executor.New[*mockLoopData](loop, executor.DefaultConfig())

			ctx := context.Background()
			data := newMockLoopData()
			execCtx := gent.NewExecutionContext(ctx, "test", data)
			execCtx.SetLimits([]gent.Limit{
				{Type: gent.LimitExactKey, Key: tt.input.gaugeKey, MaxValue: tt.mocks.maxValue},
				{Type: gent.LimitExactKey, Key: gent.KeyIterations, MaxValue: 100},
			})

			exec.Execute(execCtx)

			result := execCtx.Result()
			assert.NotNil(t, result)
			assert.Equal(t, tt.expected.terminationReason, result.TerminationReason)
			assert.NotNil(t, result.ExceededLimit)
			assert.Equal(t, tt.expected.exceededKey, result.ExceededLimit.Key)
			assert.Equal(t, tt.expected.callCount, loop.GetCalls())

			stats := execCtx.Stats()
			assert.InDelta(t, tt.expected.finalGaugeValue, stats.GetGauge(tt.input.gaugeKey), 0.001)
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callCount := 0
			loop := &mockAgentLoop{
				nextFn: func(execCtx *gent.ExecutionContext) (*gent.AgentLoopResult, error) {
					callCount++
					// Alternate between expensive and cheap resources
					if callCount%2 == 1 {
						execCtx.Stats().IncrGauge(tt.input.gaugePrefix+"expensive", 0.10)
					} else {
						execCtx.Stats().IncrGauge(tt.input.gaugePrefix+"cheap", 0.01)
					}
					return &gent.AgentLoopResult{Action: gent.LAContinue}, nil
				},
			}
			exec := executor.New[*mockLoopData](loop, executor.DefaultConfig())

			ctx := context.Background()
			data := newMockLoopData()
			execCtx := gent.NewExecutionContext(ctx, "test", data)
			execCtx.SetLimits([]gent.Limit{
				{Type: gent.LimitKeyPrefix, Key: tt.input.gaugePrefix, MaxValue: tt.mocks.maxValue},
				{Type: gent.LimitExactKey, Key: gent.KeyIterations, MaxValue: 100},
			})

			exec.Execute(execCtx)

			result := execCtx.Result()
			assert.NotNil(t, result)
			assert.Equal(t, tt.expected.terminationReason, result.TerminationReason)
			assert.Equal(t, tt.expected.exceededKeyPrefix, result.ExceededLimit.Key)

			// expensive: 0.10 per odd iteration -> iterations 1,3,5 = 0.30 > 0.25
			stats := execCtx.Stats()
			assert.InDelta(t, tt.expected.expensiveValue,
				stats.GetGauge(tt.input.gaugePrefix+"expensive"), 0.001)
			assert.InDelta(t, tt.expected.cheapValue,
				stats.GetGauge(tt.input.gaugePrefix+"cheap"), 0.001)
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callCount := 0
			loop := &mockAgentLoop{
				nextFn: func(execCtx *gent.ExecutionContext) (*gent.AgentLoopResult, error) {
					callCount++
					execCtx.Stats().IncrGauge(tt.input.gaugeKey, tt.input.gaugeIncrement)
					if callCount >= tt.input.terminateAt {
						return &gent.AgentLoopResult{
							Action: gent.LATerminate,
							Result: []gent.ContentPart{llms.TextContent{Text: "done"}},
						}, nil
					}
					return &gent.AgentLoopResult{Action: gent.LAContinue}, nil
				},
			}
			exec := executor.New[*mockLoopData](loop, executor.DefaultConfig())

			ctx := context.Background()
			data := newMockLoopData()
			execCtx := gent.NewExecutionContext(ctx, "test", data)
			execCtx.SetLimits([]gent.Limit{
				{Type: gent.LimitExactKey, Key: tt.input.gaugeKey, MaxValue: tt.mocks.maxValue},
				{Type: gent.LimitExactKey, Key: gent.KeyIterations, MaxValue: 100},
			})

			exec.Execute(execCtx)

			result := execCtx.Result()
			assert.NotNil(t, result)
			assert.Equal(t, tt.expected.terminationReason, result.TerminationReason)
			assert.Nil(t, result.ExceededLimit)
			assert.InDelta(t, tt.expected.finalGaugeValue,
				execCtx.Stats().GetGauge(tt.input.gaugeKey), 0.001)
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

			return &gent.AgentLoopResult{
				Action: gent.LATerminate,
				Result: []gent.ContentPart{llms.TextContent{Text: "done"}},
			}, nil
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

	// Verify iteration and event counts
	assert.Equal(t, 1, execCtx.Iteration())
	parentCounts := countEvents(execCtx)
	assert.Equal(t, 1, parentCounts.BeforeIteration)
	assert.Equal(t, 1, parentCounts.AfterIteration)
	assert.Equal(t, 1, parentCounts.BeforeModelCall)  // Parent's model call
	assert.Equal(t, 1, parentCounts.AfterModelCall)

	// Verify total event counts (parent + child)
	allCounts := countEventsWithChildren(execCtx)
	assert.Equal(t, 1, allCounts.BeforeIteration)    // Only parent has iterations
	assert.Equal(t, 1, allCounts.AfterIteration)
	assert.Equal(t, 2, allCounts.BeforeModelCall)    // Parent + child
	assert.Equal(t, 2, allCounts.AfterModelCall)     // Both complete
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

	// Find LimitExceededEvent
	var limitEvent *gent.LimitExceededEvent
	for _, event := range execCtx.Events() {
		if e, ok := event.(*gent.LimitExceededEvent); ok {
			limitEvent = e
			break
		}
	}

	assert.NotNil(t, limitEvent, "LimitExceededEvent should be published")
	assert.Equal(t, gent.KeyIterations, limitEvent.Limit.Key)
	assert.Equal(t, 3.0, limitEvent.Limit.MaxValue)
	assert.Equal(t, 4.0, limitEvent.CurrentValue) // 4 > 3
	assert.Equal(t, gent.KeyIterations, limitEvent.MatchedKey)
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

	// Find LimitExceededEvent
	var limitEvent *gent.LimitExceededEvent
	for _, event := range execCtx.Events() {
		if e, ok := event.(*gent.LimitExceededEvent); ok {
			limitEvent = e
			break
		}
	}

	assert.NotNil(t, limitEvent, "LimitExceededEvent should be published")
	assert.Equal(t, gent.KeyInputTokens, limitEvent.Limit.Key)
	assert.Equal(t, 1000.0, limitEvent.Limit.MaxValue)
	assert.Equal(t, 1500.0, limitEvent.CurrentValue) // 3 iterations * 500 = 1500 > 1000
	assert.Equal(t, gent.KeyInputTokens, limitEvent.MatchedKey)
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
			return &gent.AgentLoopResult{Action: gent.LAContinue}, nil
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

	// Find LimitExceededEvent
	var limitEvent *gent.LimitExceededEvent
	for _, event := range execCtx.Events() {
		if e, ok := event.(*gent.LimitExceededEvent); ok {
			limitEvent = e
			break
		}
	}

	assert.NotNil(t, limitEvent, "LimitExceededEvent should be published")
	assert.Equal(t, gent.KeyInputTokensFor, limitEvent.Limit.Key)
	// The matched key should be the specific model that exceeded
	assert.Equal(t, gent.KeyInputTokensFor+"expensive-model", limitEvent.MatchedKey)
	assert.Equal(t, 3000.0, limitEvent.CurrentValue) // 3 calls * 1000 = 3000 > 2500
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

	// Verify no LimitExceededEvent
	for _, event := range execCtx.Events() {
		_, isLimitExceeded := event.(*gent.LimitExceededEvent)
		assert.False(t, isLimitExceeded, "LimitExceededEvent should not be published on success")
	}
}
