package models

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents/react"
	"github.com/rickchristie/gent/compaction"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/format"
	"github.com/rickchristie/gent/output"
	"github.com/stretchr/testify/assert"
)

func providerTextResponse(t *testing.T, provider, text string) string {
	t.Helper()
	encoded, err := json.Marshal(text)
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	return strings.ReplaceAll(providerResponse(provider, 3), `"{\"value\":3}"`, string(encoded))
}

type compactionCallChoice struct{}

func (compactionCallChoice) OnBeforeModelCall(_ *gent.ExecutionContext,
	event *gent.BeforeModelCallEvent,
) {
	if event.StreamTopicId == "compaction" || event.Provider == "ollama" {
		return
	}
	// Require the two tool turns before the answer where the endpoint supports tool choice.
	// This makes the test's execution contract explicit instead of trusting a prompt alone.
	event.Request.Options.ToolChoice = "none"
	if event.Iteration <= 2 {
		event.Request.Options.ToolChoice = "required"
	}
}

// Use the same runner path for fixtures and live checks. Two compactions must retain the area,
// and the second compaction must retain a complete native tool call and result for replay.
func checkProviderCompaction(t *testing.T, base context.Context, model gent.Model, record string) {
	t.Helper()
	type compactionValue struct {
		Area   string `json:"area"`
		First  string `json:"first"`
		Second string `json:"second"`
	}
	type expected struct {
		value        compactionValue
		content      string
		reason       gent.TerminationReason
		calls        []providerLookupInput
		compactions  int64
		summaryCalls int
		mainCalls    int
		err          error
	}
	actual := expected{}
	tool := gent.DefineTool("lookup", func(_ context.Context, input providerLookupInput) (
		*gent.ToolExecutionResponse, error,
	) {
		actual.calls = append(actual.calls, input)
		return capturedToolResponse(fmt.Sprintf("%s-%d", record, input.ID), nil)
	}).Description("Read the record for the supplied id.").MustBuild()
	// Enforce the answer format through Gent's output contract. The test concerns retained facts
	// and native history, so a model's optional prose must not determine whether it can finish.
	out, err := output.NewJsonOutput[compactionValue]()
	if !assert.NoError(t, err) {
		return
	}
	agent := react.NewWithOutput(model, out).RegisterTool(tool).
		WithCallOptions(gent.WithMaxTokens(2048))
	ctx := liveExecutionContext(base, "compaction", out.Instructions()+`
Use history and tools to answer exactly.
The prior_work_summary is saved history. Tool calls and results in that summary are completed work.
Use those results without repeating completed calls.`,
		`Read the saved area from history. Call lookup with id 7, wait for its result,
then call lookup with id 8. Make each call exactly once, in separate turns.
Return a JSON object with area, first, and second.
Use the saved area and the two exact record texts.
Do not add any other fields or text.`)
	defer logCompactionFailure(t, ctx)
	for _, text := range []string{
		"The saved area is Tebet.", "Keep the saved area for the final answer.", "Now read the record.",
	} {
		if !assert.NoError(t, ctx.AppendRecord(&gent.HistoryEntry{
			Messages: []*gent.MessageContent{{Role: gent.ChatMessageTypeHuman,
				Parts: []gent.ContentPart{gent.TextContent{Text: text}}}},
		}, true)) {
			return
		}
	}
	ctx.SetCompaction(compaction.NewStatThresholdTrigger().OnGauge(gent.SGScratchpadLength, 3),
		compaction.NewSummarization(model, format.NewMarkdown()).WithKeepRecent(1))
	var result *gent.ExecutionResult
	runner := executor.NewRunner(agent, executor.DefaultConfig()).Subscribe(compactionCallChoice{})
	actual.value, result, actual.err = runner.Execute(ctx)
	if !assert.NoError(t, actual.err) {
		return
	}
	actual.content, actual.reason = gent.ContentText(result.Output), result.TerminationReason
	actual.compactions = ctx.Stats().GetCounter(gent.SCCompactions)
	var summaries []*gent.MessageContent
	for _, entry := range ctx.History().Entries {
		if entry.Kind != gent.HistorySummary {
			continue
		}
		assert.Equal(t, gent.RecordCompactedSynthetic, entry.Origin)
		if assert.Len(t, entry.Messages, 1) {
			message := entry.Messages[0]
			assert.Equal(t, gent.ChatMessageTypeAI, message.Role)
			assert.Nil(t, message.Continuation)
			summaries = append(summaries, message)
		}
	}
	if !assert.Len(t, summaries, 2) {
		return
	}
	for _, event := range ctx.Events() {
		before, ok := event.(*gent.BeforeModelCallEvent)
		if !ok {
			continue
		}
		if before.StreamTopicId == "compaction" {
			actual.summaryCalls++
			continue
		}
		actual.mainCalls++
		if actual.mainCalls == 1 {
			continue
		}
		if actual.mainCalls > len(summaries)+1 {
			t.Error("unexpected model call after compaction")
			continue
		}
		messages := before.Request.Messages
		if !assert.GreaterOrEqual(t, len(messages), 4) {
			return
		}
		assert.Equal(t, gent.ChatMessageTypeAI, messages[2].Role)
		assert.Equal(t, summaries[actual.mainCalls-2].Parts, messages[2].Parts)
		assert.Nil(t, messages[2].Continuation)
		assert.NotNil(t, messages[3].Continuation, "recent native tool evidence must survive")
		assert.Equal(t, gent.ChatMessageTypeTool, messages[len(messages)-1].Role)
	}
	answer := compactionValue{"Tebet", record + "-7", record + "-8"}
	encoded, err := json.Marshal(answer)
	if !assert.NoError(t, err) {
		return
	}
	want := expected{answer, string(encoded), gent.TerminationSuccess,
		[]providerLookupInput{{ID: 7}, {ID: 8}}, 2, 2, 3, nil}
	assert.Equal(t, want, actual)
	// A saved compacted run must retain its accepted value without another model or tool call.
	saved, err := ctx.Export()
	if !assert.NoError(t, err) {
		return
	}
	wire, err := ctx.Codecs().Marshal(saved)
	if !assert.NoError(t, err) {
		return
	}
	var bundle gent.RunView
	if !assert.NoError(t, ctx.Codecs().Unmarshal(wire, &bundle)) {
		return
	}
	for range 2 {
		value, restored, err := runner.Resume(base, bundle, gent.RestoreBindings{
			Configure: func(restored *gent.ExecutionContext) error {
				restored.SetCompaction(
					compaction.NewStatThresholdTrigger().OnGauge(gent.SGScratchpadLength, 3),
					compaction.NewSummarization(model, format.NewMarkdown()).WithKeepRecent(1))
				return nil
			},
		})
		assert.NoError(t, err)
		assert.Equal(t, answer, value)
		assert.Equal(t, result, restored)
	}
	assert.Equal(t, want.calls, actual.calls)
}

