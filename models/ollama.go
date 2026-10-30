package models

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/rickchristie/gent"
)

// Ollama uses /api/chat and its NDJSON stream, including native thinking and tool fields.
type Ollama struct{ config Config }

func NewOllama(config *Config) *Ollama {
	return &Ollama{config: providerConfig(config, "http://localhost:11434")}
}

func (m *Ollama) ModelName() string { return m.config.Model }
func (m *Ollama) Provider() string  { return "ollama" }
func (m *Ollama) RecoveryConfig() any {
	return map[string]any{"api": "chat", "provider": m.Provider(),
		"baseURL": m.config.BaseURL, "model": m.config.Model}
}

func (m *Ollama) GenerateContentStream(ctx context.Context,
	request gent.ModelCallRequest,
) (gent.Stream, error) {
	body, decision, err := m.requestBody(&request)
	if err != nil {
		return nil, err
	}
	headers := http.Header{"Accept": {"application/x-ndjson"}}
	if m.config.APIKey != "" {
		headers.Set("Authorization", "Bearer "+m.config.APIKey)
	}
	return startProviderStream(ctx, m.config.HTTPClient, m.config.BaseURL+"/api/chat",
		headers, body, func(ctx context.Context, response *http.Response,
			stream *gent.StreamWithDuration,
		) (*gent.ContentResponse, error) {
			result, err := readOllama(ctx, response, stream, &request.Options)
			recordSchema(result, decision)
			return result, err
		})
}

func (m *Ollama) requestBody(request *gent.ModelCallRequest) (
	map[string]any, schemaEnforcement, error,
) {
	o := &request.Options
	decision := nativeSchema("llamacpp", o.ResponseSchema)
	if err := textOutputOnly(o, m.Provider()); err != nil {
		return nil, decision, err
	}
	if o.MinLength != 0 || o.MaxLength != 0 || o.N > 1 || o.CandidateCount > 1 ||
		o.ToolChoice != nil {
		return nil, decision, errors.New("unsupported Ollama call option")
	}
	messages, err := ollamaMessages(request.Messages)
	if err != nil {
		return nil, decision, err
	}
	model := o.Model
	if model == "" {
		model = m.config.Model
	}
	body := map[string]any{"model": model, "messages": messages, "stream": true}
	options := make(map[string]any)
	if o.MaxTokens != 0 {
		options["num_predict"] = o.MaxTokens
	}
	if o.Temperature != nil {
		options["temperature"] = *o.Temperature
	}
	if o.TopP != nil {
		options["top_p"] = *o.TopP
	}
	if o.TopK != 0 {
		options["top_k"] = o.TopK
	}
	if o.Seed != 0 {
		options["seed"] = o.Seed
	}
	if o.RepetitionPenalty != 0 {
		options["repeat_penalty"] = o.RepetitionPenalty
	}
	if o.FrequencyPenalty != 0 {
		options["frequency_penalty"] = o.FrequencyPenalty
	}
	if o.PresencePenalty != 0 {
		options["presence_penalty"] = o.PresencePenalty
	}
	if len(o.StopWords) > 0 {
		options["stop"] = o.StopWords
	}
	if len(options) > 0 {
		body["options"] = options
	}
	if len(o.Tools) > 0 {
		for _, tool := range o.Tools {
			if tool.Type != "function" || tool.Function == nil || tool.Function.Strict {
				return nil, decision, errors.New("Ollama requires non-strict function tools")
			}
		}
		body["tools"] = o.Tools
	}
	if decision.Native {
		body["format"] = o.ResponseSchema
	} else if wantsJSONObject(o) {
		body["format"] = "json"
	}
	if err := ollamaMetadata(body, o.Metadata); err != nil {
		return nil, decision, err
	}
	return body, decision, nil
}

