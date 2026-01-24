package react

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
)

// ----------------------------------------------------------------------------
// Mock Model for testing
// ----------------------------------------------------------------------------

type mockModel struct {
	responses []*gent.ContentResponse
	errors    []error
	callCount int
}

func newMockModel(responses ...*gent.ContentResponse) *mockModel {
	return &mockModel{responses: responses}
}

func (m *mockModel) WithErrors(errs ...error) *mockModel {
	m.errors = errs
	return m
}

func (m *mockModel) GenerateContent(
	_ context.Context,
	_ *gent.ExecutionContext,
	_ string,
	_ string,
	_ []llms.MessageContent,
	_ ...llms.CallOption,
) (*gent.ContentResponse, error) {
	idx := m.callCount
	m.callCount++

	if idx < len(m.errors) && m.errors[idx] != nil {
		return nil, m.errors[idx]
	}

	if idx < len(m.responses) {
		return m.responses[idx], nil
	}

	return &gent.ContentResponse{Choices: []*gent.ContentChoice{{Content: ""}}}, nil
}

// ----------------------------------------------------------------------------
// Mock ToolChain for testing
// ----------------------------------------------------------------------------

type mockToolChain struct {
	name      string
	prompt    string
	results   []*gent.ToolChainResult
	errors    []error
	callCount int
}

func newMockToolChain() *mockToolChain {
	return &mockToolChain{name: "action", prompt: "Use tools here."}
}

func (m *mockToolChain) WithResults(results ...*gent.ToolChainResult) *mockToolChain {
	m.results = results
	return m
}

func (m *mockToolChain) WithErrors(errs ...error) *mockToolChain {
	m.errors = errs
	return m
}

func (m *mockToolChain) Name() string   { return m.name }
func (m *mockToolChain) Prompt() string { return m.prompt }

func (m *mockToolChain) ParseSection(content string) (any, error) {
	return content, nil
}

func (m *mockToolChain) RegisterTool(_ any) gent.ToolChain {
	return m
}

func (m *mockToolChain) Execute(
	_ context.Context,
	_ *gent.ExecutionContext,
	_ string,
) (*gent.ToolChainResult, error) {
	idx := m.callCount
	m.callCount++

	if idx < len(m.errors) && m.errors[idx] != nil {
		return nil, m.errors[idx]
	}

	if idx < len(m.results) {
		return m.results[idx], nil
	}

	return &gent.ToolChainResult{}, nil
}

// ----------------------------------------------------------------------------
// Mock Termination for testing
// ----------------------------------------------------------------------------

type mockTermination struct {
	name          string
	prompt        string
	shouldTermRes []gent.ContentPart
}

func newMockTermination() *mockTermination {
	return &mockTermination{name: "answer", prompt: "Write your final answer."}
}

func (m *mockTermination) WithTerminationResult(parts ...gent.ContentPart) *mockTermination {
	m.shouldTermRes = parts
	return m
}

func (m *mockTermination) Name() string   { return m.name }
func (m *mockTermination) Prompt() string { return m.prompt }

func (m *mockTermination) ParseSection(content string) (any, error) {
	return content, nil
}

func (m *mockTermination) ShouldTerminate(content string) []gent.ContentPart {
	if content != "" && m.shouldTermRes != nil {
		return m.shouldTermRes
	}
	if content != "" {
		return []gent.ContentPart{llms.TextContent{Text: content}}
	}
	return nil
}

// ----------------------------------------------------------------------------
// Mock TextOutputFormat for testing
// ----------------------------------------------------------------------------

type mockFormat struct {
	parseResult map[string][]string
	parseErr    error
}

func newMockFormat() *mockFormat {
	return &mockFormat{parseResult: make(map[string][]string)}
}

func (m *mockFormat) WithParseResult(result map[string][]string) *mockFormat {
	m.parseResult = result
	return m
}

func (m *mockFormat) WithParseError(err error) *mockFormat {
	m.parseErr = err
	return m
}

func (m *mockFormat) DescribeStructure(sections []gent.TextOutputSection) string {
	return "mock format structure"
}

