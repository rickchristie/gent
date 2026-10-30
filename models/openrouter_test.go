package models

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

const routerMixedFixture = `{"data":{"architecture":{"input_modalities":["text"],
"output_modalities":["text"]},"endpoints":[
{"provider_name":"OpenAI","tag":"openai","supported_parameters":
["structured_outputs","response_format"]},
{"provider_name":"Anthropic","tag":"anthropic","supported_parameters":
["structured_outputs","response_format"]}]}}`

func TestOpenRouterNativeRouting(t *testing.T) {
	type input struct {
		catalog     string
		preferences map[string]any
		schema      map[string]any
		status      int
	}
	type expected struct {
		native      bool
		format      any
		preferences map[string]any
		posts       int
		err         string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	required := providerOutputSchema()
	optional := providerOutputSchema()
	delete(optional, "required")
	schemaFormat := func(raw map[string]any) any {
		return map[string]any{"type": "json_schema", "json_schema": map[string]any{
			"name": "gent_output", "strict": true, "schema": raw}}
	}
	cases := []testCase{
		{"all eligible", input{catalog: routerMixedFixture, schema: required}, expected{native: true,
			format: schemaFormat(required), preferences: map[string]any{
				"only": []string{"openai", "anthropic"}, "require_parameters": true}, posts: 1}},
		{"optional field selects exact grammar", input{catalog: routerMixedFixture, schema: optional},
			expected{native: true, format: schemaFormat(optional), preferences: map[string]any{
				"only": []string{"anthropic"}, "require_parameters": true}, posts: 1}},
		{"caller route is retained", input{catalog: routerMixedFixture, schema: optional,
			preferences: map[string]any{"only": []string{"openai"}, "allow_fallbacks": false}},
			expected{format: map[string]any{"type": "json_object"}, preferences: map[string]any{
				"only": []string{"openai"}, "allow_fallbacks": false, "require_parameters": true}, posts: 1}},
		{"ignore is retained", input{catalog: routerMixedFixture, schema: required,
			preferences: map[string]any{"ignore": []string{"anthropic"}, "require_parameters": false}},
			expected{native: true, format: schemaFormat(required), preferences: map[string]any{
				"only": []string{"openai"}, "ignore": []string{"anthropic"},
				"require_parameters": true}, posts: 1}},
		{"JSON mode only", input{schema: required, catalog: `{"data":{"endpoints":[
{"provider_name":"DeepSeek","tag":"deepseek","supported_parameters":["response_format"]}]}}`},
			expected{format: map[string]any{"type": "json_object"},
				preferences: map[string]any{"require_parameters": true}, posts: 1}},
		{"local schema only", input{schema: required, catalog: `{"data":{"endpoints":[
{"provider_name":"local","tag":"local","supported_parameters":[]}]}}`},
			expected{preferences: map[string]any{"require_parameters": true}, posts: 1}},
		{"lookup errors do not downgrade", input{schema: required, catalog: "unavailable", status: 503},
			expected{err: "model HTTP 503: unavailable"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var body map[string]any
			posts, gets := 0, 0
			client := &http.Client{Transport: responseTransport(func(request *http.Request) (
				*http.Response, error,
			) {
				assert.Equal(t, "Bearer fixture-key", request.Header.Get("Authorization"))
				if request.Method == http.MethodGet {
					gets++
					response := fixtureHTTP(test.input.catalog, "application/json")
					if test.input.status != 0 {
						response.StatusCode = test.input.status
					}
					return response, nil
				}
				posts++
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					return nil, err
				}
				return fixtureHTTP(providerResponse("openrouter", 3), "application/json"), nil
			})}
			request := gent.NewModelCallRequest([]*gent.ModelMessage{
				gent.TextParts(gent.ChatMessageTypeHuman, "Read.")})
			request.Options.ResponseSchema = test.input.schema
			if test.input.preferences != nil {
				request.Options.Metadata["openrouter"] = map[string]any{"provider": test.input.preferences}
			}
			before, err := json.Marshal(test.input.preferences)
			assert.NoError(t, err)
			stream, err := fixtureModel("openrouter", client).GenerateContentStream(
				context.Background(), request)
			if test.expected.err != "" {
				assert.Nil(t, stream)
				assert.EqualError(t, err, test.expected.err)
				assert.Equal(t, test.expected.posts, posts)
				return
			}
			if !assert.NoError(t, err) {
				return
			}
			for range stream.Chunks() {
			}
			response, err := stream.Response()
			stream.Close()
			if !assert.NoError(t, err) {
				return
			}
			enforcement := response.Info.RawGenerationInfo["output_schema"].(map[string]any)
			assert.Equal(t, test.expected.native, enforcement["native"])
			assert.Equal(t, test.expected.posts, posts)
			assert.Equal(t, 1, gets)
			want, err := json.Marshal(map[string]any{"format": test.expected.format,
				"preferences": test.expected.preferences})
			assert.NoError(t, err)
			actual, err := json.Marshal(map[string]any{"format": body["response_format"],
				"preferences": body["provider"]})
			assert.NoError(t, err)
			assert.JSONEq(t, string(want), string(actual))
			after, err := json.Marshal(test.input.preferences)
			assert.NoError(t, err)
			assert.Equal(t, before, after, "route selection must not mutate caller settings")
		})
	}
}

