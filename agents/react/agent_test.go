package react

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/format"
	"github.com/rickchristie/gent/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
)

// ----------------------------------------------------------------------------
// Mock Model for testing
// ----------------------------------------------------------------------------

type mockModel struct {
	responses        []*gent.ContentResponse
	errors           []error
	callCount        int
	capturedOptions  [][]llms.CallOption
	capturedMessages [][]llms.MessageContent
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
	messages []llms.MessageContent,
	options ...llms.CallOption,
) (*gent.ContentResponse, error) {
	m.capturedMessages = append(m.capturedMessages, messages)
	m.capturedOptions = append(
		m.capturedOptions, options,
	)
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

func (m *mockModel) GenerateContentStream(
	execCtx *gent.ExecutionContext,
	streamId string,
	streamTopicId string,
	messages []llms.MessageContent,
	opts ...llms.CallOption,
) (gent.Stream, error) {
	resp, err := m.GenerateContent(
		execCtx, streamId, streamTopicId, messages, opts...,
	)
	if err != nil {
		return gent.NewCompletedStream(nil, err), nil
	}
	return gent.NewCompletedStream(resp, nil), nil
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

func (m *mockToolChain) GetToolSchema(
	_ string,
) *schema.Schema {
	return nil
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
	parseErr      error
	validator     gent.AnswerValidator
}

func newMockTermination() *mockTermination {
	return &mockTermination{name: "answer", guidance: "Write your final answer."}
}

func (m *mockTermination) WithTerminationResult(parts ...gent.ContentPart) *mockTermination {
	m.shouldTermRes = parts
	return m
}

func (m *mockTermination) WithParseError(err error) *mockTermination {
	m.parseErr = err
	return m
}

func (m *mockTermination) Name() string     { return m.name }
func (m *mockTermination) Guidance() string { return m.guidance }

func (m *mockTermination) ParseSection(execCtx *gent.ExecutionContext, content string) (any, error) {
	if m.parseErr != nil {
		if execCtx != nil {
			execCtx.PublishParseError(gent.ParseErrorTypeTermination, content, m.parseErr)
		}
		return nil, m.parseErr
	}
	return content, nil
}

func (m *mockTermination) SetValidator(validator gent.AnswerValidator) {
	m.validator = validator
}

func (m *mockTermination) ShouldTerminate(
	execCtx *gent.ExecutionContext,
	content string,
) *gent.TerminationResult {
	if execCtx == nil {
		panic("mockTermination: ShouldTerminate called with nil ExecutionContext")
	}
	// If parseErr is set, simulate that parsing failed so termination shouldn't happen
	if m.parseErr != nil {
		return &gent.TerminationResult{Status: gent.TerminationContinue}
	}
	if content != "" && m.shouldTermRes != nil {
		return &gent.TerminationResult{
			Status:  gent.TerminationAnswerAccepted,
			Content: m.shouldTermRes,
		}
	}
	if content != "" {
		return &gent.TerminationResult{
			Status:  gent.TerminationAnswerAccepted,
			Content: []gent.ContentPart{llms.TextContent{Text: content}},
		}
	}
	return &gent.TerminationResult{Status: gent.TerminationContinue}
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
		// Publish parse error (following the interface contract)
		if execCtx != nil {
			execCtx.PublishParseError(gent.ParseErrorTypeFormat, output, m.parseErr)
		}
		return nil, m.parseErr
	}
	// Reset consecutive counter on success
	if execCtx != nil {
		execCtx.Stats().ResetGauge(gent.SGFormatParseErrorConsecutive)
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

		data := gent.NewBasicLoopData(&gent.Task{Text: "Hello"})
		execCtx := newTestExecCtx(data)

		messages := loop.buildMessages(execCtx, data, "output prompt", "tools prompt")

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

		data := gent.NewBasicLoopData(&gent.Task{Text: "Do something"})

		// Add scratchpad with one iteration (AI response + observation)
		iter := &gent.Iteration{
			Messages: []*gent.MessageContent{
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
		execCtx := newTestExecCtx(data)

		messages := loop.buildMessages(execCtx, data, "output prompt", "tools prompt")

		// Expected: system, task, AI, observation, CONTINUE!
		require.Len(t, messages, 5,
			"expected 5 messages: system, task, AI, observation, CONTINUE!")

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

		data := gent.NewBasicLoopData(&gent.Task{Text: "", Media: nil})
		execCtx := newTestExecCtx(data)

		assert.Panics(t, func() {
			loop.buildMessages(execCtx, data, "output prompt", "tools prompt")
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

		data := gent.NewBasicLoopData(nil)
		execCtx := newTestExecCtx(data)

		assert.Panics(t, func() {
			loop.buildMessages(execCtx, data, "output prompt", "tools prompt")
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

	data := gent.NewBasicLoopData(&gent.Task{Text: "What is 6*7?"})
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

	data := gent.NewBasicLoopData(&gent.Task{Text: "Search for test"})
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

	data := gent.NewBasicLoopData(&gent.Task{Text: "Use broken tool"})
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

	data := gent.NewBasicLoopData(&gent.Task{Text: "Hello"})
	execCtx := newTestExecCtx(data)
	_, err := loop.Next(execCtx)

	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "model failed"),
		"expected error to contain 'model failed', got %q", err.Error())
}

func TestAgent_Next_ParseError_LogsExpiredIteration(t *testing.T) {
	// When format parsing fails, the agent logs the broken response as an
	// expired iteration and adds a system_error reminder that persists.
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

	data := gent.NewBasicLoopData(&gent.Task{Text: "Hello"})
	execCtx := newTestExecCtx(data)
	result, err := loop.Next(execCtx)

	assert.NoError(t, err)
	assert.Equal(t, gent.LAContinue, result.Action)
	assert.Contains(t, result.NextPrompt, "<system_error>")

	// Scratchpad: expired iteration + system error iteration
	require.Len(t, data.GetScratchPad(), 2)
	assert.Greater(t, data.GetScratchPad()[0].ExpireAfterIteration, 0)
	assert.Equal(t, 0, data.GetScratchPad()[1].ExpireAfterIteration)

	// Iteration history has both
	require.Len(t, data.GetIterationHistory(), 2)
}

func TestAgent_Next_UnclosedTag_ExpiresAndReminder(t *testing.T) {
	// When the model writes <action>...</action> followed by an unclosed
	// <answer> tag, the XML parser returns an unclosed tag error.
	// The agent should:
	// 1. Create an expired iteration with the raw response
	// 2. Inject a system_error reminder that persists
	responseText := `<thinking>
I will search for the tools I need.
</thinking>
<action>
<direct_call>
{"tool":"search","args":{"query":"test"}}
</direct_call>
</action>
<answer>
Here is my answer but I forgot to close the tag.
`
	response := &gent.ContentResponse{
		Choices: []*gent.ContentChoice{{Content: responseText}},
	}
	model := newMockModel(response)

	// Use real XML format to get unclosed tag detection
	xmlFormat := format.NewXML()
	tc := newMockToolChain()
	term := newMockTermination()

	loop := NewAgent(model).
		WithFormat(xmlFormat).
		WithToolChain(tc).
		WithTermination(term).
		WithThinking("Think step by step")

	data := gent.NewBasicLoopData(&gent.Task{Text: "Test"})
	execCtx := gent.NewExecutionContext(
		context.Background(), "test", data,
	)
	execCtx.SetLimits(nil)

	result, err := loop.Next(execCtx)

	require.NoError(t, err)
	assert.Equal(t, gent.LAContinue, result.Action)

	// NextPrompt should be the system_error reminder
	assert.Contains(t, result.NextPrompt, "<system_error>")
	assert.Contains(t, result.NextPrompt,
		"You MUST follow the output format")

	// Scratchpad: expired iteration + system_error reminder
	require.Len(t, data.GetScratchPad(), 2)

	// First iteration is expired (raw response + error)
	expiredIter := data.GetScratchPad()[0]
	assert.Greater(t, expiredIter.ExpireAfterIteration, 0,
		"first iteration should be expired")

	// Verify the expired iteration has the AI response
	require.GreaterOrEqual(t, len(expiredIter.Messages), 1)
	aiMsg, ok := expiredIter.Messages[0].Parts[0].(llms.TextContent)
	require.True(t, ok)
	assert.Contains(t, aiMsg.Text, "<action>")
	assert.Contains(t, aiMsg.Text, "<answer>")

	// Verify the expired iteration has the unclosed tag error
	errorMsg := expiredIter.Messages[len(expiredIter.Messages)-1]
	errorText, ok := errorMsg.Parts[0].(llms.TextContent)
	require.True(t, ok)
	assert.Contains(t, errorText.Text, "unclosed tag")
	assert.Contains(t, errorText.Text, "<answer>")

	// Second iteration is the system_error reminder (not expired)
	reminderIter := data.GetScratchPad()[1]
	assert.Equal(t, 0, reminderIter.ExpireAfterIteration,
		"system_error reminder should not be expired")
	assert.Equal(t, gent.IterationSystemInjected, reminderIter.Origin)

	reminderText, ok := reminderIter.Messages[0].Parts[0].(llms.TextContent)
	require.True(t, ok)
	assert.Contains(t, reminderText.Text, "<system_error>")

	// Iteration history has both
	require.Len(t, data.GetIterationHistory(), 2)

	// Stats should track the format parse error
	stats := execCtx.Stats()
	assert.Equal(t, int64(1),
		stats.GetCounter(gent.SCFormatParseErrorTotal))
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

	data := gent.NewBasicLoopData(&gent.Task{Text: "Test"})
	execCtx := newTestExecCtx(data)
	_, err := loop.Next(execCtx)

	assert.NoError(t, err)

	// Verify parse error was traced (stats updated)
	stats := execCtx.Stats()
	assert.Equal(t, int64(1), stats.GetCounter(gent.SCFormatParseErrorTotal))
	assert.Equal(t, float64(1), stats.GetGauge(gent.SGFormatParseErrorConsecutive))
}

func TestAgent_Next_ParseErrorFeedback(t *testing.T) {
	type input struct {
		responseContent string
	}

	type mocks struct {
		formatParseResult map[string][]string
		formatParseErr    error
		toolChainErr      error
		terminationErr    error
	}

	type expected struct {
		action              gent.LoopAction
		nextPrompt          string
		scratchpadLen       int
		iterationHistoryLen int
		// expired is true when the iteration should be
		// logged but already expired (format parse errors
		// and unrecognized responses).
		expired bool
		// observationContent is the expected error message
		// in the iteration's second message. Used for both
		// expired iterations and fed-back observations.
		observationContent string
	}

	// Create wrapped errors that simulate real toolchain/termination parse errors.
	toolchainJSONErr := fmt.Errorf(
		"%w: invalid character 'n' looking for beginning of value",
		gent.ErrInvalidJSON,
	)
	terminationJSONErr := fmt.Errorf(
		"%w: unexpected end of JSON input",
		gent.ErrInvalidJSON,
	)

	tests := []struct {
		name     string
		input    input
		mocks    mocks
		expected expected
	}{
		{
			name: "format parse error creates expired iteration and system error",
			input: input{
				responseContent: "completely invalid response with no sections",
			},
			mocks: mocks{
				formatParseErr: gent.ErrNoSectionsFound,
			},
			expected: expected{
				action: gent.LAContinue,
				nextPrompt: "<system_error>\n" +
					"You MUST follow the output format described in the system prompt. " +
					"Every response MUST contain properly formatted sections. " +
					"Do NOT fabricate tool outputs or observations — " +
					"only use the sections defined in your instructions.\n" +
					"</system_error>",
				scratchpadLen:       2,
				iterationHistoryLen: 2,
				expired:             true,
				observationContent: "Format parse error: " +
					"no recognized sections found in output",
			},
		},
		{
			name: "toolchain Execute error feeds back with detailed JSON parse error",
			input: input{
				responseContent: "<action>not valid json at all</action>",
			},
			mocks: mocks{
				formatParseResult: map[string][]string{
					"action": {"not valid json at all"},
				},
				toolChainErr: toolchainJSONErr,
			},
			expected: expected{
				action: gent.LAContinue,
				nextPrompt: "<observation>\n" +
					"<error>\n" +
					"Error: invalid JSON in section content: " +
					"invalid character 'n' looking for beginning of value\n" +
					"</error>\n" +
					"</observation>",
				scratchpadLen:       1,
				iterationHistoryLen: 1,
				expired:             false,
				observationContent: "<observation>\n" +
					"<error>\n" +
					"Error: invalid JSON in section content: " +
					"invalid character 'n' looking for beginning of value\n" +
					"</error>\n" +
					"</observation>",
			},
		},
		{
			name: "termination ParseSection error feeds back with detailed JSON parse error",
			input: input{
				responseContent: "<answer>{malformed json</answer>",
			},
			mocks: mocks{
				formatParseResult: map[string][]string{
					"answer": {"{malformed json"},
				},
				terminationErr: terminationJSONErr,
			},
			expected: expected{
				action: gent.LAContinue,
				nextPrompt: "<observation>\n" +
					"Termination parse error: invalid JSON in section content: " +
					"unexpected end of JSON input\n" +
					"Content: {malformed json\n" +
					"\n" +
					"Please try again with proper formatting.\n" +
					"</observation>",
				scratchpadLen:       1,
				iterationHistoryLen: 1,
				expired:             false,
				observationContent: "<observation>\n" +
					"Termination parse error: invalid JSON in section content: " +
					"unexpected end of JSON input\n" +
					"Content: {malformed json\n" +
					"\n" +
					"Please try again with proper formatting.\n" +
					"</observation>",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := &gent.ContentResponse{
				Choices: []*gent.ContentChoice{
					{Content: tt.input.responseContent},
				},
			}
			model := newMockModel(response)

			format := newMockFormat()
			if tt.mocks.formatParseErr != nil {
				format = format.WithParseError(tt.mocks.formatParseErr)
			} else if tt.mocks.formatParseResult != nil {
				format = format.WithParseResult(tt.mocks.formatParseResult)
			}

			tc := newMockToolChain()
			if tt.mocks.toolChainErr != nil {
				tc = tc.WithErrors(tt.mocks.toolChainErr)
			}

			term := newMockTermination()
			if tt.mocks.terminationErr != nil {
				term = term.WithParseError(tt.mocks.terminationErr)
			}

			loop := NewAgent(model).
				WithFormat(format).
				WithToolChain(tc).
				WithTermination(term)

			data := gent.NewBasicLoopData(&gent.Task{Text: "Test task"})
			execCtx := newTestExecCtx(data)
			result, err := loop.Next(execCtx)

			require.NoError(t, err)
			assert.Equal(t, tt.expected.action, result.Action)
			assert.Equal(t, tt.expected.nextPrompt, result.NextPrompt)
			assert.Equal(t,
				tt.expected.scratchpadLen,
				len(data.GetScratchPad()),
			)
			assert.Equal(t,
				tt.expected.iterationHistoryLen,
				len(data.GetIterationHistory()),
			)

			if tt.expected.scratchpadLen > 0 {
				scratchpad := data.GetScratchPad()

				if tt.expected.expired {
					// First iteration is the expired one with the error
					expiredIter := scratchpad[0]
					assert.Greater(t, expiredIter.ExpireAfterIteration, 0,
						"expired iteration should have ExpireAfterIteration set")

					require.GreaterOrEqual(t, len(expiredIter.Messages), 2,
						"expired iteration should have AI response + error")
					errorMsg := expiredIter.Messages[len(expiredIter.Messages)-1]
					require.Len(t, errorMsg.Parts, 1)
					textContent, ok := errorMsg.Parts[0].(llms.TextContent)
					require.True(t, ok)
					assert.Equal(t,
						tt.expected.observationContent,
						textContent.Text,
					)
				} else {
					lastIter := scratchpad[len(scratchpad)-1]
					assert.Equal(t, 0, lastIter.ExpireAfterIteration,
						"non-expired iteration should not have ExpireAfterIteration")

					require.GreaterOrEqual(t, len(lastIter.Messages), 2,
						"iteration should have at least 2 messages")
					observationMsg := lastIter.Messages[len(lastIter.Messages)-1]
					require.Len(t, observationMsg.Parts, 1)
					textContent, ok := observationMsg.Parts[0].(llms.TextContent)
					require.True(t, ok)
					assert.Equal(t,
						tt.expected.observationContent,
						textContent.Text,
					)
				}
			}
		})
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

	data := gent.NewBasicLoopData(&gent.Task{Text: "Use tools a and b"})
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
			name: "action and answer both present - action takes priority with reminder",
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
				scratchpadLen:    2,
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
			name: "action with tool error and answer - action still takes priority with reminder",
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
				scratchpadLen:    2,
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

			data := gent.NewBasicLoopData(&gent.Task{Text: "Execute the task"})
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
	t.Run("returns all sections", func(t *testing.T) {
		format := newMockFormat()
		ctx := SystemPromptContext{
			Format:             format,
			BehaviorAndContext: "You are helpful.",
			CriticalRules:      "Never lie.",
			OutputPrompt:       "Use XML tags.",
			ToolsPrompt:        "Available tools: search",
			Time:               gent.NewDefaultTimeProvider(),
		}

		sections := DefaultSystemPromptBuilder(ctx)

		require.Len(t, sections, 5)
		assert.Equal(t, "behavior", sections[0].Name)
		assert.Equal(t, "You are helpful.", sections[0].Content)
		assert.Equal(t, "re_act", sections[1].Name)
		assert.Equal(t, "critical_rules", sections[2].Name)
		assert.Equal(t, "Never lie.", sections[2].Content)
		assert.Equal(t, "available_tools", sections[3].Name)
		assert.Equal(t, "Available tools: search", sections[3].Content)
		assert.Equal(t, "output_format", sections[4].Name)
		assert.Equal(t, "Use XML tags.", sections[4].Content)
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

		sections := DefaultSystemPromptBuilder(ctx)

		require.Len(t, sections, 3)
		assert.Equal(t, "re_act", sections[0].Name)
		assert.Equal(t, "available_tools", sections[1].Name)
		assert.Equal(t, "output_format", sections[2].Name)
	})
}

func TestAgent_WithSystemPromptBuilder(t *testing.T) {
	t.Run("custom builder is used", func(t *testing.T) {
		model := newMockModel()
		format := newMockFormat()
		tc := newMockToolChain()
		term := newMockTermination()

		customBuilder := func(ctx SystemPromptContext) []gent.FormattedSection {
			return []gent.FormattedSection{
				{Name: "custom", Content: "Custom system prompt"},
			}
		}

		loop := NewAgent(model).
			WithFormat(format).
			WithToolChain(tc).
			WithTermination(term).
			WithSystemPromptBuilder(customBuilder)

		data := gent.NewBasicLoopData(&gent.Task{Text: "Hello"})
		execCtx := newTestExecCtx(data)
		messages := loop.buildMessages(execCtx, data, "output", "tools")

		// First message should be system prompt with our custom section
		require.GreaterOrEqual(t, len(messages), 1)
		assert.Equal(t, llms.ChatMessageTypeSystem, messages[0].Role)
		content, ok := messages[0].Parts[0].(llms.TextContent)
		require.True(t, ok)
		assert.Contains(t, content.Text, "Custom system prompt")
	})

	t.Run("builder receives correct context", func(t *testing.T) {
		model := newMockModel()
		format := newMockFormat()
		tc := newMockToolChain()
		term := newMockTermination()
		mockTime := gent.NewMockTimeProvider(
			time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC),
		)

		var capturedCtx SystemPromptContext
		capturingBuilder := func(ctx SystemPromptContext) []gent.FormattedSection {
			capturedCtx = ctx
			return []gent.FormattedSection{
				{Name: "test", Content: "test"},
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

		data := gent.NewBasicLoopData(&gent.Task{Text: "Hello"})
		execCtx := newTestExecCtx(data)
		loop.buildMessages(execCtx, data, "output prompt", "tools prompt")

		assert.Equal(t, format, capturedCtx.Format)
		assert.Equal(t, "Be helpful", capturedCtx.BehaviorAndContext)
		assert.Equal(t, "No lies", capturedCtx.CriticalRules)
		assert.Equal(t, "output prompt", capturedCtx.OutputPrompt)
		assert.Equal(t, "tools prompt", capturedCtx.ToolsPrompt)
		assert.Equal(t, mockTime, capturedCtx.Time)
	})
}

// mockNamerModel implements Model with ModelNamer for
// testing model-specific sampling behavior.
type mockNamerModel struct {
	mockModel
	name string
}

func (m *mockNamerModel) ModelName() string {
	return m.name
}

func TestAgent_WithCallOptions(t *testing.T) {
	// The ReAct agent prepends DefaultSamplingParams before
	// user options. mockModel does not implement ModelNamer,
	// so defaults are temp=0.2, top-p=1.0. User options
	// applied after override the defaults.

	type input struct {
		callOptions []llms.CallOption
	}

	type expected struct {
		temperature float64
		topP        float64
		maxTokens   int
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "no user options uses default " +
				"temperature and top-p",
			input: input{
				callOptions: nil,
			},
			expected: expected{
				temperature: 0.2,
				topP:        1.0,
			},
		},
		{
			name: "user temperature overrides " +
				"default",
			input: input{
				callOptions: []llms.CallOption{
					llms.WithTemperature(0.7),
				},
			},
			expected: expected{
				temperature: 0.7,
				topP:        1.0,
			},
		},
		{
			name: "user top-p overrides default",
			input: input{
				callOptions: []llms.CallOption{
					llms.WithTopP(0.9),
				},
			},
			expected: expected{
				temperature: 0.2,
				topP:        0.9,
			},
		},
		{
			name: "user overrides both temperature " +
				"and top-p",
			input: input{
				callOptions: []llms.CallOption{
					llms.WithTemperature(0.5),
					llms.WithTopP(0.8),
					llms.WithMaxTokens(512),
				},
			},
			expected: expected{
				temperature: 0.5,
				topP:        0.8,
				maxTokens:   512,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			model := newMockModel(
				&gent.ContentResponse{
					Choices: []*gent.ContentChoice{
						{Content: "<answer>done</answer>"},
					},
				},
			)

			format := newMockFormat().WithParseResult(
				map[string][]string{
					"answer": {"done"},
				},
			)
			term := newMockTermination()

			agent := NewAgent(model).
				WithFormat(format).
				WithToolChain(newMockToolChain()).
				WithTermination(term)

			if tc.input.callOptions != nil {
				agent.WithCallOptions(
					tc.input.callOptions...,
				)
			}

			data := gent.NewBasicLoopData(
				&gent.Task{Text: "test"},
			)
			execCtx := gent.NewExecutionContext(
				context.Background(), "test", data,
			)
			execCtx.SetLimits(nil)

			_, err := agent.Next(execCtx)
			require.NoError(t, err)
			require.Len(t, model.capturedOptions, 1)

			captured := model.capturedOptions[0]
			var opts llms.CallOptions
			for _, opt := range captured {
				opt(&opts)
			}
			assert.InDelta(t,
				tc.expected.temperature,
				opts.Temperature, 0.001,
				"temperature",
			)
			assert.InDelta(t,
				tc.expected.topP,
				opts.TopP, 0.001,
				"top-p",
			)
			if tc.expected.maxTokens > 0 {
				assert.Equal(t,
					tc.expected.maxTokens,
					opts.MaxTokens,
				)
			}
		})
	}
}

func TestAgent_SamplingParams_ForbiddenModel(
	t *testing.T,
) {
	// OpenAI reasoning models have both params forbidden.
	// The agent sends temperature=1.0 and top-p=1.0 as a
	// workaround for langchaingo always serializing the
	// temperature field (zero-value 0.0 is rejected).
	// User-provided options override these defaults.

	type input struct {
		modelName   string
		callOptions []llms.CallOption
	}

	type expected struct {
		temperature float64
		topP        float64
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "reasoning model sends 1.0 " +
				"for forbidden params",
			input: input{
				modelName:   "openai/o3",
				callOptions: nil,
			},
			expected: expected{
				temperature: 1.0,
				topP:        1.0,
			},
		},
		{
			name: "user can still force params " +
				"on forbidden model",
			input: input{
				modelName: "o4-mini",
				callOptions: []llms.CallOption{
					llms.WithTemperature(0.5),
				},
			},
			expected: expected{
				temperature: 0.5,
				topP:        1.0,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inner := newMockModel(
				&gent.ContentResponse{
					Choices: []*gent.ContentChoice{
						{Content: "<answer>done</answer>"},
					},
				},
			)
			model := &mockNamerModel{
				mockModel: *inner,
				name:      tc.input.modelName,
			}

			format := newMockFormat().WithParseResult(
				map[string][]string{
					"answer": {"done"},
				},
			)

			agent := NewAgent(model).
				WithFormat(format).
				WithToolChain(newMockToolChain()).
				WithTermination(newMockTermination())

			if tc.input.callOptions != nil {
				agent.WithCallOptions(
					tc.input.callOptions...,
				)
			}

			data := gent.NewBasicLoopData(
				&gent.Task{Text: "test"},
			)
			execCtx := gent.NewExecutionContext(
				context.Background(), "test", data,
			)
			execCtx.SetLimits(nil)

			_, err := agent.Next(execCtx)
			require.NoError(t, err)
			require.Len(t, model.capturedOptions, 1)

			captured := model.capturedOptions[0]
			var opts llms.CallOptions
			for _, opt := range captured {
				opt(&opts)
			}
			assert.InDelta(t,
				tc.expected.temperature,
				opts.Temperature, 0.001,
				"temperature",
			)
			assert.InDelta(t,
				tc.expected.topP,
				opts.TopP, 0.001,
				"top-p",
			)
		})
	}
}

func TestAgent_SamplingWarnings(t *testing.T) {
	type input struct {
		modelName   string
		callOptions []llms.CallOption
	}

	type expected struct {
		restrictiveWarning bool
		forbiddenWarnings  int
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "restrictive temp and top-p " +
				"emits warning",
			input: input{
				callOptions: []llms.CallOption{
					llms.WithTemperature(0.1),
					llms.WithTopP(0.5),
				},
			},
			expected: expected{
				restrictiveWarning: true,
			},
		},
		{
			name: "restrictive temp only does " +
				"not warn",
			input: input{
				callOptions: []llms.CallOption{
					llms.WithTemperature(0.1),
				},
			},
			expected: expected{
				restrictiveWarning: false,
			},
		},
		{
			name: "restrictive top-p with normal " +
				"temp does not warn",
			input: input{
				callOptions: []llms.CallOption{
					llms.WithTemperature(0.5),
					llms.WithTopP(0.5),
				},
			},
			expected: expected{
				restrictiveWarning: false,
			},
		},
		{
			name: "normal values do not warn",
			input: input{
				callOptions: nil,
			},
			expected: expected{
				restrictiveWarning: false,
			},
		},
		{
			name: "forbidden model with user " +
				"temp emits forbidden warning",
			input: input{
				modelName: "openai/o3",
				callOptions: []llms.CallOption{
					llms.WithTemperature(0.5),
				},
			},
			expected: expected{
				forbiddenWarnings: 1,
			},
		},
		{
			name: "forbidden model with user " +
				"temp and top-p emits two " +
				"forbidden warnings",
			input: input{
				modelName: "o4-mini",
				callOptions: []llms.CallOption{
					llms.WithTemperature(0.5),
					llms.WithTopP(0.9),
				},
			},
			expected: expected{
				forbiddenWarnings: 2,
			},
		},
		{
			name: "forbidden model without " +
				"user params emits no warnings",
			input: input{
				modelName:   "gpt-5",
				callOptions: nil,
			},
			expected: expected{
				forbiddenWarnings: 0,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var model gent.Model
			if tc.input.modelName != "" {
				inner := newMockModel(
					&gent.ContentResponse{
						Choices: []*gent.ContentChoice{
							{Content: "<answer>ok</answer>"},
						},
					},
				)
				model = &mockNamerModel{
					mockModel: *inner,
					name:      tc.input.modelName,
				}
			} else {
				model = newMockModel(
					&gent.ContentResponse{
						Choices: []*gent.ContentChoice{
							{Content: "<answer>ok</answer>"},
						},
					},
				)
			}

			format := newMockFormat().WithParseResult(
				map[string][]string{
					"answer": {"ok"},
				},
			)

			agent := NewAgent(model).
				WithFormat(format).
				WithToolChain(newMockToolChain()).
				WithTermination(newMockTermination())

			if tc.input.callOptions != nil {
				agent.WithCallOptions(
					tc.input.callOptions...,
				)
			}

			data := gent.NewBasicLoopData(
				&gent.Task{Text: "test"},
			)
			execCtx := gent.NewExecutionContext(
				context.Background(), "test", data,
			)
			execCtx.SetLimits(nil)

			_, err := agent.Next(execCtx)
			require.NoError(t, err)

			// Count warning events by name.
			var restrictive, forbidden int
			for _, evt := range execCtx.Events() {
				ce, ok := evt.(*gent.CommonEvent)
				if !ok {
					continue
				}
				switch ce.EventName {
				case "gent:sampling_params_restrictive":
					restrictive++
				case "gent:sampling_param_forbidden":
					forbidden++
				}
			}

			assert.Equal(t,
				tc.expected.restrictiveWarning,
				restrictive > 0,
				"restrictive warning",
			)
			assert.Equal(t,
				tc.expected.forbiddenWarnings,
				forbidden,
				"forbidden warnings",
			)
		})
	}
}

// ----------------------------------------------------------------------------
// BeforeSystemPromptEvent Tests
// ----------------------------------------------------------------------------

// systemPromptSubscriber implements BeforeSystemPromptSubscriber for testing.
type systemPromptSubscriber struct {
	capturedSections [][]gent.FormattedSection
	onEvent          func(
		execCtx *gent.ExecutionContext,
		event *gent.BeforeSystemPromptEvent,
	)
}

func (s *systemPromptSubscriber) OnBeforeSystemPrompt(
	execCtx *gent.ExecutionContext,
	event *gent.BeforeSystemPromptEvent,
) {
	s.capturedSections = append(
		s.capturedSections,
		append([]gent.FormattedSection{}, event.Sections...),
	)
	if s.onEvent != nil {
		s.onEvent(execCtx, event)
	}
}

func TestAgent_BeforeSystemPromptEvent(t *testing.T) {
	type input struct {
		behaviorAndContext string
		criticalRules      string
		responseContent    string
		parsedSections     map[string][]string
	}

	type mocks struct {
		onEvent func(
			execCtx *gent.ExecutionContext,
			event *gent.BeforeSystemPromptEvent,
		)
	}

	type expected struct {
		systemPromptContains    []string
		systemPromptNotContains []string
		capturedSectionNames    []string
	}

	tests := []struct {
		name     string
		input    input
		mocks    mocks
		expected expected
	}{
		{
			name: "subscriber appends a dynamic reminder section",
			input: input{
				behaviorAndContext: "You are helpful.",
				responseContent:   "<answer>done</answer>",
				parsedSections:    map[string][]string{"answer": {"done"}},
			},
			mocks: mocks{
				onEvent: func(
					_ *gent.ExecutionContext,
					event *gent.BeforeSystemPromptEvent,
				) {
					event.Sections = append(
						event.Sections,
						gent.FormattedSection{
							Name:    "reminder",
							Content: "Always cite your sources.",
						},
					)
				},
			},
			expected: expected{
				systemPromptContains: []string{
					"You are helpful.",
					"Always cite your sources.",
					"<reminder>",
				},
				capturedSectionNames: []string{
					"behavior",
					"re_act",
					"available_tools",
					"output_format",
				},
			},
		},
		{
			name: "subscriber replaces all sections",
			input: input{
				behaviorAndContext: "You are helpful.",
				criticalRules:     "Never lie.",
				responseContent:   "<answer>done</answer>",
				parsedSections:    map[string][]string{"answer": {"done"}},
			},
			mocks: mocks{
				onEvent: func(
					_ *gent.ExecutionContext,
					event *gent.BeforeSystemPromptEvent,
				) {
					event.Sections = []gent.FormattedSection{
						{
							Name:    "custom",
							Content: "Completely custom prompt.",
						},
					}
				},
			},
			expected: expected{
				systemPromptContains: []string{
					"Completely custom prompt.",
					"<custom>",
				},
				systemPromptNotContains: []string{
					"You are helpful.",
					"Never lie.",
					"<behavior>",
					"<re_act>",
				},
				capturedSectionNames: []string{
					"behavior",
					"re_act",
					"critical_rules",
					"available_tools",
					"output_format",
				},
			},
		},
		{
			name: "subscriber removes a section",
			input: input{
				behaviorAndContext: "You are helpful.",
				criticalRules:     "Never lie.",
				responseContent:   "<answer>done</answer>",
				parsedSections:    map[string][]string{"answer": {"done"}},
			},
			mocks: mocks{
				onEvent: func(
					_ *gent.ExecutionContext,
					event *gent.BeforeSystemPromptEvent,
				) {
					// Remove re_act section
					var filtered []gent.FormattedSection
					for _, s := range event.Sections {
						if s.Name != "re_act" {
							filtered = append(filtered, s)
						}
					}
					event.Sections = filtered
				},
			},
			expected: expected{
				systemPromptContains: []string{
					"You are helpful.",
					"Never lie.",
				},
				systemPromptNotContains: []string{
					"<re_act>",
				},
				capturedSectionNames: []string{
					"behavior",
					"re_act",
					"critical_rules",
					"available_tools",
					"output_format",
				},
			},
		},
		{
			name: "no subscriber does not modify sections",
			input: input{
				behaviorAndContext: "You are helpful.",
				responseContent:   "<answer>done</answer>",
				parsedSections:    map[string][]string{"answer": {"done"}},
			},
			mocks: mocks{
				onEvent: nil,
			},
			expected: expected{
				systemPromptContains: []string{
					"You are helpful.",
					"<behavior>",
					"<re_act>",
				},
				capturedSectionNames: []string{
					"behavior",
					"re_act",
					"available_tools",
					"output_format",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := &gent.ContentResponse{
				Choices: []*gent.ContentChoice{
					{Content: tt.input.responseContent},
				},
			}
			model := newMockModel(response)
			format := newMockFormat().WithParseResult(tt.input.parsedSections)
			tc := newMockToolChain()
			term := newMockTermination()

			agent := NewAgent(model).
				WithFormat(format).
				WithToolChain(tc).
				WithTermination(term).
				WithBehaviorAndContext(tt.input.behaviorAndContext)

			if tt.input.criticalRules != "" {
				agent.WithCriticalRules(tt.input.criticalRules)
			}

			data := gent.NewBasicLoopData(&gent.Task{Text: "test"})
			execCtx := gent.NewExecutionContext(
				context.Background(), "test", data,
			)
			execCtx.SetLimits(nil)

			sub := &systemPromptSubscriber{onEvent: tt.mocks.onEvent}
			registry := newTestRegistry(sub)
			execCtx.SetEventPublisher(registry)

			_, err := agent.Next(execCtx)
			require.NoError(t, err)

			// Verify system prompt content sent to model
			require.Len(t, model.capturedMessages, 1)
			systemMsg := model.capturedMessages[0][0]
			assert.Equal(t, llms.ChatMessageTypeSystem, systemMsg.Role)
			systemText, ok := systemMsg.Parts[0].(llms.TextContent)
			require.True(t, ok)

			for _, want := range tt.expected.systemPromptContains {
				assert.Contains(t, systemText.Text, want,
					"system prompt should contain %q", want)
			}
			for _, notWant := range tt.expected.systemPromptNotContains {
				assert.NotContains(t, systemText.Text, notWant,
					"system prompt should not contain %q", notWant)
			}

			// Verify captured sections (before modification)
			if tt.mocks.onEvent != nil {
				require.Len(t, sub.capturedSections, 1)
				var names []string
				for _, s := range sub.capturedSections[0] {
					names = append(names, s.Name)
				}
				assert.Equal(t, tt.expected.capturedSectionNames, names)
			}
		})
	}
}

func TestAgent_BeforeSystemPromptEvent_DynamicPerIteration(t *testing.T) {
	// Verify that the subscriber is called on each iteration and can inject
	// different content based on iteration count.
	responses := []*gent.ContentResponse{
		// Iteration 1: tool call → continue
		{Choices: []*gent.ContentChoice{
			{Content: "<action>tool: search\nargs:\n  q: test</action>"},
		}},
		// Iteration 2: answer → terminate
		{Choices: []*gent.ContentChoice{
			{Content: "<answer>done</answer>"},
		}},
	}
	model := newMockModel(responses...)

	callCount := 0
	format := &iterMockFormat{
		parseResults: []map[string][]string{
			{"action": {"tool: search\nargs:\n  q: test"}},
			{"answer": {"done"}},
		},
	}
	tc := newMockToolChain().WithResults(&gent.ToolChainResult{
		Text: "<observation>\n<search>\nfound\n</search>\n</observation>",
		Raw: &gent.RawToolChainResult{
			Calls:   []*gent.ToolCall{{Name: "search", Args: map[string]any{"q": "test"}}},
			Results: []*gent.RawToolCallResult{{Name: "search", Output: "found"}},
			Errors:  []error{nil},
		},
	})
	term := newMockTermination()

	agent := NewAgent(model).
		WithFormat(format).
		WithToolChain(tc).
		WithTermination(term).
		WithBehaviorAndContext("Be helpful.")

	sub := &systemPromptSubscriber{
		onEvent: func(
			execCtx *gent.ExecutionContext,
			event *gent.BeforeSystemPromptEvent,
		) {
			callCount++
			event.Sections = append(
				event.Sections,
				gent.FormattedSection{
					Name: "reminder",
					Content: fmt.Sprintf(
						"This is iteration %d.", callCount,
					),
				},
			)
		},
	}

	data := gent.NewBasicLoopData(&gent.Task{Text: "test"})
	execCtx := gent.NewExecutionContext(
		context.Background(), "test", data,
	)
	execCtx.SetLimits(nil)

	registry := newTestRegistry(sub)
	execCtx.SetEventPublisher(registry)

	// Iteration 1: tool call
	result, err := agent.Next(execCtx)
	require.NoError(t, err)
	assert.Equal(t, gent.LAContinue, result.Action)

	require.Len(t, model.capturedMessages, 1)
	sys1, ok := model.capturedMessages[0][0].Parts[0].(llms.TextContent)
	require.True(t, ok)
	assert.Contains(t, sys1.Text, "This is iteration 1.")

	// Iteration 2: answer
	result, err = agent.Next(execCtx)
	require.NoError(t, err)
	assert.Equal(t, gent.LATerminate, result.Action)

	require.Len(t, model.capturedMessages, 2)
	sys2, ok := model.capturedMessages[1][0].Parts[0].(llms.TextContent)
	require.True(t, ok)
	assert.Contains(t, sys2.Text, "This is iteration 2.")
	assert.NotContains(t, sys2.Text, "This is iteration 1.")

	// Subscriber was called twice
	assert.Len(t, sub.capturedSections, 2)
}

// iterMockFormat is a mock format that returns different parse results per call.
type iterMockFormat struct {
	parseResults []map[string][]string
	callCount    int
}

func (m *iterMockFormat) RegisterSection(_ gent.TextSection) gent.TextFormat {
	return m
}

func (m *iterMockFormat) DescribeStructure() string {
	return "mock format structure"
}

func (m *iterMockFormat) Parse(
	execCtx *gent.ExecutionContext,
	_ string,
) (map[string][]string, error) {
	idx := m.callCount
	m.callCount++
	if execCtx != nil {
		execCtx.Stats().ResetGauge(gent.SGFormatParseErrorConsecutive)
	}
	if idx < len(m.parseResults) {
		return m.parseResults[idx], nil
	}
	return map[string][]string{}, nil
}

func (m *iterMockFormat) FormatSections(
	sections []gent.FormattedSection,
) string {
	var parts []string
	for _, section := range sections {
		parts = append(parts, m.formatSection(section))
	}
	return strings.Join(parts, "\n")
}

func (m *iterMockFormat) formatSection(
	section gent.FormattedSection,
) string {
	var inner []string
	if section.Content != "" {
		inner = append(inner, section.Content)
	}
	if len(section.Children) > 0 {
		inner = append(inner, m.FormatSections(section.Children))
	}
	return "<" + section.Name + ">\n" +
		strings.Join(inner, "\n") +
		"\n</" + section.Name + ">"
}

// newTestRegistry creates a minimal EventPublisher that dispatches to a single
// subscriber. This avoids importing the events package in the test.
type testRegistry struct {
	subscriber any
}

func newTestRegistry(sub any) *testRegistry {
	return &testRegistry{subscriber: sub}
}

func (r *testRegistry) MaxRecursion() int { return 10 }

func (r *testRegistry) Dispatch(
	execCtx *gent.ExecutionContext,
	event gent.Event,
) {
	switch e := event.(type) {
	case *gent.BeforeSystemPromptEvent:
		if sub, ok := r.subscriber.(gent.BeforeSystemPromptSubscriber); ok {
			sub.OnBeforeSystemPrompt(execCtx, e)
		}
	}
}

// ----------------------------------------------------------------------------
// Iteration Expiry Tests
// ----------------------------------------------------------------------------

func TestAgent_BuildMessages_IterationExpiry(t *testing.T) {
	type input struct {
		scratchpad       []*gent.Iteration
		currentIteration int
	}

	type expected struct {
		messageCount    int
		lastMessageText string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "no expiry includes all iterations",
			input: input{
				scratchpad: []*gent.Iteration{
					{
						Messages: []*gent.MessageContent{
							{
								Role: llms.ChatMessageTypeAI,
								Parts: []gent.ContentPart{
									llms.TextContent{Text: "response 1"},
								},
							},
							{
								Role: llms.ChatMessageTypeHuman,
								Parts: []gent.ContentPart{
									llms.TextContent{Text: "obs 1"},
								},
							},
						},
					},
					{
						Messages: []*gent.MessageContent{
							{
								Role: llms.ChatMessageTypeAI,
								Parts: []gent.ContentPart{
									llms.TextContent{Text: "response 2"},
								},
							},
							{
								Role: llms.ChatMessageTypeHuman,
								Parts: []gent.ContentPart{
									llms.TextContent{Text: "obs 2"},
								},
							},
						},
					},
				},
				currentIteration: 3,
			},
			expected: expected{
				// system + task + 2*AI + 2*obs + CONTINUE!
				messageCount:    7,
				lastMessageText: "CONTINUE!",
			},
		},
		{
			name: "expired iteration is skipped",
			input: input{
				scratchpad: []*gent.Iteration{
					{
						Messages: []*gent.MessageContent{
							{
								Role: llms.ChatMessageTypeAI,
								Parts: []gent.ContentPart{
									llms.TextContent{Text: "ephemeral"},
								},
							},
							{
								Role: llms.ChatMessageTypeHuman,
								Parts: []gent.ContentPart{
									llms.TextContent{Text: "ephemeral obs"},
								},
							},
						},
						ExpireAfterIteration: 2,
					},
					{
						Messages: []*gent.MessageContent{
							{
								Role: llms.ChatMessageTypeAI,
								Parts: []gent.ContentPart{
									llms.TextContent{Text: "permanent"},
								},
							},
							{
								Role: llms.ChatMessageTypeHuman,
								Parts: []gent.ContentPart{
									llms.TextContent{Text: "permanent obs"},
								},
							},
						},
					},
				},
				currentIteration: 3,
			},
			expected: expected{
				// system + task + 1*AI + 1*obs + CONTINUE!
				messageCount:    5,
				lastMessageText: "CONTINUE!",
			},
		},
		{
			name: "not yet expired iteration is included",
			input: input{
				scratchpad: []*gent.Iteration{
					{
						Messages: []*gent.MessageContent{
							{
								Role: llms.ChatMessageTypeAI,
								Parts: []gent.ContentPart{
									llms.TextContent{Text: "ephemeral"},
								},
							},
						},
						ExpireAfterIteration: 5,
					},
				},
				currentIteration: 3,
			},
			expected: expected{
				// system + task + 1*AI + CONTINUE!
				messageCount:    4,
				lastMessageText: "CONTINUE!",
			},
		},
		{
			name: "iteration expires exactly at boundary",
			input: input{
				scratchpad: []*gent.Iteration{
					{
						Messages: []*gent.MessageContent{
							{
								Role: llms.ChatMessageTypeAI,
								Parts: []gent.ContentPart{
									llms.TextContent{Text: "at boundary"},
								},
							},
						},
						ExpireAfterIteration: 3,
					},
				},
				currentIteration: 3,
			},
			expected: expected{
				// system + task + BEGIN! (expired, no scratchpad)
				messageCount:    3,
				lastMessageText: "BEGIN!",
			},
		},
		{
			name: "all expired shows BEGIN not CONTINUE",
			input: input{
				scratchpad: []*gent.Iteration{
					{
						Messages: []*gent.MessageContent{
							{
								Role: llms.ChatMessageTypeAI,
								Parts: []gent.ContentPart{
									llms.TextContent{Text: "expired 1"},
								},
							},
						},
						ExpireAfterIteration: 1,
					},
					{
						Messages: []*gent.MessageContent{
							{
								Role: llms.ChatMessageTypeAI,
								Parts: []gent.ContentPart{
									llms.TextContent{Text: "expired 2"},
								},
							},
						},
						ExpireAfterIteration: 2,
					},
				},
				currentIteration: 5,
			},
			expected: expected{
				// system + task + BEGIN!
				messageCount:    3,
				lastMessageText: "BEGIN!",
			},
		},
		{
			name: "mixed expired and non-expired",
			input: input{
				scratchpad: []*gent.Iteration{
					{
						Messages: []*gent.MessageContent{
							{
								Role: llms.ChatMessageTypeAI,
								Parts: []gent.ContentPart{
									llms.TextContent{Text: "keep me"},
								},
							},
						},
					},
					{
						Messages: []*gent.MessageContent{
							{
								Role: llms.ChatMessageTypeAI,
								Parts: []gent.ContentPart{
									llms.TextContent{Text: "expired"},
								},
							},
						},
						ExpireAfterIteration: 2,
					},
					{
						Messages: []*gent.MessageContent{
							{
								Role: llms.ChatMessageTypeAI,
								Parts: []gent.ContentPart{
									llms.TextContent{Text: "still alive"},
								},
							},
						},
						ExpireAfterIteration: 10,
					},
				},
				currentIteration: 3,
			},
			expected: expected{
				// system + task + "keep me" + "still alive" + CONTINUE!
				messageCount:    5,
				lastMessageText: "CONTINUE!",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := newMockModel()
			format := newMockFormat()
			tc := newMockToolChain()
			term := newMockTermination()

			loop := NewAgent(model).
				WithFormat(format).
				WithToolChain(tc).
				WithTermination(term)

			data := gent.NewBasicLoopData(&gent.Task{Text: "test"})
			data.SetScratchPad(tt.input.scratchpad)

			execCtx := gent.NewExecutionContext(
				context.Background(), "test", data,
			)
			execCtx.SetLimits(nil)
			for i := 0; i < tt.input.currentIteration; i++ {
				execCtx.IncrementIteration()
			}

			messages := loop.buildMessages(
				execCtx, data, "output", "tools",
			)

			assert.Equal(t,
				tt.expected.messageCount,
				len(messages),
				"message count",
			)

			lastMsg := messages[len(messages)-1]
			lastText, ok := lastMsg.Parts[0].(llms.TextContent)
			require.True(t, ok)
			assert.Equal(t,
				tt.expected.lastMessageText,
				lastText.Text,
				"last message text",
			)
		})
	}
}
