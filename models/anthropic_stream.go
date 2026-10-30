package models

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/rickchristie/gent"
)

type anthropicStream struct {
	envelope *anthropicEnvelope
	ctx      context.Context
	stream   *gent.StreamWithDuration
	options  *gent.CallOptions
	blocks   []*anthropicBlock
	started  bool
	size     int
}

type anthropicBlock struct {
	fields    map[string]json.RawMessage
	text      strings.Builder
	thinking  strings.Builder
	signature strings.Builder
	input     strings.Builder
	closed    bool
}

func (state *anthropicStream) consume(data []byte) (bool, error) {
	state.size += len(data)
	if state.size > maxProviderBytes {
		return false, errors.New("Anthropic response exceeds 64 MiB")
	}
	var event struct {
		Type    string                     `json:"type"`
		Index   int                        `json:"index"`
		Message anthropicEnvelope          `json:"message"`
		Block   map[string]json.RawMessage `json:"content_block"`
		Delta   map[string]json.RawMessage `json:"delta"`
		Usage   map[string]int             `json:"usage"`
		Error   struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return false, err
	}
	if event.Type == "error" {
		return false, fmt.Errorf("Anthropic %s: %s", event.Error.Type, event.Error.Message)
	}
	if event.Type == "ping" {
		return false, nil
	}
	if event.Type == "message_start" {
		if state.started {
			return false, errors.New("duplicate Anthropic message_start")
		}
		state.started = true
		*state.envelope = event.Message
		if len(event.Message.Content) != 0 {
			return false, errors.New("Anthropic message_start contains unfinished blocks")
		}
		return false, nil
	}
	if !state.started {
		return false, errors.New("Anthropic stream has no message_start")
	}
	switch event.Type {
	case "content_block_start":
		if event.Index != len(state.blocks) || event.Index > 4096 {
			return false, errors.New("invalid Anthropic content block index")
		}
		block := &anthropicBlock{fields: event.Block}
		if block.fields == nil {
			return false, errors.New("missing Anthropic content block")
		}
		for _, field := range []string{"text", "thinking", "signature"} {
			if raw, exists := event.Block[field]; exists {
				var value string
				if err := json.Unmarshal(raw, &value); err != nil {
					return false, err
				}
				block.append(field, value)
			}
		}
		state.blocks = append(state.blocks, block)
	case "content_block_delta":
		block, err := state.block(event.Index)
		if err != nil {
			return false, err
		}
		var kind string
		if err := json.Unmarshal(event.Delta["type"], &kind); err != nil {
			return false, err
		}
		if kind == "citations_delta" {
			var citations []json.RawMessage
			if raw := block.fields["citations"]; raw != nil {
				if err := json.Unmarshal(raw, &citations); err != nil {
					return false, err
				}
			}
			citations = append(citations, event.Delta["citation"])
			raw, err := json.Marshal(citations)
			block.fields["citations"] = raw
			return false, err
		}
		field := map[string]string{"text_delta": "text", "thinking_delta": "thinking",
			"signature_delta": "signature", "input_json_delta": "partial_json"}[kind]
		if field == "" {
			return false, fmt.Errorf("unsupported Anthropic delta %q", kind)
		}
		var value string
		if err := json.Unmarshal(event.Delta[field], &value); err != nil {
			return false, err
		}
		block.append(field, value)
		meta := ""
		if field == "text" {
			meta = gent.MetaContentIntermediary
		}
		if field == "thinking" {
			meta = gent.MetaContentReasoning
		}
		if meta != "" {
			return false, sendProviderText(state.ctx, state.stream, state.options, meta,
				fmt.Sprintf("block:%d", event.Index), value)
		}
	case "content_block_stop":
		block, err := state.block(event.Index)
		if err != nil {
			return false, err
		}
		block.closed = true
	case "message_delta":
		if raw := event.Delta["stop_reason"]; raw != nil {
			if err := json.Unmarshal(raw, &state.envelope.StopReason); err != nil {
				return false, err
			}
		}
		usage := &state.envelope.Usage
		for name, value := range event.Usage {
			switch name {
			case "input_tokens":
				usage.Input = value
			case "output_tokens":
				usage.Output = value
			case "cache_read_input_tokens":
				usage.CacheRead = value
			case "cache_creation_input_tokens":
				usage.CacheWrite = value
			}
		}
	case "message_stop":
		for _, block := range state.blocks {
			if !block.closed {
				return false, errors.New("Anthropic stream ended with an open block")
			}
			raw, err := block.finish(state.envelope.StopReason)
			if err != nil {
				return false, err
			}
			state.envelope.Content = append(state.envelope.Content, raw)
		}
		return true, nil
	default:
		return false, fmt.Errorf("unsupported Anthropic event %q", event.Type)
	}
	return false, nil
}

func (state *anthropicStream) block(index int) (*anthropicBlock, error) {
	if index < 0 || index >= len(state.blocks) || state.blocks[index].closed {
		return nil, errors.New("invalid Anthropic content block state")
	}
	return state.blocks[index], nil
}

func (block *anthropicBlock) append(field, value string) {
	switch field {
	case "text":
		block.text.WriteString(value)
	case "thinking":
		block.thinking.WriteString(value)
	case "signature":
		block.signature.WriteString(value)
	case "partial_json":
		block.input.WriteString(value)
	}
}

func (block *anthropicBlock) finish(stopReason string) (json.RawMessage, error) {
	for field, value := range map[string]string{"text": block.text.String(),
		"thinking": block.thinking.String(), "signature": block.signature.String()} {
		if value != "" || block.fields[field] != nil {
			raw, err := json.Marshal(value)
			if err != nil {
				return nil, err
			}
			block.fields[field] = raw
		}
	}
	if block.input.Len() > 0 {
		value := []byte(block.input.String())
		if !json.Valid(value) {
			if stopReason != "max_tokens" && stopReason != "model_context_window_exceeded" {
				return nil, errors.New("Anthropic completed a tool call with invalid JSON")
			}
			// Incomplete JSON is evidence only. Keep the exact bytes outside input so it cannot
			// become an executable native call or an invalid provider replay item.
			raw, err := json.Marshal(string(value))
			if err != nil {
				return nil, err
			}
			block.fields["gent_partial_input"] = raw
		} else {
			block.fields["input"] = value
		}
	}
	return json.Marshal(block.fields)
}
