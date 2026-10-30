package models

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents/react"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/output"
	"github.com/stretchr/testify/assert"
)

type providerOutputValue struct {
	Value int `json:"value"`
}

type providerLookupInput struct {
	ID int `json:"id"`
}

func providerOutputSchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false,
		"properties": map[string]any{"value": map[string]any{"type": "integer"}},
		"required":   []string{"value"}}
}

func fixtureModel(provider string, client *http.Client) gent.Model {
	config := &Config{BaseURL: "https://fixture.test/v1", Model: "fixture/model",
		APIKey: "fixture-key", HTTPClient: client}
	switch provider {
	case "openai", "xai":
		return NewResponses(&ResponsesConfig{Provider: provider, BaseURL: config.BaseURL,
			APIKey: config.APIKey, Model: config.Model, HTTPClient: client})
	case "anthropic":
		return NewAnthropic(config)
	case "gemini":
		return NewGemini(config)
	case "deepseek":
		return NewDeepSeek(config)
	case "vllm":
		return NewVLLM(config)
	case "ollama":
		return NewOllama(config)
	case "openrouter":
		return NewOpenRouter(config)
	case "llamacpp":
		return NewLlamaCpp(config)
	case "sglang":
		return NewSGLang(config)
	default:
		panic("unknown test provider")
	}
}

func fixtureHTTP(body, contentType string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK,
		Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}
}

const routerFixture = `{"data":{"architecture":{"input_modalities":["text","image","file"],
"output_modalities":["text","image"]},"endpoints":[{"provider_name":"OpenAI","tag":"openai",
"supported_parameters":["structured_outputs","response_format","tools"]}]}}`

// This crosses the HTTP adapter, native tool execution, both validation stages, correction
// history, and typed terminal persistence. A prompt-only fake Model cannot pass these checks.
func TestProvidersTypedOutputCorrection(t *testing.T) {
	type input struct{ provider string }
	type expected struct {
		value         providerOutputValue
		calls         []providerLookupInput
		validations   []providerOutputValue
		requests      int
		local, custom int64
		rejected      int64
		reason        gent.TerminationReason
		native        bool
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	var cases []testCase
	for _, provider := range []string{"openai", "anthropic", "gemini", "xai", "deepseek",
		"vllm", "ollama", "openrouter", "llamacpp", "sglang"} {
		cases = append(cases, testCase{provider, input{provider}, expected{
			value: providerOutputValue{3}, calls: []providerLookupInput{{7}},
			validations: []providerOutputValue{{2}, {3}}, requests: 4, local: 1, custom: 1,
			rejected: 2, reason: gent.TerminationSuccess, native: provider != "deepseek",
		}})
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var requests []map[string]any
			client := &http.Client{Transport: responseTransport(func(request *http.Request) (
				*http.Response, error,
			) {
				if request.Method == http.MethodGet {
					assert.Equal(t, "/v1/models/fixture/model/endpoints", request.URL.Path)
					return fixtureHTTP(routerFixture, "application/json"), nil
				}
				var body map[string]any
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					return nil, err
				}
				requests = append(requests, body)
				if len(requests) > 4 {
					return nil, fmt.Errorf("unexpected correction request %d", len(requests))
				}
				return fixtureHTTP(providerResponse(test.input.provider, len(requests)-1),
					"application/json"), nil
			})}
			out, err := output.NewSchemaOutput[providerOutputValue](providerOutputSchema())
			if !assert.NoError(t, err) {
				return
			}
			var validations []providerOutputValue
			out.SetCustomValidator("value-three", func(_ *gent.ExecutionContext,
				value providerOutputValue,
			) (*gent.OutputValidationResult, error) {
				validations = append(validations, value)
				if value.Value != 3 {
					return &gent.OutputValidationResult{Steering: []gent.ContentPart{
						gent.TextContent{Text: "Use value 3."}}}, nil
				}
				return &gent.OutputValidationResult{Accepted: true}, nil
			})
			var calls []providerLookupInput
			agent := react.NewWithOutput(fixtureModel(test.input.provider, client), out).RegisterTool(
				gent.DefineTool("lookup", func(_ context.Context, input providerLookupInput) (string, error) {
					calls = append(calls, input)
					return "record-seven", nil
				}).Description("Read the record").MustBuild())
			ctx := gent.NewExecutionContext(context.Background(), test.name,
				gent.NewBasicLoopData(&gent.Task{Messages: []*gent.ModelMessage{
					gent.TextParts(gent.ChatMessageTypeSystem, "Read records and return JSON."),
					gent.TextParts(gent.ChatMessageTypeHuman, "Read record seven."),
				}}))
			value, result, err := executor.NewRunner(agent, executor.DefaultConfig()).Execute(ctx)
			if !assert.NoError(t, err) {
				return
			}
			var native bool
			for _, event := range ctx.Events() {
				if after, ok := event.(*gent.AfterModelCallEvent); ok {
					fields := after.Response.Info.RawGenerationInfo["output_schema"].(map[string]any)
					native = fields["native"].(bool)
				}
			}
			actual := expected{value: value, calls: calls, validations: validations,
				requests: len(requests), local: ctx.Stats().GetCounter(gent.SCOutputLocalRejectedTotal),
				custom:   ctx.Stats().GetCounter(gent.SCOutputCustomRejectedTotal),
				rejected: ctx.Stats().GetCounter(gent.SCOutputRejectedTotal),
				reason:   result.TerminationReason, native: native}
			assert.Equal(t, test.expected, actual)
			assert.Equal(t, &gent.ExecutionResult{TerminationReason: gent.TerminationSuccess,
				Output: []gent.ContentPart{gent.TextContent{Text: `{"value":3}`}}}, ctx.Result())
			assertProviderCorrectionHistory(t, ctx)
		})
	}
}

