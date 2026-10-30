package executor_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/internal/tt"
	"github.com/rickchristie/gent/output"
	"github.com/stretchr/testify/assert"
)

// Keep complete entries so an image with native tool evidence or a signed assistant turn remains
// replayable. A policy can choose all such entries or just the latest without editing originals.
type imageCompaction struct{ latest bool }

func (policy imageCompaction) Compact(_ *gent.ExecutionContext, input gent.CompactionInput) (
	gent.HistoryUpdate, error,
) {
	var selected []gent.EntryId
	for _, entry := range input.Entries {
		found := false
		for _, message := range entry.Messages {
			for _, part := range message.Parts {
				if _, ok := part.(gent.BinaryContent); ok {
					found = true
				}
			}
		}
		if found {
			if policy.latest {
				selected = selected[:0]
			}
			selected = append(selected, entry.Id)
		}
	}
	return gent.HistoryUpdate{ExpectedDraftRevision: input.Revision, Active: selected}, nil
}

type imageHistoryLoop struct {
	records  []*gent.HistoryEntry
	messages []*gent.ModelMessage
}

func (*imageHistoryLoop) Output() gent.Output[string] { return output.NewTextOutput() }

func (loop *imageHistoryLoop) Next(ctx *gent.ExecutionContext) (*gent.AgentLoopResult, error) {
	if ctx.Iteration() == 1 {
		for _, record := range loop.records {
			if err := ctx.AppendRecord(record, true); err != nil {
				return nil, err
			}
		}
		return &gent.AgentLoopResult{Action: gent.LAContinue}, nil
	}
	task, err := ctx.Task()
	if err != nil {
		return nil, err
	}
	history, err := ctx.Scratchpad()
	if err != nil {
		return nil, err
	}
	messages, _ := agents.ScratchpadToMessages(history, ctx.Iteration(), nil, nil)
	loop.messages = append(task.Messages, messages...)
	return gent.AcceptOutput(ctx, &gent.OutputResult[string]{Accepted: true, Value: "done",
		Content: []gent.ContentPart{gent.TextContent{Text: "done"}}}, nil)
}

func TestCustomCompactionRetainsImagesAndOriginalTask(t *testing.T) {
	type input struct{ latest bool }
	type expected struct{ indexes []int }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"all images", input{}, expected{[]int{0, 2}}},
		{"latest image", input{true}, expected{[]int{2}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			task := &gent.Task{Messages: []*gent.ModelMessage{
				gent.TextParts(gent.ChatMessageTypeSystem, "Keep the supplied image reference."),
				{Role: gent.ChatMessageTypeHuman, Parts: []gent.ContentPart{
					gent.TextContent{Text: "Compare images."},
					gent.BinaryContent{MIMEType: "image/png", Data: []byte{0, 1}}}},
			}}
			image := func(data byte) *gent.MessageContent {
				return &gent.MessageContent{Role: gent.ChatMessageTypeAI,
					Parts: []gent.ContentPart{gent.BinaryContent{
						MIMEType: "image/png", Data: []byte{data}}},
					Continuation: &gent.ProviderContinuation{Provider: "fixture",
						Items: []json.RawMessage{json.RawMessage(`{"signature":"keep"}`)}}}
			}
			records := []*gent.HistoryEntry{
				{Messages: []*gent.MessageContent{image(2)}},
				{Messages: []*gent.MessageContent{{Role: gent.ChatMessageTypeHuman,
					Parts: []gent.ContentPart{gent.TextContent{Text: "Temporary note."}}}}},
				{Messages: []*gent.MessageContent{
					{Role: gent.ChatMessageTypeAI, Parts: []gent.ContentPart{gent.ModelToolCall{
						ID: "c1", Type: "function", FunctionCall: &gent.FunctionCall{
							Name: "draw", Arguments: `{}`}}}},
					{Role: gent.ChatMessageTypeTool, Parts: []gent.ContentPart{gent.ToolCallResponse{
						ToolCallID: "c1", Name: "draw", Content: "Created image."}}}, image(3)}},
			}
			loop := &imageHistoryLoop{records: records}
			ctx := gent.NewExecutionContext(context.Background(), "images", gent.NewBasicLoopData(task))
			ctx.SetCompaction(tt.NewMockCompactionTrigger().WithShouldCompact(true),
				imageCompaction{latest: test.input.latest})
			value, result, err := executor.NewRunner(loop, executor.DefaultConfig()).Execute(ctx)
			assert.NoError(t, err)
			assert.Equal(t, "done", value)
			assert.Equal(t, gent.TerminationSuccess, result.TerminationReason)
			want := append([]*gent.ModelMessage(nil), task.Messages...)
			for _, index := range test.expected.indexes {
				for _, message := range records[index].Messages {
					want = append(want, &gent.ModelMessage{Role: message.Role,
						Parts: message.Parts, Continuation: message.Continuation})
				}
			}
			assert.Equal(t, want, loop.messages)
			for i, entry := range records {
				assert.Equal(t, entry.Messages, ctx.History().Entries[i].Messages)
			}
			saved, err := ctx.Export()
			assert.NoError(t, err)
			assert.NoError(t, gent.ValidateBundle(saved))
			restored, err := gent.RestoreExecutionContext(context.Background(), saved,
				gent.RestoreBindings{})
			if !assert.NoError(t, err) {
				return
			}
			defer restored.Release()
			actual, err := restored.Task()
			assert.NoError(t, err)
			assert.Equal(t, task, actual)
			assert.Equal(t, ctx.History(), restored.History())
		})
	}
}
