package models

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rickchristie/gent"
)

// Anthropic uses the Messages API. Signed thinking blocks stay in opaque continuation items.
type Anthropic struct{ config Config }

func NewAnthropic(config *Config) *Anthropic {
	return &Anthropic{config: providerConfig(config, "https://api.anthropic.com/v1")}
}

func (m *Anthropic) ModelName() string { return m.config.Model }
func (m *Anthropic) Provider() string  { return "anthropic" }
func (m *Anthropic) RecoveryConfig() any {
	return map[string]any{"api": "messages", "provider": m.Provider(),
		"baseURL": m.config.BaseURL, "model": m.config.Model}
}

func (m *Anthropic) GenerateContentStream(ctx context.Context,
	request gent.ModelCallRequest,
) (gent.Stream, error) {
	body, decision, err := m.requestBody(&request)
	if err != nil {
		return nil, err
	}
	headers := http.Header{"X-Api-Key": {m.config.APIKey},
		"Anthropic-Version": {"2023-06-01"}, "Accept": {"text/event-stream"}}
	return startProviderStream(ctx, m.config.HTTPClient, m.config.BaseURL+"/messages",
		headers, body, func(ctx context.Context, response *http.Response,
			stream *gent.StreamWithDuration,
		) (*gent.ContentResponse, error) {
			result, err := readAnthropic(ctx, response, stream, &request.Options)
			recordSchema(result, decision)
			return result, err
		})
}

func (m *Anthropic) requestBody(request *gent.ModelCallRequest) (
	map[string]any, schemaEnforcement, error,
) {
	o := &request.Options
	decision := nativeSchema("anthropic", o.ResponseSchema)
	if err := textOutputOnly(o, m.Provider()); err != nil {
		return nil, decision, err
	}
	if o.Seed != 0 || o.MinLength != 0 || o.MaxLength != 0 || o.N > 1 ||
		o.CandidateCount > 1 || o.RepetitionPenalty != 0 || o.FrequencyPenalty != 0 ||
		o.PresencePenalty != 0 {
		return nil, decision, errors.New("unsupported Anthropic sampling option")
	}
	messages, system, err := anthropicMessages(request.Messages)
	if err != nil {
		return nil, decision, err
	}
	model := o.Model
	if model == "" {
		model = m.config.Model
	}
	maxTokens := o.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}
	body := map[string]any{"model": model, "messages": messages,
		"max_tokens": maxTokens, "stream": true}
	if len(system) > 0 {
		body["system"] = system
	}
	if o.Temperature != nil {
		body["temperature"] = *o.Temperature
	}
	if o.TopP != nil {
		body["top_p"] = *o.TopP
	}
	if o.TopK != 0 {
		body["top_k"] = o.TopK
	}
	if len(o.StopWords) > 0 {
		body["stop_sequences"] = o.StopWords
	}
	tools := make([]any, 0, len(o.Tools))
	strictCount := 0
	strictTotals := schemaTotals{}
	for _, tool := range o.Tools {
		if tool.Type != "function" || tool.Function == nil {
			return nil, decision, errors.New("Anthropic requires function tool definitions")
		}
		f := tool.Function
		parameters := f.Parameters
		if parameters == nil {
			parameters = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		definition := map[string]any{"name": f.Name, "description": f.Description,
			"input_schema": parameters}
		if f.Strict {
			strictCount++
			data, err := json.Marshal(parameters)
			if err != nil {
				return nil, decision, err
			}
			var schema map[string]any
			if err := json.Unmarshal(data, &schema); err != nil {
				return nil, decision, err
			}
			strictTotals.add(schema)
			definition["strict"] = true
		}
		tools = append(tools, definition)
	}
	if strictCount > 20 {
		return nil, decision, errors.New("Anthropic supports at most 20 strict tools")
	}
	if reason := strictTotals.check("anthropic"); reason != "" {
		return nil, decision, errors.New(reason)
	}
	if decision.Native {
		// Output can use local validation when the combined grammar exceeds native limits.
		// Explicit strict tool definitions retain their contract and are never weakened.
		data, err := json.Marshal(o.ResponseSchema)
		if err != nil {
			return nil, decision, err
		}
		var schema map[string]any
		if err := json.Unmarshal(data, &schema); err != nil {
			return nil, decision, err
		}
		strictTotals.add(schema)
		if reason := strictTotals.check("anthropic"); reason != "" {
			decision.Native, decision.Reason = false, reason
		}
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}
	if err := anthropicToolChoice(body, o.ToolChoice); err != nil {
		return nil, decision, err
	}
	if err := anthropicMetadata(body, o.Metadata); err != nil {
		return nil, decision, err
	}
	// Format belongs to Output. Provider options may add effort, but cannot replace the schema.
	if decision.Native {
		config, _ := body["output_config"].(map[string]any)
		if config == nil {
			config = make(map[string]any)
		}
		config["format"] = map[string]any{"type": "json_schema", "schema": o.ResponseSchema}
		body["output_config"] = config
	}
	return body, decision, nil
}

