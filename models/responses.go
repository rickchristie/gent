package models

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/rickchristie/gent"
)

// ResponsesConfig binds a Responses API endpoint. BaseURL includes /v1. HTTPClient is optional.
// Provider identifies continuation items; use different names for incompatible endpoints.
type ResponsesConfig struct {
	Provider   string
	BaseURL    string
	APIKey     string
	Model      string
	HTTPClient *http.Client
}

type Responses struct{ config ResponsesConfig }

func NewResponses(config *ResponsesConfig) *Responses {
	copy := *config
	if copy.HTTPClient == nil {
		copy.HTTPClient = http.DefaultClient
	}
	return &Responses{config: copy}
}

func NewOpenAI(apiKey, model string) *Responses {
	return NewResponses(&ResponsesConfig{Provider: "openai", BaseURL: "https://api.openai.com/v1",
		APIKey: apiKey, Model: model})
}

func NewXAI(apiKey, model string) *Responses {
	return NewResponses(&ResponsesConfig{Provider: "xai", BaseURL: "https://api.x.ai/v1",
		APIKey: apiKey, Model: model})
}

func (m *Responses) ModelName() string { return m.config.Model }
func (m *Responses) Provider() string  { return m.config.Provider }
func (m *Responses) RecoveryConfig() any {
	return map[string]any{"api": "responses", "provider": m.config.Provider,
		"baseURL": m.config.BaseURL, "model": m.config.Model}
}

func (m *Responses) GenerateContentStream(base context.Context,
	request gent.ModelCallRequest,
) (gent.Stream, error) {
	body, err := m.requestBody(&request)
	if err != nil {
		return nil, err
	}
	decision := nativeSchema(m.config.Provider, request.Options.ResponseSchema)
	hasSchema := request.Options.ResponseSchema != nil
	headers := http.Header{"Authorization": {"Bearer " + m.config.APIKey},
		"Accept": {"text/event-stream"}}
	return startProviderStream(base, m.config.HTTPClient,
		strings.TrimRight(m.config.BaseURL, "/")+"/responses", headers, body,
		func(ctx context.Context, response *http.Response, stream *gent.StreamWithDuration) (
			*gent.ContentResponse, error,
		) {
			result, err := m.readResponse(ctx, response, stream, &request.Options)
			if hasSchema {
				recordSchema(result, decision)
			}
			return result, err
		})
}

func (m *Responses) requestBody(request *gent.ModelCallRequest) (map[string]any, error) {
	o := &request.Options
	if o.TopK != 0 || o.Seed != 0 || o.MinLength != 0 || o.MaxLength != 0 ||
		o.N > 1 || o.CandidateCount > 1 || len(o.StopWords) > 0 || o.RepetitionPenalty != 0 ||
		o.FrequencyPenalty != 0 || o.PresencePenalty != 0 {
		return nil, errors.New("unsupported Responses sampling option")
	}
	input, err := m.inputItems(request.Messages)
	if err != nil {
		return nil, err
	}
	model := o.Model
	if model == "" {
		model = m.config.Model
	}
	body := map[string]any{"model": model, "input": input, "stream": true, "store": false}
	// Saved runs own the complete context. Do not depend on provider response retention.
	// Both OpenAI and Grok need these opaque items for stateless reasoning replay.
	body["include"] = []string{"reasoning.encrypted_content"}
	if o.MaxTokens > 0 {
		body["max_output_tokens"] = o.MaxTokens
	}
	if o.Temperature != nil {
		body["temperature"] = *o.Temperature
	}
	if o.TopP != nil {
		body["top_p"] = *o.TopP
	}
	if o.ResponseMIMEType != "" && o.ResponseMIMEType != "text/plain" &&
		o.ResponseMIMEType != "application/json" {
		return nil, fmt.Errorf("unsupported Responses output type %q", o.ResponseMIMEType)
	}
	tools := make([]any, 0, len(o.Tools))
	for _, tool := range o.Tools {
		if tool.Type != "function" || tool.Function == nil {
			return nil, errors.New("Responses requires function tool definitions")
		}
		f := tool.Function
		tools = append(tools, map[string]any{"type": "function", "name": f.Name,
			"description": f.Description, "parameters": f.Parameters, "strict": f.Strict})
	}
	body["tools"] = tools
	value := o.ToolChoice
	if pointer, ok := value.(*gent.ToolChoice); ok {
		value = *pointer
	}
	switch choice := value.(type) {
	case gent.ToolChoice:
		if choice.Type != "function" || choice.Function == nil {
			return nil, errors.New("Responses named tool choice requires a function")
		}
		body["tool_choice"] = map[string]any{"type": choice.Type, "name": choice.Function.Name}
	case nil:
	default:
		body["tool_choice"] = choice
	}
	if err := applyResponsesMetadata(body, o.Metadata); err != nil {
		return nil, err
	}
	if err := m.outputControls(body, o); err != nil {
		return nil, err
	}
	return body, nil
}

