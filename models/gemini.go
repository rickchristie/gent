package models

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/rickchristie/gent"
)

// Gemini uses streamGenerateContent. Native parts retain thought signatures across tool turns.
type Gemini struct{ config Config }

func NewGemini(config *Config) *Gemini {
	return &Gemini{config: providerConfig(config, "https://generativelanguage.googleapis.com/v1beta")}
}

func (m *Gemini) ModelName() string { return m.config.Model }
func (m *Gemini) Provider() string  { return "gemini" }
func (m *Gemini) RecoveryConfig() any {
	return map[string]any{"api": "generateContent", "provider": m.Provider(),
		"baseURL": m.config.BaseURL, "model": m.config.Model}
}

func (m *Gemini) GenerateContentStream(ctx context.Context,
	request gent.ModelCallRequest,
) (gent.Stream, error) {
	body, prior, decision, err := m.requestBody(&request)
	if err != nil {
		return nil, err
	}
	model := request.Options.Model
	if model == "" {
		model = m.config.Model
	}
	endpoint := m.config.BaseURL + "/models/" + url.PathEscape(strings.TrimPrefix(model, "models/")) +
		":streamGenerateContent?alt=sse"
	headers := http.Header{"X-Goog-Api-Key": {m.config.APIKey}, "Accept": {"text/event-stream"}}
	return startProviderStream(ctx, m.config.HTTPClient, endpoint, headers, body,
		func(ctx context.Context, response *http.Response, stream *gent.StreamWithDuration) (
			*gent.ContentResponse, error,
		) {
			result, err := m.readResponse(ctx, response, stream, &request.Options, body, prior)
			recordSchema(result, decision)
			return result, err
		})
}

func (m *Gemini) requestBody(request *gent.ModelCallRequest) (
	map[string]any, *geminiContinuation, schemaEnforcement, error,
) {
	o := &request.Options
	decision := nativeSchema("gemini", o.ResponseSchema)
	if o.MinLength != 0 || o.MaxLength != 0 || o.N > 1 || o.CandidateCount > 1 ||
		o.RepetitionPenalty != 0 {
		return nil, nil, decision, errors.New("unsupported Gemini sampling option")
	}
	contents, system, prior, err := geminiMessages(request.Messages)
	if err != nil {
		return nil, nil, decision, err
	}
	body := map[string]any{"contents": contents}
	if len(system) > 0 {
		body["systemInstruction"] = map[string]any{"parts": system}
	}
	if prior != nil {
		body["contents"] = prior.RequestContents
		body["continuationToken"] = prior.Token
	}
	config := make(map[string]any)
	if o.MaxTokens != 0 {
		config["maxOutputTokens"] = o.MaxTokens
	}
	if o.Temperature != nil {
		config["temperature"] = *o.Temperature
	}
	if o.TopP != nil {
		config["topP"] = *o.TopP
	}
	if o.TopK != 0 {
		config["topK"] = o.TopK
	}
	if o.Seed != 0 {
		config["seed"] = o.Seed
	}
	if o.PresencePenalty != 0 {
		config["presencePenalty"] = o.PresencePenalty
	}
	if o.FrequencyPenalty != 0 {
		config["frequencyPenalty"] = o.FrequencyPenalty
	}
	if len(o.StopWords) > 0 {
		config["stopSequences"] = o.StopWords
	}
	if err := geminiMetadata(body, config, o.Metadata); err != nil {
		return nil, nil, decision, err
	}
	format := make(map[string]any)
	if decision.Native {
		format["text"] = map[string]any{"mimeType": "APPLICATION_JSON", "schema": o.ResponseSchema}
	} else if o.JSONMode || o.ResponseMIMEType == "application/json" {
		format["text"] = map[string]any{"mimeType": "APPLICATION_JSON"}
	} else if o.ResponseMIMEType != "" && o.ResponseMIMEType != "text/plain" {
		return nil, nil, decision, fmt.Errorf("unsupported Gemini output type %q", o.ResponseMIMEType)
	}
	var modalities []string
	for _, modality := range o.ResponseModalities {
		if modality != "text" && modality != "image" {
			return nil, nil, decision, fmt.Errorf("unsupported Gemini output modality %q", modality)
		}
		modalities = append(modalities, strings.ToUpper(modality))
	}
	if len(modalities) > 0 {
		config["responseModalities"] = modalities
	}
	if slices.Contains(o.ResponseModalities, "image") {
		image := map[string]any{"delivery": "INLINE"}
		if slices.Contains(o.ImageMIMETypes, "image/jpeg") {
			image["mimeType"] = "IMAGE_JPEG"
		}
		format["image"] = image
	}
	if len(format) > 0 {
		config["responseFormat"] = format
	}
	if len(config) > 0 {
		body["generationConfig"] = config
	}
	if err := geminiTools(body, o); err != nil {
		return nil, nil, decision, err
	}
	return body, prior, decision, nil
}

