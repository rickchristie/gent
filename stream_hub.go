package gent

import (
	"sync"

	"github.com/rickchristie/gent/internal/buffer"
)

// UnsubscribeFunc is a function that cancels a stream subscription.
// After calling, the subscription channel will be closed and no more chunks
// will be delivered. Safe to call multiple times.
type UnsubscribeFunc func()

// streamSubscription represents a single subscription to the stream hub.
type streamSubscription struct {
	id     uint64
	buffer *buffer.Unbounded[StreamChunk]
}

// streamHub manages stream subscriptions and chunk distribution.
// All methods are concurrent-safe.
type streamHub struct {
	mu sync.RWMutex

	// Subscription channels (unbounded buffers)
	allSubscribers []*streamSubscription
	byStreamId     map[string][]*streamSubscription
	byTopicId      map[string][]*streamSubscription

	// State
	closed bool

	// Subscription ID counter
	nextId uint64
}

// newStreamHub creates a new streamHub.
func newStreamHub() *streamHub {
	return &streamHub{
		byStreamId: make(map[string][]*streamSubscription),
		byTopicId:  make(map[string][]*streamSubscription),
	}
}

// subscribeAll creates a subscription that receives all chunks.
// Returns a channel and an unsubscribe function.
func (h *streamHub) subscribeAll() (<-chan StreamChunk, UnsubscribeFunc) {
	return h.subscribeAllWithMode(true)
}

func (h *streamHub) subscribeAllDraining() (<-chan StreamChunk, UnsubscribeFunc) {
	return h.subscribeAllWithMode(false)
}

func (h *streamHub) subscribeAllWithMode(
	discardOnUnsubscribe bool,
) (<-chan StreamChunk, UnsubscribeFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		// Return closed channel
		ch := make(chan StreamChunk)
		close(ch)
		return ch, func() {}
	}

	sub := &streamSubscription{
		id:     h.nextId,
		buffer: buffer.NewUnbounded[StreamChunk](),
	}
	h.nextId++
	h.allSubscribers = append(h.allSubscribers, sub)

	unsubscribe := func() {
		h.unsubscribeAll(sub, discardOnUnsubscribe)
	}

	return sub.buffer.Receive(), unsubscribe
}

// unsubscribeAll removes a subscription from allSubscribers.
func (h *streamHub) unsubscribeAll(sub *streamSubscription, discard bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	closeSubscriptionBuffer(sub, discard)

	for i, s := range h.allSubscribers {
		if s.id == sub.id {
			h.allSubscribers = append(h.allSubscribers[:i], h.allSubscribers[i+1:]...)
			return
		}
	}
}

// subscribeToStream creates a subscription for a specific streamId.
// Returns (nil, nil) if streamId is empty.
func (h *streamHub) subscribeToStream(streamId string) (<-chan StreamChunk, UnsubscribeFunc) {
	if streamId == "" {
		return nil, nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		ch := make(chan StreamChunk)
		close(ch)
		return ch, func() {}
	}

	sub := &streamSubscription{
		id:     h.nextId,
		buffer: buffer.NewUnbounded[StreamChunk](),
	}
	h.nextId++
	h.byStreamId[streamId] = append(h.byStreamId[streamId], sub)

	unsubscribe := func() {
		h.unsubscribeFromStream(streamId, sub, true)
	}

	return sub.buffer.Receive(), unsubscribe
}

// unsubscribeFromStream removes a subscription from byStreamId.
func (h *streamHub) unsubscribeFromStream(
	streamId string,
	sub *streamSubscription,
	discard bool,
) {
	h.mu.Lock()
	defer h.mu.Unlock()

	closeSubscriptionBuffer(sub, discard)

	subs := h.byStreamId[streamId]
	for i, s := range subs {
		if s.id == sub.id {
			h.byStreamId[streamId] = append(subs[:i], subs[i+1:]...)
			if len(h.byStreamId[streamId]) == 0 {
				delete(h.byStreamId, streamId)
			}
			return
		}
	}
}

// subscribeToTopic creates a subscription for a specific topicId.
// Returns (nil, nil) if topicId is empty.
func (h *streamHub) subscribeToTopic(topicId string) (<-chan StreamChunk, UnsubscribeFunc) {
	if topicId == "" {
		return nil, nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		ch := make(chan StreamChunk)
		close(ch)
		return ch, func() {}
	}

	sub := &streamSubscription{
		id:     h.nextId,
		buffer: buffer.NewUnbounded[StreamChunk](),
	}
	h.nextId++
	h.byTopicId[topicId] = append(h.byTopicId[topicId], sub)

	unsubscribe := func() {
		h.unsubscribeFromTopic(topicId, sub, true)
	}

	return sub.buffer.Receive(), unsubscribe
}

// unsubscribeFromTopic removes a subscription from byTopicId.
func (h *streamHub) unsubscribeFromTopic(
	topicId string,
	sub *streamSubscription,
	discard bool,
) {
	h.mu.Lock()
	defer h.mu.Unlock()

	closeSubscriptionBuffer(sub, discard)

	subs := h.byTopicId[topicId]
	for i, s := range subs {
		if s.id == sub.id {
			h.byTopicId[topicId] = append(subs[:i], subs[i+1:]...)
			if len(h.byTopicId[topicId]) == 0 {
				delete(h.byTopicId, topicId)
			}
			return
		}
	}
}

func closeSubscriptionBuffer(sub *streamSubscription, discard bool) {
	if discard {
		sub.buffer.Discard()
		return
	}
	sub.buffer.Close()
}

// emit sends a chunk to all relevant subscribers.
// This method is concurrent-safe and never blocks.
func (h *streamHub) emit(chunk StreamChunk) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.closed {
		return
	}

	// Send to all subscribers
	for _, sub := range h.allSubscribers {
		sub.buffer.Send(chunk)
	}

	// Send to stream-specific subscribers
	if chunk.StreamId != "" {
		for _, sub := range h.byStreamId[chunk.StreamId] {
			sub.buffer.Send(chunk)
		}
	}

	// Send to topic-specific subscribers
	if chunk.StreamTopicId != "" {
		for _, sub := range h.byTopicId[chunk.StreamTopicId] {
			sub.buffer.Send(chunk)
		}
	}
}

// close closes all subscription channels.
// Safe to call multiple times.
func (h *streamHub) close() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return
	}
	h.closed = true

	// Close all subscriber buffers
	for _, sub := range h.allSubscribers {
		sub.buffer.Close()
	}
	for _, subs := range h.byStreamId {
		for _, sub := range subs {
			sub.buffer.Close()
		}
	}
	for _, subs := range h.byTopicId {
		for _, sub := range subs {
			sub.buffer.Close()
		}
	}
}
