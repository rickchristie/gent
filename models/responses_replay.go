package models

import (
	"encoding/json"
	"errors"
)

// OpenAI resolves image-generation item IDs from server storage even when result bytes are in
// the item. Our requests use store:false, so that replay fails with HTTP 404. Send the saved image
// as input media instead. Keep the original item in Continuation for diagnostics and restoration.
// Reasoning, messages, and function calls still use their exact saved provider representation.
func (m *Responses) replayItem(raw json.RawMessage) (any, error) {
	if m.config.Provider != "openai" {
		if !json.Valid(raw) {
			return nil, errors.New("invalid saved provider item")
		}
		return raw, nil
	}
	var item struct {
		Type   string `json:"type"`
		Result string `json:"result"`
		Format string `json:"output_format"`
	}
	if err := json.Unmarshal(raw, &item); err != nil {
		return nil, errors.New("invalid saved provider item")
	}
	if item.Type != "image_generation_call" {
		return raw, nil
	}
	if item.Result == "" {
		return nil, errors.New("saved image generation item has no image bytes")
	}
	format := item.Format
	if format == "" {
		format = "png"
	}
	return map[string]any{"role": "user", "content": []any{
		map[string]any{"type": "input_text", "text": "Image from the previous assistant response:"},
		map[string]any{"type": "input_image",
			"image_url": "data:image/" + format + ";base64," + item.Result},
	}}, nil
}
