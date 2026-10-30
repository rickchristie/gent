package gent

import (
	"errors"
	"sync"

	"github.com/rickchristie/gent/internal/buffer"
)

// UnsubscribeFunc cancels a subscription and discards its queued chunks. Repeated calls are safe.
type UnsubscribeFunc func()

// StreamSubscription receives live chunks. Err reports overflow or a delivery timeout separately
// from persistence errors. Close discards queued data; Done closes when its delivery worker stops.
type StreamSubscription struct {
	queue *buffer.Delivery[StreamChunk]
	close func(bool)
}

// Chunks returns owned chunks in sequence order. The channel closes when delivery stops.
func (s *StreamSubscription) Chunks() <-chan StreamChunk { return s.queue.Receive() }

// Err returns the delivery failure, or nil for normal closure. Read it after Done closes.
func (s *StreamSubscription) Err() error { return s.queue.Err() }

// Done closes after the delivery worker has stopped and released its queued data.
func (s *StreamSubscription) Done() <-chan struct{} { return s.queue.Done() }

// Close discards pending delivery and waits for its worker. Repeated calls are safe.
func (s *StreamSubscription) Close() { s.close(true) }

type streamFilter struct{ stream, topic string }

// streamHub sends to independent bounded queues. It never invokes application code.
type streamHub struct {
	mu          sync.Mutex
	subscribers map[*StreamSubscription]streamFilter
	closed      bool
}

func newStreamHub() *streamHub {
	return &streamHub{subscribers: make(map[*StreamSubscription]streamFilter)}
}

func (h *streamHub) subscribe(
	filter streamFilter, cfg DeliveryConfig,
) (*StreamSubscription, error) {
	cfg, err := deliveryConfig(cfg)
	if err != nil {
		return nil, err
	}
	sub := &StreamSubscription{queue: buffer.NewDelivery[StreamChunk](
		cfg.MaxEvents, cfg.MaxBytes, cfg.Timeout)}
	sub.close = func(discard bool) {
		h.mu.Lock()
		delete(h.subscribers, sub)
		h.mu.Unlock()
		if discard {
			sub.queue.Discard()
			<-sub.queue.Done()
			return
		}
		sub.queue.Close()
	}
	h.mu.Lock()
	if h.closed {
		sub.queue.Close()
	} else {
		h.subscribers[sub] = filter
	}
	h.mu.Unlock()
	go func() {
		<-sub.queue.Done()
		h.mu.Lock()
		delete(h.subscribers, sub)
		h.mu.Unlock()
	}()
	return sub, nil
}

func (h *streamHub) subscribeAll() (<-chan StreamChunk, UnsubscribeFunc) {
	sub := h.subscribeDefault(streamFilter{})
	return sub.Chunks(), sub.Close
}

func (h *streamHub) subscribeAllDraining() (<-chan StreamChunk, UnsubscribeFunc) {
	sub := h.subscribeDefault(streamFilter{})
	return sub.Chunks(), func() { sub.close(false) }
}

func (h *streamHub) subscribeToStream(id string) (<-chan StreamChunk, UnsubscribeFunc) {
	if id == "" {
		return nil, nil
	}
	sub := h.subscribeDefault(streamFilter{stream: id})
	return sub.Chunks(), sub.Close
}

func (h *streamHub) subscribeToTopic(id string) (<-chan StreamChunk, UnsubscribeFunc) {
	if id == "" {
		return nil, nil
	}
	sub := h.subscribeDefault(streamFilter{topic: id})
	return sub.Chunks(), sub.Close
}

func (h *streamHub) subscribeDefault(filter streamFilter) *StreamSubscription {
	sub, err := h.subscribe(filter, DeliveryConfig{})
	if err != nil {
		panic(err)
	}
	return sub
}

// SubscribeAllWithConfig exposes delivery limits and the reason for subscription closure.
func (ctx *ExecutionContext) SubscribeAllWithConfig(
	cfg DeliveryConfig,
) (*StreamSubscription, error) {
	return ctx.streamHub.subscribe(streamFilter{}, cfg)
}

// SubscribeToStreamWithConfig receives one stream with explicit limits. The ID must be nonempty.
func (ctx *ExecutionContext) SubscribeToStreamWithConfig(
	id string, cfg DeliveryConfig,
) (*StreamSubscription, error) {
	if id == "" {
		return nil, errors.New("gent: empty stream identity")
	}
	return ctx.streamHub.subscribe(streamFilter{stream: id}, cfg)
}

// SubscribeToTopicWithConfig receives one topic with explicit limits. The ID must be nonempty.
func (ctx *ExecutionContext) SubscribeToTopicWithConfig(
	id string, cfg DeliveryConfig,
) (*StreamSubscription, error) {
	if id == "" {
		return nil, errors.New("gent: empty topic identity")
	}
	return ctx.streamHub.subscribe(streamFilter{topic: id}, cfg)
}

func (h *streamHub) emit(chunk StreamChunk) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	size := chunkSize(chunk)
	for sub, filter := range h.subscribers {
		if filter.stream != "" && filter.stream != chunk.StreamId {
			continue
		}
		if filter.topic != "" && filter.topic != chunk.StreamTopicId {
			continue
		}
		owned := cloneStreamChunk(chunk)
		if chunk.Err != nil {
			owned.Err = SaveError(chunk.Err)
		}
		if !sub.queue.Send(owned, size) {
			delete(h.subscribers, sub)
		}
	}
}

// close drains queued chunks with a finite timeout. Unsubscribe discards them when a consumer
// leaves, so no delivery worker can remain blocked indefinitely after an execution ends.
func (h *streamHub) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for sub := range h.subscribers {
		sub.queue.Close()
	}
	clear(h.subscribers)
}
