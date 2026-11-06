package agents

import "github.com/rickchristie/gent"

// ScratchpadToMessages selects unexpired history without changing its content. Repeated calls
// retain their complete prepared responses. Compaction is a separate, explicit context policy.
// The parts are read-only views; the managed request boundary isolates permitted hook edits.
func ScratchpadToMessages(scratchpad []*gent.HistoryEntry, currentIteration int) (
	[]*gent.ModelMessage, int,
) {
	var messages []*gent.ModelMessage
	included := 0
	for _, entry := range scratchpad {
		if entry.ExpireAfterIteration > 0 && currentIteration >= entry.ExpireAfterIteration {
			continue
		}
		included++
		for _, message := range entry.Messages {
			messages = append(messages, &gent.ModelMessage{Role: message.Role,
				Parts: message.Parts, Continuation: message.Continuation})
		}
	}
	return messages, included
}
