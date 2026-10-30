package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/format"
	"github.com/rickchristie/gent/policy"
	"github.com/stretchr/testify/assert"
)

// Fixed vectors make the test independent of downloaded models and native libraries.
// The real policy adapters, BM25 index, vector index, and fuser still run.
type lessonEmbedder struct{}

func (*lessonEmbedder) TokenCount(text string) int { return len(strings.Fields(text)) }
func (*lessonEmbedder) MaxTokens() int             { return 512 }
func (*lessonEmbedder) Dimensions() int            { return 2 }
func (*lessonEmbedder) Close() error               { return nil }
func (*lessonEmbedder) EmbedQuery(ctx context.Context, _ string) ([]float32, error) {
	return []float32{1, 0}, ctx.Err()
}
func (*lessonEmbedder) EmbedText(ctx context.Context, _ string) ([]float32, error) {
	return []float32{1, 0}, ctx.Err()
}
func (*lessonEmbedder) EmbedTextBatch(ctx context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for i := range vectors {
		vectors[i] = []float32{1, 0}
	}
	return vectors, ctx.Err()
}

type lessonModel struct {
	choices  []*gent.ContentChoice
	requests []gent.ModelCallRequest
}

func (m *lessonModel) GenerateContentStream(
	ctx context.Context, request gent.ModelCallRequest,
) (gent.Stream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	index := len(m.requests) % len(m.choices)
	m.requests = append(m.requests, request)
	return gent.NewCompletedStream(&gent.ContentResponse{
		Choices: []*gent.ContentChoice{m.choices[index]},
	}, nil), nil
}

func modelCall(id, name, arguments string) *gent.ContentChoice {
	return &gent.ContentChoice{Completion: gent.ModelCompletion{
		Status: gent.ModelStatusToolCalls,
	},
		ToolCalls: []gent.ModelToolCall{{
			ID: id, Type: "function",
			FunctionCall: &gent.FunctionCall{Name: name, Arguments: arguments},
		}}}
}

func TestLessonPolicyRun(t *testing.T) {
	type input struct {
		mode     policy.SearchResultMode
		question string
	}
	type mocks struct {
		choices []*gent.ContentChoice
	}
	type expected struct {
		answer      string
		err         error
		definitions []string
		toolCalls   []string
		lastResult  gent.ToolCallResponse
	}
	type testCase struct {
		name     string
		input    input
		mocks    mocks
		expected expected
	}
	const answer = "Policy return-window permits unused returns within 30 days. Supply the order ID."
	const fullPolicy = `# return-window

## Returns

An unused item can be returned within 30 days of delivery.
The customer must supply the order ID. Check the delivery date and item condition.
The refund uses the original payment method after the return is checked.`
	const fullSearch = `# return-window

## Returns

An unused item can be returned within 30 days of delivery.
The customer must supply the order ID. Check the delivery date and item condition.
The refund uses the original payment method after the return is checked.

---

# damaged-delivery

## Damage on delivery

Report damage within 7 days of delivery. Supply the order ID and a photo of the damage.
After the report is checked, the customer can request a replacement or a refund.`
	searchCall := modelCall("search-1", "search_policy", `{"query":"return refund unused"}`)
	getCall := modelCall("get-1", "get_policy", `{"policy_id":"return-window"}`)
	final := &gent.ContentChoice{
		Completion:       gent.ModelCompletion{Status: gent.ModelStatusAnswer},
		CandidateContent: []gent.ContentPart{gent.TextContent{Text: answer}},
	}
	formatted := func(name, content string) string {
		return format.NewXML().FormatSections([]gent.FormattedSection{{Name: name, Content: content}})
	}
	cases := []testCase{
		{
			name:  "full policies reach the next model call",
			input: input{policy.SearchResultFull, "Can I return an unused item after 10 days?"},
			mocks: mocks{[]*gent.ContentChoice{searchCall, final}},
			expected: expected{
				answer: answer, definitions: []string{"search_policy"},
				toolCalls: []string{"search_policy"},
				lastResult: gent.ToolCallResponse{ToolCallID: "search-1", Name: "search_policy",
					Content: formatted("search_policy", fullSearch)},
			},
		},
		{
			name:  "snippet mode can fetch the full policy",
			input: input{policy.SearchResultSnippet, "Can I return an unused item after 10 days?"},
			mocks: mocks{[]*gent.ContentChoice{searchCall, getCall, final}},
			expected: expected{
				answer: answer, definitions: []string{"search_policy", "get_policy"},
				toolCalls: []string{"search_policy", "get_policy"},
				lastResult: gent.ToolCallResponse{ToolCallID: "get-1", Name: "get_policy",
					Content: formatted("get_policy", fullPolicy)},
			},
		},
		{
			name:  "title mode can fetch the full policy",
			input: input{policy.SearchResultTitle, "Can I return an unused item after 10 days?"},
			mocks: mocks{[]*gent.ContentChoice{searchCall, getCall, final}},
			expected: expected{
				answer: answer, definitions: []string{"search_policy", "get_policy"},
				toolCalls: []string{"search_policy", "get_policy"},
				lastResult: gent.ToolCallResponse{ToolCallID: "get-1", Name: "get_policy",
					Content: formatted("get_policy", fullPolicy)},
			},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			model := &lessonModel{choices: test.mocks.choices}
			runner, err := BuildRunner(context.Background(), model, &lessonEmbedder{}, test.input.mode)
			if !assert.NoError(t, err) {
				return
			}
			// Reuse the runner to verify that each question gets a separate message history.
			for run := 0; run < 2; run++ {
				answer, err := Answer(context.Background(), runner, test.input.question)
				assert.Equal(t, test.expected.err, err)
				assert.Equal(t, test.expected.answer, answer)
				start := run * len(test.mocks.choices)
				requests := model.requests[start:]
				if !assert.Len(t, requests, len(test.mocks.choices)) {
					return
				}
				assert.Len(t, requests[0].Messages, 2)
				var definitions []string
				for _, definition := range requests[0].Options.Tools {
					definitions = append(definitions, definition.Function.Name)
				}
				assert.Equal(t, test.expected.definitions, definitions)
				var calls []string
				var last gent.ToolCallResponse
				for _, message := range requests[len(requests)-1].Messages {
					for _, part := range message.Parts {
						if result, ok := part.(gent.ToolCallResponse); ok {
							calls = append(calls, result.Name)
							last = result
						}
					}
				}
				assert.Equal(t, test.expected.toolCalls, calls)
				assert.Equal(t, test.expected.lastResult, last)
			}
		})
	}
}

func TestLessonCanceledSetup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner, err := BuildRunner(ctx, &lessonModel{}, &lessonEmbedder{}, policy.SearchResultFull)
	assert.Nil(t, runner)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, "policy: failed to index policies: context canceled", fmt.Sprint(err))
}
