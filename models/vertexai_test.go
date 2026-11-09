package models

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cloud.google.com/go/auth"
	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

type vertexTestToken func(context.Context) (*auth.Token, error)

func (f vertexTestToken) Token(ctx context.Context) (*auth.Token, error) { return f(ctx) }

func fixtureVertex(config *Config) *VertexAI {
	model, err := NewVertexAI(&VertexAIConfig{Project: "test-project", Model: config.Model,
		BaseURL: config.BaseURL, HTTPClient: config.HTTPClient,
		TokenProvider: vertexTestToken(func(context.Context) (*auth.Token, error) {
			return &auth.Token{Value: "fixture-key"}, nil
		})})
	if err != nil {
		panic(err)
	}
	return model
}

func vertexTestRequest() *gent.ModelCallRequest {
	return gent.NewModelCallRequest([]*gent.ModelMessage{
		gent.TextParts(gent.ChatMessageTypeHuman, "Return JSON.")})
}

func checkVertexResponse(t *testing.T, model *VertexAI, request *gent.ModelCallRequest) {
	t.Helper()
	stream, err := model.GenerateContentStream(context.Background(), request)
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	defer stream.Close()
	for range stream.Chunks() {
	}
	response, err := stream.Response()
	if !assert.NoError(t, err) || !assert.Len(t, response.Choices, 1) {
		t.FailNow()
	}
	assert.Equal(t, []gent.ContentPart{gent.TextPart(`{"value":3}`)},
		response.Choices[0].CandidateContent)
	assert.Equal(t, "vertexai", response.Choices[0].Continuation.Provider)
}

func TestVertexAIEndpointsAndRecovery(t *testing.T) {
	type input struct{ location, baseURL, selected string }
	type expected struct{ root, location, model string }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"global", input{}, expected{"https://aiplatform.googleapis.com/v1", "global", "configured"}},
		{"region", input{location: "us-central1"},
			expected{"https://us-central1-aiplatform.googleapis.com/v1", "us-central1", "configured"}},
		{"proxy and override", input{"global", "https://proxy.test/v1beta1/", "models/selected"},
			expected{"https://proxy.test/v1beta1", "global", "selected"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var actualURL string
			var actualHeaders http.Header
			client := &http.Client{Transport: responseTransport(func(r *http.Request) (
				*http.Response, error,
			) {
				actualURL, actualHeaders = r.URL.String(), r.Header
				return fixtureHTTP(providerResponse("vertexai", 3), "application/json"), nil
			})}
			model, err := NewVertexAI(&VertexAIConfig{Project: "test-project", Model: "configured",
				Location: test.input.location, BaseURL: test.input.baseURL, HTTPClient: client,
				QuotaProject: "quota-project",
				TokenProvider: vertexTestToken(func(context.Context) (*auth.Token, error) {
					return &auth.Token{Value: "test-token"}, nil
				})})
			if !assert.NoError(t, err) {
				return
			}
			request := vertexTestRequest()
			request.Options.Model = test.input.selected
			checkVertexResponse(t, model, request)
			base := test.expected.root + "/projects/test-project/locations/" +
				test.expected.location + "/publishers/google"
			assert.Equal(t, base+"/models/"+test.expected.model+":streamGenerateContent?alt=sse",
				actualURL)
			assert.Equal(t, http.Header{"Authorization": {"Bearer test-token"},
				"Accept": {"text/event-stream"}, "Content-Type": {"application/json"},
				"X-Goog-User-Project": {"quota-project"}}, actualHeaders)
			assert.Equal(t, "configured", model.ModelName())
			assert.Equal(t, "vertexai", model.Provider())
			assert.Equal(t, map[string]any{"api": "generateContent", "provider": "vertexai",
				"baseURL": base, "model": "configured", "project": "test-project",
				"location": test.expected.location}, model.RecoveryConfig())
		})
	}
}