// Only test records and model text appear here. Keep credentials and opaque provider items out.
func logCompactionFailure(t *testing.T, ctx *gent.ExecutionContext) {
	t.Helper()
	if !t.Failed() {
		return
	}
	for _, event := range ctx.Events() {
		after, ok := event.(*gent.AfterModelCallEvent)
		if !ok || after.Response == nil {
			continue
		}
		for _, choice := range after.Response.Choices {
			t.Logf("topic=%s iteration=%d status=%s text=%q", after.StreamTopicId, after.Iteration,
				choice.Completion.Status, gent.ContentText(choice.CandidateContent))
			for _, call := range choice.ToolCalls {
				t.Logf("tool=%s arguments=%s", call.FunctionCall.Name, call.FunctionCall.Arguments)
			}
		}
	}
}

func TestProvidersRepeatedCompaction(t *testing.T) {
	type input struct{ provider string }
	type expected struct{ requests int }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	var cases []testCase
	for _, provider := range testProviders {
		cases = append(cases, testCase{provider, input{provider}, expected{5}})
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			client := &http.Client{Transport: responseTransport(func(request *http.Request) (
				*http.Response, error,
			) {
				if request.Method == http.MethodGet {
					return fixtureHTTP(routerFixture, "application/json"), nil
				}
				requests++
				var response string
				switch requests {
				case 1:
					response = providerResponse(test.input.provider, 0)
				case 2, 4:
					response = providerTextResponse(t, test.input.provider,
						"The saved area is Tebet. The first record is record-seven-7.")
				case 3:
					response = strings.ReplaceAll(providerResponse(test.input.provider, 0),
						"call-7", "call-8")
					response = strings.ReplaceAll(response, ":7", ":8")
				case 5:
					response = providerTextResponse(t, test.input.provider,
						`{"area":"Tebet","first":"record-seven-7","second":"record-seven-8"}`)
				default:
					return nil, fmt.Errorf("unexpected compaction request %d", requests)
				}
				return fixtureHTTP(response, "application/json"), nil
			})}
			checkProviderCompaction(t, context.Background(), fixtureModel(test.input.provider, client),
				"record-seven")
			assert.Equal(t, test.expected, expected{requests})
		})
	}
}