func assertProviderCorrectionHistory(t *testing.T, ctx *gent.ExecutionContext) {
	t.Helper()
	var candidates []string
	for _, entry := range ctx.History().Entries {
		for _, message := range entry.Messages {
			if message.Role == gent.ChatMessageTypeAI {
				text := gent.ContentText(message.Parts)
				if text != "" {
					candidates = append(candidates, text)
				}
			}
		}
	}
	assert.Equal(t, []string{`{"wrong":1}`, `{"value":2}`, `{"value":3}`}, candidates)
}

func providerResponse(provider string, turn int) string {
	answers := []string{"", `{"wrong":1}`, `{"value":2}`, `{"value":3}`}
	text, err := json.Marshal(answers[turn])
	if err != nil {
		panic(err)
	}
	switch provider {
	case "openai", "xai":
		if turn == 0 {
			return `{"status":"completed","output":[{"type":"reasoning","encrypted_content":"sealed"},
{"type":"function_call","call_id":"call-7","name":"lookup","arguments":"{\"id\":7}"}]}`
		}
		return `{"status":"completed","output":[{"type":"message","role":"assistant",
"content":[{"type":"output_text","text":` + string(text) + `}]}]}`
	case "anthropic":
		if turn == 0 {
			return `{"id":"msg-7","stop_reason":"tool_use","content":[
{"type":"thinking","thinking":"Read the record.","signature":"sealed"},
{"type":"tool_use","id":"call-7","name":"lookup","input":{"id":7}}]}`
		}
		return `{"id":"msg-8","stop_reason":"end_turn","content":[{"type":"text","text":` +
			string(text) + `}]}`
	case "gemini":
		if turn == 0 {
			return `{"candidates":[{"content":{"role":"model","parts":[
{"text":"Read the record.","thought":true},
{"functionCall":{"id":"call-7","name":"lookup","args":{"id":7}},
"thoughtSignature":"sealed"}]},"finishReason":"STOP"}]}`
		}
		return `{"candidates":[{"content":{"role":"model","parts":[{"text":` + string(text) +
			`}]},"finishReason":"STOP"}]}`
	case "ollama":
		if turn == 0 {
			return `{"message":{"role":"assistant","thinking":"Read the record.",
"tool_calls":[{"function":{"name":"lookup","arguments":{"id":7}}}]},
"done":true,"done_reason":"stop"}`
		}
		return `{"message":{"role":"assistant","content":` + string(text) +
			`},"done":true,"done_reason":"stop"}`
	default:
		if turn == 0 {
			return `{"choices":[{"message":{"role":"assistant","reasoning_content":"Read the record.",
"tool_calls":[{"id":"call-7","type":"function","function":{"name":"lookup",
"arguments":"{\"id\":7}"}}]},"finish_reason":"tool_calls"}]}`
		}
		return `{"choices":[{"message":{"role":"assistant","content":` + string(text) +
			`},"finish_reason":"stop"}]}`
	}
}
