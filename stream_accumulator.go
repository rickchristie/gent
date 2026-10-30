package gent

import "sync"

// StreamAccumulator collects provisional metadata and the accepted answer separately.
// Provider completion data and tool calls come from Stream.Response, not from display chunks.
type StreamAccumulator struct {
	mu        sync.Mutex
	content   []ContentPart
	metadata  metaAccumulator
	lastError error
}

func NewStreamAccumulator() *StreamAccumulator { return &StreamAccumulator{} }

func (a *StreamAccumulator) Add(chunk StreamChunk) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(chunk.Content) > 0 {
		a.content = CloneContentParts(chunk.Content)
	}
	a.metadata.add(chunk.MetaContent)
	if chunk.Err != nil {
		a.lastError = chunk.Err
	}
}

// Content returns the accepted answer snapshot, not the provisional text.
func (a *StreamAccumulator) Content() []ContentPart {
	a.mu.Lock()
	defer a.mu.Unlock()
	return CloneContentParts(a.content)
}

func (a *StreamAccumulator) MetaContent() []*MetaContent {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.metadata.snapshot()
}

func (a *StreamAccumulator) Error() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastError
}

// Response builds a provisional provider response. It cannot establish execution success.
func (a *StreamAccumulator) Response() *ContentResponse {
	metadata := a.MetaContent()
	return &ContentResponse{Choices: []*ContentChoice{{Completion: ModelCompletion{
		Status: ModelStatusAnswer,
	},
		CandidateContent: MetaParts(metadata, MetaContentIntermediary), MetaContent: metadata,
	}}}
}

// ResponseWithInfo is used when local size or repetition checks stop a provider early.
func (a *StreamAccumulator) ResponseWithInfo(response *ContentResponse) *ContentResponse {
	result := a.Response()
	if response != nil {
		result.Info = response.Info
	}
	return result
}

func (a *StreamAccumulator) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.content = nil
	a.metadata.reset()
	a.lastError = nil
}
