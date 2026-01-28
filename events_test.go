package gent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// -----------------------------------------------------------------------------
// Event Type Tests
// -----------------------------------------------------------------------------

func TestEvent_MarkerInterface(t *testing.T) {
	// All event types should implement the Event interface
	events := []Event{
		&BeforeExecutionEvent{},
		&AfterExecutionEvent{},
		&BeforeIterationEvent{},
		&AfterIterationEvent{},
		&BeforeModelCallEvent{},
		&AfterModelCallEvent{},
		&BeforeToolCallEvent{},
		&AfterToolCallEvent{},
		&ParseErrorEvent{},
		&ValidatorCalledEvent{},
		&ValidatorResultEvent{},
		&ErrorEvent{},
		&CommonEvent{},
	}

	for _, e := range events {
		// Just verify they implement the interface (compile-time check enforced by slice type)
		assert.NotNil(t, e)
	}
}

// -----------------------------------------------------------------------------
// PublishXXX Method Tests - EventName Verification
// -----------------------------------------------------------------------------

func TestPublishBeforeExecution_SetsCorrectEventName(t *testing.T) {
	execCtx := NewExecutionContext(context.Background(), "test", nil)

	event := execCtx.PublishBeforeExecution()

	assert.Equal(t, EventNameExecutionBefore, event.EventName)
	assert.NotZero(t, event.Timestamp)
	assert.Equal(t, 0, event.Iteration)
	assert.Equal(t, 0, event.Depth)
}

func TestPublishAfterExecution_SetsCorrectEventName(t *testing.T) {
	execCtx := NewExecutionContext(context.Background(), "test", nil)

	event := execCtx.PublishAfterExecution(TerminationSuccess, nil)

	assert.Equal(t, EventNameExecutionAfter, event.EventName)
	assert.Equal(t, TerminationSuccess, event.TerminationReason)
	assert.Nil(t, event.Error)
}

func TestPublishBeforeIteration_SetsCorrectEventName(t *testing.T) {
	execCtx := NewExecutionContext(context.Background(), "test", nil)
	execCtx.IncrementIteration()

	event := execCtx.PublishBeforeIteration()

	assert.Equal(t, EventNameIterationBefore, event.EventName)
	assert.Equal(t, 1, event.Iteration)
}

func TestPublishAfterIteration_SetsCorrectEventName(t *testing.T) {
	execCtx := NewExecutionContext(context.Background(), "test", nil)
	execCtx.IncrementIteration()
	result := &AgentLoopResult{Action: LAContinue}

	event := execCtx.PublishAfterIteration(result, 100*time.Millisecond)

	assert.Equal(t, EventNameIterationAfter, event.EventName)
	assert.Equal(t, result, event.Result)
	assert.Equal(t, 100*time.Millisecond, event.Duration)
}

func TestPublishBeforeModelCall_SetsCorrectEventName(t *testing.T) {
	execCtx := NewExecutionContext(context.Background(), "test", nil)

	event := execCtx.PublishBeforeModelCall("gpt-4", nil)

	assert.Equal(t, EventNameModelCallBefore, event.EventName)
	assert.Equal(t, "gpt-4", event.Model)
}

func TestPublishAfterModelCall_SetsCorrectEventName(t *testing.T) {
	execCtx := NewExecutionContext(context.Background(), "test", nil)
	response := &ContentResponse{
		Info: &GenerationInfo{
			InputTokens:  100,
			OutputTokens: 50,
		},
	}

	event := execCtx.PublishAfterModelCall("gpt-4", nil, response, 500*time.Millisecond, nil)

	assert.Equal(t, EventNameModelCallAfter, event.EventName)
	assert.Equal(t, "gpt-4", event.Model)
	assert.Equal(t, 100, event.InputTokens)
	assert.Equal(t, 50, event.OutputTokens)
	assert.Equal(t, 500*time.Millisecond, event.Duration)
}

func TestPublishBeforeToolCall_SetsCorrectEventName(t *testing.T) {
	execCtx := NewExecutionContext(context.Background(), "test", nil)
	args := map[string]any{"query": "test"}

	event := execCtx.PublishBeforeToolCall("search", args)

	assert.Equal(t, EventNameToolCallBefore, event.EventName)
	assert.Equal(t, "search", event.ToolName)
	assert.Equal(t, args, event.Args)
}

