package gent

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestStreamHub_SubscribeAll_ReceivesAllChunks(t *testing.T) {
	hub := newStreamHub()

	ch, unsub := hub.subscribeAll()
	defer unsub()

	go func() {
		hub.emit(StreamChunk{
			MetaContent: []*MetaContent{
				{Type: MetaContentIntermediary, Content: []ContentPart{TextContent{Text: "hello"}}},
			},
		})
		hub.emit(StreamChunk{
			MetaContent: []*MetaContent{
				{
					Type: MetaContentIntermediary, Content: []ContentPart{TextContent{Text: "world"}},
				},
			}, StreamId: "s1",
		})
		hub.emit(StreamChunk{
			MetaContent: []*MetaContent{
				{
					Type: MetaContentIntermediary, Content: []ContentPart{TextContent{Text: "!"}},
				},
			}, StreamTopicId: "topic1",
		})
		hub.close()
	}()

	var chunks []StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	assert.Len(t, chunks, 3)
}

func TestStreamHub_SubscribeToStream_FiltersCorrectly(t *testing.T) {
	type input struct {
		streamID string
		chunks   []StreamChunk
	}

	type expected struct {
		contents []string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "filters to target stream only",
			input: input{
				streamID: "target-stream",
				chunks: []StreamChunk{
					{
						MetaContent: []*MetaContent{
							{
								Type: MetaContentIntermediary, Content: []ContentPart{TextContent{Text: "skip"}},
							},
						}, StreamId: "other-stream",
					},
					{
						MetaContent: []*MetaContent{
							{
								Type: MetaContentIntermediary, Content: []ContentPart{TextContent{Text: "hello"}},
							},
						}, StreamId: "target-stream",
					},
					{
						MetaContent: []*MetaContent{
							{Type: MetaContentIntermediary, Content: []ContentPart{TextContent{Text: "skip again"}}},
						},
					},
					{
						MetaContent: []*MetaContent{
							{
								Type: MetaContentIntermediary, Content: []ContentPart{TextContent{Text: "world"}},
							},
						}, StreamId: "target-stream",
					},
				},
			},
			expected: expected{
				contents: []string{"hello", "world"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hub := newStreamHub()

			ch, unsub := hub.subscribeToStream(tt.input.streamID)
			defer unsub()

			go func() {
				for _, chunk := range tt.input.chunks {
					hub.emit(chunk)
				}
				hub.close()
			}()

			var contents []string
			for chunk := range ch {
				contents = append(contents, MetaText(chunk.MetaContent, MetaContentIntermediary))
			}

			assert.Equal(t, tt.expected.contents, contents)
		})
	}
}

func TestStreamHub_SubscribeToTopic_FiltersCorrectly(t *testing.T) {
	type input struct {
		topicID string
		chunks  []StreamChunk
	}

	type expected struct {
		contents []string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "filters to target topic only",
			input: input{
				topicID: "my-topic",
				chunks: []StreamChunk{
					{
						MetaContent: []*MetaContent{
							{
								Type: MetaContentIntermediary, Content: []ContentPart{TextContent{Text: "skip"}},
							},
						}, StreamTopicId: "other-topic",
					},
					{
						MetaContent: []*MetaContent{
							{
								Type: MetaContentIntermediary, Content: []ContentPart{TextContent{Text: "hello"}},
							},
						}, StreamTopicId: "my-topic",
					},
					{
						MetaContent: []*MetaContent{
							{Type: MetaContentIntermediary, Content: []ContentPart{TextContent{Text: "skip again"}}},
						},
					},
					{
						MetaContent: []*MetaContent{
							{
								Type: MetaContentIntermediary, Content: []ContentPart{TextContent{Text: "world"}},
							},
						}, StreamTopicId: "my-topic",
					},
				},
			},
			expected: expected{
				contents: []string{"hello", "world"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hub := newStreamHub()

			ch, unsub := hub.subscribeToTopic(tt.input.topicID)
			defer unsub()

			go func() {
				for _, chunk := range tt.input.chunks {
					hub.emit(chunk)
				}
				hub.close()
			}()

			var contents []string
			for chunk := range ch {
				contents = append(contents, MetaText(chunk.MetaContent, MetaContentIntermediary))
			}

			assert.Equal(t, tt.expected.contents, contents)
		})
	}
}

