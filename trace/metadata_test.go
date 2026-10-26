package trace

import (
	"context"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestMetadataTraceRetention(t *testing.T) {
	type input struct {
		include bool
		limit   int
		redact  bool
	}
	type expected struct {
		text      string
		truncated bool
		err       error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"visible", input{true, 0, false}, expected{text: "Checking."}},
		{"hidden", input{false, 0, false}, expected{}},
		{"bounded", input{true, 5, false}, expected{text: "Check", truncated: true}},
		{"unlimited", input{true, NoContentLimit, false}, expected{text: "Checking."}},
		{"redacted", input{true, 0, true}, expected{text: "Removed."}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			cfg := Config{IncludeChunkText: test.input.include,
				MaxMetaContentBytes: test.input.limit}
			if test.input.redact {
				cfg.Redactor = RedactorFuncs{Chunk: func(chunk gent.StreamChunk) gent.StreamChunk {
					chunk.MetaContent[0].Text = "Removed."
					return chunk
				}}
			}
			seq := newTestSequencer(t, "commentary", cfg)
			ctx := gent.NewExecutionContext(context.Background(), "trace", nil,
				gent.WithRunId(seq.Snapshot().RunId), gent.WithObserver(seq))
			before := ctx.PublishBeforeModelCall("fixture", gent.NewModelCallRequest(nil))
			err := ctx.EmitChunk(gent.StreamChunk{ModelCallId: before.ModelCallId,
				ContextId: before.ContextId, MetaContent: []*gent.MetaContent{
					{Type: gent.MetaContentCommentary, Text: "Checking."}}})
			if !assert.NoError(t, err) || !assert.NoError(t, ctx.FlushProgress()) {
				return
			}
			calls := seq.Snapshot().ModelCalls
			if !assert.Len(t, calls, 1) || !assert.NotNil(t, calls[0].Stream) {
				return
			}
			stream := calls[0].Stream
			assert.Equal(t, test.expected, expected{
				gent.MetaText(stream.MetaContent, gent.MetaContentCommentary),
				stream.MetaContentTruncated, err})
		})
	}
}
