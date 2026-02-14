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
		keepRecent    int
		customPrompt  string
		scratchpad    []*gent.Iteration
		modelResponse string
		modelError    error
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
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			model := tt.NewMockModel()
			if tc.input.modelError != nil {
				model.AddError(tc.input.modelError)
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

// TestSummarization_ExistingSummaryIncludedInPrompt
// verifies that when an existing synthetic summary exists,
// its text is passed to the model as "Existing Summary".
func TestSummarization_ExistingSummaryIncludedInPrompt(
	t *testing.T,
) {
	var capturedMessages []llms.MessageContent

	model := &promptCapturingModel{
		response: &gent.ContentResponse{
			Choices: []*gent.ContentChoice{
				{Content: "updated summary"},
			},
			Info: &gent.GenerationInfo{
				InputTokens:  10,
				OutputTokens: 5,
			},
		},
		capture: func(msgs []llms.MessageContent) {
			capturedMessages = msgs
		},
	}

	existing := &gent.Iteration{
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

	data := gent.NewBasicLoopData(nil)
	data.SetScratchPad([]*gent.Iteration{
		existing,
		makeIter("new data"),
	})
	execCtx := gent.NewExecutionContext(
		context.Background(), "test", data,
	)
	execCtx.SetLimits(nil)

	strategy := NewSummarization(model)
	err := strategy.Compact(execCtx)
	assert.NoError(t, err)

	// Verify the prompt included the existing summary
	assert.Len(t, capturedMessages, 1)
	prompt := capturedMessages[0].Parts[0].(llms.TextContent)
	assert.Contains(t, prompt.Text, "old summary text")
	assert.Contains(t, prompt.Text, "Existing Summary")
}

// promptCapturingModel captures messages sent to
// GenerateContent for prompt verification.
type promptCapturingModel struct {
	response *gent.ContentResponse
	capture  func([]llms.MessageContent)
}

func (m *promptCapturingModel) GenerateContent(
	execCtx *gent.ExecutionContext,
	streamID string,
	streamTopicID string,
	messages []llms.MessageContent,
	opts ...llms.CallOption,
) (*gent.ContentResponse, error) {
	if execCtx != nil {
		execCtx.PublishBeforeModelCall(
			"test-model", messages,
		)
	}
	m.capture(messages)
	if execCtx != nil {
		execCtx.PublishAfterModelCall(
			"test-model", messages,
			m.response, 0, nil,
		)
	}
	return m.response, nil
}

// TestSummarization_PinnedNotInSummarizationInput
// verifies that pinned iterations are not included in the
// text sent to the summarization model.
func TestSummarization_PinnedNotInSummarizationInput(
	t *testing.T,
) {
	var capturedMessages []llms.MessageContent

	model := &promptCapturingModel{
		response: &gent.ContentResponse{
			Choices: []*gent.ContentChoice{
				{Content: "summary without pinned"},
			},
			Info: &gent.GenerationInfo{
				InputTokens:  10,
				OutputTokens: 5,
			},
		},
		capture: func(msgs []llms.MessageContent) {
			capturedMessages = msgs
		},
	}

	pinned := makePinnedIter("secret pinned data")

	data := gent.NewBasicLoopData(nil)
	data.SetScratchPad([]*gent.Iteration{
		makeIter("normal step"),
		pinned,
		makeIter("another step"),
	})
	execCtx := gent.NewExecutionContext(
		context.Background(), "test", data,
	)
	execCtx.SetLimits(nil)

	strategy := NewSummarization(model)
	err := strategy.Compact(execCtx)
	assert.NoError(t, err)

	// Verify pinned text NOT in prompt
	prompt := capturedMessages[0].Parts[0].(llms.TextContent)
	assert.NotContains(t, prompt.Text, "secret pinned data")
	// But normal steps are
	assert.Contains(t, prompt.Text, "normal step")
	assert.Contains(t, prompt.Text, "another step")

	// Verify pinned iteration is in result
	result := data.GetScratchPad()
	foundPinned := false
	for _, iter := range result {
		if iter == pinned {
			foundPinned = true
			break
		}
	}
	assert.True(t, foundPinned,
		"pinned iteration should be in result")
}
