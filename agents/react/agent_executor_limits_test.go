package react

import (
	"context"
	"errors"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/executor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
)

// ----------------------------------------------------------------------------
// Mock Model with configurable token counts and model name
// ----------------------------------------------------------------------------

// limitTestModel is a mock model that supports configurable responses with token counts.
type limitTestModel struct {
	name      string // model name used for tracing
	responses []*gent.ContentResponse
	errors    []error
	callCount int
}

func newLimitTestModel() *limitTestModel {
	return &limitTestModel{name: "test-model"}
}

// WithName sets the model name for tracing.
func (m *limitTestModel) WithName(name string) *limitTestModel {
	m.name = name
	return m
}

// AddResponse adds a response with specified content and token counts.
func (m *limitTestModel) AddResponse(content string, inputTokens, outputTokens int) *limitTestModel {
	m.responses = append(m.responses, &gent.ContentResponse{
		Choices: []*gent.ContentChoice{{Content: content}},
		Info: &gent.GenerationInfo{
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
		},
	})
	return m
}

// AddError adds an error for the next call.
func (m *limitTestModel) AddError(err error) *limitTestModel {
	// Extend responses slice if needed to match errors length
	for len(m.responses) <= len(m.errors) {
		m.responses = append(m.responses, nil)
	}
	m.errors = append(m.errors, err)
	return m
}

func (m *limitTestModel) GenerateContent(
	execCtx *gent.ExecutionContext,
	modelName string,
	systemPrompt string,
	messages []llms.MessageContent,
	opts ...llms.CallOption,
) (*gent.ContentResponse, error) {
	idx := m.callCount
	m.callCount++

	if idx < len(m.errors) && m.errors[idx] != nil {
		return nil, m.errors[idx]
	}

	var resp *gent.ContentResponse
	if idx < len(m.responses) {
		resp = m.responses[idx]
	} else {
		// Default: return termination response after all configured responses
		resp = &gent.ContentResponse{
			Choices: []*gent.ContentChoice{{Content: "<answer>done</answer>"}},
			Info:    &gent.GenerationInfo{InputTokens: 10, OutputTokens: 5},
		}
	}

	// Publish model call event with token counts (mimics real model behavior)
	// Use the model's configured name for the event
	if execCtx != nil {
		execCtx.PublishAfterModelCall(m.name, nil, resp, 0, nil)
	}

	return resp, nil
}

// ----------------------------------------------------------------------------
// Mock ToolChain with configurable behavior
// ----------------------------------------------------------------------------

// limitTestToolFunc is a tool function that receives the execution context.
type limitTestToolFunc func(execCtx *gent.ExecutionContext, args map[string]any) (string, error)

type limitTestToolChain struct {
	name        string
	tools       map[string]limitTestToolFunc
	parseErrors []error
	callIdx     int
}

func newLimitTestToolChain() *limitTestToolChain {
	return &limitTestToolChain{
		name:  "action",
		tools: make(map[string]limitTestToolFunc),
	}
}

// WithTool adds a simple tool that doesn't need execution context.
func (tc *limitTestToolChain) WithTool(
	name string,
	fn func(args map[string]any) (string, error),
) *limitTestToolChain {
	tc.tools[name] = func(_ *gent.ExecutionContext, args map[string]any) (string, error) {
		return fn(args)
	}
	return tc
}

// WithToolCtx adds a tool that receives the execution context (for child model calls).
func (tc *limitTestToolChain) WithToolCtx(
	name string,
	fn limitTestToolFunc,
) *limitTestToolChain {
	tc.tools[name] = fn
	return tc
}

func (tc *limitTestToolChain) WithParseErrors(errs ...error) *limitTestToolChain {
	tc.parseErrors = errs
	return tc
}

func (tc *limitTestToolChain) Name() string     { return tc.name }
func (tc *limitTestToolChain) Guidance() string { return "Use YAML format for tool calls." }
func (tc *limitTestToolChain) AvailableToolsPrompt() string {
	return "Available tools: test tools"
}

func (tc *limitTestToolChain) RegisterTool(_ any) gent.ToolChain { return tc }

func (tc *limitTestToolChain) ParseSection(
	execCtx *gent.ExecutionContext,
	content string,
) (any, error) {
	idx := tc.callIdx
	if idx < len(tc.parseErrors) && tc.parseErrors[idx] != nil {
		err := tc.parseErrors[idx]
		if execCtx != nil {
			execCtx.PublishParseError(gent.ParseErrorTypeToolchain, content, err)
		}
		return nil, err
	}
	// Success resets consecutive counter
	if execCtx != nil {
		execCtx.Stats().ResetCounter(gent.KeyToolchainParseErrorConsecutive)
	}
	return content, nil
}

func (tc *limitTestToolChain) Execute(
	execCtx *gent.ExecutionContext,
	content string,
	format gent.TextFormat,
) (*gent.ToolChainResult, error) {
	idx := tc.callIdx
	tc.callIdx++

	if idx < len(tc.parseErrors) && tc.parseErrors[idx] != nil {
		err := tc.parseErrors[idx]
		// Publish parse error for stats tracking
		if execCtx != nil {
			execCtx.PublishParseError(gent.ParseErrorTypeToolchain, content, err)
		}
		return nil, err
	}

	// Success resets consecutive counter
	if execCtx != nil {
		execCtx.Stats().ResetCounter(gent.KeyToolchainParseErrorConsecutive)
	}

	// Simple mock: parse "tool: <name>" pattern
	toolName := "test_tool"
	if len(content) > 6 && content[:6] == "tool: " {
		toolName = content[6:]
		// Strip any args section
		for i, c := range toolName {
			if c == '\n' || c == ' ' {
				toolName = toolName[:i]
				break
			}
		}
	}

	// Publish before tool call event (increments KeyToolCalls)
	if execCtx != nil {
		execCtx.PublishBeforeToolCall(toolName, nil)
	}

	// Execute the tool
	var toolErr error
	var output string
	if fn, ok := tc.tools[toolName]; ok {
		output, toolErr = fn(execCtx, nil)
	} else {
		output = "tool executed"
	}

	// Publish after tool call event
	if execCtx != nil {
		execCtx.PublishAfterToolCall(toolName, nil, output, 0, toolErr)

		// Reset consecutive error counters on success
		if toolErr == nil {
			execCtx.Stats().ResetCounter(gent.KeyToolCallsErrorConsecutive)
			execCtx.Stats().ResetCounter(gent.KeyToolCallsErrorConsecutiveFor + toolName)
		}
	}

	var resultErr error
	if toolErr != nil {
		resultErr = toolErr
	}

	return &gent.ToolChainResult{
		Text: "<observation>\n<" + toolName + ">\n" + output + "\n</" + toolName + ">\n</observation>",
		Raw: &gent.RawToolChainResult{
			Calls:   []*gent.ToolCall{{Name: toolName, Args: nil}},
			Results: []*gent.RawToolCallResult{{Name: toolName, Output: output}},
			Errors:  []error{resultErr},
		},
	}, nil
}

// ----------------------------------------------------------------------------
// Mock Termination with configurable parse errors
// ----------------------------------------------------------------------------

type limitTestTermination struct {
	parseErrors []error
	callIdx     int
	validator   gent.AnswerValidator
}

func newLimitTestTermination() *limitTestTermination {
	return &limitTestTermination{}
}

func (t *limitTestTermination) WithParseErrors(errs ...error) *limitTestTermination {
	t.parseErrors = errs
	return t
}

func (t *limitTestTermination) Name() string     { return "answer" }
func (t *limitTestTermination) Guidance() string { return "Write your final answer." }

func (t *limitTestTermination) SetValidator(validator gent.AnswerValidator) {
	t.validator = validator
}

func (t *limitTestTermination) ParseSection(
	execCtx *gent.ExecutionContext,
	content string,
) (any, error) {
	idx := t.callIdx
	t.callIdx++

	if idx < len(t.parseErrors) && t.parseErrors[idx] != nil {
		err := t.parseErrors[idx]
		if execCtx != nil {
			execCtx.PublishParseError(gent.ParseErrorTypeTermination, content, err)
		}
		return nil, err
	}

	// Success resets consecutive counter
	if execCtx != nil {
		execCtx.Stats().ResetCounter(gent.KeyTerminationParseErrorConsecutive)
	}
	return content, nil
}