func anthropicToolChoice(body map[string]any, value any) error {
	if value == nil {
		return nil
	}
	if pointer, ok := value.(*gent.ToolChoice); ok {
		value = *pointer
	}
	switch choice := value.(type) {
	case string:
		if choice == "required" {
			choice = "any"
		}
		if choice != "auto" && choice != "any" && choice != "none" {
			return fmt.Errorf("unsupported Anthropic tool choice %q", choice)
		}
		body["tool_choice"] = map[string]any{"type": choice}
	case gent.ToolChoice:
		if choice.Type != "function" || choice.Function == nil {
			return errors.New("Anthropic named tool choice requires a function")
		}
		body["tool_choice"] = map[string]any{"type": "tool", "name": choice.Function.Name}
	default:
		return fmt.Errorf("unsupported Anthropic tool choice %T", value)
	}
	return nil
}

func anthropicMetadata(body, metadata map[string]any) error {
	for key := range metadata {
		if key != "thinking_config" && key != "anthropic" {
			return fmt.Errorf("unsupported Anthropic metadata %q", key)
		}
	}
	if value, exists := metadata["thinking_config"]; exists {
		config, ok := value.(*gent.ThinkingConfig)
		if !ok {
			return errors.New("invalid thinking configuration")
		}
		if config.InterleaveThinking {
			return errors.New("use a model with native interleaved thinking")
		}
		if config.BudgetTokens > 0 {
			body["thinking"] = map[string]any{"type": "enabled", "budget_tokens": config.BudgetTokens}
		} else if config.Mode != "" && config.Mode != "auto" {
			body["thinking"] = map[string]any{"type": string(config.Mode)}
		}
	}
	value, exists := metadata["anthropic"]
	if !exists {
		return nil
	}
	options, ok := value.(map[string]any)
	if !ok {
		return errors.New("anthropic metadata must be an object")
	}
	// Apply explicit provider settings after generic settings, independent of map iteration.
	for field, option := range options {
		switch field {
		case "thinking", "service_tier":
			body[field] = option
		case "output_config":
			config, ok := option.(map[string]any)
			if !ok {
				return errors.New("Anthropic output_config must be an object")
			}
			copy := make(map[string]any, len(config))
			for name, value := range config {
				if name != "effort" {
					return fmt.Errorf("unsupported Anthropic output_config %q", name)
				}
				copy[name] = value
			}
			body[field] = copy
		default:
			return fmt.Errorf("unsupported Anthropic option %q", field)
		}
	}
	return nil
}

