package gent

// Event name constants define the EventName values for framework events.
//
// # Naming Convention
//
// Event names follow the pattern: "namespace:category:timing"
//   - namespace: "gent" for framework events, application name for custom events
//   - category: what the event is about (execution, iteration, model_call, tool_call, etc.)
//   - timing: when in the lifecycle (before, after) - omitted for single events
//
// # Examples
//
//	gent:iteration:before    // Framework: before iteration
//	gent:model_call:after    // Framework: after model call
//	gent:parse_error         // Framework: parse error (no timing)
//	myapp:cache_hit          // Application: custom event
const (
	// Execution lifecycle
	EventNameExecutionBefore = "gent:execution:before"
	EventNameExecutionAfter  = "gent:execution:after"

	// Iteration lifecycle
	EventNameIterationBefore = "gent:iteration:before"
	EventNameIterationAfter  = "gent:iteration:after"

	// Model calls
	EventNameModelCallBefore = "gent:model_call:before"
	EventNameModelCallAfter  = "gent:model_call:after"

	// Tool calls
	EventNameToolCallBefore = "gent:tool_call:before"
	EventNameToolCallAfter  = "gent:tool_call:after"

	// Errors and validation
	EventNameParseError      = "gent:parse_error"
	EventNameValidatorCalled = "gent:validator:called"
	EventNameValidatorResult = "gent:validator:result"
	EventNameError           = "gent:error"
)
