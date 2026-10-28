package models

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents/react"
	"github.com/stretchr/testify/assert"
)

func TestResponsesAgentMessagePhases(t *testing.T) {
	type input struct{ output string }
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
	const commentary = `{"type":"message","phase":"commentary","role":"assistant",
"content":[{"type":"output_text","text":"I will check this."}]}`
	const answer = `{"type":"message","phase":"final_answer","role":"assistant",
"content":[{"type":"output_text","text":"Done."}]}`
	const ordinary = `{"type":"message","role":"assistant",
"content":[{"type":"output_text","text":"Done."}]}`
	const call = `{"type":"function_call","call_id":"call-1","name":"lookup","arguments":"{}"}`
	accepted := &gent.AgentLoopResult{Action: gent.LATerminate,
		Result: []gent.ContentPart{gent.TextContent{Text: "Done."}}}
	tools := &gent.AgentLoopResult{Action: gent.LAContinue, NextPrompt: `<lookup>
record
</lookup>`}
	cases := []testCase{
		{"commentary alone", input{commentary}, expected{result: &gent.AgentLoopResult{
			Action: gent.LAContinue, NextPrompt: "Use a tool or give a final answer."}}},
		{"commentary and answer", input{commentary + "," + answer}, expected{result: accepted}},
		{"answer alone", input{answer}, expected{result: accepted}},
		{"unlabelled answer", input{ordinary}, expected{result: accepted}},
		{"commentary and tool", input{commentary + "," + call}, expected{result: tools, calls: 1}},
		{"unlabelled text and tool", input{ordinary + "," + call}, expected{result: tools, calls: 1}},
		{"answer and tool", input{answer + "," + call}, expected{result: tools, calls: 1}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			model := NewResponses(&ResponsesConfig{
				Provider: "openai", BaseURL: "https://fixture.test/v1", Model: "fixture",
				HTTPClient: &http.Client{Transport: responseTransport(func(
					*http.Request,
				) (*http.Response, error) {
					body := `{"status":"completed","output":[` + test.input.output + `]}`
					return &http.Response{StatusCode: 200,
						Header: http.Header{"Content-Type": {"application/json"}},
						Body:   io.NopCloser(strings.NewReader(body))}, nil
				})},
			})
			calls := 0
			agent := react.NewAgent(model).RegisterTool(gent.DefineTool("lookup",
				func(context.Context, map[string]any) (string, error) {
					calls++
					return "record", nil
				}).Description("Read").ParameterSchema(nil).MustBuild())
			ctx := gent.NewExecutionContext(context.Background(), "phases",
				gent.NewBasicLoopData(&gent.Task{Text: "Read the record."}))
			result, err := agent.Next(ctx)
			want := test.expected
			if want.result.Action == gent.LATerminate {
				copy := *want.result
				id := ctx.ContextId() + ":model:1"
				var ids []string
				for _, event := range ctx.Events() {
					if after, ok := event.(*gent.AfterModelCallEvent); ok {
						ids = after.Response.Choices[0].CandidateSource.MessageIds
					}
				}
				copy.ContentSource = &gent.ContentSource{ModelCallId: id, StreamId: "iter-0",
					StreamTopicId: "llm-response", MessageIds: ids}
				want.result = &copy
			}
			assert.Equal(t, want, expected{result, calls, err})
		})
	}
}
