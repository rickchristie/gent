package compaction

import (
	"context"
	"errors"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/internal/tt"
	"github.com/stretchr/testify/assert"
	"github.com/tmc/langchaingo/llms"
)

func TestSummarization_Compact(t *testing.T) {
	type input struct {
		keepRecent       int
		customPrompt     string
		scratchpad       []*gent.Iteration
		modelResponse    string
		modelError       error
		modelRawResponse *gent.ContentResponse
	}

	type expected struct {
		err              string
		scratchpadLen    int
		syntheticCount   int
		keptOriginals    int
		pinnedCount      int
		summaryContains  string
		inputTokensStat  int64
		outputTokensStat int64
	}

	pinned1 := makePinnedIter("important finding")
	pinned2 := makePinnedIter("critical data")

	existingSynthetic := &gent.Iteration{
		Messages: []*gent.MessageContent{
			{
				Role: llms.ChatMessageTypeGeneric,
				Parts: []gent.ContentPart{
					llms.TextContent{
						Text: "Previous summary content",
					},
				},
			},
		},
		Origin: gent.IterationCompactedSynthetic,
	}

	imageIter := &gent.Iteration{
		Messages: []*gent.MessageContent{
			{
				Role: llms.ChatMessageTypeAI,
				Parts: []gent.ContentPart{
					llms.TextContent{
						Text: "text part",
					},
					llms.BinaryContent{
						MIMEType: "image/png",
						Data:     []byte("fake-img"),
					},
				},
			},
		},
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "empty scratchpad no change",
			input: input{
				keepRecent:    0,
				scratchpad:    []*gent.Iteration{},
				modelResponse: "summary",
			},
			expected: expected{
				scratchpadLen:  0,
				syntheticCount: 0,
				keptOriginals:  0,
				pinnedCount:    0,
			},
		},
		{
			name: "all iterations within keepRecent " +
				"no change",
			input: input{
				keepRecent: 5,
				scratchpad: []*gent.Iteration{
					makeIter("a"), makeIter("b"),
				},
				modelResponse: "summary",
			},
			expected: expected{
				scratchpadLen:  2,
				syntheticCount: 0,
				keptOriginals:  2,
				pinnedCount:    0,
			},
		},
		{
			name: "pure progressive keepRecent 0 " +
				"summarizes all",
			input: input{
				keepRecent: 0,
				scratchpad: []*gent.Iteration{
					makeIter("step 1"),
					makeIter("step 2"),
					makeIter("step 3"),
				},
				modelResponse: "Summary of steps 1-3",
			},
			expected: expected{
				scratchpadLen:    1,
				syntheticCount:   1,
				keptOriginals:    0,
				pinnedCount:      0,
				summaryContains:  "Summary of steps 1-3",
				inputTokensStat:  10,
				outputTokensStat: 5,
			},
		},
		{
			name: "hybrid keepRecent 2 summarizes " +
				"older keeps recent",
			input: input{
				keepRecent: 2,
				scratchpad: []*gent.Iteration{
					makeIter("old 1"),
					makeIter("old 2"),
					makeIter("recent 1"),
					makeIter("recent 2"),
				},
				modelResponse: "Summary of old 1-2",
			},
			expected: expected{
				scratchpadLen:    3,
				syntheticCount:   1,
				keptOriginals:    2,
				pinnedCount:      0,
				summaryContains:  "Summary of old 1-2",
				inputTokensStat:  10,
				outputTokensStat: 5,
			},
		},
		{
			name: "existing synthetic summary is " +
				"replaced",
			input: input{
				keepRecent: 0,
				scratchpad: []*gent.Iteration{
					existingSynthetic,
					makeIter("new step 1"),
					makeIter("new step 2"),
				},
				modelResponse: "Updated summary",
			},
			expected: expected{
				scratchpadLen:    1,
				syntheticCount:   1,
				keptOriginals:    0,
				pinnedCount:      0,
				summaryContains:  "Updated summary",
				inputTokensStat:  10,
				outputTokensStat: 5,
			},
		},
		{
			name: "pinned iterations preserved",
			input: input{
				keepRecent: 1,
				scratchpad: []*gent.Iteration{
					makeIter("old"),
					pinned1,
					makeIter("recent"),
				},
				modelResponse: "Summary of old",
			},
			expected: expected{
				// synthetic + pinned + 1 recent
				scratchpadLen:    3,
				syntheticCount:   1,
				keptOriginals:    1,
				pinnedCount:      1,
				summaryContains:  "Summary of old",
				inputTokensStat:  10,
				outputTokensStat: 5,
			},
		},
		{
			name: "multiple pinned iterations preserved",
			input: input{
				keepRecent: 0,
				scratchpad: []*gent.Iteration{
					pinned1,
					makeIter("a"),
					pinned2,
					makeIter("b"),
				},
				modelResponse: "Summary of a and b",
			},
			expected: expected{
				// synthetic + 2 pinned
				scratchpadLen:    3,
				syntheticCount:   1,
				keptOriginals:    0,
				pinnedCount:      2,
				summaryContains:  "Summary of a and b",
				inputTokensStat:  10,
				outputTokensStat: 5,
			},
		},
		{
			name: "multimodal content dropped text " +
				"extracted",
			input: input{
				keepRecent: 0,
				scratchpad: []*gent.Iteration{
					imageIter,
					makeIter("text only"),
				},
				modelResponse: "Summary with text only",
			},
			expected: expected{
				scratchpadLen:    1,
				syntheticCount:   1,
				keptOriginals:    0,
				pinnedCount:      0,
				summaryContains:  "Summary with text only",
				inputTokensStat:  10,
				outputTokensStat: 5,
			},
		},
		{
			name: "model error returns error",
			input: input{
				keepRecent: 0,
				scratchpad: []*gent.Iteration{
					makeIter("data"),
				},
				modelError: errors.New(
					"API rate limit exceeded",
				),
			},
			expected: expected{
				err: "summarization model call: " +
					"API rate limit exceeded",
				// Scratchpad unchanged on error
				scratchpadLen:  1,
				syntheticCount: 0,
				keptOriginals:  1,
				pinnedCount:    0,
			},
		},
		{
			name: "custom prompt is used",
			input: input{
				keepRecent:    0,
				customPrompt:  "Summarize: %s\n%s",
				scratchpad:    []*gent.Iteration{makeIter("x")},
				modelResponse: "Custom summarized",
			},
			expected: expected{
				scratchpadLen:    1,
				syntheticCount:   1,
				keptOriginals:    0,
				pinnedCount:      0,
				summaryContains:  "Custom summarized",
				inputTokensStat:  10,
				outputTokensStat: 5,
			},
		},
		{
			name: "model returns empty choices " +
				"returns error",
			input: input{
				keepRecent: 0,
				scratchpad: []*gent.Iteration{
					makeIter("data"),
				},
				modelRawResponse: &gent.ContentResponse{
					Choices: []*gent.ContentChoice{},
					Info: &gent.GenerationInfo{
						InputTokens:  10,
						OutputTokens: 0,
					},
				},
			},
			expected: expected{
				err: "summarization model " +
					"returned no choices",
				scratchpadLen:  1,
				syntheticCount: 0,
				keptOriginals:  1,
				pinnedCount:    0,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			model := tt.NewMockModel()
			if tc.input.modelError != nil {
				model.AddError(tc.input.modelError)
			} else if tc.input.modelRawResponse != nil {
				model.AddRawResponse(
					tc.input.modelRawResponse,
				)
			} else if tc.input.modelResponse != "" {
				model.AddResponse(
					tc.input.modelResponse, 10, 5,
				)
			}

			strategy := NewSummarization(model)
			if tc.input.keepRecent > 0 {
				strategy.WithKeepRecent(
					tc.input.keepRecent,
				)
			}
			if tc.input.customPrompt != "" {
				strategy.WithPrompt(
					tc.input.customPrompt,
				)
			}

			data := gent.NewBasicLoopData(nil)
			data.SetScratchPad(tc.input.scratchpad)
			execCtx := gent.NewExecutionContext(
				context.Background(), "test", data,
			)
			execCtx.SetLimits(nil)

			err := strategy.Compact(execCtx)

			if tc.expected.err != "" {
				assert.EqualError(t, err, tc.expected.err)
				return
			}

			assert.NoError(t, err)
			result := data.GetScratchPad()
			assert.Equal(t,
				tc.expected.scratchpadLen, len(result),
			)

			// Count categories
			var synthetics, originals, pinnedCount int
			for _, iter := range result {
				switch {
				case iter.Origin ==
					gent.IterationCompactedSynthetic:
					synthetics++
				case isPinned(iter):
					pinnedCount++
				default:
					originals++
				}
			}

			assert.Equal(t,
				tc.expected.syntheticCount, synthetics,
				"synthetic iteration count",
			)
			assert.Equal(t,
				tc.expected.keptOriginals, originals,
				"kept original count",
			)
			assert.Equal(t,
				tc.expected.pinnedCount, pinnedCount,
				"pinned iteration count",
			)

			// Verify summary text content
			if tc.expected.summaryContains != "" {
				found := false
				for _, iter := range result {
					if iter.Origin !=
						gent.IterationCompactedSynthetic {
						continue
					}
					text := extractText(iter)
					if text == tc.expected.summaryContains {
						found = true
						break
					}
				}
				assert.True(t, found,
					"expected synthetic iteration with "+
						"text %q",
					tc.expected.summaryContains,
				)
			}

			// Verify token stats from model call
			if tc.expected.inputTokensStat > 0 {
				assert.Equal(t,
					tc.expected.inputTokensStat,
					execCtx.Stats().GetCounter(
						gent.SCInputTokens,
					),
					"input tokens stat",
				)
				assert.Equal(t,
					tc.expected.outputTokensStat,
					execCtx.Stats().GetCounter(
						gent.SCOutputTokens,
					),
					"output tokens stat",
				)
			}
		})
	}
}

