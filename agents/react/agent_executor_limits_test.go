package react

import (
	"context"
	"errors"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/internal/tt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ----------------------------------------------------------------------------
// Helper to run executor with limit test
// Uses mock types from internal/tt package
// ----------------------------------------------------------------------------

func runWithLimit(
	t *testing.T,
	model *tt.MockModel,
	format *tt.MockFormat,
	toolChain *tt.MockToolChain,
	termination *tt.MockTermination,
	limits []gent.Limit,
) *gent.ExecutionContext {
	t.Helper()

	agent := NewAgent(model).
		WithFormat(format).
		WithToolChain(toolChain).
		WithTermination(termination)

	data := gent.NewBasicLoopData(&gent.Task{Text: "Test task"})
	execCtx := gent.NewExecutionContext(context.Background(), "test", data)
	execCtx.SetLimits(limits)

	exec := executor.New[*gent.BasicLoopData](agent, executor.DefaultConfig())
	exec.Execute(execCtx)

	return execCtx
}

func runWithLimitAndThinking(
	t *testing.T,
	model *tt.MockModel,
	format *tt.MockFormat,
	toolChain *tt.MockToolChain,
	termination *tt.MockTermination,
	thinkingSection *tt.MockSection,
	limits []gent.Limit,
) *gent.ExecutionContext {
	t.Helper()

	agent := NewAgent(model).
		WithFormat(format).
		WithToolChain(toolChain).
		WithTermination(termination).
		WithThinkingSection(thinkingSection)

	data := gent.NewBasicLoopData(&gent.Task{Text: "Test task"})
	execCtx := gent.NewExecutionContext(context.Background(), "test", data)
	execCtx.SetLimits(limits)

	exec := executor.New[*gent.BasicLoopData](agent, executor.DefaultConfig())
	exec.Execute(execCtx)

	return execCtx
}

// ----------------------------------------------------------------------------
// Test: Iteration limit
// ----------------------------------------------------------------------------

func TestExecutorLimits_Iterations(t *testing.T) {
	t.Run("self iteration limit ignores child iterations", func(t *testing.T) {
		// Iterations propagate like all counters, but using $self:
		// limit allows per-context control.
		//
		// Setup:
		// - Parent has $self: iteration limit of 4
		// - Parent iteration 1: calls tool that spawns child
		// - Child runs 3 iterations (propagates to parent aggregate)
		// - Parent iteration 2: terminates normally
		// - Parent $self: iterations = 2 (within limit)
		// - Parent aggregated iterations = 5 (would exceed if
		//   limit was on aggregated key)
		// - Parent should succeed because child iterations don't propagate

		// Child executor that runs 3 iterations
		childModel := tt.NewMockModel().WithName("child").
			AddResponse("<action>tool: child_tool</action>", 100, 50).
			AddResponse("<action>tool: child_tool</action>", 100, 50).
			AddResponse("<answer>child done</answer>", 100, 50)

		childFormat := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: child_tool"}}).
			AddParseResult(map[string][]string{"action": {"tool: child_tool"}}).
			AddParseResult(map[string][]string{"answer": {"child done"}})

		childToolChain := tt.NewMockToolChain()
		childTermination := tt.NewMockTermination()

		// Parent model
		parentModel := tt.NewMockModel().WithName("parent").
			AddResponse("<action>tool: spawn_child</action>", 100, 50).
			AddResponse("<answer>parent done</answer>", 100, 50)

		parentFormat := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: spawn_child"}}).
			AddParseResult(map[string][]string{"answer": {"parent done"}})

		// Tool that spawns child agent loop with its own executor
		parentToolChain := tt.NewMockToolChain().
			WithToolCtx("spawn_child",
				func(execCtx *gent.ExecutionContext, args map[string]any) (string, error) {
					childData := gent.NewBasicLoopData(&gent.Task{Text: "do child work"})
					childCtx := execCtx.SpawnChild("child-agent", childData)

					// Create and run child agent
					childAgent := NewAgent(childModel).
						WithFormat(childFormat).
						WithToolChain(childToolChain).
						WithTermination(childTermination)
					childExec := executor.New[*gent.BasicLoopData](childAgent, executor.DefaultConfig())
					childExec.Execute(childCtx)

					return "child completed 3 iterations", nil
				})
		parentTermination := tt.NewMockTermination()

		// Parent uses $self: iteration limit of 4:
		// - Parent runs 2 iterations (OK: 2 <= 4)
		// - Child runs 3 iterations (OK: 3 <= 4)
		// - Iterations propagate, parent aggregated = 2 + 3 = 5
		// - But limit is on $self: key, so only 2 is checked (OK)
		parentLimit := tt.ExactLimit(
			gent.SCIterations.Self(), 4,
		)
		limits := []gent.Limit{parentLimit}

		execCtx := runWithLimit(
			t, parentModel, parentFormat,
			parentToolChain, parentTermination, limits,
		)

		// Parent should succeed because $self: limit only checks
		// the parent's own iterations (2), not aggregated (5)
		assert.Equal(t, gent.TerminationSuccess,
			execCtx.TerminationReason(),
			"parent should succeed with $self: iteration limit")
		assert.Nil(t, execCtx.ExceededLimit())
		assert.Equal(t, 2, execCtx.Iteration())

		// Aggregated iterations = parent(2) + child(3) = 5
		assert.Equal(t, int64(5),
			execCtx.Stats().GetIterations(),
			"parent aggregated iterations should include child")
		// $self: iterations = parent's own only
		assert.Equal(t, int64(2),
			execCtx.Stats().GetCounter(
				gent.SCIterations.Self(),
			),
			"parent $self iterations should be 2")

		// Verify child ran 3 iterations
		require.Len(t, execCtx.Children(), 1)
		childCtx := execCtx.Children()[0]
		assert.Equal(t, int64(3), childCtx.Stats().GetIterations(),
			"child stats should show child iterations")

		// Verify other stats DID propagate (tokens)
		// Parent: 2 calls × 100 = 200, Child: 3 calls × 100 = 300, Total = 500
		assert.Equal(t, int64(500), execCtx.Stats().GetCounter(gent.SCInputTokens),
			"input tokens should propagate from child to parent")

		// Build expected NextPrompt for parent tool execution
		toolObs := tt.ToolObservation(parentFormat, parentToolChain,
			"spawn_child", "child completed 3 iterations")

		// Assert parent event sequence - successful termination
		expectedParentEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: spawn child tool
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "parent"),
			tt.AfterModelCall(0, 1, "parent", 100, 50),
			tt.BeforeToolCall(0, 1, "spawn_child", nil),
			tt.AfterToolCall(0, 1, "spawn_child", nil, "child completed 3 iterations", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolObs)),
			// Iteration 2: answer
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "parent"),
			tt.AfterModelCall(0, 2, "parent", 100, 50),
			tt.AfterIter(0, 2, tt.Terminate("parent done")),
			tt.AfterExec(0, 2, gent.TerminationSuccess),
		}
		tt.AssertEventsEqual(t, expectedParentEvents, tt.CollectLifecycleEvents(execCtx))

		// Build expected child tool observation
		childToolObs := tt.ToolObservation(childFormat, childToolChain, "child_tool", "tool executed")

		// Assert child event sequence
		expectedChildEvents := []gent.Event{
			tt.BeforeExec(1, 0),
			// Child iteration 1: tool call
			tt.BeforeIter(1, 1),
			tt.BeforeModelCall(1, 1, "child"),
			tt.AfterModelCall(1, 1, "child", 100, 50),
			tt.BeforeToolCall(1, 1, "child_tool", nil),
			tt.AfterToolCall(1, 1, "child_tool", nil, "tool executed", nil),
			tt.AfterIter(1, 1, tt.ContinueWithPrompt(childToolObs)),
			// Child iteration 2: tool call
			tt.BeforeIter(1, 2),
			tt.BeforeModelCall(1, 2, "child"),
			tt.AfterModelCall(1, 2, "child", 100, 50),
			tt.BeforeToolCall(1, 2, "child_tool", nil),
			tt.AfterToolCall(1, 2, "child_tool", nil, "tool executed", nil),
			tt.AfterIter(1, 2, tt.ContinueWithPrompt(childToolObs)),
			// Child iteration 3: answer
			tt.BeforeIter(1, 3),
			tt.BeforeModelCall(1, 3, "child"),
			tt.AfterModelCall(1, 3, "child", 100, 50),
			tt.AfterIter(1, 3, tt.Terminate("child done")),
			tt.AfterExec(1, 3, gent.TerminationSuccess),
		}
		tt.AssertEventsEqual(t, expectedChildEvents, tt.CollectLifecycleEvents(childCtx))
	})

	t.Run("stops when iteration limit exceeded at first iteration", func(t *testing.T) {
		// Limit of 0 means iteration 1 (value 1 > 0) immediately exceeds
		model := tt.NewMockModel().
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain()
		termination := tt.NewMockTermination()

		limit := tt.ExactLimit(gent.SCIterations, 0)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())
		assert.Equal(t, 1, execCtx.Iteration())

		// Build expected NextPrompt for tool execution
		toolObs := tt.ToolObservation(format, toolChain, "test", "tool executed")

		// Assert full event sequence - limit exceeded at BeforeIter for iteration 1
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			tt.BeforeIter(0, 1),
			tt.LimitExceeded(0, 1, limit, 1, gent.SCIterations),
			// Agent continues despite cancelled context (mock doesn't check)
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.BeforeToolCall(0, 1, "test", nil),
			tt.AfterToolCall(0, 1, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolObs)),
			tt.AfterExec(0, 1, gent.TerminationLimitExceeded),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})

	t.Run("stops when iteration limit exceeded at Nth iteration", func(t *testing.T) {
		model := tt.NewMockModel().
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain()
		termination := tt.NewMockTermination()

		limit := tt.ExactLimit(gent.SCIterations, 2)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())
		assert.Equal(t, 3, execCtx.Iteration()) // Attempted 3rd but stopped

		// Build expected NextPrompt for tool execution
		toolObs := tt.ToolObservation(format, toolChain, "test", "tool executed")

		// Assert full event sequence
		expectedEvents := []gent.Event{
			// Execution starts
			tt.BeforeExec(0, 0),
			// Iteration 1: tool call succeeds
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.BeforeToolCall(0, 1, "test", nil),
			tt.AfterToolCall(0, 1, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolObs)),
			// Iteration 2: tool call succeeds
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.BeforeToolCall(0, 2, "test", nil),
			tt.AfterToolCall(0, 2, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(toolObs)),
			// Iteration 3: limit exceeded during BeforeIter stats update
			tt.BeforeIter(0, 3),
			tt.LimitExceeded(0, 3, limit, 3, gent.SCIterations),
			// Agent continues despite cancelled context (mock doesn't check)
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.BeforeToolCall(0, 3, "test", nil),
			tt.AfterToolCall(0, 3, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(toolObs)),
			// Execution ends with limit exceeded
			tt.AfterExec(0, 3, gent.TerminationLimitExceeded),
		}
		actualEvents := tt.CollectLifecycleEvents(execCtx)
		tt.AssertEventsEqual(t, expectedEvents, actualEvents)
	})
}

// ----------------------------------------------------------------------------
// Test: Token limits
// ----------------------------------------------------------------------------

func TestExecutorLimits_InputTokens(t *testing.T) {
	t.Run("stops when input token limit exceeded at first iteration", func(t *testing.T) {
		// First model call uses 600 tokens which exceeds limit of 500
		model := tt.NewMockModel().
			AddResponse("<action>tool: test</action>", 600, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain()
		termination := tt.NewMockTermination()

		limit := tt.ExactLimit(gent.SCInputTokens, 500)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())
		assert.Equal(t, 1, execCtx.Iteration())

		// Build expected NextPrompt for tool execution
		toolObs := tt.ToolObservation(format, toolChain, "test", "tool executed")

		// Assert full event sequence - limit exceeded at AfterModelCall for iteration 1
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 600, 50),
			tt.LimitExceeded(0, 1, limit, 600, gent.SCInputTokens),
			// Agent continues despite cancelled context
			tt.BeforeToolCall(0, 1, "test", nil),
			tt.AfterToolCall(0, 1, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolObs)),
			tt.AfterExec(0, 1, gent.TerminationLimitExceeded),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})

	t.Run("stops when input token limit exceeded at Nth iteration", func(t *testing.T) {
		model := tt.NewMockModel().
			AddResponse("<action>tool: test</action>", 500, 50).
			AddResponse("<action>tool: test</action>", 600, 50). // Total: 1100
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain()
		termination := tt.NewMockTermination()

		limit := tt.ExactLimit(gent.SCInputTokens, 1000)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())

		// Build expected NextPrompt for tool execution
		toolObs := tt.ToolObservation(format, toolChain, "test", "tool executed")

		// Assert full event sequence
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: 500 input tokens (total: 500, <= 1000 OK)
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 500, 50),
			tt.BeforeToolCall(0, 1, "test", nil),
			tt.AfterToolCall(0, 1, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolObs)),
			// Iteration 2: 600 input tokens (total: 1100 > 1000 EXCEEDED at AfterModelCall)
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 600, 50),
			tt.LimitExceeded(0, 2, limit, 1100, gent.SCInputTokens),
			// Agent continues despite cancelled context
			tt.BeforeToolCall(0, 2, "test", nil),
			tt.AfterToolCall(0, 2, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(toolObs)),
			tt.AfterExec(0, 2, gent.TerminationLimitExceeded),
		}
		actualEvents := tt.CollectLifecycleEvents(execCtx)
		tt.AssertEventsEqual(t, expectedEvents, actualEvents)
	})

	t.Run("stops when model-specific input token limit exceeded (prefix)", func(t *testing.T) {
		// Test with two models: alpha (main agent) and beta (called by tool).
		// Limit is set on gent:input_tokens:beta (via prefix gent:input_tokens:)
		// Alpha uses 600 tokens (under limit for beta), beta uses 600 (exceeds 500 limit)

		// Model beta is called by the tool and exceeds the limit
		modelBeta := tt.NewMockModel().WithName("beta").
			AddResponse("child response", 600, 50) // Exceeds 500 limit

		// Model alpha is the main agent model
		modelAlpha := tt.NewMockModel().WithName("alpha").
			AddResponse("<action>tool: call_beta</action>", 600, 50). // Over limit, but not triggered
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: call_beta"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain().
			WithToolCtx("call_beta", func(execCtx *gent.ExecutionContext, args map[string]any) (string, error) {
				// Create child execution context and call model beta
				childCtx := execCtx.SpawnChild("beta-call", nil)
				resp, err := modelBeta.GenerateContent(childCtx, "beta", "", nil)
				if err != nil {
					return "", err
				}
				return resp.Choices[0].Content, nil
			})
		termination := tt.NewMockTermination()

		// Limit on model-specific input tokens - only beta should trigger this
		limit := tt.PrefixLimit(gent.SCInputTokensFor+"beta", 500)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, modelAlpha, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())

		// Build expected NextPrompt for tool execution
		toolObs := tt.ToolObservation(format, toolChain, "call_beta", "child response")

		// Assert parent context events
		expectedParentEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: alpha model call, then tool calls beta which exceeds limit
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "alpha"),
			tt.AfterModelCall(0, 1, "alpha", 600, 50),
			tt.BeforeToolCall(0, 1, "call_beta", nil),
			// Limit exceeded propagates from child to parent during tool execution
			tt.LimitExceeded(0, 1, limit, 600, gent.SCInputTokensFor+"beta"),
			tt.AfterToolCall(0, 1, "call_beta", nil, "child response", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolObs)),
			tt.AfterExec(0, 1, gent.TerminationLimitExceeded),
		}
		actualParentEvents := tt.CollectLifecycleEvents(execCtx)
		tt.AssertEventsEqual(t, expectedParentEvents, actualParentEvents)

		// Assert child context events (beta-call)
		require.Len(t, execCtx.Children(), 1)
		childCtx := execCtx.Children()[0]
		expectedChildEvents := []gent.Event{
			tt.BeforeModelCall(1, 0, "beta"),
			tt.AfterModelCall(1, 0, "beta", 600, 50),
			tt.LimitExceeded(1, 0, limit, 600, gent.SCInputTokensFor+"beta"),
		}
		actualChildEvents := tt.CollectLifecycleEvents(childCtx)
		tt.AssertEventsEqual(t, expectedChildEvents, actualChildEvents)
	})

	t.Run("exceeds limit at third iteration", func(t *testing.T) {
		// Iteration 1: 300 tokens (total: 300)
		// Iteration 2: 300 tokens (total: 600)
		// Iteration 3: 500 tokens (total: 1100 > 1000 limit)
		model := tt.NewMockModel().
			AddResponse("<action>tool: test</action>", 300, 50).
			AddResponse("<action>tool: test</action>", 300, 50).
			AddResponse("<action>tool: test</action>", 500, 50). // Exceeds at iteration 3
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain()
		termination := tt.NewMockTermination()

		limit := tt.ExactLimit(gent.SCInputTokens, 1000)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())
		assert.Equal(t, 3, execCtx.Iteration())

		// Build expected NextPrompt for tool execution
		toolObs := tt.ToolObservation(format, toolChain, "test", "tool executed")

		// Assert full event sequence
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: 300 tokens (total: 300 <= 1000)
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 300, 50),
			tt.BeforeToolCall(0, 1, "test", nil),
			tt.AfterToolCall(0, 1, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolObs)),
			// Iteration 2: 300 tokens (total: 600 <= 1000)
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 300, 50),
			tt.BeforeToolCall(0, 2, "test", nil),
			tt.AfterToolCall(0, 2, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(toolObs)),
			// Iteration 3: 500 tokens (total: 1100 > 1000 EXCEEDED at AfterModelCall)
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 500, 50),
			tt.LimitExceeded(0, 3, limit, 1100, gent.SCInputTokens),
			tt.BeforeToolCall(0, 3, "test", nil),
			tt.AfterToolCall(0, 3, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(toolObs)),
			tt.AfterExec(0, 3, gent.TerminationLimitExceeded),
		}
		actualEvents := tt.CollectLifecycleEvents(execCtx)
		tt.AssertEventsEqual(t, expectedEvents, actualEvents)
	})

	t.Run("prefix limit exceeds at third iteration", func(t *testing.T) {
		// Model beta is called multiple times via tool, accumulating tokens
		modelBeta := tt.NewMockModel().WithName("beta").
			AddResponse("response1", 300, 50). // Call 1: 300 (total: 300)
			AddResponse("response2", 300, 50). // Call 2: 300 (total: 600)
			AddResponse("response3", 500, 50)  // Call 3: 500 (total: 1100 > 1000)

		modelAlpha := tt.NewMockModel().WithName("alpha").
			AddResponse("<action>tool: call_beta</action>", 100, 50).
			AddResponse("<action>tool: call_beta</action>", 100, 50).
			AddResponse("<action>tool: call_beta</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: call_beta"}}).
			AddParseResult(map[string][]string{"action": {"tool: call_beta"}}).
			AddParseResult(map[string][]string{"action": {"tool: call_beta"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain().
			WithToolCtx("call_beta", func(execCtx *gent.ExecutionContext, args map[string]any) (string, error) {
				childCtx := execCtx.SpawnChild("beta-call", nil)
				resp, err := modelBeta.GenerateContent(childCtx, "beta", "", nil)
				if err != nil {
					return "", err
				}
				return resp.Choices[0].Content, nil
			})
		termination := tt.NewMockTermination()

		limit := tt.PrefixLimit(gent.SCInputTokensFor+"beta", 1000)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, modelAlpha, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())
		assert.Equal(t, 3, execCtx.Iteration())

		// Build expected NextPrompt for tool executions
		toolObs1 := tt.ToolObservation(format, toolChain, "call_beta", "response1")
		toolObs2 := tt.ToolObservation(format, toolChain, "call_beta", "response2")
		toolObs3 := tt.ToolObservation(format, toolChain, "call_beta", "response3")

		// Assert parent context events
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: alpha call, then tool calls beta (child) -> 300 tokens (total: 300)
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "alpha"),
			tt.AfterModelCall(0, 1, "alpha", 100, 50),
			tt.BeforeToolCall(0, 1, "call_beta", nil),
			tt.AfterToolCall(0, 1, "call_beta", nil, "response1", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolObs1)),
			// Iteration 2: alpha call, then tool calls beta (child) -> 300 tokens (total: 600)
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "alpha"),
			tt.AfterModelCall(0, 2, "alpha", 100, 50),
			tt.BeforeToolCall(0, 2, "call_beta", nil),
			tt.AfterToolCall(0, 2, "call_beta", nil, "response2", nil),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(toolObs2)),
			// Iteration 3: alpha call, then tool calls beta (child) -> 500 tokens (total: 1100 > 1000)
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "alpha"),
			tt.AfterModelCall(0, 3, "alpha", 100, 50),
			tt.BeforeToolCall(0, 3, "call_beta", nil),
			// Limit exceeded propagates from child to parent
			tt.LimitExceeded(0, 3, limit, 1100, gent.SCInputTokensFor+"beta"),
			tt.AfterToolCall(0, 3, "call_beta", nil, "response3", nil),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(toolObs3)),
			tt.AfterExec(0, 3, gent.TerminationLimitExceeded),
		}
		actualEvents := tt.CollectLifecycleEvents(execCtx)
		tt.AssertEventsEqual(t, expectedEvents, actualEvents)

		// Assert we have 3 child contexts (one per tool call)
		require.Len(t, execCtx.Children(), 3)

		// Child 1: beta call with 300 tokens (no limit exceeded)
		child1Events := tt.CollectLifecycleEvents(execCtx.Children()[0])
		expectedChild1 := []gent.Event{
			tt.BeforeModelCall(1, 0, "beta"),
			tt.AfterModelCall(1, 0, "beta", 300, 50),
		}
		tt.AssertEventsEqual(t, expectedChild1, child1Events)

		// Child 2: beta call with 300 tokens (no limit exceeded)
		child2Events := tt.CollectLifecycleEvents(execCtx.Children()[1])
		expectedChild2 := []gent.Event{
			tt.BeforeModelCall(1, 0, "beta"),
			tt.AfterModelCall(1, 0, "beta", 300, 50),
		}
		tt.AssertEventsEqual(t, expectedChild2, child2Events)

		// Child 3: beta call with 500 tokens
		// Note: LimitExceeded is NOT on child - child's local stats are only 500 <= 1000
		// The aggregated total of 1100 is only in the parent's stats
		child3Events := tt.CollectLifecycleEvents(execCtx.Children()[2])
		expectedChild3 := []gent.Event{
			tt.BeforeModelCall(1, 0, "beta"),
			tt.AfterModelCall(1, 0, "beta", 500, 50),
		}
		tt.AssertEventsEqual(t, expectedChild3, child3Events)
	})

	t.Run("child context tokens propagate to global input token limit", func(t *testing.T) {
		// Test that child context model calls contribute to the global input token limit.
		// Parent model uses 200 tokens, child model uses 900 tokens.
		// Global limit is 1000, so total 1100 exceeds it.

		// Child model called by tool
		childModel := tt.NewMockModel().WithName("child").
			AddResponse("child response", 900, 50) // Large enough to trigger limit

		// Parent model
		parentModel := tt.NewMockModel().WithName("parent").
			AddResponse("<action>tool: call_child</action>", 200, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: call_child"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain().
			WithToolCtx("call_child",
				func(execCtx *gent.ExecutionContext, args map[string]any) (string, error) {
					childCtx := execCtx.SpawnChild("child-call", nil)
					resp, err := childModel.GenerateContent(childCtx, "child", "", nil)
					if err != nil {
						return "", err
					}
					return resp.Choices[0].Content, nil
				})
		termination := tt.NewMockTermination()

		// Global input token limit (not prefix) - includes all models
		limit := tt.ExactLimit(gent.SCInputTokens, 1000)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, parentModel, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())

		// Verify global input tokens include both parent and child
		// Parent: 200, Child: 900 = 1100 total
		assert.Equal(t, int64(1100), execCtx.Stats().GetCounter(gent.SCInputTokens))

		// Build expected NextPrompt for tool execution
		toolObs := tt.ToolObservation(format, toolChain, "call_child", "child response")

		// Assert parent context events
		expectedParentEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "parent"),
			tt.AfterModelCall(0, 1, "parent", 200, 50),
			tt.BeforeToolCall(0, 1, "call_child", nil),
			// Child's 900 tokens + parent's 200 = 1100 > 1000 limit
			tt.LimitExceeded(0, 1, limit, 1100, gent.SCInputTokens),
			tt.AfterToolCall(0, 1, "call_child", nil, "child response", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolObs)),
			tt.AfterExec(0, 1, gent.TerminationLimitExceeded),
		}
		tt.AssertEventsEqual(t, expectedParentEvents, tt.CollectLifecycleEvents(execCtx))

		// Assert child context events
		// Note: Child's local stats (900) don't exceed limit (1000)
		// The aggregated total (1100) only exceeds at parent level
		// So child does NOT get LimitExceeded
		require.Len(t, execCtx.Children(), 1)
		childCtx := execCtx.Children()[0]
		expectedChildEvents := []gent.Event{
			tt.BeforeModelCall(1, 0, "child"),
			tt.AfterModelCall(1, 0, "child", 900, 50),
		}
		tt.AssertEventsEqual(t, expectedChildEvents, tt.CollectLifecycleEvents(childCtx))
	})
}

