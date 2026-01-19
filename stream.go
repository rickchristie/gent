package gent

import (
	"context"
	"sync"
	"time"

	"github.com/tmc/langchaingo/llms"
)

// streamBuffer implements Stream with an unbounded internal buffer.
// It guarantees that Send() never blocks, even when:
//   - There is no listener on the channel
//   - The listener is processing chunks slowly
//
// Internally it uses a slice-based queue protected by a mutex.
// A background goroutine drains the queue to the output channel.
type streamBuffer struct {
	// Output channel for consumers
	chunks chan StreamChunk

	// Internal queue protected by mutex
	mu    sync.Mutex
	queue []StreamChunk
	cond  *sync.Cond

	// State tracking
	closed   bool
	closeErr error

	// Final response (populated when complete)
	responseMu   sync.Mutex
	response     *ContentResponse
	responseErr  error
	responseDone chan struct{}

	// Content accumulator for building final response
	contentAccum   []byte
	reasoningAccum []byte
}

// newStreamBuffer creates a new streaming buffer.
// The returned stream is ready to receive chunks via Send().
func newStreamBuffer() *streamBuffer {
	s := &streamBuffer{
		chunks:       make(chan StreamChunk, 1), // Small buffer for efficiency
		queue:        make([]StreamChunk, 0, 64),
		responseDone: make(chan struct{}),
	}
	s.cond = sync.NewCond(&s.mu)

	// Start the drain goroutine
	go s.drainLoop()

	return s
}

// drainLoop continuously moves chunks from the internal queue to the output channel.
// It runs until the stream is closed and all queued chunks are drained.
func (s *streamBuffer) drainLoop() {
	for {
		chunk, ok := s.dequeue()
		if !ok {
			// Queue is empty and stream is closed
			close(s.chunks)
			return
		}

		// Send to output channel (may block here, which is fine)
		s.chunks <- chunk
	}
}

// dequeue removes and returns the next chunk from the queue.
// It blocks until a chunk is available or the stream is closed.
// Returns (chunk, true) if a chunk was dequeued, (zero, false) if closed and empty.
func (s *streamBuffer) dequeue() (StreamChunk, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Wait until there's something in the queue or we're closed
	for len(s.queue) == 0 && !s.closed {
		s.cond.Wait()
	}

	// If closed and queue is empty, we're done
	if len(s.queue) == 0 {
		return StreamChunk{}, false
	}

	// Pop from front of queue
	chunk := s.queue[0]
	s.queue = s.queue[1:]

	return chunk, true
}

// Send adds a chunk to the stream. This method NEVER blocks.
// It's safe to call from any goroutine.
func (s *streamBuffer) Send(chunk StreamChunk) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return // Silently ignore sends after close
	}

	// Accumulate content for final response
	if chunk.Content != "" {
		s.contentAccum = append(s.contentAccum, chunk.Content...)
	}
	if chunk.ReasoningContent != "" {
		s.reasoningAccum = append(s.reasoningAccum, chunk.ReasoningContent...)
	}

	// Add to queue and signal
	s.queue = append(s.queue, chunk)
	s.cond.Signal()
}

// SendContent is a convenience method to send a content-only chunk.
func (s *streamBuffer) SendContent(content string) {
	s.Send(StreamChunk{Content: content})
}

// SendReasoning is a convenience method to send a reasoning-only chunk.
func (s *streamBuffer) SendReasoning(reasoning string) {
	s.Send(StreamChunk{ReasoningContent: reasoning})
}

// SendError sends an error chunk. This does NOT close the stream.
func (s *streamBuffer) SendError(err error) {
	s.Send(StreamChunk{Err: err})
}

// Complete marks the stream as complete with the final response.
// This closes the stream and makes Response() return.
func (s *streamBuffer) Complete(response *ContentResponse, err error) {
	s.mu.Lock()

	if s.closed {
		s.mu.Unlock()
		return
	}

	s.closed = true
	s.closeErr = err

	// If we have an error, add it as a final chunk
	if err != nil {
		s.queue = append(s.queue, StreamChunk{Err: err})
	}

	s.cond.Signal()
	s.mu.Unlock()

	// Set the final response
	s.responseMu.Lock()
	s.response = response
	s.responseErr = err
	close(s.responseDone)
	s.responseMu.Unlock()
}

