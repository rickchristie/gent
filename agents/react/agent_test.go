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
	guidance  string
	results   []*gent.ToolChainResult
	errors    []error
	callCount int
}

func newMockToolChain() *mockToolChain {
	return &mockToolChain{name: "action", guidance: "Use tools here."}
}

func (m *mockToolChain) WithResults(results ...*gent.ToolChainResult) *mockToolChain {
	m.results = results
	return m
}

func (m *mockToolChain) WithErrors(errs ...error) *mockToolChain {
	m.errors = errs
	return m
}

func (m *mockToolChain) Name() string                 { return m.name }
func (m *mockToolChain) Guidance() string             { return m.guidance }
func (m *mockToolChain) AvailableToolsPrompt() string { return "mock available tools prompt" }

func (m *mockToolChain) ParseSection(_ *gent.ExecutionContext, content string) (any, error) {
	return content, nil
}

func (m *mockToolChain) RegisterTool(_ any) gent.ToolChain {
	return m
}

func (m *mockToolChain) Execute(
	_ *gent.ExecutionContext,
	_ string,
	_ gent.TextFormat,
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
	guidance      string
	shouldTermRes []gent.ContentPart
}

func newMockTermination() *mockTermination {
	return &mockTermination{name: "answer", guidance: "Write your final answer."}
}

func (m *mockTermination) WithTerminationResult(parts ...gent.ContentPart) *mockTermination {
	m.shouldTermRes = parts
	return m
}

func (m *mockTermination) Name() string     { return m.name }
func (m *mockTermination) Guidance() string { return m.guidance }

func (m *mockTermination) ParseSection(_ *gent.ExecutionContext, content string) (any, error) {
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
// Mock TextFormat for testing
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

func (m *mockFormat) RegisterSection(_ gent.TextSection) gent.TextFormat {
	return m
}

func (m *mockFormat) DescribeStructure() string {
	return "mock format structure"
}

func (m *mockFormat) Parse(
	execCtx *gent.ExecutionContext,
	output string,
) (map[string][]string, error) {
	if m.parseErr != nil {
		// Trace parse error (following the interface contract)
		if execCtx != nil {
			execCtx.Trace(gent.ParseErrorTrace{
				ErrorType:  "format",
				RawContent: output,
				Error:      m.parseErr,
			})
		}
		return nil, m.parseErr
	}
	// Reset consecutive counter on success
	if execCtx != nil {
		execCtx.Stats().ResetCounter(gent.KeyFormatParseErrorConsecutive)
	}
	return m.parseResult, nil
}

func (m *mockFormat) FormatSections(sections []gent.FormattedSection) string {
	var parts []string
	for _, section := range sections {
		parts = append(parts, m.formatSection(section))
	}
	return strings.Join(parts, "\n")
}

func (m *mockFormat) formatSection(section gent.FormattedSection) string {
	var inner []string
	if section.Content != "" {
		inner = append(inner, section.Content)
	}
	if len(section.Children) > 0 {
		inner = append(inner, m.FormatSections(section.Children))
	}
	return "<" + section.Name + ">\n" + strings.Join(inner, "\n") + "\n</" + section.Name + ">"
}

// ----------------------------------------------------------------------------
// Helper to create ExecutionContext for tests
// ----------------------------------------------------------------------------

func newTestExecCtx(data gent.LoopData) *gent.ExecutionContext {
	return gent.NewExecutionContext(context.Background(), "test", data)
}

// ----------------------------------------------------------------------------
// LoopData Tests
// ----------------------------------------------------------------------------

func TestLoopData_GetTask(t *testing.T) {
	type input struct {
		task *gent.Task
	}

	type expected struct {
		text string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "task with text",
			input: input{
				task: &gent.Task{Text: "test input"},
			},
			expected: expected{
				text: "test input",
			},
		},
		{
			name: "nil task",
			input: input{
				task: nil,
			},
			expected: expected{
				text: "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := NewLoopData(tt.input.task)

			result := data.GetTask()

			if tt.input.task == nil {
				assert.Nil(t, result)
			} else {
				assert.Equal(t, tt.expected.text, result.Text)
			}
		})
	}
}

