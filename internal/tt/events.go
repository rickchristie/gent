// Package tt provides test helper functions for the gent testing framework.
package tt

import (
	"github.com/rickchristie/gent"
	"github.com/tmc/langchaingo/llms"
)

// -----------------------------------------------------------------------------
// AgentLoopResult Helpers
// -----------------------------------------------------------------------------

// ContinueWithPrompt creates an AgentLoopResult with LAContinue action and specified NextPrompt.
func ContinueWithPrompt(nextPrompt string) *gent.AgentLoopResult {
	return &gent.AgentLoopResult{
		Action:     gent.LAContinue,
		NextPrompt: nextPrompt,
	}
}

// ContinueWithObservation creates an AgentLoopResult with LAContinue action and NextPrompt
// built from format.FormatSections with an "observation" section.
func ContinueWithObservation(format gent.TextFormat, content string) *gent.AgentLoopResult {
	return &gent.AgentLoopResult{
		Action:     gent.LAContinue,
		NextPrompt: Observation(format, content),
	}
}

// Observation builds an observation section using the given format.
func Observation(format gent.TextFormat, content string) string {
	return format.FormatSections([]gent.FormattedSection{
		{Name: "observation", Content: content},
	})
}

// ToolObservation builds the expected NextPrompt for tool execution in the react agent.
// The agent wraps the tool chain result in an observation section. The toolChain argument
// should be a MockToolChain that provides FormatToolResult to build the inner tool result.
func ToolObservation(format gent.TextFormat, toolChain *MockToolChain, toolName, output string) string {
	toolResult := toolChain.FormatToolResult(toolName, output)
	return Observation(format, toolResult)
}

// ValidatorFeedbackObservation builds the expected NextPrompt for validator rejection.
// The validator formats feedback as <section>\nContent\n</section>.
func ValidatorFeedbackObservation(format gent.TextFormat, feedbackSections ...gent.FormattedSection) string {
	var feedback string
	for _, section := range feedbackSections {
		feedback += "<" + section.Name + ">\n" + section.Content + "\n</" + section.Name + ">"
	}
	return Observation(format, feedback)
}

// FormatParseErrorObservation builds the expected NextPrompt for format parse errors.
// This matches the react agent's buildFormatErrorObservation output.
func FormatParseErrorObservation(format gent.TextFormat, parseErr error, rawResponse string) string {
	errorContent := "Format parse error: " + parseErr.Error() + "\n\n" +
		"Your response could not be parsed. Please ensure your response follows the expected format.\n\n" +
		"Your raw response was:\n" +
		rawResponse + "\n\n" +
		"Please try again with proper formatting."
	return Observation(format, errorContent)
}

// ToolchainErrorObservation builds the expected NextPrompt for toolchain execution errors.
// The error is formatted as <error>Error: {message}</error> wrapped in observation.
func ToolchainErrorObservation(format gent.TextFormat, err error) string {
	errorSection := format.FormatSections([]gent.FormattedSection{
		{Name: "error", Content: "Error: " + err.Error()},
	})
	return Observation(format, errorSection)
}

// TerminationParseErrorObservation builds the expected NextPrompt for termination parse errors.
// This matches the react agent's termination parse error feedback format.
func TerminationParseErrorObservation(format gent.TextFormat, parseErr error, content string) string {
	errorContent := "Termination parse error: " + parseErr.Error() + "\n" +
		"Content: " + content + "\n\n" +
		"Please try again with proper formatting."
	return Observation(format, errorContent)
}

// ToolErrorObservation builds the expected NextPrompt for tool execution errors.
// The MockToolChain formats errors differently from the real agent - it includes the empty
// output in the observation rather than wrapping the error. This helper matches mock behavior.
func ToolErrorObservation(toolChain *MockToolChain, toolName string) string {
	// MockToolChain returns the tool result format even with errors, just with empty output
	return toolChain.FormatToolResult(toolName, "")
}

// Terminate creates an AgentLoopResult with LATerminate action and text result.
func Terminate(text string) *gent.AgentLoopResult {
	return &gent.AgentLoopResult{
		Action: gent.LATerminate,
		Result: []gent.ContentPart{llms.TextContent{Text: text}},
	}
}

// -----------------------------------------------------------------------------
// Limit Helpers
// -----------------------------------------------------------------------------

// Limit creates a Limit with all fields set.
func Limit(limitType gent.LimitType, key gent.StatKey, maxValue float64) gent.Limit {
	return gent.Limit{
		Type:     limitType,
		Key:      key,
		MaxValue: maxValue,
	}
}

// ExactLimit creates a Limit with LimitExactKey type.
func ExactLimit(key gent.StatKey, maxValue float64) gent.Limit {
	return gent.Limit{
		Type:     gent.LimitExactKey,
		Key:      key,
		MaxValue: maxValue,
	}
}

// PrefixLimit creates a Limit with LimitKeyPrefix type.
func PrefixLimit(key gent.StatKey, maxValue float64) gent.Limit {
	return gent.Limit{
		Type:     gent.LimitKeyPrefix,
		Key:      key,
		MaxValue: maxValue,
	}
}

// -----------------------------------------------------------------------------
// Event Builder Helpers
// -----------------------------------------------------------------------------

// BeforeExec creates a BeforeExecutionEvent with all fields set.
func BeforeExec(depth, iteration int) *gent.BeforeExecutionEvent {
	return &gent.BeforeExecutionEvent{
		BaseEvent: gent.BaseEvent{
			EventName: gent.EventNameExecutionBefore,
			Iteration: iteration,
			Depth:     depth,
		},
	}
}

