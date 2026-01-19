package models

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
)

// ChunkWriter writes streaming chunks with visible separators for debugging.
// It flushes after each write for real-time display.
type ChunkWriter struct {
	w            *bufio.Writer
	separator    string
	contentLen   int
	chunkCount   int
	reasoningLen int
}

// NewChunkWriter creates a ChunkWriter that writes to stdout with the given separator.
func NewChunkWriter(separator string) *ChunkWriter {
	return NewChunkWriterTo(os.Stdout, separator)
}

// NewChunkWriterTo creates a ChunkWriter that writes to the given writer.
func NewChunkWriterTo(w io.Writer, separator string) *ChunkWriter {
	return &ChunkWriter{
		w:         bufio.NewWriter(w),
		separator: separator,
	}
}

// Write writes a chunk with separator visualization and flushes immediately.
// Newlines are escaped as \n for visibility.
func (w *ChunkWriter) Write(chunk gent.StreamChunk) {
	if chunk.Content != "" {
		escaped := strings.ReplaceAll(chunk.Content, "\n", "\\n")
		fmt.Fprintf(w.w, "%s%s", w.separator, escaped)
		w.flush()
		w.contentLen += len(chunk.Content)
		w.chunkCount++
	}
	if chunk.ReasoningContent != "" {
		escaped := strings.ReplaceAll(chunk.ReasoningContent, "\n", "\\n")
		fmt.Fprintf(w.w, "%s[R:%s]", w.separator, escaped)
		w.flush()
		w.reasoningLen += len(chunk.ReasoningContent)
		w.chunkCount++
	}
}

// flush flushes the buffer and syncs stdout for real-time display.
func (w *ChunkWriter) flush() {
	w.w.Flush()
	os.Stdout.Sync()
}

// End prints the final separator and newline.
func (w *ChunkWriter) End() {
	fmt.Fprintf(w.w, "%s\n", w.separator)
	w.flush()
}

// Stats returns chunk count, content length, and reasoning length.
func (w *ChunkWriter) Stats() (chunks, contentLen, reasoningLen int) {
	return w.chunkCount, w.contentLen, w.reasoningLen
}

// PrintStats prints statistics about the streamed content.
func (w *ChunkWriter) PrintStats() {
	fmt.Fprintf(w.w, "Chunks: %d, Content: %d chars", w.chunkCount, w.contentLen)
	if w.reasoningLen > 0 {
		fmt.Fprintf(w.w, ", Reasoning: %d chars", w.reasoningLen)
	}
	if w.chunkCount > 0 {
		fmt.Fprintf(w.w, ", Avg: %.1f chars/chunk", float64(w.contentLen)/float64(w.chunkCount))
	}
	fmt.Fprintln(w.w)
	w.flush()
}

// TestStreamingBasic tests basic streaming functionality with a simple prompt.
func TestStreamingBasic(t *testing.T) {
	apiKey := os.Getenv("GENT_TEST_XAI_KEY")
	if apiKey == "" {
		t.Skip("GENT_TEST_XAI_KEY not set")
	}

	ctx := context.Background()

	llm, err := openai.New(
		openai.WithToken(apiKey),
		openai.WithBaseURL("https://api.x.ai/v1"),
		openai.WithModel("grok-3-fast"),
	)
	if err != nil {
		t.Fatalf("failed to create xAI LLM: %v", err)
	}

	model := NewLCGWrapper(llm).WithModelName("grok-3-fast")

	stream, err := model.GenerateContentStream(ctx, nil, []llms.MessageContent{
		llms.TextParts(
			llms.ChatMessageTypeHuman,
			"Write 3 paragraphs of a creative dark fantasy story",
		),
	})
	if err != nil {
		t.Fatalf("failed to start stream: %v", err)
	}

	fmt.Println("\n=== Basic Streaming Test ===")
	fmt.Println("Streaming response (| = chunk boundary):")

	cw := NewChunkWriter("|")
	for chunk := range stream.Chunks() {
		if chunk.Err != nil {
			t.Fatalf("stream error: %v", chunk.Err)
		}
		cw.Write(chunk)
	}
	cw.End()

	response, err := stream.Response()
	if err != nil {
		t.Fatalf("failed to get response: %v", err)
	}

	cw.PrintStats()
	if response.Info != nil {
		fmt.Printf("Input tokens: %d, Output tokens: %d\n",
			response.Info.InputTokens, response.Info.OutputTokens)
	}

	chunks, contentLen, _ := cw.Stats()
	if chunks == 0 || contentLen == 0 {
		t.Error("expected non-empty streamed content")
	}
}

