package trace

import (
	"sync"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/internal/buffer"
)

// Sequencer assigns trace event numbers and maintains a materialized snapshot.
type Sequencer struct {
	closeMu sync.Mutex
	mu      sync.RWMutex

	runId string
	cfg   config

	closed          bool
	nextEventNumber uint64

	snapshot Snapshot

	recentEvents          []*Event
	recentLifecycleEvents []*Event
	recentChunkEvents     []*Event

	contextById      map[string]*Context
	modelById        map[string]*ModelCall
	modelIdsByStream map[string][]string
	toolById         map[string]*ToolCall
	iterationByKey   map[string]*Iteration

	liveSubscribers    map[uint64]*liveSubscriber
	nextLiveSubscriber uint64

	streamObservers map[string]*streamObserver
}

type streamObserver struct {
	execCtx     *gent.ExecutionContext
	unsubscribe gent.UnsubscribeFunc
	done        chan struct{}
}

type normalizedEvent struct {
	event *Event
	apply func(*Sequencer, *Event)
}

// NewSequencer creates a trace sequencer for a run.
func NewSequencer(runId string, cfg Config) *Sequencer {
	normalized := normalizeConfig(cfg)
	now := time.Now()
	seq := &Sequencer{
		runId: runId,
		cfg:   normalized,
		snapshot: Snapshot{
			SchemaVersion: SchemaVersion,
			RunId:         runId,
			Status:        RunStatusPending,
			StartedTs:     now,
			LastUpdatedTs: now,
		},
		contextById:      make(map[string]*Context),
		modelById:        make(map[string]*ModelCall),
		modelIdsByStream: make(map[string][]string),
		toolById:         make(map[string]*ToolCall),
		iterationByKey:   make(map[string]*Iteration),
		liveSubscribers:  make(map[uint64]*liveSubscriber),
		streamObservers:  make(map[string]*streamObserver),
	}
	return seq
}

func (s *Sequencer) record(input normalizedEvent) *Event {
	if input.event == nil {
		return nil
	}
	if input.event.Ts.IsZero() {
		input.event.Ts = time.Now()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}

	s.nextEventNumber++
	input.event.EventNumber = s.nextEventNumber
	input.event.RunId = s.runId
	s.fillErrorMetadata(input.event)

	if input.apply != nil {
		input.apply(s, input.event)
	}
	input.event.Payload = jsonSafeValue(input.event.Payload)
	s.snapshot.LastEventNumber = input.event.EventNumber
	s.snapshot.LastUpdatedTs = input.event.Ts

	stored := cloneEvent(input.event)
	s.appendRecent(stored)
	s.sendLive(stored)
	return cloneEvent(stored)
}

// Snapshot returns a copy safe for concurrent readers.
func (s *Sequencer) Snapshot() *Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSnapshot(&s.snapshot)
}

// EventsAfter returns replay events after lastEventNumber from the contiguous recent buffer.
func (s *Sequencer) EventsAfter(lastEventNumber uint64) ([]*Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if lastEventNumber == s.snapshot.LastEventNumber {
		return []*Event{}, nil
	}
	if len(s.recentEvents) == 0 {
		return nil, ErrReplayUnavailable
	}
	first := s.recentEvents[0].EventNumber
	if lastEventNumber+1 < first {
		return nil, ErrReplayUnavailable
	}
	result := make([]*Event, 0)
	for _, event := range s.recentEvents {
		if event.EventNumber > lastEventNumber {
			result = append(result, cloneEvent(event))
		}
	}
	if len(result) == 0 && lastEventNumber < s.snapshot.LastEventNumber {
		return nil, ErrReplayUnavailable
	}
	return result, nil
}

