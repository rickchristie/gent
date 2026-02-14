package compaction

import (
	"fmt"
	"strings"

	"github.com/rickchristie/gent"
	"github.com/tmc/langchaingo/llms"
)

// SummarizationStrategy compacts the scratchpad by
// summarizing older iterations into a single synthetic
// iteration. Recent iterations are preserved untouched.
//
// This implements both "progressive summarization" and
// "summary buffer hybrid" patterns:
//   - KeepRecent = 0: pure progressive (summarize everything)
//   - KeepRecent > 0: hybrid (keep last N, summarize rest)
//
// Pinned iterations (importance score >=
// ImportanceScorePinned) are always preserved untouched,
// regardless of their position.
//
// # Multi-Modal Handling
//
// Non-text content parts (images, audio, etc.) in non-pinned
// iterations are dropped during summarization. Only text
// content is extracted and passed to the summarization model.
// If multi-modal content is important, pin the iteration.
//
// # Model Usage
//
// The strategy calls the injected Model to generate
// summaries. Token usage from these calls is tracked in the
// ExecutionContext's stats (via the model's standard event
// publishing). The summarization prompt can be customized.
//
// # Example
//
//	strategy := compaction.NewSummarization(model).
//	    WithKeepRecent(5)
type SummarizationStrategy struct {
	model      gent.Model
	keepRecent int
	prompt     string
}

// NewSummarization creates a SummarizationStrategy with the
// given model.
func NewSummarization(
	model gent.Model,
) *SummarizationStrategy {
	return &SummarizationStrategy{
		model:      model,
		keepRecent: 0,
		prompt:     DefaultSummarizationPrompt,
	}
}

// WithKeepRecent sets the number of recent iterations to
// preserve without summarization. Default is 0 (pure
// progressive summarization).
func (s *SummarizationStrategy) WithKeepRecent(
	n int,
) *SummarizationStrategy {
	s.keepRecent = n
	return s
}

// WithPrompt sets a custom summarization prompt.
// The prompt receives the existing summary (if any) and the
// new messages to summarize via fmt.Sprintf with two %s
// placeholders.
func (s *SummarizationStrategy) WithPrompt(
	prompt string,
) *SummarizationStrategy {
	s.prompt = prompt
	return s
}

// DefaultSummarizationPrompt is the default prompt used for
// summarization.
const DefaultSummarizationPrompt = `You are a conversation ` +
	`summarizer. Your job is to produce a concise summary ` +
	`that preserves all important information for an AI ` +
	`agent to continue its work.

%s

## New Messages to Incorporate

%s

## Instructions

Produce an updated summary that:
1. Preserves key decisions, findings, and action items
2. Retains specific values (numbers, names, paths, etc.)
3. Notes any errors encountered and their resolutions
4. Keeps tool call results that may be referenced later
5. Is concise but does not lose critical context

Write ONLY the summary, no preamble.`

// Compact implements gent.CompactionStrategy.
func (s *SummarizationStrategy) Compact(
	execCtx *gent.ExecutionContext,
) error {
	scratchpad := execCtx.Data().GetScratchPad()

	// Separate: pinned, existing summary, summarizable,
	// recent
	var (
		pinned          []*gent.Iteration
		existingSummary *gent.Iteration
		nonPinned       []*gent.Iteration
	)

	// First pass: extract pinned and existing summary
	for _, iter := range scratchpad {
		if isPinned(iter) {
			pinned = append(pinned, iter)
			continue
		}
		if iter.Origin ==
			gent.IterationCompactedSynthetic {
			existingSummary = iter
			continue
		}
		nonPinned = append(nonPinned, iter)
	}

	// Split non-pinned into toSummarize and toKeep
	var toSummarize []*gent.Iteration
	var toKeep []*gent.Iteration

	if s.keepRecent > 0 &&
		len(nonPinned) > s.keepRecent {
		splitIdx := len(nonPinned) - s.keepRecent
		toSummarize = nonPinned[:splitIdx]
		toKeep = nonPinned[splitIdx:]
	} else if s.keepRecent == 0 && len(nonPinned) > 0 {
		toSummarize = nonPinned
	} else {
		// Nothing to summarize
		return nil
	}

	if len(toSummarize) == 0 {
		return nil
	}

	// Build summarization input
	existingText := ""
	if existingSummary != nil {
		existingText = "## Existing Summary\n\n" +
			extractText(existingSummary)
	} else {
		existingText = "## Existing Summary\n\n" +
			"None (first compaction)."
	}

	newMessages := extractTextFromIterations(toSummarize)
	fullPrompt := fmt.Sprintf(
		s.prompt, existingText, newMessages,
	)

	// Call model for summarization
	messages := []llms.MessageContent{
		{
			Role: llms.ChatMessageTypeHuman,
			Parts: []llms.ContentPart{
				llms.TextContent{Text: fullPrompt},
			},
		},
	}
	streamID := fmt.Sprintf(
		"compaction-summarization-%d",
		execCtx.Iteration(),
	)
	response, err := s.model.GenerateContent(
		execCtx,
		streamID,
		"compaction",
		messages,
	)
	if err != nil {
		return fmt.Errorf(
			"summarization model call: %w", err,
		)
	}

	summaryText := response.Choices[0].Content

	// Create synthetic summary iteration
	synthetic := &gent.Iteration{
		Messages: []*gent.MessageContent{
			{
				Role: llms.ChatMessageTypeGeneric,
				Parts: []gent.ContentPart{
					llms.TextContent{
						Text: summaryText,
					},
				},
			},
		},
		Origin: gent.IterationCompactedSynthetic,
	}

	// Rebuild scratchpad: synthetic + pinned + toKeep
	result := make(
		[]*gent.Iteration,
		0,
		1+len(pinned)+len(toKeep),
	)
	result = append(result, synthetic)
	result = append(result, pinned...)
	result = append(result, toKeep...)

	execCtx.Data().SetScratchPad(result)
	return nil
}

// extractText extracts all text content from an iteration,
// dropping non-text content parts.
func extractText(iter *gent.Iteration) string {
	var parts []string
	for _, msg := range iter.Messages {
		for _, part := range msg.Parts {
			if tc, ok := part.(llms.TextContent); ok {
				parts = append(parts, tc.Text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

// extractTextFromIterations extracts text from multiple
// iterations, formatting them with iteration markers.
func extractTextFromIterations(
	iterations []*gent.Iteration,
) string {
	var sb strings.Builder
	for i, iter := range iterations {
		fmt.Fprintf(&sb, "### Message %d\n\n", i+1)
		sb.WriteString(extractText(iter))
		sb.WriteString("\n\n")
	}
	return sb.String()
}

// Compile-time check.
var _ gent.CompactionStrategy = (*SummarizationStrategy)(nil)
