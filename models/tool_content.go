package models

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/internal/modelcontext"
)

// Each endpoint owns its input grammar. This normalization retains data parts and replaces only
// content that cannot reach that endpoint. URL downloads are compatibility work, not snapshots.
func prepareMedia(ctx context.Context, client *http.Client, request *gent.ModelCallRequest,
	endpoint string,
) (*gent.ModelCallRequest, error) {
	var prepared *gent.ModelCallRequest
	steering := request.Options.SteeringMessages.Resolved()
	for i, message := range request.Messages {
		// Adapters replay the saved native items, not Parts. Do not load media that is unused
		// or replace the evidence attached to a provider continuation.
		if message.Continuation != nil {
			continue
		}
		path := fmt.Sprintf("messages[%d].parts", i)
		parts, changed, err := prepareMediaParts(ctx, client, message.Parts, endpoint, message.Role,
			steering, path)
		if err != nil {
			return nil, err
		}
		if !changed {
			continue
		}
		if prepared == nil {
			copy := *request
			copy.Messages = append([]*gent.ModelMessage(nil), request.Messages...)
			prepared = &copy
		}
		next := *message
		next.Parts = parts
		prepared.Messages[i] = &next
	}
	if prepared == nil {
		return request, nil
	}
	return prepared, nil
}

func prepareMediaParts(ctx context.Context, client *http.Client, parts []gent.ContentPart,
	endpoint string, role gent.ChatMessageType, steering gent.SteeringMessages, path string,
) ([]gent.ContentPart, bool, error) {
	result := parts
	changed := false
	// Responses assistant content only accepts output text or refusals. Use the same steering
	// contract as other unsupported media; sending input media here would reject the whole call.
	textOnly := role == gent.ChatMessageTypeAI && (endpoint == "openai" || endpoint == "xai")
	for i, part := range parts {
		original := part
		reason := ""
		partChanged := false
		var diagnostic error
		location := fmt.Sprintf("%s[%d]", path, i)
		switch value := partValue(part).(type) {
		case *gent.ModelToolCallResponse:
			nested, updated, err := prepareMediaParts(ctx, client, value.Parts(), endpoint,
				gent.ChatMessageTypeTool, steering, location+".parts")
			if err != nil {
				return nil, false, err
			}
			if updated {
				part, err = value.WithParts(nested...)
				if err != nil {
					return nil, false, err
				}
				partChanged = true
			}
		case gent.CachedContent:
			nested, updated, err := prepareMediaParts(ctx, client, []gent.ContentPart{value.ContentPart},
				endpoint, role, steering, location+".cached")
			if err != nil {
				return nil, false, err
			}
			if updated {
				value.ContentPart = nested[0]
				part, partChanged = value, true
			}
		case gent.ImageURLContent:
			if textOnly {
				part = unsupportedMedia(steering, "image URL: "+value.URL)
				reason = "unsupported_media"
			}
			if endpoint == "gemini" || endpoint == "ollama" {
				image, err := loadImage(ctx, client, value.URL)
				if ctx.Err() != nil {
					return nil, false, ctx.Err()
				}
				if err != nil {
					part = gent.TextPart(steering.ImageURLLoadFailed + " URL: " + value.URL)
					reason, diagnostic = "image_url_load_failed", err
				} else if !inputMIME(endpoint, image.MIMEType) {
					part = unsupportedMedia(steering, image.MIMEType)
					reason = "unsupported_media"
				} else {
					part, reason = image, "image_url_loaded"
				}
			}
			if endpoint == "deepseek" {
				part = unsupportedMedia(steering, "image URL: "+value.URL)
				reason = "unsupported_media"
			}
		case gent.BinaryContent:
			if textOnly || !inputMIME(endpoint, value.MIMEType) {
				part = unsupportedMedia(steering, value.MIMEType)
				reason = "unsupported_media"
			}
		}
		if reason != "" {
			partChanged = true
			err := modelcontext.Record(ctx, map[string]any{"original": original,
				"prepared": part, "reason": reason, "path": location,
				"error": gent.SaveError(diagnostic)})
			if err != nil {
				return nil, false, err
			}
		}
		if !partChanged {
			continue
		}
		if !changed {
			result = append([]gent.ContentPart(nil), parts...)
			changed = true
		}
		result[i] = part
	}
	return result, changed, nil
}

func unsupportedMedia(steering gent.SteeringMessages, mime string) gent.ContentPart {
	return gent.TextPart(steering.UnsupportedMedia + " MIME type: " + mime + ".")
}