func TestLoopData_IterationHistory(t *testing.T) {
	data := NewLoopData(nil)

	assert.Empty(t, data.GetIterationHistory(), "expected empty history initially")

	iter := &gent.Iteration{
		Messages: []gent.MessageContent{
			{Role: llms.ChatMessageTypeAI, Parts: []gent.ContentPart{llms.TextContent{Text: "test"}}},
		},
	}
	data.AddIterationHistory(iter)

	history := data.GetIterationHistory()
	assert.Len(t, history, 1)
	assert.Len(t, history[0].Messages, 1)
}

func TestLoopData_ScratchPad(t *testing.T) {
	data := NewLoopData(nil)

	assert.Empty(t, data.GetScratchPad(), "expected empty scratchpad initially")

	iter := &gent.Iteration{
		Messages: []gent.MessageContent{
			{Role: llms.ChatMessageTypeAI, Parts: []gent.ContentPart{llms.TextContent{Text: "test"}}},
		},
	}
	data.SetScratchPad([]*gent.Iteration{iter})

	scratchpad := data.GetScratchPad()
	assert.Len(t, scratchpad, 1)
}

// ----------------------------------------------------------------------------
// Agent Tests
// ----------------------------------------------------------------------------

func TestAgent_BuildOutputSections(t *testing.T) {
	type input struct {
		withThinking bool
	}

	type expected struct {
		sectionCount int
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name:     "without thinking",
			input:    input{withThinking: false},
			expected: expected{sectionCount: 2},
		},
		{
			name:     "with thinking",
			input:    input{withThinking: true},
			expected: expected{sectionCount: 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := newMockModel()
			tc := newMockToolChain()
			term := newMockTermination()

			loop := NewAgent(model).
				WithToolChain(tc).
				WithTermination(term)

			if tt.input.withThinking {
				loop.WithThinking("Think step by step")
			}

			sections := loop.buildOutputSections()

			assert.Len(t, sections, tt.expected.sectionCount)
		})
	}
}

