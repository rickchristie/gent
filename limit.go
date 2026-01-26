package gent

// LimitType specifies how to match keys for limit checking.
type LimitType string

const (
	// LimitExactKey matches an exact key.
	LimitExactKey LimitType = "exact"

	// LimitKeyPrefix matches any key with the given prefix.
	// Useful for checking limits across all models or tools.
	LimitKeyPrefix LimitType = "prefix"
)

// Limit defines a threshold that triggers execution termination.
type Limit struct {
	// Type specifies how to match keys (exact or prefix).
	Type LimitType

	// Key is the exact key or prefix to match.
	Key string

	// MaxValue is the threshold. Execution terminates when the value exceeds this.
	// For counters, the int64 value is compared as float64.
	MaxValue float64
}

// DefaultLimits returns a set of sensible default limits.
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