func TestExecutorLimits_OutputTokens(t *testing.T) {
	t.Run("stops when output token limit exceeded at first iteration", func(t *testing.T) {
		// First model call uses 600 output tokens which exceeds limit of 500
		model := tt.NewMockModel().
			AddResponse("<action>tool: test</action>", 100, 600).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain()
		termination := tt.NewMockTermination()

		limit := tt.ExactLimit(gent.SCOutputTokens, 500)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())
		assert.Equal(t, 1, execCtx.Iteration())

		// Build expected NextPrompt for tool execution
		toolObs := tt.ToolObservation(format, toolChain, "test", "tool executed")

		// Assert full event sequence - limit exceeded at AfterModelCall for iteration 1
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 600),
			tt.LimitExceeded(0, 1, limit, 600, gent.SCOutputTokens),
			// Agent continues despite cancelled context
			tt.BeforeToolCall(0, 1, "test", nil),
			tt.AfterToolCall(0, 1, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolObs)),
			tt.AfterExec(0, 1, gent.TerminationLimitExceeded),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})

	t.Run("stops when output token limit exceeded at Nth iteration", func(t *testing.T) {
		model := tt.NewMockModel().
			AddResponse("<action>tool: test</action>", 100, 500).
			AddResponse("<action>tool: test</action>", 100, 600). // Total: 1100
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain()
		termination := tt.NewMockTermination()

		limit := tt.ExactLimit(gent.SCOutputTokens, 1000)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())

		// Build expected NextPrompt for tool execution
		toolObs := tt.ToolObservation(format, toolChain, "test", "tool executed")

		// Assert full event sequence
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: 500 output tokens (total: 500 <= 1000)
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 500),
			tt.BeforeToolCall(0, 1, "test", nil),
			tt.AfterToolCall(0, 1, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolObs)),
			// Iteration 2: 600 output tokens (total: 1100 > 1000 EXCEEDED)
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 600),
			tt.LimitExceeded(0, 2, limit, 1100, gent.SCOutputTokens),
			tt.BeforeToolCall(0, 2, "test", nil),
			tt.AfterToolCall(0, 2, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(toolObs)),
			tt.AfterExec(0, 2, gent.TerminationLimitExceeded),
		}
		actualEvents := tt.CollectLifecycleEvents(execCtx)
		tt.AssertEventsEqual(t, expectedEvents, actualEvents)
	})

	t.Run("stops when model-specific output token limit exceeded (prefix)", func(t *testing.T) {
		// Test with two models: alpha (main agent) and beta (called by tool).
		// Limit is set on gent:output_tokens:beta
		// Alpha uses 50 output tokens (under limit), beta uses 600 (exceeds 500 limit)

		// Model beta is called by the tool and exceeds the limit
		modelBeta := tt.NewMockModel().WithName("beta").
			AddResponse("child response", 50, 600) // Output exceeds 500 limit

		// Model alpha is the main agent model
		modelAlpha := tt.NewMockModel().WithName("alpha").
			AddResponse("<action>tool: call_beta</action>", 100, 50). // Under limit
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: call_beta"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain().
			WithToolCtx("call_beta", func(execCtx *gent.ExecutionContext, args map[string]any) (string, error) {
				// Create child execution context and call model beta
				childCtx := execCtx.SpawnChild("beta-call", nil)
				resp, err := modelBeta.GenerateContent(childCtx, "beta", "", nil)
				if err != nil {
					return "", err
				}
				return resp.Choices[0].Content, nil
			})
		termination := tt.NewMockTermination()

		// Limit on model-specific output tokens - only beta should trigger this
		limit := tt.PrefixLimit(gent.SCOutputTokensFor+"beta", 500)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, modelAlpha, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())

		// Build expected NextPrompt for tool execution
		toolObs := tt.ToolObservation(format, toolChain, "call_beta", "child response")

		// Assert parent context events
		expectedParentEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "alpha"),
			tt.AfterModelCall(0, 1, "alpha", 100, 50),
			tt.BeforeToolCall(0, 1, "call_beta", nil),
			tt.LimitExceeded(0, 1, limit, 600, gent.SCOutputTokensFor+"beta"),
			tt.AfterToolCall(0, 1, "call_beta", nil, "child response", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolObs)),
			tt.AfterExec(0, 1, gent.TerminationLimitExceeded),
		}
		actualParentEvents := tt.CollectLifecycleEvents(execCtx)
		tt.AssertEventsEqual(t, expectedParentEvents, actualParentEvents)

		// Assert child context events
		require.Len(t, execCtx.Children(), 1)
		childCtx := execCtx.Children()[0]
		expectedChildEvents := []gent.Event{
			tt.BeforeModelCall(1, 0, "beta"),
			tt.AfterModelCall(1, 0, "beta", 50, 600),
			tt.LimitExceeded(1, 0, limit, 600, gent.SCOutputTokensFor+"beta"),
		}
		actualChildEvents := tt.CollectLifecycleEvents(childCtx)
		tt.AssertEventsEqual(t, expectedChildEvents, actualChildEvents)
	})

	t.Run("exceeds limit at third iteration", func(t *testing.T) {
		// Iteration 1: 300 output tokens (total: 300)
		// Iteration 2: 300 output tokens (total: 600)
		// Iteration 3: 500 output tokens (total: 1100 > 1000 limit)
		model := tt.NewMockModel().
			AddResponse("<action>tool: test</action>", 50, 300).
			AddResponse("<action>tool: test</action>", 50, 300).
			AddResponse("<action>tool: test</action>", 50, 500). // Exceeds at iteration 3
			AddResponse("<answer>done</answer>", 50, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain()
		termination := tt.NewMockTermination()

		limit := tt.ExactLimit(gent.SCOutputTokens, 1000)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())
		assert.Equal(t, 3, execCtx.Iteration())

		// Build expected NextPrompt for tool execution
		toolObs := tt.ToolObservation(format, toolChain, "test", "tool executed")

		// Assert full event sequence
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: 300 output tokens (total: 300)
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 50, 300),
			tt.BeforeToolCall(0, 1, "test", nil),
			tt.AfterToolCall(0, 1, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolObs)),
			// Iteration 2: 300 output tokens (total: 600)
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 50, 300),
			tt.BeforeToolCall(0, 2, "test", nil),
			tt.AfterToolCall(0, 2, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(toolObs)),
			// Iteration 3: 500 output tokens (total: 1100 > 1000 EXCEEDED)
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 50, 500),
			tt.LimitExceeded(0, 3, limit, 1100, gent.SCOutputTokens),
			tt.BeforeToolCall(0, 3, "test", nil),
			tt.AfterToolCall(0, 3, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(toolObs)),
			tt.AfterExec(0, 3, gent.TerminationLimitExceeded),
		}
		actualEvents := tt.CollectLifecycleEvents(execCtx)
		tt.AssertEventsEqual(t, expectedEvents, actualEvents)
	})

	t.Run("prefix limit exceeds at third iteration", func(t *testing.T) {
		// Model beta called multiple times, accumulating output tokens
		modelBeta := tt.NewMockModel().WithName("beta").
			AddResponse("response1", 50, 300). // Call 1: 300 (total: 300)
			AddResponse("response2", 50, 300). // Call 2: 300 (total: 600)
			AddResponse("response3", 50, 500)  // Call 3: 500 (total: 1100 > 1000)

		modelAlpha := tt.NewMockModel().WithName("alpha").
			AddResponse("<action>tool: call_beta</action>", 100, 50).
			AddResponse("<action>tool: call_beta</action>", 100, 50).
			AddResponse("<action>tool: call_beta</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: call_beta"}}).
			AddParseResult(map[string][]string{"action": {"tool: call_beta"}}).
			AddParseResult(map[string][]string{"action": {"tool: call_beta"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain().
			WithToolCtx("call_beta", func(execCtx *gent.ExecutionContext, args map[string]any) (string, error) {
				childCtx := execCtx.SpawnChild("beta-call", nil)
				resp, err := modelBeta.GenerateContent(childCtx, "beta", "", nil)
				if err != nil {
					return "", err
				}
				return resp.Choices[0].Content, nil
			})
		termination := tt.NewMockTermination()

		limit := tt.PrefixLimit(gent.SCOutputTokensFor+"beta", 1000)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, modelAlpha, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())
		assert.Equal(t, 3, execCtx.Iteration())

		// Build expected NextPrompts for tool executions
		toolObs1 := tt.ToolObservation(format, toolChain, "call_beta", "response1")
		toolObs2 := tt.ToolObservation(format, toolChain, "call_beta", "response2")
		toolObs3 := tt.ToolObservation(format, toolChain, "call_beta", "response3")

		// Assert parent context events
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: beta 300 output tokens (total: 300)
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "alpha"),
			tt.AfterModelCall(0, 1, "alpha", 100, 50),
			tt.BeforeToolCall(0, 1, "call_beta", nil),
			tt.AfterToolCall(0, 1, "call_beta", nil, "response1", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolObs1)),
			// Iteration 2: beta 300 output tokens (total: 600)
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "alpha"),
			tt.AfterModelCall(0, 2, "alpha", 100, 50),
			tt.BeforeToolCall(0, 2, "call_beta", nil),
			tt.AfterToolCall(0, 2, "call_beta", nil, "response2", nil),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(toolObs2)),
			// Iteration 3: beta 500 output tokens (total: 1100 > 1000 EXCEEDED)
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "alpha"),
			tt.AfterModelCall(0, 3, "alpha", 100, 50),
			tt.BeforeToolCall(0, 3, "call_beta", nil),
			tt.LimitExceeded(0, 3, limit, 1100, gent.SCOutputTokensFor+"beta"),
			tt.AfterToolCall(0, 3, "call_beta", nil, "response3", nil),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(toolObs3)),
			tt.AfterExec(0, 3, gent.TerminationLimitExceeded),
		}
		actualEvents := tt.CollectLifecycleEvents(execCtx)
		tt.AssertEventsEqual(t, expectedEvents, actualEvents)

		// Assert 3 child contexts
		require.Len(t, execCtx.Children(), 3)

		// Child 1-2: no limit exceeded (local stats under limit)
		for i := 0; i < 2; i++ {
			childEvents := tt.CollectLifecycleEvents(execCtx.Children()[i])
			assert.Len(t, childEvents, 2, "child %d should have 2 events", i)
		}

		// Child 3: no limit exceeded (local stats 500 <= 1000, only parent aggregated exceeds)
		child3Events := tt.CollectLifecycleEvents(execCtx.Children()[2])
		expectedChild3 := []gent.Event{
			tt.BeforeModelCall(1, 0, "beta"),
			tt.AfterModelCall(1, 0, "beta", 50, 500),
		}
		tt.AssertEventsEqual(t, expectedChild3, child3Events)
	})

	t.Run("child context tokens propagate to global output token limit", func(t *testing.T) {
		// Test that child context model calls contribute to the global output token limit.
		// Parent model uses 200 output tokens, child model uses 900 output tokens.
		// Global limit is 1000, so total 1100 exceeds it.

		// Child model called by tool
		childModel := tt.NewMockModel().WithName("child").
			AddResponse("child response", 50, 900) // Large output to trigger limit

		// Parent model
		parentModel := tt.NewMockModel().WithName("parent").
			AddResponse("<action>tool: call_child</action>", 50, 200).
			AddResponse("<answer>done</answer>", 50, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: call_child"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain().
			WithToolCtx("call_child",
				func(execCtx *gent.ExecutionContext, args map[string]any) (string, error) {
					childCtx := execCtx.SpawnChild("child-call", nil)
					resp, err := childModel.GenerateContent(childCtx, "child", "", nil)
					if err != nil {
						return "", err
					}
					return resp.Choices[0].Content, nil
				})
		termination := tt.NewMockTermination()

		// Global output token limit (not prefix) - includes all models
		limit := tt.ExactLimit(gent.SCOutputTokens, 1000)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, parentModel, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())

		// Verify global output tokens include both parent and child
		// Parent: 200, Child: 900 = 1100 total
		assert.Equal(t, int64(1100), execCtx.Stats().GetCounter(gent.SCOutputTokens))

		// Build expected NextPrompt for tool execution
		toolObs := tt.ToolObservation(format, toolChain, "call_child", "child response")

		// Assert parent context events
		expectedParentEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "parent"),
			tt.AfterModelCall(0, 1, "parent", 50, 200),
			tt.BeforeToolCall(0, 1, "call_child", nil),
			// Child's 900 output tokens + parent's 200 = 1100 > 1000 limit
			tt.LimitExceeded(0, 1, limit, 1100, gent.SCOutputTokens),
			tt.AfterToolCall(0, 1, "call_child", nil, "child response", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolObs)),
			tt.AfterExec(0, 1, gent.TerminationLimitExceeded),
		}
		tt.AssertEventsEqual(t, expectedParentEvents, tt.CollectLifecycleEvents(execCtx))

		// Assert child context events
		// Child's local stats (900) don't exceed limit (1000)
		require.Len(t, execCtx.Children(), 1)
		childCtx := execCtx.Children()[0]
		expectedChildEvents := []gent.Event{
			tt.BeforeModelCall(1, 0, "child"),
			tt.AfterModelCall(1, 0, "child", 50, 900),
		}
		tt.AssertEventsEqual(t, expectedChildEvents, tt.CollectLifecycleEvents(childCtx))
	})
}

