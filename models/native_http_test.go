package models_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents/react"
	"github.com/rickchristie/gent/models"
	"github.com/stretchr/testify/assert"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/anthropic"
	"github.com/tmc/langchaingo/llms/openai"
)

// These fixtures test the real HTTP encoders and stream decoders. A fake Model would miss the
// Anthropic block/choice mismatch and the loss of mixed assistant message parts in this adapter.
func TestNativeHTTP_ToolRoundTrip(t *testing.T) {
	type input struct {
		provider string
		newModel func(string) (llms.Model, error)
	}
	type expected struct{ path string }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"OpenAI compatible", input{"openai", func(url string) (llms.Model, error) {
			return openai.New(openai.WithToken("test-key"), openai.WithModel("wire-test"),
				openai.WithBaseURL(url+"/v1"))
		}}, expected{"/v1/chat/completions"}},
		{"Anthropic", input{"anthropic", func(url string) (llms.Model, error) {
			return anthropic.New(anthropic.WithToken("test-key"), anthropic.WithModel("wire-test"),
				anthropic.WithBaseURL(url+"/v1"))
		}}, expected{"/v1/messages"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var lock sync.Mutex
			var requests []map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				lock.Lock()
				defer lock.Unlock()
				assert.Equal(t, test.expected.path, r.URL.Path)
				assert.Equal(t, http.MethodPost, r.Method)
				var body map[string]any
				if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				requests = append(requests, body)
				data, err := os.ReadFile(filepath.Join("testdata", fmt.Sprintf(
					"native-%s-%d.sse", test.input.provider, len(requests))))
				if !assert.NoError(t, err) {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, err = w.Write(data)
				assert.NoError(t, err)
			}))
			defer server.Close()
			client, err := test.input.newModel(server.URL)
			if !assert.NoError(t, err) {
				return
			}
			var got []map[string]any
			agent := react.NewAgent(models.NewLCGWrapper(client).WithModelName("wire-test")).
				WithCallOptions(gent.WithTemperature(0.5), gent.WithTopP(1), gent.WithMaxTokens(100)).
				WithSystemPromptBuilder(func(*react.SystemPromptContext) []gent.FormattedSection {
					return []gent.FormattedSection{{Name: "instructions", Content: "Read records."}}
				}).RegisterTool(gent.DefineTool("lookup",
				func(_ context.Context, args map[string]any) (string, error) {
					got = append(got, args)
					return "record", nil
				}).Description("Read a record").ParameterSchema(nil).MustBuild())
			ctx := gent.NewExecutionContext(context.Background(), "http", gent.NewBasicLoopData(
				&gent.Task{Text: "Read two records."}))
			first, err := agent.Next(ctx)
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, &gent.AgentLoopResult{Action: gent.LAContinue, NextPrompt: `<lookup>
record
</lookup>
<lookup>
record
</lookup>`}, first)
			final, err := agent.Next(ctx)
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, &gent.AgentLoopResult{Action: gent.LATerminate,
				Result:        []gent.ContentPart{gent.TextContent{Text: "Done."}},
				ContentSource: httpTestSource(ctx, 2)}, final)
			assert.Equal(t, []map[string]any{{"id": float64(1)}, {"id": float64(2)}}, got)
			lock.Lock()
			defer lock.Unlock()
			data, err := os.ReadFile(filepath.Join("testdata",
				"native-"+test.input.provider+"-requests.json"))
			if !assert.NoError(t, err) {
				return
			}
			var want []map[string]any
			assert.NoError(t, json.Unmarshal(data, &want))
			assert.Equal(t, want, requests)
		})
	}
}

func httpTestSource(ctx *gent.ExecutionContext, call int) *gent.ContentSource {
	id := fmt.Sprintf("%s:model:%d", ctx.ContextId(), call)
	var ids []string
	for _, event := range ctx.Events() {
		if after, ok := event.(*gent.AfterModelCallEvent); ok && after.ModelCallId == id {
			ids = after.Response.Choices[0].CandidateSource.MessageIds
		}
	}
	return &gent.ContentSource{ModelCallId: id, StreamId: "iter-0", StreamTopicId: "llm-response",
		MessageIds: ids}
}
