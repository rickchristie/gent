package trace

import (
	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/internal/buffer"
)

// OverflowPolicy decides what bounded subscriptions do when their queue is full.
type OverflowPolicy string

const (
	OverflowDropOldest      OverflowPolicy = "drop_oldest"
	OverflowDropNewest      OverflowPolicy = "drop_newest"
	OverflowCloseSubscriber OverflowPolicy = "close_subscriber"
)

// SubscribeConfig configures a live trace event subscription.
type SubscribeConfig struct {
	BufferSize     int
	OverflowPolicy OverflowPolicy
	OnOverflow     func(OverflowInfo)
}

// OverflowInfo describes a bounded subscriber overflow.
type OverflowInfo struct {
	Policy             OverflowPolicy
	DroppedEventNumber uint64
	NewEventNumber     uint64
	SubscriberClosed   bool
}

// Subscribe returns a future-event subscription with an unbounded buffer.
func (s *Sequencer) Subscribe() (<-chan *Event, gent.UnsubscribeFunc) {
	return s.SubscribeWithConfig(SubscribeConfig{})
}

// SubscribeWithConfig returns a future-event subscription.
// BufferSize <= 0 uses an unbounded buffer. Bounded subscribers never block
// event recording; overflow is handled by the configured policy.
func (s *Sequencer) SubscribeWithConfig(cfg SubscribeConfig) (<-chan *Event, gent.UnsubscribeFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		ch := make(chan *Event)
		close(ch)
		return ch, func() {}
	}

	s.nextLiveSubscriber++
	id := s.nextLiveSubscriber
	sub := &liveSubscriber{
		id:         id,
		policy:     cfg.OverflowPolicy,
		onOverflow: cfg.OnOverflow,
	}
	if sub.policy == "" {
		sub.policy = OverflowCloseSubscriber
	}
	if cfg.BufferSize <= 0 {
		sub.unbounded = buffer.NewUnbounded[*Event]()
	} else {
		sub.bounded = make(chan *Event, cfg.BufferSize)
	}
	s.liveSubscribers[id] = sub

	unsubscribe := func() {
		s.mu.Lock()
		removed := s.liveSubscribers[id]
		delete(s.liveSubscribers, id)
		s.mu.Unlock()
		if removed != nil {
			removed.discard()
		}
	}
	return sub.receive(), unsubscribe
}
