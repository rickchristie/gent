package gent

import (
	"errors"
	"testing"
)

func TestStreamAccumulator_BasicAccumulation(t *testing.T) {
	acc := NewStreamAccumulator()

	acc.Add(StreamChunk{Content: "Hello"})
	acc.Add(StreamChunk{Content: " "})
	acc.Add(StreamChunk{Content: "World"})

	if acc.Content() != "Hello World" {
		t.Errorf("expected 'Hello World', got %q", acc.Content())
	}
}

func TestStreamAccumulator_ReasoningContent(t *testing.T) {
	acc := NewStreamAccumulator()

	acc.Add(StreamChunk{ReasoningContent: "Let me think..."})
	acc.Add(StreamChunk{ReasoningContent: " Step 1."})
	acc.Add(StreamChunk{Content: "The answer is 42."})

	if acc.ReasoningContent() != "Let me think... Step 1." {
		t.Errorf("expected reasoning content, got %q", acc.ReasoningContent())
	}
	if acc.Content() != "The answer is 42." {
		t.Errorf("expected content, got %q", acc.Content())
	}
}

func TestStreamAccumulator_MixedChunks(t *testing.T) {
	acc := NewStreamAccumulator()

	// Simulate interleaved reasoning and content
	acc.Add(StreamChunk{ReasoningContent: "thinking..."})
	acc.Add(StreamChunk{Content: "Hello"})
	acc.Add(StreamChunk{ReasoningContent: "more thinking"})
	acc.Add(StreamChunk{Content: " World"})

	if acc.Content() != "Hello World" {
		t.Errorf("expected 'Hello World', got %q", acc.Content())
	}
	if acc.ReasoningContent() != "thinking...more thinking" {
		t.Errorf("expected reasoning, got %q", acc.ReasoningContent())
	}
}

func TestStreamAccumulator_Error(t *testing.T) {
	acc := NewStreamAccumulator()

	expectedErr := errors.New("stream error")
	acc.Add(StreamChunk{Content: "partial"})
	acc.Add(StreamChunk{Err: expectedErr})

	if acc.Error() != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, acc.Error())
	}
	// Content should still be accumulated up to the error
	if acc.Content() != "partial" {
		t.Errorf("expected 'partial', got %q", acc.Content())
	}
}

func TestStreamAccumulator_Response(t *testing.T) {
	acc := NewStreamAccumulator()

	acc.Add(StreamChunk{Content: "Hello World"})
	acc.Add(StreamChunk{ReasoningContent: "I thought about it"})

	response := acc.Response()

	if len(response.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(response.Choices))
	}
	if response.Choices[0].Content != "Hello World" {
		t.Errorf("expected content, got %q", response.Choices[0].Content)
	}
	if response.Choices[0].ReasoningContent != "I thought about it" {
		t.Errorf("expected reasoning, got %q", response.Choices[0].ReasoningContent)
	}
}

func TestStreamAccumulator_ResponseWithInfo(t *testing.T) {
	acc := NewStreamAccumulator()

	acc.Add(StreamChunk{Content: "Hello"})

	streamResponse := &ContentResponse{
		Info: &GenerationInfo{
			InputTokens:  10,
			OutputTokens: 5,
		},
	}

	response := acc.ResponseWithInfo(streamResponse)

	if response.Choices[0].Content != "Hello" {
		t.Errorf("expected content, got %q", response.Choices[0].Content)
	}
	if response.Info == nil {
		t.Fatal("expected info to be set")
	}
	if response.Info.InputTokens != 10 {
		t.Errorf("expected 10 input tokens, got %d", response.Info.InputTokens)
	}
	if response.Info.OutputTokens != 5 {
		t.Errorf("expected 5 output tokens, got %d", response.Info.OutputTokens)
	}
}

func TestStreamAccumulator_ResponseWithInfo_NilInfo(t *testing.T) {
	acc := NewStreamAccumulator()

	acc.Add(StreamChunk{Content: "Hello"})

	response := acc.ResponseWithInfo(nil)

	if response.Choices[0].Content != "Hello" {
		t.Errorf("expected content, got %q", response.Choices[0].Content)
	}
	if response.Info != nil {
		t.Error("expected nil info")
	}
}

func TestStreamAccumulator_Reset(t *testing.T) {
	acc := NewStreamAccumulator()

	acc.Add(StreamChunk{Content: "Hello"})
	acc.Add(StreamChunk{ReasoningContent: "thinking"})
	acc.Add(StreamChunk{Err: errors.New("error")})

	acc.Reset()

	if acc.Content() != "" {
		t.Errorf("expected empty content after reset, got %q", acc.Content())
	}
	if acc.ReasoningContent() != "" {
		t.Errorf("expected empty reasoning after reset, got %q", acc.ReasoningContent())
	}
	if acc.Error() != nil {
		t.Errorf("expected nil error after reset, got %v", acc.Error())
	}
}

func TestStreamAccumulator_EmptyChunks(t *testing.T) {
	acc := NewStreamAccumulator()

	// Empty chunks should be handled gracefully
	acc.Add(StreamChunk{})
	acc.Add(StreamChunk{Content: ""})
	acc.Add(StreamChunk{ReasoningContent: ""})

	if acc.Content() != "" {
		t.Errorf("expected empty content, got %q", acc.Content())
	}
}
