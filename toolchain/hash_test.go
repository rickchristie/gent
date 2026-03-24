package toolchain

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// -----------------------------------------------------------------------------
// Mock types for DeduplicateSummary tests
// -----------------------------------------------------------------------------

type mockDedupTool struct{}

func (m *mockDedupTool) DeduplicateSummary(
	input map[string]any, output string,
) string {
	return fmt.Sprintf("summary for %v", input["query"])
}

type mockDedupToolEmpty struct{}

func (m *mockDedupToolEmpty) DeduplicateSummary(
	input map[string]any, output string,
) string {
	return ""
}

type noMethodTool struct{}

type wrongInputTypeTool struct{}

func (m *wrongInputTypeTool) DeduplicateSummary(
	input int, output string,
) string {
	return "wrong"
}

type wrongOutputTypeTool struct{}

func (m *wrongOutputTypeTool) DeduplicateSummary(
	input map[string]any, output int,
) string {
	return "wrong"
}

// -----------------------------------------------------------------------------
// TestToolCallResultHash
// -----------------------------------------------------------------------------

func TestToolCallResultHash(t *testing.T) {
	type input struct {
		name string
		args map[string]any
	}

	type expected struct {
		hash string
	}

	// Pre-compute reference hashes for comparison tests.
	refSameArgs := ToolCallResultHash("tool_a", map[string]any{
		"key": "value",
	})
	refDiffName := ToolCallResultHash("tool_b", map[string]any{
		"key": "value",
	})
	refDiffArgs := ToolCallResultHash("tool_a", map[string]any{
		"key": "other",
	})
	refEmptyArgs := ToolCallResultHash("tool_a", map[string]any{})
	refNilArgs := ToolCallResultHash("tool_a", nil)
	refNested := ToolCallResultHash("tool_a", map[string]any{
		"outer": map[string]any{
			"inner": "deep",
		},
	})
	refInt := ToolCallResultHash("types", map[string]any{
		"v": 42,
	})
	refFloat := ToolCallResultHash("types", map[string]any{
		"v": 42.5,
	})
	refString := ToolCallResultHash("types", map[string]any{
		"v": "42",
	})

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "same name and same args produce same hash",
			input: input{
				name: "tool_a",
				args: map[string]any{"key": "value"},
			},
			expected: expected{hash: refSameArgs},
		},
		{
			name: "different name same args produce different hash",
			input: input{
				name: "tool_b",
				args: map[string]any{"key": "value"},
			},
			expected: expected{hash: refDiffName},
		},
		{
			name: "same name different args produce different hash",
			input: input{
				name: "tool_a",
				args: map[string]any{"key": "other"},
			},
			expected: expected{hash: refDiffArgs},
		},
		{
			name: "arg order does not matter - order A",
			input: input{
				name: "tool_a",
				args: map[string]any{
					"alpha": "1",
					"beta":  "2",
					"gamma": "3",
				},
			},
			expected: expected{
				hash: ToolCallResultHash("tool_a", map[string]any{
					"gamma": "3",
					"alpha": "1",
					"beta":  "2",
				}),
			},
		},
		{
			name: "empty args produce deterministic hash",
			input: input{
				name: "tool_a",
				args: map[string]any{},
			},
			expected: expected{hash: refEmptyArgs},
		},
		{
			name: "nil args produce deterministic hash",
			input: input{
				name: "tool_a",
				args: nil,
			},
			expected: expected{hash: refNilArgs},
		},
		{
			name: "complex nested args produce deterministic hash",
			input: input{
				name: "tool_a",
				args: map[string]any{
					"outer": map[string]any{
						"inner": "deep",
					},
				},
			},
			expected: expected{hash: refNested},
		},
		{
			name: "int value produces distinct hash",
			input: input{
				name: "types",
				args: map[string]any{"v": 42},
			},
			expected: expected{hash: refInt},
		},
		{
			name: "float value produces distinct hash",
			input: input{
				name: "types",
				args: map[string]any{"v": 42.5},
			},
			expected: expected{hash: refFloat},
		},
		{
			name: "string value produces distinct hash",
			input: input{
				name: "types",
				args: map[string]any{"v": "42"},
			},
			expected: expected{hash: refString},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ToolCallResultHash(tt.input.name, tt.input.args)
			assert.Equal(t, tt.expected.hash, result)
		})
	}

	// Verify distinctness across the reference hashes.
	t.Run("different name and args yield different hashes", func(t *testing.T) {
		assert.NotEqual(t, refSameArgs, refDiffName)
		assert.NotEqual(t, refSameArgs, refDiffArgs)
	})

	t.Run("different value types yield different hashes", func(t *testing.T) {
		assert.NotEqual(t, refInt, refFloat)
		assert.NotEqual(t, refInt, refString)
		assert.NotEqual(t, refFloat, refString)
	})

	t.Run("hash format is 16 hex characters", func(t *testing.T) {
		h := ToolCallResultHash("x", nil)
		assert.Len(t, h, 16)
	})
}

// -----------------------------------------------------------------------------
// TestCallDeduplicateSummaryReflect
// -----------------------------------------------------------------------------

func TestCallDeduplicateSummaryReflect(t *testing.T) {
	type input struct {
		tool       any
		inputVal   any
		outputVal  any
	}

	type expected struct {
		result string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "tool with DeduplicateSummary returns non-empty string",
			input: input{
				tool:      &mockDedupTool{},
				inputVal:  map[string]any{"query": "hello"},
				outputVal: "some output",
			},
			expected: expected{result: "summary for hello"},
		},
		{
			name: "tool with DeduplicateSummary returning empty string",
			input: input{
				tool:      &mockDedupToolEmpty{},
				inputVal:  map[string]any{"query": "hello"},
				outputVal: "some output",
			},
			expected: expected{result: ""},
		},
		{
			name: "nil tool returns empty string",
			input: input{
				tool:      nil,
				inputVal:  map[string]any{"query": "hello"},
				outputVal: "some output",
			},
			expected: expected{result: ""},
		},
		{
			name: "tool without DeduplicateSummary method returns empty",
			input: input{
				tool:      &noMethodTool{},
				inputVal:  map[string]any{"query": "hello"},
				outputVal: "some output",
			},
			expected: expected{result: ""},
		},
		{
			name: "wrong input type returns empty string",
			input: input{
				tool:      &mockDedupTool{},
				inputVal:  "not a map",
				outputVal: "some output",
			},
			expected: expected{result: ""},
		},
		{
			name: "wrong output type returns empty string",
			input: input{
				tool:      &mockDedupTool{},
				inputVal:  map[string]any{"query": "hello"},
				outputVal: 12345,
			},
			expected: expected{result: ""},
		},
		{
			name: "nil input value uses zero value",
			input: input{
				tool:      &mockDedupTool{},
				inputVal:  nil,
				outputVal: "some output",
			},
			expected: expected{result: "summary for <nil>"},
		},
		{
			name: "nil output value uses zero value",
			input: input{
				tool:      &mockDedupTool{},
				inputVal:  map[string]any{"query": "test"},
				outputVal: nil,
			},
			expected: expected{result: "summary for test"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CallDeduplicateSummaryReflect(
				tt.input.tool,
				tt.input.inputVal,
				tt.input.outputVal,
			)
			assert.Equal(t, tt.expected.result, result)
		})
	}
}