func TestAgent_BuildMessages(t *testing.T) {
	t.Run("without scratchpad shows BEGIN", func(t *testing.T) {
		model := newMockModel()
		format := newMockFormat()
		tc := newMockToolChain()
		term := newMockTermination()

		loop := NewAgent(model).
			WithBehaviorAndContext("You are helpful.").
			WithFormat(format).
			WithToolChain(tc).
			WithTermination(term)

		data := NewLoopData(&gent.Task{Text: "Hello"})

		messages := loop.buildMessages(data, "output prompt", "tools prompt")

		// Expected structure: system, task, BEGIN!
		require.Len(t, messages, 3, "expected 3 messages: system, task, BEGIN!")

		// Message 1: System prompt
		assert.Equal(t, llms.ChatMessageTypeSystem, messages[0].Role)

		// Message 2: Task (role: user)
		assert.Equal(t, llms.ChatMessageTypeHuman, messages[1].Role)
		taskText, ok := messages[1].Parts[0].(llms.TextContent)
		require.True(t, ok)
		assert.Contains(t, taskText.Text, "Hello")

		// Message 3: BEGIN! (role: user)
		assert.Equal(t, llms.ChatMessageTypeHuman, messages[2].Role)
		beginText, ok := messages[2].Parts[0].(llms.TextContent)
		require.True(t, ok)
		assert.Equal(t, "BEGIN!", beginText.Text)
	})

	t.Run("with scratchpad shows CONTINUE and interleaved messages", func(t *testing.T) {
		model := newMockModel()
		format := newMockFormat()
		tc := newMockToolChain()
		term := newMockTermination()

		loop := NewAgent(model).
			WithFormat(format).
			WithToolChain(tc).
			WithTermination(term)

		data := NewLoopData(&gent.Task{Text: "Do something"})

		// Add scratchpad with one iteration (AI response + observation)
		iter := &gent.Iteration{
			Messages: []gent.MessageContent{
				{
					Role:  llms.ChatMessageTypeAI,
					Parts: []gent.ContentPart{llms.TextContent{Text: "thinking..."}},
				},
				{
					Role:  llms.ChatMessageTypeHuman,
					Parts: []gent.ContentPart{llms.TextContent{Text: "tool result"}},
				},
			},
		}
		data.SetScratchPad([]*gent.Iteration{iter})

		messages := loop.buildMessages(data, "output prompt", "tools prompt")

		// Expected: system, task, AI, observation, CONTINUE!
		require.Len(t, messages, 5, "expected 5 messages: system, task, AI, observation, CONTINUE!")

		assert.Equal(t, llms.ChatMessageTypeSystem, messages[0].Role)
		assert.Equal(t, llms.ChatMessageTypeHuman, messages[1].Role) // task
		assert.Equal(t, llms.ChatMessageTypeAI, messages[2].Role)    // scratchpad AI
		assert.Equal(t, llms.ChatMessageTypeHuman, messages[3].Role) // scratchpad observation

		// Last message: CONTINUE!
		assert.Equal(t, llms.ChatMessageTypeHuman, messages[4].Role)
		continueText, ok := messages[4].Parts[0].(llms.TextContent)
		require.True(t, ok)
		assert.Equal(t, "CONTINUE!", continueText.Text)
	})

	t.Run("panics when task is empty", func(t *testing.T) {
		model := newMockModel()
		format := newMockFormat()
		tc := newMockToolChain()
		term := newMockTermination()

		loop := NewAgent(model).
			WithFormat(format).
			WithToolChain(tc).
			WithTermination(term)

		data := NewLoopData(&gent.Task{Text: "", Media: nil})

		assert.Panics(t, func() {
			loop.buildMessages(data, "output prompt", "tools prompt")
		})
	})

	t.Run("panics when task is nil", func(t *testing.T) {
		model := newMockModel()
		format := newMockFormat()
		tc := newMockToolChain()
		term := newMockTermination()

		loop := NewAgent(model).
			WithFormat(format).
			WithToolChain(tc).
			WithTermination(term)

		data := NewLoopData(nil)

		assert.Panics(t, func() {
			loop.buildMessages(data, "output prompt", "tools prompt")
		})
	})
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

	data := NewLoopData(&gent.Task{Text: "What is 6*7?"})
	execCtx := newTestExecCtx(data)
	result, err := loop.Next(execCtx)

	require.NoError(t, err)
	assert.Equal(t, gent.LATerminate, result.Action)
	require.Len(t, result.Result, 1)

	tc2, ok := result.Result[0].(llms.TextContent)
	require.True(t, ok, "expected TextContent, got %T", result.Result[0])
	assert.Equal(t, "The answer is 42", tc2.Text)
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
		Text: "<observation>\n<search>\nfound it\n</search>\n</observation>",
		Raw: &gent.RawToolChainResult{
			Calls:   []*gent.ToolCall{{Name: "search", Args: map[string]any{"q": "test"}}},
			Results: []*gent.RawToolCallResult{{Name: "search", Output: "found it"}},
			Errors:  []error{nil},
		},
	})
	term := newMockTermination()

	loop := NewAgent(model).
		WithFormat(format).
		WithToolChain(tc).
		WithTermination(term)

	data := NewLoopData(&gent.Task{Text: "Search for test"})
	execCtx := newTestExecCtx(data)
	result, err := loop.Next(execCtx)

	require.NoError(t, err)
	assert.Equal(t, gent.LAContinue, result.Action)
	assert.NotEmpty(t, result.NextPrompt)
	assert.Len(t, data.GetScratchPad(), 1)
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
		Text: "<observation>\n<broken>\nError: tool failed\n</broken>\n</observation>",
		Raw: &gent.RawToolChainResult{
			Calls:   []*gent.ToolCall{{Name: "broken", Args: nil}},
			Results: []*gent.RawToolCallResult{nil},
			Errors:  []error{errors.New("tool failed")},
		},
	})
	term := newMockTermination()

	loop := NewAgent(model).
		WithFormat(format).
		WithToolChain(tc).
		WithTermination(term)

	data := NewLoopData(&gent.Task{Text: "Use broken tool"})
	execCtx := newTestExecCtx(data)
	result, err := loop.Next(execCtx)

	require.NoError(t, err)
	assert.Equal(t, gent.LAContinue, result.Action)
	assert.NotEmpty(t, result.NextPrompt)
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

	data := NewLoopData(&gent.Task{Text: "Hello"})
	execCtx := newTestExecCtx(data)
	_, err := loop.Next(execCtx)

	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "model failed"),
		"expected error to contain 'model failed', got %q", err.Error())
}

