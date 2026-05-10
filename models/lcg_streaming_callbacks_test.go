package models

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
)

type callbackContentMode string

const (
	callbackContentDuplicateStreamingFirst callbackContentMode = "duplicate_streaming_first"
	callbackContentDuplicateReasoningFirst callbackContentMode = "duplicate_reasoning_first"
	callbackContentStreamingOnly           callbackContentMode = "streaming_only"
	callbackContentReasoningOnly           callbackContentMode = "reasoning_only"
)

type concurrentStreamingCallbackModel struct {
	mode   callbackContentMode
	chunks []string
}

func (m *concurrentStreamingCallbackModel) GenerateContent(
	ctx context.Context,
	_ []llms.MessageContent,
	options ...llms.CallOption,
) (*llms.ContentResponse, error) {
	var opts llms.CallOptions
	for _, opt := range options {
		opt(&opts)
	}

	if opts.StreamingReasoningFunc == nil {
		return nil, fmt.Errorf("streaming reasoning callback is required")
	}

	switch m.mode {
	case callbackContentDuplicateStreamingFirst:
		if opts.StreamingFunc == nil {
			return nil, fmt.Errorf("streaming callback is required")
		}
		if err := callStreamingFuncConcurrently(ctx, opts.StreamingFunc, m.chunks); err != nil {
			return nil, err
		}
		if err := callReasoningContentConcurrently(
			ctx, opts.StreamingReasoningFunc, m.chunks,
		); err != nil {
			return nil, err
		}
	case callbackContentDuplicateReasoningFirst:
		if opts.StreamingFunc == nil {
			return nil, fmt.Errorf("streaming callback is required")
		}
		if err := callReasoningContentConcurrently(
			ctx, opts.StreamingReasoningFunc, m.chunks,
		); err != nil {
			return nil, err
		}
		if err := callStreamingFuncConcurrently(ctx, opts.StreamingFunc, m.chunks); err != nil {
			return nil, err
		}
	case callbackContentStreamingOnly:
		if opts.StreamingFunc == nil {
			return nil, fmt.Errorf("streaming callback is required")
		}
		if err := callStreamingFuncConcurrently(ctx, opts.StreamingFunc, m.chunks); err != nil {
			return nil, err
		}
	case callbackContentReasoningOnly:
		if err := callReasoningContentConcurrently(
			ctx, opts.StreamingReasoningFunc, m.chunks,
		); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown callback content mode: %s", m.mode)
	}

	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: "done"}}}, nil
}

func (m *concurrentStreamingCallbackModel) Call(
	_ context.Context,
	_ string,
	_ ...llms.CallOption,
) (string, error) {
	return "", nil
}

func TestLCGWrapperGenerateContentStream_ConcurrentCallbacksDedupeContent(t *testing.T) {
	type input struct {
		mode   callbackContentMode
		chunks []string
	}
	type expected struct {
		contentCounts map[string]int
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}

	testCases := []testCase{
		{
			name: "dedupes duplicate content when streaming callback runs first",
			input: input{
				mode:   callbackContentDuplicateStreamingFirst,
				chunks: []string{"alpha", "alpha", "bravo"},
			},
			expected: expected{contentCounts: map[string]int{
				"alpha": 2,
				"bravo": 1,
			}},
		},
		{
			name: "dedupes duplicate content when reasoning callback runs first",
			input: input{
				mode:   callbackContentDuplicateReasoningFirst,
				chunks: []string{"charlie", "charlie", "delta"},
			},
			expected: expected{contentCounts: map[string]int{
				"charlie": 2,
				"delta":   1,
			}},
		},
		{
			name: "keeps repeated content that only arrives through streaming callback",
			input: input{
				mode:   callbackContentStreamingOnly,
				chunks: []string{"echo", "echo", "foxtrot"},
			},
			expected: expected{contentCounts: map[string]int{
				"echo":    2,
				"foxtrot": 1,
			}},
		},
		{
			name: "keeps repeated content that only arrives through reasoning callback",
			input: input{
				mode:   callbackContentReasoningOnly,
				chunks: []string{"golf", "golf", "hotel"},
			},
			expected: expected{contentCounts: map[string]int{
				"golf":  2,
				"hotel": 1,
			}},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			model := &concurrentStreamingCallbackModel{
				mode:   tc.input.mode,
				chunks: tc.input.chunks,
			}
			wrapper := NewLCGWrapper(model).WithModelName("callback-race-test")
			stream, err := wrapper.GenerateContentStream(
				nil,
				"stream",
				"topic",
				[]llms.MessageContent{llms.TextParts(llms.ChatMessageTypeHuman, "hello")},
			)
			require.NoError(t, err)

			assert.Equal(t, tc.expected.contentCounts, collectStreamContentCounts(t, stream))
		})
	}
}

func callStreamingFuncConcurrently(
	ctx context.Context,
	callback func(context.Context, []byte) error,
	chunks []string,
) error {
	return callChunkCallbacksConcurrently(chunks, func(chunk string) error {
		return callback(ctx, []byte(chunk))
	})
}

func callReasoningContentConcurrently(
	ctx context.Context,
	callback func(context.Context, []byte, []byte) error,
	chunks []string,
) error {
	return callChunkCallbacksConcurrently(chunks, func(chunk string) error {
		return callback(ctx, nil, []byte(chunk))
	})
}

func callChunkCallbacksConcurrently(chunks []string, callback func(string) error) error {
	start := make(chan struct{})
	errors := make(chan error, len(chunks))
	var wg sync.WaitGroup
	for _, chunk := range chunks {
		chunk := chunk
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := callback(chunk); err != nil {
				errors <- err
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errors)
	for err := range errors {
		return err
	}
	return nil
}

func collectStreamContentCounts(t *testing.T, stream gent.Stream) map[string]int {
	t.Helper()
	contentCounts := make(map[string]int)
	for chunk := range stream.Chunks() {
		require.NoError(t, chunk.Err)
		if chunk.Content != "" {
			contentCounts[chunk.Content]++
		}
	}
	_, err := stream.Response()
	require.NoError(t, err)
	return contentCounts
}
