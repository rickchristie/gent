package models_test

import genttest "github.com/rickchristie/gent/internal/tt"

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
)

// These fixtures test the real HTTP encoders and stream decoders. A fake Model would miss the
// loss of ordered message blocks or tool arguments at the HTTP boundary.
func TestNativeHTTP_ToolRoundTrip(t *testing.T) {
	type input struct {
		provider string
		newModel func(string) (gent.Model, error)
	}
	type expected struct{ path string }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"vLLM", input{"vllm", func(url string) (gent.Model, error) {
			return models.NewVLLM(&models.Config{APIKey: "test-key", Model: "wire-test",
				BaseURL: url + "/v1"}), nil
		}}, expected{"/v1/chat/completions"}},
		{"Anthropic", input{"anthropic", func(url string) (gent.Model, error) {
			return models.NewAnthropic(&models.Config{APIKey: "test-key", Model: "wire-test",
				BaseURL: url + "/v1"}), nil
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
			agent := react.NewAgent(client).
				WithCallOptions(gent.WithTemperature(0.5), gent.WithTopP(1), gent.WithMaxTokens(100)).
				RegisterTool(gent.DefineTool("lookup",
					func(_ context.Context, args map[string]any) (string, error) {
						got = append(got, args)
						return "record", nil
					}).Description("Read a record").ParameterSchema(nil).MustBuild())
			ctx := gent.NewExecutionContext(context.Background(), "http", gent.NewBasicLoopData(
				&gent.Task{
					Messages: []*gent.ModelMessage{
						gent.TextParts(gent.ChatMessageTypeSystem, "Read records."),
						gent.TextParts(gent.ChatMessageTypeHuman, "Read two records."),
					},
				}))
			first, err := agent.Next(ctx)
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, &gent.AgentLoopResult{Action: gent.LAContinue}, first)
			final, err := agent.Next(ctx)
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, &gent.AgentLoopResult{TerminationReason: gent.TerminationSuccess,
				Action:        gent.LATerminate,
				Result:        []gent.ContentPart{gent.TextContent{Text: "Done."}},
				ContentSource: httpTestSource(ctx, 2),
				Value: genttest.Value(gent.ContentText([]gent.ContentPart{
					gent.TextContent{Text: "Done."},
				})),
			}, final)
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
