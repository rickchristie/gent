package models

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"cloud.google.com/go/auth"
	"github.com/stretchr/testify/assert"
)

func TestVertexAIRejectsCachedTokenProviders(t *testing.T) {
	type input struct {
		wrappers    int
		warm        bool
		synchronous bool
	}
	type expected struct {
		model     *VertexAI
		err       string
		exchanges int32
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	errText := "Vertex AI TokenProvider must be uncached; use CredentialsFile, CredentialsJSON, or ADC"
	cases := []testCase{
		{"Google cache", input{}, expected{err: errText}},
		{"warm Google cache", input{warm: true}, expected{err: errText, exchanges: 1}},
		{"credentials cache", input{wrappers: 1}, expected{err: errText}},
		{"warm credentials cache", input{wrappers: 1, warm: true},
			expected{err: errText, exchanges: 1}},
		{"nested credentials cache", input{wrappers: 2}, expected{err: errText}},
		{"synchronous cache", input{synchronous: true}, expected{err: errText}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var exchanges atomic.Int32
			token := &auth.Token{Value: "test-token", Expiry: time.Now().Add(time.Minute)}
			provider := auth.NewCachedTokenProvider(vertexTestToken(func(context.Context) (
				*auth.Token, error,
			) {
				exchanges.Add(1)
				return token, nil
			}), &auth.CachedTokenProviderOptions{DisableAsyncRefresh: test.input.synchronous})
			if test.input.warm {
				actual, err := provider.Token(context.Background())
				assert.NoError(t, err)
				assert.Equal(t, token, actual)
			}
			for range test.input.wrappers {
				provider = auth.NewCredentials(&auth.CredentialsOptions{TokenProvider: provider})
			}
			model, err := NewVertexAI(&VertexAIConfig{Project: "project", Model: "model",
				TokenProvider: provider})
			actual := expected{model: model, exchanges: exchanges.Load()}
			if err != nil {
				actual.err = err.Error()
			}
			assert.Equal(t, test.expected, actual)
		})
	}
}

func TestVertexAISuppliedTokenRefreshCancellation(t *testing.T) {
	type input struct{ credentials bool }
	type expected struct {
		exchanges, requests           int32
		refreshCanceled, callCanceled bool
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"uncached provider", input{}, expected{2, 1, true, true}},
		{"uncached credentials", input{true}, expected{2, 1, true, true}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			started := make(chan context.Context, 1)
			var exchanges, requests atomic.Int32
			var provider auth.TokenProvider = vertexTestToken(func(ctx context.Context) (
				*auth.Token, error,
			) {
				if exchanges.Add(1) == 1 {
					return &auth.Token{Value: "test-token", Expiry: time.Now().Add(time.Minute)}, nil
				}
				started <- ctx
				<-ctx.Done()
				return nil, ctx.Err()
			})
			if test.input.credentials {
				provider = auth.NewCredentials(&auth.CredentialsOptions{TokenProvider: provider})
			}
			client := &http.Client{Transport: responseTransport(func(*http.Request) (
				*http.Response, error,
			) {
				requests.Add(1)
				return fixtureHTTP(providerResponse("vertexai", 3), "application/json"), nil
			})}
			model, err := NewVertexAI(&VertexAIConfig{Project: "project", Model: "model",
				TokenProvider: provider, HTTPClient: client})
			if !assert.NoError(t, err) {
				return
			}
			checkVertexResponse(t, model, vertexTestRequest())
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			finished := make(chan struct{})
			var callErr error
			go func() {
				defer close(finished)
				stream, err := model.GenerateContentStream(ctx, vertexTestRequest())
				if stream != nil {
					stream.Close()
				}
				callErr = err
			}()
			// Join the call on every exit so a failed assertion cannot leave token work running.
			defer func() {
				cancel()
				<-finished
			}()
			var refreshContext context.Context
			select {
			case refreshContext = <-started:
			case <-ctx.Done():
				t.Fatal("refresh did not start")
			}
			cancel()
			<-finished
			actual := expected{exchanges: exchanges.Load(), requests: requests.Load(),
				refreshCanceled: refreshContext.Err() == context.Canceled,
				callCanceled:    errors.Is(callErr, context.Canceled)}
			assert.Equal(t, test.expected, actual)
		})
	}
}
