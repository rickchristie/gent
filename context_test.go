package gent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetCompaction_PanicsOnMixedNil(t *testing.T) {
	type input struct {
		trigger  CompactionTrigger
		strategy CompactionStrategy
	}

	type expected struct {
		panics bool
	}

	// Minimal implementations for non-nil values
	trigger := &stubTrigger{}
	strategy := &stubStrategy{}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "both nil does not panic",
			input: input{
				trigger:  nil,
				strategy: nil,
			},
			expected: expected{panics: false},
		},
		{
			name: "both non-nil does not panic",
			input: input{
				trigger:  trigger,
				strategy: strategy,
			},
			expected: expected{panics: false},
		},
		{
			name: "trigger nil strategy non-nil panics",
			input: input{
				trigger:  nil,
				strategy: strategy,
			},
			expected: expected{panics: true},
		},
		{
			name: "trigger non-nil strategy nil panics",
			input: input{
				trigger:  trigger,
				strategy: nil,
			},
			expected: expected{panics: true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			execCtx := NewExecutionContext(
				context.Background(), "test",
				NewBasicLoopData(nil),
			)

			if tc.expected.panics {
				assert.Panics(t, func() {
					execCtx.SetCompaction(
						tc.input.trigger,
						tc.input.strategy,
					)
				})
			} else {
				assert.NotPanics(t, func() {
					execCtx.SetCompaction(
						tc.input.trigger,
						tc.input.strategy,
					)
				})
			}
		})
	}
}

// stubTrigger is a minimal CompactionTrigger for testing
// SetCompaction validation.
type stubTrigger struct{}

func (s *stubTrigger) ShouldCompact(
	_ *ExecutionContext,
) bool {
	return false
}

func (s *stubTrigger) NotifyCompacted(
	_ *ExecutionContext,
) {
}

// stubStrategy is a minimal CompactionStrategy for testing
// SetCompaction validation.
type stubStrategy struct{}

func (s *stubStrategy) Compact(
	_ *ExecutionContext,
) error {
	return nil
}
