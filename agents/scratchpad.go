package agents

import (
	"strings"

	"github.com/rickchristie/gent"
	"github.com/tmc/langchaingo/llms"
)

// ScratchpadToMessages converts scratchpad iterations into
// LLM messages, applying iteration expiry filtering and tool
// call result deduplication.
//
// For each iteration in the scratchpad:
//   - Expired iterations (ExpireAfterIteration > 0 and
//     currentIteration >= ExpireAfterIteration) are skipped.
//   - Observation messages with [gent.MMKToolChainResult] metadata
//     are checked for duplicate stateless tool calls. When a
//     tool call with the same Hash appears in a later
//     iteration, the earlier occurrence's text is replaced
//     with an abbreviated summary from
//     [gent.ToolChain.DeduplicateSummary]. The most recent
//     occurrence always keeps its full text.
//   - Messages without metadata are passed through as-is.
//
// The toolChain parameter is used to call DeduplicateSummary
// for tool result deduplication. Pass nil to disable
// deduplication (only expiry filtering is applied).
//
// The format parameter is used to rebuild observation text
// when some tool calls in an iteration are deduplicated.
// Required when toolChain is non-nil.
//
// Returns the number of non-expired iterations that were
// included, which callers can use to decide between
// "BEGIN!" vs "CONTINUE!" prompts.
func ScratchpadToMessages(
	scratchpad []*gent.Iteration,
	currentIteration int,
	toolChain gent.ToolChain,
	format gent.TextFormat,
) ([]llms.MessageContent, int) {
	// First pass: collect the last occurrence index of each
	// tool call hash. Only stateless tools (non-empty dedup
	// summary) participate.
	lastOccurrence := buildLastOccurrenceIndex(
		scratchpad, currentIteration, toolChain,
	)

	// Second pass: build messages, replacing earlier
	// duplicate observations with summaries.
	var messages []llms.MessageContent
	included := 0
	globalIdx := 0

	for _, iter := range scratchpad {
		if iter.ExpireAfterIteration > 0 &&
			currentIteration >= iter.ExpireAfterIteration {
			continue
		}
		included++

		for _, msg := range iter.Messages {
			converted := convertMessage(
				msg, globalIdx, lastOccurrence, format,
			)
			messages = append(messages, converted)
		}
		globalIdx++
	}

	return messages, included
}

// lastOccurrenceEntry tracks the last non-expired iteration
// index where a given tool call hash appears.
type lastOccurrenceEntry struct {
	iterIndex int
	summary   string
}

// buildLastOccurrenceIndex walks the scratchpad and records
// the last non-expired iteration index for each deduplicatable
// tool call hash. Only tool calls where
// toolChain.DeduplicateSummary returns non-empty are tracked.
func buildLastOccurrenceIndex(
	scratchpad []*gent.Iteration,
	currentIteration int,
	toolChain gent.ToolChain,
) map[string]*lastOccurrenceEntry {
	if toolChain == nil {
		return nil
	}

	index := make(map[string]*lastOccurrenceEntry)
	iterIdx := 0

	for _, iter := range scratchpad {
		if iter.ExpireAfterIteration > 0 &&
			currentIteration >= iter.ExpireAfterIteration {
			continue
		}

		for _, msg := range iter.Messages {
			tcr := getToolChainResult(msg)
			if tcr == nil {
				continue
			}
			for _, result := range tcr.Results {
				if result.Hash == "" ||
					result.Error != nil {
					continue
				}
				summary := toolChain.DeduplicateSummary(
					result,
				)
				if summary == "" {
					continue
				}
				index[result.Hash] = &lastOccurrenceEntry{
					iterIndex: iterIdx,
					summary:   summary,
				}
			}
		}
		iterIdx++
	}

	return index
}

// convertMessage converts a single MessageContent to an
// llms.MessageContent, applying deduplication where needed.
func convertMessage(
	msg *gent.MessageContent,
	iterIndex int,
	lastOccurrence map[string]*lastOccurrenceEntry,
	format gent.TextFormat,
) llms.MessageContent {
	tcr := getToolChainResult(msg)
	if tcr == nil || lastOccurrence == nil {
		return llms.MessageContent{
			Role:  msg.Role,
			Parts: ToLLMParts(msg.Parts),
		}
	}

	// Check if any result in this message needs dedup
	needsRebuild := false
	for _, result := range tcr.Results {
		if result.Hash == "" || result.Error != nil {
			continue
		}
		entry, ok := lastOccurrence[result.Hash]
		if ok && entry.iterIndex > iterIndex {
			needsRebuild = true
			break
		}
	}

	if !needsRebuild {
		return llms.MessageContent{
			Role:  msg.Role,
			Parts: ToLLMParts(msg.Parts),
		}
	}

	// Rebuild observation with some results abbreviated
	return rebuildObservation(
		msg, iterIndex, lastOccurrence, format,
	)
}

// rebuildObservation reconstructs the observation message,
// replacing deduplicated tool call results with their
// summaries while keeping non-duplicate results intact.
func rebuildObservation(
	msg *gent.MessageContent,
	iterIndex int,
	lastOccurrence map[string]*lastOccurrenceEntry,
	format gent.TextFormat,
) llms.MessageContent {
	tcr := getToolChainResult(msg)
	var sections []string
	var allMedia []gent.ContentPart

	for _, result := range tcr.Results {
		entry, isDuplicate := lastOccurrence[result.Hash]
		if result.Hash != "" &&
			result.Error == nil &&
			isDuplicate &&
			entry.iterIndex > iterIndex {
			// Replace with summary
			sections = append(sections, format.FormatSections(
				[]gent.FormattedSection{
					{Name: result.Name, Content: entry.summary},
				},
			))
		} else {
			// Keep original
			if result.Text != "" {
				sections = append(sections, result.Text)
			}
			allMedia = append(allMedia, result.Media...)
		}
	}

	observation := ""
	if len(sections) > 0 {
		observation = format.FormatSections(
			[]gent.FormattedSection{
				{
					Name:    "observation",
					Content: strings.Join(sections, "\n"),
				},
			},
		)
	}

	var parts []gent.ContentPart
	if observation != "" {
		parts = append(
			parts,
			llms.TextContent{Text: observation},
		)
	}
	parts = append(parts, allMedia...)

	return llms.MessageContent{
		Role:  msg.Role,
		Parts: ToLLMParts(parts),
	}
}

// getToolChainResult extracts the ToolChainResult from a
// MessageContent's metadata, if present.
func getToolChainResult(
	msg *gent.MessageContent,
) *gent.ToolChainResult {
	val, ok := msg.GetMetadata(gent.MMKToolChainResult)
	if !ok {
		return nil
	}
	tcr, ok := val.(*gent.ToolChainResult)
	if !ok {
		return nil
	}
	return tcr
}

// ToLLMParts converts gent.ContentPart slice to
// llms.ContentPart slice.
func ToLLMParts(parts []gent.ContentPart) []llms.ContentPart {
	result := make([]llms.ContentPart, len(parts))
	for i, p := range parts {
		result[i] = p
	}
	return result
}
