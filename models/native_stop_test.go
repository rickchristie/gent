package models_test

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents/react"
	"github.com/rickchristie/gent/models"
	"github.com/stretchr/testify/assert"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/anthropic"
)

type nativeStopTransport func(*http.Request) (*http.Response, error)

func (transport nativeStopTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

// Real stream decoding must preserve the stop reason through the Anthropic block merge.
func TestNativeHTTP_AnthropicStopReasons(t *testing.T) {
	type input struct{ fixture, originalReason, reason string }
	type expected struct {
		result *gent.AgentLoopResult
		calls  int
		err    error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	retry := &gent.AgentLoopResult{Action: gent.LAContinue,
		NextPrompt: "The response was incomplete. Use a shorter answer or fewer tool calls."}
	answer := &gent.AgentLoopResult{Action: gent.LATerminate,
		Result: []gent.ContentPart{llms.TextContent{Text: "Done."}}}
	toolResult := &gent.AgentLoopResult{Action: gent.LAContinue, NextPrompt: `<lookup>
record
</lookup>
<lookup>
record
</lookup>`}
	cases := []testCase{
		{"tool token limit", input{"native-anthropic-1.sse", "tool_use", "max_tokens"},
			expected{retry, 0, nil}},
		{"answer token limit", input{"native-anthropic-2.sse", "end_turn", "max_tokens"},
			expected{retry, 0, nil}},
		{"tool context limit", input{"native-anthropic-1.sse", "tool_use",
			"model_context_window_exceeded"}, expected{retry, 0, nil}},
		{"answer context limit", input{"native-anthropic-2.sse", "end_turn",
			"model_context_window_exceeded"}, expected{retry, 0, nil}},
		{"complete tools", input{"native-anthropic-1.sse", "tool_use", "tool_use"},
			expected{toolResult, 2, nil}},
		{"complete answer", input{"native-anthropic-2.sse", "end_turn", "end_turn"},
			expected{answer, 0, nil}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture, err := os.ReadFile(filepath.Join("testdata", test.input.fixture))
			if !assert.NoError(t, err) {
				return
			}
			original := `"stop_reason": "` + test.input.originalReason + `"`
			if !assert.Equal(t, 1, strings.Count(string(fixture), original)) {
				return
			}
			body := strings.Replace(string(fixture), original,
				`"stop_reason": "`+test.input.reason+`"`, 1)
			client := &http.Client{Transport: nativeStopTransport(func(*http.Request) (
				*http.Response, error,
			) {
				return &http.Response{StatusCode: http.StatusOK,
					Header: http.Header{"Content-Type": {"text/event-stream"}},
					Body:   io.NopCloser(strings.NewReader(body))}, nil
			})}
			provider, err := anthropic.New(anthropic.WithToken("test-key"),
				anthropic.WithModel("wire-test"), anthropic.WithHTTPClient(client))
			if !assert.NoError(t, err) {
				return
			}
			calls := 0
			agent := react.NewNativeAgent(models.NewLCGWrapper(provider)).RegisterTool(
				gent.NewToolFunc("lookup", "Read", nil,
					func(context.Context, map[string]any) (string, error) {
						calls++
						return "record", nil
					}))
			ctx := gent.NewExecutionContext(context.Background(), "http", gent.NewBasicLoopData(
				&gent.Task{Text: "Read two records."}))
			result, err := agent.Next(ctx)
			assert.Equal(t, test.expected, expected{result, calls, err})
		})
	}
}
