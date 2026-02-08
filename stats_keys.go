package gent

import "strings"

// StatKey is a typed key for stats (counters and gauges).
//
// Standard gent keys use the "gent:" prefix. Users should use their own
// prefix (e.g., "myapp:") for custom metrics to avoid collisions.
//
// # Naming Convention
//
// Internal standard stats use a prefix convention to distinguish type:
//   - SC prefix = Stat Counter (e.g., SCInputTokens)
//   - SG prefix = Stat Gauge (e.g., SGFormatParseErrorConsecutive)
//
// # Counter Keys vs Gauge Keys
//
// Counter keys (SC*) are monotonically increasing and always propagate
// to parent contexts. Each counter key automatically has a $self:-prefixed
// counterpart that tracks only the local context's value.
//
// Gauge keys (SG*) can go up and down and never propagate to parent
// contexts. Calling Self() on a gauge key is a no-op since gauges are
// already local-only.
//
// Example custom key:
//
//	myKey := gent.StatKey("myapp:api_calls")
//	execCtx.Stats().IncrCounter(myKey, 1)
type StatKey string

const selfPrefix = "$self:"

// Self returns the local-only variant of this key. When used in limits,
// matches only the current context's value, excluding children.
//
// Only meaningful for counter keys. Gauge keys are already local-only,
// so calling Self() on them is unnecessary (the $self: key will never
// be written to).
//
// Self is idempotent: calling Self() on an already-$self: key returns
// the same key.
func (k StatKey) Self() StatKey {
	if strings.HasPrefix(string(k), selfPrefix) {
		return k
	}
	return StatKey(selfPrefix) + k
}

// IsSelf returns true if this is a $self:-prefixed local-only key.
// Useful for filtering when iterating over Counters().
func (k StatKey) IsSelf() bool {
	return strings.HasPrefix(string(k), selfPrefix)
}

// Standard key prefix for all gent library keys.
const KeyPrefix = "gent:"

// Iteration tracking (Counter).
//
// This key is PROTECTED - attempts to modify it via IncrCounter from
// user code will be silently ignored. Only the Executor can increment
// this counter internally.
//
// Propagates to parent: the parent's SCIterations reflects total
// iterations across the entire agent tree. Use SCIterations.Self()
// for per-context iteration limits.
//
// Read-only access:
//
//	iterations := execCtx.Stats().GetIterations()
const SCIterations StatKey = "gent:iterations"

// Token tracking keys (Counter).
//
// Auto-updated when AfterModelCallEvent is published:
//
//	execCtx.PublishAfterModelCall(
//	    "gpt-4", request, response, duration, nil,
//	)
//	// Auto-increments: SCInputTokens, SCInputTokensFor+"gpt-4",
//	//                  SCOutputTokens, SCOutputTokensFor+"gpt-4",
//	//                  SCTotalTokens, SCTotalTokensFor+"gpt-4"
//
// Use SCInputTokensFor/SCOutputTokensFor + model name for per-model
// limits:
//
//	{Type: LimitExactKey, Key: SCInputTokensFor + "gpt-4", MaxValue: 10000}
const (
	SCInputTokens    StatKey = "gent:input_tokens"
	SCInputTokensFor StatKey = "gent:input_tokens:" // + model name
	SCOutputTokens    StatKey = "gent:output_tokens"
	SCOutputTokensFor StatKey = "gent:output_tokens:" // + model name
)

// Total token tracking keys (Counter).
//
// Sum of input + output tokens. Auto-updated when
// AfterModelCallEvent is published alongside the individual
// input/output token counters.
//
// Use SCTotalTokensFor + model name for per-model limits:
//
//	{Type: LimitExactKey, Key: SCTotalTokensFor + "gpt-4", MaxValue: 20000}
const (
	SCTotalTokens    StatKey = "gent:total_tokens"
	SCTotalTokensFor StatKey = "gent:total_tokens:" // + model name
)

// Tool call tracking keys (Counter).
//
// Auto-updated when BeforeToolCallEvent is published:
//
//	execCtx.PublishBeforeToolCall("search", args)
//	// Auto-increments: SCToolCalls, SCToolCallsFor+"search"
//
// Use for per-tool limits:
//
//	// Limit specific tool to 10 calls
//	{
//	    Type: LimitExactKey,
//	    Key: SCToolCallsFor + "expensive_api",
//	    MaxValue: 10,
//	}
//
//	// Limit ANY tool to 50 calls
//	{Type: LimitKeyPrefix, Key: SCToolCallsFor, MaxValue: 50}
const (
	SCToolCalls    StatKey = "gent:tool_calls"
	SCToolCallsFor StatKey = "gent:tool_calls:" // + tool name
)

// Tool call error tracking keys.
//
// Auto-updated when AfterToolCallEvent with Error is published.
//
// Total/per-tool error keys are Counters (only go up, propagate).
// Consecutive error keys are Gauges (reset on success, local-only).
//
// Default limit: 3 consecutive errors (see DefaultLimits).
const (
	// Counters (monotonically increasing, propagated)
	SCToolCallsErrorTotal StatKey = "gent:tool_calls_error_total"
	SCToolCallsErrorFor   StatKey = "gent:tool_calls_error:" // + tool

	// Gauges (reset on success, local-only)
	SGToolCallsErrorConsecutive    StatKey = "gent:tool_calls_error_consecutive"
	SGToolCallsErrorConsecutiveFor StatKey = "gent:tool_calls_error_consecutive:" // + tool
)

