package gent

import (
	"context"
	"testing"
)

// Record a full answer stream through the public acceptance path. This includes digest creation,
// saved events, and delivery, so an optimization cannot omit storage work when no listener exists.
func BenchmarkRecordAnswerStream(b *testing.B) {
	for b.Loop() {
		ctx := NewExecutionContext(context.Background(), "benchmark", NewBasicLoopData(nil))
		for range 1000 {
			if err := ctx.EmitChunk(StreamChunk{MetaContent: []*MetaContent{{
				Type: MetaContentIntermediary, Content: []ContentPart{TextContent{Text: "word"}},
			}}}); err != nil {
				ctx.Release()
				b.Fatal(err)
			}
		}
		ctx.Release()
	}
	b.ReportAllocs()
}
