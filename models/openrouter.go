package models

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rickchristie/gent"
)

type routerModel struct {
	Architecture struct {
		Input  []string `json:"input_modalities"`
		Output []string `json:"output_modalities"`
	} `json:"architecture"`
	Endpoints []struct {
		Name       string   `json:"provider_name"`
		Tag        string   `json:"tag"`
		Parameters []string `json:"supported_parameters"`
	} `json:"endpoints"`
}

type routerLookup struct {
	done          chan struct{}
	model         routerModel
	err           error
	expires       time.Time
	ownerCanceled bool
}

// Cache discovery briefly and bound the number of models. Concurrent runs share the lookup,
// while each caller can cancel its wait. HTTP errors are returned, never treated as missing
// support.
type routerCatalog struct {
	mu      sync.Mutex
	entries map[string]*routerLookup
}

func (m *Chat) routerModel(ctx context.Context, model string) (routerModel, error) {
	for {
		m.router.mu.Lock()
		entry := m.router.entries[model]
		if entry != nil && (entry.expires.IsZero() || time.Now().Before(entry.expires)) {
			m.router.mu.Unlock()
			select {
			case <-ctx.Done():
				return routerModel{}, ctx.Err()
			case <-entry.done:
				// The shared request belongs to its first caller. A live waiter starts or joins a
				// fresh lookup when that caller cancels; it must not inherit another run's deadline.
				if entry.ownerCanceled && ctx.Err() == nil {
					continue
				}
				return entry.model, entry.err
			}
		}
		entry = &routerLookup{done: make(chan struct{})}
		if m.router.entries == nil {
			m.router.entries = make(map[string]*routerLookup)
		}
		if len(m.router.entries) >= 64 {
			for key, cached := range m.router.entries {
				if !cached.expires.IsZero() {
					delete(m.router.entries, key)
					break
				}
			}
		}
		if len(m.router.entries) >= 64 {
			m.router.mu.Unlock()
			return m.fetchRouterModel(ctx, model)
		}
		m.router.entries[model] = entry
		m.router.mu.Unlock()
		entry.model, entry.err = m.fetchRouterModel(ctx, model)
		m.router.mu.Lock()
		entry.expires = time.Now().Add(time.Minute)
		entry.ownerCanceled = ctx.Err() != nil
		if entry.err != nil {
			delete(m.router.entries, model)
		}
		close(entry.done)
		m.router.mu.Unlock()
		return entry.model, entry.err
	}
}

func (m *Chat) fetchRouterModel(ctx context.Context, model string) (routerModel, error) {
	author, slug, ok := strings.Cut(model, "/")
	if !ok || author == "" || slug == "" {
		return routerModel{}, errors.New("OpenRouter model must be author/slug")
	}
	endpoint := m.config.BaseURL + "/models/" + url.PathEscape(author) + "/" +
		url.PathEscape(slug) + "/endpoints"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return routerModel{}, err
	}
	request.Header.Set("Authorization", "Bearer "+m.config.APIKey)
	response, err := m.config.HTTPClient.Do(request)
	if err != nil {
		return routerModel{}, err
	}
	var envelope struct {
		Data routerModel `json:"data"`
	}
	readErr := checkHTTPResponse(response)
	if readErr == nil {
		readErr = decodeProviderJSON(response.Body, &envelope)
	}
	return envelope.Data, errors.Join(readErr, response.Body.Close())
}

func (m *Chat) openRouterControls(ctx context.Context, body map[string]any,
	request *gent.ModelCallRequest,
) (schemaEnforcement, error) {
	o := &request.Options
	decision := nativeSchema("", o.ResponseSchema)
	if o.ResponseMIMEType != "" && o.ResponseMIMEType != "text/plain" &&
		o.ResponseMIMEType != "application/json" {
		return decision, fmt.Errorf("unsupported OpenRouter output type %q", o.ResponseMIMEType)
	}
	model, err := m.routerModel(ctx, body["model"].(string))
	if err != nil {
		return decision, err
	}
	for _, modality := range o.ResponseModalities {
		if !slices.Contains(model.Architecture.Output, modality) {
			return decision, fmt.Errorf("OpenRouter model does not support %q output", modality)
		}
	}
	if len(o.ResponseModalities) > 0 {
		body["modalities"] = o.ResponseModalities
	}
	for _, message := range request.Messages {
		for _, part := range message.Parts {
			modality := ""
			switch p := partValue(part).(type) {
			case gent.ImageURLContent:
				modality = "image"
			case gent.BinaryContent:
				modality = "image"
				if p.MIMEType == "application/pdf" {
					modality = "file"
				}
			}
			if modality != "" && !slices.Contains(model.Architecture.Input, modality) {
				return decision, fmt.Errorf("OpenRouter model does not support %q input", modality)
			}
		}
	}
	preferences := make(map[string]any)
	if raw, exists := body["provider"]; exists {
		object, ok := raw.(map[string]any)
		if !ok {
			return decision, errors.New("OpenRouter provider preferences must be an object")
		}
		for key, value := range object {
			preferences[key] = value
		}
	}
	only, err := routerStringList(preferences["only"])
	if err != nil {
		return decision, err
	}
	ignore, err := routerStringList(preferences["ignore"])
	if err != nil {
		return decision, err
	}
	var eligible []string
	jsonMode := false
	for _, endpoint := range model.Endpoints {
		tag := endpoint.Tag
		if tag == "" {
			tag = strings.ToLower(endpoint.Name)
		}
		if len(only) > 0 && !slices.Contains(only, tag) && !slices.Contains(only, endpoint.Name) {
			continue
		}
		if slices.Contains(ignore, tag) || slices.Contains(ignore, endpoint.Name) {
			continue
		}
		jsonMode = jsonMode || slices.Contains(endpoint.Parameters, "response_format")
		if !slices.Contains(endpoint.Parameters, "structured_outputs") {
			continue
		}
		profile := routerSchemaProfile(endpoint.Name)
		candidate := nativeSchema(profile, o.ResponseSchema)
		if candidate.Native {
			eligible = append(eligible, tag)
		}
	}
	preferences["require_parameters"] = true
	if len(eligible) > 0 {
		preferences["only"] = eligible
		body["response_format"] = chatSchemaFormat(o.ResponseSchema)
		decision = schemaEnforcement{Native: true, Profile: "openrouter endpoints"}
	} else if wantsJSONObject(o) && jsonMode {
		body["response_format"] = map[string]any{"type": "json_object"}
	} else {
		delete(body, "response_format")
	}
	body["provider"] = preferences
	return decision, nil
}

func routerSchemaProfile(provider string) string {
	switch strings.ToLower(provider) {
	case "openai", "azure":
		return "openai"
	case "anthropic":
		return "anthropic"
	case "google", "google ai studio", "google vertex":
		return "gemini"
	case "xai", "x-ai":
		return "xai"
	default:
		// Endpoint discovery establishes schema support. Limit unknown engines to the common
		// schema subset; provider-specific constraints require a known grammar profile.
		return "portable"
	}
}

func routerStringList(value any) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	if list, ok := value.([]string); ok {
		return list, nil
	}
	if list, ok := value.([]any); ok {
		result := make([]string, 0, len(list))
		for _, value := range list {
			text, ok := value.(string)
			if !ok {
				return nil, errors.New("OpenRouter provider list must contain strings")
			}
			result = append(result, text)
		}
		return result, nil
	}
	return nil, errors.New("OpenRouter provider list must be an array")
}
