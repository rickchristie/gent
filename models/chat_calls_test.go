package models

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestChatMalformedToolCalls(t *testing.T) {
	type input struct {
		calls, reason string
		streamed      bool
	}
	type expected struct {
		err    string
		status gent.ModelStatus
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	var cases []testCase
	for _, calls := range []string{`[null]`, `[{}]`, `[{"id":"call","type":"function"}]`} {
		for _, streamed := range []bool{false, true} {
			cases = append(cases,
				testCase{fmt.Sprintf("complete/%s/stream=%t", calls, streamed),
					input{calls, "tool_calls", streamed},
					expected{err: "Chat function call requires id, type, and name"}},
				testCase{fmt.Sprintf("incomplete/%s/stream=%t", calls, streamed),
					input{calls, "length", streamed},
					expected{status: gent.ModelStatusIncomplete}})
		}
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			message := `{"role":"assistant","tool_calls":` + test.input.calls + `}`
			data := `{"choices":[{"message":` + message +
				`,"finish_reason":"` + test.input.reason + `"}]}`
			mime := "application/json"
			if test.input.streamed {
				data = fmt.Sprintf(`data: {"choices":[{"delta":%s,"finish_reason":"%s"}]}

data: [DONE]

`, message, test.input.reason)
				mime = "text/event-stream"
			}
			body := &providerBody{reader: strings.NewReader(data)}
			model := NewVLLM(&Config{BaseURL: "http://fixture.test/v1", Model: "fixture",
				HTTPClient: &http.Client{Transport: responseTransport(func(*http.Request) (
					*http.Response, error,
				) {
					return &http.Response{StatusCode: http.StatusOK,
						Header: http.Header{"Content-Type": {mime}}, Body: body}, nil
				})}})
			stream, err := model.GenerateContentStream(context.Background(),
				gent.NewModelCallRequest(nil))
			if !assert.NoError(t, err) {
				return
			}
			defer stream.Close()
			for chunk := range stream.Chunks() {
				assert.Empty(t, chunk.Content)
			}
			response, err := stream.Response()
			assert.Equal(t, int32(1), body.closed.Load(), "Response must wait for producer cleanup")
			if test.expected.err != "" {
				assert.EqualError(t, err, test.expected.err)
				assert.Nil(t, response)
				return
			}
			if !assert.NoError(t, err) || !assert.Len(t, response.Choices, 1) {
				return
			}
			choice := response.Choices[0]
			// The opaque continuation is transport-specific. Validate it, then compare the full
			// common choice: incomplete native data must not become an executable call.
			assert.Equal(t, "vllm", choice.Continuation.Provider)
			for _, item := range choice.Continuation.Items {
				assert.True(t, json.Valid(item))
			}
			choice.Continuation = nil
			assert.Equal(t, &gent.ContentChoice{Completion: gent.ModelCompletion{
				Status: test.expected.status, ProviderReason: test.input.reason}}, choice)
		})
	}
}