// Close drains observed stream chunks, then closes live subscriptions.
func (s *Sequencer) Close() {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()

	observers := s.takeStreamObservers()
	for _, observer := range observers {
		observer.closeAndWait()
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	subs := s.liveSubscribers
	s.liveSubscribers = make(map[uint64]*liveSubscriber)
	s.mu.Unlock()

	for _, sub := range subs {
		sub.close()
	}
}

func (s *Sequencer) takeStreamObservers() []*streamObserver {
	s.mu.Lock()
	defer s.mu.Unlock()

	observers := make([]*streamObserver, 0, len(s.streamObservers))
	for _, observer := range s.streamObservers {
		observers = append(observers, observer)
	}
	s.streamObservers = make(map[string]*streamObserver)
	return observers
}

func (s *Sequencer) removeStreamObserver(contextId string) *streamObserver {
	s.mu.Lock()
	defer s.mu.Unlock()

	observer := s.streamObservers[contextId]
	delete(s.streamObservers, contextId)
	return observer
}

func (o *streamObserver) closeAndWait() {
	if o.unsubscribe != nil {
		o.unsubscribe()
	}
	if o.done != nil {
		<-o.done
	}
}

func (s *Sequencer) fillErrorMetadata(event *Event) {
	if event.Error == nil {
		return
	}
	if event.Error.EventNumber == 0 {
		event.Error.EventNumber = event.EventNumber
	}
	if event.Error.Ts.IsZero() {
		event.Error.Ts = event.Ts
	}
	if event.Error.EventName == "" {
		event.Error.EventName = event.EventName
	}
	if event.Error.Source == "" {
		event.Error.Source = event.Source
	}
	if event.Error.Iteration == 0 {
		event.Error.Iteration = event.Iteration
	}
	if event.Error.Depth == 0 {
		event.Error.Depth = event.Depth
	}
	if event.Error.ContextId == "" {
		event.Error.ContextId = event.ContextId
	}
	if event.Error.ParentContextId == "" {
		event.Error.ParentContextId = event.ParentContextId
	}
	if event.Error.ModelCallId == "" {
		event.Error.ModelCallId = event.ModelCallId
	}
	if event.Error.ToolCallId == "" {
		event.Error.ToolCallId = event.ToolCallId
	}
}

func (s *Sequencer) appendRecent(event *Event) {
	if s.cfg.maxRecentEvents >= 0 {
		s.recentEvents = appendCapped(s.recentEvents, cloneEvent(event), s.cfg.maxRecentEvents)
		s.snapshot.RecentEvents = s.recentEvents
	}
	if event.Type == EventTypeModelStreamChunk {
		if s.cfg.maxRecentChunkEvents >= 0 {
			s.recentChunkEvents = appendCapped(
				s.recentChunkEvents, cloneEvent(event), s.cfg.maxRecentChunkEvents,
			)
			s.snapshot.RecentChunkEvents = s.recentChunkEvents
		}
		return
	}
	if s.cfg.maxRecentLifecycleEvents >= 0 {
		s.recentLifecycleEvents = appendCapped(
			s.recentLifecycleEvents, cloneEvent(event), s.cfg.maxRecentLifecycleEvents,
		)
		s.snapshot.RecentLifecycleEvents = s.recentLifecycleEvents
	}
}

func appendCapped(events []*Event, event *Event, max int) []*Event {
	if max < 0 {
		return events
	}
	events = append(events, event)
	if len(events) <= max {
		return events
	}
	trim := len(events) - max
	copy(events, events[trim:])
	return events[:max]
}

func (s *Sequencer) sendLive(event *Event) {
	for id, sub := range s.liveSubscribers {
		if sub.send(cloneEvent(event)) {
			continue
		}
		delete(s.liveSubscribers, id)
	}
}

type liveSubscriber struct {
	id uint64

	unbounded  *buffer.Unbounded[*Event]
	bounded    chan *Event
	policy     OverflowPolicy
	onOverflow func(OverflowInfo)

	closeOnce sync.Once
}

func (s *liveSubscriber) receive() <-chan *Event {
	if s.unbounded != nil {
		return s.unbounded.Receive()
	}
	return s.bounded
}

func (s *liveSubscriber) send(event *Event) bool {
	if s.unbounded != nil {
		s.unbounded.Send(event)
		return true
	}
	select {
	case s.bounded <- event:
		return true
	default:
	}

	switch s.policy {
	case OverflowDropOldest:
		var dropped *Event
		select {
		case dropped = <-s.bounded:
		default:
		}
		select {
		case s.bounded <- event:
			s.notifyOverflow(OverflowInfo{
				Policy:             s.policy,
				DroppedEventNumber: eventNumber(dropped),
				NewEventNumber:     event.EventNumber,
			})
			return true
		default:
			s.notifyOverflow(OverflowInfo{
				Policy:             OverflowDropNewest,
				DroppedEventNumber: event.EventNumber,
				NewEventNumber:     event.EventNumber,
			})
			return true
		}
	case OverflowDropNewest:
		s.notifyOverflow(OverflowInfo{
			Policy:             s.policy,
			DroppedEventNumber: event.EventNumber,
			NewEventNumber:     event.EventNumber,
		})
		return true
	default:
		s.notifyOverflow(OverflowInfo{
			Policy:           OverflowCloseSubscriber,
			NewEventNumber:   event.EventNumber,
			SubscriberClosed: true,
		})
		s.close()
		return false
	}
}

func (s *liveSubscriber) notifyOverflow(info OverflowInfo) {
	if s.onOverflow == nil {
		return
	}
	go s.onOverflow(info)
}

func (s *liveSubscriber) close() {
	s.closeOnce.Do(func() {
		if s.unbounded != nil {
			s.unbounded.Close()
			return
		}
		close(s.bounded)
	})
}

func eventNumber(event *Event) uint64 {
	if event == nil {
		return 0
	}
	return event.EventNumber
}
