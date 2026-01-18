package agents

import (
	"context"
	"errors"
	"testing"

	"github.com/rickchristie/gent"
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

func (m *mockFormat) Describe(sections []gent.TextOutputSection) string {
	return "mock format description"
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
// ReactLoopData Tests
// ----------------------------------------------------------------------------

func TestReactLoopData_GetOriginalInput(t *testing.T) {
	input := []gent.ContentPart{llms.TextContent{Text: "test input"}}
	data := NewReactLoopData(input...)

	result := data.GetOriginalInput()
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

func TestReactLoopData_IterationHistory(t *testing.T) {
	data := NewReactLoopData()

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

func TestReactLoopData_Iterations(t *testing.T) {
	data := NewReactLoopData()

	// Initially empty
	if len(data.GetIterations()) != 0 {
		t.Fatalf("expected empty iterations, got %d", len(data.GetIterations()))
	}

	// Set iterations
	iter := &gent.Iteration{
		Messages: []gent.MessageContent{
			{Role: llms.ChatMessageTypeAI, Parts: []gent.ContentPart{llms.TextContent{Text: "test"}}},
		},
	}
	data.SetIterations([]*gent.Iteration{iter})

	iterations := data.GetIterations()
	if len(iterations) != 1 {
		t.Fatalf("expected 1 iteration, got %d", len(iterations))
	}
}

// ----------------------------------------------------------------------------
// ReactLoop Tests
// ----------------------------------------------------------------------------

func TestReactLoop_BuildOutputSections(t *testing.T) {
	model := newMockModel()
	tc := newMockToolChain()
	term := newMockTermination()

	loop := NewReactLoop(model).
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

func TestReactLoop_BuildMessages(t *testing.T) {
	model := newMockModel()
	format := newMockFormat()
	tc := newMockToolChain()
	term := newMockTermination()

	loop := NewReactLoop(model).
		WithSystemPrompt("You are helpful.").
		WithFormat(format).
		WithToolChain(tc).
		WithTermination(term)

	data := NewReactLoopData(llms.TextContent{Text: "Hello"})

	messages := loop.buildMessages(data, "output prompt")

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

func TestReactLoop_Next_Termination(t *testing.T) {
	response := &gent.ContentResponse{
		Choices: []*gent.ContentChoice{{Content: "<answer>The answer is 42</answer>"}},
	}
	model := newMockModel(response)

	format := newMockFormat().WithParseResult(map[string][]string{
		"answer": {"The answer is 42"},
	})
	tc := newMockToolChain()
	term := newMockTermination()

	loop := NewReactLoop(model).
		WithFormat(format).
		WithToolChain(tc).
		WithTermination(term)

	data := NewReactLoopData(llms.TextContent{Text: "What is 6*7?"})
	execCtx := newTestExecCtx(data)
	result := loop.Next(context.Background(), execCtx)

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

func TestReactLoop_Next_ToolExecution(t *testing.T) {
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

	loop := NewReactLoop(model).
		WithFormat(format).
		WithToolChain(tc).
		WithTermination(term)

	data := NewReactLoopData(llms.TextContent{Text: "Search for test"})
	execCtx := newTestExecCtx(data)
	result := loop.Next(context.Background(), execCtx)

	if result.Action != gent.LAContinue {
		t.Errorf("expected LAContinue, got %v", result.Action)
	}

	if result.NextPrompt == "" {
		t.Error("expected NextPrompt to be set")
	}

	// Check that iteration was added
	if len(data.GetIterations()) != 1 {
		t.Errorf("expected 1 iteration, got %d", len(data.GetIterations()))
	}
}

func TestReactLoop_Next_ToolError(t *testing.T) {
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

	loop := NewReactLoop(model).
		WithFormat(format).
		WithToolChain(tc).
		WithTermination(term)

	data := NewReactLoopData(llms.TextContent{Text: "Use broken tool"})
	execCtx := newTestExecCtx(data)
	result := loop.Next(context.Background(), execCtx)

	if result.Action != gent.LAContinue {
		t.Errorf("expected LAContinue, got %v", result.Action)
	}

	if result.NextPrompt == "" {
		t.Error("expected NextPrompt with error info")
	}
}

func TestReactLoop_Next_ModelError(t *testing.T) {
	model := newMockModel().WithErrors(errors.New("model failed"))
	format := newMockFormat()
	tc := newMockToolChain()
	term := newMockTermination()

	loop := NewReactLoop(model).
		WithFormat(format).
		WithToolChain(tc).
		WithTermination(term)

	data := NewReactLoopData(llms.TextContent{Text: "Hello"})
	execCtx := newTestExecCtx(data)
	result := loop.Next(context.Background(), execCtx)

	if result.Action != gent.LATerminate {
		t.Errorf("expected LATerminate, got %v", result.Action)
	}

	if len(result.Result) == 0 {
		t.Fatal("expected error result")
	}

	tc2, ok := result.Result[0].(llms.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Result[0])
	}
	if tc2.Text == "" {
		t.Error("expected error message in result")
	}
}

func TestReactLoop_Next_ParseError(t *testing.T) {
	response := &gent.ContentResponse{
		Choices: []*gent.ContentChoice{{Content: "invalid response"}},
	}
	model := newMockModel(response)

	format := newMockFormat().WithParseError(gent.ErrNoSectionsFound)
	tc := newMockToolChain()
	// Termination that doesn't accept raw content
	term := &mockTermination{name: "answer", prompt: "answer"}

	loop := NewReactLoop(model).
		WithFormat(format).
		WithToolChain(tc).
		WithTermination(term)

	data := NewReactLoopData(llms.TextContent{Text: "Hello"})
	execCtx := newTestExecCtx(data)
	result := loop.Next(context.Background(), execCtx)

	// Should terminate with error since parse failed and raw content isn't valid termination
	if result.Action != gent.LATerminate {
		t.Errorf("expected LATerminate, got %v", result.Action)
	}
}

func TestReactLoop_Next_ParseError_FallbackToTermination(t *testing.T) {
	response := &gent.ContentResponse{
		Choices: []*gent.ContentChoice{{Content: "The answer is 42"}},
	}
	model := newMockModel(response)

	format := newMockFormat().WithParseError(gent.ErrNoSectionsFound)
	tc := newMockToolChain()
	term := newMockTermination().
		WithTerminationResult(llms.TextContent{Text: "The answer is 42"})

	loop := NewReactLoop(model).
		WithFormat(format).
		WithToolChain(tc).
		WithTermination(term)

	data := NewReactLoopData(llms.TextContent{Text: "What is 6*7?"})
	execCtx := newTestExecCtx(data)
	result := loop.Next(context.Background(), execCtx)

	// Should terminate successfully since raw content is valid termination
	if result.Action != gent.LATerminate {
		t.Errorf("expected LATerminate, got %v", result.Action)
	}

	if len(result.Result) == 0 {
		t.Fatal("expected result")
	}

	tc2, ok := result.Result[0].(llms.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Result[0])
	}
	if tc2.Text != "The answer is 42" {
		t.Errorf("expected 'The answer is 42', got %q", tc2.Text)
	}
}

func TestReactLoop_Next_MultipleTools(t *testing.T) {
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

	loop := NewReactLoop(model).
		WithFormat(format).
		WithToolChain(tc).
		WithTermination(term)

	data := NewReactLoopData(llms.TextContent{Text: "Use tools a and b"})
	execCtx := newTestExecCtx(data)
	result := loop.Next(context.Background(), execCtx)

	if result.Action != gent.LAContinue {
		t.Errorf("expected LAContinue, got %v", result.Action)
	}

	// Should have results from both tools in the observation
	if result.NextPrompt == "" {
		t.Error("expected NextPrompt to be set")
	}
}

func TestReactLoop_RegisterTool(t *testing.T) {
	model := newMockModel()
	tc := newMockToolChain()

	loop := NewReactLoop(model).WithToolChain(tc)

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

func TestNewReactLoop_Defaults(t *testing.T) {
	model := newMockModel()
	loop := NewReactLoop(model)

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
	if loop.observationPrefix != "Observation:\n" {
		t.Errorf("expected default observationPrefix, got %q", loop.observationPrefix)
	}
	if loop.errorPrefix != "Error:\n" {
		t.Errorf("expected default errorPrefix, got %q", loop.errorPrefix)
	}
	if loop.systemTemplate == nil {
		t.Error("expected default systemTemplate to be set")
	}
}