func TestAgent_Next_ParseError_FeedsBackAsObservation(t *testing.T) {
	// When format parsing fails, the agent should continue with the error fed back
	// as an observation, allowing the model to recover in the next iteration.
	response := &gent.ContentResponse{
		Choices: []*gent.ContentChoice{{Content: "invalid response"}},
	}
	model := newMockModel(response)

	format := newMockFormat().WithParseError(gent.ErrNoSectionsFound)
	tc := newMockToolChain()
	term := &mockTermination{name: "answer", guidance: "answer"}

	loop := NewAgent(model).
		WithFormat(format).
		WithToolChain(tc).
		WithTermination(term)

	data := NewLoopData(&gent.Task{Text: "Hello"})
	execCtx := newTestExecCtx(data)
	result, err := loop.Next(execCtx)

	// Should not return error - instead feeds back as observation
	assert.NoError(t, err)
	assert.Equal(t, gent.LAContinue, result.Action)
	assert.Contains(t, result.NextPrompt, "Format parse error")
	assert.Contains(t, result.NextPrompt, "invalid response")

	// Scratchpad should have the iteration with error feedback
	assert.Len(t, data.GetScratchPad(), 1)
}

func TestAgent_Next_ParseError_TracesError(t *testing.T) {
	// Parse errors should be traced for stats tracking
	response := &gent.ContentResponse{
		Choices: []*gent.ContentChoice{{Content: "unparseable content"}},
	}
	model := newMockModel(response)

	format := newMockFormat().WithParseError(gent.ErrNoSectionsFound)
	tc := newMockToolChain()
	term := newMockTermination()

	loop := NewAgent(model).
		WithFormat(format).
		WithToolChain(tc).
		WithTermination(term)

	data := NewLoopData(&gent.Task{Text: "Test"})
	execCtx := newTestExecCtx(data)
	_, err := loop.Next(execCtx)

	assert.NoError(t, err)

	// Verify parse error was traced (stats updated)
	stats := execCtx.Stats()
	assert.Equal(t, int64(1), stats.GetCounter(gent.KeyFormatParseErrorTotal))
	assert.Equal(t, int64(1), stats.GetCounter(gent.KeyFormatParseErrorConsecutive))
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
				Text: "<observation>\n<a>\nresult a\n</a>\n</observation>",
				Raw: &gent.RawToolChainResult{
					Calls:   []*gent.ToolCall{{Name: "a", Args: nil}},
					Results: []*gent.RawToolCallResult{{Name: "a", Output: "result a"}},
					Errors:  []error{nil},
				},
			},
			&gent.ToolChainResult{
				Text: "<observation>\n<b>\nresult b\n</b>\n</observation>",
				Raw: &gent.RawToolChainResult{
					Calls:   []*gent.ToolCall{{Name: "b", Args: nil}},
					Results: []*gent.RawToolCallResult{{Name: "b", Output: "result b"}},
					Errors:  []error{nil},
				},
			},
		)
	term := newMockTermination()

	loop := NewAgent(model).
		WithFormat(format).
		WithToolChain(tc).
		WithTermination(term)

	data := NewLoopData(&gent.Task{Text: "Use tools a and b"})
	execCtx := newTestExecCtx(data)
	result, err := loop.Next(execCtx)

	require.NoError(t, err)
	assert.Equal(t, gent.LAContinue, result.Action)
	assert.NotEmpty(t, result.NextPrompt)
}

