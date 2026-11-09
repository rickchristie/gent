package models

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"cloud.google.com/go/auth"
	"cloud.google.com/go/auth/credentials"
)

// VertexAIConfig selects Google's Gemini publisher models on Vertex AI. Model is the short model
// ID, for example gemini-3.8-flash. Project is the project that receives the model usage charges.
type VertexAIConfig struct {
	Project, Location, Model string
	// BaseURL is the service root, including the API version. The default uses v1 and the
	// selected location. Set it only for a trusted proxy or a test server: it receives tokens.
	BaseURL    string
	HTTPClient *http.Client
	// Supply at most one explicit credential source. File and JSON accept service accounts.
	// With no source, use Application Default Credentials, including workload identity or
	// GOOGLE_APPLICATION_CREDENTIALS. Only load credential configuration from trusted sources.
	CredentialsFile string
	CredentialsJSON []byte
	// TokenProvider is optional and must be uncached. Its Token method must use the supplied
	// context for all I/O and return after that work stops. It must not start refresh workers.
	// Google token caches, including caches inside auth.Credentials, are rejected because their
	// refresh mode cannot be changed. Use File, JSON, or ADC for Google's cached credentials.
	TokenProvider auth.TokenProvider
	// QuotaProject overrides the quota project from ADC. It does not change the model project.
	QuotaProject string
}

// VertexAI shares Gemini content mapping, with OAuth authentication and a project endpoint.
// It supports concurrent calls. Token refresh follows the request context and starts no worker.
type VertexAI struct {
	generateContentModel
	project, location string
}

// NewVertexAI loads credentials but does not request an access token or call a model.
// Location defaults to global. Project and Model are required.
func NewVertexAI(config *VertexAIConfig) (*VertexAI, error) {
	if config.Project == "" || config.Model == "" {
		return nil, errors.New("Vertex AI requires Project and Model")
	}
	location := config.Location
	if location == "" {
		location = "global"
	}
	for _, char := range location {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
			return nil, errors.New("Vertex AI location must contain lowercase letters, digits, or hyphens")
		}
	}
	baseURL := config.BaseURL
	if baseURL == "" {
		host := "aiplatform.googleapis.com"
		if location != "global" {
			host = location + "-" + host
		}
		baseURL = "https://" + host + "/v1"
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New(
			"Vertex AI BaseURL must be an HTTP service root without query or user info")
	}
	baseURL = strings.TrimRight(baseURL, "/") + "/projects/" + url.PathEscape(config.Project) +
		"/locations/" + location + "/publishers/google"
	common := providerConfig(&Config{BaseURL: baseURL, Model: config.Model,
		HTTPClient: config.HTTPClient}, "")
	authentication, err := newVertexAuth(config, common.HTTPClient)
	if err != nil {
		return nil, err
	}
	return &VertexAI{project: config.Project, location: location,
		generateContentModel: generateContentModel{config: common, provider: "vertexai",
			authorize: authentication.authorize}}, nil
}

// Recovery records include the target project but exclude credential files, tokens, and keys.
func (m *VertexAI) RecoveryConfig() any {
	return map[string]any{"api": "generateContent", "provider": m.Provider(),
		"baseURL": m.config.BaseURL, "model": m.config.Model,
		"project": m.project, "location": m.location}
}

type vertexAuth struct {
	tokens       auth.TokenProvider
	credentials  *auth.Credentials
	quotaProject string
	gate         chan struct{}
}

func newVertexAuth(config *VertexAIConfig, client *http.Client) (*vertexAuth, error) {
	sources := 0
	if config.CredentialsFile != "" {
		sources++
	}
	if len(config.CredentialsJSON) > 0 {
		sources++
	}
	if config.TokenProvider != nil {
		sources++
	}
	if sources > 1 {
		return nil, errors.New("Vertex AI accepts only one explicit credential source")
	}
	result := &vertexAuth{quotaProject: config.QuotaProject, gate: make(chan struct{}, 1)}
	provider := config.TokenProvider
	if provider == nil {
		options := &credentials.DetectOptions{Client: client, DisableAsyncRefresh: true,
			Scopes: []string{"https://www.googleapis.com/auth/cloud-platform"}}
		// Keep a stable JSON snapshot for later refresh after the caller releases its buffer.
		file, data := config.CredentialsFile, bytes.Clone(config.CredentialsJSON)
		load := func() (*auth.Credentials, error) {
			switch {
			case file != "":
				return credentials.NewCredentialsFromFile(credentials.ServiceAccount, file, options)
			case len(data) > 0:
				return credentials.NewCredentialsFromJSON(credentials.ServiceAccount, data, options)
			default:
				return credentials.DetectDefault(options)
			}
		}
		var err error
		result.credentials, err = load()
		if err != nil {
			return nil, fmt.Errorf("load Vertex AI credentials: %w", err)
		}
		provider = &vertexCredentialSource{first: result.credentials, load: load}
	}
	for {
		credential, ok := provider.(*auth.Credentials)
		if !ok {
			break
		}
		provider = credential.TokenProvider
	}
	result.tokens = auth.NewCachedTokenProvider(provider,
		&auth.CachedTokenProviderOptions{DisableAsyncRefresh: true})
	// Google's constructor returns an existing cache unchanged and ignores the new options.
	// Reject that source before it can start a refresh with context.Background().
	if result.tokens == provider {
		return nil, errors.New(
			"Vertex AI TokenProvider must be uncached; use CredentialsFile, CredentialsJSON, or ADC")
	}
	return result, nil
}

type vertexCredentialSource struct {
	first *auth.Credentials
	load  func() (*auth.Credentials, error)
}

func (s *vertexCredentialSource) Token(ctx context.Context) (*auth.Token, error) {
	credential := s.first
	s.first = nil
	if credential == nil {
		var err error
		credential, err = s.load()
		if err != nil {
			return nil, err
		}
	}
	// Auth v0.18.2 ignores DisableAsyncRefresh for file credentials. Use each inner cache once
	// and let our synchronous outer cache own refresh. This keeps token I/O on the call context.
	return credential.Token(ctx)
}

func (a *vertexAuth) authorize(ctx context.Context, headers http.Header) error {
	// Google's synchronous cache holds a mutex during refresh. Wait outside that mutex so a
	// canceled caller can return while another caller still waits for the token endpoint.
	select {
	case a.gate <- struct{}{}:
		defer func() { <-a.gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	token, err := a.tokens.Token(ctx)
	if err != nil {
		return fmt.Errorf("authenticate Vertex AI: %w", err)
	}
	if token == nil || token.Value == "" ||
		(!token.Expiry.IsZero() && !token.Expiry.After(time.Now())) {
		return errors.New("Vertex AI credential returned an empty or expired token")
	}
	if token.Type != "" && !strings.EqualFold(token.Type, "Bearer") {
		return errors.New("Vertex AI requires a bearer access token")
	}
	quota := a.quotaProject
	if quota == "" && a.credentials != nil {
		quota, err = a.credentials.QuotaProjectID(ctx)
		if err != nil {
			return fmt.Errorf("load Vertex AI quota project: %w", err)
		}
	}
	headers.Set("Authorization", "Bearer "+token.Value)
	if quota != "" {
		headers.Set("X-Goog-User-Project", quota)
	}
	return nil
}
