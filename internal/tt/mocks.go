package tt

import (
	"context"

	"github.com/rickchristie/gent"
)

// -----------------------------------------------------------------------------
// MockModel - implements gent.Model with fixed responses
// -----------------------------------------------------------------------------

// MockModel is a configurable mock that implements gent.Model.
// GenerateContentStream returns fixed responses. The managed context publishes model events.
type MockModel struct {
	name      string
	responses []*gent.ContentResponse
	errors    []error
	callCount int

	// CapturedRequests and CapturedMessages contain each submitted request.
	CapturedRequests []gent.ModelCallRequest
	CapturedMessages [][]*gent.ModelMessage
}

// NewMockModel creates a new MockModel with the default name "test-model".
func NewMockModel() *MockModel {
	return &MockModel{name: "test-model"}
}

// WithName sets the model name used for event publishing.
func (m *MockModel) WithName(name string) *MockModel {
	m.name = name
	return m
}

func (m *MockModel) ModelName() string { return m.name }

// AddResponse queues a response with the specified content and token counts.
func (m *MockModel) AddResponse(content string, inputTokens, outputTokens int) *MockModel {
	m.responses = append(m.responses, &gent.ContentResponse{
		Choices: []*gent.ContentChoice{
			{
				Completion:       gent.ModelCompletion{Status: gent.ModelStatusAnswer},
				CandidateContent: []gent.ContentPart{gent.TextContent{Text: content}},
			},
		},
		Info: &gent.GenerationInfo{
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
		},
	})
	return m
}

// AddRawResponse queues a raw ContentResponse.
// Use this when you need full control over the response
// structure (e.g., empty Choices slice).
func (m *MockModel) AddRawResponse(
	resp *gent.ContentResponse,
) *MockModel {
	m.responses = append(m.responses, resp)
	return m
}

// AddError queues an error for the next call.
func (m *MockModel) AddError(err error) *MockModel {
	// Extend responses slice if needed to match errors length
	for len(m.responses) <= len(m.errors) {
		m.responses = append(m.responses, nil)
	}
	m.errors = append(m.errors, err)
	return m
}

// CallCount returns the number of model requests.
func (m *MockModel) CallCount() int {
	return m.callCount
}

// GenerateContentStream returns a completed stream with the next configured response or error.
func (m *MockModel) GenerateContentStream(
	_ context.Context, request gent.ModelCallRequest,
) (gent.Stream, error) {
	idx := m.callCount
	m.callCount++
	m.CapturedRequests = append(m.CapturedRequests, request)
	m.CapturedMessages = append(m.CapturedMessages, request.Messages)
	if idx < len(m.errors) && m.errors[idx] != nil {
		return gent.NewCompletedStream(nil, m.errors[idx]), nil
	}
	if idx < len(m.responses) && m.responses[idx] != nil {
		return gent.NewCompletedStream(m.responses[idx], nil), nil
	}
	response := &gent.ContentResponse{
		Choices: []*gent.ContentChoice{
			{
				Completion:       gent.ModelCompletion{Status: gent.ModelStatusAnswer},
				CandidateContent: []gent.ContentPart{gent.TextContent{Text: "done"}},
			},
		},
		Info: &gent.GenerationInfo{InputTokens: 10, OutputTokens: 5},
	}
	return gent.NewCompletedStream(response, nil), nil
}

// -----------------------------------------------------------------------------
// MockFormat - implements gent.TextFormat with proper event publishing
// -----------------------------------------------------------------------------

type MockFormat struct{}

func NewMockFormat() *MockFormat { return &MockFormat{} }

// FormatSections implements gent.TextFormat.
func (f *MockFormat) FormatSections(sections []gent.FormattedSection) string {
	var result string
	for _, s := range sections {
		result += "<" + s.Name + ">\n" + s.Content + "\n</" + s.Name + ">\n"
	}
	return result
}

// ----------------------------------------------------------
// MockCompactionTrigger
// ----------------------------------------------------------

// MockCompactionTrigger is a configurable mock that implements
// gent.CompactionTrigger.
type MockCompactionTrigger struct {
	shouldCompact []bool
	callIdx       int
	notifiedCount int
}

