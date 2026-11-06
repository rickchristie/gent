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
	request *gent.ModelCallRequest,
) (gent.Stream, error) {
	request, err := prepareMedia(ctx, m.config.HTTPClient, request, "gemini")
	if err != nil {
		return nil, err
	}
	body, prior, decision, err := m.requestBody(request)
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
			*gent.ModelCallResponse, error,
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
	nativeMedia := geminiNativeToolMedia(o.Model, m.config.Model)
	contents, system, prior, err := geminiMessages(request.Messages, nativeMedia)
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
	forceTools, err := geminiTools(body, o, request.Messages)
	if err != nil {
		return nil, nil, decision, err
	}
	// Gemini rejects ANY function calling with JSON output controls. That turn must produce a
	// tool call, so it has no answer candidate to constrain. Keep the original output contract
	// for local validation and apply its native schema again when an answer is permitted.
	jsonUnavailable := ""
	if forceTools {
		jsonUnavailable = "forced tool choice has no answer candidate"
	} else if len(o.Tools) > 0 && o.ToolChoice != "none" &&
		!geminiToolJSON(o.Model, m.config.Model) {
		// Gemini 2.x also rejects JSON controls with default or AUTO function calling.
		// Keep the tools and the caller's output contract. Local validation still applies.
		jsonUnavailable = "model does not support JSON output with tools"
	}
	if jsonUnavailable != "" && o.ResponseSchema != nil {
		decision.Native = false
		decision.Reason = jsonUnavailable
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
	// GenerateContent models can accept responseFormat without enforcing its text schema.
	// Use the established JSON fields, verified by the live output-correction tests.
	if o.ResponseMIMEType != "" && o.ResponseMIMEType != "text/plain" &&
		o.ResponseMIMEType != "application/json" {
		return nil, nil, decision, fmt.Errorf("unsupported Gemini output type %q", o.ResponseMIMEType)
	}
	if jsonUnavailable == "" {
		if decision.Native {
			config["responseMimeType"] = "application/json"
			config["responseJsonSchema"] = o.ResponseSchema
		} else if o.JSONMode || o.ResponseMIMEType == "application/json" {
			config["responseMimeType"] = "application/json"
		}
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
		config["responseFormat"] = map[string]any{"image": image}
	}
	if len(config) > 0 {
		body["generationConfig"] = config
	}
	return body, prior, decision, nil
}

// GenerateContent documents combined function calling and structured output for Gemini 3 and 4.
// Unknown model aliases use local validation when tools are available. A request model override
// takes precedence, as it does for the actual HTTP endpoint.
func geminiToolJSON(selected, configured string) bool {
	if selected == "" {
		selected = configured
	}
	name := strings.TrimPrefix(selected, "models/")
	return strings.HasPrefix(name, "gemini-3") || strings.HasPrefix(name, "gemini-4")
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

func geminiTools(body map[string]any, options *gent.CallOptions,
	messages []*gent.ModelMessage,
) (bool, error) {
	var declarations []any
	for _, tool := range options.Tools {
		if tool.Type != "function" || tool.Function == nil {
			return false, errors.New("Gemini requires function tool definitions")
		}
		f := tool.Function
		declaration := map[string]any{"name": f.Name, "description": f.Description,
			"parametersJsonSchema": f.Parameters}
		// A single domain object maps directly to response. Text guidance remains outside it.
		// Other declared shapes are descriptive text because functionResponse requires an object.
		if f.OutputSchema != nil && f.OutputSchema["type"] == "object" &&
			geminiResponseSchemaCompatible(messages, f.Name) {
			declaration["responseJsonSchema"] = f.OutputSchema
		} else {
			description, err := toolDescription(f)
			if err != nil {
				return false, err
			}
			declaration["description"] = description
		}
		declarations = append(declarations, declaration)
		if f.Strict {
			return false, errors.New("Gemini does not support per-function strict settings")
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
		return false, nil
	case string:
		mode := map[string]string{"auto": "AUTO", "required": "ANY", "none": "NONE"}[choice]
		if mode == "" {
			return false, fmt.Errorf("unsupported Gemini tool choice %q", choice)
		}
		config = map[string]any{"mode": mode}
	case gent.ToolChoice:
		if choice.Type != "function" || choice.Function == nil {
			return false, errors.New("Gemini named tool choice requires a function")
		}
		config = map[string]any{"mode": "ANY", "allowedFunctionNames": []string{choice.Function.Name}}
	default:
		return false, fmt.Errorf("unsupported Gemini tool choice %T", value)
	}
	body["toolConfig"] = map[string]any{"functionCallingConfig": config}
	return config["mode"] == "ANY", nil
}

// Before the first result, an object declaration describes the author's promised domain data.
// Once a result uses the ordered multipart envelope, keep its schema in description text instead
// of declaring that the provider envelope itself is the domain object. Do not validate its values.
func geminiResponseSchemaCompatible(messages []*gent.ModelMessage, name string) bool {
	for _, message := range messages {
		for _, part := range message.Parts {
			reply, ok := part.(*gent.ModelToolCallResponse)
			if !ok || reply.Name() != name {
				continue
			}
			parts := reply.Parts()
			if len(parts) == 0 {
				return false
			}
			data, ok := partValue(parts[0]).(gent.DataContent)
			if !ok {
				return false
			}
			if _, ok := data.Value().(map[string]any); !ok {
				return false
			}
			for _, extra := range parts[1:] {
				if _, text := partValue(extra).(gent.TextContent); !text {
					return false
				}
			}
		}
	}
	return true
}
