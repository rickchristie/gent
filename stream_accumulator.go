package gent

import (
	"strings"
	"sync"
)

// StreamAccumulator accumulates StreamChunks into a complete ContentResponse.
// It handles both content and reasoning content, building up the full response
// as chunks arrive.
//
// Usage:
//
//	acc := NewStreamAccumulator()
//	for chunk := range stream.Chunks() {
//	    if chunk.Err != nil {
//	        return chunk.Err
//	    }
//	    acc.Add(chunk)
//	}
//	response := acc.Response()
type StreamAccumulator struct {
	mu               sync.Mutex
	content          strings.Builder
	reasoningContent strings.Builder
	lastError        error
}

// NewStreamAccumulator creates a new StreamAccumulator.
func NewStreamAccumulator() *StreamAccumulator {
	return &StreamAccumulator{}
}

// Add adds a chunk to the accumulator.
// If the chunk contains an error, it is stored and can be retrieved via Error().
func (a *StreamAccumulator) Add(chunk StreamChunk) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if chunk.Content != "" {
		a.content.WriteString(chunk.Content)
	}
	if chunk.ReasoningContent != "" {
		a.reasoningContent.WriteString(chunk.ReasoningContent)
	}
	if chunk.Err != nil {
		a.lastError = chunk.Err
	}
}

// Content returns the accumulated content so far.
func (a *StreamAccumulator) Content() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.content.String()
}

// ReasoningContent returns the accumulated reasoning content so far.
func (a *StreamAccumulator) ReasoningContent() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.reasoningContent.String()
}

// Error returns the last error encountered, if any.
func (a *StreamAccumulator) Error() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastError
}

// Response builds a ContentResponse from the accumulated chunks.
// This should be called after all chunks have been added.
// Note: This only populates content fields. Token counts and other metadata
// should come from the Stream.Response() or be set separately.
func (a *StreamAccumulator) Response() *ContentResponse {
	a.mu.Lock()
	defer a.mu.Unlock()

	return &ContentResponse{
		Choices: []*ContentChoice{
			{
				Content:          a.content.String(),
				ReasoningContent: a.reasoningContent.String(),
			},
		},
	}
}

// ResponseWithInfo builds a ContentResponse and merges it with info from Stream.Response().
// This is the preferred method when you have access to the Stream's final response,
// as it includes token counts and duration.
func (a *StreamAccumulator) ResponseWithInfo(streamResponse *ContentResponse) *ContentResponse {
	a.mu.Lock()
	defer a.mu.Unlock()

	response := &ContentResponse{
		Choices: []*ContentChoice{
			{
				Content:          a.content.String(),
				ReasoningContent: a.reasoningContent.String(),
			},
		},
	}

	// Copy info from stream response if available
	if streamResponse != nil && streamResponse.Info != nil {
		response.Info = streamResponse.Info
	}

	return response
}

// Reset clears the accumulator for reuse.
func (a *StreamAccumulator) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.content.Reset()
	a.reasoningContent.Reset()
	a.lastError = nil
}