// ----------------------------------------------------------------------------
// Test: Tool call limits
// ----------------------------------------------------------------------------

func TestExecutorLimits_ToolCalls(t *testing.T) {
	t.Run("stops when tool call limit exceeded", func(t *testing.T) {
		model := tt.NewMockModel().
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain()
		termination := tt.NewMockTermination()

		limit := tt.ExactLimit(gent.SCToolCalls, 2)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())

		// Build expected NextPrompt for tool execution
		toolObs := tt.ToolObservation(format, toolChain, "test", "tool executed")

		// Assert full event sequence
		// Tool calls limit is checked during BeforeToolCall stats update
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: tool call 1 (1 <= 2 OK)
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.BeforeToolCall(0, 1, "test", nil),
			tt.AfterToolCall(0, 1, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolObs)),
			// Iteration 2: tool call 2 (2 <= 2 OK)
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.BeforeToolCall(0, 2, "test", nil),
			tt.AfterToolCall(0, 2, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(toolObs)),
			// Iteration 3: tool call 3 (3 > 2 EXCEEDED at BeforeToolCall)
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.BeforeToolCall(0, 3, "test", nil),
			tt.LimitExceeded(0, 3, limit, 3, gent.SCToolCalls),
			tt.AfterToolCall(0, 3, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(toolObs)),
			tt.AfterExec(0, 3, gent.TerminationLimitExceeded),
		}
		actualEvents := tt.CollectLifecycleEvents(execCtx)
		tt.AssertEventsEqual(t, expectedEvents, actualEvents)
	})

	t.Run("stops when tool-specific call limit exceeded (prefix)", func(t *testing.T) {
		// Two tools: search and get_detail
		// search is called 3 times (no limit), get_detail has limit of 1
		model := tt.NewMockModel().
			AddResponse("<action>tool: search</action>", 100, 50).
			AddResponse("<action>tool: search</action>", 100, 50).
			AddResponse("<action>tool: search</action>", 100, 50). // search: 3 calls (no limit)
			AddResponse("<action>tool: get_detail</action>", 100, 50).
			AddResponse("<action>tool: get_detail</action>", 100, 50). // get_detail: 2nd call exceeds
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: search"}}).
			AddParseResult(map[string][]string{"action": {"tool: search"}}).
			AddParseResult(map[string][]string{"action": {"tool: search"}}).
			AddParseResult(map[string][]string{"action": {"tool: get_detail"}}).
			AddParseResult(map[string][]string{"action": {"tool: get_detail"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain()
		termination := tt.NewMockTermination()

		// Only limit get_detail to 1 call
		limit := tt.PrefixLimit(gent.SCToolCallsFor+"get_detail", 1)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())
		assert.Equal(t, 5, execCtx.Iteration()) // Fails on 5th iteration (2nd get_detail call)

		// Build expected NextPrompts for tool executions
		searchObs := tt.ToolObservation(format, toolChain, "search", "tool executed")
		getDetailObs := tt.ToolObservation(format, toolChain, "get_detail", "tool executed")

		// Assert full event sequence
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iterations 1-3: search calls (no limit)
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.BeforeToolCall(0, 1, "search", nil),
			tt.AfterToolCall(0, 1, "search", nil, "tool executed", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(searchObs)),

			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.BeforeToolCall(0, 2, "search", nil),
			tt.AfterToolCall(0, 2, "search", nil, "tool executed", nil),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(searchObs)),

			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.BeforeToolCall(0, 3, "search", nil),
			tt.AfterToolCall(0, 3, "search", nil, "tool executed", nil),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(searchObs)),
			// Iteration 4: get_detail call 1 (1 <= 1 OK)
			tt.BeforeIter(0, 4),
			tt.BeforeModelCall(0, 4, "test-model"),
			tt.AfterModelCall(0, 4, "test-model", 100, 50),
			tt.BeforeToolCall(0, 4, "get_detail", nil),
			tt.AfterToolCall(0, 4, "get_detail", nil, "tool executed", nil),
			tt.AfterIter(0, 4, tt.ContinueWithPrompt(getDetailObs)),
			// Iteration 5: get_detail call 2 (2 > 1 EXCEEDED)
			tt.BeforeIter(0, 5),
			tt.BeforeModelCall(0, 5, "test-model"),
			tt.AfterModelCall(0, 5, "test-model", 100, 50),
			tt.BeforeToolCall(0, 5, "get_detail", nil),
			tt.LimitExceeded(0, 5, limit, 2, gent.SCToolCallsFor+"get_detail"),
			tt.AfterToolCall(0, 5, "get_detail", nil, "tool executed", nil),
			tt.AfterIter(0, 5, tt.ContinueWithPrompt(getDetailObs)),
			tt.AfterExec(0, 5, gent.TerminationLimitExceeded),
		}
		actualEvents := tt.CollectLifecycleEvents(execCtx)
		tt.AssertEventsEqual(t, expectedEvents, actualEvents)
	})

	t.Run("exceeds limit at third iteration", func(t *testing.T) {
		// 4 tool calls across iterations, limit is 3
		model := tt.NewMockModel().
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<action>tool: test</action>", 100, 50). // 4th call exceeds limit
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain()
		termination := tt.NewMockTermination()

		limit := tt.ExactLimit(gent.SCToolCalls, 3)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())
		assert.Equal(t, 4, execCtx.Iteration())

		// Build expected NextPrompt for tool execution
		toolObs := tt.ToolObservation(format, toolChain, "test", "tool executed")

		// Assert full event sequence
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iterations 1-3: tool calls 1-3 (OK)
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.BeforeToolCall(0, 1, "test", nil),
			tt.AfterToolCall(0, 1, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolObs)),

			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.BeforeToolCall(0, 2, "test", nil),
			tt.AfterToolCall(0, 2, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(toolObs)),

			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.BeforeToolCall(0, 3, "test", nil),
			tt.AfterToolCall(0, 3, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(toolObs)),
			// Iteration 4: tool call 4 (4 > 3 EXCEEDED)
			tt.BeforeIter(0, 4),
			tt.BeforeModelCall(0, 4, "test-model"),
			tt.AfterModelCall(0, 4, "test-model", 100, 50),
			tt.BeforeToolCall(0, 4, "test", nil),
			tt.LimitExceeded(0, 4, limit, 4, gent.SCToolCalls),
			tt.AfterToolCall(0, 4, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 4, tt.ContinueWithPrompt(toolObs)),
			tt.AfterExec(0, 4, gent.TerminationLimitExceeded),
		}
		actualEvents := tt.CollectLifecycleEvents(execCtx)
		tt.AssertEventsEqual(t, expectedEvents, actualEvents)
	})

	t.Run("prefix limit exceeds at third iteration", func(t *testing.T) {
		// Call "search" 4 times, limit is 3
		model := tt.NewMockModel().
			AddResponse("<action>tool: search</action>", 100, 50).
			AddResponse("<action>tool: search</action>", 100, 50).
			AddResponse("<action>tool: search</action>", 100, 50).
			AddResponse("<action>tool: search</action>", 100, 50). // 4th call exceeds
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: search"}}).
			AddParseResult(map[string][]string{"action": {"tool: search"}}).
			AddParseResult(map[string][]string{"action": {"tool: search"}}).
			AddParseResult(map[string][]string{"action": {"tool: search"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain()
		termination := tt.NewMockTermination()

		limit := tt.PrefixLimit(gent.SCToolCallsFor, 3)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		require.NotNil(t, execCtx.ExceededLimit())
		assert.Equal(t, 4, execCtx.Iteration())

		// Build expected NextPrompt for tool execution
		searchObs := tt.ToolObservation(format, toolChain, "search", "tool executed")

		// Assert full event sequence
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iterations 1-3: search calls 1-3 (OK)
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.BeforeToolCall(0, 1, "search", nil),
			tt.AfterToolCall(0, 1, "search", nil, "tool executed", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(searchObs)),

			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.BeforeToolCall(0, 2, "search", nil),
			tt.AfterToolCall(0, 2, "search", nil, "tool executed", nil),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(searchObs)),

			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.BeforeToolCall(0, 3, "search", nil),
			tt.AfterToolCall(0, 3, "search", nil, "tool executed", nil),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(searchObs)),
			// Iteration 4: search call 4 (4 > 3 EXCEEDED)
			tt.BeforeIter(0, 4),
			tt.BeforeModelCall(0, 4, "test-model"),
			tt.AfterModelCall(0, 4, "test-model", 100, 50),
			tt.BeforeToolCall(0, 4, "search", nil),
			tt.LimitExceeded(0, 4, limit, 4, gent.SCToolCallsFor+"search"),
			tt.AfterToolCall(0, 4, "search", nil, "tool executed", nil),
			tt.AfterIter(0, 4, tt.ContinueWithPrompt(searchObs)),
			tt.AfterExec(0, 4, gent.TerminationLimitExceeded),
		}
		actualEvents := tt.CollectLifecycleEvents(execCtx)
		tt.AssertEventsEqual(t, expectedEvents, actualEvents)
	})
}

// ----------------------------------------------------------------------------
// Test: Parse error total limits
// ----------------------------------------------------------------------------

func TestExecutorLimits_FormatParseErrorTotal(t *testing.T) {
	t.Run("stops when format parse error total exceeded", func(t *testing.T) {
		model := tt.NewMockModel().
			AddResponse("invalid1", 100, 50).
			AddResponse("invalid2", 100, 50).
			AddResponse("invalid3", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseError(gent.ErrNoSectionsFound).
			AddParseError(gent.ErrNoSectionsFound).
			AddParseError(gent.ErrNoSectionsFound).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain()
		termination := tt.NewMockTermination()

		limit := tt.ExactLimit(gent.SCFormatParseErrorTotal, 2)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())

		// Build expected NextPrompts for format parse errors
		parseObs1 := tt.FormatParseErrorObservation(format, gent.ErrNoSectionsFound, "invalid1")
		parseObs2 := tt.FormatParseErrorObservation(format, gent.ErrNoSectionsFound, "invalid2")
		parseObs3 := tt.FormatParseErrorObservation(format, gent.ErrNoSectionsFound, "invalid3")

		// Assert full event sequence
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: parse error 1 (1 <= 2 OK)
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.ParseError(0, 1, gent.ParseErrorTypeFormat, "invalid1"),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(parseObs1)),
			// Iteration 2: parse error 2 (2 <= 2 OK)
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.ParseError(0, 2, gent.ParseErrorTypeFormat, "invalid2"),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(parseObs2)),
			// Iteration 3: parse error 3 (3 > 2 EXCEEDED)
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.ParseError(0, 3, gent.ParseErrorTypeFormat, "invalid3"),
			tt.LimitExceeded(0, 3, limit, 3, gent.SCFormatParseErrorTotal),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(parseObs3)),
			tt.AfterExec(0, 3, gent.TerminationLimitExceeded),
		}
		actualEvents := tt.CollectLifecycleEvents(execCtx)
		tt.AssertEventsEqual(t, expectedEvents, actualEvents)
	})

	t.Run("exceeds limit at third iteration after successful iterations", func(t *testing.T) {
		// Iteration 1: success (tool call)
		// Iteration 2: success (tool call)
		// Iteration 3: parse error (total: 1)
		// Iteration 4: parse error (total: 2)
		// Iteration 5: parse error (total: 3 > limit of 2)
		model := tt.NewMockModel().
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("invalid1", 100, 50).
			AddResponse("invalid2", 100, 50).
			AddResponse("invalid3", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseError(gent.ErrNoSectionsFound).
			AddParseError(gent.ErrNoSectionsFound).
			AddParseError(gent.ErrNoSectionsFound).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain()
		termination := tt.NewMockTermination()

		limit := tt.ExactLimit(gent.SCFormatParseErrorTotal, 2)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())
		assert.Equal(t, 5, execCtx.Iteration())

		// Build expected NextPrompts
		toolObs := tt.ToolObservation(format, toolChain, "test", "tool executed")
		parseObs1 := tt.FormatParseErrorObservation(format, gent.ErrNoSectionsFound, "invalid1")
		parseObs2 := tt.FormatParseErrorObservation(format, gent.ErrNoSectionsFound, "invalid2")
		parseObs3 := tt.FormatParseErrorObservation(format, gent.ErrNoSectionsFound, "invalid3")

		// Assert full event sequence
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iterations 1-2: successful tool calls
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.BeforeToolCall(0, 1, "test", nil),
			tt.AfterToolCall(0, 1, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolObs)),

			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.BeforeToolCall(0, 2, "test", nil),
			tt.AfterToolCall(0, 2, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(toolObs)),
			// Iteration 3: parse error 1
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.ParseError(0, 3, gent.ParseErrorTypeFormat, "invalid1"),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(parseObs1)),
			// Iteration 4: parse error 2
			tt.BeforeIter(0, 4),
			tt.BeforeModelCall(0, 4, "test-model"),
			tt.AfterModelCall(0, 4, "test-model", 100, 50),
			tt.ParseError(0, 4, gent.ParseErrorTypeFormat, "invalid2"),
			tt.AfterIter(0, 4, tt.ContinueWithPrompt(parseObs2)),
			// Iteration 5: parse error 3 (EXCEEDED)
			tt.BeforeIter(0, 5),
			tt.BeforeModelCall(0, 5, "test-model"),
			tt.AfterModelCall(0, 5, "test-model", 100, 50),
			tt.ParseError(0, 5, gent.ParseErrorTypeFormat, "invalid3"),
			tt.LimitExceeded(0, 5, limit, 3, gent.SCFormatParseErrorTotal),
			tt.AfterIter(0, 5, tt.ContinueWithPrompt(parseObs3)),
			tt.AfterExec(0, 5, gent.TerminationLimitExceeded),
		}
		actualEvents := tt.CollectLifecycleEvents(execCtx)
		tt.AssertEventsEqual(t, expectedEvents, actualEvents)
	})
}

