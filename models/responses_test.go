package models

import (
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents/react"
	"github.com/rickchristie/gent/toolchain"
	"github.com/stretchr/testify/assert"
)

type responseTransport func(*http.Request) (*http.Response, error)

func (f responseTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func sseResponse(payloads ...string) *http.Response {
	var body strings.Builder
	for _, payload := range payloads {
		body.WriteString("data: " + strings.ReplaceAll(payload, `
`, `
data: `) + `

`)
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(body.String()))}
}

type responseInput struct {
	ID string `json:"id"`
}
type responseOutput struct {
	NextID string `json:"next_id"`
}

func responseInputSchema() map[string]any {
	return map[string]any{
		"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}},
		"required": []string{"id"}, "additionalProperties": false,
	}
}

func TestResponsesNativeProgramAndContinuation(t *testing.T) {
	type input struct{ provider string }
	type expected struct {
		calls  []string
		answer string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"OpenAI", input{"openai"}, expected{[]string{"lookup:start", "read:record-9"}, "Done."}},
		{"Grok", input{"xai"}, expected{[]string{"lookup:start", "read:record-9"}, "Done."}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var requests []map[string]any
			var calls []string
			program := `const first = tool.call({tool:"lookup",args:{id:"start"}});
if (first.error) throw new Error(first.error);
tool.call({tool:"read",args:{id:first.output.next_id}});`
			args, err := json.Marshal(map[string]string{"code": program})
			assert.NoError(t, err)
			reasoning := map[string]any{"id": "reasoning-1", "type": "reasoning",
				"encrypted_content": "opaque-signature", "summary": []any{}}
			commentary := map[string]any{"id": "update-1", "type": "message", "role": "assistant",
				"phase": "commentary", "content": []any{
					map[string]any{"type": "output_text", "text": "I will read the record."}}}
			call := map[string]any{"id": "item-1", "call_id": "call-1", "type": "function_call",
				"name": "run_code", "arguments": string(args), "status": "completed"}
			transport := responseTransport(func(request *http.Request) (*http.Response, error) {
				assert.Equal(t, "https://fixture.test/v1/responses", request.URL.String())
				assert.Equal(t, "Bearer fixture-key", request.Header.Get("Authorization"))
				var body map[string]any
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					return nil, err
				}
				requests = append(requests, body)
				output := []any{reasoning, commentary, call}
				if len(requests) > 1 {
					output = []any{map[string]any{"type": "message", "id": "answer", "role": "assistant",
						"content": []any{map[string]any{"type": "output_text", "text": "Done."}}}}
				}
				payload, err := json.Marshal(map[string]any{"type": "response.completed",
					"response": map[string]any{"status": "completed", "output": output,
						"usage": map[string]int{"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}}})
				if err != nil {
					return nil, err
				}
				return sseResponse(string(payload)), nil
			})
			build := func() *react.Agent[string] {
				model := NewResponses(&ResponsesConfig{
					Provider: test.input.provider, BaseURL: "https://fixture.test/v1",
					APIKey: "fixture-key", Model: "fixture", HTTPClient: &http.Client{Transport: transport}})
				registry := toolchain.New().RegisterTool(gent.DefineTool("lookup",
					func(_ context.Context, input *responseInput) (*responseOutput, error) {
						calls = append(calls, "lookup:"+input.ID)
						return &responseOutput{NextID: "record-9"}, nil
					}).Description("Get the next record ID").ParameterSchema(responseInputSchema()).MustBuild()).
					RegisterTool(gent.DefineTool("read",
						func(_ context.Context, input *responseInput) (string, error) {
							calls = append(calls, "read:"+input.ID)
							return "record", nil
						}).Description("Read the next record").ParameterSchema(responseInputSchema()).MustBuild())
				chain := toolchain.NewJsToolChainWrapper(registry).WithDirectCallDisabled()
				return react.NewAgent(model).WithToolChain(chain).WithRecoveryVersion("responses/v1")
			}
			agent := build()
			loopData := gent.NewBasicLoopData(gent.NewTask(gent.TextContent{Text: "Read a record."}))
			ctx := gent.NewExecutionContext(context.Background(), "fixture", loopData)
			assert.NoError(t, agent.ConfigureExecution(ctx))
			_, err = agent.Next(ctx)
			if !assert.NoError(t, err) {
				return
			}
			// A fresh object graph must retain the opaque provider items before the next model call.
			assert.NoError(t, ctx.CommitCheckpoint(gent.PhaseIterationCompleted))
			bundle, err := ctx.Export()
			assert.NoError(t, err)
			data, err := ctx.Codecs().Marshal(bundle)
			assert.NoError(t, err)
			var saved gent.RunView
			assert.NoError(t, ctx.Codecs().Unmarshal(data, &saved))
			assert.NoError(t, gent.ValidateBundle(saved, ctx.Codecs()))
			agent = build()
			codecs := gent.NewCodecRegistry()
			assert.NoError(t, agent.RegisterCodecs(codecs))
			ctx, err = gent.RestoreExecutionContext(context.Background(), saved,
				gent.RestoreBindings{Codecs: codecs, Configure: agent.ConfigureExecution})
			if !assert.NoError(t, err) {
				return
			}
			result, err := agent.Next(ctx)
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, test.expected, expected{calls, result.Result[0].(gent.TextContent).Text})
			assert.Equal(t, int64(1), ctx.Stats().GetCounter(gent.SCCodeExecutions))
			assert.Equal(t, []any{reasoning, commentary, call}, requests[1]["input"].([]any)[1:4])
			assert.Equal(t, false, requests[0]["store"])
			assert.Equal(t, true, requests[0]["stream"])
			assert.Equal(t, "fixture", requests[0]["model"])
			assert.Equal(t, []any{"reasoning.encrypted_content"}, requests[0]["include"])
			assert.Len(t, requests[0]["tools"].([]any), 1)
		})
	}
}

