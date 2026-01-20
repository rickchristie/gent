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
	"time"

	"github.com/rickchristie/gent"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
)

// StreamOutputConfig configures how streaming output is displayed.
type StreamOutputConfig struct {
	// ShowChunkBoundaries shows "|" between chunks (useful for tests).
	ShowChunkBoundaries bool
	// EscapeNewlines replaces \n with \\n for visibility (useful for tests).
	EscapeNewlines bool
	// ShowStats prints chunk statistics at the end.
	ShowStats bool
}

// TestOutputConfig returns a config suitable for go test (boundaries, escaped newlines, stats).
func TestOutputConfig() StreamOutputConfig {
	return StreamOutputConfig{
		ShowChunkBoundaries: true,
		EscapeNewlines:      true,
		ShowStats:           true,
	}
}

// InteractiveOutputConfig returns a config for interactive CLI (natural streaming output).
func InteractiveOutputConfig() StreamOutputConfig {
	return StreamOutputConfig{
		ShowChunkBoundaries: false,
		EscapeNewlines:      false,
		ShowStats:           true,
	}
}

// ChunkWriter writes streaming chunks with configurable output format.
// It flushes after each write for real-time display.
type ChunkWriter struct {
	w            io.Writer
	bw           *bufio.Writer
	config       StreamOutputConfig
	contentLen   int
	chunkCount   int
	reasoningLen int
}

// NewChunkWriter creates a ChunkWriter with test output config.
func NewChunkWriter() *ChunkWriter {
	return NewChunkWriterWithConfig(os.Stdout, TestOutputConfig())
}

// NewChunkWriterTo creates a ChunkWriter with test output config writing to w.
func NewChunkWriterTo(w io.Writer) *ChunkWriter {
	return NewChunkWriterWithConfig(w, TestOutputConfig())
}

// NewChunkWriterWithConfig creates a ChunkWriter with the given config.
func NewChunkWriterWithConfig(w io.Writer, config StreamOutputConfig) *ChunkWriter {
	return &ChunkWriter{
		w:      w,
		bw:     bufio.NewWriter(w),
		config: config,
	}
}

// Write writes a chunk and flushes immediately.
func (w *ChunkWriter) Write(chunk gent.StreamChunk) {
	if chunk.Content != "" {
		content := chunk.Content
		if w.config.EscapeNewlines {
			content = strings.ReplaceAll(content, "\n", "\\n")
		}
		if w.config.ShowChunkBoundaries {
			fmt.Fprintf(w.bw, "|%s", content)
		} else {
			fmt.Fprint(w.bw, content)
		}
		w.flush()
		w.contentLen += len(chunk.Content)
		w.chunkCount++
	}
	if chunk.ReasoningContent != "" {
		content := chunk.ReasoningContent
		if w.config.EscapeNewlines {
			content = strings.ReplaceAll(content, "\n", "\\n")
		}
		if w.config.ShowChunkBoundaries {
			fmt.Fprintf(w.bw, "|[R:%s]", content)
		} else {
			fmt.Fprintf(w.bw, "[Reasoning: %s]", content)
		}
		w.flush()
		w.reasoningLen += len(chunk.ReasoningContent)
		w.chunkCount++
	}
}

// flush flushes the buffer and syncs if writing to a file.
func (w *ChunkWriter) flush() {
	w.bw.Flush()
	if f, ok := w.w.(*os.File); ok {
		f.Sync()
	}
}

// End prints the final boundary (if configured) and newline.
func (w *ChunkWriter) End() {
	if w.config.ShowChunkBoundaries {
		fmt.Fprint(w.bw, "|")
	}
	fmt.Fprintln(w.bw)
	w.flush()
}

// Stats returns chunk count, content length, and reasoning length.
func (w *ChunkWriter) Stats() (chunks, contentLen, reasoningLen int) {
	return w.chunkCount, w.contentLen, w.reasoningLen
}

