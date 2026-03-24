package react

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/rickchristie/gent"
	agentutil "github.com/rickchristie/gent/agents"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Integration test — agent recovers from repetition via format error path
// ============================================================================

func TestAgent_RepetitionDetection_Recovery(t *testing.T) {
	cfg := agentutil.DefaultRepetitionConfig()
	cfg.BlockSize = 50

	loopBlock := strings.Repeat("I am stuck in a loop and cannot proceed. ", 2)
	if len(loopBlock) > cfg.BlockSize {
		loopBlock = loopBlock[:cfg.BlockSize]
	} else {
		loopBlock += strings.Repeat("x", cfg.BlockSize-len(loopBlock))
	}
	loopingResponse := strings.Repeat(loopBlock, 5)

	validResponse := "<thought>\nLet me try a different approach.\n</thought>\n" +
		"<answer>\nThe issue has been resolved.\n</answer>"

	model := newMockModel(
		&gent.ContentResponse{
			Choices: []*gent.ContentChoice{{Content: loopingResponse}},
			Info:    &gent.GenerationInfo{InputTokens: 100, OutputTokens: 500},
		},
		&gent.ContentResponse{
			Choices: []*gent.ContentChoice{{Content: validResponse}},
			Info:    &gent.GenerationInfo{InputTokens: 100, OutputTokens: 50},
		},
	)

	agent := NewAgent(model).
		WithRepetitionConfig(cfg).
		WithToolChain(&mockToolChain{name: "action", guidance: "test"})

	data := gent.NewBasicLoopData(&gent.Task{Text: "test task"})
	execCtx := gent.NewExecutionContext(context.Background(), "repetition-test", data)

	// First call: model loops -> detected -> truncated -> format error -> LAContinue.
	result, err := agent.Next(execCtx)
	require.NoError(t, err, "agent should not return error on repetition")
	assert.Equal(t, gent.LAContinue, result.Action,
		"agent should continue after repetition detection")

	// Verify loop stats were incremented.
	assert.Equal(t, int64(1),
		execCtx.Stats().GetCounter(gent.SCRepetitionLoopTotal))
	assert.Equal(t, float64(1),
		execCtx.Stats().GetGauge(gent.SGRepetitionLoopConsecutive))

	// Second call: model returns valid answer -> agent terminates.
	// Consecutive gauge should reset.
	result, err = agent.Next(execCtx)
	require.NoError(t, err)
	assert.Equal(t, gent.LATerminate, result.Action,
		"agent should terminate with valid answer")
	assert.Equal(t, float64(0),
		execCtx.Stats().GetGauge(gent.SGRepetitionLoopConsecutive),
		"consecutive gauge should reset on successful termination")
}

// ============================================================================
// Terminate mode test
// ============================================================================

func TestAgent_RepetitionDetection_TerminateImmediately(t *testing.T) {
	cfg := agentutil.DefaultRepetitionConfig()
	cfg.BlockSize = 50
	cfg.Action = agentutil.RepetitionTerminate

	loopBlock := strings.Repeat("x", cfg.BlockSize)
	loopingResponse := strings.Repeat(loopBlock, 5)

	model := newMockModel(&gent.ContentResponse{
		Choices: []*gent.ContentChoice{{Content: loopingResponse}},
		Info:    &gent.GenerationInfo{InputTokens: 100, OutputTokens: 500},
	})

	agent := NewAgent(model).
		WithRepetitionConfig(cfg).
		WithToolChain(&mockToolChain{name: "action", guidance: "test"})

	data := gent.NewBasicLoopData(&gent.Task{Text: "test"})
	execCtx := gent.NewExecutionContext(context.Background(), "term-test", data)

	_, err := agent.Next(execCtx)
	require.Error(t, err, "should return fatal error in terminate mode")
	assert.Contains(t, err.Error(), "repetition detected")
}

// ============================================================================
// Gauge reset test — tool execution resets consecutive gauge
// ============================================================================

func TestAgent_RepetitionGauge_ResetsOnToolExecution(t *testing.T) {
	cfg := agentutil.DefaultRepetitionConfig()
	cfg.BlockSize = 50

	loopBlock := strings.Repeat("x", cfg.BlockSize)
	loopingResponse := strings.Repeat(loopBlock, 5)

	// Call 1: loop detected (gauge -> 1)
	// Call 2: valid tool call (gauge -> 0)
	toolCallResponse := "<thought>\nLet me try.\n</thought>\n" +
		"<action>\ntool_name: test_tool\nargs: {}\n</action>"

	model := newMockModel(
		&gent.ContentResponse{
			Choices: []*gent.ContentChoice{{Content: loopingResponse}},
			Info:    &gent.GenerationInfo{InputTokens: 100, OutputTokens: 500},
		},
		&gent.ContentResponse{
			Choices: []*gent.ContentChoice{{Content: toolCallResponse}},
			Info:    &gent.GenerationInfo{InputTokens: 100, OutputTokens: 50},
		},
	)

	tc := newMockToolChain()
	tc.WithResults(&gent.ToolChainResult{
		Results: []*gent.ToolCallResult{
			{Text: "<observation>tool result</observation>"},
		},
	})

	agent := NewAgent(model).WithRepetitionConfig(cfg).WithToolChain(tc)

	data := gent.NewBasicLoopData(&gent.Task{Text: "test"})
	execCtx := gent.NewExecutionContext(context.Background(), "gauge-test", data)

	// Iteration 1: loop -> gauge=1
	result, err := agent.Next(execCtx)
	require.NoError(t, err)
	assert.Equal(t, gent.LAContinue, result.Action)
	assert.Equal(t, float64(1),
		execCtx.Stats().GetGauge(gent.SGRepetitionLoopConsecutive))

	// Iteration 2: tool call -> gauge reset to 0
	result, err = agent.Next(execCtx)
	require.NoError(t, err)
	assert.Equal(t, gent.LAContinue, result.Action)
	assert.Equal(t, float64(0),
		execCtx.Stats().GetGauge(gent.SGRepetitionLoopConsecutive),
		"gauge should reset after successful tool execution")
}