func TestOpenRouterConcurrentDiscoveryAndCanceledWait(t *testing.T) {
	var gets, posts atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	client := &http.Client{Transport: responseTransport(func(request *http.Request) (
		*http.Response, error,
	) {
		if request.Method == http.MethodGet {
			if gets.Add(1) == 1 {
				close(started)
			}
			select {
			case <-release:
				return fixtureHTTP(routerFixture, "application/json"), nil
			case <-request.Context().Done():
				return nil, request.Context().Err()
			}
		}
		posts.Add(1)
		return fixtureHTTP(providerResponse("openrouter", 3), "application/json"), nil
	})}
	model := fixtureModel("openrouter", client)
	request := gent.NewModelCallRequest([]*gent.ModelMessage{
		gent.TextParts(gent.ChatMessageTypeHuman, "Read.")})
	request.Options.ResponseSchema = providerOutputSchema()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	results := make(chan error, 16)
	call := func() {
		stream, err := model.GenerateContentStream(ctx, request)
		if err == nil {
			for range stream.Chunks() {
			}
			_, err = stream.Response()
			stream.Close()
		}
		results <- err
	}
	go call()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("catalog lookup did not start")
	}
	waiter, stop := context.WithCancel(ctx)
	stop()
	stream, err := model.GenerateContentStream(waiter, request)
	assert.Nil(t, stream)
	assert.ErrorIs(t, err, context.Canceled)
	var group sync.WaitGroup
	for range 15 {
		group.Add(1)
		go func() { defer group.Done(); call() }()
	}
	close(release)
	group.Wait()
	for range 16 {
		assert.NoError(t, <-results)
	}
	assert.Equal(t, int32(1), gets.Load())
	assert.Equal(t, int32(16), posts.Load())
}

// Observing Done synchronizes this test with the public call's wait for shared discovery.
// No sleeps or changes to the catalog's private state are needed.
type routerWaitContext struct {
	context.Context
	entered chan struct{}
	once    sync.Once
}

func (ctx *routerWaitContext) Done() <-chan struct{} {
	ctx.once.Do(func() { close(ctx.entered) })
	return ctx.Context.Done()
}

func TestOpenRouterCanceledOwnerDoesNotCancelWaiter(t *testing.T) {
	var gets, posts atomic.Int32
	started := make(chan struct{})
	client := &http.Client{Transport: responseTransport(func(request *http.Request) (
		*http.Response, error,
	) {
		if request.Method == http.MethodGet {
			if gets.Add(1) == 1 {
				close(started)
				<-request.Context().Done()
				return nil, request.Context().Err()
			}
			return fixtureHTTP(routerFixture, "application/json"), nil
		}
		posts.Add(1)
		return fixtureHTTP(providerResponse("openrouter", 3), "application/json"), nil
	})}
	model := fixtureModel("openrouter", client)
	request := gent.NewModelCallRequest([]*gent.ModelMessage{
		gent.TextParts(gent.ChatMessageTypeHuman, "Read.")})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	owner, stopOwner := context.WithCancel(ctx)
	defer stopOwner()
	call := func(ctx context.Context, result chan<- error) {
		stream, err := model.GenerateContentStream(ctx, request)
		if err == nil {
			for range stream.Chunks() {
			}
			_, err = stream.Response()
			stream.Close()
		}
		result <- err
	}
	ownerResult := make(chan error, 1)
	go call(owner, ownerResult)
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	waiter := &routerWaitContext{Context: ctx, entered: make(chan struct{})}
	waiterResult := make(chan error, 1)
	go call(waiter, waiterResult)
	select {
	case <-waiter.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	stopOwner()
	assert.ErrorIs(t, <-ownerResult, context.Canceled)
	assert.NoError(t, <-waiterResult)
	assert.Equal(t, int32(2), gets.Load())
	assert.Equal(t, int32(1), posts.Load())
}