func TestAgent_Next_ActionTakesPriorityOverTermination(t *testing.T) {
	type input struct {
		responseContent string
		parsedSections  map[string][]string
		toolResult      *gent.ToolChainResult
	}

	type expected struct {
		action           gent.LoopAction
		shouldHavePrompt bool
		promptContains   string
		scratchpadLen    int
		toolChainCalled  bool
		shouldNotBeFinal bool
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "action and answer both present - action takes priority",
			input: input{
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
					Text: "<observation>\n<reschedule_booking>\nBooking rescheduled\n</reschedule_booking>\n</observation>",
					Raw: &gent.RawToolChainResult{
						Calls: []*gent.ToolCall{{Name: "reschedule_booking", Args: map[string]any{
							"booking_id": "BK001",
						}}},
						Results: []*gent.RawToolCallResult{{Name: "reschedule_booking", Output: "Booking rescheduled"}},
						Errors:  []error{nil},
					},
				},
			},
			expected: expected{
				action:           gent.LAContinue,
				shouldHavePrompt: true,
				promptContains:   "reschedule_booking",
				scratchpadLen:    1,
				toolChainCalled:  true,
				shouldNotBeFinal: true,
			},
		},
		{
			name: "only answer present - should terminate",
			input: input{
				responseContent: "<answer>The final answer is 42</answer>",
				parsedSections: map[string][]string{
					"answer": {"The final answer is 42"},
				},
				toolResult: nil,
			},
			expected: expected{
				action:           gent.LATerminate,
				shouldHavePrompt: false,
				promptContains:   "",
				scratchpadLen:    0,
				toolChainCalled:  false,
				shouldNotBeFinal: false,
			},
		},
		{
			name: "only action present - should continue",
			input: input{
				responseContent: "<action>- tool: search\n  args:\n    q: test</action>",
				parsedSections: map[string][]string{
					"action": {"- tool: search\n  args:\n    q: test"},
				},
				toolResult: &gent.ToolChainResult{
					Text: "<observation>\n<search>\nsearch results\n</search>\n</observation>",
					Raw: &gent.RawToolChainResult{
						Calls:   []*gent.ToolCall{{Name: "search", Args: map[string]any{"q": "test"}}},
						Results: []*gent.RawToolCallResult{{Name: "search", Output: "search results"}},
						Errors:  []error{nil},
					},
				},
			},
			expected: expected{
				action:           gent.LAContinue,
				shouldHavePrompt: true,
				promptContains:   "search",
				scratchpadLen:    1,
				toolChainCalled:  true,
				shouldNotBeFinal: true,
			},
		},
		{
			name: "action with tool error and answer - action still takes priority",
			input: input{
				responseContent: `<action>- tool: failing_tool</action>
<answer>I completed the task!</answer>`,
				parsedSections: map[string][]string{
					"action": {"- tool: failing_tool"},
					"answer": {"I completed the task!"},
				},
				toolResult: &gent.ToolChainResult{
					Text: "<observation>\n<failing_tool>\nError: tool execution failed\n</failing_tool>\n</observation>",
					Raw: &gent.RawToolChainResult{
						Calls:   []*gent.ToolCall{{Name: "failing_tool", Args: nil}},
						Results: []*gent.RawToolCallResult{nil},
						Errors:  []error{errors.New("tool execution failed")},
					},
				},
			},
			expected: expected{
				action:           gent.LAContinue,
				shouldHavePrompt: true,
				promptContains:   "Error",
				scratchpadLen:    1,
				toolChainCalled:  true,
				shouldNotBeFinal: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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

			data := NewLoopData(&gent.Task{Text: "Execute the task"})
			execCtx := newTestExecCtx(data)

			result, err := loop.Next(execCtx)

			require.NoError(t, err)
			assert.Equal(t, tt.expected.action, result.Action)

			if tt.expected.shouldHavePrompt {
				assert.NotEmpty(t, result.NextPrompt)
				assert.Contains(t, result.NextPrompt, tt.expected.promptContains)
			}

			assert.Equal(t, tt.expected.scratchpadLen, len(data.GetScratchPad()))

			if tt.expected.toolChainCalled {
				assert.Equal(t, 1, tc.callCount)
			} else {
				assert.Equal(t, 0, tc.callCount)
			}

			if tt.expected.shouldNotBeFinal {
				assert.Nil(t, result.Result)
			}

			if tt.expected.action == gent.LATerminate {
				assert.NotNil(t, result.Result)
			}
		})
	}
}

