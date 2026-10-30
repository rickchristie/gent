package gent

import "slices"

// Event payloads already contain owned, typed bytes. Copy those bytes directly instead of encoding
// and decoding their envelope again for every subscriber, save, or Export. History changes still
// need the registry because their entries can contain application content and metadata types.
func cloneRecordedEvent(registry *CodecRegistry, event *RecordedEvent) (*RecordedEvent, error) {
	owned := *event
	owned.Ts = owned.Ts.Round(0)
	if event.Payload != nil {
		payload := *event.Payload
		payload.Data = slices.Clone(payload.Data)
		owned.Payload = &payload
	}
	if event.Change != nil {
		change, err := cloneOwned(registry, *event.Change)
		if err != nil {
			return nil, err
		}
		owned.Change = &change
	}
	return &owned, nil
}

func cloneRecordedEvents(
	registry *CodecRegistry, events []*RecordedEvent,
) ([]*RecordedEvent, error) {
	owned := slices.Clone(events)
	for i, event := range events {
		var err error
		owned[i], err = cloneRecordedEvent(registry, event)
		if err != nil {
			return nil, err
		}
	}
	return owned, nil
}

func cloneSaveBatch(registry *CodecRegistry, batch *SaveBatch) (*SaveBatch, error) {
	owned := *batch
	var err error
	owned.Events, err = cloneRecordedEvents(registry, batch.Events)
	if err != nil {
		return nil, err
	}
	owned.Runtime, err = cloneOwned(registry, batch.Runtime)
	if err != nil {
		return nil, err
	}
	// Preserve checkpoint values, including nil and empty maps, for an exact save retry.
	owned.Checkpoint, err = cloneOwned(registry, batch.Checkpoint)
	if err != nil {
		return nil, err
	}
	return &owned, nil
}
