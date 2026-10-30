package models

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents/react"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/output"
	"github.com/stretchr/testify/assert"
)

func TestGeminiTokenContinuationRespectsTokenCeiling(t *testing.T) {
	type input struct{ maxTokens int }
	type expected struct {
		value, token, messages string
		completion             gent.ModelStatus
		maxTokens              any
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	const original = `[{"role":"user","parts":[{"text":"Read."}]}]`
	cases := []testCase{
		{"native continuation", input{}, expected{value: "start end", token: "c2VhbGVk",
			messages: original, completion: gent.ModelStatusContinue}},
		{"explicit ceiling retries", input{64}, expected{value: "end", maxTokens: float64(64),
			completion: gent.ModelStatusIncomplete, messages: `[
{"role":"user","parts":[{"text":"Read."}]},
{"role":"user","parts":[{
"text":"The response was incomplete. Use a shorter answer or fewer tool calls."}]}]`}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var bodies []map[string]json.RawMessage
			client := &http.Client{Transport: responseTransport(func(request *http.Request) (
				*http.Response, error,
			) {
				var body map[string]json.RawMessage
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					return nil, err
				}
				bodies = append(bodies, body)
				response := `{"candidates":[{"finishReason":"CONTINUATION","continuationToken":"c2VhbGVk",
"content":{"role":"model","parts":[{"text":"start ","thoughtSignature":"c2ln"}]}}]}`
				if len(bodies) > 1 {
					response = `{"candidates":[{"finishReason":"STOP",
"content":{"role":"model","parts":[{"text":"end"}]}}]}`
				}
				return fixtureHTTP(response, "application/json"), nil
			})}
			validations := 0
			out := output.NewTextOutput().SetCustomValidator("complete", func(_ *gent.ExecutionContext,
				value string,
			) (*gent.OutputValidationResult, error) {
				validations++
				assert.Equal(t, test.expected.value, value)
				return &gent.OutputValidationResult{Accepted: true}, nil
			})
			agent := react.NewWithOutput(fixtureModel("gemini", client), out).
				WithCallOptions(gent.WithMaxTokens(test.input.maxTokens))
			ctx := gent.NewExecutionContext(context.Background(), "continue", gent.NewBasicLoopData(
				gent.NewTask(gent.TextContent{Text: "Read."})))
			value, result, err := executor.Run(ctx, agent, executor.DefaultConfig())
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, test.expected.value, value)
			assert.Equal(t, gent.TerminationSuccess, result.TerminationReason)
			assert.Equal(t, 1, validations)
			if !assert.Len(t, bodies, 2) {
				return
			}
			assert.JSONEq(t, original, string(bodies[0]["contents"]))
			assert.JSONEq(t, test.expected.messages, string(bodies[1]["contents"]))
			token := ""
			if raw := bodies[1]["continuationToken"]; raw != nil {
				assert.NoError(t, json.Unmarshal(raw, &token))
			}
			assert.Equal(t, test.expected.token, token)
			for _, body := range bodies {
				var config map[string]any
				assert.NoError(t, json.Unmarshal(body["generationConfig"], &config))
				assert.Equal(t, test.expected.maxTokens, config["maxOutputTokens"])
			}
			var statuses []gent.ModelStatus
			for _, event := range ctx.Events() {
				if after, ok := event.(*gent.AfterModelCallEvent); ok {
					statuses = append(statuses, after.Response.Choices[0].Completion.Status)
				}
			}
			assert.Equal(t, []gent.ModelStatus{test.expected.completion, gent.ModelStatusAnswer}, statuses)
		})
	}
}
