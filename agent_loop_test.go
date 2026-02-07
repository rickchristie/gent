package gent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tmc/langchaingo/llms"
)

// -----------------------------------------------------------------------------
// BasicLoopData Tests
// -----------------------------------------------------------------------------

func TestBasicLoopData_GetTask(t *testing.T) {
	type input struct {
		task *Task
	}

	type expected struct {
		text string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "task with text",
			input: input{
				task: &Task{Text: "test input"},
			},
			expected: expected{
				text: "test input",
			},
		},
		{
			name: "nil task",
			input: input{
				task: nil,
			},
			expected: expected{
				text: "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := NewBasicLoopData(tt.input.task)

			result := data.GetTask()

			if tt.input.task == nil {
				assert.Nil(t, result)
			} else {
				assert.Equal(t, tt.expected.text, result.Text)
			}
		})
	}
}

func TestBasicLoopData_IterationHistory(t *testing.T) {
	data := NewBasicLoopData(nil)

	assert.Empty(t, data.GetIterationHistory(), "expected empty history initially")

	iter := &Iteration{
		Messages: []*MessageContent{
			{Role: llms.ChatMessageTypeAI, Parts: []ContentPart{llms.TextContent{Text: "test"}}},
		},
	}
	data.AddIterationHistory(iter)

	history := data.GetIterationHistory()
	assert.Len(t, history, 1)
	assert.Len(t, history[0].Messages, 1)
}

func TestBasicLoopData_ScratchPad(t *testing.T) {
	data := NewBasicLoopData(nil)

	assert.Empty(t, data.GetScratchPad(), "expected empty scratchpad initially")

	iter := &Iteration{
		Messages: []*MessageContent{
			{Role: llms.ChatMessageTypeAI, Parts: []ContentPart{llms.TextContent{Text: "test"}}},
		},
	}
	data.SetScratchPad([]*Iteration{iter})

	scratchpad := data.GetScratchPad()
	assert.Len(t, scratchpad, 1)
}
