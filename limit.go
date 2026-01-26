package gent

// LimitType specifies how to match keys for limit checking.
type LimitType string

const (
	// LimitExactKey matches an exact key.
	// Use for specific counters like KeyIterations or KeyInputTokens.
	LimitExactKey LimitType = "exact"

	// LimitKeyPrefix matches any key with the given prefix.
	// Use for limits across all models/tools (e.g., KeyToolCallsFor matches all tool calls).
	LimitKeyPrefix LimitType = "prefix"
)

// Limit defines a threshold that triggers execution termination.
//
// # How Limits Work
//
// Limits are checked automatically whenever stats are updated. When any limit is
// exceeded, the ExecutionContext is cancelled and execution terminates with
// [TerminationLimitExceeded].
//
// # Exact Key Limits
//
// Match a specific stat key:
//
//	// Stop after 50 iterations
//	{Type: LimitExactKey, Key: KeyIterations, MaxValue: 50}
//
//	// Stop after 100k input tokens total
//	{Type: LimitExactKey, Key: KeyInputTokens, MaxValue: 100000}
//
//	// Stop after 10 errors for a specific tool
//	{Type: LimitExactKey, Key: KeyToolCallsErrorFor + "api_call", MaxValue: 10}
//
// # Prefix Limits
//
// Match any key starting with the prefix. Useful for aggregate limits:
//
//	// Stop if ANY tool is called more than 20 times
//	{Type: LimitKeyPrefix, Key: KeyToolCallsFor, MaxValue: 20}
//
//	// Stop if ANY model uses more than 50k input tokens
//	{Type: LimitKeyPrefix, Key: KeyInputTokensFor, MaxValue: 50000}
//
//	// Stop if ANY tool has more than 5 errors
//	{Type: LimitKeyPrefix, Key: KeyToolCallsErrorFor, MaxValue: 5}
//
// # Hierarchical Limits
//
// Stats propagate from child to parent contexts. A limit on the root context
// applies to the total across all nested agent loops.
type Limit struct {
	// Type specifies how to match keys (exact or prefix).
	Type LimitType

	// Key is the exact key or prefix to match.
	// For exact: use the full key (e.g., "gent:iterations")
	// For prefix: use the prefix (e.g., "gent:tool_calls:")
	Key string

	// MaxValue is the threshold. Execution terminates when the value exceeds this.
	// The comparison is: currentValue > MaxValue (not >=).
	// For counters, the int64 value is compared as float64.
	MaxValue float64
}

// DefaultLimits returns a set of sensible default limits.
//
// These defaults prevent runaway execution:
//   - 100 iterations max
//   - 3 consecutive parse errors (format, toolchain, section, termination)
//   - 3 consecutive tool call errors
//   - 10 total answer rejections
//
// Override with ExecutionContext.SetLimits():
//
//	customLimits := []gent.Limit{
//	    {Type: gent.LimitExactKey, Key: gent.KeyIterations, MaxValue: 20},
//	    {Type: gent.LimitExactKey, Key: gent.KeyInputTokens, MaxValue: 50000},
//	}
//	execCtx.SetLimits(customLimits)
//
// To add limits without replacing defaults, append to DefaultLimits():
//
//	limits := append(gent.DefaultLimits(),
//	    gent.Limit{Type: gent.LimitExactKey, Key: gent.KeyInputTokens, MaxValue: 50000},
//	)
//	execCtx.SetLimits(limits)
func DefaultLimits() []Limit {
	return []Limit{
		// Stop after 100 iterations
		{Type: LimitExactKey, Key: KeyIterations, MaxValue: 100},

		// Stop after 3 consecutive format parse errors
		{Type: LimitExactKey, Key: KeyFormatParseErrorConsecutive, MaxValue: 3},

		// Stop after 3 consecutive toolchain parse errors
		{Type: LimitExactKey, Key: KeyToolchainParseErrorConsecutive, MaxValue: 3},

		// Stop after 3 consecutive section parse errors
		{Type: LimitExactKey, Key: KeySectionParseErrorConsecutive, MaxValue: 3},

		// Stop after 3 consecutive tool call errors
		{Type: LimitExactKey, Key: KeyToolCallsErrorConsecutive, MaxValue: 3},

		// Stop after 10 total answer rejections (by validators)
		{Type: LimitExactKey, Key: KeyAnswerRejectedTotal, MaxValue: 10},
	}
}
