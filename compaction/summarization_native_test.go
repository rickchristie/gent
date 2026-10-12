package compaction_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents/react"
	"github.com/rickchristie/gent/compaction"
	"github.com/rickchristie/gent/format"
	"github.com/rickchristie/gent/internal/tt"
	"github.com/stretchr/testify/assert"
	"github.com/tmc/langchaingo/llms"
)

func TestSummarization_NativeToolEvidence(t *testing.T) {
	type input struct{ content string }
	type expected struct {
		prompt string
		err    error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"calls without assistant text", input{}, expected{`## Existing Summary

None (first compaction).
### Message 1

Tool call "call-a": lookup
Arguments: {"id":7}
Tool call "call-b": lookup
Arguments: {"id":8}
Tool result "call-a": lookup
<lookup>
record 7
</lookup>
Tool result "call-b": lookup
<lookup>
record 8
</lookup>

`, nil}},
		{"calls with assistant text", input{"I will read both records."},
			expected{`## Existing Summary

None (first compaction).
### Message 1

I will read both records.
Tool call "call-a": lookup
Arguments: {"id":7}
Tool call "call-b": lookup
Arguments: {"id":8}
Tool result "call-a": lookup
<lookup>
record 7
</lookup>
Tool result "call-b": lookup
<lookup>
record 8
</lookup>

`, nil}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			model := tt.NewMockModel().AddRawResponse(&gent.ContentResponse{
				Choices: []*gent.ContentChoice{{Content: test.input.content,
					ToolCalls: []llms.ToolCall{
						{ID: "call-a", Type: "function", FunctionCall: &llms.FunctionCall{
							Name: "lookup", Arguments: `{"id":7}`}},
						{ID: "call-b", Type: "function", FunctionCall: &llms.FunctionCall{
							Name: "lookup", Arguments: `{"id":8}`}},
					}}},
			})
			agent := react.NewNativeAgent(model).RegisterTool(gent.NewToolFunc("lookup", "Read",
				nil, func(_ context.Context, args map[string]any) (string, error) {
					return fmt.Sprintf("record %v", args["id"]), nil
				}))
			ctx := gent.NewExecutionContext(context.Background(), "summary", gent.NewBasicLoopData(
				&gent.Task{Text: "Read both records."}))
			result, err := agent.Next(ctx)
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, &gent.AgentLoopResult{Action: gent.LAContinue, NextPrompt: `<lookup>
record 7
</lookup>
<lookup>
record 8
</lookup>`}, result)
			original, err := ctx.Scratchpad()
			if !assert.NoError(t, err) {
				return
			}
			summary := tt.NewMockModel().AddResponse("Read records 7 and 8.", 0, 0)
			strategy := compaction.NewSummarization(summary, format.NewXML()).WithPrompt(`%s
%s`)
			selection, err := ctx.CompactionInput()
			if !assert.NoError(t, err) {
				return
			}
			update, err := strategy.Compact(ctx, selection)
			if !assert.Equal(t, test.expected.err, err) {
				return
			}
			assert.Equal(t, [][]llms.MessageContent{{llms.TextParts(llms.ChatMessageTypeHuman,
				test.expected.prompt)}}, summary.CapturedMessages)
			assert.NoError(t, ctx.UpdateHistory(update))
			active, err := ctx.Scratchpad()
			if !assert.NoError(t, err) || !assert.Len(t, active, 1) {
				return
			}
			assert.Equal(t, update.Active, ctx.ContextDraft().Active)
			assert.Equal(t, []*gent.MessageContent{{Role: llms.ChatMessageTypeAI,
				Parts: []gent.ContentPart{llms.TextContent{Text: `<prior_work_summary>
Read records 7 and 8.
</prior_work_summary>`}}}}, active[0].Messages)
			for _, entry := range original {
				saved, found := ctx.History().Entry(entry.Id)
				assert.True(t, found)
				assert.Equal(t, entry, saved)
			}
		})
	}
}

func TestSummarization_TextAndMedia(t *testing.T) {
	ctx := gent.NewExecutionContext(context.Background(), "summary", gent.NewBasicLoopData(nil))
	assert.NoError(t, ctx.AppendRecord(&gent.HistoryEntry{Messages: []*gent.MessageContent{
		{Role: llms.ChatMessageTypeHuman, Parts: []gent.ContentPart{
			llms.TextContent{Text: "Read the image."},
			llms.ImageURLContent{URL: "https://example.com/image.png"},
		}},
		{Role: llms.ChatMessageTypeAI, Parts: []gent.ContentPart{
			llms.TextContent{Text: "The image has two records."},
		}},
	}}, true))
	model := tt.NewMockModel().AddResponse("Two records.", 0, 0)
	strategy := compaction.NewSummarization(model, format.NewXML()).WithPrompt(`%s
%s`)
	selection, err := ctx.CompactionInput()
	if !assert.NoError(t, err) {
		return
	}
	_, err = strategy.Compact(ctx, selection)
	assert.NoError(t, err)
	assert.Equal(t, [][]llms.MessageContent{{llms.TextParts(llms.ChatMessageTypeHuman,
		`## Existing Summary

None (first compaction).
### Message 1

Read the image.
The image has two records.

`)}}, model.CapturedMessages)
}
