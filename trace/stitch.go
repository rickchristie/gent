package trace

import "errors"

// ErrReplayUnavailable means the requested event range is no longer in memory.
var ErrReplayUnavailable = errors.New("trace replay unavailable")

// CanStitch reports whether the first buffered event could stitch to a snapshot.
func CanStitch(snapshotLastEventNumber uint64, firstBufferedEventNumber uint64) bool {
	return firstBufferedEventNumber <= snapshotLastEventNumber+1
}

// StitchStatus describes whether buffered events can be applied to a snapshot.
type StitchStatus string

const (
	StitchStatusOK       StitchStatus = "ok"
	StitchStatusGap      StitchStatus = "gap"
	StitchStatusUnsorted StitchStatus = "unsorted"
)

// StitchResult describes the result of validating buffered trace events.
type StitchResult struct {
	Status             StitchStatus `json:"status"`
	SnapshotLastEvent  uint64       `json:"snapshotLastEvent"`
	FirstBufferedEvent uint64       `json:"firstBufferedEvent,omitempty"`
	MissingAfterEvent  uint64       `json:"missingAfterEvent,omitempty"`
	MissingBeforeEvent uint64       `json:"missingBeforeEvent,omitempty"`
}

// ValidateStitch validates that events are sorted, contiguous, and can apply to a snapshot.
func ValidateStitch(snapshotLastEventNumber uint64, events []*Event) StitchResult {
	result := StitchResult{
		Status:            StitchStatusOK,
		SnapshotLastEvent: snapshotLastEventNumber,
	}
	if len(events) == 0 {
		return result
	}
	result.FirstBufferedEvent = events[0].EventNumber

	var previous uint64
	for i, event := range events {
		if event == nil {
			continue
		}
		if i > 0 && event.EventNumber <= previous {
			result.Status = StitchStatusUnsorted
			return result
		}
		if i > 0 && event.EventNumber != previous+1 {
			result.Status = StitchStatusGap
			result.MissingAfterEvent = previous
			result.MissingBeforeEvent = event.EventNumber
			return result
		}
		previous = event.EventNumber
	}

	for _, event := range events {
		if event == nil || event.EventNumber <= snapshotLastEventNumber {
			continue
		}
		if event.EventNumber != snapshotLastEventNumber+1 {
			result.Status = StitchStatusGap
			result.MissingAfterEvent = snapshotLastEventNumber
			result.MissingBeforeEvent = event.EventNumber
		}
		return result
	}
	return result
}