func TestExecutorLimits_ToolchainParseErrorTotal(t *testing.T) {
	t.Run("stops when toolchain parse error total exceeded", func(t *testing.T) {
		model := tt.NewMockModel().
			AddResponse("<action>invalid json</action>", 100, 50).
			AddResponse("<action>invalid json</action>", 100, 50).
			AddResponse("<action>invalid json</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"invalid json"}}).
			AddParseResult(map[string][]string{"action": {"invalid json"}}).
			AddParseResult(map[string][]string{"action": {"invalid json"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain().
			WithParseErrors(gent.ErrInvalidJSON, gent.ErrInvalidJSON, gent.ErrInvalidJSON, nil)
		termination := tt.NewMockTermination()

		limit := tt.ExactLimit(gent.SCToolchainParseErrorTotal, 2)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())

		// Build expected NextPrompt for toolchain parse errors
		tcObs := tt.ToolchainErrorObservation(format, gent.ErrInvalidJSON)

		// Assert events: 3 iterations with toolchain parse errors, limit exceeded at 3rd
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: parse error (count: 1)
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.ParseError(0, 1, gent.ParseErrorTypeToolchain, "invalid json"),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(tcObs)),
			// Iteration 2: parse error (count: 2)
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.ParseError(0, 2, gent.ParseErrorTypeToolchain, "invalid json"),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(tcObs)),
			// Iteration 3: parse error (count: 3 > limit 2)
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.ParseError(0, 3, gent.ParseErrorTypeToolchain, "invalid json"),
			tt.LimitExceeded(0, 3, limit, 3, gent.SCToolchainParseErrorTotal),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(tcObs)),
			tt.AfterExec(0, 3, gent.TerminationLimitExceeded),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})

	t.Run("exceeds limit at third iteration after successful iterations", func(t *testing.T) {
		// Iteration 1-2: successful tool calls
		// Iteration 3-5: toolchain parse errors (3rd error exceeds limit of 2)
		model := tt.NewMockModel().
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<action>bad1</action>", 100, 50).
			AddResponse("<action>bad2</action>", 100, 50).
			AddResponse("<action>bad3</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"bad1"}}).
			AddParseResult(map[string][]string{"action": {"bad2"}}).
			AddParseResult(map[string][]string{"action": {"bad3"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain().
			WithParseErrors(nil, nil, gent.ErrInvalidJSON, gent.ErrInvalidJSON, gent.ErrInvalidJSON, nil)
		termination := tt.NewMockTermination()

		limit := tt.ExactLimit(gent.SCToolchainParseErrorTotal, 2)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())
		assert.Equal(t, 5, execCtx.Iteration())

		// Build expected NextPrompts
		toolObs := tt.ToolObservation(format, toolChain, "test", "tool executed")
		tcObs := tt.ToolchainErrorObservation(format, gent.ErrInvalidJSON)

		// Assert events: 2 successful iterations, then 3 with toolchain parse errors
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: successful tool call
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.BeforeToolCall(0, 1, "test", nil),
			tt.AfterToolCall(0, 1, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolObs)),
			// Iteration 2: successful tool call
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.BeforeToolCall(0, 2, "test", nil),
			tt.AfterToolCall(0, 2, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(toolObs)),
			// Iteration 3: parse error (count: 1)
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.ParseError(0, 3, gent.ParseErrorTypeToolchain, "bad1"),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(tcObs)),
			// Iteration 4: parse error (count: 2)
			tt.BeforeIter(0, 4),
			tt.BeforeModelCall(0, 4, "test-model"),
			tt.AfterModelCall(0, 4, "test-model", 100, 50),
			tt.ParseError(0, 4, gent.ParseErrorTypeToolchain, "bad2"),
			tt.AfterIter(0, 4, tt.ContinueWithPrompt(tcObs)),
			// Iteration 5: parse error (count: 3 > limit 2)
			tt.BeforeIter(0, 5),
			tt.BeforeModelCall(0, 5, "test-model"),
			tt.AfterModelCall(0, 5, "test-model", 100, 50),
			tt.ParseError(0, 5, gent.ParseErrorTypeToolchain, "bad3"),
			tt.LimitExceeded(0, 5, limit, 3, gent.SCToolchainParseErrorTotal),
			tt.AfterIter(0, 5, tt.ContinueWithPrompt(tcObs)),
			tt.AfterExec(0, 5, gent.TerminationLimitExceeded),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})
}

func TestExecutorLimits_TerminationParseErrorTotal(t *testing.T) {
	t.Run("stops when termination parse error total exceeded", func(t *testing.T) {
		model := tt.NewMockModel().
			AddResponse("<answer>malformed</answer>", 100, 50).
			AddResponse("<answer>malformed</answer>", 100, 50).
			AddResponse("<answer>malformed</answer>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"answer": {"malformed"}}).
			AddParseResult(map[string][]string{"answer": {"malformed"}}).
			AddParseResult(map[string][]string{"answer": {"malformed"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain()
		termination := tt.NewMockTermination().
			WithParseErrors(gent.ErrInvalidJSON, gent.ErrInvalidJSON, gent.ErrInvalidJSON, nil)

		limit := tt.ExactLimit(gent.SCTerminationParseErrorTotal, 2)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())

		// Build expected NextPrompt for termination parse error
		termObs := tt.TerminationParseErrorObservation(format, gent.ErrInvalidJSON, "malformed")

		// Assert events: 3 iterations with termination parse errors, limit exceeded at 3rd
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: termination parse error (count: 1)
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.ParseError(0, 1, gent.ParseErrorTypeTermination, "malformed"),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(termObs)),
			// Iteration 2: termination parse error (count: 2)
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.ParseError(0, 2, gent.ParseErrorTypeTermination, "malformed"),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(termObs)),
			// Iteration 3: termination parse error (count: 3 > limit 2)
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.ParseError(0, 3, gent.ParseErrorTypeTermination, "malformed"),
			tt.LimitExceeded(0, 3, limit, 3, gent.SCTerminationParseErrorTotal),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(termObs)),
			tt.AfterExec(0, 3, gent.TerminationLimitExceeded),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})

	t.Run("exceeds limit at third iteration after successful iterations", func(t *testing.T) {
		// Iteration 1-2: successful tool calls (no termination parsing)
		// Iteration 3-5: termination parse errors (3rd error exceeds limit of 2)
		// Note: termination parsing only happens for answer sections, so parse error
		// indices start at 0 for the first answer section
		model := tt.NewMockModel().
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<answer>bad1</answer>", 100, 50).
			AddResponse("<answer>bad2</answer>", 100, 50).
			AddResponse("<answer>bad3</answer>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"answer": {"bad1"}}).
			AddParseResult(map[string][]string{"answer": {"bad2"}}).
			AddParseResult(map[string][]string{"answer": {"bad3"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain()
		// Parse errors indices: 0=bad1, 1=bad2, 2=bad3, 3=done
		termination := tt.NewMockTermination().
			WithParseErrors(gent.ErrInvalidJSON, gent.ErrInvalidJSON, gent.ErrInvalidJSON, nil)

		limit := tt.ExactLimit(gent.SCTerminationParseErrorTotal, 2)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())
		assert.Equal(t, 5, execCtx.Iteration())

		// Build expected NextPrompts
		toolObs := tt.ToolObservation(format, toolChain, "test", "tool executed")
		termObs1 := tt.TerminationParseErrorObservation(format, gent.ErrInvalidJSON, "bad1")
		termObs2 := tt.TerminationParseErrorObservation(format, gent.ErrInvalidJSON, "bad2")
		termObs3 := tt.TerminationParseErrorObservation(format, gent.ErrInvalidJSON, "bad3")

		// Assert events: 2 successful iterations, then 3 with termination parse errors
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: successful tool call
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.BeforeToolCall(0, 1, "test", nil),
			tt.AfterToolCall(0, 1, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolObs)),
			// Iteration 2: successful tool call
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.BeforeToolCall(0, 2, "test", nil),
			tt.AfterToolCall(0, 2, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(toolObs)),
			// Iteration 3: termination parse error (count: 1)
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.ParseError(0, 3, gent.ParseErrorTypeTermination, "bad1"),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(termObs1)),
			// Iteration 4: termination parse error (count: 2)
			tt.BeforeIter(0, 4),
			tt.BeforeModelCall(0, 4, "test-model"),
			tt.AfterModelCall(0, 4, "test-model", 100, 50),
			tt.ParseError(0, 4, gent.ParseErrorTypeTermination, "bad2"),
			tt.AfterIter(0, 4, tt.ContinueWithPrompt(termObs2)),
			// Iteration 5: termination parse error (count: 3 > limit 2)
			tt.BeforeIter(0, 5),
			tt.BeforeModelCall(0, 5, "test-model"),
			tt.AfterModelCall(0, 5, "test-model", 100, 50),
			tt.ParseError(0, 5, gent.ParseErrorTypeTermination, "bad3"),
			tt.LimitExceeded(0, 5, limit, 3, gent.SCTerminationParseErrorTotal),
			tt.AfterIter(0, 5, tt.ContinueWithPrompt(termObs3)),
			tt.AfterExec(0, 5, gent.TerminationLimitExceeded),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})
}

func TestExecutorLimits_SectionParseErrorTotal(t *testing.T) {
	t.Run("stops when section parse error total exceeded", func(t *testing.T) {
		// Section parse errors are triggered by thinking section ParseSection failures.
		// The agent processes thinking content silently - errors track stats but don't
		// provide feedback, so the iteration continues normally with tool calls.

		model := tt.NewMockModel().
			AddResponse("<thinking>bad</thinking><action>tool: test</action>", 100, 50).
			AddResponse("<thinking>bad</thinking><action>tool: test</action>", 100, 50).
			AddResponse("<thinking>bad</thinking><action>tool: test</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"thinking": {"bad"}, "action": {"tool: test"}}).
			AddParseResult(map[string][]string{"thinking": {"bad"}, "action": {"tool: test"}}).
			AddParseResult(map[string][]string{"thinking": {"bad"}, "action": {"tool: test"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain()
		termination := tt.NewMockTermination()

		// MockSection configured to fail ParseSection with errors
		thinkingSection := tt.NewMockSection("thinking").
			WithParseErrors(gent.ErrInvalidYAML, gent.ErrInvalidYAML, gent.ErrInvalidYAML, nil)

		limit := tt.ExactLimit(gent.SCSectionParseErrorTotal, 2)
		limits := []gent.Limit{limit}

		execCtx := runWithLimitAndThinking(t, model, format, toolChain, termination,
			thinkingSection, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())

		// Build expected NextPrompt for tool execution
		toolObs := tt.ToolObservation(format, toolChain, "test", "tool executed")

		// Section parse errors are silent - don't affect NextPrompt
		// The iteration continues normally with tool execution
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: thinking parse error (total: 1 <= 2 OK), then tool call
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.ParseError(0, 1, gent.ParseErrorTypeSection, "bad"),
			tt.BeforeToolCall(0, 1, "test", nil),
			tt.AfterToolCall(0, 1, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolObs)),
			// Iteration 2: thinking parse error (total: 2 <= 2 OK), then tool call
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.ParseError(0, 2, gent.ParseErrorTypeSection, "bad"),
			tt.BeforeToolCall(0, 2, "test", nil),
			tt.AfterToolCall(0, 2, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(toolObs)),
			// Iteration 3: thinking parse error (total: 3 > 2 EXCEEDED), then tool call
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.ParseError(0, 3, gent.ParseErrorTypeSection, "bad"),
			tt.LimitExceeded(0, 3, limit, 3, gent.SCSectionParseErrorTotal),
			tt.BeforeToolCall(0, 3, "test", nil),
			tt.AfterToolCall(0, 3, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(toolObs)),
			tt.AfterExec(0, 3, gent.TerminationLimitExceeded),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})

	t.Run("exceeds limit at fifth iteration after successful iterations", func(t *testing.T) {
		// Iterations 1-2: thinking parses successfully, tool executes
		// Iterations 3-5: thinking fails to parse (errors: 1, 2, 3 - exceeds limit of 2)
		model := tt.NewMockModel().
			AddResponse("<thinking>good</thinking><action>tool: test</action>", 100, 50).
			AddResponse("<thinking>good</thinking><action>tool: test</action>", 100, 50).
			AddResponse("<thinking>bad</thinking><action>tool: test</action>", 100, 50).
			AddResponse("<thinking>bad</thinking><action>tool: test</action>", 100, 50).
			AddResponse("<thinking>bad</thinking><action>tool: test</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"thinking": {"good"}, "action": {"tool: test"}}).
			AddParseResult(map[string][]string{"thinking": {"good"}, "action": {"tool: test"}}).
			AddParseResult(map[string][]string{"thinking": {"bad"}, "action": {"tool: test"}}).
			AddParseResult(map[string][]string{"thinking": {"bad"}, "action": {"tool: test"}}).
			AddParseResult(map[string][]string{"thinking": {"bad"}, "action": {"tool: test"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain()
		termination := tt.NewMockTermination()

		// MockSection: first 2 calls succeed, then 3 consecutive failures
		thinkingSection := tt.NewMockSection("thinking").
			WithParseErrors(nil, nil, gent.ErrInvalidYAML, gent.ErrInvalidYAML, gent.ErrInvalidYAML, nil)

		limit := tt.ExactLimit(gent.SCSectionParseErrorTotal, 2)
		limits := []gent.Limit{limit}

		execCtx := runWithLimitAndThinking(t, model, format, toolChain, termination,
			thinkingSection, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())
		assert.Equal(t, 5, execCtx.Iteration())

		// Build expected NextPrompt for tool execution
		toolObs := tt.ToolObservation(format, toolChain, "test", "tool executed")

		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: thinking OK, tool call
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.BeforeToolCall(0, 1, "test", nil),
			tt.AfterToolCall(0, 1, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolObs)),
			// Iteration 2: thinking OK, tool call
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.BeforeToolCall(0, 2, "test", nil),
			tt.AfterToolCall(0, 2, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(toolObs)),
			// Iteration 3: thinking error (total: 1), tool call
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.ParseError(0, 3, gent.ParseErrorTypeSection, "bad"),
			tt.BeforeToolCall(0, 3, "test", nil),
			tt.AfterToolCall(0, 3, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(toolObs)),
			// Iteration 4: thinking error (total: 2), tool call
			tt.BeforeIter(0, 4),
			tt.BeforeModelCall(0, 4, "test-model"),
			tt.AfterModelCall(0, 4, "test-model", 100, 50),
			tt.ParseError(0, 4, gent.ParseErrorTypeSection, "bad"),
			tt.BeforeToolCall(0, 4, "test", nil),
			tt.AfterToolCall(0, 4, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 4, tt.ContinueWithPrompt(toolObs)),
			// Iteration 5: thinking error (total: 3 > 2 EXCEEDED), tool call
			tt.BeforeIter(0, 5),
			tt.BeforeModelCall(0, 5, "test-model"),
			tt.AfterModelCall(0, 5, "test-model", 100, 50),
			tt.ParseError(0, 5, gent.ParseErrorTypeSection, "bad"),
			tt.LimitExceeded(0, 5, limit, 3, gent.SCSectionParseErrorTotal),
			tt.BeforeToolCall(0, 5, "test", nil),
			tt.AfterToolCall(0, 5, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 5, tt.ContinueWithPrompt(toolObs)),
			tt.AfterExec(0, 5, gent.TerminationLimitExceeded),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})
}

// ----------------------------------------------------------------------------
// Test: Parse error consecutive limits
// ----------------------------------------------------------------------------

func TestExecutorLimits_FormatParseErrorConsecutive(t *testing.T) {
	t.Run("stops when format parse error consecutive exceeded", func(t *testing.T) {
		model := tt.NewMockModel().
			AddResponse("invalid1", 100, 50).
			AddResponse("invalid2", 100, 50).
			AddResponse("invalid3", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseError(gent.ErrNoSectionsFound).
			AddParseError(gent.ErrNoSectionsFound).
			AddParseError(gent.ErrNoSectionsFound).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain()
		termination := tt.NewMockTermination()

		limit := tt.ExactLimit(gent.SGFormatParseErrorConsecutive, 2)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())

		// Build expected NextPrompts for format parse errors
		parseObs1 := tt.FormatParseErrorObservation(format, gent.ErrNoSectionsFound, "invalid1")
		parseObs2 := tt.FormatParseErrorObservation(format, gent.ErrNoSectionsFound, "invalid2")
		parseObs3 := tt.FormatParseErrorObservation(format, gent.ErrNoSectionsFound, "invalid3")

		// Assert events: 3 iterations with consecutive format parse errors
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: parse error (consecutive: 1)
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.ParseError(0, 1, gent.ParseErrorTypeFormat, "invalid1"),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(parseObs1)),
			// Iteration 2: parse error (consecutive: 2)
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.ParseError(0, 2, gent.ParseErrorTypeFormat, "invalid2"),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(parseObs2)),
			// Iteration 3: parse error (consecutive: 3 > limit 2)
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.ParseError(0, 3, gent.ParseErrorTypeFormat, "invalid3"),
			tt.LimitExceeded(0, 3, limit, 3, gent.SGFormatParseErrorConsecutive),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(parseObs3)),
			tt.AfterExec(0, 3, gent.TerminationLimitExceeded),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})

	t.Run("exceeds consecutive limit at third iteration after successful iterations", func(t *testing.T) {
		// Iteration 1-2: successful tool calls
		// Iteration 3-5: consecutive format parse errors (3rd error exceeds limit of 2)
		model := tt.NewMockModel().
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("invalid1", 100, 50).
			AddResponse("invalid2", 100, 50).
			AddResponse("invalid3", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseError(gent.ErrNoSectionsFound).
			AddParseError(gent.ErrNoSectionsFound).
			AddParseError(gent.ErrNoSectionsFound).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain()
		termination := tt.NewMockTermination()

		limit := tt.ExactLimit(gent.SGFormatParseErrorConsecutive, 2)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())
		assert.Equal(t, 5, execCtx.Iteration())

		// Build expected NextPrompts
		toolObs := tt.ToolObservation(format, toolChain, "test", "tool executed")
		parseObs1 := tt.FormatParseErrorObservation(format, gent.ErrNoSectionsFound, "invalid1")
		parseObs2 := tt.FormatParseErrorObservation(format, gent.ErrNoSectionsFound, "invalid2")
		parseObs3 := tt.FormatParseErrorObservation(format, gent.ErrNoSectionsFound, "invalid3")

		// Assert events: 2 successful iterations, then 3 consecutive format parse errors
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: successful tool call
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.BeforeToolCall(0, 1, "test", nil),
			tt.AfterToolCall(0, 1, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolObs)),
			// Iteration 2: successful tool call
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.BeforeToolCall(0, 2, "test", nil),
			tt.AfterToolCall(0, 2, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(toolObs)),
			// Iteration 3: parse error (consecutive: 1)
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.ParseError(0, 3, gent.ParseErrorTypeFormat, "invalid1"),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(parseObs1)),
			// Iteration 4: parse error (consecutive: 2)
			tt.BeforeIter(0, 4),
			tt.BeforeModelCall(0, 4, "test-model"),
			tt.AfterModelCall(0, 4, "test-model", 100, 50),
			tt.ParseError(0, 4, gent.ParseErrorTypeFormat, "invalid2"),
			tt.AfterIter(0, 4, tt.ContinueWithPrompt(parseObs2)),
			// Iteration 5: parse error (consecutive: 3 > limit 2)
			tt.BeforeIter(0, 5),
			tt.BeforeModelCall(0, 5, "test-model"),
			tt.AfterModelCall(0, 5, "test-model", 100, 50),
			tt.ParseError(0, 5, gent.ParseErrorTypeFormat, "invalid3"),
			tt.LimitExceeded(0, 5, limit, 3, gent.SGFormatParseErrorConsecutive),
			tt.AfterIter(0, 5, tt.ContinueWithPrompt(parseObs3)),
			tt.AfterExec(0, 5, gent.TerminationLimitExceeded),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})
}

func TestExecutorLimits_ToolchainParseErrorConsecutive(t *testing.T) {
	t.Run("stops when toolchain parse error consecutive exceeded", func(t *testing.T) {
		model := tt.NewMockModel().
			AddResponse("<action>invalid</action>", 100, 50).
			AddResponse("<action>invalid</action>", 100, 50).
			AddResponse("<action>invalid</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"invalid"}}).
			AddParseResult(map[string][]string{"action": {"invalid"}}).
			AddParseResult(map[string][]string{"action": {"invalid"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain().
			WithParseErrors(gent.ErrInvalidJSON, gent.ErrInvalidJSON, gent.ErrInvalidJSON, nil)
		termination := tt.NewMockTermination()

		limit := tt.ExactLimit(gent.SGToolchainParseErrorConsecutive, 2)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())

		// Build expected NextPrompt for toolchain parse error
		errObs := tt.ToolchainErrorObservation(format, gent.ErrInvalidJSON)

		// Assert events: 3 iterations with consecutive toolchain parse errors
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: parse error (consecutive: 1)
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.ParseError(0, 1, gent.ParseErrorTypeToolchain, "invalid"),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(errObs)),
			// Iteration 2: parse error (consecutive: 2)
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.ParseError(0, 2, gent.ParseErrorTypeToolchain, "invalid"),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(errObs)),
			// Iteration 3: parse error (consecutive: 3 > limit 2)
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.ParseError(0, 3, gent.ParseErrorTypeToolchain, "invalid"),
			tt.LimitExceeded(0, 3, limit, 3, gent.SGToolchainParseErrorConsecutive),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(errObs)),
			tt.AfterExec(0, 3, gent.TerminationLimitExceeded),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})

	t.Run("exceeds consecutive limit at third iteration after successful iterations", func(t *testing.T) {
		// Iteration 1-2: successful tool calls
		// Iteration 3-5: consecutive toolchain parse errors (3rd error exceeds limit of 2)
		model := tt.NewMockModel().
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<action>bad1</action>", 100, 50).
			AddResponse("<action>bad2</action>", 100, 50).
			AddResponse("<action>bad3</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"bad1"}}).
			AddParseResult(map[string][]string{"action": {"bad2"}}).
			AddParseResult(map[string][]string{"action": {"bad3"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain().
			WithParseErrors(nil, nil, gent.ErrInvalidJSON, gent.ErrInvalidJSON, gent.ErrInvalidJSON, nil)
		termination := tt.NewMockTermination()

		limit := tt.ExactLimit(gent.SGToolchainParseErrorConsecutive, 2)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())
		assert.Equal(t, 5, execCtx.Iteration())

		// Build expected NextPrompts
		toolObs := tt.ToolObservation(format, toolChain, "test", "tool executed")
		errObs := tt.ToolchainErrorObservation(format, gent.ErrInvalidJSON)

		// Assert events: 2 successful iterations, then 3 consecutive toolchain parse errors
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: successful tool call
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.BeforeToolCall(0, 1, "test", nil),
			tt.AfterToolCall(0, 1, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolObs)),
			// Iteration 2: successful tool call
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.BeforeToolCall(0, 2, "test", nil),
			tt.AfterToolCall(0, 2, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(toolObs)),
			// Iteration 3: parse error (consecutive: 1)
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.ParseError(0, 3, gent.ParseErrorTypeToolchain, "bad1"),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(errObs)),
			// Iteration 4: parse error (consecutive: 2)
			tt.BeforeIter(0, 4),
			tt.BeforeModelCall(0, 4, "test-model"),
			tt.AfterModelCall(0, 4, "test-model", 100, 50),
			tt.ParseError(0, 4, gent.ParseErrorTypeToolchain, "bad2"),
			tt.AfterIter(0, 4, tt.ContinueWithPrompt(errObs)),
			// Iteration 5: parse error (consecutive: 3 > limit 2)
			tt.BeforeIter(0, 5),
			tt.BeforeModelCall(0, 5, "test-model"),
			tt.AfterModelCall(0, 5, "test-model", 100, 50),
			tt.ParseError(0, 5, gent.ParseErrorTypeToolchain, "bad3"),
			tt.LimitExceeded(0, 5, limit, 3, gent.SGToolchainParseErrorConsecutive),
			tt.AfterIter(0, 5, tt.ContinueWithPrompt(errObs)),
			tt.AfterExec(0, 5, gent.TerminationLimitExceeded),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})
}

func TestExecutorLimits_TerminationParseErrorConsecutive(t *testing.T) {
	t.Run("stops when termination parse error consecutive exceeded", func(t *testing.T) {
		model := tt.NewMockModel().
			AddResponse("<answer>bad1</answer>", 100, 50).
			AddResponse("<answer>bad2</answer>", 100, 50).
			AddResponse("<answer>bad3</answer>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"answer": {"bad1"}}).
			AddParseResult(map[string][]string{"answer": {"bad2"}}).
			AddParseResult(map[string][]string{"answer": {"bad3"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain()
		termination := tt.NewMockTermination().
			WithParseErrors(gent.ErrInvalidJSON, gent.ErrInvalidJSON, gent.ErrInvalidJSON, nil)

		limit := tt.ExactLimit(gent.SGTerminationParseErrorConsecutive, 2)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())

		// Build expected NextPrompts for termination parse errors
		termObs1 := tt.TerminationParseErrorObservation(format, gent.ErrInvalidJSON, "bad1")
		termObs2 := tt.TerminationParseErrorObservation(format, gent.ErrInvalidJSON, "bad2")
		termObs3 := tt.TerminationParseErrorObservation(format, gent.ErrInvalidJSON, "bad3")

		// Assert events: 3 iterations with consecutive termination parse errors
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: termination parse error (consecutive: 1)
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.ParseError(0, 1, gent.ParseErrorTypeTermination, "bad1"),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(termObs1)),
			// Iteration 2: termination parse error (consecutive: 2)
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.ParseError(0, 2, gent.ParseErrorTypeTermination, "bad2"),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(termObs2)),
			// Iteration 3: termination parse error (consecutive: 3 > limit 2)
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.ParseError(0, 3, gent.ParseErrorTypeTermination, "bad3"),
			tt.LimitExceeded(0, 3, limit, 3, gent.SGTerminationParseErrorConsecutive),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(termObs3)),
			tt.AfterExec(0, 3, gent.TerminationLimitExceeded),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})

	t.Run("exceeds consecutive limit at third iteration after successful iterations", func(t *testing.T) {
		// Iteration 1-2: successful tool calls (no termination parsing)
		// Iteration 3-5: consecutive termination parse errors (3rd error exceeds limit of 2)
		// Note: termination parsing only happens for answer sections, so parse error
		// indices start at 0 for the first answer section
		model := tt.NewMockModel().
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<answer>bad1</answer>", 100, 50).
			AddResponse("<answer>bad2</answer>", 100, 50).
			AddResponse("<answer>bad3</answer>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"answer": {"bad1"}}).
			AddParseResult(map[string][]string{"answer": {"bad2"}}).
			AddParseResult(map[string][]string{"answer": {"bad3"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain()
		// Parse errors indices: 0=bad1, 1=bad2, 2=bad3, 3=done
		termination := tt.NewMockTermination().
			WithParseErrors(gent.ErrInvalidJSON, gent.ErrInvalidJSON, gent.ErrInvalidJSON, nil)

		limit := tt.ExactLimit(gent.SGTerminationParseErrorConsecutive, 2)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())
		assert.Equal(t, 5, execCtx.Iteration())

		// Build expected NextPrompts
		toolObs := tt.ToolObservation(format, toolChain, "test", "tool executed")
		termObs1 := tt.TerminationParseErrorObservation(format, gent.ErrInvalidJSON, "bad1")
		termObs2 := tt.TerminationParseErrorObservation(format, gent.ErrInvalidJSON, "bad2")
		termObs3 := tt.TerminationParseErrorObservation(format, gent.ErrInvalidJSON, "bad3")

		// Assert events: 2 successful iterations, then 3 consecutive termination parse errors
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: successful tool call
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.BeforeToolCall(0, 1, "test", nil),
			tt.AfterToolCall(0, 1, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolObs)),
			// Iteration 2: successful tool call
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.BeforeToolCall(0, 2, "test", nil),
			tt.AfterToolCall(0, 2, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(toolObs)),
			// Iteration 3: termination parse error (consecutive: 1)
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.ParseError(0, 3, gent.ParseErrorTypeTermination, "bad1"),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(termObs1)),
			// Iteration 4: termination parse error (consecutive: 2)
			tt.BeforeIter(0, 4),
			tt.BeforeModelCall(0, 4, "test-model"),
			tt.AfterModelCall(0, 4, "test-model", 100, 50),
			tt.ParseError(0, 4, gent.ParseErrorTypeTermination, "bad2"),
			tt.AfterIter(0, 4, tt.ContinueWithPrompt(termObs2)),
			// Iteration 5: termination parse error (consecutive: 3 > limit 2)
			tt.BeforeIter(0, 5),
			tt.BeforeModelCall(0, 5, "test-model"),
			tt.AfterModelCall(0, 5, "test-model", 100, 50),
			tt.ParseError(0, 5, gent.ParseErrorTypeTermination, "bad3"),
			tt.LimitExceeded(0, 5, limit, 3, gent.SGTerminationParseErrorConsecutive),
			tt.AfterIter(0, 5, tt.ContinueWithPrompt(termObs3)),
			tt.AfterExec(0, 5, gent.TerminationLimitExceeded),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})
}

func TestExecutorLimits_SectionParseErrorConsecutive(t *testing.T) {
	t.Run("stops when section parse error consecutive exceeded", func(t *testing.T) {
		// Section parse errors are triggered by thinking section ParseSection failures.
		// Consecutive errors accumulate until limit is exceeded.

		model := tt.NewMockModel().
			AddResponse("<thinking>bad</thinking><action>tool: test</action>", 100, 50).
			AddResponse("<thinking>bad</thinking><action>tool: test</action>", 100, 50).
			AddResponse("<thinking>bad</thinking><action>tool: test</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"thinking": {"bad"}, "action": {"tool: test"}}).
			AddParseResult(map[string][]string{"thinking": {"bad"}, "action": {"tool: test"}}).
			AddParseResult(map[string][]string{"thinking": {"bad"}, "action": {"tool: test"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain()
		termination := tt.NewMockTermination()

		// MockSection configured with consecutive parse errors
		thinkingSection := tt.NewMockSection("thinking").
			WithParseErrors(gent.ErrInvalidYAML, gent.ErrInvalidYAML, gent.ErrInvalidYAML, nil)

		limit := tt.ExactLimit(gent.SGSectionParseErrorConsecutive, 2)
		limits := []gent.Limit{limit}

		execCtx := runWithLimitAndThinking(t, model, format, toolChain, termination,
			thinkingSection, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())

		// Build expected NextPrompt for tool execution
		toolObs := tt.ToolObservation(format, toolChain, "test", "tool executed")

		// Section parse errors are silent - don't affect NextPrompt
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: thinking error (consecutive: 1), tool call
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.ParseError(0, 1, gent.ParseErrorTypeSection, "bad"),
			tt.BeforeToolCall(0, 1, "test", nil),
			tt.AfterToolCall(0, 1, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolObs)),
			// Iteration 2: thinking error (consecutive: 2), tool call
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.ParseError(0, 2, gent.ParseErrorTypeSection, "bad"),
			tt.BeforeToolCall(0, 2, "test", nil),
			tt.AfterToolCall(0, 2, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(toolObs)),
			// Iteration 3: thinking error (consecutive: 3 > 2 EXCEEDED), tool call
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.ParseError(0, 3, gent.ParseErrorTypeSection, "bad"),
			tt.LimitExceeded(0, 3, limit, 3, gent.SGSectionParseErrorConsecutive),
			tt.BeforeToolCall(0, 3, "test", nil),
			tt.AfterToolCall(0, 3, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(toolObs)),
			tt.AfterExec(0, 3, gent.TerminationLimitExceeded),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})

	t.Run("exceeds consecutive limit at fifth iteration after successful iterations", func(t *testing.T) {
		// Iterations 1-2: thinking parses successfully, tool executes
		// Iterations 3-5: thinking fails consecutively (consecutive: 1, 2, 3 > limit 2)
		model := tt.NewMockModel().
			AddResponse("<thinking>good</thinking><action>tool: test</action>", 100, 50).
			AddResponse("<thinking>good</thinking><action>tool: test</action>", 100, 50).
			AddResponse("<thinking>bad</thinking><action>tool: test</action>", 100, 50).
			AddResponse("<thinking>bad</thinking><action>tool: test</action>", 100, 50).
			AddResponse("<thinking>bad</thinking><action>tool: test</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"thinking": {"good"}, "action": {"tool: test"}}).
			AddParseResult(map[string][]string{"thinking": {"good"}, "action": {"tool: test"}}).
			AddParseResult(map[string][]string{"thinking": {"bad"}, "action": {"tool: test"}}).
			AddParseResult(map[string][]string{"thinking": {"bad"}, "action": {"tool: test"}}).
			AddParseResult(map[string][]string{"thinking": {"bad"}, "action": {"tool: test"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain()
		termination := tt.NewMockTermination()

		// MockSection: first 2 succeed, then 3 consecutive failures
		thinkingSection := tt.NewMockSection("thinking").
			WithParseErrors(nil, nil, gent.ErrInvalidYAML, gent.ErrInvalidYAML, gent.ErrInvalidYAML, nil)

		limit := tt.ExactLimit(gent.SGSectionParseErrorConsecutive, 2)
		limits := []gent.Limit{limit}

		execCtx := runWithLimitAndThinking(t, model, format, toolChain, termination,
			thinkingSection, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())
		assert.Equal(t, 5, execCtx.Iteration())

		// Build expected NextPrompt for tool execution
		toolObs := tt.ToolObservation(format, toolChain, "test", "tool executed")

		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: thinking OK, tool call
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.BeforeToolCall(0, 1, "test", nil),
			tt.AfterToolCall(0, 1, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolObs)),
			// Iteration 2: thinking OK, tool call
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.BeforeToolCall(0, 2, "test", nil),
			tt.AfterToolCall(0, 2, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(toolObs)),
			// Iteration 3: thinking error (consecutive: 1), tool call
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.ParseError(0, 3, gent.ParseErrorTypeSection, "bad"),
			tt.BeforeToolCall(0, 3, "test", nil),
			tt.AfterToolCall(0, 3, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(toolObs)),
			// Iteration 4: thinking error (consecutive: 2), tool call
			tt.BeforeIter(0, 4),
			tt.BeforeModelCall(0, 4, "test-model"),
			tt.AfterModelCall(0, 4, "test-model", 100, 50),
			tt.ParseError(0, 4, gent.ParseErrorTypeSection, "bad"),
			tt.BeforeToolCall(0, 4, "test", nil),
			tt.AfterToolCall(0, 4, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 4, tt.ContinueWithPrompt(toolObs)),
			// Iteration 5: thinking error (consecutive: 3 > 2 EXCEEDED), tool call
			tt.BeforeIter(0, 5),
			tt.BeforeModelCall(0, 5, "test-model"),
			tt.AfterModelCall(0, 5, "test-model", 100, 50),
			tt.ParseError(0, 5, gent.ParseErrorTypeSection, "bad"),
			tt.LimitExceeded(0, 5, limit, 3, gent.SGSectionParseErrorConsecutive),
			tt.BeforeToolCall(0, 5, "test", nil),
			tt.AfterToolCall(0, 5, "test", nil, "tool executed", nil),
			tt.AfterIter(0, 5, tt.ContinueWithPrompt(toolObs)),
			tt.AfterExec(0, 5, gent.TerminationLimitExceeded),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})
}

// ----------------------------------------------------------------------------
// Test: Tool call error limits
// ----------------------------------------------------------------------------

func TestExecutorLimits_ToolCallsErrorTotal(t *testing.T) {
	t.Run("stops when tool call error total exceeded", func(t *testing.T) {
		model := tt.NewMockModel().
			AddResponse("<action>tool: failing</action>", 100, 50).
			AddResponse("<action>tool: failing</action>", 100, 50).
			AddResponse("<action>tool: failing</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: failing"}}).
			AddParseResult(map[string][]string{"action": {"tool: failing"}}).
			AddParseResult(map[string][]string{"action": {"tool: failing"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolErr := errors.New("tool failed")
		toolChain := tt.NewMockToolChain().
			WithTool("failing", func(args map[string]any) (string, error) {
				return "", toolErr
			})
		termination := tt.NewMockTermination()

		limit := tt.ExactLimit(gent.SCToolCallsErrorTotal, 2)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())

		// Build expected NextPrompt for tool error (empty output)
		toolErrObs := tt.ToolObservation(format, toolChain, "failing", "")

		// Assert events: 3 iterations with tool call errors, limit exceeded at 3rd
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: tool error (count: 1)
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.BeforeToolCall(0, 1, "failing", nil),
			tt.AfterToolCall(0, 1, "failing", nil, "", toolErr),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolErrObs)),
			// Iteration 2: tool error (count: 2)
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.BeforeToolCall(0, 2, "failing", nil),
			tt.AfterToolCall(0, 2, "failing", nil, "", toolErr),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(toolErrObs)),
			// Iteration 3: tool error (count: 3 > limit 2)
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.BeforeToolCall(0, 3, "failing", nil),
			tt.AfterToolCall(0, 3, "failing", nil, "", toolErr),
			tt.LimitExceeded(0, 3, limit, 3, gent.SCToolCallsErrorTotal),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(toolErrObs)),
			tt.AfterExec(0, 3, gent.TerminationLimitExceeded),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})

	t.Run("exceeds limit at third iteration after successful iterations", func(t *testing.T) {
		// Iteration 1-2: successful tool calls
		// Iteration 3-5: failing tool calls (3rd error exceeds limit of 2)
		callCount := 0
		toolErr := errors.New("tool failed")
		model := tt.NewMockModel().
			AddResponse("<action>tool: maybe</action>", 100, 50).
			AddResponse("<action>tool: maybe</action>", 100, 50).
			AddResponse("<action>tool: maybe</action>", 100, 50).
			AddResponse("<action>tool: maybe</action>", 100, 50).
			AddResponse("<action>tool: maybe</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: maybe"}}).
			AddParseResult(map[string][]string{"action": {"tool: maybe"}}).
			AddParseResult(map[string][]string{"action": {"tool: maybe"}}).
			AddParseResult(map[string][]string{"action": {"tool: maybe"}}).
			AddParseResult(map[string][]string{"action": {"tool: maybe"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain().
			WithTool("maybe", func(args map[string]any) (string, error) {
				callCount++
				if callCount <= 2 {
					return "success", nil
				}
				return "", toolErr
			})
		termination := tt.NewMockTermination()

		limit := tt.ExactLimit(gent.SCToolCallsErrorTotal, 2)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())
		assert.Equal(t, 5, execCtx.Iteration())

		// Build expected NextPrompts
		successObs := tt.ToolObservation(format, toolChain, "maybe", "success")
		toolErrObs := tt.ToolObservation(format, toolChain, "maybe", "")

		// Assert events: 2 successful iterations, then 3 with tool errors
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: successful tool call
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.BeforeToolCall(0, 1, "maybe", nil),
			tt.AfterToolCall(0, 1, "maybe", nil, "success", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(successObs)),
			// Iteration 2: successful tool call
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.BeforeToolCall(0, 2, "maybe", nil),
			tt.AfterToolCall(0, 2, "maybe", nil, "success", nil),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(successObs)),
			// Iteration 3: tool error (count: 1)
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.BeforeToolCall(0, 3, "maybe", nil),
			tt.AfterToolCall(0, 3, "maybe", nil, "", toolErr),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(toolErrObs)),
			// Iteration 4: tool error (count: 2)
			tt.BeforeIter(0, 4),
			tt.BeforeModelCall(0, 4, "test-model"),
			tt.AfterModelCall(0, 4, "test-model", 100, 50),
			tt.BeforeToolCall(0, 4, "maybe", nil),
			tt.AfterToolCall(0, 4, "maybe", nil, "", toolErr),
			tt.AfterIter(0, 4, tt.ContinueWithPrompt(toolErrObs)),
			// Iteration 5: tool error (count: 3 > limit 2)
			tt.BeforeIter(0, 5),
			tt.BeforeModelCall(0, 5, "test-model"),
			tt.AfterModelCall(0, 5, "test-model", 100, 50),
			tt.BeforeToolCall(0, 5, "maybe", nil),
			tt.AfterToolCall(0, 5, "maybe", nil, "", toolErr),
			tt.LimitExceeded(0, 5, limit, 3, gent.SCToolCallsErrorTotal),
			tt.AfterIter(0, 5, tt.ContinueWithPrompt(toolErrObs)),
			tt.AfterExec(0, 5, gent.TerminationLimitExceeded),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})
}

func TestExecutorLimits_ToolCallsErrorForTool(t *testing.T) {
	t.Run("stops when tool-specific error limit exceeded (prefix)", func(t *testing.T) {
		model := tt.NewMockModel().
			AddResponse("<action>tool: broken</action>", 100, 50).
			AddResponse("<action>tool: broken</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: broken"}}).
			AddParseResult(map[string][]string{"action": {"tool: broken"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolErr := errors.New("broken tool")
		toolChain := tt.NewMockToolChain().
			WithTool("broken", func(args map[string]any) (string, error) {
				return "", toolErr
			})
		termination := tt.NewMockTermination()

		limit := tt.PrefixLimit(gent.SCToolCallsErrorFor, 1)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		require.NotNil(t, execCtx.ExceededLimit())

		// Build expected NextPrompt for tool error (empty output)
		toolErrObs := tt.ToolObservation(format, toolChain, "broken", "")

		// Assert events: 2 iterations with tool errors, limit exceeded at 2nd
		matchedKey := gent.SCToolCallsErrorFor + "broken"
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: tool error (count: 1)
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.BeforeToolCall(0, 1, "broken", nil),
			tt.AfterToolCall(0, 1, "broken", nil, "", toolErr),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolErrObs)),
			// Iteration 2: tool error (count: 2 > limit 1)
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.BeforeToolCall(0, 2, "broken", nil),
			tt.AfterToolCall(0, 2, "broken", nil, "", toolErr),
			tt.LimitExceeded(0, 2, limit, 2, matchedKey),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(toolErrObs)),
			tt.AfterExec(0, 2, gent.TerminationLimitExceeded),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})

	t.Run("prefix limit exceeds at third iteration after successful iterations", func(t *testing.T) {
		// Iteration 1-2: successful tool calls
		// Iteration 3-4: failing tool calls (2nd error exceeds limit of 1)
		callCount := 0
		toolErr := errors.New("broken tool")
		model := tt.NewMockModel().
			AddResponse("<action>tool: broken</action>", 100, 50).
			AddResponse("<action>tool: broken</action>", 100, 50).
			AddResponse("<action>tool: broken</action>", 100, 50).
			AddResponse("<action>tool: broken</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: broken"}}).
			AddParseResult(map[string][]string{"action": {"tool: broken"}}).
			AddParseResult(map[string][]string{"action": {"tool: broken"}}).
			AddParseResult(map[string][]string{"action": {"tool: broken"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain().
			WithTool("broken", func(args map[string]any) (string, error) {
				callCount++
				if callCount <= 2 {
					return "success", nil
				}
				return "", toolErr
			})
		termination := tt.NewMockTermination()

		limit := tt.PrefixLimit(gent.SCToolCallsErrorFor, 1)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		require.NotNil(t, execCtx.ExceededLimit())
		assert.Equal(t, 4, execCtx.Iteration())

		// Build expected NextPrompts
		successObs := tt.ToolObservation(format, toolChain, "broken", "success")
		toolErrObs := tt.ToolObservation(format, toolChain, "broken", "")

		// Assert events: 2 successful iterations, then 2 with tool errors
		matchedKey := gent.SCToolCallsErrorFor + "broken"
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: successful tool call
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.BeforeToolCall(0, 1, "broken", nil),
			tt.AfterToolCall(0, 1, "broken", nil, "success", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(successObs)),
			// Iteration 2: successful tool call
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.BeforeToolCall(0, 2, "broken", nil),
			tt.AfterToolCall(0, 2, "broken", nil, "success", nil),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(successObs)),
			// Iteration 3: tool error (count: 1)
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.BeforeToolCall(0, 3, "broken", nil),
			tt.AfterToolCall(0, 3, "broken", nil, "", toolErr),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(toolErrObs)),
			// Iteration 4: tool error (count: 2 > limit 1)
			tt.BeforeIter(0, 4),
			tt.BeforeModelCall(0, 4, "test-model"),
			tt.AfterModelCall(0, 4, "test-model", 100, 50),
			tt.BeforeToolCall(0, 4, "broken", nil),
			tt.AfterToolCall(0, 4, "broken", nil, "", toolErr),
			tt.LimitExceeded(0, 4, limit, 2, matchedKey),
			tt.AfterIter(0, 4, tt.ContinueWithPrompt(toolErrObs)),
			tt.AfterExec(0, 4, gent.TerminationLimitExceeded),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})
}

func TestExecutorLimits_ToolCallsErrorConsecutive(t *testing.T) {
	t.Run("stops when tool call error consecutive exceeded", func(t *testing.T) {
		model := tt.NewMockModel().
			AddResponse("<action>tool: failing</action>", 100, 50).
			AddResponse("<action>tool: failing</action>", 100, 50).
			AddResponse("<action>tool: failing</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: failing"}}).
			AddParseResult(map[string][]string{"action": {"tool: failing"}}).
			AddParseResult(map[string][]string{"action": {"tool: failing"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolErr := errors.New("tool failed")
		toolChain := tt.NewMockToolChain().
			WithTool("failing", func(args map[string]any) (string, error) {
				return "", toolErr
			})
		termination := tt.NewMockTermination()

		limit := tt.ExactLimit(gent.SGToolCallsErrorConsecutive, 2)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())

		// Build expected NextPrompt for tool error (empty output)
		toolErrObs := tt.ToolObservation(format, toolChain, "failing", "")

		// Assert events: 3 iterations with consecutive tool errors, limit exceeded at 3rd
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: tool error (consecutive: 1)
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.BeforeToolCall(0, 1, "failing", nil),
			tt.AfterToolCall(0, 1, "failing", nil, "", toolErr),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolErrObs)),
			// Iteration 2: tool error (consecutive: 2)
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.BeforeToolCall(0, 2, "failing", nil),
			tt.AfterToolCall(0, 2, "failing", nil, "", toolErr),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(toolErrObs)),
			// Iteration 3: tool error (consecutive: 3 > limit 2)
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.BeforeToolCall(0, 3, "failing", nil),
			tt.AfterToolCall(0, 3, "failing", nil, "", toolErr),
			tt.LimitExceeded(0, 3, limit, 3, gent.SGToolCallsErrorConsecutive),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(toolErrObs)),
			tt.AfterExec(0, 3, gent.TerminationLimitExceeded),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})

	t.Run("exceeds consecutive limit at third iteration after successful iterations", func(t *testing.T) {
		// Iteration 1-2: successful tool calls
		// Iteration 3-5: consecutive failing tool calls (3rd error exceeds limit of 2)
		callCount := 0
		toolErr := errors.New("tool failed")
		model := tt.NewMockModel().
			AddResponse("<action>tool: maybe</action>", 100, 50).
			AddResponse("<action>tool: maybe</action>", 100, 50).
			AddResponse("<action>tool: maybe</action>", 100, 50).
			AddResponse("<action>tool: maybe</action>", 100, 50).
			AddResponse("<action>tool: maybe</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: maybe"}}).
			AddParseResult(map[string][]string{"action": {"tool: maybe"}}).
			AddParseResult(map[string][]string{"action": {"tool: maybe"}}).
			AddParseResult(map[string][]string{"action": {"tool: maybe"}}).
			AddParseResult(map[string][]string{"action": {"tool: maybe"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain().
			WithTool("maybe", func(args map[string]any) (string, error) {
				callCount++
				if callCount <= 2 {
					return "success", nil
				}
				return "", toolErr
			})
		termination := tt.NewMockTermination()

		limit := tt.ExactLimit(gent.SGToolCallsErrorConsecutive, 2)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())
		assert.Equal(t, 5, execCtx.Iteration())

		// Build expected NextPrompts
		successObs := tt.ToolObservation(format, toolChain, "maybe", "success")
		toolErrObs := tt.ToolObservation(format, toolChain, "maybe", "")

		// Assert events: 2 successful iterations, then 3 consecutive tool errors
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: successful tool call
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.BeforeToolCall(0, 1, "maybe", nil),
			tt.AfterToolCall(0, 1, "maybe", nil, "success", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(successObs)),
			// Iteration 2: successful tool call
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.BeforeToolCall(0, 2, "maybe", nil),
			tt.AfterToolCall(0, 2, "maybe", nil, "success", nil),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(successObs)),
			// Iteration 3: tool error (consecutive: 1)
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.BeforeToolCall(0, 3, "maybe", nil),
			tt.AfterToolCall(0, 3, "maybe", nil, "", toolErr),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(toolErrObs)),
			// Iteration 4: tool error (consecutive: 2)
			tt.BeforeIter(0, 4),
			tt.BeforeModelCall(0, 4, "test-model"),
			tt.AfterModelCall(0, 4, "test-model", 100, 50),
			tt.BeforeToolCall(0, 4, "maybe", nil),
			tt.AfterToolCall(0, 4, "maybe", nil, "", toolErr),
			tt.AfterIter(0, 4, tt.ContinueWithPrompt(toolErrObs)),
			// Iteration 5: tool error (consecutive: 3 > limit 2)
			tt.BeforeIter(0, 5),
			tt.BeforeModelCall(0, 5, "test-model"),
			tt.AfterModelCall(0, 5, "test-model", 100, 50),
			tt.BeforeToolCall(0, 5, "maybe", nil),
			tt.AfterToolCall(0, 5, "maybe", nil, "", toolErr),
			tt.LimitExceeded(0, 5, limit, 3, gent.SGToolCallsErrorConsecutive),
			tt.AfterIter(0, 5, tt.ContinueWithPrompt(toolErrObs)),
			tt.AfterExec(0, 5, gent.TerminationLimitExceeded),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})
}