func TestAgent_RegisterTool(t *testing.T) {
	model := newMockModel()
	tc := newMockToolChain()

	loop := NewAgent(model).WithToolChain(tc)

	result := loop.RegisterTool("dummy")
	assert.Equal(t, loop, result, "expected RegisterTool to return same loop for chaining")
}

func TestNewAgent_Defaults(t *testing.T) {
	model := newMockModel()
	loop := NewAgent(model)

	assert.NotNil(t, loop.format, "expected default format to be set")
	assert.NotNil(t, loop.toolChain, "expected default toolChain to be set")
	assert.NotNil(t, loop.termination, "expected default termination to be set")
	assert.NotNil(t, loop.timeProvider, "expected default timeProvider to be set")
	assert.NotNil(t, loop.systemPromptBuilder, "expected default systemPromptBuilder to be set")
}

func TestAgent_WithTimeProvider(t *testing.T) {
	model := newMockModel()
	mockTime := gent.NewMockTimeProvider(time.Date(2025, 6, 15, 14, 30, 0, 0, time.UTC))

	loop := NewAgent(model).WithTimeProvider(mockTime)

	assert.Equal(t, mockTime, loop.TimeProvider())
	assert.Equal(t, "2025-06-15", loop.TimeProvider().Today())
	assert.Equal(t, "Sunday", loop.TimeProvider().Weekday())
}

func TestDefaultSystemPromptBuilder(t *testing.T) {
	t.Run("formats all sections with TextFormat", func(t *testing.T) {
		format := newMockFormat()
		ctx := SystemPromptContext{
			Format:             format,
			BehaviorAndContext: "You are helpful.",
			CriticalRules:      "Never lie.",
			OutputPrompt:       "Use XML tags.",
			ToolsPrompt:        "Available tools: search",
			Time:               gent.NewDefaultTimeProvider(),
		}

		messages := DefaultSystemPromptBuilder(ctx)

		require.Len(t, messages, 1)
		assert.Equal(t, llms.ChatMessageTypeSystem, messages[0].Role)

		// Check that content contains all formatted sections
		content, ok := messages[0].Parts[0].(llms.TextContent)
		require.True(t, ok)

		// All sections should be formatted with XML tags (from mockFormat)
		assert.Contains(t, content.Text, "<behavior>")
		assert.Contains(t, content.Text, "You are helpful.")
		assert.Contains(t, content.Text, "<re_act>")
		assert.Contains(t, content.Text, "<critical_rules>")
		assert.Contains(t, content.Text, "Never lie.")
		assert.Contains(t, content.Text, "<available_tools>")
		assert.Contains(t, content.Text, "<output_format>")
	})

	t.Run("skips empty optional sections", func(t *testing.T) {
		format := newMockFormat()
		ctx := SystemPromptContext{
			Format:             format,
			BehaviorAndContext: "", // empty
			CriticalRules:      "", // empty
			OutputPrompt:       "Use XML.",
			ToolsPrompt:        "tools",
			Time:               gent.NewDefaultTimeProvider(),
		}

		messages := DefaultSystemPromptBuilder(ctx)

		require.Len(t, messages, 1)
		content, ok := messages[0].Parts[0].(llms.TextContent)
		require.True(t, ok)

		// Required sections should be present
		assert.Contains(t, content.Text, "<re_act>")
		assert.Contains(t, content.Text, "<available_tools>")
		assert.Contains(t, content.Text, "<output_format>")

		// Optional sections should not be present when empty
		assert.NotContains(t, content.Text, "<behavior>")
		assert.NotContains(t, content.Text, "<critical_rules>")
	})
}

