package agents_test

import (
	"fmt"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents"
	"github.com/rickchristie/gent/schema"
	"github.com/stretchr/testify/assert"
	"github.com/tmc/langchaingo/llms"
)

// -----------------------------------------------------------------------------
// TestToolChainResult_ToIteration
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

func TestToolChainResult_ToIteration(t *testing.T) {
	xmlFmt := &testXMLFormat{}

	type input struct {
		results []*gent.ToolCallResult
	}

	type expected struct {
		messageCount int
		role         llms.ChatMessageType
		textPart     string
		mediaCount   int
		hasMetadata  bool
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "single tool call result",
			input: input{
				results: []*gent.ToolCallResult{
					{
						Name: "search",
						Args: map[string]any{"query": "weather"},
						Hash: "search:weather",
						Text: "<search>\nSunny, 72F\n</search>",
					},
				},
			},
			expected: expected{
				messageCount: 1,
				role:         llms.ChatMessageTypeHuman,
				textPart: `<observation>
<search>
Sunny, 72F
</search>
</observation>`,
				mediaCount:  0,
				hasMetadata: true,
			},
		},
		{
			name: "multiple tool call results merged",
			input: input{
				results: []*gent.ToolCallResult{
					{
						Name: "search",
						Text: "<search>\nresult1\n</search>",
					},
					{
						Name: "lookup",
						Text: "<lookup>\nresult2\n</lookup>",
					},
				},
			},
			expected: expected{
				messageCount: 1,
				role:         llms.ChatMessageTypeHuman,
				textPart: `<observation>
<search>
result1
</search>
<lookup>
result2
</lookup>
</observation>`,
				mediaCount:  0,
				hasMetadata: true,
			},
		},
		{
			name: "results with media",
			input: input{
				results: []*gent.ToolCallResult{
					{
						Name: "screenshot",
						Text: "<screenshot>\nCapture done\n</screenshot>",
						Media: []gent.ContentPart{
							llms.BinaryContent{
								MIMEType: "image/png",
								Data:     []byte("fake-png"),
							},
						},
					},
				},
			},
			expected: expected{
				messageCount: 1,
				role:         llms.ChatMessageTypeHuman,
				textPart: `<observation>
<screenshot>
Capture done
</screenshot>
</observation>`,
				mediaCount:  1,
				hasMetadata: true,
			},
		},
		{
			name: "empty results produces iteration with no text",
			input: input{
				results: []*gent.ToolCallResult{
					{
						Name: "noop",
						Text: "",
					},
				},
			},
			expected: expected{
				messageCount: 1,
				role:         llms.ChatMessageTypeHuman,
				textPart:     "",
				mediaCount:   0,
				hasMetadata:  true,
			},
		},
		{
			name: "error result has text with error, no Output",
			input: input{
				results: []*gent.ToolCallResult{
					{
						Name:  "failing_tool",
						Args:  map[string]any{"x": 1},
						Hash:  "failing_tool:x=1",
						Error: fmt.Errorf("connection refused"),
						Text:  "<failing_tool>\nError: connection refused\n</failing_tool>",
					},
				},
			},
			expected: expected{
				messageCount: 1,
				role:         llms.ChatMessageTypeHuman,
				textPart: `<observation>
<failing_tool>
Error: connection refused
</failing_tool>
</observation>`,
				mediaCount:  0,
				hasMetadata: true,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tcr := &gent.ToolChainResult{Results: tc.input.results}
			iter := tcr.ToIteration(xmlFmt)

			assert.Equal(
				t, tc.expected.messageCount, len(iter.Messages),
			)

			msg := iter.Messages[0]
			assert.Equal(t, tc.expected.role, msg.Role)

			// Check metadata points to original ToolChainResult
			assert.Equal(t, tc.expected.hasMetadata, msg.Metadata != nil)
			if tc.expected.hasMetadata {
				val, ok := msg.GetMetadata(gent.MMKToolChainResult)
				assert.True(t, ok)
				assert.Same(t, tcr, val)
			}

			// Count text and media parts
			textParts := ""
			mediaCount := 0
			for _, part := range msg.Parts {
				switch p := part.(type) {
				case llms.TextContent:
					textParts = p.Text
				default:
					mediaCount++
				}
			}

			assert.Equal(t, tc.expected.textPart, textParts)
			assert.Equal(t, tc.expected.mediaCount, mediaCount)
		})
	}
}

