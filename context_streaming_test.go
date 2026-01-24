package gent

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// testLoopData is a simple LoopData implementation for testing.
type testLoopData struct{}

func (d *testLoopData) GetTask() []ContentPart               { return nil }
func (d *testLoopData) GetIterationHistory() []*Iteration    { return nil }
func (d *testLoopData) AddIterationHistory(iter *Iteration)  {}
func (d *testLoopData) GetScratchPad() []*Iteration          { return nil }
func (d *testLoopData) SetScratchPad(iterations []*Iteration) {}

func TestExecutionContext_SubscribeAll(t *testing.T) {
	ctx := NewExecutionContext("test", &testLoopData{})

	ch, unsub := ctx.SubscribeAll()
	defer unsub()

	go func() {
		ctx.EmitChunk(StreamChunk{Content: "hello"})
		ctx.EmitChunk(StreamChunk{Content: "world"})
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
					{Content: "skip", StreamId: "other"},
					{Content: "hello", StreamId: "my-stream"},
					{Content: "world", StreamId: "my-stream"},
				},
			},
			expected: expected{count: 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := NewExecutionContext("test", &testLoopData{})

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
					{Content: "skip", StreamTopicId: "other"},
					{Content: "hello", StreamTopicId: "llm-response"},
					{Content: "world", StreamTopicId: "llm-response"},
				},
			},
			expected: expected{count: 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := NewExecutionContext("test", &testLoopData{})

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
					ctx := NewExecutionContext("main", &testLoopData{})
					ctx.StartIteration()
					return ctx
				},
			},
			expected: expected{path: "main/1"},
		},
		{
			name: "child context",
			input: input{
				setupFn: func() *ExecutionContext {
					parent := NewExecutionContext("main", &testLoopData{})
					parent.StartIteration()
					parent.StartIteration() // iteration 2

					child := parent.SpawnChild("research", &testLoopData{})
					child.StartIteration()
					return child
				},
			},
			expected: expected{path: "main/2/research/1"},
		},
		{
			name: "deeply nested context",
			input: input{
				setupFn: func() *ExecutionContext {
					root := NewExecutionContext("main", &testLoopData{})
					root.StartIteration()

					child1 := root.SpawnChild("orchestrator", &testLoopData{})
					child1.StartIteration()
					child1.StartIteration()
					child1.StartIteration() // iteration 3

					child2 := child1.SpawnChild("worker", &testLoopData{})
					child2.StartIteration()
					child2.StartIteration() // iteration 2
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
	ctx := NewExecutionContext("test", &testLoopData{})
	ctx.StartIteration()

	ch, unsub := ctx.SubscribeAll()
	defer unsub()

	go func() {
		ctx.EmitChunk(StreamChunk{Content: "hello"})
		ctx.CloseStreams()
	}()

	chunk := <-ch
	assert.Equal(t, "test/1", chunk.Source)
}

func TestExecutionContext_EmitChunk_PreservesExistingSource(t *testing.T) {
	ctx := NewExecutionContext("test", &testLoopData{})
	ctx.StartIteration()

	ch, unsub := ctx.SubscribeAll()
	defer unsub()

	go func() {
		ctx.EmitChunk(StreamChunk{Content: "hello", Source: "custom/path"})
		ctx.CloseStreams()
	}()

	chunk := <-ch
	assert.Equal(t, "custom/path", chunk.Source)
}

func TestExecutionContext_ParentPropagation(t *testing.T) {
	parent := NewExecutionContext("parent", &testLoopData{})
	parent.StartIteration()

	child := parent.SpawnChild("child", &testLoopData{})
	child.StartIteration()

	parentCh, parentUnsub := parent.SubscribeAll()
	defer parentUnsub()

	go func() {
		child.EmitChunk(StreamChunk{Content: "from child"})
		parent.CloseStreams()
		child.CloseStreams()
	}()

	chunk := <-parentCh
	assert.Equal(t, "from child", chunk.Content)
	assert.Equal(t, "parent/1/child/1", chunk.Source)
}

func TestExecutionContext_ParentPropagation_MultipleLevels(t *testing.T) {
	root := NewExecutionContext("root", &testLoopData{})
	root.StartIteration()

	child := root.SpawnChild("child", &testLoopData{})
	child.StartIteration()

	grandchild := child.SpawnChild("grandchild", &testLoopData{})
	grandchild.StartIteration()

	rootCh, rootUnsub := root.SubscribeAll()
	defer rootUnsub()

	go func() {
		grandchild.EmitChunk(StreamChunk{Content: "from grandchild"})
		root.CloseStreams()
		child.CloseStreams()
		grandchild.CloseStreams()
	}()

	chunk := <-rootCh
	assert.Equal(t, "from grandchild", chunk.Content)
	assert.Equal(t, "root/1/child/1/grandchild/1", chunk.Source)
}

func TestExecutionContext_ConcurrentChildEmit(t *testing.T) {
	root := NewExecutionContext("root", &testLoopData{})
	root.StartIteration()

	const numChildren = 5
	const chunksPerChild = 10

	children := make([]*ExecutionContext, numChildren)
	for i := range numChildren {
		children[i] = root.SpawnChild("child", &testLoopData{})
		children[i].StartIteration()
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
					Content:  "chunk",
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
	ctx := NewExecutionContext("test", &testLoopData{})

	ch, unsub := ctx.SubscribeAll()

	go func() {
		for i := range 100 {
			ctx.EmitChunk(StreamChunk{Content: string(rune('A' + i%26))})
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
	ctx := NewExecutionContext("test", &testLoopData{})
	ctx.CloseStreams()
	ctx.CloseStreams() // Should not panic
}

func TestExecutionContext_MultipleSubscribers_SameTopic(t *testing.T) {
	ctx := NewExecutionContext("test", &testLoopData{})

	ch1, unsub1 := ctx.SubscribeToTopic("topic")
	ch2, unsub2 := ctx.SubscribeToTopic("topic")
	defer unsub1()
	defer unsub2()

	go func() {
		ctx.EmitChunk(StreamChunk{Content: "hello", StreamTopicId: "topic"})
		ctx.CloseStreams()
	}()

	chunk1 := <-ch1
	chunk2 := <-ch2

	assert.Equal(t, "hello", chunk1.Content)
	assert.Equal(t, "hello", chunk2.Content)
}

func TestExecutionContext_NoListener_EmitDoesNotBlock(t *testing.T) {
	ctx := NewExecutionContext("test", &testLoopData{})

	const numChunks = 1000
	start := time.Now()

	for i := range numChunks {
		ctx.EmitChunk(StreamChunk{Content: string(rune('A' + i%26))})
	}

	elapsed := time.Since(start)
	ctx.CloseStreams()

	assert.Less(t, elapsed, 100*time.Millisecond,
		"EmitChunk blocked without listeners: took %v", elapsed)
}

func TestExecutionContext_SlowListener_DoesNotBlockEmitter(t *testing.T) {
	ctx := NewExecutionContext("test", &testLoopData{})

	ch, unsub := ctx.SubscribeAll()
	defer unsub()

	const numChunks = 100
	emitDone := make(chan time.Duration, 1)

	go func() {
		start := time.Now()
		for i := range numChunks {
			ctx.EmitChunk(StreamChunk{Content: string(rune('A' + i%26))})
		}
		emitDone <- time.Since(start)
		ctx.CloseStreams()
	}()

	var received int
	for range ch {
		received++
		time.Sleep(10 * time.Millisecond)
	}

	assert.Equal(t, numChunks, received)

	emitElapsed := <-emitDone
	assert.Less(t, emitElapsed, 50*time.Millisecond,
		"EmitChunk was blocked by slow listener: emit took %v", emitElapsed)
}

func TestExecutionContext_SlowListener_DoesNotAffectFastListener(t *testing.T) {
	ctx := NewExecutionContext("test", &testLoopData{})

	slowCh, slowUnsub := ctx.SubscribeAll()
	fastCh, fastUnsub := ctx.SubscribeAll()
	defer slowUnsub()
	defer fastUnsub()

	const numChunks = 50
	fastDone := make(chan time.Duration, 1)
	slowDone := make(chan int, 1)

	go func() {
		for i := range numChunks {
			ctx.EmitChunk(StreamChunk{Content: string(rune('A' + i%26))})
		}
		ctx.CloseStreams()
	}()

	go func() {
		count := 0
		for range slowCh {
			count++
			time.Sleep(20 * time.Millisecond)
		}
		slowDone <- count
	}()

	go func() {
		start := time.Now()
		for range fastCh {
			// Process instantly
		}
		fastDone <- time.Since(start)
	}()

	fastElapsed := <-fastDone
	assert.Less(t, fastElapsed, 50*time.Millisecond,
		"fast listener was blocked by slow listener: took %v", fastElapsed)

	slowReceived := <-slowDone
	assert.Equal(t, numChunks, slowReceived)
}

func TestExecutionContext_SlowListener_ParentPropagation_DoesNotBlockChild(t *testing.T) {
	parent := NewExecutionContext("parent", &testLoopData{})
	parent.StartIteration()

	child := parent.SpawnChild("child", &testLoopData{})
	child.StartIteration()

	parentCh, parentUnsub := parent.SubscribeAll()
	defer parentUnsub()

	const numChunks = 100
	emitDone := make(chan time.Duration, 1)

	go func() {
		start := time.Now()
		for i := range numChunks {
			child.EmitChunk(StreamChunk{Content: string(rune('A' + i%26))})
		}
		emitDone <- time.Since(start)
		parent.CloseStreams()
		child.CloseStreams()
	}()

	var received int
	for range parentCh {
		received++
		time.Sleep(10 * time.Millisecond)
	}

	emitElapsed := <-emitDone
	assert.Less(t, emitElapsed, 50*time.Millisecond,
		"child EmitChunk was blocked by slow parent listener: took %v", emitElapsed)

	assert.Equal(t, numChunks, received)
}