func TestAgent_WithSystemPromptBuilder(t *testing.T) {
	t.Run("custom builder is used", func(t *testing.T) {
		model := newMockModel()
		format := newMockFormat()
		tc := newMockToolChain()
		term := newMockTermination()

		customBuilder := func(ctx SystemPromptContext) []gent.MessageContent {
			return []gent.MessageContent{
				{
					Role:  llms.ChatMessageTypeSystem,
					Parts: []gent.ContentPart{llms.TextContent{Text: "Custom system prompt"}},
				},
			}
		}

		loop := NewAgent(model).
			WithFormat(format).
			WithToolChain(tc).
			WithTermination(term).
			WithSystemPromptBuilder(customBuilder)

		data := NewLoopData(&gent.Task{Text: "Hello"})
		messages := loop.buildMessages(data, "output", "tools")

		// First message should be our custom system prompt
		require.GreaterOrEqual(t, len(messages), 1)
		assert.Equal(t, llms.ChatMessageTypeSystem, messages[0].Role)
		content, ok := messages[0].Parts[0].(llms.TextContent)
		require.True(t, ok)
		assert.Equal(t, "Custom system prompt", content.Text)
	})

	t.Run("builder can return multiple messages", func(t *testing.T) {
		model := newMockModel()
		format := newMockFormat()
		tc := newMockToolChain()
		term := newMockTermination()

		multiMessageBuilder := func(ctx SystemPromptContext) []gent.MessageContent {
			return []gent.MessageContent{
				{
					Role:  llms.ChatMessageTypeSystem,
					Parts: []gent.ContentPart{llms.TextContent{Text: "System 1"}},
				},
				{
					Role:  llms.ChatMessageTypeHuman,
					Parts: []gent.ContentPart{llms.TextContent{Text: "Example user"}},
				},
				{
					Role:  llms.ChatMessageTypeAI,
					Parts: []gent.ContentPart{llms.TextContent{Text: "Example response"}},
				},
			}
		}

		loop := NewAgent(model).
			WithFormat(format).
			WithToolChain(tc).
			WithTermination(term).
			WithSystemPromptBuilder(multiMessageBuilder)

		data := NewLoopData(&gent.Task{Text: "Hello"})
		messages := loop.buildMessages(data, "output", "tools")

		// Should have: 3 from builder + 1 task + 1 BEGIN!
		require.Len(t, messages, 5)
		assert.Equal(t, llms.ChatMessageTypeSystem, messages[0].Role)
		assert.Equal(t, llms.ChatMessageTypeHuman, messages[1].Role)
		assert.Equal(t, llms.ChatMessageTypeAI, messages[2].Role)
		assert.Equal(t, llms.ChatMessageTypeHuman, messages[3].Role) // task
		assert.Equal(t, llms.ChatMessageTypeHuman, messages[4].Role) // BEGIN!
	})

	t.Run("builder receives correct context", func(t *testing.T) {
		model := newMockModel()
		format := newMockFormat()
		tc := newMockToolChain()
		term := newMockTermination()
		mockTime := gent.NewMockTimeProvider(time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC))

		var capturedCtx SystemPromptContext
		capturingBuilder := func(ctx SystemPromptContext) []gent.MessageContent {
			capturedCtx = ctx
			return []gent.MessageContent{
				{
					Role:  llms.ChatMessageTypeSystem,
					Parts: []gent.ContentPart{llms.TextContent{Text: "test"}},
				},
			}
		}

		loop := NewAgent(model).
			WithFormat(format).
			WithToolChain(tc).
			WithTermination(term).
			WithTimeProvider(mockTime).
			WithBehaviorAndContext("Be helpful").
			WithCriticalRules("No lies").
			WithSystemPromptBuilder(capturingBuilder)

		data := NewLoopData(&gent.Task{Text: "Hello"})
		loop.buildMessages(data, "output prompt", "tools prompt")

		assert.Equal(t, format, capturedCtx.Format)
		assert.Equal(t, "Be helpful", capturedCtx.BehaviorAndContext)
		assert.Equal(t, "No lies", capturedCtx.CriticalRules)
		assert.Equal(t, "output prompt", capturedCtx.OutputPrompt)
		assert.Equal(t, "tools prompt", capturedCtx.ToolsPrompt)
		assert.Equal(t, mockTime, capturedCtx.Time)
	})
}

