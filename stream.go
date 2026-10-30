package gent

import (
	"context"
	"errors"
	"sync"
	"time"
)

// streamCodecs owns response copies at the producer/consumer boundary. Provider metadata must
// use the built-in wire types, as do provider JSON values.
var streamCodecs = NewCodecRegistry()

// streamBuffer implements Stream with an unbounded internal buffer.
// It guarantees that Send() never blocks, even when:
//   - There is no listener on the channel
//   - The listener is processing chunks slowly
//
// Internally it uses a slice-based queue protected by a mutex. The drain goroutine starts
// lazily when Chunks is called, so callers that only need Response don't leak a blocked
// sender goroutine.
type streamBuffer struct {
	// Output channel for consumers
	chunks          chan StreamChunk
	drainOnce       sync.Once
	drainCancel     chan struct{}
	drainCancelOnce sync.Once
	drainDone       chan struct{}

	// Internal queue protected by mutex
	mu        sync.Mutex
	queue     []StreamChunk
	cond      *sync.Cond
	capacity  int
	stopWatch func() bool

	// State tracking
	closed         bool
	closeRequested bool
	onClose        func()
	closeErr       error

	// Final response (populated when complete)
	responseMu   sync.Mutex
	response     *ContentResponse
	responseErr  error
	responseDone chan struct{}

	// Content accumulator for building final response
	metaAccum metaAccumulator
}

// newStreamBuffer creates a new streaming buffer.
// The returned stream is ready to receive chunks via Send().
func newStreamBuffer() *streamBuffer {
	return newStreamBufferWithClose(nil)
}

// newStreamBufferWithClose creates a new streaming buffer with a hook that runs when the
// consumer closes the stream before it completes.
func newStreamBufferWithClose(onClose func()) *streamBuffer {
	s := &streamBuffer{
		chunks:      make(chan StreamChunk, 1), // Small buffer for efficiency
		drainCancel: make(chan struct{}),
		drainDone:   make(chan struct{}),
		queue:       make([]StreamChunk, 0, 64),
		onClose:     onClose,

		responseDone: make(chan struct{}),
	}
	s.cond = sync.NewCond(&s.mu)

	return s
}

// drainLoop continuously moves chunks from the internal queue to the output channel.
// It runs until the stream is complete and all queued chunks are drained, or until the
// consumer closes the stream and queued chunks should be discarded.
func (s *streamBuffer) drainLoop() {
	defer close(s.drainDone)
	defer close(s.chunks)

	for {
		chunk, ok := s.dequeue()
		if !ok {
			// Queue is empty and stream is closed
			return
		}

		if !s.sendDrainedChunk(chunk) {
			return
		}
	}
}

func (s *streamBuffer) sendDrainedChunk(chunk StreamChunk) bool {
	select {
	case <-s.drainCancel:
		return false
	default:
	}

	select {
	case s.chunks <- chunk:
		return true
	default:
	}

	// Close must be able to stop this goroutine while the consumer is no longer reading.
	select {
	case <-s.drainCancel:
		return false
	case s.chunks <- chunk:
		return true
	}
}

// dequeue removes and returns the next chunk from the queue.
// It blocks until a chunk is available or the stream is closed.
// Returns (chunk, true) if a chunk was dequeued, (zero, false) if closed and empty.
func (s *streamBuffer) dequeue() (StreamChunk, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Wait until there's something in the queue or we're closed
	for len(s.queue) == 0 && !s.closed && !s.closeRequested {
		s.cond.Wait()
	}

	// If closed and queue is empty, we're done
	if len(s.queue) == 0 {
		return StreamChunk{}, false
	}

	// Pop from front of queue
	chunk := s.queue[0]
	s.queue[0] = StreamChunk{} // Release metadata and content after delivery.
	s.queue = s.queue[1:]
	if len(s.queue) == 0 {
		s.queue = nil
	}
	s.cond.Broadcast()

	return chunk, true
}

// Send adds a chunk. Bounded provider streams apply backpressure until space is available or
// the stream is closed; convenience streams created without a capacity do not block.
func (s *streamBuffer) Send(chunk StreamChunk) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for s.capacity > 0 && len(s.queue) >= s.capacity && !s.closed && !s.closeRequested {
		s.cond.Wait()
	}

	if s.closed || s.closeRequested {
		return false // Silently ignore sends after close
	}

	chunk = cloneStreamChunk(chunk)
	s.metaAccum.add(chunk.MetaContent)

	// Add to queue and signal
	s.queue = append(s.queue, chunk)
	s.cond.Signal()
	return true
}

// SendMeta sends one provisional text delta. The buffer owns a copy of the entry.
func (s *streamBuffer) SendMeta(content *MetaContent) bool {
	return s.Send(StreamChunk{MetaContent: []*MetaContent{content}})
}

// SendError sends an error chunk. This does NOT close the stream.
func (s *streamBuffer) SendError(err error) bool {
	return s.Send(StreamChunk{Err: err})
}

// Complete marks the stream as complete with the final response.
// This closes the stream and makes Response() return.
func (s *streamBuffer) Complete(response *ContentResponse, err error) {
	s.TryComplete(response, err)
}

