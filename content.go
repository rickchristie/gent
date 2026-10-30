package gent

import "strings"

const (
	MetaContentReasoning    = "reasoning"
	MetaContentCommentary   = "commentary"
	MetaContentIntermediary = "intermediary"
)

// MetaContent is visible text outside the accepted answer. Type is open to new provider labels.
// MessageId groups deltas. The managed boundary scopes adapter-local IDs to one physical call.
// IDs are opaque. Saved replay keeps them; a new provider request uses new IDs.
type MetaContent struct {
	Type      string        `json:"type"`
	Content   []ContentPart `json:"content"`
	MessageId string        `json:"messageId,omitempty"`
}

// ContentSource links an accepted answer to the previews it replaces. It is optional for a custom
// loop that produces output without a model. MessageIds must identify only the selected candidate.
type ContentSource struct {
	ModelCallId   string
	StreamId      string
	StreamTopicId string
	MessageIds    []string
}

// MetaText joins one metadata type without guessing meaning from its text.
func MetaText(items []*MetaContent, kind string) string {
	var text strings.Builder
	for _, item := range items {
		if item.Type == kind {
			text.WriteString(ContentText(item.Content))
		}
	}
	return text.String()
}

// CloneMetaContent returns owned entries for independent consumers.
func CloneMetaContent(items []*MetaContent) []*MetaContent {
	if items == nil {
		return nil
	}
	result := make([]*MetaContent, len(items))
	for i, item := range items {
		copy := *item
		copy.Content = CloneContentParts(item.Content)
		result[i] = &copy
	}
	return result
}

func cloneContentSource(source *ContentSource) *ContentSource {
	if source == nil {
		return nil
	}
	result := *source
	result.MessageIds = append([]string(nil), source.MessageIds...)
	return &result
}

func cloneStreamChunk(chunk StreamChunk) StreamChunk {
	chunk.Content = CloneContentParts(chunk.Content)
	chunk.MetaContent = CloneMetaContent(chunk.MetaContent)
	chunk.SourceMessageIds = append([]string(nil), chunk.SourceMessageIds...)
	return chunk
}

// metaAccumulator joins adjacent pure text deltas without copying all prior text for each token.
// Media keeps its original order. Snapshots own all mutable content.
type metaAccumulator struct {
	entries    []*MetaContent
	text       strings.Builder
	textActive bool
}

func (a *metaAccumulator) finishText() {
	if a.textActive {
		a.entries[len(a.entries)-1].Content = []ContentPart{TextContent{Text: a.text.String()}}
		a.text.Reset()
		a.textActive = false
	}
}

func (a *metaAccumulator) add(items []*MetaContent) {
	for _, item := range items {
		text, pure := metaTextDelta(item.Content)
		if pure && text == "" {
			continue
		}
		if pure && a.textActive {
			last := a.entries[len(a.entries)-1]
			if last.Type == item.Type && last.MessageId == item.MessageId {
				a.text.WriteString(text)
				continue
			}
		}
		a.finishText()
		copy := *item
		copy.Content = CloneContentParts(item.Content)
		a.entries = append(a.entries, &copy)
		if pure {
			a.text.WriteString(text)
			a.textActive = true
		}
	}
}

func metaTextDelta(parts []ContentPart) (string, bool) {
	if len(parts) != 1 {
		return "", false
	}
	switch part := parts[0].(type) {
	case TextContent:
		return part.Text, true
	case *TextContent:
		return part.Text, true
	default:
		return "", false
	}
}

func (a *metaAccumulator) snapshot() []*MetaContent {
	result := CloneMetaContent(a.entries)
	if a.textActive {
		result[len(result)-1].Content = []ContentPart{TextContent{Text: a.text.String()}}
	}
	return result
}

func (a *metaAccumulator) reset() {
	a.entries = nil
	a.text.Reset()
	a.textActive = false
}

// MetaParts returns the ordered content from one metadata channel, with owned media bytes.
func MetaParts(items []*MetaContent, kind string) []ContentPart {
	var parts []ContentPart
	for _, item := range items {
		if item.Type == kind {
			parts = append(parts, CloneContentParts(item.Content)...)
		}
	}
	return parts
}
