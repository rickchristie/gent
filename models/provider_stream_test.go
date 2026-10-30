package models

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

var testProviders = []string{"openai", "anthropic", "gemini", "xai", "deepseek",
	"vllm", "ollama", "openrouter", "llamacpp", "sglang"}

func streamFixture(t *testing.T, provider, turn string) (string, string) {
	t.Helper()
	protocol, extension, mime := provider, "sse", "text/event-stream"
	switch provider {
	case "openai", "xai":
		protocol = "responses"
	case "deepseek", "vllm", "openrouter", "llamacpp", "sglang":
		protocol = "chat"
	case "ollama":
		extension, mime = "ndjson", "application/x-ndjson"
	}
	path := filepath.Join("testdata", "output-"+protocol+"-"+turn+"."+extension)
	data, err := os.ReadFile(path)
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	return string(data), mime
}

// The second HTTP request must replay the assembled native message, including signed data.
// This checks the transport boundary rather than comparing an accumulator to itself.
func TestProvidersStreamingAndContinuation(t *testing.T) {
	type input struct{ provider string }
	type expected struct {
		reasoningID, answerID string
		providerReason        string
		nativeMessages        string
		reasoningTokens       int
		callID                string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	const chatMessages = `[{"role":"user","content":"Read."},
{"role":"assistant","reasoning_content":"Read the record.","tool_calls":[
{"id":"call-7","type":"function","function":{"name":"lookup","arguments":"{\"id\":7}"}}]},
{"role":"tool","tool_call_id":"call-7","content":"record-seven"}]`
	const responsesMessages = `[{"role":"user","content":[{"type":"input_text","text":"Read."}]},
{"type":"reasoning","id":"reason-7","encrypted_content":"sealed",
"summary":[{"type":"summary_text","text":"Read the record."}]},
{"type":"function_call","call_id":"call-7","name":"lookup","arguments":"{\"id\":7}"},
{"type":"function_call_output","call_id":"call-7","output":"record-seven"}]`
	var cases []testCase
	for _, provider := range testProviders {
		want := expected{"reasoning_content", "content", "stop", chatMessages, 3, "call-7"}
		switch provider {
		case "openai", "xai":
			want = expected{"output:0", "output:0", "completed", responsesMessages, 3, "call-7"}
		case "anthropic":
			want = expected{"block:0", "block:0", "end_turn", `[
{"role":"user","content":[{"type":"text","text":"Read."}]},
{"role":"assistant","content":[{"type":"thinking","thinking":"Read the record.",
"signature":"sealed"},{"type":"tool_use","id":"call-7","name":"lookup","input":{"id":7}}]},
{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-7",
"content":"record-seven"}]}]`, 0, "call-7"}
		case "gemini":
			want = expected{"part:0", "part:0", "STOP", `[
{"role":"user","parts":[{"text":"Read."}]},
{"role":"model","parts":[{"text":"Read ","thought":true},
{"text":"the record.","thought":true},{"functionCall":{"id":"call-7","name":"lookup",
"args":{"id":7}},"thoughtSignature":"sealed"}]},
{"role":"user","parts":[{"functionResponse":{"id":"call-7","name":"lookup",
"response":{"result":"record-seven"}}}]}]`, 3, "call-7"}
		case "ollama":
			want = expected{"thinking", "content", "stop", `[
{"role":"user","content":"Read."},
{"role":"assistant","content":"","thinking":"Read the record.",
"tool_calls":[{"function":{"name":"lookup","arguments":{"id":7}}}]},
{"role":"tool","tool_name":"lookup","content":"record-seven"}]`, 0, ""}
		}
		cases = append(cases, testCase{provider, input{provider}, want})
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var requests []map[string]json.RawMessage
			toolsBody, toolsMIME := streamFixture(t, test.input.provider, "tools")
			answerBody, answerMIME := streamFixture(t, test.input.provider, "answer")
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
					return fixtureHTTP(toolsBody, toolsMIME), nil
				}
				return fixtureHTTP(answerBody, answerMIME), nil
			})}
			model := fixtureModel(test.input.provider, client)
			messages := []*gent.ModelMessage{gent.TextParts(gent.ChatMessageTypeHuman, "Read.")}
			var reasoning, text []string
			options := []gent.CallOption{gent.WithStreamingFunc(func(_ context.Context, data []byte) error {
				text = append(text, string(data))
				return nil
			}), gent.WithStreamingReasoningFunc(func(_ context.Context, data []byte, _ []byte) error {
				reasoning = append(reasoning, string(data))
				return nil
			})}
			stream, err := model.GenerateContentStream(context.Background(),
				gent.NewModelCallRequest(messages, options...))
			if !assert.NoError(t, err) {
				return
			}
			meta := collectProviderMeta(t, stream)
			response, err := stream.Response()
			stream.Close()
			if !assert.NoError(t, err) || !assert.Len(t, response.Choices, 1) {
				return
			}
			choice := response.Choices[0]
			wantMeta := []*gent.MetaContent{
				{Type: gent.MetaContentReasoning, MessageId: test.expected.reasoningID,
					Content: []gent.ContentPart{gent.TextContent{Text: "Read "}}},
				{Type: gent.MetaContentReasoning, MessageId: test.expected.reasoningID,
					Content: []gent.ContentPart{gent.TextContent{Text: "the record."}}},
			}
			if test.input.provider == "gemini" {
				wantMeta[1].MessageId = "part:1"
			}
			assert.Equal(t, wantMeta, meta)
			assert.Equal(t, []string{"Read ", "the record."}, reasoning)
			assert.Empty(t, text)
			assert.Equal(t, gent.ModelStatusToolCalls, choice.Completion.Status)
			assert.Nil(t, choice.CandidateContent)
			assert.Equal(t, []gent.ModelToolCall{{ID: test.expected.callID, Type: "function",
				FunctionCall: &gent.FunctionCall{Name: "lookup", Arguments: `{"id":7}`}}},
				choice.ToolCalls)
			messages = append(messages, &gent.ModelMessage{Role: gent.ChatMessageTypeAI,
				Continuation: choice.Continuation}, &gent.ModelMessage{Role: gent.ChatMessageTypeTool,
				Parts: []gent.ContentPart{gent.ToolCallResponse{ToolCallID: "call-7", Name: "lookup",
					Content: "record-seven"}}})
			stream, err = model.GenerateContentStream(context.Background(),
				gent.NewModelCallRequest(messages, options...))
			if !assert.NoError(t, err) {
				return
			}
			meta = collectProviderMeta(t, stream)
			response, err = stream.Response()
			stream.Close()
			if !assert.NoError(t, err) {
				return
			}
			choice = response.Choices[0]
			wantMeta = []*gent.MetaContent{
				{Type: gent.MetaContentIntermediary, MessageId: test.expected.answerID,
					Content: []gent.ContentPart{gent.TextContent{Text: "<answer>Done."}}},
				{Type: gent.MetaContentIntermediary, MessageId: test.expected.answerID,
					Content: []gent.ContentPart{gent.TextContent{Text: "</answer>"}}},
			}
			parts := []gent.ContentPart{gent.TextContent{Text: "<answer>Done.</answer>"}}
			if test.input.provider == "gemini" {
				wantMeta[1].MessageId = "part:1"
				parts = []gent.ContentPart{gent.TextContent{Text: "<answer>Done."},
					gent.TextContent{Text: "</answer>"}}
			}
			assert.Equal(t, wantMeta, meta)
			assert.Equal(t, []string{"<answer>Done.", "</answer>"}, text)
			assert.Equal(t, gent.ModelCompletion{Status: gent.ModelStatusAnswer,
				ProviderReason: test.expected.providerReason}, choice.Completion)
			assert.Equal(t, parts, choice.CandidateContent)
			assert.Nil(t, choice.ToolCalls)
			info := *response.Info
			info.Duration, info.RawGenerationInfo = 0, nil
			assert.Equal(t, gent.GenerationInfo{InputTokens: 13, OutputTokens: 8, TotalTokens: 21,
				CachedInputTokens: 2, ReasoningTokens: test.expected.reasoningTokens}, info)
			if !assert.Len(t, requests, 2) {
				return
			}
			field := "messages"
			switch test.input.provider {
			case "openai", "xai":
				field = "input"
			case "gemini":
				field = "contents"
			}
			assert.JSONEq(t, test.expected.nativeMessages, string(requests[1][field]))
		})
	}
}

func collectProviderMeta(t *testing.T, stream gent.Stream) []*gent.MetaContent {
	t.Helper()
	var result []*gent.MetaContent
	for chunk := range stream.Chunks() {
		assert.Empty(t, chunk.Content, "provider chunks cannot publish accepted output")
		assert.NoError(t, chunk.Err)
		result = append(result, chunk.MetaContent...)
	}
	return result
}
