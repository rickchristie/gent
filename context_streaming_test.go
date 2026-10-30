package gent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// testLoopData is a simple LoopData implementation for testing.
type testLoopData struct{}

func (d *testLoopData) GetTask() *Task { return nil }

func TestExecutionContext_SubscribeAll(t *testing.T) {
	ctx := NewExecutionContext(context.Background(), "test", &testLoopData{})

	ch, unsub := ctx.SubscribeAll()
	defer unsub()

	go func() {
		ctx.EmitChunk(StreamChunk{
			MetaContent: []*MetaContent{
				{
					Type: MetaContentIntermediary, Content: []ContentPart{TextContent{Text: "hello"}},
				},
			},
		})
		ctx.EmitChunk(StreamChunk{
			MetaContent: []*MetaContent{
				{
					Type: MetaContentIntermediary, Content: []ContentPart{TextContent{Text: "world"}},
				},
			},
		})
		ctx.CloseStreams()
	}()

	var chunks []StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	assert.Len(t, chunks, 2)
}

func TestExecutionContext_SubscribeToStream(t *testing.T) {
	type input struct {
		streamID string
		chunks   []StreamChunk
	}

	type expected struct {
		count int
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "filters to target stream",
			input: input{
				streamID: "my-stream",
				chunks: []StreamChunk{
					{
						MetaContent: []*MetaContent{
							{
								Type: MetaContentIntermediary, Content: []ContentPart{TextContent{Text: "skip"}},
							},
						}, StreamId: "other",
					},
					{
						MetaContent: []*MetaContent{
							{
								Type: MetaContentIntermediary, Content: []ContentPart{TextContent{Text: "hello"}},
							},
						}, StreamId: "my-stream",
					},
					{
						MetaContent: []*MetaContent{
							{
								Type: MetaContentIntermediary, Content: []ContentPart{TextContent{Text: "world"}},
							},
						}, StreamId: "my-stream",
					},
				},
			},
			expected: expected{count: 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := NewExecutionContext(context.Background(), "test", &testLoopData{})

			ch, unsub := ctx.SubscribeToStream(tt.input.streamID)
			defer unsub()

			go func() {
				for _, chunk := range tt.input.chunks {
					ctx.EmitChunk(chunk)
				}
				ctx.CloseStreams()
			}()

			var chunks []StreamChunk
			for chunk := range ch {
				chunks = append(chunks, chunk)
			}

			assert.Len(t, chunks, tt.expected.count)
		})
	}
}

func TestExecutionContext_SubscribeToTopic(t *testing.T) {
	type input struct {
		topicID string
		chunks  []StreamChunk
	}

	type expected struct {
		count int
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "filters to target topic",
			input: input{
				topicID: "llm-response",
				chunks: []StreamChunk{
					{
						MetaContent: []*MetaContent{
							{
								Type: MetaContentIntermediary, Content: []ContentPart{TextContent{Text: "skip"}},
							},
						}, StreamTopicId: "other",
					},
					{
						MetaContent: []*MetaContent{
							{
								Type: MetaContentIntermediary, Content: []ContentPart{TextContent{Text: "hello"}},
							},
						}, StreamTopicId: "llm-response",
					},
					{
						MetaContent: []*MetaContent{
							{
								Type: MetaContentIntermediary, Content: []ContentPart{TextContent{Text: "world"}},
							},
						}, StreamTopicId: "llm-response",
					},
				},
			},
			expected: expected{count: 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := NewExecutionContext(context.Background(), "test", &testLoopData{})

			ch, unsub := ctx.SubscribeToTopic(tt.input.topicID)
			defer unsub()

			go func() {
				for _, chunk := range tt.input.chunks {
					ctx.EmitChunk(chunk)
				}
				ctx.CloseStreams()
			}()

			var chunks []StreamChunk
			for chunk := range ch {
				chunks = append(chunks, chunk)
			}

			assert.Len(t, chunks, tt.expected.count)
		})
	}
}

