package gent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIteration_SetMetadata(t *testing.T) {
	type input struct {
		initialMetadata map[IterationMetadataKey]any
		key             IterationMetadataKey
		value           any
	}

	type expected struct {
		metadata map[IterationMetadataKey]any
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "initializes nil map and sets value",
			input: input{
				initialMetadata: nil,
				key:             IMKImportanceScore,
				value:           5.0,
			},
			expected: expected{
				metadata: map[IterationMetadataKey]any{
					IMKImportanceScore: 5.0,
				},
			},
		},
		{
			name: "sets value on existing map",
			input: input{
				initialMetadata: map[IterationMetadataKey]any{
					"existing_key": "existing_value",
				},
				key:   IMKImportanceScore,
				value: 3.0,
			},
			expected: expected{
				metadata: map[IterationMetadataKey]any{
					"existing_key":     "existing_value",
					IMKImportanceScore: 3.0,
				},
			},
		},
		{
			name: "overwrites existing key",
			input: input{
				initialMetadata: map[IterationMetadataKey]any{
					IMKImportanceScore: 1.0,
				},
				key:   IMKImportanceScore,
				value: 9.0,
			},
			expected: expected{
				metadata: map[IterationMetadataKey]any{
					IMKImportanceScore: 9.0,
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			iter := &Iteration{
				Metadata: tc.input.initialMetadata,
			}

			iter.SetMetadata(tc.input.key, tc.input.value)

			assert.Equal(t, tc.expected.metadata, iter.Metadata)
		})
	}
}

func TestIteration_GetMetadata(t *testing.T) {
	type input struct {
		metadata map[IterationMetadataKey]any
		key      IterationMetadataKey
	}

	type expected struct {
		value any
		ok    bool
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "nil map returns nil false",
			input: input{
				metadata: nil,
				key:      IMKImportanceScore,
			},
			expected: expected{
				value: nil,
				ok:    false,
			},
		},
		{
			name: "missing key returns nil false",
			input: input{
				metadata: map[IterationMetadataKey]any{
					"other_key": "other_value",
				},
				key: IMKImportanceScore,
			},
			expected: expected{
				value: nil,
				ok:    false,
			},
		},
		{
			name: "present key returns value true",
			input: input{
				metadata: map[IterationMetadataKey]any{
					IMKImportanceScore: 7.5,
				},
				key: IMKImportanceScore,
			},
			expected: expected{
				value: 7.5,
				ok:    true,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			iter := &Iteration{
				Metadata: tc.input.metadata,
			}

			val, ok := iter.GetMetadata(tc.input.key)

			assert.Equal(t, tc.expected.value, val)
			assert.Equal(t, tc.expected.ok, ok)
		})
	}
}

func TestGetImportanceScore(t *testing.T) {
	type input struct {
		metadata map[IterationMetadataKey]any
	}

	type expected struct {
		score float64
		ok    bool
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "nil metadata returns 0 false",
			input: input{
				metadata: nil,
			},
			expected: expected{
				score: 0,
				ok:    false,
			},
		},
		{
			name: "missing key returns 0 false",
			input: input{
				metadata: map[IterationMetadataKey]any{
					"other": "value",
				},
			},
			expected: expected{
				score: 0,
				ok:    false,
			},
		},
		{
			name: "wrong type returns 0 false",
			input: input{
				metadata: map[IterationMetadataKey]any{
					IMKImportanceScore: "not a float",
				},
			},
			expected: expected{
				score: 0,
				ok:    false,
			},
		},
		{
			name: "int type returns 0 false",
			input: input{
				metadata: map[IterationMetadataKey]any{
					IMKImportanceScore: 5,
				},
			},
			expected: expected{
				score: 0,
				ok:    false,
			},
		},
		{
			name: "valid positive float64 returns score true",
			input: input{
				metadata: map[IterationMetadataKey]any{
					IMKImportanceScore: 8.5,
				},
			},
			expected: expected{
				score: 8.5,
				ok:    true,
			},
		},
		{
			name: "valid negative float64 returns score true",
			input: input{
				metadata: map[IterationMetadataKey]any{
					IMKImportanceScore: -3.0,
				},
			},
			expected: expected{
				score: -3.0,
				ok:    true,
			},
		},
		{
			name: "zero float64 returns 0 true",
			input: input{
				metadata: map[IterationMetadataKey]any{
					IMKImportanceScore: 0.0,
				},
			},
			expected: expected{
				score: 0.0,
				ok:    true,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			iter := &Iteration{
				Metadata: tc.input.metadata,
			}

			score, ok := GetImportanceScore(iter)

			assert.Equal(t, tc.expected.score, score)
			assert.Equal(t, tc.expected.ok, ok)
		})
	}
}