func TestExecutorLimits_ToolCallsErrorConsecutiveForTool(t *testing.T) {
	t.Run("stops when tool-specific consecutive error limit exceeded (prefix)", func(t *testing.T) {
		model := tt.NewMockModel().
			AddResponse("<action>tool: flaky</action>", 100, 50).
			AddResponse("<action>tool: flaky</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: flaky"}}).
			AddParseResult(map[string][]string{"action": {"tool: flaky"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolErr := errors.New("flaky error")
		toolChain := tt.NewMockToolChain().
			WithTool("flaky", func(args map[string]any) (string, error) {
				return "", toolErr
			})
		termination := tt.NewMockTermination()

		limit := tt.PrefixLimit(gent.SGToolCallsErrorConsecutiveFor, 1)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		require.NotNil(t, execCtx.ExceededLimit())

		// Build expected NextPrompt for tool error (empty output)
		toolErrObs := tt.ToolObservation(format, toolChain, "flaky", "")

		// Assert events: 2 iterations with consecutive tool errors, limit exceeded at 2nd
		matchedKey := gent.SGToolCallsErrorConsecutiveFor + "flaky"
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: tool error (consecutive: 1)
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.BeforeToolCall(0, 1, "flaky", nil),
			tt.AfterToolCall(0, 1, "flaky", nil, "", toolErr),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolErrObs)),
			// Iteration 2: tool error (consecutive: 2 > limit 1)
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.BeforeToolCall(0, 2, "flaky", nil),
			tt.AfterToolCall(0, 2, "flaky", nil, "", toolErr),
			tt.LimitExceeded(0, 2, limit, 2, matchedKey),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(toolErrObs)),
			tt.AfterExec(0, 2, gent.TerminationLimitExceeded),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})

	t.Run("prefix consecutive limit exceeds at third iteration after successful iterations", func(t *testing.T) {
		// Iteration 1-2: successful tool calls
		// Iteration 3-4: consecutive failing tool calls (2nd error exceeds limit of 1)
		callCount := 0
		toolErr := errors.New("flaky error")
		model := tt.NewMockModel().
			AddResponse("<action>tool: flaky</action>", 100, 50).
			AddResponse("<action>tool: flaky</action>", 100, 50).
			AddResponse("<action>tool: flaky</action>", 100, 50).
			AddResponse("<action>tool: flaky</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: flaky"}}).
			AddParseResult(map[string][]string{"action": {"tool: flaky"}}).
			AddParseResult(map[string][]string{"action": {"tool: flaky"}}).
			AddParseResult(map[string][]string{"action": {"tool: flaky"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain().
			WithTool("flaky", func(args map[string]any) (string, error) {
				callCount++
				if callCount <= 2 {
					return "success", nil
				}
				return "", toolErr
			})
		termination := tt.NewMockTermination()

		limit := tt.PrefixLimit(gent.SGToolCallsErrorConsecutiveFor, 1)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		require.NotNil(t, execCtx.ExceededLimit())
		assert.Equal(t, 4, execCtx.Iteration())

		// Build expected NextPrompts
		successObs := tt.ToolObservation(format, toolChain, "flaky", "success")
		toolErrObs := tt.ToolObservation(format, toolChain, "flaky", "")

		// Assert events: 2 successful iterations, then 2 consecutive tool errors
		matchedKey := gent.SGToolCallsErrorConsecutiveFor + "flaky"
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: successful tool call
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.BeforeToolCall(0, 1, "flaky", nil),
			tt.AfterToolCall(0, 1, "flaky", nil, "success", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(successObs)),
			// Iteration 2: successful tool call
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.BeforeToolCall(0, 2, "flaky", nil),
			tt.AfterToolCall(0, 2, "flaky", nil, "success", nil),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(successObs)),
			// Iteration 3: tool error (consecutive: 1)
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.BeforeToolCall(0, 3, "flaky", nil),
			tt.AfterToolCall(0, 3, "flaky", nil, "", toolErr),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(toolErrObs)),
			// Iteration 4: tool error (consecutive: 2 > limit 1)
			tt.BeforeIter(0, 4),
			tt.BeforeModelCall(0, 4, "test-model"),
			tt.AfterModelCall(0, 4, "test-model", 100, 50),
			tt.BeforeToolCall(0, 4, "flaky", nil),
			tt.AfterToolCall(0, 4, "flaky", nil, "", toolErr),
			tt.LimitExceeded(0, 4, limit, 2, matchedKey),
			tt.AfterIter(0, 4, tt.ContinueWithPrompt(toolErrObs)),
			tt.AfterExec(0, 4, gent.TerminationLimitExceeded),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})
}