// TestStreamingWithReasoning tests streaming with a reasoning model.
func TestStreamingWithReasoning(t *testing.T) {
	apiKey := os.Getenv("GENT_TEST_XAI_KEY")
	if apiKey == "" {
		t.Skip("GENT_TEST_XAI_KEY not set")
	}

	ctx := context.Background()

	llm, err := openai.New(
		openai.WithToken(apiKey),
		openai.WithBaseURL("https://api.x.ai/v1"),
		openai.WithModel("grok-4-1-fast-reasoning"),
	)
	if err != nil {
		t.Fatalf("failed to create xAI LLM: %v", err)
	}

	model := NewLCGWrapper(llm).WithModelName("grok-4-1-fast-reasoning")

	stream, err := model.GenerateContentStream(ctx, nil, []llms.MessageContent{
		llms.TextParts(
			llms.ChatMessageTypeHuman,
			"What is 15 * 17? Show your reasoning step by step.",
		),
	})
	if err != nil {
		t.Fatalf("failed to start stream: %v", err)
	}

	fmt.Println("\n=== Reasoning Streaming Test ===")
	fmt.Println("Streaming (| = chunk, [R:...] = reasoning):")

	cw := NewChunkWriter("|")
	for chunk := range stream.Chunks() {
		if chunk.Err != nil {
			t.Fatalf("stream error: %v", chunk.Err)
		}
		cw.Write(chunk)
	}
	cw.End()

	response, err := stream.Response()
	if err != nil {
		t.Fatalf("failed to get response: %v", err)
	}

	cw.PrintStats()
	if response.Info != nil {
		fmt.Printf("Input: %d, Output: %d, Reasoning: %d tokens\n",
			response.Info.InputTokens, response.Info.OutputTokens, response.Info.ReasoningTokens)
	}

	_, contentLen, _ := cw.Stats()
	if contentLen == 0 {
		t.Error("expected non-empty content")
	}
}

// TestStreamingSlowConsumer tests that streaming works with a slow consumer.
func TestStreamingSlowConsumer(t *testing.T) {
	apiKey := os.Getenv("GENT_TEST_XAI_KEY")
	if apiKey == "" {
		t.Skip("GENT_TEST_XAI_KEY not set")
	}

	ctx := context.Background()

	llm, err := openai.New(
		openai.WithToken(apiKey),
		openai.WithBaseURL("https://api.x.ai/v1"),
		openai.WithModel("grok-3-fast"),
	)
	if err != nil {
		t.Fatalf("failed to create xAI LLM: %v", err)
	}

	model := NewLCGWrapper(llm).WithModelName("grok-3-fast")

	stream, err := model.GenerateContentStream(ctx, nil, []llms.MessageContent{
		llms.TextParts(
			llms.ChatMessageTypeHuman,
			"Write a short poem about Go programming (4 lines).",
		),
	})
	if err != nil {
		t.Fatalf("failed to start stream: %v", err)
	}

	fmt.Println("\n=== Slow Consumer Test ===")
	fmt.Println("Processing with 50ms delay per chunk (| = chunk boundary):")

	cw := NewChunkWriter("|")
	startTime := time.Now()

	for chunk := range stream.Chunks() {
		if chunk.Err != nil {
			t.Fatalf("stream error: %v", chunk.Err)
		}
		if chunk.Content != "" {
			time.Sleep(50 * time.Millisecond)
			cw.Write(chunk)
		}
	}
	cw.End()

	elapsed := time.Since(startTime)

	response, err := stream.Response()
	if err != nil {
		t.Fatalf("failed to get response: %v", err)
	}

	cw.PrintStats()
	fmt.Printf("Consumer elapsed: %v, API duration: %v\n", elapsed, response.Info.Duration)

	_, contentLen, _ := cw.Stats()
	if contentLen == 0 {
		t.Error("expected non-empty content")
	}
}

// TestStreamingNoListener tests stream completion without reading chunks.
func TestStreamingNoListener(t *testing.T) {
	apiKey := os.Getenv("GENT_TEST_XAI_KEY")
	if apiKey == "" {
		t.Skip("GENT_TEST_XAI_KEY not set")
	}

	ctx := context.Background()

	llm, err := openai.New(
		openai.WithToken(apiKey),
		openai.WithBaseURL("https://api.x.ai/v1"),
		openai.WithModel("grok-3-fast"),
	)
	if err != nil {
		t.Fatalf("failed to create xAI LLM: %v", err)
	}

	model := NewLCGWrapper(llm).WithModelName("grok-3-fast")

	stream, err := model.GenerateContentStream(ctx, nil, []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeHuman, "Say hello in one word."),
	})
	if err != nil {
		t.Fatalf("failed to start stream: %v", err)
	}

	fmt.Println("\n=== No Listener Test ===")
	fmt.Println("Waiting for response without reading chunks...")

	response, err := stream.Response()
	if err != nil {
		t.Fatalf("failed to get response: %v", err)
	}

	fmt.Printf("Response received: %q\n", response.Choices[0].Content)

	if len(response.Choices) == 0 || response.Choices[0].Content == "" {
		t.Error("expected non-empty response")
	}
}