// TestSummarization_PromptContent verifies that the prompt
// sent to the summarization model contains the correct
// content — existing summaries included, pinned iteration
// text excluded.
func TestSummarization_PromptContent(t *testing.T) {
	type input struct {
		keepRecent    int
		scratchpad    []*gent.Iteration
		modelResponse string
	}

	type expected struct {
		promptContains    []string
		promptNotContains []string
		pinnedInResult    []*gent.Iteration
	}

	existingSynthetic := &gent.Iteration{
		Messages: []*gent.MessageContent{
			{
				Role: llms.ChatMessageTypeGeneric,
				Parts: []gent.ContentPart{
					llms.TextContent{
						Text: "old summary text",
					},
				},
			},
		},
		Origin: gent.IterationCompactedSynthetic,
	}

	pinned := makePinnedIter("secret pinned data")

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "existing summary included in " +
				"prompt",
			input: input{
				keepRecent: 0,
				scratchpad: []*gent.Iteration{
					existingSynthetic,
					makeIter("new data"),
				},
				modelResponse: "updated summary",
			},
			expected: expected{
				promptContains: []string{
					"old summary text",
					"Existing Summary",
					"new data",
				},
				promptNotContains: nil,
				pinnedInResult:    nil,
			},
		},
		{
			name: "first compaction shows none as " +
				"existing summary",
			input: input{
				keepRecent: 0,
				scratchpad: []*gent.Iteration{
					makeIter("step 1"),
				},
				modelResponse: "summary",
			},
			expected: expected{
				promptContains: []string{
					"None (first compaction)",
					"step 1",
				},
				promptNotContains: nil,
				pinnedInResult:    nil,
			},
		},
		{
			name: "pinned iteration text excluded " +
				"from prompt",
			input: input{
				keepRecent: 0,
				scratchpad: []*gent.Iteration{
					makeIter("normal step"),
					pinned,
					makeIter("another step"),
				},
				modelResponse: "summary without pinned",
			},
			expected: expected{
				promptContains: []string{
					"normal step",
					"another step",
				},
				promptNotContains: []string{
					"secret pinned data",
				},
				pinnedInResult: []*gent.Iteration{
					pinned,
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			model := tt.NewMockModel()
			model.AddResponse(
				tc.input.modelResponse, 10, 5,
			)

			strategy := NewSummarization(model)
			if tc.input.keepRecent > 0 {
				strategy.WithKeepRecent(
					tc.input.keepRecent,
				)
			}

			data := gent.NewBasicLoopData(nil)
			data.SetScratchPad(tc.input.scratchpad)
			execCtx := gent.NewExecutionContext(
				context.Background(), "test", data,
			)
			execCtx.SetLimits(nil)

			err := strategy.Compact(execCtx)
			assert.NoError(t, err)

			// Verify prompt content
			assert.Len(t,
				model.CapturedMessages, 1,
				"model should be called once",
			)
			prompt := model.CapturedMessages[0][0].
				Parts[0].(llms.TextContent)

			for _, s := range tc.expected.promptContains {
				assert.Contains(t,
					prompt.Text, s,
					"prompt should contain %q", s,
				)
			}
			for _, s := range tc.expected.promptNotContains {
				assert.NotContains(t,
					prompt.Text, s,
					"prompt should not contain %q", s,
				)
			}

			// Verify pinned iterations in result
			result := data.GetScratchPad()
			for _, pin := range tc.expected.pinnedInResult {
				found := false
				for _, iter := range result {
					if iter == pin {
						found = true
						break
					}
				}
				assert.True(t, found,
					"pinned iteration should be "+
						"in result",
				)
			}
		})
	}
}
