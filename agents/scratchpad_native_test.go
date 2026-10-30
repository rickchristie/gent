package agents_test

import (
	"slices"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents"
	"github.com/stretchr/testify/assert"
)

func TestScratchpad_NativeDedupKeepsCallIdentity(t *testing.T) {
	image := gent.ImageURLContent{URL: "https://example.com/record.png"}
	type input struct {
		expired bool
		code    bool
	}
	type expected struct {
		firstText  string
		firstMedia bool
		count      int
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"duplicate", input{}, expected{`<lookup>
saved summary
</lookup>`, false, 2}},
		{"later result expired", input{expired: true}, expected{"full result", true, 1}},
		{"program log retained", input{code: true}, expected{`<lookup>
saved summary
</lookup>
full program log`, true, 2}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var entries []*gent.HistoryEntry
			for _, id := range []string{"old-call", "new-call"} {
				result := &gent.ToolCallResult{Name: "lookup", Hash: "same-call", Text: "full result",
					SummaryCaptured: true, DeduplicationSummary: "saved summary",
					Media: []gent.ContentPart{image}}
				metadata := &gent.ToolChainResult{Results: []*gent.ToolCallResult{result}}
				text := "full result"
				if test.input.code {
					result.Text, result.Media = "", nil
					text = "full program log"
					metadata.Results = append(metadata.Results, &gent.ToolCallResult{
						Text: text, Media: []gent.ContentPart{image}})
				}
				message := &gent.MessageContent{Role: gent.ChatMessageTypeTool,
					Parts: []gent.ContentPart{gent.ToolCallResponse{ToolCallID: id,
						Name: "lookup", Content: text}, image}}
				message.SetMetadata(gent.MMKToolChainResult, metadata)
				entries = append(entries, &gent.HistoryEntry{Messages: []*gent.MessageContent{message}})
			}
			if test.input.expired {
				entries[1].ExpireAfterIteration = 2
			}
			messages, count := agents.ScratchpadToMessages(entries, 2, &mockToolChain{}, &testXMLFormat{})
			firstParts := []gent.ContentPart{gent.ToolCallResponse{ToolCallID: "old-call",
				Name: "lookup", Content: test.expected.firstText}}
			if test.expected.firstMedia {
				firstParts = append(firstParts, image)
			}
			want := []*gent.ModelMessage{{Role: gent.ChatMessageTypeTool, Parts: firstParts}}
			if test.expected.count == 2 {
				want = append(want, &gent.ModelMessage{Role: gent.ChatMessageTypeTool,
					Parts: slices.Clone(entries[1].Messages[0].Parts)})
			}
			assert.Equal(t, want, messages)
			assert.Equal(t, test.expected.count, count)
			assert.Equal(t, "old-call",
				entries[0].Messages[0].Parts[0].(gent.ToolCallResponse).ToolCallID)
		})
	}
}
