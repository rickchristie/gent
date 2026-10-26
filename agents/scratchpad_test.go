package agents_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents"
	"github.com/stretchr/testify/assert"
)

// -----------------------------------------------------------------------------
// Tool observations in history
// -----------------------------------------------------------------------------

// testXMLFormat is a minimal TextFormat that wraps content in XML
// tags, matching format.XML behavior without importing the package.
type testXMLFormat struct{}

func (f *testXMLFormat) RegisterSection(
	section gent.TextSection,
) gent.TextFormat {
	return f
}

func (f *testXMLFormat) DescribeStructure() string {
	return ""
}

func (f *testXMLFormat) Parse(
	execCtx *gent.ExecutionContext,
	output string,
) (map[string][]string, error) {
	return nil, nil
}

func (f *testXMLFormat) FormatSections(
	sections []gent.FormattedSection,
) string {
	result := ""
	for i, s := range sections {
		if i > 0 {
			result += "\n"
		}
		result += fmt.Sprintf("<%s>\n%s\n</%s>", s.Name, s.Content, s.Name)
	}
	return result
}

// mockToolChain supplies captured summaries for the history rendering tests.
type mockToolChain struct {
	// summaries maps tool name to a summary string.
	// If a tool name is present with non-empty value, that
	// value is returned by DeduplicateSummary.
	summaries map[string]string
}

func (m *mockToolChain) DeduplicateSummary(
	result *gent.ToolCallResult,
) string {
	if m.summaries == nil {
		return ""
	}
	return m.summaries[result.Name]
}

// helper to build a full iteration (AI message + observation)
// from a ToolChainResult. The AI message is a simple text part.
func buildIteration(
	aiText string,
	tcr *gent.ToolChainResult,
	xmlFmt gent.TextFormat,
) *gent.HistoryEntry {
	var text []string
	var media []gent.ContentPart
	for _, result := range tcr.Results {
		if result.Text != "" {
			text = append(text, result.Text)
		}
		media = append(media, result.Media...)
	}
	observation := &gent.MessageContent{Role: gent.ChatMessageTypeHuman}
	if len(text) > 0 {
		observation.Parts = append(observation.Parts, gent.TextContent{
			Text: xmlFmt.FormatSections([]gent.FormattedSection{
				{Name: "observation", Content: strings.Join(text, "\n")},
			}),
		})
	}
	observation.Parts = append(observation.Parts, media...)
	observation.SetMetadata(gent.MMKToolChainResult, tcr)
	obsIter := &gent.HistoryEntry{Messages: []*gent.MessageContent{observation}}
	aiMsg := &gent.MessageContent{
		Role:  gent.ChatMessageTypeAI,
		Parts: []gent.ContentPart{gent.TextContent{Text: aiText}},
	}
	return &gent.HistoryEntry{
		Messages: append(
			[]*gent.MessageContent{aiMsg}, obsIter.Messages...,
		),
	}
}

// helper to build a plain iteration without metadata (passthrough).
func buildPlainIteration(
	aiText string,
	humanText string,
) *gent.HistoryEntry {
	return &gent.HistoryEntry{
		Messages: []*gent.MessageContent{
			{
				Role: gent.ChatMessageTypeAI,
				Parts: []gent.ContentPart{
					gent.TextContent{Text: aiText},
				},
			},
			{
				Role: gent.ChatMessageTypeHuman,
				Parts: []gent.ContentPart{
					gent.TextContent{Text: humanText},
				},
			},
		},
	}
}

