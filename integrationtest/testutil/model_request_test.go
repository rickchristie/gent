package testutil

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

type modelRequestTransport func(*http.Request) (*http.Response, error)

func (f modelRequestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestCreateModel_HTTPRequest(t *testing.T) {
	type input struct {
		modelName string
	}
	type expected struct {
		url     string
		authKey string
		auth    string
		version string
		body    string
		err     string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}

	// Stop at the transport so this test checks the real clients without live API calls.
	requestErr := errors.New("request captured")
	grok := expected{
		url: "https://api.x.ai/v1/responses", authKey: "Authorization", auth: "Bearer xai-key",
		err: `Post "https://api.x.ai/v1/responses": request captured`,
		body: `{
  "model": "grok-4.7",
  "input": [{"role": "user", "content": [{"type": "input_text", "text": "Hello"}]}],
  "stream": true,
  "store": false,
  "include": ["reasoning.encrypted_content"],
  "temperature": 0.7,
  "top_p": 0.95,
  "tools": []
}`,
	}
	cases := []testCase{
		{"default", input{}, grok},
		{"xAI", input{"grok-4.7"}, grok},
		{"OpenAI", input{"gpt-6-astra"}, expected{
			url:     "https://api.openai.com/v1/responses",
			authKey: "Authorization", auth: "Bearer openai-key",
			err: `Post "https://api.openai.com/v1/responses": request captured`,
			body: `{
  "model": "gpt-6-astra",
  "input": [{"role": "user", "content": [{"type": "input_text", "text": "Hello"}]}],
  "stream": true,
  "store": false,
  "include": ["reasoning.encrypted_content"],
  "tools": []
}`,
		}},
		{"Google", input{"gemini-4-argon"}, expected{
			url:     "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions",
			authKey: "Authorization", auth: "Bearer gemini-key",
			err: "network error: failed to reach API server",
			body: `{
  "model": "gemini-4-argon",
  "messages": [{"role": "user", "content": "Hello"}],
  "stream": true,
  "stream_options": {"include_usage": true},
  "temperature": 1
}`,
		}},
		{"Anthropic", input{"claude-fable-5-1"}, expected{
			url:     "https://api.anthropic.com/v1/messages",
			authKey: "X-Api-Key", auth: "anthropic-key", version: "2023-06-01",
			err: `anthropic: failed to create message: send request: ` +
				`Post "https://api.anthropic.com/v1/messages": request captured`,
			// This SDK sends temperature even when it is omitted. The pinned API version
			// accepts the provider default of 1; the SDK adapter already supplies it.
			body: `{
  "model": "claude-fable-5-1",
  "messages": [{"role": "user", "content": [{"type": "text", "text": "Hello"}]}],
  "max_tokens": 2048,
  "stream": true,
  "temperature": 1
}`,
		}},
	}

	t.Setenv(envKeyXAI, "xai-key")
	t.Setenv(envKeyOpenAI, "openai-key")
	t.Setenv(envKeyGemini, "gemini-key")
	t.Setenv(envKeyAnthropic, "anthropic-key")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Each client uses ErrorCaptureTransport, which reads this public default.
			// Keep these cases sequential and restore it after the stream producer stops.
			previous := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = previous })
			calls := 0
			http.DefaultTransport = modelRequestTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, tc.expected.url, r.URL.String())
				assert.Equal(t, tc.expected.auth, r.Header.Get(tc.expected.authKey))
				assert.Equal(t, tc.expected.version, r.Header.Get("Anthropic-Version"))
				var body json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					return nil, err
				}
				assert.JSONEq(t, tc.expected.body, string(body))
				if err := r.Body.Close(); err != nil {
					return nil, err
				}
				return nil, requestErr
			})
			model, err := CreateModel(tc.input.modelName)
			if !assert.NoError(t, err) {
				return
			}
			params := gent.DefaultSamplingParams(model)
			request := gent.NewModelCallRequest([]gent.ModelMessage{
				gent.TextParts(gent.ChatMessageTypeHuman, "Hello"),
			})
			if value, send := params.Temperature.EffectiveValue(); send {
				request.Options.Temperature = &value
			}
			if value, send := params.TopP.EffectiveValue(); send {
				request.Options.TopP = &value
			}
			stream, err := model.GenerateContentStream(context.Background(), request)
			if !assert.NoError(t, err) {
				return
			}
			defer stream.Close()
			for range stream.Chunks() {
			}
			_, err = stream.Response()
			assert.EqualError(t, err, tc.expected.err)
			assert.Equal(t, 1, calls)
		})
	}
}