func geminiMetadata(body, config, metadata map[string]any) error {
	for key := range metadata {
		if key != "thinking_config" && key != "gemini" {
			return fmt.Errorf("unsupported Gemini metadata %q", key)
		}
	}
	if value, exists := metadata["thinking_config"]; exists {
		thinking, ok := value.(*gent.ThinkingConfig)
		if !ok {
			return errors.New("invalid thinking configuration")
		}
		if thinking.InterleaveThinking {
			return errors.New("Gemini controls interleaved thinking natively")
		}
		fields := make(map[string]any)
		if thinking.BudgetTokens != 0 {
			fields["thinkingBudget"] = thinking.BudgetTokens
		}
		if thinking.Mode != "" && thinking.Mode != "auto" {
			fields["thinkingLevel"] = strings.ToUpper(string(thinking.Mode))
		}
		if thinking.ReturnThinking || thinking.StreamThinking {
			fields["includeThoughts"] = true
		}
		if len(fields) > 0 {
			config["thinkingConfig"] = fields
		}
	}
	if value, exists := metadata["gemini"]; exists {
		options, ok := value.(map[string]any)
		if !ok {
			return errors.New("gemini metadata must be an object")
		}
		for field, value := range options {
			switch field {
			case "thinkingConfig", "imageConfig", "mediaResolution":
				config[field] = value
			case "cachedContent", "serviceTier":
				body[field] = value
			default:
				return fmt.Errorf("unsupported Gemini option %q", field)
			}
		}
	}
	return nil
}

func geminiTools(body map[string]any, options *gent.CallOptions) error {
	var declarations []any
	for _, tool := range options.Tools {
		if tool.Type != "function" || tool.Function == nil {
			return errors.New("Gemini requires function tool definitions")
		}
		f := tool.Function
		declarations = append(declarations, map[string]any{"name": f.Name,
			"description": f.Description, "parametersJsonSchema": f.Parameters})
		if f.Strict {
			return errors.New("Gemini does not support per-function strict settings")
		}
	}
	if len(declarations) > 0 {
		body["tools"] = []any{map[string]any{"functionDeclarations": declarations}}
	}
	value := options.ToolChoice
	if pointer, ok := value.(*gent.ToolChoice); ok {
		value = *pointer
	}
	var config map[string]any
	switch choice := value.(type) {
	case nil:
		return nil
	case string:
		mode := map[string]string{"auto": "AUTO", "required": "ANY", "none": "NONE"}[choice]
		if mode == "" {
			return fmt.Errorf("unsupported Gemini tool choice %q", choice)
		}
		config = map[string]any{"mode": mode}
	case gent.ToolChoice:
		if choice.Type != "function" || choice.Function == nil {
			return errors.New("Gemini named tool choice requires a function")
		}
		config = map[string]any{"mode": "ANY", "allowedFunctionNames": []string{choice.Function.Name}}
	default:
		return fmt.Errorf("unsupported Gemini tool choice %T", value)
	}
	body["toolConfig"] = map[string]any{"functionCallingConfig": config}
	return nil
}
