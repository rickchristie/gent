package compaction_test

import (
	"context"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/compaction"
	"github.com/rickchristie/gent/format"
	"github.com/rickchristie/gent/internal/tt"
	"github.com/stretchr/testify/assert"
)

func TestSummarization_RequiresCompletedSummary(t *testing.T) {
	type input struct{ choice *gent.ContentChoice }
	type expected struct {
		summary string
		err     string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"completed summary", input{&gent.ContentChoice{
			CandidateContent: []gent.ContentPart{
				gent.TextContent{Text: "The order is paid."},
			},
			Completion: gent.ModelCompletion{
				Status:         gent.ModelStatusAnswer,
				ProviderReason: "stop",
			},
			MetaContent: []*gent.MetaContent{
				{
					Type:    gent.MetaContentReasoning,
					Content: []gent.ContentPart{gent.TextContent{Text: "Check payment."}},
				},
				{
					Type:    gent.MetaContentIntermediary,
					Content: []gent.ContentPart{gent.TextContent{Text: "The order is paid."}},
				},
			},
		}}, expected{summary: `<prior_work_summary>
The order is paid.
</prior_work_summary>`}},
		{"custom adapter without stop reason",
			input{&gent.ContentChoice{Completion: gent.ModelCompletion{
				Status: gent.ModelStatusAnswer,
			},
				CandidateContent: []gent.ContentPart{gent.TextContent{Text: "The order is paid."}},
			}}, expected{summary: `<prior_work_summary>
The order is paid.
</prior_work_summary>`}},
		{"commentary only",
			input{&gent.ContentChoice{Completion: gent.ModelCompletion{
				Status:         gent.ModelStatusAnswer,
				ProviderReason: "stop",
			},
				MetaContent: []*gent.MetaContent{
					{
						Type:    gent.MetaContentCommentary,
						Content: []gent.ContentPart{gent.TextContent{Text: "I will summarize the order."}},
					},
				},
			}}, expected{err: "summarization model returned no summary text"}},
		{"reasoning only",
			input{&gent.ContentChoice{Completion: gent.ModelCompletion{
				Status:         gent.ModelStatusAnswer,
				ProviderReason: "stop",
			},
				MetaContent: []*gent.MetaContent{
					{
						Type:    gent.MetaContentReasoning,
						Content: []gent.ContentPart{gent.TextContent{Text: "Check payment."}},
					},
				},
			}}, expected{err: "summarization model returned no summary text"}},
		{"unknown metadata only",
			input{&gent.ContentChoice{Completion: gent.ModelCompletion{
				Status:         gent.ModelStatusAnswer,
				ProviderReason: "stop",
			},
				MetaContent: []*gent.MetaContent{
					{Type: "status", Content: []gent.ContentPart{gent.TextContent{Text: "Preparing summary."}}},
				},
			}}, expected{err: "summarization model returned no summary text"}},
		{"empty candidate",
			input{
				&gent.ContentChoice{
					Completion: gent.ModelCompletion{Status: gent.ModelStatusAnswer, ProviderReason: "stop"},
				},
			},
			expected{err: "summarization model returned no summary text"}},
		{"blank candidate",
			input{&gent.ContentChoice{Completion: gent.ModelCompletion{
				Status:         gent.ModelStatusAnswer,
				ProviderReason: "stop",
			},
				CandidateContent: []gent.ContentPart{gent.TextContent{Text: `
	 `}}}}, expected{err: "summarization model returned no summary text"}},
		{"truncated summary", input{&gent.ContentChoice{
			CandidateContent: []gent.ContentPart{
				gent.TextContent{Text: "The order is"},
			},
			Completion: gent.ModelCompletion{
				Status:         gent.ModelStatusIncomplete,
				ProviderReason: "max_output_tokens",
			},
		}}, expected{
			err: "summarization model returned incomplete instead of a completed summary"}},
		{"tool request with text", input{&gent.ContentChoice{
			CandidateContent: []gent.ContentPart{
				gent.TextContent{Text: "Check payment first."},
			},
			Completion: gent.ModelCompletion{
				Status:         gent.ModelStatusToolCalls,
				ProviderReason: "tool_calls",
			},
			ToolCalls: []gent.ModelToolCall{{ID: "call-1", Type: "function",
				FunctionCall: &gent.FunctionCall{Name: "lookup", Arguments: `{}`}}},
		}}, expected{err: "summarization model returned tool_calls instead of a completed summary"}},
		{"continuation with text", input{&gent.ContentChoice{
			Continuation: &gent.ProviderContinuation{Provider: "fixture"},
			CandidateContent: []gent.ContentPart{
				gent.TextContent{Text: "The order is"},
			},
			Completion: gent.ModelCompletion{
				Status:         gent.ModelStatusContinue,
				ProviderReason: "pause_turn",
			},
		}}, expected{err: "summarization model returned continue instead of a completed summary"}},
		{"unknown completion state", input{&gent.ContentChoice{
			CandidateContent: []gent.ContentPart{
				gent.TextContent{Text: "The order is paid."},
			},
			Completion: gent.ModelCompletion{
				Status:         gent.ModelStatusFailed,
				ProviderReason: "unknown",
			},
		}}, expected{err: "summarization model returned failed instead of a completed summary"}},
		{"answer with tool calls", input{&gent.ContentChoice{
			Completion:       gent.ModelCompletion{Status: gent.ModelStatusAnswer},
			CandidateContent: []gent.ContentPart{gent.TextContent{Text: "Check payment first."}},
			ToolCalls: []gent.ModelToolCall{{ID: "call-1", Type: "function",
				FunctionCall: &gent.FunctionCall{Name: "lookup", Arguments: `{}`}}},
		}}, expected{err: "summarization model completion: gent: answer completion contains tool calls"}},
		{"answer with refusal data", input{&gent.ContentChoice{
			Completion: gent.ModelCompletion{Status: gent.ModelStatusAnswer,
				Refusal: &gent.ModelRefusal{Code: "blocked"}},
			CandidateContent: []gent.ContentPart{gent.TextContent{Text: "Check payment first."}},
		}}, expected{err: `summarization model completion: ` +
			`gent: completion "answer" has inconsistent refusal data`}},
		{"invalid custom state", input{&gent.ContentChoice{
			Completion: gent.ModelCompletion{Status: "custom"},
		}}, expected{err: `summarization model completion: gent: unknown model completion "custom"`}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx := gent.NewExecutionContext(context.Background(), "summary",
				gent.NewBasicLoopData(nil))
			defer ctx.Release()
			if !assert.NoError(t, ctx.AppendRecord(&gent.HistoryEntry{
				Messages: []*gent.MessageContent{{Role: gent.ChatMessageTypeHuman,
					Parts: []gent.ContentPart{gent.TextContent{Text: "The order is paid."}}}},
			}, true)) {
				return
			}
			original, err := ctx.Scratchpad()
			if !assert.NoError(t, err) {
				return
			}
			draft := ctx.ContextDraft()
			selection, err := ctx.CompactionInput()
			if !assert.NoError(t, err) {
				return
			}
			model := tt.NewMockModel().AddRawResponse(&gent.ContentResponse{
				Choices: []*gent.ContentChoice{test.input.choice},
			})
			strategy := compaction.NewSummarization(model, format.NewXML())
			update, err := strategy.Compact(ctx, selection)
			if test.expected.err != "" {
				assert.EqualError(t, err, test.expected.err)
				assert.Equal(t, gent.HistoryUpdate{}, update)
				active, readErr := ctx.Scratchpad()
				assert.NoError(t, readErr)
				assert.Equal(t, original, active)
				assert.Equal(t, draft, ctx.ContextDraft())
				return
			}
			if !assert.NoError(t, err) || !assert.NoError(t, ctx.UpdateHistory(update)) {
				return
			}
			active, err := ctx.Scratchpad()
			if !assert.NoError(t, err) || !assert.Len(t, active, 1) {
				return
			}
			assert.Equal(t, []*gent.MessageContent{{Role: gent.ChatMessageTypeAI,
				Parts: []gent.ContentPart{gent.TextContent{Text: test.expected.summary}}}},
				active[0].Messages)
			assert.Equal(t, update.Active, ctx.ContextDraft().Active)
			for _, entry := range original {
				saved, found := ctx.History().Entry(entry.Id)
				assert.True(t, found)
				assert.Equal(t, entry, saved)
			}
		})
	}
}