func TestVertexAIInvalidConfig(t *testing.T) {
	type input struct{ config VertexAIConfig }
	type expected struct{ err string }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"project", input{VertexAIConfig{Model: "model"}},
			expected{"Vertex AI requires Project and Model"}},
		{"model", input{VertexAIConfig{Project: "project"}},
			expected{"Vertex AI requires Project and Model"}},
		{"location", input{VertexAIConfig{Project: "project", Model: "model", Location: "a/b"}},
			expected{"Vertex AI location must contain lowercase letters, digits, or hyphens"}},
		{"URL", input{VertexAIConfig{Project: "project", Model: "model", BaseURL: "ftp://host"}},
			expected{"Vertex AI BaseURL must be an HTTP service root without query or user info"}},
		{"query", input{VertexAIConfig{Project: "project", Model: "model", BaseURL: "https://host?k=v"}},
			expected{"Vertex AI BaseURL must be an HTTP service root without query or user info"}},
		{"multiple sources", input{VertexAIConfig{Project: "project", Model: "model",
			CredentialsFile: "file", CredentialsJSON: []byte(`{}`)}},
			expected{"Vertex AI accepts only one explicit credential source"}},
		{"wrong type", input{VertexAIConfig{Project: "project", Model: "model",
			CredentialsJSON: []byte(`{"type":"authorized_user"}`)}}, expected{
			`load Vertex AI credentials: credentials: expected type "service_account", ` +
				`found "authorized_user"`}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			model, err := NewVertexAI(&test.input.config)
			assert.Nil(t, model)
			assert.EqualError(t, err, test.expected.err)
		})
	}
}

func TestVertexAITokenErrors(t *testing.T) {
	type input struct {
		token *auth.Token
		err   error
	}
	type expected struct{ err string }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"exchange", input{err: errors.New("test failure")},
			expected{"authenticate Vertex AI: test failure"}},
		{"empty", input{token: &auth.Token{}},
			expected{"Vertex AI credential returned an empty or expired token"}},
		{"expired", input{token: &auth.Token{Value: "expired", Expiry: time.Unix(1, 0)}},
			expected{"Vertex AI credential returned an empty or expired token"}},
		{"type", input{token: &auth.Token{Value: "test", Type: "Basic"}},
			expected{"Vertex AI requires a bearer access token"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: responseTransport(func(*http.Request) (*http.Response, error) {
				calls++
				return fixtureHTTP(providerResponse("vertexai", 3), "application/json"), nil
			})}
			model, err := NewVertexAI(&VertexAIConfig{Project: "project", Model: "model",
				HTTPClient: client, TokenProvider: vertexTestToken(func(context.Context) (*auth.Token, error) {
					return test.input.token, test.input.err
				})})
			if !assert.NoError(t, err) {
				return
			}
			stream, err := model.GenerateContentStream(context.Background(), vertexTestRequest())
			assert.Nil(t, stream)
			assert.EqualError(t, err, test.expected.err)
			assert.Equal(t, 0, calls)
		})
	}
}

func TestVertexAIConcurrentRefreshAndCancellation(t *testing.T) {
	started := make(chan struct{})
	var calls atomic.Int32
	model, err := NewVertexAI(&VertexAIConfig{Project: "project", Model: "model",
		HTTPClient: &http.Client{Transport: responseTransport(func(*http.Request) (
			*http.Response, error,
		) {
			return fixtureHTTP(providerResponse("vertexai", 3), "application/json"), nil
		})}, TokenProvider: vertexTestToken(func(ctx context.Context) (*auth.Token, error) {
			if calls.Add(1) == 1 {
				close(started)
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return &auth.Token{Value: "test-token", Expiry: time.Now().Add(time.Hour)}, nil
		})})
	if !assert.NoError(t, err) {
		return
	}
	first, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := model.GenerateContentStream(first, vertexTestRequest())
		finished <- err
	}()
	select {
	case <-started:
	case <-first.Done():
		t.Fatal("token request did not start")
	}
	waiting, stopWaiting := context.WithCancel(context.Background())
	stopWaiting()
	_, err = model.GenerateContentStream(waiting, vertexTestRequest())
	assert.ErrorIs(t, err, context.Canceled)
	cancel()
	assert.ErrorIs(t, <-finished, context.Canceled)
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			checkVertexResponse(t, model, vertexTestRequest())
		}()
	}
	workers.Wait()
	assert.Equal(t, int32(2), calls.Load())
}

func TestVertexAIContinuationIsolation(t *testing.T) {
	type input struct{ provider, saved string }
	type expected struct{ err string }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"to Vertex", input{"vertexai", "gemini"},
			expected{"cannot replay gemini items through vertexai"}},
		{"to Gemini", input{"gemini", "vertexai"},
			expected{"cannot replay vertexai items through gemini"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: responseTransport(func(*http.Request) (*http.Response, error) {
				t.Error("invalid continuation reached HTTP")
				return nil, errors.New("unexpected request")
			})}
			request := gent.NewModelCallRequest([]*gent.ModelMessage{{Role: gent.ChatMessageTypeAI,
				Continuation: &gent.ProviderContinuation{Provider: test.input.saved}}})
			stream, err := fixtureModel(test.input.provider, client).GenerateContentStream(
				context.Background(), request)
			assert.Nil(t, stream)
			assert.EqualError(t, err, test.expected.err)
		})
	}
}