// -----------------------------------------------------------------------------
// TestScratchpadToMessages
// -----------------------------------------------------------------------------

// mockToolChain implements the ToolChain interface for testing
// deduplication. Only DeduplicateSummary is meaningful; other
// methods are stubs.
type mockToolChain struct {
	// summaries maps tool name to a summary string.
	// If a tool name is present with non-empty value, that
	// value is returned by DeduplicateSummary.
	summaries map[string]string
}

func (m *mockToolChain) Name() string     { return "action" }
func (m *mockToolChain) Guidance() string { return "" }
func (m *mockToolChain) ParseSection(
	_ *gent.ExecutionContext, _ string,
) (any, error) {
	return nil, nil
}
func (m *mockToolChain) RegisterTool(_ any) gent.ToolChain {
	return m
}
func (m *mockToolChain) AvailableToolsPrompt() string {
	return ""
}
func (m *mockToolChain) GetToolSchema(_ string) *schema.Schema {
	return nil
}
func (m *mockToolChain) Execute(
	_ *gent.ExecutionContext, _ string, _ gent.TextFormat,
) (*gent.ToolChainResult, error) {
	return nil, nil
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
) *gent.Iteration {
	obsIter := tcr.ToIteration(xmlFmt)
	aiMsg := &gent.MessageContent{
		Role:  llms.ChatMessageTypeAI,
		Parts: []gent.ContentPart{llms.TextContent{Text: aiText}},
	}
	return &gent.Iteration{
		Messages: append(
			[]*gent.MessageContent{aiMsg}, obsIter.Messages...,
		),
	}
}

// helper to build a plain iteration without metadata (passthrough).
func buildPlainIteration(
	aiText string,
	humanText string,
) *gent.Iteration {
	return &gent.Iteration{
		Messages: []*gent.MessageContent{
			{
				Role: llms.ChatMessageTypeAI,
				Parts: []gent.ContentPart{
					llms.TextContent{Text: aiText},
				},
			},
			{
				Role: llms.ChatMessageTypeHuman,
				Parts: []gent.ContentPart{
					llms.TextContent{Text: humanText},
				},
			},
		},
	}
}

