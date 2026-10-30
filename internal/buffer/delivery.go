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
	cond      *sync.Cond
	items     []deliveryItem[T]
	count     int
	bytes     int
	maxEvents int
	maxBytes  int
	timeout   time.Duration
	closed    bool
	err       error
	out       chan T
	stop      chan struct{}
	done      chan struct{}
	stopOnce  sync.Once
}

func NewDelivery[T any](maxEvents, maxBytes int, timeout time.Duration) *Delivery[T] {
	b := &Delivery[T]{maxEvents: maxEvents, maxBytes: maxBytes, timeout: timeout,
		out: make(chan T), stop: make(chan struct{}), done: make(chan struct{})}
	b.cond = sync.NewCond(&b.mu)
	go b.drain()
	return b
}

func (b *Delivery[T]) Send(value T, size int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return false
	}
	if b.count >= b.maxEvents || size > b.maxBytes-b.bytes {
		b.failLocked(ErrDeliveryOverflow)
		return false
	}
	b.items = append(b.items, deliveryItem[T]{value, size})
	b.count++
	b.bytes += size
	b.cond.Signal()
	return true
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
	b.closed = true
	b.cond.Signal()
	b.mu.Unlock()
}

// Discard releases queued values even when nobody receives from the output channel.
func (b *Delivery[T]) Discard() { b.Fail(nil) }

func (b *Delivery[T]) Fail(err error) {
	b.mu.Lock()
	b.failLocked(err)
	b.mu.Unlock()
}

func (b *Delivery[T]) failLocked(err error) {
	b.closed = true
	if b.err == nil {
		b.err = err
	}
	b.items = nil
	b.count, b.bytes = 0, 0
	b.stopOnce.Do(func() { close(b.stop) })
	b.cond.Signal()
}

func (b *Delivery[T]) drain() {
	defer close(b.done)
	defer close(b.out)
	for {
		b.mu.Lock()
		for len(b.items) == 0 && !b.closed {
			b.cond.Wait()
		}
		if len(b.items) == 0 {
			b.mu.Unlock()
			return
		}
		item := b.items[0]
		b.items[0] = deliveryItem[T]{}
		b.items = b.items[1:]
		if len(b.items) == 0 {
			b.items = nil
		}
		b.mu.Unlock()
		timer := time.NewTimer(b.timeout)
		select {
		case <-b.stop:
			timer.Stop()
			return
		case <-timer.C:
			b.Fail(ErrDeliveryTimeout)
			return
		case b.out <- item.value:
			timer.Stop()
		}
		b.mu.Lock()
		// Discard can race with the successful send. It has already released the accounting.
		if b.count > 0 {
			b.count--
			b.bytes -= item.size
		}
		b.mu.Unlock()
	}
}