func TestPublishAfterToolCall_SetsCorrectEventName(t *testing.T) {
	execCtx := NewExecutionContext(context.Background(), "test", nil)
	args := map[string]any{"query": "test"}
	output := "search results"

	event := execCtx.PublishAfterToolCall("search", args, output, 200*time.Millisecond, nil)

	assert.Equal(t, EventNameToolCallAfter, event.EventName)
	assert.Equal(t, "search", event.ToolName)
	assert.Equal(t, args, event.Args)
	assert.Equal(t, output, event.Output)
	assert.Equal(t, 200*time.Millisecond, event.Duration)
}

func TestPublishParseError_SetsCorrectEventName(t *testing.T) {
	execCtx := NewExecutionContext(context.Background(), "test", nil)
	execCtx.IncrementIteration()

	event := execCtx.PublishParseError(ParseErrorTypeFormat, "invalid content", assert.AnError)

	assert.Equal(t, EventNameParseError, event.EventName)
	assert.Equal(t, ParseErrorTypeFormat, event.ErrorType)
	assert.Equal(t, "invalid content", event.RawContent)
	assert.Equal(t, assert.AnError, event.Error)
}

func TestPublishValidatorCalled_SetsCorrectEventName(t *testing.T) {
	execCtx := NewExecutionContext(context.Background(), "test", nil)

	event := execCtx.PublishValidatorCalled("my_validator", "answer text")

	assert.Equal(t, EventNameValidatorCalled, event.EventName)
	assert.Equal(t, "my_validator", event.ValidatorName)
	assert.Equal(t, "answer text", event.Answer)
}

func TestPublishValidatorResult_SetsCorrectEventName(t *testing.T) {
	execCtx := NewExecutionContext(context.Background(), "test", nil)
	feedback := []FormattedSection{{Name: "error", Content: "too short"}}

	event := execCtx.PublishValidatorResult("my_validator", "answer", false, feedback)

	assert.Equal(t, EventNameValidatorResult, event.EventName)
	assert.Equal(t, "my_validator", event.ValidatorName)
	assert.Equal(t, "answer", event.Answer)
	assert.False(t, event.Accepted)
	assert.Equal(t, feedback, event.Feedback)
}

func TestPublishError_SetsCorrectEventName(t *testing.T) {
	execCtx := NewExecutionContext(context.Background(), "test", nil)

	event := execCtx.PublishError(assert.AnError)

	assert.Equal(t, EventNameError, event.EventName)
	assert.Equal(t, assert.AnError, event.Error)
}

func TestPublishCommonEvent_SetsCustomEventName(t *testing.T) {
	execCtx := NewExecutionContext(context.Background(), "test", nil)
	data := map[string]any{"key": "value"}

	event := execCtx.PublishCommonEvent("myapp:custom_event", "Something happened", data)

	assert.Equal(t, "myapp:custom_event", event.EventName)
	assert.Equal(t, "Something happened", event.Description)
	assert.Equal(t, data, event.Data)
}

// -----------------------------------------------------------------------------
// BaseEvent Population Tests
// -----------------------------------------------------------------------------

func TestBaseEvent_TimestampPopulated(t *testing.T) {
	execCtx := NewExecutionContext(context.Background(), "test", nil)
	before := time.Now()

	event := execCtx.PublishBeforeExecution()

	after := time.Now()
	assert.True(t, event.Timestamp.After(before) || event.Timestamp.Equal(before))
	assert.True(t, event.Timestamp.Before(after) || event.Timestamp.Equal(after))
}

func TestBaseEvent_IterationPopulated(t *testing.T) {
	execCtx := NewExecutionContext(context.Background(), "test", nil)

	// Before first iteration
	event1 := execCtx.PublishBeforeExecution()
	assert.Equal(t, 0, event1.Iteration)

	// After incrementing iteration
	execCtx.IncrementIteration()
	event2 := execCtx.PublishBeforeIteration()
	assert.Equal(t, 1, event2.Iteration)

	execCtx.IncrementIteration()
	event3 := execCtx.PublishBeforeIteration()
	assert.Equal(t, 2, event3.Iteration)
}

func TestBaseEvent_DepthPopulated(t *testing.T) {
	execCtx := NewExecutionContext(context.Background(), "root", nil)

	// Root context has depth 0
	event1 := execCtx.PublishBeforeExecution()
	assert.Equal(t, 0, event1.Depth)

	// Child context has depth 1
	child := execCtx.SpawnChild("child", nil)
	event2 := child.PublishBeforeExecution()
	assert.Equal(t, 1, event2.Depth)

	// Grandchild context has depth 2
	grandchild := child.SpawnChild("grandchild", nil)
	event3 := grandchild.PublishBeforeExecution()
	assert.Equal(t, 2, event3.Depth)
}