// NewMockCompactionTrigger creates a new MockCompactionTrigger.
func NewMockCompactionTrigger() *MockCompactionTrigger {
	return &MockCompactionTrigger{}
}

// WithShouldCompact sets the sequence of ShouldCompact
// return values. Panics if exhausted.
func (t *MockCompactionTrigger) WithShouldCompact(
	values ...bool,
) *MockCompactionTrigger {
	t.shouldCompact = values
	return t
}

// ShouldCompact implements gent.CompactionTrigger.
func (t *MockCompactionTrigger) ShouldCompact(
	_ *gent.ExecutionContext,
) (bool, error) {
	if t.callIdx >= len(t.shouldCompact) {
		panic(
			"MockCompactionTrigger: exhausted " +
				"ShouldCompact sequence",
		)
	}
	result := t.shouldCompact[t.callIdx]
	t.callIdx++
	return result, nil
}

// NotifyCompacted implements gent.CompactionTrigger.
func (t *MockCompactionTrigger) NotifyCompacted(_ *gent.ExecutionContext) error {
	t.notifiedCount++
	return nil
}

// NotifiedCount returns how many times NotifyCompacted was
// called.
func (t *MockCompactionTrigger) NotifiedCount() int {
	return t.notifiedCount
}

// Compile-time check.
var _ gent.CompactionTrigger = (*MockCompactionTrigger)(nil)

// ----------------------------------------------------------
// MockCompactionStrategy
// ----------------------------------------------------------

// MockCompactionStrategy is a configurable mock that
// implements gent.CompactionStrategy.
type MockCompactionStrategy struct {
	compactFunc func(*gent.ExecutionContext, gent.CompactionInput) (gent.HistoryUpdate, error)
	callCount   int
}

// NewMockCompactionStrategy creates a new
// MockCompactionStrategy.
func NewMockCompactionStrategy() *MockCompactionStrategy {
	return &MockCompactionStrategy{}
}

// WithCompactFunc sets the Compact implementation.
func (s *MockCompactionStrategy) WithCompactFunc(
	fn func(*gent.ExecutionContext, gent.CompactionInput) (gent.HistoryUpdate, error),
) *MockCompactionStrategy {
	s.compactFunc = fn
	return s
}

// WithError creates a strategy that always returns the
// given error.
func (s *MockCompactionStrategy) WithError(
	err error,
) *MockCompactionStrategy {
	s.compactFunc = func(
		_ *gent.ExecutionContext, _ gent.CompactionInput,
	) (gent.HistoryUpdate, error) {
		return gent.HistoryUpdate{}, err
	}
	return s
}

// Compact implements gent.CompactionStrategy.
func (s *MockCompactionStrategy) Compact(
	execCtx *gent.ExecutionContext, input gent.CompactionInput,
) (gent.HistoryUpdate, error) {
	s.callCount++
	if s.compactFunc != nil {
		return s.compactFunc(execCtx, input)
	}
	return gent.HistoryUpdate{ExpectedDraftRevision: input.Revision, Active: input.EntryIds()}, nil
}

// CallCount returns how many times Compact was called.
func (s *MockCompactionStrategy) CallCount() int {
	return s.callCount
}

// Compile-time check.
var _ gent.CompactionStrategy = (*MockCompactionStrategy)(nil)

// AddToolCall queues a native function call with explicit arguments.
func (m *MockModel) AddToolCall(name, arguments string, inputTokens, outputTokens int) *MockModel {
	m.responses = append(m.responses, &gent.ContentResponse{
		Choices: []*gent.ContentChoice{{Completion: gent.ModelCompletion{
			Status: gent.ModelStatusToolCalls,
		},
			ToolCalls: []gent.ModelToolCall{{
				ID: "test-call", Type: "function",
				FunctionCall: &gent.FunctionCall{Name: name, Arguments: arguments}}}}},
		Info: &gent.GenerationInfo{InputTokens: inputTokens, OutputTokens: outputTokens}})
	return m
}