func TestResponsesStreamFailures(t *testing.T) {
	type input struct {
		payload      string
		continuation *gent.ProviderContinuation
	}
	type expected struct {
		err      string
		requests int
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"unknown stream phase", input{payload: `{"type":"response.output_item.added",
"output_index":0,"item":{"type":"message","phase":"unknown"}}`},
			expected{"model stream ended without a terminal event", 1}},
		{"truncated", input{payload: `{"type":"response.output_text.delta","delta":"partial"}`},
			expected{"model stream ended without a terminal event", 1}},
		{"failed", input{payload: `{"type":"response.failed",
"response":{"status":"failed","error":{"code":"bad","message":"failed"}}}`},
			expected{"Responses bad: failed", 1}},
		{"missing call ID", input{payload: `{"type":"response.completed",
"response":{"status":"completed","output":[
{"type":"function_call","name":"lookup","arguments":"{}"}]}}`},
			expected{"Responses function call requires call_id and name", 1}},
		{"missing function name", input{payload: `{"type":"response.completed",
"response":{"status":"completed","output":[
{"type":"function_call","call_id":"call-1","arguments":"{}"}]}}`},
			expected{"Responses function call requires call_id and name", 1}},
		{"wrong provider", input{continuation: &gent.ProviderContinuation{Provider: "xai"}},
			expected{"cannot replay xai items through openai", 0}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			model := NewResponses(&ResponsesConfig{Provider: "openai", BaseURL: "https://fixture.test/v1",
				HTTPClient: &http.Client{Transport: responseTransport(func(
					*http.Request,
				) (*http.Response, error) {
					requests++
					return sseResponse(test.input.payload), nil
				})}})
			request := gent.NewModelCallRequest([]*gent.ModelMessage{
				{Role: gent.ChatMessageTypeAI, Continuation: test.input.continuation},
			})
			stream, err := model.GenerateContentStream(context.Background(), request)
			if err == nil {
				for range stream.Chunks() {
				}
				_, err = stream.Response()
			}
			if assert.Error(t, err) {
				assert.Equal(t, test.expected, expected{err.Error(), requests})
			}
		})
	}
}

type cancelResponseBody struct {
	ctx    context.Context
	closed chan struct{}
}

func (b *cancelResponseBody) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}
func (b *cancelResponseBody) Close() error { close(b.closed); return nil }

func TestResponsesCancellationStopsProducer(t *testing.T) {
	type input struct{ closeStream bool }
	type expected struct{ err error }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"parent context", input{}, expected{context.Canceled}},
		{"stream close", input{true}, expected{context.Canceled}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			started := make(chan *cancelResponseBody, 1)
			transport := responseTransport(func(request *http.Request) (*http.Response, error) {
				body := &cancelResponseBody{ctx: request.Context(), closed: make(chan struct{})}
				started <- body
				return &http.Response{StatusCode: 200,
					Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: body}, nil
			})
			model := NewResponses(&ResponsesConfig{Provider: "openai", BaseURL: "https://fixture.test",
				HTTPClient: &http.Client{Transport: transport}})
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			stream, err := model.GenerateContentStream(ctx, gent.NewModelCallRequest(nil))
			if !assert.NoError(t, err) {
				return
			}
			var body *cancelResponseBody
			select {
			case body = <-started:
			case <-ctx.Done():
				t.Fatal("provider did not start")
			}
			if test.input.closeStream {
				stream.Close()
			} else {
				cancel()
			}
			_, err = stream.Response()
			assert.ErrorIs(t, err, test.expected.err)
			select {
			case <-body.closed:
			default:
				t.Fatal("Response returned before the provider body closed")
			}
		})
	}
}

func TestResponsesSamplingPresence(t *testing.T) {
	type input struct{ options []gent.CallOption }
	type expected struct{ body map[string]any }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	base := map[string]any{"model": "fixture", "input": []any{}, "stream": true,
		"store": false, "tools": []any{}, "include": []string{"reasoning.encrypted_content"}}
	zero := maps.Clone(base)
	zero["temperature"], zero["top_p"] = float64(0), float64(0)
	cases := []testCase{
		{"omitted", input{}, expected{base}},
		{"explicit zero", input{[]gent.CallOption{gent.WithTemperature(0), gent.WithTopP(0)}},
			expected{zero}},
	}
	model := NewOpenAI("key", "fixture")
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			request := gent.NewModelCallRequest(nil, test.input.options...)
			body, err := model.requestBody(&request)
			assert.NoError(t, err)
			assert.Equal(t, test.expected, expected{body})
		})
	}
}
