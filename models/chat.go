package models

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/rickchristie/gent"
)

// Chat shares the Chat Completions wire protocol. Each constructor selects provider-specific
// request controls, content support, and continuation identity. It is not an OpenAI adapter.
type Chat struct {
	config   Config
	provider string
	router   routerCatalog
}

func NewDeepSeek(config *Config) *Chat {
	return &Chat{provider: "deepseek", config: providerConfig(config, "https://api.deepseek.com/v1")}
}

func NewVLLM(config *Config) *Chat {
	return &Chat{provider: "vllm", config: providerConfig(config, "http://localhost:8000/v1")}
}

func NewSGLang(config *Config) *Chat {
	return &Chat{provider: "sglang", config: providerConfig(config, "http://localhost:30000/v1")}
}

func NewLlamaCpp(config *Config) *Chat {
	return &Chat{provider: "llamacpp", config: providerConfig(config, "http://localhost:8080/v1")}
}

func NewOpenRouter(config *Config) *Chat {
	return &Chat{provider: "openrouter",
		config: providerConfig(config, "https://openrouter.ai/api/v1")}
}

func (m *Chat) ModelName() string { return m.config.Model }
func (m *Chat) Provider() string  { return m.provider }
func (m *Chat) RecoveryConfig() any {
	return map[string]any{"api": "chat", "provider": m.provider,
		"baseURL": m.config.BaseURL, "model": m.config.Model}
}

func (m *Chat) GenerateContentStream(ctx context.Context,
	request gent.ModelCallRequest,
) (gent.Stream, error) {
	body, decision, err := m.requestBody(&request)
	if err != nil {
		return nil, err
	}
	if m.provider == "openrouter" {
		decision, err = m.openRouterControls(ctx, body, &request)
		if err != nil {
			return nil, err
		}
	}
	headers := http.Header{"Accept": {"text/event-stream"}}
	if m.config.APIKey != "" {
		headers.Set("Authorization", "Bearer "+m.config.APIKey)
	}
	return startProviderStream(ctx, m.config.HTTPClient, m.config.BaseURL+"/chat/completions",
		headers, body, func(ctx context.Context, response *http.Response,
			stream *gent.StreamWithDuration,
		) (*gent.ContentResponse, error) {
			result, err := m.readResponse(ctx, response, stream, &request.Options)
			recordSchema(result, decision)
			return result, err
		})
}

func (m *Chat) requestBody(request *gent.ModelCallRequest) (
	map[string]any, schemaEnforcement, error,
) {
	o := &request.Options
	profile := ""
	switch m.provider {
	case "vllm", "sglang":
		profile = "xgrammar"
	case "llamacpp":
		profile = "llamacpp"
	}
	decision := nativeSchema(profile, o.ResponseSchema)
	if m.provider != "openrouter" {
		if err := textOutputOnly(o, m.provider); err != nil {
			return nil, decision, err
		}
	}
	if o.MinLength != 0 || o.MaxLength != 0 || o.N > 1 || o.CandidateCount > 1 {
		return nil, decision, errors.New("unsupported Chat sampling option")
	}
	if m.provider == "deepseek" && (o.TopK != 0 || o.Seed != 0 || o.RepetitionPenalty != 0) {
		return nil, decision, errors.New("unsupported DeepSeek sampling option")
	}
	messages, err := m.messages(request.Messages)
	if err != nil {
		return nil, decision, err
	}
	model := o.Model
	if model == "" {
		model = m.config.Model
	}
	body := map[string]any{"model": model, "messages": messages, "stream": true,
		"stream_options": map[string]any{"include_usage": true}}
	if o.MaxTokens != 0 {
		body["max_tokens"] = o.MaxTokens
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
	if o.Seed != 0 {
		body["seed"] = o.Seed
	}
	if o.RepetitionPenalty != 0 {
		field := "repetition_penalty"
		if m.provider == "llamacpp" {
			field = "repeat_penalty"
		}
		body[field] = o.RepetitionPenalty
	}
	if o.FrequencyPenalty != 0 {
		body["frequency_penalty"] = o.FrequencyPenalty
	}
	if o.PresencePenalty != 0 {
		body["presence_penalty"] = o.PresencePenalty
	}
	if len(o.StopWords) > 0 {
		body["stop"] = o.StopWords
	}
	if len(o.Tools) > 0 {
		for _, tool := range o.Tools {
			if tool.Type != "function" || tool.Function == nil {
				return nil, decision, errors.New("Chat requires function tool definitions")
			}
		}
		body["tools"] = o.Tools
	}
	if o.ToolChoice != nil {
		body["tool_choice"] = o.ToolChoice
	}
	if err := m.metadata(body, o.Metadata); err != nil {
		return nil, decision, err
	}
	if decision.Native {
		body["response_format"] = chatSchemaFormat(o.ResponseSchema)
		if m.provider == "llamacpp" {
			body["response_format"] = map[string]any{"type": "json_schema",
				"schema": o.ResponseSchema}
		}
	} else if wantsJSONObject(o) {
		body["response_format"] = map[string]any{"type": "json_object"}
	}
	return body, decision, nil
}

func chatSchemaFormat(schema map[string]any) map[string]any {
	return map[string]any{"type": "json_schema", "json_schema": map[string]any{
		"name": "gent_output", "strict": true, "schema": schema}}
}

func wantsJSONObject(options *gent.CallOptions) bool {
	if options.ResponseSchema != nil {
		return options.ResponseSchema["type"] == "object"
	}
	return options.JSONMode || options.ResponseMIMEType == "application/json"
}

func (m *Chat) metadata(body, metadata map[string]any) error {
	for key := range metadata {
		if key != "thinking_config" && key != m.provider {
			return fmt.Errorf("unsupported %s metadata %q", m.provider, key)
		}
	}
	if value, exists := metadata["thinking_config"]; exists {
		config, ok := value.(*gent.ThinkingConfig)
		if !ok {
			return errors.New("invalid thinking configuration")
		}
		if config.BudgetTokens != 0 || config.InterleaveThinking {
			return fmt.Errorf("unsupported %s thinking setting", m.provider)
		}
		if config.Mode != "" && config.Mode != "auto" {
			body["reasoning_effort"] = string(config.Mode)
		}
	}
	if value, exists := metadata[m.provider]; exists {
		options, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s metadata must be an object", m.provider)
		}
		for key, value := range options {
			allowed := key == "reasoning_effort" || key == "parallel_tool_calls"
			switch m.provider {
			case "deepseek":
				allowed = key == "thinking" || key == "reasoning_effort"
			case "vllm", "sglang", "llamacpp":
				allowed = allowed || key == "chat_template_kwargs"
			case "openrouter":
				allowed = allowed || key == "provider" || key == "reasoning" || key == "image_config"
			}
			if !allowed {
				return fmt.Errorf("unsupported %s option %q", m.provider, key)
			}
			body[key] = value
		}
	}
	return nil
}