func TestScratchpadToMessages(t *testing.T) {
	xmlFmt := &testXMLFormat{}

	type input struct {
		scratchpad       []*gent.HistoryEntry
		currentIteration int
		toolChain        interface {
			DeduplicateSummary(*gent.ToolCallResult) string
		}
	}

	type expected struct {
		messageTexts []string
		messageRoles []gent.ChatMessageType
		included     int
	}

	// Helper: extract text content from messages for comparison.
	extractTexts := func(
		msgs []gent.ModelMessage,
	) []string {
		var texts []string
		for _, msg := range msgs {
			for _, part := range msg.Parts {
				if tp, ok := part.(gent.TextContent); ok {
					texts = append(texts, tp.Text)
				}
			}
		}
		return texts
	}

	extractRoles := func(
		msgs []gent.ModelMessage,
	) []gent.ChatMessageType {
		var roles []gent.ChatMessageType
		for _, msg := range msgs {
			roles = append(roles, msg.Role)
		}
		return roles
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "empty scratchpad returns empty messages",
			input: input{
				scratchpad:       nil,
				currentIteration: 1,
				toolChain:        nil,
			},
			expected: expected{
				messageTexts: nil,
				messageRoles: nil,
				included:     0,
			},
		},
		{
			name: "no metadata passthrough as-is",
			input: input{
				scratchpad: []*gent.HistoryEntry{
					buildPlainIteration(
						"I will search", "Search result",
					),
				},
				currentIteration: 1,
				toolChain:        nil,
			},
			expected: expected{
				messageTexts: []string{
					"I will search",
					"Search result",
				},
				messageRoles: []gent.ChatMessageType{
					gent.ChatMessageTypeAI,
					gent.ChatMessageTypeHuman,
				},
				included: 1,
			},
		},
		{
			name: "expired iterations skipped",
			input: input{
				scratchpad: []*gent.HistoryEntry{
					{
						Messages: []*gent.MessageContent{
							{
								Role: gent.ChatMessageTypeAI,
								Parts: []gent.ContentPart{
									gent.TextContent{
										Text: "expired msg",
									},
								},
							},
						},
						ExpireAfterIteration: 2,
					},
					buildPlainIteration("kept", "reply"),
				},
				currentIteration: 3,
				toolChain:        nil,
			},
			expected: expected{
				messageTexts: []string{"kept", "reply"},
				messageRoles: []gent.ChatMessageType{
					gent.ChatMessageTypeAI,
					gent.ChatMessageTypeHuman,
				},
				included: 1,
			},
		},
		{
			name: "non-expired iterations included",
			input: input{
				scratchpad: []*gent.HistoryEntry{
					{
						Messages: []*gent.MessageContent{
							{
								Role: gent.ChatMessageTypeAI,
								Parts: []gent.ContentPart{
									gent.TextContent{
										Text: "not expired",
									},
								},
							},
						},
						ExpireAfterIteration: 5,
					},
					buildPlainIteration("also kept", "reply2"),
				},
				currentIteration: 3,
				toolChain:        nil,
			},
			expected: expected{
				messageTexts: []string{
					"not expired", "also kept", "reply2",
				},
				messageRoles: []gent.ChatMessageType{
					gent.ChatMessageTypeAI,
					gent.ChatMessageTypeAI,
					gent.ChatMessageTypeHuman,
				},
				included: 2,
			},
		},
		{
			name: "single dedup: same tool+args in iter 1 and 2",
			input: input{
				scratchpad: []*gent.HistoryEntry{
					buildIteration(
						"Searching...",
						&gent.ToolChainResult{
							Results: []*gent.ToolCallResult{
								{
									Name: "search",
									Hash: "search:q=weather",
									Text: "<search>\nSunny, 72F\n</search>",
								},
							},
						},
						xmlFmt,
					),
					buildIteration(
						"Searching again...",
						&gent.ToolChainResult{
							Results: []*gent.ToolCallResult{
								{
									Name: "search",
									Hash: "search:q=weather",
									Text: "<search>\nSunny, 72F\n</search>",
								},
							},
						},
						xmlFmt,
					),
				},
				currentIteration: 2,
				toolChain: &mockToolChain{
					summaries: map[string]string{
						"search": "(search result cached)",
					},
				},
			},
			expected: expected{
				messageTexts: []string{
					"Searching...",
					// iter 0 observation gets deduplicated
					`<observation>
<search>
(search result cached)
</search>
</observation>`,
					"Searching again...",
					// iter 1 observation keeps full text (last occurrence)
					`<observation>
<search>
Sunny, 72F
</search>
</observation>`,
				},
				messageRoles: []gent.ChatMessageType{
					gent.ChatMessageTypeAI,
					gent.ChatMessageTypeHuman,
					gent.ChatMessageTypeAI,
					gent.ChatMessageTypeHuman,
				},
				included: 2,
			},
		},
		{
			name: "multiple dedup: iters 1,2,3 same tool+args",
			input: input{
				scratchpad: []*gent.HistoryEntry{
					buildIteration(
						"Iter1",
						&gent.ToolChainResult{
							Results: []*gent.ToolCallResult{
								{
									Name: "fetch",
									Hash: "fetch:url=example.com",
									Text: "<fetch>\nPage content\n</fetch>",
								},
							},
						},
						xmlFmt,
					),
					buildIteration(
						"Iter2",
						&gent.ToolChainResult{
							Results: []*gent.ToolCallResult{
								{
									Name: "fetch",
									Hash: "fetch:url=example.com",
									Text: "<fetch>\nPage content\n</fetch>",
								},
							},
						},
						xmlFmt,
					),
					buildIteration(
						"Iter3",
						&gent.ToolChainResult{
							Results: []*gent.ToolCallResult{
								{
									Name: "fetch",
									Hash: "fetch:url=example.com",
									Text: "<fetch>\nPage content\n</fetch>",
								},
							},
						},
						xmlFmt,
					),
				},
				currentIteration: 3,
				toolChain: &mockToolChain{
					summaries: map[string]string{
						"fetch": "(cached fetch)",
					},
				},
			},
			expected: expected{
				messageTexts: []string{
					"Iter1",
					`<observation>
<fetch>
(cached fetch)
</fetch>
</observation>`,
					"Iter2",
					`<observation>
<fetch>
(cached fetch)
</fetch>
</observation>`,
					"Iter3",
					// last occurrence keeps full text
					`<observation>
<fetch>
Page content
</fetch>
</observation>`,
				},
				messageRoles: []gent.ChatMessageType{
					gent.ChatMessageTypeAI,
					gent.ChatMessageTypeHuman,
					gent.ChatMessageTypeAI,
					gent.ChatMessageTypeHuman,
					gent.ChatMessageTypeAI,
					gent.ChatMessageTypeHuman,
				},
				included: 3,
			},
		},
		{
			name: "mixed observation: one deduplicatable, one not",
			input: input{
				scratchpad: []*gent.HistoryEntry{
					buildIteration(
						"Iter1",
						&gent.ToolChainResult{
							Results: []*gent.ToolCallResult{
								{
									Name: "search",
									Hash: "search:q=weather",
									Text: "<search>\nSunny\n</search>",
								},
								{
									Name: "unique_tool",
									Hash: "unique_tool:x=1",
									Text: "<unique_tool>\nUnique result\n</unique_tool>",
								},
							},
						},
						xmlFmt,
					),
					buildIteration(
						"Iter2",
						&gent.ToolChainResult{
							Results: []*gent.ToolCallResult{
								{
									Name: "search",
									Hash: "search:q=weather",
									Text: "<search>\nSunny\n</search>",
								},
							},
						},
						xmlFmt,
					),
				},
				currentIteration: 2,
				toolChain: &mockToolChain{
					summaries: map[string]string{
						"search": "(cached search)",
					},
				},
			},
			expected: expected{
				messageTexts: []string{
					"Iter1",
					// iter 0: search deduped, unique_tool kept
					`<observation>
<search>
(cached search)
</search>
<unique_tool>
Unique result
</unique_tool>
</observation>`,
					"Iter2",
					// iter 1: search is last occurrence, kept
					`<observation>
<search>
Sunny
</search>
</observation>`,
				},
				messageRoles: []gent.ChatMessageType{
					gent.ChatMessageTypeAI,
					gent.ChatMessageTypeHuman,
					gent.ChatMessageTypeAI,
					gent.ChatMessageTypeHuman,
				},
				included: 2,
			},
		},
		{
			name: "errored calls not deduped",
			input: input{
				scratchpad: []*gent.HistoryEntry{
					buildIteration(
						"Iter1",
						&gent.ToolChainResult{
							Results: []*gent.ToolCallResult{
								{
									Name:  "search",
									Hash:  "search:q=weather",
									Error: fmt.Errorf("timeout"),
									Text:  "<search>\nError: timeout\n</search>",
								},
							},
						},
						xmlFmt,
					),
					buildIteration(
						"Iter2",
						&gent.ToolChainResult{
							Results: []*gent.ToolCallResult{
								{
									Name: "search",
									Hash: "search:q=weather",
									Text: "<search>\nSunny\n</search>",
								},
							},
						},
						xmlFmt,
					),
				},
				currentIteration: 2,
				toolChain: &mockToolChain{
					summaries: map[string]string{
						"search": "(cached search)",
					},
				},
			},
			expected: expected{
				messageTexts: []string{
					"Iter1",
					// errored call keeps full text, not deduped
					`<observation>
<search>
Error: timeout
</search>
</observation>`,
					"Iter2",
					// only one non-error occurrence, so it's the last — kept
					`<observation>
<search>
Sunny
</search>
</observation>`,
				},
				messageRoles: []gent.ChatMessageType{
					gent.ChatMessageTypeAI,
					gent.ChatMessageTypeHuman,
					gent.ChatMessageTypeAI,
					gent.ChatMessageTypeHuman,
				},
				included: 2,
			},
		},
		{
			name: "tool returns empty DeduplicateSummary: no dedup",
			input: input{
				scratchpad: []*gent.HistoryEntry{
					buildIteration(
						"Iter1",
						&gent.ToolChainResult{
							Results: []*gent.ToolCallResult{
								{
									Name: "stateful_tool",
									Hash: "stateful_tool:x=1",
									Text: "<stateful_tool>\nResult A\n</stateful_tool>",
								},
							},
						},
						xmlFmt,
					),
					buildIteration(
						"Iter2",
						&gent.ToolChainResult{
							Results: []*gent.ToolCallResult{
								{
									Name: "stateful_tool",
									Hash: "stateful_tool:x=1",
									Text: "<stateful_tool>\nResult B\n</stateful_tool>",
								},
							},
						},
						xmlFmt,
					),
				},
				currentIteration: 2,
				// summaries is empty for stateful_tool -> returns ""
				toolChain: &mockToolChain{
					summaries: map[string]string{},
				},
			},
			expected: expected{
				messageTexts: []string{
					"Iter1",
					`<observation>
<stateful_tool>
Result A
</stateful_tool>
</observation>`,
					"Iter2",
					`<observation>
<stateful_tool>
Result B
</stateful_tool>
</observation>`,
				},
				messageRoles: []gent.ChatMessageType{
					gent.ChatMessageTypeAI,
					gent.ChatMessageTypeHuman,
					gent.ChatMessageTypeAI,
					gent.ChatMessageTypeHuman,
				},
				included: 2,
			},
		},
		{
			name: "nil toolChain disables dedup, only expiry",
			input: input{
				scratchpad: []*gent.HistoryEntry{
					buildIteration(
						"Iter1",
						&gent.ToolChainResult{
							Results: []*gent.ToolCallResult{
								{
									Name: "search",
									Hash: "search:q=weather",
									Text: "<search>\nSunny\n</search>",
								},
							},
						},
						xmlFmt,
					),
					buildIteration(
						"Iter2",
						&gent.ToolChainResult{
							Results: []*gent.ToolCallResult{
								{
									Name: "search",
									Hash: "search:q=weather",
									Text: "<search>\nSunny\n</search>",
								},
							},
						},
						xmlFmt,
					),
				},
				currentIteration: 2,
				toolChain:        nil,
			},
			expected: expected{
				messageTexts: []string{
					"Iter1",
					// no dedup, kept as-is
					`<observation>
<search>
Sunny
</search>
</observation>`,
					"Iter2",
					`<observation>
<search>
Sunny
</search>
</observation>`,
				},
				messageRoles: []gent.ChatMessageType{
					gent.ChatMessageTypeAI,
					gent.ChatMessageTypeHuman,
					gent.ChatMessageTypeAI,
					gent.ChatMessageTypeHuman,
				},
				included: 2,
			},
		},
		{
			name: "last occurrence keeps full text",
			input: input{
				scratchpad: []*gent.HistoryEntry{
					buildIteration(
						"First",
						&gent.ToolChainResult{
							Results: []*gent.ToolCallResult{
								{
									Name: "read",
									Hash: "read:file=main.go",
									Text: "<read>\nfunc main() {}\n</read>",
								},
							},
						},
						xmlFmt,
					),
					buildIteration(
						"Second",
						&gent.ToolChainResult{
							Results: []*gent.ToolCallResult{
								{
									Name: "read",
									Hash: "read:file=main.go",
									Text: "<read>\nfunc main() {}\n</read>",
								},
							},
						},
						xmlFmt,
					),
					buildIteration(
						"Third",
						&gent.ToolChainResult{
							Results: []*gent.ToolCallResult{
								{
									Name: "read",
									Hash: "read:file=main.go",
									Text: "<read>\nfunc main() {}\n</read>",
								},
							},
						},
						xmlFmt,
					),
				},
				currentIteration: 3,
				toolChain: &mockToolChain{
					summaries: map[string]string{
						"read": "(file cached)",
					},
				},
			},
			expected: expected{
				messageTexts: []string{
					"First",
					`<observation>
<read>
(file cached)
</read>
</observation>`,
					"Second",
					`<observation>
<read>
(file cached)
</read>
</observation>`,
					"Third",
					// last occurrence, full text preserved
					`<observation>
<read>
func main() {}
</read>
</observation>`,
				},
				messageRoles: []gent.ChatMessageType{
					gent.ChatMessageTypeAI,
					gent.ChatMessageTypeHuman,
					gent.ChatMessageTypeAI,
					gent.ChatMessageTypeHuman,
					gent.ChatMessageTypeAI,
					gent.ChatMessageTypeHuman,
				},
				included: 3,
			},
		},
		{
			name: "different args produce different hash, not deduped",
			input: input{
				scratchpad: []*gent.HistoryEntry{
					buildIteration(
						"Iter1",
						&gent.ToolChainResult{
							Results: []*gent.ToolCallResult{
								{
									Name: "search",
									Hash: "search:q=weather",
									Text: "<search>\nSunny\n</search>",
								},
							},
						},
						xmlFmt,
					),
					buildIteration(
						"Iter2",
						&gent.ToolChainResult{
							Results: []*gent.ToolCallResult{
								{
									Name: "search",
									Hash: "search:q=news",
									Text: "<search>\nHeadlines\n</search>",
								},
							},
						},
						xmlFmt,
					),
				},
				currentIteration: 2,
				toolChain: &mockToolChain{
					summaries: map[string]string{
						"search": "(cached search)",
					},
				},
			},
			expected: expected{
				messageTexts: []string{
					"Iter1",
					// different hash, no dedup
					`<observation>
<search>
Sunny
</search>
</observation>`,
					"Iter2",
					`<observation>
<search>
Headlines
</search>
</observation>`,
				},
				messageRoles: []gent.ChatMessageType{
					gent.ChatMessageTypeAI,
					gent.ChatMessageTypeHuman,
					gent.ChatMessageTypeAI,
					gent.ChatMessageTypeHuman,
				},
				included: 2,
			},
		},
		{
			name: "included count correct with mixed expiry",
			input: input{
				scratchpad: []*gent.HistoryEntry{
					func() *gent.HistoryEntry {
						iter := buildPlainIteration("a", "b")
						iter.ExpireAfterIteration = 2
						return iter
					}(),
					buildPlainIteration("c", "d"),
					func() *gent.HistoryEntry {
						iter := buildPlainIteration("e", "f")
						iter.ExpireAfterIteration = 3
						return iter
					}(),
					buildPlainIteration("g", "h"),
					func() *gent.HistoryEntry {
						iter := buildPlainIteration("i", "j")
						iter.ExpireAfterIteration = 10
						return iter
					}(),
				},
				currentIteration: 5,
				toolChain:        nil,
			},
			expected: expected{
				messageTexts: []string{
					// iter 0 expired (ExpireAfterIteration=2, current=5)
					// iter 1 kept
					"c", "d",
					// iter 2 expired (ExpireAfterIteration=3, current=5)
					// iter 3 kept
					"g", "h",
					// iter 4 kept (ExpireAfterIteration=10, current=5)
					"i", "j",
				},
				messageRoles: []gent.ChatMessageType{
					gent.ChatMessageTypeAI,
					gent.ChatMessageTypeHuman,
					gent.ChatMessageTypeAI,
					gent.ChatMessageTypeHuman,
					gent.ChatMessageTypeAI,
					gent.ChatMessageTypeHuman,
				},
				included: 3,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msgs, included := agents.ScratchpadToMessages(
				tc.input.scratchpad,
				tc.input.currentIteration,
				tc.input.toolChain,
				xmlFmt,
			)

			assert.Equal(t, tc.expected.included, included)

			actualTexts := extractTexts(msgs)
			assert.Equal(t, tc.expected.messageTexts, actualTexts)

			actualRoles := extractRoles(msgs)
			assert.Equal(
				t, tc.expected.messageRoles, actualRoles,
			)
		})
	}
}
