package models

import (
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/anthropic"
)

// LangChainGo v0.1.14 reads only the first part of an Anthropic assistant message. Split those
// parts into consecutive assistant messages; the Messages API combines consecutive same-role
// messages. Keep this adapter repair here so agent history retains the complete logical turn.
func (m *LCGWrapper) providerMessages(messages []llms.MessageContent) []llms.MessageContent {
	if _, ok := m.model.(*anthropic.LLM); !ok {
		return messages
	}
	var result []llms.MessageContent
	for _, message := range messages {
		if message.Role != llms.ChatMessageTypeAI || len(message.Parts) < 2 {
			result = append(result, message)
			continue
		}
		for _, part := range message.Parts {
			result = append(result, llms.MessageContent{Role: message.Role,
				Parts: []llms.ContentPart{part}})
		}
	}
	return result
}

// The same adapter returns Anthropic content blocks as separate choices. They are one answer,
// not alternative completions. Merge them before the agent selects a choice. Keep block metadata
// as evidence; replay of signed thinking blocks needs a richer provider contract in later work.
func (m *LCGWrapper) providerResponse(response *llms.ContentResponse) *llms.ContentResponse {
	if _, ok := m.model.(*anthropic.LLM); !ok || len(response.Choices) == 0 {
		return response
	}
	choice := &llms.ContentChoice{GenerationInfo: map[string]any{}}
	var blocks []any
	for _, block := range response.Choices {
		choice.Content += block.Content
		choice.ReasoningContent += block.ReasoningContent
		choice.ToolCalls = append(choice.ToolCalls, block.ToolCalls...)
		choice.StopReason = block.StopReason
		if thinking, ok := block.GenerationInfo["ThinkingContent"].(string); ok {
			choice.ReasoningContent += thinking
		}
		for key, value := range block.GenerationInfo {
			if _, exists := choice.GenerationInfo[key]; !exists {
				choice.GenerationInfo[key] = value
			}
		}
		blocks = append(blocks, block.GenerationInfo)
	}
	choice.GenerationInfo["AnthropicContentBlocks"] = blocks
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{choice}}
}