// TestStreamingCancellation tests context cancellation stops the stream.
func TestStreamingCancellation(t *testing.T) {
	apiKey := os.Getenv("GENT_TEST_XAI_KEY")
	if apiKey == "" {
		t.Skip("GENT_TEST_XAI_KEY not set")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	llm, err := openai.New(
		openai.WithToken(apiKey),
		openai.WithBaseURL("https://api.x.ai/v1"),
		openai.WithModel("grok-3-fast"),
	)
	if err != nil {
		t.Fatalf("failed to create xAI LLM: %v", err)
	}

	model := NewLCGWrapper(llm).WithModelName("grok-3-fast")

	stream, err := model.GenerateContentStream(ctx, nil, []llms.MessageContent{
		llms.TextParts(
			llms.ChatMessageTypeHuman,
			"Write a very long story about a programmer. Make it at least 500 words.",
		),
	})
	if err != nil {
		t.Fatalf("failed to start stream: %v", err)
	}

	fmt.Println("\n=== Cancellation Test ===")
	fmt.Println("Starting stream, will cancel after 5 chunks (| = chunk boundary):")

	cw := NewChunkWriter("|")
	cancelled := false

	for chunk := range stream.Chunks() {
		if chunk.Err != nil {
			fmt.Printf("\nStream error (expected): %v\n", chunk.Err)
			break
		}
		cw.Write(chunk)
		chunks, _, _ := cw.Stats()
		if chunks >= 5 && !cancelled {
			fmt.Print("| [CANCELLING]")
			cancel()
			cancelled = true
		}
	}
	cw.End()

	_, err = stream.Response()
	if err != nil {
		fmt.Printf("Response error (expected): %v\n", err)
	}

	cw.PrintStats()
}

// TestStreamingConcurrent tests multiple concurrent streams.
func TestStreamingConcurrent(t *testing.T) {
	apiKey := os.Getenv("GENT_TEST_XAI_KEY")
	if apiKey == "" {
		t.Skip("GENT_TEST_XAI_KEY not set")
	}

	ctx := context.Background()

	llm, err := openai.New(
		openai.WithToken(apiKey),
		openai.WithBaseURL("https://api.x.ai/v1"),
		openai.WithModel("grok-3-fast"),
	)
	if err != nil {
		t.Fatalf("failed to create xAI LLM: %v", err)
	}

	model := NewLCGWrapper(llm).WithModelName("grok-3-fast")

	fmt.Println("\n=== Concurrent Streaming Test ===")
	fmt.Println("Starting 3 concurrent streams...")

	prompts := []string{
		"Say 'Hello One' in exactly 2 words.",
		"Say 'Hello Two' in exactly 2 words.",
		"Say 'Hello Three' in exactly 2 words.",
	}

	var wg sync.WaitGroup
	type result struct {
		content string
		chunks  int
		err     error
	}
	results := make([]result, len(prompts))

	for i, prompt := range prompts {
		wg.Add(1)
		go func(idx int, p string) {
			defer wg.Done()

			stream, err := model.GenerateContentStream(ctx, nil, []llms.MessageContent{
				llms.TextParts(llms.ChatMessageTypeHuman, p),
			})
			if err != nil {
				results[idx].err = err
				return
			}

			var content strings.Builder
			chunks := 0
			for chunk := range stream.Chunks() {
				if chunk.Err != nil {
					results[idx].err = chunk.Err
					return
				}
				content.WriteString(chunk.Content)
				chunks++
			}

			if _, err := stream.Response(); err != nil {
				results[idx].err = err
				return
			}

			results[idx].content = content.String()
			results[idx].chunks = chunks
		}(i, prompt)
	}

	wg.Wait()

	for i, r := range results {
		if r.err != nil {
			t.Errorf("Stream %d error: %v", i, r.err)
		} else {
			fmt.Printf("Stream %d: %q (%d chunks)\n", i, strings.TrimSpace(r.content), r.chunks)
		}
	}
}

// TestStreamingResponseInfo tests response info is correctly populated.
func TestStreamingResponseInfo(t *testing.T) {
	apiKey := os.Getenv("GENT_TEST_XAI_KEY")
	if apiKey == "" {
		t.Skip("GENT_TEST_XAI_KEY not set")
	}

	ctx := context.Background()

	llm, err := openai.New(
		openai.WithToken(apiKey),
		openai.WithBaseURL("https://api.x.ai/v1"),
		openai.WithModel("grok-3-fast"),
	)
	if err != nil {
		t.Fatalf("failed to create xAI LLM: %v", err)
	}

	model := NewLCGWrapper(llm).WithModelName("grok-3-fast")

	stream, err := model.GenerateContentStream(ctx, nil, []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeHuman, "What is 2+2?"),
	})
	if err != nil {
		t.Fatalf("failed to start stream: %v", err)
	}

	fmt.Println("\n=== Response Info Test ===")
	fmt.Println("Streaming (| = chunk boundary):")

	cw := NewChunkWriter("|")
	for chunk := range stream.Chunks() {
		if chunk.Err != nil {
			t.Fatalf("stream error: %v", chunk.Err)
		}
		cw.Write(chunk)
	}
	cw.End()

	response, err := stream.Response()
	if err != nil {
		t.Fatalf("failed to get response: %v", err)
	}

	cw.PrintStats()
	fmt.Println("\nFull Response Info:")
	responseJSON, _ := json.MarshalIndent(response, "", "  ")
	fmt.Println(string(responseJSON))

	if response.Info == nil {
		t.Error("expected response info to be populated")
	}
	if response.Info.Duration == 0 {
		t.Error("expected non-zero duration")
	}
}