// AfterExec creates an AfterExecutionEvent with all fields set.
func AfterExec(
	depth, iteration int,
	termReason gent.TerminationReason,
) *gent.AfterExecutionEvent {
	return &gent.AfterExecutionEvent{
		BaseEvent: gent.BaseEvent{
			EventName: gent.EventNameExecutionAfter,
			Iteration: iteration,
			Depth:     depth,
		},
		TerminationReason: termReason,
	}
}

// BeforeIter creates a BeforeIterationEvent with all fields set.
func BeforeIter(depth, iteration int) *gent.BeforeIterationEvent {
	return &gent.BeforeIterationEvent{
		BaseEvent: gent.BaseEvent{
			EventName: gent.EventNameIterationBefore,
			Iteration: iteration,
			Depth:     depth,
		},
	}
}

// AfterIter creates an AfterIterationEvent with all fields set.
func AfterIter(depth, iteration int, result *gent.AgentLoopResult) *gent.AfterIterationEvent {
	return &gent.AfterIterationEvent{
		BaseEvent: gent.BaseEvent{
			EventName: gent.EventNameIterationAfter,
			Iteration: iteration,
			Depth:     depth,
		},
		Result: result,
	}
}

// BeforeModelCall creates a BeforeModelCallEvent with all fields set.
func BeforeModelCall(depth, iteration int, model string) *gent.BeforeModelCallEvent {
	return &gent.BeforeModelCallEvent{
		BaseEvent: gent.BaseEvent{
			EventName: gent.EventNameModelCallBefore,
			Iteration: iteration,
			Depth:     depth,
		},
		Model:   model,
		Request: nil,
	}
}

// AfterModelCall creates an AfterModelCallEvent with all fields set.
func AfterModelCall(
	depth, iteration int,
	model string,
	inputTokens, outputTokens int,
) *gent.AfterModelCallEvent {
	return &gent.AfterModelCallEvent{
		BaseEvent: gent.BaseEvent{
			EventName: gent.EventNameModelCallAfter,
			Iteration: iteration,
			Depth:     depth,
		},
		Model:        model,
		Request:      nil,
		Response:     &gent.ContentResponse{Info: &gent.GenerationInfo{InputTokens: inputTokens, OutputTokens: outputTokens}},
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		Error:        nil,
	}
}

// BeforeToolCall creates a BeforeToolCallEvent with all fields set.
func BeforeToolCall(depth, iteration int, toolName string, args any) *gent.BeforeToolCallEvent {
	return &gent.BeforeToolCallEvent{
		BaseEvent: gent.BaseEvent{
			EventName: gent.EventNameToolCallBefore,
			Iteration: iteration,
			Depth:     depth,
		},
		ToolName: toolName,
		Args:     args,
	}
}

// AfterToolCall creates an AfterToolCallEvent with all fields set.
func AfterToolCall(
	depth, iteration int,
	toolName string,
	args, output any,
	err error,
) *gent.AfterToolCallEvent {
	return &gent.AfterToolCallEvent{
		BaseEvent: gent.BaseEvent{
			EventName: gent.EventNameToolCallAfter,
			Iteration: iteration,
			Depth:     depth,
		},
		ToolName: toolName,
		Args:     args,
		Output:   output,
		Error:    err,
	}
}

// LimitExceeded creates a LimitExceededEvent with all fields set.
func LimitExceeded(
	depth, iteration int,
	limit gent.Limit,
	currentValue float64,
	matchedKey gent.StatKey,
) *gent.LimitExceededEvent {
	return &gent.LimitExceededEvent{
		BaseEvent: gent.BaseEvent{
			EventName: gent.EventNameLimitExceeded,
			Iteration: iteration,
			Depth:     depth,
		},
		Limit:        limit,
		CurrentValue: currentValue,
		MatchedKey:   matchedKey,
	}
}

// ParseError creates a ParseErrorEvent with all fields set.
func ParseError(
	depth, iteration int,
	errorType gent.ParseErrorType,
	rawContent string,
) *gent.ParseErrorEvent {
	return &gent.ParseErrorEvent{
		BaseEvent: gent.BaseEvent{
			EventName: gent.EventNameParseError,
			Iteration: iteration,
			Depth:     depth,
		},
		ErrorType:  errorType,
		RawContent: rawContent,
		Error:      nil,
	}
}

// ValidatorCalled creates a ValidatorCalledEvent with all fields set.
func ValidatorCalled(
	depth, iteration int,
	validatorName string,
	answer any,
) *gent.ValidatorCalledEvent {
	return &gent.ValidatorCalledEvent{
		BaseEvent: gent.BaseEvent{
			EventName: gent.EventNameValidatorCalled,
			Iteration: iteration,
			Depth:     depth,
		},
		ValidatorName: validatorName,
		Answer:        answer,
	}
}

// ValidatorResult creates a ValidatorResultEvent with all fields set.
func ValidatorResult(
	depth, iteration int,
	validatorName string,
	answer any,
	accepted bool,
	feedback []gent.FormattedSection,
) *gent.ValidatorResultEvent {
	return &gent.ValidatorResultEvent{
		BaseEvent: gent.BaseEvent{
			EventName: gent.EventNameValidatorResult,
			Iteration: iteration,
			Depth:     depth,
		},
		ValidatorName: validatorName,
		Answer:        answer,
		Accepted:      accepted,
		Feedback:      feedback,
	}
}

// Error creates an ErrorEvent with all fields set.
func Error(depth, iteration int, err error) *gent.ErrorEvent {
	return &gent.ErrorEvent{
		BaseEvent: gent.BaseEvent{
			EventName: gent.EventNameError,
			Iteration: iteration,
			Depth:     depth,
		},
		Error: err,
	}
}