// PrintStats prints statistics about the streamed content (if ShowStats is enabled).
func (w *ChunkWriter) PrintStats() {
	if !w.config.ShowStats {
		return
	}
	fmt.Fprintf(w.bw, "Chunks: %d, Content: %d chars", w.chunkCount, w.contentLen)
	if w.reasoningLen > 0 {
		fmt.Fprintf(w.bw, ", Reasoning: %d chars", w.reasoningLen)
	}
	if w.chunkCount > 0 {
		fmt.Fprintf(w.bw, ", Avg: %.1f chars/chunk", float64(w.contentLen)/float64(w.chunkCount))
	}
	fmt.Fprintln(w.bw)
	w.flush()
}

// StreamTestCase represents a streaming test that can be run.
type StreamTestCase struct {
	Name        string
	Description string
	Run         func(ctx context.Context, w io.Writer, config StreamOutputConfig) error
}

// GetStreamingTestCases returns all available streaming test cases.
func GetStreamingTestCases() []StreamTestCase {
	return []StreamTestCase{
		{
			Name:        "Basic",
			Description: "Tests basic streaming functionality with a simple prompt",
			Run:         RunStreamingBasic,
		},
		{
			Name:        "WithReasoning",
			Description: "Tests streaming with a reasoning model",
			Run:         RunStreamingWithReasoning,
		},
		{
			Name:        "SlowConsumer",
			Description: "Tests that streaming works with a slow consumer",
			Run:         RunStreamingSlowConsumer,
		},
		{
			Name:        "NoListener",
			Description: "Tests stream completion without reading chunks",
			Run:         RunStreamingNoListener,
		},
		{
			Name:        "Cancellation",
			Description: "Tests context cancellation stops the stream",
			Run:         RunStreamingCancellation,
		},
		{
			Name:        "Concurrent",
			Description: "Tests multiple concurrent streams",
			Run:         RunStreamingConcurrent,
		},
		{
			Name:        "ResponseInfo",
			Description: "Tests response info is correctly populated",
			Run:         RunStreamingResponseInfo,
		},
	}
}

// createXAIModel creates an xAI model for testing, returning an error if API key is missing.
func createXAIModel(modelName string) (gent.StreamingModel, error) {
	apiKey := os.Getenv("GENT_TEST_XAI_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("GENT_TEST_XAI_KEY environment variable not set")
	}

	llm, err := openai.New(
		openai.WithToken(apiKey),
		openai.WithBaseURL("https://api.x.ai/v1"),
		openai.WithModel(modelName),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create xAI LLM: %w", err)
	}

	return NewLCGWrapper(llm).WithModelName(modelName), nil
}

// RunStreamingBasic tests basic streaming functionality with a simple prompt.
func RunStreamingBasic(ctx context.Context, w io.Writer, config StreamOutputConfig) error {
	model, err := createXAIModel("grok-3-fast")
	if err != nil {
		return err
	}

	stream, err := model.GenerateContentStream(ctx, nil, "", "", []llms.MessageContent{
		llms.TextParts(
			llms.ChatMessageTypeHuman,
			"Write 3 paragraphs of a creative dark fantasy story",
		),
	})
	if err != nil {
		return fmt.Errorf("failed to start stream: %w", err)
	}

	fmt.Fprintln(w, "\n=== Basic Streaming Test ===")
	if config.ShowChunkBoundaries {
		fmt.Fprintln(w, "Streaming response (| = chunk boundary):")
	}

	cw := NewChunkWriterWithConfig(w, config)
	for chunk := range stream.Chunks() {
		if chunk.Err != nil {
			return fmt.Errorf("stream error: %w", chunk.Err)
		}
		cw.Write(chunk)
	}
	cw.End()

	response, err := stream.Response()
	if err != nil {
		return fmt.Errorf("failed to get response: %w", err)
	}

	cw.PrintStats()
	if config.ShowStats && response.Info != nil {
		fmt.Fprintf(w, "Input tokens: %d, Output tokens: %d\n",
			response.Info.InputTokens, response.Info.OutputTokens)
	}

	chunks, contentLen, _ := cw.Stats()
	if chunks == 0 || contentLen == 0 {
		return fmt.Errorf("expected non-empty streamed content")
	}

	return nil
}

