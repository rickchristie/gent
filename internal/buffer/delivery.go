// Package buffer provides bounded queues for concurrent live delivery.
package buffer

import (
	"errors"
	"sync"
	"time"
)

var (
	ErrDeliveryOverflow = errors.New("gent: subscription queue limit exceeded")
	ErrDeliveryTimeout  = errors.New("gent: subscription delivery timed out")
)

type deliveryItem[T any] struct {
	value T
	size  int
}

// Delivery bounds queued and in-flight data. Send never waits for the consumer. A failed
// subscription discards its queue; Err tells the consumer to reconstruct its saved state.
type Delivery[T any] struct {
	mu        sync.Mutex
	sendMu    sync.Mutex
	items     []deliveryItem[T]
	bytes     int
	maxEvents int
	maxBytes  int
	timeout   time.Duration
	closed    bool
	err       error
	in        chan deliveryItem[T]
	accepted  chan bool
	out       chan T
	closing   chan struct{}
	stop      chan struct{}
	done      chan struct{}
	stopOnce  sync.Once
}

func NewDelivery[T any](maxEvents, maxBytes int, timeout time.Duration) *Delivery[T] {
	b := &Delivery[T]{maxEvents: maxEvents, maxBytes: maxBytes, timeout: timeout,
		in: make(chan deliveryItem[T]), accepted: make(chan bool, 1), out: make(chan T),
		closing: make(chan struct{}), stop: make(chan struct{}), done: make(chan struct{})}
	go b.drain()
	return b
}

func (b *Delivery[T]) Send(value T, size int) bool {
	// One sender at a time can use the shared reply channel. The worker accepts sends even
	// when the consumer is blocked; this waits for queue accounting, never for consumption.
	b.sendMu.Lock()
	defer b.sendMu.Unlock()
	if b.isClosed() {
		return false
	}
	select {
	case <-b.done:
		return false
	case b.in <- deliveryItem[T]{value, size}:
		return <-b.accepted
	}
}

func (b *Delivery[T]) Receive() <-chan T     { return b.out }
func (b *Delivery[T]) Done() <-chan struct{} { return b.done }

func (b *Delivery[T]) Err() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.err
}

// Close drains the finite queue. The delivery timeout also applies while draining.
func (b *Delivery[T]) Close() {
	b.mu.Lock()
	if !b.closed {
		b.closed = true
		close(b.closing)
	}
	b.mu.Unlock()
}

// Discard releases queued values even when nobody receives from the output channel.
func (b *Delivery[T]) Discard() { b.Fail(nil) }

func (b *Delivery[T]) Fail(err error) {
	b.mu.Lock()
	b.closed = true
	if b.err == nil {
		b.err = err
	}
	b.stopOnce.Do(func() { close(b.stop) })
	b.mu.Unlock()
}

func (b *Delivery[T]) isClosed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}

func (b *Delivery[T]) drain() {
	defer close(b.done)
	defer close(b.out)
	defer func() { b.items = nil }()
	timer := time.NewTimer(b.timeout)
	timer.Stop()
	defer timer.Stop()
	var expired <-chan time.Time
	closing := b.closing
	draining := false
	// This worker owns the queue and its byte count. A handoff releases capacity before the
	// next Send is checked, even when the consumer sends again before this worker is scheduled.
	for {
		if draining && len(b.items) == 0 {
			return
		}
		var out chan T
		var value T
		if len(b.items) != 0 {
			out, value = b.out, b.items[0].value
		}
		select {
		case <-b.stop:
			return
		case <-closing:
			draining, closing = true, nil
		case item := <-b.in:
			if b.isClosed() {
				b.accepted <- false
				continue
			}
			if len(b.items) >= b.maxEvents || item.size > b.maxBytes-b.bytes {
				b.Fail(ErrDeliveryOverflow)
				b.accepted <- false
				return
			}
			b.items = append(b.items, item)
			b.bytes += item.size
			if len(b.items) == 1 {
				timer.Reset(b.timeout)
				expired = timer.C
			}
			b.accepted <- true
		case <-expired:
			b.Fail(ErrDeliveryTimeout)
			return
		case out <- value:
			b.bytes -= b.items[0].size
			b.items[0] = deliveryItem[T]{}
			b.items = b.items[1:]
			timer.Stop()
			if len(b.items) == 0 {
				b.items = nil
				expired = nil
				continue
			}
			timer.Reset(b.timeout)
			expired = timer.C
		}
	}
}