func (m *mockFormat) Parse(_ string) (map[string][]string, error) {
	if m.parseErr != nil {
		return nil, m.parseErr
	}
	return m.parseResult, nil
}

// ----------------------------------------------------------------------------
// Helper to create ExecutionContext for tests
// ----------------------------------------------------------------------------

func newTestExecCtx(data gent.LoopData) *gent.ExecutionContext {
	return gent.NewExecutionContext("test", data)
}

// ----------------------------------------------------------------------------
// LoopData Tests
// ----------------------------------------------------------------------------

func TestLoopData_GetTask(t *testing.T) {
	input := []gent.ContentPart{llms.TextContent{Text: "test input"}}
	data := NewLoopData(input...)

	result := data.GetTask()
	if len(result) != 1 {
		t.Fatalf("expected 1 part, got %d", len(result))
	}

	tc, ok := result[0].(llms.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result[0])
	}
	if tc.Text != "test input" {
		t.Errorf("expected 'test input', got %q", tc.Text)
	}
}

func TestLoopData_IterationHistory(t *testing.T) {
	data := NewLoopData()

	// Initially empty
	if len(data.GetIterationHistory()) != 0 {
		t.Fatalf("expected empty history, got %d", len(data.GetIterationHistory()))
	}

	// Add iteration
	iter := &gent.Iteration{
		Messages: []gent.MessageContent{
			{Role: llms.ChatMessageTypeAI, Parts: []gent.ContentPart{llms.TextContent{Text: "test"}}},
		},
	}
	data.AddIterationHistory(iter)

	history := data.GetIterationHistory()
	if len(history) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(history))
	}
	if len(history[0].Messages) != 1 {
		t.Fatalf("expected 1 message in iteration, got %d", len(history[0].Messages))
	}
}

func TestLoopData_ScratchPad(t *testing.T) {
	data := NewLoopData()

	// Initially empty
	if len(data.GetScratchPad()) != 0 {
		t.Fatalf("expected empty scratchpad, got %d", len(data.GetScratchPad()))
	}

	// Set scratchpad
	iter := &gent.Iteration{
		Messages: []gent.MessageContent{
			{Role: llms.ChatMessageTypeAI, Parts: []gent.ContentPart{llms.TextContent{Text: "test"}}},
		},
	}
	data.SetScratchPad([]*gent.Iteration{iter})

	scratchpad := data.GetScratchPad()
	if len(scratchpad) != 1 {
		t.Fatalf("expected 1 iteration in scratchpad, got %d", len(scratchpad))
	}
}

// ----------------------------------------------------------------------------
// Agent Tests
// ----------------------------------------------------------------------------

func TestAgent_BuildOutputSections(t *testing.T) {
	model := newMockModel()
	tc := newMockToolChain()
	term := newMockTermination()

	loop := NewAgent(model).
		WithToolChain(tc).
		WithTermination(term)

	sections := loop.buildOutputSections()
	if len(sections) != 2 {
		t.Fatalf("expected 2 sections (toolchain, termination), got %d", len(sections))
	}

	// With thinking
	loop.WithThinking("Think step by step")
	sections = loop.buildOutputSections()
	if len(sections) != 3 {
		t.Fatalf("expected 3 sections (thinking, toolchain, termination), got %d", len(sections))
	}
}

func TestAgent_BuildMessages(t *testing.T) {
	model := newMockModel()
	format := newMockFormat()
	tc := newMockToolChain()
	term := newMockTermination()

	loop := NewAgent(model).
		WithBehaviorAndContext("You are helpful.").
		WithFormat(format).
		WithToolChain(tc).
		WithTermination(term)

	data := NewLoopData(llms.TextContent{Text: "Hello"})

	messages := loop.buildMessages(data, "output prompt", "tools prompt")

	// Should have system message + user message
	if len(messages) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(messages))
	}

	// Check system message
	if messages[0].Role != llms.ChatMessageTypeSystem {
		t.Errorf("expected system role, got %v", messages[0].Role)
	}

	// Check user message
	if messages[1].Role != llms.ChatMessageTypeHuman {
		t.Errorf("expected human role, got %v", messages[1].Role)
	}
}