func anthropicMessages(messages []*gent.ModelMessage) ([]any, []any, error) {
	result := make([]any, 0, len(messages))
	var system []any
	for _, message := range messages {
		role, err := messageRole(message.Role)
		if err != nil {
			return nil, nil, err
		}
		var content []any
		if saved := message.Continuation; saved != nil {
			if err := checkContinuation(saved, "anthropic"); err != nil {
				return nil, nil, err
			}
			for _, item := range saved.Items {
				content = append(content, item)
			}
		} else {
			for _, part := range message.Parts {
				block, err := anthropicPart(partValue(part))
				if err != nil {
					return nil, nil, err
				}
				if role == "system" && block["type"] != "text" {
					return nil, nil, errors.New("Anthropic system content must be text")
				}
				content = append(content, block)
			}
		}
		if role == "system" {
			if len(result) > 0 {
				return nil, nil, errors.New("Anthropic system messages must precede the conversation")
			}
			system = append(system, content...)
			continue
		}
		if role == "tool" {
			role = "user"
		}
		result = append(result, map[string]any{"role": role, "content": content})
	}
	return result, system, nil
}

func anthropicPart(part gent.ContentPart) (map[string]any, error) {
	switch p := part.(type) {
	case gent.CachedContent:
		block, err := anthropicPart(partValue(p.ContentPart))
		if err != nil {
			return nil, err
		}
		control := map[string]any{"type": p.CacheControl.Type}
		switch p.CacheControl.Duration {
		case 0, 5 * time.Minute:
		case time.Hour:
			control["ttl"] = "1h"
		default:
			return nil, errors.New("unsupported Anthropic cache duration")
		}
		block["cache_control"] = control
		return block, nil
	case gent.TextContent:
		return map[string]any{"type": "text", "text": p.Text}, nil
	case gent.ImageURLContent:
		if p.Detail != "" {
			return nil, errors.New("Anthropic does not support image detail")
		}
		return map[string]any{"type": "image",
			"source": map[string]any{"type": "url", "url": p.URL}}, nil
	case gent.BinaryContent:
		kind := "image"
		switch p.MIMEType {
		case "image/png", "image/jpeg", "image/gif", "image/webp":
		case "application/pdf":
			kind = "document"
		default:
			return nil, fmt.Errorf("unsupported Anthropic media %q", p.MIMEType)
		}
		return map[string]any{"type": kind, "source": map[string]any{
			"type": "base64", "media_type": p.MIMEType,
			"data": base64.StdEncoding.EncodeToString(p.Data)}}, nil
	case gent.ModelToolCall:
		var input map[string]any
		decoder := json.NewDecoder(strings.NewReader(p.FunctionCall.Arguments))
		decoder.UseNumber()
		if err := decoder.Decode(&input); err != nil {
			return nil, err
		}
		return map[string]any{"type": "tool_use", "id": p.ID,
			"name": p.FunctionCall.Name, "input": input}, nil
	case gent.ToolCallResponse:
		return map[string]any{"type": "tool_result", "tool_use_id": p.ToolCallID,
			"content": p.Content}, nil
	default:
		return nil, fmt.Errorf("unsupported Anthropic content %T", part)
	}
}

type anthropicEnvelope struct {
	ID         string            `json:"id"`
	Content    []json.RawMessage `json:"content"`
	StopReason string            `json:"stop_reason"`
	Usage      anthropicUsage    `json:"usage"`
}

type anthropicUsage struct {
	Input      int `json:"input_tokens"`
	Output     int `json:"output_tokens"`
	CacheRead  int `json:"cache_read_input_tokens"`
	CacheWrite int `json:"cache_creation_input_tokens"`
}

func readAnthropic(ctx context.Context, response *http.Response,
	stream *gent.StreamWithDuration, options *gent.CallOptions,
) (*gent.ContentResponse, error) {
	var envelope anthropicEnvelope
	if !strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
		if err := decodeProviderJSON(response.Body, &envelope); err != nil {
			return nil, err
		}
		result, err := convertAnthropic(&envelope)
		if err != nil {
			return nil, err
		}
		return previewProviderResponse(ctx, stream, options, result)
	}
	state := anthropicStream{envelope: &envelope, ctx: ctx, stream: stream, options: options}
	if err := readSSE(response.Body, state.consume); err != nil {
		return nil, err
	}
	return convertAnthropic(&envelope)
}

