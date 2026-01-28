package events

import (
	"context"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

// -----------------------------------------------------------------------------
// Test Subscribers
// -----------------------------------------------------------------------------

type mockBeforeExecutionSubscriber struct {
	called bool
	event  *gent.BeforeExecutionEvent
}

func (s *mockBeforeExecutionSubscriber) OnBeforeExecution(
	_ *gent.ExecutionContext,
	e *gent.BeforeExecutionEvent,
) {
	s.called = true
	s.event = e
}

type mockAfterExecutionSubscriber struct {
	called bool
	event  *gent.AfterExecutionEvent
}

func (s *mockAfterExecutionSubscriber) OnAfterExecution(
	_ *gent.ExecutionContext,
	e *gent.AfterExecutionEvent,
) {
	s.called = true
	s.event = e
}

type mockBeforeIterationSubscriber struct {
	called bool
	event  *gent.BeforeIterationEvent
}

func (s *mockBeforeIterationSubscriber) OnBeforeIteration(
	_ *gent.ExecutionContext,
	e *gent.BeforeIterationEvent,
) {
	s.called = true
	s.event = e
}

type mockAfterModelCallSubscriber struct {
	called bool
	event  *gent.AfterModelCallEvent
}

func (s *mockAfterModelCallSubscriber) OnAfterModelCall(
	_ *gent.ExecutionContext,
	e *gent.AfterModelCallEvent,
) {
	s.called = true
	s.event = e
}

type mockBeforeToolCallSubscriber struct {
	called bool
	event  *gent.BeforeToolCallEvent
}

func (s *mockBeforeToolCallSubscriber) OnBeforeToolCall(
	_ *gent.ExecutionContext,
	e *gent.BeforeToolCallEvent,
) {
	s.called = true
	s.event = e
}

type mockParseErrorSubscriber struct {
	called bool
	event  *gent.ParseErrorEvent
}

func (s *mockParseErrorSubscriber) OnParseError(
	_ *gent.ExecutionContext,
	e *gent.ParseErrorEvent,
) {
	s.called = true
	s.event = e
}

type mockCommonEventSubscriber struct {
	called bool
	event  *gent.CommonEvent
}

func (s *mockCommonEventSubscriber) OnCommonEvent(
	_ *gent.ExecutionContext,
	e *gent.CommonEvent,
) {
	s.called = true
	s.event = e
}

// multiSubscriber implements multiple interfaces
type multiSubscriber struct {
	beforeExecCalled   bool
	afterExecCalled    bool
	beforeIterCalled   bool
	afterModelCalled   bool
}

func (s *multiSubscriber) OnBeforeExecution(_ *gent.ExecutionContext, _ *gent.BeforeExecutionEvent) {
	s.beforeExecCalled = true
}

func (s *multiSubscriber) OnAfterExecution(_ *gent.ExecutionContext, _ *gent.AfterExecutionEvent) {
	s.afterExecCalled = true
}

func (s *multiSubscriber) OnBeforeIteration(_ *gent.ExecutionContext, _ *gent.BeforeIterationEvent) {
	s.beforeIterCalled = true
}

func (s *multiSubscriber) OnAfterModelCall(_ *gent.ExecutionContext, _ *gent.AfterModelCallEvent) {
	s.afterModelCalled = true
}

// -----------------------------------------------------------------------------
// Registry Tests
// -----------------------------------------------------------------------------

func TestNewRegistry_ReturnsEmptyRegistry(t *testing.T) {
	registry := NewRegistry()

	assert.NotNil(t, registry)
	assert.Equal(t, 0, registry.Len())
	assert.Equal(t, DefaultMaxRecursion, registry.MaxRecursion())
}

func TestRegistry_Subscribe_AddsSubscriber(t *testing.T) {
	registry := NewRegistry()
	sub := &mockBeforeExecutionSubscriber{}

	result := registry.Subscribe(sub)

	assert.Equal(t, registry, result, "Subscribe should return registry for chaining")
	assert.Equal(t, 1, registry.Len())
}

func TestRegistry_Subscribe_ChainMultiple(t *testing.T) {
	registry := NewRegistry()
	sub1 := &mockBeforeExecutionSubscriber{}
	sub2 := &mockAfterExecutionSubscriber{}

	registry.Subscribe(sub1).Subscribe(sub2)

	assert.Equal(t, 2, registry.Len())
}

func TestRegistry_SetMaxRecursion(t *testing.T) {
	registry := NewRegistry()

	result := registry.SetMaxRecursion(5)

	assert.Equal(t, registry, result, "SetMaxRecursion should return registry for chaining")
	assert.Equal(t, 5, registry.MaxRecursion())
}

func TestRegistry_Clear_RemovesAllSubscribers(t *testing.T) {
	registry := NewRegistry()
	registry.Subscribe(&mockBeforeExecutionSubscriber{})
	registry.Subscribe(&mockAfterExecutionSubscriber{})

	registry.Clear()

	assert.Equal(t, 0, registry.Len())
}

// -----------------------------------------------------------------------------
// Dispatch Tests
// -----------------------------------------------------------------------------

func TestRegistry_Dispatch_BeforeExecutionEvent(t *testing.T) {
	registry := NewRegistry()
	sub := &mockBeforeExecutionSubscriber{}
	registry.Subscribe(sub)

	execCtx := gent.NewExecutionContext(context.Background(), "test", nil)
	event := &gent.BeforeExecutionEvent{}

	registry.Dispatch(execCtx, event)

	assert.True(t, sub.called)
	assert.Equal(t, event, sub.event)
}

func TestRegistry_Dispatch_AfterExecutionEvent(t *testing.T) {
	registry := NewRegistry()
	sub := &mockAfterExecutionSubscriber{}
	registry.Subscribe(sub)

	execCtx := gent.NewExecutionContext(context.Background(), "test", nil)
	event := &gent.AfterExecutionEvent{
		TerminationReason: gent.TerminationSuccess,
	}

	registry.Dispatch(execCtx, event)

	assert.True(t, sub.called)
	assert.Equal(t, event, sub.event)
}

func TestRegistry_Dispatch_BeforeIterationEvent(t *testing.T) {
	registry := NewRegistry()
	sub := &mockBeforeIterationSubscriber{}
	registry.Subscribe(sub)

	execCtx := gent.NewExecutionContext(context.Background(), "test", nil)
	event := &gent.BeforeIterationEvent{}

	registry.Dispatch(execCtx, event)

	assert.True(t, sub.called)
	assert.Equal(t, event, sub.event)
}

func TestRegistry_Dispatch_AfterModelCallEvent(t *testing.T) {
	registry := NewRegistry()
	sub := &mockAfterModelCallSubscriber{}
	registry.Subscribe(sub)

	execCtx := gent.NewExecutionContext(context.Background(), "test", nil)
	event := &gent.AfterModelCallEvent{
		Model:        "gpt-4",
		InputTokens:  100,
		OutputTokens: 50,
	}

	registry.Dispatch(execCtx, event)

	assert.True(t, sub.called)
	assert.Equal(t, event, sub.event)
}

func TestRegistry_Dispatch_BeforeToolCallEvent(t *testing.T) {
	registry := NewRegistry()
	sub := &mockBeforeToolCallSubscriber{}
	registry.Subscribe(sub)

	execCtx := gent.NewExecutionContext(context.Background(), "test", nil)
	event := &gent.BeforeToolCallEvent{
		ToolName: "search",
		Args:     map[string]any{"query": "test"},
	}

	registry.Dispatch(execCtx, event)

	assert.True(t, sub.called)
	assert.Equal(t, event, sub.event)
}

func TestRegistry_Dispatch_ParseErrorEvent(t *testing.T) {
	registry := NewRegistry()
	sub := &mockParseErrorSubscriber{}
	registry.Subscribe(sub)

	execCtx := gent.NewExecutionContext(context.Background(), "test", nil)
	event := &gent.ParseErrorEvent{
		ErrorType:  "format",
		RawContent: "invalid",
	}

	registry.Dispatch(execCtx, event)

	assert.True(t, sub.called)
	assert.Equal(t, event, sub.event)
}

func TestRegistry_Dispatch_CommonEvent(t *testing.T) {
	registry := NewRegistry()
	sub := &mockCommonEventSubscriber{}
	registry.Subscribe(sub)

	execCtx := gent.NewExecutionContext(context.Background(), "test", nil)
	event := &gent.CommonEvent{
		Description: "test event",
		Data:        "test data",
	}

	registry.Dispatch(execCtx, event)

	assert.True(t, sub.called)
	assert.Equal(t, event, sub.event)
}

func TestRegistry_Dispatch_OnlyCallsMatchingSubscribers(t *testing.T) {
	registry := NewRegistry()
	beforeExecSub := &mockBeforeExecutionSubscriber{}
	afterExecSub := &mockAfterExecutionSubscriber{}
	registry.Subscribe(beforeExecSub)
	registry.Subscribe(afterExecSub)

	execCtx := gent.NewExecutionContext(context.Background(), "test", nil)
	event := &gent.BeforeExecutionEvent{}

	registry.Dispatch(execCtx, event)

	assert.True(t, beforeExecSub.called, "matching subscriber should be called")
	assert.False(t, afterExecSub.called, "non-matching subscriber should not be called")
}

func TestRegistry_Dispatch_CallsInOrder(t *testing.T) {
	registry := NewRegistry()
	var order []int

	sub1 := &orderTrackingSubscriber{order: &order, id: 1}
	sub2 := &orderTrackingSubscriber{order: &order, id: 2}
	sub3 := &orderTrackingSubscriber{order: &order, id: 3}

	registry.Subscribe(sub1).Subscribe(sub2).Subscribe(sub3)

	execCtx := gent.NewExecutionContext(context.Background(), "test", nil)
	registry.Dispatch(execCtx, &gent.BeforeExecutionEvent{})

	assert.Equal(t, []int{1, 2, 3}, order)
}

type orderTrackingSubscriber struct {
	order *[]int
	id    int
}

func (s *orderTrackingSubscriber) OnBeforeExecution(_ *gent.ExecutionContext, _ *gent.BeforeExecutionEvent) {
	*s.order = append(*s.order, s.id)
}

func TestRegistry_Dispatch_MultiInterfaceSubscriber(t *testing.T) {
	registry := NewRegistry()
	sub := &multiSubscriber{}
	registry.Subscribe(sub)

	execCtx := gent.NewExecutionContext(context.Background(), "test", nil)

	// Dispatch different events
	registry.Dispatch(execCtx, &gent.BeforeExecutionEvent{})
	registry.Dispatch(execCtx, &gent.AfterExecutionEvent{})
	registry.Dispatch(execCtx, &gent.BeforeIterationEvent{})
	registry.Dispatch(execCtx, &gent.AfterModelCallEvent{})

	assert.True(t, sub.beforeExecCalled)
	assert.True(t, sub.afterExecCalled)
	assert.True(t, sub.beforeIterCalled)
	assert.True(t, sub.afterModelCalled)
}

func TestRegistry_Dispatch_UnknownEventType_DoesNotPanic(t *testing.T) {
	registry := NewRegistry()
	registry.Subscribe(&mockBeforeExecutionSubscriber{})

	execCtx := gent.NewExecutionContext(context.Background(), "test", nil)

	// Dispatch an event type not handled by any case - should not panic
	assert.NotPanics(t, func() {
		registry.Dispatch(execCtx, &customUnhandledEvent{})
	})
}

type customUnhandledEvent struct {
	gent.BaseEvent
}
