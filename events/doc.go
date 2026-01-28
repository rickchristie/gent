// Package events provides the event subscription registry for the gent framework.
//
// # Overview
//
// The events package is part of gent's unified event system. Events are published via
// ExecutionContext.PublishXXX() methods and subscribers registered with Registry
// receive them via type-safe interfaces.
//
// # Quick Start
//
//	// 1. Create subscribers by implementing subscriber interfaces
//	type LoggingSubscriber struct{}
//
//	func (s *LoggingSubscriber) OnBeforeIteration(
//	    execCtx *gent.ExecutionContext,
//	    event *gent.BeforeIterationEvent,
//	) {
//	    log.Printf("Starting iteration %d", event.Iteration)
//	}
//
//	func (s *LoggingSubscriber) OnAfterModelCall(
//	    execCtx *gent.ExecutionContext,
//	    event *gent.AfterModelCallEvent,
//	) {
//	    log.Printf("Model %s: %d tokens in %v",
//	        event.Model, event.InputTokens, event.Duration)
//	}
//
//	// 2. Create and configure registry
//	registry := events.NewRegistry()
//	registry.Subscribe(&LoggingSubscriber{})
//
//	// 3. Use with executor
//	exec := executor.New(agent, executor.Config{Events: registry})
//
// # Event Types
//
// Framework events (published automatically during execution):
//   - BeforeExecutionEvent, AfterExecutionEvent: Execution lifecycle
//   - BeforeIterationEvent, AfterIterationEvent: Iteration lifecycle
//   - BeforeModelCallEvent, AfterModelCallEvent: Model API calls
//   - BeforeToolCallEvent, AfterToolCallEvent: Tool executions
//   - ParseErrorEvent: Format/toolchain/termination parse failures
//   - ValidatorCalledEvent, ValidatorResultEvent: Answer validation
//   - ErrorEvent: General errors
//
// Custom events:
//   - CommonEvent: User-defined events via execCtx.PublishCommonEvent()
//
// # Subscriber Interfaces
//
// Implement any combination of these interfaces to receive specific events:
//   - gent.BeforeExecutionSubscriber, gent.AfterExecutionSubscriber
//   - gent.BeforeIterationSubscriber, gent.AfterIterationSubscriber
//   - gent.BeforeModelCallSubscriber, gent.AfterModelCallSubscriber
//   - gent.BeforeToolCallSubscriber, gent.AfterToolCallSubscriber
//   - gent.ParseErrorSubscriber
//   - gent.ValidatorCalledSubscriber, gent.ValidatorResultSubscriber
//   - gent.ErrorSubscriber
//   - gent.CommonEventSubscriber
//
// # Modifying Events
//
// Some "Before" events allow modification for interception:
//
//	func (s *ContextInjector) OnBeforeModelCall(
//	    execCtx *gent.ExecutionContext,
//	    event *gent.BeforeModelCallEvent,
//	) {
//	    // Add a system message (ephemeral - not persisted)
//	    if messages, ok := event.Request.([]llms.MessageContent); ok {
//	        event.Request = append(messages, llms.TextParts(
//	            llms.ChatMessageTypeSystem,
//	            "Current time: " + time.Now().Format(time.RFC3339),
//	        ))
//	    }
//	}
//
// # Publishing Custom Events
//
// Use ExecutionContext.PublishCommonEvent() for application-specific events:
//
//	// In your tool or agent code
//	execCtx.PublishCommonEvent("myapp:cache_hit", "Cache lookup succeeded", cacheData)
//
//	// Subscribe to receive them
//	func (s *MySubscriber) OnCommonEvent(
//	    execCtx *gent.ExecutionContext,
//	    event *gent.CommonEvent,
//	) {
//	    if event.EventName == "myapp:cache_hit" {
//	        s.metrics.IncrementCacheHits()
//	    }
//	}
//
// # Recursion Limits
//
// If a subscriber publishes events (which triggers other subscribers), recursion
// is tracked. Configure the limit to prevent infinite loops:
//
//	registry := events.NewRegistry()
//	registry.SetMaxRecursion(5) // Default is 10
//
// Exceeding the limit causes a panic with a descriptive message.
//
// See the gent package documentation for the complete event system design.
package events
