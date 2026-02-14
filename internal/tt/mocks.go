package tt

import (
	"time"

	"github.com/rickchristie/gent"
	"github.com/tmc/langchaingo/llms"
)

// -----------------------------------------------------------------------------
// MockModel - implements gent.Model with proper event publishing
// -----------------------------------------------------------------------------

// MockModel is a configurable mock that implements gent.Model.
// It publishes BeforeModelCall and AfterModelCall events as required by the interface.
type MockModel struct {
	name      string
	responses []*gent.ContentResponse
	errors    []error
	callCount int

	// CapturedMessages stores the messages passed to each
	// GenerateContent call. Populated automatically on
	// every call.
	CapturedMessages [][]llms.MessageContent
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

// AddResponse queues a response with the specified content and token counts.
func (m *MockModel) AddResponse(content string, inputTokens, outputTokens int) *MockModel {
	m.responses = append(m.responses, &gent.ContentResponse{
		Choices: []*gent.ContentChoice{{Content: content}},
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

// CallCount returns the number of times GenerateContent has been called.
func (m *MockModel) CallCount() int {
	return m.callCount
}

// GenerateContent implements gent.Model with proper event publishing.
func (m *MockModel) GenerateContent(
	execCtx *gent.ExecutionContext,
	streamId string,
	streamTopicId string,
	messages []llms.MessageContent,
	opts ...llms.CallOption,
) (*gent.ContentResponse, error) {
	idx := m.callCount
	m.callCount++

	// Capture messages for test verification
	m.CapturedMessages = append(
		m.CapturedMessages, messages,
	)

	// Publish BeforeModelCall event
	if execCtx != nil {
		execCtx.PublishBeforeModelCall(m.name, messages)
	}

	startTime := time.Now()

	// Check for configured error
	var err error
	if idx < len(m.errors) && m.errors[idx] != nil {
		err = m.errors[idx]
	}

	// Get response
	var resp *gent.ContentResponse
	if err == nil {
		if idx < len(m.responses) && m.responses[idx] != nil {
			resp = m.responses[idx]
		} else {
			// Default: return termination response
			resp = &gent.ContentResponse{
				Choices: []*gent.ContentChoice{{Content: "<answer>done</answer>"}},
				Info:    &gent.GenerationInfo{InputTokens: 10, OutputTokens: 5},
			}
		}
	}

	duration := time.Since(startTime)

	// Publish AfterModelCall event (stats are auto-updated)
	if execCtx != nil {
		execCtx.PublishAfterModelCall(m.name, messages, resp, duration, err)
	}

	return resp, err
}

// -----------------------------------------------------------------------------
// MockToolChain - implements gent.ToolChain with proper event publishing
// -----------------------------------------------------------------------------

// MockToolFunc is a tool function signature that receives the execution context.
type MockToolFunc func(execCtx *gent.ExecutionContext, args map[string]any) (string, error)

// MockToolChain is a configurable mock that implements gent.ToolChain.
// It publishes BeforeToolCall, AfterToolCall, and ParseError events as required.
type MockToolChain struct {
	name        string
	tools       map[string]MockToolFunc
	parseErrors []error
	callIdx     int
}

// NewMockToolChain creates a new MockToolChain with default name "action".
func NewMockToolChain() *MockToolChain {
	return &MockToolChain{
		name:  "action",
		tools: make(map[string]MockToolFunc),
	}
}

// WithTool adds a simple tool that doesn't need execution context.
func (tc *MockToolChain) WithTool(
	name string,
	fn func(args map[string]any) (string, error),
) *MockToolChain {
	tc.tools[name] = func(_ *gent.ExecutionContext, args map[string]any) (string, error) {
		return fn(args)
	}
	return tc
}

// WithToolCtx adds a tool that receives the execution context.
func (tc *MockToolChain) WithToolCtx(name string, fn MockToolFunc) *MockToolChain {
	tc.tools[name] = fn
	return tc
}

// WithParseErrors configures parse errors to return on subsequent Execute calls.
func (tc *MockToolChain) WithParseErrors(errs ...error) *MockToolChain {
	tc.parseErrors = errs
	return tc
}

// Name implements gent.TextSection.
func (tc *MockToolChain) Name() string { return tc.name }

// Guidance implements gent.TextSection.
func (tc *MockToolChain) Guidance() string { return "Use YAML format for tool calls." }

// AvailableToolsPrompt implements gent.ToolChain.
func (tc *MockToolChain) AvailableToolsPrompt() string {
	return "Available tools: test tools"
}

// RegisterTool implements gent.ToolChain.
func (tc *MockToolChain) RegisterTool(_ any) gent.ToolChain { return tc }

// ParseSection implements gent.TextSection.
func (tc *MockToolChain) ParseSection(
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
		execCtx.Stats().ResetGauge(gent.SGToolchainParseErrorConsecutive)
	}
	return content, nil
}

// Execute implements gent.ToolChain with proper event publishing.
func (tc *MockToolChain) Execute(
	execCtx *gent.ExecutionContext,
	content string,
	format gent.TextFormat,
) (*gent.ToolChainResult, error) {
	idx := tc.callIdx
	tc.callIdx++

	// Check for parse error
	if idx < len(tc.parseErrors) && tc.parseErrors[idx] != nil {
		err := tc.parseErrors[idx]
		if execCtx != nil {
			execCtx.PublishParseError(gent.ParseErrorTypeToolchain, content, err)
		}
		return nil, err
	}

	// Success resets consecutive counter
	if execCtx != nil {
		execCtx.Stats().ResetGauge(gent.SGToolchainParseErrorConsecutive)
	}

	// Parse tool name from content (simple mock: "tool: <name>")
	toolName := "test_tool"
	if len(content) > 6 && content[:6] == "tool: " {
		toolName = content[6:]
		for i, c := range toolName {
			if c == '\n' || c == ' ' {
				toolName = toolName[:i]
				break
			}
		}
	}

	// Publish BeforeToolCall event (stats are auto-updated)
	if execCtx != nil {
		execCtx.PublishBeforeToolCall(toolName, nil)
	}

	startTime := time.Now()

	// Execute the tool
	var toolErr error
	var output string
	if fn, ok := tc.tools[toolName]; ok {
		output, toolErr = fn(execCtx, nil)
	} else {
		output = "tool executed"
	}

	duration := time.Since(startTime)

	// Publish AfterToolCall event (stats are auto-updated)
	if execCtx != nil {
		execCtx.PublishAfterToolCall(toolName, nil, output, duration, toolErr)

		// Reset consecutive error counters on success
		if toolErr == nil {
			execCtx.Stats().ResetGauge(
				gent.SGToolCallsErrorConsecutive,
			)
			execCtx.Stats().ResetGauge(
				gent.SGToolCallsErrorConsecutiveFor +
					gent.StatKey(toolName),
			)
		}
	}

	var resultErr error
	if toolErr != nil {
		resultErr = toolErr
	}

	return &gent.ToolChainResult{
		Text: "<observation>\n<" + toolName + ">\n" + output + "\n</" + toolName +
			">\n</observation>",
		Raw: &gent.RawToolChainResult{
			Calls:   []*gent.ToolCall{{Name: toolName, Args: nil}},
			Results: []*gent.RawToolCallResult{{Name: toolName, Output: output}},
			Errors:  []error{resultErr},
		},
	}, nil
}

// FormatToolResult returns the tool result text that MockToolChain.Execute generates.
// This is useful for tests to build expected NextPrompt values.
// The result format is: <observation>\n<toolname>\noutput\n</toolname>\n</observation>
func (tc *MockToolChain) FormatToolResult(toolName, output string) string {
	return "<observation>\n<" + toolName + ">\n" + output + "\n</" + toolName + ">\n</observation>"
}

// -----------------------------------------------------------------------------
// MockTermination - implements gent.Termination with proper event publishing
// -----------------------------------------------------------------------------

// MockTermination is a configurable mock that implements gent.Termination.
// It publishes ParseError, ValidatorCalled, and ValidatorResult events as required.
type MockTermination struct {
	parseErrors []error
	callIdx     int
	validator   gent.AnswerValidator
}

// NewMockTermination creates a new MockTermination.
func NewMockTermination() *MockTermination {
	return &MockTermination{}
}

// WithParseErrors configures parse errors to return on subsequent calls.
func (t *MockTermination) WithParseErrors(errs ...error) *MockTermination {
	t.parseErrors = errs
	return t
}

// Name implements gent.TextSection.
func (t *MockTermination) Name() string { return "answer" }

// Guidance implements gent.TextSection.
func (t *MockTermination) Guidance() string { return "Write your final answer." }

// SetValidator implements gent.Termination.
func (t *MockTermination) SetValidator(validator gent.AnswerValidator) {
	t.validator = validator
}

// ParseSection implements gent.TextSection with proper event publishing.
func (t *MockTermination) ParseSection(
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
		execCtx.Stats().ResetGauge(
			gent.SGTerminationParseErrorConsecutive,
		)
	}
	return content, nil
}

// ShouldTerminate implements gent.Termination with proper event publishing.
func (t *MockTermination) ShouldTerminate(
	execCtx *gent.ExecutionContext,
	content string,
) *gent.TerminationResult {
	if execCtx == nil {
		panic("MockTermination: ShouldTerminate called with nil ExecutionContext")
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
		validatorName := t.validator.Name()

		// Publish ValidatorCalled event
		execCtx.PublishValidatorCalled(validatorName, content)

		result := t.validator.Validate(execCtx, content)
		if !result.Accepted {
			// Publish ValidatorResult event (stats are auto-updated)
			execCtx.PublishValidatorResult(validatorName, content, false, result.Feedback)

			var feedback []gent.ContentPart
			for _, section := range result.Feedback {
				formatted := "<" + section.Name + ">\n" + section.Content + "\n</" + section.Name +
					">"
				feedback = append(feedback, llms.TextContent{Text: formatted})
			}

			return &gent.TerminationResult{
				Status:  gent.TerminationAnswerRejected,
				Content: feedback,
			}
		}

		// Publish ValidatorResult event for acceptance
		execCtx.PublishValidatorResult(validatorName, content, true, nil)
	}

	return &gent.TerminationResult{
		Status:  gent.TerminationAnswerAccepted,
		Content: []gent.ContentPart{llms.TextContent{Text: content}},
	}
}

// -----------------------------------------------------------------------------
// MockFormat - implements gent.TextFormat with proper event publishing
// -----------------------------------------------------------------------------

// MockFormatCall represents a single parse call configuration.
type MockFormatCall struct {
	Result map[string][]string
	Err    error
}

// MockFormat is a configurable mock that implements gent.TextFormat.
// It publishes ParseError events as required.
type MockFormat struct {
	calls   []MockFormatCall
	callIdx int
}

// NewMockFormat creates a new MockFormat.
func NewMockFormat() *MockFormat {
	return &MockFormat{}
}

// AddParseResult queues a successful parse result.
func (f *MockFormat) AddParseResult(result map[string][]string) *MockFormat {
	f.calls = append(f.calls, MockFormatCall{Result: result})
	return f
}

// AddParseError queues a parse error.
func (f *MockFormat) AddParseError(err error) *MockFormat {
	f.calls = append(f.calls, MockFormatCall{Err: err})
	return f
}

// RegisterSection implements gent.TextFormat.
func (f *MockFormat) RegisterSection(_ gent.TextSection) gent.TextFormat { return f }

// DescribeStructure implements gent.TextFormat.
func (f *MockFormat) DescribeStructure() string { return "XML format" }

// Parse implements gent.TextFormat with proper event publishing.
func (f *MockFormat) Parse(
	execCtx *gent.ExecutionContext,
	output string,
) (map[string][]string, error) {
	idx := f.callIdx
	f.callIdx++

	if idx < len(f.calls) {
		call := f.calls[idx]
		if call.Err != nil {
			if execCtx != nil {
				execCtx.PublishParseError(gent.ParseErrorTypeFormat, output, call.Err)
			}
			return nil, call.Err
		}

		// Success resets consecutive counter
		if execCtx != nil {
			execCtx.Stats().ResetGauge(gent.SGFormatParseErrorConsecutive)
		}
		return call.Result, nil
	}

	// Default: terminate
	if execCtx != nil {
		execCtx.Stats().ResetGauge(gent.SGFormatParseErrorConsecutive)
	}
	return map[string][]string{"answer": {"done"}}, nil
}

// FormatSections implements gent.TextFormat.
func (f *MockFormat) FormatSections(sections []gent.FormattedSection) string {
	var result string
	for _, s := range sections {
		result += "<" + s.Name + ">\n" + s.Content + "\n</" + s.Name + ">\n"
	}
	return result
}

// -----------------------------------------------------------------------------
// MockValidator - implements gent.AnswerValidator
// -----------------------------------------------------------------------------

// MockValidator is a configurable mock that implements gent.AnswerValidator.
// It supports both simple accept/reject modes and sequence-based acceptances.
type MockValidator struct {
	name        string
	acceptances []bool
	feedback    []gent.FormattedSection
	callIdx     int
	validate    func(execCtx *gent.ExecutionContext, answer any) *gent.ValidationResult
}

// NewMockValidator creates a new MockValidator with the given name.
// By default, it accepts all answers.
func NewMockValidator(name string) *MockValidator {
	return &MockValidator{
		name: name,
	}
}

// WithValidateFunc sets a custom validation function.
// This overrides sequence-based acceptances.
func (v *MockValidator) WithValidateFunc(
	fn func(execCtx *gent.ExecutionContext, answer any) *gent.ValidationResult,
) *MockValidator {
	v.validate = fn
	return v
}

// WithAcceptances configures a sequence of accept/reject results.
// Each call to Validate will use the next value in the sequence.
// After the sequence is exhausted, further calls will accept.
func (v *MockValidator) WithAcceptances(acceptances ...bool) *MockValidator {
	v.acceptances = acceptances
	return v
}

// WithFeedback sets the feedback sections to return when rejecting.
func (v *MockValidator) WithFeedback(sections ...gent.FormattedSection) *MockValidator {
	v.feedback = sections
	return v
}

// WithReject configures the validator to always reject with the given feedback.
func (v *MockValidator) WithReject(feedback ...gent.FormattedSection) *MockValidator {
	v.feedback = feedback
	v.validate = func(_ *gent.ExecutionContext, _ any) *gent.ValidationResult {
		return &gent.ValidationResult{
			Accepted: false,
			Feedback: feedback,
		}
	}
	return v
}

// WithAccept configures the validator to always accept.
func (v *MockValidator) WithAccept() *MockValidator {
	v.validate = func(_ *gent.ExecutionContext, _ any) *gent.ValidationResult {
		return &gent.ValidationResult{Accepted: true}
	}
	return v
}

// Name implements gent.AnswerValidator.
func (v *MockValidator) Name() string { return v.name }

// Validate implements gent.AnswerValidator.
func (v *MockValidator) Validate(
	execCtx *gent.ExecutionContext,
	answer any,
) *gent.ValidationResult {
	// If custom validate function is set, use it
	if v.validate != nil {
		return v.validate(execCtx, answer)
	}

	// Use sequence-based acceptances
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

// -----------------------------------------------------------------------------
// MockSection - implements gent.TextSection with proper event publishing
// -----------------------------------------------------------------------------

// MockSection is a configurable mock that implements gent.TextSection.
// It publishes ParseError events with ParseErrorTypeSection as required.
type MockSection struct {
	name        string
	guidance    string
	parseErrors []error
	callIdx     int
}

// NewMockSection creates a new MockSection with the given name.
func NewMockSection(name string) *MockSection {
	return &MockSection{
		name:     name,
		guidance: "Mock section guidance",
	}
}

// WithGuidance sets the guidance text for this section.
func (s *MockSection) WithGuidance(guidance string) *MockSection {
	s.guidance = guidance
	return s
}

// WithParseErrors configures parse errors to return on subsequent ParseSection calls.
func (s *MockSection) WithParseErrors(errs ...error) *MockSection {
	s.parseErrors = errs
	return s
}

// Name implements gent.TextSection.
func (s *MockSection) Name() string { return s.name }

// Guidance implements gent.TextSection.
func (s *MockSection) Guidance() string { return s.guidance }

// ParseSection implements gent.TextSection with proper event publishing.
func (s *MockSection) ParseSection(
	execCtx *gent.ExecutionContext,
	content string,
) (any, error) {
	idx := s.callIdx
	s.callIdx++

	if idx < len(s.parseErrors) && s.parseErrors[idx] != nil {
		err := s.parseErrors[idx]
		if execCtx != nil {
			execCtx.PublishParseError(gent.ParseErrorTypeSection, content, err)
		}
		return nil, err
	}

	// Success resets consecutive counter
	if execCtx != nil {
		execCtx.Stats().ResetGauge(
			gent.SGSectionParseErrorConsecutive,
		)
	}
	return content, nil
}

// Compile-time check that MockSection implements gent.TextSection.
var _ gent.TextSection = (*MockSection)(nil)

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
) bool {
	if t.callIdx >= len(t.shouldCompact) {
		panic(
			"MockCompactionTrigger: exhausted " +
				"ShouldCompact sequence",
		)
	}
	result := t.shouldCompact[t.callIdx]
	t.callIdx++
	return result
}

// NotifyCompacted implements gent.CompactionTrigger.
func (t *MockCompactionTrigger) NotifyCompacted(
	_ *gent.ExecutionContext,
) {
	t.notifiedCount++
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
	compactFunc func(execCtx *gent.ExecutionContext) error
	callCount   int
}

// NewMockCompactionStrategy creates a new
// MockCompactionStrategy.
func NewMockCompactionStrategy() *MockCompactionStrategy {
	return &MockCompactionStrategy{}
}

// WithCompactFunc sets the Compact implementation.
func (s *MockCompactionStrategy) WithCompactFunc(
	fn func(execCtx *gent.ExecutionContext) error,
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
		_ *gent.ExecutionContext,
	) error {
		return err
	}
	return s
}

// Compact implements gent.CompactionStrategy.
func (s *MockCompactionStrategy) Compact(
	execCtx *gent.ExecutionContext,
) error {
	s.callCount++
	if s.compactFunc != nil {
		return s.compactFunc(execCtx)
	}
	return nil
}

// CallCount returns how many times Compact was called.
func (s *MockCompactionStrategy) CallCount() int {
	return s.callCount
}

// Compile-time check.
var _ gent.CompactionStrategy = (*MockCompactionStrategy)(nil)
