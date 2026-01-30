// Package tt provides test helper functions for the gent testing framework.
package tt

import (
	"github.com/rickchristie/gent"
	"github.com/tmc/langchaingo/llms"
)

// -----------------------------------------------------------------------------
// AgentLoopResult Helpers
// -----------------------------------------------------------------------------

// Continue creates an AgentLoopResult with LAContinue action.
func Continue() *gent.AgentLoopResult {
	return &gent.AgentLoopResult{Action: gent.LAContinue}
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
func Limit(limitType gent.LimitType, key string, maxValue float64) gent.Limit {
	return gent.Limit{
		Type:     limitType,
		Key:      key,
		MaxValue: maxValue,
	}
}

// ExactLimit creates a Limit with LimitExactKey type.
func ExactLimit(key string, maxValue float64) gent.Limit {
	return gent.Limit{
		Type:     gent.LimitExactKey,
		Key:      key,
		MaxValue: maxValue,
	}
}

// PrefixLimit creates a Limit with LimitKeyPrefix type.
func PrefixLimit(key string, maxValue float64) gent.Limit {
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
	matchedKey string,
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
