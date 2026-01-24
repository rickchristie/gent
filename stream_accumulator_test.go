package gent

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStreamAccumulator_BasicAccumulation(t *testing.T) {
	type input struct {
		chunks []StreamChunk
	}

	type expected struct {
		content string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "accumulates multiple chunks",
			input: input{
				chunks: []StreamChunk{
					{Content: "Hello"},
					{Content: " "},
					{Content: "World"},
				},
			},
			expected: expected{content: "Hello World"},
		},
		{
			name: "empty chunks",
			input: input{
				chunks: []StreamChunk{
					{},
					{Content: ""},
					{ReasoningContent: ""},
				},
			},
			expected: expected{content: ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			acc := NewStreamAccumulator()

			for _, chunk := range tt.input.chunks {
				acc.Add(chunk)
			}

			assert.Equal(t, tt.expected.content, acc.Content())
		})
	}
}

func TestStreamAccumulator_ReasoningContent(t *testing.T) {
	type input struct {
		chunks []StreamChunk
	}

	type expected struct {
		reasoningContent string
		content          string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "accumulates reasoning and content separately",
			input: input{
				chunks: []StreamChunk{
					{ReasoningContent: "Let me think..."},
					{ReasoningContent: " Step 1."},
					{Content: "The answer is 42."},
				},
			},
			expected: expected{
				reasoningContent: "Let me think... Step 1.",
				content:          "The answer is 42.",
			},
		},
		{
			name: "interleaved reasoning and content",
			input: input{
				chunks: []StreamChunk{
					{ReasoningContent: "thinking..."},
					{Content: "Hello"},
					{ReasoningContent: "more thinking"},
					{Content: " World"},
				},
			},
			expected: expected{
				reasoningContent: "thinking...more thinking",
				content:          "Hello World",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			acc := NewStreamAccumulator()

			for _, chunk := range tt.input.chunks {
				acc.Add(chunk)
			}

			assert.Equal(t, tt.expected.reasoningContent, acc.ReasoningContent())
			assert.Equal(t, tt.expected.content, acc.Content())
		})
	}
}

func TestStreamAccumulator_Error(t *testing.T) {
	expectedErr := errors.New("stream error")
	acc := NewStreamAccumulator()

	acc.Add(StreamChunk{Content: "partial"})
	acc.Add(StreamChunk{Err: expectedErr})

	assert.Equal(t, expectedErr, acc.Error())
	assert.Equal(t, "partial", acc.Content())
}

func TestStreamAccumulator_Response(t *testing.T) {
	acc := NewStreamAccumulator()

	acc.Add(StreamChunk{Content: "Hello World"})
	acc.Add(StreamChunk{ReasoningContent: "I thought about it"})

	response := acc.Response()

	assert.Len(t, response.Choices, 1)
	assert.Equal(t, "Hello World", response.Choices[0].Content)
	assert.Equal(t, "I thought about it", response.Choices[0].ReasoningContent)
}

func TestStreamAccumulator_ResponseWithInfo(t *testing.T) {
	type input struct {
		chunks         []StreamChunk
		streamResponse *ContentResponse
	}

	type expected struct {
		content      string
		inputTokens  int
		outputTokens int
		infoIsNil    bool
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "with valid info",
			input: input{
				chunks: []StreamChunk{{Content: "Hello"}},
				streamResponse: &ContentResponse{
					Info: &GenerationInfo{
						InputTokens:  10,
						OutputTokens: 5,
					},
				},
			},
			expected: expected{
				content:      "Hello",
				inputTokens:  10,
				outputTokens: 5,
				infoIsNil:    false,
			},
		},
		{
			name: "with nil response",
			input: input{
				chunks:         []StreamChunk{{Content: "Hello"}},
				streamResponse: nil,
			},
			expected: expected{
				content:   "Hello",
				infoIsNil: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			acc := NewStreamAccumulator()

			for _, chunk := range tt.input.chunks {
				acc.Add(chunk)
			}

			response := acc.ResponseWithInfo(tt.input.streamResponse)

			assert.Equal(t, tt.expected.content, response.Choices[0].Content)

			if tt.expected.infoIsNil {
				assert.Nil(t, response.Info)
			} else {
				assert.NotNil(t, response.Info)
				assert.Equal(t, tt.expected.inputTokens, response.Info.InputTokens)
				assert.Equal(t, tt.expected.outputTokens, response.Info.OutputTokens)
			}
		})
	}
}

func TestStreamAccumulator_Reset(t *testing.T) {
	acc := NewStreamAccumulator()

	acc.Add(StreamChunk{Content: "Hello"})
	acc.Add(StreamChunk{ReasoningContent: "thinking"})
	acc.Add(StreamChunk{Err: errors.New("error")})

	acc.Reset()

	assert.Equal(t, "", acc.Content())
	assert.Equal(t, "", acc.ReasoningContent())
	assert.Nil(t, acc.Error())
}