func TestExecutionContext_BuildSourcePath(t *testing.T) {
	type input struct {
		setupFn func() *ExecutionContext
	}

	type expected struct {
		path string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "root context with single iteration",
			input: input{
				setupFn: func() *ExecutionContext {
					ctx := NewExecutionContext(context.Background(), "main", &testLoopData{})
					ctx.IncrementIteration()
					return ctx
				},
			},
			expected: expected{path: "main/1"},
		},
		{
			name: "child context",
			input: input{
				setupFn: func() *ExecutionContext {
					parent := NewExecutionContext(context.Background(), "main", &testLoopData{})
					parent.IncrementIteration()
					parent.IncrementIteration() // iteration 2

					child := parent.SpawnChild("research", &testLoopData{})
					child.IncrementIteration()
					return child
				},
			},
			expected: expected{path: "main/2/research/1"},
		},
		{
			name: "deeply nested context",
			input: input{
				setupFn: func() *ExecutionContext {
					root := NewExecutionContext(context.Background(), "main", &testLoopData{})
					root.IncrementIteration()

					child1 := root.SpawnChild("orchestrator", &testLoopData{})
					child1.IncrementIteration()
					child1.IncrementIteration()
					child1.IncrementIteration() // iteration 3

					child2 := child1.SpawnChild("worker", &testLoopData{})
					child2.IncrementIteration()
					child2.IncrementIteration() // iteration 2
					return child2
				},
			},
			expected: expected{path: "main/1/orchestrator/3/worker/2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.input.setupFn()
			path := ctx.BuildSourcePath()
			assert.Equal(t, tt.expected.path, path)
		})
	}
}

func TestExecutionContext_EmitChunk_AutoPopulatesSource(t *testing.T) {
	ctx := NewExecutionContext(context.Background(), "test", &testLoopData{})
	ctx.IncrementIteration()

	ch, unsub := ctx.SubscribeAll()
	defer unsub()

	go func() {
		ctx.EmitChunk(StreamChunk{
			MetaContent: []*MetaContent{
				{
					Type: MetaContentIntermediary, Content: []ContentPart{TextContent{Text: "hello"}},
				},
			},
		})
		ctx.CloseStreams()
	}()

	chunk := <-ch
	assert.Equal(t, "test/1", chunk.Source)
}

func TestExecutionContext_EmitChunk_PreservesExistingSource(t *testing.T) {
	ctx := NewExecutionContext(context.Background(), "test", &testLoopData{})
	ctx.IncrementIteration()

	ch, unsub := ctx.SubscribeAll()
	defer unsub()

	go func() {
		ctx.EmitChunk(StreamChunk{
			MetaContent: []*MetaContent{
				{
					Type: MetaContentIntermediary, Content: []ContentPart{TextContent{Text: "hello"}},
				},
			}, Source: "custom/path",
		})
		ctx.CloseStreams()
	}()

	chunk := <-ch
	assert.Equal(t, "custom/path", chunk.Source)
}

func TestExecutionContext_ParentPropagation(t *testing.T) {
	parent := NewExecutionContext(context.Background(), "parent", &testLoopData{})
	parent.IncrementIteration()

	child := parent.SpawnChild("child", &testLoopData{})
	child.IncrementIteration()

	parentCh, parentUnsub := parent.SubscribeAll()
	defer parentUnsub()

	go func() {
		child.EmitChunk(StreamChunk{
			MetaContent: []*MetaContent{
				{
					Type: MetaContentIntermediary, Content: []ContentPart{TextContent{Text: "from child"}},
				},
			},
		})
		parent.CloseStreams()
		child.CloseStreams()
	}()

	chunk := <-parentCh
	assert.Equal(t, "from child", MetaText(chunk.MetaContent, MetaContentIntermediary))
	assert.Equal(t, "parent/1/child/1", chunk.Source)
}

func TestExecutionContext_ParentPropagation_MultipleLevels(t *testing.T) {
	root := NewExecutionContext(context.Background(), "root", &testLoopData{})
	root.IncrementIteration()

	child := root.SpawnChild("child", &testLoopData{})
	child.IncrementIteration()

	grandchild := child.SpawnChild("grandchild", &testLoopData{})
	grandchild.IncrementIteration()

	rootCh, rootUnsub := root.SubscribeAll()
	defer rootUnsub()

	go func() {
		grandchild.EmitChunk(StreamChunk{
			MetaContent: []*MetaContent{
				{
					Type: MetaContentIntermediary, Content: []ContentPart{TextContent{Text: "from grandchild"}},
				},
			},
		})
		root.CloseStreams()
		child.CloseStreams()
		grandchild.CloseStreams()
	}()

	chunk := <-rootCh
	assert.Equal(t, "from grandchild", MetaText(chunk.MetaContent, MetaContentIntermediary))
	assert.Equal(t, "root/1/child/1/grandchild/1", chunk.Source)
}

