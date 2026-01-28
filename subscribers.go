package gent

// Subscriber interfaces define type-safe event subscriptions.
//
// Implement any combination of these interfaces on a single struct to receive
// multiple event types. The events/Registry will automatically detect which
// interfaces your struct implements and call the appropriate methods.
//
// # Example
//
//	type LoggingSubscriber struct {
//	    logger *log.Logger
//	}
//
//	// Implement multiple subscriber interfaces
//	func (s *LoggingSubscriber) OnBeforeIteration(
//	    execCtx *ExecutionContext,
//	    event *BeforeIterationEvent,
//	) {
//	    s.logger.Printf("Starting iteration %d", event.Iteration)
//	}
//
//	func (s *LoggingSubscriber) OnAfterModelCall(
//	    execCtx *ExecutionContext,
//	    event *AfterModelCallEvent,
//	) {
//	    s.logger.Printf("Model %s used %d tokens", event.Model, event.InputTokens)
//	}
//
//	// Register with the event registry
//	registry := events.NewRegistry()
//	registry.Subscribe(&LoggingSubscriber{logger: myLogger})

// BeforeExecutionSubscriber receives BeforeExecutionEvent events.
type BeforeExecutionSubscriber interface {
	OnBeforeExecution(execCtx *ExecutionContext, event *BeforeExecutionEvent)
}

// AfterExecutionSubscriber receives AfterExecutionEvent events.
type AfterExecutionSubscriber interface {
	OnAfterExecution(execCtx *ExecutionContext, event *AfterExecutionEvent)
}

// BeforeIterationSubscriber receives BeforeIterationEvent events.
type BeforeIterationSubscriber interface {
	OnBeforeIteration(execCtx *ExecutionContext, event *BeforeIterationEvent)
}

// AfterIterationSubscriber receives AfterIterationEvent events.
type AfterIterationSubscriber interface {
	OnAfterIteration(execCtx *ExecutionContext, event *AfterIterationEvent)
}

// BeforeModelCallSubscriber receives BeforeModelCallEvent events.
type BeforeModelCallSubscriber interface {
	OnBeforeModelCall(execCtx *ExecutionContext, event *BeforeModelCallEvent)
}

// AfterModelCallSubscriber receives AfterModelCallEvent events.
type AfterModelCallSubscriber interface {
	OnAfterModelCall(execCtx *ExecutionContext, event *AfterModelCallEvent)
}

// BeforeToolCallSubscriber receives BeforeToolCallEvent events.
type BeforeToolCallSubscriber interface {
	OnBeforeToolCall(execCtx *ExecutionContext, event *BeforeToolCallEvent)
}

// AfterToolCallSubscriber receives AfterToolCallEvent events.
type AfterToolCallSubscriber interface {
	OnAfterToolCall(execCtx *ExecutionContext, event *AfterToolCallEvent)
}

// ParseErrorSubscriber receives ParseErrorEvent events.
type ParseErrorSubscriber interface {
	OnParseError(execCtx *ExecutionContext, event *ParseErrorEvent)
}

// ValidatorCalledSubscriber receives ValidatorCalledEvent events.
type ValidatorCalledSubscriber interface {
	OnValidatorCalled(execCtx *ExecutionContext, event *ValidatorCalledEvent)
}

// ValidatorResultSubscriber receives ValidatorResultEvent events.
type ValidatorResultSubscriber interface {
	OnValidatorResult(execCtx *ExecutionContext, event *ValidatorResultEvent)
}

// ErrorSubscriber receives ErrorEvent events.
type ErrorSubscriber interface {
	OnError(execCtx *ExecutionContext, event *ErrorEvent)
}

// CommonEventSubscriber receives CommonEvent events.
// This is useful for receiving all user-defined events.
type CommonEventSubscriber interface {
	OnCommonEvent(execCtx *ExecutionContext, event *CommonEvent)
}

// CommonDiffEventSubscriber receives CommonDiffEvent events.
// This is useful for receiving state change events with automatic diff generation.
type CommonDiffEventSubscriber interface {
	OnCommonDiffEvent(execCtx *ExecutionContext, event *CommonDiffEvent)
}
