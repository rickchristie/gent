package models

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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

func NewGrok(apiKey, model string) *Responses {
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
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(base)
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(m.config.BaseURL, "/")+"/responses", bytes.NewReader(data))
	if err != nil {
		cancel()
		return nil, err
	}
	httpRequest.Header.Set("Authorization", "Bearer "+m.config.APIKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "text/event-stream")
	stream, err := gent.NewBoundedStreamWithDuration(ctx, 64, cancel)
	if err != nil {
		cancel()
		return nil, err
	}
	go func() {
		defer cancel()
		response, err := m.config.HTTPClient.Do(httpRequest)
		if err != nil {
			stream.Complete(nil, err)
			return
		}
		result, readErr := m.readResponse(ctx, response, stream, &request.Options)
		closeErr := response.Body.Close()
		if result != nil {
			result.Info.Duration = stream.Duration()
		}
		stream.Complete(result, errors.Join(readErr, closeErr))
	}()
	return stream, nil
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
	if o.JSONMode || o.ResponseMIMEType == "application/json" {
		body["text"] = map[string]any{"format": map[string]any{"type": "json_object"}}
	} else if o.ResponseMIMEType != "" && o.ResponseMIMEType != "text/plain" {
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
	switch choice := o.ToolChoice.(type) {
	case gent.ToolChoice:
		body["tool_choice"] = map[string]any{"type": choice.Type, "name": choice.Function.Name}
	case *gent.ToolChoice:
		body["tool_choice"] = map[string]any{"type": choice.Type, "name": choice.Function.Name}
	case nil:
	default:
		body["tool_choice"] = choice
	}
	if err := applyResponsesMetadata(body, o.Metadata); err != nil {
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
		case "parallel_tool_calls", "text", "service_tier", "max_tool_calls":
			body[name] = option
		default:
			return fmt.Errorf("unsupported Responses option %q", name)
		}
	}
	return nil
}