// ============================================================================
// MaxResponseChars two-step check tests
// ============================================================================

func TestAgent_MaxResponseChars_TooLongButNotLooping(t *testing.T) {
	// A long unique response exceeds MaxResponseChars. Since there's no repetition,
	// the agent should inject a "too long" reminder, not a loop reminder.
	// Generate unique text where every paragraph has completely different vocabulary
	// to avoid SimHash near-duplicate false positives across overlapping windows.
	words := []string{
		"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel",
		"india", "juliet", "kilo", "lima", "mike", "november", "oscar", "papa",
		"quebec", "romeo", "sierra", "tango", "uniform", "victor", "whiskey", "xray",
	}
	var sb strings.Builder
	for i := 0; i < 200; i++ {
		w := words[i%len(words)]
		fmt.Fprintf(&sb, "%s%d_%d_%d_%d ", w, i, i*7, i*13, i*31)
	}
	longResponse := sb.String()

	validResponse := "<thought>\nBe concise.\n</thought>\n<answer>\nDone.\n</answer>"

	model := newMockModel(
		&gent.ContentResponse{
			Choices: []*gent.ContentChoice{{Content: longResponse}},
			Info:    &gent.GenerationInfo{InputTokens: 100, OutputTokens: 3000},
		},
		&gent.ContentResponse{
			Choices: []*gent.ContentChoice{{Content: validResponse}},
			Info:    &gent.GenerationInfo{InputTokens: 100, OutputTokens: 50},
		},
	)

	agent := NewAgent(model).
		WithMaxResponseChars(500). // Very low limit to trigger truncation.
		WithToolChain(&mockToolChain{name: "action", guidance: "test"})

	data := gent.NewBasicLoopData(&gent.Task{Text: "test"})
	execCtx := gent.NewExecutionContext(context.Background(), "too-long-test", data)

	// First call: too long but not looping -> conciseness reminder, LAContinue.
	result, err := agent.Next(execCtx)
	require.NoError(t, err)
	assert.Equal(t, gent.LAContinue, result.Action)

	// Loop stats should NOT be incremented.
	assert.Equal(t, int64(0),
		execCtx.Stats().GetCounter(gent.SCRepetitionLoopTotal),
		"loop counter should not increment for non-looping long response")
	assert.Equal(t, float64(0),
		execCtx.Stats().GetGauge(gent.SGRepetitionLoopConsecutive),
		"loop gauge should not increment for non-looping long response")

	// Second call: concise valid answer -> terminate.
	result, err = agent.Next(execCtx)
	require.NoError(t, err)
	assert.Equal(t, gent.LATerminate, result.Action)
}

func TestAgent_MaxResponseChars_TooLongAndLooping(t *testing.T) {
	// A response exceeds MaxResponseChars AND contains repetition. The two-step check
	// should detect the loop and use the loop recovery path (with loop stats).
	cfg := agentutil.DefaultRepetitionConfig()
	cfg.BlockSize = 50

	loopBlock := strings.Repeat("I am stuck in this loop. ", 3)
	if len(loopBlock) > cfg.BlockSize {
		loopBlock = loopBlock[:cfg.BlockSize]
	} else {
		loopBlock += strings.Repeat(" ", cfg.BlockSize-len(loopBlock))
	}
	// 2 repetitions: below the streaming threshold (3) but above CheckAccumulated's
	// relaxed threshold (2). MaxResponseChars triggers first, then the two-step
	// check finds the repetition.
	loopingResponse := strings.Repeat(loopBlock, 2) + strings.Repeat("padding text ", 10)

	validResponse := "<thought>\nDifferent approach.\n</thought>\n<answer>\nResolved.\n</answer>"

	model := newMockModel(
		&gent.ContentResponse{
			Choices: []*gent.ContentChoice{{Content: loopingResponse}},
			Info:    &gent.GenerationInfo{InputTokens: 100, OutputTokens: 200},
		},
		&gent.ContentResponse{
			Choices: []*gent.ContentChoice{{Content: validResponse}},
			Info:    &gent.GenerationInfo{InputTokens: 100, OutputTokens: 50},
		},
	)

	agent := NewAgent(model).
		WithRepetitionConfig(cfg).
		WithMaxResponseChars(len(loopingResponse) - 10). // Just under total, forces truncation.
		WithToolChain(&mockToolChain{name: "action", guidance: "test"})

	data := gent.NewBasicLoopData(&gent.Task{Text: "test"})
	execCtx := gent.NewExecutionContext(context.Background(), "long-loop-test", data)

	// First call: too long + loop detected -> loop recovery path.
	result, err := agent.Next(execCtx)
	require.NoError(t, err)
	assert.Equal(t, gent.LAContinue, result.Action)

	// Loop stats SHOULD be incremented (it's a genuine loop).
	assert.Equal(t, int64(1),
		execCtx.Stats().GetCounter(gent.SCRepetitionLoopTotal))
	assert.Equal(t, float64(1),
		execCtx.Stats().GetGauge(gent.SGRepetitionLoopConsecutive))
}
