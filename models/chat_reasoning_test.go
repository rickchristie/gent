package models

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestOpenRouterReasoningBlocksReplay(t *testing.T) {
	type input struct{ details []string }
	type expected struct{ details string }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"summary followed by encrypted with same index", input{[]string{
			`{"type":"reasoning.summary","summary":"Read ","index":0}`,
			`{"type":"reasoning.summary","summary":"the record.","index":0,` +
				`"format":"openai-responses-v1"}`,
			`{"type":"reasoning.encrypted","data":"sealed","id":"rs_1","index":0,` +
				`"format":"openai-responses-v1"}`,
		}}, expected{`[
{"type":"reasoning.summary","summary":"Read the record.","index":0,"format":"openai-responses-v1"},
{"type":"reasoning.encrypted","data":"sealed","id":"rs_1","index":0,"format":"openai-responses-v1"}
]`}},
		{"signed text fragments and late identity", input{[]string{
			`{"type":"reasoning.text","text":"Read ","signature":null,"id":null,"index":0}`,
			`{"type":"reasoning.text","text":"the record.","id":"thinking-1",` +
				`"format":"anthropic-claude-v1","index":0,"provider_field":{"v":1}}`,
			`{"type":"reasoning.text","signature":"signed-","index":0}`,
			`{"type":"reasoning.text","signature":"bytes","id":null,"format":"unknown"}`,
		}}, expected{`[
{"type":"reasoning.text","text":"Read the record.","signature":"signed-bytes", "id":"thinking-1",
"index":0,"format":"anthropic-claude-v1","provider_field":{"v":1}}
]`}},
		{"type transitions and separate opaque blocks", input{[]string{
			`{"type":"reasoning.summary","summary":"First.","index":0}`,
			`{"type":"reasoning.text","text":"Think.","signature":"sig","index":0}`,
			`{"type":"reasoning.encrypted","data":"one","id":"first","index":0}`,
			`{"type":"reasoning.encrypted","data":"two","id":"second","index":0}`,
			`{"type":"reasoning.summary","summary":"Next.","index":0}`,
		}}, expected{`[
{"type":"reasoning.summary","summary":"First.","index":0},
{"type":"reasoning.text","text":"Think.","signature":"sig","index":0},
{"type":"reasoning.encrypted","data":"one","id":"first","index":0},
{"type":"reasoning.encrypted","data":"two","id":"second","index":0},
{"type":"reasoning.summary","summary":"Next.","index":0}
]`}},
		{"different identities stay separate", input{[]string{
			`{"type":"reasoning.text","text":"A","id":"a","index":0,"format":"one"}`,
			`{"type":"reasoning.text","text":"B","id":"b","index":0,"format":"one"}`,
			`{"type":"reasoning.text","text":"C","id":"b","index":1,"format":"one"}`,
			`{"type":"reasoning.text","text":"D","id":"b","index":1,"format":"two"}`,
		}}, expected{`[
{"type":"reasoning.text","text":"A","id":"a","index":0,"format":"one"},
{"type":"reasoning.text","text":"B","id":"b","index":0,"format":"one"},
{"type":"reasoning.text","text":"C","id":"b","index":1,"format":"one"},
{"type":"reasoning.text","text":"D","id":"b","index":1,"format":"two"}
]`}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var events strings.Builder
			for _, detail := range test.input.details {
				events.WriteString(`data: {"choices":[{"index":0,"delta":{"reasoning_details":[`)
				events.WriteString(detail)
				events.WriteString(`]}}]}

`)
			}
			events.WriteString(`data: {"choices":[{"index":0,"delta":{"tool_calls":[
data: {"index":0,"id":"call-1","type":"function",
data: "function":{"name":"lookup","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}

data: [DONE]

`)
			var requests []map[string]json.RawMessage
			client := &http.Client{Transport: responseTransport(func(request *http.Request) (
				*http.Response, error,
			) {
				if request.Method == http.MethodGet {
					return fixtureHTTP(routerFixture, "application/json"), nil
				}
				var body map[string]json.RawMessage
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					return nil, err
				}
				requests = append(requests, body)
				if len(requests) == 1 {
					return fixtureHTTP(events.String(), "text/event-stream"), nil
				}
				return fixtureHTTP(providerResponse("openrouter", 3), "application/json"), nil
			})}
			model := fixtureModel("openrouter", client)
			messages := []*gent.ModelMessage{gent.TextParts(gent.ChatMessageTypeHuman, "Read.")}
			stream, err := model.GenerateContentStream(context.Background(),
				gent.NewModelCallRequest(messages))
			if !assert.NoError(t, err) {
				return
			}
			collectProviderMeta(t, stream)
			response, err := stream.Response()
			stream.Close()
			if !assert.NoError(t, err) {
				return
			}
			messages = append(messages, &gent.ModelMessage{Role: gent.ChatMessageTypeAI,
				Continuation: response.Choices[0].Continuation}, &gent.ModelMessage{
				Role: gent.ChatMessageTypeTool, Parts: []gent.ContentPart{gent.ToolCallResponse{
					ToolCallID: "call-1", Name: "lookup", Content: "record-one"}}})
			stream, err = model.GenerateContentStream(context.Background(),
				gent.NewModelCallRequest(messages))
			if !assert.NoError(t, err) {
				return
			}
			collectProviderMeta(t, stream)
			_, err = stream.Response()
			stream.Close()
			if !assert.NoError(t, err) || !assert.Len(t, requests, 2) {
				return
			}
			want := `[{"role":"user","content":"Read."},
{"role":"assistant","tool_calls":[
{"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{}"}}],
"reasoning_details":` + test.expected.details + `},
{"role":"tool","tool_call_id":"call-1","content":"record-one"}]`
			assert.JSONEq(t, want, string(requests[1]["messages"]))
		})
	}
}