func applyResponsesMetadata(body, metadata map[string]any) error {
	for key := range metadata {
		if key != "thinking_config" && key != "responses" {
			return fmt.Errorf("unsupported Responses metadata %q", key)
		}
	}
	if value, exists := metadata["thinking_config"]; exists {
		config, ok := value.(*gent.ThinkingConfig)
		if !ok {
			return errors.New("invalid thinking configuration")
		}
		if config.BudgetTokens != 0 || config.InterleaveThinking {
			return errors.New("unsupported Responses thinking setting")
		}
		if config.Mode != "" && config.Mode != "auto" {
			body["reasoning"] = map[string]any{"effort": string(config.Mode)}
		}
	}
	value, exists := metadata["responses"]
	if !exists {
		return nil
	}
	options, ok := value.(map[string]any)
	if !ok {
		return errors.New("responses metadata must be an object")
	}
	for name, option := range options {
		switch name {
		case "reasoning":
			fields, ok := option.(map[string]any)
			if !ok {
				return errors.New("Responses reasoning option must be an object")
			}
			reasoning, ok := body[name].(map[string]any)
			if !ok {
				reasoning = map[string]any{}
			}
			// Provider options override matching generic fields. Merge in this fixed order so
			// independent fields such as effort and summary do not replace each other.
			for field, value := range fields {
				reasoning[field] = value
			}
			body[name] = reasoning
		case "text":
			fields, ok := option.(map[string]any)
			if !ok {
				return errors.New("Responses text option must be an object")
			}
			copy := make(map[string]any, len(fields))
			for field, value := range fields {
				if field != "verbosity" {
					return fmt.Errorf("unsupported Responses text option %q", field)
				}
				copy[field] = value
			}
			body[name] = copy
		case "parallel_tool_calls", "service_tier", "max_tool_calls":
			body[name] = option
		default:
			return fmt.Errorf("unsupported Responses option %q", name)
		}
	}
	return nil
}

func (m *Responses) inputItems(messages []*gent.ModelMessage) ([]any, error) {
	items := make([]any, 0, len(messages))
	for _, message := range messages {
		if saved := message.Continuation; saved != nil {
			if saved.Provider != m.config.Provider {
				return nil, fmt.Errorf("cannot replay %s items through %s", saved.Provider, m.config.Provider)
			}
			for _, item := range saved.Items {
				if !json.Valid(item) {
					return nil, errors.New("invalid saved provider item")
				}
				items = append(items, item)
			}
			continue
		}
		role, err := messageRole(message.Role)
		if err != nil {
			return nil, err
		}
		var content []any
		flush := func() {
			if len(content) > 0 {
				items = append(items, map[string]any{"role": role, "content": content})
				content = nil
			}
		}
		for _, part := range message.Parts {
			switch p := partValue(part).(type) {
			case gent.TextContent:
				content = append(content, map[string]any{"type": "input_text", "text": p.Text})
			case gent.ImageURLContent:
				image := map[string]any{"type": "input_image", "image_url": p.URL}
				if p.Detail != "" {
					image["detail"] = p.Detail
				}
				content = append(content, image)
			case gent.BinaryContent:
				if p.MIMEType == "application/pdf" && m.config.Provider == "openai" {
					content = append(content, map[string]any{"type": "input_file",
						"filename": "document.pdf", "file_data": p.String()})
					continue
				}
				if !strings.HasPrefix(p.MIMEType, "image/") {
					return nil, fmt.Errorf("unsupported Responses media %q", p.MIMEType)
				}
				content = append(content, map[string]any{"type": "input_image", "image_url": p.String()})
			case gent.ModelToolCall:
				flush()
				items = append(items, map[string]any{"type": "function_call", "call_id": p.ID,
					"name": p.FunctionCall.Name, "arguments": p.FunctionCall.Arguments})
			case gent.ToolCallResponse:
				flush()
				items = append(items, map[string]any{"type": "function_call_output",
					"call_id": p.ToolCallID, "output": p.Content})
			default:
				return nil, fmt.Errorf("unsupported Responses content %T", part)
			}
		}
		flush()
	}
	return items, nil
}

