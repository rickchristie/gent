package events

import (
	"github.com/rickchristie/gent"
)

// Registry manages event subscribers and dispatches events to them.
//
// # Overview
//
// Registry is the central coordination point for event subscribers. It:
//   - Stores registered subscribers in order
//   - Dispatches events to subscribers that implement the relevant interface
//   - Provides configuration for recursion limits
//
// Subscribers can implement any combination of subscriber interfaces - they only
// receive events for the interfaces they implement.
//
// # Creating and Using
//
//	// Create a registry and register subscribers
//	registry := events.NewRegistry()
//	registry.Subscribe(&LoggingSubscriber{})
//	registry.Subscribe(&MetricsSubscriber{})
//
//	// Use with executor
//	exec := executor.New(loop, executor.Config{Events: registry})
//
// # Subscribers with Multiple Interfaces
//
// A single subscriber can implement multiple interfaces:
//
//	type FullSubscriber struct {
//	    logger *log.Logger
//	}
//
//	func (s *FullSubscriber) OnBeforeExecution(
//	    execCtx *gent.ExecutionContext,
//	    e *gent.BeforeExecutionEvent,
//	) {
//	    s.logger.Print("Execution started")
//	}
//
//	func (s *FullSubscriber) OnAfterToolCall(
//	    execCtx *gent.ExecutionContext,
//	    e *gent.AfterToolCallEvent,
//	) {
//	    s.logger.Printf("Tool %s: %v", e.ToolName, e.Duration)
//	}
//
//	// Register once - receives both event types
//	registry.Subscribe(&FullSubscriber{logger: log.Default()})
//
// # Thread Safety
//
// Registry is NOT thread-safe. Register all subscribers before starting
// execution. Dispatch should only be called by ExecutionContext.
type Registry struct {
	subscribers  []any
	maxRecursion int
}

// DefaultMaxRecursion is the default maximum event recursion depth.
const DefaultMaxRecursion = 10

// NewRegistry creates a new empty Registry with default settings.
func NewRegistry() *Registry {
	return &Registry{
		subscribers:  make([]any, 0),
		maxRecursion: DefaultMaxRecursion,
	}
}

// Subscribe adds a subscriber to the registry. The subscriber can implement any
// combination of subscriber interfaces (BeforeExecutionSubscriber,
// AfterExecutionSubscriber, etc.).
//
// Subscribers are called in the order they are registered.
func (r *Registry) Subscribe(subscriber any) *Registry {
	r.subscribers = append(r.subscribers, subscriber)
	return r
}

// SetMaxRecursion sets the maximum event recursion depth.
// If a subscriber publishes an event that triggers another subscriber
// that publishes an event, etc., this limit prevents infinite loops.
//
// Default is 10. Panics if a publish operation exceeds this depth.
func (r *Registry) SetMaxRecursion(max int) *Registry {
	r.maxRecursion = max
	return r
}

// MaxRecursion returns the configured maximum recursion depth.
func (r *Registry) MaxRecursion() int {
	return r.maxRecursion
}

// Dispatch sends an event to all matching subscribers.
// This is called by ExecutionContext.publish() after recording the event and updating stats.
func (r *Registry) Dispatch(execCtx *gent.ExecutionContext, event gent.Event) {
	switch e := event.(type) {
	case *gent.BeforeExecutionEvent:
		for _, s := range r.subscribers {
			if sub, ok := s.(gent.BeforeExecutionSubscriber); ok {
				sub.OnBeforeExecution(execCtx, e)
			}
		}
	case *gent.AfterExecutionEvent:
		for _, s := range r.subscribers {
			if sub, ok := s.(gent.AfterExecutionSubscriber); ok {
				sub.OnAfterExecution(execCtx, e)
			}
		}
	case *gent.BeforeIterationEvent:
		for _, s := range r.subscribers {
			if sub, ok := s.(gent.BeforeIterationSubscriber); ok {
				sub.OnBeforeIteration(execCtx, e)
			}
		}
	case *gent.AfterIterationEvent:
		for _, s := range r.subscribers {
			if sub, ok := s.(gent.AfterIterationSubscriber); ok {
				sub.OnAfterIteration(execCtx, e)
			}
		}
	case *gent.BeforeModelCallEvent:
		for _, s := range r.subscribers {
			if sub, ok := s.(gent.BeforeModelCallSubscriber); ok {
				sub.OnBeforeModelCall(execCtx, e)
			}
		}
	case *gent.AfterModelCallEvent:
		for _, s := range r.subscribers {
			if sub, ok := s.(gent.AfterModelCallSubscriber); ok {
				sub.OnAfterModelCall(execCtx, e)
			}
		}
	case *gent.BeforeToolCallEvent:
		for _, s := range r.subscribers {
			if sub, ok := s.(gent.BeforeToolCallSubscriber); ok {
				sub.OnBeforeToolCall(execCtx, e)
			}
		}
	case *gent.AfterToolCallEvent:
		for _, s := range r.subscribers {
			if sub, ok := s.(gent.AfterToolCallSubscriber); ok {
				sub.OnAfterToolCall(execCtx, e)
			}
		}
	case *gent.ParseErrorEvent:
		for _, s := range r.subscribers {
			if sub, ok := s.(gent.ParseErrorSubscriber); ok {
				sub.OnParseError(execCtx, e)
			}
		}
	case *gent.ValidatorCalledEvent:
		for _, s := range r.subscribers {
			if sub, ok := s.(gent.ValidatorCalledSubscriber); ok {
				sub.OnValidatorCalled(execCtx, e)
			}
		}
	case *gent.ValidatorResultEvent:
		for _, s := range r.subscribers {
			if sub, ok := s.(gent.ValidatorResultSubscriber); ok {
				sub.OnValidatorResult(execCtx, e)
			}
		}
	case *gent.ErrorEvent:
		for _, s := range r.subscribers {
			if sub, ok := s.(gent.ErrorSubscriber); ok {
				sub.OnError(execCtx, e)
			}
		}
	case *gent.CommonEvent:
		for _, s := range r.subscribers {
			if sub, ok := s.(gent.CommonEventSubscriber); ok {
				sub.OnCommonEvent(execCtx, e)
			}
		}
	case *gent.CompactionEvent:
		for _, s := range r.subscribers {
			if sub, ok := s.(gent.CompactionSubscriber); ok {
				sub.OnCompaction(execCtx, e)
			}
		}
	case *gent.LimitExceededEvent:
		for _, s := range r.subscribers {
			if sub, ok := s.(gent.LimitExceededSubscriber); ok {
				sub.OnLimitExceeded(execCtx, e)
			}
		}
	case *gent.CommonDiffEvent:
		for _, s := range r.subscribers {
			if sub, ok := s.(gent.CommonDiffEventSubscriber); ok {
				sub.OnCommonDiffEvent(execCtx, e)
			}
		}
	}
}

// Len returns the number of registered subscribers.
func (r *Registry) Len() int {
	return len(r.subscribers)
}

// Clear removes all registered subscribers.
func (r *Registry) Clear() {
	r.subscribers = make([]any, 0)
}
