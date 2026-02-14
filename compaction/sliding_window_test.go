package compaction

import (
	"context"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
	"github.com/tmc/langchaingo/llms"
)

// makeIter creates a simple Iteration with text content.
func makeIter(text string) *gent.Iteration {
	return &gent.Iteration{
		Messages: []*gent.MessageContent{
			{
				Role: llms.ChatMessageTypeAI,
				Parts: []gent.ContentPart{
					llms.TextContent{Text: text},
				},
			},
		},
	}
}

// makePinnedIter creates an Iteration with an importance
// score >= ImportanceScorePinned (i.e., it will be treated
// as pinned by the standard strategies).
func makePinnedIter(text string) *gent.Iteration {
	iter := makeIter(text)
	iter.SetMetadata(
		gent.IMKImportanceScore,
		gent.ImportanceScorePinned,
	)
	return iter
}

func TestSlidingWindow_Compact(t *testing.T) {
	type input struct {
		windowSize int
		scratchpad []*gent.Iteration
	}

	type expected struct {
		scratchpadLen  int
		keptIterations []*gent.Iteration
	}

	// Pre-create iterations so we can check pointer identity
	a := makeIter("a")
	b := makeIter("b")
	c := makeIter("c")
	d := makeIter("d")
	e := makeIter("e")
	pinAt2 := makePinnedIter("pinned-at-2")
	pinAt4 := makePinnedIter("pinned-at-4")
	zeroScore := makeIter("zero-score")
	zeroScore.SetMetadata(gent.IMKImportanceScore, 0.0)
	negScore := makeIter("neg-score")
	negScore.SetMetadata(gent.IMKImportanceScore, -2.0)
	noMeta := makeIter("no-metadata")
	almostPinned := makeIter("almost-pinned")
	almostPinned.SetMetadata(gent.IMKImportanceScore, 9.9)
	exactPinned := makeIter("exact-pinned")
	exactPinned.SetMetadata(
		gent.IMKImportanceScore,
		gent.ImportanceScorePinned,
	)

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "scratchpad within window no change",
			input: input{
				windowSize: 5,
				scratchpad: []*gent.Iteration{a, b, c},
			},
			expected: expected{
				scratchpadLen:  3,
				keptIterations: []*gent.Iteration{a, b, c},
			},
		},
		{
			name: "scratchpad exceeds window oldest " +
				"dropped",
			input: input{
				windowSize: 2,
				scratchpad: []*gent.Iteration{
					a, b, c, d, e,
				},
			},
			expected: expected{
				scratchpadLen:  2,
				keptIterations: []*gent.Iteration{d, e},
			},
		},
		{
			name: "all iterations pinned no change",
			input: input{
				windowSize: 1,
				scratchpad: []*gent.Iteration{
					pinAt2, pinAt4,
				},
			},
			expected: expected{
				scratchpadLen: 2,
				keptIterations: []*gent.Iteration{
					pinAt2, pinAt4,
				},
			},
		},
		{
			name: "mixed pinned and unpinned preserves " +
				"pinned as bonus",
			input: input{
				windowSize: 2,
				// [a, pinAt2, b, c, pinAt4, d, e]
				scratchpad: []*gent.Iteration{
					a, pinAt2, b, c, pinAt4, d, e,
				},
			},
			expected: expected{
				// pinned (2) + last 2 unpinned (d, e) = 4
				scratchpadLen: 4,
				keptIterations: []*gent.Iteration{
					pinAt2, pinAt4, d, e,
				},
			},
		},
		{
			name: "pinned iterations preserve relative " +
				"order",
			input: input{
				windowSize: 1,
				// [a, pinAt2, b, c]
				scratchpad: []*gent.Iteration{
					a, pinAt2, b, c,
				},
			},
			expected: expected{
				// pinAt2 before c in original order
				scratchpadLen: 2,
				keptIterations: []*gent.Iteration{
					pinAt2, c,
				},
			},
		},
		{
			name: "window size 1 keeps only most recent " +
				"unpinned",
			input: input{
				windowSize: 1,
				scratchpad: []*gent.Iteration{
					a, b, c, d, e,
				},
			},
			expected: expected{
				scratchpadLen:  1,
				keptIterations: []*gent.Iteration{e},
			},
		},
		{
			name: "importance score exactly 0 " +
				"NOT pinned",
			input: input{
				windowSize: 1,
				scratchpad: []*gent.Iteration{
					zeroScore, a, b,
				},
			},
			expected: expected{
				scratchpadLen:  1,
				keptIterations: []*gent.Iteration{b},
			},
		},
		{
			name: "negative importance score " +
				"NOT pinned",
			input: input{
				windowSize: 1,
				scratchpad: []*gent.Iteration{
					negScore, a, b,
				},
			},
			expected: expected{
				scratchpadLen:  1,
				keptIterations: []*gent.Iteration{b},
			},
		},
		{
			name: "nil metadata NOT pinned",
			input: input{
				windowSize: 1,
				scratchpad: []*gent.Iteration{
					noMeta, a, b,
				},
			},
			expected: expected{
				scratchpadLen:  1,
				keptIterations: []*gent.Iteration{b},
			},
		},
		{
			name: "score 9.9 NOT pinned " +
				"(below threshold)",
			input: input{
				windowSize: 1,
				scratchpad: []*gent.Iteration{
					almostPinned, a, b,
				},
			},
			expected: expected{
				scratchpadLen:  1,
				keptIterations: []*gent.Iteration{b},
			},
		},
		{
			name: "score exactly 10.0 IS pinned " +
				"(at threshold)",
			input: input{
				windowSize: 1,
				scratchpad: []*gent.Iteration{
					a, exactPinned, b,
				},
			},
			expected: expected{
				scratchpadLen: 2,
				keptIterations: []*gent.Iteration{
					exactPinned, b,
				},
			},
		},
		{
			name: "scratchpad exactly at window size " +
				"no change",
			input: input{
				windowSize: 3,
				scratchpad: []*gent.Iteration{a, b, c},
			},
			expected: expected{
				scratchpadLen:  3,
				keptIterations: []*gent.Iteration{a, b, c},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := gent.NewBasicLoopData(nil)
			data.SetScratchPad(tc.input.scratchpad)
			execCtx := gent.NewExecutionContext(
				context.Background(), "test", data,
			)
			execCtx.SetLimits(nil)

			strategy := NewSlidingWindow(
				tc.input.windowSize,
			)
			err := strategy.Compact(execCtx)

			assert.NoError(t, err)
			result := data.GetScratchPad()
			assert.Equal(t,
				tc.expected.scratchpadLen, len(result),
			)
			// Check pointer identity to ensure we kept
			// the right iterations
			assert.Equal(t,
				tc.expected.keptIterations, result,
			)
		})
	}
}

func TestSlidingWindow_PanicsOnInvalidWindowSize(t *testing.T) {
	tests := []struct {
		name       string
		windowSize int
	}{
		{name: "zero", windowSize: 0},
		{name: "negative", windowSize: -1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Panics(t, func() {
				NewSlidingWindow(tc.windowSize)
			})
		})
	}
}
