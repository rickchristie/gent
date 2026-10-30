package trace

import (
	"context"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestFinalContentTraceRetention(t *testing.T) {
	type input struct {
		include bool
		limit   int
		redact  func([]gent.ContentPart) any
	}
	type expected struct {
		content   string
		truncated bool
		err       error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"visible", input{include: true}, expected{content: "Accepted."}},
		{"hidden", input{}, expected{}},
		{"bounded", input{include: true, limit: 5}, expected{"Accep", true, nil}},
		{"redacted text", input{include: true, redact: func([]gent.ContentPart) any {
			return "Removed."
		}}, expected{content: "Removed."}},
		{"redacted parts", input{include: true, redact: func([]gent.ContentPart) any {
			return []gent.ContentPart{gent.TextContent{Text: "Removed."}}
		}}, expected{content: "Removed."}},
		{"redacted non-text", input{include: true, redact: func([]gent.ContentPart) any {
			return map[string]any{"removed": true}
		}}, expected{}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			cfg := Config{IncludeRunOutput: test.input.include, MaxContentBytes: test.input.limit,
				Redactor: RedactorFuncs{RunOutput: test.input.redact}}
			seq := newTestSequencer(t, "answer", cfg)
			ctx := gent.NewExecutionContext(context.Background(), "root", nil,
				gent.WithRunId("answer"), gent.WithObserver(seq))
			defer ctx.Release()
			ctx.PublishBeforeExecution()
			assert.NoError(t, ctx.CommitCheckpoint(gent.PhaseInitial))
			ctx.SetFinalContentSource(&gent.ContentSource{MessageIds: []string{"preview"}})
			ctx.SetTermination(gent.TerminationSuccess,
				[]gent.ContentPart{gent.TextContent{Text: "Accepted."}}, nil)
			ctx.PublishAfterExecution(gent.TerminationSuccess, nil)
			err := ctx.CommitCheckpoint(gent.PhaseTerminal)
			snapshot := seq.Snapshot()
			assert.Equal(t, test.expected, expected{gent.ContentText(snapshot.Contexts[0].Content),
				snapshot.Contexts[0].ContentTruncated, err})
			assert.Equal(t, ctx.ContextId()+":answer", snapshot.Contexts[0].MessageId)
			assert.Equal(t, []string{"preview"}, snapshot.Contexts[0].SourceMessageIds)
			assert.Equal(t, snapshot.LastEventNumber, snapshot.Contexts[0].LastEventNumber)
			snapshot.Contexts[0].SourceMessageIds[0] = "changed"
			assert.Equal(t, []string{"preview"}, seq.Snapshot().Contexts[0].SourceMessageIds)
			view, err := ctx.Export()
			assert.NoError(t, err)
			replay := newTestSequencer(t, "answer", cfg)
			assert.NoError(t, replay.ObserveCommitted(view.Activity, ctx.Codecs()))
			assert.Equal(t, seq.Snapshot(), replay.Snapshot())
		})
	}
}

func TestTraceMetadataOrderAndSharedLimit(t *testing.T) {
	seq := newTestSequencer(t, "meta", Config{IncludeChunkText: true, MaxMetaContentBytes: 5})
	ctx := gent.NewExecutionContext(context.Background(), "root", nil,
		gent.WithRunId("meta"), gent.WithObserver(seq))
	defer ctx.Release()
	before := ctx.PublishBeforeModelCall("fixture", gent.NewModelCallRequest(nil))
	for _, meta := range []*gent.MetaContent{
		{
			Type:      gent.MetaContentReasoning,
			Content:   []gent.ContentPart{gent.TextContent{Text: "abc"}},
			MessageId: "reason",
		},
		{
			Type:      gent.MetaContentCommentary,
			Content:   []gent.ContentPart{gent.TextContent{Text: "def"}},
			MessageId: "comment",
		},
		{
			Type:      "progress",
			Content:   []gent.ContentPart{gent.TextContent{Text: "ghi"}},
			MessageId: "progress",
		},
	} {
		assert.NoError(t, ctx.EmitChunk(gent.StreamChunk{ModelCallId: before.ModelCallId,
			MetaContent: []*gent.MetaContent{meta}}))
	}
	assert.NoError(t, ctx.FlushProgress())
	want := []*gent.MetaContent{
		{
			Type:      gent.MetaContentReasoning,
			Content:   []gent.ContentPart{gent.TextContent{Text: "abc"}},
			MessageId: "reason",
		},
		{
			Type:      gent.MetaContentCommentary,
			Content:   []gent.ContentPart{gent.TextContent{Text: "de"}},
			MessageId: "comment",
		},
	}
	snapshot := seq.Snapshot()
	assert.Equal(t, want, snapshot.ModelCalls[0].Stream.MetaContent)
	assert.True(t, snapshot.ModelCalls[0].Stream.MetaContentTruncated)
	snapshot.ModelCalls[0].Stream.MetaContent[0].Content = []gent.ContentPart{
		gent.TextContent{Text: "changed"},
	}
	assert.Equal(t, want, seq.Snapshot().ModelCalls[0].Stream.MetaContent)
}
