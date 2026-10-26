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
	Type      string `json:"type"`
	Text      string `json:"text"`
	MessageId string `json:"messageId,omitempty"`
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
			text.WriteString(item.Text)
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
	chunk.MetaContent = CloneMetaContent(chunk.MetaContent)
	chunk.SourceMessageIds = append([]string(nil), chunk.SourceMessageIds...)
	return chunk
}

// metaAccumulator keeps each contiguous text segment in order. Builders avoid copying all previous
// text for each token. Snapshots copy the entries so callers cannot change retained evidence.
type metaAccumulator struct {
	entries []*MetaContent
	text    strings.Builder
}

func (a *metaAccumulator) add(items []*MetaContent) {
	for _, item := range items {
		if item.Text == "" {
			continue
		}
		if n := len(a.entries); n > 0 {
			last := a.entries[n-1]
			if last.Type == item.Type && last.MessageId == item.MessageId {
				a.text.WriteString(item.Text)
				continue
			}
			last.Text = a.text.String()
		}
		a.text.Reset()
		copy := *item
		copy.Text = ""
		a.entries = append(a.entries, &copy)
		a.text.WriteString(item.Text)
	}
}

func (a *metaAccumulator) snapshot() []*MetaContent {
	result := CloneMetaContent(a.entries)
	if len(result) > 0 {
		result[len(result)-1].Text = a.text.String()
	}
	return result
}

func (a *metaAccumulator) reset() { a.entries = nil; a.text.Reset() }

const (
	ModelAnswer     = "answer"
	ModelTools      = "tools"
	ModelContinue   = "continue"
	ModelIncomplete = "incomplete"
	ModelFailed     = "failed"
)

// State normalizes request completion without treating a completed tool request as an answer.
// Unknown stop reasons fail closed. A custom adapter may leave StopReason empty after Complete.
func (choice *ContentChoice) State() string {
	switch choice.StopReason {
	case "length", "max_tokens", "model_context_window_exceeded", "MAX_TOKENS",
		"FinishReasonMaxTokens", "max_output_tokens":
		return ModelIncomplete
	case "pause_turn":
		return ModelContinue
	case "", "stop", "end_turn", "stop_sequence", "STOP", "FinishReasonStop", "refusal":
	case "tool_calls", "tool_use":
		if len(choice.ToolCalls) == 0 {
			return ModelFailed
		}
	default:
		return ModelFailed
	}
	if len(choice.ToolCalls) > 0 {
		return ModelTools
	}
	return ModelAnswer
}
