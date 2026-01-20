// Package buffer provides buffer implementations for concurrent streaming.
package buffer

import (
	"sync"
)

// Unbounded provides non-blocking sends with unlimited buffering.
// This ensures producers never block waiting for consumers.
//
// Usage:
//
//	buf := buffer.NewUnbounded[MyType]()
//	go func() {
//	    for item := range buf.Receive() {
//	        // Process item
//	    }
//	}()
//	buf.Send(item1)  // Never blocks
//	buf.Send(item2)  // Never blocks
//	buf.Close()      // Closes the receive channel
type Unbounded[T any] struct {
	mu       sync.Mutex
	items    []T
	cond     *sync.Cond
	closed   bool
	out      chan T
	draining bool
}

// NewUnbounded creates a new unbounded buffer.
// The returned buffer is ready to receive items via Send().
func NewUnbounded[T any]() *Unbounded[T] {
	b := &Unbounded[T]{
		items: make([]T, 0, 64),
		out:   make(chan T, 1),
	}
	b.cond = sync.NewCond(&b.mu)
	go b.drainLoop()
	return b
}

// drainLoop continuously moves items from the internal queue to the output channel.
// It runs until the buffer is closed and all queued items are drained.
func (b *Unbounded[T]) drainLoop() {
	for {
		item, ok := b.dequeue()
		if !ok {
			close(b.out)
			return
		}
		b.out <- item
	}
}

// dequeue removes and returns the next item from the queue.
// It blocks until an item is available or the buffer is closed.
// Returns (item, true) if an item was dequeued, (zero, false) if closed and empty.
func (b *Unbounded[T]) dequeue() (T, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Wait until there's something in the queue or we're closed
	for len(b.items) == 0 && !b.closed {
		b.cond.Wait()
	}

	// If closed and queue is empty, we're done
	if len(b.items) == 0 {
		var zero T
		return zero, false
	}

	// Pop from front of queue
	item := b.items[0]
	b.items = b.items[1:]

	return item, true
}

// Send adds an item to the buffer. This method NEVER blocks.
// It's safe to call from any goroutine.
// Items sent after Close() are silently ignored.
func (b *Unbounded[T]) Send(item T) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}

	b.items = append(b.items, item)
	b.cond.Signal()
}

// Receive returns a channel that receives items from the buffer.
// The channel is closed when Close() is called and all pending items are drained.
func (b *Unbounded[T]) Receive() <-chan T {
	return b.out
}

// Close marks the buffer as closed.
// After Close(), Send() calls are ignored and Receive() channel will close
// after all pending items are drained.
// It's safe to call multiple times.
func (b *Unbounded[T]) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}

	b.closed = true
	b.cond.Signal()
}

// Len returns the current number of items in the buffer.
// This is primarily useful for testing and debugging.
func (b *Unbounded[T]) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.items)
}

// IsClosed returns true if the buffer has been closed.
func (b *Unbounded[T]) IsClosed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}
