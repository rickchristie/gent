package models

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/rickchristie/gent"
)

type geminiContent struct {
	Role  string            `json:"role"`
	Parts []json.RawMessage `json:"parts"`
}

// A token continuation requires the exact original request contents. Store it with the signed
// response parts, rather than depend on mutable working history or remote session retention.
type geminiContinuation struct {
	Content         geminiContent   `json:"content"`
	Token           string          `json:"continuationToken,omitempty"`
	RequestContents json.RawMessage `json:"requestContents,omitempty"`
}

func geminiMessages(messages []*gent.ModelMessage, nativeMedia ...bool) (
	[]any, []any, *geminiContinuation, error,
) {
	move := func(part gent.ContentPart) bool {
		if len(nativeMedia) > 0 && !nativeMedia[0] {
			return true
		}
		if binary, ok := partValue(part).(gent.BinaryContent); ok {
			return !geminiToolMIME(binary.MIMEType)
		}
		return true
	}
	var err error
	messages, err = separateSelectedToolMedia(messages, move)
	if err != nil {
		return nil, nil, nil, err
	}
	var result, system []any
	var pending *geminiContinuation
	callIDs := make(map[string]bool)
	for _, message := range messages {
		if saved := message.Continuation; saved != nil {
			if err := checkContinuation(saved, "gemini"); err != nil {
				return nil, nil, nil, err
			}
			for _, raw := range saved.Items {
				var state geminiContinuation
				if err := json.Unmarshal(raw, &state); err != nil {
					return nil, nil, nil, err
				}
				if state.Token != "" {
					pending = &state
					continue
				}
				pending = nil
				for _, part := range state.Content.Parts {
					var value struct {
						Call *struct {
							ID string `json:"id"`
						} `json:"functionCall"`
					}
					if err := json.Unmarshal(part, &value); err != nil {
						return nil, nil, nil, err
					}
					if value.Call != nil && value.Call.ID != "" {
						callIDs[value.Call.ID] = true
					}
				}
				result = append(result, state.Content)
			}
			continue
		}
		if pending != nil {
			return nil, nil, nil, errors.New("Gemini token continuation cannot add messages")
		}
		role, err := messageRole(message.Role)
		if err != nil {
			return nil, nil, nil, err
		}
		var parts []any
		for _, part := range message.Parts {
			if reply, ok := part.(*gent.ModelToolCallResponse); ok {
				values, err := geminiToolResponse(reply, callIDs)
				if err != nil {
					return nil, nil, nil, err
				}
				parts = append(parts, values...)
				continue
			}
			value, err := geminiPart(partValue(part), callIDs)
			if err != nil {
				return nil, nil, nil, err
			}
			if role == "system" && value["text"] == nil {
				return nil, nil, nil, errors.New("Gemini system content must be text")
			}
			parts = append(parts, value)
		}
		if role == "system" {
			if len(result) > 0 {
				return nil, nil, nil, errors.New("Gemini system messages must precede the conversation")
			}
			system = append(system, parts...)
			continue
		}
		if role == "assistant" {
			role = "model"
		}
		if role == "tool" {
			role = "user"
		}
		result = append(result, map[string]any{"role": role, "parts": parts})
	}
	return result, system, pending, nil
}

func geminiPart(part gent.ContentPart, callIDs map[string]bool) (map[string]any, error) {
	switch p := part.(type) {
	case gent.DataContent:
		return map[string]any{"text": p.String()}, nil
	case gent.TextContent:
		return map[string]any{"text": p.Text}, nil
	case gent.BinaryContent:
		if !geminiInputMIME(p.MIMEType) {
			return nil, fmt.Errorf("unsupported Gemini media %q", p.MIMEType)
		}
		return map[string]any{"inlineData": map[string]any{"mimeType": p.MIMEType,
			"data": base64.StdEncoding.EncodeToString(p.Data)}}, nil
	case gent.ImageURLContent:
		// Shared media preparation resolves URL parts before native encoding.
		return nil, errors.New("Gemini image input requires BinaryContent with a MIME type")
	case gent.ModelToolCallRequest:
		if !json.Valid([]byte(p.FunctionCall.Arguments)) {
			return nil, errors.New("invalid Gemini tool arguments")
		}
		call := map[string]any{"name": p.FunctionCall.Name,
			"args": json.RawMessage(p.FunctionCall.Arguments)}
		if p.ID != "" {
			call["id"] = p.ID
			callIDs[p.ID] = true
		}
		return map[string]any{"functionCall": call}, nil

	default:
		return nil, fmt.Errorf("unsupported Gemini content %T", part)
	}
}

func geminiInputMIME(mime string) bool {
	if mime == "application/pdf" || mime == "text/plain" {
		return true
	}
	// Explicit protocol types prevent arbitrary file bytes from being relabelled as text.
	images := "|image/png|image/jpeg|image/webp|image/heic|image/heif|"
	audio := "|audio/wav|audio/mp3|audio/mpeg|audio/aiff|audio/aac|audio/ogg|audio/flac|"
	video := "|video/mp4|video/mpeg|video/mov|video/avi|video/x-flv|video/webm|video/wmv|video/3gpp|"
	return strings.Contains(images, "|"+mime+"|") || strings.Contains(audio, "|"+mime+"|") ||
		strings.Contains(video, "|"+mime+"|")
}

// GenerateContent supports media inside a function response on Gemini 3 and newer. Older
// models receive supported media after the complete response group, with source labels.
func geminiNativeToolMedia(selected, configured string) bool {
	if selected == "" {
		selected = configured
	}
	name := strings.TrimPrefix(selected, "models/")
	return strings.HasPrefix(name, "gemini-3")
}

func geminiToolResponse(reply *gent.ModelToolCallResponse, callIDs map[string]bool) ([]any, error) {
	response := map[string]any{"name": reply.Name()}
	if callIDs[reply.ToolCallID()] {
		response["id"] = reply.ToolCallID()
	}
	var data []any
	var guidance, media []any
	var domain map[string]any
	dataCount := 0
	for i, part := range reply.Parts() {
		switch value := partValue(part).(type) {
		case gent.DataContent:
			data = append(data, value.Value())
			domain, _ = value.Value().(map[string]any)
			dataCount++
		case gent.TextContent:
			data = append(data, value.Text)
			guidance = append(guidance, map[string]any{"text": value.Text})
		case gent.BinaryContent:
			name := fmt.Sprintf("tool_part_%d", i)
			media = append(media, map[string]any{"inlineData": map[string]any{
				"mimeType": value.MIMEType, "data": base64.StdEncoding.EncodeToString(value.Data),
				"displayName": name}})
			data = append(data, map[string]any{"$ref": name})
		default:
			return nil, fmt.Errorf("unsupported Gemini function response content %T", part)
		}
	}
	if dataCount == 1 && domain != nil && len(media) == 0 && len(data) > 0 &&
		partIsData(reply.Parts()[0]) {
		response["response"] = domain
	} else {
		response["response"] = map[string]any{"result": data}
		guidance = nil
	}
	if len(media) > 0 {
		response["parts"] = media
	}
	result := []any{map[string]any{"functionResponse": response}}
	return append(result, guidance...), nil
}

func geminiToolMIME(mime string) bool {
	switch mime {
	case "image/png", "image/jpeg", "image/webp", "application/pdf", "text/plain":
		return true
	}
	return false
}

func partIsData(part gent.ContentPart) bool {
	_, ok := partValue(part).(gent.DataContent)
	return ok
}