// CompleteWithGenerationInfo completes the stream using accumulated content
// and the provided generation info.
func (s *streamBuffer) CompleteWithGenerationInfo(info *GenerationInfo, err error) {
	s.mu.Lock()
	content := string(s.contentAccum)
	reasoning := string(s.reasoningAccum)
	s.mu.Unlock()

	var response *ContentResponse
	if err == nil {
		response = &ContentResponse{
			Choices: []*ContentChoice{
				{
					Content:          content,
					ReasoningContent: reasoning,
				},
			},
			Info: info,
		}
	}

	s.Complete(response, err)
}

// Chunks implements Stream.Chunks.
func (s *streamBuffer) Chunks() <-chan StreamChunk {
	return s.chunks
}

// Response implements Stream.Response.
// It blocks until the stream is complete.
func (s *streamBuffer) Response() (*ContentResponse, error) {
	<-s.responseDone
	s.responseMu.Lock()
	defer s.responseMu.Unlock()
	return s.response, s.responseErr
}

// Close implements Stream.Close.
func (s *streamBuffer) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.cond.Signal()
	s.mu.Unlock()

	// Also signal response done if not already done
	s.responseMu.Lock()
	select {
	case <-s.responseDone:
		// Already closed
	default:
		close(s.responseDone)
	}
	s.responseMu.Unlock()
}

// AccumulatedContent returns the content accumulated so far.
func (s *streamBuffer) AccumulatedContent() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.contentAccum)
}

// AccumulatedReasoning returns the reasoning content accumulated so far.
func (s *streamBuffer) AccumulatedReasoning() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.reasoningAccum)
}

// Compile-time check that streamBuffer implements Stream.
var _ Stream = (*streamBuffer)(nil)

// StreamingCallbackAdapter creates llms.CallOption callbacks that feed into a streamBuffer.
// It returns the callbacks and the stream buffer.
//
// This is useful for adapting LangChainGo's callback-based streaming to gent's channel-based
// streaming.
func StreamingCallbackAdapter() (contentCallback, reasoningCallback llms.CallOption, stream *streamBuffer) {
	stream = newStreamBuffer()

	contentCallback = llms.WithStreamingFunc(func(ctx context.Context, chunk []byte) error {
		stream.SendContent(string(chunk))
		return nil
	})

	reasoningCallback = llms.WithStreamingReasoningFunc(
		func(ctx context.Context, reasoningChunk, contentChunk []byte) error {
			if len(reasoningChunk) > 0 {
				stream.SendReasoning(string(reasoningChunk))
			}
			if len(contentChunk) > 0 {
				stream.SendContent(string(contentChunk))
			}
			return nil
		},
	)

	return contentCallback, reasoningCallback, stream
}

// SimpleStreamingCallback creates a simple streaming callback that only handles content.
// Use StreamingCallbackAdapter for full reasoning support.
func SimpleStreamingCallback() (llms.CallOption, *streamBuffer) {
	stream := newStreamBuffer()

	callback := llms.WithStreamingFunc(func(ctx context.Context, chunk []byte) error {
		stream.SendContent(string(chunk))
		return nil
	})

	return callback, stream
}

// StreamWithDuration is a helper to measure streaming duration.
// It wraps a stream buffer to track start time.
type StreamWithDuration struct {
	*streamBuffer
	startTime time.Time
}

// NewStreamWithDuration creates a stream buffer that tracks duration.
func NewStreamWithDuration() *StreamWithDuration {
	return &StreamWithDuration{
		streamBuffer: newStreamBuffer(),
		startTime:    time.Now(),
	}
}

// Duration returns the time elapsed since the stream started.
func (s *StreamWithDuration) Duration() time.Duration {
	return time.Since(s.startTime)
}