type responseEnvelope struct {
	Status            string `json:"status"`
	IncompleteDetails struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
	Output []json.RawMessage `json:"output"`
	Error  *responseError    `json:"error"`
	Usage  struct {
		Input        int `json:"input_tokens"`
		Output       int `json:"output_tokens"`
		Total        int `json:"total_tokens"`
		InputDetails struct {
			Cached int `json:"cached_tokens"`
		} `json:"input_tokens_details"`
		OutputDetails struct {
			Reasoning int `json:"reasoning_tokens"`
		} `json:"output_tokens_details"`
	} `json:"usage"`
}
type responseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *responseError) Error() string { return fmt.Sprintf("Responses %s: %s", e.Code, e.Message) }

func (m *Responses) readResponse(ctx context.Context, response *http.Response,
	stream *gent.StreamWithDuration, options *gent.CallOptions,
) (*gent.ContentResponse, error) {
	if !strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
		var envelope responseEnvelope
		if err := decodeProviderJSON(response.Body, &envelope); err != nil {
			return nil, err
		}
		result, err := m.convertResponse(&envelope)
		if err != nil {
			return nil, err
		}
		return previewProviderResponse(ctx, stream, options, result)
	}
	var result *gent.ContentResponse
	// An output item's phase applies to all its text deltas, including interleaved items.
	itemTypes := make(map[int]string)
	size := 0
	err := readSSE(response.Body, func(data []byte) (bool, error) {
		size += len(data)
		if size > maxProviderBytes {
			return false, errors.New("Responses response exceeds 64 MiB")
		}
		if string(data) == "[DONE]" {
			return false, errors.New("Responses stream ended without a final response")
		}
		var event struct {
			Type        string           `json:"type"`
			Delta       string           `json:"delta"`
			Response    responseEnvelope `json:"response"`
			Code        string           `json:"code"`
			Message     string           `json:"message"`
			OutputIndex int              `json:"output_index"`
			Item        struct {
				Type  string `json:"type"`
				Phase string `json:"phase"`
			} `json:"item"`
		}
		if err := json.Unmarshal(data, &event); err != nil {
			return false, err
		}
		switch event.Type {
		case "response.output_item.added":
			if event.OutputIndex < 0 || event.OutputIndex > 4096 {
				return false, errors.New("invalid Responses output index")
			}
			if event.Item.Type == "message" {
				itemTypes[event.OutputIndex] = responseMetaType(event.Item.Phase)
			}
		case "response.output_text.delta", "response.refusal.delta":
			kind := itemTypes[event.OutputIndex]
			if kind == "" {
				kind = gent.MetaContentIntermediary
			}
			return false, sendProviderText(ctx, stream, options, kind,
				fmt.Sprintf("output:%d", event.OutputIndex), event.Delta)
		case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
			return false, sendProviderText(ctx, stream, options, gent.MetaContentReasoning,
				fmt.Sprintf("output:%d", event.OutputIndex), event.Delta)
		case "response.completed", "response.incomplete", "response.failed":
			var err error
			result, err = m.convertResponse(&event.Response)
			return true, err
		case "error":
			return false, &responseError{Code: event.Code, Message: event.Message}
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (m *Responses) convertResponse(response *responseEnvelope) (*gent.ContentResponse, error) {
	if response.Error != nil {
		return nil, response.Error
	}
	if response.Status != "completed" && response.Status != "incomplete" {
		return nil, fmt.Errorf("Responses ended with status %q", response.Status)
	}
	choice := &gent.ContentChoice{Completion: gent.ModelCompletion{
		Status:         gent.ModelStatusAnswer,
		ProviderReason: response.Status,
	},
		Continuation: &gent.ProviderContinuation{
			Provider: m.config.Provider, Items: response.Output}}
	if response.Status == "incomplete" {
		choice.Completion.Status = gent.ModelStatusIncomplete
		choice.Completion.ProviderReason = response.IncompleteDetails.Reason
	}
	for index, raw := range response.Output {
		var item struct {
			Type      string `json:"type"`
			Status    string `json:"status"`
			Result    string `json:"result"`
			Format    string `json:"output_format"`
			Phase     string `json:"phase"`
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
			Summary   []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"summary"`
			Content []struct {
				Type    string `json:"type"`
				Text    string `json:"text"`
				Refusal string `json:"refusal"`
			} `json:"content"`
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, err
		}
		switch item.Type {
		case "image_generation_call":
			if item.Status != "completed" {
				if response.Status == "incomplete" {
					continue
				}
				return nil, fmt.Errorf("image generation ended with status %q", item.Status)
			}
			format := item.Format
			if format == "" {
				format = "png"
				if m.config.Provider == "xai" {
					format = "jpeg"
				}
			}
			part, err := decodeOutputImage("image/"+format, item.Result)
			if err != nil {
				return nil, err
			}
			id := fmt.Sprintf("output:%d", index)
			choice.CandidateContent = append(choice.CandidateContent, part)
			if choice.CandidateSource == nil {
				choice.CandidateSource = &gent.ContentSource{}
			}
			choice.CandidateSource.MessageIds = append(choice.CandidateSource.MessageIds, id)
			choice.MetaContent = append(choice.MetaContent, &gent.MetaContent{
				Type: gent.MetaContentIntermediary, MessageId: id, Content: []gent.ContentPart{part}})
		case "function_call":
			// The raw item is replayed without changes. A generated ID would disagree with it.
			if item.CallID == "" || item.Name == "" {
				if response.Status == "incomplete" {
					continue
				}
				return nil, errors.New("Responses function call requires call_id and name")
			}
			choice.ToolCalls = append(choice.ToolCalls, gent.ModelToolCall{ID: item.CallID, Type: "function",
				FunctionCall: &gent.FunctionCall{Name: item.Name, Arguments: item.Arguments}})
		case "message":
			kind := responseMetaType(item.Phase)
			// A new message supersedes earlier text candidates. Generated images remain ordered
			// answer parts even when the provider also emits an assistant message.
			var images []gent.ContentPart
			var imageIDs []string
			for i, part := range choice.CandidateContent {
				if _, ok := part.(gent.BinaryContent); ok {
					images = append(images, part)
					imageIDs = append(imageIDs, choice.CandidateSource.MessageIds[i])
				}
			}
			choice.CandidateContent, choice.CandidateSource = images, nil
			if len(imageIDs) > 0 {
				choice.CandidateSource = &gent.ContentSource{MessageIds: imageIDs}
			}
			var text strings.Builder
			for _, part := range item.Content {
				if part.Type != "output_text" && part.Type != "refusal" {
					return nil, fmt.Errorf("unsupported Responses output content %q", part.Type)
				}
				if part.Type == "refusal" {
					if choice.Completion.Refusal == nil {
						choice.Completion.Refusal = &gent.ModelRefusal{}
					}
					if part.Refusal != "" {
						choice.Completion.Refusal.Content = append(choice.Completion.Refusal.Content,
							gent.TextContent{Text: part.Refusal})
					}
					continue
				}
				text.WriteString(part.Text)
			}
			id := fmt.Sprintf("output:%d", index)
			choice.MetaContent = append(choice.MetaContent,
				&gent.MetaContent{
					Type:      kind,
					Content:   []gent.ContentPart{gent.TextContent{Text: text.String()}},
					MessageId: id,
				})
			if item.Phase == "" || item.Phase == "final_answer" {
				// A Responses output can contain multiple assistant messages. Only the last
				// message can be the answer candidate; earlier messages remain previews.
				choice.CandidateContent = append(choice.CandidateContent,
					gent.TextContent{Text: text.String()})
				if choice.CandidateSource == nil {
					choice.CandidateSource = &gent.ContentSource{}
				}
				choice.CandidateSource.MessageIds = append(choice.CandidateSource.MessageIds, id)
			}
		case "reasoning":
			// Opaque data stays in the raw continuation. Display only explicit visible text.
			for _, part := range item.Summary {
				if part.Type == "summary_text" {
					choice.MetaContent = append(choice.MetaContent, &gent.MetaContent{
						Type: gent.MetaContentReasoning,
						Content: []gent.ContentPart{
							gent.TextContent{Text: part.Text},
						},
						MessageId: fmt.Sprintf("output:%d", index)})
				}
			}
			for _, part := range item.Content {
				if part.Type == "reasoning_text" {
					choice.MetaContent = append(choice.MetaContent, &gent.MetaContent{
						Type: gent.MetaContentReasoning,
						Content: []gent.ContentPart{
							gent.TextContent{Text: part.Text},
						},
						MessageId: fmt.Sprintf("output:%d", index)})
				}
			}
		default:
			return nil, fmt.Errorf("unsupported Responses output item %q", item.Type)
		}
	}
	if choice.Completion.Refusal != nil {
		choice.Completion.Status = gent.ModelStatusRefused
	} else if choice.Completion.Status == gent.ModelStatusAnswer && len(choice.ToolCalls) > 0 {
		choice.Completion.Status = gent.ModelStatusToolCalls
	} else if choice.Completion.Status == gent.ModelStatusAnswer && len(choice.CandidateContent) == 0 {
		choice.Completion.Status = gent.ModelStatusContinue
	}
	if err := choice.ValidateCompletion(); err != nil {
		return nil, err
	}
	usage := response.Usage
	return &gent.ContentResponse{Choices: []*gent.ContentChoice{choice}, Info: &gent.GenerationInfo{
		InputTokens: usage.Input, OutputTokens: usage.Output, TotalTokens: usage.Total,
		CachedInputTokens: usage.InputDetails.Cached, ReasoningTokens: usage.OutputDetails.Reasoning,
	}}, nil
}

// Keep unknown labels as metadata. They must not become answer candidates by default.
func responseMetaType(phase string) string {
	if phase == "" || phase == "final_answer" {
		return gent.MetaContentIntermediary
	}
	return phase
}

func (m *Responses) outputControls(body map[string]any, options *gent.CallOptions) error {
	decision := nativeSchema(m.config.Provider, options.ResponseSchema)
	text, _ := body["text"].(map[string]any)
	if text == nil {
		text = make(map[string]any)
	}
	if decision.Native {
		text["format"] = map[string]any{"type": "json_schema", "name": "gent_output",
			"schema": options.ResponseSchema, "strict": true}
	} else if wantsJSONObject(options) {
		text["format"] = map[string]any{"type": "json_object"}
	}
	if len(text) > 0 {
		body["text"] = text
	}
	for _, modality := range options.ResponseModalities {
		if modality != "text" && modality != "image" {
			return fmt.Errorf("unsupported Responses output modality %q", modality)
		}
	}
	if !slices.Contains(options.ResponseModalities, "image") {
		return nil
	}
	if m.config.Provider != "openai" && m.config.Provider != "xai" {
		return errors.New("this Responses endpoint has no image generation adapter")
	}
	image := map[string]any{"type": "image_generation"}
	if m.config.Provider == "xai" && len(options.ImageMIMETypes) > 0 &&
		!slices.Contains(options.ImageMIMETypes, "image/jpeg") {
		return errors.New("xAI image generation requires JPEG output")
	}
	if m.config.Provider == "openai" && len(options.ImageMIMETypes) > 0 {
		for _, mime := range options.ImageMIMETypes {
			if mime == "image/png" || mime == "image/jpeg" || mime == "image/webp" {
				image["output_format"] = strings.TrimPrefix(mime, "image/")
				break
			}
		}
		if image["output_format"] == nil {
			return errors.New("OpenAI cannot generate the required image type")
		}
	}
	body["tools"] = append(body["tools"].([]any), image)
	// Let the model run application tools first when present. With no application tools, forcing
	// the native image tool avoids a text-only response to an image request.
	if len(options.Tools) == 0 && options.ToolChoice == nil {
		body["tool_choice"] = map[string]any{"type": "image_generation"}
		if m.config.Provider == "xai" {
			// xAI rejects OpenAI's image-specific choice object. With only the native image
			// tool available, "required" selects that tool without changing the output contract.
			body["tool_choice"] = "required"
		}
	}
	return nil
}
