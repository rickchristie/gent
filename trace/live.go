package trace

import "github.com/rickchristie/gent"

// LiveFeed projects a bounded live subscription independently of checkpoint persistence.
// Wait reports delivery or projection failure. Such failures do not reject a saved checkpoint.
type LiveFeed struct {
	sub  *gent.EventSubscription
	done chan struct{}
	err  error
}

// Follow attaches before execution starts. The sequencer must already contain the saved view
// when following a restored context. Use a new sequencer when replacing an interrupted preview.
// ObserveCommitted remains available for callers that require a durable-only display.
func (s *Sequencer) Follow(ctx *gent.ExecutionContext, cfg gent.DeliveryConfig) (*LiveFeed, error) {
	sub, err := ctx.SubscribeEvents(cfg)
	if err != nil {
		return nil, err
	}
	feed := &LiveFeed{sub: sub, done: make(chan struct{})}
	go func() {
		defer close(feed.done)
		defer sub.Close()
		for event := range sub.Events() {
			if err := s.observeEvents([]*gent.RecordedEvent{event}, ctx.Codecs()); err != nil {
				feed.err = err
				return
			}
		}
		feed.err = sub.Err()
	}()
	return feed, nil
}

// Wait drains after ExecutionContext.Release closes the live source. Delivery has a timeout,
// so a stopped consumer cannot leave an unbounded queue or prevent checkpoint acceptance.
func (f *LiveFeed) Wait() error { <-f.done; return f.err }

// Close abandons this feed's pending preview and waits for its worker to stop.
func (f *LiveFeed) Close() error { f.sub.Close(); return f.Wait() }