func TestScratchpadToMessages(t *testing.T) {
	xmlFmt := &testXMLFormat{}

	type input struct {
		scratchpad       []*gent.Iteration
		currentIteration int
		toolChain        gent.ToolChain
	}

	type expected struct {
		messageTexts []string
		messageRoles []llms.ChatMessageType
		included     int
	}

	// Helper: extract text content from messages for comparison.
	extractTexts := func(
		msgs []llms.MessageContent,
	) []string {
		var texts []string
		for _, msg := range msgs {
			for _, part := range msg.Parts {
				if tp, ok := part.(llms.TextContent); ok {
					texts = append(texts, tp.Text)
				}
			}
		}
		return texts
	}

	extractRoles := func(
		msgs []llms.MessageContent,
	) []llms.ChatMessageType {
		var roles []llms.ChatMessageType
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
				scratchpad: []*gent.Iteration{
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
				messageRoles: []llms.ChatMessageType{
					llms.ChatMessageTypeAI,
					llms.ChatMessageTypeHuman,
				},
				included: 1,
			},
		},
		{
			name: "expired iterations skipped",
			input: input{
				scratchpad: []*gent.Iteration{
					{
						Messages: []*gent.MessageContent{
							{
								Role: llms.ChatMessageTypeAI,
								Parts: []gent.ContentPart{
									llms.TextContent{
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
				messageRoles: []llms.ChatMessageType{
					llms.ChatMessageTypeAI,
					llms.ChatMessageTypeHuman,
				},
				included: 1,
			},
		},
		{
			name: "non-expired iterations included",
			input: input{
				scratchpad: []*gent.Iteration{
					{
						Messages: []*gent.MessageContent{
							{
								Role: llms.ChatMessageTypeAI,
								Parts: []gent.ContentPart{
									llms.TextContent{
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
				messageRoles: []llms.ChatMessageType{
					llms.ChatMessageTypeAI,
					llms.ChatMessageTypeAI,
					llms.ChatMessageTypeHuman,
				},
				included: 2,
			},
		},
		{
			name: "single dedup: same tool+args in iter 1 and 2",
			input: input{
				scratchpad: []*gent.Iteration{
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
				messageRoles: []llms.ChatMessageType{
					llms.ChatMessageTypeAI,
					llms.ChatMessageTypeHuman,
					llms.ChatMessageTypeAI,
					llms.ChatMessageTypeHuman,
				},
				included: 2,
			},
		},
		{
			name: "multiple dedup: iters 1,2,3 same tool+args",
			input: input{
				scratchpad: []*gent.Iteration{
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
				messageRoles: []llms.ChatMessageType{
					llms.ChatMessageTypeAI,
					llms.ChatMessageTypeHuman,
					llms.ChatMessageTypeAI,
					llms.ChatMessageTypeHuman,
					llms.ChatMessageTypeAI,
					llms.ChatMessageTypeHuman,
				},
				included: 3,
			},
		},
		{
			name: "mixed observation: one deduplicatable, one not",
			input: input{
				scratchpad: []*gent.Iteration{
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
				messageRoles: []llms.ChatMessageType{
					llms.ChatMessageTypeAI,
					llms.ChatMessageTypeHuman,
					llms.ChatMessageTypeAI,
					llms.ChatMessageTypeHuman,
				},
				included: 2,
			},
		},
		{
			name: "errored calls not deduped",
			input: input{
				scratchpad: []*gent.Iteration{
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
				messageRoles: []llms.ChatMessageType{
					llms.ChatMessageTypeAI,
					llms.ChatMessageTypeHuman,
					llms.ChatMessageTypeAI,
					llms.ChatMessageTypeHuman,
				},
				included: 2,
			},
		},
		{
			name: "tool returns empty DeduplicateSummary: no dedup",
			input: input{
				scratchpad: []*gent.Iteration{
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
				messageRoles: []llms.ChatMessageType{
					llms.ChatMessageTypeAI,
					llms.ChatMessageTypeHuman,
					llms.ChatMessageTypeAI,
					llms.ChatMessageTypeHuman,
				},
				included: 2,
			},
		},
		{
			name: "nil toolChain disables dedup, only expiry",
			input: input{
				scratchpad: []*gent.Iteration{
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
				messageRoles: []llms.ChatMessageType{
					llms.ChatMessageTypeAI,
					llms.ChatMessageTypeHuman,
					llms.ChatMessageTypeAI,
					llms.ChatMessageTypeHuman,
				},
				included: 2,
			},
		},
		{
			name: "last occurrence keeps full text",
			input: input{
				scratchpad: []*gent.Iteration{
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
				messageRoles: []llms.ChatMessageType{
					llms.ChatMessageTypeAI,
					llms.ChatMessageTypeHuman,
					llms.ChatMessageTypeAI,
					llms.ChatMessageTypeHuman,
					llms.ChatMessageTypeAI,
					llms.ChatMessageTypeHuman,
				},
				included: 3,
			},
		},
		{
			name: "different args produce different hash, not deduped",
			input: input{
				scratchpad: []*gent.Iteration{
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
				messageRoles: []llms.ChatMessageType{
					llms.ChatMessageTypeAI,
					llms.ChatMessageTypeHuman,
					llms.ChatMessageTypeAI,
					llms.ChatMessageTypeHuman,
				},
				included: 2,
			},
		},
		{
			name: "included count correct with mixed expiry",
			input: input{
				scratchpad: []*gent.Iteration{
					func() *gent.Iteration {
						iter := buildPlainIteration("a", "b")
						iter.ExpireAfterIteration = 2
						return iter
					}(),
					buildPlainIteration("c", "d"),
					func() *gent.Iteration {
						iter := buildPlainIteration("e", "f")
						iter.ExpireAfterIteration = 3
						return iter
					}(),
					buildPlainIteration("g", "h"),
					func() *gent.Iteration {
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
				messageRoles: []llms.ChatMessageType{
					llms.ChatMessageTypeAI,
					llms.ChatMessageTypeHuman,
					llms.ChatMessageTypeAI,
					llms.ChatMessageTypeHuman,
					llms.ChatMessageTypeAI,
					llms.ChatMessageTypeHuman,
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