func TestStreamHub_EmptyIdReturnsNil(t *testing.T) {
	hub := newStreamHub()

	ch1, unsub1 := hub.subscribeToStream("")
	assert.Nil(t, ch1)
	assert.Nil(t, unsub1)

	ch2, unsub2 := hub.subscribeToTopic("")
	assert.Nil(t, ch2)
	assert.Nil(t, unsub2)
}

func TestStreamHub_Unsubscribe(t *testing.T) {
	hub := newStreamHub()

	ch, unsub := hub.subscribeAll()

	hub.emit(StreamChunk{
		MetaContent: []*MetaContent{
			{Type: MetaContentIntermediary, Content: []ContentPart{TextContent{Text: "first"}}},
		},
	})

	unsub()

	select {
	case _, ok := <-ch:
		assert.False(t, ok, "unsubscribe should discard queued chunks")
	case <-time.After(time.Second):
		t.Error("channel did not close after unsubscribe")
	}

	// Double unsubscribe should not panic
	unsub()

	hub.close()
}

func TestStreamHub_DrainingUnsubscribeKeepsQueuedChunks(t *testing.T) {
	hub := newStreamHub()

	ch, unsub := hub.subscribeAllDraining()
	hub.emit(StreamChunk{
		MetaContent: []*MetaContent{
			{Type: MetaContentIntermediary, Content: []ContentPart{TextContent{Text: "first"}}},
		},
	})
	hub.emit(StreamChunk{
		MetaContent: []*MetaContent{
			{Type: MetaContentIntermediary, Content: []ContentPart{TextContent{Text: "second"}}},
		},
	})
	unsub()

	var contents []string
	for chunk := range ch {
		contents = append(contents, MetaText(chunk.MetaContent, MetaContentIntermediary))
	}
	assert.Equal(t, []string{"first", "second"}, contents)
}

func TestStreamHub_MultipleSubscribers(t *testing.T) {
	hub := newStreamHub()

	ch1, unsub1 := hub.subscribeAll()
	ch2, unsub2 := hub.subscribeAll()
	defer unsub1()
	defer unsub2()

	go func() {
		hub.emit(StreamChunk{
			MetaContent: []*MetaContent{
				{Type: MetaContentIntermediary, Content: []ContentPart{TextContent{Text: "hello"}}},
			},
		})
		hub.close()
	}()

	chunk1 := <-ch1
	chunk2 := <-ch2

	assert.Equal(t, "hello", MetaText(chunk1.MetaContent, MetaContentIntermediary))
	assert.Equal(t, "hello", MetaText(chunk2.MetaContent, MetaContentIntermediary))
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
					MetaContent: []*MetaContent{
						{Type: MetaContentIntermediary, Content: []ContentPart{TextContent{Text: "chunk"}}},
					},
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
	assert.Equal(t, expected, count)
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
	assert.NotNil(t, unsub, "expected non-nil unsubscribe function")

	select {
	case _, ok := <-ch:
		assert.False(t, ok, "expected closed channel")
	case <-time.After(time.Second):
		t.Error("timed out waiting for closed channel")
	}
}

func TestStreamHub_EmitAfterClose(t *testing.T) {
	hub := newStreamHub()

	ch, unsub := hub.subscribeAll()
	defer unsub()

	hub.close()
	hub.emit(StreamChunk{
		MetaContent: []*MetaContent{
			{
				Type: MetaContentIntermediary, Content: []ContentPart{TextContent{Text: "should be ignored"}},
			},
		},
	}) // Should not panic

	count := 0
	for range ch {
		count++
	}
	assert.Equal(t, 0, count)
}
