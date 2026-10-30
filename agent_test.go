package gent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// -----------------------------------------------------------------------------
// BasicLoopData Tests
// -----------------------------------------------------------------------------

func TestBasicLoopData_GetTask(t *testing.T) {
	type input struct{ task *Task }
	type expected struct{ task *Task }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"text", input{NewTask(TextContent{Text: "test input"})},
			expected{NewTask(TextContent{Text: "test input"})}},
		{"nil", input{}, expected{}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected.task, NewBasicLoopData(tt.input.task).GetTask())
		})
	}
}

// -----------------------------------------------------------------------------
// TestMessageContent_Metadata
// -----------------------------------------------------------------------------

func TestMessageContent_Metadata(t *testing.T) {
	type input struct {
		initialMetadata map[MessageContentMetadataKey]any
		setOps          []struct {
			key   MessageContentMetadataKey
			value any
		}
		getKey MessageContentMetadataKey
	}

	type expected struct {
		getValue any
		getOk    bool
		metadata map[MessageContentMetadataKey]any
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "SetMetadata on nil map initializes and stores",
			input: input{
				initialMetadata: nil,
				setOps: []struct {
					key   MessageContentMetadataKey
					value any
				}{
					{key: MMKToolChainResult, value: "test_value"},
				},
				getKey: MMKToolChainResult,
			},
			expected: expected{
				getValue: "test_value",
				getOk:    true,
				metadata: map[MessageContentMetadataKey]any{
					MMKToolChainResult: "test_value",
				},
			},
		},
		{
			name: "GetMetadata returns value when present",
			input: input{
				initialMetadata: map[MessageContentMetadataKey]any{
					MMKToolChainResult: "stored",
				},
				setOps: nil,
				getKey: MMKToolChainResult,
			},
			expected: expected{
				getValue: "stored",
				getOk:    true,
				metadata: map[MessageContentMetadataKey]any{
					MMKToolChainResult: "stored",
				},
			},
		},
		{
			name: "GetMetadata returns nil false when key absent",
			input: input{
				initialMetadata: map[MessageContentMetadataKey]any{
					"other_key": "other_value",
				},
				setOps: nil,
				getKey: MMKToolChainResult,
			},
			expected: expected{
				getValue: nil,
				getOk:    false,
				metadata: map[MessageContentMetadataKey]any{
					"other_key": "other_value",
				},
			},
		},
		{
			name: "GetMetadata returns nil false when map is nil",
			input: input{
				initialMetadata: nil,
				setOps:          nil,
				getKey:          MMKToolChainResult,
			},
			expected: expected{
				getValue: nil,
				getOk:    false,
				metadata: nil,
			},
		},
		{
			name: "multiple keys on same MessageContent",
			input: input{
				initialMetadata: nil,
				setOps: []struct {
					key   MessageContentMetadataKey
					value any
				}{
					{key: MMKToolChainResult, value: "tcr_value"},
					{key: "custom:key", value: 42},
					{key: "custom:another", value: true},
				},
				getKey: "custom:key",
			},
			expected: expected{
				getValue: 42,
				getOk:    true,
				metadata: map[MessageContentMetadataKey]any{
					MMKToolChainResult: "tcr_value",
					"custom:key":       42,
					"custom:another":   true,
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := &MessageContent{
				Role:     ChatMessageTypeHuman,
				Metadata: tc.input.initialMetadata,
			}

			for _, op := range tc.input.setOps {
				msg.SetMetadata(op.key, op.value)
			}

			val, ok := msg.GetMetadata(tc.input.getKey)

			assert.Equal(t, tc.expected.getValue, val)
			assert.Equal(t, tc.expected.getOk, ok)
			assert.Equal(t, tc.expected.metadata, msg.Metadata)
		})
	}
}