// ----------------------------------------------------------------------------
// Test: Consecutive error reset scenarios
// These test that consecutive counters reset on success and don't trigger
// the limit when: fail, fail, success, fail (3 failures but max consecutive is 2)
// ----------------------------------------------------------------------------

func TestExecutorLimits_ConsecutiveReset_FormatParseError(t *testing.T) {
	t.Run("consecutive counter resets on success - does not exceed limit", func(t *testing.T) {
		// Sequence: fail, fail, success, fail
		// With limit of 2, this should NOT trigger because consecutive resets after success
		model := tt.NewMockModel().
			AddResponse("invalid1", 100, 50).                 // fail
			AddResponse("invalid2", 100, 50).                 // fail (consecutive=2)
			AddResponse("<action>tool: t</action>", 100, 50). // success (resets)
			AddResponse("invalid3", 100, 50).                 // fail (consecutive=1)
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseError(gent.ErrNoSectionsFound).                     // fail
			AddParseError(gent.ErrNoSectionsFound).                     // fail
			AddParseResult(map[string][]string{"action": {"tool: t"}}). // success
			AddParseError(gent.ErrNoSectionsFound).                     // fail
			AddParseResult(map[string][]string{"answer": {"done"}})     // terminate

		toolChain := tt.NewMockToolChain()
		termination := tt.NewMockTermination()

		limit := tt.ExactLimit(gent.SGFormatParseErrorConsecutive, 2)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		// Should complete successfully because consecutive never exceeded 2
		assert.Equal(t, gent.TerminationSuccess, execCtx.TerminationReason())
		assert.Nil(t, execCtx.ExceededLimit())

		// Build expected NextPrompts
		parseObs1 := tt.FormatParseErrorObservation(format, gent.ErrNoSectionsFound, "invalid1")
		parseObs2 := tt.FormatParseErrorObservation(format, gent.ErrNoSectionsFound, "invalid2")
		toolObs := tt.ToolObservation(format, toolChain, "t", "tool executed")
		parseObs3 := tt.FormatParseErrorObservation(format, gent.ErrNoSectionsFound, "invalid3")

		// Assert events: fail, fail, success (tool), fail, success (answer)
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: parse error (consecutive: 1)
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.ParseError(0, 1, gent.ParseErrorTypeFormat, "invalid1"),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(parseObs1)),
			// Iteration 2: parse error (consecutive: 2, at limit but not exceeded)
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.ParseError(0, 2, gent.ParseErrorTypeFormat, "invalid2"),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(parseObs2)),
			// Iteration 3: success - tool call (consecutive resets)
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.BeforeToolCall(0, 3, "t", nil),
			tt.AfterToolCall(0, 3, "t", nil, "tool executed", nil),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(toolObs)),
			// Iteration 4: parse error (consecutive: 1, reset from success)
			tt.BeforeIter(0, 4),
			tt.BeforeModelCall(0, 4, "test-model"),
			tt.AfterModelCall(0, 4, "test-model", 100, 50),
			tt.ParseError(0, 4, gent.ParseErrorTypeFormat, "invalid3"),
			tt.AfterIter(0, 4, tt.ContinueWithPrompt(parseObs3)),
			// Iteration 5: success - terminate
			tt.BeforeIter(0, 5),
			tt.BeforeModelCall(0, 5, "test-model"),
			tt.AfterModelCall(0, 5, "test-model", 100, 50),
			tt.AfterIter(0, 5, tt.Terminate("done")),
			tt.AfterExec(0, 5, gent.TerminationSuccess),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})
}

func TestExecutorLimits_ConsecutiveReset_ToolchainParseError(t *testing.T) {
	t.Run("consecutive counter resets on success - does not exceed limit", func(t *testing.T) {
		// Sequence: fail, fail, success, fail
		model := tt.NewMockModel().
			AddResponse("<action>bad1</action>", 100, 50).
			AddResponse("<action>bad2</action>", 100, 50).
			AddResponse("<action>tool: good</action>", 100, 50).
			AddResponse("<action>bad3</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"bad1"}}).
			AddParseResult(map[string][]string{"action": {"bad2"}}).
			AddParseResult(map[string][]string{"action": {"tool: good"}}).
			AddParseResult(map[string][]string{"action": {"bad3"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain().
			WithParseErrors(
				gent.ErrInvalidJSON, // fail
				gent.ErrInvalidJSON, // fail (consecutive=2)
				nil,                 // success (resets)
				gent.ErrInvalidJSON, // fail (consecutive=1)
				nil,                 // N/A
			)
		termination := tt.NewMockTermination()

		limit := tt.ExactLimit(gent.SGToolchainParseErrorConsecutive, 2)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationSuccess, execCtx.TerminationReason())
		assert.Nil(t, execCtx.ExceededLimit())

		// Build expected NextPrompts
		errObs := tt.ToolchainErrorObservation(format, gent.ErrInvalidJSON)
		toolObs := tt.ToolObservation(format, toolChain, "good", "tool executed")

		// Assert events: fail, fail, success (tool), fail, success (answer)
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: toolchain parse error (consecutive: 1)
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.ParseError(0, 1, gent.ParseErrorTypeToolchain, "bad1"),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(errObs)),
			// Iteration 2: toolchain parse error (consecutive: 2, at limit)
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.ParseError(0, 2, gent.ParseErrorTypeToolchain, "bad2"),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(errObs)),
			// Iteration 3: success - tool call (consecutive resets)
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.BeforeToolCall(0, 3, "good", nil),
			tt.AfterToolCall(0, 3, "good", nil, "tool executed", nil),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(toolObs)),
			// Iteration 4: toolchain parse error (consecutive: 1, reset)
			tt.BeforeIter(0, 4),
			tt.BeforeModelCall(0, 4, "test-model"),
			tt.AfterModelCall(0, 4, "test-model", 100, 50),
			tt.ParseError(0, 4, gent.ParseErrorTypeToolchain, "bad3"),
			tt.AfterIter(0, 4, tt.ContinueWithPrompt(errObs)),
			// Iteration 5: success - terminate
			tt.BeforeIter(0, 5),
			tt.BeforeModelCall(0, 5, "test-model"),
			tt.AfterModelCall(0, 5, "test-model", 100, 50),
			tt.AfterIter(0, 5, tt.Terminate("done")),
			tt.AfterExec(0, 5, gent.TerminationSuccess),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})
}

func TestExecutorLimits_ConsecutiveReset_TerminationParseError(t *testing.T) {
	t.Run("consecutive counter resets on parse success - does not exceed limit", func(t *testing.T) {
		// Termination parse error consecutive resets when termination parsing succeeds.
		// Since successful termination parsing ends execution, we use a validator to reject
		// the answer and continue the loop. This tests: fail, fail, success+reject, fail, done
		model := tt.NewMockModel().
			AddResponse("<answer>bad1</answer>", 100, 50).       // parse error
			AddResponse("<answer>bad2</answer>", 100, 50).       // parse error (consecutive=2)
			AddResponse("<answer>rejected</answer>", 100, 50).   // parse OK, validator rejects
			AddResponse("<answer>bad3</answer>", 100, 50).       // parse error (consecutive=1)
			AddResponse("<answer>accepted</answer>", 100, 50)    // parse OK, validator accepts

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"answer": {"bad1"}}).
			AddParseResult(map[string][]string{"answer": {"bad2"}}).
			AddParseResult(map[string][]string{"answer": {"rejected"}}).
			AddParseResult(map[string][]string{"answer": {"bad3"}}).
			AddParseResult(map[string][]string{"answer": {"accepted"}})

		toolChain := tt.NewMockToolChain()
		termination := tt.NewMockTermination().
			WithParseErrors(
				gent.ErrInvalidJSON, // iter 1: fail (consecutive=1)
				gent.ErrInvalidJSON, // iter 2: fail (consecutive=2, at limit)
				nil,                 // iter 3: success (consecutive resets)
				gent.ErrInvalidJSON, // iter 4: fail (consecutive=1, after reset)
				nil,                 // iter 5: success
			)

		// Validator rejects first valid answer, accepts second
		validator := tt.NewMockValidator("test_validator").
			WithAcceptances(false, true).
			WithFeedback(gent.FormattedSection{Name: "error", Content: "Answer rejected"})
		termination.SetValidator(validator)

		limit := tt.ExactLimit(gent.SGTerminationParseErrorConsecutive, 2)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationSuccess, execCtx.TerminationReason())
		assert.Nil(t, execCtx.ExceededLimit())

		// Build expected NextPrompts
		termObs1 := tt.TerminationParseErrorObservation(format, gent.ErrInvalidJSON, "bad1")
		termObs2 := tt.TerminationParseErrorObservation(format, gent.ErrInvalidJSON, "bad2")
		rejectObs := tt.ValidatorFeedbackObservation(format,
			gent.FormattedSection{Name: "error", Content: "Answer rejected"})
		termObs3 := tt.TerminationParseErrorObservation(format, gent.ErrInvalidJSON, "bad3")

		// Assert events: fail, fail, success+reject, fail, success+accept
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: termination parse error (consecutive: 1)
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.ParseError(0, 1, gent.ParseErrorTypeTermination, "bad1"),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(termObs1)),
			// Iteration 2: termination parse error (consecutive: 2, at limit)
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.ParseError(0, 2, gent.ParseErrorTypeTermination, "bad2"),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(termObs2)),
			// Iteration 3: termination parse success, validator rejects (consecutive resets)
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.ValidatorCalled(0, 3, "test_validator", "rejected"),
			tt.ValidatorResult(0, 3, "test_validator", "rejected", false,
				[]gent.FormattedSection{{Name: "error", Content: "Answer rejected"}}),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(rejectObs)),
			// Iteration 4: termination parse error (consecutive: 1, reset from success)
			tt.BeforeIter(0, 4),
			tt.BeforeModelCall(0, 4, "test-model"),
			tt.AfterModelCall(0, 4, "test-model", 100, 50),
			tt.ParseError(0, 4, gent.ParseErrorTypeTermination, "bad3"),
			tt.AfterIter(0, 4, tt.ContinueWithPrompt(termObs3)),
			// Iteration 5: termination parse success, validator accepts
			tt.BeforeIter(0, 5),
			tt.BeforeModelCall(0, 5, "test-model"),
			tt.AfterModelCall(0, 5, "test-model", 100, 50),
			tt.ValidatorCalled(0, 5, "test_validator", "accepted"),
			tt.ValidatorResult(0, 5, "test_validator", "accepted", true, nil),
			tt.AfterIter(0, 5, tt.Terminate("accepted")),
			tt.AfterExec(0, 5, gent.TerminationSuccess),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})
}