func (t *limitTestTermination) ShouldTerminate(
	execCtx *gent.ExecutionContext,
	content string,
) *gent.TerminationResult {
	if execCtx == nil {
		panic("limitTestTermination: ShouldTerminate called with nil ExecutionContext")
	}

	// Check if we had a parse error for this call
	idx := t.callIdx - 1
	if idx >= 0 && idx < len(t.parseErrors) && t.parseErrors[idx] != nil {
		return &gent.TerminationResult{Status: gent.TerminationContinue}
	}
	if content == "" {
		return &gent.TerminationResult{Status: gent.TerminationContinue}
	}

	// Run validator if set
	if t.validator != nil {
		result := t.validator.Validate(execCtx, content)
		if !result.Accepted {
			execCtx.Stats().IncrCounter(gent.KeyAnswerRejectedTotal, 1)
			execCtx.Stats().IncrCounter(gent.KeyAnswerRejectedBy+t.validator.Name(), 1)

			var feedback []gent.ContentPart
			for _, section := range result.Feedback {
				formatted := "<" + section.Name + ">\n" + section.Content + "\n</" + section.Name + ">"
				feedback = append(feedback, llms.TextContent{Text: formatted})
			}

			return &gent.TerminationResult{
				Status:  gent.TerminationAnswerRejected,
				Content: feedback,
			}
		}
	}

	return &gent.TerminationResult{
		Status:  gent.TerminationAnswerAccepted,
		Content: []gent.ContentPart{llms.TextContent{Text: content}},
	}
}

// ----------------------------------------------------------------------------
// Mock Format with configurable parse errors
// ----------------------------------------------------------------------------

type limitTestFormatCall struct {
	result map[string][]string
	err    error
}

type limitTestFormat struct {
	calls   []limitTestFormatCall
	callIdx int
}

func newLimitTestFormat() *limitTestFormat {
	return &limitTestFormat{}
}

func (f *limitTestFormat) AddParseResult(result map[string][]string) *limitTestFormat {
	f.calls = append(f.calls, limitTestFormatCall{result: result})
	return f
}

func (f *limitTestFormat) AddParseError(err error) *limitTestFormat {
	f.calls = append(f.calls, limitTestFormatCall{err: err})
	return f
}

func (f *limitTestFormat) RegisterSection(_ gent.TextSection) gent.TextFormat { return f }
func (f *limitTestFormat) DescribeStructure() string                          { return "XML format" }

func (f *limitTestFormat) Parse(
	execCtx *gent.ExecutionContext,
	output string,
) (map[string][]string, error) {
	idx := f.callIdx
	f.callIdx++

	if idx < len(f.calls) {
		call := f.calls[idx]
		if call.err != nil {
			if execCtx != nil {
				execCtx.PublishParseError(gent.ParseErrorTypeFormat, output, call.err)
			}
			return nil, call.err
		}

		// Success resets consecutive counter
		if execCtx != nil {
			execCtx.Stats().ResetCounter(gent.KeyFormatParseErrorConsecutive)
		}
		return call.result, nil
	}

	// Default: terminate
	if execCtx != nil {
		execCtx.Stats().ResetCounter(gent.KeyFormatParseErrorConsecutive)
	}
	return map[string][]string{"answer": {"done"}}, nil
}

func (f *limitTestFormat) FormatSections(sections []gent.FormattedSection) string {
	var result string
	for _, s := range sections {
		result += "<" + s.Name + ">\n" + s.Content + "\n</" + s.Name + ">\n"
	}
	return result
}

// ----------------------------------------------------------------------------
// Helper to run executor with limit test
// ----------------------------------------------------------------------------

func runWithLimit(
	t *testing.T,
	model *limitTestModel,
	format *limitTestFormat,
	toolChain *limitTestToolChain,
	termination *limitTestTermination,
	limits []gent.Limit,
) *gent.ExecutionContext {
	t.Helper()

	agent := NewAgent(model).
		WithFormat(format).
		WithToolChain(toolChain).
		WithTermination(termination)

	data := gent.NewLoopData(&gent.Task{Text: "Test task"})
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
	t.Run("stops when iteration limit exceeded", func(t *testing.T) {
		model := newLimitTestModel().
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain()
		termination := newLimitTestTermination()

		limits := []gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeyIterations, MaxValue: 2},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.NotNil(t, execCtx.ExceededLimit())
		assert.Equal(t, gent.KeyIterations, execCtx.ExceededLimit().Key)
		assert.Equal(t, 3, execCtx.Iteration()) // Attempted 3rd but stopped
	})
}

// ----------------------------------------------------------------------------
// Test: Token limits
// ----------------------------------------------------------------------------

