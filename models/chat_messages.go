package models

import (
	"errors"
	"fmt"
	"strings"

	"github.com/rickchristie/gent"
)

func (m *Chat) messages(messages []*gent.ModelMessage) ([]any, error) {
	result := make([]any, 0, len(messages))
	for _, message := range messages {
		if saved := message.Continuation; saved != nil {
			if err := checkContinuation(saved, m.provider); err != nil {
				return nil, err
			}
			for _, item := range saved.Items {
				result = append(result, item)
			}
			continue
		}
		role, err := messageRole(message.Role)
		if err != nil {
			return nil, err
		}
		value := map[string]any{"role": role}
		var content []any
		var calls []gent.ModelToolCall
		var text strings.Builder
		textOnly := true
		for _, part := range message.Parts {
			switch p := partValue(part).(type) {
			case gent.TextContent:
				text.WriteString(p.Text)
				content = append(content, map[string]any{"type": "text", "text": p.Text})
			case gent.ImageURLContent:
				image := map[string]any{"url": p.URL}
				if p.Detail != "" {
					image["detail"] = p.Detail
				}
				content = append(content, map[string]any{"type": "image_url", "image_url": image})
				textOnly = false
			case gent.BinaryContent:
				switch p.MIMEType {
				case "image/png", "image/jpeg", "image/webp", "image/gif":
					content = append(content, map[string]any{"type": "image_url",
						"image_url": map[string]any{"url": p.String()}})
				case "application/pdf":
					if m.provider != "openrouter" {
						return nil, fmt.Errorf("%s does not support PDF input", m.provider)
					}
					content = append(content, map[string]any{"type": "file", "file": map[string]any{
						"filename": "document.pdf", "file_data": p.String()}})
				default:
					return nil, fmt.Errorf("unsupported %s media %q", m.provider, p.MIMEType)
				}
				textOnly = false
			case gent.ModelToolCall:
				if role != "assistant" {
					return nil, errors.New("native calls require an assistant message")
				}
				calls = append(calls, p)
			case gent.ToolCallResponse:
				if role != "tool" || len(message.Parts) != 1 {
					return nil, errors.New("Chat requires one tool result per tool message")
				}
				value["tool_call_id"] = p.ToolCallID
				text.WriteString(p.Content)
			default:
				return nil, fmt.Errorf("unsupported %s content %T", m.provider, part)
			}
		}
		if textOnly {
			value["content"] = text.String()
		} else {
			value["content"] = content
		}
		if len(calls) > 0 {
			value["tool_calls"] = calls
		}
		result = append(result, value)
	}
	return result, nil
}
