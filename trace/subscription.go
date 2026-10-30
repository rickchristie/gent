package trace

import (
	"errors"
	"time"

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
	// BufferSize limits queued events. Zero uses 4096; use SubscribeWithLimits for byte limits.
	BufferSize int
	// OverflowPolicy selects a drop or close action. The default closes the subscriber.
	OverflowPolicy OverflowPolicy
	// OnOverflow is optional. One worker calls it with coalesced notifications. Return promptly.
	OnOverflow func(OverflowInfo)
}

// OverflowInfo describes a bounded subscriber overflow.
type OverflowInfo struct {
	Policy             OverflowPolicy
	DroppedEventNumber uint64
	NewEventNumber     uint64
	SubscriberClosed   bool
}

// Subscribe uses bounded default limits. Use SubscribeWithLimits to read delivery errors.
func (s *Sequencer) Subscribe() (<-chan *Event, gent.UnsubscribeFunc) {
	sub, err := s.SubscribeWithLimits(gent.DeliveryConfig{})
	if err != nil {
		panic(err)
	}
	return sub.Events(), sub.Close
}

// Subscription reports overflow and timeout independently of execution and checkpoint errors.
type Subscription struct {
	queue *buffer.Delivery[*Event]
	close func()
}

// Events returns owned display events in sequence order until delivery stops.
func (s *Subscription) Events() <-chan *Event { return s.queue.Receive() }

// Err returns the delivery failure, or nil for normal closure. Read it after Done closes.
func (s *Subscription) Err() error { return s.queue.Err() }

// Done closes after the delivery worker has stopped and released its queued data.
func (s *Subscription) Done() <-chan struct{} { return s.queue.Done() }

// Close discards pending delivery and waits for its worker. Repeated calls are safe.
func (s *Subscription) Close() { s.close() }

// SubscribeWithLimits bounds both event count and JSON bytes. A slow consumer is disconnected;
// the application must reload saved state after Err reports a delivery failure.
func (s *Sequencer) SubscribeWithLimits(cfg gent.DeliveryConfig) (*Subscription, error) {
	if cfg.MaxEvents < 0 || cfg.MaxBytes < 0 || cfg.Timeout < 0 {
		return nil, errors.New("trace: delivery limits must not be negative")
	}
	if cfg.MaxEvents == 0 {
		cfg.MaxEvents = 4096
	}
	if cfg.MaxBytes == 0 {
		cfg.MaxBytes = 8 * 1024 * 1024
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	queue := buffer.NewDelivery[*Event](cfg.MaxEvents, cfg.MaxBytes, cfg.Timeout)
	s.mu.Lock()
	s.nextLiveSubscriber++
	id := s.nextLiveSubscriber
	if s.closed {
		queue.Close()
	} else {
		s.liveSubscribers[id] = &liveSubscriber{id: id, delivery: queue}
	}
	s.mu.Unlock()
	remove := func() { s.mu.Lock(); delete(s.liveSubscribers, id); s.mu.Unlock() }
	go func() { <-queue.Done(); remove() }()
	return &Subscription{queue: queue, close: func() {
		remove()
		queue.Discard()
		<-queue.Done()
	}}, nil
}

// SubscribeWithConfig returns a future-event subscription.
// This legacy API supports count-based drop policies. Use SubscribeWithLimits for byte limits,
// delivery timeouts, and a readable error. Zero BufferSize uses a finite default of 4096 events.
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
	if sub.onOverflow != nil {
		sub.overflow = make(chan OverflowInfo, 1)
		go func() {
			for info := range sub.overflow {
				sub.onOverflow(info)
			}
		}()
	}
	if sub.policy == "" {
		sub.policy = OverflowCloseSubscriber
	}
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 4096
	}
	sub.bounded = make(chan *Event, cfg.BufferSize)
	s.liveSubscribers[id] = sub

	unsubscribe := func() {
		s.mu.Lock()
		delete(s.liveSubscribers, id)
		s.mu.Unlock()
		// Close removes the map entry, but the consumer still owns its unsubscribe handle.
		sub.discard()
	}
	return sub.receive(), unsubscribe
}