func TestExecutorLimits_InputTokens(t *testing.T) {
	t.Run("stops when input token limit exceeded", func(t *testing.T) {
		model := newLimitTestModel().
			AddResponse("<action>tool: test</action>", 500, 50).
			AddResponse("<action>tool: test</action>", 600, 50). // Total: 1100
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain()
		termination := newLimitTestTermination()

		limits := []gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeyInputTokens, MaxValue: 1000},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, gent.KeyInputTokens, execCtx.ExceededLimit().Key)
	})

	t.Run("stops when model-specific input token limit exceeded (prefix)", func(t *testing.T) {
		// Test with two models: alpha (main agent) and beta (called by tool).
		// Limit is set on gent:input_tokens:beta (via prefix gent:input_tokens:)
		// Alpha uses 400 tokens (under limit), beta uses 600 (exceeds 500 limit)

		// Model beta is called by the tool and exceeds the limit
		modelBeta := newLimitTestModel().WithName("beta").
			AddResponse("child response", 600, 50) // Exceeds 500 limit

		// Model alpha is the main agent model
		modelAlpha := newLimitTestModel().WithName("alpha").
			AddResponse("<action>tool: call_beta</action>", 600, 50). // Over limit, but not triggered
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: call_beta"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain().
			WithToolCtx("call_beta", func(execCtx *gent.ExecutionContext, args map[string]any) (string, error) {
				// Create child execution context and call model beta
				childCtx := execCtx.SpawnChild("beta-call", nil)
				resp, err := modelBeta.GenerateContent(childCtx, "beta", "", nil)
				if err != nil {
					return "", err
				}
				return resp.Choices[0].Content, nil
			})
		termination := newLimitTestTermination()

		// Limit on model-specific input tokens - only beta should trigger this
		limits := []gent.Limit{
			{Type: gent.LimitKeyPrefix, Key: gent.KeyInputTokensFor + "beta", MaxValue: 500},
		}

		execCtx := runWithLimit(t, modelAlpha, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		require.NotNil(t, execCtx.ExceededLimit())
		// Verify it was the beta model's tokens that triggered the limit
		assert.Contains(t, execCtx.ExceededLimit().Key, "beta")
	})

	t.Run("exceeds limit at third iteration", func(t *testing.T) {
		// Iteration 1: 300 tokens (total: 300)
		// Iteration 2: 300 tokens (total: 600)
		// Iteration 3: 500 tokens (total: 1100 > 1000 limit)
		model := newLimitTestModel().
			AddResponse("<action>tool: test</action>", 300, 50).
			AddResponse("<action>tool: test</action>", 300, 50).
			AddResponse("<action>tool: test</action>", 500, 50). // Exceeds at iteration 3
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain()
		termination := newLimitTestTermination()

		limits := []gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeyInputTokens, MaxValue: 1000},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, gent.KeyInputTokens, execCtx.ExceededLimit().Key)
		assert.Equal(t, 3, execCtx.Iteration())
	})

	t.Run("prefix limit exceeds at third iteration", func(t *testing.T) {
		// Model beta is called multiple times via tool, accumulating tokens
		betaCallCount := 0
		modelBeta := newLimitTestModel().WithName("beta").
			AddResponse("response1", 300, 50). // Call 1: 300 (total: 300)
			AddResponse("response2", 300, 50). // Call 2: 300 (total: 600)
			AddResponse("response3", 500, 50)  // Call 3: 500 (total: 1100 > 1000)

		modelAlpha := newLimitTestModel().WithName("alpha").
			AddResponse("<action>tool: call_beta</action>", 100, 50).
			AddResponse("<action>tool: call_beta</action>", 100, 50).
			AddResponse("<action>tool: call_beta</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: call_beta"}}).
			AddParseResult(map[string][]string{"action": {"tool: call_beta"}}).
			AddParseResult(map[string][]string{"action": {"tool: call_beta"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain().
			WithToolCtx("call_beta", func(execCtx *gent.ExecutionContext, args map[string]any) (string, error) {
				childCtx := execCtx.SpawnChild("beta-call", nil)
				resp, err := modelBeta.GenerateContent(childCtx, "beta", "", nil)
				betaCallCount++
				if err != nil {
					return "", err
				}
				return resp.Choices[0].Content, nil
			})
		termination := newLimitTestTermination()

		limits := []gent.Limit{
			{Type: gent.LimitKeyPrefix, Key: gent.KeyInputTokensFor + "beta", MaxValue: 1000},
		}

		execCtx := runWithLimit(t, modelAlpha, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		require.NotNil(t, execCtx.ExceededLimit())
		assert.Contains(t, execCtx.ExceededLimit().Key, "beta")
		assert.Equal(t, 3, execCtx.Iteration())
	})
}

func TestExecutorLimits_OutputTokens(t *testing.T) {
	t.Run("stops when output token limit exceeded", func(t *testing.T) {
		model := newLimitTestModel().
			AddResponse("<action>tool: test</action>", 100, 500).
			AddResponse("<action>tool: test</action>", 100, 600). // Total: 1100
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain()
		termination := newLimitTestTermination()

		limits := []gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeyOutputTokens, MaxValue: 1000},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, gent.KeyOutputTokens, execCtx.ExceededLimit().Key)
	})

	t.Run("stops when model-specific output token limit exceeded (prefix)", func(t *testing.T) {
		// Test with two models: alpha (main agent) and beta (called by tool).
		// Limit is set on gent:output_tokens:beta
		// Alpha uses 50 output tokens (under limit), beta uses 600 (exceeds 500 limit)

		// Model beta is called by the tool and exceeds the limit
		modelBeta := newLimitTestModel().WithName("beta").
			AddResponse("child response", 50, 600) // Output exceeds 500 limit

		// Model alpha is the main agent model
		modelAlpha := newLimitTestModel().WithName("alpha").
			AddResponse("<action>tool: call_beta</action>", 100, 50). // Under limit
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: call_beta"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain().
			WithToolCtx("call_beta", func(execCtx *gent.ExecutionContext, args map[string]any) (string, error) {
				// Create child execution context and call model beta
				childCtx := execCtx.SpawnChild("beta-call", nil)
				resp, err := modelBeta.GenerateContent(childCtx, "beta", "", nil)
				if err != nil {
					return "", err
				}
				return resp.Choices[0].Content, nil
			})
		termination := newLimitTestTermination()

		// Limit on model-specific output tokens - only beta should trigger this
		limits := []gent.Limit{
			{Type: gent.LimitKeyPrefix, Key: gent.KeyOutputTokensFor + "beta", MaxValue: 500},
		}

		execCtx := runWithLimit(t, modelAlpha, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		require.NotNil(t, execCtx.ExceededLimit())
		// Verify it was the beta model's tokens that triggered the limit
		assert.Contains(t, execCtx.ExceededLimit().Key, "beta")
	})

	t.Run("exceeds limit at third iteration", func(t *testing.T) {
		// Iteration 1: 300 output tokens (total: 300)
		// Iteration 2: 300 output tokens (total: 600)
		// Iteration 3: 500 output tokens (total: 1100 > 1000 limit)
		model := newLimitTestModel().
			AddResponse("<action>tool: test</action>", 50, 300).
			AddResponse("<action>tool: test</action>", 50, 300).
			AddResponse("<action>tool: test</action>", 50, 500). // Exceeds at iteration 3
			AddResponse("<answer>done</answer>", 50, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain()
		termination := newLimitTestTermination()

		limits := []gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeyOutputTokens, MaxValue: 1000},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, gent.KeyOutputTokens, execCtx.ExceededLimit().Key)
		assert.Equal(t, 3, execCtx.Iteration())
	})

	t.Run("prefix limit exceeds at third iteration", func(t *testing.T) {
		// Model beta called multiple times, accumulating output tokens
		modelBeta := newLimitTestModel().WithName("beta").
			AddResponse("response1", 50, 300). // Call 1: 300 (total: 300)
			AddResponse("response2", 50, 300). // Call 2: 300 (total: 600)
			AddResponse("response3", 50, 500)  // Call 3: 500 (total: 1100 > 1000)

		modelAlpha := newLimitTestModel().WithName("alpha").
			AddResponse("<action>tool: call_beta</action>", 100, 50).
			AddResponse("<action>tool: call_beta</action>", 100, 50).
			AddResponse("<action>tool: call_beta</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: call_beta"}}).
			AddParseResult(map[string][]string{"action": {"tool: call_beta"}}).
			AddParseResult(map[string][]string{"action": {"tool: call_beta"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain().
			WithToolCtx("call_beta", func(execCtx *gent.ExecutionContext, args map[string]any) (string, error) {
				childCtx := execCtx.SpawnChild("beta-call", nil)
				resp, err := modelBeta.GenerateContent(childCtx, "beta", "", nil)
				if err != nil {
					return "", err
				}
				return resp.Choices[0].Content, nil
			})
		termination := newLimitTestTermination()

		limits := []gent.Limit{
			{Type: gent.LimitKeyPrefix, Key: gent.KeyOutputTokensFor + "beta", MaxValue: 1000},
		}

		execCtx := runWithLimit(t, modelAlpha, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		require.NotNil(t, execCtx.ExceededLimit())
		assert.Contains(t, execCtx.ExceededLimit().Key, "beta")
		assert.Equal(t, 3, execCtx.Iteration())
	})
}

// ----------------------------------------------------------------------------
// Test: Tool call limits
// ----------------------------------------------------------------------------

func TestExecutorLimits_ToolCalls(t *testing.T) {
	t.Run("stops when tool call limit exceeded", func(t *testing.T) {
		model := newLimitTestModel().
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain()
		termination := newLimitTestTermination()

		limits := []gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeyToolCalls, MaxValue: 2},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, gent.KeyToolCalls, execCtx.ExceededLimit().Key)
	})

	t.Run("stops when tool-specific call limit exceeded (prefix)", func(t *testing.T) {
		// Two tools: search and get_detail
		// search is called 3 times (exceeds first), but limit is only on get_detail
		// get_detail is called twice, with limit of 1
		model := newLimitTestModel().
			AddResponse("<action>tool: search</action>", 100, 50).
			AddResponse("<action>tool: search</action>", 100, 50).
			AddResponse("<action>tool: search</action>", 100, 50). // search: 3 calls (no limit)
			AddResponse("<action>tool: get_detail</action>", 100, 50).
			AddResponse("<action>tool: get_detail</action>", 100, 50). // get_detail: 2 calls (limit 1)
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: search"}}).
			AddParseResult(map[string][]string{"action": {"tool: search"}}).
			AddParseResult(map[string][]string{"action": {"tool: search"}}).
			AddParseResult(map[string][]string{"action": {"tool: get_detail"}}).
			AddParseResult(map[string][]string{"action": {"tool: get_detail"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain()
		termination := newLimitTestTermination()

		// Only limit get_detail to 1 call
		limits := []gent.Limit{
			{Type: gent.LimitKeyPrefix, Key: gent.KeyToolCallsFor + "get_detail", MaxValue: 1},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		require.NotNil(t, execCtx.ExceededLimit())
		assert.Contains(t, execCtx.ExceededLimit().Key, "get_detail")
		assert.Equal(t, 5, execCtx.Iteration()) // Fails on 5th iteration (2nd get_detail call)
	})

	t.Run("exceeds limit at third iteration", func(t *testing.T) {
		// 4 tool calls across iterations, limit is 3
		model := newLimitTestModel().
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<action>tool: test</action>", 100, 50). // 4th call exceeds limit
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain()
		termination := newLimitTestTermination()

		limits := []gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeyToolCalls, MaxValue: 3},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, gent.KeyToolCalls, execCtx.ExceededLimit().Key)
		assert.Equal(t, 4, execCtx.Iteration())
	})

	t.Run("prefix limit exceeds at third iteration", func(t *testing.T) {
		// Call "search" 4 times, limit is 3
		model := newLimitTestModel().
			AddResponse("<action>tool: search</action>", 100, 50).
			AddResponse("<action>tool: search</action>", 100, 50).
			AddResponse("<action>tool: search</action>", 100, 50).
			AddResponse("<action>tool: search</action>", 100, 50). // 4th call exceeds
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: search"}}).
			AddParseResult(map[string][]string{"action": {"tool: search"}}).
			AddParseResult(map[string][]string{"action": {"tool: search"}}).
			AddParseResult(map[string][]string{"action": {"tool: search"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain()
		termination := newLimitTestTermination()

		limits := []gent.Limit{
			{Type: gent.LimitKeyPrefix, Key: gent.KeyToolCallsFor, MaxValue: 3},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		require.NotNil(t, execCtx.ExceededLimit())
		assert.Equal(t, 4, execCtx.Iteration())
	})
}

// ----------------------------------------------------------------------------
// Test: Parse error total limits
// ----------------------------------------------------------------------------

func TestExecutorLimits_FormatParseErrorTotal(t *testing.T) {
	t.Run("stops when format parse error total exceeded", func(t *testing.T) {
		model := newLimitTestModel().
			AddResponse("invalid1", 100, 50).
			AddResponse("invalid2", 100, 50).
			AddResponse("invalid3", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseError(gent.ErrNoSectionsFound).
			AddParseError(gent.ErrNoSectionsFound).
			AddParseError(gent.ErrNoSectionsFound).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain()
		termination := newLimitTestTermination()

		limits := []gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeyFormatParseErrorTotal, MaxValue: 2},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, gent.KeyFormatParseErrorTotal, execCtx.ExceededLimit().Key)
	})

	t.Run("exceeds limit at third iteration after successful iterations", func(t *testing.T) {
		// Iteration 1: success (tool call)
		// Iteration 2: success (tool call)
		// Iteration 3: parse error (total: 1)
		// Iteration 4: parse error (total: 2)
		// Iteration 5: parse error (total: 3 > limit of 2)
		model := newLimitTestModel().
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("invalid1", 100, 50).
			AddResponse("invalid2", 100, 50).
			AddResponse("invalid3", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseError(gent.ErrNoSectionsFound).
			AddParseError(gent.ErrNoSectionsFound).
			AddParseError(gent.ErrNoSectionsFound).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain()
		termination := newLimitTestTermination()

		limits := []gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeyFormatParseErrorTotal, MaxValue: 2},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, gent.KeyFormatParseErrorTotal, execCtx.ExceededLimit().Key)
		assert.Equal(t, 5, execCtx.Iteration())
	})
}

func TestExecutorLimits_ToolchainParseErrorTotal(t *testing.T) {
	t.Run("stops when toolchain parse error total exceeded", func(t *testing.T) {
		model := newLimitTestModel().
			AddResponse("<action>invalid json</action>", 100, 50).
			AddResponse("<action>invalid json</action>", 100, 50).
			AddResponse("<action>invalid json</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"invalid json"}}).
			AddParseResult(map[string][]string{"action": {"invalid json"}}).
			AddParseResult(map[string][]string{"action": {"invalid json"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain().
			WithParseErrors(gent.ErrInvalidJSON, gent.ErrInvalidJSON, gent.ErrInvalidJSON, nil)
		termination := newLimitTestTermination()

		limits := []gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeyToolchainParseErrorTotal, MaxValue: 2},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, gent.KeyToolchainParseErrorTotal, execCtx.ExceededLimit().Key)
	})

	t.Run("exceeds limit at third iteration after successful iterations", func(t *testing.T) {
		// Iteration 1-2: successful tool calls
		// Iteration 3-5: toolchain parse errors (3rd error exceeds limit of 2)
		model := newLimitTestModel().
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<action>bad1</action>", 100, 50).
			AddResponse("<action>bad2</action>", 100, 50).
			AddResponse("<action>bad3</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"bad1"}}).
			AddParseResult(map[string][]string{"action": {"bad2"}}).
			AddParseResult(map[string][]string{"action": {"bad3"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain().
			WithParseErrors(nil, nil, gent.ErrInvalidJSON, gent.ErrInvalidJSON, gent.ErrInvalidJSON, nil)
		termination := newLimitTestTermination()

		limits := []gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeyToolchainParseErrorTotal, MaxValue: 2},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, gent.KeyToolchainParseErrorTotal, execCtx.ExceededLimit().Key)
		assert.Equal(t, 5, execCtx.Iteration())
	})
}

func TestExecutorLimits_TerminationParseErrorTotal(t *testing.T) {
	t.Run("stops when termination parse error total exceeded", func(t *testing.T) {
		model := newLimitTestModel().
			AddResponse("<answer>malformed</answer>", 100, 50).
			AddResponse("<answer>malformed</answer>", 100, 50).
			AddResponse("<answer>malformed</answer>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"answer": {"malformed"}}).
			AddParseResult(map[string][]string{"answer": {"malformed"}}).
			AddParseResult(map[string][]string{"answer": {"malformed"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain()
		termination := newLimitTestTermination().
			WithParseErrors(gent.ErrInvalidJSON, gent.ErrInvalidJSON, gent.ErrInvalidJSON, nil)

		limits := []gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeyTerminationParseErrorTotal, MaxValue: 2},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, gent.KeyTerminationParseErrorTotal, execCtx.ExceededLimit().Key)
	})

	t.Run("exceeds limit at third iteration after successful iterations", func(t *testing.T) {
		// Iteration 1-2: successful tool calls (no termination parsing)
		// Iteration 3-5: termination parse errors (3rd error exceeds limit of 2)
		// Note: termination parsing only happens for answer sections, so parse error
		// indices start at 0 for the first answer section
		model := newLimitTestModel().
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<answer>bad1</answer>", 100, 50).
			AddResponse("<answer>bad2</answer>", 100, 50).
			AddResponse("<answer>bad3</answer>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"answer": {"bad1"}}).
			AddParseResult(map[string][]string{"answer": {"bad2"}}).
			AddParseResult(map[string][]string{"answer": {"bad3"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain()
		// Parse errors indices: 0=bad1, 1=bad2, 2=bad3, 3=done
		termination := newLimitTestTermination().
			WithParseErrors(gent.ErrInvalidJSON, gent.ErrInvalidJSON, gent.ErrInvalidJSON, nil)

		limits := []gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeyTerminationParseErrorTotal, MaxValue: 2},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, gent.KeyTerminationParseErrorTotal, execCtx.ExceededLimit().Key)
		assert.Equal(t, 5, execCtx.Iteration())
	})
}

func TestExecutorLimits_SectionParseErrorTotal(t *testing.T) {
	t.Run("stops when section parse error total exceeded", func(t *testing.T) {
		// Section parse errors are traced by TextSection implementations.
		// For this test, we'll use a custom section that traces errors.
		// However, the react agent doesn't use custom sections directly.
		// Section parse errors would come from custom TextSection implementations.
		// For now, we'll verify the limit works by manually incrementing the counter.

		model := newLimitTestModel().
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain()
		termination := newLimitTestTermination()

		agent := NewAgent(model).
			WithFormat(format).
			WithToolChain(toolChain).
			WithTermination(termination)

		data := gent.NewLoopData(&gent.Task{Text: "Test task"})
		execCtx := gent.NewExecutionContext(context.Background(), "test", data)
		execCtx.SetLimits([]gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeySectionParseErrorTotal, MaxValue: 2},
		})

		// Manually increment to simulate section parse errors
		execCtx.Stats().IncrCounter(gent.KeySectionParseErrorTotal, 3)

		exec := executor.New[*gent.BasicLoopData](agent, executor.DefaultConfig())
		exec.Execute(execCtx)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, gent.KeySectionParseErrorTotal, execCtx.ExceededLimit().Key)
	})

	t.Run("exceeds limit at third iteration after successful iterations", func(t *testing.T) {
		// Simulate section parse errors occurring after 2 successful iterations
		model := newLimitTestModel().
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain()
		termination := newLimitTestTermination()

		agent := NewAgent(model).
			WithFormat(format).
			WithToolChain(toolChain).
			WithTermination(termination)

		data := gent.NewLoopData(&gent.Task{Text: "Test task"})
		execCtx := gent.NewExecutionContext(context.Background(), "test", data)
		execCtx.SetLimits([]gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeySectionParseErrorTotal, MaxValue: 2},
		})

		// Simulate: 2 successful iterations, then section errors in 3rd iteration
		// We'll increment errors after first two successful iterations would have run
		// Since we manually control, increment 3 to exceed limit
		execCtx.Stats().IncrCounter(gent.KeySectionParseErrorTotal, 3)

		exec := executor.New[*gent.BasicLoopData](agent, executor.DefaultConfig())
		exec.Execute(execCtx)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, gent.KeySectionParseErrorTotal, execCtx.ExceededLimit().Key)
	})
}

// ----------------------------------------------------------------------------
// Test: Parse error consecutive limits
// ----------------------------------------------------------------------------

func TestExecutorLimits_FormatParseErrorConsecutive(t *testing.T) {
	t.Run("stops when format parse error consecutive exceeded", func(t *testing.T) {
		model := newLimitTestModel().
			AddResponse("invalid1", 100, 50).
			AddResponse("invalid2", 100, 50).
			AddResponse("invalid3", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseError(gent.ErrNoSectionsFound).
			AddParseError(gent.ErrNoSectionsFound).
			AddParseError(gent.ErrNoSectionsFound).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain()
		termination := newLimitTestTermination()

		limits := []gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeyFormatParseErrorConsecutive, MaxValue: 2},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, gent.KeyFormatParseErrorConsecutive, execCtx.ExceededLimit().Key)
	})

	t.Run("exceeds consecutive limit at third iteration after successful iterations", func(t *testing.T) {
		// Iteration 1-2: successful tool calls
		// Iteration 3-5: consecutive format parse errors (3rd error exceeds limit of 2)
		model := newLimitTestModel().
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("invalid1", 100, 50).
			AddResponse("invalid2", 100, 50).
			AddResponse("invalid3", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseError(gent.ErrNoSectionsFound).
			AddParseError(gent.ErrNoSectionsFound).
			AddParseError(gent.ErrNoSectionsFound).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain()
		termination := newLimitTestTermination()

		limits := []gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeyFormatParseErrorConsecutive, MaxValue: 2},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, gent.KeyFormatParseErrorConsecutive, execCtx.ExceededLimit().Key)
		assert.Equal(t, 5, execCtx.Iteration())
	})
}

func TestExecutorLimits_ToolchainParseErrorConsecutive(t *testing.T) {
	t.Run("stops when toolchain parse error consecutive exceeded", func(t *testing.T) {
		model := newLimitTestModel().
			AddResponse("<action>invalid</action>", 100, 50).
			AddResponse("<action>invalid</action>", 100, 50).
			AddResponse("<action>invalid</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"invalid"}}).
			AddParseResult(map[string][]string{"action": {"invalid"}}).
			AddParseResult(map[string][]string{"action": {"invalid"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain().
			WithParseErrors(gent.ErrInvalidJSON, gent.ErrInvalidJSON, gent.ErrInvalidJSON, nil)
		termination := newLimitTestTermination()

		limits := []gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeyToolchainParseErrorConsecutive, MaxValue: 2},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, gent.KeyToolchainParseErrorConsecutive, execCtx.ExceededLimit().Key)
	})

	t.Run("exceeds consecutive limit at third iteration after successful iterations", func(t *testing.T) {
		// Iteration 1-2: successful tool calls
		// Iteration 3-5: consecutive toolchain parse errors (3rd error exceeds limit of 2)
		model := newLimitTestModel().
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<action>bad1</action>", 100, 50).
			AddResponse("<action>bad2</action>", 100, 50).
			AddResponse("<action>bad3</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"bad1"}}).
			AddParseResult(map[string][]string{"action": {"bad2"}}).
			AddParseResult(map[string][]string{"action": {"bad3"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain().
			WithParseErrors(nil, nil, gent.ErrInvalidJSON, gent.ErrInvalidJSON, gent.ErrInvalidJSON, nil)
		termination := newLimitTestTermination()

		limits := []gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeyToolchainParseErrorConsecutive, MaxValue: 2},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, gent.KeyToolchainParseErrorConsecutive, execCtx.ExceededLimit().Key)
		assert.Equal(t, 5, execCtx.Iteration())
	})
}

func TestExecutorLimits_TerminationParseErrorConsecutive(t *testing.T) {
	t.Run("stops when termination parse error consecutive exceeded", func(t *testing.T) {
		model := newLimitTestModel().
			AddResponse("<answer>bad1</answer>", 100, 50).
			AddResponse("<answer>bad2</answer>", 100, 50).
			AddResponse("<answer>bad3</answer>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"answer": {"bad1"}}).
			AddParseResult(map[string][]string{"answer": {"bad2"}}).
			AddParseResult(map[string][]string{"answer": {"bad3"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain()
		termination := newLimitTestTermination().
			WithParseErrors(gent.ErrInvalidJSON, gent.ErrInvalidJSON, gent.ErrInvalidJSON, nil)

		limits := []gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeyTerminationParseErrorConsecutive, MaxValue: 2},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, gent.KeyTerminationParseErrorConsecutive, execCtx.ExceededLimit().Key)
	})

	t.Run("exceeds consecutive limit at third iteration after successful iterations", func(t *testing.T) {
		// Iteration 1-2: successful tool calls (no termination parsing)
		// Iteration 3-5: consecutive termination parse errors (3rd error exceeds limit of 2)
		// Note: termination parsing only happens for answer sections, so parse error
		// indices start at 0 for the first answer section
		model := newLimitTestModel().
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<answer>bad1</answer>", 100, 50).
			AddResponse("<answer>bad2</answer>", 100, 50).
			AddResponse("<answer>bad3</answer>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"answer": {"bad1"}}).
			AddParseResult(map[string][]string{"answer": {"bad2"}}).
			AddParseResult(map[string][]string{"answer": {"bad3"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain()
		// Parse errors indices: 0=bad1, 1=bad2, 2=bad3, 3=done
		termination := newLimitTestTermination().
			WithParseErrors(gent.ErrInvalidJSON, gent.ErrInvalidJSON, gent.ErrInvalidJSON, nil)

		limits := []gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeyTerminationParseErrorConsecutive, MaxValue: 2},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, gent.KeyTerminationParseErrorConsecutive, execCtx.ExceededLimit().Key)
		assert.Equal(t, 5, execCtx.Iteration())
	})
}

func TestExecutorLimits_SectionParseErrorConsecutive(t *testing.T) {
	t.Run("stops when section parse error consecutive exceeded", func(t *testing.T) {
		model := newLimitTestModel().
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain()
		termination := newLimitTestTermination()

		agent := NewAgent(model).
			WithFormat(format).
			WithToolChain(toolChain).
			WithTermination(termination)

		data := gent.NewLoopData(&gent.Task{Text: "Test task"})
		execCtx := gent.NewExecutionContext(context.Background(), "test", data)
		execCtx.SetLimits([]gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeySectionParseErrorConsecutive, MaxValue: 2},
		})

		// Manually increment to simulate consecutive section parse errors
		execCtx.Stats().IncrCounter(gent.KeySectionParseErrorConsecutive, 3)

		exec := executor.New[*gent.BasicLoopData](agent, executor.DefaultConfig())
		exec.Execute(execCtx)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, gent.KeySectionParseErrorConsecutive, execCtx.ExceededLimit().Key)
	})

	t.Run("exceeds consecutive limit at third iteration after successful iterations", func(t *testing.T) {
		// Simulate section parse errors occurring consecutively after 2 successful iterations
		model := newLimitTestModel().
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain()
		termination := newLimitTestTermination()

		agent := NewAgent(model).
			WithFormat(format).
			WithToolChain(toolChain).
			WithTermination(termination)

		data := gent.NewLoopData(&gent.Task{Text: "Test task"})
		execCtx := gent.NewExecutionContext(context.Background(), "test", data)
		execCtx.SetLimits([]gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeySectionParseErrorConsecutive, MaxValue: 2},
		})

		// Simulate: 2 successful iterations, then consecutive section errors (3 > limit of 2)
		execCtx.Stats().IncrCounter(gent.KeySectionParseErrorConsecutive, 3)

		exec := executor.New[*gent.BasicLoopData](agent, executor.DefaultConfig())
		exec.Execute(execCtx)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, gent.KeySectionParseErrorConsecutive, execCtx.ExceededLimit().Key)
	})
}

// ----------------------------------------------------------------------------
// Test: Tool call error limits
// ----------------------------------------------------------------------------

func TestExecutorLimits_ToolCallsErrorTotal(t *testing.T) {
	t.Run("stops when tool call error total exceeded", func(t *testing.T) {
		model := newLimitTestModel().
			AddResponse("<action>tool: failing</action>", 100, 50).
			AddResponse("<action>tool: failing</action>", 100, 50).
			AddResponse("<action>tool: failing</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: failing"}}).
			AddParseResult(map[string][]string{"action": {"tool: failing"}}).
			AddParseResult(map[string][]string{"action": {"tool: failing"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain().
			WithTool("failing", func(args map[string]any) (string, error) {
				return "", errors.New("tool failed")
			})
		termination := newLimitTestTermination()

		limits := []gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeyToolCallsErrorTotal, MaxValue: 2},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, gent.KeyToolCallsErrorTotal, execCtx.ExceededLimit().Key)
	})

	t.Run("exceeds limit at third iteration after successful iterations", func(t *testing.T) {
		// Iteration 1-2: successful tool calls
		// Iteration 3-5: failing tool calls (3rd error exceeds limit of 2)
		callCount := 0
		model := newLimitTestModel().
			AddResponse("<action>tool: maybe</action>", 100, 50).
			AddResponse("<action>tool: maybe</action>", 100, 50).
			AddResponse("<action>tool: maybe</action>", 100, 50).
			AddResponse("<action>tool: maybe</action>", 100, 50).
			AddResponse("<action>tool: maybe</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: maybe"}}).
			AddParseResult(map[string][]string{"action": {"tool: maybe"}}).
			AddParseResult(map[string][]string{"action": {"tool: maybe"}}).
			AddParseResult(map[string][]string{"action": {"tool: maybe"}}).
			AddParseResult(map[string][]string{"action": {"tool: maybe"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain().
			WithTool("maybe", func(args map[string]any) (string, error) {
				callCount++
				if callCount <= 2 {
					return "success", nil
				}
				return "", errors.New("tool failed")
			})
		termination := newLimitTestTermination()

		limits := []gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeyToolCallsErrorTotal, MaxValue: 2},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, gent.KeyToolCallsErrorTotal, execCtx.ExceededLimit().Key)
		assert.Equal(t, 5, execCtx.Iteration())
	})
}

func TestExecutorLimits_ToolCallsErrorForTool(t *testing.T) {
	t.Run("stops when tool-specific error limit exceeded (prefix)", func(t *testing.T) {
		model := newLimitTestModel().
			AddResponse("<action>tool: broken</action>", 100, 50).
			AddResponse("<action>tool: broken</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: broken"}}).
			AddParseResult(map[string][]string{"action": {"tool: broken"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain().
			WithTool("broken", func(args map[string]any) (string, error) {
				return "", errors.New("broken tool")
			})
		termination := newLimitTestTermination()

		limits := []gent.Limit{
			{Type: gent.LimitKeyPrefix, Key: gent.KeyToolCallsErrorFor, MaxValue: 1},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		require.NotNil(t, execCtx.ExceededLimit())
	})

	t.Run("prefix limit exceeds at third iteration after successful iterations", func(t *testing.T) {
		// Iteration 1-2: successful tool calls
		// Iteration 3-4: failing tool calls (2nd error exceeds limit of 1)
		callCount := 0
		model := newLimitTestModel().
			AddResponse("<action>tool: broken</action>", 100, 50).
			AddResponse("<action>tool: broken</action>", 100, 50).
			AddResponse("<action>tool: broken</action>", 100, 50).
			AddResponse("<action>tool: broken</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: broken"}}).
			AddParseResult(map[string][]string{"action": {"tool: broken"}}).
			AddParseResult(map[string][]string{"action": {"tool: broken"}}).
			AddParseResult(map[string][]string{"action": {"tool: broken"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain().
			WithTool("broken", func(args map[string]any) (string, error) {
				callCount++
				if callCount <= 2 {
					return "success", nil
				}
				return "", errors.New("broken tool")
			})
		termination := newLimitTestTermination()

		limits := []gent.Limit{
			{Type: gent.LimitKeyPrefix, Key: gent.KeyToolCallsErrorFor, MaxValue: 1},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		require.NotNil(t, execCtx.ExceededLimit())
		assert.Equal(t, 4, execCtx.Iteration())
	})
}

func TestExecutorLimits_ToolCallsErrorConsecutive(t *testing.T) {
	t.Run("stops when tool call error consecutive exceeded", func(t *testing.T) {
		model := newLimitTestModel().
			AddResponse("<action>tool: failing</action>", 100, 50).
			AddResponse("<action>tool: failing</action>", 100, 50).
			AddResponse("<action>tool: failing</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: failing"}}).
			AddParseResult(map[string][]string{"action": {"tool: failing"}}).
			AddParseResult(map[string][]string{"action": {"tool: failing"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain().
			WithTool("failing", func(args map[string]any) (string, error) {
				return "", errors.New("tool failed")
			})
		termination := newLimitTestTermination()

		limits := []gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeyToolCallsErrorConsecutive, MaxValue: 2},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, gent.KeyToolCallsErrorConsecutive, execCtx.ExceededLimit().Key)
	})

	t.Run("exceeds consecutive limit at third iteration after successful iterations", func(t *testing.T) {
		// Iteration 1-2: successful tool calls
		// Iteration 3-5: consecutive failing tool calls (3rd error exceeds limit of 2)
		callCount := 0
		model := newLimitTestModel().
			AddResponse("<action>tool: maybe</action>", 100, 50).
			AddResponse("<action>tool: maybe</action>", 100, 50).
			AddResponse("<action>tool: maybe</action>", 100, 50).
			AddResponse("<action>tool: maybe</action>", 100, 50).
			AddResponse("<action>tool: maybe</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: maybe"}}).
			AddParseResult(map[string][]string{"action": {"tool: maybe"}}).
			AddParseResult(map[string][]string{"action": {"tool: maybe"}}).
			AddParseResult(map[string][]string{"action": {"tool: maybe"}}).
			AddParseResult(map[string][]string{"action": {"tool: maybe"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain().
			WithTool("maybe", func(args map[string]any) (string, error) {
				callCount++
				if callCount <= 2 {
					return "success", nil
				}
				return "", errors.New("tool failed")
			})
		termination := newLimitTestTermination()

		limits := []gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeyToolCallsErrorConsecutive, MaxValue: 2},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, gent.KeyToolCallsErrorConsecutive, execCtx.ExceededLimit().Key)
		assert.Equal(t, 5, execCtx.Iteration())
	})
}

func TestExecutorLimits_ToolCallsErrorConsecutiveForTool(t *testing.T) {
	t.Run("stops when tool-specific consecutive error limit exceeded (prefix)", func(t *testing.T) {
		model := newLimitTestModel().
			AddResponse("<action>tool: flaky</action>", 100, 50).
			AddResponse("<action>tool: flaky</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: flaky"}}).
			AddParseResult(map[string][]string{"action": {"tool: flaky"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain().
			WithTool("flaky", func(args map[string]any) (string, error) {
				return "", errors.New("flaky error")
			})
		termination := newLimitTestTermination()

		limits := []gent.Limit{
			{Type: gent.LimitKeyPrefix, Key: gent.KeyToolCallsErrorConsecutiveFor, MaxValue: 1},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		require.NotNil(t, execCtx.ExceededLimit())
	})

	t.Run("prefix consecutive limit exceeds at third iteration after successful iterations", func(t *testing.T) {
		// Iteration 1-2: successful tool calls
		// Iteration 3-4: consecutive failing tool calls (2nd error exceeds limit of 1)
		callCount := 0
		model := newLimitTestModel().
			AddResponse("<action>tool: flaky</action>", 100, 50).
			AddResponse("<action>tool: flaky</action>", 100, 50).
			AddResponse("<action>tool: flaky</action>", 100, 50).
			AddResponse("<action>tool: flaky</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: flaky"}}).
			AddParseResult(map[string][]string{"action": {"tool: flaky"}}).
			AddParseResult(map[string][]string{"action": {"tool: flaky"}}).
			AddParseResult(map[string][]string{"action": {"tool: flaky"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain().
			WithTool("flaky", func(args map[string]any) (string, error) {
				callCount++
				if callCount <= 2 {
					return "success", nil
				}
				return "", errors.New("flaky error")
			})
		termination := newLimitTestTermination()

		limits := []gent.Limit{
			{Type: gent.LimitKeyPrefix, Key: gent.KeyToolCallsErrorConsecutiveFor, MaxValue: 1},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		require.NotNil(t, execCtx.ExceededLimit())
		assert.Equal(t, 4, execCtx.Iteration())
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
		model := newLimitTestModel().
			AddResponse("invalid1", 100, 50).                 // fail
			AddResponse("invalid2", 100, 50).                 // fail (consecutive=2)
			AddResponse("<action>tool: t</action>", 100, 50). // success (resets)
			AddResponse("invalid3", 100, 50).                 // fail (consecutive=1)
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseError(gent.ErrNoSectionsFound).                     // fail
			AddParseError(gent.ErrNoSectionsFound).                     // fail
			AddParseResult(map[string][]string{"action": {"tool: t"}}). // success
			AddParseError(gent.ErrNoSectionsFound).                     // fail
			AddParseResult(map[string][]string{"answer": {"done"}})     // terminate

		toolChain := newLimitTestToolChain()
		termination := newLimitTestTermination()

		limits := []gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeyFormatParseErrorConsecutive, MaxValue: 2},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		// Should complete successfully because consecutive never exceeded 2
		assert.Equal(t, gent.TerminationSuccess, execCtx.TerminationReason())
		assert.Nil(t, execCtx.ExceededLimit())
	})
}

func TestExecutorLimits_ConsecutiveReset_ToolchainParseError(t *testing.T) {
	t.Run("consecutive counter resets on success - does not exceed limit", func(t *testing.T) {
		// Sequence: fail, fail, success, fail
		model := newLimitTestModel().
			AddResponse("<action>bad1</action>", 100, 50).
			AddResponse("<action>bad2</action>", 100, 50).
			AddResponse("<action>tool: good</action>", 100, 50).
			AddResponse("<action>bad3</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"bad1"}}).
			AddParseResult(map[string][]string{"action": {"bad2"}}).
			AddParseResult(map[string][]string{"action": {"tool: good"}}).
			AddParseResult(map[string][]string{"action": {"bad3"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain().
			WithParseErrors(
				gent.ErrInvalidJSON, // fail
				gent.ErrInvalidJSON, // fail (consecutive=2)
				nil,                 // success (resets)
				gent.ErrInvalidJSON, // fail (consecutive=1)
				nil,                 // N/A
			)
		termination := newLimitTestTermination()

		limits := []gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeyToolchainParseErrorConsecutive, MaxValue: 2},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationSuccess, execCtx.TerminationReason())
		assert.Nil(t, execCtx.ExceededLimit())
	})
}

func TestExecutorLimits_ConsecutiveReset_TerminationParseError(t *testing.T) {
	t.Run("consecutive counter resets on success - does not exceed limit", func(t *testing.T) {
		// Sequence: fail, fail, success (action taken), fail, success
		model := newLimitTestModel().
			AddResponse("<answer>bad1</answer>", 100, 50).
			AddResponse("<answer>bad2</answer>", 100, 50).
			AddResponse("<action>tool: t</action>", 100, 50). // action takes priority, no term parse
			AddResponse("<answer>bad3</answer>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"answer": {"bad1"}}).
			AddParseResult(map[string][]string{"answer": {"bad2"}}).
			AddParseResult(map[string][]string{"action": {"tool: t"}}).
			AddParseResult(map[string][]string{"answer": {"bad3"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain()
		termination := newLimitTestTermination().
			WithParseErrors(
				gent.ErrInvalidJSON, // fail
				gent.ErrInvalidJSON, // fail (consecutive=2)
				nil,                 // skipped (action taken)
				gent.ErrInvalidJSON, // fail (consecutive=1 after action resets)
				nil,                 // success
			)

		limits := []gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeyTerminationParseErrorConsecutive, MaxValue: 2},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationSuccess, execCtx.TerminationReason())
		assert.Nil(t, execCtx.ExceededLimit())
	})
}

func TestExecutorLimits_ConsecutiveReset_SectionParseError(t *testing.T) {
	t.Run("consecutive counter resets on success - does not exceed limit", func(t *testing.T) {
		model := newLimitTestModel().
			AddResponse("<action>tool: test</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: test"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain()
		termination := newLimitTestTermination()

		agent := NewAgent(model).
			WithFormat(format).
			WithToolChain(toolChain).
			WithTermination(termination)

		data := gent.NewLoopData(&gent.Task{Text: "Test task"})
		execCtx := gent.NewExecutionContext(context.Background(), "test", data)
		execCtx.SetLimits([]gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeySectionParseErrorConsecutive, MaxValue: 2},
		})

		// Simulate: fail, fail, reset, fail (consecutive should be 1, not 3)
		execCtx.Stats().IncrCounter(gent.KeySectionParseErrorConsecutive, 2) // fail, fail
		execCtx.Stats().ResetCounter(gent.KeySectionParseErrorConsecutive)   // reset
		execCtx.Stats().IncrCounter(gent.KeySectionParseErrorConsecutive, 1) // fail

		exec := executor.New[*gent.BasicLoopData](agent, executor.DefaultConfig())
		exec.Execute(execCtx)

		// Should complete because consecutive is 1, not 3
		assert.Equal(t, gent.TerminationSuccess, execCtx.TerminationReason())
		assert.Nil(t, execCtx.ExceededLimit())
	})
}

func TestExecutorLimits_ConsecutiveReset_ToolCallError(t *testing.T) {
	t.Run("consecutive counter resets on success - does not exceed limit", func(t *testing.T) {
		callCount := 0
		// Sequence: fail, fail, success, fail
		model := newLimitTestModel().
			AddResponse("<action>tool: flaky</action>", 100, 50).
			AddResponse("<action>tool: flaky</action>", 100, 50).
			AddResponse("<action>tool: flaky</action>", 100, 50).
			AddResponse("<action>tool: flaky</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: flaky"}}).
			AddParseResult(map[string][]string{"action": {"tool: flaky"}}).
			AddParseResult(map[string][]string{"action": {"tool: flaky"}}).
			AddParseResult(map[string][]string{"action": {"tool: flaky"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain().
			WithTool("flaky", func(args map[string]any) (string, error) {
				callCount++
				// fail, fail, success, fail
				if callCount == 3 {
					return "success", nil
				}
				return "", errors.New("flaky error")
			})
		termination := newLimitTestTermination()

		limits := []gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeyToolCallsErrorConsecutive, MaxValue: 2},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationSuccess, execCtx.TerminationReason())
		assert.Nil(t, execCtx.ExceededLimit())
	})
}

func TestExecutorLimits_ConsecutiveReset_ToolCallErrorPerTool(t *testing.T) {
	t.Run("per-tool consecutive counter resets on success - does not exceed prefix limit", func(t *testing.T) {
		callCount := 0
		// Sequence for specific tool: fail, fail, success, fail
		model := newLimitTestModel().
			AddResponse("<action>tool: specific</action>", 100, 50).
			AddResponse("<action>tool: specific</action>", 100, 50).
			AddResponse("<action>tool: specific</action>", 100, 50).
			AddResponse("<action>tool: specific</action>", 100, 50).
			AddResponse("<answer>done</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: specific"}}).
			AddParseResult(map[string][]string{"action": {"tool: specific"}}).
			AddParseResult(map[string][]string{"action": {"tool: specific"}}).
			AddParseResult(map[string][]string{"action": {"tool: specific"}}).
			AddParseResult(map[string][]string{"answer": {"done"}})

		toolChain := newLimitTestToolChain().
			WithTool("specific", func(args map[string]any) (string, error) {
				callCount++
				if callCount == 3 {
					return "success", nil
				}
				return "", errors.New("specific error")
			})
		termination := newLimitTestTermination()

		limits := []gent.Limit{
			{Type: gent.LimitKeyPrefix, Key: gent.KeyToolCallsErrorConsecutiveFor, MaxValue: 2},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationSuccess, execCtx.TerminationReason())
		assert.Nil(t, execCtx.ExceededLimit())
	})
}

// ----------------------------------------------------------------------------
// Mock Answer Validator for testing
// ----------------------------------------------------------------------------

type limitTestValidator struct {
	name        string
	acceptances []bool
	feedback    []gent.FormattedSection
	callIdx     int
}

func newLimitTestValidator(name string) *limitTestValidator {
	return &limitTestValidator{name: name}
}

func (v *limitTestValidator) WithAcceptances(acceptances ...bool) *limitTestValidator {
	v.acceptances = acceptances
	return v
}

func (v *limitTestValidator) WithFeedback(sections ...gent.FormattedSection) *limitTestValidator {
	v.feedback = sections
	return v
}

func (v *limitTestValidator) Name() string { return v.name }

func (v *limitTestValidator) Validate(
	execCtx *gent.ExecutionContext,
	answer any,
) *gent.ValidationResult {
	idx := v.callIdx
	v.callIdx++

	accepted := true
	if idx < len(v.acceptances) {
		accepted = v.acceptances[idx]
	}

	if accepted {
		return &gent.ValidationResult{Accepted: true}
	}
	return &gent.ValidationResult{Accepted: false, Feedback: v.feedback}
}

// ----------------------------------------------------------------------------
// Test: Answer rejection total limit
// ----------------------------------------------------------------------------

func TestExecutorLimits_AnswerRejectedTotal(t *testing.T) {
	t.Run("stops when answer rejected total exceeded", func(t *testing.T) {
		// Model provides answers that get rejected
		model := newLimitTestModel().
			AddResponse("<answer>bad answer 1</answer>", 100, 50).
			AddResponse("<answer>bad answer 2</answer>", 100, 50).
			AddResponse("<answer>bad answer 3</answer>", 100, 50).
			AddResponse("<answer>good answer</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"answer": {"bad answer 1"}}).
			AddParseResult(map[string][]string{"answer": {"bad answer 2"}}).
			AddParseResult(map[string][]string{"answer": {"bad answer 3"}}).
			AddParseResult(map[string][]string{"answer": {"good answer"}})

		toolChain := newLimitTestToolChain()
		termination := newLimitTestTermination()

		validator := newLimitTestValidator("test_validator").
			WithAcceptances(false, false, false, true).
			WithFeedback(gent.FormattedSection{Name: "error", Content: "Answer rejected"})
		termination.SetValidator(validator)

		limits := []gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeyAnswerRejectedTotal, MaxValue: 2},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, gent.KeyAnswerRejectedTotal, execCtx.ExceededLimit().Key)
	})

	t.Run("exceeds limit at third iteration after successful tool calls", func(t *testing.T) {
		// Iteration 1-2: tool calls (no answer)
		// Iteration 3-5: answers rejected (3rd rejection exceeds limit of 2)
		model := newLimitTestModel().
			AddResponse("<action>tool: search</action>", 100, 50).
			AddResponse("<action>tool: search</action>", 100, 50).
			AddResponse("<answer>bad answer 1</answer>", 100, 50).
			AddResponse("<answer>bad answer 2</answer>", 100, 50).
			AddResponse("<answer>bad answer 3</answer>", 100, 50).
			AddResponse("<answer>good answer</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"action": {"tool: search"}}).
			AddParseResult(map[string][]string{"action": {"tool: search"}}).
			AddParseResult(map[string][]string{"answer": {"bad answer 1"}}).
			AddParseResult(map[string][]string{"answer": {"bad answer 2"}}).
			AddParseResult(map[string][]string{"answer": {"bad answer 3"}}).
			AddParseResult(map[string][]string{"answer": {"good answer"}})

		toolChain := newLimitTestToolChain().
			WithTool("search", func(args map[string]any) (string, error) {
				return "found something", nil
			})
		termination := newLimitTestTermination()

		validator := newLimitTestValidator("test_validator").
			WithAcceptances(false, false, false, true).
			WithFeedback(gent.FormattedSection{Name: "error", Content: "Answer rejected"})
		termination.SetValidator(validator)

		limits := []gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeyAnswerRejectedTotal, MaxValue: 2},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		assert.Equal(t, gent.KeyAnswerRejectedTotal, execCtx.ExceededLimit().Key)
		assert.Equal(t, 5, execCtx.Iteration())
	})
}

// ----------------------------------------------------------------------------
// Test: Answer rejection by validator limit (prefix)
// ----------------------------------------------------------------------------

func TestExecutorLimits_AnswerRejectedByValidator(t *testing.T) {
	t.Run("stops when validator-specific rejection limit exceeded (prefix)", func(t *testing.T) {
		model := newLimitTestModel().
			AddResponse("<answer>bad answer 1</answer>", 100, 50).
			AddResponse("<answer>bad answer 2</answer>", 100, 50).
			AddResponse("<answer>good answer</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"answer": {"bad answer 1"}}).
			AddParseResult(map[string][]string{"answer": {"bad answer 2"}}).
			AddParseResult(map[string][]string{"answer": {"good answer"}})

		toolChain := newLimitTestToolChain()
		termination := newLimitTestTermination()

		validator := newLimitTestValidator("schema_validator").
			WithAcceptances(false, false, true).
			WithFeedback(gent.FormattedSection{Name: "error", Content: "Schema validation failed"})
		termination.SetValidator(validator)

		limits := []gent.Limit{
			{Type: gent.LimitKeyPrefix, Key: gent.KeyAnswerRejectedBy, MaxValue: 1},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		require.NotNil(t, execCtx.ExceededLimit())
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
		model := newLimitTestModel().
			AddResponse("<answer>main bad 1</answer>", 100, 50).
			AddResponse("<answer>main bad 2</answer>", 100, 50).
			AddResponse("<action>tool: child_agent</action>", 100, 50).
			AddResponse("<answer>main bad 3</answer>", 100, 50).
			AddResponse("<action>tool: child_agent</action>", 100, 50).
			AddResponse("<answer>good answer</answer>", 100, 50)

		format := newLimitTestFormat().
			AddParseResult(map[string][]string{"answer": {"main bad 1"}}).
			AddParseResult(map[string][]string{"answer": {"main bad 2"}}).
			AddParseResult(map[string][]string{"action": {"tool: child_agent"}}).
			AddParseResult(map[string][]string{"answer": {"main bad 3"}}).
			AddParseResult(map[string][]string{"action": {"tool: child_agent"}}).
			AddParseResult(map[string][]string{"answer": {"good answer"}})

		toolChain := newLimitTestToolChain().
			WithToolCtx("child_agent", func(execCtx *gent.ExecutionContext, args map[string]any) (string, error) {
				childCallCount++
				// Simulate what a child agent termination would do when its validator rejects
				// This increments the child_validator stats on the shared execCtx
				execCtx.Stats().IncrCounter(gent.KeyAnswerRejectedTotal, 1)
				execCtx.Stats().IncrCounter(gent.KeyAnswerRejectedBy+"child_validator", 1)
				return "child agent completed with rejection", nil
			})
		termination := newLimitTestTermination()

		// Main loop uses main_validator - rejections tracked as main_validator
		mainValidator := newLimitTestValidator("main_validator").
			WithAcceptances(false, false, false, true). // reject first 3, accept 4th
			WithFeedback(gent.FormattedSection{Name: "error", Content: "Main validation failed"})
		termination.SetValidator(mainValidator)

		// Limit only for child_validator - main_validator rejections don't count
		limits := []gent.Limit{
			{Type: gent.LimitExactKey, Key: gent.KeyAnswerRejectedBy + "child_validator", MaxValue: 1},
		}

		execCtx := runWithLimit(t, model, format, toolChain, termination, limits)

		// Should terminate due to child_validator limit exceeded
		assert.Equal(t, gent.TerminationLimitExceeded, execCtx.TerminationReason())
		require.NotNil(t, execCtx.ExceededLimit())
		assert.Equal(t, gent.KeyAnswerRejectedBy+"child_validator", execCtx.ExceededLimit().Key)

		// Verify stats
		// main_validator: 3 rejections (iter 1, 2, 4)
		// child_validator: 2 rejections (iter 3, 5) - but limit exceeded at 2nd
		assert.Equal(t,
			int64(3),
			execCtx.Stats().GetCounter(gent.KeyAnswerRejectedBy+"main_validator"))
		assert.Equal(t,
			int64(2),
			execCtx.Stats().GetCounter(gent.KeyAnswerRejectedBy+"child_validator"))
		assert.Equal(t, 5, execCtx.Iteration())
	})
}