func TestAgent_Next_Termination(t *testing.T) {
	response := &gent.ContentResponse{
		Choices: []*gent.ContentChoice{{Content: "<answer>The answer is 42</answer>"}},
	}
	model := newMockModel(response)

	format := newMockFormat().WithParseResult(map[string][]string{
		"answer": {"The answer is 42"},
	})
	tc := newMockToolChain()
	term := newMockTermination()

	loop := NewAgent(model).
		WithFormat(format).
		WithToolChain(tc).
		WithTermination(term)

	data := NewLoopData(llms.TextContent{Text: "What is 6*7?"})
	execCtx := newTestExecCtx(data)
	result, err := loop.Next(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Action != gent.LATerminate {
		t.Errorf("expected LATerminate, got %v", result.Action)
	}

	if len(result.Result) != 1 {
		t.Fatalf("expected 1 result part, got %d", len(result.Result))
	}

	tc2, ok := result.Result[0].(llms.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Result[0])
	}
	if tc2.Text != "The answer is 42" {
		t.Errorf("expected 'The answer is 42', got %q", tc2.Text)
	}
}

func TestAgent_Next_ToolExecution(t *testing.T) {
	response := &gent.ContentResponse{
		Choices: []*gent.ContentChoice{{Content: "<action>tool: search\nargs:\n  q: test</action>"}},
	}
	model := newMockModel(response)

	format := newMockFormat().WithParseResult(map[string][]string{
		"action": {"tool: search\nargs:\n  q: test"},
	})
	tc := newMockToolChain().WithResults(&gent.ToolChainResult{
		Calls: []*gent.ToolCall{{Name: "search", Args: map[string]any{"q": "test"}}},
		Results: []*gent.ToolCallResult{{
			Name:   "search",
			Result: []gent.ContentPart{llms.TextContent{Text: "found it"}},
		}},
		Errors: []error{nil},
	})
	term := newMockTermination()

	loop := NewAgent(model).
		WithFormat(format).
		WithToolChain(tc).
		WithTermination(term)

	data := NewLoopData(llms.TextContent{Text: "Search for test"})
	execCtx := newTestExecCtx(data)
	result, err := loop.Next(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Action != gent.LAContinue {
		t.Errorf("expected LAContinue, got %v", result.Action)
	}

	if result.NextPrompt == "" {
		t.Error("expected NextPrompt to be set")
	}

	// Check that iteration was added to scratchpad
	if len(data.GetScratchPad()) != 1 {
		t.Errorf("expected 1 iteration in scratchpad, got %d", len(data.GetScratchPad()))
	}
}

func TestAgent_Next_ToolError(t *testing.T) {
	response := &gent.ContentResponse{
		Choices: []*gent.ContentChoice{{Content: "<action>tool: broken</action>"}},
	}
	model := newMockModel(response)

	format := newMockFormat().WithParseResult(map[string][]string{
		"action": {"tool: broken"},
	})
	tc := newMockToolChain().WithResults(&gent.ToolChainResult{
		Calls:   []*gent.ToolCall{{Name: "broken", Args: nil}},
		Results: []*gent.ToolCallResult{nil},
		Errors:  []error{errors.New("tool failed")},
	})
	term := newMockTermination()

	loop := NewAgent(model).
		WithFormat(format).
		WithToolChain(tc).
		WithTermination(term)

	data := NewLoopData(llms.TextContent{Text: "Use broken tool"})
	execCtx := newTestExecCtx(data)
	result, err := loop.Next(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Action != gent.LAContinue {
		t.Errorf("expected LAContinue, got %v", result.Action)
	}

	if result.NextPrompt == "" {
		t.Error("expected NextPrompt with error info")
	}
}

func TestAgent_Next_ModelError(t *testing.T) {
	model := newMockModel().WithErrors(errors.New("model failed"))
	format := newMockFormat()
	tc := newMockToolChain()
	term := newMockTermination()

	loop := NewAgent(model).
		WithFormat(format).
		WithToolChain(tc).
		WithTermination(term)

	data := NewLoopData(llms.TextContent{Text: "Hello"})
	execCtx := newTestExecCtx(data)
	_, err := loop.Next(context.Background(), execCtx)

	// Should return an error when model fails
	if err == nil {
		t.Fatal("expected error from model failure, got nil")
	}
	if !strings.Contains(err.Error(), "model failed") {
		t.Errorf("expected error to contain 'model failed', got %q", err.Error())
	}
}

func TestAgent_Next_ParseError(t *testing.T) {
	response := &gent.ContentResponse{
		Choices: []*gent.ContentChoice{{Content: "invalid response"}},
	}
	model := newMockModel(response)

	format := newMockFormat().WithParseError(gent.ErrNoSectionsFound)
	tc := newMockToolChain()
	// Termination that doesn't accept raw content
	term := &mockTermination{name: "answer", prompt: "answer"}

	loop := NewAgent(model).
		WithFormat(format).
		WithToolChain(tc).
		WithTermination(term)

	data := NewLoopData(llms.TextContent{Text: "Hello"})
	execCtx := newTestExecCtx(data)
	_, err := loop.Next(context.Background(), execCtx)

	// Should return an error since parse failed
	if err == nil {
		t.Error("expected error from parse failure, got nil")
	}
}

func TestAgent_Next_ParseError_FallbackToTermination(t *testing.T) {
	response := &gent.ContentResponse{
		Choices: []*gent.ContentChoice{{Content: "The answer is 42"}},
	}
	model := newMockModel(response)

	format := newMockFormat().WithParseError(gent.ErrNoSectionsFound)
	tc := newMockToolChain()
	term := newMockTermination().
		WithTerminationResult(llms.TextContent{Text: "The answer is 42"})

	loop := NewAgent(model).
		WithFormat(format).
		WithToolChain(tc).
		WithTermination(term)

	data := NewLoopData(llms.TextContent{Text: "What is 6*7?"})
	execCtx := newTestExecCtx(data)
	_, err := loop.Next(context.Background(), execCtx)

	// Should return an error since parse failed (no fallback anymore)
	if err == nil {
		t.Error("expected error from parse failure, got nil")
	}
}

func TestAgent_Next_MultipleTools(t *testing.T) {
	response := &gent.ContentResponse{
		Choices: []*gent.ContentChoice{{Content: "<action>tool: a</action><action>tool: b</action>"}},
	}
	model := newMockModel(response)

	format := newMockFormat().WithParseResult(map[string][]string{
		"action": {"tool: a", "tool: b"},
	})
	tc := newMockToolChain().
		WithResults(
			&gent.ToolChainResult{
				Calls: []*gent.ToolCall{{Name: "a", Args: nil}},
				Results: []*gent.ToolCallResult{{
					Name:   "a",
					Result: []gent.ContentPart{llms.TextContent{Text: "result a"}},
				}},
				Errors: []error{nil},
			},
			&gent.ToolChainResult{
				Calls: []*gent.ToolCall{{Name: "b", Args: nil}},
				Results: []*gent.ToolCallResult{{
					Name:   "b",
					Result: []gent.ContentPart{llms.TextContent{Text: "result b"}},
				}},
				Errors: []error{nil},
			},
		)
	term := newMockTermination()

	loop := NewAgent(model).
		WithFormat(format).
		WithToolChain(tc).
		WithTermination(term)

	data := NewLoopData(llms.TextContent{Text: "Use tools a and b"})
	execCtx := newTestExecCtx(data)
	result, err := loop.Next(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Action != gent.LAContinue {
		t.Errorf("expected LAContinue, got %v", result.Action)
	}

	// Should have results from both tools in the observation
	if result.NextPrompt == "" {
		t.Error("expected NextPrompt to be set")
	}
}

// TestAgent_Next_ActionTakesPriorityOverTermination verifies that when the LLM outputs both
// an action (tool call) and an answer (termination) in the same response, the action takes
// priority. The agent should execute the tool calls and continue the loop, discarding the
// answer for that iteration.
//
// This behavior is critical because:
// 1. Tool calls may fail, and the answer might be premature
// 2. The answer should only be given after observing actual tool results
func TestAgent_Next_ActionTakesPriorityOverTermination(t *testing.T) {
	tests := []struct {
		name  string
		input struct {
			responseContent string
			parsedSections  map[string][]string
			toolResult      *gent.ToolChainResult
		}
		expected struct {
			action           gent.LoopAction
			shouldHavePrompt bool
			promptContains   string
			scratchpadLen    int
			toolChainCalled  bool
			terminationUsed  bool
			shouldNotBeFinal bool
		}
	}{
		{
			name: "action and answer both present - action takes priority",
			input: struct {
				responseContent string
				parsedSections  map[string][]string
				toolResult      *gent.ToolChainResult
			}{
				responseContent: `<thinking>I'll reschedule and confirm</thinking>
<action>
- tool: reschedule_booking
  args:
    booking_id: BK001
</action>
<answer>Your booking has been rescheduled successfully!</answer>`,
				parsedSections: map[string][]string{
					"action": {"- tool: reschedule_booking\n  args:\n    booking_id: BK001"},
					"answer": {"Your booking has been rescheduled successfully!"},
				},
				toolResult: &gent.ToolChainResult{
					Calls: []*gent.ToolCall{{Name: "reschedule_booking", Args: map[string]any{
						"booking_id": "BK001",
					}}},
					Results: []*gent.ToolCallResult{{
						Name:   "reschedule_booking",
						Result: []gent.ContentPart{llms.TextContent{Text: "Booking rescheduled"}},
					}},
					Errors: []error{nil},
				},
			},
			expected: struct {
				action           gent.LoopAction
				shouldHavePrompt bool
				promptContains   string
				scratchpadLen    int
				toolChainCalled  bool
				terminationUsed  bool
				shouldNotBeFinal bool
			}{
				action:           gent.LAContinue,
				shouldHavePrompt: true,
				promptContains:   "reschedule_booking",
				scratchpadLen:    1,
				toolChainCalled:  true,
				terminationUsed:  false,
				shouldNotBeFinal: true,
			},
		},
		{
			name: "only answer present - should terminate",
			input: struct {
				responseContent string
				parsedSections  map[string][]string
				toolResult      *gent.ToolChainResult
			}{
				responseContent: "<answer>The final answer is 42</answer>",
				parsedSections: map[string][]string{
					"answer": {"The final answer is 42"},
				},
				toolResult: nil,
			},
			expected: struct {
				action           gent.LoopAction
				shouldHavePrompt bool
				promptContains   string
				scratchpadLen    int
				toolChainCalled  bool
				terminationUsed  bool
				shouldNotBeFinal bool
			}{
				action:           gent.LATerminate,
				shouldHavePrompt: false,
				promptContains:   "",
				scratchpadLen:    0,
				toolChainCalled:  false,
				terminationUsed:  true,
				shouldNotBeFinal: false,
			},
		},
		{
			name: "only action present - should continue",
			input: struct {
				responseContent string
				parsedSections  map[string][]string
				toolResult      *gent.ToolChainResult
			}{
				responseContent: "<action>- tool: search\n  args:\n    q: test</action>",
				parsedSections: map[string][]string{
					"action": {"- tool: search\n  args:\n    q: test"},
				},
				toolResult: &gent.ToolChainResult{
					Calls: []*gent.ToolCall{{Name: "search", Args: map[string]any{"q": "test"}}},
					Results: []*gent.ToolCallResult{{
						Name:   "search",
						Result: []gent.ContentPart{llms.TextContent{Text: "search results"}},
					}},
					Errors: []error{nil},
				},
			},
			expected: struct {
				action           gent.LoopAction
				shouldHavePrompt bool
				promptContains   string
				scratchpadLen    int
				toolChainCalled  bool
				terminationUsed  bool
				shouldNotBeFinal bool
			}{
				action:           gent.LAContinue,
				shouldHavePrompt: true,
				promptContains:   "search",
				scratchpadLen:    1,
				toolChainCalled:  true,
				terminationUsed:  false,
				shouldNotBeFinal: true,
			},
		},
		{
			name: "action with tool error and answer - action still takes priority",
			input: struct {
				responseContent string
				parsedSections  map[string][]string
				toolResult      *gent.ToolChainResult
			}{
				responseContent: `<action>- tool: failing_tool</action>
<answer>I completed the task!</answer>`,
				parsedSections: map[string][]string{
					"action": {"- tool: failing_tool"},
					"answer": {"I completed the task!"},
				},
				toolResult: &gent.ToolChainResult{
					Calls:   []*gent.ToolCall{{Name: "failing_tool", Args: nil}},
					Results: []*gent.ToolCallResult{nil},
					Errors:  []error{errors.New("tool execution failed")},
				},
			},
			expected: struct {
				action           gent.LoopAction
				shouldHavePrompt bool
				promptContains   string
				scratchpadLen    int
				toolChainCalled  bool
				terminationUsed  bool
				shouldNotBeFinal bool
			}{
				action:           gent.LAContinue,
				shouldHavePrompt: true,
				promptContains:   "Error",
				scratchpadLen:    1,
				toolChainCalled:  true,
				terminationUsed:  false,
				shouldNotBeFinal: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mocks
			response := &gent.ContentResponse{
				Choices: []*gent.ContentChoice{{Content: tt.input.responseContent}},
			}
			model := newMockModel(response)

			format := newMockFormat().WithParseResult(tt.input.parsedSections)

			tc := newMockToolChain()
			if tt.input.toolResult != nil {
				tc = tc.WithResults(tt.input.toolResult)
			}

			term := newMockTermination()

			loop := NewAgent(model).
				WithFormat(format).
				WithToolChain(tc).
				WithTermination(term)

			data := NewLoopData(llms.TextContent{Text: "Execute the task"})
			execCtx := newTestExecCtx(data)

			// Execute
			result, err := loop.Next(context.Background(), execCtx)

			// Assert
			require.NoError(t, err, "Next() should not return an error")
			assert.Equal(t, tt.expected.action, result.Action, "unexpected loop action")

			if tt.expected.shouldHavePrompt {
				assert.NotEmpty(t, result.NextPrompt, "expected NextPrompt to be set")
				assert.Contains(t, result.NextPrompt, tt.expected.promptContains,
					"NextPrompt should contain expected content")
			}

			assert.Equal(t, tt.expected.scratchpadLen, len(data.GetScratchPad()),
				"unexpected scratchpad length")

			if tt.expected.toolChainCalled {
				assert.Equal(t, 1, tc.callCount, "tool chain should have been called")
			} else {
				assert.Equal(t, 0, tc.callCount, "tool chain should not have been called")
			}

			if tt.expected.shouldNotBeFinal {
				assert.Nil(t, result.Result, "result should be nil when continuing")
			}

			if tt.expected.action == gent.LATerminate {
				assert.NotNil(t, result.Result, "result should be set when terminating")
			}
		})
	}
}

func TestAgent_RegisterTool(t *testing.T) {
	model := newMockModel()
	tc := newMockToolChain()

	loop := NewAgent(model).WithToolChain(tc)

	// Should be able to chain RegisterTool
	result := loop.RegisterTool("dummy")
	if result != loop {
		t.Error("expected RegisterTool to return same loop for chaining")
	}
}

func TestSimpleSection(t *testing.T) {
	s := &simpleSection{name: "thinking", prompt: "Think step by step"}

	if s.Name() != "thinking" {
		t.Errorf("expected 'thinking', got %q", s.Name())
	}

	if s.Prompt() != "Think step by step" {
		t.Errorf("expected 'Think step by step', got %q", s.Prompt())
	}

	parsed, err := s.ParseSection("some content")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if parsed != "some content" {
		t.Errorf("expected 'some content', got %v", parsed)
	}
}

func TestNewAgent_Defaults(t *testing.T) {
	model := newMockModel()
	loop := NewAgent(model)

	// Verify defaults are set
	if loop.format == nil {
		t.Error("expected default format to be set")
	}
	if loop.toolChain == nil {
		t.Error("expected default toolChain to be set")
	}
	if loop.termination == nil {
		t.Error("expected default termination to be set")
	}
	if loop.timeProvider == nil {
		t.Error("expected default timeProvider to be set")
	}
	if loop.systemTemplate == nil {
		t.Error("expected default systemTemplate to be set")
	}
	if loop.taskTemplate == nil {
		t.Error("expected default taskTemplate to be set")
	}
}

func TestAgent_WithTimeProvider(t *testing.T) {
	model := newMockModel()
	mockTime := gent.NewMockTimeProvider(time.Date(2025, 6, 15, 14, 30, 0, 0, time.UTC))

	loop := NewAgent(model).WithTimeProvider(mockTime)

	if loop.TimeProvider() != mockTime {
		t.Error("expected custom time provider to be set")
	}

	// Verify the mock time is used
	tp := loop.TimeProvider()
	if tp.Today() != "2025-06-15" {
		t.Errorf("TimeProvider().Today() = %q, want %q", tp.Today(), "2025-06-15")
	}
	if tp.Weekday() != "Sunday" {
		t.Errorf("TimeProvider().Weekday() = %q, want %q", tp.Weekday(), "Sunday")
	}
}

func TestAgent_ProcessTemplateString_ExpandsTemplateVariables(t *testing.T) {
	model := newMockModel()
	mockTime := gent.NewMockTimeProvider(time.Date(2025, 6, 15, 14, 30, 0, 0, time.UTC))

	loop := NewAgent(model).
		WithTimeProvider(mockTime)

	// Call processTemplateString to expand template variables
	result := loop.processTemplateString("Today is {{.Time.Today}} ({{.Time.Weekday}}).")

	expected := "Today is 2025-06-15 (Sunday)."
	if result != expected {
		t.Errorf("processTemplateString() = %q, want %q", result, expected)
	}
}

func TestAgent_ProcessTemplateString_NoTemplateVariables(t *testing.T) {
	model := newMockModel()

	loop := NewAgent(model)

	result := loop.processTemplateString("You are a helpful assistant.")

	expected := "You are a helpful assistant."
	if result != expected {
		t.Errorf("processTemplateString() = %q, want %q", result, expected)
	}
}

func TestAgent_ProcessTemplateString_EmptyInput(t *testing.T) {
	model := newMockModel()

	loop := NewAgent(model)

	result := loop.processTemplateString("")

	if result != "" {
		t.Errorf("processTemplateString() = %q, want empty string", result)
	}
}

func TestAgent_ProcessTemplateString_InvalidTemplate(t *testing.T) {
	model := newMockModel()

	// Invalid template syntax should return original string
	loop := NewAgent(model)

	result := loop.processTemplateString("This has {{ invalid syntax")

	// Should return original string when parsing fails
	expected := "This has {{ invalid syntax"
	if result != expected {
		t.Errorf("processTemplateString() = %q, want %q", result, expected)
	}
}

func TestAgent_ProcessTemplateString_MultipleVariables(t *testing.T) {
	model := newMockModel()
	mockTime := gent.NewMockTimeProvider(time.Date(2024, 12, 25, 10, 0, 0, 0, time.UTC))

	prompt := `## Task
You are helping on {{.Time.Today}}.
It's a {{.Time.Weekday}}.
Current time: {{.Time.Format "15:04"}}`

	loop := NewAgent(model).
		WithTimeProvider(mockTime)

	result := loop.processTemplateString(prompt)

	expected := `## Task
You are helping on 2024-12-25.
It's a Wednesday.
Current time: 10:00`

	if result != expected {
		t.Errorf("processTemplateString() = %q, want %q", result, expected)
	}
}