func TestExecutorLimits_ConsecutiveReset_SectionParseError(t *testing.T) {
	t.Run("consecutive counter resets on success - does not exceed limit", func(t *testing.T) {
		// Sequence: fail, fail, success, fail, success
		// With limit of 2, this should NOT trigger because consecutive resets after success
		// Iterations: 1 (fail consec=1), 2 (fail consec=2), 3 (success resets),
		//             4 (fail consec=1), 5 (success terminates)
		model := tt.NewMockModel().
			AddResponse("<thinking>bad1</thinking><action>tool: t</action>", 100, 50).
			AddResponse("<thinking>bad2</thinking><action>tool: t</action>", 100, 50).
			AddResponse("<thinking>good</thinking><action>tool: t</action>", 100, 50).
			AddResponse("<thinking>bad3</thinking><action>tool: t</action>", 100, 50).
			AddResponse("<thinking>good</thinking><answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"thinking": {"bad1"}, "action": {"tool: t"}}).
			AddParseResult(map[string][]string{"thinking": {"bad2"}, "action": {"tool: t"}}).
			AddParseResult(map[string][]string{"thinking": {"good"}, "action": {"tool: t"}}).
			AddParseResult(map[string][]string{"thinking": {"bad3"}, "action": {"tool: t"}}).
			AddParseResult(map[string][]string{"thinking": {"good"}, "answer": {"done"}})

		toolChain := tt.NewMockToolChain()
		termination := tt.NewMockTermination()

		// MockSection: fail, fail, success, fail, success
		thinkingSection := tt.NewMockSection("thinking").
			WithParseErrors(
				gent.ErrInvalidYAML, // iter 1: fail (consecutive=1)
				gent.ErrInvalidYAML, // iter 2: fail (consecutive=2, at limit)
				nil,                 // iter 3: success (consecutive resets)
				gent.ErrInvalidYAML, // iter 4: fail (consecutive=1, after reset)
				nil,                 // iter 5: success
			)

		limit := tt.ExactLimit(gent.SGSectionParseErrorConsecutive, 2)
		limits := []gent.Limit{limit}

		execCtx := runWithLimitAndThinking(t, model, format, toolChain, termination,
			thinkingSection, limits)

		// Should complete successfully because consecutive never exceeded 2
		assert.Equal(t, gent.TerminationSuccess, execCtx.TerminationReason())
		assert.Nil(t, execCtx.ExceededLimit())
		assert.Equal(t, 5, execCtx.Iteration())

		// Build expected NextPrompt for tool execution
		toolObs := tt.ToolObservation(format, toolChain, "t", "tool executed")

		// Assert events: fail, fail, success (tool), fail (tool), success (answer)
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: section parse error (consecutive: 1), tool call
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.ParseError(0, 1, gent.ParseErrorTypeSection, "bad1"),
			tt.BeforeToolCall(0, 1, "t", nil),
			tt.AfterToolCall(0, 1, "t", nil, "tool executed", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolObs)),
			// Iteration 2: section parse error (consecutive: 2, at limit but not exceeded)
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.ParseError(0, 2, gent.ParseErrorTypeSection, "bad2"),
			tt.BeforeToolCall(0, 2, "t", nil),
			tt.AfterToolCall(0, 2, "t", nil, "tool executed", nil),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(toolObs)),
			// Iteration 3: section parse success (consecutive resets), tool call
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.BeforeToolCall(0, 3, "t", nil),
			tt.AfterToolCall(0, 3, "t", nil, "tool executed", nil),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(toolObs)),
			// Iteration 4: section parse error (consecutive: 1, reset from success), tool call
			tt.BeforeIter(0, 4),
			tt.BeforeModelCall(0, 4, "test-model"),
			tt.AfterModelCall(0, 4, "test-model", 100, 50),
			tt.ParseError(0, 4, gent.ParseErrorTypeSection, "bad3"),
			tt.BeforeToolCall(0, 4, "t", nil),
			tt.AfterToolCall(0, 4, "t", nil, "tool executed", nil),
			tt.AfterIter(0, 4, tt.ContinueWithPrompt(toolObs)),
			// Iteration 5: section parse success, terminate
			tt.BeforeIter(0, 5),
			tt.BeforeModelCall(0, 5, "test-model"),
			tt.AfterModelCall(0, 5, "test-model", 100, 50),
			tt.AfterIter(0, 5, tt.Terminate("done")),
			tt.AfterExec(0, 5, gent.TerminationSuccess),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})
}

func TestExecutorLimits_ConsecutiveReset_ToolCallError(t *testing.T) {
	t.Run("consecutive counter resets on success - does not exceed limit", func(t *testing.T) {
		callCount := 0
		toolErr := errors.New("flaky error")
		// Sequence: fail, fail, success, fail
		model := tt.NewMockModel().
			AddResponse("<action>tool: flaky</action>", 100, 50).
			AddResponse("<action>tool: flaky</action>", 100, 50).
			AddResponse("<action>tool: flaky</action>", 100, 50).
			AddResponse("<action>tool: flaky</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: flaky"}}).
			AddParseResult(map[string][]string{"action": {"tool: flaky"}}).
			AddParseResult(map[string][]string{"action": {"tool: flaky"}}).
			AddParseResult(map[string][]string{"action": {"tool: flaky"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain().
			WithTool("flaky", func(args map[string]any) (string, error) {
				callCount++
				// fail, fail, success, fail
				if callCount == 3 {
					return "success", nil
				}
				return "", toolErr
			})
		termination := tt.NewMockTermination()

		limit := tt.ExactLimit(gent.SGToolCallsErrorConsecutive, 2)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationSuccess, execCtx.TerminationReason())
		assert.Nil(t, execCtx.ExceededLimit())

		// Build expected NextPrompts
		toolErrObs := tt.ToolObservation(format, toolChain, "flaky", "")
		successObs := tt.ToolObservation(format, toolChain, "flaky", "success")

		// Assert events: fail, fail, success, fail, success (answer)
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: tool error (consecutive: 1)
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.BeforeToolCall(0, 1, "flaky", nil),
			tt.AfterToolCall(0, 1, "flaky", nil, "", toolErr),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolErrObs)),
			// Iteration 2: tool error (consecutive: 2, at limit)
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.BeforeToolCall(0, 2, "flaky", nil),
			tt.AfterToolCall(0, 2, "flaky", nil, "", toolErr),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(toolErrObs)),
			// Iteration 3: tool success (consecutive resets)
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.BeforeToolCall(0, 3, "flaky", nil),
			tt.AfterToolCall(0, 3, "flaky", nil, "success", nil),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(successObs)),
			// Iteration 4: tool error (consecutive: 1, reset)
			tt.BeforeIter(0, 4),
			tt.BeforeModelCall(0, 4, "test-model"),
			tt.AfterModelCall(0, 4, "test-model", 100, 50),
			tt.BeforeToolCall(0, 4, "flaky", nil),
			tt.AfterToolCall(0, 4, "flaky", nil, "", toolErr),
			tt.AfterIter(0, 4, tt.ContinueWithPrompt(toolErrObs)),
			// Iteration 5: terminate
			tt.BeforeIter(0, 5),
			tt.BeforeModelCall(0, 5, "test-model"),
			tt.AfterModelCall(0, 5, "test-model", 100, 50),
			tt.AfterIter(0, 5, tt.Terminate("done")),
			tt.AfterExec(0, 5, gent.TerminationSuccess),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})
}

func TestExecutorLimits_ConsecutiveReset_ToolCallErrorPerTool(t *testing.T) {
	t.Run("per-tool consecutive counter resets on success - does not exceed prefix limit", func(t *testing.T) {
		callCount := 0
		toolErr := errors.New("specific error")
		// Sequence for specific tool: fail, fail, success, fail
		model := tt.NewMockModel().
			AddResponse("<action>tool: specific</action>", 100, 50).
			AddResponse("<action>tool: specific</action>", 100, 50).
			AddResponse("<action>tool: specific</action>", 100, 50).
			AddResponse("<action>tool: specific</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: specific"}}).
			AddParseResult(map[string][]string{"action": {"tool: specific"}}).
			AddParseResult(map[string][]string{"action": {"tool: specific"}}).
			AddParseResult(map[string][]string{"action": {"tool: specific"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := tt.NewMockToolChain().
			WithTool("specific", func(args map[string]any) (string, error) {
				callCount++
				if callCount == 3 {
					return "success", nil
				}
				return "", toolErr
			})
		termination := tt.NewMockTermination()

		limit := tt.PrefixLimit(gent.SGToolCallsErrorConsecutiveFor, 2)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationSuccess, execCtx.TerminationReason())
		assert.Nil(t, execCtx.ExceededLimit())

		// Build expected NextPrompts
		toolErrObs := tt.ToolObservation(format, toolChain, "specific", "")
		successObs := tt.ToolObservation(format, toolChain, "specific", "success")

		// Assert events: fail, fail, success, fail, success (answer)
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: tool error (consecutive: 1)
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.BeforeToolCall(0, 1, "specific", nil),
			tt.AfterToolCall(0, 1, "specific", nil, "", toolErr),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolErrObs)),
			// Iteration 2: tool error (consecutive: 2, at limit)
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.BeforeToolCall(0, 2, "specific", nil),
			tt.AfterToolCall(0, 2, "specific", nil, "", toolErr),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(toolErrObs)),
			// Iteration 3: tool success (consecutive resets)
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.BeforeToolCall(0, 3, "specific", nil),
			tt.AfterToolCall(0, 3, "specific", nil, "success", nil),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(successObs)),
			// Iteration 4: tool error (consecutive: 1, reset)
			tt.BeforeIter(0, 4),
			tt.BeforeModelCall(0, 4, "test-model"),
			tt.AfterModelCall(0, 4, "test-model", 100, 50),
			tt.BeforeToolCall(0, 4, "specific", nil),
			tt.AfterToolCall(0, 4, "specific", nil, "", toolErr),
			tt.AfterIter(0, 4, tt.ContinueWithPrompt(toolErrObs)),
			// Iteration 5: terminate
			tt.BeforeIter(0, 5),
			tt.BeforeModelCall(0, 5, "test-model"),
			tt.AfterModelCall(0, 5, "test-model", 100, 50),
			tt.AfterIter(0, 5, tt.Terminate("done")),
			tt.AfterExec(0, 5, gent.TerminationSuccess),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})
}

// ----------------------------------------------------------------------------
// Mock Answer Validator for testing
// ----------------------------------------------------------------------------

// Validator mock is provided by internal/tt package (tt.MockValidator)

// ----------------------------------------------------------------------------
// Test: Answer rejection total limit
// ----------------------------------------------------------------------------

func TestExecutorLimits_AnswerRejectedTotal(t *testing.T) {
	t.Run("stops when answer rejected total exceeded", func(t *testing.T) {
		// Model provides answers that get rejected
		model := tt.NewMockModel().
			AddResponse("<answer>bad answer 1</answer>", 100, 50).
			AddResponse("<answer>bad answer 2</answer>", 100, 50).
			AddResponse("<answer>bad answer 3</answer>", 100, 50).
			AddResponse("<answer>good answer</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"answer": {"bad answer 1"}}).
			AddParseResult(map[string][]string{"answer": {"bad answer 2"}}).
			AddParseResult(map[string][]string{"answer": {"bad answer 3"}}).
			AddParseResult(map[string][]string{"answer": {"good answer"}})

		toolChain := tt.NewMockToolChain()
		termination := tt.NewMockTermination()

		validator := tt.NewMockValidator("test_validator").
			WithAcceptances(false, false, false, true).
			WithFeedback(gent.FormattedSection{Name: "error", Content: "Answer rejected"})
		termination.SetValidator(validator)

		limit := tt.ExactLimit(gent.SCAnswerRejectedTotal, 2)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())

		// Event assertions
		feedback := []gent.FormattedSection{{Name: "error", Content: "Answer rejected"}}
		rejectObs := tt.ValidatorFeedbackObservation(format, feedback...)
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: answer rejected (count=1)
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.ValidatorCalled(0, 1, "test_validator", "bad answer 1"),
			tt.ValidatorResult(0, 1, "test_validator", "bad answer 1", false, feedback),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(rejectObs)),
			// Iteration 2: answer rejected (count=2)
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.ValidatorCalled(0, 2, "test_validator", "bad answer 2"),
			tt.ValidatorResult(0, 2, "test_validator", "bad answer 2", false, feedback),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(rejectObs)),
			// Iteration 3: answer rejected (count=3) -> limit exceeded
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.ValidatorCalled(0, 3, "test_validator", "bad answer 3"),
			tt.ValidatorResult(0, 3, "test_validator", "bad answer 3", false, feedback),
			tt.LimitExceeded(0, 3, limit, 3, gent.SCAnswerRejectedTotal),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(rejectObs)),
			tt.AfterExec(0, 3, gent.TerminationLimitExceeded),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})

	t.Run("exceeds limit at third iteration after successful tool calls", func(t *testing.T) {
		// Iteration 1-2: tool calls (no answer)
		// Iteration 3-5: answers rejected (3rd rejection exceeds limit of 2)
		model := tt.NewMockModel().
			AddResponse("<action>tool: search</action>", 100, 50).
			AddResponse("<action>tool: search</action>", 100, 50).
			AddResponse("<answer>bad answer 1</answer>", 100, 50).
			AddResponse("<answer>bad answer 2</answer>", 100, 50).
			AddResponse("<answer>bad answer 3</answer>", 100, 50).
			AddResponse("<answer>good answer</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: search"}}).
			AddParseResult(map[string][]string{"action": {"tool: search"}}).
			AddParseResult(map[string][]string{"answer": {"bad answer 1"}}).
			AddParseResult(map[string][]string{"answer": {"bad answer 2"}}).
			AddParseResult(map[string][]string{"answer": {"bad answer 3"}}).
			AddParseResult(map[string][]string{"answer": {"good answer"}})

		toolChain := tt.NewMockToolChain().
			WithTool("search", func(args map[string]any) (string, error) {
				return "found something", nil
			})
		termination := tt.NewMockTermination()

		validator := tt.NewMockValidator("test_validator").
			WithAcceptances(false, false, false, true).
			WithFeedback(gent.FormattedSection{Name: "error", Content: "Answer rejected"})
		termination.SetValidator(validator)

		limit := tt.ExactLimit(gent.SCAnswerRejectedTotal, 2)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())
		assert.Equal(t, 5, execCtx.Iteration())

		// Event assertions
		feedback := []gent.FormattedSection{{Name: "error", Content: "Answer rejected"}}
		toolObs := tt.ToolObservation(format, toolChain, "search", "found something")
		rejectObs := tt.ValidatorFeedbackObservation(format, feedback...)
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: tool call
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.BeforeToolCall(0, 1, "search", nil),
			tt.AfterToolCall(0, 1, "search", nil, "found something", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolObs)),
			// Iteration 2: tool call
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.BeforeToolCall(0, 2, "search", nil),
			tt.AfterToolCall(0, 2, "search", nil, "found something", nil),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(toolObs)),
			// Iteration 3: answer rejected (count=1)
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.ValidatorCalled(0, 3, "test_validator", "bad answer 1"),
			tt.ValidatorResult(0, 3, "test_validator", "bad answer 1", false, feedback),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(rejectObs)),
			// Iteration 4: answer rejected (count=2)
			tt.BeforeIter(0, 4),
			tt.BeforeModelCall(0, 4, "test-model"),
			tt.AfterModelCall(0, 4, "test-model", 100, 50),
			tt.ValidatorCalled(0, 4, "test_validator", "bad answer 2"),
			tt.ValidatorResult(0, 4, "test_validator", "bad answer 2", false, feedback),
			tt.AfterIter(0, 4, tt.ContinueWithPrompt(rejectObs)),
			// Iteration 5: answer rejected (count=3) -> limit exceeded
			tt.BeforeIter(0, 5),
			tt.BeforeModelCall(0, 5, "test-model"),
			tt.AfterModelCall(0, 5, "test-model", 100, 50),
			tt.ValidatorCalled(0, 5, "test_validator", "bad answer 3"),
			tt.ValidatorResult(0, 5, "test_validator", "bad answer 3", false, feedback),
			tt.LimitExceeded(0, 5, limit, 3, gent.SCAnswerRejectedTotal),
			tt.AfterIter(0, 5, tt.ContinueWithPrompt(rejectObs)),
			tt.AfterExec(0, 5, gent.TerminationLimitExceeded),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})
}

// ----------------------------------------------------------------------------
// Test: Answer rejection by validator limit (prefix)
// ----------------------------------------------------------------------------