// -----------------------------------------------------------------------------
// Events() Collection Tests
// -----------------------------------------------------------------------------

func TestEvents_RecordsAllPublishedEvents(t *testing.T) {
	execCtx := NewExecutionContext(context.Background(), "test", nil)

	execCtx.PublishBeforeExecution()
	execCtx.IncrementIteration()
	execCtx.PublishBeforeIteration()
	execCtx.PublishBeforeModelCall("gpt-4", nil)
	execCtx.PublishAfterModelCall("gpt-4", nil, nil, 0, nil)
	execCtx.PublishAfterIteration(nil, 0)
	execCtx.PublishAfterExecution(TerminationSuccess, nil)

	events := execCtx.Events()
	assert.Len(t, events, 6)

	// Verify event types in order
	assert.IsType(t, &BeforeExecutionEvent{}, events[0])
	assert.IsType(t, &BeforeIterationEvent{}, events[1])
	assert.IsType(t, &BeforeModelCallEvent{}, events[2])
	assert.IsType(t, &AfterModelCallEvent{}, events[3])
	assert.IsType(t, &AfterIterationEvent{}, events[4])
	assert.IsType(t, &AfterExecutionEvent{}, events[5])
}

func TestEvents_ReturnsCopy(t *testing.T) {
	execCtx := NewExecutionContext(context.Background(), "test", nil)
	execCtx.PublishBeforeExecution()

	events1 := execCtx.Events()
	events2 := execCtx.Events()

	// Modifying one should not affect the other
	events1[0] = nil
	assert.NotNil(t, events2[0])
}

// -----------------------------------------------------------------------------
// Recursion Limit Tests
// -----------------------------------------------------------------------------

// recursiveSubscriber publishes another event when it receives one, causing recursion.
type recursiveSubscriber struct {
	callCount int
	maxCalls  int
}

func (s *recursiveSubscriber) OnBeforeIteration(execCtx *ExecutionContext, _ *BeforeIterationEvent) {
	s.callCount++
	if s.callCount < s.maxCalls {
		// Publish another event, causing recursion
		execCtx.PublishBeforeIteration()
	}
}

// recursiveRegistry implements EventPublisher and dispatches to a recursive subscriber.
type recursiveRegistry struct {
	subscriber   *recursiveSubscriber
	maxRecursion int
}

func (r *recursiveRegistry) Dispatch(execCtx *ExecutionContext, event Event) {
	if e, ok := event.(*BeforeIterationEvent); ok && r.subscriber != nil {
		r.subscriber.OnBeforeIteration(execCtx, e)
	}
}

func (r *recursiveRegistry) MaxRecursion() int {
	return r.maxRecursion
}

func TestPublish_PanicsOnRecursionLimitExceeded(t *testing.T) {
	type input struct {
		maxRecursion   int
		subscriberMax  int // how many times subscriber will try to recurse
	}

	type expected struct {
		shouldPanic bool
		callCount   int // expected call count before panic (or final if no panic)
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "panics when recursion exceeds limit of 3",
			input: input{
				maxRecursion:  3,
				subscriberMax: 10, // tries to recurse 10 times
			},
			expected: expected{
				shouldPanic: true,
				callCount:   3, // panics on 4th call (depth 0,1,2 succeed, depth 3 panics)
			},
		},
		{
			name: "panics when recursion exceeds limit of 1",
			input: input{
				maxRecursion:  1,
				subscriberMax: 5,
			},
			expected: expected{
				shouldPanic: true,
				callCount:   1, // panics on 2nd call
			},
		},
		{
			name: "does not panic when recursion stays within limit",
			input: input{
				maxRecursion:  5,
				subscriberMax: 3, // only recurses 3 times, within limit
			},
			expected: expected{
				shouldPanic: false,
				callCount:   3,
			},
		},
		{
			name: "does not panic with no recursion",
			input: input{
				maxRecursion:  10,
				subscriberMax: 1, // only one call, no recursion
			},
			expected: expected{
				shouldPanic: false,
				callCount:   1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subscriber := &recursiveSubscriber{maxCalls: tt.input.subscriberMax}
			registry := &recursiveRegistry{
				subscriber:   subscriber,
				maxRecursion: tt.input.maxRecursion,
			}

			execCtx := NewExecutionContext(context.Background(), "test", nil)
			execCtx.SetEventPublisher(registry)

			if tt.expected.shouldPanic {
				assert.Panics(t, func() {
					execCtx.PublishBeforeIteration()
				}, "expected panic when recursion limit exceeded")
			} else {
				assert.NotPanics(t, func() {
					execCtx.PublishBeforeIteration()
				}, "should not panic when within recursion limit")
			}

			assert.Equal(t, tt.expected.callCount, subscriber.callCount,
				"subscriber call count mismatch")
		})
	}
}

