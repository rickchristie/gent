package gent

// LimitType specifies how to match keys for limit checking.
type LimitType string

const (
	// LimitExactKey matches an exact key.
	// Use for specific stats like SCIterations or SCInputTokens.
	LimitExactKey LimitType = "exact"

	// LimitKeyPrefix matches any key with the given prefix.
	// Use for limits across all models/tools (e.g., SCToolCallsFor
	// matches all tool calls).
	LimitKeyPrefix LimitType = "prefix"
)

// Limit defines a threshold that triggers execution termination.
//
// # How Limits Work
//
// Limits are checked automatically whenever stats are updated. When
// any limit is exceeded, the ExecutionContext is cancelled and
// execution terminates with [TerminationLimitExceeded].
//
// # Exact Key Limits
//
// Match a specific stat key:
//
//	// Stop after 50 iterations in this context
//	{Type: LimitExactKey, Key: SCIterations.Self(), MaxValue: 50}
//
//	// Stop after 100k input tokens total (including children)
//	{Type: LimitExactKey, Key: SCInputTokens, MaxValue: 100000}
//
//	// Stop after 10 errors for a specific tool
//	{
//	    Type: LimitExactKey,
//	    Key: SCToolCallsErrorFor + "api_call",
//	    MaxValue: 10,
//	}
//
// # Prefix Limits
//
// Match any key starting with the prefix. Useful for aggregate
// limits:
//
//	// Stop if ANY tool is called more than 20 times
//	{Type: LimitKeyPrefix, Key: SCToolCallsFor, MaxValue: 20}
//
//	// Stop if ANY model uses more than 50k input tokens
//	{Type: LimitKeyPrefix, Key: SCInputTokensFor, MaxValue: 50000}
//
//	// Stop if ANY tool has more than 5 errors
//	{Type: LimitKeyPrefix, Key: SCToolCallsErrorFor, MaxValue: 5}
//
// # Hierarchical Limits
//
// Counter stats propagate from child to parent contexts. A limit on
// the root context applies to the total across all nested agent
// loops. Use [StatKey.Self] for per-context limits.
//
// Gauge stats are local to each context and never propagate.
type Limit struct {
	// Type specifies how to match keys (exact or prefix).
	Type LimitType

	// Key is the exact key or prefix to match.
	// For exact: use the full key (e.g., SCIterations.Self())
	// For prefix: use the prefix (e.g., SCToolCallsFor)
	Key StatKey

	// MaxValue is the threshold. Execution terminates when the
	// value exceeds this.
	// The comparison is: currentValue > MaxValue (not >=).
	// For counters, the int64 value is compared as float64.
	MaxValue float64
}

// DefaultLimits returns a set of sensible default limits.
//
// These defaults prevent runaway execution:
//   - 100 iterations max (per-context)
//   - 3 consecutive parse errors (format, toolchain, section,
//     termination)
//   - 3 consecutive tool call errors
//   - 10 total answer rejections
//
// Override with ExecutionContext.SetLimits():
//
//	customLimits := []gent.Limit{
//	    {
//	        Type:     gent.LimitExactKey,
//	        Key:      gent.SCIterations.Self(),
//	        MaxValue: 20,
//	    },
//	    {
//	        Type:     gent.LimitExactKey,
//	        Key:      gent.SCInputTokens,
//	        MaxValue: 50000,
//	    },
//	}
//	execCtx.SetLimits(customLimits)
//
// To add limits without replacing defaults, append to
// DefaultLimits():
//
//	limits := append(gent.DefaultLimits(),
//	    gent.Limit{
//	        Type:     gent.LimitExactKey,
//	        Key:      gent.SCInputTokens,
//	        MaxValue: 50000,
//	    },
//	)
//	execCtx.SetLimits(limits)
func DefaultLimits() []Limit {
	return []Limit{
		// Stop after 100 iterations (per-context)
		{
			Type:     LimitExactKey,
			Key:      SCIterations.Self(),
			MaxValue: 100,
		},

		// Stop after 3 consecutive format parse errors (gauge)
		{
			Type:     LimitExactKey,
			Key:      SGFormatParseErrorConsecutive,
			MaxValue: 3,
		},

		// Stop after 3 consecutive toolchain parse errors (gauge)
		{
			Type:     LimitExactKey,
			Key:      SGToolchainParseErrorConsecutive,
			MaxValue: 3,
		},

		// Stop after 3 consecutive section parse errors (gauge)
		{
			Type:     LimitExactKey,
			Key:      SGSectionParseErrorConsecutive,
			MaxValue: 3,
		},

		// Stop after 3 consecutive tool call errors (gauge)
		{
			Type:     LimitExactKey,
			Key:      SGToolCallsErrorConsecutive,
			MaxValue: 3,
		},

		// Stop after 10 total answer rejections (by validators)
		{
			Type:     LimitExactKey,
			Key:      SCAnswerRejectedTotal,
			MaxValue: 10,
		},
	}
}
