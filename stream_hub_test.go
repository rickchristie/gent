package gent

import (
	"sync"
	"testing"
	"time"
)

func TestStreamHub_SubscribeAll_ReceivesAllChunks(t *testing.T) {
	hub := newStreamHub()

	ch, unsub := hub.subscribeAll()
	defer unsub()

	// Emit chunks
	go func() {
		hub.emit(StreamChunk{Content: "hello"})
		hub.emit(StreamChunk{Content: "world", StreamId: "s1"})
		hub.emit(StreamChunk{Content: "!", StreamTopicId: "topic1"})
		hub.close()
	}()

	// Collect chunks
	var chunks []StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	if len(chunks) != 3 {
		t.Errorf("expected 3 chunks, got %d", len(chunks))
	}
}

func TestStreamHub_SubscribeToStream_FiltersCorrectly(t *testing.T) {
	hub := newStreamHub()

	ch, unsub := hub.subscribeToStream("target-stream")
	defer unsub()

	go func() {
		hub.emit(StreamChunk{Content: "skip", StreamId: "other-stream"})
		hub.emit(StreamChunk{Content: "hello", StreamId: "target-stream"})
		hub.emit(StreamChunk{Content: "skip again"})
		hub.emit(StreamChunk{Content: "world", StreamId: "target-stream"})
		hub.close()
	}()

	var chunks []StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	if len(chunks) != 2 {
		t.Errorf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].Content != "hello" || chunks[1].Content != "world" {
		t.Errorf("unexpected chunks: %+v", chunks)
	}
}

func TestStreamHub_SubscribeToTopic_FiltersCorrectly(t *testing.T) {
	hub := newStreamHub()

	ch, unsub := hub.subscribeToTopic("my-topic")
	defer unsub()

	go func() {
		hub.emit(StreamChunk{Content: "skip", StreamTopicId: "other-topic"})
		hub.emit(StreamChunk{Content: "hello", StreamTopicId: "my-topic"})
		hub.emit(StreamChunk{Content: "skip again"})
		hub.emit(StreamChunk{Content: "world", StreamTopicId: "my-topic"})
		hub.close()
	}()

	var chunks []StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	if len(chunks) != 2 {
		t.Errorf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].Content != "hello" || chunks[1].Content != "world" {
		t.Errorf("unexpected chunks: %+v", chunks)
	}
}

func TestStreamHub_EmptyIdReturnsNil(t *testing.T) {
	hub := newStreamHub()

	ch1, unsub1 := hub.subscribeToStream("")
	if ch1 != nil || unsub1 != nil {
		t.Error("expected nil for empty stream ID")
	}

	ch2, unsub2 := hub.subscribeToTopic("")
	if ch2 != nil || unsub2 != nil {
		t.Error("expected nil for empty topic ID")
	}
}

func TestStreamHub_Unsubscribe(t *testing.T) {
	hub := newStreamHub()

	ch, unsub := hub.subscribeAll()

	// Send one chunk
	hub.emit(StreamChunk{Content: "first"})

	// Unsubscribe
	unsub()

	// Channel should close after unsubscribe
	select {
	case _, ok := <-ch:
		// We may or may not receive the first chunk depending on timing
		// But eventually the channel should close
		if ok {
			// Drain any remaining
			for range ch {
			}
		}
	case <-time.After(time.Second):
		t.Error("channel did not close after unsubscribe")
	}

	// Double unsubscribe should not panic
	unsub()

	hub.close()
}

func TestStreamHub_MultipleSubscribers(t *testing.T) {
	hub := newStreamHub()

	ch1, unsub1 := hub.subscribeAll()
	ch2, unsub2 := hub.subscribeAll()
	defer unsub1()
	defer unsub2()

	go func() {
		hub.emit(StreamChunk{Content: "hello"})
		hub.close()
	}()

	// Both subscribers should receive the chunk
	chunk1 := <-ch1
	chunk2 := <-ch2

	if chunk1.Content != "hello" || chunk2.Content != "hello" {
		t.Errorf("expected both to receive 'hello', got %q and %q", chunk1.Content, chunk2.Content)
	}
}

func TestStreamHub_ConcurrentEmit(t *testing.T) {
	hub := newStreamHub()

	ch, unsub := hub.subscribeAll()
	defer unsub()

	const numEmitters = 10
	const chunksPerEmitter = 100

	var wg sync.WaitGroup
	wg.Add(numEmitters)

	for i := range numEmitters {
		go func(emitterID int) {
			defer wg.Done()
			for j := range chunksPerEmitter {
				hub.emit(StreamChunk{
					Content:  "chunk",
					StreamId: string(rune(emitterID*1000 + j)),
				})
			}
		}(i)
	}

	go func() {
		wg.Wait()
		hub.close()
	}()

	count := 0
	for range ch {
		count++
	}

	expected := numEmitters * chunksPerEmitter
	if count != expected {
		t.Errorf("expected %d chunks, got %d", expected, count)
	}
}

func TestStreamHub_CloseAfterClose(t *testing.T) {
	hub := newStreamHub()
	hub.close()
	hub.close() // Should not panic
}

func TestStreamHub_SubscribeAfterClose(t *testing.T) {
	hub := newStreamHub()
	hub.close()

	ch, unsub := hub.subscribeAll()
	if unsub == nil {
		t.Error("expected non-nil unsubscribe function")
	}

	// Channel should be closed
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected closed channel")
		}
	case <-time.After(time.Second):
		t.Error("timed out waiting for closed channel")
	}
}

func TestStreamHub_EmitAfterClose(t *testing.T) {
	hub := newStreamHub()

	ch, unsub := hub.subscribeAll()
	defer unsub()

	hub.close()
	hub.emit(StreamChunk{Content: "should be ignored"}) // Should not panic

	// Channel should be closed with no chunks
	count := 0
	for range ch {
		count++
	}
	if count != 0 {
		t.Errorf("expected 0 chunks after close, got %d", count)
	}
}