// Format parse error tracking keys.
//
// Auto-updated when ParseErrorEvent with ErrorType="format" is
// published. These track errors when the TextFormat fails to parse
// LLM output structure.
//
// Default limit: 3 consecutive errors (see DefaultLimits).
const (
	// Counters
	SCFormatParseErrorTotal StatKey = "gent:format_parse_error_total"

	// Gauges
	SGFormatParseErrorConsecutive StatKey = "gent:format_parse_error_consecutive"
)

// Toolchain parse error tracking keys.
//
// Auto-updated when ParseErrorEvent with ErrorType="toolchain" is
// published. These track errors when the ToolChain fails to parse
// tool calls (YAML/JSON).
//
// Default limit: 3 consecutive errors (see DefaultLimits).
const (
	// Counters
	SCToolchainParseErrorTotal StatKey = "gent:toolchain_parse_error_total"

	// Gauges
	SGToolchainParseErrorConsecutive StatKey = "gent:toolchain_parse_error_consecutive"
)

// Termination parse error tracking keys.
//
// Auto-updated when ParseErrorEvent with ErrorType="termination" is
// published. These track errors when the Termination fails to parse
// answer content.
//
// Default limit: 3 consecutive errors (see DefaultLimits).
const (
	// Counters
	SCTerminationParseErrorTotal StatKey = "gent:termination_parse_error_total"

	// Gauges
	SGTerminationParseErrorConsecutive StatKey = "gent:termination_parse_error_consecutive"
)

// Section parse error tracking keys.
//
// Auto-updated when ParseErrorEvent with ErrorType="section" is
// published. These track errors when generic TextSections fail to
// parse content.
//
// Default limit: 3 consecutive errors (see DefaultLimits).
const (
	// Counters
	SCSectionParseErrorTotal StatKey = "gent:section_parse_error_total"

	// Gauges
	SGSectionParseErrorConsecutive StatKey = "gent:section_parse_error_consecutive"
)

// Scratchpad length tracking key (Gauge).
//
// Auto-updated when BasicLoopData.SetScratchPad is called.
// Reflects the current number of iterations in the scratchpad.
// Use for limits to prevent unbounded scratchpad growth.
//
// Example limit:
//
//	{Type: LimitExactKey, Key: SGScratchpadLength, MaxValue: 50}
const SGScratchpadLength StatKey = "gent:scratchpad_length"

// Last-iteration token tracking keys (Gauge).
//
// These gauges track token usage for the current/last iteration
// only. Unlike cumulative counters (SCInputTokens etc.), these
// gauges reset to 0 at each iteration start via
// BeforeIterationEvent.
//
// As gauges, they never propagate to parent contexts — each
// context tracks its own per-iteration usage independently.
//
// Auto-updated when AfterModelCallEvent is published. Within a
// single iteration, multiple model calls accumulate (e.g., main
// agent + tool that calls a model). Reset to 0 on the next
// BeforeIterationEvent.
//
// Example limits:
//
//	// No single iteration should use more than 50000 total tokens
//	{
//	    Type: LimitExactKey,
//	    Key: SGTotalTokensLastIteration,
//	    MaxValue: 50000,
//	}
//
//	// No single iteration should use more than 10000 tokens on
//	// a specific model
//	{
//	    Type: LimitExactKey,
//	    Key: SGTotalTokensLastIterationFor + "gpt-4",
//	    MaxValue: 10000,
//	}
//
//	// Prefix limit on per-model last-iteration tokens
//	{
//	    Type: LimitKeyPrefix,
//	    Key: SGTotalTokensLastIterationFor,
//	    MaxValue: 10000,
//	}
const (
	// Aggregate keys (all models combined)
	SGInputTokensLastIteration  StatKey = "gent:input_tokens_last_iteration"
	SGOutputTokensLastIteration StatKey = "gent:output_tokens_last_iteration"
	SGTotalTokensLastIteration  StatKey = "gent:total_tokens_last_iteration"

	// Per-model keys (append model name as suffix)
	SGInputTokensLastIterationFor  StatKey = "gent:input_tokens_last_iteration:"  // + model
	SGOutputTokensLastIterationFor StatKey = "gent:output_tokens_last_iteration:" // + model
	SGTotalTokensLastIterationFor  StatKey = "gent:total_tokens_last_iteration:"  // + model
)

// Answer rejection tracking keys (Counter).
//
// Updated by Termination implementations when a validator rejects an
// answer. Use SCAnswerRejectedBy + validator name for per-validator
// tracking.
//
// Example limits:
//
//	// Stop after 10 total rejections
//	{Type: LimitExactKey, Key: SCAnswerRejectedTotal, MaxValue: 10}
//
//	// Stop if specific validator rejects 5 times
//	{
//	    Type: LimitExactKey,
//	    Key: SCAnswerRejectedBy + "schema_validator",
//	    MaxValue: 5,
//	}
//
//	// Stop if ANY validator rejects 3 times
//	{Type: LimitKeyPrefix, Key: SCAnswerRejectedBy, MaxValue: 3}
//
// Default limit: 10 total rejections (see DefaultLimits).
const (
	SCAnswerRejectedTotal StatKey = "gent:answer_rejected_total"
	SCAnswerRejectedBy    StatKey = "gent:answer_rejected_by:" // + validator name
)

// protectedKeys contains keys that cannot be modified by user code
// via IncrCounter. Protected keys can still be incremented internally
// by the framework (e.g., the executor increments SCIterations).
var protectedKeys = map[StatKey]bool{
	SCIterations: true,
}

// isProtectedKey returns true if the key is protected from user
// modification.
func isProtectedKey(key StatKey) bool {
	return protectedKeys[key]
}