func TestExecutorLimits_AnswerRejectedByValidator(t *testing.T) {
	t.Run("stops when validator-specific rejection limit exceeded (prefix)", func(t *testing.T) {
		model := tt.NewMockModel().
			AddResponse("<answer>bad answer 1</answer>", 100, 50).
			AddResponse("<answer>bad answer 2</answer>", 100, 50).
			AddResponse("<answer>good answer</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"answer": {"bad answer 1"}}).
			AddParseResult(map[string][]string{"answer": {"bad answer 2"}}).
			AddParseResult(map[string][]string{"answer": {"good answer"}})

		toolChain := tt.NewMockToolChain()
		termination := tt.NewMockTermination()

		validator := tt.NewMockValidator("schema_validator").
			WithAcceptances(false, false, true).
			WithFeedback(gent.FormattedSection{Name: "error", Content: "Schema validation failed"})
		termination.SetValidator(validator)

		limits := []gent.Limit{
			{Type: gent.LimitKeyPrefix, Key: gent.SCAnswerRejectedBy, MaxValue: 1},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		require.NotNil(t, execCtx.ExceededLimit())

		// Event assertions
		feedback := []gent.FormattedSection{
			{Name: "error", Content: "Schema validation failed"},
		}
		rejectObs := tt.ValidatorFeedbackObservation(format, feedback...)
		limit := tt.PrefixLimit(gent.SCAnswerRejectedBy, 1)
		matchedKey := gent.SCAnswerRejectedBy + "schema_validator"
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: answer rejected (count=1)
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.ValidatorCalled(0, 1, "schema_validator", "bad answer 1"),
			tt.ValidatorResult(0, 1, "schema_validator", "bad answer 1", false, feedback),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(rejectObs)),
			// Iteration 2: answer rejected (count=2) -> limit exceeded
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.ValidatorCalled(0, 2, "schema_validator", "bad answer 2"),
			tt.ValidatorResult(0, 2, "schema_validator", "bad answer 2", false, feedback),
			tt.LimitExceeded(0, 2, limit, 2, matchedKey),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(rejectObs)),
			tt.AfterExec(0, 2, gent.TerminationLimitExceeded),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})

	t.Run("prefix limit exceeds at third iteration after successful tool calls", func(t *testing.T) {
		// Iteration 1-2: tool calls (no answer)
		// Iteration 3-5: answers rejected by validator (3rd rejection exceeds limit of 2)
		model := tt.NewMockModel().
			AddResponse("<action>tool: search</action>", 100, 50).
			AddResponse("<action>tool: search</action>", 100, 50).
			AddResponse("<answer>bad answer 1</answer>", 100, 50).
			AddResponse("<answer>bad answer 2</answer>", 100, 50).
			AddResponse("<answer>bad answer 3</answer>", 100, 50).
			AddResponse("<answer>good answer</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: search"}}).
			AddParseResult(map[string][]string{"action": {"tool: search"}}).
			AddParseResult(map[string][]string{"answer": {"bad answer 1"}}).
			AddParseResult(map[string][]string{"answer": {"bad answer 2"}}).
			AddParseResult(map[string][]string{"answer": {"bad answer 3"}}).
			AddParseResult(map[string][]string{"answer": {"good answer"}})

		toolChain := tt.NewMockToolChain().
			WithTool("search", func(args map[string]any) (string, error) {
				return "found something", nil
			})
		termination := tt.NewMockTermination()

		validator := tt.NewMockValidator("schema_validator").
			WithAcceptances(false, false, false, true).
			WithFeedback(gent.FormattedSection{Name: "error", Content: "Schema validation failed"})
		termination.SetValidator(validator)

		limit := tt.PrefixLimit(gent.SCAnswerRejectedBy, 2)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())
		assert.Equal(t, 5, execCtx.Iteration())

		// Event assertions
		feedback := []gent.FormattedSection{
			{Name: "error", Content: "Schema validation failed"},
		}
		toolObs := tt.ToolObservation(format, toolChain, "search", "found something")
		rejectObs := tt.ValidatorFeedbackObservation(format, feedback...)
		matchedKey := gent.SCAnswerRejectedBy + "schema_validator"
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: tool call
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.BeforeToolCall(0, 1, "search", nil),
			tt.AfterToolCall(0, 1, "search", nil, "found something", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolObs)),
			// Iteration 2: tool call
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.BeforeToolCall(0, 2, "search", nil),
			tt.AfterToolCall(0, 2, "search", nil, "found something", nil),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(toolObs)),
			// Iteration 3: answer rejected (count=1)
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.ValidatorCalled(0, 3, "schema_validator", "bad answer 1"),
			tt.ValidatorResult(0, 3, "schema_validator", "bad answer 1", false, feedback),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(rejectObs)),
			// Iteration 4: answer rejected (count=2)
			tt.BeforeIter(0, 4),
			tt.BeforeModelCall(0, 4, "test-model"),
			tt.AfterModelCall(0, 4, "test-model", 100, 50),
			tt.ValidatorCalled(0, 4, "schema_validator", "bad answer 2"),
			tt.ValidatorResult(0, 4, "schema_validator", "bad answer 2", false, feedback),
			tt.AfterIter(0, 4, tt.ContinueWithPrompt(rejectObs)),
			// Iteration 5: answer rejected (count=3) -> limit exceeded
			tt.BeforeIter(0, 5),
			tt.BeforeModelCall(0, 5, "test-model"),
			tt.AfterModelCall(0, 5, "test-model", 100, 50),
			tt.ValidatorCalled(0, 5, "schema_validator", "bad answer 3"),
			tt.ValidatorResult(0, 5, "schema_validator", "bad answer 3", false, feedback),
			tt.LimitExceeded(0, 5, limit, 3, matchedKey),
			tt.AfterIter(0, 5, tt.ContinueWithPrompt(rejectObs)),
			tt.AfterExec(0, 5, gent.TerminationLimitExceeded),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})

	t.Run("child model call validator exceeds limit while main validator does not", func(t *testing.T) {
		// Scenario:
		// - Main loop uses "main_validator" - its rejections should NOT trigger the limit
		// - Tool makes child calls that simulate child agent with "child_validator"
		// - Limit is set for child_validator only (using exact key, not prefix)
		// - Main rejections: 3 (don't trigger)
		// - Child rejections: 2 (2nd exceeds limit of 1)
		//
		// Iteration 1: main answer rejected by main_validator
		// Iteration 2: main answer rejected by main_validator
		// Iteration 3: tool call - simulates child agent rejection by child_validator
		// Iteration 4: main answer rejected by main_validator
		// Iteration 5: tool call - simulates child agent rejection by child_validator -> LIMIT!

		childCallCount := 0
		model := tt.NewMockModel().
			AddResponse("<answer>main bad 1</answer>", 100, 50).
			AddResponse("<answer>main bad 2</answer>", 100, 50).
			AddResponse("<action>tool: child_agent</action>", 100, 50).
			AddResponse("<answer>main bad 3</answer>", 100, 50).
			AddResponse("<action>tool: child_agent</action>", 100, 50).
			AddResponse("<answer>good answer</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"answer": {"main bad 1"}}).
			AddParseResult(map[string][]string{"answer": {"main bad 2"}}).
			AddParseResult(map[string][]string{"action": {"tool: child_agent"}}).
			AddParseResult(map[string][]string{"answer": {"main bad 3"}}).
			AddParseResult(map[string][]string{"action": {"tool: child_agent"}}).
			AddParseResult(map[string][]string{"answer": {"good answer"}})

		toolChain := tt.NewMockToolChain().
			WithToolCtx("child_agent", func(execCtx *gent.ExecutionContext, args map[string]any) (string, error) {
				childCallCount++
				// Simulate what a child agent termination would do when its validator rejects
				// This increments the child_validator stats on the shared execCtx
				execCtx.Stats().IncrCounter(gent.SCAnswerRejectedTotal, 1)
				execCtx.Stats().IncrCounter(gent.SCAnswerRejectedBy+"child_validator", 1)
				return "child agent completed with rejection", nil
			})
		termination := tt.NewMockTermination()

		// Main loop uses main_validator - rejections tracked as main_validator
		mainValidator := tt.NewMockValidator("main_validator").
			WithAcceptances(false, false, false, true). // reject first 3, accept 4th
			WithFeedback(gent.FormattedSection{Name: "error", Content: "Main validation failed"})
		termination.SetValidator(mainValidator)

		// Limit only for child_validator - main_validator rejections don't count
		limit := tt.ExactLimit(gent.SCAnswerRejectedBy+"child_validator", 1)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		// Should terminate due to child_validator limit exceeded
		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())

		// Verify stats
		// main_validator: 3 rejections (iter 1, 2, 4)
		// child_validator: 2 rejections (iter 3, 5) - but limit exceeded at 2nd
		assert.Equal(t,
			int64(3),
			execCtx.Stats().GetCounter(gent.SCAnswerRejectedBy+"main_validator"))
		assert.Equal(t,
			int64(2),
			execCtx.Stats().GetCounter(gent.SCAnswerRejectedBy+"child_validator"))
		assert.Equal(t, 5, execCtx.Iteration())

		// Event assertions
		mainFeedback := []gent.FormattedSection{
			{Name: "error", Content: "Main validation failed"},
		}
		mainRejectObs := tt.ValidatorFeedbackObservation(format, mainFeedback...)
		childToolObs := tt.ToolObservation(format, toolChain,
			"child_agent", "child agent completed with rejection")
		matchedKey := gent.SCAnswerRejectedBy + "child_validator"
		expectedEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			// Iteration 1: main answer rejected by main_validator
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "test-model"),
			tt.AfterModelCall(0, 1, "test-model", 100, 50),
			tt.ValidatorCalled(0, 1, "main_validator", "main bad 1"),
			tt.ValidatorResult(0, 1, "main_validator", "main bad 1", false, mainFeedback),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(mainRejectObs)),
			// Iteration 2: main answer rejected by main_validator
			tt.BeforeIter(0, 2),
			tt.BeforeModelCall(0, 2, "test-model"),
			tt.AfterModelCall(0, 2, "test-model", 100, 50),
			tt.ValidatorCalled(0, 2, "main_validator", "main bad 2"),
			tt.ValidatorResult(0, 2, "main_validator", "main bad 2", false, mainFeedback),
			tt.AfterIter(0, 2, tt.ContinueWithPrompt(mainRejectObs)),
			// Iteration 3: tool call (child count=1, no limit exceeded)
			tt.BeforeIter(0, 3),
			tt.BeforeModelCall(0, 3, "test-model"),
			tt.AfterModelCall(0, 3, "test-model", 100, 50),
			tt.BeforeToolCall(0, 3, "child_agent", nil),
			tt.AfterToolCall(0, 3, "child_agent", nil, "child agent completed with rejection", nil),
			tt.AfterIter(0, 3, tt.ContinueWithPrompt(childToolObs)),
			// Iteration 4: main answer rejected by main_validator
			tt.BeforeIter(0, 4),
			tt.BeforeModelCall(0, 4, "test-model"),
			tt.AfterModelCall(0, 4, "test-model", 100, 50),
			tt.ValidatorCalled(0, 4, "main_validator", "main bad 3"),
			tt.ValidatorResult(0, 4, "main_validator", "main bad 3", false, mainFeedback),
			tt.AfterIter(0, 4, tt.ContinueWithPrompt(mainRejectObs)),
			// Iteration 5: tool call (child count=2) -> limit exceeded during tool exec
			tt.BeforeIter(0, 5),
			tt.BeforeModelCall(0, 5, "test-model"),
			tt.AfterModelCall(0, 5, "test-model", 100, 50),
			tt.BeforeToolCall(0, 5, "child_agent", nil),
			tt.LimitExceeded(0, 5, limit, 2, matchedKey),
			tt.AfterToolCall(0, 5, "child_agent", nil, "child agent completed with rejection", nil),
			tt.AfterIter(0, 5, tt.ContinueWithPrompt(childToolObs)),
			tt.AfterExec(0, 5, gent.TerminationLimitExceeded),
		}
		tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
	})
}

// ----------------------------------------------------------------------------
// Global Test: Multiple Limits Race
// ----------------------------------------------------------------------------

func TestExecutorLimits_MultipleLimitsRace(t *testing.T) {
	t.Run("first limit by check order is reported when multiple limits exceeded simultaneously",
		func(t *testing.T) {
			// Both input and output token limits will be exceeded on the same iteration.
			// Iteration 1: 500 input, 400 output (both under 800 limit)
			// Iteration 2: 400 input, 500 output (totals: 900 input > 800, 900 output > 800)
			// Both limits exceeded simultaneously - first limit (input) should be reported.
			model := tt.NewMockModel().
				AddResponse("<action>tool: test</action>", 500, 400).
				AddResponse("<action>tool: test</action>", 400, 500). // Both totals exceed 800
				AddResponse("<answer>done</answer>", 100, 50)

			format := tt.NewMockFormat().
				AddParseResult(map[string][]string{"action": {"tool: test"}}).
				AddParseResult(map[string][]string{"action": {"tool: test"}}).
				AddParseResult(map[string][]string{"answer": {"done"}})

			toolChain := tt.NewMockToolChain()
			termination := tt.NewMockTermination()

			// Both limits set to 800 - exceeded at iteration 2
			inputLimit := tt.ExactLimit(gent.SCInputTokens, 800)
			outputLimit := tt.ExactLimit(gent.SCOutputTokens, 800)
			limits := []gent.Limit{inputLimit, outputLimit}

			execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

			assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
			// First limit in the list should be reported (input tokens)
			assert.Equal(t, inputLimit, *execCtx.ExceededLimit())

			// Should only have ONE LimitExceededEvent for input tokens
			limitEvents := 0
			for _, event := range tt.CollectLifecycleEvents(execCtx) {
				if _, ok := event.(*gent.LimitExceededEvent); ok {
					limitEvents++
				}
			}
			assert.Equal(t, 1, limitEvents, "should have exactly one LimitExceededEvent")

			// Build expected NextPrompt for tool execution
			toolObs := tt.ToolObservation(format, toolChain, "test", "tool executed")

			// Full event verification
			expectedEvents := []gent.Event{
				tt.BeforeExec(0, 0),
				// Iteration 1: 500 input, 400 output (both under 800)
				tt.BeforeIter(0, 1),
				tt.BeforeModelCall(0, 1, "test-model"),
				tt.AfterModelCall(0, 1, "test-model", 500, 400),
				tt.BeforeToolCall(0, 1, "test", nil),
				tt.AfterToolCall(0, 1, "test", nil, "tool executed", nil),
				tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolObs)),
				// Iteration 2: +400 input (total 900 > 800), +500 output (total 900 > 800)
				// Input limit checked first and exceeded
				tt.BeforeIter(0, 2),
				tt.BeforeModelCall(0, 2, "test-model"),
				tt.AfterModelCall(0, 2, "test-model", 400, 500),
				tt.LimitExceeded(0, 2, inputLimit, 900, gent.SCInputTokens),
				// Agent continues despite cancelled context (mock doesn't check)
				tt.BeforeToolCall(0, 2, "test", nil),
				tt.AfterToolCall(0, 2, "test", nil, "tool executed", nil),
				tt.AfterIter(0, 2, tt.ContinueWithPrompt(toolObs)),
				tt.AfterExec(0, 2, gent.TerminationLimitExceeded),
			}
			tt.AssertEventsEqual(t, expectedEvents, tt.CollectLifecycleEvents(execCtx))
		})
}

// ----------------------------------------------------------------------------
// Global Test: Deep Propagation
// ----------------------------------------------------------------------------

func TestExecutorLimits_DeepPropagation(t *testing.T) {
	t.Run("limit exceeded in grandchild propagates to root parent", func(t *testing.T) {
		// Parent → Child → Grandchild hierarchy
		// Grandchild model call exceeds the limit set on parent
		// Verify: limit propagates through all levels to root

		// Grandchild model - exceeds the input token limit
		grandchildModel := tt.NewMockModel().WithName("grandchild").
			AddResponse("grandchild response", 600, 50) // Exceeds 500 limit

		// Child model (not used directly, child just calls grandchild tool)
		childModel := tt.NewMockModel().WithName("child").
			AddResponse("child response", 100, 50)

		// Parent model
		parentModel := tt.NewMockModel().WithName("parent").
			AddResponse("<action>tool: call_child</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := tt.NewMockFormat().
			AddParseResult(map[string][]string{"action": {"tool: call_child"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		// Tool that creates a child context which creates a grandchild context
		toolChain := tt.NewMockToolChain().
			WithToolCtx("call_child",
				func(execCtx *gent.ExecutionContext, args map[string]any) (string, error) {
					// Create child context
					childCtx := execCtx.SpawnChild("child-call", nil)

					// Child calls its model (accumulates some tokens)
					_, err := childModel.GenerateContent(childCtx, "child", "", nil)
					if err != nil {
						return "", err
					}

					// Child spawns grandchild context
					grandchildCtx := childCtx.SpawnChild("grandchild-call", nil)

					// Grandchild model call exceeds the limit
					resp, err := grandchildModel.GenerateContent(grandchildCtx, "grandchild", "", nil)
					if err != nil {
						return "", err
					}

					return "child completed: " + resp.Choices[0].Content, nil
				})
		termination := tt.NewMockTermination()

		// Limit on grandchild model's input tokens via prefix
		limit := tt.PrefixLimit(gent.SCInputTokensFor+"grandchild", 500)
		limits := []gent.Limit{limit}

		execCtx := runWithLimit(t, parentModel, format, toolChain, termination, limits)

		// Parent should terminate with limit exceeded
		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, limit, *execCtx.ExceededLimit())

		// Verify child context exists and was affected
		require.Len(t, execCtx.Children(), 1)
		childCtx := execCtx.Children()[0]
		assert.Equal(t, "child-call", childCtx.Name())

		// Verify grandchild context exists and was the source
		require.Len(t, childCtx.Children(), 1)
		grandchildCtx := childCtx.Children()[0]
		assert.Equal(t, "grandchild-call", grandchildCtx.Name())

		// Verify stats propagated through all levels
		// Grandchild's 600 input tokens should be tracked at gent:input_tokens:grandchild
		assert.Equal(t,
			int64(600),
			execCtx.Stats().GetCounter(gent.SCInputTokensFor+"grandchild"))

		// Build expected NextPrompt for tool execution
		toolObs := tt.ToolObservation(format, toolChain,
			"call_child", "child completed: grandchild response")

		// Assert parent context events
		matchedKey := gent.SCInputTokensFor + "grandchild"
		expectedParentEvents := []gent.Event{
			tt.BeforeExec(0, 0),
			tt.BeforeIter(0, 1),
			tt.BeforeModelCall(0, 1, "parent"),
			tt.AfterModelCall(0, 1, "parent", 100, 50),
			tt.BeforeToolCall(0, 1, "call_child", nil),
			// Limit exceeded during tool execution (from grandchild)
			tt.LimitExceeded(0, 1, limit, 600, matchedKey),
			tt.AfterToolCall(0, 1, "call_child", nil, "child completed: grandchild response", nil),
			tt.AfterIter(0, 1, tt.ContinueWithPrompt(toolObs)),
			tt.AfterExec(0, 1, gent.TerminationLimitExceeded),
		}
		tt.AssertEventsEqual(t, expectedParentEvents, tt.CollectLifecycleEvents(execCtx))

		// Assert child context events
		// Child itself doesn't exceed, but receives propagated LimitExceededEvent from grandchild
		expectedChildEvents := []gent.Event{
			tt.BeforeModelCall(1, 0, "child"),
			tt.AfterModelCall(1, 0, "child", 100, 50),
			// LimitExceeded propagates up through child context
			tt.LimitExceeded(1, 0, limit, 600, matchedKey),
		}
		tt.AssertEventsEqual(t, expectedChildEvents, tt.CollectLifecycleEvents(childCtx))

		// Assert grandchild context events (this is where the limit is exceeded)
		expectedGrandchildEvents := []gent.Event{
			tt.BeforeModelCall(2, 0, "grandchild"),
			tt.AfterModelCall(2, 0, "grandchild", 600, 50),
			// LimitExceeded published on the context that triggered it
			tt.LimitExceeded(2, 0, limit, 600, matchedKey),
		}
		tt.AssertEventsEqual(t, expectedGrandchildEvents, tt.CollectLifecycleEvents(grandchildCtx))
	})
}
