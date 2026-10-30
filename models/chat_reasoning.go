package models

import (
	"bytes"
	"encoding/json"
	"strings"
)

type chatReasoningIdentity struct {
	Type   string `json:"type"`
	ID     string `json:"id"`
	Format string `json:"format"`
	Index  *int   `json:"index"`
}

type chatReasoningBlock struct {
	identity chatReasoningIdentity
	fields   map[string]json.RawMessage
	text     map[string]*strings.Builder
}

// OpenRouter streams one logical summary or signed text block in many deltas. Index alone
// cannot identify a block: summary and encrypted items can share index zero. Join only adjacent
// text fragments with compatible identities. Keep opaque encrypted items separate and ordered.
func (state *chatAccumulator) addReasoning(raw json.RawMessage) error {
	var details []json.RawMessage
	if err := json.Unmarshal(raw, &details); err != nil {
		return err
	}
	for _, detail := range details {
		var identity chatReasoningIdentity
		if err := json.Unmarshal(detail, &identity); err != nil {
			return err
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(detail, &fields); err != nil {
			return err
		}
		var block *chatReasoningBlock
		if len(state.detail) > 0 {
			previous := state.detail[len(state.detail)-1]
			if previous.identity.matches(identity) {
				block = previous
			}
		}
		if block == nil {
			block = &chatReasoningBlock{identity: identity, fields: map[string]json.RawMessage{},
				text: map[string]*strings.Builder{}}
			state.detail = append(state.detail, block)
		}
		if err := block.add(identity, fields); err != nil {
			return err
		}
	}
	return nil
}

func (a chatReasoningIdentity) matches(b chatReasoningIdentity) bool {
	if a.Type != b.Type || (a.Type != "reasoning.summary" && a.Type != "reasoning.text") {
		return false
	}
	if a.ID != "" && b.ID != "" && a.ID != b.ID {
		return false
	}
	if a.Format != "" && a.Format != "unknown" && b.Format != "" && b.Format != "unknown" &&
		a.Format != b.Format {
		return false
	}
	return a.Index == nil || b.Index == nil || *a.Index == *b.Index
}

func (block *chatReasoningBlock) add(
	identity chatReasoningIdentity, fields map[string]json.RawMessage,
) error {
	if identity.Type != "reasoning.summary" && identity.Type != "reasoning.text" {
		block.fields = fields
		return nil
	}
	for key, raw := range fields {
		if bytes.Equal(raw, []byte("null")) || bytes.Equal(raw, []byte(`""`)) {
			if block.fields[key] == nil {
				block.fields[key] = raw
			}
			continue
		}
		if key == "summary" || key == "text" || key == "signature" {
			var value string
			if err := json.Unmarshal(raw, &value); err != nil {
				return err
			}
			if block.text[key] == nil {
				block.text[key] = &strings.Builder{}
			}
			block.text[key].WriteString(value)
			continue
		}
		if key == "format" && bytes.Equal(raw, []byte(`"unknown"`)) &&
			block.fields[key] != nil {
			continue
		}
		block.fields[key] = raw
	}
	// Identity fields can arrive after the first text fragment. Use the accumulated fields
	// when deciding whether a later fragment belongs to this block.
	if identity.ID != "" {
		block.identity.ID = identity.ID
	}
	if identity.Format != "" && identity.Format != "unknown" {
		block.identity.Format = identity.Format
	}
	if identity.Index != nil {
		block.identity.Index = identity.Index
	}
	return nil
}

func (block *chatReasoningBlock) value() map[string]any {
	value := make(map[string]any, len(block.fields)+len(block.text))
	for key, raw := range block.fields {
		value[key] = raw
	}
	for key, text := range block.text {
		value[key] = text.String()
	}
	return value
}