// TryComplete marks the stream as complete and reports whether it completed the stream.
// It returns false after a previous producer completion. Consumer Close only requests stopping.
func (s *streamBuffer) TryComplete(response *ContentResponse, err error) bool {
	s.mu.Lock()

	if s.closed {
		s.mu.Unlock()
		return false
	}

	s.closed = true
	if s.stopWatch != nil {
		s.stopWatch()
	}
	s.onClose = nil
	owned, copyErr := cloneOwned(streamCodecs, response)
	if copyErr != nil {
		err = errors.Join(err, copyErr)
	}
	s.closeErr = err

	// If we have an error, add it as a final chunk
	if err != nil {
		s.queue = append(s.queue, StreamChunk{Err: err})
	}

	// Set the final response
	s.responseMu.Lock()
	s.response = owned
	s.responseErr = err
	close(s.responseDone)
	s.responseMu.Unlock()

	s.cond.Broadcast()
	s.mu.Unlock()
	return true
}

// CompleteWithGenerationInfo completes the stream using accumulated content
// and the provided generation info.
func (s *streamBuffer) CompleteWithGenerationInfo(info *GenerationInfo, err error) {
	s.mu.Lock()
	metadata := s.metaAccum.snapshot()
	s.mu.Unlock()

	var response *ContentResponse
	if err == nil {
		response = &ContentResponse{Choices: []*ContentChoice{{Completion: ModelCompletion{
			Status: ModelStatusAnswer,
		},
			CandidateContent: MetaParts(metadata, MetaContentIntermediary),
			MetaContent:      metadata,
		}}, Info: info}
	}

	s.Complete(response, err)
}

// Chunks implements Stream.Chunks.
func (s *streamBuffer) Chunks() <-chan StreamChunk {
	// Start draining lazily so Response-only callers don't leave a goroutine blocked on
	// the output channel. Chunks are still queued and replayed if Chunks is called later.
	s.drainOnce.Do(func() {
		go s.drainLoop()
	})
	return s.chunks
}

// Response implements Stream.Response.
// It blocks until the stream is complete.
func (s *streamBuffer) Response() (*ContentResponse, error) {
	<-s.responseDone
	s.responseMu.Lock()
	defer s.responseMu.Unlock()
	response, err := cloneOwned(streamCodecs, s.response)
	if err != nil {
		return nil, errors.Join(s.responseErr, err)
	}
	return response, s.responseErr
}

// Close implements Stream.Close.
func (s *streamBuffer) Close() {
	s.mu.Lock()
	wasClosed := s.closed || s.closeRequested
	s.closeRequested = true
	s.queue = nil
	s.closeDrain()
	onClose := s.onClose
	s.onClose = nil
	s.cond.Broadcast()
	s.mu.Unlock()

	if !wasClosed && onClose != nil {
		onClose()
	}
	// Only producer completion closes responseDone. Cancellation is a request, not evidence
	// that the provider stopped or that its final token usage has been accounted for.
}

func (s *streamBuffer) closeDrain() {
	s.drainCancelOnce.Do(func() {
		close(s.drainCancel)
	})
}

// AccumulatedMetaContent returns an owned snapshot of the provisional text.
func (s *streamBuffer) AccumulatedMetaContent() []*MetaContent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.metaAccum.snapshot()
}

var _ Stream = (*streamBuffer)(nil)

// NewCompletedStream creates a Stream that is already complete with the given
// response. The stream emits each non-empty text channel then closes.
// This is used by mock models and non-streaming callers that need to satisfy the
// [Model] interface without actual streaming.
func NewCompletedStream(resp *ContentResponse, err error) Stream {
	s := newStreamBuffer()
	if resp != nil && len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		for _, item := range choice.MetaContent {
			s.SendMeta(item)
		}
		if len(choice.CandidateContent) > 0 &&
			len(MetaParts(choice.MetaContent, MetaContentIntermediary)) == 0 {
			id := ""
			if choice.CandidateSource != nil && len(choice.CandidateSource.MessageIds) == 1 {
				id = choice.CandidateSource.MessageIds[0]
			}
			s.SendMeta(&MetaContent{Type: MetaContentIntermediary,
				Content: choice.CandidateContent, MessageId: id})
		}
	}
	s.Complete(resp, err)
	return s
}

// StreamWithDuration is a helper to measure streaming duration.
// It wraps a stream buffer to track start time.
type StreamWithDuration struct {
	*streamBuffer
	startTime time.Time
}

// NewStreamWithDuration creates a stream buffer that tracks duration.
func NewStreamWithDuration() *StreamWithDuration {
	return NewStreamWithDurationWithClose(nil)
}

// NewStreamWithDurationWithClose creates a stream buffer that tracks duration and calls
// onClose when the consumer closes the stream before it completes.
func NewStreamWithDurationWithClose(onClose func()) *StreamWithDuration {
	return &StreamWithDuration{
		streamBuffer: newStreamBufferWithClose(onClose),
		startTime:    time.Now(),
	}
}

// NewBoundedStreamWithDuration is intended for provider adapters that produce in a goroutine.
// The framework drains Chunks while accepting evidence. Cancellation releases a blocked Send;
// Response still waits for the adapter to finish and report its partial output and usage.
func NewBoundedStreamWithDuration(
	ctx context.Context, capacity int, onClose func(),
) (*StreamWithDuration, error) {
	if capacity < 1 {
		return nil, errors.New("gent: stream capacity must be positive")
	}
	stream := NewStreamWithDurationWithClose(onClose)
	stream.capacity = capacity
	stream.stopWatch = context.AfterFunc(ctx, stream.Close)
	return stream, nil
}

// Duration returns the time elapsed since the stream started.
func (s *StreamWithDuration) Duration() time.Duration {
	return time.Since(s.startTime)
}