// RunStreamingWithReasoning tests streaming with a reasoning model.
func RunStreamingWithReasoning(ctx context.Context, w io.Writer, config StreamOutputConfig) error {
	model, err := createXAIModel("grok-4-1-fast-reasoning")
	if err != nil {
		return err
	}

	stream, err := model.GenerateContentStream(ctx, nil, "", "", []llms.MessageContent{
		llms.TextParts(
			llms.ChatMessageTypeHuman,
			"What is 15 * 17? Show your reasoning step by step.",
		),
	})
	if err != nil {
		return fmt.Errorf("failed to start stream: %w", err)
	}

	fmt.Fprintln(w, "\n=== Reasoning Streaming Test ===")
	if config.ShowChunkBoundaries {
		fmt.Fprintln(w, "Streaming (| = chunk, [R:...] = reasoning):")
	}

	cw := NewChunkWriterWithConfig(w, config)
	for chunk := range stream.Chunks() {
		if chunk.Err != nil {
			return fmt.Errorf("stream error: %w", chunk.Err)
		}
		cw.Write(chunk)
	}
	cw.End()

	response, err := stream.Response()
	if err != nil {
		return fmt.Errorf("failed to get response: %w", err)
	}

	cw.PrintStats()
	if config.ShowStats && response.Info != nil {
		fmt.Fprintf(w, "Input: %d, Output: %d, Reasoning: %d tokens\n",
			response.Info.InputTokens, response.Info.OutputTokens, response.Info.ReasoningTokens)
	}

	_, contentLen, _ := cw.Stats()
	if contentLen == 0 {
		return fmt.Errorf("expected non-empty content")
	}

	return nil
}

// RunStreamingSlowConsumer tests that streaming works with a slow consumer.
func RunStreamingSlowConsumer(ctx context.Context, w io.Writer, config StreamOutputConfig) error {
	model, err := createXAIModel("grok-3-fast")
	if err != nil {
		return err
	}

	stream, err := model.GenerateContentStream(ctx, nil, "", "", []llms.MessageContent{
		llms.TextParts(
			llms.ChatMessageTypeHuman,
			"Write a short poem about Go programming (4 lines).",
		),
	})
	if err != nil {
		return fmt.Errorf("failed to start stream: %w", err)
	}

	fmt.Fprintln(w, "\n=== Slow Consumer Test ===")
	if config.ShowChunkBoundaries {
		fmt.Fprintln(w, "Processing with 50ms delay per chunk (| = chunk boundary):")
	} else {
		fmt.Fprintln(w, "Processing with 50ms delay per chunk:")
	}

	cw := NewChunkWriterWithConfig(w, config)
	startTime := time.Now()

	for chunk := range stream.Chunks() {
		if chunk.Err != nil {
			return fmt.Errorf("stream error: %w", chunk.Err)
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
		return fmt.Errorf("failed to get response: %w", err)
	}

	cw.PrintStats()
	if config.ShowStats {
		fmt.Fprintf(w, "Consumer elapsed: %v, API duration: %v\n", elapsed, response.Info.Duration)
	}

	_, contentLen, _ := cw.Stats()
	if contentLen == 0 {
		return fmt.Errorf("expected non-empty content")
	}

	return nil
}

// RunStreamingNoListener tests stream completion without reading chunks.
func RunStreamingNoListener(ctx context.Context, w io.Writer, _ StreamOutputConfig) error {
	model, err := createXAIModel("grok-3-fast")
	if err != nil {
		return err
	}

	stream, err := model.GenerateContentStream(ctx, nil, "", "", []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeHuman, "Say hello in one word."),
	})
	if err != nil {
		return fmt.Errorf("failed to start stream: %w", err)
	}

	fmt.Fprintln(w, "\n=== No Listener Test ===")
	fmt.Fprintln(w, "Waiting for response without reading chunks...")

	response, err := stream.Response()
	if err != nil {
		return fmt.Errorf("failed to get response: %w", err)
	}

	fmt.Fprintf(w, "Response received: %q\n", response.Choices[0].Content)

	if len(response.Choices) == 0 || response.Choices[0].Content == "" {
		return fmt.Errorf("expected non-empty response")
	}

	return nil
}

