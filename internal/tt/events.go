// Package tt provides test helper functions for the gent testing framework.
package tt

import (
	"github.com/rickchristie/gent"
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

// Terminate creates an AgentLoopResult with LATerminate action and text result.
func Terminate(text string) *gent.AgentLoopResult {
	return &gent.AgentLoopResult{
		Action: gent.LATerminate,
		Result: []gent.ContentPart{gent.TextContent{Text: text}},
	}
}

// -----------------------------------------------------------------------------
// Limit Helpers
// -----------------------------------------------------------------------------

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
		Request: gent.ModelCallRequest{},
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
		Request:      gent.ModelCallRequest{},
		Response:     &gent.ContentResponse{Info: &gent.GenerationInfo{InputTokens: inputTokens, OutputTokens: outputTokens}},
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		Error:        nil,
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

// Compaction creates a CompactionEvent with all fields set.
func Compaction(
	depth, iteration int,
	lengthBefore, lengthAfter int,
) *gent.CompactionEvent {
	return &gent.CompactionEvent{
		BaseEvent: gent.BaseEvent{
			EventName: gent.EventNameCompaction,
			Iteration: iteration,
			Depth:     depth,
		},
		ScratchpadLengthBefore: lengthBefore,
		ScratchpadLengthAfter:  lengthAfter,
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
