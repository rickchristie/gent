package gent

// Standard key prefix for all gent library keys.
// Users should use their own prefix (e.g., "myapp:") for custom metrics
// to avoid collisions with gent's standard keys.
const KeyPrefix = "gent:"

// Iteration tracking.
// This key is protected - attempts to modify it via SetCounter or ResetCounter
// will be silently ignored. Only the Executor can increment this counter.
const KeyIterations = "gent:iterations"

// Token tracking keys.
const (
	KeyInputTokens     = "gent:input_tokens"
	KeyInputTokensFor  = "gent:input_tokens:"  // + model name
	KeyOutputTokens    = "gent:output_tokens"
	KeyOutputTokensFor = "gent:output_tokens:" // + model name
)

// Tool call tracking keys.
const (
	KeyToolCalls    = "gent:tool_calls"
	KeyToolCallsFor = "gent:tool_calls:" // + tool name
)

// Format parse error tracking keys (errors parsing LLM output sections).
const (
	KeyFormatParseErrorTotal       = "gent:format_parse_error_total"
	KeyFormatParseErrorAt          = "gent:format_parse_error:"             // + iteration
	KeyFormatParseErrorConsecutive = "gent:format_parse_error_consecutive"
)

// Toolchain parse error tracking keys (errors parsing YAML/JSON tool calls).
const (
	KeyToolchainParseErrorTotal       = "gent:toolchain_parse_error_total"
	KeyToolchainParseErrorAt          = "gent:toolchain_parse_error:"             // + iteration
	KeyToolchainParseErrorConsecutive = "gent:toolchain_parse_error_consecutive"
)

// Termination parse error tracking keys (errors parsing termination section content).
const (
	KeyTerminationParseErrorTotal       = "gent:termination_parse_error_total"
	KeyTerminationParseErrorAt          = "gent:termination_parse_error:"             // + iteration
	KeyTerminationParseErrorConsecutive = "gent:termination_parse_error_consecutive"
)

// Section parse error tracking keys (errors parsing generic output sections).
const (
	KeySectionParseErrorTotal       = "gent:section_parse_error_total"
	KeySectionParseErrorAt          = "gent:section_parse_error:"             // + iteration
	KeySectionParseErrorConsecutive = "gent:section_parse_error_consecutive"
)

// protectedKeys contains keys that cannot be modified by user code.
var protectedKeys = map[string]bool{
	KeyIterations: true,
}

// isProtectedKey returns true if the key is protected from user modification.
func isProtectedKey(key string) bool {
	return protectedKeys[key]
}
