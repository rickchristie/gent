package gent

import (
	"errors"
	"time"

	"github.com/rickchristie/gent/internal/buffer"
)

// DeliveryConfig bounds one live consumer independently of checkpoint persistence.
// Zero fields use defaults. Negative fields are invalid; unbounded delivery is not supported.
type DeliveryConfig struct {
	// MaxEvents includes the event currently waiting for the consumer. The default is 4096.
	MaxEvents int
	// MaxBytes bounds encoded queued data, including the current event. The default is 8 MiB.
	// An event larger than this limit closes this subscription without stopping execution.
	MaxBytes int
	// Timeout limits each wait for the consumer, including after execution ends. Default: 30s.
	// Apply a separate write deadline in the socket worker after receiving an event.
	Timeout time.Duration
}

var (
	// ErrDeliveryOverflow means a consumer exceeded its event or byte queue limit.
	ErrDeliveryOverflow = buffer.ErrDeliveryOverflow
	// ErrDeliveryTimeout means a consumer did not receive an event within its delivery timeout.
	ErrDeliveryTimeout = buffer.ErrDeliveryTimeout
)

func deliveryConfig(cfg DeliveryConfig) (DeliveryConfig, error) {
	if cfg.MaxEvents < 0 || cfg.MaxBytes < 0 || cfg.Timeout < 0 {
		return cfg, errors.New("gent: delivery limits must not be negative")
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
	return cfg, nil
}

// EventSubscription receives future live events from every context in the run, in order.
// Events may precede persistence. On a delivery error, replace the preview with saved state.
type EventSubscription struct {
	queue *buffer.Delivery[*RecordedEvent]
	close func()
}

// Events returns owned events in sequence order. The channel closes when delivery stops.
func (s *EventSubscription) Events() <-chan *RecordedEvent { return s.queue.Receive() }

// Err returns the delivery failure, or nil for normal closure. Read it after Done closes.
func (s *EventSubscription) Err() error { return s.queue.Err() }

// Done closes after the delivery worker has stopped and released its queued data.
func (s *EventSubscription) Done() <-chan struct{} { return s.queue.Done() }

// Close discards pending delivery and waits for its worker. Repeated calls are safe.
func (s *EventSubscription) Close() { s.close() }

// SubscribeEvents installs a bounded future-event subscription. Subscribe before loading saved
// state, buffer this connection's events, then apply events beyond the saved cursor. Close the
// old subscription before recovery. A new worker can reuse numbers from an unsaved preview.
func (ctx *ExecutionContext) SubscribeEvents(cfg DeliveryConfig) (*EventSubscription, error) {
	cfg, err := deliveryConfig(cfg)
	if err != nil {
		return nil, err
	}
	run := ctx.run
	sub := &EventSubscription{queue: buffer.NewDelivery[*RecordedEvent](
		cfg.MaxEvents, cfg.MaxBytes, cfg.Timeout)}
	run.mu.Lock()
	if run.liveClosed {
		sub.queue.Close()
	} else {
		if run.liveSubscribers == nil {
			run.liveSubscribers = make(map[*EventSubscription]struct{})
		}
		run.liveSubscribers[sub] = struct{}{}
	}
	run.mu.Unlock()
	sub.close = func() {
		run.mu.Lock()
		delete(run.liveSubscribers, sub)
		run.mu.Unlock()
		sub.queue.Discard()
		<-sub.queue.Done()
	}
	go func() {
		<-sub.queue.Done()
		run.mu.Lock()
		delete(run.liveSubscribers, sub)
		run.mu.Unlock()
	}()
	return sub, nil
}