func (m *Responses) inputItems(messages []gent.ModelMessage) ([]any, error) {
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
		role := string(message.Role)
		if message.Role == gent.ChatMessageTypeHuman {
			role = "user"
		}
		if message.Role == gent.ChatMessageTypeAI {
			role = "assistant"
		}
		var content []any
		flush := func() {
			if len(content) > 0 {
				items = append(items, map[string]any{"role": role, "content": content})
				content = nil
			}
		}
		for _, part := range message.Parts {
			switch p := part.(type) {
			case gent.TextContent:
				content = append(content, map[string]any{"type": "input_text", "text": p.Text})
			case gent.ImageURLContent:
				image := map[string]any{"type": "input_image", "image_url": p.URL}
				if p.Detail != "" {
					image["detail"] = p.Detail
				}
				content = append(content, image)
			case gent.BinaryContent:
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
	Status string            `json:"status"`
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
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, err := io.ReadAll(io.LimitReader(response.Body, 64*1024))
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("Responses HTTP %d: %s",
			response.StatusCode, strings.TrimSpace(string(data)))
	}
	if !strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
		var envelope responseEnvelope
		decoder := json.NewDecoder(io.LimitReader(response.Body, 32*1024*1024))
		if err := decoder.Decode(&envelope); err != nil {
			return nil, err
		}
		result, err := m.convertResponse(&envelope)
		if err != nil {
			return nil, err
		}
		for _, item := range result.Choices[0].MetaContent {
			if !stream.SendMeta(item) {
				return nil, ctx.Err()
			}
		}
		return result, nil
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64*1024), 32*1024*1024)
	var data strings.Builder
	var result *gent.ContentResponse
	// An output item's phase applies to all its text deltas, including interleaved items.
	itemTypes := make(map[int]string)
	consume := func() error {
		if data.Len() == 0 {
			return nil
		}
		payload := data.String()
		data.Reset()
		if payload == "[DONE]" {
			return nil
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
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			return err
		}
		switch event.Type {
		case "response.output_item.added":
			if event.Item.Type == "message" {
				itemTypes[event.OutputIndex] = responseMetaType(event.Item.Phase)
			}
		case "response.output_text.delta", "response.refusal.delta":
			kind := itemTypes[event.OutputIndex]
			if kind == "" {
				kind = gent.MetaContentIntermediary
			}
			if kind == gent.MetaContentIntermediary && options.StreamingFunc != nil {
				if err := options.StreamingFunc(ctx, []byte(event.Delta)); err != nil {
					return err
				}
			}
			if !stream.SendMeta(&gent.MetaContent{Type: kind, Text: event.Delta,
				MessageId: fmt.Sprintf("output:%d", event.OutputIndex)}) {
				return ctx.Err()
			}
		case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
			if options.StreamingReasoningFunc != nil {
				if err := options.StreamingReasoningFunc(ctx, []byte(event.Delta), nil); err != nil {
					return err
				}
			}
			if !stream.SendMeta(&gent.MetaContent{Type: gent.MetaContentReasoning, Text: event.Delta,
				MessageId: fmt.Sprintf("output:%d", event.OutputIndex)}) {
				return ctx.Err()
			}
		case "response.completed", "response.incomplete", "response.failed":
			var err error
			result, err = m.convertResponse(&event.Response)
			return err
		case "error":
			return &responseError{Code: event.Code, Message: event.Message}
		}
		return nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := consume(); err != nil {
				return nil, err
			}
			if result != nil {
				return result, nil
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			if data.Len()+len(line) > 32*1024*1024 {
				return nil, errors.New("Responses event exceeds 32 MiB")
			}
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if err := consume(); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.New("Responses stream ended without a final response")
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
	choice := &gent.ContentChoice{StopReason: "stop", Continuation: &gent.ProviderContinuation{
		Provider: m.config.Provider, Items: response.Output}}
	if response.Status == "incomplete" {
		choice.StopReason = "length"
	}
	for index, raw := range response.Output {
		var item struct {
			Type      string `json:"type"`
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
		case "function_call":
			// The raw item is replayed without changes. A generated ID would disagree with it.
			if item.CallID == "" || item.Name == "" {
				return nil, errors.New("Responses function call requires call_id and name")
			}
			choice.ToolCalls = append(choice.ToolCalls, gent.ModelToolCall{ID: item.CallID, Type: "function",
				FunctionCall: &gent.FunctionCall{Name: item.Name, Arguments: item.Arguments}})
		case "message":
			kind := responseMetaType(item.Phase)
			choice.CandidateContent, choice.CandidateSource = "", nil
			var text strings.Builder
			for _, part := range item.Content {
				if part.Type != "output_text" && part.Type != "refusal" {
					return nil, fmt.Errorf("unsupported Responses output content %q", part.Type)
				}
				text.WriteString(part.Text + part.Refusal)
			}
			id := fmt.Sprintf("output:%d", index)
			choice.MetaContent = append(choice.MetaContent,
				&gent.MetaContent{Type: kind, Text: text.String(), MessageId: id})
			if item.Phase == "" || item.Phase == "final_answer" {
				// A Responses output can contain multiple assistant messages. Only the last
				// message can be the answer candidate; earlier messages remain previews.
				choice.CandidateContent = text.String()
				choice.CandidateSource = &gent.ContentSource{MessageIds: []string{id}}
			}
		case "reasoning":
			// Opaque data stays in the raw continuation. Display only explicit visible text.
			for _, part := range item.Summary {
				if part.Type == "summary_text" {
					choice.MetaContent = append(choice.MetaContent, &gent.MetaContent{
						Type: gent.MetaContentReasoning, Text: part.Text,
						MessageId: fmt.Sprintf("output:%d", index)})
				}
			}
			for _, part := range item.Content {
				if part.Type == "reasoning_text" {
					choice.MetaContent = append(choice.MetaContent, &gent.MetaContent{
						Type: gent.MetaContentReasoning, Text: part.Text,
						MessageId: fmt.Sprintf("output:%d", index)})
				}
			}
		default:
			return nil, fmt.Errorf("unsupported Responses output item %q", item.Type)
		}
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
