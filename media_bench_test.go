package gent

import (
	"bytes"
	"context"
	"testing"
)

var copiedMediaMessage ModelMessage
var copiedMediaPointer *ModelMessage

// A value copy and a pointer assignment do not copy the file payload. Measure them separately
// from the owned snapshots and saved restoration that must copy bytes for independent consumers.
func BenchmarkLargeMediaOwnership(b *testing.B) {
	data := bytes.Repeat([]byte{0, 1, 127, 255}, 1<<20)
	message := &ModelMessage{Role: ChatMessageTypeHuman,
		Parts: []ContentPart{BinaryContent{MIMEType: "application/pdf", Data: data}}}
	task := &Task{Messages: []*ModelMessage{
		TextParts(ChatMessageTypeSystem, "Read the attached file."), message}}
	b.Run("message_value", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			copiedMediaMessage = *message
		}
	})
	b.Run("message_pointer", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			copiedMediaPointer = message
		}
	})
	b.Run("owned_task_snapshot", func(b *testing.B) {
		ctx := NewExecutionContext(context.Background(), "media", NewBasicLoopData(task))
		defer ctx.Release()
		b.SetBytes(int64(len(data)))
		b.ReportAllocs()
		for b.Loop() {
			owned, err := ctx.Task()
			if err != nil {
				b.Fatal(err)
			}
			copiedMediaPointer = owned.Messages[1]
		}
	})
	b.Run("codec_save_restore", func(b *testing.B) {
		codecs := NewCodecRegistry()
		b.SetBytes(int64(len(data)))
		b.ReportAllocs()
		for b.Loop() {
			wire, err := codecs.Marshal(task)
			if err != nil {
				b.Fatal(err)
			}
			var restored *Task
			if err := codecs.Unmarshal(wire, &restored); err != nil {
				b.Fatal(err)
			}
			payload := restored.Messages[1].Parts[0].(BinaryContent).Data
			if !bytes.Equal(data, payload) {
				b.Fatal("restored attachment differs from the saved bytes")
			}
			b.ReportMetric(float64(len(wire)), "encoded-bytes/op")
			copiedMediaPointer = restored.Messages[1]
		}
	})
	copiedMediaMessage = ModelMessage{}
	copiedMediaPointer = nil
}