func ollamaMetadata(body, metadata map[string]any) error {
	for key := range metadata {
		if key != "thinking_config" && key != "ollama" {
			return fmt.Errorf("unsupported Ollama metadata %q", key)
		}
	}
	if value, exists := metadata["thinking_config"]; exists {
		config, ok := value.(*gent.ThinkingConfig)
		if !ok {
			return errors.New("invalid thinking configuration")
		}
		if config.BudgetTokens != 0 || config.InterleaveThinking {
			return errors.New("unsupported Ollama thinking setting")
		}
		if config.Mode != "" && config.Mode != "auto" {
			body["think"] = string(config.Mode)
		}
	}
	if value, exists := metadata["ollama"]; exists {
		options, ok := value.(map[string]any)
		if !ok {
			return errors.New("ollama metadata must be an object")
		}
		for field, value := range options {
			if field != "think" && field != "keep_alive" {
				return fmt.Errorf("unsupported Ollama option %q", field)
			}
			body[field] = value
		}
	}
	return nil
}

func ollamaMessages(messages []*gent.ModelMessage) ([]any, error) {
	result := make([]any, 0, len(messages))
	for _, message := range messages {
		if saved := message.Continuation; saved != nil {
			if err := checkContinuation(saved, "ollama"); err != nil {
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
		var text strings.Builder
		var images []string
		var calls []any
		for _, part := range message.Parts {
			switch p := partValue(part).(type) {
			case gent.TextContent:
				text.WriteString(p.Text)
			case gent.BinaryContent:
				if !strings.HasPrefix(p.MIMEType, "image/") {
					return nil, fmt.Errorf("unsupported Ollama media %q", p.MIMEType)
				}
				images = append(images, base64.StdEncoding.EncodeToString(p.Data))
			case gent.ModelToolCall:
				if !json.Valid([]byte(p.FunctionCall.Arguments)) {
					return nil, errors.New("invalid Ollama tool arguments")
				}
				calls = append(calls, map[string]any{"function": map[string]any{
					"name": p.FunctionCall.Name, "arguments": json.RawMessage(p.FunctionCall.Arguments)}})
			case gent.ToolCallResponse:
				if role != "tool" || len(message.Parts) != 1 {
					return nil, errors.New("Ollama requires one tool result per tool message")
				}
				value["tool_name"] = p.Name
				text.WriteString(p.Content)
			default:
				return nil, fmt.Errorf("unsupported Ollama content %T", part)
			}
		}
		value["content"] = text.String()
		if len(images) > 0 {
			value["images"] = images
		}
		if len(calls) > 0 {
			value["tool_calls"] = calls
		}
		result = append(result, value)
	}
	return result, nil
}

type ollamaEnvelope struct {
	Message ollamaMessage `json:"message"`
	Done    bool          `json:"done"`
	Reason  string        `json:"done_reason"`
	Input   int           `json:"prompt_eval_count"`
	Cached  int           `json:"prompt_eval_cached_count"`
	Output  int           `json:"eval_count"`
	Error   string        `json:"error"`
}

type ollamaMessage struct {
	Role     string            `json:"role"`
	Content  string            `json:"content"`
	Thinking string            `json:"thinking"`
	Calls    []json.RawMessage `json:"tool_calls"`
	raw      json.RawMessage
}

func (message *ollamaMessage) UnmarshalJSON(data []byte) error {
	type fields ollamaMessage
	if err := json.Unmarshal(data, (*fields)(message)); err != nil {
		return err
	}
	message.raw = bytes.Clone(data)
	return nil
}

func readOllama(ctx context.Context, response *http.Response,
	stream *gent.StreamWithDuration, options *gent.CallOptions,
) (*gent.ContentResponse, error) {
	if !strings.Contains(response.Header.Get("Content-Type"), "ndjson") {
		var envelope ollamaEnvelope
		if err := decodeProviderJSON(response.Body, &envelope); err != nil {
			return nil, err
		}
		if envelope.Error != "" {
			return nil, errors.New(envelope.Error)
		}
		if !envelope.Done {
			return nil, errors.New("Ollama response is not done")
		}
		result, err := convertOllama(&envelope)
		if err != nil {
			return nil, err
		}
		return previewProviderResponse(ctx, stream, options, result)
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64<<10), maxProviderBytes)
	var text, thinking strings.Builder
	var calls []json.RawMessage
	fields := make(map[string]json.RawMessage)
	size := 0
	for scanner.Scan() {
		data := scanner.Bytes()
		if len(bytes.TrimSpace(data)) == 0 {
			continue
		}
		size += len(data)
		if size > maxProviderBytes {
			return nil, errors.New("Ollama response exceeds 64 MiB")
		}
		var event ollamaEnvelope
		if err := json.Unmarshal(data, &event); err != nil {
			return nil, err
		}
		if event.Error != "" {
			return nil, errors.New(event.Error)
		}
		var delta map[string]json.RawMessage
		if err := json.Unmarshal(event.Message.raw, &delta); err != nil {
			return nil, err
		}
		for field, raw := range delta {
			fields[field] = raw
		}
		text.WriteString(event.Message.Content)
		thinking.WriteString(event.Message.Thinking)
		calls = append(calls, event.Message.Calls...)
		if err := sendProviderText(ctx, stream, options, gent.MetaContentReasoning,
			"thinking", event.Message.Thinking); err != nil {
			return nil, err
		}
		if err := sendProviderText(ctx, stream, options, gent.MetaContentIntermediary,
			"content", event.Message.Content); err != nil {
			return nil, err
		}
		if event.Done {
			event.Message.Content, event.Message.Thinking = text.String(), thinking.String()
			event.Message.Calls = calls
			// Replay native fields as received. Only streamed strings and calls need assembly.
			for field, value := range map[string]any{"content": text.String(),
				"thinking": thinking.String(), "tool_calls": calls} {
				if _, present := fields[field]; !present {
					continue
				}
				raw, err := json.Marshal(value)
				if err != nil {
					return nil, err
				}
				fields[field] = raw
			}
			raw, err := json.Marshal(fields)
			if err != nil {
				return nil, err
			}
			event.Message.raw = raw
			return convertOllama(&event)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, errors.New("Ollama stream ended without done")
}

func convertOllama(response *ollamaEnvelope) (*gent.ContentResponse, error) {
	choice := &gent.ContentChoice{Continuation: &gent.ProviderContinuation{
		Provider: "ollama", Items: []json.RawMessage{response.Message.raw}},
		Completion: gent.ModelCompletion{ProviderReason: response.Reason}}
	switch response.Reason {
	case "stop":
		choice.Completion.Status = gent.ModelStatusAnswer
	case "length":
		choice.Completion.Status = gent.ModelStatusIncomplete
	default:
		return nil, fmt.Errorf("unknown Ollama done reason %q", response.Reason)
	}
	for _, raw := range response.Message.Calls {
		var call struct {
			Function struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"function"`
		}
		if err := json.Unmarshal(raw, &call); err != nil {
			return nil, err
		}
		if call.Function.Name == "" {
			if choice.Completion.Status == gent.ModelStatusIncomplete {
				continue
			}
			return nil, errors.New("Ollama tool call has no name")
		}
		// Ollama correlates results by tool_name. Gent supplies a local call ID when it prepares
		// the execution plan; that ID is never inserted into these opaque provider items.
		choice.ToolCalls = append(choice.ToolCalls, gent.ModelToolCall{Type: "function",
			FunctionCall: &gent.FunctionCall{Name: call.Function.Name,
				Arguments: string(call.Function.Arguments)}})
	}
	if len(choice.ToolCalls) > 0 && choice.Completion.Status == gent.ModelStatusAnswer {
		choice.Completion.Status = gent.ModelStatusToolCalls
	}
	if response.Message.Content != "" {
		choice.CandidateContent = []gent.ContentPart{gent.TextContent{Text: response.Message.Content}}
		choice.CandidateSource = &gent.ContentSource{MessageIds: []string{"content"}}
		choice.MetaContent = append(choice.MetaContent, &gent.MetaContent{
			Type: gent.MetaContentIntermediary, MessageId: "content",
			Content: gent.CloneContentParts(choice.CandidateContent)})
	}
	if response.Message.Thinking != "" {
		choice.MetaContent = append(choice.MetaContent, &gent.MetaContent{
			Type: gent.MetaContentReasoning, MessageId: "thinking",
			Content: []gent.ContentPart{gent.TextContent{Text: response.Message.Thinking}}})
	}
	if err := choice.ValidateCompletion(); err != nil {
		return nil, err
	}
	return &gent.ContentResponse{Choices: []*gent.ContentChoice{choice}, Info: &gent.GenerationInfo{
		InputTokens: response.Input, OutputTokens: response.Output,
		TotalTokens: response.Input + response.Output, CachedInputTokens: response.Cached,
	}}, nil
}