func TestExecutionContext_ConcurrentChildEmit(t *testing.T) {
	root := NewExecutionContext(context.Background(), "root", &testLoopData{})
	root.IncrementIteration()

	const numChildren = 5
	const chunksPerChild = 10

	children := make([]*ExecutionContext, numChildren)
	for i := range numChildren {
		children[i] = root.SpawnChild("child", &testLoopData{})
		children[i].IncrementIteration()
	}

	rootCh, rootUnsub := root.SubscribeAll()
	defer rootUnsub()

	var wg sync.WaitGroup
	wg.Add(numChildren)

	for i, child := range children {
		go func(childID int, c *ExecutionContext) {
			defer wg.Done()
			for j := range chunksPerChild {
				c.EmitChunk(StreamChunk{
					MetaContent: []*MetaContent{
						{Type: MetaContentIntermediary, Content: []ContentPart{TextContent{Text: "chunk"}}},
					},
					StreamId: string(rune(childID*100 + j)),
				})
			}
		}(i, child)
	}

	go func() {
		wg.Wait()
		root.CloseStreams()
		for _, c := range children {
			c.CloseStreams()
		}
	}()

	count := 0
	for range rootCh {
		count++
	}

	expected := numChildren * chunksPerChild
	assert.Equal(t, expected, count)
}

func TestExecutionContext_EarlyUnsubscribe(t *testing.T) {
	ctx := NewExecutionContext(context.Background(), "test", &testLoopData{})

	ch, unsub := ctx.SubscribeAll()

	go func() {
		for i := range 100 {
			ctx.EmitChunk(StreamChunk{
				MetaContent: []*MetaContent{
					{
						Type: MetaContentIntermediary,
						Content: []ContentPart{
							TextContent{Text: string(rune('A' + i%26))},
						},
					},
				},
			})
			time.Sleep(time.Millisecond)
		}
		ctx.CloseStreams()
	}()

	count := 0
	for range ch {
		count++
		if count >= 5 {
			unsub()
			break
		}
	}

	time.Sleep(50 * time.Millisecond)

	select {
	case _, ok := <-ch:
		assert.False(t, ok, "expected channel to be closed after unsubscribe")
	default:
		// Channel closed, good
	}
}

func TestExecutionContext_CloseStreams_MultipleCallsSafe(t *testing.T) {
	ctx := NewExecutionContext(context.Background(), "test", &testLoopData{})
	ctx.CloseStreams()
	ctx.CloseStreams() // Should not panic
}

func TestExecutionContext_MultipleSubscribers_SameTopic(t *testing.T) {
	ctx := NewExecutionContext(context.Background(), "test", &testLoopData{})

	ch1, unsub1 := ctx.SubscribeToTopic("topic")
	ch2, unsub2 := ctx.SubscribeToTopic("topic")
	defer unsub1()
	defer unsub2()

	go func() {
		ctx.EmitChunk(StreamChunk{
			MetaContent: []*MetaContent{
				{
					Type: MetaContentIntermediary, Content: []ContentPart{TextContent{Text: "hello"}},
				},
			}, StreamTopicId: "topic",
		})
		ctx.CloseStreams()
	}()

	chunk1 := <-ch1
	chunk2 := <-ch2

	assert.Equal(t, "hello", MetaText(chunk1.MetaContent, MetaContentIntermediary))
	assert.Equal(t, "hello", MetaText(chunk2.MetaContent, MetaContentIntermediary))
}

func TestExecutionContext_EmitChunkRecordsWithoutListeners(t *testing.T) {
	ctx := NewExecutionContext(context.Background(), "test", &testLoopData{})
	done := make(chan error, 1)
	stopped := make(chan struct{})
	t.Cleanup(func() {
		ctx.Release()
		<-stopped
	})
	go func() {
		defer close(stopped)
		for i := range 1000 {
			if err := ctx.Context().Err(); err != nil {
				done <- err
				return
			}
			if err := ctx.EmitChunk(StreamChunk{
				MetaContent: []*MetaContent{
					{
						Type: MetaContentIntermediary,
						Content: []ContentPart{
							TextContent{Text: string(rune('A' + i%26))},
						},
					},
				},
			}); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("recording 1000 chunks exceeded five seconds")
	}
	ctx.CloseStreams()
	bundle, err := ctx.Export()
	assert.NoError(t, err)
	assert.Equal(t, uint64(1000), bundle.LastAppliedEventNumber)
	assert.NoError(t, ValidateBundle(bundle))
}
