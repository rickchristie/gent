package agents_test

import (
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents"
	"github.com/stretchr/testify/assert"
)

func TestScratchpadRetainsCompleteRepeatedResults(t *testing.T) {
	execution := &gent.ToolExecutionResponse{Status: gent.ToolExecuted,
		Parts: []gent.ContentPart{gent.TextPart("Original order"), gent.PDFPart([]byte{0,
			1, 255})},
		Instructions: "Retain this evidence."}
	record, err := gent.NewToolCallRecordBuilder("logical", "attempt", "read").Execution(execution).
		ModelReply("provider", "read", gent.PrepareExecutionParts(execution,
			gent.SteeringMessages{})...).Build()
	assert.NoError(t, err)
	source := &gent.MessageContent{Role: gent.ChatMessageTypeTool,
		Parts: []gent.ContentPart{record.Response()}}
	source.SetMetadata(gent.MMKToolCallRecords, []*gent.ToolCallRecord{record})
	entries := []*gent.HistoryEntry{{Messages: []*gent.MessageContent{source}},
		{Messages: []*gent.MessageContent{source}}}
	messages, count := agents.ScratchpadToMessages(entries, 2)
	assert.Equal(t, 2, count)
	assert.Equal(t, []*gent.ModelMessage{record.ModelMessage(), record.ModelMessage()}, messages)
	assert.Equal(t, []gent.ContentPart{record.Response()}, source.Parts)
}

func TestScratchpadExpiryAndContinuation(t *testing.T) {
	type input struct{ iteration int }
	type expected struct {
		count    int
		messages []*gent.ModelMessage
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	saved := &gent.ProviderContinuation{Provider: "test"}
	first := &gent.ModelMessage{Role: gent.ChatMessageTypeAI,
		Parts: []gent.ContentPart{gent.TextPart("first")}, Continuation: saved}
	second := gent.TextParts(gent.ChatMessageTypeHuman, "second")
	entries := []*gent.HistoryEntry{
		{ExpireAfterIteration: 2, Messages: []*gent.MessageContent{{Role: first.Role, Parts: first.Parts,
			Continuation: saved}}},
		{Messages: []*gent.MessageContent{{Role: second.Role, Parts: second.Parts}}},
	}
	cases := []testCase{
		{"before expiry", input{1}, expected{2, []*gent.ModelMessage{first, second}}},
		{"at expiry", input{2}, expected{1, []*gent.ModelMessage{second}}},
		{"after expiry", input{3}, expected{1, []*gent.ModelMessage{second}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			messages, count := agents.ScratchpadToMessages(entries, test.input.iteration)
			assert.Equal(t, test.expected, expected{count, messages})
		})
	}
}