// -----------------------------------------------------------------------------
// Concurrency Tests
// -----------------------------------------------------------------------------

func TestPublish_ConcurrentSafety(t *testing.T) {
	type input struct {
		goroutines     int
		eventsPerGo    int
	}

	type expected struct {
		totalEvents int
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "10 goroutines publishing 100 events each",
			input: input{
				goroutines:  10,
				eventsPerGo: 100,
			},
			expected: expected{
				totalEvents: 1000,
			},
		},
		{
			name: "50 goroutines publishing 20 events each",
			input: input{
				goroutines:  50,
				eventsPerGo: 20,
			},
			expected: expected{
				totalEvents: 1000,
			},
		},
		{
			name: "100 goroutines publishing 10 events each",
			input: input{
				goroutines:  100,
				eventsPerGo: 10,
			},
			expected: expected{
				totalEvents: 1000,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			execCtx := NewExecutionContext(context.Background(), "test", nil)

			// Use a channel to synchronize start
			start := make(chan struct{})
			done := make(chan struct{}, tt.input.goroutines)

			// Launch goroutines
			for i := 0; i < tt.input.goroutines; i++ {
				go func(id int) {
					<-start // Wait for signal to start
					for j := 0; j < tt.input.eventsPerGo; j++ {
						execCtx.PublishCommonEvent(
							"test:concurrent",
							"concurrent event",
							map[string]int{"goroutine": id, "event": j},
						)
					}
					done <- struct{}{}
				}(i)
			}

			// Start all goroutines simultaneously
			close(start)

			// Wait for all to complete
			for i := 0; i < tt.input.goroutines; i++ {
				<-done
			}

			// Verify all events were recorded
			events := execCtx.Events()
			assert.Len(t, events, tt.expected.totalEvents,
				"all events should be recorded without data races")

			// Verify all events are CommonEvent type
			for i, event := range events {
				_, ok := event.(*CommonEvent)
				assert.True(t, ok, "event %d should be CommonEvent", i)
			}
		})
	}
}

func TestPublish_ConcurrentWithDifferentEventTypes(t *testing.T) {
	execCtx := NewExecutionContext(context.Background(), "test", nil)

	start := make(chan struct{})
	done := make(chan struct{}, 4)

	// Goroutine 1: Publish BeforeIteration events
	go func() {
		<-start
		for i := 0; i < 50; i++ {
			execCtx.PublishBeforeIteration()
		}
		done <- struct{}{}
	}()

	// Goroutine 2: Publish AfterModelCall events
	go func() {
		<-start
		for i := 0; i < 50; i++ {
			execCtx.PublishAfterModelCall("model", nil, nil, 0, nil)
		}
		done <- struct{}{}
	}()

	// Goroutine 3: Publish BeforeToolCall events
	go func() {
		<-start
		for i := 0; i < 50; i++ {
			execCtx.PublishBeforeToolCall("tool", nil)
		}
		done <- struct{}{}
	}()

	// Goroutine 4: Publish CommonEvent events
	go func() {
		<-start
		for i := 0; i < 50; i++ {
			execCtx.PublishCommonEvent("test:event", "description", nil)
		}
		done <- struct{}{}
	}()

	// Start all goroutines
	close(start)

	// Wait for completion
	for i := 0; i < 4; i++ {
		<-done
	}

	// Verify total count
	events := execCtx.Events()
	assert.Len(t, events, 200, "all 200 events should be recorded")

	// Count by type
	var beforeIter, afterModel, beforeTool, common int
	for _, event := range events {
		switch event.(type) {
		case *BeforeIterationEvent:
			beforeIter++
		case *AfterModelCallEvent:
			afterModel++
		case *BeforeToolCallEvent:
			beforeTool++
		case *CommonEvent:
			common++
		}
	}

	assert.Equal(t, 50, beforeIter, "should have 50 BeforeIterationEvent")
	assert.Equal(t, 50, afterModel, "should have 50 AfterModelCallEvent")
	assert.Equal(t, 50, beforeTool, "should have 50 BeforeToolCallEvent")
	assert.Equal(t, 50, common, "should have 50 CommonEvent")
}