func inputMIME(endpoint, mime string) bool {
	if endpoint == "deepseek" {
		return false
	}
	if endpoint == "gemini" {
		return geminiInputMIME(mime)
	}
	if mime == "application/pdf" {
		return endpoint == "openai" || endpoint == "anthropic" || endpoint == "openrouter"
	}
	switch mime {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return true
	}
	return false
}

func loadImage(base context.Context, client *http.Client, address string) (
	gent.BinaryContent, error,
) {
	uri, err := url.Parse(address)
	if err != nil || (uri.Scheme != "http" && uri.Scheme != "https") {
		return gent.BinaryContent{}, fmt.Errorf("image URL must use HTTP or HTTPS")
	}
	ctx, cancel := context.WithTimeout(base, 30*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return gent.BinaryContent{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return gent.BinaryContent{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return gent.BinaryContent{}, fmt.Errorf("image load returned HTTP %d", response.StatusCode)
	}
	const limit = 32 << 20
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return gent.BinaryContent{}, err
	}
	if len(data) > limit {
		return gent.BinaryContent{}, fmt.Errorf("image exceeds 32 MiB")
	}
	mime := http.DetectContentType(data)
	if !strings.HasPrefix(mime, "image/") {
		return gent.BinaryContent{}, fmt.Errorf("URL did not return an image")
	}
	return gent.BinaryContent{MIMEType: mime, Data: data}, nil
}

// Text-only tool protocols require media in a following user message. Flush after the whole
// tool-result group so no user message separates responses to one assistant's batch.
func separateToolMedia(messages []*gent.ModelMessage) ([]*gent.ModelMessage, error) {
	return separateSelectedToolMedia(messages, func(gent.ContentPart) bool { return true })
}

func separateSelectedToolMedia(messages []*gent.ModelMessage,
	move func(gent.ContentPart) bool,
) ([]*gent.ModelMessage, error) {
	var result []*gent.ModelMessage
	var pending []gent.ContentPart
	flush := func() {
		if len(pending) > 0 {
			result = append(result, &gent.ModelMessage{Role: gent.ChatMessageTypeHuman, Parts: pending})
			pending = nil
		}
	}
	for _, message := range messages {
		if message.Role != gent.ChatMessageTypeTool {
			flush()
			result = append(result, message)
			continue
		}
		next := *message
		next.Parts = make([]gent.ContentPart, 0, len(message.Parts))
		for _, part := range message.Parts {
			reply, ok := part.(*gent.ModelToolCallResponse)
			if !ok {
				next.Parts = append(next.Parts, part)
				continue
			}
			var text, media []gent.ContentPart
			for _, content := range reply.Parts() {
				switch partValue(content).(type) {
				case gent.TextContent, gent.DataContent:
					text = append(text, content)
				default:
					if move(content) {
						media = append(media, content)
					} else {
						text = append(text, content)
					}
				}
			}
			if len(media) > 0 {
				pending = append(pending, gent.TextPart(fmt.Sprintf("Media from %s (call %s):",
					reply.Name(), reply.ToolCallID())))
				pending = append(pending, media...)
			}
			adjusted, err := reply.WithParts(text...)
			if err != nil {
				return nil, err
			}
			next.Parts = append(next.Parts, adjusted)
		}
		result = append(result, &next)
	}
	flush()
	return result, nil
}

func toolDescription(function *gent.FunctionDefinition) (string, error) {
	if function.OutputSchema == nil {
		return function.Description, nil
	}
	schema, err := json.Marshal(function.OutputSchema)
	if err != nil {
		return "", err
	}
	description := function.Description + "\n\nDomain output schema (descriptive only):\n"
	return description + string(schema), nil
}

func describedTools(tools []*gent.ToolDefinition) ([]*gent.ToolDefinition, error) {
	result := make([]*gent.ToolDefinition, len(tools))
	for i, tool := range tools {
		function := *tool.Function
		description, err := toolDescription(&function)
		if err != nil {
			return nil, err
		}
		function.Description = description
		function.OutputSchema = nil
		result[i] = &gent.ToolDefinition{Type: tool.Type, Function: &function}
	}
	return result, nil
}

func toolResponseIsError(reply *gent.ModelToolCallResponse) bool {
	if reply.Kind() == gent.ToolResponseCallError || reply.Kind() == gent.ToolResponseRecoveryError {
		return true
	}
	status, present := reply.ExecutionStatus()
	return present && status == gent.ToolInvalidArguments
}
