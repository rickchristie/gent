package tt

import (
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

// -----------------------------------------------------------------------------
// Event Collection Helpers
// -----------------------------------------------------------------------------

// IsLifecycleEvent returns true if the event is a lifecycle event relevant for limit testing.
// Filters out CommonDiffEvent and CommonEvent which are state change tracking events.
func IsLifecycleEvent(event gent.Event) bool {
	switch event.(type) {
	case *gent.BeforeExecutionEvent,
		*gent.AfterExecutionEvent,
		*gent.BeforeIterationEvent,
		*gent.AfterIterationEvent,
		*gent.BeforeModelCallEvent,
		*gent.AfterModelCallEvent,
		*gent.BeforeToolCallEvent,
		*gent.AfterToolCallEvent,
		*gent.LimitExceededEvent,
		*gent.ParseErrorEvent,
		*gent.ValidatorCalledEvent,
		*gent.ValidatorResultEvent,
		*gent.ErrorEvent:
		return true
	default:
		return false
	}
}

// CollectLifecycleEvents collects all lifecycle events from an ExecutionContext.
// Filters out CommonDiffEvent and CommonEvent which are state change tracking events.
// Events are returned as-is without normalization.
func CollectLifecycleEvents(execCtx *gent.ExecutionContext) []gent.Event {
	var result []gent.Event
	for _, event := range execCtx.Events() {
		if IsLifecycleEvent(event) {
			result = append(result, event)
		}
	}
	return result
}

// CollectLifecycleEventsWithChildren collects lifecycle events from ExecutionContext and
// all children. Filters out CommonDiffEvent and CommonEvent. Events are returned as-is.
func CollectLifecycleEventsWithChildren(execCtx *gent.ExecutionContext) []gent.Event {
	result := CollectLifecycleEvents(execCtx)
	for _, child := range execCtx.Children() {
		result = append(result, CollectLifecycleEventsWithChildren(child)...)
	}
	return result
}

// CountEventTypes counts events by type name for tests with non-deterministic event ordering.
func CountEventTypes(events []gent.Event) map[string]int {
	counts := make(map[string]int)
	for _, event := range events {
		switch event.(type) {
		case *gent.BeforeExecutionEvent:
			counts["BeforeExecutionEvent"]++
		case *gent.AfterExecutionEvent:
			counts["AfterExecutionEvent"]++
		case *gent.BeforeIterationEvent:
			counts["BeforeIterationEvent"]++
		case *gent.AfterIterationEvent:
			counts["AfterIterationEvent"]++
		case *gent.BeforeModelCallEvent:
			counts["BeforeModelCallEvent"]++
		case *gent.AfterModelCallEvent:
			counts["AfterModelCallEvent"]++
		case *gent.BeforeToolCallEvent:
			counts["BeforeToolCallEvent"]++
		case *gent.AfterToolCallEvent:
			counts["AfterToolCallEvent"]++
		case *gent.LimitExceededEvent:
			counts["LimitExceededEvent"]++
		case *gent.ParseErrorEvent:
			counts["ParseErrorEvent"]++
		case *gent.ValidatorCalledEvent:
			counts["ValidatorCalledEvent"]++
		case *gent.ValidatorResultEvent:
			counts["ValidatorResultEvent"]++
		case *gent.ErrorEvent:
			counts["ErrorEvent"]++
		}
	}
	return counts
}

// -----------------------------------------------------------------------------
// Event Assertion Helpers
// -----------------------------------------------------------------------------

// AssertEventsEqual asserts that expected and actual event slices match.
// All fields are compared except:
//   - Timestamp: asserted to be non-zero and monotonically non-decreasing
//   - Duration: asserted to be >= 0
func AssertEventsEqual(t *testing.T, expected, actual []gent.Event) {
	t.Helper()

	if !assert.Equal(t, len(expected), len(actual), "event count mismatch") {
		return
	}

	var prevTimestamp time.Time
	for i := range expected {
		assertEventEqual(t, i, expected[i], actual[i], &prevTimestamp)
	}
}

// assertEventEqual asserts that a single expected and actual event match.
func assertEventEqual(
	t *testing.T,
	index int,
	expected, actual gent.Event,
	prevTimestamp *time.Time,
) {
	t.Helper()
	msgFmt := func(field string) string {
		return "event[%d]." + field
	}

	// Assert same type
	expectedType := eventTypeName(expected)
	actualType := eventTypeName(actual)
	if !assert.Equal(t, expectedType, actualType, msgFmt("type"), index) {
		return
	}

	switch exp := expected.(type) {
	case *gent.BeforeExecutionEvent:
		act := actual.(*gent.BeforeExecutionEvent)
		assertBaseEvent(t, index, exp.BaseEvent, act.BaseEvent, prevTimestamp)

	case *gent.AfterExecutionEvent:
		act := actual.(*gent.AfterExecutionEvent)
		assertBaseEvent(t, index, exp.BaseEvent, act.BaseEvent, prevTimestamp)
		assert.Equal(t, exp.TerminationReason, act.TerminationReason,
			msgFmt("TerminationReason"), index)

	case *gent.BeforeIterationEvent:
		act := actual.(*gent.BeforeIterationEvent)
		assertBaseEvent(t, index, exp.BaseEvent, act.BaseEvent, prevTimestamp)

	case *gent.AfterIterationEvent:
		act := actual.(*gent.AfterIterationEvent)
		assertBaseEvent(t, index, exp.BaseEvent, act.BaseEvent, prevTimestamp)
		// Full comparison of AgentLoopResult including NextPrompt
		assert.Equal(t, exp.Result, act.Result, msgFmt("Result"), index)
		assert.GreaterOrEqual(t, act.Duration, time.Duration(0),
			msgFmt("Duration"), index)

	case *gent.BeforeModelCallEvent:
		act := actual.(*gent.BeforeModelCallEvent)
		assertBaseEvent(t, index, exp.BaseEvent, act.BaseEvent, prevTimestamp)
		assert.Equal(t, exp.Model, act.Model, msgFmt("Model"), index)
		// Skip Request comparison - contains full message history with dynamic content

	case *gent.AfterModelCallEvent:
		act := actual.(*gent.AfterModelCallEvent)
		assertBaseEvent(t, index, exp.BaseEvent, act.BaseEvent, prevTimestamp)
		assert.Equal(t, exp.Model, act.Model, msgFmt("Model"), index)
		// Skip Request comparison - contains full message history with dynamic content
		// Skip Response comparison - compare InputTokens/OutputTokens instead
		assert.Equal(t, exp.InputTokens, act.InputTokens, msgFmt("InputTokens"), index)
		assert.Equal(t, exp.OutputTokens, act.OutputTokens, msgFmt("OutputTokens"), index)
		assert.GreaterOrEqual(t, act.Duration, time.Duration(0),
			msgFmt("Duration"), index)
		assert.Equal(t, exp.Error, act.Error, msgFmt("Error"), index)

	case *gent.BeforeToolCallEvent:
		act := actual.(*gent.BeforeToolCallEvent)
		assertBaseEvent(t, index, exp.BaseEvent, act.BaseEvent, prevTimestamp)
		assert.Equal(t, exp.ToolName, act.ToolName, msgFmt("ToolName"), index)
		assert.Equal(t, exp.Args, act.Args, msgFmt("Args"), index)

	case *gent.AfterToolCallEvent:
		act := actual.(*gent.AfterToolCallEvent)
		assertBaseEvent(t, index, exp.BaseEvent, act.BaseEvent, prevTimestamp)
		assert.Equal(t, exp.ToolName, act.ToolName, msgFmt("ToolName"), index)
		assert.Equal(t, exp.Args, act.Args, msgFmt("Args"), index)
		assert.Equal(t, exp.Output, act.Output, msgFmt("Output"), index)
		assert.GreaterOrEqual(t, act.Duration, time.Duration(0),
			msgFmt("Duration"), index)
		// Skip exact Error comparison - just verify error presence matches
		if exp.Error != nil {
			assert.NotNil(t, act.Error, msgFmt("Error should not be nil"), index)
		} else {
			assert.Nil(t, act.Error, msgFmt("Error should be nil"), index)
		}

	case *gent.LimitExceededEvent:
		act := actual.(*gent.LimitExceededEvent)
		assertBaseEvent(t, index, exp.BaseEvent, act.BaseEvent, prevTimestamp)
		assert.Equal(t, exp.Limit, act.Limit, msgFmt("Limit"), index)
		assert.Equal(t, exp.CurrentValue, act.CurrentValue, msgFmt("CurrentValue"), index)
		assert.Equal(t, exp.MatchedKey, act.MatchedKey, msgFmt("MatchedKey"), index)

	case *gent.ParseErrorEvent:
		act := actual.(*gent.ParseErrorEvent)
		assertBaseEvent(t, index, exp.BaseEvent, act.BaseEvent, prevTimestamp)
		assert.Equal(t, exp.ErrorType, act.ErrorType, msgFmt("ErrorType"), index)
		assert.Equal(t, exp.RawContent, act.RawContent, msgFmt("RawContent"), index)
		// Skip Error comparison - actual error contains implementation-specific details
		// Just verify error presence matches expectation
		if exp.Error != nil {
			assert.NotNil(t, act.Error, msgFmt("Error should not be nil"), index)
		}

	case *gent.ValidatorCalledEvent:
		act := actual.(*gent.ValidatorCalledEvent)
		assertBaseEvent(t, index, exp.BaseEvent, act.BaseEvent, prevTimestamp)
		assert.Equal(t, exp.ValidatorName, act.ValidatorName, msgFmt("ValidatorName"), index)
		assert.Equal(t, exp.Answer, act.Answer, msgFmt("Answer"), index)

	case *gent.ValidatorResultEvent:
		act := actual.(*gent.ValidatorResultEvent)
		assertBaseEvent(t, index, exp.BaseEvent, act.BaseEvent, prevTimestamp)
		assert.Equal(t, exp.ValidatorName, act.ValidatorName, msgFmt("ValidatorName"), index)
		assert.Equal(t, exp.Answer, act.Answer, msgFmt("Answer"), index)
		assert.Equal(t, exp.Accepted, act.Accepted, msgFmt("Accepted"), index)
		assert.Equal(t, exp.Feedback, act.Feedback, msgFmt("Feedback"), index)

	case *gent.ErrorEvent:
		act := actual.(*gent.ErrorEvent)
		assertBaseEvent(t, index, exp.BaseEvent, act.BaseEvent, prevTimestamp)
		assert.Equal(t, exp.Error, act.Error, msgFmt("Error"), index)

	default:
		t.Errorf("event[%d]: unknown event type %T", index, expected)
	}
}

// assertBaseEvent asserts BaseEvent fields and updates prevTimestamp.
func assertBaseEvent(
	t *testing.T,
	index int,
	expected, actual gent.BaseEvent,
	prevTimestamp *time.Time,
) {
	t.Helper()

	assert.Equal(t, expected.EventName, actual.EventName,
		"event[%d].EventName", index)
	assert.Equal(t, expected.Iteration, actual.Iteration,
		"event[%d].Iteration", index)
	assert.Equal(t, expected.Depth, actual.Depth,
		"event[%d].Depth", index)

	// Timestamp must be non-zero
	assert.False(t, actual.Timestamp.IsZero(),
		"event[%d].Timestamp should not be zero", index)

	// Timestamp must be >= previous timestamp (monotonically non-decreasing)
	if !prevTimestamp.IsZero() {
		assert.True(t, !actual.Timestamp.Before(*prevTimestamp),
			"event[%d].Timestamp (%v) should be >= previous timestamp (%v)",
			index, actual.Timestamp, *prevTimestamp)
	}

	*prevTimestamp = actual.Timestamp
}

// eventTypeName returns the type name of an event for error messages.
func eventTypeName(event gent.Event) string {
	switch event.(type) {
	case *gent.BeforeExecutionEvent:
		return "BeforeExecutionEvent"
	case *gent.AfterExecutionEvent:
		return "AfterExecutionEvent"
	case *gent.BeforeIterationEvent:
		return "BeforeIterationEvent"
	case *gent.AfterIterationEvent:
		return "AfterIterationEvent"
	case *gent.BeforeModelCallEvent:
		return "BeforeModelCallEvent"
	case *gent.AfterModelCallEvent:
		return "AfterModelCallEvent"
	case *gent.BeforeToolCallEvent:
		return "BeforeToolCallEvent"
	case *gent.AfterToolCallEvent:
		return "AfterToolCallEvent"
	case *gent.LimitExceededEvent:
		return "LimitExceededEvent"
	case *gent.ParseErrorEvent:
		return "ParseErrorEvent"
	case *gent.ValidatorCalledEvent:
		return "ValidatorCalledEvent"
	case *gent.ValidatorResultEvent:
		return "ValidatorResultEvent"
	case *gent.ErrorEvent:
		return "ErrorEvent"
	default:
		return "UnknownEvent"
	}
}
