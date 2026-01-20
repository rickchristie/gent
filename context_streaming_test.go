package gent

import (
	"sync"
	"testing"
	"time"
)

// testLoopData is a simple LoopData implementation for testing.
type testLoopData struct{}

func (d *testLoopData) GetOriginalInput() []ContentPart               { return nil }
func (d *testLoopData) GetIterationHistory() []*Iteration             { return nil }
func (d *testLoopData) AddIterationHistory(iter *Iteration)           {}
func (d *testLoopData) GetIterations() []*Iteration                   { return nil }
func (d *testLoopData) SetIterations(iterations []*Iteration)         {}

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

	if len(chunks) != 2 {
		t.Errorf("expected 2 chunks, got %d", len(chunks))
	}
}

func TestExecutionContext_SubscribeToStream(t *testing.T) {
	ctx := NewExecutionContext("test", &testLoopData{})

	ch, unsub := ctx.SubscribeToStream("my-stream")
	defer unsub()

	go func() {
		ctx.EmitChunk(StreamChunk{Content: "skip", StreamId: "other"})
		ctx.EmitChunk(StreamChunk{Content: "hello", StreamId: "my-stream"})
		ctx.EmitChunk(StreamChunk{Content: "world", StreamId: "my-stream"})
		ctx.CloseStreams()
	}()

	var chunks []StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	if len(chunks) != 2 {
		t.Errorf("expected 2 chunks, got %d", len(chunks))
	}
}

func TestExecutionContext_SubscribeToTopic(t *testing.T) {
	ctx := NewExecutionContext("test", &testLoopData{})

	ch, unsub := ctx.SubscribeToTopic("llm-response")
	defer unsub()

	go func() {
		ctx.EmitChunk(StreamChunk{Content: "skip", StreamTopicId: "other"})
		ctx.EmitChunk(StreamChunk{Content: "hello", StreamTopicId: "llm-response"})
		ctx.EmitChunk(StreamChunk{Content: "world", StreamTopicId: "llm-response"})
		ctx.CloseStreams()
	}()

	var chunks []StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	if len(chunks) != 2 {
		t.Errorf("expected 2 chunks, got %d", len(chunks))
	}
}

func TestExecutionContext_BuildSourcePath_Root(t *testing.T) {
	ctx := NewExecutionContext("main", &testLoopData{})
	ctx.StartIteration()

	path := ctx.BuildSourcePath()
	expected := "main/1"
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

func TestExecutionContext_BuildSourcePath_Child(t *testing.T) {
	parent := NewExecutionContext("main", &testLoopData{})
	parent.StartIteration()
	parent.StartIteration() // iteration 2

	child := parent.SpawnChild("research", &testLoopData{})
	child.StartIteration()

	path := child.BuildSourcePath()
	expected := "main/2/research/1"
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

func TestExecutionContext_BuildSourcePath_DeepNesting(t *testing.T) {
	root := NewExecutionContext("main", &testLoopData{})
	root.StartIteration()

	child1 := root.SpawnChild("orchestrator", &testLoopData{})
	child1.StartIteration()
	child1.StartIteration()
	child1.StartIteration() // iteration 3

	child2 := child1.SpawnChild("worker", &testLoopData{})
	child2.StartIteration()
	child2.StartIteration() // iteration 2

	path := child2.BuildSourcePath()
	expected := "main/1/orchestrator/3/worker/2"
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
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
	expected := "test/1"
	if chunk.Source != expected {
		t.Errorf("expected source %q, got %q", expected, chunk.Source)
	}
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
	if chunk.Source != "custom/path" {
		t.Errorf("expected source %q, got %q", "custom/path", chunk.Source)
	}
}

func TestExecutionContext_ParentPropagation(t *testing.T) {
	parent := NewExecutionContext("parent", &testLoopData{})
	parent.StartIteration()

	child := parent.SpawnChild("child", &testLoopData{})
	child.StartIteration()

	// Subscribe at parent level
	parentCh, parentUnsub := parent.SubscribeAll()
	defer parentUnsub()

	// Emit from child
	go func() {
		child.EmitChunk(StreamChunk{Content: "from child"})
		parent.CloseStreams()
		child.CloseStreams()
	}()

	// Parent should receive chunk from child
	chunk := <-parentCh
	if chunk.Content != "from child" {
		t.Errorf("expected 'from child', got %q", chunk.Content)
	}
	if chunk.Source != "parent/1/child/1" {
		t.Errorf("expected source 'parent/1/child/1', got %q", chunk.Source)
	}
}

func TestExecutionContext_ParentPropagation_MultipleLevels(t *testing.T) {
	root := NewExecutionContext("root", &testLoopData{})
	root.StartIteration()

	child := root.SpawnChild("child", &testLoopData{})
	child.StartIteration()

	grandchild := child.SpawnChild("grandchild", &testLoopData{})
	grandchild.StartIteration()

	// Subscribe at root level
	rootCh, rootUnsub := root.SubscribeAll()
	defer rootUnsub()

	// Emit from grandchild
	go func() {
		grandchild.EmitChunk(StreamChunk{Content: "from grandchild"})
		root.CloseStreams()
		child.CloseStreams()
		grandchild.CloseStreams()
	}()

	// Root should receive chunk from grandchild
	chunk := <-rootCh
	if chunk.Content != "from grandchild" {
		t.Errorf("expected 'from grandchild', got %q", chunk.Content)
	}
	if chunk.Source != "root/1/child/1/grandchild/1" {
		t.Errorf("expected source 'root/1/child/1/grandchild/1', got %q", chunk.Source)
	}
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

	// Subscribe at root
	rootCh, rootUnsub := root.SubscribeAll()
	defer rootUnsub()

	var wg sync.WaitGroup
	wg.Add(numChildren)

	// Emit concurrently from all children
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

	// Count received chunks
	count := 0
	for range rootCh {
		count++
	}

	expected := numChildren * chunksPerChild
	if count != expected {
		t.Errorf("expected %d chunks, got %d", expected, count)
	}
}

func TestExecutionContext_EarlyUnsubscribe(t *testing.T) {
	ctx := NewExecutionContext("test", &testLoopData{})

	ch, unsub := ctx.SubscribeAll()

	// Emit some chunks
	go func() {
		for i := range 100 {
			ctx.EmitChunk(StreamChunk{Content: string(rune('A' + i%26))})
			time.Sleep(time.Millisecond)
		}
		ctx.CloseStreams()
	}()

	// Read a few then unsubscribe
	count := 0
	for range ch {
		count++
		if count >= 5 {
			unsub()
			break
		}
	}

	// Channel should be closed after unsubscribe
	// Wait a bit to ensure no more chunks arrive
	time.Sleep(50 * time.Millisecond)

	// The channel should be closed now
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel to be closed after unsubscribe")
		}
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

	// Both should receive the chunk
	chunk1 := <-ch1
	chunk2 := <-ch2

	if chunk1.Content != "hello" || chunk2.Content != "hello" {
		t.Errorf("expected both to receive 'hello'")
	}
}