func convertAnthropic(response *anthropicEnvelope) (*gent.ContentResponse, error) {
	choice := &gent.ContentChoice{Continuation: &gent.ProviderContinuation{
		Provider: "anthropic", Items: response.Content},
		Completion: gent.ModelCompletion{ProviderReason: response.StopReason}}
	switch response.StopReason {
	case "end_turn", "stop_sequence":
		choice.Completion.Status = gent.ModelStatusAnswer
	case "tool_use":
		choice.Completion.Status = gent.ModelStatusToolCalls
	case "pause_turn":
		choice.Completion.Status = gent.ModelStatusContinue
	case "max_tokens", "model_context_window_exceeded":
		choice.Completion.Status = gent.ModelStatusIncomplete
	case "refusal":
		choice.Completion.Status = gent.ModelStatusRefused
		choice.Completion.Refusal = &gent.ModelRefusal{Code: response.StopReason}
	default:
		return nil, fmt.Errorf("unknown Anthropic stop reason %q", response.StopReason)
	}
	for index, raw := range response.Content {
		var block struct {
			Type     string          `json:"type"`
			Text     string          `json:"text"`
			Thinking string          `json:"thinking"`
			ID       string          `json:"id"`
			Name     string          `json:"name"`
			Input    json.RawMessage `json:"input"`
		}
		if err := json.Unmarshal(raw, &block); err != nil {
			return nil, err
		}
		id := fmt.Sprintf("block:%d", index)
		switch block.Type {
		case "text":
			part := gent.TextContent{Text: block.Text}
			choice.CandidateContent = append(choice.CandidateContent, part)
			if choice.CandidateSource == nil {
				choice.CandidateSource = &gent.ContentSource{}
			}
			choice.CandidateSource.MessageIds = append(choice.CandidateSource.MessageIds, id)
			choice.MetaContent = append(choice.MetaContent, &gent.MetaContent{
				Type: gent.MetaContentIntermediary, MessageId: id, Content: []gent.ContentPart{part}})
		case "thinking":
			choice.MetaContent = append(choice.MetaContent, &gent.MetaContent{
				Type: gent.MetaContentReasoning, MessageId: id,
				Content: []gent.ContentPart{gent.TextContent{Text: block.Thinking}}})
		case "tool_use":
			if block.ID == "" || block.Name == "" {
				if choice.Completion.Status == gent.ModelStatusIncomplete {
					continue
				}
				return nil, errors.New("Anthropic tool use requires id and name")
			}
			choice.ToolCalls = append(choice.ToolCalls, gent.ModelToolCall{ID: block.ID,
				Type: "function", FunctionCall: &gent.FunctionCall{
					Name: block.Name, Arguments: string(block.Input)}})
		case "redacted_thinking", "server_tool_use", "web_search_tool_result", "web_fetch_tool_result":
			// These are provider-owned blocks, not instructions to Gent's tool registry.
		default:
			return nil, fmt.Errorf("unsupported Anthropic block %q", block.Type)
		}
	}
	if choice.Completion.Refusal != nil {
		choice.Completion.Refusal.Content = gent.CloneContentParts(choice.CandidateContent)
		choice.CandidateContent, choice.CandidateSource = nil, nil
	}
	if err := choice.ValidateCompletion(); err != nil {
		return nil, err
	}
	u := response.Usage
	input := u.Input + u.CacheRead + u.CacheWrite
	return &gent.ContentResponse{Choices: []*gent.ContentChoice{choice}, Info: &gent.GenerationInfo{
		InputTokens: input, OutputTokens: u.Output, TotalTokens: input + u.Output,
		CachedInputTokens: u.CacheRead,
		RawGenerationInfo: map[string]any{"cache_creation_input_tokens": u.CacheWrite},
	}}, nil
}
