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
		execCtx.Stats().ResetCounter(gent.KeyToolchainParseErrorConsecutive)
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
		execCtx.Stats().ResetCounter(gent.KeyToolchainParseErrorConsecutive)
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
			execCtx.Stats().ResetCounter(gent.KeyToolCallsErrorConsecutive)
			execCtx.Stats().ResetCounter(gent.KeyToolCallsErrorConsecutiveFor + toolName)
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
		execCtx.Stats().ResetCounter(gent.KeyTerminationParseErrorConsecutive)
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
			execCtx.Stats().ResetCounter(gent.KeyFormatParseErrorConsecutive)
		}
		return call.Result, nil
	}

	// Default: terminate
	if execCtx != nil {
		execCtx.Stats().ResetCounter(gent.KeyFormatParseErrorConsecutive)
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
