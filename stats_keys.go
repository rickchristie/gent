package gent

// Standard key prefix for all gent library keys.
// Users should use their own prefix (e.g., "myapp:") for custom metrics
// to avoid collisions with gent's standard keys.
//
// Example custom key:
//
//	execCtx.Stats().IncrCounter("myapp:api_calls", 1)
const KeyPrefix = "gent:"

// Iteration tracking.
//
// This key is PROTECTED - attempts to modify it via SetCounter or ResetCounter
// will be silently ignored. Only the Executor can increment this counter.
//
// Read-only access:
//
//	iterations := execCtx.Stats().GetIterations()
const KeyIterations = "gent:iterations"

// Token tracking keys.
//
// These are auto-updated when ModelCallTrace is traced:
//
//	execCtx.Trace(ModelCallTrace{
//	    Model:        "gpt-4",
//	    InputTokens:  1500,
//	    OutputTokens: 500,
//	})
//	// Auto-increments: KeyInputTokens, KeyInputTokensFor+"gpt-4",
//	//                  KeyOutputTokens, KeyOutputTokensFor+"gpt-4"
//
// Use KeyInputTokensFor/KeyOutputTokensFor + model name for per-model limits:
//
//	{Type: LimitExactKey, Key: KeyInputTokensFor + "gpt-4", MaxValue: 10000}
const (
	KeyInputTokens     = "gent:input_tokens"
	KeyInputTokensFor  = "gent:input_tokens:"  // + model name
	KeyOutputTokens    = "gent:output_tokens"
	KeyOutputTokensFor = "gent:output_tokens:" // + model name
)

// Tool call tracking keys.
//
// These are auto-updated when ToolCallTrace is traced:
//
//	execCtx.Trace(ToolCallTrace{ToolName: "search", ...})
//	// Auto-increments: KeyToolCalls, KeyToolCallsFor+"search"
//
// Use for per-tool limits:
//
//	// Limit specific tool to 10 calls
//	{Type: LimitExactKey, Key: KeyToolCallsFor + "expensive_api", MaxValue: 10}
//
//	// Limit ANY tool to 50 calls
//	{Type: LimitKeyPrefix, Key: KeyToolCallsFor, MaxValue: 50}
const (
	KeyToolCalls    = "gent:tool_calls"
	KeyToolCallsFor = "gent:tool_calls:" // + tool name
)

// Tool call error tracking keys.
//
// Auto-updated when ToolCallTrace with Error is traced. Includes both total
// counters and consecutive counters (reset on success).
//
// Consecutive counters are useful for detecting stuck loops where the same
// error keeps occurring.
//
// Default limit: 3 consecutive errors (see DefaultLimits).
const (
	KeyToolCallsErrorTotal          = "gent:tool_calls_error_total"
	KeyToolCallsErrorFor            = "gent:tool_calls_error:"             // + tool name
	KeyToolCallsErrorConsecutive    = "gent:tool_calls_error_consecutive"
	KeyToolCallsErrorConsecutiveFor = "gent:tool_calls_error_consecutive:" // + tool name
)

// Format parse error tracking keys.
//
// Auto-updated when ParseErrorTrace with ErrorType="format" is traced.
// These track errors when the TextFormat fails to parse LLM output structure.
//
// Default limit: 3 consecutive errors (see DefaultLimits).
const (
	KeyFormatParseErrorTotal       = "gent:format_parse_error_total"
	KeyFormatParseErrorAt          = "gent:format_parse_error:"             // + iteration
	KeyFormatParseErrorConsecutive = "gent:format_parse_error_consecutive"
)

// Toolchain parse error tracking keys.
//
// Auto-updated when ParseErrorTrace with ErrorType="toolchain" is traced.
// These track errors when the ToolChain fails to parse tool calls (YAML/JSON).
//
// Default limit: 3 consecutive errors (see DefaultLimits).
const (
	KeyToolchainParseErrorTotal       = "gent:toolchain_parse_error_total"
	KeyToolchainParseErrorAt          = "gent:toolchain_parse_error:"             // + iteration
	KeyToolchainParseErrorConsecutive = "gent:toolchain_parse_error_consecutive"
)

// Termination parse error tracking keys.
//
// Auto-updated when ParseErrorTrace with ErrorType="termination" is traced.
// These track errors when the Termination fails to parse answer content.
//
// Default limit: 3 consecutive errors (see DefaultLimits).
const (
	KeyTerminationParseErrorTotal       = "gent:termination_parse_error_total"
	KeyTerminationParseErrorAt          = "gent:termination_parse_error:"             // + iteration
	KeyTerminationParseErrorConsecutive = "gent:termination_parse_error_consecutive"
)

// Section parse error tracking keys.
//
// Auto-updated when ParseErrorTrace with ErrorType="section" is traced.
// These track errors when generic TextSections fail to parse content.
//
// Default limit: 3 consecutive errors (see DefaultLimits).
const (
	KeySectionParseErrorTotal       = "gent:section_parse_error_total"
	KeySectionParseErrorAt          = "gent:section_parse_error:"             // + iteration
	KeySectionParseErrorConsecutive = "gent:section_parse_error_consecutive"
)

// Answer rejection tracking keys.
//
// Updated by Termination implementations when a validator rejects an answer.
// Use KeyAnswerRejectedBy + validator name for per-validator tracking.
//
// Example limits:
//
//	// Stop after 10 total rejections
//	{Type: LimitExactKey, Key: KeyAnswerRejectedTotal, MaxValue: 10}
//
//	// Stop if specific validator rejects 5 times
//	{Type: LimitExactKey, Key: KeyAnswerRejectedBy + "schema_validator", MaxValue: 5}
//
//	// Stop if ANY validator rejects 3 times
//	{Type: LimitKeyPrefix, Key: KeyAnswerRejectedBy, MaxValue: 3}
//
// Default limit: 10 total rejections (see DefaultLimits).
const (
	KeyAnswerRejectedTotal = "gent:answer_rejected_total"
	KeyAnswerRejectedBy    = "gent:answer_rejected_by:" // + validator name
)

// protectedKeys contains keys that cannot be modified by user code.
var protectedKeys = map[string]bool{
	KeyIterations: true,
}

// isProtectedKey returns true if the key is protected from user modification.
func isProtectedKey(key string) bool {
	return protectedKeys[key]
}