// RunStreamingCancellation tests context cancellation stops the stream.
func RunStreamingCancellation(ctx context.Context, w io.Writer, config StreamOutputConfig) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	model, err := createXAIModel("grok-3-fast")
	if err != nil {
		return err
	}

	stream, err := model.GenerateContentStream(ctx, nil, "", "", []llms.MessageContent{
		llms.TextParts(
			llms.ChatMessageTypeHuman,
			"Write a very long story about a programmer. Make it at least 500 words.",
		),
	})
	if err != nil {
		return fmt.Errorf("failed to start stream: %w", err)
	}

	fmt.Fprintln(w, "\n=== Cancellation Test ===")
	if config.ShowChunkBoundaries {
		fmt.Fprintln(w, "Starting stream, will cancel after 5 chunks (| = chunk boundary):")
	} else {
		fmt.Fprintln(w, "Starting stream, will cancel after 5 chunks:")
	}

	cw := NewChunkWriterWithConfig(w, config)
	cancelled := false

	for chunk := range stream.Chunks() {
		if chunk.Err != nil {
			fmt.Fprintf(w, "\nStream error (expected): %v\n", chunk.Err)
			break
		}
		cw.Write(chunk)
		chunks, _, _ := cw.Stats()
		if chunks >= 5 && !cancelled {
			if config.ShowChunkBoundaries {
				fmt.Fprint(w, "| [CANCELLING]")
			} else {
				fmt.Fprint(w, " [CANCELLING]")
			}
			cancel()
			cancelled = true
		}
	}
	cw.End()

	_, err = stream.Response()
	if err != nil {
		fmt.Fprintf(w, "Response error (expected): %v\n", err)
	}

	cw.PrintStats()
	return nil
}

// RunStreamingConcurrent tests multiple concurrent streams.
func RunStreamingConcurrent(ctx context.Context, w io.Writer, _ StreamOutputConfig) error {
	model, err := createXAIModel("grok-3-fast")
	if err != nil {
		return err
	}

	fmt.Fprintln(w, "\n=== Concurrent Streaming Test ===")
	fmt.Fprintln(w, "Starting 3 concurrent streams...")

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

			stream, err := model.GenerateContentStream(ctx, nil, "", "", []llms.MessageContent{
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

	var errs []error
	for i, r := range results {
		if r.err != nil {
			errs = append(errs, fmt.Errorf("stream %d error: %w", i, r.err))
		} else {
			fmt.Fprintf(w, "Stream %d: %q (%d chunks)\n", i, strings.TrimSpace(r.content), r.chunks)
		}
	}

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// RunStreamingResponseInfo tests response info is correctly populated.
func RunStreamingResponseInfo(ctx context.Context, w io.Writer, config StreamOutputConfig) error {
	model, err := createXAIModel("grok-3-fast")
	if err != nil {
		return err
	}

	stream, err := model.GenerateContentStream(ctx, nil, "", "", []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeHuman, "What is 2+2?"),
	})
	if err != nil {
		return fmt.Errorf("failed to start stream: %w", err)
	}

	fmt.Fprintln(w, "\n=== Response Info Test ===")
	if config.ShowChunkBoundaries {
		fmt.Fprintln(w, "Streaming (| = chunk boundary):")
	}

	cw := NewChunkWriterWithConfig(w, config)
	for chunk := range stream.Chunks() {
		if chunk.Err != nil {
			return fmt.Errorf("stream error: %w", chunk.Err)
		}
		cw.Write(chunk)
	}
	cw.End()

	response, err := stream.Response()
	if err != nil {
		return fmt.Errorf("failed to get response: %w", err)
	}

	cw.PrintStats()
	if config.ShowStats {
		fmt.Fprintln(w, "\nFull Response Info:")
		responseJSON, _ := json.MarshalIndent(response, "", "  ")
		fmt.Fprintln(w, string(responseJSON))
	}

	if response.Info == nil {
		return fmt.Errorf("expected response info to be populated")
	}
	if response.Info.Duration == 0 {
		return fmt.Errorf("expected non-zero duration")
	}

	return nil
}
